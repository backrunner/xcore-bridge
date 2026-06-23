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
	var names repeatedFlag
	fs.Var(&names, "name", "policy name override; repeat once per VLESS link")
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
	if len(names) > 0 && len(names) != len(nodes) {
		return fmt.Errorf("add --name must be supplied once per VLESS link; got %d names for %d links", len(names), len(nodes))
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
		Names:     []string(names),
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
	ui := newUI(stdout)
	ui.Success("added %d external proxy policies", len(updated.PolicyNames))
	ui.KeyValue("profile", profilePath)
	if updated.BackupPath != "" {
		ui.KeyValue("backup", updated.BackupPath)
	}
	ui.Info("policies")
	for i, name := range updated.PolicyNames {
		ui.Item(name, fmt.Sprintf("local-port=%d", updated.LocalPorts[i]))
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
	var flagNames repeatedFlag
	fs.Var(&flagNames, "name", "managed policy name to remove; repeat to remove several")
	if err := fs.Parse(args); err != nil {
		return err
	}
	names := append([]string{}, []string(flagNames)...)
	names = append(names, fs.Args()...)
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
	ui := newUI(stdout)
	ui.Success("removed %d external proxy policies", len(updated.RemovedNames))
	ui.KeyValue("profile", profilePath)
	if updated.BackupPath != "" {
		ui.KeyValue("backup", updated.BackupPath)
	}
	ui.Info("removed")
	for _, name := range updated.RemovedNames {
		ui.Item(name)
	}
	return nil
}

func renameCommand(args []string, stdout, stderr io.Writer) error {
	if stderr == nil {
		stderr = io.Discard
	}
	fs := flag.NewFlagSet("rename", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	profile := fs.String("profile", "", "Surge profile path; auto-detected from iCloud when omitted")
	from := fs.String("from", "", "managed policy name to rename")
	to := fs.String("to", "", "new managed policy name")
	dryRun := fs.Bool("dry-run", false, "print updated profile instead of writing")
	if err := fs.Parse(args); err != nil {
		return err
	}
	positional := fs.Args()
	oldName := strings.TrimSpace(*from)
	newName := strings.TrimSpace(*to)
	switch {
	case oldName == "" && newName == "" && len(positional) == 2:
		oldName = positional[0]
		newName = positional[1]
	case oldName != "" && newName != "" && len(positional) == 0:
	case oldName != "" && newName == "" && len(positional) == 1:
		newName = positional[0]
	case oldName == "" && newName != "" && len(positional) == 1:
		oldName = positional[0]
	default:
		return errors.New("rename requires old and new managed policy names")
	}

	profilePath, err := selectedProfilePath(*profile, stderr, "rename")
	if err != nil {
		return err
	}
	updated, err := surge.Rename(profilePath, surge.RenameOptions{
		From:      oldName,
		To:        newName,
		WriteFile: !*dryRun,
	})
	if err != nil {
		return err
	}
	if *dryRun {
		fmt.Fprint(stdout, updated.Profile)
		return nil
	}
	ui := newUI(stdout)
	ui.Success("renamed external proxy policy")
	ui.KeyValue("profile", profilePath)
	if updated.BackupPath != "" {
		ui.KeyValue("backup", updated.BackupPath)
	}
	ui.Item(updated.OldName, "->", updated.NewName)
	return nil
}

type repeatedFlag []string

func (f *repeatedFlag) String() string {
	return strings.Join(*f, ", ")
}

func (f *repeatedFlag) Set(value string) error {
	value = strings.TrimSpace(value)
	if value == "" {
		return errors.New("name cannot be empty")
	}
	*f = append(*f, value)
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
		ui := newUI(stderr)
		ui.Info("found Surge profile")
		ui.KeyValue("profile", profilePath)
		ui.KeyValue("source", selected.Source)
	} else {
		ui := newUI(stderr)
		ui.Info("found %d Surge profiles; using first match", len(candidates))
		ui.KeyValue("profile", profilePath)
		ui.KeyValue("source", selected.Source)
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
	ui := newUI(stderr)
	ui.Title("First Profile Update")
	ui.KeyValue("profile", profilePath)
	ui.KeyValue("backup", profilePath+".bak")
	fmt.Fprint(stderr, "Continue? [y/N] ")
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
