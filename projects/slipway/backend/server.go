package backend

import (
	"bufio"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// apiVersion is the version of the server's API this app understands.
const apiVersion = 1

// Server is the server's API: the bots' projects and threads, and what the
// bots are doing.
type Server struct {
	db       *sql.DB
	settings *Settings
}

// Project is a project with a bot.
type Project struct {
	Repo   string `json:"repo"`
	Base   string `json:"base"`
	Runner string `json:"runner"`
	// Projects are the monorepo's projects, from its harness.yaml.
	Projects []string `json:"projects"`
	Ready    bool     `json:"ready"`
	Threads  int      `json:"threads"`
	Running  int      `json:"running"`
}

// Thread is a bot's open thread: one issue, and its pull request.
type Thread struct {
	Repo   string `json:"repo"`
	Number int    `json:"number"`
	State  string `json:"state"`
	// Agent works the thread; Scope is what it works on, such as
	// project:harbor, or framework.
	Agent   string    `json:"agent"`
	Scope   string    `json:"scope"`
	PR      int       `json:"pr"`
	Session string    `json:"session"`
	Started time.Time `json:"started"`
	Waiting int       `json:"waiting"`
}

// Line is one thing a bot did during a turn.
type Line struct {
	Repo   string    `json:"repo"`
	Number int       `json:"number"`
	Time   time.Time `json:"time"`
	Kind   string    `json:"kind"`
	Text   string    `json:"text"`
}

// Status is whether the app can reach the server.
type Status struct {
	Server  string `json:"server"`
	OK      bool   `json:"ok"`
	Harness string `json:"harness"`
	Error   string `json:"error"`
}

// Status checks the API and its version.
func (s *Server) Status() Status {
	server, _ := s.settings.Server()
	st := Status{Server: server}
	if server == "" {
		st.Error = "Set the server's address, such as http://server:7420."
		return st
	}
	var v struct {
		API     int    `json:"api"`
		Harness string `json:"harness"`
	}
	if err := s.get("/api/v1/version", &v); err != nil {
		st.Error = err.Error()
		return st
	}
	if v.API != apiVersion {
		st.Error = fmt.Sprintf("The server runs harness %s, whose API (%d) this app does not know (%d). Update one of them.", v.Harness, v.API, apiVersion)
		return st
	}
	st.OK, st.Harness = true, v.Harness
	return st
}

// Projects lists the projects with a bot.
func (s *Server) Projects() ([]Project, error) {
	var out []Project
	return out, s.get("/api/v1/projects", &out)
}

// Threads lists a project's open threads.
func (s *Server) Threads(repo string) ([]Thread, error) {
	var out []Thread
	return out, s.get("/api/v1/threads?repo="+url.QueryEscape(repo), &out)
}

// Open fetches what the server holds of a thread's turns into the app, for
// the live view, which the stream then keeps current.
func (s *Server) Open(repo string, number int) (Thread, error) {
	var v struct {
		Thread Thread `json:"thread"`
		Lines  []Line `json:"lines"`
	}
	if err := s.get(fmt.Sprintf("/api/v1/threads/%s/%d", repo, number), &v); err != nil {
		return Thread{}, err
	}
	for _, l := range v.Lines {
		s.save(l)
	}
	return v.Thread, nil
}

// Log is a thread's whole log, every turn's prompt and what the bot did.
func (s *Server) Log(repo string, number int) (string, error) {
	resp, err := s.do("GET", fmt.Sprintf("/api/v1/threads/%s/%d/log", repo, number))
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	return string(b), err
}

// Stop stops a thread's running turn.
func (s *Server) Stop(repo string, number int) error {
	return s.post(fmt.Sprintf("/api/v1/threads/%s/%d/stop", repo, number))
}

// Retry gives a thread another turn.
func (s *Server) Retry(repo string, number int) error {
	return s.post(fmt.Sprintf("/api/v1/threads/%s/%d/retry", repo, number))
}

// follow keeps the live stream flowing into the lines table for as long as
// the app runs, reconnecting when the server or the network drops it.
func (s *Server) follow() {
	for {
		if err := s.stream(); err != nil {
			time.Sleep(5 * time.Second)
		}
	}
}

func (s *Server) stream() error {
	server, err := s.settings.Server()
	if err != nil || server == "" {
		return errors.New("no server")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	// A changed server ends this stream, and follow starts the next.
	go func() {
		for ctx.Err() == nil {
			time.Sleep(2 * time.Second)
			if now, _ := s.settings.Server(); now != server {
				cancel()
			}
		}
	}()
	req, err := http.NewRequestWithContext(ctx, "GET", server+"/api/v1/live", nil)
	if err != nil {
		return err
	}
	resp, err := httpClient(0).Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return errors.New(resp.Status)
	}
	sc := bufio.NewScanner(resp.Body)
	sc.Buffer(make([]byte, 64*1024), 8*1024*1024)
	for sc.Scan() {
		data, ok := strings.CutPrefix(sc.Text(), "data: ")
		if !ok {
			continue
		}
		var l Line
		if json.Unmarshal([]byte(data), &l) == nil {
			s.save(l)
		}
	}
	return sc.Err()
}

// keep is how many lines of each thread the app holds.
const keep = 2000

func (s *Server) save(l Line) {
	s.db.Exec(`INSERT OR IGNORE INTO lines (repo, number, time, kind, text) VALUES (?, ?, ?, ?, ?)`,
		l.Repo, l.Number, l.Time.UTC().Format(time.RFC3339Nano), l.Kind, l.Text)
	s.db.Exec(`DELETE FROM lines WHERE repo = ? AND number = ? AND id <= (
		SELECT id FROM lines WHERE repo = ? AND number = ? ORDER BY id DESC LIMIT 1 OFFSET ?)`,
		l.Repo, l.Number, l.Repo, l.Number, keep)
}

func (s *Server) do(method, path string) (*http.Response, error) {
	server, err := s.settings.Server()
	if err != nil {
		return nil, err
	}
	if server == "" {
		return nil, errors.New("set the server's address first")
	}
	req, err := http.NewRequest(method, server+path, nil)
	if err != nil {
		return nil, err
	}
	resp, err := httpClient(15 * time.Second).Do(req)
	if err != nil {
		return nil, fmt.Errorf("cannot reach the server at %s: is this computer on the tailnet?", server)
	}
	if resp.StatusCode/100 != 2 {
		b, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		return nil, errors.New(strings.TrimSpace(string(b)))
	}
	return resp, nil
}

func (s *Server) get(path string, out any) error {
	resp, err := s.do("GET", path)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	return json.NewDecoder(resp.Body).Decode(out)
}

func (s *Server) post(path string) error {
	resp, err := s.do("POST", path)
	if err != nil {
		return err
	}
	resp.Body.Close()
	return nil
}
