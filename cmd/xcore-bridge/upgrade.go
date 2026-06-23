package main

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

const defaultUpgradeRepo = "backrunner/xcore-bridge"

var (
	upgradeExecutable = os.Executable
	upgradePlatform   = func() (string, string) { return runtime.GOOS, runtime.GOARCH }
	upgradeInstall    = installUpgradeBinary
)

type upgradeOptions struct {
	Repo           string
	Channel        string
	ExactVersion   string
	APIBase        string
	DownloadBase   string
	TargetPath     string
	CurrentVersion string
	DryRun         bool
	Force          bool
	HTTPClient     *http.Client
	Stdin          io.Reader
	Stderr         io.Writer
}

type upgradeResult struct {
	CurrentVersion string
	TargetVersion  string
	Channel        string
	TargetPath     string
	AssetName      string
	Skipped        bool
	DryRun         bool
}

type upgradeRelease struct {
	TagName string
	Channel string
}

type githubRelease struct {
	TagName    string `json:"tag_name"`
	Prerelease bool   `json:"prerelease"`
	Draft      bool   `json:"draft"`
}

func upgradeCommand(args []string, stdout, stderr io.Writer, stdin io.Reader) error {
	if stderr == nil {
		stderr = io.Discard
	}
	fs := flag.NewFlagSet("upgrade", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	channel := fs.String("channel", upgradeEnv("XCORE_BRIDGE_CHANNEL", "auto"), "release channel: auto, stable, or beta")
	stable := fs.Bool("stable", false, "upgrade to the latest stable release")
	beta := fs.Bool("beta", false, "upgrade to the latest beta/prerelease")
	prerelease := fs.Bool("prerelease", false, "upgrade to the latest beta/prerelease")
	exactVersion := fs.String("version", upgradeEnv("XCORE_BRIDGE_VERSION", ""), "exact GitHub release tag")
	repo := fs.String("repo", upgradeEnv("XCORE_BRIDGE_REPO", defaultUpgradeRepo), "GitHub owner/repo")
	bindir := fs.String("bindir", upgradeEnv("XCORE_BRIDGE_INSTALL_DIR", ""), "install directory containing xcore-bridge")
	target := fs.String("target", "", "binary path to replace; defaults to the current executable")
	apiBase := fs.String("api-url", upgradeEnv("GITHUB_API_URL", "https://api.github.com"), "GitHub API base URL")
	downloadBase := fs.String("download-url", upgradeEnv("GITHUB_DOWNLOAD_URL", "https://github.com"), "GitHub download base URL")
	dryRun := fs.Bool("dry-run", false, "resolve the release and print what would change")
	force := fs.Bool("force", false, "reinstall even when the selected release matches the current version")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if (*stable && (*beta || *prerelease)) || (*beta && *prerelease) {
		return errors.New("upgrade accepts only one channel flag")
	}
	if *stable {
		*channel = "stable"
	}
	if *beta || *prerelease {
		*channel = "beta"
	}
	positionals := fs.Args()
	if len(positionals) > 1 {
		return errors.New("upgrade accepts at most one positional channel")
	}
	if len(positionals) == 1 {
		if *stable || *beta || *prerelease {
			return errors.New("upgrade accepts either a positional channel or a channel flag, not both")
		}
		*channel = positionals[0]
	}
	if _, err := normalizeUpgradeChannel(*channel); err != nil {
		return err
	}
	if strings.TrimSpace(*repo) == "" {
		return errors.New("upgrade requires a GitHub repo")
	}
	if strings.TrimSpace(*target) != "" && strings.TrimSpace(*bindir) != "" {
		return errors.New("upgrade accepts either --target or --bindir, not both")
	}

	targetPath := strings.TrimSpace(*target)
	if targetPath == "" && strings.TrimSpace(*bindir) != "" {
		targetPath = filepath.Join(strings.TrimSpace(*bindir), "xcore-bridge")
	}
	if targetPath == "" {
		executable, err := upgradeExecutable()
		if err != nil {
			return err
		}
		targetPath = executable
	}

	result, err := runUpgrade(context.Background(), upgradeOptions{
		Repo:           strings.Trim(strings.TrimSpace(*repo), "/"),
		Channel:        *channel,
		ExactVersion:   strings.TrimSpace(*exactVersion),
		APIBase:        strings.TrimSpace(*apiBase),
		DownloadBase:   strings.TrimSpace(*downloadBase),
		TargetPath:     targetPath,
		CurrentVersion: version,
		DryRun:         *dryRun,
		Force:          *force,
		HTTPClient:     &http.Client{Timeout: 60 * time.Second},
		Stdin:          stdin,
		Stderr:         stderr,
	})
	if err != nil {
		return err
	}

	switch {
	case result.Skipped:
		ui := newUI(stdout)
		ui.Success("xcore-bridge is already up to date")
		ui.KeyValue("version", result.TargetVersion)
		ui.KeyValue("channel", result.Channel)
	case result.DryRun:
		ui := newUI(stdout)
		ui.Info("would upgrade xcore-bridge")
		ui.KeyValue("from", result.CurrentVersion)
		ui.KeyValue("to", result.TargetVersion)
		ui.KeyValue("channel", result.Channel)
		ui.KeyValue("asset", result.AssetName)
		ui.KeyValue("path", result.TargetPath)
	default:
		ui := newUI(stdout)
		ui.Success("upgraded xcore-bridge")
		ui.KeyValue("from", result.CurrentVersion)
		ui.KeyValue("to", result.TargetVersion)
		ui.KeyValue("channel", result.Channel)
		ui.KeyValue("path", result.TargetPath)
	}
	return nil
}

func runUpgrade(ctx context.Context, opts upgradeOptions) (upgradeResult, error) {
	channel, err := normalizeUpgradeChannel(opts.Channel)
	if err != nil {
		return upgradeResult{}, err
	}
	client := opts.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 60 * time.Second}
	}
	release, err := resolveUpgradeRelease(ctx, client, opts.APIBase, opts.Repo, channel, opts.ExactVersion)
	if err != nil {
		return upgradeResult{}, err
	}
	goos, goarch := upgradePlatform()
	targetOS, targetArch, err := upgradeTargetPlatform(goos, goarch)
	if err != nil {
		return upgradeResult{}, err
	}
	assetName := fmt.Sprintf("xcore-bridge_%s_%s_%s.tar.gz", release.TagName, targetOS, targetArch)
	result := upgradeResult{
		CurrentVersion: opts.CurrentVersion,
		TargetVersion:  release.TagName,
		Channel:        release.Channel,
		TargetPath:     opts.TargetPath,
		AssetName:      assetName,
		DryRun:         opts.DryRun,
	}
	if opts.CurrentVersion == release.TagName && !opts.Force {
		result.Skipped = true
		result.DryRun = false
		return result, nil
	}
	if opts.DryRun {
		return result, nil
	}

	tmpdir, err := os.MkdirTemp("", "xcore-bridge-upgrade-*")
	if err != nil {
		return upgradeResult{}, err
	}
	defer os.RemoveAll(tmpdir)

	archivePath := filepath.Join(tmpdir, assetName)
	if err := downloadUpgradeFile(ctx, client, releaseDownloadURL(opts.DownloadBase, opts.Repo, release.TagName, assetName), archivePath); err != nil {
		return upgradeResult{}, err
	}
	checksums, err := downloadUpgradeBytes(ctx, client, releaseDownloadURL(opts.DownloadBase, opts.Repo, release.TagName, "checksums.txt"))
	if err != nil {
		return upgradeResult{}, err
	}
	if err := verifyUpgradeChecksum(archivePath, assetName, checksums); err != nil {
		return upgradeResult{}, err
	}
	binaryPath, err := extractUpgradeBinary(archivePath, tmpdir)
	if err != nil {
		return upgradeResult{}, err
	}
	if err := upgradeInstall(binaryPath, opts.TargetPath, opts.Stdin, opts.Stderr); err != nil {
		return upgradeResult{}, err
	}
	return result, nil
}

