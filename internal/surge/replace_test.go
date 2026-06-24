package surge

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReplaceManagedPolicyLinkPreservesNamePortAndOtherLines(t *testing.T) {
	dir := t.TempDir()
	profile := filepath.Join(dir, "surge.conf")
	oldLink := "vless://00000000-0000-0000-0000-000000000000@old.example.com:443?encryption=none#Old"
	newNode := testSurgeNode(t, "Replacement")
	initial := `[Proxy]
Manual = direct
# xcore-bridge managed external proxies begin
Demo = external, exec = "/custom/bin/xcore-bridge", args = "run", args = "--profile", args = "` + profile + `", args = "--local-port", args = "61080", args = "--link", args = "` + oldLink + `", local-port = 61080, udp-relay = true
Keep = external, exec = "/custom/bin/xcore-bridge", args = "run", local-port = 61081
# xcore-bridge managed external proxies end
`
	if err := os.WriteFile(profile, []byte(initial), 0o644); err != nil {
		t.Fatal(err)
	}

	result, err := Replace(profile, ReplaceOptions{
		Name:      "Demo",
		Node:      newNode,
		WriteFile: false,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.PolicyName != "Demo" || result.LocalPort != 61080 {
		t.Fatalf("unexpected replace result %#v", result)
	}
	for _, want := range []string{
		"Manual = direct",
		"Demo = external",
		`exec = "/custom/bin/xcore-bridge"`,
		`args = "--local-port", args = "61080"`,
		`args = "--link", args = "` + newNode.Raw + `"`,
		"local-port = 61080",
		"udp-relay = true",
		"Keep = external",
	} {
		if !strings.Contains(result.Profile, want) {
			t.Fatalf("replaced profile missing %q:\n%s", want, result.Profile)
		}
	}
	if strings.Contains(result.Profile, oldLink) {
		t.Fatalf("old VLESS link still exists:\n%s", result.Profile)
	}
	assertManagedProxyOrder(t, result.Profile, "Demo = external")
}

func TestReplaceWritesBackup(t *testing.T) {
	dir := t.TempDir()
	profile := filepath.Join(dir, "surge.conf")
	oldLink := "vless://00000000-0000-0000-0000-000000000000@old.example.com:443?encryption=none#Old"
	newNode := testSurgeNode(t, "Replacement")
	initial := `[Proxy]
# xcore-bridge managed external proxies begin
Demo = external, exec = "/opt/homebrew/bin/xcore-bridge", args = "run", args = "--local-port", args = "61080", args = "--link", args = "` + oldLink + `", local-port = 61080, udp-relay = true
# xcore-bridge managed external proxies end
`
	if err := os.WriteFile(profile, []byte(initial), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := Replace(profile, ReplaceOptions{
		Name:      "Demo",
		Node:      newNode,
		WriteFile: true,
	}); err != nil {
		t.Fatal(err)
	}
	backup, err := os.ReadFile(profile + ".bak")
	if err != nil {
		t.Fatal(err)
	}
	if string(backup) != initial {
		t.Fatalf("backup should contain original profile:\n%s", backup)
	}
	updated, err := os.ReadFile(profile)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(updated), newNode.Raw) || strings.Contains(string(updated), oldLink) {
		t.Fatalf("profile was not replaced:\n%s", updated)
	}
}
