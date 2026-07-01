package surge

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRenameManagedPolicyPreservesOtherLines(t *testing.T) {
	dir := t.TempDir()
	profile := filepath.Join(dir, "surge.conf")
	initial := `[Proxy]
Manual = direct
# xcore-bridge managed external proxies begin
` + testManagedProxyLine(t, profile, "Old", 61080) + `
` + testManagedProxyLine(t, profile, "Keep", 61081) + `
# xcore-bridge managed external proxies end
`
	if err := os.WriteFile(profile, []byte(initial), 0o644); err != nil {
		t.Fatal(err)
	}
	result, err := Rename(profile, RenameOptions{
		From:      "Old",
		To:        "New",
		WriteFile: false,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.OldName != "Old" || result.NewName != "New" {
		t.Fatalf("unexpected rename result %#v", result)
	}
	for _, want := range []string{"Manual = direct", "New = external", "Keep = external"} {
		if !strings.Contains(result.Profile, want) {
			t.Fatalf("renamed profile missing %q:\n%s", want, result.Profile)
		}
	}
	if strings.Contains(result.Profile, "Old = external") {
		t.Fatalf("old policy name still exists:\n%s", result.Profile)
	}
	assertManagedProxyOrder(t, result.Profile, "New = external")
}

func TestRenameAvoidsExistingPolicyNames(t *testing.T) {
	dir := t.TempDir()
	profile := filepath.Join(dir, "surge.conf")
	initial := `[Proxy]
Target = direct
# xcore-bridge managed external proxies begin
` + testManagedProxyLine(t, profile, "Old", 61080) + `
# xcore-bridge managed external proxies end
`
	if err := os.WriteFile(profile, []byte(initial), 0o644); err != nil {
		t.Fatal(err)
	}
	result, err := Rename(profile, RenameOptions{
		From:      "Old",
		To:        "Target",
		WriteFile: false,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.NewName != "Target 2" {
		t.Fatalf("expected uniqued target name, got %q", result.NewName)
	}
	if !strings.Contains(result.Profile, "Target 2 = external") {
		t.Fatalf("renamed profile missing unique name:\n%s", result.Profile)
	}
}

func TestRenameWritesBackup(t *testing.T) {
	dir := t.TempDir()
	profile := filepath.Join(dir, "surge.conf")
	initial := `[Proxy]
# xcore-bridge managed external proxies begin
` + testManagedProxyLine(t, profile, "Old", 61080) + `
# xcore-bridge managed external proxies end
`
	if err := os.WriteFile(profile, []byte(initial), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Rename(profile, RenameOptions{
		From:      "Old",
		To:        "New",
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
	if !strings.Contains(string(updated), "New = external") {
		t.Fatalf("profile was not renamed:\n%s", updated)
	}
}
