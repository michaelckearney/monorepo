// Package steps holds the pieces `harness run` and `harness test` share:
// checking the toolchain and packages, generating .desktop/ and the bindings,
// building the app, and running child processes.
package steps

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/michaelckearney/monorepo/framework/internal/appstate"
	"github.com/michaelckearney/monorepo/framework/internal/deps"
	"github.com/michaelckearney/monorepo/framework/internal/generate"
	"github.com/michaelckearney/monorepo/framework/internal/home"
	"github.com/michaelckearney/monorepo/framework/internal/pin"
	"github.com/michaelckearney/monorepo/framework/internal/project"
)

func init() { deps.Logf = Logf }

// RunPackages must be in package.json for `harness run`. They stay in the
// client's package.json, not in a node_modules of desktop's own: a second
// copy of Vue resolved from somewhere else is a bug that shows up only at
// runtime. @wailsio/runtime is not one of them: desktop builds it from the
// pinned Wails module's source. See WailsRuntime.
var RunPackages = []string{"vue", "vite", "@vitejs/plugin-vue"}

func Fail(format string, args ...any) error {
	return &project.Error{Msg: fmt.Sprintf(format, args...)}
}

func Logf(format string, args ...any) {
	outMu.Lock()
	defer outMu.Unlock()
	fmt.Fprintf(os.Stderr, "desktop: "+format+"\n", args...)
}

func CheckTools() error {
	for _, t := range []struct{ name, hint string }{
		{"go", "install Go 1.26 or later from https://go.dev/dl/"},
		{"node", "install Node.js 20 or later from https://nodejs.org/"},
		{"npm", "npm comes with Node.js; reinstall Node.js"},
	} {
		if _, err := exec.LookPath(t.name); err != nil {
			return Fail("%s is not on PATH; %s", t.name, t.hint)
		}
	}
	return nil
}

// Packages makes the project's npm packages ready: installed, with need and
// RunPackages among them.
func Packages(ctx context.Context, p *project.Project, need ...string) error {
	if err := ensureNodeModules(ctx, p.Root); err != nil {
		return err
	}
	if err := requirePackages(p.Root, append(append([]string{}, RunPackages...), need...)); err != nil {
		return err
	}
	return nil
}

// requirePackages fails, with the install command, if any of names is not
// installed in dir.
func requirePackages(dir string, names []string) error {
	var missing []string
	for _, n := range names {
		if _, err := os.Stat(filepath.Join(dir, "node_modules", n, "package.json")); err != nil {
			missing = append(missing, n)
		}
	}
	if len(missing) == 0 {
		return nil
	}
	return Fail("package.json is missing %s; run: npm install %s",
		strings.Join(missing, ", "), strings.Join(missing, " "))
}

// WailsRuntime returns the folder of the @wailsio/runtime source in the
// pinned Wails module, which the generated Vite, Vitest and TypeScript
// configs point @wailsio/runtime at. Go keeps the module in its module cache,
// and downloads it, as it would to build the app, only if it is not there.
func WailsRuntime(ctx context.Context) (string, error) {
	cmd := exec.CommandContext(ctx, "go", "mod", "download", "-json", "github.com/wailsapp/wails/v3@"+pin.Wails)
	out, err := cmd.Output()
	var mod struct{ Dir, Error string }
	if jerr := json.Unmarshal(out, &mod); jerr != nil || err != nil || mod.Error != "" || mod.Dir == "" {
		if mod.Error != "" {
			PrintBlock("[go] ", []byte(mod.Error))
		} else if ee, ok := err.(*exec.ExitError); ok {
			PrintBlock("[go] ", ee.Stderr)
		}
		return "", Fail("could not get Wails %s, which has the frontend's runtime; check the network, or GOPROXY", pin.Wails)
	}
	dir := filepath.Join(mod.Dir, filepath.FromSlash(pin.WailsRuntimeSource))
	if _, err := os.Stat(filepath.Join(dir, "index.ts")); err != nil {
		return "", Fail("Wails %s has no runtime source at %s; this desktop does not match its Wails pin", pin.Wails, pin.WailsRuntimeSource)
	}
	return dir, nil
}

// Wails3 returns the pinned Wails CLI, installing it into desktop's cache on
// first use. Only its bindings generator is used. It is not run with
// `go run pkg@version` because that relinks it on every call, and bindings
// are regenerated on every backend change.
func Wails3(ctx context.Context) (string, error) {
	bin, err := wailsPath()
	if err != nil {
		return "", err
	}
	if _, err := os.Stat(bin); err == nil {
		return bin, nil
	}
	Logf("installing the Wails %s bindings generator (first run only)", pin.Wails)
	// Without cgo, on the purego backend, as the app is built: with cgo, on
	// Linux, Wails' packages need GTK and WebKit headers, and the release
	// runner has none ("Package gtk4 was not found").
	cmd := exec.CommandContext(ctx, "go", "install", "-tags=purego", "github.com/wailsapp/wails/v3/cmd/wails3@"+pin.Wails)
	cmd.Env = append(append(os.Environ(), GoEnv()...), "GOBIN="+filepath.Dir(bin))
	if out, err := cmd.CombinedOutput(); err != nil {
		os.Stderr.Write(out)
		return "", Fail("could not install the Wails CLI; see the go output above")
	}
	return bin, nil
}

