package steps

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"strings"
	"sync"

	"golang.org/x/mod/modfile"

	"github.com/michaelckearney/monorepo/framework/internal/generate"
	"github.com/michaelckearney/monorepo/framework/internal/project"
)

var (
	sourceOnce sync.Once
	sourceDir  string
)

// DesktopSource is the folder of the desktop module this desktop was built
// from: the checkout `go install ./cmd/desktop` ran in, or the module cache
// folder Go downloaded it to for `go install .../cmd/desktop@version`. Every
// Go build desktop runs uses the library there, so the app is built against
// the desktop that is building it: not another version, nothing fetched,
// and changes not yet pushed work at once.
//
// It is "" when the source is not on disk any more, or desktop was built
// with -trimpath; the project's go.mod then decides, as Go would.
func DesktopSource() string {
	sourceOnce.Do(func() {
		// The compiler records where each file was when it built desktop.
		_, file, _, ok := runtime.Caller(0)
		if !ok || !filepath.IsAbs(file) {
			return
		}
		dir := filepath.Dir(filepath.Dir(filepath.Dir(file)))
		raw, err := os.ReadFile(filepath.Join(dir, "go.mod"))
		if err != nil || modfile.ModulePath(raw) != generate.DesktopModule {
			return
		}
		sourceDir = dir
		warnIfMoved(dir)
	})
	return sourceDir
}

// warnIfMoved says so when the checkout desktop was installed from has
// moved to another commit since: the app is then built with a library that
// does not match this desktop.
func warnIfMoved(dir string) {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return
	}
	var built string
	for _, s := range info.Settings {
		if s.Key == "vcs.revision" {
			built = s.Value
		}
	}
	if built == "" {
		return
	}
	out, err := exec.Command("git", "-C", dir, "rev-parse", "HEAD").Output()
	if err != nil {
		return
	}
	if head := strings.TrimSpace(string(out)); head != built {
		Logf("this desktop was installed from commit %.7s, but %s is now at %.7s, and the app builds with the library there; to match, run there: go install ./cmd/desktop",
			built, dir, head)
	}
}

// ProjectGoEnv is the environment for a Go command in the project's own
// module, such as go test: GoEnv, and a workspace that puts DesktopSource
// in place of the desktop in go.mod. The workspace is .desktop/work/go.work,
// written here; with no source, or a project that replaces desktop itself,
// there is none, and go.mod decides.
func ProjectGoEnv(p *project.Project) ([]string, error) {
	env := append(os.Environ(), GoEnv()...)
	work, err := generate.Workspace(p, DesktopSource())
	if err != nil || work == "" {
		return env, err
	}
	return append(env, "GOWORK="+work), nil
}
