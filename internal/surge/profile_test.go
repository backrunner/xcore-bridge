package surge

import (
	"os"
	"path/filepath"
	"testing"
)

func TestProfileHasManagedBlock(t *testing.T) {
	dir := t.TempDir()
	profile := filepath.Join(dir, "surge.conf")
	if err := os.WriteFile(profile, []byte("[Proxy]\nDIRECTISH = direct\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	ok, err := ProfileHasManagedBlock(profile)
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Fatal("profile should not be marked as managed")
	}
	if err := os.WriteFile(profile, []byte("[Proxy]\n"+markerBegin+"\n"+markerEnd+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	ok, err = ProfileHasManagedBlock(profile)
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("profile should be marked as managed")
	}
}
