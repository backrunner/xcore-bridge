package surge

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/backrunner/xcore-bridge/internal/vless"
)

func TestInstallCreatesProxySectionWhenMissing(t *testing.T) {
	dir := t.TempDir()
	profile := filepath.Join(dir, "surge.conf")
	initial := `[General]
loglevel = notify

[Proxy Group]
Group = select, DIRECT

[Rule]
FINAL,DIRECT
`
	if err := os.WriteFile(profile, []byte(initial), 0o644); err != nil {
		t.Fatal(err)
	}
	node := testSurgeNode(t, "Demo")
	result, err := Install(profile, InstallOptions{
		Nodes:     []vless.Node{node},
		ExecPath:  "/opt/homebrew/bin/xcore-bridge",
		BasePort:  61080,
		WriteFile: false,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result.Profile, "[Proxy]\n\n# xcore-bridge managed external proxies begin") {
		t.Fatalf("proxy section was not created before managed block:\n%s", result.Profile)
	}
	if strings.Index(result.Profile, "[Proxy]") > strings.Index(result.Profile, "[Proxy Group]") {
		t.Fatalf("[Proxy] section was inserted after [Proxy Group]:\n%s", result.Profile)
	}
}

func TestInstallHandlesBOMProxySection(t *testing.T) {
	dir := t.TempDir()
	profile := filepath.Join(dir, "surge.conf")
	initial := "\ufeff[Proxy]\nDIRECTISH = direct\n"
	if err := os.WriteFile(profile, []byte(initial), 0o644); err != nil {
		t.Fatal(err)
	}
	result, err := Install(profile, InstallOptions{
		Nodes:     []vless.Node{testSurgeNode(t, "Demo")},
		ExecPath:  "/opt/homebrew/bin/xcore-bridge",
		BasePort:  61080,
		WriteFile: false,
	})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(result.Profile, "[Proxy]") != 1 {
		t.Fatalf("expected existing BOM [Proxy] section to be reused:\n%s", result.Profile)
	}
}
