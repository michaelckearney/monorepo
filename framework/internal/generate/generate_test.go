package generate

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/mod/modfile"
	"golang.org/x/mod/module"

	"github.com/michaelckearney/monorepo/framework/internal/migrate"
	"github.com/michaelckearney/monorepo/framework/internal/pin"
	"github.com/michaelckearney/monorepo/framework/internal/project"
)

func TestAppGoModRestatesReplaces(t *testing.T) {
	root := t.TempDir()
	p := &project.Project{
		Root:       root,
		BackendDir: filepath.Join(root, "backend"),
		DesktopDir: filepath.Join(root, ".desktop"),
		Module:     "example.com/app",
		Backend:    "example.com/app/backend",
		Replaces: []*modfile.Replace{
			{Old: module.Version{Path: "example.com/lib"}, New: module.Version{Path: "../lib"}},
			{Old: module.Version{Path: "example.com/fork"}, New: module.Version{Path: "example.com/other", Version: "v1.2.3"}},
		},
	}
	out, err := appGoMod(p, PathsFor(p), "")
	if err != nil {
		t.Fatal(err)
	}
	got := string(out)
	for _, want := range []string{
		"github.com/wailsapp/wails/v3 " + pin.Wails,
		"example.com/app => ../..",
		// Relative to the root in the source, relative to .desktop/app here.
		"example.com/lib => ../../../lib",
		"example.com/fork => example.com/other v1.2.3",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("go.mod is missing %q:\n%s", want, got)
		}
	}
}

