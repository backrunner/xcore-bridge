package surge

import (
	"net"
	"testing"
)

func TestFindAvailablePortSkipsUsedAndUnavailablePorts(t *testing.T) {
	used := map[int]bool{61080: true}
	available := func(_ string, port int) bool {
		return port == 61082
	}

	port, err := findAvailablePort(localProxyHost, 61080, used, nil, available)
	if err != nil {
		t.Fatal(err)
	}
	if port != 61082 {
		t.Fatalf("expected 61082, got %d", port)
	}
}

func TestFindAvailablePortAllowsReusableUnavailablePort(t *testing.T) {
	used := map[int]bool{}
	reusable := map[int]bool{61080: true}
	available := func(_ string, _ int) bool {
		return false
	}

	port, err := findAvailablePort(localProxyHost, 61080, used, reusable, available)
	if err != nil {
		t.Fatal(err)
	}
	if port != 61080 {
		t.Fatalf("expected reusable port 61080, got %d", port)
	}
}

func TestTCPPortAvailableDetectsOccupiedPort(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()

	port := listener.Addr().(*net.TCPAddr).Port
	if TCPPortAvailable(localProxyHost, port) {
		t.Fatalf("expected occupied port %d to be unavailable", port)
	}
}
