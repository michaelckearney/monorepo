package backend

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/michaelckearney/monorepo/framework"
)

// The answers GitHub gave the app's queries, cut down to one of each: a
// pull request whose checks mix a check run in progress, a finished one,
// and a commit status, and a comment from the bots' GitHub App.
const (
	issuesAnswer = `{"data":{"repository":{"list":{"nodes":[{"number":3,"title":"Say hello","state":"OPEN",
"url":"https://github.com/o/r/issues/3","updatedAt":"2026-11-07T10:00:00Z","createdAt":"2026-11-07T09:00:00Z",
"author":{"__typename":"User","login":"owner"},"labels":{"nodes":[{"name":"project:harbor"}]}}]}}}}`
	pullsAnswer = `{"data":{"repository":{"list":{"nodes":[{"number":4,"title":"Say hello","state":"OPEN",
"url":"https://github.com/o/r/pull/4","updatedAt":"2026-11-07T10:00:00Z","createdAt":"2026-11-07T09:30:00Z",
"author":{"__typename":"Bot","login":"slipway-bot"},"labels":{"nodes":[]},"isDraft":true,"headRefName":"harness/3",
"isCrossRepository":true,"additions":6,"deletions":10,"closingIssuesReferences":{"nodes":[{"number":3}]},
"commits":{"nodes":[{"commit":{"statusCheckRollup":{"contexts":{"nodes":[
{"__typename":"CheckRun","name":"test","status":"IN_PROGRESS","conclusion":null,"detailsUrl":"https://ci/1"},
{"__typename":"CheckRun","name":"build","status":"COMPLETED","conclusion":"SUCCESS","detailsUrl":"https://ci/3"},
{"__typename":"StatusContext","context":"lint","state":"SUCCESS","targetUrl":"https://ci/2"}]}}}}]}}]}}}}`
	issueAnswer = `{"data":{"repository":{"item":{"__typename":"Issue","number":3,"title":"Say hello","state":"OPEN",
"url":"https://github.com/o/r/issues/3","updatedAt":"2026-11-07T10:00:00Z","createdAt":"2026-11-07T09:00:00Z",
"author":{"__typename":"User","login":"owner"},"labels":{"nodes":[]},"body":"Add hello.txt.",
"comments":{"nodes":[{"author":{"__typename":"Bot","login":"slipway-bot"},"body":"Done.","createdAt":"2026-11-07T10:05:00Z",
"url":"https://github.com/o/r/issues/3#issuecomment-1"}]}}}}}`
)

// fakeGitHub answers like GitHub, and records what it was sent.
func fakeGitHub(t *testing.T, sent *[]string) *GitHub {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		*sent = append(*sent, r.Method+" "+r.URL.Path+" "+string(b))
		if r.Header.Get("Authorization") != "Bearer secret" {
			w.WriteHeader(401)
			fmt.Fprint(w, `{"message":"Bad credentials"}`)
			return
		}
		body := string(b)
		switch {
		case r.URL.Path == "/graphql" && strings.Contains(body, "query Issues"):
			fmt.Fprint(w, issuesAnswer)
		case r.URL.Path == "/graphql" && strings.Contains(body, "query PullRequests"):
			fmt.Fprint(w, pullsAnswer)
		case r.URL.Path == "/graphql" && strings.Contains(body, `"number":3`):
			fmt.Fprint(w, issueAnswer)
		case r.URL.Path == "/repos/o/r/issues/3/comments":
			w.WriteHeader(201)
			fmt.Fprint(w, `{}`)
		case r.URL.Path == "/repos/o/r/labels" && strings.Contains(body, `"framework"`):
			w.WriteHeader(422)
			fmt.Fprint(w, `{"message":"Validation Failed","errors":[{"resource":"Label","code":"already_exists","field":"name"}]}`)
		case r.URL.Path == "/repos/o/r/labels":
			w.WriteHeader(201)
			fmt.Fprint(w, `{}`)
		case r.URL.Path == "/repos/o/r/issues":
			w.WriteHeader(201)
			fmt.Fprint(w, `{"number":12}`)
		case r.URL.Path == "/user":
			fmt.Fprint(w, `{"login":"owner"}`)
		default:
			w.WriteHeader(404)
			fmt.Fprint(w, `{"message":"Not Found"}`)
		}
	}))
	t.Cleanup(srv.Close)
	settings := &Settings{db: desktop.TestDB(t)}
	settings.SetToken(" secret ")
	return &GitHub{settings: settings, api: srv.URL}
}

