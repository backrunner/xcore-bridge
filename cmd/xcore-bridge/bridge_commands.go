package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/backrunner/xcore-bridge/internal/bridge"
	"github.com/backrunner/xcore-bridge/internal/vless"
)

func runCommand(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("run", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	localPort := fs.Int("local-port", 0, "SOCKS5 port Surge will connect to")
	localHost := fs.String("listen", "127.0.0.1", "SOCKS5 listen address")
	link := fs.String("link", "", "VLESS share link")
	logLevel := fs.String("log-level", "warning", "Xray log level")
	if err := fs.Parse(args); err != nil {
		return err
	}
	shareLink, err := oneLinkArg(*link, fs.Args(), "run")
	if err != nil {
		return err
	}
	if shareLink == "" {
		return errors.New("run requires --link or a positional VLESS share link")
	}
	if *localPort <= 0 || *localPort > 65535 {
		return errors.New("run requires --local-port in 1..65535")
	}

	node, err := vless.Parse(shareLink)
	if err != nil {
		return err
	}
	cfg := bridge.Config{
		Node:      node,
		LocalHost: *localHost,
		LocalPort: *localPort,
		LogLevel:  *logLevel,
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	server, err := bridge.Start(ctx, cfg)
	if err != nil {
		return err
	}
	defer server.Close()
	fmt.Fprintf(stdout, "xcore-bridge ready on %s:%d for %s\n", *localHost, *localPort, node.DisplayName())
	<-ctx.Done()
	return nil
}

func xrayConfigCommand(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("xray-config", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	localPort := fs.Int("local-port", 1080, "SOCKS5 listen port")
	localHost := fs.String("listen", "127.0.0.1", "SOCKS5 listen address")
	link := fs.String("link", "", "VLESS share link")
	logLevel := fs.String("log-level", "warning", "Xray log level")
	if err := fs.Parse(args); err != nil {
		return err
	}
	shareLink, err := oneLinkArg(*link, fs.Args(), "xray-config")
	if err != nil {
		return err
	}
	if shareLink == "" {
		return errors.New("xray-config requires --link or a positional VLESS share link")
	}
	node, err := vless.Parse(shareLink)
	if err != nil {
		return err
	}
	data, err := bridge.JSONConfig(bridge.Config{
		Node:      node,
		LocalHost: *localHost,
		LocalPort: *localPort,
		LogLevel:  *logLevel,
	})
	if err != nil {
		return err
	}
	_, err = stdout.Write(append(data, '\n'))
	return err
}

func oneLinkArg(flagValue string, positional []string, command string) (string, error) {
	flagValue = strings.TrimSpace(flagValue)
	if flagValue != "" {
		if len(positional) > 0 {
			return "", fmt.Errorf("%s accepts either --link or one positional link, not both", command)
		}
		return flagValue, nil
	}
	if len(positional) == 0 {
		return "", nil
	}
	if len(positional) > 1 {
		return "", fmt.Errorf("%s accepts exactly one positional VLESS share link", command)
	}
	return strings.TrimSpace(positional[0]), nil
}
