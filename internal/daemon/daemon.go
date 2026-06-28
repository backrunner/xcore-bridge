package daemon

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/backrunner/xcore-bridge/internal/bridge"
	"github.com/backrunner/xcore-bridge/internal/surge"
	"github.com/backrunner/xcore-bridge/internal/vless"
)

const defaultLogLevel = "warning"

var startBridgeMulti = bridge.StartMulti

type RuntimePaths struct {
	Dir       string
	PID       string
	State     string
	Log       string
	BridgeLog string
	Lock      string
	Agent     string
}

type Options struct {
	ProfilePath string
	ExecPath    string
	LogLevel    string
	Timeout     time.Duration
}

type Status struct {
	Running     bool
	PID         int
	ProfilePath string
	Policies    []Policy
	StalePID    bool
	LaunchAgent bool
	Error       string
}

type LaunchAgentInfo struct {
	Label string
	Path  string
}

type Policy struct {
	Name      string
	LocalHost string
	LocalPort int
	LinkHash  string
}

type stateFile struct {
	PID         int       `json:"pid"`
	ProfilePath string    `json:"profilePath"`
	StartedAt   time.Time `json:"startedAt"`
	Policies    []Policy  `json:"policies"`
}

func Start(ctx context.Context, opts Options) (Status, error) {
	_ = AppendBridgeLog("start requested profile=%q", opts.ProfilePath)
	return withControlLock(ctx, opts, func() (Status, error) {
		status, err := startLocked(ctx, opts)
		if err != nil {
			_ = AppendBridgeLog("start failed profile=%q error=%q", opts.ProfilePath, err)
			return status, err
		}
		_ = AppendBridgeLog("start ready profile=%q pid=%d", status.ProfilePath, status.PID)
		return status, nil
	})
}

func startLocked(ctx context.Context, opts Options) (Status, error) {
	if err := validateProfile(opts.ProfilePath); err != nil {
		return Status{}, err
	}
	status, _ := GetStatus(opts)
	if status.LaunchAgent {
		if status.Running && policyReady(ctx, status, opts) {
			return status, nil
		}
		return StartLaunchAgent(ctx, opts)
	}
	if status.Running {
		if policyReady(ctx, status, opts) {
			return status, nil
		}
		return status, fmt.Errorf("daemon already running for %s; use daemon restart to switch profiles", status.ProfilePath)
	}
	return startFresh(ctx, opts)
}

func Serve(ctx context.Context, opts Options) error {
	_ = AppendDaemonLog("serve starting profile=%q logLevel=%q", opts.ProfilePath, opts.LogLevel)
	if err := validateProfile(opts.ProfilePath); err != nil {
		_ = AppendDaemonLog("serve profile validation failed profile=%q error=%q", opts.ProfilePath, err)
		return err
	}
	paths, err := Paths()
	if err != nil {
		_ = AppendDaemonLog("serve paths failed error=%q", err)
		return err
	}
	if err := os.MkdirAll(paths.Dir, 0o755); err != nil {
		_ = AppendDaemonLog("serve mkdir failed dir=%q error=%q", paths.Dir, err)
		return err
	}
	cfg, policies, err := ConfigFromProfile(opts.ProfilePath, opts.LogLevel)
	if err != nil {
		_ = AppendDaemonLog("serve config failed profile=%q error=%q", opts.ProfilePath, err)
		return err
	}
	cfg.AccessLogPath = paths.Log
	cfg.ErrorLogPath = paths.Log
	_ = AppendDaemonLog("serve config loaded profile=%q policies=%d", opts.ProfilePath, len(policies))
	state := stateFile{
		PID:         os.Getpid(),
		ProfilePath: opts.ProfilePath,
		StartedAt:   time.Now(),
		Policies:    policies,
	}
	if err := writeState(paths, state); err != nil {
		_ = AppendDaemonLog("serve state write failed profile=%q error=%q", opts.ProfilePath, err)
		return err
	}
	defer cleanupState(paths, os.Getpid())
	server, err := startBridgeMulti(ctx, cfg)
	if err != nil {
		_ = AppendDaemonLog("serve xray start failed profile=%q error=%q", opts.ProfilePath, err)
		return err
	}
	defer func() {
		if err := server.Close(); err != nil {
			_ = AppendDaemonLog("serve xray close failed error=%q", err)
		}
	}()
	_ = AppendDaemonLog("serve ready profile=%q pid=%d policies=%d", opts.ProfilePath, os.Getpid(), len(policies))
	<-ctx.Done()
	_ = AppendDaemonLog("serve stopping profile=%q pid=%d reason=%q", opts.ProfilePath, os.Getpid(), ctx.Err())
	return nil
}

