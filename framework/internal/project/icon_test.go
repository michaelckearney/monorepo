package project

import (
	"image"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writePNG(t *testing.T, path string, w, h int) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := png.Encode(f, image.NewNRGBA(image.Rect(0, 0, w, h))); err != nil {
		t.Fatal(err)
	}
}

func TestIcon(t *testing.T) {
	root := writeProject(t, "name: Demo\nicon: assets/icon.png\ndefault_mode: prod\nmodes:\n  prod:\n  dev:\n    name: Demo Dev\n    id: com.example.demo.dev\n    icon: assets/dev.png\n")
	writePNG(t, filepath.Join(root, "assets/icon.png"), 1024, 1024)
	writePNG(t, filepath.Join(root, "assets/dev.png"), 2048, 2048)
	p, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := p.Config.Mode("prod").Icon, filepath.Join(root, "assets/icon.png"); got != want {
		t.Errorf("prod icon %q, want %q", got, want)
	}
	if got, want := p.Config.Mode("dev").Icon, filepath.Join(root, "assets/dev.png"); got != want {
		t.Errorf("dev icon %q, want %q", got, want)
	}
}

func TestNoIcon(t *testing.T) {
	p, err := Load(writeProject(t, "name: Demo\n"))
	if err != nil {
		t.Fatal(err)
	}
	if icon := p.Config.Mode("prod").Icon; icon != "" {
		t.Errorf("icon %q, want none", icon)
	}
}

func TestBadIcon(t *testing.T) {
	for want, setup := range map[string]func(root string) string{
		"must be square": func(root string) string {
			writePNG(t, filepath.Join(root, "icon.png"), 1024, 512)
			return "icon.png"
		},
		"must be at least 1024x1024": func(root string) string {
			writePNG(t, filepath.Join(root, "icon.png"), 512, 512)
			return "icon.png"
		},
		"is not a PNG file": func(root string) string {
			os.WriteFile(filepath.Join(root, "icon.png"), []byte("GIF89a"), 0o644)
			return "icon.png"
		},
		"not found":                            func(string) string { return "missing.png" },
		"must be a path inside the repository": func(string) string { return "../icon.png" },
	} {
		t.Run(want, func(t *testing.T) {
			root := writeProject(t, "")
			config := "name: Demo\nicon: " + setup(root) + "\n"
			os.WriteFile(filepath.Join(root, ConfigFile), []byte(config), 0o644)
			_, err := Load(root)
			if err == nil || !strings.Contains(err.Error(), want) {
				t.Errorf("error %v, want one containing %q", err, want)
			}
		})
	}
}
