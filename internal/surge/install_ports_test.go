package surge

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/backrunner/xcore-bridge/internal/vless"
)

func TestInstallSkipsLocallyOccupiedPorts(t *testing.T) {
	listener, basePort := listenOnTestPort(t)
	defer listener.Close()

	dir := t.TempDir()
	profile := filepath.Join(dir, "surge.conf")
	if err := os.WriteFile(profile, []byte("[Proxy]\nDIRECTISH = direct\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	result, err := Install(profile, InstallOptions{
		Nodes:     []vless.Node{testSurgeNode(t, "Demo")},
		ExecPath:  "/opt/homebrew/bin/xcore-bridge",
		BasePort:  basePort,
		WriteFile: false,
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := result.LocalPorts[0]; got == basePort {
		t.Fatalf("expected occupied port %d to be skipped", basePort)
	}
}

func TestInstallAllocatesDistinctPortsForSeveralChildren(t *testing.T) {
	dir := t.TempDir()
	profile := filepath.Join(dir, "surge.conf")
	if err := os.WriteFile(profile, []byte("[Proxy]\nDIRECTISH = direct\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	result, err := Install(profile, InstallOptions{
		Nodes: []vless.Node{
			testSurgeNode(t, "First"),
			testSurgeNode(t, "Second"),
			testSurgeNode(t, "Third"),
		},
		ExecPath:  "/opt/homebrew/bin/xcore-bridge",
		BasePort:  61080,
		WriteFile: false,
		portAvailable: func(_ string, _ int) bool {
			return true
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	want := []int{61080, 61081, 61082}
	if len(result.LocalPorts) != len(want) {
		t.Fatalf("unexpected allocated ports %#v", result.LocalPorts)
	}
	for i, port := range result.LocalPorts {
		if port != want[i] {
			t.Fatalf("child %d got port %d, want %d", i, port, want[i])
		}
	}
}

func TestInstallReusesPreviousManagedPortEvenWhenOccupied(t *testing.T) {
	dir := t.TempDir()
	profile := filepath.Join(dir, "surge.conf")
	initial := `[Proxy]
# xcore-bridge managed external proxies begin
` + testManagedProxyLine(t, profile, "Demo", 61080) + `
# xcore-bridge managed external proxies end
`
	if err := os.WriteFile(profile, []byte(initial), 0o644); err != nil {
		t.Fatal(err)
	}
	result, err := Install(profile, InstallOptions{
		Nodes:     []vless.Node{testSurgeNode(t, "Demo")},
		ExecPath:  "/opt/homebrew/bin/xcore-bridge",
		BasePort:  61080,
		WriteFile: false,
		portAvailable: func(_ string, port int) bool {
			return port != 61080
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := result.LocalPorts[0]; got != 61080 {
		t.Fatalf("expected previous managed port to be reused, got %d", got)
	}
}

func TestInstallIgnoresCommentedLocalPorts(t *testing.T) {
	dir := t.TempDir()
	profile := filepath.Join(dir, "surge.conf")
	initial := `[Proxy]
# Example = external, exec = "/opt/homebrew/bin/xcore-bridge", args = "run", args = "--profile", args = "` + profile + `", args = "--local-port", args = "61080", args = "--link", args = "` + testSurgeNode(t, "Example").Raw + `", local-port = 61080, udp-relay = true
DIRECTISH = direct
`
	if err := os.WriteFile(profile, []byte(initial), 0o644); err != nil {
		t.Fatal(err)
	}
	result, err := Install(profile, InstallOptions{
		Nodes:     []vless.Node{testSurgeNode(t, "Demo")},
		ExecPath:  "/opt/homebrew/bin/xcore-bridge",
		BasePort:  61080,
		WriteFile: false,
		portAvailable: func(_ string, _ int) bool {
			return true
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := result.LocalPorts[0]; got != 61080 {
		t.Fatalf("expected commented local-port to be ignored, got %d", got)
	}
}

func TestInstallRejectsInvalidBasePort(t *testing.T) {
	dir := t.TempDir()
	profile := filepath.Join(dir, "surge.conf")
	if err := os.WriteFile(profile, []byte("[Proxy]\nDIRECTISH = direct\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	node := testSurgeNode(t, "Demo")
	if _, err := Install(profile, InstallOptions{
		Nodes:    []vless.Node{node},
		BasePort: 70000,
	}); err == nil {
		t.Fatal("expected invalid base port to be rejected")
	}
	if _, err := Install(profile, InstallOptions{
		Nodes:    []vless.Node{node},
		BasePort: -1,
	}); err == nil {
		t.Fatal("expected negative base port to be rejected")
	}
}

func TestInstallDefaultBasePortWhenZero(t *testing.T) {
	dir := t.TempDir()
	profile := filepath.Join(dir, "surge.conf")
	if err := os.WriteFile(profile, []byte("[Proxy]\nDIRECTISH = direct\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	result, err := Install(profile, InstallOptions{
		Nodes:    []vless.Node{testSurgeNode(t, "Demo")},
		BasePort: 0,
		portAvailable: func(_ string, port int) bool {
			return port == 61080
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := result.LocalPorts[0]; got != 61080 {
		t.Fatalf("expected default base port 61080, got %d", got)
	}
}
