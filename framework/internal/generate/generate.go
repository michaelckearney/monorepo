// Package generate writes .desktop/: the Wails project that a client
// repository never sees.
//
//	.desktop/
//	  .gitignore        "*", so a client repository needs no entry for it
//	  app/              the main package: go.mod, main.go, dist/,
//	                    migrations/<mode>/, and icons/<mode>.png, with files
//	                    per mode embedding them
//	  bindings/         written by the Wails bindings generator, not here
//	  vite.config.mjs
//	  vitest.config.mjs, vitest.setup.mjs
//	  test-results/     Playwright traces and screenshots
//	  tsconfig.json, env.d.ts
//	  desktop.ts        the frontend's @desktop module: sql, useQuery, onMenu
//	  deps/             links into ~/.desktop/deps, one per dependency
//	  data/<mode>/      the app's data folder under `harness run` and
//	                    `harness cli`
//	  packages/<key>/   the npm installs every checkout's node_modules links
//	                    to, one per package.json and package-lock.json
//	  worktrees/<key>/  the branches the sidecar of `harness run` checks out
//	  cli/<mode>/       the migrations `harness cli` gives the program
//	  tmp/              data folders for tests and recordings
//	  work/go.work      the workspace for Go commands in the project module,
//	                    when desktop builds with its own library
package generate

import (
	"bytes"
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"text/template"

	"golang.org/x/mod/modfile"

	"github.com/michaelckearney/monorepo/framework/internal/migrate"
	"github.com/michaelckearney/monorepo/framework/internal/pin"
	"github.com/michaelckearney/monorepo/framework/internal/project"
	"github.com/michaelckearney/monorepo/framework/ui"
)

//go:embed templates/*.tmpl
var templates embed.FS

var tmpl = template.Must(template.New("").Funcs(template.FuncMap{
	"json": func(v any) (string, error) {
		b, err := json.Marshal(v)
		return string(b), err
	},
}).ParseFS(templates, "templates/*.tmpl"))

// Paths are the generated locations that the run loop also needs.
type Paths struct {
	AppDir      string
	DistDir     string
	BindingsDir string
	ViteConfig  string
	BinDir      string

	VitestConfig   string
	TestResultsDir string

	MigrationsDir string
	// EmbeddedDepsDir is what the app embeds of its dependencies: nothing,
	// except in harness build.
	EmbeddedDepsDir string
	DepsDir         string
	DataDir         string
	// CLIDir holds each mode's migrations for harness cli, rendered as the
	// app embeds them. It is apart from the app's, which harness run and
	// build rewrite while they compile.
	CLIDir string
	TmpDir string
	// UIDir is the shared frontend, framework/ui, which the frontend
	// imports as "@ui".
	UIDir string
}

func PathsFor(p *project.Project) Paths {
	return Paths{
		AppDir:      filepath.Join(p.DesktopDir, "app"),
		DistDir:     filepath.Join(p.DesktopDir, "app", "dist"),
		BindingsDir: filepath.Join(p.DesktopDir, "bindings"),
		ViteConfig:  filepath.Join(p.DesktopDir, "vite.config.mjs"),
		BinDir:      filepath.Join(p.DesktopDir, "bin"),

		VitestConfig:   filepath.Join(p.DesktopDir, "vitest.config.mjs"),
		TestResultsDir: filepath.Join(p.DesktopDir, "test-results"),

		MigrationsDir:   filepath.Join(p.DesktopDir, "app", "migrations"),
		EmbeddedDepsDir: filepath.Join(p.DesktopDir, "app", "deps"),
		DepsDir:         filepath.Join(p.DesktopDir, "deps"),
		DataDir:         filepath.Join(p.DesktopDir, "data"),
		CLIDir:          filepath.Join(p.DesktopDir, "cli"),
		TmpDir:          filepath.Join(p.DesktopDir, "tmp"),
		UIDir:           filepath.Join(p.DesktopDir, "ui"),
	}
}