func wailsPath() (string, error) {
	return home.Path("tools", pin.Wails, "wails3")
}

// Prepare regenerates .desktop/ and the bindings, and fetches any missing
// dependencies. Everything that builds, tests, or type-checks the app runs it
// first.
func Prepare(ctx context.Context, p *project.Project, wails string) (generate.Paths, error) {
	runtime, err := WailsRuntime(ctx)
	if err != nil {
		return generate.Paths{}, err
	}
	paths, err := generate.Write(p, runtime, DesktopSource())
	if err != nil {
		return paths, err
	}
	if err := deps.Ensure(ctx, p, paths.DepsDir); err != nil {
		return paths, err
	}
	if err := goModTidy(ctx, paths.AppDir); err != nil {
		return paths, err
	}
	return paths, generateBindings(ctx, wails, paths.AppDir, paths.BindingsDir)
}

func goModTidy(ctx context.Context, appDir string) error {
	cmd := exec.CommandContext(ctx, "go", "mod", "tidy")
	cmd.Dir = appDir
	if out, err := cmd.CombinedOutput(); err != nil {
		PrintBlock("[go] ", RelPaths(out))
		return Fail("go mod tidy failed for the app; check go.mod against the errors above")
	}
	return nil
}

func generateBindings(ctx context.Context, wails, appDir, bindingsDir string) error {
	cmd := exec.CommandContext(ctx, wails, "generate", "bindings",
		"-clean=true", "-ts", "-i", "-silent", "-d", bindingsDir)
	cmd.Dir = appDir
	if out, err := cmd.CombinedOutput(); err != nil {
		PrintBlock("[bindings] ", RelPaths(out))
		if bytes.Contains(out, []byte("Services")) {
			return Fail("could not generate bindings; backend/ must export func Services() []desktop.Service")
		}
		return Fail("could not generate bindings; fix the backend errors above")
	}
	return nil
}

// Build is one way of compiling the generated app.
type Build struct {
	// Mode is the mode the app is built in.
	Mode project.Mode
	Tags []string
	// Server builds the app as an HTTP server instead of a window. The e2e
	// tests drive it from a browser.
	Server bool
	// Release strips debug information and local paths from the binary.
	Release bool
	// Platform is what the app is built for, such as windows/arm64, or ""
	// for this machine.
	Platform string
}

// GoBuild compiles the app to out. A failure prints the compiler's output and
// returns an error that says which contract was broken when it can tell.
func GoBuild(ctx context.Context, p *project.Project, appDir, out string, b Build) error {
	tags := append([]string{generate.ModeTag(b.Mode.Mode)}, b.Tags...)
	if b.Server {
		tags = append([]string{"server"}, tags...)
	} else {
		tags = append([]string{"purego"}, tags...)
	}
	// The data folder and mode are linked in rather than set by the
	// generated main, so they are in place before any of the backend's
	// package initializers run.
	ldflags := fmt.Sprintf("-X '%s.DataDefault=%s' -X '%s.Mode=%s'", appstateImport, b.Mode.Data, appstateImport, b.Mode.Mode)
	if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
		return err
	}
	args := []string{"build", "-buildvcs=false", "-o", out}
	if b.Release {
		args = append(args, "-trimpath")
		ldflags = "-s -w " + ldflags
	}
	goos, goarch, _ := strings.Cut(b.Platform, "/")
	if goos == "windows" && !b.Server {
		// A window app, with no console window of its own.
		ldflags = "-H windowsgui " + ldflags
	}
	args = append(args, "-ldflags="+ldflags)
	if len(tags) > 0 {
		args = append(args, "-tags", strings.Join(tags, ","))
	}
	cmd := exec.CommandContext(ctx, "go", append(args, ".")...)
	cmd.Dir = appDir
	cmd.Env = append(os.Environ(), GoEnv()...)
	if b.Platform != "" {
		cmd.Env = append(cmd.Env, "GOOS="+goos, "GOARCH="+goarch)
	}
	output, err := cmd.CombinedOutput()
	if err == nil {
		return nil
	}
	output = RelPaths(output)
	PrintBlock("[go] ", output)
	switch {
	case bytes.Contains(output, []byte("errno=28")) || bytes.Contains(output, []byte("no space left on device")):
		return Fail("the disk is full; free some space and try again")
	case bytes.Contains(output, []byte("backend.Services")):
		return Fail("backend/ must export func Services() []desktop.Service")
	default:
		return Fail("the backend did not compile; fix the errors above")
	}
}