func Stop(opts Options) (Status, error) {
	_ = AppendBridgeLog("stop requested profile=%q", opts.ProfilePath)
	timeout := opts.Timeout
	if timeout == 0 {
		timeout = 3 * time.Second
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout+time.Second)
	defer cancel()
	return withControlLock(ctx, opts, func() (Status, error) {
		status, _ := GetStatus(opts)
		if status.LaunchAgent {
			if _, err := StopLaunchAgent(ctx); err != nil {
				_ = AppendBridgeLog("stop launch agent failed profile=%q error=%q", opts.ProfilePath, err)
				return status, err
			}
			if !status.Running {
				return status, nil
			}
		}
		status, err := stopLocked(opts)
		if err != nil {
			_ = AppendBridgeLog("stop failed profile=%q pid=%d error=%q", opts.ProfilePath, status.PID, err)
			return status, err
		}
		_ = AppendBridgeLog("stop complete profile=%q pid=%d running=%t", status.ProfilePath, status.PID, status.Running)
		return status, nil
	})
}

func stopLocked(opts Options) (Status, error) {
	status, err := GetStatus(opts)
	if err != nil {
		return status, err
	}
	if !status.Running {
		if status.StalePID {
			_ = removeRuntimeFiles()
		}
		return status, nil
	}
	process, err := os.FindProcess(status.PID)
	if err != nil {
		return status, err
	}
	if err := process.Signal(syscall.SIGTERM); err != nil && !errors.Is(err, os.ErrProcessDone) {
		return status, err
	}
	timeout := opts.Timeout
	if timeout == 0 {
		timeout = 3 * time.Second
	}
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		current, _ := GetStatus(opts)
		if !current.Running {
			_ = removeRuntimeFiles()
			return current, nil
		}
		time.Sleep(50 * time.Millisecond)
	}
	return status, fmt.Errorf("daemon pid %d did not stop within %s", status.PID, timeout)
}

func Restart(ctx context.Context, opts Options) (Status, error) {
	_ = AppendBridgeLog("restart requested profile=%q", opts.ProfilePath)
	return withControlLock(ctx, opts, func() (Status, error) {
		status, _ := GetStatus(opts)
		if status.LaunchAgent {
			if _, err := StopLaunchAgent(ctx); err != nil {
				_ = AppendBridgeLog("restart launch agent stop failed profile=%q error=%q", opts.ProfilePath, err)
				return status, err
			}
			status, err := StartLaunchAgent(ctx, opts)
			if err != nil {
				_ = AppendBridgeLog("restart launch agent start failed profile=%q error=%q", opts.ProfilePath, err)
				return status, err
			}
			_ = AppendBridgeLog("restart launch agent ready profile=%q pid=%d", status.ProfilePath, status.PID)
			return status, nil
		}
		if _, err := stopLocked(opts); err != nil {
			_ = AppendBridgeLog("restart stop failed profile=%q error=%q", opts.ProfilePath, err)
			return Status{}, err
		}
		status, err := startLocked(ctx, opts)
		if err != nil {
			_ = AppendBridgeLog("restart start failed profile=%q error=%q", opts.ProfilePath, err)
			return status, err
		}
		_ = AppendBridgeLog("restart ready profile=%q pid=%d", status.ProfilePath, status.PID)
		return status, nil
	})
}

func InstallLaunchAgent(ctx context.Context, opts Options) (LaunchAgentInfo, Status, error) {
	_ = AppendBridgeLog("launch agent install requested profile=%q", opts.ProfilePath)
	if err := validateProfile(opts.ProfilePath); err != nil {
		return LaunchAgentInfo{}, Status{}, err
	}
	execPath := strings.TrimSpace(opts.ExecPath)
	if execPath == "" {
		var err error
		execPath, err = os.Executable()
		if err != nil {
			return LaunchAgentInfo{}, Status{}, err
		}
	}
	existingStatus, _ := GetStatus(opts)
	info, err := writeLaunchAgent(opts, execPath)
	if err != nil {
		_ = AppendBridgeLog("launch agent write failed profile=%q error=%q", opts.ProfilePath, err)
		return info, Status{}, err
	}
	if existingStatus.Running && !existingStatus.LaunchAgent {
		if _, err := Stop(opts); err != nil {
			_ = AppendBridgeLog("launch agent stop manual daemon failed profile=%q error=%q", opts.ProfilePath, err)
			return info, existingStatus, err
		}
	}
	status, err := StartLaunchAgent(ctx, opts)
	if err != nil {
		_ = AppendBridgeLog("launch agent start failed profile=%q error=%q", opts.ProfilePath, err)
		return info, status, err
	}
	_ = AppendBridgeLog("launch agent ready profile=%q pid=%d path=%q", status.ProfilePath, status.PID, info.Path)
	return info, status, nil
}

