package surge

import (
	"strings"
	"testing"

	"github.com/backrunner/xcore-bridge/internal/vless"
)

func TestProxyLine(t *testing.T) {
	node, err := vless.Parse("vless://00000000-0000-0000-0000-000000000000@203.0.113.10:443?encryption=none&flow=xtls-rprx-vision&security=reality&sni=example.com&fp=chrome&pbk=AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA&sid=0123&type=tcp#Demo")
	if err != nil {
		t.Fatal(err)
	}
	line, err := ProxyLine(ProxyLineOptions{Node: node, ExecPath: "/opt/homebrew/bin/xcore-bridge", LocalPort: 61080, IncludeAddresses: true})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"Demo = external",
		"exec = \"/opt/homebrew/bin/xcore-bridge\"",
		"args = \"run\"",
		"args = \"--local-port\"",
		"args = \"61080\"",
		"local-port = 61080",
		"udp-relay = true",
		"addresses = 203.0.113.10",
	} {
		if !strings.Contains(line, want) {
			t.Fatalf("line %q does not contain %q", line, want)
		}
	}
}

func TestProxyLineIncludesProfileForDaemonSupervisor(t *testing.T) {
	node, err := vless.Parse("vless://00000000-0000-0000-0000-000000000000@example.com:443?encryption=none&flow=xtls-rprx-vision&security=reality&sni=example.com&fp=chrome&pbk=AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA&sid=0123&type=tcp#Demo")
	if err != nil {
		t.Fatal(err)
	}
	line, err := ProxyLine(ProxyLineOptions{
		Node:        node,
		ExecPath:    "/opt/homebrew/bin/xcore-bridge",
		ProfilePath: "/Users/me/Surge/default.conf",
		LocalPort:   61080,
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"args = \"--profile\"",
		"args = \"/Users/me/Surge/default.conf\"",
		"args = \"--local-port\"",
		"args = \"--link\"",
	} {
		if !strings.Contains(line, want) {
			t.Fatalf("line %q does not contain %q", line, want)
		}
	}
}

func TestProxyLineOmitsAddressesForDomainHost(t *testing.T) {
	node, err := vless.Parse("vless://00000000-0000-0000-0000-000000000000@example.com:443?encryption=none&flow=xtls-rprx-vision&security=reality&sni=example.com&fp=chrome&pbk=AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA&sid=0123&type=tcp#Demo")
	if err != nil {
		t.Fatal(err)
	}
	line, err := ProxyLine(ProxyLineOptions{Node: node, ExecPath: "/opt/homebrew/bin/xcore-bridge", LocalPort: 61080, IncludeAddresses: true})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(line, "addresses =") {
		t.Fatalf("domain hosts must not be emitted as Surge addresses: %s", line)
	}
}

func TestProxyLinePreservesUnicodePolicyNames(t *testing.T) {
	node, err := vless.Parse("vless://00000000-0000-0000-0000-000000000000@example.com:443?encryption=none&flow=xtls-rprx-vision&security=reality&sni=example.com&fp=chrome&pbk=AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA&sid=0123&type=tcp#香港%20节点")
	if err != nil {
		t.Fatal(err)
	}
	line, err := ProxyLine(ProxyLineOptions{Node: node, ExecPath: "/opt/homebrew/bin/xcore-bridge", LocalPort: 61080})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(line, "香港 节点 = external") {
		t.Fatalf("unicode policy name was not preserved: %s", line)
	}
}
