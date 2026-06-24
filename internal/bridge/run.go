package bridge

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net"
	"strconv"
	"time"

	_ "github.com/xtls/xray-core/main/distro/all"

	"github.com/xtls/xray-core/core"
)

type Server struct {
	coreServer core.Server
}

func Start(ctx context.Context, cfg Config) (_ *Server, err error) {
	return StartMulti(ctx, MultiConfig{
		Policies: []PolicyConfig{
			{
				Node:      cfg.Node,
				LocalHost: cfg.LocalHost,
				LocalPort: cfg.LocalPort,
			},
		},
		LogLevel: cfg.LogLevel,
	})
}

func StartMulti(ctx context.Context, cfg MultiConfig) (_ *Server, err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			err = fmt.Errorf("xray panic during startup: %v", recovered)
		}
	}()
	data, err := MultiJSONConfig(cfg)
	if err != nil {
		return nil, err
	}
	c, err := core.LoadConfig("json", bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("load xray config: %w", err)
	}
	server, err := core.New(c)
	if err != nil {
		return nil, fmt.Errorf("create xray server: %w", err)
	}
	if err := server.Start(); err != nil {
		_ = server.Close()
		return nil, fmt.Errorf("start xray server: %w", err)
	}
	started := &Server{coreServer: server}
	for _, policy := range cfg.Policies {
		if err := waitForReady(ctx, policy.LocalHost, policy.LocalPort, 2*time.Second); err != nil {
			_ = started.Close()
			return nil, err
		}
	}
	return started, nil
}

func Run(ctx context.Context, cfg Config) error {
	server, err := Start(ctx, cfg)
	if err != nil {
		return err
	}
	defer server.Close()
	<-ctx.Done()
	return nil
}

func (s *Server) Close() error {
	if s == nil || s.coreServer == nil {
		return nil
	}
	return s.coreServer.Close()
}

func waitForReady(ctx context.Context, host string, port int, timeout time.Duration) error {
	if host == "" {
		host = "127.0.0.1"
	}
	address := net.JoinHostPort(host, strconv.Itoa(port))
	deadline := time.Now().Add(timeout)
	var lastErr error
	for {
		dialTimeout := 100 * time.Millisecond
		if remaining := time.Until(deadline); remaining < dialTimeout {
			dialTimeout = remaining
		}
		if dialTimeout <= 0 {
			if lastErr != nil {
				return fmt.Errorf("xray SOCKS inbound did not become ready at %s within %s: last error: %w", address, timeout, lastErr)
			}
			return fmt.Errorf("xray SOCKS inbound did not become ready at %s within %s", address, timeout)
		}
		conn, err := (&net.Dialer{Timeout: dialTimeout}).DialContext(ctx, "tcp", address)
		if err == nil {
			if err := checkSOCKS5Ready(conn, dialTimeout); err == nil {
				_ = conn.Close()
				return nil
			} else {
				lastErr = err
			}
			_ = conn.Close()
		} else {
			lastErr = err
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		timer := time.NewTimer(25 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
}

func WaitForReady(ctx context.Context, host string, port int, timeout time.Duration) error {
	return waitForReady(ctx, host, port, timeout)
}

func checkSOCKS5Ready(conn net.Conn, timeout time.Duration) error {
	if timeout <= 0 {
		timeout = 100 * time.Millisecond
	}
	if err := conn.SetDeadline(time.Now().Add(timeout)); err != nil {
		return err
	}
	if _, err := conn.Write([]byte{0x05, 0x01, 0x00}); err != nil {
		return err
	}
	reply := make([]byte, 2)
	if _, err := io.ReadFull(conn, reply); err != nil {
		return err
	}
	if reply[0] != 0x05 || reply[1] != 0x00 {
		return fmt.Errorf("unexpected SOCKS5 greeting response %x", reply)
	}
	if _, err := conn.Write([]byte{0x05, 0x03, 0x00, 0x01, 0, 0, 0, 0, 0, 0}); err != nil {
		return err
	}
	return readSOCKS5Reply(conn)
}

func readSOCKS5Reply(conn net.Conn) error {
	header := make([]byte, 4)
	if _, err := io.ReadFull(conn, header); err != nil {
		return err
	}
	if header[0] != 0x05 {
		return fmt.Errorf("unexpected SOCKS5 reply version %x", header[0])
	}
	if header[1] != 0x00 {
		return fmt.Errorf("SOCKS5 readiness request rejected with code %x", header[1])
	}
	var extra int
	switch header[3] {
	case 0x01:
		extra = net.IPv4len + 2
	case 0x03:
		length := make([]byte, 1)
		if _, err := io.ReadFull(conn, length); err != nil {
			return err
		}
		extra = int(length[0]) + 2
	case 0x04:
		extra = net.IPv6len + 2
	default:
		return fmt.Errorf("unexpected SOCKS5 reply address type %x", header[3])
	}
	if extra > 0 {
		discard := make([]byte, extra)
		if _, err := io.ReadFull(conn, discard); err != nil {
			return err
		}
	}
	return nil
}