// Write regenerates every file desktop owns. It leaves bindings/, bin/, and
// app/go.sum alone: the first two are written by later steps, and keeping
// go.sum saves `go mod tidy` from re-verifying every module on each run.
//
// runtime is the folder of the @wailsio/runtime source in the Wails module,
// which the frontend's configs resolve @wailsio/runtime to. desktop is the
// folder of the desktop module to build the app with, in place of the one
// in the project's go.mod, or "" for that one.
func Write(p *project.Project, runtime, desktop string) (Paths, error) {
	paths := PathsFor(p)
	for _, d := range []string{paths.AppDir, paths.DistDir, paths.BinDir, paths.MigrationsDir} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			return paths, err
		}
	}

	backendBindings := filepath.Join(paths.BindingsDir, filepath.FromSlash(p.Backend))
	data := map[string]any{
		"Config":               p.Config,
		"Backend":              p.Backend,
		"FrontendDir":          p.FrontendDir,
		"FrontendSrc":          filepath.Join(p.FrontendDir, "src"),
		"DesktopDir":           p.DesktopDir,
		"NodeModules":          filepath.Join(p.Root, "node_modules"),
		"DistDir":              paths.DistDir,
		"BindingsDir":          paths.BindingsDir,
		"BackendBindings":      backendBindings,
		"BackendBindingsIndex": "./bindings/" + p.Backend + "/index.ts",
		"FrontendSrcGlobTS":    "../frontend/src/**/*.ts",
		"FrontendSrcGlobVue":   "../frontend/src/**/*.vue",
		"UI":                   paths.UIDir,
		"TestResultsDir":       paths.TestResultsDir,
		"DesktopModule":        filepath.Join(p.DesktopDir, "desktop.ts"),
		"Modes":                modes(p),
		"VitestSetup":          filepath.Join(p.DesktopDir, "vitest.setup.mjs"),
		"DefaultMode":          p.Config.DefaultMode,
		"WailsRuntime":         runtime,
		"WailsRuntimeIndex":    filepath.Join(runtime, "index.ts"),
		"WailsRuntimeGlobals":  filepath.Join(runtime, "global.d.ts"),
		"WailsVitePlugin":      filepath.Join(runtime, "plugins", "vite.ts"),
	}

	files := []struct{ tmpl, path string }{
		{"main.go.tmpl", filepath.Join(paths.AppDir, "main.go")},
		{"vite.config.mjs.tmpl", paths.ViteConfig},
		{"vitest.config.mjs.tmpl", paths.VitestConfig},
		{"vitest.setup.mjs.tmpl", filepath.Join(p.DesktopDir, "vitest.setup.mjs")},
		{"tsconfig.json.tmpl", filepath.Join(p.DesktopDir, "tsconfig.json")},
		{"env.d.ts.tmpl", filepath.Join(p.DesktopDir, "env.d.ts")},
		{"desktop.ts.tmpl", filepath.Join(p.DesktopDir, "desktop.ts")},
	}
	for _, f := range files {
		var buf bytes.Buffer
		if err := tmpl.ExecuteTemplate(&buf, f.tmpl, data); err != nil {
			return paths, fmt.Errorf("render %s: %w", f.tmpl, err)
		}
		if err := writeIfChanged(f.path, buf.Bytes()); err != nil {
			return paths, err
		}
	}

	goMod, err := appGoMod(p, paths, desktop)
	if err != nil {
		return paths, err
	}
	if err := writeIfChanged(filepath.Join(paths.AppDir, "go.mod"), goMod); err != nil {
		return paths, err
	}

	// In dev mode the window loads from Vite, but go:embed still refuses to
	// compile a pattern that matches nothing.
	placeholder := filepath.Join(paths.DistDir, "index.html")
	if _, err := os.Stat(placeholder); os.IsNotExist(err) {
		if err := os.WriteFile(placeholder, []byte("<!-- desktop dev placeholder -->\n"), 0o644); err != nil {
			return paths, err
		}
	}

	if err := copyMigrations(p, paths); err != nil {
		return paths, err
	}
	if err := copyUI(paths.UIDir); err != nil {
		return paths, err
	}
	if err := copyIcons(p, paths); err != nil {
		return paths, err
	}
	if err := EmptyEmbeddedDeps(paths); err != nil {
		return paths, err
	}
	// harness build writes a Windows resource file for each Windows build
	// and removes it after; one left by a build that was stopped would put
	// the wrong icon in the next.
	sysos, err := filepath.Glob(filepath.Join(paths.AppDir, "*.syso"))
	if err != nil {
		return paths, err
	}
	for _, f := range sysos {
		if err := os.Remove(f); err != nil {
			return paths, err
		}
	}

	return paths, writeIfChanged(filepath.Join(p.DesktopDir, ".gitignore"), []byte("*\n"))
}