func StartLaunchAgent(ctx context.Context, opts Options) (Status, error) {
	info, err := launchAgentInfo()
	if err != nil {
		return Status{}, err
	}
	if _, err := os.Stat(info.Path); err != nil {
		return Status{}, err
	}
	if err := runLaunchctl(ctx, "bootstrap", guiDomain(), info.Path); err != nil {
		_ = runLaunchctl(context.Background(), "bootout", guiDomain(), info.Path)
		if retryErr := runLaunchctl(ctx, "bootstrap", guiDomain(), info.Path); retryErr != nil {
			_ = AppendBridgeLog("launch agent bootstrap failed profile=%q error=%q", opts.ProfilePath, retryErr)
			return Status{}, fmt.Errorf("bootstrap launch agent: %w", retryErr)
		}
	}
	if err := runLaunchctl(ctx, "kickstart", "-k", guiDomain()+"/"+info.Label); err != nil {
		_ = AppendBridgeLog("launch agent kickstart failed profile=%q error=%q", opts.ProfilePath, err)
		return Status{}, fmt.Errorf("kickstart launch agent: %w", err)
	}
	if strings.TrimSpace(opts.ProfilePath) == "" {
		status, err := GetStatus(opts)
		status.LaunchAgent = true
		return status, err
	}
	status, err := waitForLaunchAgentReady(ctx, opts)
	if err != nil {
		_ = AppendBridgeLog("launch agent readiness failed profile=%q error=%q", opts.ProfilePath, err)
		return status, err
	}
	status.LaunchAgent = true
	return status, nil
}

func StopLaunchAgent(ctx context.Context) (LaunchAgentInfo, error) {
	info, err := launchAgentInfo()
	if err != nil {
		return LaunchAgentInfo{}, err
	}
	status, _ := GetStatus(Options{})
	if _, err := os.Stat(info.Path); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return info, nil
		}
		return info, err
	}
	if err := runLaunchctl(ctx, "bootout", guiDomain(), info.Path); err != nil && !isLaunchctlNoService(err) {
		return info, fmt.Errorf("bootout launch agent: %w", err)
	}
	if status.Running {
		if err := waitForStopped(ctx, status.PID); err != nil {
			return info, err
		}
	}
	return info, nil
}

func UninstallLaunchAgent(ctx context.Context) (LaunchAgentInfo, Status, error) {
	_ = AppendBridgeLog("launch agent uninstall requested")
	info, err := launchAgentInfo()
	if err != nil {
		return LaunchAgentInfo{}, Status{}, err
	}
	status, _ := GetStatus(Options{})
	if _, err := os.Stat(info.Path); err == nil {
		if _, err := StopLaunchAgent(ctx); err != nil {
			_ = AppendBridgeLog("launch agent bootout failed path=%q error=%q", info.Path, err)
			return info, status, err
		}
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return info, status, err
	}
	if err := os.Remove(info.Path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return info, status, err
	}
	status, _ = GetStatus(Options{})
	status.LaunchAgent = false
	_ = AppendBridgeLog("launch agent uninstalled path=%q", info.Path)
	return info, status, nil
}

func Ensure(ctx context.Context, opts Options) (Status, error) {
	_ = AppendBridgeLog("ensure requested profile=%q", opts.ProfilePath)
	return withControlLock(ctx, opts, func() (Status, error) {
		status, err := ensureLocked(ctx, opts)
		if err != nil {
			_ = AppendBridgeLog("ensure failed profile=%q pid=%d error=%q", opts.ProfilePath, status.PID, err)
			return status, err
		}
		_ = AppendBridgeLog("ensure ready profile=%q pid=%d policies=%d", status.ProfilePath, status.PID, len(status.Policies))
		return status, nil
	})
}

