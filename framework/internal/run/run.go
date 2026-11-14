// Package run implements `harness run`: generate .desktop/, start the Vite dev
// server, build and start the app, and rebuild it when the backend changes.
// In a git repository it also serves a control API on 127.0.0.1, named in
// .desktop/run.json, through which the Desktop app lists the branches and
// pull requests and runs the one the user picks in their place.
//
// Frontend changes need nothing from here: the window loads from Vite, and
// Vite's hot reload handles them. Backend changes regenerate the bindings,
// rebuild, and restart the app. A rebuild that fails leaves the running app
// alone, so a typo in Go does not close the window.
package run

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/fsnotify/fsnotify"

	"github.com/michaelckearney/monorepo/framework/internal/appstate"
	"github.com/michaelckearney/monorepo/framework/internal/branches"
	"github.com/michaelckearney/monorepo/framework/internal/generate"
	"github.com/michaelckearney/monorepo/framework/internal/project"
	"github.com/michaelckearney/monorepo/framework/internal/steps"
)

// debounce collapses the burst of events an editor's save produces, and the
// burst a `git checkout` produces, into one rebuild.
const debounce = 300 * time.Millisecond

// checkout is one checkout of the project that the run can show: the
// user's own, or a branch's worktree. Each has its own .desktop/, so its
// own build, bindings, and data folder.
type checkout struct {
	// key names the branch to the app; it is "" for the user's own
	// checkout.
	key  string
	name string
	// label is what the checkout runs, for the window's title: its pull
	// request, else its branch.
	label   string
	root    string
	p       *project.Project
	mode    project.Mode
	paths   generate.Paths
	appBin  string
	vite    *steps.Proc
	viteURL string
}

type runner struct {
	// home is the project in the user's own checkout, where the run began.
	home     *project.Project
	modeName string
	wails    string
	cur      *checkout
	app      *steps.Proc
	appCount int
	// side is the control API, or nil when the project is not in git.
	side *side
}

// Run blocks until the user closes the app or interrupts desktop. mode is
// the mode to run in, or "" for the default.
func Run(ctx context.Context, dir, mode string) error {
	if err := steps.CheckTools(); err != nil {
		return err
	}
	root, err := project.Find(dir)
	if err != nil {
		return err
	}
	r := &runner{}
	if r.home, err = project.Load(root); err != nil {
		return err
	}
	m, err := r.home.FindMode(mode)
	if err != nil {
		return err
	}
	r.modeName = m.Mode
	if err := steps.Packages(ctx, r.home); err != nil {
		return err
	}
	if r.wails, err = steps.Wails3(ctx); err != nil {
		return err
	}

	// The first build has to succeed: with no app yet, there is nothing to
	// keep running while the user fixes the error.
	if r.cur, err = r.open(ctx, "", "", r.home, m); err != nil {
		return err
	}
	if out, err := exec.Command("git", "-C", root, "branch", "--show-current").Output(); err == nil {
		r.cur.label = strings.TrimSpace(string(out))
	}
	defer func() { r.cur.vite.Stop() }()

	if err := r.startApp(); err != nil {
		return err
	}
	defer func() { r.app.Stop() }()

	if repo, err := branches.Open(root); err == nil {
		r.side = newSide(r, repo)
		defer r.side.stop()
		go r.side.start(ctx)
	}

	return r.watch(ctx)
}

// open prepares and builds the checkout of p, and starts Vite in it. Nothing
// it does touches the app that is running, so a switch that fails leaves the
// user where they were.
func (r *runner) open(ctx context.Context, key, name string, p *project.Project, mode project.Mode) (*checkout, error) {
	c := &checkout{key: key, name: name, root: p.Root, p: p, mode: mode}
	var err error
	if c.paths, err = steps.Prepare(ctx, p, r.wails); err != nil {
		return nil, err
	}
	c.appBin = steps.AppBinary(c.paths, mode, "")
	steps.Logf("building the backend (%s)", mode.Mode)
	if err := steps.GoBuild(ctx, p, c.paths.AppDir, c.appBin, steps.Build{Mode: mode}); err != nil {
		return nil, err
	}

	port, err := steps.FreePort()
	if err != nil {
		return nil, err
	}
	vite := exec.Command(filepath.Join(p.Root, "node_modules", ".bin", "vite"), "--config", c.paths.ViteConfig, "--mode", mode.Mode)
	vite.Dir = p.FrontendDir
	vite.Env = append(os.Environ(), "DESKTOP_VITE_PORT="+strconv.Itoa(port))
	if c.vite, err = steps.Start(vite, "[vite] "); err != nil {
		return nil, steps.Fail("could not start Vite: %v", err)
	}
	if err := steps.WaitForPort(port, c.vite, "the Vite dev server", "[vite]"); err != nil {
		c.vite.Stop()
		return nil, err
	}
	c.viteURL = fmt.Sprintf("http://127.0.0.1:%d", port)
	return c, nil
}