func TestWriteIsStable(t *testing.T) {
	root := t.TempDir()
	for _, d := range []string{"backend", "frontend"} {
		if err := os.MkdirAll(filepath.Join(root, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	p := &project.Project{
		Root:        root,
		Config:      project.Config{Name: `Say "hi"`, Window: project.Window{Title: "t", Width: 1, Height: 2}},
		BackendDir:  filepath.Join(root, "backend"),
		FrontendDir: filepath.Join(root, "frontend"),
		DesktopDir:  filepath.Join(root, ".desktop"),
		DatabaseDir: filepath.Join(root, "database"),
		Module:      "example.com/app",
		Backend:     "example.com/app/backend",
	}
	p.Config.Modes, p.Config.DefaultMode = map[string]project.ModeConfig{"prod": {}}, "prod"
	paths, err := Write(p, testRuntime, "")
	if err != nil {
		t.Fatal(err)
	}
	main, err := os.ReadFile(filepath.Join(paths.AppDir, "main.go"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(main), `"prod": {name: "Say \"hi\""`) {
		t.Errorf("app name is not quoted safely:\n%s", main)
	}

	before, _ := os.Stat(filepath.Join(paths.AppDir, "main.go"))
	if _, err := Write(p, testRuntime, ""); err != nil {
		t.Fatal(err)
	}
	after, _ := os.Stat(filepath.Join(paths.AppDir, "main.go"))
	if !after.ModTime().Equal(before.ModTime()) {
		t.Error("a second Write with no change touched main.go")
	}
}

func TestWriteMenu(t *testing.T) {
	root := t.TempDir()
	for _, d := range []string{"backend", "frontend"} {
		if err := os.MkdirAll(filepath.Join(root, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	p := &project.Project{
		Root: root,
		Config: project.Config{Name: "Notes", Window: project.Window{Title: "t", Width: 1, Height: 2}, Menu: project.Menu{
			Settings: "open-settings",
			Menus: []project.Submenu{{Title: `Say "hi"`, Items: []project.MenuItem{
				{Label: "New…", Key: "CmdOrCtrl+N", Event: "new-note"},
				{},
			}}},
		}},
		BackendDir:  filepath.Join(root, "backend"),
		FrontendDir: filepath.Join(root, "frontend"),
		DesktopDir:  filepath.Join(root, ".desktop"),
		DatabaseDir: filepath.Join(root, "database"),
		Module:      "example.com/app",
		Backend:     "example.com/app/backend",
	}
	p.Config.Modes, p.Config.DefaultMode = map[string]project.ModeConfig{"prod": {}}, "prod"
	paths, err := Write(p, testRuntime, "")
	if err != nil {
		t.Fatal(err)
	}
	main, _ := os.ReadFile(filepath.Join(paths.AppDir, "main.go"))
	for _, want := range []string{
		`generated.Menu(mode.name, "open-settings", []generated.Submenu{`,
		`{Title: "Say \"hi\"", Items: []generated.MenuItem{`,
		`{Label: "New…", Key: "CmdOrCtrl+N", Event: "new-note"},`,
		`{Label: "", Key: "", Event: ""},`,
	} {
		if !strings.Contains(string(main), want) {
			t.Errorf("main.go is missing %s:\n%s", want, main)
		}
	}
	ts, _ := os.ReadFile(filepath.Join(p.DesktopDir, "desktop.ts"))
	if want := `export type MenuEvent = "open-settings" | "new-note";`; !strings.Contains(string(ts), want) {
		t.Errorf("desktop.ts is missing %s", want)
	}
	if want := "export function openLinksInBrowser()"; !strings.Contains(string(ts), want) {
		t.Errorf("desktop.ts is missing %s", want)
	}

	p.Config.Menu = project.Menu{}
	if _, err := Write(p, testRuntime, ""); err != nil {
		t.Fatal(err)
	}
	ts, _ = os.ReadFile(filepath.Join(p.DesktopDir, "desktop.ts"))
	if want := `export type MenuEvent = never;`; !strings.Contains(string(ts), want) {
		t.Errorf("with no menu, desktop.ts is missing %s", want)
	}
}

// Only an app with browser: true gets useTab and LiveView.
func TestWriteBrowser(t *testing.T) {
	root := t.TempDir()
	for _, d := range []string{"backend", "frontend"} {
		if err := os.MkdirAll(filepath.Join(root, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	p := &project.Project{
		Root:        root,
		Config:      project.Config{Name: "Web", Window: project.Window{Title: "t", Width: 1, Height: 2}},
		BackendDir:  filepath.Join(root, "backend"),
		FrontendDir: filepath.Join(root, "frontend"),
		DesktopDir:  filepath.Join(root, ".desktop"),
		DatabaseDir: filepath.Join(root, "database"),
		Module:      "example.com/app",
		Backend:     "example.com/app/backend",
	}
	p.Config.Modes, p.Config.DefaultMode = map[string]project.ModeConfig{"prod": {}}, "prod"
	for _, browser := range []bool{false, true} {
		p.Config.Browser = browser
		if _, err := Write(p, testRuntime, ""); err != nil {
			t.Fatal(err)
		}
		ts, _ := os.ReadFile(filepath.Join(p.DesktopDir, "desktop.ts"))
		for _, want := range []string{"export function useTab(", "export const LiveView = "} {
			if strings.Contains(string(ts), want) != browser {
				t.Errorf("with browser: %v, desktop.ts has %s: %v", browser, want, !browser)
			}
		}
	}
}

// Each mode's migrations are written on their own, and embedded only under
// that mode's build tag, so one mode's fixtures never reach another's build.
func TestWriteMigrationsPerMode(t *testing.T) {
	root := t.TempDir()
	files := map[string]string{
		"database/001_notes.sql":        "CREATE TABLE notes (body TEXT);",
		"database/dev/002_fixtures.sql": `INSERT INTO notes VALUES ({{file "assets/a.md"}});`,
		"database/dev/assets/a.md":      "it's",
		"database/003_more.sql":         "SELECT 1;",
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
	for _, d := range []string{"backend", "frontend"} {
		if err := os.MkdirAll(filepath.Join(root, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	p := &project.Project{
		Root: root,
		Config: project.Config{Name: "Notes", ID: "a.notes", Window: project.Window{Title: "Notes", Width: 1, Height: 2},
			Modes: map[string]project.ModeConfig{"prod": {}, "dev": {Name: "Notes Dev", ID: "a.notes.dev"}}, DefaultMode: "prod"},
		BackendDir:  filepath.Join(root, "backend"),
		FrontendDir: filepath.Join(root, "frontend"),
		DesktopDir:  filepath.Join(root, ".desktop"),
		DatabaseDir: filepath.Join(root, "database"),
		Module:      "example.com/app",
		Backend:     "example.com/app/backend",
	}
	paths, err := Write(p, testRuntime, "")
	if err != nil {
		t.Fatal(err)
	}
	list := func(mode string) string {
		entries, _ := os.ReadDir(filepath.Join(paths.MigrationsDir, mode))
		var names []string
		for _, e := range entries {
			names = append(names, e.Name())
		}
		return strings.Join(names, ",")
	}
	if got := list("dev"); got != ".keep,001_notes.sql,002_fixtures.sql,003_more.sql,shared.txt" {
		t.Errorf("dev: %s", got)
	}
	if got := list("prod"); got != ".keep,001_notes.sql,003_more.sql,shared.txt" {
		t.Errorf("prod: %s", got)
	}
	// desktop.NewDB's migrations, in the dev build: not the fixtures.
	shared, err := migrate.LoadShared(os.DirFS(filepath.Join(paths.MigrationsDir, "dev")))
	if err != nil || len(shared) != 2 || shared[0].Name != "001_notes.sql" || shared[1].Name != "003_more.sql" {
		t.Errorf("the dev build's shared migrations: %+v, %v", shared, err)
	}
	fixture, _ := os.ReadFile(filepath.Join(paths.MigrationsDir, "dev", "002_fixtures.sql"))
	if string(fixture) != "INSERT INTO notes VALUES ('it''s');" {
		t.Errorf("the fixture was rendered as %q", fixture)
	}
	for name, want := range map[string]string{
		"migrations_dev.go":  "//go:build desktop_mode_dev\n",
		"migrations_prod.go": "//go:embed all:migrations/prod\n",
		"migrations_none.go": "//go:build !desktop_mode_prod && !desktop_mode_dev\n",
	} {
		src, _ := os.ReadFile(filepath.Join(paths.AppDir, name))
		if !strings.Contains(string(src), want) {
			t.Errorf("%s is missing %q:\n%s", name, want, src)
		}
	}
	main, _ := os.ReadFile(filepath.Join(paths.AppDir, "main.go"))
	if want := `"dev": {name: "Notes Dev", id: "a.notes.dev", title: "Notes Dev"},`; !strings.Contains(string(main), want) {
		t.Errorf("main.go is missing %s", want)
	}
	env, _ := os.ReadFile(filepath.Join(p.DesktopDir, "env.d.ts"))
	if want := `type DesktopMode = "prod" | "dev";`; !strings.Contains(string(env), want) {
		t.Errorf("env.d.ts is missing %s", want)
	}

	// A mode taken out of desktop.yaml leaves nothing behind.
	delete(p.Config.Modes, "dev")
	os.RemoveAll(filepath.Join(root, "database", "dev"))
	os.Rename(filepath.Join(root, "database", "003_more.sql"), filepath.Join(root, "database", "002_more.sql"))
	if _, err := Write(p, testRuntime, ""); err != nil {
		t.Fatal(err)
	}
	for _, gone := range []string{filepath.Join(paths.MigrationsDir, "dev"), filepath.Join(paths.AppDir, "migrations_dev.go")} {
		if _, err := os.Stat(gone); err == nil {
			t.Errorf("%s is still there", gone)
		}
	}
}

func TestCLIMigrationsAreTheModesRendered(t *testing.T) {
	root := t.TempDir()
	for name, content := range map[string]string{
		"database/001_notes.sql":        "CREATE TABLE notes (body TEXT);",
		"database/dev/002_fixtures.sql": "INSERT INTO notes VALUES ({{file \"assets/a.md\"}});",
		"database/dev/assets/a.md":      "hi",
	} {
		path := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	p := &project.Project{Root: root, DesktopDir: filepath.Join(root, ".desktop"), DatabaseDir: filepath.Join(root, "database")}
	p.Config.Modes, p.Config.DefaultMode = map[string]project.ModeConfig{"prod": {}, "dev": {}}, "prod"
	dir, err := CLIMigrations(p, "dev")
	if err != nil {
		t.Fatal(err)
	}
	if dir != filepath.Join(root, ".desktop", "cli", "dev", "migrations") {
		t.Errorf("dir: %s", dir)
	}
	got, err := os.ReadFile(filepath.Join(dir, "002_fixtures.sql"))
	if err != nil || string(got) != "INSERT INTO notes VALUES ('hi');" {
		t.Errorf("fixtures: %q, %v", got, err)
	}
	shared, err := os.ReadFile(filepath.Join(dir, migrate.SharedFile))
	if err != nil || string(shared) != "001_notes.sql\n" {
		t.Errorf("shared: %q, %v", shared, err)
	}
}

// Each mode embeds its own icon alone, and a mode with none embeds nothing.
func TestWriteIconsPerMode(t *testing.T) {
	root := t.TempDir()
	for _, d := range []string{"backend", "frontend", "database"} {
		if err := os.MkdirAll(filepath.Join(root, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	icon := filepath.Join(root, "icon.png")
	if err := os.WriteFile(icon, []byte("png bytes"), 0o644); err != nil {
		t.Fatal(err)
	}
	p := &project.Project{
		Root: root,
		Config: project.Config{Name: "Notes", ID: "a.notes", Window: project.Window{Title: "Notes", Width: 1, Height: 2},
			Modes: map[string]project.ModeConfig{"prod": {Icon: icon}, "dev": {Name: "Notes Dev", ID: "a.notes.dev"}}, DefaultMode: "prod"},
		BackendDir:  filepath.Join(root, "backend"),
		FrontendDir: filepath.Join(root, "frontend"),
		DesktopDir:  filepath.Join(root, ".desktop"),
		DatabaseDir: filepath.Join(root, "database"),
		Module:      "example.com/app",
		Backend:     "example.com/app/backend",
	}
	paths, err := Write(p, testRuntime, "")
	if err != nil {
		t.Fatal(err)
	}
	read := func(name string) string {
		raw, err := os.ReadFile(filepath.Join(paths.AppDir, name))
		if err != nil {
			t.Fatal(err)
		}
		return string(raw)
	}
	if got := read("icons/prod.png"); got != "png bytes" {
		t.Errorf("icons/prod.png = %q", got)
	}
	if _, err := os.Stat(filepath.Join(paths.AppDir, "icons", "dev.png")); err == nil {
		t.Error("dev has no icon but icons/dev.png was written")
	}
	if got := read("icon_prod.go"); !strings.Contains(got, "//go:embed icons/prod.png") || !strings.Contains(got, "//go:build "+ModeTag("prod")) {
		t.Errorf("icon_prod.go does not embed the icon under prod's tag:\n%s", got)
	}
	if got := read("icon_dev.go"); strings.Contains(got, "go:embed") {
		t.Errorf("icon_dev.go embeds an icon:\n%s", got)
	}
	if got := read("icon_none.go"); !strings.Contains(got, "!"+ModeTag("prod")+" && !"+ModeTag("dev")) && !strings.Contains(got, "!"+ModeTag("dev")+" && !"+ModeTag("prod")) {
		t.Errorf("icon_none.go is not built without a mode:\n%s", got)
	}
	if !strings.Contains(read("main.go"), "Icon:     appIcon") {
		t.Error("main.go does not give the app its icon")
	}

	// Taking the icon away removes the copy.
	p.Config.Modes["prod"] = project.ModeConfig{}
	if _, err := Write(p, testRuntime, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(paths.AppDir, "icons", "prod.png")); err == nil {
		t.Error("icons/prod.png is left after the icon was removed")
	}
}

// testRuntime stands in for the @wailsio/runtime source in the Wails module.
const testRuntime = "/go/pkg/mod/wails/runtime/src"

// Every config that resolves the frontend's imports resolves
// @wailsio/runtime to the Wails module's source, and none to node_modules.
func TestWriteRuntimeFromWails(t *testing.T) {
	root := t.TempDir()
	p := &project.Project{
		Root:        root,
		Config:      project.Config{Name: "App", Window: project.Window{Title: "t", Width: 1, Height: 2}},
		BackendDir:  filepath.Join(root, "backend"),
		FrontendDir: filepath.Join(root, "frontend"),
		DatabaseDir: filepath.Join(root, "database"),
		DesktopDir:  filepath.Join(root, ".desktop"),
		Module:      "example.com/app",
		Backend:     "example.com/app/backend",
	}
	p.Config.Modes, p.Config.DefaultMode = map[string]project.ModeConfig{"prod": {}}, "prod"
	paths, err := Write(p, testRuntime, "")
	if err != nil {
		t.Fatal(err)
	}
	index := `"@wailsio/runtime": "` + testRuntime + `/index.ts"`
	for file, wants := range map[string][]string{
		paths.ViteConfig: {index, `import wails from "` + testRuntime + `/plugins/vite.ts"`,
			`, "` + testRuntime + `", realpathSync(`},
		paths.VitestConfig: {index},
		filepath.Join(p.DesktopDir, "tsconfig.json"): {`"@wailsio/runtime": ["` + testRuntime + `/index.ts"]`,
			`"` + testRuntime + `/global.d.ts"`},
	} {
		raw, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		for _, want := range wants {
			if !strings.Contains(string(raw), want) {
				t.Errorf("%s is missing %s:\n%s", filepath.Base(file), want, raw)
			}
		}
		if strings.Contains(string(raw), `from "@wailsio/runtime`) {
			t.Errorf("%s imports @wailsio/runtime from node_modules:\n%s", filepath.Base(file), raw)
		}
	}
}

// The app, and go test in the project, build with the desktop they are
// given, unless the project replaces desktop itself.
func TestDesktopSource(t *testing.T) {
	src := t.TempDir()
	if err := os.WriteFile(filepath.Join(src, "go.mod"), []byte("module "+DesktopModule+"\n\ngo 1.27\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example.com/app\n\ngo 1.26.0\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	p := &project.Project{Root: root, DesktopDir: filepath.Join(root, ".desktop"), Module: "example.com/app"}

	out, err := appGoMod(p, PathsFor(p), src)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(out), "replace "+DesktopModule+" => "+src) {
		t.Errorf("app go.mod does not use the source:\n%s", out)
	}
	work, err := Workspace(p, src)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(work)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"go 1.27\n", "use ../..\n", "replace " + DesktopModule + " => " + src} {
		if !strings.Contains(string(raw), want) {
			t.Errorf("go.work is missing %q:\n%s", want, raw)
		}
	}

	// A project that replaces desktop keeps its own, and loses the
	// workspace.
	p.Replaces = []*modfile.Replace{{Old: module.Version{Path: DesktopModule}, New: module.Version{Path: "../desktop"}}}
	if out, _ := appGoMod(p, PathsFor(p), src); strings.Contains(string(out), src) {
		t.Errorf("app go.mod overrides the project's replace:\n%s", out)
	}
	if work, err := Workspace(p, src); err != nil || work != "" {
		t.Errorf("Workspace with the project's replace: %q, %v", work, err)
	}
	if _, err := os.Stat(filepath.Join(p.DesktopDir, "work", "go.work")); !os.IsNotExist(err) {
		t.Errorf("go.work was left: %v", err)
	}
}

func TestCopyUIWritesTheSharedFrontendAndRemovesWhatItLacks(t *testing.T) {
	dir := t.TempDir()
	stale := filepath.Join(dir, "components", "Gone.vue")
	if err := os.MkdirAll(filepath.Dir(stale), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(stale, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := copyUI(dir); err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{"style.css", "themes.ts", "components/SidePanel.vue", "components/button/Button.vue", "lib/utils.ts"} {
		if _, err := os.Stat(filepath.Join(dir, filepath.FromSlash(f))); err != nil {
			t.Errorf("%s: %v", f, err)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "ui.go")); !os.IsNotExist(err) {
		t.Errorf("ui.go was copied: %v", err)
	}
	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Errorf("a file framework/ui does not have is still there: %v", err)
	}
}
