package surge

import (
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/orchiliao/xcore-bridge/internal/vless"
)

func TestInstallReplacesOnlyManagedProxyBlock(t *testing.T) {
	dir := t.TempDir()
	profile := filepath.Join(dir, "surge.conf")
	initial := `[General]
loglevel = notify

[Proxy]
DIRECTISH = direct
# xcore-bridge managed external proxies
Old = external, exec = "old", args = "old", local-port = 1

[Rule]
FINAL,DIRECT
`
	if err := os.WriteFile(profile, []byte(initial), 0o644); err != nil {
		t.Fatal(err)
	}

	node, err := vless.Parse("vless://00000000-0000-0000-0000-000000000000@203.0.113.10:443?encryption=none&flow=xtls-rprx-vision&security=reality&sni=example.com&fp=chrome&pbk=AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA&sid=0123&type=tcp#Demo")
	if err != nil {
		t.Fatal(err)
	}
	result, err := Install(profile, InstallOptions{
		Nodes:     []vless.Node{node},
		ExecPath:  "/opt/homebrew/bin/xcore-bridge",
		BasePort:  61080,
		WriteFile: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.PolicyNames) != 1 || result.PolicyNames[0] != "Demo" {
		t.Fatalf("unexpected policy names %#v", result.PolicyNames)
	}
	updatedBytes, err := os.ReadFile(profile)
	if err != nil {
		t.Fatal(err)
	}
	updated := string(updatedBytes)
	for _, want := range []string{
		"DIRECTISH = direct",
		"Demo = external",
		"local-port = 61080",
		"[Rule]",
		"FINAL,DIRECT",
	} {
		if !strings.Contains(updated, want) {
			t.Fatalf("updated profile does not contain %q:\n%s", want, updated)
		}
	}
	if strings.Contains(updated, "Old = external") {
		t.Fatalf("old managed line was not removed:\n%s", updated)
	}
	if _, err := os.Stat(profile + ".bak"); err != nil {
		t.Fatalf("backup was not written: %v", err)
	}
}

func TestInstallSkipsExistingNamesAndPorts(t *testing.T) {
	dir := t.TempDir()
	profile := filepath.Join(dir, "surge.conf")
	initial := `[Proxy]
Demo = direct
Other = external, exec = "old", args = "run", local-port = 61080  # occupied

[Proxy Group]
Group = select, Demo
`
	if err := os.WriteFile(profile, []byte(initial), 0o644); err != nil {
		t.Fatal(err)
	}
	node, err := vless.Parse("vless://00000000-0000-0000-0000-000000000000@example.com:443?encryption=none&flow=xtls-rprx-vision&security=reality&sni=example.com&fp=chrome&pbk=AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA&sid=0123&type=tcp#Demo")
	if err != nil {
		t.Fatal(err)
	}
	result, err := Install(profile, InstallOptions{
		Nodes:     []vless.Node{node},
		ExecPath:  "/opt/homebrew/bin/xcore-bridge",
		BasePort:  61080,
		WriteFile: false,
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := result.PolicyNames[0]; got != "Demo 2" {
		t.Fatalf("expected conflicting name to become Demo 2, got %q", got)
	}
	if got := result.LocalPorts[0]; got != 61081 {
		t.Fatalf("expected occupied port to be skipped, got %d", got)
	}
	if strings.Index(result.Profile, "Demo 2 = external") > strings.Index(result.Profile, "[Proxy Group]") {
		t.Fatalf("managed block was inserted after [Proxy Group]:\n%s", result.Profile)
	}
	if !strings.Contains(result.Profile, "Demo = direct") {
		t.Fatalf("existing proxy was removed:\n%s", result.Profile)
	}
}

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

func TestInstallReusesPreviousManagedPortEvenWhenOccupied(t *testing.T) {
	dir := t.TempDir()
	profile := filepath.Join(dir, "surge.conf")
	initial := `[Proxy]
# xcore-bridge managed external proxies begin
Demo = external, exec = "/opt/homebrew/bin/xcore-bridge", args = "run", local-port = 61080
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
# Example = external, exec = "old", args = "run", local-port = 61080
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

func TestInstallDryRunDoesNotWriteFile(t *testing.T) {
	dir := t.TempDir()
	profile := filepath.Join(dir, "surge.conf")
	initial := "[Proxy]\nDIRECTISH = direct\n"
	if err := os.WriteFile(profile, []byte(initial), 0o644); err != nil {
		t.Fatal(err)
	}
	node, err := vless.Parse("vless://00000000-0000-0000-0000-000000000000@example.com:443?encryption=none&flow=xtls-rprx-vision&security=reality&sni=example.com&fp=chrome&pbk=AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA&sid=0123&type=tcp#Demo")
	if err != nil {
		t.Fatal(err)
	}
	result, err := Install(profile, InstallOptions{
		Nodes:     []vless.Node{node},
		ExecPath:  "/opt/homebrew/bin/xcore-bridge",
		BasePort:  61080,
		WriteFile: false,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result.Profile, "Demo = external") {
		t.Fatalf("dry-run profile missing generated line:\n%s", result.Profile)
	}
	if !strings.Contains(result.Profile, markerBegin) || !strings.Contains(result.Profile, markerEnd) {
		t.Fatalf("dry-run profile missing managed markers:\n%s", result.Profile)
	}
	after, err := os.ReadFile(profile)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != initial {
		t.Fatalf("dry-run changed file:\n%s", after)
	}
}

func TestInstallRejectsInvalidNodeBeforeWrite(t *testing.T) {
	dir := t.TempDir()
	profile := filepath.Join(dir, "surge.conf")
	initial := "[Proxy]\nDIRECTISH = direct\n"
	if err := os.WriteFile(profile, []byte(initial), 0o644); err != nil {
		t.Fatal(err)
	}
	node, err := vless.Parse("vless://00000000-0000-0000-0000-000000000000@example.com:443?encryption=none&flow=xtls-rprx-vision&security=reality&sni=example.com&fp=chrome&pbk=AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA&sid=abc&type=tcp#Demo")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Install(profile, InstallOptions{
		Nodes:     []vless.Node{node},
		ExecPath:  "/opt/homebrew/bin/xcore-bridge",
		BasePort:  61080,
		WriteFile: true,
	}); err == nil {
		t.Fatal("expected invalid shortId to be rejected")
	}
	after, err := os.ReadFile(profile)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != initial {
		t.Fatalf("invalid node changed file:\n%s", after)
	}
}

func TestInstallDoesNotDeleteUserProxyAfterLegacyMarker(t *testing.T) {
	dir := t.TempDir()
	profile := filepath.Join(dir, "surge.conf")
	initial := `[Proxy]
DIRECTISH = direct
# xcore-bridge managed external proxies
Old = external, exec = "/opt/homebrew/bin/xcore-bridge", args = "run", local-port = 61080
Manual = ss, 203.0.113.1, 8388, encrypt-method=aes-128-gcm, password=p

[Rule]
FINAL,DIRECT
`
	if err := os.WriteFile(profile, []byte(initial), 0o644); err != nil {
		t.Fatal(err)
	}
	node, err := vless.Parse("vless://00000000-0000-0000-0000-000000000000@example.com:443?encryption=none&flow=xtls-rprx-vision&security=reality&sni=example.com&fp=chrome&pbk=AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA&sid=0123&type=tcp#Demo")
	if err != nil {
		t.Fatal(err)
	}
	result, err := Install(profile, InstallOptions{
		Nodes:     []vless.Node{node},
		ExecPath:  "/opt/homebrew/bin/xcore-bridge",
		BasePort:  61080,
		WriteFile: false,
	})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(result.Profile, "Old = external") {
		t.Fatalf("legacy managed line was not removed:\n%s", result.Profile)
	}
	if !strings.Contains(result.Profile, "Manual = ss") {
		t.Fatalf("manual proxy after legacy marker was removed:\n%s", result.Profile)
	}
}

func TestInstallRejectsInvalidBasePort(t *testing.T) {
	dir := t.TempDir()
	profile := filepath.Join(dir, "surge.conf")
	if err := os.WriteFile(profile, []byte("[Proxy]\nDIRECTISH = direct\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	node, err := vless.Parse("vless://00000000-0000-0000-0000-000000000000@example.com:443?encryption=none&flow=xtls-rprx-vision&security=reality&sni=example.com&fp=chrome&pbk=AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA&sid=0123&type=tcp#Demo")
	if err != nil {
		t.Fatal(err)
	}
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

func TestInstallDoesNotDeleteUserProxyAfterUnclosedManagedMarker(t *testing.T) {
	dir := t.TempDir()
	profile := filepath.Join(dir, "surge.conf")
	initial := `[Proxy]
DIRECTISH = direct
# xcore-bridge managed external proxies begin
Old = external, exec = "/opt/homebrew/bin/xcore-bridge", args = "run", local-port = 61080
Manual = ss, 203.0.113.1, 8388, encrypt-method=aes-128-gcm, password=p
`
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
	if strings.Contains(result.Profile, "Old = external") {
		t.Fatalf("old managed line was not removed:\n%s", result.Profile)
	}
	if !strings.Contains(result.Profile, "Manual = ss") {
		t.Fatalf("manual proxy after unclosed marker was removed:\n%s", result.Profile)
	}
}

func testSurgeNode(t *testing.T, name string) vless.Node {
	t.Helper()
	node, err := vless.Parse("vless://00000000-0000-0000-0000-000000000000@example.com:443?encryption=none&flow=xtls-rprx-vision&security=reality&sni=example.com&fp=chrome&pbk=AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA&sid=0123&type=tcp#" + name)
	if err != nil {
		t.Fatal(err)
	}
	return node
}

func listenOnTestPort(t *testing.T) (net.Listener, int) {
	t.Helper()
	for port := 61080; port < 65535; port++ {
		listener, err := net.Listen("tcp", net.JoinHostPort(localProxyHost, strconv.Itoa(port)))
		if err == nil {
			return listener, port
		}
	}
	t.Skip("no available local test port below 65535")
	return nil, 0
}
