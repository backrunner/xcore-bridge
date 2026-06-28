package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/backrunner/xcore-bridge/internal/bridge"
	"github.com/backrunner/xcore-bridge/internal/daemon"
	"github.com/backrunner/xcore-bridge/internal/vless"
)

func TestRunManagedPolicyOwnsListenerLifecycle(t *testing.T) {
	withRunDaemonStatus(t, daemon.Status{})
	withRunLogSink(t)
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

func TestRunManagedPolicyReusesMatchingDaemon(t *testing.T) {
	const profile = "test-profile.conf"
	const port = 61080
	node, err := vless.Parse(testLink("Managed"))
	if err != nil {
		t.Fatal(err)
	}
	withRunLogSink(t)
	withRunDaemonStatus(t, daemon.Status{
		Running:     true,
		PID:         1234,
		ProfilePath: profile,
		Policies: []daemon.Policy{
			{Name: "Managed", LocalHost: "127.0.0.1", LocalPort: port, LinkHash: daemon.PolicyLinkHash(node.Raw)},
		},
	})
	waitCalls := 0
	oldWait := waitForDaemonPolicy
	waitForDaemonPolicy = func(context.Context, string, int, time.Duration) error {
		waitCalls++
		return nil
	}
	t.Cleanup(func() { waitForDaemonPolicy = oldWait })
	oldStart := startBridgeServer
	startBridgeServer = func(context.Context, bridge.Config) (*bridge.Server, error) {
		return nil, errors.New("bridge.Start should not be called when daemon can serve the policy")
	}
	t.Cleanup(func() { startBridgeServer = oldStart })

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	var stdout bytes.Buffer
	go func() {
		done <- runManagedPolicy(ctx, node, profile, "127.0.0.1", port, "warning", &stdout)
	}()

	eventuallyRun(t, time.Second, func() bool {
		return strings.Contains(stdout.String(), "mode: daemon")
	})
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("run did not stop after context cancellation")
	}
	if waitCalls == 0 {
		t.Fatal("expected run to wait for daemon policy readiness")
	}
}

func TestRunManagedPolicyRefusesStaleSameProfileDaemon(t *testing.T) {
	const profile = "test-profile.conf"
	const port = 61080
	node, err := vless.Parse(testLink("Managed"))
	if err != nil {
		t.Fatal(err)
	}
	withRunLogSink(t)
	withRunDaemonStatus(t, daemon.Status{
		Running:     true,
		PID:         1234,
		ProfilePath: profile,
		Policies: []daemon.Policy{
			{Name: "Other", LocalHost: "127.0.0.1", LocalPort: 61081},
		},
	})
	oldStart := startBridgeServer
	startBridgeServer = func(context.Context, bridge.Config) (*bridge.Server, error) {
		return nil, errors.New("bridge.Start should not be called while same-profile daemon is stale")
	}
	t.Cleanup(func() { startBridgeServer = oldStart })

	err = runManagedPolicy(context.Background(), node, profile, "127.0.0.1", port, "warning", &bytes.Buffer{})
	if err == nil {
		t.Fatal("expected stale daemon to be rejected")
	}
	if !strings.Contains(err.Error(), "does not expose") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRunManagedPolicyRefusesSamePortDaemonWithDifferentLink(t *testing.T) {
	const profile = "test-profile.conf"
	const port = 61080
	node, err := vless.Parse(testLink("Managed"))
	if err != nil {
		t.Fatal(err)
	}
	withRunLogSink(t)
	withRunDaemonStatus(t, daemon.Status{
		Running:     true,
		PID:         1234,
		ProfilePath: profile,
		Policies: []daemon.Policy{
			{Name: "Managed", LocalHost: "127.0.0.1", LocalPort: port, LinkHash: daemon.PolicyLinkHash(testLink("Old"))},
		},
	})
	oldStart := startBridgeServer
	startBridgeServer = func(context.Context, bridge.Config) (*bridge.Server, error) {
		return nil, errors.New("bridge.Start should not be called when daemon link hash differs")
	}
	t.Cleanup(func() { startBridgeServer = oldStart })

	err = runManagedPolicy(context.Background(), node, profile, "127.0.0.1", port, "warning", &bytes.Buffer{})
	if err == nil {
		t.Fatal("expected stale daemon link to be rejected")
	}
	if !strings.Contains(err.Error(), "does not expose") {
		t.Fatalf("unexpected error: %v", err)
	}
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

func withRunDaemonStatus(t *testing.T, status daemon.Status) {
	t.Helper()
	old := getDaemonStatus
	getDaemonStatus = func(daemon.Options) (daemon.Status, error) {
		return status, nil
	}
	t.Cleanup(func() { getDaemonStatus = old })
}

func withRunLogSink(t *testing.T) {
	t.Helper()
	oldLogPath := bridgeRuntimeLogPath
	oldAppend := appendRuntimeLog
	bridgeRuntimeLogPath = func() (string, error) {
		return "", fmt.Errorf("disabled in test")
	}
	appendRuntimeLog = func(string, ...any) error {
		return nil
	}
	t.Cleanup(func() {
		bridgeRuntimeLogPath = oldLogPath
		appendRuntimeLog = oldAppend
	})
}
