package main

import (
	"fmt"
	"io"
	"os"
	"strings"
)

const (
	uiReset = "\x1b[0m"
	uiBold  = "\x1b[1m"
	uiDim   = "\x1b[2m"
	uiRed   = "\x1b[31m"
	uiGreen = "\x1b[32m"
	uiCyan  = "\x1b[36m"
	uiGray  = "\x1b[90m"
)

type uiWriter struct {
	w     io.Writer
	color bool
}

func newUI(w io.Writer) uiWriter {
	return uiWriter{w: w, color: colorEnabled(w)}
}

func colorEnabled(w io.Writer) bool {
	if os.Getenv("NO_COLOR") != "" || os.Getenv("XCORE_BRIDGE_NO_COLOR") != "" {
		return false
	}
	if os.Getenv("XCORE_BRIDGE_COLOR") == "always" {
		return true
	}
	file, ok := w.(*os.File)
	if !ok {
		return false
	}
	info, err := file.Stat()
	return err == nil && info.Mode()&os.ModeCharDevice != 0
}

func (ui uiWriter) c(color, value string) string {
	if !ui.color || value == "" {
		return value
	}
	return color + value + uiReset
}

func (ui uiWriter) Title(title string) {
	fmt.Fprintf(ui.w, "%s\n", ui.c(uiBold, title))
}

func (ui uiWriter) Success(format string, args ...any) {
	fmt.Fprintf(ui.w, "%s %s\n", ui.c(uiGreen, "✓"), fmt.Sprintf(format, args...))
}

func (ui uiWriter) Info(format string, args ...any) {
	fmt.Fprintf(ui.w, "%s %s\n", ui.c(uiCyan, "•"), fmt.Sprintf(format, args...))
}

func (ui uiWriter) Warn(format string, args ...any) {
	fmt.Fprintf(ui.w, "%s %s\n", ui.c(uiRed, "!"), fmt.Sprintf(format, args...))
}

func (ui uiWriter) KeyValue(key, value string) {
	fmt.Fprintf(ui.w, "  %s %s\n", ui.c(uiDim, keyWithColon(key)), value)
}

func (ui uiWriter) Item(name string, parts ...string) {
	fmt.Fprintf(ui.w, "  %s", ui.c(uiCyan, name))
	if len(parts) > 0 {
		fmt.Fprintf(ui.w, " %s", ui.c(uiGray, strings.Join(parts, " ")))
	}
	fmt.Fprintln(ui.w)
}

func keyWithColon(key string) string {
	if strings.HasSuffix(key, ":") {
		return key
	}
	return key + ":"
}
