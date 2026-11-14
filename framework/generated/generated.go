// Package generated is called by the main package desktop generates. A
// backend never imports it.
package generated

import (
	"io/fs"
	"os"

	"github.com/michaelckearney/monorepo/framework/internal/appstate"
)

// Start hands package desktop what only the generated main package has: the
// mode's migrations, at the top of migrations, and the dependencies. It runs
// before the backend's Services, which is the earliest the backend can use
// the database.
func Start(migrations, deps fs.FS) {
	d, err := fs.Sub(deps, "deps")
	if err != nil {
		panic(err)
	}
	appstate.Start(migrations, d)
}

// Title is the window's title: the mode's, then, in `harness run`, what it
// runs, so a branch's window is never taken for the app it was started from.
func Title(title string) string {
	if run := os.Getenv(appstate.EnvRun); run != "" {
		return title + " — " + run
	}
	return title
}

// Mode is the mode desktop built the app in.
func Mode() string { return appstate.Mode }