// appGoMod requires the pinned Wails and the project's module, which is
// always the project itself, two folders up, and replaces desktop with the
// folder desktop names, if any.
func appGoMod(p *project.Project, paths Paths, desktop string) ([]byte, error) {
	f := &modfile.File{}
	if err := f.AddModuleStmt("desktop.local/app"); err != nil {
		return nil, err
	}
	if err := f.AddGoStmt("1.26"); err != nil {
		return nil, err
	}
	if err := f.AddRequire("github.com/wailsapp/wails/v3", pin.Wails); err != nil {
		return nil, err
	}
	if err := f.AddRequire(p.Module, "v0.0.0"); err != nil {
		return nil, err
	}
	if err := f.AddReplace(p.Module, "", rel(paths.AppDir, p.Root), ""); err != nil {
		return nil, err
	}
	for _, r := range p.Replaces {
		newPath := r.New.Path
		if modfile.IsDirectoryPath(newPath) && !filepath.IsAbs(newPath) {
			newPath = rel(paths.AppDir, filepath.Join(p.Root, newPath))
		}
		if err := f.AddReplace(r.Old.Path, r.Old.Version, newPath, r.New.Version); err != nil {
			return nil, err
		}
	}
	if useSource(p, desktop) {
		if err := f.AddReplace(DesktopModule, "", desktop, ""); err != nil {
			return nil, err
		}
	}
	f.Cleanup()
	out, err := f.Format()
	if err != nil {
		return nil, err
	}
	return append([]byte("// Code generated by desktop. DO NOT EDIT.\n\n"), out...), nil
}

// DesktopModule is desktop's own module path.
const DesktopModule = "github.com/michaelckearney/monorepo/framework"

// useSource reports whether to build p with the desktop in folder src. Not
// when there is none, nor for desktop itself, nor for a project whose
// go.mod already replaces desktop: that is its owner's choice.
func useSource(p *project.Project, src string) bool {
	if src == "" || p.Module == DesktopModule {
		return false
	}
	for _, r := range p.Replaces {
		if r.Old.Path == DesktopModule {
			return false
		}
	}
	return true
}

// Workspace writes .desktop/work/go.work, which puts the desktop module in
// folder src in place of the one in the project's go.mod, for Go commands
// run in the project's own module, and returns its path. With no desktop
// to use, it removes the file and returns "".
func Workspace(p *project.Project, src string) (string, error) {
	// In a folder of its own: Go looks for a go.work in every folder above
	// where it runs, and .desktop/app, a module of its own, must not be
	// taken into the project's workspace.
	dir := filepath.Join(p.DesktopDir, "work")
	path := filepath.Join(dir, "go.work")
	if !useSource(p, src) {
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return "", err
		}
		return "", nil
	}
	// A workspace's go version must be at least each module's.
	version := "1.26"
	for _, dir := range []string{p.Root, src} {
		raw, err := os.ReadFile(filepath.Join(dir, "go.mod"))
		if err != nil {
			return "", err
		}
		mf, err := modfile.ParseLax("go.mod", raw, nil)
		if err != nil {
			return "", err
		}
		if mf.Go != nil && gover(mf.Go.Version) > gover(version) {
			version = mf.Go.Version
		}
	}
	w := &modfile.WorkFile{Syntax: &modfile.FileSyntax{}}
	if err := w.AddGoStmt(version); err != nil {
		return "", err
	}
	if err := w.AddUse(rel(dir, p.Root), ""); err != nil {
		return "", err
	}
	// A replace, not a second use: Go would still read the go.mod of the
	// desktop version go.mod requires, from the network if it is not cached,
	// when it loads the whole module graph, as go list ./... can.
	if err := w.AddReplace(DesktopModule, "", src, ""); err != nil {
		return "", err
	}
	out := append([]byte("// Code generated by desktop. DO NOT EDIT.\n\n"), modfile.Format(w.Syntax)...)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	return path, writeIfChanged(path, out)
}

// gover makes a go directive's version, such as 1.26 or 1.26.0, comparable
// as a string: each number padded to four digits.
func gover(v string) string {
	var b strings.Builder
	for _, n := range strings.Split(v, ".") {
		b.WriteString(strings.Repeat("0", max(0, 4-len(n))) + n + ".")
	}
	return b.String()
}

// rel returns target relative to base, in the "./x" or "../x" form that
// go.mod requires of a directory replacement.
func rel(base, target string) string {
	r, err := filepath.Rel(base, target)
	if err != nil {
		return target
	}
	r = filepath.ToSlash(r)
	if r[0] != '.' {
		r = "./" + r
	}
	return r
}