// dataDir is the app's data folder in the checkout c. A branch's starts
// empty, the first time the branch runs, so its database has exactly the
// branch's migrations, and the user's own data never sees them. It is kept
// between switches.
func (r *runner) dataDir(c *checkout) string {
	return filepath.Join(c.paths.DataDir, c.mode.Mode)
}

func (r *runner) startApp() error {
	r.appCount++
	c := r.cur
	cmd := exec.Command(c.appBin)
	cmd.Dir = c.root
	// .desktop/data/<mode> persists between restarts, so the app behaves as
	// it would for a user, without touching the user's real data folder.
	cmd.Env = append(os.Environ(), "FRONTEND_DEVSERVER_URL="+c.viteURL)
	cmd.Env = append(cmd.Env, steps.AppEnv(c.paths, r.dataDir(c))...)
	if c.label != "" {
		cmd.Env = append(cmd.Env, appstate.EnvRun+"="+c.label)
	}
	app, err := steps.Start(cmd, "[app] ", appNoise...)
	if err != nil {
		return steps.Fail("could not start the app: %v", err)
	}
	r.app = app
	if r.side != nil {
		r.side.started()
	}
	switch {
	case r.appCount == 1:
		steps.Logf("running %s in %s mode; edit frontend/, backend/, or database/ and the app updates. Ctrl-C to stop.", c.mode.Name, c.mode.Mode)
	case c.key != "":
		steps.Logf("running branch %s", c.name)
	default:
		steps.Logf("restarted the app")
	}
	return nil
}

// rebuild runs after a backend change. The new binary is built beside the old
// one and swapped in only once it compiles.
func (r *runner) rebuild(ctx context.Context, configChanged bool) {
	c := r.cur
	if configChanged {
		p, err := project.Load(c.root)
		if err != nil {
			steps.Logf("%v", err)
			return
		}
		// The run keeps its mode, even if the default changed: the Vite
		// server was started in it.
		mode, err := p.FindMode(r.modeName)
		if err != nil {
			steps.Logf("%v; restart harness run", err)
			return
		}
		c.p, c.mode = p, mode
		if c.key == "" {
			r.home = p
		}
	}
	steps.Logf("backend changed; rebuilding")
	var err error
	if c.paths, err = steps.Prepare(ctx, c.p, r.wails); err != nil {
		steps.Logf("%v", err)
		return
	}
	next := c.appBin + ".next"
	if err := steps.GoBuild(ctx, c.p, c.paths.AppDir, next, steps.Build{Mode: c.mode}); err != nil {
		steps.Logf("%v; the running app is unchanged, and desktop rebuilds on the next save", err)
		return
	}
	r.app.Stop()
	if err := os.Rename(next, c.appBin); err != nil {
		steps.Logf("%v", err)
		return
	}
	if err := r.startApp(); err != nil {
		steps.Logf("%v", err)
	}
}

// switchTo runs the branch b in place of what is running. Everything is
// built before the running app stops, so a branch that does not build
// leaves it running.
func (r *runner) switchTo(ctx context.Context, b branches.Branch) error {
	root := r.home.Root
	var err error
	if b.Key != "" {
		r.side.progress("checking out %s", b.Name)
		if root, err = r.side.repo.Checkout(ctx, b); err != nil {
			return err
		}
	}
	p, err := project.Load(root)
	if err != nil {
		return err
	}
	mode, err := p.FindMode(r.modeName)
	if err != nil {
		return fmt.Errorf("%v; this run is in %s mode", err, r.modeName)
	}
	r.side.progress("installing packages")
	if err := steps.Packages(ctx, p); err != nil {
		return err
	}
	r.side.progress("building")
	c, err := r.open(ctx, b.Key, b.Name, p, mode)
	if err != nil {
		return err
	}
	c.label = label(b)

	r.app.Stop()
	r.cur.vite.Stop()
	r.cur = c
	if c.key == "" {
		r.home = p
	}
	return r.startApp()
}

// label is what a branch is called in its window's title: its pull request
// and the branch, else the branch.
func label(b branches.Branch) string {
	if b.PR != nil {
		return fmt.Sprintf("#%d %s · %s", b.PR.Number, b.PR.Title, b.Name)
	}
	return b.Name
}

// resetData empties the running branch's data, so it starts again with
// only the branch's migrations.
func (r *runner) resetData() error {
	if r.cur.key == "" {
		return fmt.Errorf("your own checkout's data is not reset from here")
	}
	r.app.Stop()
	if err := os.RemoveAll(r.dataDir(r.cur)); err != nil {
		return err
	}
	steps.Logf("reset the data of %s", r.cur.name)
	return r.startApp()
}

