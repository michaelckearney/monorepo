package tasks

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/michaelckearney/monorepo/projects/harbor/backend/fixture/site"
)

const schema = `
schema { query: Query mutation: Mutation }

type Query {
  viewer: User!
  users: [User!]!
  projects: [Project!]!
  issues(first: Int = 20, after: String, project: String, state: IssueState, assignee: String): IssueConnection!
  issue(key: String!): Issue
}

type Mutation {
  createIssue(input: CreateIssueInput!): Issue!
  updateIssue(key: String!, input: UpdateIssueInput!): Issue!
}

enum IssueState { TODO IN_PROGRESS IN_REVIEW DONE }
enum Priority { LOW MEDIUM HIGH URGENT }

type User { username: String! name: String! }
type Project { key: String! name: String! }

type Issue {
  key: String!
  project: Project!
  title: String!
  description: String!
  state: IssueState!
  priority: Priority!
  assignee: User
  labels: [String!]!
  created: String!
  updated: String!
}

type IssueConnection { edges: [IssueEdge!]! pageInfo: PageInfo! totalCount: Int! }
type IssueEdge { cursor: String! node: Issue! }
type PageInfo { hasNextPage: Boolean! endCursor: String }

input CreateIssueInput {
  project: String!
  title: String!
  description: String
  priority: Priority
  assignee: String
  labels: [String!]
}

input UpdateIssueInput {
  title: String
  description: String
  state: IssueState
  priority: Priority
  assignee: String
}
`

// operations are the only queries the server runs, by their SHA-256 hash:
// Tasklane's app sends the hash, never the query text.
var operations = map[string]string{
	"Viewer":   `query Viewer { viewer { username name } }`,
	"Users":    `query Users { users { username name } }`,
	"Projects": `query Projects { projects { key name } }`,
	"Issues": `query Issues($first: Int, $after: String, $project: String, $state: IssueState, $assignee: String) {
  issues(first: $first, after: $after, project: $project, state: $state, assignee: $assignee) {
    totalCount
    pageInfo { hasNextPage endCursor }
    edges { cursor node { key title state priority labels updated project { key } assignee { username name } } }
  }
}`,
	"Issue":       `query Issue($key: String!) { issue(key: $key) { key title description state priority labels created updated project { key name } assignee { username name } } }`,
	"CreateIssue": `mutation CreateIssue($input: CreateIssueInput!) { createIssue(input: $input) { key title state priority } }`,
	"UpdateIssue": `mutation UpdateIssue($key: String!, $input: UpdateIssueInput!) { updateIssue(key: $key, input: $input) { key title state priority assignee { username } } }`,
}

func hash(q string) string {
	sum := sha256.Sum256([]byte(q))
	return hex.EncodeToString(sum[:])
}

// operationHashes is the name and hash of each operation, for the app.
func operationHashes() map[string]string {
	out := map[string]string{}
	for name, q := range operations {
		out[name] = hash(q)
	}
	return out
}

var byHash = func() map[string]string {
	out := map[string]string{}
	for _, q := range operations {
		out[hash(q)] = q
	}
	return out
}()

// ---- Data

type User struct {
	Username string `json:"username"`
	Name     string `json:"name"`
}

type Project struct {
	Key  string `json:"key"`
	Name string `json:"name"`
}

type Issue struct {
	Key         string   `json:"key"`
	Project     string   `json:"project"`
	Title       string   `json:"title"`
	Description string   `json:"description"`
	State       string   `json:"state"`
	Priority    string   `json:"priority"`
	Assignee    string   `json:"assignee"`
	Labels      []string `json:"labels"`
	Created     string   `json:"created"`
	Updated     string   `json:"updated"`
}

type store struct {
	mu       sync.Mutex
	Users    []User    `json:"users"`
	Projects []Project `json:"projects"`
	Issues   []*Issue  `json:"issues"`
}

func newStore() *store {
	s := &store{}
	site.Load(files, "data/tasks.json", s)
	return s
}

func (s *store) user(username string) *User {
	for i := range s.Users {
		if s.Users[i].Username == username {
			return &s.Users[i]
		}
	}
	return nil
}

func (s *store) project(key string) *Project {
	for i := range s.Projects {
		if s.Projects[i].Key == key {
			return &s.Projects[i]
		}
	}
	return nil
}