func ensureLocked(ctx context.Context, opts Options) (Status, error) {
	status, _ := GetStatus(opts)
	if status.Running && policyReady(ctx, status, opts) {
		_ = AppendBridgeLog("ensure reused daemon profile=%q pid=%d", status.ProfilePath, status.PID)
		return status, nil
	}
	if status.Running {
		_ = AppendBridgeLog("ensure restarting daemon profile=%q pid=%d", status.ProfilePath, status.PID)
		if _, err := stopLocked(opts); err != nil {
			return status, err
		}
	}
	return startFresh(ctx, opts)
}

func startFresh(ctx context.Context, opts Options) (Status, error) {
	if err := validateProfile(opts.ProfilePath); err != nil {
		return Status{}, err
	}
	paths, err := Paths()
	if err != nil {
		return Status{}, err
	}
	if err := os.MkdirAll(paths.Dir, 0o755); err != nil {
		return Status{}, err
	}
	execPath := strings.TrimSpace(opts.ExecPath)
	if execPath == "" {
		execPath, err = os.Executable()
		if err != nil {
			return Status{}, err
		}
	}
	logFile, err := os.OpenFile(paths.Log, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return Status{}, err
	}
	defer logFile.Close()

	args := []string{"daemon", "serve", "--profile", opts.ProfilePath}
	if opts.LogLevel != "" {
		args = append(args, "--log-level", opts.LogLevel)
	}
	cmd := exec.Command(execPath, args...)
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	cmd.Stdin = nil
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		_ = AppendBridgeLog("daemon child start failed profile=%q exec=%q error=%q", opts.ProfilePath, execPath, err)
		return Status{}, err
	}
	_ = AppendBridgeLog("daemon child started profile=%q exec=%q pid=%d", opts.ProfilePath, execPath, cmd.Process.Pid)
	released := false
	defer func() {
		if !released {
			_ = AppendBridgeLog("daemon child cleanup profile=%q pid=%d", opts.ProfilePath, cmd.Process.Pid)
			_ = cmd.Process.Signal(syscall.SIGTERM)
			_ = cmd.Process.Release()
		}
	}()

	timeout := opts.Timeout
	if timeout == 0 {
		timeout = 3 * time.Second
	}
	deadline := time.Now().Add(timeout)
	var last Status
	for time.Now().Before(deadline) {
		current, err := GetStatus(opts)
		if err == nil && current.Running && current.ProfilePath == opts.ProfilePath {
			if ready(ctx, current.Policies, 100*time.Millisecond) {
				_ = cmd.Process.Release()
				released = true
				_ = AppendBridgeLog("daemon child ready profile=%q pid=%d policies=%d", current.ProfilePath, current.PID, len(current.Policies))
				return current, nil
			}
		}
		last = current
		time.Sleep(50 * time.Millisecond)
	}
	if last.Error != "" {
		return last, fmt.Errorf("daemon did not become ready: %s", last.Error)
	}
	return last, fmt.Errorf("daemon did not become ready within %s", timeout)
}

func GetStatus(opts Options) (Status, error) {
	paths, err := Paths()
	if err != nil {
		return Status{}, err
	}
	state, stateErr := readState(paths)
	pid := state.PID
	if pid == 0 {
		pid = readPID(paths)
	}
	status := Status{
		PID:         pid,
		ProfilePath: state.ProfilePath,
		Policies:    state.Policies,
		LaunchAgent: launchAgentInstalled(),
	}
	if status.ProfilePath == "" {
		status.ProfilePath = opts.ProfilePath
	}
	if stateErr != nil && !errors.Is(stateErr, os.ErrNotExist) {
		status.Error = stateErr.Error()
	}
	if pid == 0 {
		return status, nil
	}
	running := processRunning(pid)
	status.Running = running
	status.StalePID = !running
	status.LaunchAgent = launchAgentInstalled()
	if running && len(status.Policies) > 0 {
		if err := readyError(context.Background(), status.Policies, 100*time.Millisecond); err != nil {
			status.Error = "daemon process is running but SOCKS5 listeners are not ready: " + err.Error()
		}
	}
	return status, nil
}

