package main

import (
	"bytes"
	"context"
	"net"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/backrunner/xcore-bridge/internal/bridge"
	"github.com/backrunner/xcore-bridge/internal/vless"
)

func TestRunManagedPolicyOwnsListenerLifecycle(t *testing.T) {
	node, err := vless.Parse(testLink("Managed"))
	if err != nil {
		t.Fatal(err)
	}
	port := freeRunTCPPort(t)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	var stdout bytes.Buffer

	go func() {
		done <- runManagedPolicy(ctx, node, "test-profile.conf", "127.0.0.1", port, "warning", &stdout)
	}()

	if err := bridge.WaitForReady(context.Background(), "127.0.0.1", port, time.Second); err != nil {
		cancel()
		t.Fatalf("run did not start its SOCKS5 listener: %v", err)
	}

	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("run did not stop after context cancellation")
	}
	if output := stdout.String(); !strings.Contains(output, "xcore-bridge ready") || !strings.Contains(output, "pid: ") {
		t.Fatalf("unexpected run output:\n%s", output)
	}
	eventuallyRun(t, time.Second, func() bool {
		listener, err := net.Listen("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)))
		if err != nil {
			return false
		}
		_ = listener.Close()
		return true
	})
}

func freeRunTCPPort(t *testing.T) int {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	return listener.Addr().(*net.TCPAddr).Port
}

func eventuallyRun(t *testing.T, timeout time.Duration, condition func() bool) {
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