// ---- Resolvers

type resolver struct {
	t *tasks
}

type ctxKey struct{}

func viewer(ctx context.Context) string { return ctx.Value(ctxKey{}).(string) }

func (r *resolver) Viewer(ctx context.Context) *User { return r.t.data.user(viewer(ctx)) }
func (r *resolver) Users() []User                    { return r.t.data.Users }
func (r *resolver) Projects() []Project              { return r.t.data.Projects }

type issueResolver struct {
	i *Issue
	s *store
}

func (i issueResolver) Key() string         { return i.i.Key }
func (i issueResolver) Project() *Project   { return i.s.project(i.i.Project) }
func (i issueResolver) Title() string       { return i.i.Title }
func (i issueResolver) Description() string { return i.i.Description }
func (i issueResolver) State() string       { return i.i.State }
func (i issueResolver) Priority() string    { return i.i.Priority }
func (i issueResolver) Assignee() *User     { return i.s.user(i.i.Assignee) }
func (i issueResolver) Labels() []string    { return i.i.Labels }
func (i issueResolver) Created() string     { return i.i.Created }
func (i issueResolver) Updated() string     { return i.i.Updated }

type edge struct {
	cursor string
	node   issueResolver
}

func (e edge) Cursor() string      { return e.cursor }
func (e edge) Node() issueResolver { return e.node }

type pageInfo struct {
	next bool
	end  *string
}

func (p pageInfo) HasNextPage() bool  { return p.next }
func (p pageInfo) EndCursor() *string { return p.end }

type connection struct {
	edges []edge
	info  pageInfo
	total int32
}

func (c connection) Edges() []edge      { return c.edges }
func (c connection) PageInfo() pageInfo { return c.info }
func (c connection) TotalCount() int32  { return c.total }

func cursorOf(key string) string { return base64.StdEncoding.EncodeToString([]byte("issue:" + key)) }

func (r *resolver) Issues(args struct {
	First    int32 // 20 when not given
	After    *string
	Project  *string
	State    *string
	Assignee *string
}) (connection, error) {
	first := min(max(args.First, 1), 50)
	s := r.t.data
	s.mu.Lock()
	defer s.mu.Unlock()
	var all []*Issue
	for _, i := range s.Issues {
		if (args.Project == nil || i.Project == *args.Project) && (args.State == nil || i.State == *args.State) &&
			(args.Assignee == nil || i.Assignee == *args.Assignee) {
			all = append(all, i)
		}
	}
	// Newest change first.
	slices.SortStableFunc(all, func(a, b *Issue) int {
		if c := strings.Compare(b.Updated, a.Updated); c != 0 {
			return c
		}
		return strings.Compare(a.Key, b.Key)
	})
	start := 0
	if args.After != nil {
		start = -1
		for n, i := range all {
			if cursorOf(i.Key) == *args.After {
				start = n + 1
			}
		}
		if start < 0 {
			return connection{}, errors.New("after is not a cursor from this query")
		}
	}
	end := min(start+int(first), len(all))
	c := connection{total: int32(len(all)), edges: []edge{}}
	for _, i := range all[start:end] {
		cp := *i
		c.edges = append(c.edges, edge{cursor: cursorOf(i.Key), node: issueResolver{&cp, s}})
	}
	c.info.next = end < len(all)
	if len(c.edges) > 0 {
		last := c.edges[len(c.edges)-1].cursor
		c.info.end = &last
	}
	return c, nil
}

func (r *resolver) Issue(args struct{ Key string }) *issueResolver {
	s := r.t.data
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, i := range s.Issues {
		if i.Key == args.Key {
			cp := *i
			return &issueResolver{&cp, s}
		}
	}
	return nil
}

type createInput struct {
	Project     string
	Title       string
	Description *string
	Priority    *string
	Assignee    *string
	Labels      *[]string
}

