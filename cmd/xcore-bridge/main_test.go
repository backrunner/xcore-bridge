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

var localPortLinePattern = regexp.MustCompile(`local-port = ([0-9]+)`)

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
