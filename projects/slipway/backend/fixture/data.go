package fixture

import (
	"fmt"
	"html"
	"regexp"
	"sort"
	"strings"
	"time"
)

// data is what the made-up server and GitHub hold: one monorepo, with a
// thread in each state, issues and pull requests in each state, and the
// bots' turns.
type data struct {
	issues  []*issue // and pull requests, which are issues with a PR
	threads []*thread
	lines   map[int][]line
	labels  map[string]bool
}

// thread and line are the server's, as its API writes them.
type thread struct {
	Repo    string    `json:"repo"`
	Number  int       `json:"number"`
	State   string    `json:"state"`
	Agent   string    `json:"agent"`
	Scope   string    `json:"scope"`
	PR      int       `json:"pr"`
	Session string    `json:"session"`
	Started time.Time `json:"started"`
	Waiting int       `json:"waiting"`
}

type line struct {
	Repo   string    `json:"repo"`
	Number int       `json:"number"`
	Time   time.Time `json:"time"`
	Kind   string    `json:"kind"`
	Text   string    `json:"text"`
}

// actor is who wrote something, as GitHub's GraphQL API names them: a
// GitHub App is a Bot, with no [bot] in its login.
type actor struct {
	Type  string `json:"__typename"`
	Login string `json:"login"`
}

func user(login string) actor { return actor{"User", login} }

var bot = actor{"Bot", "slipway-bot"}

type issue struct {
	Number   int
	Title    string
	Body     string
	Author   actor
	Labels   []string
	Created  time.Time
	Updated  time.Time
	Comments []comment
	PR       *pr
}

type comment struct {
	Author  actor
	Body    string
	Created time.Time
	URL     string
}

type pr struct {
	Draft     bool
	Branch    string
	Additions int
	Deletions int
	Closes    []int
	Checks    []check
}

// check is a check run: its status, then its conclusion once completed.
type check struct {
	Name, Status, Conclusion string
}

func (i *issue) url() string {
	kind := "issues"
	if i.PR != nil {
		kind = "pull"
	}
	return fmt.Sprintf("https://github.com/%s/%s/%d", Repo, kind, i.Number)
}

// node is the issue as the app's GraphQL queries ask for it, with its body
// and comments if full.
func (i *issue) node(full bool) map[string]any {
	labels := []any{}
	for _, l := range i.Labels {
		labels = append(labels, map[string]any{"name": l})
	}
	n := map[string]any{"__typename": "Issue", "number": i.Number, "title": i.Title, "state": "OPEN", "url": i.url(),
		"updatedAt": i.Updated, "createdAt": i.Created, "author": i.Author, "labels": map[string]any{"nodes": labels}}
	if p := i.PR; p != nil {
		closes, checks := []any{}, []any{}
		for _, c := range p.Closes {
			closes = append(closes, map[string]any{"number": c})
		}
		for _, c := range p.Checks {
			var conclusion any
			if c.Conclusion != "" {
				conclusion = c.Conclusion
			}
			checks = append(checks, map[string]any{"__typename": "CheckRun", "name": c.Name, "status": c.Status,
				"conclusion": conclusion, "detailsUrl": fmt.Sprintf("https://github.com/%s/actions/runs/%d", Repo, 1000+i.Number)})
		}
		var rollup any
		if len(checks) > 0 {
			rollup = map[string]any{"contexts": map[string]any{"nodes": checks}}
		}
		n["__typename"] = "PullRequest"
		n["isDraft"], n["headRefName"], n["isCrossRepository"] = p.Draft, p.Branch, false
		n["additions"], n["deletions"] = p.Additions, p.Deletions
		n["closingIssuesReferences"] = map[string]any{"nodes": closes}
		n["commits"] = map[string]any{"nodes": []any{map[string]any{"commit": map[string]any{"statusCheckRollup": rollup}}}}
	}
	if full {
		comments := []any{}
		for _, c := range i.Comments {
			comments = append(comments, map[string]any{"author": c.Author, "body": c.Body, "bodyHTML": rendered(c.Body), "createdAt": c.Created, "url": c.URL})
		}
		n["body"] = i.Body
		n["bodyHTML"] = rendered(i.Body)
		n["comments"] = map[string]any{"nodes": comments}
	}
	return n
}

// attached is a file attached on GitHub, as Markdown names it.
var attached = regexp.MustCompile(`https://github\.com/user-attachments/assets/([0-9a-f-]{36})`)

// rendered is Markdown as GitHub renders it for a private repository, near
// enough: each attachment's link becomes a signed link to the file, which
// only that HTML has.
func rendered(body string) string {
	out := "<p>" + html.EscapeString(body) + "</p>"
	return attached.ReplaceAllStringFunc(out, func(m string) string {
		id := attached.FindStringSubmatch(m)[1]
		return fmt.Sprintf(`<img src="https://%s/1/%s.png?jwt=%s&amp;expires=1">`, imagesHost, id, signature)
	})
}

