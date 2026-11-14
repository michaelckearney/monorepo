package browser

import (
	"os"
	"path/filepath"
	"testing"
)

// The Windows download says its version only in a file's name.
func TestManifestVersion(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"chrome.exe", "chrome.dll", "151.0.7922.34.manifest", "ABOUT"} {
		if err := os.WriteFile(filepath.Join(dir, name), nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if v, err := manifestVersion(dir); err != nil || v != "151.0.7922.34" {
		t.Errorf("manifestVersion = %q, %v", v, err)
	}
	if _, err := manifestVersion(t.TempDir()); err == nil {
		t.Error("no manifest, no error")
	}
}
