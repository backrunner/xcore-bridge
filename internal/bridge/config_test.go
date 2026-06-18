package bridge

import (
	"bytes"
	"encoding/json"
	"testing"

	_ "github.com/xtls/xray-core/main/distro/all"

	"github.com/orchiliao/xcore-bridge/internal/vless"
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
