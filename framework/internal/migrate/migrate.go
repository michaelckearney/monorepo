// Package migrate applies database/ to an app's SQLite database.
//
// Migrations are files named <number>_<words>.sql. Those at the top of
// database/ are every mode's; those in database/<mode>/ are that mode's
// alone, such as the fixtures a developer build starts with. The numbers run
// from 1 with no gaps across all the folders together, so each number is one
// file, and a mode skips the numbers of the others. They only go forward. The
// database's user_version is the number of the last one applied, so there is
// no bookkeeping table to get out of step.
//
// A migration is a Go template. {{file "assets/x.vue"}} is the file
// assets/x.vue beside it, as a quoted SQL string, so a document can be kept
// as itself rather than escaped into SQL.
//
// It uses only database/sql, so the CLI can check the files without linking
// a SQLite driver.
package migrate

import (
	"bytes"
	"database/sql"
	"errors"
	"fmt"
	"io/fs"
	"path"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"text/template"
	"unicode/utf8"
)

// Dir is how the user knows the migrations' folder, for the errors.
const Dir = "database"

// AssetsDir is the folder beside a mode's migrations that {{file}} reads.
const AssetsDir = "assets"

// Migration is one migration file, rendered.
type Migration struct {
	Version int
	Name    string
	SQL     string
}

var fileName = regexp.MustCompile(`^(\d+)_[A-Za-z0-9_-]+\.sql$`)

// Collect reads database/, checks it, and returns mode's migrations,
// rendered, in order: the shared ones and the mode's own. modes are all the
// app's modes, since the numbering runs across them all; nil takes every
// folder but assets/ for one, for a caller that has not read desktop.yaml.
// An empty mode is the shared migrations alone.
func Collect(fsys fs.FS, modes []string, mode string) ([]Migration, error) {
	entries, err := fs.ReadDir(fsys, ".")
	if errors.Is(err, fs.ErrNotExist) {
		// An app need not have a database.
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if modes == nil {
		for _, e := range entries {
			if e.IsDir() && e.Name() != AssetsDir {
				modes = append(modes, e.Name())
			}
		}
	}
	for _, e := range entries {
		if e.IsDir() && e.Name() != AssetsDir && !slices.Contains(modes, e.Name()) {
			return nil, fmt.Errorf("%s/%s/ is not a mode; the folders in %s/ are %s/ and one for each mode in desktop.yaml: %s",
				Dir, e.Name(), Dir, AssetsDir, strings.Join(modes, ", "))
		}
	}

	type source struct{ folder, name string }
	var all []source
	add := func(folder string) error {
		names, err := sqlFiles(fsys, folder)
		for _, n := range names {
			all = append(all, source{folder, n})
		}
		return err
	}
	if err := add("."); err != nil {
		return nil, err
	}
	for _, m := range modes {
		if err := add(m); err != nil {
			return nil, err
		}
	}

	byVersion := map[int]source{}
	for _, s := range all {
		v, err := version(s.name)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", display(s.folder, s.name), err)
		}
		if other, ok := byVersion[v]; ok {
			return nil, fmt.Errorf("%s and %s have the same number; renumber the newer one", display(other.folder, other.name), display(s.folder, s.name))
		}
		byVersion[v] = s
	}
	for v := 1; v <= len(all); v++ {
		if _, ok := byVersion[v]; !ok {
			return nil, fmt.Errorf("%s/: there is no migration %03d; migrations are numbered from 1 with no gaps, across %s/ and every mode's folder", Dir, v, Dir)
		}
	}

	var out []Migration
	for v := 1; v <= len(all); v++ {
		s := byVersion[v]
		if s.folder != "." && s.folder != mode {
			continue
		}
		sql, err := render(fsys, s.folder, s.name)
		if err != nil {
			return nil, err
		}
		out = append(out, Migration{Version: v, Name: s.name, SQL: sql})
	}
	return out, nil
}