func (r *resolver) CreateIssue(ctx context.Context, args struct{ Input createInput }) (issueResolver, error) {
	s := r.t.data
	s.mu.Lock()
	in := args.Input
	if s.project(in.Project) == nil {
		s.mu.Unlock()
		return issueResolver{}, fmt.Errorf("there is no project %s", in.Project)
	}
	if strings.TrimSpace(in.Title) == "" {
		s.mu.Unlock()
		return issueResolver{}, errors.New("title is required")
	}
	n := 0
	for _, i := range s.Issues {
		if i.Project == in.Project {
			n++
		}
	}
	now := time.Now().UTC().Format(time.RFC3339)
	i := &Issue{Key: fmt.Sprintf("%s-%d", in.Project, n+1), Project: in.Project, Title: in.Title, State: "TODO",
		Priority: "MEDIUM", Assignee: viewer(ctx), Labels: []string{}, Created: now, Updated: now}
	if in.Description != nil {
		i.Description = *in.Description
	}
	if in.Priority != nil {
		i.Priority = *in.Priority
	}
	if in.Assignee != nil {
		if s.user(*in.Assignee) == nil {
			s.mu.Unlock()
			return issueResolver{}, fmt.Errorf("there is no user %s", *in.Assignee)
		}
		i.Assignee = *in.Assignee
	}
	if in.Labels != nil {
		i.Labels = *in.Labels
	}
	s.Issues = append(s.Issues, i)
	cp := *i
	s.mu.Unlock()
	r.t.broadcast("issue.created", &cp)
	return issueResolver{&cp, s}, nil
}

type updateInput struct {
	Title       *string
	Description *string
	State       *string
	Priority    *string
	Assignee    *string
}

func (r *resolver) UpdateIssue(args struct {
	Key   string
	Input updateInput
}) (issueResolver, error) {
	s := r.t.data
	s.mu.Lock()
	var i *Issue
	for _, x := range s.Issues {
		if x.Key == args.Key {
			i = x
		}
	}
	if i == nil {
		s.mu.Unlock()
		return issueResolver{}, fmt.Errorf("there is no issue %s", args.Key)
	}
	in := args.Input
	if in.Assignee != nil && *in.Assignee != "" && s.user(*in.Assignee) == nil {
		s.mu.Unlock()
		return issueResolver{}, fmt.Errorf("there is no user %s", *in.Assignee)
	}
	for _, f := range []struct{ to, from *string }{{&i.Title, in.Title}, {&i.Description, in.Description}, {&i.State, in.State}, {&i.Priority, in.Priority}, {&i.Assignee, in.Assignee}} {
		if f.from != nil {
			*f.to = *f.from
		}
	}
	i.Updated = time.Now().UTC().Format(time.RFC3339)
	cp := *i
	s.mu.Unlock()
	r.t.broadcast("issue.updated", &cp)
	return issueResolver{&cp, s}, nil
}

// ---- The endpoint

type gqlError struct {
	Message    string            `json:"message"`
	Extensions map[string]string `json:"extensions,omitempty"`
}

func (t *tasks) graphql(w http.ResponseWriter, r *http.Request) {
	token, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	if !ok {
		token = ""
	}
	username := t.user(w, token)
	if username == "" {
		return
	}
	var in struct {
		OperationName string         `json:"operationName"`
		Query         string         `json:"query"`
		Variables     map[string]any `json:"variables"`
		Extensions    struct {
			PersistedQuery struct {
				Version int    `json:"version"`
				Hash    string `json:"sha256Hash"`
			} `json:"persistedQuery"`
		} `json:"extensions"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		site.JSON(w, 400, map[string]any{"errors": []gqlError{{Message: "The body is not JSON: " + err.Error()}}})
		return
	}
	if in.Query != "" {
		site.JSON(w, 400, map[string]any{"errors": []gqlError{{Message: "Tasklane runs persisted queries only: send extensions.persistedQuery.sha256Hash, not query.",
			Extensions: map[string]string{"code": "PERSISTED_QUERY_ONLY"}}}})
		return
	}
	query, ok := byHash[in.Extensions.PersistedQuery.Hash]
	if !ok {
		site.JSON(w, 200, map[string]any{"errors": []gqlError{{Message: "PersistedQueryNotFound", Extensions: map[string]string{"code": "PERSISTED_QUERY_NOT_FOUND"}}}})
		return
	}
	ctx := context.WithValue(r.Context(), ctxKey{}, username)
	site.JSON(w, 200, t.gql.Exec(ctx, query, in.OperationName, in.Variables))
}
