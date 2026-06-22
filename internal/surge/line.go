package surge

import (
	"crypto/sha1"
	"encoding/binary"
	"fmt"
	"net"
	"regexp"
	"strconv"
	"strings"

	"github.com/backrunner/xcore-bridge/internal/vless"
)

type ProxyLineOptions struct {
	Node             vless.Node
	Name             string
	ExecPath         string
	LocalPort        int
	IncludeAddresses bool
}

func StablePort(node vless.Node) int {
	sum := sha1.Sum([]byte(node.Raw))
	return 61080 + int(binary.BigEndian.Uint16(sum[:2])%3000)
}

func ProxyLine(opts ProxyLineOptions) (string, error) {
	if err := opts.Node.Validate(); err != nil {
		return "", err
	}
	if opts.LocalPort <= 0 || opts.LocalPort > 65535 {
		return "", fmt.Errorf("invalid local port %d", opts.LocalPort)
	}
	if opts.ExecPath == "" {
		opts.ExecPath = "xcore-bridge"
	}
	name := sanitizeName(opts.Name)
	if name == "" {
		name = sanitizeName(opts.Node.DisplayName())
	}
	args := []string{
		"run",
		"--local-port", strconv.Itoa(opts.LocalPort),
		"--link", opts.Node.Raw,
	}
	fields := []string{
		name + " = external",
		"exec = " + quote(opts.ExecPath),
	}
	for _, arg := range args {
		fields = append(fields, "args = "+quote(arg))
	}
	fields = append(fields, fmt.Sprintf("local-port = %d", opts.LocalPort))
	if opts.IncludeAddresses {
		if ip := net.ParseIP(opts.Node.Host); ip != nil {
			fields = append(fields, "addresses = "+ip.String())
		}
	}
	return strings.Join(fields, ", "), nil
}

var unsafeNameChars = regexp.MustCompile(`[^A-Za-z0-9_. -]+`)

func sanitizeName(name string) string {
	name = strings.TrimSpace(name)
	name = unsafeNameChars.ReplaceAllString(name, "-")
	name = strings.Join(strings.Fields(name), " ")
	if name == "" {
		return ""
	}
	return name
}

func quote(value string) string {
	return strconv.Quote(value)
}
