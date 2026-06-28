package bridge

import (
	"bytes"
	"encoding/json"
	"testing"

	_ "github.com/xtls/xray-core/main/distro/all"

	"github.com/backrunner/xcore-bridge/internal/vless"
	"github.com/xtls/xray-core/core"
)

func TestJSONConfigRealityVisionLoadsInXray(t *testing.T) {
	node, err := vless.Parse("vless://00000000-0000-0000-0000-000000000000@example.com:443?encryption=none&flow=xtls-rprx-vision&security=reality&sni=www.example.com&fp=chrome&pbk=AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA&sid=0123&type=tcp#Example")
	if err != nil {
		t.Fatal(err)
	}
	data, err := JSONConfig(Config{Node: node, LocalHost: "127.0.0.1", LocalPort: 61080})
	if err != nil {
		t.Fatal(err)
	}
	if !json.Valid(data) {
		t.Fatalf("generated config is not valid JSON:\n%s", data)
	}
	if _, err := core.LoadConfig("json", bytes.NewReader(data)); err != nil {
		t.Fatalf("xray rejected generated config: %v\n%s", err, data)
	}
}

func TestJSONConfigWithLevelLoadsInXray(t *testing.T) {
	node, err := vless.Parse("vless://00000000000000000000000000000000@example.com:443?encryption=none&flow=xtls-rprx-vision&security=reality&sni=www.example.com&fp=chrome&pbk=AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA&sid=0123&type=tcp&level=1#Example")
	if err != nil {
		t.Fatal(err)
	}
	data, err := JSONConfig(Config{Node: node, LocalHost: "127.0.0.1", LocalPort: 61080})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := core.LoadConfig("json", bytes.NewReader(data)); err != nil {
		t.Fatalf("xray rejected generated config: %v\n%s", err, data)
	}
}

