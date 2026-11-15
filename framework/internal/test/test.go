// Package test implements `harness test`: lint, then unit tests, then e2e
// tests, stopping after the first phase that fails.
//
// Every check in a phase runs even when an earlier one fails, so a Go error
// and a TypeScript error are reported together and fixed in one pass. The
// next phase does not start: there is no point waiting on a browser for an
// app that does not type-check.
package test

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/michaelckearney/monorepo/framework/internal/appstate"
	"github.com/michaelckearney/monorepo/framework/internal/generate"
	"github.com/michaelckearney/monorepo/framework/internal/home"
	"github.com/michaelckearney/monorepo/framework/internal/project"
	"github.com/michaelckearney/monorepo/framework/internal/steps"
)

// Phases selects what to run. With none set, all of them run.
type Phases struct {
	Lint, Unit, E2E bool
	// Run, if set, runs only the tests whose names match it: go test's -run
	// and vitest's -t.
	Run string
	// Paths, if any, run only the tests in these files or folders: a
	// frontend test file for vitest, a folder of Go tests for go test.
	Paths []string
}

func (ph Phases) all() bool { return !ph.Lint && !ph.Unit && !ph.E2E }

// Full reports whether this is the whole of harness test: every phase, and
// every test. A bot's turn records only a full run.
func (ph Phases) Full() bool { return ph.all() && !ph.filtered() }

func (ph Phases) filtered() bool { return ph.Run != "" || len(ph.Paths) > 0 }

// Packages each phase needs in package.json, beyond RunPackages.
var (
	lintPackages = []string{"typescript", "vue-tsc"}
	unitPackages = []string{"vitest", "jsdom"}
)

type phase struct {
	name     string
	selected bool
	run      func(t *tester, ctx context.Context) (note string, ok bool)
}

type tester struct {
	p      *project.Project
	paths  generate.Paths
	phases Phases
	// goPaths and tsPaths are Phases.Paths, as Go package folders and as
	// frontend test files, relative to the project's root and its frontend.
	goPaths, tsPaths []string
	// goDataDir is the data folder Go tests see, emptied for each run of
	// harness test, so desktop.DataDir works in a test without reaching
	// .desktop/data or the real one. It is the same folder each run: go
	// test caches a package's result only while the environment it read is
	// the same, and a new folder each time made every run a full one.
	goDataDir string
	// projectEnv is the environment for Go commands in the project's
	// module. See steps.ProjectGoEnv.
	projectEnv []string
}

// Run runs the selected phases in order and prints a summary. It returns an
// error if any phase failed.
func Run(ctx context.Context, dir string, ph Phases) error {
	if err := steps.CheckTools(); err != nil {
		return err
	}
	root, err := project.Find(dir)
	if err != nil {
		return err
	}
	p, err := project.Load(root)
	if err != nil {
		return err
	}
	t := &tester{p: p, phases: ph}
	if err := t.splitPaths(); err != nil {
		return err
	}
	// Filters with no phase run the tests they pick: the unit tests, and
	// the e2e tests if a path is in tests/.
	if ph.filtered() && ph.all() {
		ph.Unit = true
		for _, d := range t.goPaths {
			if d == "tests" || strings.HasPrefix(d, "tests/") {
				ph.E2E = true
			}
		}
		t.phases = ph
	}
	all := ph.all()
	phases := []phase{
		{"lint", all || ph.Lint, (*tester).lint},
		{"unit", all || ph.Unit, (*tester).unit},
		{"e2e", all || ph.E2E, (*tester).e2e},
	}

	var need []string
	if all || ph.Lint {
		need = append(need, lintPackages...)
	}
	if all || ph.Unit {
		need = append(need, unitPackages...)
	}
	if err := steps.Packages(ctx, p, need...); err != nil {
		return err
	}
	wails, err := steps.Wails3(ctx)
	if err != nil {
		return err
	}
	// Lint type-checks against the bindings, and e2e builds the app, so the
	// generated project has to be current before either.
	if t.paths, err = steps.Prepare(ctx, p, wails); err != nil {
		return err
	}
	if t.projectEnv, err = steps.ProjectGoEnv(p); err != nil {
		return err
	}
	t.goDataDir = filepath.Join(t.paths.TmpDir, "go-test-data")
	if err := os.RemoveAll(t.goDataDir); err != nil {
		return err
	}
	if err := os.MkdirAll(t.goDataDir, 0o755); err != nil {
		return err
	}

	var summary []string
	var failed string
	for _, ph := range phases {
		if !ph.selected {
			continue
		}
		if failed != "" {
			summary = append(summary, fmt.Sprintf("%-5s not run", ph.name))
			continue
		}
		steps.Logf("%s", ph.name)
		note, ok := ph.run(t, ctx)
		if ctx.Err() != nil {
			return steps.Fail("interrupted")
		}
		status := "passed"
		if !ok {
			status = "failed"
			failed = ph.name
		}
		if note != "" {
			status += " (" + note + ")"
		}
		summary = append(summary, fmt.Sprintf("%-5s %s", ph.name, status))
	}

	for _, line := range summary {
		steps.Logf("%s", line)
	}
	if failed != "" {
		return steps.Fail("%s failed; fix the problems above, then run: harness test -%s", failed, failed)
	}
	return nil
}

