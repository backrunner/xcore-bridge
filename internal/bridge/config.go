package bridge

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/orchiliao/xcore-bridge/internal/vless"
)

type Config struct {
	Node      vless.Node
	LocalHost string
	LocalPort int
	LogLevel  string
}

func JSONConfig(cfg Config) ([]byte, error) {
	if err := cfg.Node.Validate(); err != nil {
		return nil, err
	}
	if cfg.LocalHost == "" {
		cfg.LocalHost = "127.0.0.1"
	}
	if cfg.LocalPort <= 0 || cfg.LocalPort > 65535 {
		return nil, fmt.Errorf("invalid local port %d", cfg.LocalPort)
	}
	if cfg.LogLevel == "" {
		cfg.LogLevel = "warning"
	}
	outboundSettings, err := vlessSettings(cfg.Node)
	if err != nil {
		return nil, err
	}
	doc := map[string]any{
		"log": map[string]any{
			"loglevel": cfg.LogLevel,
		},
		"inbounds": []any{
			map[string]any{
				"tag":      "surge-in",
				"listen":   cfg.LocalHost,
				"port":     cfg.LocalPort,
				"protocol": "socks",
				"settings": map[string]any{
					"udp":       true,
					"auth":      "noauth",
					"userLevel": 0,
				},
			},
		},
		"outbounds": []any{
			map[string]any{
				"tag":            "vless-out",
				"protocol":       "vless",
				"settings":       outboundSettings,
				"streamSettings": streamSettings(cfg.Node),
			},
		},
		"routing": map[string]any{
			"domainStrategy": "AsIs",
			"rules": []any{
				map[string]any{
					"type":        "field",
					"inboundTag":  []string{"surge-in"},
					"outboundTag": "vless-out",
				},
			},
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

func vlessSettings(node vless.Node) (map[string]any, error) {
	user := map[string]any{
		"id":         node.ID,
		"encryption": strings.ToLower(valueOrDefault(node.Param("encryption"), "none")),
	}
	if flow := node.Flow(); flow != "" {
		user["flow"] = flow
	}
	if level := node.Param("level"); level != "" {
		parsedLevel, err := strconv.ParseUint(level, 10, 32)
		if err != nil {
			return nil, fmt.Errorf("invalid VLESS user level %q", level)
		}
		user["level"] = uint32(parsedLevel)
	}
	return map[string]any{
		"vnext": []any{
			map[string]any{
				"address": node.Host,
				"port":    node.Port,
				"users":   []any{user},
			},
		},
	}, nil
}

func streamSettings(node vless.Node) map[string]any {
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
		settings["wsSettings"] = websocketSettings(node)
	case "httpupgrade":
		settings["httpupgradeSettings"] = httpUpgradeSettings(node)
	case "splithttp":
		settings["splithttpSettings"] = splitHTTPSettings(node)
	case "grpc":
		settings["grpcSettings"] = grpcSettings(node)
	}
	return settings
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

func websocketSettings(node vless.Node) map[string]any {
	out := map[string]any{}
	copyParam(out, "host", node, "host")
	copyParam(out, "path", node, "path")
	return out
}

func httpUpgradeSettings(node vless.Node) map[string]any {
	out := map[string]any{}
	copyParam(out, "host", node, "host")
	copyParam(out, "path", node, "path")
	return out
}

func splitHTTPSettings(node vless.Node) map[string]any {
	out := map[string]any{}
	copyParam(out, "host", node, "host")
	copyParam(out, "path", node, "path")
	copyParam(out, "mode", node, "mode")
	return out
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
