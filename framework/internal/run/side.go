package run

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sync"
	"time"

	"github.com/michaelckearney/monorepo/framework/internal/branches"
	"github.com/michaelckearney/monorepo/framework/internal/sidecar"
	"github.com/michaelckearney/monorepo/framework/internal/steps"
)

// side is what the Desktop app drives a run with: the branch list, kept
// fresh in the background, and a local server the app talks to. Where the
// server is, and its token, are in .desktop/run.json while the run lasts.
// What the app asks for, the run loop does, one thing at a time, through
// reqs.
type side struct {
	r    *runner
	repo *branches.Repo
	// token is in run.json, readable only by this user, and every API
	// request must carry it in a header. A web page elsewhere can send
	// requests to 127.0.0.1, but it cannot read the token or set the header.
	token   string
	reqs    chan request
	refresh chan struct{}
	ln      net.Listener
	file    string // run.json, while the run lasts
	cancel  context.CancelFunc

	mu       sync.Mutex
	list     []branches.Branch
	prsErr   string
	fetching bool
	current  string
	status   status
}

type request struct {
	kind   string // "switch" or "reset"
	branch branches.Branch
}

// status is what the run is doing, for the window.
type status struct {
	// State is "running", "building" (switching or resetting), "failed"
	// (the last switch; what was running still is), or "stopped" (the app
	// exited).
	State   string `json:"state"`
	Target  string `json:"target,omitempty"`
	Message string `json:"message,omitempty"`
}

// listRefresh is how often the list is fetched again with the window open
// and no one pressing Refresh.
const listRefresh = 5 * time.Minute

func newSide(r *runner, repo *branches.Repo) *side {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return &side{
		r: r, repo: repo, token: hex.EncodeToString(b),
		reqs:    make(chan request, 1),
		refresh: make(chan struct{}, 1),
		status:  status{State: "running"},
	}
}

// ControlAPI is the version of the API in run.json. The app refuses a run
// whose version it does not know, rather than misreading it.
const ControlAPI = 1

// Control is .desktop/run.json: how the app finds a running `harness run`.
type Control struct {
	API   int    `json:"api"`
	URL   string `json:"url"`
	Token string `json:"token"`
	PID   int    `json:"pid"`
	App   string `json:"app"`
	Mode  string `json:"mode"`
}

// start serves the API, says where in run.json, and keeps the list fresh
// until the run ends. One that cannot start is logged and left out; the run
// goes on without it.
func (s *side) start(ctx context.Context) {
	ctx, s.cancel = context.WithCancel(ctx)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		steps.Logf("the branch switcher could not start: %v", err)
		return
	}
	s.mu.Lock()
	s.ln = ln
	s.mu.Unlock()
	go func() { _ = http.Serve(ln, s.handler()) }()
	go s.keepFresh(ctx)

	c := Control{API: ControlAPI, URL: "http://" + ln.Addr().String(), Token: s.token, PID: os.Getpid(),
		App: s.r.home.Config.Name, Mode: s.r.modeName}
	file := filepath.Join(s.repo.Home, ".desktop", "run.json")
	data, _ := json.MarshalIndent(c, "", "  ")
	if err := os.MkdirAll(filepath.Dir(file), 0o755); err == nil {
		err = os.WriteFile(file, append(data, '\n'), 0o600)
	}
	if err != nil {
		steps.Logf("the Desktop app cannot find this run: %v", err)
		return
	}
	s.mu.Lock()
	s.file = file
	s.mu.Unlock()
	steps.Logf("open this project in the Desktop app to run another branch or pull request in place of this checkout")
}

func (s *side) stop() {
	if s.cancel != nil {
		s.cancel()
	}
	s.mu.Lock()
	file, ln := s.file, s.ln
	s.mu.Unlock()
	if file != "" {
		os.Remove(file)
	}
	if ln != nil {
		ln.Close()
	}
}

// keepFresh lists the branches, then fetches and asks GitHub, and again on
// each Refresh and every listRefresh. The local list comes first, so the
// window has something to show while the network is slow.
func (s *side) keepFresh(ctx context.Context) {
	tick := time.NewTicker(listRefresh)
	defer tick.Stop()
	for {
		s.load(ctx)
		select {
		case <-ctx.Done():
			return
		case <-s.refresh:
		case <-tick.C:
		}
	}
}

func (s *side) load(ctx context.Context) {
	if list, err := s.repo.List(ctx); err == nil {
		s.mu.Lock()
		if s.list == nil {
			s.list = list
		}
		s.fetching = true
		s.mu.Unlock()
	}
	var (
		wg       sync.WaitGroup
		prs      []branches.PR
		prsErr   error
		fetchErr error
	)
	wg.Add(2)
	go func() { defer wg.Done(); fetchErr = s.repo.Fetch(ctx) }()
	go func() { defer wg.Done(); prs, prsErr = s.repo.PRs(ctx) }()
	wg.Wait()
	if ctx.Err() != nil {
		return
	}
	list, err := s.repo.List(ctx)
	s.mu.Lock()
	defer s.mu.Unlock()
	s.fetching = false
	if err != nil {
		steps.Logf("sidecar: %v", err)
		return
	}
	s.prsErr = ""
	if prsErr != nil {
		s.prsErr = prsErr.Error()
	} else if fetchErr != nil {
		s.prsErr = fetchErr.Error()
	}
	s.list = branches.WithPRs(list, prs)
}

