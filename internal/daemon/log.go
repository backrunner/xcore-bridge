package daemon

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

var sensitiveLinkPattern = regexp.MustCompile(`(?i)vless://[^\s"']+`)

func BridgeLogPath() (string, error) {
	paths, err := Paths()
	if err != nil {
		return "", err
	}
	return paths.BridgeLog, nil
}

func DaemonLogPath() (string, error) {
	paths, err := Paths()
	if err != nil {
		return "", err
	}
	return paths.Log, nil
}

func AppendBridgeLog(format string, args ...any) error {
	path, err := BridgeLogPath()
	if err != nil {
		return err
	}
	return appendRuntimeLog(path, "bridge", format, args...)
}

func AppendDaemonLog(format string, args ...any) error {
	path, err := DaemonLogPath()
	if err != nil {
		return err
	}
	return appendRuntimeLog(path, "daemon", format, args...)
}

func appendRuntimeLog(path, component, format string, args ...any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return err
	}
	defer file.Close()

	message := redactLogMessage(fmt.Sprintf(format, args...))
	line := fmt.Sprintf("%s pid=%d component=%s %s\n", time.Now().Format(time.RFC3339), os.Getpid(), component, message)
	_, err = file.WriteString(line)
	return err
}

func redactLogMessage(message string) string {
	message = sensitiveLinkPattern.ReplaceAllString(message, "vless://<redacted>")
	message = strings.ReplaceAll(message, "\r", "\\r")
	message = strings.ReplaceAll(message, "\n", "\\n")
	return message
}