func TestGitHubIsAskedWithYourToken(t *testing.T) {
	var sent []string
	g := fakeGitHub(t, &sent)

	issues, err := g.Issues("o/r")
	if err != nil || len(issues) != 1 || issues[0].Author != "owner" || issues[0].State != "open" || len(issues[0].Labels) != 1 || issues[0].Labels[0] != "project:harbor" {
		t.Fatalf("issues: %+v %v", issues, err)
	}
	prs, err := g.PullRequests("o/r")
	if err != nil || len(prs) != 1 || prs[0].Checks != "pending" || !prs[0].Draft || prs[0].Branch != "harness/3" ||
		!prs[0].Fork || len(prs[0].Closes) != 1 || prs[0].Closes[0] != 3 || prs[0].Author != "slipway-bot[bot]" {
		t.Errorf("pull requests: %+v %v", prs, err)
	}
	c, err := g.Conversation("o/r", 3, false)
	if err != nil || c.Body != "Add hello.txt." || len(c.Comments) != 1 || c.Comments[0].Author != "slipway-bot[bot]" {
		t.Errorf("conversation: %+v %v", c, err)
	}
	if _, err := g.Conversation("o/r", 3, true); err == nil {
		t.Error("an issue was read as a pull request")
	}
	if _, err := g.Issues("not a repo"); err == nil {
		t.Error("a repository with no owner was asked for")
	}
	if login, err := g.Login(); err != nil || login != "owner" {
		t.Errorf("login: %q %v", login, err)
	}
	if err := g.Reply("o/r", 3, "  "); err == nil {
		t.Error("an empty reply was sent")
	}
	sent = nil
	if err := g.Reply("o/r", 3, "Make it say hello world."); err != nil {
		t.Fatal(err)
	}
	if len(sent) != 1 || sent[0] != `POST /repos/o/r/issues/3/comments {"body":"Make it say hello world."}` {
		t.Errorf("the reply sent was %q", sent)
	}
	// A scope is its label, which the repository gets if it has none yet.
	sent = nil
	for _, scope := range []string{"", "project:harbor", "framework"} {
		if n, err := g.NewIssue("o/r", "Add a page", "Please.", scope); err != nil || n != 12 {
			t.Errorf("new issue for %q: %d %v", scope, n, err)
		}
	}
	all := strings.Join(sent, "\n")
	for _, want := range []string{`POST /repos/o/r/labels {"color":"1d76db","description":"What a bot works on: see harness.yaml","name":"project:harbor"}`,
		`POST /repos/o/r/issues {"body":"Please.","labels":["project:harbor"],"title":"Add a page"}`,
		`POST /repos/o/r/issues {"body":"Please.","labels":["framework"],"title":"Add a page"}`,
		`POST /repos/o/r/issues {"body":"Please.","title":"Add a page"}`} {
		if !strings.Contains(all, want) {
			t.Errorf("GitHub was not sent %s:\n%s", want, all)
		}
	}
	if _, err := g.NewIssue("o/r", "Add a page", "Please.", "framework:desktop"); err == nil {
		t.Error("an issue was opened with a scope that is none")
	}

	// With a wrong token, or none, it says what to do.
	g.settings.SetToken("wrong")
	if _, err := g.Login(); err == nil || !strings.Contains(err.Error(), "refused the token") {
		t.Errorf("a wrong token: %v", err)
	}
	g.settings.SetToken("")
	if _, err := g.Issues("o/r"); err != errNoToken {
		t.Errorf("no token: %v", err)
	}
}

func TestGitTakesTheTokenFromTheEnvironment(t *testing.T) {
	// Git configuration from elsewhere, as a bot's shell has, is not git's.
	t.Setenv("GIT_CONFIG_COUNT", "2")
	t.Setenv("GIT_CONFIG_VALUE_0", "*")
	t.Setenv("GH_TOKEN", "someone else's")
	joined := strings.Join(gitEnv("secret"), "\n")
	for _, want := range []string{"GIT_CONFIG_KEY_0=http.https://github.com/.extraheader",
		"GIT_CONFIG_VALUE_0=Authorization: Basic eC1hY2Nlc3MtdG9rZW46c2VjcmV0", "GH_TOKEN=secret", "GIT_TERMINAL_PROMPT=0"} {
		if !strings.Contains(joined, want) {
			t.Errorf("no %s", want)
		}
	}
	if strings.Contains(joined, "GIT_CONFIG_COUNT=2") || strings.Contains(joined, "someone else's") {
		t.Error("git was given the environment's own configuration")
	}
	if strings.Contains(strings.Join(gitEnv(""), "\n"), "GIT_CONFIG_COUNT") {
		t.Error("no token, yet git was given one")
	}
}

