package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAddDryRunUsesDefaultExecutableAndManagedBlock(t *testing.T) {
	dir := t.TempDir()
	profile := filepath.Join(dir, "surge.conf")
	if err := os.WriteFile(profile, []byte("[Proxy]\nDIRECTISH = direct\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	links := writeLinksFile(t, dir)

	var stdout bytes.Buffer
	err := run([]string{"add", "--dry-run", "--profile", profile, "--links-file", links}, &stdout, &bytes.Buffer{})
	if err != nil {
		t.Fatal(err)
	}
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	output := stdout.String()
	for _, want := range []string{
		"Demo = external",
		`exec = "` + exe + `"`,
		"udp-relay = true",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("expected %q in output:\n%s", want, output)
		}
	}
	assertInsideManagedBlock(t, output, "Demo = external")
}

func TestVersionVerboseIncludesXrayCoreVersion(t *testing.T) {
	var stdout bytes.Buffer
	if err := run([]string{"version", "--verbose"}, &stdout, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	output := stdout.String()
	if !strings.Contains(output, "xcore-bridge ") {
		t.Fatalf("verbose version missing xcore-bridge version:\n%s", output)
	}
	if !strings.Contains(output, "xray-core ") {
		t.Fatalf("verbose version missing xray-core version:\n%s", output)
	}
}

func TestRunRejectsBothLinkForms(t *testing.T) {
	err := run([]string{
		"xray-config",
		"--link", "vless://00000000-0000-0000-0000-000000000000@example.com:443?encryption=none#A",
		"vless://00000000-0000-0000-0000-000000000000@example.com:443?encryption=none#B",
	}, &bytes.Buffer{}, &bytes.Buffer{})
	if err == nil {
		t.Fatal("expected mixed --link and positional link to be rejected")
	}
}

func TestOldSurgeCommandsAreRemoved(t *testing.T) {
	for _, command := range []string{"surge-line", "surge-install"} {
		if err := run([]string{command}, &bytes.Buffer{}, &bytes.Buffer{}); err == nil {
			t.Fatalf("expected old command %q to be removed", command)
		}
	}
}

func TestAddLinksFileAcceptsUppercaseScheme(t *testing.T) {
	dir := t.TempDir()
	profile := filepath.Join(dir, "surge.conf")
	if err := os.WriteFile(profile, []byte("[Proxy]\nDIRECTISH = direct\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	links := filepath.Join(dir, "links.txt")
	if err := os.WriteFile(links, []byte(testLink("Demo")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	var stdout bytes.Buffer
	err := run([]string{"add", "--dry-run", "--profile", profile, "--links-file", links}, &stdout, &bytes.Buffer{})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stdout.String(), "Demo = external") {
		t.Fatalf("expected generated proxy line, got:\n%s", stdout.String())
	}
}

func TestAddAutoDiscoversICloudProfileAndConfirmsFirstWrite(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	profiles := filepath.Join(home, "Library", "Mobile Documents", "iCloud~com~nssurge~Inc~Surge", "Documents", "Profiles")
	if err := os.MkdirAll(profiles, 0o755); err != nil {
		t.Fatal(err)
	}
	profile := filepath.Join(profiles, "default.conf")
	if err := os.WriteFile(profile, []byte("[Proxy]\nDIRECTISH = direct\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer
	err := runWithIO([]string{
		"add",
		"--links-file", writeLinksFile(t, home),
	}, &stdout, &stderr, strings.NewReader("y\n"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stdout.String(), "added 1 external proxy policies into "+profile) {
		t.Fatalf("unexpected add output:\n%s", stdout.String())
	}
	if !strings.Contains(stdout.String(), "backup: "+profile+".bak") {
		t.Fatalf("add output should mention backup:\n%s", stdout.String())
	}
	if !strings.Contains(stderr.String(), "using "+profile) && !strings.Contains(stderr.String(), "found Surge profile "+profile) {
		t.Fatalf("expected discovery notice, got:\n%s", stderr.String())
	}
	updated, err := os.ReadFile(profile)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(updated), "Demo = external") {
		t.Fatalf("profile was not updated:\n%s", updated)
	}
	assertInsideManagedBlock(t, string(updated), "Demo = external")
}

func TestAddRequiresConfirmationForFirstWrite(t *testing.T) {
	dir := t.TempDir()
	profile := filepath.Join(dir, "surge.conf")
	if err := os.WriteFile(profile, []byte("[Proxy]\nDIRECTISH = direct\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer
	err := runWithIO([]string{
		"add",
		"--profile", profile,
		"--links-file", writeLinksFile(t, dir),
	}, &stdout, &stderr, strings.NewReader("n\n"))
	if err == nil {
		t.Fatal("expected missing confirmation to fail")
	}
	after, readErr := os.ReadFile(profile)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if string(after) != "[Proxy]\nDIRECTISH = direct\n" {
		t.Fatalf("profile changed after denied confirmation:\n%s", after)
	}
	if stdout.Len() != 0 {
		t.Fatalf("expected no stdout, got:\n%s", stdout.String())
	}
	if !strings.Contains(stderr.String(), "Continue? [y/N]") {
		t.Fatalf("expected confirmation prompt, got:\n%s", stderr.String())
	}
}

func TestAddYesSkipsFirstWritePrompt(t *testing.T) {
	dir := t.TempDir()
	profile := filepath.Join(dir, "surge.conf")
	if err := os.WriteFile(profile, []byte("[Proxy]\nDIRECTISH = direct\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer
	err := runWithIO([]string{
		"add",
		"--yes",
		"--profile", profile,
		"--links-file", writeLinksFile(t, dir),
	}, &stdout, &stderr, strings.NewReader(""))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(stderr.String(), "Continue?") {
		t.Fatalf("did not expect confirmation prompt:\n%s", stderr.String())
	}
	if !strings.Contains(stdout.String(), "added 1 external proxy policies") {
		t.Fatalf("unexpected output:\n%s", stdout.String())
	}
}

func TestRemoveDeletesManagedPolicyAndBackup(t *testing.T) {
	dir := t.TempDir()
	profile := filepath.Join(dir, "surge.conf")
	initial := `[Proxy]
DIRECTISH = direct
# xcore-bridge managed external proxies begin
Demo = external, exec = "/opt/homebrew/bin/xcore-bridge", args = "run", local-port = 61080
# xcore-bridge managed external proxies end
Manual = ss, 203.0.113.1, 8388, encrypt-method=aes-128-gcm, password=p
`
	if err := os.WriteFile(profile, []byte(initial), 0o644); err != nil {
		t.Fatal(err)
	}

	var stdout bytes.Buffer
	err := runWithIO([]string{"remove", "--profile", profile, "Demo"}, &stdout, &bytes.Buffer{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stdout.String(), "removed 1 external proxy policies from "+profile) {
		t.Fatalf("unexpected remove output:\n%s", stdout.String())
	}
	if !strings.Contains(stdout.String(), "backup: "+profile+".bak") {
		t.Fatalf("remove output should mention backup:\n%s", stdout.String())
	}
	updatedBytes, err := os.ReadFile(profile)
	if err != nil {
		t.Fatal(err)
	}
	updated := string(updatedBytes)
	for _, gone := range []string{"Demo = external", "# xcore-bridge managed external proxies begin", "# xcore-bridge managed external proxies end"} {
		if strings.Contains(updated, gone) {
			t.Fatalf("expected %q to be removed:\n%s", gone, updated)
		}
	}
	if !strings.Contains(updated, "Manual = ss") {
		t.Fatalf("manual proxy was removed:\n%s", updated)
	}
	backup, err := os.ReadFile(profile + ".bak")
	if err != nil {
		t.Fatal(err)
	}
	if string(backup) != initial {
		t.Fatalf("backup should contain original profile:\n%s", backup)
	}
}

func writeLinksFile(t *testing.T, dir string) string {
	t.Helper()
	path := filepath.Join(dir, "links.txt")
	if err := os.WriteFile(path, []byte(testLink("Demo")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func testLink(name string) string {
	return "VLESS://00000000-0000-0000-0000-000000000000@example.com:443?encryption=none&flow=xtls-rprx-vision&security=reality&sni=example.com&fp=chrome&pbk=AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA&sid=0123&type=tcp#" + name
}

func assertInsideManagedBlock(t *testing.T, profile, needle string) {
	t.Helper()
	begin := strings.Index(profile, "# xcore-bridge managed external proxies begin")
	line := strings.Index(profile, needle)
	end := strings.Index(profile, "# xcore-bridge managed external proxies end")
	if begin == -1 || line == -1 || end == -1 || !(begin < line && line < end) {
		t.Fatalf("%q was not inside the managed block:\n%s", needle, profile)
	}
}
