package bridge

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net"
	"strconv"
	"sync"
	"time"

	_ "github.com/xtls/xray-core/main/distro/all"

	"github.com/xtls/xray-core/core"
)

type Server struct {
	coreServer core.Server
	closeOnce  sync.Once
	closeErr   error
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
		LogLevel:      cfg.LogLevel,
		AccessLogPath: cfg.AccessLogPath,
		ErrorLogPath:  cfg.ErrorLogPath,
	})
}

func StartMulti(ctx context.Context, cfg MultiConfig) (_ *Server, err error) {
	var created *Server
	defer func() {
		if recovered := recover(); recovered != nil {
			err = fmt.Errorf("xray panic during startup: %v", recovered)
		}
		if err != nil && created != nil {
			_ = created.Close()
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
	server, err := core.NewWithContext(ctx, c)
	if err != nil {
		return nil, fmt.Errorf("create xray server: %w", err)
	}
	created = &Server{coreServer: server}
	if err := server.Start(); err != nil {
		return nil, fmt.Errorf("start xray server: %w", err)
	}
	for _, policy := range cfg.Policies {
		if err := waitForReady(ctx, policy.LocalHost, policy.LocalPort, 2*time.Second); err != nil {
			return nil, err
		}
	}
	return created, nil
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
	s.closeOnce.Do(func() {
		s.closeErr = s.coreServer.Close()
	})
	return s.closeErr
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
	return nil
}