func modes(p *project.Project) []project.Mode {
	var out []project.Mode
	for _, name := range p.Config.ModeNames() {
		out = append(out, p.Config.Mode(name))
	}
	return out
}

// copyMigrations checks database/ and writes each mode's migrations,
// rendered, into the app, with a Go file per mode that embeds them under
// that mode's build tag. go:embed cannot reach outside the app's own
// folder, and a tag per mode keeps one mode's fixtures out of another
// mode's binary.
func copyMigrations(p *project.Project, paths Paths) error {
	names := p.Config.ModeNames()
	keepDirs := map[string]bool{}
	for _, mode := range names {
		if err := writeMigrations(p, mode, filepath.Join(paths.MigrationsDir, mode)); err != nil {
			return err
		}
		keepDirs[mode] = true
	}
	if err := removeOthers(paths.MigrationsDir, keepDirs); err != nil {
		return err
	}

	keepFiles := map[string]bool{}
	var tags []string
	for _, mode := range names {
		name := "migrations_" + mode + ".go"
		keepFiles[name] = true
		tags = append(tags, "!"+ModeTag(mode))
		src := fmt.Sprintf(`// Code generated by desktop. DO NOT EDIT.

//go:build %s

package main

import "embed"

//go:embed all:migrations/%s
var migrations embed.FS

const migrationsDir = "migrations/%s"
`, ModeTag(mode), mode, mode)
		if err := writeIfChanged(filepath.Join(paths.AppDir, name), []byte(src)); err != nil {
			return err
		}
	}
	// Built with no mode's tag, as the bindings generator reads it, the app
	// has no migrations, rather than not compiling.
	keepFiles["migrations_none.go"] = true
	none := fmt.Sprintf(`// Code generated by desktop. DO NOT EDIT.

//go:build %s

package main

import "embed"

var migrations embed.FS

const migrationsDir = "."
`, strings.Join(tags, " && "))
	if err := writeIfChanged(filepath.Join(paths.AppDir, "migrations_none.go"), []byte(none)); err != nil {
		return err
	}
	entries, err := filepath.Glob(filepath.Join(paths.AppDir, "migrations_*.go"))
	if err != nil {
		return err
	}
	for _, f := range entries {
		if !keepFiles[filepath.Base(f)] {
			if err := os.Remove(f); err != nil {
				return err
			}
		}
	}
	return nil
}