func (r *runner) watch(ctx context.Context) error {
	w, err := r.watcher()
	if err != nil {
		return err
	}
	defer func() { w.Close() }()

	var reqs chan request
	if r.side != nil {
		reqs = r.side.reqs
	}
	var (
		timer         <-chan time.Time
		configChanged bool
	)
	for {
		// A fresh app is watched each time round, since rebuild replaces it.
		appDone := r.app.Done
		select {
		case <-ctx.Done():
			steps.Logf("stopping")
			return nil

		case <-r.cur.vite.Done:
			return steps.Fail("the Vite dev server stopped; see the [vite] lines above")

		case <-appDone:
			// Exit status 0 is the user closing the window: that ends the run.
			// Anything else is a crash, and the next backend change may fix it.
			if r.app.Err == nil {
				steps.Logf("the app was closed")
				return nil
			}
			exit := r.app.Err
			steps.Logf("the app exited (%v); desktop will restart it when the backend changes", exit)
			// Stop selecting on a channel that is already closed.
			r.app = steps.Gone()
			if r.side != nil {
				r.side.stopped(exit)
			}

		case req := <-reqs:
			before := r.cur
			switch req.kind {
			case "switch":
				err = r.switchTo(ctx, req.branch)
			case "reset":
				err = r.resetData()
			}
			if err != nil {
				steps.Logf("%v", err)
			}
			r.side.done(err)
			if r.cur != before {
				// The new checkout is the one edits now come from.
				w.Close()
				if w, err = r.watcher(); err != nil {
					return err
				}
				timer, configChanged = nil, false
			}

		case ev, ok := <-w.Events:
			if !ok {
				return nil
			}
			if ev.Has(fsnotify.Create) {
				if fi, err := os.Stat(ev.Name); err == nil && fi.IsDir() {
					_ = r.addTree(w, ev.Name)
				}
			}
			switch {
			// A file's metadata changing is not an edit. macOS changes it
			// unasked, Spotlight updating an extended attribute after a save,
			// and each such event reloaded the app with no code changed.
			case ev.Op == fsnotify.Chmod:
				continue
			case filepath.Join(r.cur.root, project.ConfigFile) == ev.Name:
				configChanged = true
			case inside(r.cur.p.DatabaseDir, ev.Name):
				// Every file in database/ is a migration or one a migration
				// includes, but for what Finder and editors leave beside them.
				if scratch(ev.Name) {
					continue
				}
			case !relevant(ev.Name):
				continue
			}
			timer = time.After(debounce)

		case err, ok := <-w.Errors:
			if !ok {
				return nil
			}
			steps.Logf("watch: %v", err)

		case <-timer:
			timer = nil
			r.rebuild(ctx, configChanged)
			configChanged = false
		}
	}
}

// watcher watches the whole module of the running checkout but the folders
// that cannot change the app: the backend may import any package in it.
func (r *runner) watcher() (*fsnotify.Watcher, error) {
	w, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}
	if err := r.addTree(w, r.cur.root); err != nil {
		w.Close()
		return nil, err
	}
	return w, nil
}

// appNoise are lines Wails prints in dev mode that are wrong under desktop.
// Wails assumes its own watcher started the app, so every restart told the
// user to press Ctrl-C to stop a watcher that is not there.
var appNoise = []string{
	"but the watcher is still running",
	"To terminate the watcher",
}

// relevant reports whether a change to path, in a watched folder, can
// change the app. Tests cannot, nor can the files go build ignores, as an
// editor's .#main.go lock file.
func relevant(path string) bool {
	base := filepath.Base(path)
	switch {
	case base == "go.mod", base == "go.sum":
		return true
	case strings.HasSuffix(base, "_test.go"), strings.HasPrefix(base, "."), strings.HasPrefix(base, "_"):
		return false
	default:
		return strings.HasSuffix(base, ".go")
	}
}

// scratch reports whether path is a file no one wrote to be part of the
// app: hidden, as Finder's .DS_Store and vim's swap files are, or an
// editor's backup (name~) or autosave (#name#).
func scratch(path string) bool {
	base := filepath.Base(path)
	return strings.HasPrefix(base, ".") || strings.HasSuffix(base, "~") ||
		(len(base) > 1 && strings.HasPrefix(base, "#") && strings.HasSuffix(base, "#"))
}

func inside(dir, path string) bool {
	rel, err := filepath.Rel(dir, path)
	return err == nil && !strings.HasPrefix(rel, "..")
}

// addTree watches dir and every directory under it but those that cannot
// change the app. fsnotify does not recurse.
func (r *runner) addTree(w *fsnotify.Watcher, dir string) error {
	return filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil || !d.IsDir() {
			return nil
		}
		if path != r.cur.root && r.skip(path) {
			return filepath.SkipDir
		}
		return w.Add(path)
	})
}

// skip reports whether a folder cannot change the app: the frontend, which
// Vite watches; the e2e tests and the command-line program, which the app
// does not import; and the npm packages, desktop's own files, and build/.
func (r *runner) skip(dir string) bool {
	name := filepath.Base(dir)
	if strings.HasPrefix(name, ".") || name == "node_modules" || name == "vendor" || name == "testdata" {
		return true
	}
	p := r.cur.p
	for _, d := range []string{p.FrontendDir, p.TestsDir, p.CLIDir, filepath.Join(p.Root, "build")} {
		if dir == d {
			return true
		}
	}
	return false
}