func normalizeUpgradeChannel(raw string) (string, error) {
	channel := strings.ToLower(strings.TrimSpace(raw))
	if channel == "" {
		channel = "auto"
	}
	switch channel {
	case "auto", "stable", "beta":
		return channel, nil
	case "prerelease":
		return "beta", nil
	default:
		return "", fmt.Errorf("upgrade channel must be auto, stable, or beta")
	}
}

func resolveUpgradeRelease(ctx context.Context, client *http.Client, apiBase, repo, channel, exactVersion string) (upgradeRelease, error) {
	if exactVersion != "" {
		return upgradeRelease{TagName: exactVersion, Channel: "tag"}, nil
	}
	switch channel {
	case "stable":
		tag, err := resolveStableRelease(ctx, client, apiBase, repo)
		if err != nil {
			return upgradeRelease{}, err
		}
		return upgradeRelease{TagName: tag, Channel: "stable"}, nil
	case "beta":
		tag, err := resolveBetaRelease(ctx, client, apiBase, repo)
		if err != nil {
			return upgradeRelease{}, err
		}
		return upgradeRelease{TagName: tag, Channel: "beta"}, nil
	case "auto":
		tag, stableErr := resolveStableRelease(ctx, client, apiBase, repo)
		if stableErr == nil {
			return upgradeRelease{TagName: tag, Channel: "stable"}, nil
		}
		tag, betaErr := resolveBetaRelease(ctx, client, apiBase, repo)
		if betaErr == nil {
			return upgradeRelease{TagName: tag, Channel: "beta"}, nil
		}
		return upgradeRelease{}, fmt.Errorf("could not resolve release for %s: stable: %v; beta: %v", repo, stableErr, betaErr)
	default:
		return upgradeRelease{}, fmt.Errorf("upgrade channel must be auto, stable, or beta")
	}
}

