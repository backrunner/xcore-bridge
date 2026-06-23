package daemon

import (
	"os"
	"strings"
	"testing"
)

func TestAppendRuntimeLogRedactsSensitiveValuesAndNewlines(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	raw := "vless://00000000-0000-0000-0000-000000000000@example.com:443?encryption=none#Secret"
	if err := AppendBridgeLog("message=%s\nnext", raw); err != nil {
		t.Fatal(err)
	}
	path, err := BridgeLogPath()
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	if strings.Contains(text, raw) {
		t.Fatalf("log leaked VLESS link:\n%s", text)
	}
	if !strings.Contains(text, "vless://<redacted>") {
		t.Fatalf("log missing redaction marker:\n%s", text)
	}
	if strings.Contains(strings.TrimSuffix(text, "\n"), "\n") {
		t.Fatalf("log entry should stay on one line:\n%s", text)
	}
}
