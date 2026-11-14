package desktop

import (
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/michaelckearney/monorepo/framework/internal/migrate"
)

func migrations(files ...string) fstest.MapFS {
	m := fstest.MapFS{}
	for i := 0; i+1 < len(files); i += 2 {
		m[files[i]] = &fstest.MapFile{Data: []byte(files[i+1])}
	}
	return m
}

// openMode opens path with mode's migrations from database, as the app
// built in mode does.
func openMode(path string, database fstest.MapFS, modes []string, mode string) (*sql.DB, error) {
	ms, err := migrate.Collect(database, modes, mode)
	if err != nil {
		return nil, err
	}
	return open(path, ms)
}

func count(t *testing.T, path string, database fstest.MapFS, mode string) int {
	t.Helper()
	db, err := openMode(path, database, []string{"dev", "prod"}, mode)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var n int
	if err := db.QueryRow("SELECT count(*) FROM notes").Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

const notes = "CREATE TABLE notes (id INTEGER PRIMARY KEY, body TEXT NOT NULL);\nCREATE INDEX notes_body ON notes(body);"

func TestOpenAppliesEachMigrationOnce(t *testing.T) {
	path := filepath.Join(t.TempDir(), "app.db")
	// Two statements in one file: migrations are written as plain SQL
	// scripts, not one statement each.
	one := migrations("001_notes.sql", notes)
	if n := count(t, path, one, "prod"); n != 0 {
		t.Fatalf("new database: %d notes", n)
	}
	two := migrations("001_notes.sql", notes, "002_more.sql", "INSERT INTO notes (body) VALUES ('from a migration');")
	for range 2 {
		if n := count(t, path, two, "prod"); n != 1 {
			t.Fatalf("after a new migration: %d notes, want 1", n)
		}
	}

	// The older app opening a database a newer one migrated.
	if _, err := openMode(path, one, nil, "prod"); err == nil || !strings.Contains(err.Error(), "newer version of the app") {
		t.Fatalf("downgrade: %v", err)
	}
}

func TestEachModeHasTheSharedMigrationsAndItsOwn(t *testing.T) {
	database := migrations(
		"001_notes.sql", notes,
		"dev/002_fixtures.sql", "INSERT INTO notes (body) VALUES ({{file \"assets/note.md\"}});",
		"dev/assets/note.md", "it's {{not}} a template",
		"003_shared.sql", "INSERT INTO notes (body) VALUES ('every mode');",
	)
	dev := filepath.Join(t.TempDir(), "app.db")
	if n := count(t, dev, database, "dev"); n != 2 {
		t.Errorf("dev: %d notes, want the fixture and the shared one", n)
	}
	db, _ := openMode(dev, database, []string{"dev", "prod"}, "dev")
	var body string
	db.QueryRow("SELECT body FROM notes WHERE id = 1").Scan(&body)
	db.Close()
	if body != "it's {{not}} a template" {
		t.Errorf("the asset went in as %q", body)
	}
	// prod skips 002 and still applies 003.
	if n := count(t, filepath.Join(t.TempDir(), "app.db"), database, "prod"); n != 1 {
		t.Errorf("prod: %d notes, want only the shared one", n)
	}
}

func TestFailedMigrationLeavesTheDatabaseAsItWas(t *testing.T) {
	path := filepath.Join(t.TempDir(), "app.db")
	good := "CREATE TABLE notes (id INTEGER PRIMARY KEY, body TEXT NOT NULL);"
	bad := migrations("001_notes.sql", good, "002_bad.sql", "INSERT INTO notes (body) VALUES ('x'); SELEC broken;")
	if _, err := openMode(path, bad, nil, "prod"); err == nil || !strings.Contains(err.Error(), "002_bad.sql") {
		t.Fatalf("got %v, want the failing file named", err)
	}
	// 001 stays applied; 002 left nothing behind, and runs again once fixed.
	fixed := migrations("001_notes.sql", good, "002_bad.sql", "INSERT INTO notes (body) VALUES ('x');")
	if n := count(t, path, fixed, "prod"); n != 1 {
		t.Fatalf("after fixing: %d notes, want 1", n)
	}
}

func TestDatabaseSettings(t *testing.T) {
	db, err := open(filepath.Join(t.TempDir(), "app.db"), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var fk int
	var mode string
	db.QueryRow("PRAGMA foreign_keys").Scan(&fk)
	db.QueryRow("PRAGMA journal_mode").Scan(&mode)
	if fk != 1 || mode != "wal" {
		t.Errorf("foreign_keys %d, journal_mode %q", fk, mode)
	}
}

func TestMigrationChecks(t *testing.T) {
	for files, want := range map[string]string{
		"notes.sql":                 "database/notes.sql: a migration is named <number>_<words>.sql",
		"002_a.sql":                 "there is no migration 001",
		"001_a.sql,001_b.sql":       "have the same number",
		"001_a.sql,dev/001_b.sql":   "database/001_a.sql and database/dev/001_b.sql have the same number",
		"001_a.sql,003_b.sql":       "there is no migration 002",
		"001_a.sql,prod/003_b.sql":  "there is no migration 002",
		"001_a.sql,stage/002_b.sql": "database/stage/ is not a mode",
		"001_{{file \"x.md\"}}.sql": "a migration is named",
	} {
		m := fstest.MapFS{}
		for _, f := range strings.Split(files, ",") {
			m[f] = &fstest.MapFile{Data: []byte("SELECT 1;")}
		}
		if _, err := migrate.Collect(m, []string{"dev", "prod"}, "dev"); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%s: got %v, want %q", files, err, want)
		}
	}
	for body, want := range map[string]string{
		`{{file "assets/missing.md"}}`: "there is no database/dev/assets/missing.md",
		`{{file "../001_a.sql"}}`:      "is not in assets/",
		`{{file "assets/../x"}}`:       "is not in assets/",
		`{{file "assets/bin"}}`:        "database/dev/assets/bin is not text",
		`{{nope}}`:                     "database/dev/002_b.sql",
	} {
		m := migrations("001_a.sql", "SELECT 1;", "dev/002_b.sql", body, "dev/assets/bin", "\x00")
		if _, err := migrate.Collect(m, []string{"dev", "prod"}, "dev"); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%s: got %v, want %q", body, err, want)
		}
	}
}

