package surge

import (
	"os"
	"strings"
)

const (
	markerBegin  = "# xcore-bridge managed external proxies begin"
	markerEnd    = "# xcore-bridge managed external proxies end"
	legacyMarker = "# xcore-bridge managed external proxies"
)

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

func managedProxyBlock(lines []string, proxyStart, proxyEnd int) ([]string, bool) {
	for i := proxyStart + 1; i < proxyEnd; i++ {
		trimmed := strings.TrimSpace(lines[i])
		if trimmed == markerBegin {
			if end := findMarkerEnd(lines, i+1, proxyEnd); end != -1 {
				return proxyContentLines(lines[i+1 : end]), true
			}
			end := skipLegacyManagedLines(lines, i, proxyEnd)
			return proxyContentLines(lines[i+1 : end+1]), true
		}
		if trimmed == legacyMarker {
			end := skipLegacyManagedLines(lines, i, proxyEnd)
			return proxyContentLines(lines[i+1 : end+1]), true
		}
	}
	return nil, false
}

func proxyContentLines(lines []string) []string {
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || trimmed == markerBegin || trimmed == markerEnd || trimmed == legacyMarker {
			continue
		}
		out = append(out, line)
	}
	return out
}

func managedPolicyLineCount(lines []string) int {
	count := 0
	for _, line := range lines {
		if _, ok := proxyLineName(line); ok {
			count++
		}
	}
	return count
}

func renderManagedProxyBlock(lines []string, proxyStart, proxyEnd int, managed []string) string {
	insertAt := proxyEnd
	for insertAt > proxyStart+1 && strings.TrimSpace(lines[insertAt-1]) == "" {
		insertAt--
	}

	var block []string
	if len(managed) > 0 {
		block = append(block, "")
		block = append(block, markerBegin)
		block = append(block, managed...)
		block = append(block, markerEnd)
	}
	if len(block) > 0 && insertAt < len(lines) && strings.TrimSpace(lines[insertAt]) != "" {
		block = append(block, "")
	}
	nextLines := append(lines[:insertAt], append(block, lines[insertAt:]...)...)

	rendered := strings.Join(nextLines, "\n")
	if !strings.HasSuffix(rendered, "\n") {
		rendered += "\n"
	}
	return rendered
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