func resolveStableRelease(ctx context.Context, client *http.Client, apiBase, repo string) (string, error) {
	var release githubRelease
	if err := getUpgradeJSON(ctx, client, apiURL(apiBase, repo, "/releases/latest"), &release); err != nil {
		return "", err
	}
	if release.TagName == "" {
		return "", errors.New("latest release did not include a tag")
	}
	return release.TagName, nil
}

func resolveBetaRelease(ctx context.Context, client *http.Client, apiBase, repo string) (string, error) {
	var releases []githubRelease
	if err := getUpgradeJSON(ctx, client, apiURL(apiBase, repo, "/releases"), &releases); err != nil {
		return "", err
	}
	for _, release := range releases {
		if release.TagName != "" && release.Prerelease && !release.Draft {
			return release.TagName, nil
		}
	}
	return "", errors.New("no beta/prerelease release found")
}

func upgradeTargetPlatform(goos, goarch string) (string, string, error) {
	if goos != "darwin" {
		return "", "", fmt.Errorf("unsupported OS: %s; xcore-bridge is only distributed for macOS because Surge for Mac is required", goos)
	}
	switch goarch {
	case "amd64", "x86_64":
		return "darwin", "amd64", nil
	case "arm64", "aarch64":
		return "darwin", "arm64", nil
	default:
		return "", "", fmt.Errorf("unsupported architecture: %s", goarch)
	}
}

func getUpgradeJSON(ctx context.Context, client *http.Client, rawURL string, dst any) error {
	data, err := downloadUpgradeBytes(ctx, client, rawURL)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(data, dst); err != nil {
		return fmt.Errorf("decode %s: %w", rawURL, err)
	}
	return nil
}

func downloadUpgradeBytes(ctx context.Context, client *http.Client, rawURL string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return nil, fmt.Errorf("GET %s: %s", rawURL, resp.Status)
	}
	return io.ReadAll(resp.Body)
}

func downloadUpgradeFile(ctx context.Context, client *http.Client, rawURL, dst string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return err
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return fmt.Errorf("GET %s: %s", rawURL, resp.Status)
	}
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, resp.Body); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

func verifyUpgradeChecksum(archivePath, assetName string, checksums []byte) error {
	expected, err := checksumForAsset(checksums, assetName)
	if err != nil {
		return err
	}
	archive, err := os.Open(archivePath)
	if err != nil {
		return err
	}
	defer archive.Close()
	hash := sha256.New()
	if _, err := io.Copy(hash, archive); err != nil {
		return err
	}
	actual := hex.EncodeToString(hash.Sum(nil))
	if actual != expected {
		return fmt.Errorf("checksum mismatch for %s", assetName)
	}
	return nil
}

