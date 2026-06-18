package main

import (
	"bytes"
	"os"
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
