package surge

import (
	"fmt"
	"os"
	"strings"
)

func Replace(profilePath string, opts ReplaceOptions) (ReplaceResult, error) {
	name := sanitizeName(opts.Name)
	if name == "" {
		return ReplaceResult{}, fmt.Errorf("policy name is required")
	}
	if err := opts.Node.Validate(); err != nil {
		return ReplaceResult{}, err
	}
	original, err := os.ReadFile(profilePath)
	if err != nil {
		return ReplaceResult{}, err
	}
	lines := strings.Split(string(original), "\n")
	proxyStart, proxyEnd := sectionBounds(lines, "Proxy")
	if proxyStart == -1 {
		return ReplaceResult{}, fmt.Errorf("%s has no [Proxy] section", profilePath)
	}
	managed, hasManaged := managedProxyBlock(lines, proxyStart, proxyEnd)
	if !hasManaged {
		return ReplaceResult{}, fmt.Errorf("%s has no xcore-bridge managed proxy block", profilePath)
	}

	replaced := false
	localPort := 0
	nextManaged := make([]string, 0, len(managed))
	for _, line := range managed {
		lineName, ok := proxyLineName(line)
		if !ok || lineName != name {
			nextManaged = append(nextManaged, line)
			continue
		}
		policy, ok := managedPolicy(line)
		if !ok {
			return ReplaceResult{}, fmt.Errorf("managed policy %q is not a valid xcore-bridge managed policy", name)
		}
		execPath, ok := proxyLineExecPath(line)
		if !ok {
			execPath = opts.ExecPath
		}
		updated, err := ProxyLine(ProxyLineOptions{
			Node:             opts.Node,
			Name:             lineName,
			ExecPath:         execPath,
			ProfilePath:      profilePath,
			LocalPort:        policy.LocalPort,
			IncludeAddresses: true,
		})
		if err != nil {
			return ReplaceResult{}, err
		}
		nextManaged = append(nextManaged, updated)
		replaced = true
		localPort = policy.LocalPort
	}
	if !replaced {
		return ReplaceResult{}, fmt.Errorf("no matching managed policy found")
	}

	cleaned, proxyStart, proxyEnd := removeManagedProxyBlock(lines, proxyStart, proxyEnd)
	rendered := renderManagedProxyBlock(cleaned, proxyStart, proxyEnd, nextManaged)
	var backupPath string
	if opts.WriteFile {
		backupPath = profilePath + ".bak"
		if err := atomicWriteFile(backupPath, original, fileMode(profilePath)); err != nil {
			return ReplaceResult{}, err
		}
		if err := atomicWriteFile(profilePath, []byte(rendered), fileMode(profilePath)); err != nil {
			return ReplaceResult{}, err
		}
	}
	return ReplaceResult{Profile: rendered, PolicyName: name, LocalPort: localPort, BackupPath: backupPath}, nil
}