func checksumForAsset(checksums []byte, assetName string) (string, error) {
	for _, line := range strings.Split(string(checksums), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		name := strings.TrimPrefix(fields[len(fields)-1], "*")
		if name != assetName && filepath.Base(name) != assetName {
			continue
		}
		sum := strings.ToLower(fields[0])
		if len(sum) != sha256.Size*2 {
			return "", fmt.Errorf("invalid checksum for %s", assetName)
		}
		if _, err := hex.DecodeString(sum); err != nil {
			return "", fmt.Errorf("invalid checksum for %s: %w", assetName, err)
		}
		return sum, nil
	}
	return "", fmt.Errorf("checksums.txt does not contain %s", assetName)
}

func extractUpgradeBinary(archivePath, tmpdir string) (string, error) {
	archive, err := os.Open(archivePath)
	if err != nil {
		return "", err
	}
	defer archive.Close()
	gzipReader, err := gzip.NewReader(archive)
	if err != nil {
		return "", err
	}
	defer gzipReader.Close()

	tarReader := tar.NewReader(gzipReader)
	for {
		header, err := tarReader.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return "", err
		}
		name := path.Clean(header.Name)
		if path.IsAbs(name) || strings.HasPrefix(name, "../") {
			return "", fmt.Errorf("release archive contains unsafe path %q", header.Name)
		}
		if header.Typeflag != tar.TypeReg || path.Base(name) != "xcore-bridge" {
			continue
		}
		binaryPath := filepath.Join(tmpdir, "xcore-bridge")
		out, err := os.OpenFile(binaryPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o755)
		if err != nil {
			return "", err
		}
		if _, err := io.Copy(out, tarReader); err != nil {
			out.Close()
			return "", err
		}
		if err := out.Close(); err != nil {
			return "", err
		}
		return binaryPath, nil
	}
	return "", errors.New("release archive does not contain xcore-bridge")
}

func installUpgradeBinary(src, target string, stdin io.Reader, stderr io.Writer) error {
	if strings.TrimSpace(target) == "" {
		return errors.New("upgrade target path is empty")
	}
	err := installUpgradeBinaryDirect(src, target)
	if err == nil {
		return nil
	}
	if !errors.Is(err, fs.ErrPermission) {
		return err
	}
	if _, lookErr := exec.LookPath("sudo"); lookErr != nil {
		return fmt.Errorf("cannot write %s: %w", target, err)
	}
	if stderr == nil {
		stderr = io.Discard
	}
	if stdin == nil {
		stdin = os.Stdin
	}
	newUI(stderr).Warn("installing to %s requires administrator permission", target)
	command := exec.Command("sudo", "install", "-m", "0755", src, target)
	command.Stdin = stdin
	command.Stdout = stderr
	command.Stderr = stderr
	if err := command.Run(); err != nil {
		return fmt.Errorf("sudo install %s: %w", target, err)
	}
	return nil
}

func installUpgradeBinaryDirect(src, target string) error {
	mode := fs.FileMode(0o755)
	if info, err := os.Stat(target); err == nil && info.Mode().Perm() != 0 {
		mode = info.Mode().Perm() | 0o111
	}
	dir := filepath.Dir(target)
	tmp, err := os.CreateTemp(dir, ".xcore-bridge-upgrade-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	keepTemp := false
	defer func() {
		if !keepTemp {
			os.Remove(tmpName)
		}
	}()

	in, err := os.Open(src)
	if err != nil {
		tmp.Close()
		return err
	}
	if _, err := io.Copy(tmp, in); err != nil {
		in.Close()
		tmp.Close()
		return err
	}
	if err := in.Close(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Chmod(mode); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpName, target); err != nil {
		return err
	}
	keepTemp = true
	return nil
}

func apiURL(base, repo, suffix string) string {
	return strings.TrimRight(base, "/") + "/repos/" + strings.Trim(repo, "/") + suffix
}

func releaseDownloadURL(base, repo, tag, name string) string {
	return strings.TrimRight(base, "/") + "/" + strings.Trim(repo, "/") + "/releases/download/" + tag + "/" + name
}

func upgradeEnv(name, fallback string) string {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return fallback
	}
	return value
}
