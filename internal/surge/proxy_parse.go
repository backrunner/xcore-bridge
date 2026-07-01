package surge

import (
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"
)

func ManagedPolicies(profilePath string) ([]ManagedPolicy, error) {
	data, err := os.ReadFile(profilePath)
	if err != nil {
		return nil, err
	}
	lines := strings.Split(string(data), "\n")
	proxyStart, proxyEnd := sectionBounds(lines, "Proxy")
	if proxyStart == -1 {
		return nil, fmt.Errorf("%s has no [Proxy] section", profilePath)
	}
	managed, hasManaged := managedProxyBlock(lines, proxyStart, proxyEnd)
	if !hasManaged {
		return nil, fmt.Errorf("%s has no xcore-bridge managed proxy block", profilePath)
	}
	if err := validateCurrentManagedProxyLines(managed); err != nil {
		return nil, err
	}
	var policies []ManagedPolicy
	for _, line := range managed {
		policy, _ := managedPolicy(line)
		policies = append(policies, policy)
	}
	if len(policies) == 0 {
		return nil, fmt.Errorf("%s has no xcore-bridge managed policies", profilePath)
	}
	return policies, nil
}

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

func managedPolicy(line string) (ManagedPolicy, bool) {
	name, ok := proxyLineName(line)
	if !ok {
		return ManagedPolicy{}, false
	}
	fields := splitProxyFields(line)
	if len(fields) == 0 {
		return ManagedPolicy{}, false
	}
	if !strings.EqualFold(strings.TrimSpace(fields[0]), name+" = external") {
		left, right, ok := strings.Cut(fields[0], "=")
		if !ok || sanitizeName(left) != name || !strings.EqualFold(strings.TrimSpace(right), "external") {
			return ManagedPolicy{}, false
		}
	}
	var args []string
	port := 0
	host := localProxyHost
	for _, field := range fields[1:] {
		key, value, ok := strings.Cut(field, "=")
		if !ok {
			continue
		}
		key = strings.ToLower(strings.TrimSpace(key))
		value = strings.TrimSpace(value)
		switch key {
		case "args":
			arg, err := strconv.Unquote(value)
			if err != nil {
				arg = value
			}
			args = append(args, arg)
		case "local-port":
			parsed, err := strconv.Atoi(value)
			if err == nil && parsed > 0 && parsed <= 65535 {
				port = parsed
			}
		case "local-host", "listen":
			if parsed, err := strconv.Unquote(value); err == nil {
				value = parsed
			}
			if value != "" {
				host = value
			}
		}
	}
	if !isRunProxyArgs(args) {
		return ManagedPolicy{}, false
	}
	if _, ok := proxyLineExecPath(line); !ok {
		return ManagedPolicy{}, false
	}
	link := linkArg(args)
	runHost, runPort := runListenArgs(args)
	if runHost == "" {
		runHost = host
	}
	if runPort == 0 {
		runPort = port
	}
	if runHost != host || runPort != port {
		return ManagedPolicy{}, false
	}
	if link == "" || port == 0 {
		return ManagedPolicy{}, false
	}
	return ManagedPolicy{
		Name:      name,
		Link:      link,
		LocalHost: host,
		LocalPort: port,
		RunHost:   runHost,
		RunPort:   runPort,
	}, true
}

func isRunProxyArgs(args []string) bool {
	return len(args) > 0 && args[0] == "run"
}

func proxyLineExecPath(line string) (string, bool) {
	for _, field := range splitProxyFields(line) {
		key, value, ok := strings.Cut(field, "=")
		if !ok {
			continue
		}
		if !strings.EqualFold(strings.TrimSpace(key), "exec") {
			continue
		}
		value = strings.TrimSpace(value)
		if parsed, err := strconv.Unquote(value); err == nil {
			value = parsed
		}
		value = strings.TrimSpace(value)
		return value, value != ""
	}
	return "", false
}

func linkArg(args []string) string {
	for i, arg := range args {
		if arg == "--link" && i+1 < len(args) {
			return strings.TrimSpace(args[i+1])
		}
	}
	for _, arg := range args {
		if strings.HasPrefix(strings.ToLower(strings.TrimSpace(arg)), "vless://") {
			return strings.TrimSpace(arg)
		}
	}
	return ""
}

func runListenArgs(args []string) (string, int) {
	host := ""
	port := 0
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--listen":
			if i+1 < len(args) {
				host = strings.TrimSpace(args[i+1])
				i++
			}
		case "--local-port":
			if i+1 < len(args) {
				parsed, err := strconv.Atoi(strings.TrimSpace(args[i+1]))
				if err == nil && parsed > 0 && parsed <= 65535 {
					port = parsed
				}
				i++
			}
		}
	}
	return host, port
}

func splitProxyFields(line string) []string {
	var fields []string
	start := 0
	inQuote := false
	escaped := false
	for i, r := range line {
		if escaped {
			escaped = false
			continue
		}
		switch r {
		case '\\':
			if inQuote {
				escaped = true
			}
		case '"':
			inQuote = !inQuote
		case ',':
			if !inQuote {
				fields = appendProxyField(fields, line[start:i])
				start = i + len(string(r))
			}
		}
	}
	fields = appendProxyField(fields, line[start:])
	return fields
}

func appendProxyField(fields []string, field string) []string {
	field = strings.TrimSpace(field)
	if field == "" {
		return fields
	}
	return append(fields, field)
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
