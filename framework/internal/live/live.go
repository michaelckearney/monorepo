// Package live keeps the frontend's useQuery results current.
//
// A live query is an ordinary SELECT. When the frontend subscribes, the
// engine runs it on a read-only connection, asks SQLite which tables it reads
// (EXPLAIN), and remembers the result. Hooks on every connection record which
// tables each committed transaction wrote. The engine reruns only the
// queries that read one of them, compares each new result with the last, and
// sends just the rows that differ, as one splice.
//
// The hooks say which tables changed; PRAGMA data_version says when that
// change is visible to other connections. A commit hook fires before the
// commit is complete, so a query rerun from it could read the data as it was
// and conclude nothing changed. Waiting for data_version to move avoids that,
// and the same counter catches writes from another process, which no hook in
// this one sees: those rerun every query.
package live

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"math"
	"sync"
	"sync/atomic"
	"time"

	"modernc.org/sqlite"
)

// Event is the Wails event every update is sent on.
const Event = "desktop:live"

// Polling intervals. tick bounds how late a write from another process shows;
// settle bounds how long the engine waits for a commit it was told about to
// become visible.
const (
	tick       = 250 * time.Millisecond
	settleStep = 2 * time.Millisecond
	settleMax  = 100 * time.Millisecond
	// A client that has not been heard from in clientTTL is a window that
	// closed or reloaded; its queries are dropped. The frontend checks in
	// every 15 seconds, and resubscribes if it was dropped anyway, such as
	// after the machine slept.
	clientTTL = time.Minute
)

// Update is one change to one subscription. Rows replace Delete rows at
// Start: the frontend applies it as rows.splice(Start, Delete, ...Rows).
// Version is one more than the last version the frontend saw; anything else
// means an update was lost, and it subscribes again.
type Update struct {
	Client  string            `json:"client"`
	ID      string            `json:"id"`
	Version int               `json:"version"`
	Start   int               `json:"start"`
	Delete  int               `json:"delete"`
	Rows    []json.RawMessage `json:"rows"`
	Error   string            `json:"error,omitempty"`
}

// Snapshot is a subscription's first result.
type Snapshot struct {
	Version int               `json:"version"`
	Rows    []json.RawMessage `json:"rows"`
}

type key struct{ client, id string }

type sub struct {
	key   key
	query string
	args  []any

	// mu is held while the query runs, so a rerun never overlaps the first
	// run or another rerun. Lock order: sub.mu, then Engine.mu.
	mu      sync.Mutex
	closed  bool
	rows    []string
	version int

	// Guarded by Engine.mu.
	tables map[string]bool
	all    bool // reads something the engine cannot track, so every change reruns it
}

// Engine is the live queries of one database.
type Engine struct {
	reader *sql.DB
	conn   *sql.Conn // for data_version, which counts other connections' commits
	emit   func(Update)

	mu      sync.Mutex
	subs    map[key]*sub
	clients map[string]time.Time
	pending map[string]bool
	wake    chan struct{}
	stop    chan struct{}
	running sync.WaitGroup
}

// current is the running engine, which the connection hooks report to. Nil
// means no engine, such as in a Go test, and the hooks do nothing.
var current atomic.Pointer[Engine]

func init() {
	sqlite.RegisterConnectionHook(func(c sqlite.ExecQuerierContext, _ string) error {
		hooks, ok := c.(sqlite.HookRegisterer)
		if !ok {
			return nil
		}
		var mu sync.Mutex
		dirty := map[string]bool{}
		hooks.RegisterPreUpdateHook(func(d sqlite.SQLitePreUpdateData) {
			if current.Load() == nil || d.DatabaseName != "main" {
				return
			}
			mu.Lock()
			dirty[d.TableName] = true
			mu.Unlock()
		})
		hooks.RegisterCommitHook(func() int32 {
			mu.Lock()
			tables := dirty
			dirty = map[string]bool{}
			mu.Unlock()
			if e := current.Load(); e != nil && len(tables) > 0 {
				e.changed(tables)
			}
			return 0 // let the commit proceed
		})
		hooks.RegisterRollbackHook(func() {
			mu.Lock()
			clear(dirty)
			mu.Unlock()
		})
		return nil
	})
}

// Start opens path read-only for live queries and starts watching it. emit
// sends an update to the frontend.
func Start(path string, emit func(Update)) (*Engine, error) {
	// query_only makes a live query that tries to write fail, rather than
	// write: the frontend can read anything, and change nothing.
	reader, err := sql.Open("sqlite", path+"?_pragma=query_only(1)&_pragma=busy_timeout(5000)")
	if err != nil {
		return nil, err
	}
	conn, err := reader.Conn(context.Background())
	if err != nil {
		reader.Close()
		return nil, err
	}
	e := &Engine{
		reader:  reader,
		conn:    conn,
		emit:    emit,
		subs:    map[key]*sub{},
		clients: map[string]time.Time{},
		pending: map[string]bool{},
		wake:    make(chan struct{}, 1),
		stop:    make(chan struct{}),
	}
	current.Store(e)
	e.running.Add(2)
	go e.loop()
	go e.sweep()
	return e, nil
}

// Close stops the engine. The app's engine runs until the app exits; tests
// start and stop their own.
func (e *Engine) Close() error {
	current.CompareAndSwap(e, nil)
	close(e.stop)
	e.running.Wait()
	e.conn.Close()
	return e.reader.Close()
}

// changed is called from a commit hook, on the committing connection's
// goroutine, so it only records and signals.
func (e *Engine) changed(tables map[string]bool) {
	e.mu.Lock()
	for t := range tables {
		e.pending[t] = true
	}
	e.mu.Unlock()
	select {
	case e.wake <- struct{}{}:
	default:
	}
}

