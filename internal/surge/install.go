package surge

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/backrunner/xcore-bridge/internal/vless"
)

const (
	markerBegin  = "# xcore-bridge managed external proxies begin"
	markerEnd    = "# xcore-bridge managed external proxies end"
	legacyMarker = "# xcore-bridge managed external proxies"
)

type InstallOptions struct {
	Nodes     []vless.Node
	ExecPath  string
	BasePort  int
	WriteFile bool

	portAvailable func(string, int) bool
}

type InstallResult struct {
	Profile     string
	PolicyNames []string
	LocalPorts  []int
	BackupPath  string
}

type ProfileCandidate struct {
	Path   string
	Source string
}

func DiscoverProfiles() ([]ProfileCandidate, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	icloudRoot := filepath.Join(home, "Library", "Mobile Documents")
	roots := []ProfileCandidate{
		{
			Path:   filepath.Join(icloudRoot, "iCloud~com~nssurge~Inc~Surge", "Documents", "Profiles"),
			Source: "iCloud Drive",
		},
		{
			Path:   filepath.Join(icloudRoot, "iCloud~com~nssurge~inc~Surge", "Documents", "Profiles"),
			Source: "iCloud Drive",
		},
		{
			Path:   filepath.Join(icloudRoot, "iCloud~com~nssurge~Inc~Surge", "Documents"),
			Source: "iCloud Drive",
		},
		{
			Path:   filepath.Join(icloudRoot, "com~apple~CloudDocs", "Surge", "Profiles"),
			Source: "iCloud Drive",
		},
		{
			Path:   filepath.Join(icloudRoot, "com~apple~CloudDocs", "Surge"),
			Source: "iCloud Drive",
		},
	}
	if dynamicRoots, err := discoverICloudSurgeRoots(icloudRoot); err != nil {
		return nil, err
	} else {
		roots = append(roots, dynamicRoots...)
	}
	roots = append(roots, ProfileCandidate{
		Path:   filepath.Join(home, "Library", "Application Support", "Surge", "Profiles"),
		Source: "local Surge profiles",
	})
	seenRoots := map[string]bool{}
	seenProfiles := map[string]bool{}
	var candidates []ProfileCandidate
	for _, root := range roots {
		rootKey := profilePathKey(root.Path)
		if seenRoots[rootKey] {
			continue
		}
		seenRoots[rootKey] = true
		matches, err := profileFiles(root.Path)
		if err != nil {
			return nil, err
		}
		for _, path := range matches {
			profileKey := profilePathKey(path)
			if seenProfiles[profileKey] {
				continue
			}
			seenProfiles[profileKey] = true
			candidates = append(candidates, ProfileCandidate{Path: path, Source: root.Source})
		}
	}
	return candidates, nil
}

func discoverICloudSurgeRoots(icloudRoot string) ([]ProfileCandidate, error) {
	entries, err := os.ReadDir(icloudRoot)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var roots []ProfileCandidate
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		if !strings.Contains(strings.ToLower(entry.Name()), "surge") {
			continue
		}
		root := filepath.Join(icloudRoot, entry.Name())
		roots = append(roots,
			ProfileCandidate{Path: filepath.Join(root, "Documents", "Profiles"), Source: "iCloud Drive"},
			ProfileCandidate{Path: filepath.Join(root, "Documents"), Source: "iCloud Drive"},
			ProfileCandidate{Path: filepath.Join(root, "Profiles"), Source: "iCloud Drive"},
			ProfileCandidate{Path: root, Source: "iCloud Drive"},
		)
	}
	return roots, nil
}

func ProfileHasManagedBlock(profilePath string) (bool, error) {
	data, err := os.ReadFile(profilePath)
	if err != nil {
		return false, err
	}
	for _, line := range strings.Split(string(data), "\n") {
		switch strings.TrimSpace(line) {
		case markerBegin, legacyMarker:
			return true, nil
		}
	}
	return false, nil
}

