package site

import (
	"crypto/rand"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sync"

	_ "modernc.org/sqlite"
)

// Store keeps what the sites must not forget when Harbor restarts: their
// sessions, the tokens that stand for a sign-in, and their keys. Harbor
// restarts under harness run after each change to the backend, and with
// them in memory, each change signed you out of every site. It is a SQLite
// database of its own, apart from Harbor's. The sites' data is not kept: it
// starts afresh from apps/*/data each time.
//
// A broken database is not a case a site can answer, so its errors panic.
type Store struct {
	db *sql.DB

	mu     sync.Mutex
	tables map[string]bool
}

// OpenStore opens the store at path, made if there is none.
func OpenStore(path string) (*Store, error) {
	db, err := sql.Open("sqlite", path+"?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=synchronous(NORMAL)")
	if err != nil {
		return nil, err
	}
	_, err = db.Exec(`
		CREATE TABLE IF NOT EXISTS secrets (name TEXT PRIMARY KEY, value BLOB NOT NULL);
		CREATE TABLE IF NOT EXISTS rows (tbl TEXT NOT NULL, id TEXT NOT NULL, value TEXT NOT NULL, PRIMARY KEY (tbl, id));
	`)
	if err != nil {
		db.Close()
		return nil, fmt.Errorf("the fixtures' store at %s: %w", path, err)
	}
	return &Store{db: db, tables: map[string]bool{}}, nil
}

func (s *Store) Close() error { return s.db.Close() }

func must(err error) {
	if err != nil {
		panic(fmt.Errorf("the fixtures' store: %w", err))
	}
}

// Secret is the secret called name, made with gen the first time it is
// asked for. Two processes on one store, as Harbor and a benchmark, get the
// same one.
func (s *Store) Secret(name string, gen func() []byte) []byte {
	var v []byte
	err := s.db.QueryRow(`SELECT value FROM secrets WHERE name = ?`, name).Scan(&v)
	if err == nil {
		return v
	}
	if !errors.Is(err, sql.ErrNoRows) {
		must(err)
	}
	_, err = s.db.Exec(`INSERT INTO secrets (name, value) VALUES (?, ?) ON CONFLICT DO NOTHING`, name, gen())
	must(err)
	must(s.db.QueryRow(`SELECT value FROM secrets WHERE name = ?`, name).Scan(&v))
	return v
}

// Key is a random key called name, as for signing a site's tokens.
func (s *Store) Key(name string) []byte {
	return s.Secret(name, func() []byte {
		b := make([]byte, 32)
		rand.Read(b)
		return b
	})
}

// Table is one kind of thing a site keeps, by ID, as JSON.
type Table[T any] struct {
	s    *Store
	name string
}

// NewTable is the table called name. Each name is one table's, and two
// sites that asked for the same one would share it, so it panics.
func NewTable[T any](s *Store, name string) *Table[T] {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.tables[name] {
		panic("two of the fixtures' tables are called " + name)
	}
	s.tables[name] = true
	return &Table[T]{s: s, name: name}
}

func (t *Table[T]) decode(raw string) T {
	var v T
	must(json.Unmarshal([]byte(raw), &v))
	return v
}

// Get is the row with id, if there is one.
func (t *Table[T]) Get(id string) (T, bool) {
	var raw string
	err := t.s.db.QueryRow(`SELECT value FROM rows WHERE tbl = ? AND id = ?`, t.name, id).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		var zero T
		return zero, false
	}
	must(err)
	return t.decode(raw), true
}

// Put saves v as the row with id.
func (t *Table[T]) Put(id string, v T) {
	raw, err := json.Marshal(v)
	must(err)
	_, err = t.s.db.Exec(`INSERT INTO rows (tbl, id, value) VALUES (?, ?, ?) ON CONFLICT (tbl, id) DO UPDATE SET value = excluded.value`, t.name, id, string(raw))
	must(err)
}

// Take gets the row with id and deletes it, at once, so a token meant to
// work once does, even for two requests at the same time.
func (t *Table[T]) Take(id string) (T, bool) {
	var raw string
	err := t.s.db.QueryRow(`DELETE FROM rows WHERE tbl = ? AND id = ? RETURNING value`, t.name, id).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		var zero T
		return zero, false
	}
	must(err)
	return t.decode(raw), true
}

func (t *Table[T]) Delete(id string) {
	_, err := t.s.db.Exec(`DELETE FROM rows WHERE tbl = ? AND id = ?`, t.name, id)
	must(err)
}

// Each calls fn with every row. The rows are read first, so fn may change
// the table.
func (t *Table[T]) Each(fn func(id string, v T)) {
	rows, err := t.s.db.Query(`SELECT id, value FROM rows WHERE tbl = ?`, t.name)
	must(err)
	type row struct{ id, raw string }
	var all []row
	for rows.Next() {
		var r row
		must(rows.Scan(&r.id, &r.raw))
		all = append(all, r)
	}
	must(rows.Err())
	rows.Close()
	for _, r := range all {
		fn(r.id, t.decode(r.raw))
	}
}
