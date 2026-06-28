package bridge

import (
	"context"
	"net"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/backrunner/xcore-bridge/internal/vless"
)

func TestStartListensAndCloseReleasesPort(t *testing.T) {
	port := freeTCPPort(t)
	server, err := Start(context.Background(), Config{
		Node:      testNode(t),
		LocalHost: "127.0.0.1",
		LocalPort: port,
	})
	if err != nil {
		t.Fatal(err)
	}

	conn, err := net.DialTimeout("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)), time.Second)
	if err != nil {
		t.Fatalf("expected SOCKS inbound to accept TCP connections: %v", err)
	}
	_ = conn.Close()

	if err := server.Close(); err != nil {
		t.Fatal(err)
	}
	eventually(t, time.Second, func() bool {
		listener, err := net.Listen("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)))
		if err != nil {
			return false
		}
		_ = listener.Close()
		return true
	})
}

func TestStartHandlesConcurrentSOCKSHandshakes(t *testing.T) {
	port := freeTCPPort(t)
	server, err := Start(context.Background(), Config{
		Node:      testNode(t),
		LocalHost: "127.0.0.1",
		LocalPort: port,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()

	var wg sync.WaitGroup
	errs := make(chan error, 24)
	for i := 0; i < cap(errs); i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs <- WaitForReady(context.Background(), "127.0.0.1", port, time.Second)
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("concurrent SOCKS5 readiness failed: %v", err)
		}
	}
}

func TestStartFailsWhenLocalPortIsOccupied(t *testing.T) {
	port := freeTCPPort(t)
	listener, err := net.Listen("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)))
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()

	if _, err := Start(context.Background(), Config{
		Node:      testNode(t),
		LocalHost: "127.0.0.1",
		LocalPort: port,
	}); err == nil {
		t.Fatal("expected occupied local port to fail startup")
	}
}

func TestStartFailsWhenUDPPortIsOccupied(t *testing.T) {
	port := freeTCPPort(t)
	listener, err := net.ListenPacket("udp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)))
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()

	if _, err := Start(context.Background(), Config{
		Node:      testNode(t),
		LocalHost: "127.0.0.1",
		LocalPort: port,
	}); err == nil {
		t.Fatal("expected occupied UDP port to fail startup")
	}
}

func TestWaitForReadyRequiresSOCKSHandshake(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	done := make(chan struct{})
	defer func() {
		_ = listener.Close()
		<-done
	}()
	go func() {
		defer close(done)
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			_ = conn.Close()
		}
	}()

	port := listener.Addr().(*net.TCPAddr).Port
	if err := waitForReady(context.Background(), "127.0.0.1", port, 50*time.Millisecond); err == nil {
		t.Fatal("expected non-SOCKS TCP listener to be rejected")
	} else if !strings.Contains(err.Error(), "last error") {
		t.Fatalf("expected readiness error to include last SOCKS failure, got %v", err)
	}
}

func freeTCPPort(t *testing.T) int {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	return listener.Addr().(*net.TCPAddr).Port
}

func testNode(t *testing.T) vless.Node {
	t.Helper()
	node, err := vless.Parse("vless://00000000-0000-0000-0000-000000000000@example.com:443?encryption=none&flow=xtls-rprx-vision&security=reality&sni=www.example.com&fp=chrome&pbk=AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA&sid=0123&type=tcp#Example")
	if err != nil {
		t.Fatal(err)
	}
	return node
}

func eventually(t *testing.T, timeout time.Duration, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("condition was not met before timeout")
}
