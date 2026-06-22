package surge

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/backrunner/xcore-bridge/internal/vless"
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
	assertManagedProxyOrder(t, result.Profile, "Demo 2 = external")
}

func TestAddAppendsInsideExistingManagedBlock(t *testing.T) {
	dir := t.TempDir()
	profile := filepath.Join(dir, "surge.conf")
	initial := `[Proxy]
DIRECTISH = direct
# xcore-bridge managed external proxies begin
First = external, exec = "/opt/homebrew/bin/xcore-bridge", args = "run", local-port = 61080
# xcore-bridge managed external proxies end
Manual = ss, 203.0.113.1, 8388, encrypt-method=aes-128-gcm, password=p
`
	if err := os.WriteFile(profile, []byte(initial), 0o644); err != nil {
		t.Fatal(err)
	}
	result, err := Add(profile, InstallOptions{
		Nodes:     []vless.Node{testSurgeNode(t, "Second")},
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
	if strings.Count(result.Profile, markerBegin) != 1 || strings.Count(result.Profile, markerEnd) != 1 {
		t.Fatalf("expected a single managed block:\n%s", result.Profile)
	}
	for _, want := range []string{"First = external", "Second = external"} {
		assertManagedProxyOrder(t, result.Profile, want)
	}
	if !strings.Contains(result.Profile, "Manual = ss") {
		t.Fatalf("manual proxy was removed:\n%s", result.Profile)
	}
	if got := result.LocalPorts[0]; got != 61081 {
		t.Fatalf("expected new add to avoid existing managed port, got %d", got)
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
