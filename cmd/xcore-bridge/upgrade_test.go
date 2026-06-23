package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestUpgradeDryRunUsesStableRelease(t *testing.T) {
	withUpgradePlatform(t, "darwin", "arm64")
	t.Setenv("XCORE_BRIDGE_CHANNEL", "")
	t.Setenv("XCORE_BRIDGE_VERSION", "")
	t.Setenv("XCORE_BRIDGE_REPO", "")
	t.Setenv("XCORE_BRIDGE_INSTALL_DIR", "")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/backrunner/xcore-bridge/releases/latest" {
			http.NotFound(w, r)
			return
		}
		fmt.Fprint(w, `{"tag_name":"v1.2.3","prerelease":false}`)
	}))
	defer server.Close()

	var stdout bytes.Buffer
	err := run([]string{
		"upgrade",
		"--dry-run",
		"--api-url", server.URL,
		"--download-url", server.URL,
		"--target", filepath.Join(t.TempDir(), "xcore-bridge"),
	}, &stdout, &bytes.Buffer{})
	if err != nil {
		t.Fatal(err)
	}
	output := stdout.String()
	for _, want := range []string{
		"would upgrade xcore-bridge",
		"from: dev",
		"to: v1.2.3",
		"channel: stable",
		"asset: xcore-bridge_v1.2.3_darwin_arm64.tar.gz",
		"path: ",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("expected %q in output:\n%s", want, output)
		}
	}
}

func TestUpgradeAutoFallsBackToBetaRelease(t *testing.T) {
	withUpgradePlatform(t, "darwin", "amd64")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repos/backrunner/xcore-bridge/releases/latest":
			http.NotFound(w, r)
		case "/repos/backrunner/xcore-bridge/releases":
			fmt.Fprint(w, `[
				{"tag_name":"v2.0.0-beta.2","prerelease":true,"draft":true},
				{"tag_name":"v2.0.0-beta.1","prerelease":true,"draft":false},
				{"tag_name":"v1.9.0","prerelease":false,"draft":false}
			]`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	result, err := runUpgrade(context.Background(), upgradeOptions{
		Repo:           defaultUpgradeRepo,
		Channel:        "auto",
		APIBase:        server.URL,
		DownloadBase:   server.URL,
		TargetPath:     filepath.Join(t.TempDir(), "xcore-bridge"),
		CurrentVersion: "v1.0.0",
		DryRun:         true,
		HTTPClient:     server.Client(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.TargetVersion != "v2.0.0-beta.1" || result.Channel != "beta" {
		t.Fatalf("expected beta fallback, got %#v", result)
	}
	if result.AssetName != "xcore-bridge_v2.0.0-beta.1_darwin_amd64.tar.gz" {
		t.Fatalf("unexpected asset name: %s", result.AssetName)
	}
}

func TestUpgradeVersionUsesExactTag(t *testing.T) {
	withUpgradePlatform(t, "darwin", "arm64")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatalf("exact version should not call release API: %s", r.URL.Path)
	}))
	defer server.Close()

	result, err := runUpgrade(context.Background(), upgradeOptions{
		Repo:           defaultUpgradeRepo,
		Channel:        "beta",
		ExactVersion:   "v1.2.3",
		APIBase:        server.URL,
		DownloadBase:   server.URL,
		TargetPath:     filepath.Join(t.TempDir(), "xcore-bridge"),
		CurrentVersion: "v1.0.0",
		DryRun:         true,
		HTTPClient:     server.Client(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.TargetVersion != "v1.2.3" || result.Channel != "tag" {
		t.Fatalf("expected exact tag, got %#v", result)
	}
}

func TestUpgradeDownloadsVerifiesAndInstallsBetaRelease(t *testing.T) {
	withUpgradePlatform(t, "darwin", "arm64")
	dir := t.TempDir()
	target := filepath.Join(dir, "xcore-bridge")
	if err := os.WriteFile(target, []byte("old-binary"), 0o700); err != nil {
		t.Fatal(err)
	}

	assetName := "xcore-bridge_v2.0.0-beta.1_darwin_arm64.tar.gz"
	newBinary := []byte("new-binary")
	archive := makeUpgradeArchive(t, "xcore-bridge_v2.0.0-beta.1_darwin_arm64/xcore-bridge", newBinary)
	sum := sha256.Sum256(archive)
	checksums := fmt.Sprintf("%x  %s\n", sum, assetName)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repos/backrunner/xcore-bridge/releases":
			fmt.Fprint(w, `[{"tag_name":"v2.0.0-beta.1","prerelease":true,"draft":false}]`)
		case "/backrunner/xcore-bridge/releases/download/v2.0.0-beta.1/" + assetName:
			w.Write(archive)
		case "/backrunner/xcore-bridge/releases/download/v2.0.0-beta.1/checksums.txt":
			fmt.Fprint(w, checksums)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	result, err := runUpgrade(context.Background(), upgradeOptions{
		Repo:           defaultUpgradeRepo,
		Channel:        "beta",
		APIBase:        server.URL,
		DownloadBase:   server.URL,
		TargetPath:     target,
		CurrentVersion: "v1.0.0",
		HTTPClient:     server.Client(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.TargetVersion != "v2.0.0-beta.1" || result.Channel != "beta" {
		t.Fatalf("unexpected result: %#v", result)
	}
	installed, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if string(installed) != string(newBinary) {
		t.Fatalf("target was not replaced:\n%s", installed)
	}
	info, err := os.Stat(target)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm()&0o111 == 0 {
		t.Fatalf("installed binary should be executable, mode=%v", info.Mode().Perm())
	}
}

func TestUpgradeSkipsMatchingVersionWithoutDownloading(t *testing.T) {
	withUpgradePlatform(t, "darwin", "arm64")
	downloadHit := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repos/backrunner/xcore-bridge/releases/latest":
			fmt.Fprint(w, `{"tag_name":"v1.2.3","prerelease":false}`)
		default:
			downloadHit = true
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	result, err := runUpgrade(context.Background(), upgradeOptions{
		Repo:           defaultUpgradeRepo,
		Channel:        "stable",
		APIBase:        server.URL,
		DownloadBase:   server.URL,
		TargetPath:     filepath.Join(t.TempDir(), "xcore-bridge"),
		CurrentVersion: "v1.2.3",
		HTTPClient:     server.Client(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Skipped {
		t.Fatalf("expected matching version to be skipped: %#v", result)
	}
	if downloadHit {
		t.Fatal("matching version should not download release assets")
	}
}

func TestUpgradeRejectsUnsupportedChannel(t *testing.T) {
	err := run([]string{"upgrade", "--channel", "nightly", "--dry-run"}, &bytes.Buffer{}, &bytes.Buffer{})
	if err == nil {
		t.Fatal("expected unsupported channel to fail")
	}
	if !strings.Contains(err.Error(), "auto, stable, or beta") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func makeUpgradeArchive(t *testing.T, name string, content []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	gzipWriter := gzip.NewWriter(&buf)
	tarWriter := tar.NewWriter(gzipWriter)
	if err := tarWriter.WriteHeader(&tar.Header{
		Name: name,
		Mode: 0o755,
		Size: int64(len(content)),
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := tarWriter.Write(content); err != nil {
		t.Fatal(err)
	}
	if err := tarWriter.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gzipWriter.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func withUpgradePlatform(t *testing.T, goos, goarch string) {
	t.Helper()
	previous := upgradePlatform
	upgradePlatform = func() (string, string) { return goos, goarch }
	t.Cleanup(func() {
		upgradePlatform = previous
	})
}
