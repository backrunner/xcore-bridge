package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"runtime/debug"
	"strconv"
	"strings"
	"syscall"

	"github.com/backrunner/xcore-bridge/internal/bridge"
	"github.com/backrunner/xcore-bridge/internal/surge"
	"github.com/backrunner/xcore-bridge/internal/vless"
)

var version = "dev"

func main() {
	if err := runWithIO(os.Args[1:], os.Stdout, os.Stderr, os.Stdin); err != nil {
		fmt.Fprintln(os.Stderr, "xcore-bridge:", err)
		os.Exit(1)
	}
}

func run(args []string, stdout, stderr io.Writer) error {
	return runWithIO(args, stdout, stderr, nil)
}

func runWithIO(args []string, stdout, stderr io.Writer, stdin io.Reader) error {
	if len(args) == 0 {
		printUsage(stdout)
		return nil
	}

	switch args[0] {
	case "run":
		return runCommand(args[1:], stdout)
	case "xray-config":
		return xrayConfigCommand(args[1:], stdout)
	case "add":
		return addCommand(args[1:], stdout, stderr, stdin)
	case "remove":
		return removeCommand(args[1:], stdout, stderr)
	case "version", "--version", "-v":
		return versionCommand(args[1:], stdout)
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
	server, err := bridge.Start(ctx, cfg)
	if err != nil {
		return err
	}
	defer server.Close()
	fmt.Fprintf(stdout, "xcore-bridge ready on %s:%d for %s\n", *localHost, *localPort, node.DisplayName())
	<-ctx.Done()
	return nil
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

func addCommand(args []string, stdout, stderr io.Writer, stdin io.Reader) error {
	if stderr == nil {
		stderr = io.Discard
	}
	fs := flag.NewFlagSet("add", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	profile := fs.String("profile", "", "Surge profile path; auto-detected from iCloud when omitted")
	linksFile := fs.String("links-file", "", "file with one VLESS share link per line")
	execPath := fs.String("exec", defaultExecPath(), "path to xcore-bridge executable")
	basePort := fs.Int("base-port", 61080, "first local port to assign")
	dryRun := fs.Bool("dry-run", false, "print updated profile instead of writing")
	yes := fs.Bool("yes", false, "confirm first-time profile changes without prompting")
	if err := fs.Parse(args); err != nil {
		return err
	}

	profilePath, err := selectedProfilePath(*profile, stderr, "add")
	if err != nil {
		return err
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
		return errors.New("add requires at least one VLESS share link or --links-file")
	}

	var nodes []vless.Node
	for _, raw := range links {
		node, err := vless.Parse(raw)
		if err != nil {
			return err
		}
		nodes = append(nodes, node)
	}

	alreadyManaged, err := surge.ProfileHasManagedBlock(profilePath)
	if err != nil {
		return err
	}
	if !*dryRun && !*yes && !alreadyManaged {
		ok, err := confirmFirstProfileChange(stdin, stderr, profilePath)
		if err != nil {
			return err
		}
		if !ok {
			return errors.New("profile change was not confirmed")
		}
	}

	updated, err := surge.Add(profilePath, surge.InstallOptions{
		Nodes:     nodes,
		ExecPath:  *execPath,
		BasePort:  *basePort,
		WriteFile: !*dryRun,
	})
	if err != nil {
		return err
	}
	if *dryRun {
		fmt.Fprint(stdout, updated.Profile)
		return nil
	}
	fmt.Fprintf(stdout, "added %d external proxy policies into %s\n", len(updated.PolicyNames), profilePath)
	if updated.BackupPath != "" {
		fmt.Fprintf(stdout, "backup: %s\n", updated.BackupPath)
	}
	for i, name := range updated.PolicyNames {
		fmt.Fprintf(stdout, "%s local-port=%d\n", name, updated.LocalPorts[i])
	}
	return nil
}

func removeCommand(args []string, stdout, stderr io.Writer) error {
	if stderr == nil {
		stderr = io.Discard
	}
	fs := flag.NewFlagSet("remove", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	profile := fs.String("profile", "", "Surge profile path; auto-detected from iCloud when omitted")
	dryRun := fs.Bool("dry-run", false, "print updated profile instead of writing")
	if err := fs.Parse(args); err != nil {
		return err
	}
	names := fs.Args()
	if len(names) == 0 {
		return errors.New("remove requires at least one managed policy name")
	}

	profilePath, err := selectedProfilePath(*profile, stderr, "remove")
	if err != nil {
		return err
	}
	updated, err := surge.Remove(profilePath, surge.RemoveOptions{
		Names:     names,
		WriteFile: !*dryRun,
	})
	if err != nil {
		return err
	}
	if *dryRun {
		fmt.Fprint(stdout, updated.Profile)
		return nil
	}
	fmt.Fprintf(stdout, "removed %d external proxy policies from %s\n", len(updated.RemovedNames), profilePath)
	if updated.BackupPath != "" {
		fmt.Fprintf(stdout, "backup: %s\n", updated.BackupPath)
	}
	for _, name := range updated.RemovedNames {
		fmt.Fprintf(stdout, "%s\n", name)
	}
	return nil
}

func versionCommand(args []string, stdout io.Writer) error {
	if len(args) > 1 {
		return errors.New("version accepts at most one flag")
	}
	if len(args) == 1 {
		switch args[0] {
		case "--verbose", "-v":
			fmt.Fprintf(stdout, "xcore-bridge %s\n", version)
			fmt.Fprintf(stdout, "xray-core %s\n", xrayCoreVersion())
			return nil
		default:
			return fmt.Errorf("unknown version flag %q", args[0])
		}
	}
	fmt.Fprintln(stdout, version)
	return nil
}

func xrayCoreVersion() string {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return "unknown"
	}
	for _, dep := range info.Deps {
		if dep.Path == "github.com/xtls/xray-core" {
			if dep.Replace != nil {
				return dep.Replace.Version
			}
			if dep.Version != "" {
				return dep.Version
			}
			return "(devel)"
		}
	}
	return "unknown"
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
		if !strings.HasPrefix(strings.ToLower(line), "vless://") {
			return nil, fmt.Errorf("%s:%s is not a VLESS share link", path, strconv.Itoa(lineNo+1))
		}
		links = append(links, line)
	}
	return links, nil
}

func selectedProfilePath(raw string, stderr io.Writer, command string) (string, error) {
	profilePath := strings.TrimSpace(raw)
	if profilePath != "" {
		return profilePath, nil
	}
	candidates, err := surge.DiscoverProfiles()
	if err != nil {
		return "", err
	}
	if len(candidates) == 0 {
		return "", fmt.Errorf("%s could not find a Surge profile in iCloud Drive or local Surge profiles; pass --profile to choose one explicitly", command)
	}
	selected := candidates[0]
	profilePath = selected.Path
	if len(candidates) == 1 {
		fmt.Fprintf(stderr, "xcore-bridge: found Surge profile %s (%s)\n", profilePath, selected.Source)
	} else {
		fmt.Fprintf(stderr, "xcore-bridge: found %d Surge profiles; using %s (%s)\n", len(candidates), profilePath, selected.Source)
	}
	return profilePath, nil
}

func confirmFirstProfileChange(stdin io.Reader, stderr io.Writer, profilePath string) (bool, error) {
	if stdin == nil {
		return false, nil
	}
	if stderr == nil {
		stderr = io.Discard
	}
	fmt.Fprintf(stderr, "xcore-bridge will update this Surge profile for the first time:\n  %s\nA single backup will be kept at:\n  %s.bak\nContinue? [y/N] ", profilePath, profilePath)
	answer, err := bufio.NewReader(stdin).ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return false, err
	}
	switch strings.ToLower(strings.TrimSpace(answer)) {
	case "y", "yes":
		return true, nil
	default:
		return false, nil
	}
}

func printUsage(w io.Writer) {
	fmt.Fprint(w, `xcore-bridge wraps xray-core as a Surge External Proxy program.

Usage:
  xcore-bridge run --local-port 61080 --link 'vless://...'
  xcore-bridge add 'vless://...'
  xcore-bridge remove 'Policy Name'
  xcore-bridge xray-config --local-port 61080 --link 'vless://...'

Commands:
  run            start one local SOCKS5 inbound and forward everything to the VLESS node
  add            add VLESS links as managed Surge External Proxy policies
  remove         remove managed Surge External Proxy policies by name
  xray-config    print the generated xray-core JSON config
  version        print version
`)
}
