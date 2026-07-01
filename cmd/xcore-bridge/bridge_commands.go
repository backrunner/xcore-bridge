package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/backrunner/xcore-bridge/internal/bridge"
	"github.com/backrunner/xcore-bridge/internal/daemon"
	"github.com/backrunner/xcore-bridge/internal/surge"
	"github.com/backrunner/xcore-bridge/internal/vless"
)

var (
	startBridgeServer    = bridge.Start
	getDaemonStatus      = daemon.GetStatus
	waitForDaemonPolicy  = daemon.WaitForPolicy
	bridgeRuntimeLogPath = daemon.BridgeLogPath
	appendRuntimeLog     = daemon.AppendBridgeLog
)

func runCommand(args []string, stdout, stderr io.Writer) error {
	if stderr == nil {
		stderr = io.Discard
	}
	_ = daemon.AppendBridgeLog("run invoked args=%d", len(args))
	fs := flag.NewFlagSet("run", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	profile := fs.String("profile", "", "Surge profile path; auto-detected when omitted")
	localPort := fs.Int("local-port", 0, "SOCKS5 port Surge will connect to")
	localHost := fs.String("listen", "127.0.0.1", "SOCKS5 listen address")
	link := fs.String("link", "", "VLESS share link")
	logLevel := fs.String("log-level", "warning", "Xray log level")
	if err := fs.Parse(args); err != nil {
		_ = daemon.AppendBridgeLog("run flag parse failed error=%q", err)
		return err
	}
	shareLink, err := oneLinkArg(*link, fs.Args(), "run")
	if err != nil {
		_ = daemon.AppendBridgeLog("run link argument failed error=%q", err)
		return err
	}
	if shareLink == "" {
		_ = daemon.AppendBridgeLog("run missing link")
		return errors.New("run requires --link or a positional VLESS share link")
	}
	if *localPort <= 0 || *localPort > 65535 {
		_ = daemon.AppendBridgeLog("run invalid local port port=%d", *localPort)
		return errors.New("run requires --local-port in 1..65535")
	}

	node, err := vless.Parse(shareLink)
	if err != nil {
		_ = daemon.AppendBridgeLog("run parse link failed port=%d error=%q", *localPort, err)
		return err
	}
	_ = daemon.AppendBridgeLog("run starting policy=%q socks=%s:%d", node.DisplayName(), *localHost, *localPort)
	profilePath, err := selectedProfilePath(*profile, stderr, "run")
	if err != nil {
		_ = daemon.AppendBridgeLog("run profile selection failed policy=%q error=%q", node.DisplayName(), err)
		return err
	}
	if err := verifyManagedRunTarget(profilePath, shareLink, *localHost, *localPort); err != nil {
		_ = daemon.AppendBridgeLog("run target verification failed policy=%q profile=%q socks=%s:%d error=%q", node.DisplayName(), profilePath, *localHost, *localPort, err)
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return runManagedPolicy(ctx, node, profilePath, *localHost, *localPort, *logLevel, stdout)
}

func runManagedPolicy(ctx context.Context, node vless.Node, profilePath, localHost string, localPort int, logLevel string, stdout io.Writer) error {
	logPath, err := bridgeRuntimeLogPath()
	if err != nil {
		_ = appendRuntimeLog("run log path failed policy=%q profile=%q error=%q", node.DisplayName(), profilePath, err)
		logPath = ""
	} else if err := appendRuntimeLog("run xray logs path=%q policy=%q profile=%q", logPath, node.DisplayName(), profilePath); err != nil {
		logPath = ""
	}
	status, statusErr := getDaemonStatus(daemon.Options{ProfilePath: profilePath})
	if statusErr != nil {
		_ = appendRuntimeLog("run daemon status failed policy=%q profile=%q error=%q", node.DisplayName(), profilePath, statusErr)
	} else if status.Running {
		if _, ok := matchingDaemonPolicy(status, profilePath, localHost, localPort, node.Raw); ok {
			return runManagedDaemonPolicy(ctx, status, node, profilePath, localHost, localPort, stdout)
		}
		if status.ProfilePath == profilePath {
			return fmt.Errorf("daemon is running for %s but does not expose %s; restart daemon or stop it before Surge starts this policy", profilePath, netJoin(localHost, localPort))
		}
		if _, ok := daemonPolicyOnPort(status, localHost, localPort); ok {
			return fmt.Errorf("daemon is already using %s for %s; stop or restart daemon before Surge starts this policy", netJoin(localHost, localPort), status.ProfilePath)
		}
	}
	server, err := startBridgeServer(ctx, bridge.Config{
		Node:          node,
		LocalHost:     localHost,
		LocalPort:     localPort,
		LogLevel:      logLevel,
		AccessLogPath: logPath,
		ErrorLogPath:  logPath,
	})
	if err != nil {
		_ = appendRuntimeLog("run xray start failed policy=%q profile=%q socks=%s:%d error=%q", node.DisplayName(), profilePath, localHost, localPort, err)
		return err
	}
	defer func() {
		if err := server.Close(); err != nil {
			_ = appendRuntimeLog("run xray close failed policy=%q profile=%q error=%q", node.DisplayName(), profilePath, err)
		}
	}()
	_ = appendRuntimeLog("run ready policy=%q profile=%q socks=%s:%d pid=%d", node.DisplayName(), profilePath, localHost, localPort, os.Getpid())
	ui := newUI(stdout)
	ui.Success("xcore-bridge ready")
	ui.KeyValue("policy", node.DisplayName())
	ui.KeyValue("socks5", fmt.Sprintf("%s:%d", localHost, localPort))
	ui.KeyValue("pid", fmt.Sprintf("%d", os.Getpid()))
	<-ctx.Done()
	_ = appendRuntimeLog("run stopping policy=%q profile=%q reason=%q", node.DisplayName(), profilePath, ctx.Err())
	return nil
}

func runManagedDaemonPolicy(ctx context.Context, status daemon.Status, node vless.Node, profilePath, localHost string, localPort int, stdout io.Writer) error {
	if err := waitForDaemonPolicy(ctx, localHost, localPort, 2*time.Second); err != nil {
		if ctx.Err() != nil {
			return nil
		}
		_ = appendRuntimeLog("run daemon policy not ready policy=%q profile=%q daemon_pid=%d socks=%s:%d error=%q", node.DisplayName(), profilePath, status.PID, localHost, localPort, err)
		return fmt.Errorf("daemon policy %s is not ready: %w", netJoin(localHost, localPort), err)
	}
	_ = appendRuntimeLog("run reusing daemon policy=%q profile=%q daemon_pid=%d socks=%s:%d pid=%d", node.DisplayName(), profilePath, status.PID, localHost, localPort, os.Getpid())
	ui := newUI(stdout)
	ui.Success("xcore-bridge ready")
	ui.KeyValue("policy", node.DisplayName())
	ui.KeyValue("socks5", fmt.Sprintf("%s:%d", localHost, localPort))
	ui.KeyValue("pid", fmt.Sprintf("%d", os.Getpid()))
	ui.KeyValue("mode", "daemon")
	ui.KeyValue("daemon-pid", fmt.Sprintf("%d", status.PID))

	<-ctx.Done()
	_ = appendRuntimeLog("run daemon proxy stopping policy=%q profile=%q reason=%q", node.DisplayName(), profilePath, ctx.Err())
	return nil
}

func matchingDaemonPolicy(status daemon.Status, profilePath, localHost string, localPort int, link string) (daemon.Policy, bool) {
	if !status.Running || status.ProfilePath != profilePath {
		return daemon.Policy{}, false
	}
	policy, ok := daemonPolicyOnPort(status, localHost, localPort)
	if !ok || policy.LinkHash == "" || policy.LinkHash != daemon.PolicyLinkHash(link) {
		return daemon.Policy{}, false
	}
	return policy, true
}

func daemonPolicyOnPort(status daemon.Status, localHost string, localPort int) (daemon.Policy, bool) {
	if localHost == "" {
		localHost = "127.0.0.1"
	}
	for _, policy := range status.Policies {
		host := policy.LocalHost
		if host == "" {
			host = "127.0.0.1"
		}
		if host == localHost && policy.LocalPort == localPort {
			return policy, true
		}
	}
	return daemon.Policy{}, false
}

func netJoin(host string, port int) string {
	if host == "" {
		host = "127.0.0.1"
	}
	return net.JoinHostPort(host, strconv.Itoa(port))
}

func verifyManagedRunTarget(profilePath, link, host string, port int) error {
	policies, err := surge.ManagedPolicies(profilePath)
	if err != nil {
		return err
	}
	if host == "" {
		host = "127.0.0.1"
	}
	for _, policy := range policies {
		runHost := policy.RunHost
		if runHost == "" {
			runHost = "127.0.0.1"
		}
		if policy.Link == strings.TrimSpace(link) && runHost == host && policy.RunPort == port {
			return nil
		}
	}
	return fmt.Errorf("run target is not managed by %s", profilePath)
}

func xrayConfigCommand(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("xray-config", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	localPort := fs.Int("local-port", 1080, "SOCKS5 listen port")
	localHost := fs.String("listen", "127.0.0.1", "SOCKS5 listen address")
	link := fs.String("link", "", "VLESS share link")
	logLevel := fs.String("log-level", "warning", "Xray log level")
	if err := fs.Parse(args); err != nil {
		return err
	}
	shareLink, err := oneLinkArg(*link, fs.Args(), "xray-config")
	if err != nil {
		return err
	}
	if shareLink == "" {
		return errors.New("xray-config requires --link or a positional VLESS share link")
	}
	node, err := vless.Parse(shareLink)
	if err != nil {
		return err
	}
	data, err := bridge.JSONConfig(bridge.Config{
		Node:      node,
		LocalHost: *localHost,
		LocalPort: *localPort,
		LogLevel:  *logLevel,
	})
	if err != nil {
		return err
	}
	_, err = stdout.Write(append(data, '\n'))
	return err
}

func statusCommand(args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("status", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	profile := fs.String("profile", "", "Surge profile path")
	if err := fs.Parse(args); err != nil {
		return err
	}
	status, err := daemon.GetStatus(daemon.Options{ProfilePath: strings.TrimSpace(*profile)})
	if err != nil {
		return err
	}
	printDaemonStatus(stdout, status)
	return nil
}

func daemonCommand(args []string, stdout, stderr io.Writer) error {
	if len(args) == 0 {
		return errors.New("daemon requires start, stop, restart, install, uninstall, status, log, or serve")
	}
	switch args[0] {
	case "start":
		return daemonStartCommand(args[1:], stdout, stderr)
	case "stop":
		return daemonStopCommand(args[1:], stdout, stderr)
	case "restart":
		return daemonRestartCommand(args[1:], stdout, stderr)
	case "install":
		return daemonInstallCommand(args[1:], stdout, stderr)
	case "uninstall":
		return daemonUninstallCommand(args[1:], stdout, stderr)
	case "status":
		return statusCommand(args[1:], stdout, stderr)
	case "log":
		return daemonLogCommand(args[1:], stdout, stderr)
	case "serve":
		return daemonServeCommand(args[1:])
	default:
		return fmt.Errorf("unknown daemon command %q", args[0])
	}
}

func daemonStartCommand(args []string, stdout, stderr io.Writer) error {
	opts, err := daemonControlOptions("daemon start", args, stderr)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), opts.Timeout+time.Second)
	defer cancel()
	status, err := daemon.Start(ctx, opts)
	if err != nil {
		return err
	}
	printDaemonStatus(stdout, status)
	return nil
}

func daemonStopCommand(args []string, stdout, stderr io.Writer) error {
	opts, err := daemonStopOptions(args)
	if err != nil {
		return err
	}
	status, err := daemon.Stop(opts)
	if err != nil {
		return err
	}
	if status.PID == 0 || !status.Running {
		newUI(stdout).Success("daemon stopped")
		return nil
	}
	ui := newUI(stdout)
	ui.Success("daemon stopped")
	ui.KeyValue("pid", fmt.Sprintf("%d", status.PID))
	return nil
}

func daemonRestartCommand(args []string, stdout, stderr io.Writer) error {
	opts, err := daemonControlOptions("daemon restart", args, stderr)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), opts.Timeout+time.Second)
	defer cancel()
	status, err := daemon.Restart(ctx, opts)
	if err != nil {
		return err
	}
	printDaemonStatus(stdout, status)
	return nil
}

