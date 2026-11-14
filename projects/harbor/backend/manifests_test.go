package backend

import (
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/michaelckearney/monorepo/framework"
)

func manifestOf(t *testing.T, db *sql.DB, name string) (manifest, problem sql.NullString) {
	t.Helper()
	if err := db.QueryRow(`SELECT manifest, manifest_error FROM services WHERE name = ?`, name).Scan(&manifest, &problem); err != nil {
		t.Fatal(err)
	}
	return manifest, problem
}

func TestReadManifestsFillsTheMissingOnes(t *testing.T) {
	db := desktop.TestDB(t)
	for name, src := range map[string]string{
		"one":     source("one"),
		"broken":  "export default {",
		"renamed": source("other"),
	} {
		if _, err := db.Exec(`INSERT INTO services (name, source) VALUES (?, ?)`, name, src); err != nil {
			t.Fatal(err)
		}
	}
	if err := readManifests(db); err != nil {
		t.Fatal(err)
	}
	if m, problem := manifestOf(t, db, "one"); !strings.Contains(m.String, `"name":"one"`) || problem.Valid {
		t.Errorf("one: manifest %q, error %q", m.String, problem.String)
	}
	if m, problem := manifestOf(t, db, "broken"); m.Valid || problem.String == "" {
		t.Errorf("broken: manifest %q, error %q", m.String, problem.String)
	}
	if _, problem := manifestOf(t, db, "renamed"); !strings.Contains(problem.String, "names the service other, but it is saved as renamed") {
		t.Errorf("renamed: error %q", problem.String)
	}

	// Saving the broken one fixed reads it, and clears the error.
	var id int64
	db.QueryRow(`SELECT id FROM services WHERE name = 'broken'`).Scan(&id)
	d := Documents{db: db, icons: newIcons(db)}
	if _, err := d.SaveService(id, source("broken")); err != nil {
		t.Fatal(err)
	}
	if m, problem := manifestOf(t, db, "broken"); !m.Valid || problem.Valid {
		t.Errorf("after the fix: manifest %q, error %q", m.String, problem.String)
	}

	// A service that cannot be read has no tools, and says why.
	tools := newTools(db, t.TempDir(), nil)
	if _, err := tools.service("renamed"); err == nil || !strings.Contains(err.Error(), "could not be read") {
		t.Errorf("tools of an unread service: %v", err)
	}
}

// Every dev fixture service reads, under the name database/dev/010_fixtures.sql
// saves it as: its file's name.
func TestFixtureServicesRead(t *testing.T) {
	files, _ := filepath.Glob("../database/dev/assets/services/*.ts")
	if len(files) != 11 {
		t.Fatalf("%d fixture services, want one for each site in backend/fixture", len(files))
	}
	db := desktop.TestDB(t)
	for _, f := range files {
		src, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec(`INSERT INTO services (name, source) VALUES (?, ?)`, strings.TrimSuffix(filepath.Base(f), ".ts"), string(src)); err != nil {
			t.Fatal(err)
		}
	}
	if err := readManifests(db); err != nil {
		t.Fatal(err)
	}
	rows, _ := db.Query(`SELECT name, manifest_error FROM services WHERE manifest_error IS NOT NULL`)
	defer rows.Close()
	for rows.Next() {
		var name, problem string
		rows.Scan(&name, &problem)
		t.Errorf("%s: %s", name, problem)
	}
}