// check is one tool's verdict within a phase.
type check struct {
	name string
	ok   bool
}

func verdict(checks []check) (string, bool) {
	var bad []string
	for _, c := range checks {
		if !c.ok {
			bad = append(bad, c.name)
		}
	}
	if len(bad) > 0 {
		return strings.Join(bad, ", "), false
	}
	return "", true
}

// mod is the project's Go module: the app in backend/, the e2e tests in
// tests/, and any other package, such as cli/.
type mod struct {
	dir string
	// path is the module path, from go.mod.
	path string
}

func (t *tester) mod() mod { return mod{dir: t.p.Root, path: t.p.Module} }

// hasTests reports whether there is a tests/.
func (t *tester) hasTests() bool {
	_, err := os.Stat(t.p.TestsDir)
	return err == nil
}

func (t *tester) lint(ctx context.Context) (string, bool) {
	m := t.mod()
	return verdict([]check{
		{"gofmt", t.gofmt(ctx, m)},
		{"go vet", t.goTool(ctx, m, "[go vet] ", "vet", "./...")},
		{"vue-tsc", t.vueTSC(ctx)},
	})
}

func (t *tester) unit(ctx context.Context) (string, bool) {
	return verdict([]check{
		{"go test", t.goUnitTests(ctx)},
		{"test layout", t.testLayout()},
		{"vitest", t.vitest(ctx)},
	})
}

// packages lists the module's packages, as go list sees them in a server
// build: go.mod's ignore keeps node_modules out of them. It prints go
// list's own errors and returns false if there are any.
func (t *tester) packages(ctx context.Context, m mod, format string) ([]string, bool) {
	cmd := exec.CommandContext(ctx, "go", "list", "-tags", "server", "-f", format, "./...")
	cmd.Dir = m.dir
	cmd.Env = t.goEnv()
	out, err := cmd.Output()
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			steps.PrintBlock("[go list] ", ee.Stderr)
		}
		return nil, false
	}
	return strings.Fields(string(out)), true
}

// gofmt lists unformatted files and the command that fixes them. It checks
// the files of the module's packages by name: gofmt given a folder walks
// all of it, node_modules included.
func (t *tester) gofmt(ctx context.Context, m mod) bool {
	dirs, ok := t.packages(ctx, m, "{{.Dir}}")
	if !ok {
		return false
	}
	var files []string
	for _, d := range dirs {
		fs, _ := filepath.Glob(filepath.Join(d, "*.go"))
		files = append(files, fs...)
	}
	if len(files) == 0 {
		return true
	}
	cmd := exec.CommandContext(ctx, "gofmt", append([]string{"-l"}, files...)...)
	cmd.Dir = m.dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		steps.PrintBlock("[gofmt] ", out)
		return false
	}
	var bad []string
	for _, f := range strings.Fields(string(out)) {
		rel, _ := filepath.Rel(m.dir, f)
		bad = append(bad, filepath.ToSlash(rel))
	}
	if len(bad) == 0 {
		return true
	}
	for _, f := range bad {
		steps.PrintBlock("[gofmt] ", []byte(f+" is not formatted"))
	}
	steps.PrintBlock("[gofmt] ", []byte("fix with: gofmt -w "+strings.Join(bad, " ")))
	return false
}