func daemonInstallCommand(args []string, stdout, stderr io.Writer) error {
	opts, err := daemonControlOptions("daemon install", args, stderr)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), opts.Timeout+time.Second)
	defer cancel()
	info, status, err := daemon.InstallLaunchAgent(ctx, opts)
	if err != nil {
		return err
	}
	ui := newUI(stdout)
	ui.Success("daemon launch agent installed")
	ui.KeyValue("label", info.Label)
	ui.KeyValue("plist", info.Path)
	printDaemonStatus(stdout, status)
	return nil
}

func daemonUninstallCommand(args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("daemon uninstall", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	timeout := fs.Duration("timeout", 5*time.Second, "launchd uninstall timeout")
	if err := fs.Parse(args); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), *timeout+time.Second)
	defer cancel()
	info, _, err := daemon.UninstallLaunchAgent(ctx)
	if err != nil {
		return err
	}
	ui := newUI(stdout)
	ui.Success("daemon launch agent uninstalled")
	ui.KeyValue("label", info.Label)
	ui.KeyValue("plist", info.Path)
	return nil
}

func daemonServeCommand(args []string) error {
	fs := flag.NewFlagSet("daemon serve", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	profile := fs.String("profile", "", "Surge profile path")
	logLevel := fs.String("log-level", "warning", "Xray log level")
	if err := fs.Parse(args); err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return daemon.Serve(ctx, daemon.Options{
		ProfilePath: strings.TrimSpace(*profile),
		LogLevel:    *logLevel,
	})
}

func daemonControlOptions(command string, args []string, stderr io.Writer) (daemon.Options, error) {
	if stderr == nil {
		stderr = io.Discard
	}
	fs := flag.NewFlagSet(command, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	profile := fs.String("profile", "", "Surge profile path; auto-detected when omitted")
	execPath := fs.String("exec", defaultExecPath(), "path to xcore-bridge executable")
	logLevel := fs.String("log-level", "warning", "Xray log level")
	timeout := fs.Duration("timeout", 5*time.Second, "daemon start/stop timeout")
	if err := fs.Parse(args); err != nil {
		return daemon.Options{}, err
	}
	profilePath, err := selectedProfilePath(*profile, stderr, command)
	if err != nil {
		return daemon.Options{}, err
	}
	return daemon.Options{
		ProfilePath: profilePath,
		ExecPath:    *execPath,
		LogLevel:    *logLevel,
		Timeout:     *timeout,
	}, nil
}

func daemonStopOptions(args []string) (daemon.Options, error) {
	fs := flag.NewFlagSet("daemon stop", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	profile := fs.String("profile", "", "Surge profile path")
	timeout := fs.Duration("timeout", 5*time.Second, "daemon stop timeout")
	if err := fs.Parse(args); err != nil {
		return daemon.Options{}, err
	}
	return daemon.Options{
		ProfilePath: strings.TrimSpace(*profile),
		Timeout:     *timeout,
	}, nil
}

func printDaemonStatus(stdout io.Writer, status daemon.Status) {
	ui := newUI(stdout)
	ui.Title("xcore-bridge daemon")
	if status.Running {
		ui.Success("daemon running")
		ui.KeyValue("pid", fmt.Sprintf("%d", status.PID))
		if status.ProfilePath != "" {
			ui.KeyValue("profile", status.ProfilePath)
		}
	} else if status.StalePID {
		ui.Warn("daemon stopped (stale pid=%d)", status.PID)
	} else {
		ui.Info("daemon stopped")
	}
	if status.LaunchAgent {
		ui.KeyValue("launchd", "installed")
	}
	if status.Error != "" {
		ui.Warn("%s", status.Error)
	}
	if len(status.Policies) > 0 {
		ui.Info("policies")
	}
	for _, policy := range status.Policies {
		ui.Item(policy.Name, "socks5="+policy.LocalHost+":"+strconv.Itoa(policy.LocalPort))
	}
}

func oneLinkArg(flagValue string, positional []string, command string) (string, error) {
	flagValue = strings.TrimSpace(flagValue)
	if flagValue != "" {
		if len(positional) > 0 {
			return "", fmt.Errorf("%s accepts either --link or one positional link, not both", command)
		}
		return flagValue, nil
	}
	if len(positional) == 0 {
		return "", nil
	}
	if len(positional) > 1 {
		return "", fmt.Errorf("%s accepts exactly one positional VLESS share link", command)
	}
	return strings.TrimSpace(positional[0]), nil
}
