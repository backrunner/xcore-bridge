package surge

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRemovePreservesOtherManagedPolicies(t *testing.T) {
	dir := t.TempDir()
	profile := filepath.Join(dir, "surge.conf")
	initial := `[Proxy]
DIRECTISH = direct
# xcore-bridge managed external proxies begin
First = external, exec = "/opt/homebrew/bin/xcore-bridge", args = "run", local-port = 61080
Second = external, exec = "/opt/homebrew/bin/xcore-bridge", args = "run", local-port = 61081
# xcore-bridge managed external proxies end
Manual = ss, 203.0.113.1, 8388, encrypt-method=aes-128-gcm, password=p
`
	if err := os.WriteFile(profile, []byte(initial), 0o644); err != nil {
		t.Fatal(err)
	}
	result, err := Remove(profile, RemoveOptions{
		Names:     []string{"First"},
		WriteFile: false,
	})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(result.Profile, "First = external") {
		t.Fatalf("removed policy still exists:\n%s", result.Profile)
	}
	assertManagedProxyOrder(t, result.Profile, "Second = external")
	if !strings.Contains(result.Profile, "Manual = ss") {
		t.Fatalf("manual proxy was removed:\n%s", result.Profile)
	}
}

func TestRemoveDropsEmptyManagedBlock(t *testing.T) {
	dir := t.TempDir()
	profile := filepath.Join(dir, "surge.conf")
	initial := `[Proxy]
DIRECTISH = direct
# xcore-bridge managed external proxies begin
Only = external, exec = "/opt/homebrew/bin/xcore-bridge", args = "run", local-port = 61080
# xcore-bridge managed external proxies end
Manual = ss, 203.0.113.1, 8388, encrypt-method=aes-128-gcm, password=p
`
	if err := os.WriteFile(profile, []byte(initial), 0o644); err != nil {
		t.Fatal(err)
	}
	result, err := Remove(profile, RemoveOptions{
		Names:     []string{"Only"},
		WriteFile: false,
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, gone := range []string{markerBegin, markerEnd, "Only = external"} {
		if strings.Contains(result.Profile, gone) {
			t.Fatalf("expected %q to be removed:\n%s", gone, result.Profile)
		}
	}
	if !strings.Contains(result.Profile, "Manual = ss") {
		t.Fatalf("manual proxy was removed:\n%s", result.Profile)
	}
}
