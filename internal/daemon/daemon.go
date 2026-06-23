package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/backrunner/xcore-bridge/internal/bridge"
	"github.com/backrunner/xcore-bridge/internal/surge"
	"github.com/backrunner/xcore-bridge/internal/vless"
)

const defaultLogLevel = "warning"

type RuntimePaths struct {
	Dir   string
	PID   string
	State string
	Log   string
}

type Options struct {
	ProfilePath string
	ExecPath    string
	LogLevel    string
	Timeout     time.Duration
}

type Status struct {
	Running     bool
	PID         int
	ProfilePath string
	Policies    []Policy
	StalePID    bool
	Error       string
}

type Policy struct {
	Name      string
	LocalHost string
	LocalPort int
}

type stateFile struct {
	PID         int       `json:"pid"`
	ProfilePath string    `json:"profilePath"`
	StartedAt   time.Time `json:"startedAt"`
	Policies    []Policy  `json:"policies"`
}

func Start(ctx context.Context, opts Options) (Status, error) {
	if err := validateProfile(opts.ProfilePath); err != nil {
		return Status{}, err
	}
	status, _ := GetStatus(opts)
	if status.Running {
		if policyReady(ctx, status, opts) {
			return status, nil
		}
		return status, fmt.Errorf("daemon already running for %s; use daemon restart to switch profiles", status.ProfilePath)
	}
	return startFresh(ctx, opts)
}

func Serve(ctx context.Context, opts Options) error {
	if err := validateProfile(opts.ProfilePath); err != nil {
		return err
	}
	paths, err := Paths()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(paths.Dir, 0o755); err != nil {
		return err
	}
	cfg, policies, err := ConfigFromProfile(opts.ProfilePath, opts.LogLevel)
	if err != nil {
		return err
	}
	server, err := bridge.StartMulti(ctx, cfg)
	if err != nil {
		return err
	}
	defer server.Close()
	state := stateFile{
		PID:         os.Getpid(),
		ProfilePath: opts.ProfilePath,
		StartedAt:   time.Now(),
		Policies:    policies,
	}
	if err := writeState(paths, state); err != nil {
		return err
	}
	defer cleanupState(paths, os.Getpid())
	<-ctx.Done()
	return nil
}

func Stop(opts Options) (Status, error) {
	status, err := GetStatus(opts)
	if err != nil {
		return status, err
	}
	if !status.Running {
		if status.StalePID {
			_ = removeRuntimeFiles()
		}
		return status, nil
	}
	process, err := os.FindProcess(status.PID)
	if err != nil {
		return status, err
	}
	if err := process.Signal(syscall.SIGTERM); err != nil && !errors.Is(err, os.ErrProcessDone) {
		return status, err
	}
	timeout := opts.Timeout
	if timeout == 0 {
		timeout = 3 * time.Second
	}
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		current, _ := GetStatus(opts)
		if !current.Running {
			_ = removeRuntimeFiles()
			return current, nil
		}
		time.Sleep(50 * time.Millisecond)
	}
	return status, fmt.Errorf("daemon pid %d did not stop within %s", status.PID, timeout)
}

func Restart(ctx context.Context, opts Options) (Status, error) {
	if _, err := Stop(opts); err != nil {
		return Status{}, err
	}
	return Start(ctx, opts)
}

func Ensure(ctx context.Context, opts Options) (Status, error) {
	status, _ := GetStatus(opts)
	if status.Running && policyReady(ctx, status, opts) {
		return status, nil
	}
	if status.Running {
		if _, err := Stop(opts); err != nil {
			return status, err
		}
	}
	return startFresh(ctx, opts)
}

func startFresh(ctx context.Context, opts Options) (Status, error) {
	if err := validateProfile(opts.ProfilePath); err != nil {
		return Status{}, err
	}
	paths, err := Paths()
	if err != nil {
		return Status{}, err
	}
	if err := os.MkdirAll(paths.Dir, 0o755); err != nil {
		return Status{}, err
	}
	execPath := strings.TrimSpace(opts.ExecPath)
	if execPath == "" {
		execPath, err = os.Executable()
		if err != nil {
			return Status{}, err
		}
	}
	logFile, err := os.OpenFile(paths.Log, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return Status{}, err
	}
	defer logFile.Close()

	args := []string{"daemon", "serve", "--profile", opts.ProfilePath}
	if opts.LogLevel != "" {
		args = append(args, "--log-level", opts.LogLevel)
	}
	cmd := exec.Command(execPath, args...)
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	cmd.Stdin = nil
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		return Status{}, err
	}
	released := false
	defer func() {
		if !released {
			_ = cmd.Process.Signal(syscall.SIGTERM)
			_ = cmd.Process.Release()
		}
	}()

	timeout := opts.Timeout
	if timeout == 0 {
		timeout = 3 * time.Second
	}
	deadline := time.Now().Add(timeout)
	var last Status
	for time.Now().Before(deadline) {
		current, err := GetStatus(opts)
		if err == nil && current.Running && current.ProfilePath == opts.ProfilePath {
			if ready(ctx, current.Policies, 100*time.Millisecond) {
				_ = cmd.Process.Release()
				released = true
				return current, nil
			}
		}
		last = current
		time.Sleep(50 * time.Millisecond)
	}
	if last.Error != "" {
		return last, fmt.Errorf("daemon did not become ready: %s", last.Error)
	}
	return last, fmt.Errorf("daemon did not become ready within %s", timeout)
}