func TestUnpackEmbeddedDep(t *testing.T) {
	root := t.TempDir()
	sum := strings.Repeat("ab", 32)
	embedded := fstest.MapFS{
		"jq/" + sum + "/jq-macos-arm64": {Data: []byte("#!/bin/sh\necho jq\n")},
	}
	dir, err := unpackedDep(embedded, root, "jq")
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(root, "jq-"+sum[:16]); dir != want {
		t.Errorf("unpacked into %s, want %s", dir, want)
	}
	info, err := os.Stat(filepath.Join(dir, "jq"))
	if err != nil || info.Mode()&0o111 == 0 {
		t.Fatalf("jq is not an executable file: %v %v", info, err)
	}

	// A later call reuses the folder. A new version replaces the old one.
	os.WriteFile(filepath.Join(dir, "marker"), nil, 0o644)
	if again, _ := unpackedDep(embedded, root, "jq"); again != dir {
		t.Errorf("second call gave %s", again)
	}
	if _, err := os.Stat(filepath.Join(dir, "marker")); err != nil {
		t.Error("the second call unpacked again")
	}
	newer := strings.Repeat("cd", 32)
	updated := fstest.MapFS{"jq/" + newer + "/jq-macos-arm64": {Data: []byte("v2")}}
	if _, err := unpackedDep(updated, root, "jq"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Error("the old version is still there")
	}

	if _, err := unpackedDep(embedded, root, "nope"); err == nil || !strings.Contains(err.Error(), "does not embed a dependency nope") {
		t.Errorf("missing dependency: %v", err)
	}
}