func (e *Engine) dataVersion() int64 {
	var v int64
	if err := e.conn.QueryRowContext(context.Background(), "PRAGMA data_version").Scan(&v); err != nil {
		log.Printf("desktop: live queries: %v", err)
	}
	return v
}

func (e *Engine) loop() {
	defer e.running.Done()
	ticker := time.NewTicker(tick)
	defer ticker.Stop()
	last := e.dataVersion()
	for {
		woke := false
		select {
		case <-e.wake:
			woke = true
		case <-ticker.C:
		case <-e.stop:
			return
		}
		v := e.dataVersion()
		for waited := time.Duration(0); woke && v == last && waited < settleMax; waited += settleStep {
			time.Sleep(settleStep)
			v = e.dataVersion()
		}
		if v == last {
			continue
		}
		last = v
		e.mu.Lock()
		tables := e.pending
		e.pending = map[string]bool{}
		e.mu.Unlock()
		// No tables means the change came from somewhere the hooks cannot
		// see: another process, or a schema change. Rerun everything, and
		// look again at what each query reads.
		e.refresh(tables, len(tables) == 0)
	}
}

func (e *Engine) refresh(tables map[string]bool, everything bool) {
	e.mu.Lock()
	var due []*sub
	for _, s := range e.subs {
		if everything || s.all || intersects(s.tables, tables) {
			due = append(due, s)
		}
	}
	e.mu.Unlock()
	for _, s := range due {
		e.rerun(s, everything)
	}
}

func intersects(a, b map[string]bool) bool {
	for t := range a {
		if b[t] {
			return true
		}
	}
	return false
}

func (e *Engine) rerun(s *sub, redepend bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return
	}
	if redepend {
		e.depend(s)
	}
	rows, err := e.run(s.query, s.args)
	if err != nil {
		// The last good rows stay as they are, and later updates are
		// relative to them.
		s.version++
		e.emit(Update{Client: s.key.client, ID: s.key.id, Version: s.version, Error: err.Error()})
		return
	}
	start, del, ins, changed := diff(s.rows, rows)
	if !changed {
		return
	}
	s.rows = rows
	s.version++
	e.emit(Update{Client: s.key.client, ID: s.key.id, Version: s.version, Start: start, Delete: del, Rows: raw(ins)})
}

// Subscribe starts a live query for a client and returns its first result.
// Subscribing again with the same id replaces the query.
func (e *Engine) Subscribe(client, id, query string, args []any) (Snapshot, error) {
	args, err := normalize(args)
	if err != nil {
		return Snapshot{}, err
	}
	s := &sub{key: key{client, id}, query: query, args: args}
	// Locked before it is visible, so no rerun can start ahead of the first
	// result.
	s.mu.Lock()
	defer s.mu.Unlock()

	e.mu.Lock()
	e.clients[client] = time.Now()
	if old := e.subs[s.key]; old != nil {
		s.version = old.version
		defer e.close(old)
	}
	e.subs[s.key] = s
	e.mu.Unlock()

	e.depend(s)
	rows, err := e.run(query, args)
	if err != nil {
		s.closed = true
		e.mu.Lock()
		delete(e.subs, s.key)
		e.mu.Unlock()
		return Snapshot{}, err
	}
	s.rows = rows
	s.version++
	return Snapshot{Version: s.version, Rows: raw(rows)}, nil
}

// Unsubscribe ends a live query. Ending one that does not exist does
// nothing.
func (e *Engine) Unsubscribe(client, id string) {
	e.mu.Lock()
	s := e.subs[key{client, id}]
	delete(e.subs, key{client, id})
	e.mu.Unlock()
	if s != nil {
		e.close(s)
	}
}

func (e *Engine) close(s *sub) {
	s.mu.Lock()
	s.closed = true
	s.mu.Unlock()
}

// Keep records that a client is still there. It returns false if the client
// was already dropped, so the frontend knows to subscribe again.
func (e *Engine) Keep(client string) bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	if _, ok := e.clients[client]; !ok {
		return false
	}
	e.clients[client] = time.Now()
	return true
}

func (e *Engine) sweep() {
	defer e.running.Done()
	ticker := time.NewTicker(clientTTL / 4)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
		case <-e.stop:
			return
		}
		e.mu.Lock()
		var gone []*sub
		for client, seen := range e.clients {
			if time.Since(seen) < clientTTL {
				continue
			}
			delete(e.clients, client)
			for k, s := range e.subs {
				if k.client == client {
					delete(e.subs, k)
					gone = append(gone, s)
				}
			}
		}
		e.mu.Unlock()
		for _, s := range gone {
			e.close(s)
		}
	}
}

// normalize turns the frontend's arguments into values SQLite binds as
// expected. JSON has one number type, so 20 arrives as 20.0, which SQLite
// would bind as a real: LIMIT refuses one.
func normalize(args []any) ([]any, error) {
	out := make([]any, len(args))
	for i, a := range args {
		switch v := a.(type) {
		case float64:
			if v == math.Trunc(v) && math.Abs(v) < 1<<53 {
				out[i] = int64(v)
			} else {
				out[i] = v
			}
		case nil, string, bool:
			out[i] = v
		default:
			return nil, fmt.Errorf("query value %d is a %T; a value in a query must be a string, number, boolean, or null", i+1, a)
		}
	}
	return out, nil
}

func raw(rows []string) []json.RawMessage {
	out := make([]json.RawMessage, len(rows))
	for i, r := range rows {
		out[i] = json.RawMessage(r)
	}
	return out
}
