// Package project reads a desktop client repository: desktop.yaml, and the
// Go module and npm package at its root.
package project

import (
	"bytes"
	"errors"
	"fmt"
	"image/png"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"golang.org/x/mod/modfile"
	"gopkg.in/yaml.v3"

	"github.com/michaelckearney/monorepo/framework/internal/pin"
)

const ConfigFile = "desktop.yaml"

// Config is desktop.yaml. Unknown fields are an error rather than ignored: a
// misspelt field that silently does nothing is the mistake a model makes and
// never notices.
type Config struct {
	Name string `yaml:"name"`
	// ID identifies the app to the operating system: macOS keys an app's
	// settings and permissions to it. Changing it makes a different app.
	ID string `yaml:"id"`
	// Data is the built app's data folder, such as ~/.harbor. Only the built
	// app uses it: run, test, and record each use a folder of their own, so
	// developing the app never touches its real data.
	Data   string `yaml:"data"`
	Window Window `yaml:"window"`
	// Deps are files the app needs at runtime, by name, then by platform
	// ("darwin/arm64"), to a download URL.
	Deps map[string]map[string]string `yaml:"deps"`
	// Menu is the app's own menus. See Menu.
	Menu Menu `yaml:"menu"`
	// Browser is set for an app that uses package desktop/browser. Desktop
	// then adds the Chromium it pins to Deps, as "chromium".
	Browser bool `yaml:"browser"`
	// Modes are the versions of the app, such as one for users and one with
	// tools for its developers. Each is built, and has its data, its
	// migrations, and its e2e tests, on its own. With none, the app has one
	// mode, prod.
	Modes map[string]ModeConfig `yaml:"modes"`
	// DefaultMode is the mode run and record use without -mode. It may be
	// left out when there is only one.
	DefaultMode string `yaml:"default_mode"`
	// Icon is the app's icon: a square PNG in the repository, at least
	// IconSize pixels on a side. Load makes it an absolute path. With none,
	// the app has the operating system's default icon.
	Icon string `yaml:"icon"`
}

// ModeConfig is what a mode changes. Each field defaults to the top-level
// one. Two modes with the same id would be one app to macOS, sharing its
// settings, so each mode but one sets its own.
type ModeConfig struct {
	Name string `yaml:"name"`
	ID   string `yaml:"id"`
	Icon string `yaml:"icon"`
}

// Mode is one mode, with the defaults filled in.
type Mode struct {
	// Mode is the mode's own name, such as dev: the folder name in
	// database/ and tests/, and what -mode takes.
	Mode string
	// Name is the app's name in this mode, such as Harbor Dev.
	Name  string
	ID    string
	Title string
	// Data is the built app's data folder in this mode: data/<mode> in the
	// app's data folder.
	Data string
	// Icon is the absolute path of the mode's icon, or "" for none.
	Icon string
}

type Window struct {
	Title  string `yaml:"title"`
	Width  int    `yaml:"width"`
	Height int    `yaml:"height"`
}

var unknownField = regexp.MustCompile(`(field \S+) not found in type \S+`)

const knownFields = "name, id, data, icon, window.title, window.width, window.height, deps, menu, browser, modes, default_mode, and modes.<mode>.name, .id, and .icon"