// render runs one migration's template.
func render(fsys fs.FS, folder, name string) (string, error) {
	body, err := fs.ReadFile(fsys, path.Join(folder, name))
	if err != nil {
		return "", err
	}
	file := func(rel string) (string, error) {
		if rel != path.Clean(rel) || !strings.HasPrefix(rel, AssetsDir+"/") {
			return "", fmt.Errorf("%q is not in %s/; a migration includes only files from the %s/ folder beside it", rel, AssetsDir, AssetsDir)
		}
		data, err := fs.ReadFile(fsys, path.Join(folder, rel))
		if err != nil {
			return "", fmt.Errorf("there is no %s", display(folder, rel))
		}
		return Quote(data, display(folder, rel))
	}
	t, err := template.New(name).Option("missingkey=error").Funcs(template.FuncMap{"file": file}).Parse(string(body))
	if err != nil {
		return "", fmt.Errorf("%s: %v", display(folder, name), err)
	}
	var b bytes.Buffer
	if err := t.Execute(&b, nil); err != nil {
		return "", fmt.Errorf("%s: %v", display(folder, name), err)
	}
	return b.String(), nil
}

// Quote is data as an SQL string literal. SQLite text is UTF-8 and stops at
// a NUL, so a file that is not text is refused rather than cut short.
func Quote(data []byte, what string) (string, error) {
	if !utf8.Valid(data) || bytes.IndexByte(data, 0) >= 0 {
		return "", fmt.Errorf("%s is not text, so it cannot go into SQL as a string", what)
	}
	return "'" + strings.ReplaceAll(string(data), "'", "''") + "'", nil
}

func display(folder, name string) string {
	return path.Join(Dir, folder, name)
}

func version(name string) (int, error) {
	m := fileName.FindStringSubmatch(name)
	if m == nil {
		return 0, errors.New("a migration is named <number>_<words>.sql, like 001_create_notes.sql")
	}
	return strconv.Atoi(m[1])
}

// SharedFile is written beside a mode's rendered migrations, and names, one
// to a line, those every mode shares: a built app has only its own mode's,
// and cannot tell them apart otherwise.
const SharedFile = "shared.txt"

// LoadShared is Load, with only the migrations SharedFile names.
func LoadShared(fsys fs.FS) ([]Migration, error) {
	list, err := fs.ReadFile(fsys, SharedFile)
	if err != nil {
		return nil, err
	}
	shared := strings.Fields(string(list))
	all, err := Load(fsys)
	if err != nil {
		return nil, err
	}
	return slices.DeleteFunc(all, func(m Migration) bool { return !slices.Contains(shared, m.Name) }), nil
}

// Load reads the migrations the app embeds, which Collect rendered: the
// .sql files at the top of fsys, in their numbers' order.
func Load(fsys fs.FS) ([]Migration, error) {
	names, err := sqlFiles(fsys, ".")
	if err != nil {
		return nil, err
	}
	var out []Migration
	for _, name := range names {
		v, err := version(name)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", name, err)
		}
		body, err := fs.ReadFile(fsys, name)
		if err != nil {
			return nil, err
		}
		out = append(out, Migration{Version: v, Name: name, SQL: string(body)})
	}
	slices.SortFunc(out, func(a, b Migration) int { return a.Version - b.Version })
	return out, nil
}

// sqlFiles lists the .sql files in dir, by name. A missing folder is no
// files: a mode need not have migrations of its own.
func sqlFiles(fsys fs.FS, dir string) ([]string, error) {
	entries, err := fs.ReadDir(fsys, dir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var names []string
	for _, e := range entries {
		if !e.IsDir() && path.Ext(e.Name()) == ".sql" {
			names = append(names, e.Name())
		}
	}
	slices.Sort(names)
	return names, nil
}

// Apply runs the migrations db has not had yet, each in its own transaction.
func Apply(db *sql.DB, migrations []Migration) error {
	var current int
	if err := db.QueryRow("PRAGMA user_version").Scan(&current); err != nil {
		return err
	}
	last := 0
	if len(migrations) > 0 {
		last = migrations[len(migrations)-1].Version
	}
	if current > last {
		return fmt.Errorf("the database is at migration %d, but this app has only %d; a newer version of the app has used it", current, last)
	}
	for _, m := range migrations {
		if m.Version <= current {
			continue
		}
		tx, err := db.Begin()
		if err != nil {
			return err
		}
		if _, err := tx.Exec(m.SQL); err != nil {
			tx.Rollback()
			return fmt.Errorf("migration %s: %w", m.Name, err)
		}
		// user_version is part of the database file, so it commits or rolls
		// back with the migration.
		if _, err := tx.Exec("PRAGMA user_version = " + strconv.Itoa(m.Version)); err != nil {
			tx.Rollback()
			return err
		}
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("migration %s: %w", m.Name, err)
		}
	}
	return nil
}
