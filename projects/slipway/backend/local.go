package backend

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/michaelckearney/monorepo/framework"
)

// Local runs the server's monorepos' projects on this computer. It keeps its
// own clone of each monorepo, in clones/ in the app's data folder, made the
// first time you run one of its projects, so your own checkouts are never
// touched and the app never has to guess where a project is. Each project
// of a clone runs on its own, at the same time as the others, with `harness
// run -p <project>`, in the mode you pick, which then runs whichever branch
// or pull request you pick. The
// harness is the version the clone's harness.yaml pins, which the app
// installs itself.
type Local struct {
	// settings hold your GitHub token, which clones the monorepos.
	settings *Settings
	// root is where the clones go; tests set it.
	root string

	mu    sync.Mutex
	runs  map[string]*localRun // the runs this app started, by runKey
	modes map[string][]Mode    // each project's modes, by its folder and desktop.yaml's time
}

type localRun struct {
	// phase is cloning, starting, or running, then stopped when the
	// process ends.
	phase string
	// project is the monorepo's project the run is of, and mode its mode,
	// or "" for the project's default.
	project string
	mode    string
	err     string
	cmd     *exec.Cmd
	done    chan struct{}
	log     *locked
}

// controlAPI is the version of harness run's control API this app knows.
const controlAPI = 1

// Branch is a branch harness run can run, as its control API lists it.
type Branch struct {
	// Key is "" for the clone's own checkout: the default branch.
	Key     string `json:"key"`
	Name    string `json:"name"`
	Home    bool   `json:"home"`
	Subject string `json:"subject"`
	Time    int64  `json:"time"`
	PR      *PR    `json:"pr"`
}

// PR is a branch's open pull request.
type PR struct {
	Number int    `json:"number"`
	Title  string `json:"title"`
	Author string `json:"author"`
	URL    string `json:"url"`
	Draft  bool   `json:"draft"`
	Fork   bool   `json:"fork"`
	Checks string `json:"checks"`
}

// Run is what a project's harness run is doing.
type Run struct {
	// Phase is "" when nothing runs, cloning or starting while this app
	// gets it going, running, or stopped when a run this app started ended.
	Phase   string `json:"phase"`
	Running bool   `json:"running"`
	Started bool   `json:"started"` // this app started it, so it can stop it
	Path    string `json:"path"`
	// PR is the pull request running, or 0 for the default branch.
	PR       int      `json:"pr"`
	Current  string   `json:"current"`
	State    string   `json:"state"` // harness run's: building, running, failed...
	Target   string   `json:"target"`
	Message  string   `json:"message"`
	Fetching bool     `json:"fetching"`
	Branches []Branch `json:"branches"`
	// Project is the project that runs, or would; Projects are the
	// monorepo's, once it is cloned.
	Project  string   `json:"project"`
	Projects []string `json:"projects"`
	// Mode is the mode a run this app started runs in, "" for the
	// project's default.
	Mode string `json:"mode"`
	// Error is why the last Run failed.
	Error string `json:"error"`
	// Output is the end of what a run this app started printed.
	Output string `json:"output"`
}

type control struct {
	API   int    `json:"api"`
	URL   string `json:"url"`
	Token string `json:"token"`
	PID   int    `json:"pid"`
}

type runState struct {
	API      int    `json:"api"`
	Current  string `json:"current"`
	Fetching bool   `json:"fetching"`
	// PRsError is why harness run could not list the pull requests.
	PRsError string   `json:"prsError"`
	Branches []Branch `json:"branches"`
	Status   struct {
		State, Target, Message string
	} `json:"status"`
}

var repoName = regexp.MustCompile(`^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$`)

// path is where repo's clone is, or would be.
func (l *Local) path(repo string) (string, error) {
	if !repoName.MatchString(repo) || strings.Contains(repo, "..") {
		return "", fmt.Errorf("%q is not a repository", repo)
	}
	root := l.root
	if root == "" {
		root = filepath.Join(desktop.DataDir(), "clones")
	}
	return filepath.Join(root, filepath.FromSlash(repo)), nil
}

