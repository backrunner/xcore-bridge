package vless

import (
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"net/url"
	"regexp"
	"strconv"
	"strings"
)

type Node struct {
	Raw      string
	ID       string
	Host     string
	Port     int
	Name     string
	Params   url.Values
	Fragment string
}

func Parse(raw string) (Node, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return Node{}, errors.New("empty VLESS share link")
	}
	u, err := url.Parse(raw)
	if err != nil {
		return Node{}, err
	}
	if strings.ToLower(u.Scheme) != "vless" {
		return Node{}, fmt.Errorf("unsupported share link scheme %q", u.Scheme)
	}
	if u.User == nil || u.User.Username() == "" {
		return Node{}, errors.New("VLESS share link is missing UUID userinfo")
	}
	id := u.User.Username()
	if !uuidPattern.MatchString(id) {
		return Node{}, fmt.Errorf("VLESS share link has invalid UUID %q", id)
	}
	host := u.Hostname()
	if host == "" {
		return Node{}, errors.New("VLESS share link is missing host")
	}
	port := 443
	if u.Port() != "" {
		parsed, err := strconv.Atoi(u.Port())
		if err != nil || parsed <= 0 || parsed > 65535 {
			return Node{}, fmt.Errorf("invalid VLESS port %q", u.Port())
		}
		port = parsed
	}
	name, err := url.QueryUnescape(u.Fragment)
	if err != nil {
		name = u.Fragment
	}
	return Node{
		Raw:      raw,
		ID:       id,
		Host:     host,
		Port:     port,
		Name:     strings.TrimSpace(name),
		Params:   u.Query(),
		Fragment: u.Fragment,
	}, nil
}

func (n Node) DisplayName() string {
	if n.Name != "" {
		return n.Name
	}
	return net.JoinHostPort(n.Host, strconv.Itoa(n.Port))
}

func (n Node) Param(keys ...string) string {
	for _, key := range keys {
		if value := n.Params.Get(key); value != "" {
			return value
		}
	}
	return ""
}

func (n Node) Security() string {
	return strings.ToLower(n.Param("security"))
}

func (n Node) Network() string {
	network := strings.ToLower(n.Param("type"))
	switch network {
	case "", "tcp", "raw":
		return "tcp"
	case "xhttp", "splithttp":
		return "splithttp"
	case "ws", "websocket":
		return "ws"
	case "httpupgrade", "http-upgrade":
		return "httpupgrade"
	default:
		return network
	}
}

func (n Node) Flow() string {
	return n.Param("flow")
}

func (n Node) Validate() error {
	if n.ID == "" || n.Host == "" || n.Port <= 0 || n.Port > 65535 {
		return errors.New("incomplete VLESS node")
	}
	if encryption := n.Param("encryption"); encryption != "" && strings.ToLower(encryption) != "none" {
		return fmt.Errorf("unsupported VLESS encryption %q", encryption)
	}
	switch n.Security() {
	case "", "none", "tls", "reality":
	default:
		return fmt.Errorf("unsupported VLESS security %q", n.Security())
	}
	switch n.Network() {
	case "tcp", "ws", "grpc", "httpupgrade", "splithttp":
	default:
		return fmt.Errorf("unsupported VLESS transport type %q", n.Param("type"))
	}
	if n.Security() == "reality" {
		return n.validateReality()
	}
	return nil
}

func (n Node) validateReality() error {
	if n.Param("sni", "serverName", "servername") == "" {
		return errors.New("REALITY VLESS link requires sni/serverName")
	}
	if n.Param("fp", "fingerprint") == "" {
		return errors.New("REALITY VLESS link requires fp/fingerprint")
	}
	publicKey := n.Param("pbk", "publicKey")
	if publicKey == "" {
		return errors.New("REALITY VLESS link requires pbk/publicKey")
	}
	key, err := base64.RawURLEncoding.DecodeString(publicKey)
	if err != nil || len(key) != 32 {
		return fmt.Errorf("REALITY public key must be base64url without padding for 32 bytes")
	}
	shortID := n.Param("sid", "shortId")
	if len(shortID) > 16 {
		return fmt.Errorf("REALITY shortId must be at most 16 hex characters")
	}
	if _, err := hex.DecodeString(shortID); err != nil {
		return fmt.Errorf("REALITY shortId must be hex")
	}
	if spiderX := n.Param("spx", "spiderX"); spiderX != "" && !strings.HasPrefix(spiderX, "/") {
		return fmt.Errorf("REALITY spiderX must start with /")
	}
	return nil
}

var uuidPattern = regexp.MustCompile(`(?i)^(?:[0-9a-f]{32}|[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12})$`)