func Install(profilePath string, opts InstallOptions) (InstallResult, error) {
	if opts.BasePort == 0 {
		opts.BasePort = 61080
	}
	if opts.BasePort <= 0 || opts.BasePort > 65535 {
		return InstallResult{}, fmt.Errorf("base port must be in 1..65535, or 0 for the default")
	}
	if len(opts.Nodes) == 0 {
		return InstallResult{}, fmt.Errorf("no VLESS nodes supplied")
	}
	for _, node := range opts.Nodes {
		if err := node.Validate(); err != nil {
			return InstallResult{}, err
		}
	}
	original, err := os.ReadFile(profilePath)
	if err != nil {
		return InstallResult{}, err
	}
	lines := strings.Split(string(original), "\n")
	proxyStart, proxyEnd := sectionBounds(lines, "Proxy")
	if proxyStart == -1 {
		lines, proxyStart, proxyEnd = insertProxySection(lines)
	}

	previousPorts := localPorts(lines[proxyStart+1 : proxyEnd])
	cleaned, proxyStart, proxyEnd := removeManagedProxyBlock(lines, proxyStart, proxyEnd)
	if proxyStart == -1 {
		return InstallResult{}, fmt.Errorf("%s has no [Proxy] section after cleanup", profilePath)
	}
	existingNames := proxyNames(cleaned[proxyStart+1 : proxyEnd])
	usedPorts := localPorts(cleaned[proxyStart+1 : proxyEnd])
	reusablePorts := subtractPorts(previousPorts, usedPorts)

	generated := []string{markerBegin}
	names := make([]string, 0, len(opts.Nodes))
	ports := make([]int, 0, len(opts.Nodes))
	nextPort := opts.BasePort
	for _, node := range opts.Nodes {
		name := uniqueName(existingNames, node.DisplayName())
		existingNames = append(existingNames, name)
		port, err := findAvailablePort(localProxyHost, nextPort, usedPorts, reusablePorts, opts.portAvailable)
		if err != nil {
			return InstallResult{}, fmt.Errorf("no available local port at or above %d", opts.BasePort)
		}
		usedPorts[port] = true
		nextPort = port + 1
		line, err := ProxyLine(ProxyLineOptions{
			Node:             node,
			Name:             name,
			ExecPath:         opts.ExecPath,
			LocalPort:        port,
			IncludeAddresses: true,
		})
		if err != nil {
			return InstallResult{}, err
		}
		generated = append(generated, line)
		names = append(names, name)
		ports = append(ports, port)
	}
	generated = append(generated, markerEnd)

	insertAt := proxyEnd
	for insertAt > proxyStart+1 && strings.TrimSpace(cleaned[insertAt-1]) == "" {
		insertAt--
	}
	block := append([]string{""}, generated...)
	if insertAt < len(cleaned) {
		if strings.TrimSpace(cleaned[insertAt]) != "" {
			block = append(block, "")
		}
	}
	nextLines := append(cleaned[:insertAt], append(block, cleaned[insertAt:]...)...)

	rendered := strings.Join(nextLines, "\n")
	if !strings.HasSuffix(rendered, "\n") {
		rendered += "\n"
	}
	var backupPath string
	if opts.WriteFile {
		backupPath = profilePath + ".bak"
		if err := atomicWriteFile(backupPath, original, fileMode(profilePath)); err != nil {
			return InstallResult{}, err
		}
		if err := atomicWriteFile(profilePath, []byte(rendered), fileMode(profilePath)); err != nil {
			return InstallResult{}, err
		}
	}
	return InstallResult{Profile: rendered, PolicyNames: names, LocalPorts: ports, BackupPath: backupPath}, nil
}

func profileFiles(root string) ([]string, error) {
	info, err := os.Stat(root)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	if !info.IsDir() {
		return nil, nil
	}
	var matches []string
	err = filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			if path != root && strings.HasPrefix(entry.Name(), ".") {
				return filepath.SkipDir
			}
			return nil
		}
		if strings.EqualFold(filepath.Ext(path), ".conf") {
			matches = append(matches, path)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	sortProfileFiles(matches)
	return matches, nil
}

func sortProfileFiles(paths []string) {
	sort.Slice(paths, func(i, j int) bool {
		iBase := strings.ToLower(filepath.Base(paths[i]))
		jBase := strings.ToLower(filepath.Base(paths[j]))
		iRank := profileFileRank(iBase)
		jRank := profileFileRank(jBase)
		if iRank != jRank {
			return iRank < jRank
		}
		return paths[i] < paths[j]
	})
}

func profileFileRank(base string) int {
	switch {
	case base == "default.conf":
		return 0
	case strings.Contains(base, "default"):
		return 1
	default:
		return 2
	}
}

func profilePathKey(path string) string {
	return strings.ToLower(filepath.Clean(path))
}

func sectionBounds(lines []string, section string) (start, end int) {
	want := "[" + strings.ToLower(section) + "]"
	start = -1
	end = -1
	for i, line := range lines {
		if normalizedSectionLine(line) == want {
			start = i
			break
		}
	}
	if start == -1 {
		return -1, -1
	}
	for i := start + 1; i < len(lines); i++ {
		if normalizedSectionLine(lines[i]) != "" {
			return start, i
		}
	}
	return start, len(lines)
}