func TestJSONConfigCanWriteXrayLogsToFile(t *testing.T) {
	node, err := vless.Parse("vless://00000000-0000-0000-0000-000000000000@example.com:443?encryption=none&flow=xtls-rprx-vision&security=reality&sni=www.example.com&fp=chrome&pbk=AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA&sid=0123&type=tcp#Example")
	if err != nil {
		t.Fatal(err)
	}
	data, err := JSONConfig(Config{
		Node:          node,
		LocalHost:     "127.0.0.1",
		LocalPort:     61080,
		AccessLogPath: "/tmp/xcore-bridge-access.log",
		ErrorLogPath:  "/tmp/xcore-bridge-error.log",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := core.LoadConfig("json", bytes.NewReader(data)); err != nil {
		t.Fatalf("xray rejected generated config: %v\n%s", err, data)
	}
	var doc map[string]any
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatal(err)
	}
	logConfig := doc["log"].(map[string]any)
	if got := logConfig["access"]; got != "/tmp/xcore-bridge-access.log" {
		t.Fatalf("unexpected access log path %#v in %s", got, data)
	}
	if got := logConfig["error"]; got != "/tmp/xcore-bridge-error.log" {
		t.Fatalf("unexpected error log path %#v in %s", got, data)
	}
}

func TestMultiJSONConfigRoutesEachInboundToMatchingOutbound(t *testing.T) {
	first, err := vless.Parse("vless://00000000-0000-0000-0000-000000000000@example.com:443?encryption=none&flow=xtls-rprx-vision&security=reality&sni=www.example.com&fp=chrome&pbk=AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA&sid=0123&type=tcp#First")
	if err != nil {
		t.Fatal(err)
	}
	second, err := vless.Parse("vless://00000000000000000000000000000000@example.org:443?encryption=none&security=tls&sni=www.example.org&type=ws&host=cdn.example.org&path=%2Fws#Second")
	if err != nil {
		t.Fatal(err)
	}
	data, err := MultiJSONConfig(MultiConfig{
		Policies: []PolicyConfig{
			{Name: "First", Node: first, LocalHost: "127.0.0.1", LocalPort: 61080},
			{Name: "Second", Node: second, LocalHost: "127.0.0.1", LocalPort: 61081},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := core.LoadConfig("json", bytes.NewReader(data)); err != nil {
		t.Fatalf("xray rejected generated config: %v\n%s", err, data)
	}
	var doc map[string]any
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatal(err)
	}
	inbounds := doc["inbounds"].([]any)
	outbounds := doc["outbounds"].([]any)
	routing := doc["routing"].(map[string]any)
	rules := routing["rules"].([]any)
	if len(inbounds) != 2 || len(outbounds) != 2 || len(rules) != 2 {
		t.Fatalf("expected two inbounds/outbounds/rules:\n%s", data)
	}
	firstInbound := inbounds[0].(map[string]any)["tag"]
	firstOutbound := outbounds[0].(map[string]any)["tag"]
	firstRule := rules[0].(map[string]any)
	if got := firstRule["inboundTag"].([]any)[0]; got != firstInbound {
		t.Fatalf("first rule inbound mismatch: %#v vs %#v", got, firstInbound)
	}
	if got := firstRule["outboundTag"]; got != firstOutbound {
		t.Fatalf("first rule outbound mismatch: %#v vs %#v", got, firstOutbound)
	}
	if _, ok := routing["balancers"]; ok {
		t.Fatalf("daemon config must not emit routing balancers:\n%s", data)
	}
}

func TestJSONConfigRejectsInvalidLevel(t *testing.T) {
	node, err := vless.Parse("vless://00000000000000000000000000000000@example.com:443?encryption=none&security=tls&sni=www.example.com&type=tcp&level=bad#Example")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := JSONConfig(Config{Node: node, LocalHost: "127.0.0.1", LocalPort: 61080}); err == nil {
		t.Fatal("expected invalid level to be rejected")
	}
}

func TestJSONConfigHTTPUpgradeHostLoadsInXray(t *testing.T) {
	node, err := vless.Parse("vless://00000000000000000000000000000000@example.com:443?encryption=none&security=tls&sni=www.example.com&type=httpupgrade&host=cdn.example.com&path=%2Fup#HTTPUpgrade")
	if err != nil {
		t.Fatal(err)
	}
	data, err := JSONConfig(Config{Node: node, LocalHost: "127.0.0.1", LocalPort: 61080})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := core.LoadConfig("json", bytes.NewReader(data)); err != nil {
		t.Fatalf("xray rejected generated config: %v\n%s", err, data)
	}
}

func TestJSONConfigGRPCMultiModeLoadsInXray(t *testing.T) {
	node, err := vless.Parse("vless://00000000000000000000000000000000@example.com:443?encryption=none&security=tls&sni=www.example.com&type=grpc&serviceName=svc&mode=multiMode#GRPC")
	if err != nil {
		t.Fatal(err)
	}
	data, err := JSONConfig(Config{Node: node, LocalHost: "127.0.0.1", LocalPort: 61080})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := core.LoadConfig("json", bytes.NewReader(data)); err != nil {
		t.Fatalf("xray rejected generated config: %v\n%s", err, data)
	}
}

func TestJSONConfigWebSocketHostUsesTopLevelHost(t *testing.T) {
	node, err := vless.Parse("vless://00000000000000000000000000000000@example.com:443?encryption=none&security=tls&sni=www.example.com&type=ws&host=cdn.example.com&path=%2Fws#WS")
	if err != nil {
		t.Fatal(err)
	}
	data, err := JSONConfig(Config{Node: node, LocalHost: "127.0.0.1", LocalPort: 61080})
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatal(err)
	}
	outbounds := doc["outbounds"].([]any)
	streamSettings := outbounds[0].(map[string]any)["streamSettings"].(map[string]any)
	wsSettings := streamSettings["wsSettings"].(map[string]any)
	if got := wsSettings["host"]; got != "cdn.example.com" {
		t.Fatalf("expected WebSocket host to be top-level host, got %#v in %s", got, data)
	}
	if _, ok := wsSettings["headers"]; ok {
		t.Fatalf("WebSocket settings should not emit deprecated Host header:\n%s", data)
	}
}

func TestJSONConfigSplitHTTPHostUsesTopLevelHost(t *testing.T) {
	node, err := vless.Parse("vless://00000000000000000000000000000000@example.com:443?encryption=none&security=tls&sni=www.example.com&type=splithttp&host=cdn.example.com&path=%2Fxhttp&mode=stream-up#SplitHTTP")
	if err != nil {
		t.Fatal(err)
	}
	data, err := JSONConfig(Config{Node: node, LocalHost: "127.0.0.1", LocalPort: 61080})
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatal(err)
	}
	outbounds := doc["outbounds"].([]any)
	streamSettings := outbounds[0].(map[string]any)["streamSettings"].(map[string]any)
	splitHTTPSettings := streamSettings["splithttpSettings"].(map[string]any)
	if got := splitHTTPSettings["host"]; got != "cdn.example.com" {
		t.Fatalf("expected splitHTTP host to be top-level host, got %#v in %s", got, data)
	}
	if _, ok := splitHTTPSettings["headers"]; ok {
		t.Fatalf("splitHTTP settings should not emit Host header:\n%s", data)
	}
}
