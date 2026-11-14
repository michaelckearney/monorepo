package backend

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"
)

// GitHub is GitHub as you, through its API with your token: it reads issues
// and pull requests, and posts your replies, so what you write comes from
// your account and not the bots' GitHub App.
type GitHub struct {
	settings *Settings
	// api is GitHub's API; tests set a fake.
	api string
}

// githubAPI is where GitHub's API is. A dev build's transport sends it to
// the fixtures (package fixture).
const githubAPI = "https://api.github.com"

// Issue is an issue or a pull request in a list.
type Issue struct {
	Number    int       `json:"number"`
	Title     string    `json:"title"`
	State     string    `json:"state"`
	Author    string    `json:"author"`
	URL       string    `json:"url"`
	UpdatedAt time.Time `json:"updatedAt"`
	// Checks is a pull request's checks: passed, failed, pending, or "".
	Checks string `json:"checks"`
	Draft  bool   `json:"draft"`
	Branch string `json:"branch"`
	// Fork is a pull request from another repository: running it runs
	// someone else's code on this computer.
	Fork      bool      `json:"fork"`
	CreatedAt time.Time `json:"createdAt"`
	Additions int       `json:"additions"`
	Deletions int       `json:"deletions"`
	// Closes is the issues a pull request says it closes, which is how the
	// app shows a pull request the bot did not open with its issue.
	Closes []int `json:"closes"`
	// Labels are its labels' names, such as project:harbor, which says what
	// it is for.
	Labels []string `json:"labels"`
}

// Check is one of a pull request's checks.
type Check struct {
	Name  string `json:"name"`
	State string `json:"state"` // passed, failed, pending, or skipped
	URL   string `json:"url"`
}

// Comment is one message in a conversation.
type Comment struct {
	Author    string    `json:"author"`
	Body      string    `json:"body"`
	CreatedAt time.Time `json:"createdAt"`
	URL       string    `json:"url"`
}

// Conversation is an issue with its comments.
type Conversation struct {
	Issue
	Body     string    `json:"body"`
	Comments []Comment `json:"comments"`
	// CheckRuns is a pull request's checks, one by one.
	CheckRuns []Check `json:"checkRuns"`
}

// The GraphQL queries. An issue's fields, then a pull request's.
const (
	issueFields = `number title state url updatedAt createdAt author { __typename login } labels(first: 20) { nodes { name } }`
	prFields    = issueFields + ` isDraft headRefName isCrossRepository additions deletions
		closingIssuesReferences(first: 10) { nodes { number } }
		commits(last: 1) { nodes { commit { statusCheckRollup { contexts(first: 100) { nodes {
			__typename
			... on CheckRun { name status conclusion detailsUrl }
			... on StatusContext { context state targetUrl }
		} } } } } }`
	comments = `body comments(first: 100) { nodes { author { __typename login } body createdAt url } }`

	issuesQuery = `query Issues($owner: String!, $name: String!) { repository(owner: $owner, name: $name) {
		list: issues(first: 100, states: OPEN, orderBy: {field: UPDATED_AT, direction: DESC}) { nodes { ` + issueFields + ` } } } }`
	pullsQuery = `query PullRequests($owner: String!, $name: String!) { repository(owner: $owner, name: $name) {
		list: pullRequests(first: 100, states: OPEN, orderBy: {field: UPDATED_AT, direction: DESC}) { nodes { ` + prFields + ` } } } }`
	conversationQuery = `query Conversation($owner: String!, $name: String!, $number: Int!) { repository(owner: $owner, name: $name) {
		item: issueOrPullRequest(number: $number) { __typename
			... on Issue { ` + issueFields + ` ` + comments + ` }
			... on PullRequest { ` + prFields + ` ` + comments + ` } } } }`
)

// actor is who wrote something. A GitHub App is a Bot, whose login has no
// [bot]: the app adds it, as GitHub's own pages do.
type actor struct {
	Type  string `json:"__typename"`
	Login string `json:"login"`
}

func (a actor) name() string {
	if a.Type == "Bot" {
		return a.Login + "[bot]"
	}
	return a.Login
}

// node is an issue or a pull request, as the queries ask for it.
type node struct {
	Type      string    `json:"__typename"`
	Number    int       `json:"number"`
	Title     string    `json:"title"`
	State     string    `json:"state"`
	URL       string    `json:"url"`
	UpdatedAt time.Time `json:"updatedAt"`
	CreatedAt time.Time `json:"createdAt"`
	Body      string    `json:"body"`
	Author    actor     `json:"author"`
	Labels    struct {
		Nodes []struct {
			Name string `json:"name"`
		} `json:"nodes"`
	} `json:"labels"`
	IsDraft     bool   `json:"isDraft"`
	HeadRefName string `json:"headRefName"`
	Fork        bool   `json:"isCrossRepository"`
	Additions   int    `json:"additions"`
	Deletions   int    `json:"deletions"`
	Closes      struct {
		Nodes []struct {
			Number int `json:"number"`
		} `json:"nodes"`
	} `json:"closingIssuesReferences"`
	Commits struct {
		Nodes []struct {
			Commit struct {
				Rollup *struct {
					Contexts struct {
						Nodes []ghCheck `json:"nodes"`
					} `json:"contexts"`
				} `json:"statusCheckRollup"`
			} `json:"commit"`
		} `json:"nodes"`
	} `json:"commits"`
	Comments struct {
		Nodes []struct {
			Author    actor     `json:"author"`
			Body      string    `json:"body"`
			CreatedAt time.Time `json:"createdAt"`
			URL       string    `json:"url"`
		} `json:"nodes"`
	} `json:"comments"`
}

