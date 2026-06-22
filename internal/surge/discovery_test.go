package surge

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDiscoverProfilesPrefersICloudDefaultProfile(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	icloudProfiles := filepath.Join(home, "Library", "Mobile Documents", "iCloud~com~nssurge~Inc~Surge", "Documents", "Profiles")
	localProfiles := filepath.Join(home, "Library", "Application Support", "Surge", "Profiles")
	for _, dir := range []string{icloudProfiles, localProfiles} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	for _, path := range []string{
		filepath.Join(icloudProfiles, "work.conf"),
		filepath.Join(icloudProfiles, "default.conf"),
		filepath.Join(localProfiles, "default.conf"),
	} {
		if err := os.WriteFile(path, []byte("[Proxy]\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	candidates, err := DiscoverProfiles()
	if err != nil {
		t.Fatal(err)
	}
	if len(candidates) != 3 {
		t.Fatalf("expected 3 candidates, got %#v", candidates)
	}
	want := filepath.Join(icloudProfiles, "default.conf")
	if candidates[0].Path != want {
		t.Fatalf("expected iCloud default profile first, got %#v", candidates)
	}
	if candidates[0].Source != "iCloud Drive" {
		t.Fatalf("unexpected source: %#v", candidates[0])
	}
}
