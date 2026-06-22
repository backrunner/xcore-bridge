package surge

import (
	"fmt"
	"os"
	"strings"
)

func Remove(profilePath string, opts RemoveOptions) (RemoveResult, error) {
	names := normalizedRemoveNames(opts.Names)
	if len(names) == 0 {
		return RemoveResult{}, fmt.Errorf("no policy names supplied")
	}
	original, err := os.ReadFile(profilePath)
	if err != nil {
		return RemoveResult{}, err
	}
	lines := strings.Split(string(original), "\n")
	proxyStart, proxyEnd := sectionBounds(lines, "Proxy")
	if proxyStart == -1 {
		return RemoveResult{}, fmt.Errorf("%s has no [Proxy] section", profilePath)
	}
	managed, hasManaged := managedProxyBlock(lines, proxyStart, proxyEnd)
	if !hasManaged {
		return RemoveResult{}, fmt.Errorf("%s has no xcore-bridge managed proxy block", profilePath)
	}
	var removed []string
	var nextManaged []string
	for _, line := range managed {
		name, ok := proxyLineName(line)
		if ok && names[sanitizeName(name)] {
			removed = append(removed, sanitizeName(name))
			continue
		}
		nextManaged = append(nextManaged, line)
	}
	if len(removed) == 0 {
		return RemoveResult{}, fmt.Errorf("no matching managed policies found")
	}
	if managedPolicyLineCount(nextManaged) == 0 {
		nextManaged = nil
	}
	cleaned, proxyStart, proxyEnd := removeManagedProxyBlock(lines, proxyStart, proxyEnd)
	rendered := renderManagedProxyBlock(cleaned, proxyStart, proxyEnd, nextManaged)
	var backupPath string
	if opts.WriteFile {
		backupPath = profilePath + ".bak"
		if err := atomicWriteFile(backupPath, original, fileMode(profilePath)); err != nil {
			return RemoveResult{}, err
		}
		if err := atomicWriteFile(profilePath, []byte(rendered), fileMode(profilePath)); err != nil {
			return RemoveResult{}, err
		}
	}
	return RemoveResult{Profile: rendered, RemovedNames: removed, BackupPath: backupPath}, nil
}
