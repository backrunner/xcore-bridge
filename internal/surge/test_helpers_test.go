package surge

import (
	"fmt"
	"net"
	"strconv"
	"strings"
	"testing"

	"github.com/backrunner/xcore-bridge/internal/vless"
)

func testSurgeNode(t *testing.T, name string) vless.Node {
	t.Helper()
	node, err := vless.Parse("vless://00000000-0000-0000-0000-000000000000@example.com:443?encryption=none&flow=xtls-rprx-vision&security=reality&sni=example.com&fp=chrome&pbk=AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA&sid=0123&type=tcp#" + name)
	if err != nil {
		t.Fatal(err)
	}
	return node
}

func testManagedProxyLine(t *testing.T, profile, name string, port int) string {
	t.Helper()
	link := testSurgeNode(t, name).Raw
	return fmt.Sprintf(`%s = external, exec = "/opt/homebrew/bin/xcore-bridge", args = "run", args = "--profile", args = "%s", args = "--local-port", args = "%d", args = "--link", args = "%s", local-port = %d, udp-relay = true`, name, profile, port, link, port)
}

func listenOnTestPort(t *testing.T) (net.Listener, int) {
	t.Helper()
	for port := 61080; port < 65535; port++ {
		listener, err := net.Listen("tcp", net.JoinHostPort(localProxyHost, strconv.Itoa(port)))
		if err == nil {
			return listener, port
		}
	}
	t.Skip("no available local test port below 65535")
	return nil, 0
}

func assertManagedProxyOrder(t *testing.T, profile, proxyLine string) {
	t.Helper()
	begin := strings.Index(profile, markerBegin)
	line := strings.Index(profile, proxyLine)
	end := strings.Index(profile, markerEnd)
	if begin == -1 || line == -1 || end == -1 || !(begin < line && line < end) {
		t.Fatalf("%q was not inside the managed block:\n%s", proxyLine, profile)
	}
}
