package surge

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

func normalizedRemoveNames(values []string) map[string]bool {
	out := map[string]bool{}
	for _, value := range values {
		if name := sanitizeName(value); name != "" {
			out[name] = true
		}
	}
	return out
}

func proxyLineName(line string) (string, bool) {
	trimmed := strings.TrimSpace(line)
	if trimmed == "" || strings.HasPrefix(trimmed, "#") || strings.HasPrefix(trimmed, ";") {
		return "", false
	}
	left, _, ok := strings.Cut(trimmed, "=")
	if !ok {
		return "", false
	}
	name := sanitizeName(left)
	return name, name != ""
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