// projects are the projects in a clone's harness.yaml, by name, with their
// folders. A project is its folder, or, in a harness.yaml from before v0.3,
// { path: <folder> }.
func projects(clone string) map[string]string {
	cfg, err := readHarnessYAML(clone)
	if err != nil {
		return nil
	}
	out := map[string]string{}
	for name, n := range cfg.Projects {
		var dir string
		if n.Kind == yaml.ScalarNode {
			dir = n.Value
		} else {
			var p struct {
				Path string `yaml:"path"`
			}
			if n.Decode(&p) != nil {
				continue
			}
			dir = p.Path
		}
		if dir != "" {
			out[name] = filepath.Join(clone, filepath.FromSlash(dir))
		}
	}
	return out
}

type harnessYAML struct {
	Harness  string               `yaml:"harness"`
	Projects map[string]yaml.Node `yaml:"projects"`
}

func readHarnessYAML(clone string) (harnessYAML, error) {
	var cfg harnessYAML
	raw, err := os.ReadFile(filepath.Join(clone, "harness.yaml"))
	if err != nil {
		return cfg, err
	}
	return cfg, yaml.Unmarshal(raw, &cfg)
}

// runKey is a run's key in Local.runs: its repository and project, which is
// "" only while the monorepo is cloned and its projects are not yet known.
func runKey(repo, project string) string { return repo + "#" + project }

// pick is the project a clone runs: the one asked for, else one this app
// started, else one whose harness run is running, else the first.
func (l *Local) pick(repo, clone, asked string) (string, string) {
	all := projects(clone)
	names := sortedNames(all)
	if asked != "" {
		return asked, all[asked]
	}
	l.mu.Lock()
	for _, n := range names {
		if l.runs[runKey(repo, n)] != nil {
			l.mu.Unlock()
			return n, all[n]
		}
	}
	l.mu.Unlock()
	for _, n := range names {
		if _, err := readControl(all[n]); err == nil {
			return n, all[n]
		}
	}
	if len(names) > 0 {
		return names[0], all[names[0]]
	}
	return "", clone
}