const appstateImport = "github.com/michaelckearney/monorepo/framework/internal/appstate"

// AppEnv is the environment for an app desktop starts: where its data and
// dependencies are.
func AppEnv(paths generate.Paths, dataDir string) []string {
	return []string{
		appstate.EnvData + "=" + dataDir,
		appstate.EnvDeps + "=" + paths.DepsDir,
	}
}

// TempDataDir is a new, empty data folder for one test run or recording, in
// .desktop/tmp. The caller removes it.
func TempDataDir(paths generate.Paths) (string, error) {
	if err := os.MkdirAll(paths.TmpDir, 0o755); err != nil {
		return "", err
	}
	return os.MkdirTemp(paths.TmpDir, "data-")
}

// GoEnv is the environment for compiling against Wails. Nothing is built
// with cgo: a window build uses Wails' purego backend, which loads the
// system's UI libraries when the app starts, and a server build has no
// window. So the same source makes the same app on any machine, for any
// platform, with no C toolchain, Xcode, or GTK headers.
func GoEnv() []string {
	return []string{"CGO_ENABLED=0"}
}

// RelPaths rewrites the file names the Go toolchain prints relative to
// .desktop/app so they are relative to the project root, where the user and
// their editor are. An agent told "../../backend/x.go" opened a path that
// does not exist from there.
func RelPaths(out []byte) []byte {
	return upTwo.ReplaceAll(out, []byte("$1"))
}

// upTwo is "../../" at the start of a file name: the project root, from
// .desktop/app.
var upTwo = regexp.MustCompile(`(?m)(^|[\s(])\.\./\.\./`)

func FreePort() (int, error) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port, nil
}

// WaitForPort returns once something accepts connections on port, or fails
// if p exits first or a minute passes. what and label name the process in
// the error, such as "the Vite dev server" and "[vite]".
func WaitForPort(port int, p *Proc, what, label string) error {
	deadline := time.Now().Add(time.Minute)
	for time.Now().Before(deadline) {
		if p.Exited() {
			return Fail("%s exited before it started; see the %s lines above", what, label)
		}
		c, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", port), 200*time.Millisecond)
		if err == nil {
			c.Close()
			return nil
		}
		time.Sleep(100 * time.Millisecond)
	}
	return Fail("%s did not start within a minute; see the %s lines above", what, label)
}

func PrintBlock(prefix string, out []byte) {
	w := NewPrefixWriter(prefix, os.Stderr)
	_, _ = w.Write(out)
	if len(out) > 0 && out[len(out)-1] != '\n' {
		_, _ = w.Write([]byte("\n"))
	}
}

// ViteBuild builds the frontend for production in mode into
// .desktop/app/dist, where the Go build embeds it from. The mode is Vite's
// own, so import.meta.env.MODE is the mode, and code for another mode is
// left out of the bundle.
func ViteBuild(ctx context.Context, p *project.Project, paths generate.Paths, mode project.Mode) error {
	cmd := exec.CommandContext(ctx, filepath.Join(p.Root, "node_modules", ".bin", "vite"),
		"build", "--config", paths.ViteConfig, "--mode", mode.Mode, "--logLevel", "warn")
	cmd.Dir = p.FrontendDir
	if out, err := cmd.CombinedOutput(); err != nil {
		PrintBlock("[vite] ", out)
		return Fail("the frontend did not build; fix the errors above")
	}
	return nil
}

// BuildServer builds the frontend and a production server build of the app
// in mode, the form a browser can drive, and returns the binary's path. The
// e2e tests and recordings run this build.
func BuildServer(ctx context.Context, p *project.Project, paths generate.Paths, mode project.Mode) (string, error) {
	Logf("building the frontend (%s)", mode.Mode)
	if err := ViteBuild(ctx, p, paths, mode); err != nil {
		return "", err
	}
	Logf("building the backend (%s)", mode.Mode)
	bin := AppBinary(paths, mode, "-server")
	if err := GoBuild(ctx, p, paths.AppDir, bin, Build{Mode: mode, Server: true, Tags: []string{"production"}}); err != nil {
		return "", err
	}
	return bin, nil
}

// AppBinary is where an app build in mode goes. It is named after the app
// because macOS puts an unbundled binary's file name in the menu bar: the
// first demo showed "app" there. Each mode has its own folder, so a test of
// one mode does not replace the binary a run of another is using.
func AppBinary(paths generate.Paths, mode project.Mode, suffix string) string {
	return filepath.Join(paths.BinDir, mode.Mode, strings.ReplaceAll(mode.Name, "/", "-")+suffix)
}
