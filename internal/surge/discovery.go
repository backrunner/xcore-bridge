package surge

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
)

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