// progress says what a switch is doing now.
func (s *side) progress(format string, args ...any) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.status.Message = fmt.Sprintf(format, args...)
}

// done ends a switch or a reset, with what is now running.
func (s *side) done(err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.current = s.r.cur.key
	if err != nil {
		s.status = status{State: "failed", Target: s.status.Target, Message: err.Error()}
		if !s.r.app.Exited() {
			return
		}
		s.status.Message += "; the app is not running"
		return
	}
	s.status = status{State: "running"}
}

// stopped says the app exited on its own.
func (s *side) stopped(err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.status = status{State: "stopped", Message: fmt.Sprintf("it exited (%v); desktop restarts it when the backend changes", err)}
}

// started says the app is running again after it stopped.
func (s *side) started() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.status.State == "stopped" {
		s.status = status{State: "running"}
	}
}

func (s *side) find(key string) (branches.Branch, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, b := range s.list {
		if b.Key == key {
			return b, true
		}
	}
	return branches.Branch{}, false
}

func (s *side) handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		_, _ = w.Write(sidecar.Page)
	})
	api := func(pattern string, h func(http.ResponseWriter, *http.Request) error) {
		mux.HandleFunc(pattern, func(w http.ResponseWriter, req *http.Request) {
			if subtle.ConstantTimeCompare([]byte(req.Header.Get("X-Desktop-Token")), []byte(s.token)) != 1 {
				http.Error(w, "forbidden", http.StatusForbidden)
				return
			}
			if err := h(w, req); err != nil {
				code := http.StatusBadRequest
				if he, ok := err.(httpError); ok {
					code = he.code
				}
				http.Error(w, err.Error(), code)
			}
		})
	}

	api("GET /api/state", func(w http.ResponseWriter, _ *http.Request) error {
		s.mu.Lock()
		defer s.mu.Unlock()
		list := s.list
		if list == nil {
			list = []branches.Branch{}
		}
		return writeJSON(w, map[string]any{
			"api":      ControlAPI,
			"app":      s.r.home.Config.Name,
			"mode":     s.r.modeName,
			"current":  s.current,
			"status":   s.status,
			"branches": list,
			"prsError": s.prsErr,
			"fetching": s.fetching,
		})
	})

	api("POST /api/switch", func(w http.ResponseWriter, req *http.Request) error {
		var body struct {
			Key   string `json:"key"`
			Trust bool   `json:"trust"`
		}
		if err := readJSON(req, &body); err != nil {
			return err
		}
		b, ok := s.find(body.Key)
		if !ok {
			return httpError{http.StatusNotFound, "that branch is not in the list any more; refresh it"}
		}
		if b.PR != nil && b.PR.Fork && !body.Trust {
			return fmt.Errorf("pull request #%d is from a fork; confirm before running it", b.PR.Number)
		}
		return s.send(w, request{kind: "switch", branch: b}, b.Name)
	})

	api("POST /api/reset", func(w http.ResponseWriter, _ *http.Request) error {
		s.mu.Lock()
		cur := s.current
		s.mu.Unlock()
		if cur == "" {
			return fmt.Errorf("your own checkout's data is not reset from here")
		}
		b, _ := s.find(cur)
		return s.send(w, request{kind: "reset"}, "the data of "+b.Name)
	})

	api("POST /api/refresh", func(w http.ResponseWriter, _ *http.Request) error {
		select {
		case s.refresh <- struct{}{}:
		default:
		}
		s.mu.Lock()
		s.fetching = true
		s.mu.Unlock()
		w.WriteHeader(http.StatusNoContent)
		return nil
	})

	api("POST /api/open", func(w http.ResponseWriter, req *http.Request) error {
		var body struct {
			Key string `json:"key"`
		}
		if err := readJSON(req, &body); err != nil {
			return err
		}
		b, ok := s.find(body.Key)
		if !ok || b.PR == nil {
			return httpError{http.StatusNotFound, "no pull request"}
		}
		// Only a GitHub page from gh's own answer is opened.
		if u, err := url.Parse(b.PR.URL); err != nil || u.Scheme != "https" {
			return fmt.Errorf("not a web page: %s", b.PR.URL)
		}
		if err := openURL(b.PR.URL); err != nil {
			return err
		}
		w.WriteHeader(http.StatusNoContent)
		return nil
	})
	return mux
}

// send hands req to the run loop, unless it is busy with another.
func (s *side) send(w http.ResponseWriter, req request, target string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.status.State == "building" {
		return httpError{http.StatusConflict, "desktop is busy with " + s.status.Target}
	}
	select {
	case s.reqs <- req:
	default:
		return httpError{http.StatusConflict, "desktop is busy"}
	}
	s.status = status{State: "building", Target: target}
	w.WriteHeader(http.StatusAccepted)
	_, _ = w.Write([]byte("{}"))
	return nil
}

type httpError struct {
	code int
	msg  string
}

func (e httpError) Error() string { return e.msg }

func writeJSON(w http.ResponseWriter, v any) error {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	return json.NewEncoder(w).Encode(v)
}

func readJSON(req *http.Request, v any) error {
	return json.NewDecoder(io.LimitReader(req.Body, 1<<16)).Decode(v)
}

func openURL(u string) error {
	name := "xdg-open"
	if runtime.GOOS == "darwin" {
		name = "open"
	}
	return exec.Command(name, u).Start()
}