// ghCheck is a check run (name, status, conclusion, detailsUrl) or a commit
// status (context, state, targetUrl): a status check rollup mixes the two.
type ghCheck struct {
	Name       string `json:"name"`
	Context    string `json:"context"`
	Conclusion string `json:"conclusion"`
	State      string `json:"state"`
	Status     string `json:"status"`
	DetailsURL string `json:"detailsUrl"`
	TargetURL  string `json:"targetUrl"`
}

func (c ghCheck) state() string {
	v := strings.ToUpper(c.Conclusion + c.State)
	switch {
	case v == "FAILURE" || v == "ERROR" || v == "TIMED_OUT" || v == "CANCELLED" || v == "ACTION_REQUIRED" || v == "STARTUP_FAILURE":
		return "failed"
	case c.Status != "" && c.Status != "COMPLETED", v == "PENDING" || v == "EXPECTED" || v == "":
		return "pending"
	case v == "SKIPPED" || v == "NEUTRAL" || v == "STALE":
		return "skipped"
	}
	return "passed"
}

// checks are the checks on a pull request's last commit.
func (n node) checks() []ghCheck {
	if len(n.Commits.Nodes) == 0 || n.Commits.Nodes[0].Commit.Rollup == nil {
		return nil
	}
	return n.Commits.Nodes[0].Commit.Rollup.Contexts.Nodes
}

func (n node) issue() Issue {
	i := Issue{Number: n.Number, Title: n.Title, State: strings.ToLower(n.State), Author: n.Author.name(), URL: n.URL,
		UpdatedAt: n.UpdatedAt, Checks: summary(n.checks()), Draft: n.IsDraft, Branch: n.HeadRefName, Fork: n.Fork,
		CreatedAt: n.CreatedAt, Additions: n.Additions, Deletions: n.Deletions, Closes: []int{}, Labels: []string{}}
	for _, c := range n.Closes.Nodes {
		i.Closes = append(i.Closes, c.Number)
	}
	for _, l := range n.Labels.Nodes {
		i.Labels = append(i.Labels, l.Name)
	}
	return i
}

// summary sums up a pull request's checks the way GitHub's list does.
func summary(checks []ghCheck) string {
	if len(checks) == 0 {
		return ""
	}
	out := "passed"
	for _, c := range checks {
		switch c.state() {
		case "failed":
			return "failed"
		case "pending":
			out = "pending"
		}
	}
	return out
}

// Issues lists a repository's open issues, most recently updated first.
func (g *GitHub) Issues(repo string) ([]Issue, error) {
	return g.list(issuesQuery, repo)
}

// PullRequests lists a repository's open pull requests, with their checks.
func (g *GitHub) PullRequests(repo string) ([]Issue, error) {
	return g.list(pullsQuery, repo)
}

func (g *GitHub) list(query, repo string) ([]Issue, error) {
	vars, err := repoVars(repo)
	if err != nil {
		return nil, err
	}
	var v struct {
		Repository *struct {
			List struct {
				Nodes []node `json:"nodes"`
			} `json:"list"`
		} `json:"repository"`
	}
	if err := g.graphql(query, vars, &v); err != nil {
		return nil, err
	}
	if v.Repository == nil {
		return nil, fmt.Errorf("GitHub has no repository %s that your token can see", repo)
	}
	out := []Issue{}
	for _, n := range v.Repository.List.Nodes {
		out = append(out, n.issue())
	}
	return out, nil
}

// Conversation reads an issue, or a pull request, and its comments.
func (g *GitHub) Conversation(repo string, number int, pr bool) (Conversation, error) {
	vars, err := repoVars(repo)
	if err != nil {
		return Conversation{}, err
	}
	vars["number"] = number
	var v struct {
		Repository *struct {
			Item *node `json:"item"`
		} `json:"repository"`
	}
	if err := g.graphql(conversationQuery, vars, &v); err != nil {
		return Conversation{}, err
	}
	if v.Repository == nil || v.Repository.Item == nil || (v.Repository.Item.Type == "PullRequest") != pr {
		return Conversation{}, fmt.Errorf("%s has no such %s #%d", repo, map[bool]string{true: "pull request", false: "issue"}[pr], number)
	}
	n := v.Repository.Item
	c := Conversation{Issue: n.issue(), Body: n.Body, Comments: []Comment{}, CheckRuns: []Check{}}
	for _, k := range n.checks() {
		name, url := k.Name, k.DetailsURL
		if name == "" {
			name, url = k.Context, k.TargetURL
		}
		c.CheckRuns = append(c.CheckRuns, Check{Name: name, State: k.state(), URL: url})
	}
	for _, m := range n.Comments.Nodes {
		c.Comments = append(c.Comments, Comment{Author: m.Author.name(), Body: m.Body, CreatedAt: m.CreatedAt, URL: m.URL})
	}
	return c, nil
}