func TestTheServersStreamReachesTheDatabase(t *testing.T) {
	lines := make(chan string, 4)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/version":
			fmt.Fprint(w, `{"api":1,"harness":"v0.4.0"}`)
		case "/api/v1/threads/o/r/3":
			fmt.Fprint(w, `{"thread":{"repo":"o/r","number":3,"state":"running"},"lines":[{"repo":"o/r","number":3,"time":"2026-11-07T10:00:00Z","kind":"prompt","text":"New activity"}]}`)
		case "/api/v1/live":
			w.Header().Set("Content-Type", "text/event-stream")
			w.(http.Flusher).Flush()
			for {
				select {
				case <-r.Context().Done():
					return
				case l := <-lines:
					fmt.Fprintf(w, "data: %s\n\n", l)
					w.(http.Flusher).Flush()
				}
			}
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	// The stream never ends by itself, and Close waits for every request.
	defer srv.CloseClientConnections()

	db := desktop.TestDB(t)
	settings := &Settings{db: db}
	s := &Server{db: db, settings: settings}
	if st := s.Status(); st.OK || st.Error == "" {
		t.Errorf("with no server: %+v", st)
	}
	settings.SetServer(srv.URL + "/")
	if st := s.Status(); !st.OK || st.Harness != "v0.4.0" {
		t.Errorf("status: %+v", st)
	}
	if th, err := s.Open("o/r", 3); err != nil || th.State != "running" {
		t.Errorf("open: %+v %v", th, err)
	}
	go s.stream()
	l, _ := json.Marshal(Line{Repo: "o/r", Number: 3, Time: time.Now(), Kind: "text", Text: "I did the work."})
	lines <- string(l)
	deadline := time.Now().Add(5 * time.Second)
	for {
		var n int
		db.QueryRow(`SELECT count(*) FROM lines WHERE repo = 'o/r' AND number = 3`).Scan(&n)
		if n == 2 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("%d lines in the database, want 2", n)
		}
		time.Sleep(20 * time.Millisecond)
	}
	// The same line twice is kept once.
	s.Open("o/r", 3)
	var n int
	db.QueryRow(`SELECT count(*) FROM lines`).Scan(&n)
	if n != 2 {
		t.Errorf("%d lines after opening the thread again, want 2", n)
	}
}

func TestLocalDrivesHarnessRun(t *testing.T) {
	var switched []string
	refreshed := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Desktop-Token") != "secret" {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		switch r.URL.Path {
		case "/api/state":
			// Pull request 9 shows up only once the list is refreshed.
			extra := ""
			if refreshed {
				extra = `,{"key":"pr-9","name":"new","pr":{"number":9}}`
			}
			fmt.Fprintf(w, `{"api":1,"app":"Harbor","mode":"prod","current":"pr-7","status":{"state":"running"},
"branches":[{"key":"","name":"main","home":true},{"key":"pr-7","name":"feature","pr":{"number":7,"title":"Add it","fork":true}}%s]}`, extra)
		case "/api/switch":
			var b struct {
				Key   string
				Trust bool
			}
			json.NewDecoder(r.Body).Decode(&b)
			switched = append(switched, fmt.Sprintf("%s %v", b.Key, b.Trust))
			w.WriteHeader(http.StatusAccepted)
		case "/api/refresh":
			refreshed = true
			w.WriteHeader(http.StatusAccepted)
		}
	}))
	defer srv.Close()

	l := &Local{root: t.TempDir(), runs: map[string]*localRun{}}
	if _, err := l.State("../etc"); err == nil {
		t.Error("a path that is not a repository has a state")
	}
	// A monorepo's clone, with two projects; the second runs.
	clone, _ := l.path("o/poc")
	os.MkdirAll(clone, 0o755)
	os.WriteFile(filepath.Join(clone, "harness.yaml"), []byte("projects:\n  app: projects/app\n  harbor: { path: projects/harbor }\n"), 0o644)
	project := filepath.Join(clone, "projects", "harbor")
	if rs, err := l.State("o/poc"); err != nil || len(rs) != 2 || rs[0].Running || rs[0].Phase != "" || rs[0].Project != "app" || rs[1].Project != "harbor" || len(rs[0].Projects) != 2 {
		t.Errorf("with no run: %+v %v", rs, err)
	}
	os.MkdirAll(filepath.Join(project, ".desktop"), 0o755)
	os.WriteFile(filepath.Join(project, ".desktop", "run.json"),
		[]byte(fmt.Sprintf(`{"api":1,"url":%q,"token":"secret","pid":%d}`, srv.URL, os.Getpid())), 0o600)
	rs, err := l.State("o/poc")
	if err != nil || len(rs) != 2 || rs[0].Running {
		t.Fatalf("state: %+v %v", rs, err)
	}
	if r := rs[1]; !r.Running || r.Project != "harbor" || r.PR != 7 || len(r.Branches) != 2 || !r.Branches[1].PR.Fork {
		t.Fatalf("harbor's state: %+v", r)
	}
	// Running what runs already does nothing; main switches to the clone's
	// own checkout; a pull request missing from the list is looked for again
	// after a refresh.
	if err := l.Run("o/poc", "harbor", "", 7, true); err != nil || len(switched) != 0 {
		t.Errorf("running pull request 7 again: %v %v", switched, err)
	}
	if err := l.Run("o/poc", "", "", 0, false); err != nil || len(switched) != 1 || switched[0] != " false" {
		t.Errorf("running main: %v %v", switched, err)
	}
	if err := l.Run("o/poc", "harbor", "", 9, false); err != nil || !refreshed || len(switched) != 2 || switched[1] != "pr-9 false" {
		t.Errorf("running a new pull request: %v %v", switched, err)
	}
}

