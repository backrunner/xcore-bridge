package daemon

import (
	"context"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"
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
	if cfg.Policies[1].Name != "Second" || cfg.Policies[1].LocalPort != 61081 {
		t.Fatalf("unexpected second bridge policy: %#v", cfg.Policies[1])
	}
}

func TestSamePoliciesDetectsProfileChanges(t *testing.T) {
	current := []Policy{{Name: "First", LocalHost: "127.0.0.1", LocalPort: 61080}}
	if !samePolicies(current, []Policy{{Name: "First", LocalHost: "127.0.0.1", LocalPort: 61080}}) {
		t.Fatal("expected matching policies to compare equal")
	}
	if samePolicies(current, []Policy{{Name: "First", LocalHost: "127.0.0.1", LocalPort: 61081}}) {
		t.Fatal("expected changed local port to compare different")
	}
	if samePolicies(current, append(current, Policy{Name: "Second", LocalHost: "127.0.0.1", LocalPort: 61081})) {
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

func testDaemonLink(name string) string {
	return "vless://00000000-0000-0000-0000-000000000000@example.com:443?encryption=none&flow=xtls-rprx-vision&security=reality&sni=example.com&fp=chrome&pbk=AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA&sid=0123&type=tcp#" + name
}