func insertProxySection(lines []string) ([]string, int, int) {
	insertAt := len(lines)
	if start, end := sectionBounds(lines, "General"); start != -1 {
		insertAt = end
	} else {
		for i, line := range lines {
			switch normalizedSectionLine(line) {
			case "[proxy group]", "[rule]":
				insertAt = i
				goto found
			}
		}
	}
found:
	block := []string{"[Proxy]"}
	if insertAt > 0 && strings.TrimSpace(lines[insertAt-1]) != "" {
		block = append([]string{""}, block...)
	}
	if insertAt < len(lines) && strings.TrimSpace(lines[insertAt]) != "" {
		block = append(block, "")
	}
	lines = append(lines[:insertAt], append(block, lines[insertAt:]...)...)
	start, end := sectionBounds(lines, "Proxy")
	return lines, start, end
}

func normalizedSectionLine(line string) string {
	trimmed := strings.TrimPrefix(strings.TrimSpace(line), "\ufeff")
	if !strings.HasPrefix(trimmed, "[") || !strings.HasSuffix(trimmed, "]") {
		return ""
	}
	return strings.ToLower(trimmed)
}

func removeManagedProxyBlock(lines []string, proxyStart, proxyEnd int) ([]string, int, int) {
	out := make([]string, 0, len(lines))
	for i := 0; i < len(lines); i++ {
		if i > proxyStart && i < proxyEnd {
			trimmed := strings.TrimSpace(lines[i])
			if trimmed == markerBegin {
				if end := findMarkerEnd(lines, i+1, proxyEnd); end != -1 {
					i = end
					continue
				}
				i = skipLegacyManagedLines(lines, i, proxyEnd)
				continue
			}
			if trimmed == markerEnd {
				continue
			}
			if trimmed == legacyMarker {
				i = skipLegacyManagedLines(lines, i, proxyEnd)
				continue
			}
		}
		out = append(out, lines[i])
	}
	start, end := sectionBounds(out, "Proxy")
	return out, start, end
}

func findMarkerEnd(lines []string, start, end int) int {
	for i := start; i < end; i++ {
		if strings.TrimSpace(lines[i]) == markerEnd {
			return i
		}
	}
	return -1
}

func skipLegacyManagedLines(lines []string, markerIndex, proxyEnd int) int {
	i := markerIndex
	for i+1 < proxyEnd {
		next := strings.TrimSpace(lines[i+1])
		if next == "" {
			return i + 1
		}
		if !isLegacyManagedLine(next) {
			return i
		}
		i++
	}
	return i
}

func isLegacyManagedLine(line string) bool {
	if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, ";") {
		return true
	}
	lower := strings.ToLower(line)
	return strings.Contains(lower, "= external") &&
		strings.Contains(lower, "local-port")
}

func uniqueName(existing []string, raw string) string {
	base := sanitizeName(raw)
	if base == "" {
		base = "xcore-bridge"
	}
	name := base
	for i := 2; contains(existing, name); i++ {
		name = fmt.Sprintf("%s %d", base, i)
	}
	return name
}

func contains(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func proxyNames(lines []string) []string {
	names := make([]string, 0, len(lines))
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") || strings.HasPrefix(trimmed, ";") {
			continue
		}
		left, _, ok := strings.Cut(trimmed, "=")
		if !ok {
			continue
		}
		name := sanitizeName(left)
		if name != "" {
			names = append(names, name)
		}
	}
	return names
}

var localPortPattern = regexp.MustCompile(`(?i)(?:^|,\s*)local-port\s*=\s*([0-9]+)\s*(?:,|[#;]|$)`)

func localPorts(lines []string) map[int]bool {
	ports := map[int]bool{}
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") || strings.HasPrefix(trimmed, ";") {
			continue
		}
		for _, match := range localPortPattern.FindAllStringSubmatch(line, -1) {
			port, err := strconv.Atoi(match[1])
			if err == nil && port > 0 && port <= 65535 {
				ports[port] = true
			}
		}
	}
	return ports
}

func fileMode(path string) os.FileMode {
	info, err := os.Stat(path)
	if err != nil {
		return 0o644
	}
	return info.Mode().Perm()
}

func atomicWriteFile(path string, data []byte, mode os.FileMode) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".xcore-bridge-*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Chmod(mode); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, path)
}
