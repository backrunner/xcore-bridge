package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"

	"github.com/orchiliao/xcore-bridge/internal/bridge"
	"github.com/orchiliao/xcore-bridge/internal/surge"
	"github.com/orchiliao/xcore-bridge/internal/vless"
)

const version = "0.1.0"

func main() {
	if err := run(os.Args[1:], os.Stdout, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, "xcore-bridge:", err)
		os.Exit(1)
	}
}

func run(args []string, stdout, stderr io.Writer) error {
	_ = stderr
	if len(args) == 0 {
		printUsage(stdout)
		return nil
	}

	switch args[0] {
	case "run":
		return runCommand(args[1:], stdout)
	case "xray-config":
		return xrayConfigCommand(args[1:], stdout)
	case "surge-line":
		return surgeLineCommand(args[1:], stdout)
	case "surge-install":
		return surgeInstallCommand(args[1:], stdout)
	case "version", "--version", "-v":
		fmt.Fprintln(stdout, version)
		return nil
	case "help", "--help", "-h":
		printUsage(stdout)
		return nil
	default:
		return fmt.Errorf("unknown command %q", args[0])
	}
}

func runCommand(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("run", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	localPort := fs.Int("local-port", 0, "SOCKS5 port Surge will connect to")
	localHost := fs.String("listen", "127.0.0.1", "SOCKS5 listen address")
	link := fs.String("link", "", "VLESS share link")
	logLevel := fs.String("log-level", "warning", "Xray log level")
	if err := fs.Parse(args); err != nil {
		return err
	}
	shareLink, err := oneLinkArg(*link, fs.Args(), "run")
	if err != nil {
		return err
	}
	if shareLink == "" {
		return errors.New("run requires --link or a positional VLESS share link")
	}
	if *localPort <= 0 || *localPort > 65535 {
		return errors.New("run requires --local-port in 1..65535")
	}

	node, err := vless.Parse(shareLink)
	if err != nil {
		return err
	}
	cfg := bridge.Config{
		Node:      node,
		LocalHost: *localHost,
		LocalPort: *localPort,
		LogLevel:  *logLevel,
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	fmt.Fprintf(stdout, "xcore-bridge listening on %s:%d for %s\n", *localHost, *localPort, node.DisplayName())
	return bridge.Run(ctx, cfg)
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

func surgeLineCommand(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("surge-line", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	localPort := fs.Int("local-port", 0, "Surge External Proxy local-port")
	name := fs.String("name", "", "Surge policy name")
	execPath := fs.String("exec", defaultExecPath(), "path to xcore-bridge executable")
	link := fs.String("link", "", "VLESS share link")
	includeAddresses := fs.Bool("addresses", true, "include resolved IP addresses for Surge VIF exclusion")
	if err := fs.Parse(args); err != nil {
		return err
	}
	shareLink, err := oneLinkArg(*link, fs.Args(), "surge-line")
	if err != nil {
		return err
	}
	if shareLink == "" {
		return errors.New("surge-line requires --link or a positional VLESS share link")
	}
	node, err := vless.Parse(shareLink)
	if err != nil {
		return err
	}
	port := *localPort
	if port == 0 {
		port = surge.StablePort(node)
	}
	line, err := surge.ProxyLine(surge.ProxyLineOptions{
		Node:             node,
		Name:             *name,
		ExecPath:         *execPath,
		LocalPort:        port,
		IncludeAddresses: *includeAddresses,
	})
	if err != nil {
		return err
	}
	fmt.Fprintln(stdout, line)
	return nil
}

func surgeInstallCommand(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("surge-install", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	profile := fs.String("profile", "", "Surge profile path")
	linksFile := fs.String("links-file", "", "file with one VLESS share link per line")
	execPath := fs.String("exec", defaultExecPath(), "path to xcore-bridge executable")
	basePort := fs.Int("base-port", 61080, "first local port to assign")
	backup := fs.Bool("backup", true, "write a .bak copy before changing the profile")
	dryRun := fs.Bool("dry-run", false, "print updated profile instead of writing")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *profile == "" {
		return errors.New("surge-install requires --profile")
	}

	links := fs.Args()
	if *linksFile != "" {
		fileLinks, err := readLinksFile(*linksFile)
		if err != nil {
			return err
		}
		links = append(links, fileLinks...)
	}
	if len(links) == 0 {
		return errors.New("surge-install requires at least one VLESS share link or --links-file")
	}

	var nodes []vless.Node
	for _, raw := range links {
		node, err := vless.Parse(raw)
		if err != nil {
			return err
		}
		nodes = append(nodes, node)
	}
	updated, err := surge.Install(*profile, surge.InstallOptions{
		Nodes:     nodes,
		ExecPath:  *execPath,
		BasePort:  *basePort,
		NoBackup:  !*backup,
		WriteFile: !*dryRun,
	})
	if err != nil {
		return err
	}
	if *dryRun {
		fmt.Fprint(stdout, updated.Profile)
		return nil
	}
	fmt.Fprintf(stdout, "installed %d external proxy policies into %s\n", len(updated.PolicyNames), *profile)
	for i, name := range updated.PolicyNames {
		fmt.Fprintf(stdout, "%s local-port=%d\n", name, updated.LocalPorts[i])
	}
	return nil
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			return value
		}
	}
	return ""
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

func defaultExecPath() string {
	path, err := os.Executable()
	if err != nil || path == "" {
		return "xcore-bridge"
	}
	return path
}

func readLinksFile(path string) ([]string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var links []string
	for lineNo, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if !strings.HasPrefix(line, "vless://") {
			return nil, fmt.Errorf("%s:%s is not a VLESS share link", path, strconv.Itoa(lineNo+1))
		}
		links = append(links, line)
	}
	return links, nil
}

func printUsage(w io.Writer) {
	fmt.Fprint(w, `xcore-bridge wraps xray-core as a Surge External Proxy program.

Usage:
  xcore-bridge run --local-port 61080 --link 'vless://...'
  xcore-bridge surge-line --link 'vless://...'
  xcore-bridge surge-install --profile ~/Library/Application\ Support/Surge/Profiles/example.conf --links-file links.txt
  xcore-bridge xray-config --local-port 61080 --link 'vless://...'

Commands:
  run            start one local SOCKS5 inbound and forward everything to the VLESS node
  surge-line     print a Surge [Proxy] external policy line
  surge-install  inject generated external policies into a Surge profile
  xray-config    print the generated xray-core JSON config
  version        print version
`)
}