// leadingDot matches the "./x.go:3:1" form go vet uses for files in the
// package it was run from.
var leadingDot = regexp.MustCompile(`(?m)^\./`)

// goTool runs go vet or go test over a module, as a server build: the tests
// need no window, and a window build would load GTK and WebKit, which a
// headless machine, the Spark among them, does not have.
func (t *tester) goTool(ctx context.Context, m mod, label string, args ...string) bool {
	args = append([]string{args[0], "-tags", "server"}, args[1:]...)
	cmd := exec.CommandContext(ctx, "go", args...)
	cmd.Dir = m.dir
	cmd.Env = t.goEnv()
	out, err := cmd.CombinedOutput()
	out = leadingDot.ReplaceAll(out, nil)
	if err != nil {
		steps.PrintBlock(label, out)
		if strings.Contains(string(out), "go mod tidy") {
			steps.Logf("go.mod is out of date, often after a desktop upgrade; run: go mod tidy")
		}
		return false
	}
	return true
}

// goEnv is the environment for go vet and go test: a server build, with the
// dependencies and an empty data folder. A Go test that needs a database
// uses desktop.TestDB.
func (t *tester) goEnv() []string {
	env := append([]string{}, t.projectEnv...)
	return append(env,
		appstate.EnvData+"="+t.goDataDir,
		appstate.EnvDeps+"="+t.paths.DepsDir)
}

// testFile matches the indented "x_test.go:12:" that t.Errorf output starts
// with. go test names the file without its directory, which sent an agent
// looking for greeter_test.go at the project root.
var testFile = regexp.MustCompile(`(?m)^(\s+)([\w.-]+\.go:\d+)`)

// goUnitTests runs the Go tests of every package but the e2e tests, which
// need the app running, or of the packages Paths names.
//
// A run that is not the whole of harness test passes -short, so a test that
// is slow, such as one that starts a browser, can skip itself with
// testing.Short() while someone works, and still runs in the full one.
func (t *tester) goUnitTests(ctx context.Context) bool {
	m := t.mod()
	if len(t.phases.Paths) > 0 && len(t.goPaths) == 0 {
		return true
	}
	all, ok := t.packages(ctx, m, "{{.ImportPath}}")
	if !ok {
		return false
	}
	var pkgs []string
	for _, pkg := range all {
		if pkg == m.path+"/tests" || strings.HasPrefix(pkg, m.path+"/tests/") || !t.picked(m, pkg) {
			continue
		}
		pkgs = append(pkgs, pkg)
	}
	if len(pkgs) == 0 {
		if len(t.goPaths) > 0 {
			steps.Logf("no Go packages in %s", strings.Join(t.goPaths, ", "))
			return false
		}
		return true
	}
	var flags []string
	if !t.phases.Full() {
		flags = append(flags, "-short")
	}
	if t.phases.Run != "" {
		flags = append(flags, "-run", t.phases.Run)
	}
	ok, _ = t.goTest(ctx, m, append(flags, pkgs...), t.goEnv())
	return ok
}

// picked reports whether Paths, if any, includes a package.
func (t *tester) picked(m mod, pkg string) bool {
	if len(t.phases.Paths) == 0 {
		return true
	}
	rel := strings.TrimPrefix(strings.TrimPrefix(pkg, m.path), "/")
	for _, d := range t.goPaths {
		if d == "." || rel == d || strings.HasPrefix(rel, d+"/") {
			return true
		}
	}
	return false
}