func GetStatus(opts Options) (Status, error) {
	paths, err := Paths()
	if err != nil {
		return Status{}, err
	}
	state, stateErr := readState(paths)
	pid := state.PID
	if pid == 0 {
		pid = readPID(paths)
	}
	status := Status{
		PID:         pid,
		ProfilePath: state.ProfilePath,
		Policies:    state.Policies,
	}
	if status.ProfilePath == "" {
		status.ProfilePath = opts.ProfilePath
	}
	if stateErr != nil && !errors.Is(stateErr, os.ErrNotExist) {
		status.Error = stateErr.Error()
	}
	if pid == 0 {
		return status, nil
	}
	running := processRunning(pid)
	status.Running = running
	status.StalePID = !running
	if running && len(status.Policies) > 0 && !ready(context.Background(), status.Policies, 100*time.Millisecond) {
		status.Error = "daemon process is running but SOCKS5 listeners are not ready"
	}
	return status, nil
}

func ConfigFromProfile(profilePath, logLevel string) (bridge.MultiConfig, []Policy, error) {
	managed, err := surge.ManagedPolicies(profilePath)
	if err != nil {
		return bridge.MultiConfig{}, nil, err
	}
	policies := make([]Policy, 0, len(managed))
	bridgePolicies := make([]bridge.PolicyConfig, 0, len(managed))
	for _, item := range managed {
		node, err := vless.Parse(item.Link)
		if err != nil {
			return bridge.MultiConfig{}, nil, fmt.Errorf("%s: %w", item.Name, err)
		}
		host := item.LocalHost
		if host == "" {
			host = "127.0.0.1"
		}
		policies = append(policies, Policy{
			Name:      item.Name,
			LocalHost: host,
			LocalPort: item.LocalPort,
		})
		bridgePolicies = append(bridgePolicies, bridge.PolicyConfig{
			Name:      item.Name,
			Node:      node,
			LocalHost: host,
			LocalPort: item.LocalPort,
		})
	}
	if logLevel == "" {
		logLevel = defaultLogLevel
	}
	return bridge.MultiConfig{Policies: bridgePolicies, LogLevel: logLevel}, policies, nil
}

func Paths() (RuntimePaths, error) {
	base, err := os.UserCacheDir()
	if err != nil || base == "" {
		base = os.TempDir()
	}
	dir := filepath.Join(base, "xcore-bridge")
	return RuntimePaths{
		Dir:   dir,
		PID:   filepath.Join(dir, "daemon.pid"),
		State: filepath.Join(dir, "daemon.json"),
		Log:   filepath.Join(dir, "daemon.log"),
	}, nil
}

func policyReady(ctx context.Context, status Status, opts Options) bool {
	if opts.ProfilePath != "" && status.ProfilePath != "" && opts.ProfilePath != status.ProfilePath {
		return false
	}
	if len(status.Policies) == 0 {
		return false
	}
	if opts.ProfilePath != "" {
		_, expected, err := ConfigFromProfile(opts.ProfilePath, opts.LogLevel)
		if err != nil || !samePolicies(status.Policies, expected) {
			return false
		}
	}
	return ready(ctx, status.Policies, 100*time.Millisecond)
}

func samePolicies(a, b []Policy) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func ready(ctx context.Context, policies []Policy, timeout time.Duration) bool {
	for _, policy := range policies {
		if err := bridge.WaitForReady(ctx, policy.LocalHost, policy.LocalPort, timeout); err != nil {
			return false
		}
	}
	return true
}

func validateProfile(profilePath string) error {
	if strings.TrimSpace(profilePath) == "" {
		return fmt.Errorf("profile path is required")
	}
	if _, err := os.Stat(profilePath); err != nil {
		return err
	}
	return nil
}

func writeState(paths RuntimePaths, state stateFile) error {
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(paths.PID, []byte(strconv.Itoa(state.PID)+"\n"), 0o644); err != nil {
		return err
	}
	return os.WriteFile(paths.State, append(data, '\n'), 0o644)
}

func readState(paths RuntimePaths) (stateFile, error) {
	data, err := os.ReadFile(paths.State)
	if err != nil {
		return stateFile{}, err
	}
	var state stateFile
	if err := json.Unmarshal(data, &state); err != nil {
		return stateFile{}, err
	}
	return state, nil
}

func readPID(paths RuntimePaths) int {
	data, err := os.ReadFile(paths.PID)
	if err != nil {
		return 0
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil || pid <= 0 {
		return 0
	}
	return pid
}

func cleanupState(paths RuntimePaths, pid int) {
	current := readPID(paths)
	if current == 0 || current == pid {
		_ = removeRuntimeFiles()
	}
}

func removeRuntimeFiles() error {
	paths, err := Paths()
	if err != nil {
		return err
	}
	_ = os.Remove(paths.PID)
	_ = os.Remove(paths.State)
	return nil
}

func processRunning(pid int) bool {
	if pid <= 0 {
		return false
	}
	process, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	err = process.Signal(syscall.Signal(0))
	if err == nil {
		return true
	}
	return errors.Is(err, syscall.EPERM)
}

func WaitForPolicy(ctx context.Context, host string, port int, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for {
		if err := bridge.WaitForReady(ctx, host, port, 100*time.Millisecond); err == nil {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("daemon SOCKS5 listener did not become ready at %s within %s", net.JoinHostPort(host, strconv.Itoa(port)), timeout)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(50 * time.Millisecond):
		}
	}
}
