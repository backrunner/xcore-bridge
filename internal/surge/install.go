package surge

import (
	"fmt"
	"os"
	"strings"
)

func Install(profilePath string, opts InstallOptions) (InstallResult, error) {
	return install(profilePath, opts, true)
}

func Add(profilePath string, opts InstallOptions) (InstallResult, error) {
	return install(profilePath, opts, false)
}

func install(profilePath string, opts InstallOptions, replaceManaged bool) (InstallResult, error) {
	if opts.BasePort == 0 {
		opts.BasePort = 61080
	}
	if opts.BasePort <= 0 || opts.BasePort > 65535 {
		return InstallResult{}, fmt.Errorf("base port must be in 1..65535, or 0 for the default")
	}
	if len(opts.Nodes) == 0 {
		return InstallResult{}, fmt.Errorf("no VLESS nodes supplied")
	}
	if len(opts.Names) > 0 && len(opts.Names) != len(opts.Nodes) {
		return InstallResult{}, fmt.Errorf("policy name count must match VLESS node count")
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
	var existingManaged []string
	var cleaned []string
	if replaceManaged {
		cleaned, proxyStart, proxyEnd = removeManagedProxyBlock(lines, proxyStart, proxyEnd)
	} else {
		existingManaged, _ = managedProxyBlock(lines, proxyStart, proxyEnd)
		if err := validateCurrentManagedProxyLines(existingManaged); err != nil {
			return InstallResult{}, err
		}
		cleaned, proxyStart, proxyEnd = removeManagedProxyBlock(lines, proxyStart, proxyEnd)
	}
	if proxyStart == -1 {
		return InstallResult{}, fmt.Errorf("%s has no [Proxy] section after cleanup", profilePath)
	}
	existingNames := proxyNames(cleaned[proxyStart+1 : proxyEnd])
	existingNames = append(existingNames, proxyNames(existingManaged)...)
	usedPorts := localPorts(cleaned[proxyStart+1 : proxyEnd])
	for port := range localPorts(existingManaged) {
		usedPorts[port] = true
	}
	reusablePorts := subtractPorts(previousPorts, usedPorts)

	generated := append([]string{}, existingManaged...)
	names := make([]string, 0, len(opts.Nodes))
	ports := make([]int, 0, len(opts.Nodes))
	nextPort := opts.BasePort
	for i, node := range opts.Nodes {
		displayName := node.DisplayName()
		if len(opts.Names) > 0 {
			displayName = opts.Names[i]
		}
		name := uniqueName(existingNames, displayName)
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
			ProfilePath:      profilePath,
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

	rendered := renderManagedProxyBlock(cleaned, proxyStart, proxyEnd, generated)
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

func validateCurrentManagedProxyLines(lines []string) error {
	for _, line := range lines {
		if _, ok := managedPolicy(line); !ok {
			name, _ := proxyLineName(line)
			if name == "" {
				name = strings.TrimSpace(line)
			}
			return fmt.Errorf("managed proxy %q is not a current xcore-bridge run policy", name)
		}
	}
	return nil
}
