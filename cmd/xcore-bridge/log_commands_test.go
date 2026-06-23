package main

import (
	"bytes"
	"os"
	"strings"
	"testing"

	"github.com/backrunner/xcore-bridge/internal/daemon"
)

func TestLogCommandsShowBridgeAndDaemonLogs(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	if err := daemon.AppendBridgeLog("bridge test line"); err != nil {
		t.Fatal(err)
	}
	if err := daemon.AppendDaemonLog("daemon test line"); err != nil {
		t.Fatal(err)
	}

	var bridgeOut bytes.Buffer
	if err := runWithIO([]string{"log"}, &bridgeOut, &bytes.Buffer{}, nil); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(bridgeOut.String(), "bridge test line") {
		t.Fatalf("bridge log output missing entry:\n%s", bridgeOut.String())
	}

	var daemonOut bytes.Buffer
	if err := runWithIO([]string{"daemon", "log"}, &daemonOut, &bytes.Buffer{}, nil); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(daemonOut.String(), "daemon test line") {
		t.Fatalf("daemon log output missing entry:\n%s", daemonOut.String())
	}
}

func TestLogCommandPath(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	var stdout bytes.Buffer
	if err := runWithIO([]string{"log", "--path"}, &stdout, &bytes.Buffer{}, nil); err != nil {
		t.Fatal(err)
	}
	path := strings.TrimSpace(stdout.String())
	if !strings.HasSuffix(path, "bridge.log") {
		t.Fatalf("expected bridge log path, got %q", path)
	}
}

func TestRuntimeLogRedactsVLESSLinks(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	raw := "vless://00000000-0000-0000-0000-000000000000@example.com:443?encryption=none#Secret"
	if err := daemon.AppendBridgeLog("failed link=%s", raw); err != nil {
		t.Fatal(err)
	}
	path, err := daemon.BridgeLogPath()
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), raw) {
		t.Fatalf("log leaked VLESS link:\n%s", data)
	}
	if !strings.Contains(string(data), "vless://<redacted>") {
		t.Fatalf("log did not contain redaction marker:\n%s", data)
	}
}