// writeMigrations writes mode's migrations, rendered, into dir: the
// migrations every mode has and the mode's own, with the list of those
// every mode has, for desktop.NewDB.
func writeMigrations(p *project.Project, mode, dir string) error {
	names := p.Config.ModeNames()
	ms, err := migrate.Collect(os.DirFS(p.DatabaseDir), names, mode)
	if err != nil {
		return &project.Error{Msg: err.Error()}
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	keep := map[string]bool{".keep": true, migrate.SharedFile: true}
	for _, m := range ms {
		keep[m.Name] = true
		if err := writeIfChanged(filepath.Join(dir, m.Name), []byte(m.SQL)); err != nil {
			return err
		}
	}
	// For desktop.NewDB, which a built app runs with its own mode's
	// migrations alone.
	shared, err := migrate.Collect(os.DirFS(p.DatabaseDir), names, "")
	if err != nil {
		return &project.Error{Msg: err.Error()}
	}
	var list strings.Builder
	for _, m := range shared {
		list.WriteString(m.Name + "\n")
	}
	if err := writeIfChanged(filepath.Join(dir, migrate.SharedFile), []byte(list.String())); err != nil {
		return err
	}
	// An embed pattern that matches nothing does not compile, and an app
	// need not have migrations.
	if err := writeIfChanged(filepath.Join(dir, ".keep"), nil); err != nil {
		return err
	}
	return removeOthers(dir, keep)
}

// CLIMigrations writes mode's migrations for harness cli, which reads them
// from disk, and returns their folder. It writes nothing else of
// .desktop/, so a harness run or build compiling at the same time is not
// disturbed.
func CLIMigrations(p *project.Project, mode string) (string, error) {
	dir := filepath.Join(PathsFor(p).CLIDir, mode, "migrations")
	return dir, writeMigrations(p, mode, dir)
}

// copyIcons copies each mode's icon into the app, with a Go file per mode
// that embeds it under the mode's build tag, so a binary carries its own
// mode's icon alone. A mode with no icon embeds none.
func copyIcons(p *project.Project, paths Paths) error {
	dir := filepath.Join(paths.AppDir, "icons")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	names := p.Config.ModeNames()
	keepPNGs := map[string]bool{}
	keepFiles := map[string]bool{"icon_none.go": true}
	var tags []string
	for _, name := range names {
		mode := p.Config.Mode(name)
		file := "icon_" + name + ".go"
		keepFiles[file] = true
		tags = append(tags, "!"+ModeTag(name))
		src := fmt.Sprintf("// Code generated by desktop. DO NOT EDIT.\n\n//go:build %s\n\npackage main\n\n// The mode has no icon.\nvar appIcon []byte\n", ModeTag(name))
		if mode.Icon != "" {
			raw, err := os.ReadFile(mode.Icon)
			if err != nil {
				return err
			}
			keepPNGs[name+".png"] = true
			if err := writeIfChanged(filepath.Join(dir, name+".png"), raw); err != nil {
				return err
			}
			src = fmt.Sprintf("// Code generated by desktop. DO NOT EDIT.\n\n//go:build %s\n\npackage main\n\nimport _ \"embed\"\n\n//go:embed icons/%s.png\nvar appIcon []byte\n", ModeTag(name), name)
		}
		if err := writeIfChanged(filepath.Join(paths.AppDir, file), []byte(src)); err != nil {
			return err
		}
	}
	if err := removeOthers(dir, keepPNGs); err != nil {
		return err
	}
	// Built with no mode's tag, as the bindings generator reads it.
	none := fmt.Sprintf("// Code generated by desktop. DO NOT EDIT.\n\n//go:build %s\n\npackage main\n\nvar appIcon []byte\n", strings.Join(tags, " && "))
	if err := writeIfChanged(filepath.Join(paths.AppDir, "icon_none.go"), []byte(none)); err != nil {
		return err
	}
	entries, err := filepath.Glob(filepath.Join(paths.AppDir, "icon_*.go"))
	if err != nil {
		return err
	}
	for _, f := range entries {
		if !keepFiles[filepath.Base(f)] {
			if err := os.Remove(f); err != nil {
				return err
			}
		}
	}
	return nil
}

// ModeTag is the Go build tag a mode's build has.
func ModeTag(mode string) string { return "desktop_mode_" + mode }

// removeOthers deletes what is in dir and not in keep.
func removeOthers(dir string, keep map[string]bool) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	for _, e := range entries {
		if !keep[e.Name()] {
			if err := os.RemoveAll(filepath.Join(dir, e.Name())); err != nil {
				return err
			}
		}
	}
	return nil
}

// EmptyEmbeddedDeps leaves the app embedding no dependencies. harness build
// fills the folder after this, for each platform it builds; every other
// build finds its dependencies in .desktop/deps, and embedding them would
// only slow it down.
func EmptyEmbeddedDeps(paths Paths) error {
	if err := os.RemoveAll(paths.EmbeddedDepsDir); err != nil {
		return err
	}
	if err := os.MkdirAll(paths.EmbeddedDepsDir, 0o755); err != nil {
		return err
	}
	// An embed pattern that matches nothing does not compile.
	return os.WriteFile(filepath.Join(paths.EmbeddedDepsDir, ".keep"), nil, 0o644)
}

// writeIfChanged leaves a file's mtime alone when its content is the same, so
// Vite and Go see no change on a regeneration that changed nothing.
// copyUI writes the shared frontend, framework/ui, to dir, and removes what
// dir has that it does not. It is a copy, not a link, so its imports of npm
// packages find the project's node_modules, as the frontend's own do.
func copyUI(dir string) error {
	want := map[string]bool{}
	err := fs.WalkDir(ui.Files, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		want[filepath.FromSlash(path)] = true
		data, err := ui.Files.ReadFile(path)
		if err != nil {
			return err
		}
		out := filepath.Join(dir, filepath.FromSlash(path))
		if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
			return err
		}
		return writeIfChanged(out, data)
	})
	if err != nil {
		return err
	}
	return filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		rel, err := filepath.Rel(dir, path)
		if err != nil || want[rel] {
			return err
		}
		return os.Remove(path)
	})
}

func writeIfChanged(path string, content []byte) error {
	if old, err := os.ReadFile(path); err == nil && bytes.Equal(old, content) {
		return nil
	}
	return os.WriteFile(path, content, 0o644)
}
