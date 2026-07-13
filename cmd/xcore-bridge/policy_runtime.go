package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"syscall"
	"time"

	"github.com/backrunner/xcore-bridge/internal/bridge"
	"github.com/backrunner/xcore-bridge/internal/daemon"
	"github.com/backrunner/xcore-bridge/internal/vless"
)

type managedPolicyRuntime struct {
	file  *os.File
	path  string
	owned bool
}

type managedPolicyRuntimeState struct {
	PID      int    `json:"pid"`
	LinkHash string `json:"linkHash"`
}

func runForegroundManagedPolicy(ctx context.Context, node vless.Node, profilePath, localHost string, localPort int, logLevel, logPath string, stdout io.Writer) error {
	runtimeLock, err := openManagedPolicyRuntime(profilePath, localHost, localPort)
	if err != nil {
		return err
	}
	defer runtimeLock.close()

	state := managedPolicyRuntimeState{
		PID:      os.Getpid(),
		LinkHash: daemon.PolicyLinkHash(node.Raw),
	}
	standbyReady := false
	for {
		owned, err := runtimeLock.tryAcquire(state)
		if err != nil {
			return err
		}
		if owned {
			if standbyReady {
				_ = appendRuntimeLog("run standby taking ownership policy=%q profile=%q socks=%s:%d pid=%d", node.DisplayName(), profilePath, localHost, localPort, os.Getpid())
			}
			return runOwnedManagedPolicy(ctx, node, profilePath, localHost, localPort, logLevel, logPath, stdout)
		}

		owner, matches := runtimeLock.matchingOwner(state.LinkHash)
		if matches && !standbyReady {
			if err := bridge.WaitForReady(ctx, localHost, localPort, 100*time.Millisecond); err == nil {
				standbyReady = true
				_ = appendRuntimeLog("run standby ready policy=%q profile=%q owner_pid=%d socks=%s:%d pid=%d", node.DisplayName(), profilePath, owner.PID, localHost, localPort, os.Getpid())
				ui := newUI(stdout)
				ui.Success("xcore-bridge ready")
				ui.KeyValue("policy", node.DisplayName())
				ui.KeyValue("socks5", fmt.Sprintf("%s:%d", localHost, localPort))
				ui.KeyValue("pid", fmt.Sprintf("%d", os.Getpid()))
				ui.KeyValue("mode", "standby")
				ui.KeyValue("owner-pid", fmt.Sprintf("%d", owner.PID))
			}
		}

		timer := time.NewTimer(25 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			_ = appendRuntimeLog("run standby stopping policy=%q profile=%q reason=%q", node.DisplayName(), profilePath, ctx.Err())
			return nil
		case <-timer.C:
		}
	}
}

func runOwnedManagedPolicy(ctx context.Context, node vless.Node, profilePath, localHost string, localPort int, logLevel, logPath string, stdout io.Writer) error {
	server, err := startBridgeServer(ctx, bridge.Config{
		Node:          node,
		LocalHost:     localHost,
		LocalPort:     localPort,
		LogLevel:      logLevel,
		AccessLogPath: logPath,
		ErrorLogPath:  logPath,
	})
	if err != nil {
		_ = appendRuntimeLog("run xray start failed policy=%q profile=%q socks=%s:%d error=%q", node.DisplayName(), profilePath, localHost, localPort, err)
		return err
	}
	defer func() {
		if err := server.Close(); err != nil {
			_ = appendRuntimeLog("run xray close failed policy=%q profile=%q error=%q", node.DisplayName(), profilePath, err)
		}
	}()
	_ = appendRuntimeLog("run ready policy=%q profile=%q socks=%s:%d pid=%d", node.DisplayName(), profilePath, localHost, localPort, os.Getpid())
	ui := newUI(stdout)
	ui.Success("xcore-bridge ready")
	ui.KeyValue("policy", node.DisplayName())
	ui.KeyValue("socks5", fmt.Sprintf("%s:%d", localHost, localPort))
	ui.KeyValue("pid", fmt.Sprintf("%d", os.Getpid()))
	<-ctx.Done()
	_ = appendRuntimeLog("run stopping policy=%q profile=%q reason=%q", node.DisplayName(), profilePath, ctx.Err())
	return nil
}

func openManagedPolicyRuntime(profilePath, localHost string, localPort int) (*managedPolicyRuntime, error) {
	paths, err := daemon.Paths()
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(paths.Dir, 0o755); err != nil {
		return nil, err
	}
	if localHost == "" {
		localHost = "127.0.0.1"
	}
	key := filepath.Clean(profilePath) + "\x00" + net.JoinHostPort(localHost, strconv.Itoa(localPort))
	sum := sha256.Sum256([]byte(key))
	path := filepath.Join(paths.Dir, "policy-"+hex.EncodeToString(sum[:])+".lock")
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	return &managedPolicyRuntime{file: file, path: path}, nil
}

func (r *managedPolicyRuntime) tryAcquire(state managedPolicyRuntimeState) (bool, error) {
	if r == nil || r.file == nil {
		return false, fmt.Errorf("managed policy runtime is not open")
	}
	if r.owned {
		return true, nil
	}
	if err := syscall.Flock(int(r.file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		if errors.Is(err, syscall.EWOULDBLOCK) || errors.Is(err, syscall.EAGAIN) {
			return false, nil
		}
		return false, err
	}
	r.owned = true
	data, err := json.Marshal(state)
	if err == nil {
		data = append(data, '\n')
		err = r.file.Truncate(0)
	}
	if err == nil {
		_, err = r.file.WriteAt(data, 0)
	}
	if err != nil {
		_ = syscall.Flock(int(r.file.Fd()), syscall.LOCK_UN)
		r.owned = false
		return false, err
	}
	return true, nil
}

func (r *managedPolicyRuntime) matchingOwner(linkHash string) (managedPolicyRuntimeState, bool) {
	if r == nil || r.path == "" || linkHash == "" {
		return managedPolicyRuntimeState{}, false
	}
	data, err := os.ReadFile(r.path)
	if err != nil {
		return managedPolicyRuntimeState{}, false
	}
	var state managedPolicyRuntimeState
	if err := json.Unmarshal(data, &state); err != nil {
		return managedPolicyRuntimeState{}, false
	}
	return state, state.PID > 0 && state.LinkHash == linkHash
}

func (r *managedPolicyRuntime) close() error {
	if r == nil || r.file == nil {
		return nil
	}
	if r.owned {
		_ = syscall.Flock(int(r.file.Fd()), syscall.LOCK_UN)
		r.owned = false
	}
	err := r.file.Close()
	r.file = nil
	return err
}