// splitPaths sorts Paths into frontend test files and Go folders.
func (t *tester) splitPaths() error {
	for _, arg := range t.phases.Paths {
		// The verb gives the paths relative to the project, as it is not
		// where the framework runs.
		abs := filepath.Join(t.p.Root, arg)
		info, err := os.Stat(abs)
		if err != nil {
			return steps.Fail("harness test: %s does not exist", arg)
		}
		rel, err := filepath.Rel(t.p.Root, abs)
		if err != nil || strings.HasPrefix(rel, "..") {
			return steps.Fail("harness test: %s is not in the project", arg)
		}
		rel = filepath.ToSlash(rel)
		front, ferr := filepath.Rel(t.p.FrontendDir, abs)
		inFrontend := ferr == nil && !strings.HasPrefix(front, "..")
		switch {
		case !info.IsDir() && (strings.HasSuffix(rel, ".test.ts") || strings.HasSuffix(rel, ".spec.ts")):
			t.tsPaths = append(t.tsPaths, filepath.ToSlash(front))
		case !info.IsDir() && strings.HasSuffix(rel, ".go"):
			t.goPaths = append(t.goPaths, path.Dir(rel))
		case info.IsDir() && inFrontend && !hasGo(abs):
			t.tsPaths = append(t.tsPaths, filepath.ToSlash(front))
		case info.IsDir():
			t.goPaths = append(t.goPaths, rel)
		default:
			return steps.Fail("harness test: %s is not a test file or a folder of tests", arg)
		}
	}
	return nil
}

func hasGo(dir string) bool {
	files, _ := filepath.Glob(filepath.Join(dir, "*.go"))
	return len(files) > 0
}

// goTest runs go test in a module, as goTool does, with -json so each
// output line carries its package. That is what puts a directory in front of
// the file names in failures. It returns how many tests passed.
func (t *tester) goTest(ctx context.Context, m mod, pkgs, env []string) (bool, int) {
	args := append([]string{"test", "-tags", "server", "-json"}, pkgs...)
	cmd := exec.CommandContext(ctx, "go", args...)
	cmd.Dir = m.dir
	cmd.Env = env
	out, err := cmd.Output()
	var b strings.Builder
	passed := 0
	for _, line := range strings.Split(string(out), "\n") {
		var ev struct {
			Action, Package, Test, Output string
		}
		if json.Unmarshal([]byte(line), &ev) != nil {
			b.WriteString(line + "\n")
			continue
		}
		if ev.Action == "pass" && ev.Test != "" && !strings.Contains(ev.Test, "/") {
			passed++
		}
		if ev.Output == "" || strings.HasPrefix(ev.Output, "=== ") {
			continue
		}
		b.WriteString(testFile.ReplaceAllString(ev.Output, "${1}"+packageDir(m, ev.Package)+"${2}"))
	}
	if err == nil {
		return true, passed
	}
	if ee, ok := err.(*exec.ExitError); ok {
		b.Write(leadingDot.ReplaceAll(ee.Stderr, nil))
	}
	steps.PrintBlock("[go test] ", leadingDot.ReplaceAll([]byte(b.String()), nil))
	return false, passed
}

// packageDir is a package's folder as the user knows it, such as
// tests/dev/.
func packageDir(m mod, pkg string) string {
	if rel := strings.TrimPrefix(pkg, m.path); rel != pkg && rel != "" {
		return strings.TrimPrefix(rel, "/") + "/"
	}
	return ""
}

// vueTSC type-checks the frontend against the generated tsconfig, which is
// what frontend/tsconfig.json extends. Run from the project root, its errors
// name files as frontend/src/....
func (t *tester) vueTSC(ctx context.Context) bool {
	bin := filepath.Join(t.p.Root, "node_modules", ".bin", "vue-tsc")
	cmd := exec.CommandContext(ctx, bin, "--noEmit", "--pretty", "false",
		"-p", filepath.Join(".desktop", "tsconfig.json"))
	cmd.Dir = t.p.Root
	out, err := cmd.CombinedOutput()
	if err != nil {
		steps.PrintBlock("[vue-tsc] ", []byte(strings.ReplaceAll(string(out), "../frontend/", "frontend/")))
		return false
	}
	return true
}