func ConfigFromProfile(profilePath, logLevel string) (bridge.MultiConfig, []Policy, error) {
	managed, err := surge.ManagedPolicies(profilePath)
	if err != nil {
		return bridge.MultiConfig{}, nil, err
	}
	policies := make([]Policy, 0, len(managed))
	bridgePolicies := make([]bridge.PolicyConfig, 0, len(managed))
	for _, item := range managed {
		node, err := vless.Parse(item.Link)
		if err != nil {
			return bridge.MultiConfig{}, nil, fmt.Errorf("%s: %w", item.Name, err)
		}
		host := item.LocalHost
		if host == "" {
			host = "127.0.0.1"
		}
		policies = append(policies, Policy{
			Name:      item.Name,
			LocalHost: host,
			LocalPort: item.LocalPort,
			LinkHash:  PolicyLinkHash(item.Link),
		})
		bridgePolicies = append(bridgePolicies, bridge.PolicyConfig{
			Name:      item.Name,
			Node:      node,
			LocalHost: host,
			LocalPort: item.LocalPort,
		})
	}
	if logLevel == "" {
		logLevel = defaultLogLevel
	}
	return bridge.MultiConfig{Policies: bridgePolicies, LogLevel: logLevel}, policies, nil
}

func PolicyLinkHash(link string) string {
	link = strings.TrimSpace(link)
	if link == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(link))
	return hex.EncodeToString(sum[:])
}

func Paths() (RuntimePaths, error) {
	base, err := os.UserCacheDir()
	if err != nil || base == "" {
		base = os.TempDir()
	}
	dir := filepath.Join(base, "xcore-bridge")
	agent, _ := launchAgentPath()
	return RuntimePaths{
		Dir:       dir,
		PID:       filepath.Join(dir, "daemon.pid"),
		State:     filepath.Join(dir, "daemon.json"),
		Log:       filepath.Join(dir, "daemon.log"),
		BridgeLog: filepath.Join(dir, "bridge.log"),
		Lock:      filepath.Join(dir, "daemon.lock"),
		Agent:     agent,
	}, nil
}

func writeLaunchAgent(opts Options, execPath string) (LaunchAgentInfo, error) {
	if runtime.GOOS != "darwin" {
		return LaunchAgentInfo{}, fmt.Errorf("launch agents are only supported on macOS")
	}
	info, err := launchAgentInfo()
	if err != nil {
		return LaunchAgentInfo{}, err
	}
	paths, err := Paths()
	if err != nil {
		return LaunchAgentInfo{}, err
	}
	if err := os.MkdirAll(filepath.Dir(info.Path), 0o755); err != nil {
		return LaunchAgentInfo{}, err
	}
	if err := os.MkdirAll(paths.Dir, 0o755); err != nil {
		return LaunchAgentInfo{}, err
	}
	logLevel := opts.LogLevel
	if logLevel == "" {
		logLevel = defaultLogLevel
	}
	args := []string{
		execPath,
		"daemon",
		"serve",
		"--profile",
		opts.ProfilePath,
		"--log-level",
		logLevel,
	}
	data := renderLaunchAgentPlist(info.Label, args, paths.Log)
	tmp, err := os.CreateTemp(filepath.Dir(info.Path), ".xcore-bridge-launch-agent-*")
	if err != nil {
		return LaunchAgentInfo{}, err
	}
	tmpName := tmp.Name()
	keepTemp := false
	defer func() {
		if !keepTemp {
			_ = os.Remove(tmpName)
		}
	}()
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return LaunchAgentInfo{}, err
	}
	if err := tmp.Close(); err != nil {
		return LaunchAgentInfo{}, err
	}
	if err := os.Chmod(tmpName, 0o644); err != nil {
		return LaunchAgentInfo{}, err
	}
	if err := os.Rename(tmpName, info.Path); err != nil {
		return LaunchAgentInfo{}, err
	}
	keepTemp = true
	return info, nil
}

func renderLaunchAgentPlist(label string, args []string, logPath string) []byte {
	var buf bytes.Buffer
	buf.WriteString(`<?xml version="1.0" encoding="UTF-8"?>` + "\n")
	buf.WriteString(`<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">` + "\n")
	buf.WriteString(`<plist version="1.0">` + "\n")
	buf.WriteString("<dict>\n")
	writePlistString(&buf, "Label", label)
	buf.WriteString("\t<key>ProgramArguments</key>\n")
	buf.WriteString("\t<array>\n")
	for _, arg := range args {
		buf.WriteString("\t\t<string>")
		escapePlistString(&buf, arg)
		buf.WriteString("</string>\n")
	}
	buf.WriteString("\t</array>\n")
	writePlistBool(&buf, "RunAtLoad", true)
	writePlistBool(&buf, "KeepAlive", true)
	writePlistString(&buf, "StandardOutPath", logPath)
	writePlistString(&buf, "StandardErrorPath", logPath)
	buf.WriteString("</dict>\n")
	buf.WriteString("</plist>\n")
	return buf.Bytes()
}

