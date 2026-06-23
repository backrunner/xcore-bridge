package surge

import (
	"fmt"
	"os"
	"strings"
)

func Rename(profilePath string, opts RenameOptions) (RenameResult, error) {
	from := sanitizeName(opts.From)
	if from == "" {
		return RenameResult{}, fmt.Errorf("old policy name is required")
	}
	to := sanitizeName(opts.To)
	if to == "" {
		return RenameResult{}, fmt.Errorf("new policy name is required")
	}
	original, err := os.ReadFile(profilePath)
	if err != nil {
		return RenameResult{}, err
	}
	lines := strings.Split(string(original), "\n")
	proxyStart, proxyEnd := sectionBounds(lines, "Proxy")
	if proxyStart == -1 {
		return RenameResult{}, fmt.Errorf("%s has no [Proxy] section", profilePath)
	}
	managed, hasManaged := managedProxyBlock(lines, proxyStart, proxyEnd)
	if !hasManaged {
		return RenameResult{}, fmt.Errorf("%s has no xcore-bridge managed proxy block", profilePath)
	}

	cleaned, cleanedProxyStart, cleanedProxyEnd := removeManagedProxyBlock(lines, proxyStart, proxyEnd)
	existingNames := proxyNames(cleaned[cleanedProxyStart+1 : cleanedProxyEnd])
	for _, line := range managed {
		name, ok := proxyLineName(line)
		if ok && name != from {
			existingNames = append(existingNames, name)
		}
	}
	newName := uniqueName(existingNames, to)

	renamed := false
	nextManaged := make([]string, 0, len(managed))
	for _, line := range managed {
		name, ok := proxyLineName(line)
		if ok && name == from {
			nextManaged = append(nextManaged, renameProxyLine(line, newName))
			renamed = true
			continue
		}
		nextManaged = append(nextManaged, line)
	}
	if !renamed {
		return RenameResult{}, fmt.Errorf("no matching managed policy found")
	}

	rendered := renderManagedProxyBlock(cleaned, cleanedProxyStart, cleanedProxyEnd, nextManaged)
	var backupPath string
	if opts.WriteFile {
		backupPath = profilePath + ".bak"
		if err := atomicWriteFile(backupPath, original, fileMode(profilePath)); err != nil {
			return RenameResult{}, err
		}
		if err := atomicWriteFile(profilePath, []byte(rendered), fileMode(profilePath)); err != nil {
			return RenameResult{}, err
		}
	}
	return RenameResult{Profile: rendered, OldName: from, NewName: newName, BackupPath: backupPath}, nil
}

func renameProxyLine(line, name string) string {
	left, right, ok := strings.Cut(line, "=")
	if !ok {
		return line
	}
	prefixLen := len(left) - len(strings.TrimLeft(left, " \t"))
	return left[:prefixLen] + name + " =" + right
}
