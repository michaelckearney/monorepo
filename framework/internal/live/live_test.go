package live

import (
	"database/sql"
	"encoding/json"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// quiet is how long a test waits to be sure no update is coming: longer than
// the engine's tick and settle time together.
const quiet = 600 * time.Millisecond

type harness struct {
	t       *testing.T
	path    string
	db      *sql.DB
	e       *Engine
	updates chan Update
}

func start(t *testing.T) *harness {
	t.Helper()
	h := &harness{t: t, path: filepath.Join(t.TempDir(), "app.db"), updates: make(chan Update, 100)}
	var err error
	h.db, err = sql.Open("sqlite", h.path+"?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)")
	if err != nil {
		t.Fatal(err)
	}
	h.exec(`CREATE TABLE notes (id INTEGER PRIMARY KEY, folder TEXT, body TEXT);
		CREATE INDEX notes_folder ON notes(folder);
		CREATE TABLE settings (k TEXT PRIMARY KEY, v TEXT);
		INSERT INTO notes (folder, body) VALUES ('inbox', 'one'), ('inbox', 'two'), ('archive', 'old');`)
	h.e, err = Start(h.path, func(u Update) { h.updates <- u })
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		h.e.Close()
		h.db.Close()
	})
	return h
}

func (h *harness) exec(q string, args ...any) {
	h.t.Helper()
	if _, err := h.db.Exec(q, args...); err != nil {
		h.t.Fatal(err)
	}
}

func (h *harness) next() Update {
	h.t.Helper()
	select {
	case u := <-h.updates:
		return u
	case <-time.After(3 * time.Second):
		h.t.Fatal("no update arrived")
		return Update{}
	}
}

func (h *harness) none() {
	h.t.Helper()
	select {
	case u := <-h.updates:
		h.t.Fatalf("unexpected update: %+v", u)
	case <-time.After(quiet):
	}
}

func rowsText(rows []json.RawMessage) string {
	var parts []string
	for _, r := range rows {
		parts = append(parts, string(r))
	}
	return strings.Join(parts, " ")
}

func TestUpdatesAreSplicesOfChangedRows(t *testing.T) {
	h := start(t)
	snap, err := h.e.Subscribe("c", "1", "SELECT id, body FROM notes WHERE folder = ? ORDER BY id", []any{"inbox"})
	if err != nil {
		t.Fatal(err)
	}
	if got := rowsText(snap.Rows); got != `{"id":1,"body":"one"} {"id":2,"body":"two"}` || snap.Version != 1 {
		t.Fatalf("snapshot v%d: %s", snap.Version, got)
	}

	h.exec("INSERT INTO notes (folder, body) VALUES ('inbox', 'three')")
	u := h.next()
	if u.Version != 2 || u.Start != 2 || u.Delete != 0 || rowsText(u.Rows) != `{"id":4,"body":"three"}` {
		t.Fatalf("append: %+v %s", u, rowsText(u.Rows))
	}

	h.exec("UPDATE notes SET body = 'ONE' WHERE id = 1")
	u = h.next()
	if u.Version != 3 || u.Start != 0 || u.Delete != 1 || rowsText(u.Rows) != `{"id":1,"body":"ONE"}` {
		t.Fatalf("edit: %+v %s", u, rowsText(u.Rows))
	}

	// In a transaction with several writes, one update.
	tx, _ := h.db.Begin()
	tx.Exec("DELETE FROM notes WHERE id = 2")
	tx.Exec("DELETE FROM notes WHERE id = 4")
	tx.Commit()
	u = h.next()
	if u.Version != 4 || u.Start != 1 || u.Delete != 2 || len(u.Rows) != 0 {
		t.Fatalf("deletes: %+v", u)
	}
	h.none()
}

func TestOnlyChangesThatMatterSendAnything(t *testing.T) {
	h := start(t)
	if _, err := h.e.Subscribe("c", "1", "SELECT body FROM notes WHERE folder = 'inbox'", nil); err != nil {
		t.Fatal(err)
	}
	h.exec("INSERT INTO settings VALUES ('theme', 'dark')") // a table it does not read
	h.exec("UPDATE notes SET body = 'older' WHERE id = 3")  // a row outside its result
	tx, _ := h.db.Begin()                                   // never committed
	tx.Exec("INSERT INTO notes (folder) VALUES ('inbox')")
	tx.Rollback()
	h.none()
}