func sortedNames(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for n := range m {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

// State says what each project of repo is running, one Run for each, by
// name; before the monorepo is cloned, only the run this app is starting.
func (l *Local) State(repo string) ([]Run, error) {
	clone, err := l.path(repo)
	if err != nil {
		return nil, err
	}
	all := projects(clone)
	names := sortedNames(all)
	out := []Run{}
	for _, n := range names {
		r, err := l.state(repo, n, all[n], names)
		if err != nil {
			return out, err
		}
		out = append(out, r)
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if lr := l.runs[runKey(repo, "")]; lr != nil {
		out = append(out, Run{Branches: []Branch{}, Projects: names, Started: true, Phase: lr.phase, Error: lr.err,
			Mode: lr.mode, Output: tail(lr.log.String(), 8000)})
	}
	return out, nil
}

func (l *Local) state(repo, name, path string, names []string) (Run, error) {
	r := Run{Path: path, Branches: []Branch{}, Project: name, Projects: names}
	l.mu.Lock()
	if lr := l.runs[runKey(repo, name)]; lr != nil {
		r.Started, r.Phase, r.Error, r.Mode = true, lr.phase, lr.err, lr.mode
		r.Output = tail(lr.log.String(), 8000)
	}
	l.mu.Unlock()
	_, s, err := l.ask(path)
	if err != nil {
		if errors.Is(err, errVersion) {
			r.Error = err.Error()
		}
		return r, nil
	}
	r.Running, r.Phase = true, "running"
	r.Current, r.Fetching = s.Current, s.Fetching
	r.State, r.Target, r.Message = s.Status.State, s.Status.Target, s.Status.Message
	if s.Branches != nil {
		r.Branches = s.Branches
	}
	for _, b := range r.Branches {
		if b.Key == s.Current && b.PR != nil {
			r.PR = b.PR.Number
		}
	}
	return r, nil
}

var errVersion = errors.New("control API")

var errNotAnswering = errors.New("harness run is not answering")

// ask reads a clone's run.json and asks its harness run for its state.
func (l *Local) ask(path string) (control, runState, error) {
	var s runState
	c, err := readControl(path)
	if err != nil {
		return c, s, err
	}
	if err := call(c, "GET", "/api/state", nil, &s); err != nil {
		return c, s, err
	}
	if s.API != controlAPI {
		return c, s, fmt.Errorf("this project's harness run speaks %w %d, and this app knows %d: update one of them", errVersion, s.API, controlAPI)
	}
	return c, s, nil
}

// Run runs a pull request of a project of repo on this computer, or with
// pr 0 its default branch: project is one of the monorepo's, or "" for the
// one that runs or else the first, and mode one of its modes, or "" for
// the one that runs or else its default. It clones the monorepo the first
// time, starts harness run if that project is not running in that mode,
// and switches it to the pull request. The monorepo's other projects keep
// running. It returns at once; State says how it goes. A pull request from
// a fork needs trust: running it runs its code on this computer.
func (l *Local) Run(repo, project, mode string, pr int, trust bool) error {
	clone, err := l.path(repo)
	if err != nil {
		return err
	}
	name, path := l.pick(repo, clone, project)
	key := runKey(repo, name)
	l.mu.Lock()
	lr := l.runs[key]
	// A run started elsewhere keeps its mode: the app cannot stop it.
	sameMode := mode == "" || lr == nil || lr.mode == mode
	l.mu.Unlock()
	if _, _, err := l.ask(path); err == nil && sameMode {
		err := l.switchTo(path, pr, trust)
		l.mu.Lock()
		if lr != nil {
			lr.err = ""
			if err != nil {
				lr.err = err.Error()
			}
		}
		l.mu.Unlock()
		return err
	}
	if mode == "" && lr != nil {
		mode = lr.mode
	}
	// This project runs in another mode: it stops, for this one.
	if lr != nil && lr.cmd != nil && lr.phase != "stopped" {
		if err := l.Stop(repo, name); err != nil {
			return err
		}
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if lr := l.runs[key]; lr != nil && (lr.phase == "cloning" || lr.phase == "starting") {
		return errors.New("it is starting already")
	}
	if lr := l.runs[runKey(repo, "")]; lr != nil && lr.phase == "cloning" {
		return errors.New("the monorepo is being cloned; wait for it")
	}
	lr = &localRun{phase: "cloning", project: name, mode: mode, done: make(chan struct{}), log: &locked{w: &bytes.Buffer{}}}
	l.runs[key] = lr
	go l.start(repo, clone, pr, trust, lr)
	return nil
}

// live reports whether a harness run of another project of the clone is
// running, from this app or not.
func (l *Local) live(clone, project string) bool {
	for name, dir := range projects(clone) {
		if name == project {
			continue
		}
		if _, err := readControl(dir); err == nil {
			return true
		}
	}
	return false
}

func (l *Local) start(repo, clone string, pr int, trust bool, lr *localRun) {
	fail := func(err error) {
		l.mu.Lock()
		lr.phase, lr.err = "stopped", err.Error()
		l.mu.Unlock()
	}
	// The clone is brought up to date only while nothing else runs from
	// it: a pull would change the default branch under another project's
	// run, which would rebuild.
	if err := l.ensure(repo, clone, !l.live(clone, lr.project), lr.log); err != nil {
		fail(err)
		close(lr.done)
		return
	}
	all := projects(clone)
	if len(all) == 0 {
		fail(errors.New("the clone has no projects in its harness.yaml"))
		close(lr.done)
		return
	}
	if lr.project == "" {
		// Cloned: the run is the first project's from now on.
		l.mu.Lock()
		delete(l.runs, runKey(repo, ""))
		lr.project = sortedNames(all)[0]
		if other := l.runs[runKey(repo, lr.project)]; other != nil && other.phase != "stopped" {
			l.mu.Unlock()
			fail(fmt.Errorf("%s is running already", lr.project))
			close(lr.done)
			return
		}
		l.runs[runKey(repo, lr.project)] = lr
		l.mu.Unlock()
	}
	path, ok := all[lr.project]
	if !ok {
		fail(fmt.Errorf("the monorepo has no project %q", lr.project))
		close(lr.done)
		return
	}
	harness, err := l.harness(clone, lr.log)
	if err != nil {
		fail(err)
		close(lr.done)
		return
	}
	args := []string{"run", "-p", lr.project}
	if lr.mode != "" {
		args = append(args, "-mode="+lr.mode)
	}
	cmd := exec.Command(harness, args...)
	cmd.Dir = clone
	cmd.Env = gitEnv(l.settings.token())
	cmd.Stdout, cmd.Stderr = lr.log, lr.log
	group(cmd)
	if err := cmd.Start(); err != nil {
		fail(fmt.Errorf("could not start harness run: %w", err))
		close(lr.done)
		return
	}
	l.mu.Lock()
	lr.cmd, lr.phase = cmd, "starting"
	l.mu.Unlock()
	go func() {
		err := cmd.Wait()
		l.mu.Lock()
		lr.phase = "stopped"
		if err != nil && lr.err == "" {
			lr.err = "harness run ended: " + err.Error()
		}
		l.mu.Unlock()
		close(lr.done)
	}()
	// The first build can take minutes; switch once it answers. One that
	// answers with an error, such as refusing the token, will not answer
	// otherwise later: it says so.
	var askErr error
	for {
		select {
		case <-lr.done:
			return
		case <-time.After(time.Second):
		}
		_, _, askErr = l.ask(path)
		if askErr == nil || !errors.Is(askErr, fs.ErrNotExist) && !errors.Is(askErr, errNotAnswering) {
			break
		}
	}
	l.mu.Lock()
	lr.phase = "running"
	if askErr != nil {
		lr.err = "harness run answered: " + askErr.Error()
	}
	l.mu.Unlock()
	if askErr != nil {
		return
	}
	if pr != 0 {
		if err := l.switchTo(path, pr, trust); err != nil {
			l.mu.Lock()
			lr.err = fmt.Sprintf("harness run started on the default branch, and could not switch to #%d: %v", pr, err)
			l.mu.Unlock()
		}
	}
}

// harness is the harness the clone's harness.yaml pins, installed in the
// app's data folder with go install the first time it is needed, so each
// monorepo runs with the version it says. A harness.yaml that pins none
// gets the harness on PATH.
func (l *Local) harness(clone string, log io.Writer) (string, error) {
	cfg, err := readHarnessYAML(clone)
	if err != nil {
		return "", fmt.Errorf("the clone's harness.yaml: %w", err)
	}
	v := cfg.Harness
	if v == "" {
		return tool("harness"), nil
	}
	if !harnessVersion.MatchString(v) {
		return "", fmt.Errorf("harness.yaml pins harness %q, which is not a version such as v0.3.0", v)
	}
	root := l.root
	if root == "" {
		root = filepath.Join(desktop.DataDir(), "clones")
	}
	dir := filepath.Join(filepath.Dir(root), "harness", v)
	bin := filepath.Join(dir, "harness")
	if _, err := os.Stat(bin); err == nil {
		return bin, nil
	}
	harnessMu.Lock()
	defer harnessMu.Unlock()
	if _, err := os.Stat(bin); err == nil {
		return bin, nil
	}
	fmt.Fprintf(log, "go install github.com/michaelckearney/harness/cmd/harness@%s\n", v)
	cmd := exec.Command(tool("go"), "install", "github.com/michaelckearney/harness/cmd/harness@"+v)
	cmd.Env = append(gitEnv(l.settings.token()), "GOBIN="+dir)
	cmd.Stdout, cmd.Stderr = log, log
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("could not install harness %s: is go installed? %w", v, err)
	}
	return bin, nil
}

var (
	harnessVersion = regexp.MustCompile(`^v[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z.-]+)?$`)
	harnessMu      sync.Mutex
)

// Mode is one of a project's modes, as its framework's harness modes says.
type Mode struct {
	Mode string `json:"mode"`
	Name string `json:"name"`
}

// Modes are a project's modes in repo's clone, the default first, from
// `harness modes`, so the clone's own framework says what they are. Before
// the monorepo is cloned there are none.
func (l *Local) Modes(repo, project string) ([]Mode, error) {
	clone, err := l.path(repo)
	if err != nil {
		return nil, err
	}
	dir := projects(clone)[project]
	if dir == "" {
		return []Mode{}, nil
	}
	info, err := os.Stat(filepath.Join(dir, "desktop.yaml"))
	if err != nil {
		return []Mode{}, nil
	}
	key := dir + "@" + info.ModTime().String()
	l.mu.Lock()
	if m, ok := l.modes[key]; ok {
		l.mu.Unlock()
		return m, nil
	}
	l.mu.Unlock()
	harness, err := l.harness(clone, io.Discard)
	if err != nil {
		return nil, err
	}
	cmd := exec.Command(harness, "modes", "-p", project)
	cmd.Dir, cmd.Env = clone, env()
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("harness modes: %s", strings.TrimSpace(stderr.String()))
	}
	var got struct {
		Modes []Mode `json:"modes"`
	}
	if err := json.Unmarshal(out, &got); err != nil {
		return nil, fmt.Errorf("harness modes printed %q", strings.TrimSpace(string(out)))
	}
	if got.Modes == nil {
		got.Modes = []Mode{}
	}
	l.mu.Lock()
	if l.modes == nil {
		l.modes = map[string][]Mode{}
	}
	l.modes[key] = got.Modes
	l.mu.Unlock()
	return got.Modes, nil
}

// ensure clones repo the first time, with your GitHub token, and otherwise,
// with pull, brings its default branch up to date, so "run main" runs what
// is on GitHub.
func (l *Local) ensure(repo, path string, pull bool, log io.Writer) error {
	token := l.settings.token()
	if _, err := os.Stat(filepath.Join(path, ".git")); err == nil {
		if !pull {
			fmt.Fprintln(log, "another project runs from this clone, so its default branch is left where it is")
			return nil
		}
		cmd := exec.Command(tool("git"), "pull", "--ff-only")
		cmd.Dir, cmd.Env, cmd.Stdout, cmd.Stderr = path, gitEnv(token), log, log
		// A clone the app keeps should never have changes of its own; if a
		// pull fails anyway, run what is there rather than nothing.
		_ = cmd.Run()
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	url := "https://github.com/" + repo + ".git"
	fmt.Fprintf(log, "git clone %s\n", url)
	cmd := exec.Command(tool("git"), "clone", url, path)
	cmd.Env, cmd.Stdout, cmd.Stderr = gitEnv(token), log, log
	if err := cmd.Run(); err != nil {
		os.RemoveAll(path)
		if token == "" {
			return fmt.Errorf("git clone %s failed: add a GitHub token first", repo)
		}
		return fmt.Errorf("git clone %s failed: can your GitHub token read it?", repo)
	}
	return nil
}

// switchTo switches a running harness run to a pull request, or to the
// default branch. A pull request opened a moment ago may not be in its
// list yet, nor any when harness run has only just started, so it waits
// for the list, refreshes it once, and looks again.
func (l *Local) switchTo(path string, pr int, trust bool) error {
	for try := 0; ; try++ {
		c, s, err := l.ask(path)
		if err != nil {
			return errNotAnswering
		}
		for i := 0; pr != 0 && s.Fetching && i < 60; i++ {
			time.Sleep(time.Second)
			if c, s, err = l.ask(path); err != nil {
				return errNotAnswering
			}
		}
		for _, b := range s.Branches {
			if (pr == 0 && b.Home) || (pr != 0 && b.PR != nil && b.PR.Number == pr) {
				if b.Key == s.Current {
					return nil
				}
				return call(c, "POST", "/api/switch", map[string]any{"key": b.Key, "trust": trust}, nil)
			}
		}
		if try == 1 {
			if s.PRsError != "" {
				return fmt.Errorf("pull request #%d is not in harness run's list, which could not list the pull requests: %s", pr, s.PRsError)
			}
			return fmt.Errorf("pull request #%d is not in harness run's list; is it still open?", pr)
		}
		if err := call(c, "POST", "/api/refresh", nil, nil); err != nil {
			return err
		}
		for i := 0; i < 30; i++ {
			time.Sleep(time.Second)
			if _, s, err := l.ask(path); err == nil && !s.Fetching {
				break
			}
		}
	}
}

// Stop ends the harness run of a project of repo, if this app started it.
func (l *Local) Stop(repo, project string) error {
	l.mu.Lock()
	lr := l.runs[runKey(repo, project)]
	l.mu.Unlock()
	if lr == nil || lr.cmd == nil {
		return errors.New("this app did not start that run; stop it where it was started")
	}
	interrupt(lr.cmd)
	select {
	case <-lr.done:
	case <-time.After(10 * time.Second):
		kill(lr.cmd)
	}
	return nil
}

// Reset deletes the running branch's data, so it starts again with none.
func (l *Local) Reset(repo, project string) error { return l.send(repo, project, "/api/reset") }

// Refresh fetches, so the running branch catches up with its pull request.
func (l *Local) Refresh(repo, project string) error { return l.send(repo, project, "/api/refresh") }

func (l *Local) send(repo, project, route string) error {
	p, err := l.path(repo)
	if err != nil {
		return err
	}
	_, path := l.pick(repo, p, project)
	c, err := readControl(path)
	if err != nil {
		return errors.New("harness run is not running for this project")
	}
	return call(c, "POST", route, nil, nil)
}

func readControl(project string) (control, error) {
	var c control
	data, err := os.ReadFile(filepath.Join(project, ".desktop", "run.json"))
	if err != nil {
		return c, err
	}
	if err := json.Unmarshal(data, &c); err != nil {
		return c, err
	}
	if c.URL == "" || !strings.HasPrefix(c.URL, "http://127.0.0.1:") {
		return c, errors.New("run.json names no local server")
	}
	return c, nil
}

func call(c control, method, path string, body, out any) error {
	var r io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		r = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, c.URL+path, r)
	if err != nil {
		return err
	}
	req.Header.Set("X-Desktop-Token", c.Token)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := httpClient(15 * time.Second).Do(req)
	if err != nil {
		return errNotAnswering
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	if resp.StatusCode/100 != 2 {
		return errors.New(strings.TrimSpace(string(data)))
	}
	if out != nil {
		return json.Unmarshal(data, out)
	}
	return nil
}

func tail(s string, n int) string {
	if len(s) > n {
		return s[len(s)-n:]
	}
	return s
}

// locked keeps the end of a run's output: a run that lasts all day must not
// grow without end.
type locked struct {
	mu sync.Mutex
	w  *bytes.Buffer
}

func (l *locked) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.w.Len() > 1<<20 {
		keep := append([]byte(nil), l.w.Bytes()[l.w.Len()-64<<10:]...)
		l.w.Reset()
		l.w.Write(keep)
	}
	return l.w.Write(p)
}

func (l *locked) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.w.String()
}
