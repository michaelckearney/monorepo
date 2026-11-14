package pack

import (
	"archive/zip"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPack(t *testing.T) {
	root := t.TempDir()
	exe := filepath.Join(root, "build", "darwin-arm64", "Harbor Dev.app", "Contents", "MacOS", "harbor-dev")
	linux := filepath.Join(root, "build", "linux-amd64", "harbor")
	for _, f := range []string{exe, linux} {
		if err := os.MkdirAll(filepath.Dir(f), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(f, []byte("binary"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	assets, err := Pack(root, []string{"build/darwin-arm64/Harbor Dev.app", "build/linux-amd64/harbor"})
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, a := range assets {
		names = append(names, filepath.Base(a))
	}
	if got := strings.Join(names, " "); got != "harbor-dev-darwin-arm64.zip harbor-linux-amd64 SHA256SUMS" {
		t.Errorf("assets: %s", got)
	}

	zr, err := zip.OpenReader(assets[0])
	if err != nil {
		t.Fatal(err)
	}
	defer zr.Close()
	found := false
	for _, f := range zr.File {
		if f.Name == "Harbor Dev.app/Contents/MacOS/harbor-dev" {
			found = true
			if f.Mode().Perm()&0o100 == 0 {
				t.Errorf("the executable lost its mode: %v", f.Mode())
			}
		}
	}
	if !found {
		t.Error("the zip has no Harbor Dev.app/Contents/MacOS/harbor-dev")
	}
	if info, err := os.Stat(assets[1]); err != nil || info.Mode().Perm()&0o100 == 0 {
		t.Errorf("the Linux binary: %v, %v", info, err)
	}
	sums, _ := os.ReadFile(assets[2])
	if !strings.Contains(string(sums), "  harbor-linux-amd64\n") {
		t.Errorf("SHA256SUMS:\n%s", sums)
	}
}
