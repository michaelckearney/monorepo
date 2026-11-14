package desktop

import (
	"database/sql"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	// A pure Go driver: the server builds that tests and recordings run are
	// compiled without cgo.
	_ "modernc.org/sqlite"

	"github.com/michaelckearney/monorepo/framework/internal/appstate"
	"github.com/michaelckearney/monorepo/framework/internal/migrate"
	"github.com/michaelckearney/monorepo/framework/internal/unpack"
)

// DataDir is the app's data folder. It is created on first use.
//
// Where it is depends on how the app was started:
//
//   - harness run: .desktop/data/<mode> in the project, kept between restarts
//   - harness cli: the same folder as harness run, so the program and the
//     running app share one database
//   - harness test and harness record: a new, empty folder for each run
//   - the built app: data/<mode> in data from desktop.yaml, such as
//     ~/.harbor/data/prod
//
// It panics outside these, such as in a plain `go test`; a Go test should
// use t.TempDir or TestDB.
func DataDir() string {
	dir, err := dataDir()
	if err != nil {
		panic("desktop.DataDir: " + err.Error())
	}
	return dir
}

var dataDir = sync.OnceValues(func() (string, error) {
	dir := os.Getenv(appstate.EnvData)
	if dir == "" && appstate.DataDefault != "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		dir = filepath.Join(home, strings.TrimPrefix(appstate.DataDefault, "~/"))
	}
	if dir == "" {
		return "", errors.New("no data folder; start the app with harness run, test, or record, or build it with harness build, and start cli/ with harness cli. In a Go test, use t.TempDir() or desktop.TestDB(t)")
	}
	return dir, os.MkdirAll(dir, 0o700)
})

// Dep is the folder that holds the dependency name from desktop.yaml, with
// its download unpacked in it. A download that is not a zip, tar.gz, or tgz
// is one file in the folder, named after the dependency: Dep("jq") + "/jq".
//
// Under harness run, test, and record it is in desktop's shared cache. A
// built app embeds each download, and its first Dep call for a dependency
// unpacks it into DataDir, in deps/<name>-<hash>; later calls, and later
// launches, reuse that folder.
//
// It panics if there is no dependency by that name, or a built app cannot
// unpack it.
func Dep(name string) string {
	if d := os.Getenv(appstate.EnvDeps); d != "" {
		path := filepath.Join(d, name)
		resolved, err := filepath.EvalSymlinks(path)
		if err != nil {
			panic(fmt.Sprintf("desktop.Dep(%q): there is no dependency %s in %s; add it under deps in desktop.yaml", name, name, d))
		}
		return resolved
	}
	embedded := appstate.EmbeddedDeps()
	if embedded == nil {
		panic(fmt.Sprintf("desktop.Dep(%q): no dependencies; start the app with harness run, test, or record, or build it with harness build, and start cli/ with harness cli", name))
	}
	data, err := dataDir()
	if err != nil {
		panic(fmt.Sprintf("desktop.Dep(%q): %v", name, err))
	}
	dir, err := unpackedDep(embedded, filepath.Join(data, "deps"), name)
	if err != nil {
		panic(fmt.Sprintf("desktop.Dep(%q): %v", name, err))
	}
	return dir
}

// unpacking serializes first calls, so two goroutines asking for the same
// dependency do not both unpack it.
var unpacking sync.Mutex

func unpackedDep(embedded fs.FS, root, name string) (string, error) {
	sums, err := fs.ReadDir(embedded, name)
	if err != nil || len(sums) != 1 {
		return "", fmt.Errorf("the app does not embed a dependency %s; add it under deps in desktop.yaml", name)
	}
	sum := sums[0].Name()
	files, err := fs.ReadDir(embedded, path.Join(name, sum))
	if err != nil || len(files) != 1 {
		return "", fmt.Errorf("the app's copy of %s is damaged; build it again", name)
	}
	final := filepath.Join(root, name+"-"+sum[:16])

	unpacking.Lock()
	defer unpacking.Unlock()
	if _, err := os.Stat(final); err == nil {
		return final, nil
	}
	if err := os.MkdirAll(root, 0o700); err != nil {
		return "", err
	}
	f, err := embedded.Open(path.Join(name, sum, files[0].Name()))
	if err != nil {
		return "", err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return "", err
	}
	// Unpacked beside its final place and then renamed, so another copy of
	// the app never sees half a dependency.
	tmp, err := os.MkdirTemp(root, ".unpack-")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(tmp)
	if err := unpack.Unpack(f.(io.ReaderAt), info.Size(), files[0].Name(), name, tmp); err != nil {
		return "", fmt.Errorf("could not unpack %s: %v", name, err)
	}
	if err := os.Rename(tmp, final); err != nil {
		if _, statErr := os.Stat(final); statErr == nil {
			return final, nil
		}
		return "", err
	}
	removeOldVersions(root, name, final)
	return final, nil
}

