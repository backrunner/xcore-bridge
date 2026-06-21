package main

import (
	"bytes"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

func TestSurgeLineUsesExecutablePathByDefault(t *testing.T) {
	var stdout bytes.Buffer
	err := run([]string{
		"surge-line",
		"--link", "vless://00000000-0000-0000-0000-000000000000@203.0.113.10:443?encryption=none&flow=xtls-rprx-vision&security=reality&sni=example.com&fp=chrome&pbk=AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA&sid=0123&type=tcp#Demo",
	}, &stdout, &bytes.Buffer{})
	if err != nil {
		t.Fatal(err)
	}
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stdout.String(), `exec = "`+exe+`"`) {
		t.Fatalf("expected default exec path %q in output:\n%s", exe, stdout.String())
	}
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

func TestSurgeLineSkipsOccupiedStablePort(t *testing.T) {
	link := "vless://00000000-0000-0000-0000-000000000000@203.0.113.10:443?encryption=none&flow=xtls-rprx-vision&security=reality&sni=example.com&fp=chrome&pbk=AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA&sid=0123&type=tcp#Demo"
	var probe bytes.Buffer
	err := run([]string{"surge-line", "--link", link}, &probe, &bytes.Buffer{})
	if err != nil {
		t.Fatal(err)
	}
	stablePort := extractLocalPort(t, probe.String())
	listener, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", stablePort))
	if err != nil {
		t.Skipf("stable port %d is not available for test: %v", stablePort, err)
	}
	defer listener.Close()

	var stdout bytes.Buffer
	err = run([]string{"surge-line", "--link", link}, &stdout, &bytes.Buffer{})
	if err != nil {
		t.Fatal(err)
	}
	if got := extractLocalPort(t, stdout.String()); got == stablePort {
		t.Fatalf("expected occupied stable port %d to be skipped, output:\n%s", stablePort, stdout.String())
	}
}

func TestSurgeInstallLinksFileAcceptsUppercaseScheme(t *testing.T) {
	dir := t.TempDir()
	profile := filepath.Join(dir, "surge.conf")
	if err := os.WriteFile(profile, []byte("[Proxy]\nDIRECTISH = direct\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	links := filepath.Join(dir, "links.txt")
	if err := os.WriteFile(links, []byte("VLESS://00000000-0000-0000-0000-000000000000@example.com:443?encryption=none&flow=xtls-rprx-vision&security=reality&sni=example.com&fp=chrome&pbk=AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA&sid=0123&type=tcp#Demo\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	var stdout bytes.Buffer
	err := run([]string{"surge-install", "--dry-run", "--profile", profile, "--links-file", links}, &stdout, &bytes.Buffer{})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stdout.String(), "Demo = external") {
		t.Fatalf("expected generated proxy line, got:\n%s", stdout.String())
	}
}

func TestSurgeInstallAutoDiscoversICloudProfileAndConfirmsFirstWrite(t *testing.T) {
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
		"surge-install",
		"--links-file", writeLinksFile(t, home),
	}, &stdout, &stderr, strings.NewReader("y\n"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stdout.String(), "installed 1 external proxy policies into "+profile) {
		t.Fatalf("unexpected install output:\n%s", stdout.String())
	}
	if !strings.Contains(stdout.String(), "backup: "+profile+".bak") {
		t.Fatalf("install output should mention backup:\n%s", stdout.String())
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
}

func TestSurgeInstallRequiresConfirmationForFirstWrite(t *testing.T) {
	dir := t.TempDir()
	profile := filepath.Join(dir, "surge.conf")
	if err := os.WriteFile(profile, []byte("[Proxy]\nDIRECTISH = direct\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer
	err := runWithIO([]string{
		"surge-install",
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

func TestSurgeInstallYesSkipsFirstWritePrompt(t *testing.T) {
	dir := t.TempDir()
	profile := filepath.Join(dir, "surge.conf")
	if err := os.WriteFile(profile, []byte("[Proxy]\nDIRECTISH = direct\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer
	err := runWithIO([]string{
		"surge-install",
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
	if !strings.Contains(stdout.String(), "installed 1 external proxy policies") {
		t.Fatalf("unexpected output:\n%s", stdout.String())
	}
}

func TestSurgeInstallRejectsBackupFalse(t *testing.T) {
	dir := t.TempDir()
	profile := filepath.Join(dir, "surge.conf")
	if err := os.WriteFile(profile, []byte("[Proxy]\nDIRECTISH = direct\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	err := runWithIO([]string{
		"surge-install",
		"--backup=false",
		"--profile", profile,
		"--links-file", writeLinksFile(t, dir),
	}, &bytes.Buffer{}, &bytes.Buffer{}, strings.NewReader("y\n"))
	if err == nil {
		t.Fatal("expected --backup=false to be rejected")
	}
}

var localPortLinePattern = regexp.MustCompile(`local-port = ([0-9]+)`)

func writeLinksFile(t *testing.T, dir string) string {
	t.Helper()
	path := filepath.Join(dir, "links.txt")
	data := "VLESS://00000000-0000-0000-0000-000000000000@example.com:443?encryption=none&flow=xtls-rprx-vision&security=reality&sni=example.com&fp=chrome&pbk=AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA&sid=0123&type=tcp#Demo\n"
	if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func extractLocalPort(t *testing.T, line string) int {
	t.Helper()
	matches := localPortLinePattern.FindStringSubmatch(line)
	if len(matches) != 2 {
		t.Fatalf("missing local-port in line:\n%s", line)
	}
	port, err := strconv.Atoi(matches[1])
	if err != nil {
		t.Fatal(err)
	}
	return port
}