// validID is what both macOS (letters, digits, '-' and '.') and GTK (no
// element starting with a digit, at least two elements) accept.
var validID = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9-]*(\.[A-Za-z][A-Za-z0-9-]*)+$`)

var notSlug = regexp.MustCompile(`[^a-z0-9]+`)

// validMode is a mode's name. It becomes a Go build tag and a folder name,
// so it is kept to what both allow without quoting.
var validMode = regexp.MustCompile(`^[a-z][a-z0-9]*$`)

// reservedMode is a name a mode cannot have: database/assets holds the
// assets every mode's migrations share.
const reservedMode = "assets"

// validDepName keeps a dependency's name usable as a folder name.
var validDepName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)

// Platforms are the platforms harness build builds the app for, in the
// order it builds them, and the keys a dependency has a URL under.
var Platforms = []string{"darwin/arm64", "darwin/amd64", "linux/arm64", "linux/amd64", "windows/arm64", "windows/amd64"}

// Slug is the app's name in lower case with dashes, for file names and the
// default ID.
func Slug(name string) string {
	s := strings.Trim(notSlug.ReplaceAllString(strings.ToLower(name), "-"), "-")
	if s == "" || s[0] >= '0' && s[0] <= '9' {
		s = "app-" + s
	}
	return strings.TrimSuffix(s, "-")
}

// Project is a loaded client repository. All paths are absolute.
//
// The repository is one Go module and one npm package, both at the root, so
// go get and npm install work where they are typed. The folders under it are
// packages of that module: backend/, the app; tests/, its e2e tests; and
// cli/, its command-line program.
type Project struct {
	Root        string
	Config      Config
	BackendDir  string
	FrontendDir string
	// DesktopDir holds everything desktop generates. Nothing in it is edited
	// by hand, and it is rewritten on every run.
	DesktopDir string

	// Module is the module path in go.mod.
	Module string
	// Backend is the backend package's import path, Module/backend. The
	// generated main package imports it, and the bindings land under it.
	Backend string
	// DatabaseDir holds the migrations: those every mode applies at the top,
	// and each mode's own in a folder named after it.
	DatabaseDir string
	// TestsDir holds the e2e tests, each mode's in a folder named after it.
	TestsDir string
	// CLIDir holds the app's command-line program, the main package that
	// harness cli runs. It is optional.
	CLIDir string

	// Replaces are go.mod's replace directives. Go ignores a dependency's
	// replaces, so the generated app module has to restate them, or a
	// project that points at a local checkout would not build.
	Replaces []*modfile.Replace
}

// Error is a problem the user can fix. Its message says what is wrong and
// what to do, on one line.
type Error struct{ Msg string }

func (e *Error) Error() string { return e.Msg }

func fail(format string, args ...any) error {
	return &Error{Msg: fmt.Sprintf(format, args...)}
}

// Find walks up from dir to the directory that contains desktop.yaml. With
// none above, it takes the one folder directly inside dir that has one: a
// repository whose app is in a subfolder, such as desktop's own app/, works
// from its top, where a bot's session starts.
func Find(dir string) (string, error) {
	dir, err := filepath.Abs(dir)
	if err != nil {
		return "", err
	}
	for d := dir; ; d = filepath.Dir(d) {
		if _, err := os.Stat(filepath.Join(d, ConfigFile)); err == nil {
			return d, nil
		}
		if filepath.Dir(d) == d {
			break
		}
	}
	below, _ := filepath.Glob(filepath.Join(dir, "*", ConfigFile))
	switch len(below) {
	case 1:
		return filepath.Dir(below[0]), nil
	case 0:
		return "", fail("no %s in %s, any parent directory, or a folder directly inside it; run desktop from inside a desktop project", ConfigFile, dir)
	default:
		var names []string
		for _, b := range below {
			names = append(names, filepath.Base(filepath.Dir(b)))
		}
		return "", fail("no %s in %s or any parent directory, and more than one folder inside it has one (%s); run desktop from inside the one you mean", ConfigFile, dir, strings.Join(names, ", "))
	}
}

// checkData sets the default data folder, ~/. and the last part of the id,
// and rejects one that is not a folder of its own under the home folder.
func checkData(c *Config) error {
	if c.Data == "" {
		parts := strings.Split(c.ID, ".")
		c.Data = "~/." + parts[len(parts)-1]
	}
	rest, ok := strings.CutPrefix(c.Data, "~/")
	clean := filepath.ToSlash(filepath.Clean(rest))
	switch {
	case !ok:
		return fail("%s: data %q must start with ~/, so the app works for any user, like data: ~/.my-app", ConfigFile, c.Data)
	case rest == "" || clean == "." || clean == ".." || strings.HasPrefix(clean, "../"):
		return fail("%s: data %q must be a folder inside the home folder, like data: ~/.my-app", ConfigFile, c.Data)
	case clean == ".desktop" || strings.HasPrefix(clean, ".desktop/"):
		return fail("%s: data %q is where desktop keeps its own files; choose another folder", ConfigFile, c.Data)
	case strings.ContainsAny(clean, "'\"\n\\"):
		return fail("%s: data %q must not contain quotes or backslashes", ConfigFile, c.Data)
	}
	c.Data = "~/" + clean
	return nil
}

func checkModes(c *Config) error {
	if len(c.Modes) == 0 {
		c.Modes = map[string]ModeConfig{"prod": {}}
	}
	for name, m := range c.Modes {
		switch {
		case !validMode.MatchString(name):
			return fail("%s: mode %q is not valid; use lower-case letters and digits, starting with a letter, like dev", ConfigFile, name)
		case name == reservedMode:
			return fail("%s: a mode cannot be called %s; database/%s holds the assets every mode shares", ConfigFile, name, name)
		case m.ID != "" && !validID.MatchString(m.ID):
			return fail("%s: mode %s: id %q is not valid; use letters, digits, and dashes in dot-separated parts, like com.example.my-app.dev", ConfigFile, name, m.ID)
		}
	}
	if c.DefaultMode == "" {
		if len(c.Modes) > 1 {
			return fail("%s: default_mode is missing; name the mode harness run uses without -mode, one of %s", ConfigFile, strings.Join(c.ModeNames(), ", "))
		}
		for name := range c.Modes {
			c.DefaultMode = name
		}
	}
	if _, ok := c.Modes[c.DefaultMode]; !ok {
		return fail("%s: default_mode %q is not a mode; the modes are %s", ConfigFile, c.DefaultMode, strings.Join(c.ModeNames(), ", "))
	}
	ids, names := map[string]string{}, map[string]string{}
	for _, name := range c.ModeNames() {
		m := c.Mode(name)
		if other, ok := ids[m.ID]; ok {
			return fail("%s: modes %s and %s have the same id, %s, so macOS would take them for one app; give one of them its own id", ConfigFile, other, name, m.ID)
		}
		if other, ok := names[m.Name]; ok {
			return fail("%s: modes %s and %s have the same name, %s, so their builds would replace each other; give one of them its own name", ConfigFile, other, name, m.Name)
		}
		ids[m.ID], names[m.Name] = name, name
	}
	return nil
}

// ModeNames lists the modes, the default first and the rest by name.
func (c Config) ModeNames() []string {
	var names []string
	for name := range c.Modes {
		if name != c.DefaultMode {
			names = append(names, name)
		}
	}
	slices.Sort(names)
	if _, ok := c.Modes[c.DefaultMode]; ok {
		names = append([]string{c.DefaultMode}, names...)
	}
	return names
}

// Mode is the named mode with its defaults filled in. The name must be one
// of the modes; Find checks one the user gave.
func (c Config) Mode(name string) Mode {
	m := c.Modes[name]
	out := Mode{Mode: name, Name: c.Name, ID: c.ID, Title: c.Window.Title, Data: c.Data + "/data/" + name, Icon: c.Icon}
	if m.Name != "" {
		out.Name = m.Name
		// A title left to default follows the mode's name, so the window
		// says which mode it is.
		if c.Window.Title == c.Name {
			out.Title = m.Name
		}
	}
	if m.ID != "" {
		out.ID = m.ID
	}
	if m.Icon != "" {
		out.Icon = m.Icon
	}
	return out
}

// IconSize is the smallest an icon may be on a side: the largest size macOS
// shows, so it is never drawn larger than it is.
const IconSize = 1024

// checkIcon checks the icon at path, as desktop.yaml gives it under field,
// and returns its absolute path. "" is no icon.
func checkIcon(root, field, path string) (string, error) {
	if path == "" {
		return "", nil
	}
	clean := filepath.Clean(filepath.FromSlash(path))
	if filepath.IsAbs(clean) || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return "", fail("%s: %s %q must be a path inside the repository, relative to %s, like icon: assets/icon.png", ConfigFile, field, path, ConfigFile)
	}
	abs := filepath.Join(root, clean)
	f, err := os.Open(abs)
	if err != nil {
		return "", fail("%s: %s %q not found; give the path of a PNG file, relative to %s", ConfigFile, field, path, ConfigFile)
	}
	defer f.Close()
	cfg, err := png.DecodeConfig(f)
	switch {
	case err != nil:
		return "", fail("%s: %s %q is not a PNG file; the icon must be a PNG", ConfigFile, field, path)
	case cfg.Width != cfg.Height:
		return "", fail("%s: %s %q is %dx%d; the icon must be square", ConfigFile, field, path, cfg.Width, cfg.Height)
	case cfg.Width < IconSize:
		return "", fail("%s: %s %q is %dx%d; the icon must be at least %dx%d", ConfigFile, field, path, cfg.Width, cfg.Height, IconSize, IconSize)
	}
	return abs, nil
}

// checkIcons checks every icon and makes each path absolute.
func checkIcons(root string, c *Config) error {
	var err error
	if c.Icon, err = checkIcon(root, "icon", c.Icon); err != nil {
		return err
	}
	for name, m := range c.Modes {
		if m.Icon, err = checkIcon(root, "modes."+name+".icon", m.Icon); err != nil {
			return err
		}
		c.Modes[name] = m
	}
	return nil
}

// FindMode is the mode -mode names, or the default for "".
func (p *Project) FindMode(name string) (Mode, error) {
	if name == "" {
		name = p.Config.DefaultMode
	}
	if _, ok := p.Config.Modes[name]; !ok {
		return Mode{}, fail("there is no mode %q; the modes in %s are %s", name, ConfigFile, strings.Join(p.Config.ModeNames(), ", "))
	}
	return p.Config.Mode(name), nil
}

func checkDeps(deps map[string]map[string]string) error {
	for name, urls := range deps {
		if !validDepName.MatchString(name) {
			return fail("%s: dependency name %q is not valid; use letters, digits, '.', '-', and '_'", ConfigFile, name)
		}
		if len(urls) == 0 {
			return fail("%s: dependency %s has no URLs; add one per platform, like darwin/arm64: https://...", ConfigFile, name)
		}
		for platform, url := range urls {
			if !slices.Contains(Platforms, platform) {
				return fail("%s: dependency %s: %q is not a platform; the platforms are %s", ConfigFile, name, platform, strings.Join(Platforms, ", "))
			}
			if !strings.HasPrefix(url, "https://") {
				return fail("%s: dependency %s, %s: the URL must start with https://", ConfigFile, name, platform)
			}
		}
	}
	return nil
}

// Load reads and checks the project rooted at root.
func Load(root string) (*Project, error) {
	p := &Project{
		Root:        root,
		BackendDir:  filepath.Join(root, "backend"),
		FrontendDir: filepath.Join(root, "frontend"),
		DesktopDir:  filepath.Join(root, ".desktop"),
		DatabaseDir: filepath.Join(root, "database"),
		TestsDir:    filepath.Join(root, "tests"),
		CLIDir:      filepath.Join(root, "cli"),
	}

	raw, err := os.ReadFile(filepath.Join(root, ConfigFile))
	if err != nil {
		return nil, err
	}
	dec := yaml.NewDecoder(bytes.NewReader(raw))
	dec.KnownFields(true)
	if err := dec.Decode(&p.Config); err != nil && !errors.Is(err, io.EOF) {
		// yaml.v3 reports an unknown field as "field nmae not found in type
		// project.Config", which names a Go type the user has never seen.
		var pe *Error
		if errors.As(err, &pe) {
			return nil, pe
		}
		var te *yaml.TypeError
		if errors.As(err, &te) && len(te.Errors) > 0 && strings.Contains(te.Errors[0], "field version not found") {
			return nil, fail("%s: version is not a desktop field any more; remove it: harness build takes the version from the project's git tags and the pull requests merged since (see harness release)", ConfigFile)
		}
		if errors.As(err, &te) && len(te.Errors) > 0 {
			return nil, fail("%s: %s; the fields are %s", ConfigFile,
				unknownField.ReplaceAllString(te.Errors[0], "$1 is not a desktop field"), knownFields)
		}
		return nil, fail("%s: %v", ConfigFile, err)
	}
	if p.Config.Name == "" {
		return nil, fail("%s: name is missing; add a line like `name: My App`", ConfigFile)
	}
	if p.Config.ID == "" {
		p.Config.ID = "local.desktop." + Slug(p.Config.Name)
	} else if !validID.MatchString(p.Config.ID) {
		return nil, fail("%s: id %q is not valid; use letters, digits, and dashes in dot-separated parts, like com.example.my-app", ConfigFile, p.Config.ID)
	}
	if err := checkData(&p.Config); err != nil {
		return nil, err
	}
	if err := checkDeps(p.Config.Deps); err != nil {
		return nil, err
	}
	if p.Config.Browser {
		if _, ok := p.Config.Deps[pin.ChromiumDep]; ok {
			return nil, fail("%s: browser: true brings desktop's own Chromium, as the dependency %s; remove %s from deps", ConfigFile, pin.ChromiumDep, pin.ChromiumDep)
		}
		if p.Config.Deps == nil {
			p.Config.Deps = map[string]map[string]string{}
		}
		urls := map[string]string{}
		for platform, d := range pin.Chromium {
			urls[platform] = d.URL
		}
		p.Config.Deps[pin.ChromiumDep] = urls
	}
	if p.Config.Window.Title == "" {
		p.Config.Window.Title = p.Config.Name
	}
	if err := checkModes(&p.Config); err != nil {
		return nil, err
	}
	if err := checkIcons(root, &p.Config); err != nil {
		return nil, err
	}
	if p.Config.Window.Width == 0 {
		p.Config.Window.Width = 1024
	}
	if p.Config.Window.Height == 0 {
		p.Config.Window.Height = 768
	}

	// Each was its own module or package before both moved to the root. One
	// left behind would be built and installed on its own, out of step with
	// the root.
	for _, f := range []string{"backend/go.mod", "tests/go.mod", "frontend/package.json", "frontend/package-lock.json"} {
		if _, err := os.Stat(filepath.Join(root, f)); err == nil {
			return nil, fail("%s: the project has one Go module and one npm package, at its root; merge %s into ./%s and delete it", f, f, filepath.Base(f))
		}
	}

	raw, err = os.ReadFile(filepath.Join(root, "go.mod"))
	if err != nil {
		return nil, fail("go.mod not found; the project must be a Go module, with go.mod beside desktop.yaml")
	}
	mf, err := modfile.Parse("go.mod", raw, nil)
	if err != nil {
		return nil, fail("go.mod: %v", err)
	}
	if mf.Module == nil {
		return nil, fail("go.mod has no module line")
	}
	// Go does not skip node_modules, and some npm packages ship Go files, so
	// go build ./... would try to compile them.
	if !slices.ContainsFunc(mf.Ignore, func(i *modfile.Ignore) bool { return i.Path == "./node_modules" }) {
		return nil, fail("go.mod: add the line `ignore ./node_modules`, so go build ./... and go test ./... skip the npm packages")
	}
	p.Module = mf.Module.Mod.Path
	p.Backend = p.Module + "/backend"
	p.Replaces = mf.Replace

	if _, err := os.Stat(p.BackendDir); err != nil {
		return nil, fail("backend/ not found; the backend is the Go package in backend/")
	}
	if _, err := os.Stat(filepath.Join(root, "package.json")); err != nil {
		return nil, fail("package.json not found; the project must be an npm package, with package.json beside desktop.yaml")
	}
	if _, err := os.Stat(filepath.Join(p.FrontendDir, "index.html")); err != nil {
		return nil, fail("frontend/index.html not found; Vite starts the app from it")
	}
	return p, nil
}
