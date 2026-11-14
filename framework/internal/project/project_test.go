package project

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/michaelckearney/monorepo/framework/internal/pin"
)

func writeProject(t *testing.T, config string) string {
	t.Helper()
	root := t.TempDir()
	files := map[string]string{
		"desktop.yaml":        config,
		"go.mod":              "module example.com/app\n\ngo 1.26\n\nignore ./node_modules\n\nreplace example.com/lib => ../lib\n",
		"package.json":        "{}",
		"backend/app.go":      "package backend\n",
		"frontend/index.html": "",
	}
	for name, content := range files {
		path := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func TestLoadDefaultsAndBackend(t *testing.T) {
	p, err := Load(writeProject(t, "name: Demo\n"))
	if err != nil {
		t.Fatal(err)
	}
	if p.Config.Window.Title != "Demo" || p.Config.Window.Width != 1024 || p.Config.Window.Height != 768 {
		t.Errorf("window defaults: %+v", p.Config.Window)
	}
	if p.Module != "example.com/app" || p.Backend != "example.com/app/backend" {
		t.Errorf("module %q, backend %q", p.Module, p.Backend)
	}
	// ParseLax drops replace directives, which broke the first demo run: the
	// generated app could not find a backend dependency on a local path.
	if len(p.Replaces) != 1 {
		t.Errorf("replaces: %d, want 1", len(p.Replaces))
	}
}

func TestLoadNeedsOneRootModuleAndPackage(t *testing.T) {
	for name, change := range map[string]func(root string){
		"go.mod not found":       func(root string) { os.Remove(filepath.Join(root, "go.mod")) },
		"package.json not found": func(root string) { os.Remove(filepath.Join(root, "package.json")) },
		"add the line `ignore ./node_modules`": func(root string) {
			os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example.com/app\n\ngo 1.26\n"), 0o644)
		},
		"merge backend/go.mod into ./go.mod": func(root string) {
			os.WriteFile(filepath.Join(root, "backend/go.mod"), []byte("module example.com/app/backend\n"), 0o644)
		},
		"merge tests/go.mod into ./go.mod": func(root string) {
			os.MkdirAll(filepath.Join(root, "tests"), 0o755)
			os.WriteFile(filepath.Join(root, "tests/go.mod"), []byte("module example.com/app/tests\n"), 0o644)
		},
		"merge frontend/package.json into ./package.json": func(root string) {
			os.WriteFile(filepath.Join(root, "frontend/package.json"), []byte("{}"), 0o644)
		},
	} {
		root := writeProject(t, "name: Demo\n")
		change(root)
		_, err := Load(root)
		if err == nil || !strings.Contains(err.Error(), name) {
			t.Errorf("want an error containing %q, got %v", name, err)
		}
	}
}

func TestLoadErrorsAreOneActionableLine(t *testing.T) {
	for config, want := range map[string]string{
		"":                                                        "name is missing",
		"nmae: Demo\n":                                            "field nmae is not a desktop field; the fields are name,",
		"name: Demo\nwindow:\n  colour: red\n":                    "field colour is not a desktop field",
		"name: Demo\nid: 1password\n":                             "id \"1password\" is not valid",
		"name: Demo\nid: com.example.my_app\n":                    "id \"com.example.my_app\" is not valid",
		"name: Demo\nversion: 0.1.0\n":                            "version is not a desktop field any more",
		"name: Demo\ndata: /var/demo\n":                           "must start with ~/",
		"name: Demo\ndata: ~/\n":                                  "must be a folder inside the home folder",
		"name: Demo\ndata: ~/../x\n":                              "must be a folder inside the home folder",
		"name: Demo\ndata: ~/.desktop/x\n":                        "where desktop keeps its own files",
		"name: Demo\ndeps:\n  t:\n    plan9/arm: https://x\n":     "\"plan9/arm\" is not a platform",
		"name: Demo\ndeps:\n  t:\n    linux/arm64: http://x\n":    "must start with https://",
		"name: Demo\ndeps:\n  a/b:\n    linux/arm64: https://x\n": "dependency name \"a/b\" is not valid",
	} {
		_, err := Load(writeProject(t, config))
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("config %q: got %v, want it to contain %q", config, err, want)
		}
		if err != nil && strings.Contains(err.Error(), "\n") {
			t.Errorf("config %q: error spans lines: %q", config, err)
		}
	}
}

func TestIDDefaults(t *testing.T) {
	p, err := Load(writeProject(t, "name: My Cool App!\n"))
	if err != nil {
		t.Fatal(err)
	}
	if p.Config.ID != "local.desktop.my-cool-app" {
		t.Errorf("default id: %q", p.Config.ID)
	}
	if p.Config.Data != "~/.my-cool-app" {
		t.Errorf("default data: %q", p.Config.Data)
	}
	// The last part of the id, not the name: renaming the app must not
	// strand its data.
	p, err = Load(writeProject(t, "name: Renamed\nid: com.example.harbor\n"))
	if err != nil {
		t.Fatal(err)
	}
	if p.Config.Data != "~/.harbor" {
		t.Errorf("data from id: %q", p.Config.Data)
	}
	for name, want := range map[string]string{"3D Viewer": "app-3d-viewer", "---": "app"} {
		if got := Slug(name); got != want {
			t.Errorf("Slug(%q) = %q, want %q", name, got, want)
		}
	}
}

func TestFindWalksUp(t *testing.T) {
	root := writeProject(t, "name: Demo\n")
	got, err := Find(filepath.Join(root, "backend"))
	if err != nil {
		t.Fatal(err)
	}
	if got != root {
		t.Errorf("Find: got %s, want %s", got, root)
	}
	if _, err := Find(t.TempDir()); err == nil {
		t.Error("Find outside a project: want an error")
	}
}

// A repository whose app is in a subfolder, as desktop's own app/ is, works
// from its top.
func TestFindTakesTheOneProjectBelow(t *testing.T) {
	root := writeProject(t, "name: Demo\n")
	got, err := Find(filepath.Dir(root))
	if err != nil {
		t.Fatal(err)
	}
	if got != root {
		t.Errorf("Find from above: got %s, want %s", got, root)
	}

	top := t.TempDir()
	for _, sub := range []string{"one", "two"} {
		if err := os.MkdirAll(filepath.Join(top, sub), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(top, sub, ConfigFile), []byte("name: Demo\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := Find(top); err == nil || !strings.Contains(err.Error(), "one, two") {
		t.Errorf("Find with two projects below: %v", err)
	}
}

// browser: true brings desktop's own Chromium, for every platform, as the
// dependency package browser looks for.
func TestBrowserAddsTheChromiumDesktopPins(t *testing.T) {
	p, err := Load(writeProject(t, "name: Demo\nbrowser: true\n"))
	if err != nil {
		t.Fatal(err)
	}
	urls := p.Config.Deps[pin.ChromiumDep]
	for platform, d := range pin.Chromium {
		if urls[platform] != d.URL {
			t.Errorf("chromium for %s is %q, want %q", platform, urls[platform], d.URL)
		}
	}

	_, err = Load(writeProject(t, "name: Demo\nbrowser: true\ndeps:\n  chromium:\n    darwin/arm64: https://example.com/c.zip\n"))
	if err == nil || !strings.Contains(err.Error(), "remove chromium from deps") {
		t.Errorf("a chromium of the app's own beside browser: true: %v", err)
	}
}

func TestModes(t *testing.T) {
	p, err := Load(writeProject(t, "name: Harbor\nid: com.example.harbor\n"))
	if err != nil {
		t.Fatal(err)
	}
	if got := p.Config.ModeNames(); len(got) != 1 || got[0] != "prod" || p.Config.DefaultMode != "prod" {
		t.Errorf("with no modes: %v, default %q; want prod alone", got, p.Config.DefaultMode)
	}

	p, err = Load(writeProject(t, `name: Harbor
id: com.example.harbor
default_mode: prod
modes:
  prod:
  dev: { name: Harbor Dev, id: com.example.harbor.dev }
`))
	if err != nil {
		t.Fatal(err)
	}
	if got := p.Config.ModeNames(); strings.Join(got, ",") != "prod,dev" {
		t.Errorf("modes %v, want the default first", got)
	}
	prod, dev := p.Config.Mode("prod"), p.Config.Mode("dev")
	if prod.Name != "Harbor" || prod.ID != "com.example.harbor" || prod.Data != "~/.harbor/data/prod" {
		t.Errorf("prod: %+v", prod)
	}
	if dev.Name != "Harbor Dev" || dev.ID != "com.example.harbor.dev" || dev.Title != "Harbor Dev" || dev.Data != "~/.harbor/data/dev" {
		t.Errorf("dev: %+v", dev)
	}
	if m, err := p.FindMode(""); err != nil || m.Mode != "prod" {
		t.Errorf("FindMode(\"\") = %+v, %v; want the default", m, err)
	}
	if _, err := p.FindMode("stage"); err == nil || !strings.Contains(err.Error(), "the modes in desktop.yaml are prod, dev") {
		t.Errorf("FindMode(stage): %v", err)
	}

	for config, want := range map[string]string{
		"modes:\n  prod:\n  dev:\n":                                   "default_mode is missing",
		"default_mode: stage\nmodes:\n  prod:\n":                      "default_mode \"stage\" is not a mode",
		"default_mode: prod\nmodes:\n  prod:\n  dev:\n":               "the same id",
		"default_mode: prod\nmodes:\n  prod:\n  dev: { id: a.b.c }\n": "the same name",
		"modes:\n  Dev:\n":                                            "mode \"Dev\" is not valid",
		"modes:\n  assets:\n":                                         "cannot be called assets",
		"modes:\n  dev: { colour: red }\n":                            "field colour is not a desktop field",
	} {
		_, err := Load(writeProject(t, "name: Harbor\n"+config))
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("config %q: got %v, want %q", config, err, want)
		}
	}
}
