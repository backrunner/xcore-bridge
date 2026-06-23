package surge

import (
	"os"
	"path/filepath"
	"testing"
)

func TestManagedPoliciesParsesGeneratedRunArgs(t *testing.T) {
	dir := t.TempDir()
	profile := filepath.Join(dir, "surge.conf")
	link := testLinkForManagedPolicy("Demo")
	initial := `[Proxy]
DIRECTISH = direct
# xcore-bridge managed external proxies begin
Demo = external, exec = "/opt/homebrew/bin/xcore-bridge", args = "run", args = "--profile", args = "/tmp/surge.conf", args = "--local-port", args = "61080", args = "--link", args = "` + link + `", local-port = 61080, udp-relay = true
# xcore-bridge managed external proxies end
Manual = direct
`
	if err := os.WriteFile(profile, []byte(initial), 0o644); err != nil {
		t.Fatal(err)
	}
	policies, err := ManagedPolicies(profile)
	if err != nil {
		t.Fatal(err)
	}
	if len(policies) != 1 {
		t.Fatalf("expected one managed policy, got %#v", policies)
	}
	got := policies[0]
	if got.Name != "Demo" || got.Link != link || got.LocalHost != "127.0.0.1" || got.LocalPort != 61080 || got.RunHost != "127.0.0.1" || got.RunPort != 61080 {
		t.Fatalf("unexpected managed policy: %#v", got)
	}
}

func TestManagedPoliciesRejectsMismatchedRunPort(t *testing.T) {
	dir := t.TempDir()
	profile := filepath.Join(dir, "surge.conf")
	link := testLinkForManagedPolicy("Demo")
	initial := `[Proxy]
# xcore-bridge managed external proxies begin
Demo = external, exec = "/opt/homebrew/bin/xcore-bridge", args = "run", args = "--local-port", args = "61081", args = "--link", args = "` + link + `", local-port = 61080, udp-relay = true
# xcore-bridge managed external proxies end
`
	if err := os.WriteFile(profile, []byte(initial), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := ManagedPolicies(profile); err == nil {
		t.Fatal("expected mismatched run/local ports to be rejected")
	}
}

func testLinkForManagedPolicy(name string) string {
	return "vless://00000000-0000-0000-0000-000000000000@example.com:443?encryption=none&flow=xtls-rprx-vision&security=reality&sni=example.com&fp=chrome&pbk=AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA&sid=0123&type=tcp#" + name
}