// imageID is the attachment's id in a signed link's path.
func imageID(path string) string {
	return strings.TrimSuffix(path[strings.LastIndex(path, "/")+1:], ".png")
}

func (d *data) find(n int) *issue {
	for _, i := range d.issues {
		if i.Number == n {
			return i
		}
	}
	return nil
}

func (d *data) next() int {
	n := 0
	for _, i := range d.issues {
		n = max(n, i.Number)
	}
	return n + 1
}

// sorted is the issues and pull requests, most recently updated first.
func (d *data) sorted() []*issue {
	out := append([]*issue{}, d.issues...)
	sort.SliceStable(out, func(a, b int) bool { return out[a].Updated.After(out[b].Updated) })
	return out
}

// seed is the made-up monorepo, as of now: every time is minutes before it,
// so the app shows the same ages each start.
func seed(now time.Time) *data {
	ago := func(minutes int) time.Time {
		return now.Add(-time.Duration(minutes) * time.Minute).Truncate(time.Second)
	}
	me := user(Login)
	d := &data{lines: map[int][]line{}, labels: map[string]bool{
		"project:harbor": true, "project:slipway": true, "framework": true, "agent:claude-code": true,
	}}
	d.issues = []*issue{
		{Number: 11, Title: "Harbor's README still describes the old layout", Author: me, Labels: []string{"project:harbor"},
			Created: ago(180), Updated: ago(70),
			Body: "The README says Harbor is its own repository. It is a project in the monorepo now: say so, and link the framework.",
			Comments: []comment{{Author: bot, Created: ago(70),
				Body: "I've opened #12, which rewrites the README's first section for the monorepo and links `framework/`."}}},
		{Number: 12, Title: "docs: describe Harbor's layout in the monorepo", Author: bot, Labels: []string{"project:harbor"},
			Created: ago(75), Updated: ago(70), Body: "The README now says Harbor is a project in the monorepo.\n\n---\nCloses #11.",
			PR: &pr{Branch: "harness/11", Additions: 6, Deletions: 10, Closes: []int{11},
				Checks: []check{{"check / test", "COMPLETED", "SUCCESS"}, {"check / lint", "COMPLETED", "SUCCESS"}}}},
		{Number: 13, Title: "Indent the New issue buttons", Author: me, Labels: []string{"project:slipway", "agent:claude-code"},
			Created: ago(30), Updated: ago(4),
			Body: "Line the sidebar's \"+ New issue\" buttons up with the items under each project.",
			Comments: []comment{{Author: bot, Created: ago(19),
				Body: "I've opened #14. Each \"+ New issue\" now starts at its project's indent:\n\n![The sidebar, each New issue at its project's indent](" + Attachment + ")"}}},
		{Number: 14, Title: "fix: indent the sidebar's New issue buttons", Author: bot, Labels: []string{"project:slipway"},
			Created: ago(20), Updated: ago(4), Body: "Each \"+ New issue\" now starts at the indent of its project's items.\n\n---\nCloses #13.",
			PR: &pr{Draft: true, Branch: "harness/13", Additions: 1, Deletions: 1, Closes: []int{13},
				Checks: []check{{"check / test", "IN_PROGRESS", ""}, {"check / lint", "COMPLETED", "SUCCESS"}}}},
		{Number: 15, Title: "Add a dark theme to Harbor", Author: me, Labels: []string{"project:harbor"},
			Created: ago(300), Updated: ago(40),
			Body: "Harbor should have a dark theme.",
			Comments: []comment{{Author: bot, Created: ago(40),
				Body: "Should the dark theme follow the system's appearance, or be a setting of its own? I'll wait for your answer before I start."}}},
		{Number: 16, Title: "Cache the framework's e2e builds", Author: me, Labels: []string{"framework"},
			Created: ago(600), Updated: ago(600),
			Body: "Each e2e run builds the frontend again, even when nothing in it changed."},
		{Number: 17, Title: "Jump the timeline to its top", Author: me, Labels: []string{"project:harbor"},
			Created: ago(90), Updated: ago(6),
			Body: "Harbor's timeline has no way back to the top of a long thread.",
			Comments: []comment{{Author: bot, Created: ago(8),
				Body: "I've read the thread and know what to change; I'll start when the others ahead of me are done."}}},
		{Number: 18, Title: "chore: try the harness's next release", Author: me, Labels: []string{},
			Created: ago(120), Updated: ago(100), Body: "Pins the harness's release candidate, to see what breaks.",
			PR: &pr{Branch: "try-harness-next", Additions: 2, Deletions: 2,
				Checks: []check{{"check / test", "COMPLETED", "FAILURE"}, {"check / lint", "COMPLETED", "SUCCESS"}}}},
		{Number: 19, Title: "Notes on naming the releases", Author: me, Labels: []string{},
			Created: ago(1440), Updated: ago(1440), Body: "Not a bot's: just notes."},
	}
	for _, i := range d.issues {
		for n := range i.Comments {
			i.Comments[n].URL = fmt.Sprintf("%s#issuecomment-%d", i.url(), n+1)
		}
	}
	newThread := func(number int, state, agent, scope string, pr, started int) *thread {
		return &thread{Repo: Repo, Number: number, State: state, Agent: agent, Scope: scope, PR: pr,
			Session: fmt.Sprintf("fixture-%d", number), Started: ago(started)}
	}
	d.threads = []*thread{
		newThread(11, "idle", "opencode", "project:harbor", 12, 80),
		newThread(13, "running", "claude-code", "project:slipway", 14, 25),
		newThread(15, "blocked", "opencode", "project:harbor", 0, 45),
		newThread(17, "idle", "opencode", "project:harbor", 0, 20),
	}
	// #17's turn is over and the thread is idle, but new events have come in
	// since, so its item shows that state of the status line rather than
	// waiting for you.
	d.threads[3].Waiting = 3
	say := func(number, minutes int, kind, text string) {
		d.lines[number] = append(d.lines[number], line{Repo: Repo, Number: number, Time: ago(minutes), Kind: kind, Text: text})
	}
	prompt := func(number, minutes int, agent, title, what string) {
		say(number, minutes, "prompt", fmt.Sprintf("=== %s %s session fixture-%d\nNew activity on %s issue #%d: %s\n\n- %s\n\nStart with `harness status`, which shows these in full. Answer with `harness reply`, then end this turn with `harness done`.",
			ago(minutes).Format(time.RFC3339), agent, number, Repo, number, title, what))
	}
	// opencode's lines name the tool on its result too; Claude Code's do not.
	status := `bash: {"command":"harness status"}`

	prompt(11, 80, "opencode", "Harbor's README still describes the old layout", "@alex-morgan opened the issue.")
	say(11, 79, "tool", status)
	say(11, 79, "result", "bash: harness status\nIssue #11: Harbor's README still describes the old layout\nBranch: harness/11\nPull request: none yet")
	say(11, 76, "tool", `bash: {"command":"harness commit \"docs: describe Harbor's layout in the monorepo\""}`)
	say(11, 75, "result", "bash: harness commit \"docs: describe Harbor's layout in the monorepo\"\nPushed harness/11, and opened #12.")
	say(11, 72, "tool", `bash: {"command":"harness test"}`)
	say(11, 71, "result", "bash: harness test\nlint: passed\nunit: passed\ne2e: passed")
	say(11, 70, "text", "I've opened #12, which rewrites the README's first section for the monorepo and links `framework/`.")

	prompt(13, 25, "claude-code", "Indent the New issue buttons", "@alex-morgan opened the issue.")
	say(13, 24, "tool", `Bash: {"command":"harness status","description":"Show issue status"}`)
	say(13, 24, "result", "Issue #13: Indent the New issue buttons\nBranch: harness/13\nPull request: none yet")
	say(13, 22, "text", "The plus icon is pulled 22px to the left with `-ml-5.5`. I'll remove that so it lines up with the items.")
	say(13, 20, "tool", `Bash: {"command":"harness commit \"fix: indent the sidebar's New issue buttons\"","description":"Commit and push"}`)
	say(13, 20, "result", "Pushed harness/13, and opened #14 as a draft.")
	say(13, 4, "tool", `Bash: {"command":"harness test","description":"Test the change"}`)

	prompt(15, 45, "opencode", "Add a dark theme to Harbor", "@alex-morgan opened the issue.")
	say(15, 44, "tool", status)
	say(15, 44, "result", "bash: harness status\nIssue #15: Add a dark theme to Harbor\nBranch: harness/15\nPull request: none yet")
	say(15, 41, "tool", `bash: {"command":"harness reply -"}`)
	say(15, 41, "result", "bash: harness reply -\nReplied on #15.")
	say(15, 40, "text", "I asked whether the dark theme should follow the system's appearance, and I'm waiting for an answer.")

	prompt(17, 20, "opencode", "Jump the timeline to its top", "@alex-morgan opened the issue.")
	say(17, 19, "tool", status)
	say(17, 19, "result", "bash: harness status\nIssue #17: Jump the timeline to its top\nBranch: harness/17\nPull request: none yet")
	say(17, 18, "tool", `bash: {"command":"harness reply -"}`)
	say(17, 18, "result", "bash: harness reply -\nReplied on #17.")
	say(17, 8, "text", "I've read the thread and know what to change; I'll start when the others ahead of me are done.")
	return d
}