func writePlistString(buf *bytes.Buffer, key, value string) {
	buf.WriteString("\t<key>")
	escapePlistString(buf, key)
	buf.WriteString("</key>\n\t<string>")
	escapePlistString(buf, value)
	buf.WriteString("</string>\n")
}

func writePlistBool(buf *bytes.Buffer, key string, value bool) {
	buf.WriteString("\t<key>")
	escapePlistString(buf, key)
	if value {
		buf.WriteString("</key>\n\t<true/>\n")
		return
	}
	buf.WriteString("</key>\n\t<false/>\n")
}

func escapePlistString(buf *bytes.Buffer, value string) {
	for _, r := range value {
		switch r {
		case '&':
			buf.WriteString("&amp;")
		case '<':
			buf.WriteString("&lt;")
		case '>':
			buf.WriteString("&gt;")
		case '"':
			buf.WriteString("&quot;")
		case '\'':
			buf.WriteString("&apos;")
		default:
			buf.WriteRune(r)
		}
	}
}

func waitForLaunchAgentReady(ctx context.Context, opts Options) (Status, error) {
	timeout := opts.Timeout
	if timeout == 0 {
		timeout = 5 * time.Second
	}
	deadline := time.Now().Add(timeout)
	var last Status
	for time.Now().Before(deadline) {
		current, err := GetStatus(opts)
		if err == nil && current.Running && current.ProfilePath == opts.ProfilePath && policyReady(ctx, current, opts) {
			return current, nil
		}
		if err != nil {
			current.Error = err.Error()
		}
		last = current
		select {
		case <-ctx.Done():
			return last, ctx.Err()
		case <-time.After(50 * time.Millisecond):
		}
	}
	if last.Error != "" {
		return last, fmt.Errorf("launch agent daemon did not become ready: %s", last.Error)
	}
	return last, fmt.Errorf("launch agent daemon did not become ready within %s", timeout)
}

func launchAgentInfo() (LaunchAgentInfo, error) {
	path, err := launchAgentPath()
	if err != nil {
		return LaunchAgentInfo{}, err
	}
	return LaunchAgentInfo{Label: launchAgentLabel(), Path: path}, nil
}

func launchAgentLabel() string {
	return "io.github.backrunner.xcore-bridge.daemon"
}

func launchAgentPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return "", fmt.Errorf("user home directory is required for launch agent")
	}
	return filepath.Join(home, "Library", "LaunchAgents", launchAgentLabel()+".plist"), nil
}

func launchAgentInstalled() bool {
	path, err := launchAgentPath()
	if err != nil {
		return false
	}
	_, err = os.Stat(path)
	return err == nil
}

func runLaunchctl(ctx context.Context, args ...string) error {
	command := exec.CommandContext(ctx, "launchctl", args...)
	output, err := command.CombinedOutput()
	if err != nil {
		prefix := "launchctl " + strings.Join(args, " ")
		message := strings.TrimSpace(string(output))
		if message == "" {
			return fmt.Errorf("%s: %w", prefix, err)
		}
		return fmt.Errorf("%s: %w: %s", prefix, err, message)
	}
	return nil
}

func guiDomain() string {
	return "gui/" + strconv.Itoa(os.Getuid())
}

func isLaunchctlNoService(err error) bool {
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "no such process") ||
		strings.Contains(message, "no such service") ||
		strings.Contains(message, "could not find service")
}

func withControlLock(ctx context.Context, opts Options, fn func() (Status, error)) (Status, error) {
	paths, err := Paths()
	if err != nil {
		return Status{}, err
	}
	if err := os.MkdirAll(paths.Dir, 0o755); err != nil {
		return Status{}, err
	}
	file, err := os.OpenFile(paths.Lock, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return Status{}, err
	}
	defer file.Close()

	timeout := opts.Timeout
	if timeout == 0 {
		timeout = 5 * time.Second
	}
	if err := acquireLock(ctx, file, timeout); err != nil {
		_ = AppendBridgeLog("control lock failed path=%q error=%q", paths.Lock, err)
		return Status{}, err
	}
	defer func() {
		_ = syscall.Flock(int(file.Fd()), syscall.LOCK_UN)
	}()
	return fn()
}