// testLayout fails on a test that would never run, and nothing would say
// so: a frontend test file outside a __tests__ directory, where Vitest does
// not look, or an e2e test outside a mode's folder in tests/.
func (t *tester) testLayout() bool {
	var stray []string
	src := filepath.Join(t.p.FrontendDir, "src")
	_ = filepath.WalkDir(src, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if d.Name() == "node_modules" {
				return filepath.SkipDir
			}
			return nil
		}
		name := d.Name()
		isTest := strings.Contains(name, ".test.") || strings.Contains(name, ".spec.")
		if isTest && filepath.Base(filepath.Dir(path)) != "__tests__" {
			rel, _ := filepath.Rel(t.p.Root, path)
			stray = append(stray, filepath.ToSlash(rel))
		}
		return nil
	})
	for _, f := range stray {
		steps.PrintBlock("[layout] ", []byte(f+" is not in a __tests__ directory, so it would never run; move it into one, named <name>.test.ts"))
	}

	modes := t.p.Config.ModeNames()
	var strayE2E []string
	_ = filepath.WalkDir(t.p.TestsDir, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(d.Name(), "_test.go") {
			return nil
		}
		rel, _ := filepath.Rel(t.p.TestsDir, path)
		first, _, _ := strings.Cut(filepath.ToSlash(rel), "/")
		if !slices.Contains(modes, first) {
			strayE2E = append(strayE2E, "tests/"+filepath.ToSlash(rel))
		}
		return nil
	})
	for _, f := range strayE2E {
		steps.PrintBlock("[layout] ", []byte(fmt.Sprintf("%s is not in a mode's folder, so it would never run; move it into tests/<mode>/, where the modes are %s", f, strings.Join(modes, ", "))))
	}
	return len(stray) == 0 && len(strayE2E) == 0
}

func (t *tester) vitest(ctx context.Context) bool {
	bin := filepath.Join(t.p.Root, "node_modules", ".bin", "vitest")
	if len(t.phases.Paths) > 0 && len(t.tsPaths) == 0 {
		return true
	}
	args := []string{"run", "--config", t.paths.VitestConfig}
	if t.phases.Run != "" {
		args = append(args, "-t", t.phases.Run)
	}
	if len(t.phases.Paths) > 0 {
		// Without it, filters that match no file fail the run.
		args = append(append(args, "--passWithNoTests"), t.tsPaths...)
	}
	cmd := exec.CommandContext(ctx, bin, args...)
	cmd.Dir = t.p.FrontendDir
	cmd.Stdout = steps.NewPrefixWriter("[vitest] ", os.Stdout)
	cmd.Stderr = steps.NewPrefixWriter("[vitest] ", os.Stderr)
	return cmd.Run() == nil
}

// e2e runs each mode's tests, in tests/<mode>/, against the app built in
// that mode, starting from a new data folder. Those tests open WebKit pages
// with package desktop/e2e, which installs the Playwright driver and WebKit
// into ~/.desktop the first time. A mode with no tests is not built.
func (t *tester) e2e(ctx context.Context) (string, bool) {
	var modes []string
	if t.hasTests() {
		for _, name := range t.p.Config.ModeNames() {
			if _, err := os.Stat(filepath.Join(t.p.TestsDir, name)); err == nil {
				modes = append(modes, name)
			}
		}
	}
	if len(t.phases.Paths) > 0 {
		var picked []string
		for _, m := range modes {
			if len(t.e2ePackages(m)) > 0 {
				picked = append(picked, m)
			}
		}
		modes = picked
	}
	if len(modes) == 0 {
		if t.phases.E2E && len(t.phases.Paths) == 0 {
			steps.Logf("no e2e tests; they are Go tests in tests/<mode>/ that use package github.com/michaelckearney/monorepo/framework/e2e")
			return "no tests/<mode>/", false
		}
		return "skipped: no tests/<mode>/", true
	}

	browsers, err := home.BrowsersEnv()
	if err != nil {
		steps.Logf("%v", err)
		return "browsers", false
	}
	driver, err := home.Path("tools", "playwright-go")
	if err != nil {
		steps.Logf("%v", err)
		return "browsers", false
	}
	if err := os.RemoveAll(t.paths.TestResultsDir); err != nil {
		steps.Logf("%v", err)
		return "test results", false
	}

	var counts []string
	total := 0
	for _, name := range modes {
		steps.Logf("e2e: %s", name)
		note, passed, ok := t.e2eMode(ctx, t.p.Config.Mode(name), browsers, driver)
		if !ok {
			if entries, _ := os.ReadDir(t.paths.TestResultsDir); len(entries) > 0 {
				steps.Logf("traces and screenshots of failed tests are in .desktop/test-results; open a trace with: %s %s show-trace <path>/trace.zip",
					filepath.Join(driver, "node"), filepath.Join(driver, "package", "cli.js"))
			}
			return name + ": " + note, false
		}
		total += passed
		counts = append(counts, fmt.Sprintf("%s %d", name, passed))
	}
	return fmt.Sprintf("%d tests: %s", total, strings.Join(counts, ", ")), true
}

