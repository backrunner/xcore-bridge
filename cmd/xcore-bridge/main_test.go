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

func TestAddNameOverridesLinkName(t *testing.T) {
	dir := t.TempDir()
	profile := filepath.Join(dir, "surge.conf")
	if err := os.WriteFile(profile, []byte("[Proxy]\nDIRECTISH = direct\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	var stdout bytes.Buffer
	err := run([]string{"add", "--dry-run", "--profile", profile, "--name", "Custom Node", testLink("Link Node")}, &stdout, &bytes.Buffer{})
	if err != nil {
		t.Fatal(err)
	}
	output := stdout.String()
	if !strings.Contains(output, "Custom Node = external") {
		t.Fatalf("expected override name in output:\n%s", output)
	}
	if strings.Contains(output, "Link Node = external") {
		t.Fatalf("link name was used instead of override:\n%s", output)
	}
}

func TestAddNameCountMustMatchLinks(t *testing.T) {
	dir := t.TempDir()
	profile := filepath.Join(dir, "surge.conf")
	if err := os.WriteFile(profile, []byte("[Proxy]\nDIRECTISH = direct\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	err := run([]string{
		"add",
		"--dry-run",
		"--profile", profile,
		"--name", "Only One",
		testLink("First"),
		testLink("Second"),
	}, &bytes.Buffer{}, &bytes.Buffer{})
	if err == nil {
		t.Fatal("expected mismatched --name count to fail")
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
	if !strings.Contains(stdout.String(), "added 1 external proxy policies") || !strings.Contains(stdout.String(), "profile: "+profile) {
		t.Fatalf("unexpected add output:\n%s", stdout.String())
	}
	if !strings.Contains(stdout.String(), "backup: "+profile+".bak") {
		t.Fatalf("add output should mention backup:\n%s", stdout.String())
	}
	if !strings.Contains(stderr.String(), "profile: "+profile) {
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

func TestRemoveNameFlagDeletesManagedPolicy(t *testing.T) {
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
	err := runWithIO([]string{"remove", "--profile", profile, "--name", "Demo"}, &stdout, &bytes.Buffer{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stdout.String(), "removed 1 external proxy policies") || !strings.Contains(stdout.String(), "profile: "+profile) {
		t.Fatalf("unexpected remove output:\n%s", stdout.String())
	}
	updated, err := os.ReadFile(profile)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(updated), "Demo = external") {
		t.Fatalf("expected Demo to be removed:\n%s", updated)
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
	if !strings.Contains(stdout.String(), "removed 1 external proxy policies") || !strings.Contains(stdout.String(), "profile: "+profile) {
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

func TestRenameManagedPolicyAndBackup(t *testing.T) {
	dir := t.TempDir()
	profile := filepath.Join(dir, "surge.conf")
	initial := `[Proxy]
DIRECTISH = direct
# xcore-bridge managed external proxies begin
Old = external, exec = "/opt/homebrew/bin/xcore-bridge", args = "run", local-port = 61080
# xcore-bridge managed external proxies end
Manual = ss, 203.0.113.1, 8388, encrypt-method=aes-128-gcm, password=p
`
	if err := os.WriteFile(profile, []byte(initial), 0o644); err != nil {
		t.Fatal(err)
	}

	var stdout bytes.Buffer
	err := runWithIO([]string{"rename", "--profile", profile, "Old", "New"}, &stdout, &bytes.Buffer{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stdout.String(), "Old -> New") {
		t.Fatalf("unexpected rename output:\n%s", stdout.String())
	}
	if !strings.Contains(stdout.String(), "backup: "+profile+".bak") {
		t.Fatalf("rename output should mention backup:\n%s", stdout.String())
	}
	updated, err := os.ReadFile(profile)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(updated), "Old = external") || !strings.Contains(string(updated), "New = external") {
		t.Fatalf("profile was not renamed:\n%s", updated)
	}
	backup, err := os.ReadFile(profile + ".bak")
	if err != nil {
		t.Fatal(err)
	}
	if string(backup) != initial {
		t.Fatalf("backup should contain original profile:\n%s", backup)
	}
}

func TestReplaceManagedPolicyAndBackup(t *testing.T) {
	dir := t.TempDir()
	profile := filepath.Join(dir, "surge.conf")
	oldLink := testLink("Old")
	newLink := testLink("Replacement")
	initial := `[Proxy]
DIRECTISH = direct
# xcore-bridge managed external proxies begin
Demo = external, exec = "/opt/homebrew/bin/xcore-bridge", args = "run", args = "--profile", args = "` + profile + `", args = "--local-port", args = "61080", args = "--link", args = "` + oldLink + `", local-port = 61080, udp-relay = true
# xcore-bridge managed external proxies end
Manual = ss, 203.0.113.1, 8388, encrypt-method=aes-128-gcm, password=p
`
	if err := os.WriteFile(profile, []byte(initial), 0o644); err != nil {
		t.Fatal(err)
	}

	var stdout bytes.Buffer
	err := runWithIO([]string{"replace", "--profile", profile, "Demo", newLink}, &stdout, &bytes.Buffer{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stdout.String(), "replaced external proxy policy") || !strings.Contains(stdout.String(), "Demo local-port=61080") {
		t.Fatalf("unexpected replace output:\n%s", stdout.String())
	}
	if !strings.Contains(stdout.String(), "backup: "+profile+".bak") {
		t.Fatalf("replace output should mention backup:\n%s", stdout.String())
	}
	updated, err := os.ReadFile(profile)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(updated), newLink) || strings.Contains(string(updated), oldLink) {
		t.Fatalf("profile was not replaced:\n%s", updated)
	}
	for _, want := range []string{"Demo = external", "local-port = 61080", "Manual = ss"} {
		if !strings.Contains(string(updated), want) {
			t.Fatalf("updated profile missing %q:\n%s", want, updated)
		}
	}
	backup, err := os.ReadFile(profile + ".bak")
	if err != nil {
		t.Fatal(err)
	}
	if string(backup) != initial {
		t.Fatalf("backup should contain original profile:\n%s", backup)
	}
}

func TestVerifyManagedRunTargetRejectsUnmanagedLink(t *testing.T) {
	dir := t.TempDir()
	profile := filepath.Join(dir, "surge.conf")
	managedLink := testLink("Managed")
	initial := `[Proxy]
# xcore-bridge managed external proxies begin
Managed = external, exec = "/opt/homebrew/bin/xcore-bridge", args = "run", args = "--profile", args = "` + profile + `", args = "--local-port", args = "61080", args = "--link", args = "` + managedLink + `", local-port = 61080, udp-relay = true
# xcore-bridge managed external proxies end
`
	if err := os.WriteFile(profile, []byte(initial), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := verifyManagedRunTarget(profile, managedLink, "127.0.0.1", 61080); err != nil {
		t.Fatal(err)
	}
	if err := verifyManagedRunTarget(profile, testLink("Other"), "127.0.0.1", 61080); err == nil {
		t.Fatal("expected unmanaged link to be rejected")
	}
	if err := verifyManagedRunTarget(profile, managedLink, "127.0.0.1", 61081); err == nil {
		t.Fatal("expected unmanaged port to be rejected")
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
