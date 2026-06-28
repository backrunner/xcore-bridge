package daemon

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/backrunner/xcore-bridge/internal/bridge"
)

func TestConfigFromProfileUsesManagedPolicies(t *testing.T) {
	dir := t.TempDir()
	profile := filepath.Join(dir, "surge.conf")
	first := testDaemonLink("First")
	second := testDaemonLink("Second")
	initial := `[Proxy]
DIRECTISH = direct
# xcore-bridge managed external proxies begin
First = external, exec = "/opt/homebrew/bin/xcore-bridge", args = "run", args = "--profile", args = "` + profile + `", args = "--local-port", args = "61080", args = "--link", args = "` + first + `", local-port = 61080, udp-relay = true
Second = external, exec = "/opt/homebrew/bin/xcore-bridge", args = "run", args = "--profile", args = "` + profile + `", args = "--local-port", args = "61081", args = "--link", args = "` + second + `", local-port = 61081, udp-relay = true
# xcore-bridge managed external proxies end
Manual = direct
`
	if err := os.WriteFile(profile, []byte(initial), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, policies, err := ConfigFromProfile(profile, "debug")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.LogLevel != "debug" {
		t.Fatalf("unexpected log level %q", cfg.LogLevel)
	}
	if len(cfg.Policies) != 2 || len(policies) != 2 {
		t.Fatalf("expected two policies, got %#v / %#v", cfg.Policies, policies)
	}
	if policies[0].Name != "First" || policies[0].LocalHost != "127.0.0.1" || policies[0].LocalPort != 61080 {
		t.Fatalf("unexpected first status policy: %#v", policies[0])
	}
	if policies[0].LinkHash == "" || policies[0].LinkHash == first {
		t.Fatalf("expected first policy to store a link hash, got %#v", policies[0])
	}
	if cfg.Policies[1].Name != "Second" || cfg.Policies[1].LocalPort != 61081 {
		t.Fatalf("unexpected second bridge policy: %#v", cfg.Policies[1])
	}
}

func TestServeWritesStateBeforeStartingBridge(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	profile := filepath.Join(t.TempDir(), "surge.conf")
	link := testDaemonLink("Managed")
	initial := `[Proxy]
# xcore-bridge managed external proxies begin
Managed = external, exec = "/opt/homebrew/bin/xcore-bridge", args = "run", args = "--profile", args = "` + profile + `", args = "--local-port", args = "61080", args = "--link", args = "` + link + `", local-port = 61080, udp-relay = true
# xcore-bridge managed external proxies end
`
	if err := os.WriteFile(profile, []byte(initial), 0o644); err != nil {
		t.Fatal(err)
	}

	oldStart := startBridgeMulti
	started := make(chan struct{})
	checked := make(chan error, 1)
	startBridgeMulti = func(context.Context, bridge.MultiConfig) (*bridge.Server, error) {
		paths, err := Paths()
		if err != nil {
			checked <- err
		} else {
			state, err := readState(paths)
			if err != nil {
				checked <- err
			} else if len(state.Policies) != 1 || state.Policies[0].LinkHash != PolicyLinkHash(link) {
				checked <- errors.New("state did not include expected policy before bridge start")
			} else {
				checked <- nil
			}
		}
		close(started)
		return &bridge.Server{}, nil
	}
	t.Cleanup(func() { startBridgeMulti = oldStart })

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- Serve(ctx, Options{ProfilePath: profile})
	}()
	select {
	case <-started:
	case <-time.After(time.Second):
		cancel()
		t.Fatal("bridge start was not called")
	}
	if err := <-checked; err != nil {
		cancel()
		t.Fatal(err)
	}
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("serve did not stop after context cancellation")
	}
}

func TestSamePoliciesDetectsProfileChanges(t *testing.T) {
	current := []Policy{{Name: "First", LocalHost: "127.0.0.1", LocalPort: 61080, LinkHash: PolicyLinkHash("vless://old")}}
	if !samePolicies(current, []Policy{{Name: "First", LocalHost: "127.0.0.1", LocalPort: 61080, LinkHash: PolicyLinkHash("vless://old")}}) {
		t.Fatal("expected matching policies to compare equal")
	}
	if samePolicies(current, []Policy{{Name: "First", LocalHost: "127.0.0.1", LocalPort: 61081, LinkHash: PolicyLinkHash("vless://old")}}) {
		t.Fatal("expected changed local port to compare different")
	}
	if samePolicies(current, []Policy{{Name: "First", LocalHost: "127.0.0.1", LocalPort: 61080, LinkHash: PolicyLinkHash("vless://new")}}) {
		t.Fatal("expected changed link hash to compare different")
	}
	if samePolicies(current, append(current, Policy{Name: "Second", LocalHost: "127.0.0.1", LocalPort: 61081, LinkHash: PolicyLinkHash("vless://second")})) {
		t.Fatal("expected added policy to compare different")
	}
}

func TestControlLockSerializesDaemonOperations(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	paths, err := Paths()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(paths.Dir, 0o755); err != nil {
		t.Fatal(err)
	}
	lock, err := os.OpenFile(paths.Lock, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Close()
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX); err != nil {
		t.Fatal(err)
	}

	acquired := make(chan struct{})
	errs := make(chan error, 1)
	go func() {
		_, err := withControlLock(context.Background(), Options{Timeout: time.Second}, func() (Status, error) {
			close(acquired)
			return Status{}, nil
		})
		errs <- err
	}()

	select {
	case <-acquired:
		t.Fatal("lock was acquired while another process held it")
	case <-time.After(100 * time.Millisecond):
	}
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_UN); err != nil {
		t.Fatal(err)
	}
	select {
	case <-acquired:
	case <-time.After(time.Second):
		t.Fatal("lock was not acquired after release")
	}
	if err := <-errs; err != nil {
		t.Fatal(err)
	}
}

func TestRenderLaunchAgentPlistEscapesArguments(t *testing.T) {
	data := renderLaunchAgentPlist(
		"io.github.backrunner.xcore-bridge.daemon",
		[]string{"/tmp/xcore-bridge", "daemon", "serve", "--profile", "/tmp/A&B <profile>.conf"},
		"/tmp/xcore-bridge daemon.log",
	)
	text := string(data)
	for _, want := range []string{
		"<key>KeepAlive</key>",
		"<true/>",
		"<string>/tmp/A&amp;B &lt;profile&gt;.conf</string>",
		"<key>StandardErrorPath</key>",
		"<string>/tmp/xcore-bridge daemon.log</string>",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("expected %q in plist:\n%s", want, text)
		}
	}
}

func TestGetStatusReportsInstalledLaunchAgentWithoutPID(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	agentPath := filepath.Join(home, "Library", "LaunchAgents", launchAgentLabel()+".plist")
	if err := os.MkdirAll(filepath.Dir(agentPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(agentPath, []byte("plist"), 0o644); err != nil {
		t.Fatal(err)
	}

	status, err := GetStatus(Options{ProfilePath: "/tmp/surge.conf"})
	if err != nil {
		t.Fatal(err)
	}
	if !status.LaunchAgent {
		t.Fatalf("expected launch agent status, got %#v", status)
	}
	if status.Running {
		t.Fatalf("status should not report running without pid: %#v", status)
	}
}

func testDaemonLink(name string) string {
	return "vless://00000000-0000-0000-0000-000000000000@example.com:443?encryption=none&flow=xtls-rprx-vision&security=reality&sni=example.com&fp=chrome&pbk=AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA&sid=0123&type=tcp#" + name
}
