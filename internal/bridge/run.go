package bridge

import (
	"bytes"
	"context"
	"fmt"
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
	defer func() {
		if recovered := recover(); recovered != nil {
			err = fmt.Errorf("xray panic during startup: %v", recovered)
		}
	}()
	data, err := JSONConfig(cfg)
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
	if err := waitForReady(ctx, cfg.LocalHost, cfg.LocalPort, 2*time.Second); err != nil {
		_ = started.Close()
		return nil, err
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
	for {
		dialTimeout := 100 * time.Millisecond
		if remaining := time.Until(deadline); remaining < dialTimeout {
			dialTimeout = remaining
		}
		if dialTimeout <= 0 {
			return fmt.Errorf("xray SOCKS inbound did not become ready at %s within %s", address, timeout)
		}
		conn, err := (&net.Dialer{Timeout: dialTimeout}).DialContext(ctx, "tcp", address)
		if err == nil {
			_ = conn.Close()
			return nil
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
