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
	Dir       string
	PID       string
	State     string
	Log       string
	BridgeLog string
	Lock      string
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
	_ = AppendBridgeLog("start requested profile=%q", opts.ProfilePath)
	return withControlLock(ctx, opts, func() (Status, error) {
		status, err := startLocked(ctx, opts)
		if err != nil {
			_ = AppendBridgeLog("start failed profile=%q error=%q", opts.ProfilePath, err)
			return status, err
		}
		_ = AppendBridgeLog("start ready profile=%q pid=%d", status.ProfilePath, status.PID)
		return status, nil
	})
}

func startLocked(ctx context.Context, opts Options) (Status, error) {
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
	_ = AppendDaemonLog("serve starting profile=%q logLevel=%q", opts.ProfilePath, opts.LogLevel)
	if err := validateProfile(opts.ProfilePath); err != nil {
		_ = AppendDaemonLog("serve profile validation failed profile=%q error=%q", opts.ProfilePath, err)
		return err
	}
	paths, err := Paths()
	if err != nil {
		_ = AppendDaemonLog("serve paths failed error=%q", err)
		return err
	}
	if err := os.MkdirAll(paths.Dir, 0o755); err != nil {
		_ = AppendDaemonLog("serve mkdir failed dir=%q error=%q", paths.Dir, err)
		return err
	}
	cfg, policies, err := ConfigFromProfile(opts.ProfilePath, opts.LogLevel)
	if err != nil {
		_ = AppendDaemonLog("serve config failed profile=%q error=%q", opts.ProfilePath, err)
		return err
	}
	_ = AppendDaemonLog("serve config loaded profile=%q policies=%d", opts.ProfilePath, len(policies))
	server, err := bridge.StartMulti(ctx, cfg)
	if err != nil {
		_ = AppendDaemonLog("serve xray start failed profile=%q error=%q", opts.ProfilePath, err)
		return err
	}
	defer func() {
		if err := server.Close(); err != nil {
			_ = AppendDaemonLog("serve xray close failed error=%q", err)
		}
	}()
	state := stateFile{
		PID:         os.Getpid(),
		ProfilePath: opts.ProfilePath,
		StartedAt:   time.Now(),
		Policies:    policies,
	}
	if err := writeState(paths, state); err != nil {
		_ = AppendDaemonLog("serve state write failed profile=%q error=%q", opts.ProfilePath, err)
		return err
	}
	_ = AppendDaemonLog("serve ready profile=%q pid=%d policies=%d", opts.ProfilePath, os.Getpid(), len(policies))
	defer cleanupState(paths, os.Getpid())
	<-ctx.Done()
	_ = AppendDaemonLog("serve stopping profile=%q pid=%d reason=%q", opts.ProfilePath, os.Getpid(), ctx.Err())
	return nil
}

func Stop(opts Options) (Status, error) {
	_ = AppendBridgeLog("stop requested profile=%q", opts.ProfilePath)
	timeout := opts.Timeout
	if timeout == 0 {
		timeout = 3 * time.Second
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout+time.Second)
	defer cancel()
	return withControlLock(ctx, opts, func() (Status, error) {
		status, err := stopLocked(opts)
		if err != nil {
			_ = AppendBridgeLog("stop failed profile=%q pid=%d error=%q", opts.ProfilePath, status.PID, err)
			return status, err
		}
		_ = AppendBridgeLog("stop complete profile=%q pid=%d running=%t", status.ProfilePath, status.PID, status.Running)
		return status, nil
	})
}

func stopLocked(opts Options) (Status, error) {
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
	_ = AppendBridgeLog("restart requested profile=%q", opts.ProfilePath)
	return withControlLock(ctx, opts, func() (Status, error) {
		if _, err := stopLocked(opts); err != nil {
			_ = AppendBridgeLog("restart stop failed profile=%q error=%q", opts.ProfilePath, err)
			return Status{}, err
		}
		status, err := startLocked(ctx, opts)
		if err != nil {
			_ = AppendBridgeLog("restart start failed profile=%q error=%q", opts.ProfilePath, err)
			return status, err
		}
		_ = AppendBridgeLog("restart ready profile=%q pid=%d", status.ProfilePath, status.PID)
		return status, nil
	})
}

func Ensure(ctx context.Context, opts Options) (Status, error) {
	_ = AppendBridgeLog("ensure requested profile=%q", opts.ProfilePath)
	return withControlLock(ctx, opts, func() (Status, error) {
		status, err := ensureLocked(ctx, opts)
		if err != nil {
			_ = AppendBridgeLog("ensure failed profile=%q pid=%d error=%q", opts.ProfilePath, status.PID, err)
			return status, err
		}
		_ = AppendBridgeLog("ensure ready profile=%q pid=%d policies=%d", status.ProfilePath, status.PID, len(status.Policies))
		return status, nil
	})
}

func ensureLocked(ctx context.Context, opts Options) (Status, error) {
	status, _ := GetStatus(opts)
	if status.Running && policyReady(ctx, status, opts) {
		_ = AppendBridgeLog("ensure reused daemon profile=%q pid=%d", status.ProfilePath, status.PID)
		return status, nil
	}
	if status.Running {
		_ = AppendBridgeLog("ensure restarting daemon profile=%q pid=%d", status.ProfilePath, status.PID)
		if _, err := stopLocked(opts); err != nil {
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
		_ = AppendBridgeLog("daemon child start failed profile=%q exec=%q error=%q", opts.ProfilePath, execPath, err)
		return Status{}, err
	}
	_ = AppendBridgeLog("daemon child started profile=%q exec=%q pid=%d", opts.ProfilePath, execPath, cmd.Process.Pid)
	released := false
	defer func() {
		if !released {
			_ = AppendBridgeLog("daemon child cleanup profile=%q pid=%d", opts.ProfilePath, cmd.Process.Pid)
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
				_ = AppendBridgeLog("daemon child ready profile=%q pid=%d policies=%d", current.ProfilePath, current.PID, len(current.Policies))
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
		Dir:       dir,
		PID:       filepath.Join(dir, "daemon.pid"),
		State:     filepath.Join(dir, "daemon.json"),
		Log:       filepath.Join(dir, "daemon.log"),
		BridgeLog: filepath.Join(dir, "bridge.log"),
		Lock:      filepath.Join(dir, "daemon.lock"),
	}, nil
}

func withControlLock(ctx context.Context, opts Options, fn func() (Status, error)) (Status, error) {
	paths, err := Paths()
	if err != nil {
		return Status{}, err
	}
	if err := os.MkdirAll(paths.Dir, 0o755); err != nil {
		return Status{}, err
	}
	file, err := os.OpenFile(paths.Lock, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return Status{}, err
	}
	defer file.Close()

	timeout := opts.Timeout
	if timeout == 0 {
		timeout = 5 * time.Second
	}
	if err := acquireLock(ctx, file, timeout); err != nil {
		_ = AppendBridgeLog("control lock failed path=%q error=%q", paths.Lock, err)
		return Status{}, err
	}
	defer func() {
		_ = syscall.Flock(int(file.Fd()), syscall.LOCK_UN)
	}()
	return fn()
}

func acquireLock(ctx context.Context, file *os.File, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for {
		err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
		if err == nil {
			return nil
		}
		if !errors.Is(err, syscall.EWOULDBLOCK) && !errors.Is(err, syscall.EAGAIN) {
			return err
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("daemon control lock did not become available within %s", timeout)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(25 * time.Millisecond):
		}
	}
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
