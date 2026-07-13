package bridge

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/backrunner/xcore-bridge/internal/vless"
)

const (
	connectionIdleSeconds   = 3600
	halfCloseTimeoutSeconds = 10
)

type Config struct {
	Node          vless.Node
	LocalHost     string
	LocalPort     int
	LogLevel      string
	AccessLogPath string
	ErrorLogPath  string
}

type PolicyConfig struct {
	Name      string
	Node      vless.Node
	LocalHost string
	LocalPort int
}

type MultiConfig struct {
	Policies      []PolicyConfig
	LogLevel      string
	AccessLogPath string
	ErrorLogPath  string
}

func JSONConfig(cfg Config) ([]byte, error) {
	return MultiJSONConfig(MultiConfig{
		Policies: []PolicyConfig{
			{
				Node:      cfg.Node,
				LocalHost: cfg.LocalHost,
				LocalPort: cfg.LocalPort,
			},
		},
		LogLevel:      cfg.LogLevel,
		AccessLogPath: cfg.AccessLogPath,
		ErrorLogPath:  cfg.ErrorLogPath,
	})
}

func MultiJSONConfig(cfg MultiConfig) ([]byte, error) {
	if len(cfg.Policies) == 0 {
		return nil, fmt.Errorf("no policies supplied")
	}
	logLevel := cfg.LogLevel
	if logLevel == "" {
		logLevel = "warning"
	}

	inbounds := make([]any, 0, len(cfg.Policies))
	outbounds := make([]any, 0, len(cfg.Policies))
	rules := make([]any, 0, len(cfg.Policies))
	policyLevels := map[string]any{}
	seenPorts := map[string]bool{}
	for i, policy := range cfg.Policies {
		inbound, outbound, rule, level, err := policyConfigParts(policy, i)
		if err != nil {
			return nil, err
		}
		host := policy.LocalHost
		if host == "" {
			host = "127.0.0.1"
		}
		key := host + ":" + strconv.Itoa(policy.LocalPort)
		if seenPorts[key] {
			return nil, fmt.Errorf("duplicate local SOCKS5 listener %s", key)
		}
		seenPorts[key] = true
		inbounds = append(inbounds, inbound)
		outbounds = append(outbounds, outbound)
		rules = append(rules, rule)
		policyLevels[strconv.FormatUint(uint64(level), 10)] = map[string]any{
			"connIdle":     connectionIdleSeconds,
			"uplinkOnly":   halfCloseTimeoutSeconds,
			"downlinkOnly": halfCloseTimeoutSeconds,
		}
	}

	logConfig := map[string]any{
		"loglevel": logLevel,
	}
	if cfg.AccessLogPath != "" {
		logConfig["access"] = cfg.AccessLogPath
	}
	if cfg.ErrorLogPath != "" {
		logConfig["error"] = cfg.ErrorLogPath
	}

	doc := map[string]any{
		"log":       logConfig,
		"inbounds":  inbounds,
		"outbounds": outbounds,
		"policy": map[string]any{
			"levels": policyLevels,
		},
		"routing": map[string]any{
			"domainStrategy": "AsIs",
			"rules":          rules,
		},
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(doc); err != nil {
		return nil, err
	}
	return bytes.TrimRight(buf.Bytes(), "\n"), nil
}

func policyConfigParts(cfg PolicyConfig, index int) (map[string]any, map[string]any, map[string]any, uint32, error) {
	if err := cfg.Node.Validate(); err != nil {
		return nil, nil, nil, 0, err
	}
	if cfg.LocalHost == "" {
		cfg.LocalHost = "127.0.0.1"
	}
	if cfg.LocalPort <= 0 || cfg.LocalPort > 65535 {
		return nil, nil, nil, 0, fmt.Errorf("invalid local port %d", cfg.LocalPort)
	}
	outboundSettings, level, err := vlessSettings(cfg.Node)
	if err != nil {
		return nil, nil, nil, 0, err
	}
	stream, err := streamSettings(cfg.Node)
	if err != nil {
		return nil, nil, nil, 0, err
	}
	inboundTag := taggedName("surge-in", cfg.LocalPort, index)
	outboundTag := taggedName("vless-out", cfg.LocalPort, index)
	inbound := map[string]any{
		"tag":      inboundTag,
		"listen":   cfg.LocalHost,
		"port":     cfg.LocalPort,
		"protocol": "socks",
		"settings": map[string]any{
			"udp":       true,
			"auth":      "noauth",
			"userLevel": 0,
		},
	}
	outbound := map[string]any{
		"tag":            outboundTag,
		"protocol":       "vless",
		"settings":       outboundSettings,
		"streamSettings": stream,
	}
	rule := map[string]any{
		"type":        "field",
		"inboundTag":  []string{inboundTag},
		"outboundTag": outboundTag,
	}
	return inbound, outbound, rule, level, nil
}

func taggedName(prefix string, port, index int) string {
	return prefix + "-" + strconv.Itoa(port) + "-" + strconv.Itoa(index+1)
}

func vlessSettings(node vless.Node) (map[string]any, uint32, error) {
	user := map[string]any{
		"id":         node.ID,
		"encryption": strings.ToLower(valueOrDefault(node.Param("encryption"), "none")),
	}
	if flow := node.Flow(); flow != "" {
		user["flow"] = flow
	}
	var userLevel uint32
	if level := node.Param("level"); level != "" {
		parsedLevel, err := strconv.ParseUint(level, 10, 32)
		if err != nil {
			return nil, 0, fmt.Errorf("invalid VLESS user level %q", level)
		}
		userLevel = uint32(parsedLevel)
		user["level"] = userLevel
	}
	return map[string]any{
		"vnext": []any{
			map[string]any{
				"address": node.Host,
				"port":    node.Port,
				"users":   []any{user},
			},
		},
	}, userLevel, nil
}

func streamSettings(node vless.Node) (map[string]any, error) {
	network := node.Network()
	security := node.Security()
	settings := map[string]any{
		"network":  network,
		"security": valueOrDefault(security, "none"),
	}
	switch security {
	case "reality":
		settings["realitySettings"] = realitySettings(node)
	case "tls":
		settings["tlsSettings"] = tlsSettings(node)
	}
	switch network {
	case "tcp":
		if headerType := node.Param("headerType"); headerType != "" && headerType != "none" {
			settings["tcpSettings"] = map[string]any{
				"header": map[string]any{
					"type": headerType,
				},
			}
		}
	case "ws":
		ws, err := websocketSettings(node)
		if err != nil {
			return nil, err
		}
		settings["wsSettings"] = ws
	case "httpupgrade":
		settings["httpupgradeSettings"] = httpUpgradeSettings(node)
	case "splithttp":
		splitHTTP, err := splitHTTPSettings(node)
		if err != nil {
			return nil, err
		}
		settings["splithttpSettings"] = splitHTTP
	case "grpc":
		settings["grpcSettings"] = grpcSettings(node)
	}
	return settings, nil
}

func realitySettings(node vless.Node) map[string]any {
	out := map[string]any{}
	copyParam(out, "serverName", node, "sni", "serverName", "servername")
	copyParam(out, "fingerprint", node, "fp", "fingerprint")
	copyParam(out, "publicKey", node, "pbk", "publicKey")
	copyParam(out, "shortId", node, "sid", "shortId")
	copyParam(out, "spiderX", node, "spx", "spiderX")
	copyParam(out, "mldsa65Verify", node, "mldsa65Verify", "mldsa65verify")
	return out
}

func tlsSettings(node vless.Node) map[string]any {
	out := map[string]any{}
	copyParam(out, "serverName", node, "sni", "serverName", "servername")
	copyParam(out, "fingerprint", node, "fp", "fingerprint")
	if alpn := splitCSV(node.Param("alpn")); len(alpn) > 0 {
		out["alpn"] = alpn
	}
	return out
}

func websocketSettings(node vless.Node) (map[string]any, error) {
	out := map[string]any{}
	copyParam(out, "host", node, "host")
	copyParam(out, "path", node, "path")
	if value := node.Param("heartbeatPeriod"); value != "" {
		period, err := strconv.ParseUint(value, 10, 32)
		if err != nil {
			return nil, fmt.Errorf("invalid WebSocket heartbeat period %q", value)
		}
		out["heartbeatPeriod"] = uint32(period)
	}
	return out, nil
}

func httpUpgradeSettings(node vless.Node) map[string]any {
	out := map[string]any{}
	copyParam(out, "host", node, "host")
	copyParam(out, "path", node, "path")
	return out
}

func splitHTTPSettings(node vless.Node) (map[string]any, error) {
	out := map[string]any{}
	copyParam(out, "host", node, "host")
	copyParam(out, "path", node, "path")
	copyParam(out, "mode", node, "mode")
	if value := node.Param("extra"); value != "" {
		var extra map[string]json.RawMessage
		if err := json.Unmarshal([]byte(value), &extra); err != nil || extra == nil {
			return nil, fmt.Errorf("invalid SplitHTTP extra settings")
		}
		out["extra"] = json.RawMessage(value)
	}
	return out, nil
}

func grpcSettings(node vless.Node) map[string]any {
	out := map[string]any{}
	copyParam(out, "authority", node, "authority", "host")
	copyParam(out, "serviceName", node, "serviceName", "service")
	if mode := strings.ToLower(node.Param("mode")); mode == "multi" || mode == "multimode" {
		out["multiMode"] = true
	}
	return out
}

func copyParam(out map[string]any, dst string, node vless.Node, keys ...string) {
	if value := node.Param(keys...); value != "" {
		out[dst] = value
	}
}

func splitCSV(value string) []string {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	parts := strings.Split(value, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		if part = strings.TrimSpace(part); part != "" {
			out = append(out, part)
		}
	}
	return out
}

func valueOrDefault(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}
