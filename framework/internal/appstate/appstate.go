// Package appstate is what the CLI tells a running app, and what the
// generated main package tells package desktop, about where things are.
//
// The CLI imports it for the environment variable names. It imports nothing
// that would link Wails or a SQLite driver into the CLI.
package appstate

import (
	"io/fs"
	"os"
	"sync"
)

// The CLI sets these for the app it starts. The built app has none of them,
// and falls back to its configured data folder and its embedded dependencies.
const (
	// EnvData is the data folder: .desktop/data/<mode> for `harness run`, a
	// new temporary folder for each test or recording.
	EnvData = "DESKTOP_DATA_DIR"
	// EnvDeps is the folder of dependencies, .desktop/deps.
	EnvDeps = "DESKTOP_DEPS_DIR"
	// EnvMigrations is the folder of the mode's migrations, rendered, for
	// the program `harness cli` runs. It has no generated main to embed
	// them.
	EnvMigrations = "DESKTOP_MIGRATIONS_DIR"
	// EnvRun is what `harness run` runs, for the window's title: the pull
	// request, such as "#18 Fix the sidebar · harness/17", or the branch.
	EnvRun = "DESKTOP_RUN"
)

// The environment harness test sets for the e2e tests, which package
// desktop/e2e reads.
const (
	// EnvE2EURL is the running app's address.
	EnvE2EURL = "DESKTOP_E2E_URL"
	// EnvE2EViewport is the window size from desktop.yaml, as 1280x820.
	EnvE2EViewport = "DESKTOP_E2E_VIEWPORT"
	// EnvE2EResults is where a failed test's trace and screenshot go.
	EnvE2EResults = "DESKTOP_E2E_RESULTS"
	// EnvE2EGallery is the mode's gallery folder, .desktop/gallery/<mode>:
	// every test that opens the app leaves a screenshot of its page there.
	EnvE2EGallery = "DESKTOP_E2E_GALLERY"
)

// DBFile is the database's file name in the data folder.
const DBFile = "app.db"

// The CLI sets these when it links the app, with -ldflags -X, so they are in
// place before any package's init runs.
var (
	// DataDefault is the built app's data folder in its mode, such as
	// ~/.harbor/data/prod.
	DataDefault string
	// Mode is the mode the app was built in, such as dev.
	Mode string
)

var (
	mu         sync.Mutex
	started    bool
	migrations fs.FS
	deps       fs.FS
)

// Start records that the generated main has begun, with the migrations and
// dependencies it embeds.
func Start(m, d fs.FS) {
	mu.Lock()
	defer mu.Unlock()
	started, migrations, deps = true, m, d
}

// EmbeddedDeps returns the dependencies the app embeds: for each, a folder
// named after it holding one file, <sha256>/<file name from its URL>. It is
// empty except in a built app, and nil before Start.
func EmbeddedDeps() fs.FS {
	mu.Lock()
	defer mu.Unlock()
	return deps
}

// Migrations returns the migrations: those the app embeds, or, in the
// program harness cli runs, those in EnvMigrations. It returns false in an
// app before Start.
func Migrations() (fs.FS, bool) {
	mu.Lock()
	defer mu.Unlock()
	if !started {
		if dir := os.Getenv(EnvMigrations); dir != "" {
			return os.DirFS(dir), true
		}
	}
	return migrations, started
}