func TestLocalRunsTheHarnessTheClonePins(t *testing.T) {
	root := t.TempDir()
	l := &Local{root: filepath.Join(root, "clones")}
	clone, _ := l.path("o/poc")
	os.MkdirAll(clone, 0o755)
	// One installed already is used as it is.
	bin := filepath.Join(root, "harness", "v9.8.7", "harness")
	os.MkdirAll(filepath.Dir(bin), 0o755)
	os.WriteFile(bin, []byte("#!/bin/sh\n"), 0o755)
	os.WriteFile(filepath.Join(clone, "harness.yaml"), []byte("harness: v9.8.7\nprojects: {}\n"), 0o644)
	if got, err := l.harness(clone, io.Discard); err != nil || got != bin {
		t.Errorf("the pinned harness: %q %v", got, err)
	}
	os.WriteFile(filepath.Join(clone, "harness.yaml"), []byte("harness: latest\n"), 0o644)
	if _, err := l.harness(clone, io.Discard); err == nil {
		t.Error("a pin that is not a version was installed")
	}
}

func TestLocalClonesAProjectTheFirstTime(t *testing.T) {
	bin := t.TempDir()
	// git clones into the folder it is given; the fake makes a checkout,
	// and, as GitHub does for a private repository, wants the token.
	os.WriteFile(filepath.Join(bin, "git"), []byte("#!/bin/sh\n[ -n \"$GIT_CONFIG_VALUE_0\" ] || exit 128\necho \"$@ $GIT_CONFIG_VALUE_0\" >> \"$FAKE\"\nmkdir -p \"$3/.git\"\n"), 0o755)
	t.Setenv("FAKE", filepath.Join(bin, "calls"))
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("GIT_CONFIG_COUNT", "1")
	t.Setenv("GIT_CONFIG_VALUE_0", "*")
	settings := &Settings{db: desktop.TestDB(t)}
	l := &Local{root: t.TempDir(), settings: settings}
	path, _ := l.path("o/harbor")
	var log strings.Builder
	if err := l.ensure("o/harbor", path, true, &log); err == nil || !strings.Contains(err.Error(), "token") {
		t.Errorf("with no token: %v", err)
	}
	settings.SetToken("secret")
	if err := l.ensure("o/harbor", path, true, &log); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(filepath.Join(bin, "calls")); string(b) != "clone https://github.com/o/harbor.git "+path+" Authorization: Basic eC1hY2Nlc3MtdG9rZW46c2VjcmV0\n" {
		t.Errorf("git was asked %q", b)
	}
	if _, err := os.Stat(filepath.Join(path, ".git")); err != nil {
		t.Errorf("no clone: %v", err)
	}
}

func TestNewer(t *testing.T) {
	for _, c := range []struct {
		a, b string
		want bool
	}{{"v0.4.0", "v0.3.9", true}, {"v0.10.0", "v0.9.0", true}, {"v0.4.0", "v0.4.0", false}, {"v0.3.0", "v1.0.0", false}} {
		if got := newer(c.a, c.b); got != c.want {
			t.Errorf("newer(%s, %s) = %v", c.a, c.b, got)
		}
	}
}
