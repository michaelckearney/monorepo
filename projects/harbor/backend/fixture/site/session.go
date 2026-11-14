// Package site has what every fixture app shares: sessions, pages, JSON,
// tokens, and the data files.
package site

import (
	"net/http"
	"time"
)

// Session is one signed-in browser. Each Get reads it afresh from the
// store: Save it after changing Data.
type Session[T any] struct {
	ID       string
	Created  time.Time
	LastSeen time.Time
	Data     T
}

// SessionOptions says how a site keeps its sessions.
type SessionOptions struct {
	Cookie      string
	Domain      string // "" for this host only
	Path        string // "" for "/"
	SameSite    http.SameSite
	Partitioned bool
	// Idle ends a session not used for this long; each use renews it.
	Idle time.Duration
	// Absolute ends a session this long after it started, used or not.
	Absolute time.Duration
}

// stored is a session as the store keeps it. A session that ended stays,
// with why, so the site can say so.
type stored[T any] struct {
	Created  time.Time
	LastSeen time.Time
	Data     T
	Ended    string `json:",omitempty"`
}

// Sessions keeps a site's sessions in the store, by cookie name, so they
// last while Harbor restarts.
type Sessions[T any] struct {
	opts  SessionOptions
	table *Table[stored[T]]
}

func NewSessions[T any](store *Store, opts SessionOptions) *Sessions[T] {
	if opts.Path == "" {
		opts.Path = "/"
	}
	if opts.SameSite == 0 {
		opts.SameSite = http.SameSiteLaxMode
	}
	return &Sessions[T]{opts: opts, table: NewTable[stored[T]](store, "session "+opts.Cookie)}
}

// Start starts a session, and sets its cookie.
func (s *Sessions[T]) Start(w http.ResponseWriter, data T) *Session[T] {
	now := time.Now()
	sess := &Session[T]{ID: Token(), Created: now, LastSeen: now, Data: data}
	s.table.Put(sess.ID, stored[T]{Created: now, LastSeen: now, Data: data})
	http.SetCookie(w, s.cookie(sess.ID, 0))
	return sess
}

// Get returns the request's session, or nil and why it ended ("" when there
// was none).
func (s *Sessions[T]) Get(r *http.Request) (*Session[T], string) {
	c, err := r.Cookie(s.opts.Cookie)
	if err != nil {
		return nil, ""
	}
	st, ok := s.table.Get(c.Value)
	if !ok {
		return nil, ""
	}
	if st.Ended != "" {
		return nil, st.Ended
	}
	now := time.Now()
	switch {
	case s.opts.Idle > 0 && now.Sub(st.LastSeen) > s.opts.Idle:
		st.Ended = "Your session ended because it was not used for a while."
	case s.opts.Absolute > 0 && now.Sub(st.Created) > s.opts.Absolute:
		st.Ended = "Your session ended. Sessions last a fixed time."
	default:
		st.LastSeen = now
	}
	s.table.Put(c.Value, st)
	if st.Ended != "" {
		return nil, st.Ended
	}
	return &Session[T]{ID: c.Value, Created: st.Created, LastSeen: st.LastSeen, Data: st.Data}, ""
}

// Save keeps the changes to sess's Data, unless it ended meanwhile.
func (s *Sessions[T]) Save(sess *Session[T]) {
	st, ok := s.table.Get(sess.ID)
	if !ok || st.Ended != "" {
		return
	}
	st.Data = sess.Data
	s.table.Put(sess.ID, st)
}

// End ends the request's session, and clears its cookie.
func (s *Sessions[T]) End(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(s.opts.Cookie); err == nil {
		s.end(c.Value, "You signed out.")
	}
	http.SetCookie(w, s.cookie("", -1))
}

// EndWhere ends every session whose data matches, for why.
func (s *Sessions[T]) EndWhere(match func(T) bool, why string) {
	s.table.Each(func(id string, st stored[T]) {
		if st.Ended == "" && match(st.Data) {
			s.end(id, why)
		}
	})
}

func (s *Sessions[T]) end(id, why string) {
	if st, ok := s.table.Get(id); ok {
		st.Ended = why
		s.table.Put(id, st)
	}
}

func (s *Sessions[T]) cookie(value string, maxAge int) *http.Cookie {
	return &http.Cookie{
		Name: s.opts.Cookie, Value: value, Domain: s.opts.Domain, Path: s.opts.Path,
		MaxAge: maxAge, Secure: true, HttpOnly: true, SameSite: s.opts.SameSite,
		Partitioned: s.opts.Partitioned,
	}
}
