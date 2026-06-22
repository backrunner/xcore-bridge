package main

import (
	"bufio"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"github.com/backrunner/xcore-bridge/internal/surge"
	"github.com/backrunner/xcore-bridge/internal/vless"
)

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