func acquireLock(ctx context.Context, file *os.File, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for {
		err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
		if err == nil {
			return nil
		}
		if !errors.Is(err, syscall.EWOULDBLOCK) && !errors.Is(err, syscall.EAGAIN) {
			return err
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("daemon control lock did not become available within %s", timeout)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(25 * time.Millisecond):
		}
	}
}

func policyReady(ctx context.Context, status Status, opts Options) bool {
	if opts.ProfilePath != "" && status.ProfilePath != "" && opts.ProfilePath != status.ProfilePath {
		return false
	}
	if len(status.Policies) == 0 {
		return false
	}
	if opts.ProfilePath != "" {
		_, expected, err := ConfigFromProfile(opts.ProfilePath, opts.LogLevel)
		if err != nil || !samePolicies(status.Policies, expected) {
			return false
		}
	}
	return readyError(ctx, status.Policies, 100*time.Millisecond) == nil
}

func samePolicies(a, b []Policy) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func ready(ctx context.Context, policies []Policy, timeout time.Duration) bool {
	return readyError(ctx, policies, timeout) == nil
}

func readyError(ctx context.Context, policies []Policy, timeout time.Duration) error {
	for _, policy := range policies {
		if err := bridge.WaitForReady(ctx, policy.LocalHost, policy.LocalPort, timeout); err != nil {
			return fmt.Errorf("%s at %s: %w", policy.Name, net.JoinHostPort(policy.LocalHost, strconv.Itoa(policy.LocalPort)), err)
		}
	}
	return nil
}

func validateProfile(profilePath string) error {
	if strings.TrimSpace(profilePath) == "" {
		return fmt.Errorf("profile path is required")
	}
	if _, err := os.Stat(profilePath); err != nil {
		return err
	}
	return nil
}

func writeState(paths RuntimePaths, state stateFile) error {
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(paths.PID, []byte(strconv.Itoa(state.PID)+"\n"), 0o644); err != nil {
		return err
	}
	return os.WriteFile(paths.State, append(data, '\n'), 0o644)
}

func readState(paths RuntimePaths) (stateFile, error) {
	data, err := os.ReadFile(paths.State)
	if err != nil {
		return stateFile{}, err
	}
	var state stateFile
	if err := json.Unmarshal(data, &state); err != nil {
		return stateFile{}, err
	}
	return state, nil
}

func readPID(paths RuntimePaths) int {
	data, err := os.ReadFile(paths.PID)
	if err != nil {
		return 0
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil || pid <= 0 {
		return 0
	}
	return pid
}

func cleanupState(paths RuntimePaths, pid int) {
	current := readPID(paths)
	if current == 0 || current == pid {
		_ = removeRuntimeFiles()
	}
}

func removeRuntimeFiles() error {
	paths, err := Paths()
	if err != nil {
		return err
	}
	_ = os.Remove(paths.PID)
	_ = os.Remove(paths.State)
	return nil
}

func waitForStopped(ctx context.Context, pid int) error {
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()
	for {
		if !processRunning(pid) {
			_ = removeRuntimeFiles()
			return nil
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("daemon pid %d did not stop after launch agent unload: %w", pid, ctx.Err())
		case <-ticker.C:
		}
	}
}

func processRunning(pid int) bool {
	if pid <= 0 {
		return false
	}
	process, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	err = process.Signal(syscall.Signal(0))
	if err == nil {
		return true
	}
	return errors.Is(err, syscall.EPERM)
}

func WaitForPolicy(ctx context.Context, host string, port int, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	var lastErr error
	for {
		if err := bridge.WaitForReady(ctx, host, port, 100*time.Millisecond); err == nil {
			return nil
		} else {
			lastErr = err
		}
		if time.Now().After(deadline) {
			if lastErr != nil {
				return fmt.Errorf("daemon SOCKS5 listener did not become ready at %s within %s: last error: %w", net.JoinHostPort(host, strconv.Itoa(port)), timeout, lastErr)
			}
			return fmt.Errorf("daemon SOCKS5 listener did not become ready at %s within %s", net.JoinHostPort(host, strconv.Itoa(port)), timeout)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(50 * time.Millisecond):
		}
	}
}