// Reply comments on an issue or pull request, as you.
func (g *GitHub) Reply(repo string, number int, body string) error {
	if strings.TrimSpace(body) == "" {
		return errors.New("write something first")
	}
	return g.rest("POST", fmt.Sprintf("/repos/%s/issues/%d/comments", repo, number), map[string]any{"body": body}, nil)
}

// NewIssue opens an issue, as you, and returns its number. scope is its
// scope label, project:<name> or framework, which is what a bot works on;
// "" opens an issue no bot works.
func (g *GitHub) NewIssue(repo, title, body, scope string) (int, error) {
	if strings.TrimSpace(title) == "" {
		return 0, errors.New("give the issue a title")
	}
	issue := map[string]any{"title": title, "body": body}
	if scope != "" {
		if !scopeLabel.MatchString(scope) {
			return 0, fmt.Errorf("%q is not a scope; a scope is project:<name> or framework", scope)
		}
		// The first issue of a project makes its label, so it has the
		// colour and description the others do.
		err := g.rest("POST", "/repos/"+repo+"/labels", map[string]any{"name": scope, "color": "1d76db",
			"description": "What a bot works on: see harness.yaml"}, nil)
		if err != nil && !strings.Contains(err.Error(), "already_exists") {
			return 0, err
		}
		issue["labels"] = []string{scope}
	}
	var out struct {
		Number int `json:"number"`
	}
	if err := g.rest("POST", "/repos/"+repo+"/issues", issue, &out); err != nil {
		return 0, err
	}
	return out.Number, nil
}

var scopeLabel = regexp.MustCompile(`^(framework|project:[a-z0-9][a-z0-9_-]{0,63})$`)

// Login is whose token the app has, or an error saying what to do.
func (g *GitHub) Login() (string, error) {
	var v struct {
		Login string `json:"login"`
	}
	if err := g.rest("GET", "/user", nil, &v); err != nil {
		return "", err
	}
	return v.Login, nil
}

var errNoToken = errors.New("no GitHub token: add one in Settings")

func repoVars(repo string) (map[string]any, error) {
	owner, name, ok := strings.Cut(repo, "/")
	if !ok || owner == "" || name == "" || strings.Contains(name, "/") {
		return nil, fmt.Errorf("%q is not a repository, such as owner/name", repo)
	}
	return map[string]any{"owner": owner, "name": name}, nil
}

// graphql asks GitHub's GraphQL API, and reads its data into out.
func (g *GitHub) graphql(query string, vars map[string]any, out any) error {
	var v struct {
		Data   json.RawMessage `json:"data"`
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}
	if err := g.rest("POST", "/graphql", map[string]any{"query": query, "variables": vars}, &v); err != nil {
		return err
	}
	if len(v.Errors) > 0 {
		return errors.New("GitHub: " + v.Errors[0].Message)
	}
	return json.Unmarshal(v.Data, out)
}

// rest sends body as JSON, and reads the JSON answer into out, if any.
func (g *GitHub) rest(method, path string, body, out any) error {
	resp, err := g.do(method, path, body, "application/vnd.github+json", 30*time.Second)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if out == nil {
		return nil
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

// do sends a request with your token. A failure is GitHub's own message.
func (g *GitHub) do(method, path string, body any, accept string, timeout time.Duration) (*http.Response, error) {
	token := g.settings.token()
	if token == "" {
		return nil, errNoToken
	}
	var r io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		r = bytes.NewReader(b)
	}
	api := g.api
	if api == "" {
		api = githubAPI
	}
	if strings.HasPrefix(path, "https://") {
		api = ""
	}
	req, err := http.NewRequest(method, api+path, r)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", accept)
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := httpClient(timeout).Do(req)
	if err != nil {
		return nil, errors.New("cannot reach GitHub: is this computer online?")
	}
	if resp.StatusCode/100 == 2 {
		return resp, nil
	}
	defer resp.Body.Close()
	var v struct {
		Message string `json:"message"`
		Errors  []struct {
			Code string `json:"code"`
		} `json:"errors"`
	}
	json.NewDecoder(resp.Body).Decode(&v)
	switch {
	case resp.StatusCode == http.StatusUnauthorized:
		return nil, errors.New("GitHub refused the token: add a new one in Settings")
	case len(v.Errors) > 0 && v.Errors[0].Code != "":
		return nil, fmt.Errorf("GitHub: %s (%s)", v.Message, v.Errors[0].Code)
	case v.Message != "":
		return nil, errors.New("GitHub: " + v.Message)
	}
	return nil, errors.New("GitHub: " + resp.Status)
}

// download writes a release asset, by its API URL, to w.
func (g *GitHub) download(url string, w io.Writer) error {
	resp, err := g.do("GET", url, nil, "application/octet-stream", 10*time.Minute)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	_, err = io.Copy(w, resp.Body)
	return err
}
