package surge

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/backrunner/xcore-bridge/internal/vless"
)

func TestInstallOverwritesSingleBackup(t *testing.T) {
	dir := t.TempDir()
	profile := filepath.Join(dir, "surge.conf")
	initial := "[Proxy]\nDIRECTISH = direct\n"
	if err := os.WriteFile(profile, []byte(initial), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Install(profile, InstallOptions{
		Nodes:     []vless.Node{testSurgeNode(t, "First")},
		ExecPath:  "/opt/homebrew/bin/xcore-bridge",
		BasePort:  61080,
		WriteFile: true,
	}); err != nil {
		t.Fatal(err)
	}
	firstBackup, err := os.ReadFile(profile + ".bak")
	if err != nil {
		t.Fatal(err)
	}
	if string(firstBackup) != initial {
		t.Fatalf("first backup should contain the original profile:\n%s", firstBackup)
	}

	beforeSecond, err := os.ReadFile(profile)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Install(profile, InstallOptions{
		Nodes:     []vless.Node{testSurgeNode(t, "Second")},
		ExecPath:  "/opt/homebrew/bin/xcore-bridge",
		BasePort:  61080,
		WriteFile: true,
	}); err != nil {
		t.Fatal(err)
	}
	secondBackup, err := os.ReadFile(profile + ".bak")
	if err != nil {
		t.Fatal(err)
	}
	if string(secondBackup) != string(beforeSecond) {
		t.Fatalf("backup should be overwritten with the immediate previous profile:\n%s", secondBackup)
	}
	matches, err := filepath.Glob(profile + ".bak*")
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 1 || matches[0] != profile+".bak" {
		t.Fatalf("expected only one backup, got %#v", matches)
	}
}
