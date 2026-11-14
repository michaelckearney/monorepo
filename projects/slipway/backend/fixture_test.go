package backend

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/michaelckearney/monorepo/framework"
	"github.com/michaelckearney/monorepo/projects/slipway/backend/fixture"
)

// The app's own code, asking the fixtures as a dev build does: what the
// e2e tests and recordings see.
func TestTheAppReadsAndWritesTheFixtures(t *testing.T) {
	f, err := fixture.Start()
	if err != nil {
		t.Fatal(err)
	}
	defer f.Stop()
	was := transport
	transport = f.Transport(http.DefaultTransport)
	defer func() { transport = was }()

	db := desktop.TestDB(t)
	settings := &Settings{db: db}
	settings.SetServer(fixture.Server)
	settings.SetToken(fixture.Token)
	s := &Server{db: db, settings: settings}
	g := &GitHub{settings: settings}

	if st := s.Status(); !st.OK || st.Harness != "fixture" {
		t.Fatalf("status: %+v", st)
	}
	projects, err := s.Projects()
	if err != nil || len(projects) != 1 || projects[0].Repo != fixture.Repo || len(projects[0].Projects) != 2 {
		t.Fatalf("projects: %+v %v", projects, err)
	}
	threads, err := s.Threads("")
	states := map[string]bool{}
	for _, th := range threads {
		states[th.State] = true
	}
	if err != nil || !states["running"] || !states["idle"] || !states["blocked"] {
		t.Errorf("threads: %+v %v", threads, err)
	}
	if th, err := s.Open(fixture.Repo, 11); err != nil || th.PR != 12 {
		t.Errorf("open: %+v %v", th, err)
	}
	var lines int
	db.QueryRow(`SELECT count(*) FROM lines WHERE number = 11`).Scan(&lines)
	if lines == 0 {
		t.Error("opening a thread kept none of its lines")
	}

	if login, err := g.Login(); err != nil || login != fixture.Login {
		t.Errorf("login: %q %v", login, err)
	}
	issues, err := g.Issues(fixture.Repo)
	if err != nil || len(issues) < 4 {
		t.Fatalf("issues: %+v %v", issues, err)
	}
	prs, err := g.PullRequests(fixture.Repo)
	checks := map[string]bool{}
	for _, p := range prs {
		checks[p.Checks] = true
	}
	if err != nil || !checks["passed"] || !checks["pending"] || !checks["failed"] {
		t.Errorf("pull requests: %+v %v", prs, err)
	}
	if c, err := g.Conversation(fixture.Repo, 12, true); err != nil || len(c.CheckRuns) != 2 || c.Author != "slipway-bot[bot]" {
		t.Errorf("pull request: %+v %v", c, err)
	}

	// A reply is kept, and starts the thread's bot.
	if err := g.Reply(fixture.Repo, 11, "Thanks, merging."); err != nil {
		t.Fatal(err)
	}
	c, err := g.Conversation(fixture.Repo, 11, false)
	if err != nil || c.Comments[len(c.Comments)-1].Body != "Thanks, merging." || c.Comments[len(c.Comments)-1].Author != fixture.Login {
		t.Errorf("after the reply: %+v %v", c, err)
	}
	if th, _ := s.Open(fixture.Repo, 11); th.State != "running" {
		t.Errorf("the reply started no turn: %+v", th)
	}
	// A new issue, with a scope the repository has no label for yet.
	n, err := g.NewIssue(fixture.Repo, "Add a log view", "Please.", "project:docs")
	if err != nil {
		t.Fatal(err)
	}
	if c, err := g.Conversation(fixture.Repo, n, false); err != nil || c.Title != "Add a log view" || c.Labels[0] != "project:docs" {
		t.Errorf("the new issue: %+v %v", c, err)
	}
	// Retry and Stop work on the server's threads.
	if err := s.Retry(fixture.Repo, 11); err == nil || !strings.Contains(err.Error(), "running") {
		t.Errorf("retrying a running thread: %v", err)
	}
	if err := s.Stop(fixture.Repo, 11); err != nil {
		t.Errorf("stop: %v", err)
	}
	if err := s.Retry(fixture.Repo, 11); err != nil {
		t.Errorf("retry: %v", err)
	}

	// The made-up GitHub refuses a wrong token, as GitHub does.
	settings.SetToken("wrong")
	if _, err := g.Login(); err == nil {
		t.Error("a wrong token was taken")
	}
	// And the stream reaches the database.
	settings.SetToken(fixture.Token)
	go s.stream()
	time.Sleep(200 * time.Millisecond)
	g.Reply(fixture.Repo, 15, "Follow the system.")
	deadline := time.Now().Add(5 * time.Second)
	for {
		var n int
		db.QueryRow(`SELECT count(*) FROM lines WHERE number = 15 AND text LIKE '%commented%'`).Scan(&n)
		if n > 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the live stream brought no line of the new turn")
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// An image attached in a private repository shows, from the signed link in
// GitHub's rendering, as the window cannot fetch the attachment's own link.
func TestAnAttachedImageComesFromItsSignedLink(t *testing.T) {
	f, err := fixture.Start()
	if err != nil {
		t.Fatal(err)
	}
	defer f.Stop()
	was := transport
	transport = f.Transport(http.DefaultTransport)
	defer func() { transport = was }()

	settings := &Settings{db: desktop.TestDB(t)}
	settings.SetToken(fixture.Token)
	g := &GitHub{settings: settings}

	issue, err := g.Conversation(fixture.Repo, 13, false)
	if err != nil || len(issue.Comments) == 0 || !strings.Contains(issue.Comments[0].Body, fixture.Attachment) {
		t.Fatalf("the bot's comment does not attach the screenshot: %+v %v", issue.Comments, err)
	}
	data, err := g.Image(fixture.Repo, 13, fixture.Attachment)
	if err != nil || !strings.HasPrefix(data, "data:image/png;base64,") {
		t.Fatalf("image: %.60q %v", data, err)
	}
	if _, err := g.Image(fixture.Repo, 13, "https://example.com/a.png"); err == nil {
		t.Error("an image that is not attached on GitHub was fetched")
	}
}