func TestTablesAreFoundThroughJoinsAndIndexes(t *testing.T) {
	h := start(t)
	h.exec("CREATE VIEW inbox AS SELECT n.body, s.v FROM notes n JOIN settings s ON s.k = n.folder")
	// Answered from the index alone: the table itself is never opened.
	if _, err := h.e.Subscribe("c", "count", "SELECT count(*) AS n FROM notes WHERE folder = 'inbox'", nil); err != nil {
		t.Fatal(err)
	}
	if _, err := h.e.Subscribe("c", "view", "SELECT * FROM inbox", nil); err != nil {
		t.Fatal(err)
	}
	h.exec("INSERT INTO settings VALUES ('inbox', 'x')")
	if u := h.next(); u.ID != "view" {
		t.Fatalf("settings change reached %q", u.ID)
	}
	h.exec("INSERT INTO notes (folder, body) VALUES ('inbox', 'n')")
	got := map[string]bool{h.next().ID: true, h.next().ID: true}
	if !got["count"] || !got["view"] {
		t.Fatalf("notes change reached %v", got)
	}
}

func TestAnotherProcessWritingIsSeen(t *testing.T) {
	cli, err := exec.LookPath("sqlite3")
	if err != nil {
		t.Skip("no sqlite3 command")
	}
	h := start(t)
	if _, err := h.e.Subscribe("c", "1", "SELECT body FROM notes ORDER BY id", nil); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command(cli, h.path, "INSERT INTO notes (folder, body) VALUES ('x', 'from outside')").CombinedOutput(); err != nil {
		t.Fatalf("%v: %s", err, out)
	}
	if u := h.next(); rowsText(u.Rows) != `{"body":"from outside"}` {
		t.Fatalf("outside write: %+v", u)
	}
}

func TestQueriesCannotWriteAndErrorsAreReported(t *testing.T) {
	h := start(t)
	if _, err := h.e.Subscribe("c", "w", "DELETE FROM notes RETURNING id", nil); err == nil {
		t.Fatal("a live query wrote to the database")
	}
	var n int
	h.db.QueryRow("SELECT count(*) FROM notes").Scan(&n)
	if n != 3 {
		t.Fatalf("notes: %d, want 3", n)
	}
	if _, err := h.e.Subscribe("c", "bad", "SELECT nope FROM notes", nil); err == nil {
		t.Fatal("want an error for a missing column")
	}
	if _, err := h.e.Subscribe("c", "obj", "SELECT ?", []any{map[string]any{}}); err == nil || !strings.Contains(err.Error(), "must be a string") {
		t.Fatalf("object argument: %v", err)
	}
	// JSON numbers arrive as floats; LIMIT refuses a real.
	snap, err := h.e.Subscribe("c", "limit", "SELECT id FROM notes ORDER BY id LIMIT ?", []any{float64(2)})
	if err != nil || len(snap.Rows) != 2 {
		t.Fatalf("limit: %v %v", snap, err)
	}
}

func TestClients(t *testing.T) {
	h := start(t)
	if h.e.Keep("c") {
		t.Fatal("an unknown client was kept")
	}
	h.e.Subscribe("c", "1", "SELECT id FROM notes", nil)
	if !h.e.Keep("c") {
		t.Fatal("a subscribed client was not kept")
	}
	h.e.Unsubscribe("c", "1")
	h.exec("INSERT INTO notes (folder) VALUES ('inbox')")
	h.none()
}

func TestDiff(t *testing.T) {
	for _, c := range []struct {
		old, new   string
		start, del int
		ins        string
		changed    bool
	}{
		{"a b c", "a b c", 0, 0, "", false},
		{"a b", "a b c", 2, 0, "c", true},
		{"a b c", "a x c", 1, 1, "x", true},
		{"a b c", "a c", 1, 1, "", true},
		{"", "a", 0, 0, "a", true},
		{"a b c d", "x b c y", 0, 4, "x b c y", true},
		{"a a", "a a a", 2, 0, "a", true},
	} {
		start, del, ins, changed := diff(strings.Fields(c.old), strings.Fields(c.new))
		if start != c.start || del != c.del || strings.Join(ins, " ") != c.ins || changed != c.changed {
			t.Errorf("diff(%q, %q) = %d, %d, %q, %v", c.old, c.new, start, del, ins, changed)
		}
	}
}