// emptyGallery leaves the mode's gallery folder, .desktop/gallery/<mode>/,
// existing and empty. harness test -e2e runs it before the mode's tests, so
// the gallery is one run's at a time: a deleted test leaves no stale picture.
func emptyGallery(dir string) error {
	if err := os.RemoveAll(dir); err != nil {
		return err
	}
	return os.MkdirAll(dir, 0o755)
}

// e2ePackages are the e2e tests of a mode to run: all of tests/<mode>/, or
// the parts of it Paths names.
func (t *tester) e2ePackages(mode string) []string {
	all := "./tests/" + mode + "/..."
	if len(t.phases.Paths) == 0 {
		return []string{all}
	}
	var out []string
	for _, d := range t.goPaths {
		switch {
		case d == "." || d == "tests" || d == "tests/"+mode:
			return []string{all}
		case strings.HasPrefix(d, "tests/"+mode+"/"):
			out = append(out, "./"+d+"/...")
		}
	}
	return out
}

// e2eMode builds the app in mode, starts it, and runs tests/<mode>/...
// against it.
func (t *tester) e2eMode(ctx context.Context, mode project.Mode, browsers []string, driver string) (string, int, bool) {
	// The gallery is the last run's: empty the mode's folder first, so a
	// test that no longer exists leaves no stale picture behind.
	gallery := filepath.Join(t.paths.GalleryDir, mode.Mode)
	if err := emptyGallery(gallery); err != nil {
		steps.Logf("%v", err)
		return "gallery", 0, false
	}
	bin, err := steps.BuildServer(ctx, t.p, t.paths, mode)
	if err != nil {
		steps.Logf("%v", err)
		return "build", 0, false
	}
	port, err := steps.FreePort()
	if err != nil {
		steps.Logf("%v", err)
		return "no port", 0, false
	}
	// A new data folder, so every e2e run starts from the migrations alone,
	// whatever the last run or `harness run` left behind.
	dataDir, err := steps.TempDataDir(t.paths)
	if err != nil {
		steps.Logf("%v", err)
		return "data folder", 0, false
	}
	defer os.RemoveAll(dataDir)
	server := exec.Command(bin)
	server.Dir = t.p.Root
	server.Env = append(os.Environ(), "WAILS_SERVER_HOST=127.0.0.1", "WAILS_SERVER_PORT="+strconv.Itoa(port))
	server.Env = append(server.Env, steps.AppEnv(t.paths, dataDir)...)
	app, err := steps.Start(server, "[app] ")
	if err != nil {
		steps.Logf("could not start the app: %v", err)
		return "app start", 0, false
	}
	defer app.Stop()
	if err := steps.WaitForPort(port, app, "the app", "[app]"); err != nil {
		steps.Logf("%v", err)
		return "app start", 0, false
	}

	env := append(t.goEnv(), browsers...)
	env = append(env,
		"PLAYWRIGHT_DRIVER_PATH="+driver,
		appstate.EnvE2EURL+"=http://127.0.0.1:"+strconv.Itoa(port),
		fmt.Sprintf("%s=%dx%d", appstate.EnvE2EViewport, t.p.Config.Window.Width, t.p.Config.Window.Height),
		appstate.EnvE2EResults+"="+t.paths.TestResultsDir,
		appstate.EnvE2EGallery+"="+gallery,
	)
	// -count=1: go test would otherwise reuse a passing result from an
	// earlier run, against an earlier build of the app.
	args := []string{"-count=1"}
	if t.phases.Run != "" {
		args = append(args, "-run", t.phases.Run)
	}
	ok, passed := t.goTest(ctx, t.mod(), append(args, t.e2ePackages(mode.Mode)...), env)
	if !ok {
		return "go test", passed, false
	}
	return "", passed, true
}