// removeOldVersions deletes the copies of name that an earlier version of
// the app unpacked. A failure leaves them for the next time.
func removeOldVersions(root, name, keep string) {
	old, _ := filepath.Glob(filepath.Join(root, name+"-*"))
	for _, dir := range old {
		if dir != keep && len(filepath.Base(dir)) == len(filepath.Base(keep)) {
			os.RemoveAll(dir)
		}
	}
}

// DB is the app's SQLite database, app.db in DataDir. The first call opens
// it and applies any new migrations from database/: those every mode has,
// and those of the app's mode.
//
// Call it from Services or later, not when a package is initialized: the
// migrations are not available before the app starts. It panics if the
// database cannot be opened or a migration fails, since the app cannot work
// without it.
//
// It is opened with foreign keys on, write-ahead logging, a five-second wait
// for a lock, and transactions that take the write lock when they begin.
func DB() *sql.DB {
	migrations, started := appstate.Migrations()
	if !started {
		panic("desktop.DB was called before the app started; call it from Services() or later, not when a package is initialized. Start cli/ with harness cli. In a Go test, use desktop.TestDB(t)")
	}
	db, err := openDB(migrations)
	if err != nil {
		panic("desktop.DB: " + err.Error())
	}
	return db
}

var (
	dbOnce sync.Once
	db     *sql.DB
	dbErr  error
)

func openDB(migrations fs.FS) (*sql.DB, error) {
	dbOnce.Do(func() {
		dir, err := dataDir()
		if err != nil {
			dbErr = err
			return
		}
		ms, err := migrate.Load(migrations)
		if err != nil {
			dbErr = err
			return
		}
		db, dbErr = open(filepath.Join(dir, appstate.DBFile), ms)
	})
	return db, dbErr
}

// NewDB opens a new database at path, a file that does not exist yet, with
// the migrations every mode shares applied: those at the top of database/.
// It is for a second database beside the app's, such as a sandbox the app
// runs something in and then deletes; the caller closes it. Like DB, call it
// from Services or later.
func NewDB(path string) (*sql.DB, error) {
	migrations, started := appstate.Migrations()
	if !started {
		return nil, errors.New("desktop.NewDB was called before the app started; call it from Services() or later. Start cli/ with harness cli. In a Go test, use desktop.TestDB(t)")
	}
	if _, err := os.Stat(path); err == nil {
		return nil, fmt.Errorf("desktop.NewDB: %s already exists", path)
	}
	ms, err := migrate.LoadShared(migrations)
	if err != nil {
		return nil, err
	}
	return open(path, ms)
}

// Mode is the mode the app was built in, such as dev: what the frontend has
// as DESKTOP_MODE.
func Mode() string { return appstate.Mode }

// TestDB is a new database for one test, with the migrations every mode
// shares applied: those at the top of database/. A mode's own migrations
// are data, such as fixtures, which a Go test makes for itself. It is closed
// and deleted when the test ends.
func TestDB(t testing.TB) *sql.DB {
	t.Helper()
	root, err := projectRoot()
	if err != nil {
		t.Fatalf("desktop.TestDB: %v", err)
	}
	ms, err := migrate.Collect(os.DirFS(filepath.Join(root, migrate.Dir)), nil, "")
	if err != nil {
		t.Fatalf("desktop.TestDB: %v", err)
	}
	db, err := open(filepath.Join(t.TempDir(), appstate.DBFile), ms)
	if err != nil {
		t.Fatalf("desktop.TestDB: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

// projectRoot is the folder with desktop.yaml above the test's package,
// which is where go test runs it.
func projectRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for d := dir; ; d = filepath.Dir(d) {
		if _, err := os.Stat(filepath.Join(d, "desktop.yaml")); err == nil {
			return d, nil
		}
		if filepath.Dir(d) == d {
			return "", fmt.Errorf("no desktop.yaml above %s", dir)
		}
	}
}

func open(path string, ms []migrate.Migration) (*sql.DB, error) {
	db, err := sql.Open("sqlite", path+
		"?_pragma=foreign_keys(1)&_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=synchronous(NORMAL)&_txlock=immediate")
	if err != nil {
		return nil, err
	}
	if err := migrate.Apply(db, ms); err != nil {
		db.Close()
		return nil, err
	}
	return db, nil
}
