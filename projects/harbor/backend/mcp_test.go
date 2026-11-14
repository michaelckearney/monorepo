package backend

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"path/filepath"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/michaelckearney/monorepo/framework"
	"github.com/michaelckearney/monorepo/framework/browser"
	"github.com/michaelckearney/monorepo/projects/harbor/backend/engine"
	"github.com/michaelckearney/monorepo/projects/harbor/backend/service"
)

// connect returns a client of Harbor's MCP server, on a new database.
func connect(t *testing.T) *mcp.ClientSession {
	db := desktop.TestDB(t)
	server := newMCPServer(&Documents{db: db, icons: newIcons(db)}, &Tools{db: db})
	ct, st := mcp.NewInMemoryTransports()
	if _, err := server.Connect(t.Context(), st, nil); err != nil {
		t.Fatal(err)
	}
	cs, err := mcp.NewClient(&mcp.Implementation{Name: "test"}, nil).Connect(t.Context(), ct, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cs.Close() })
	return cs
}

// call calls a tool, and returns its structured output, or its error text.
func call(t *testing.T, cs *mcp.ClientSession, tool string, args map[string]any) (map[string]any, string) {
	t.Helper()
	res, err := cs.CallTool(t.Context(), &mcp.CallToolParams{Name: tool, Arguments: args})
	if err != nil {
		t.Fatalf("%s: %v", tool, err)
	}
	if res.IsError {
		return nil, res.Content[0].(*mcp.TextContent).Text
	}
	data, _ := json.Marshal(res.StructuredContent)
	var out map[string]any
	json.Unmarshal(data, &out)
	return out, ""
}

// sentence calls a tool whose result is a sentence, and returns it.
func sentence(t *testing.T, cs *mcp.ClientSession, tool string, args map[string]any) string {
	t.Helper()
	res, err := cs.CallTool(t.Context(), &mcp.CallToolParams{Name: tool, Arguments: args})
	if err != nil || res.IsError || res.StructuredContent != nil || len(res.Content) != 1 {
		t.Fatalf("%s: %+v, %v", tool, res, err)
	}
	return res.Content[0].(*mcp.TextContent).Text
}

func TestMCPServerSearchesTheKit(t *testing.T) {
	cs := connect(t)
	out, problem := call(t, cs, "search_kit", map[string]any{"query": "messages from the doctor"})
	if matches, _ := out["matches"].([]any); problem != "" || len(matches) == 0 || matches[0].(map[string]any)["name"] != "InboxWidget" {
		t.Errorf("search_kit: %v %s", out, problem)
	}
	if text := sentence(t, cs, "search_kit", map[string]any{"query": "zzz"}); !strings.HasPrefix(text, `Nothing in the kit matches "zzz".`) {
		t.Errorf("search_kit with no match: %q", text)
	}
	// A widget's props, with the shape of its data and the frame's props.
	out, problem = call(t, cs, "read_kit", map[string]any{"name": "InboxWidget"})
	if props, _ := out["props"].(string); problem != "" || out["kind"] != "widget" || !strings.Contains(props, "messages: Message[];") ||
		!strings.Contains(props, "export interface Message {") || !strings.Contains(props, "search?: string | boolean;") || len(out["examples"].([]any)) == 0 {
		t.Errorf("read_kit: %v %s", out, problem)
	}
	if _, problem := call(t, cs, "read_kit", map[string]any{"name": "Inbox"}); problem != "the kit has no Inbox; did you mean InboxWidget?" {
		t.Errorf("read_kit of a name it lacks: %q", problem)
	}
}

func TestMCPServerSavesAndReads(t *testing.T) {
	cs := connect(t)

	tools, err := cs.ListTools(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, tool := range tools.Tools {
		names = append(names, tool.Name)
	}
	if got := strings.Join(names, " "); got != "browser_clear_data browser_click browser_evaluate browser_fill_form browser_handle_dialog browser_navigate browser_navigate_back browser_network_request browser_network_requests browser_press_key browser_select_option browser_sign_in browser_snapshot browser_take_screenshot browser_type browser_wait_for call_service_tool delete_dashboard delete_service list_dashboards list_services read_dashboard read_file read_kit read_service search_kit test_service_tool write_dashboard write_service" {
		t.Errorf("tools: %s", got)
	}

	if _, problem := call(t, cs, "write_service", map[string]any{"source": `export default { name: "Bad" }`}); !strings.HasPrefix(problem, "it was not saved:\n") || !strings.Contains(problem, "title") {
		t.Errorf("a file with problems: %q", problem)
	}
	out, problem := call(t, cs, "write_service", map[string]any{"source": source("gitea")})
	if got, _ := json.Marshal(out); problem != "" || string(got) != `{"login":false,"replaced":false,"saved":"service gitea","tools":[]}` {
		t.Fatalf("new service: %s %s", got, problem)
	}
	if out, _ := call(t, cs, "write_service", map[string]any{"source": source("gitea")}); out["replaced"] != true {
		t.Errorf("same name again: %v", out)
	}
	if out, _ := call(t, cs, "read_service", map[string]any{"name": "gitea"}); out["source"] != source("gitea") {
		t.Errorf("read: %v", out)
	}
	out, _ = call(t, cs, "list_services", nil)
	if s := out["services"].([]any); len(s) != 1 || s[0].(map[string]any)["name"] != "gitea" {
		t.Errorf("list: %v", out)
	}

	if _, problem := call(t, cs, "write_dashboard", map[string]any{"name": "Inbox", "source": "<template><p></template>"}); !strings.Contains(problem, "line 1: ") {
		t.Errorf("a dashboard that does not compile: %q", problem)
	}
	if out, problem := call(t, cs, "write_dashboard", map[string]any{"name": "Inbox", "source": dashboardStarter}); out["saved"] != "dashboard Inbox" || out["replaced"] != false {
		t.Errorf("new dashboard: %v %s", out, problem)
	}
	if out, _ := call(t, cs, "list_dashboards", nil); len(out["dashboards"].([]any)) != 1 {
		t.Errorf("dashboards: %v", out)
	}

	if text := sentence(t, cs, "delete_service", map[string]any{"name": "gitea"}); text != "Deleted the service gitea." {
		t.Errorf("delete: %q", text)
	}
	if text := sentence(t, cs, "delete_dashboard", map[string]any{"name": "Inbox"}); text != "Deleted the dashboard Inbox." {
		t.Errorf("delete: %q", text)
	}
	if _, problem := call(t, cs, "read_service", map[string]any{"name": "gitea"}); problem != "there is no service named gitea" {
		t.Errorf("after delete: %q", problem)
	}
	if _, problem := call(t, cs, "delete_dashboard", map[string]any{"name": "Inbox"}); problem != "there is no dashboard named Inbox" {
		t.Errorf("delete twice: %q", problem)
	}
}

func TestAgentAsksBeforeChanges(t *testing.T) {
	db := desktop.TestDB(t)
	d := Documents{db: db, icons: newIcons(db)}
	if _, err := d.SaveService(0, `export default {
		name: "gitea", title: "Gitea", url: "https://example.com",
		tools: {
			list: { description: "List.", annotations: { readOnlyHint: true }, async run() { return {} } },
			close: { description: "Close.", async run() { return {} } },
		},
	}`); err != nil {
		t.Fatal(err)
	}
	confirm := confirmer(db)
	for _, tc := range []struct {
		tool  string
		input map[string]any
		ask   bool
	}{
		{"write_service", nil, false},
		{"write_dashboard", nil, false},
		{"delete_service", nil, true},
		{"delete_dashboard", nil, true},
		{"read_service", nil, false},
		{"browser_click", nil, false},
		{"browser_clear_data", nil, false},
		// The sign-in it shows is the user's to do, or not.
		{"browser_sign_in", nil, false},
		{"call_service_tool", map[string]any{"service": "gitea", "tool": "list"}, false},
		{"call_service_tool", map[string]any{"service": "gitea", "tool": "close"}, true},
		{"call_service_tool", map[string]any{"service": "nope", "tool": "list"}, true},
		{"test_service_tool", map[string]any{"service": "gitea", "tool": "list"}, false},
		{"test_service_tool", map[string]any{"service": "gitea", "tool": "close"}, true},
		{"test_service_tool", map[string]any{"service": "gitea"}, false},
	} {
		if got := confirm(tc.tool, tc.input); got != tc.ask {
			t.Errorf("%s %v: asks %v", tc.tool, tc.input, got)
		}
	}
}

func TestMCPOverHTTPNeedsTheToken(t *testing.T) {
	db := desktop.TestDB(t)
	server := newMCPServer(&Documents{db: db, icons: newIcons(db)}, &Tools{db: db})
	path := filepath.Join(t.TempDir(), mcpFile)
	m, err := serveMCP(server, path)
	if err != nil {
		t.Fatal(err)
	}
	defer m.http.Close()
	conn, _ := m.Connection()

	if code := mcpStatus(t, conn.URL, ""); code != http.StatusUnauthorized {
		t.Errorf("no token: %d", code)
	}

	client := &http.Client{Transport: bearer{conn.Token}}
	cs, err := mcp.NewClient(&mcp.Implementation{Name: "test"}, nil).
		Connect(t.Context(), &mcp.StreamableClientTransport{Endpoint: conn.URL, HTTPClient: client}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer cs.Close()
	if out, problem := call(t, cs, "list_dashboards", nil); problem != "" || out["dashboards"] == nil {
		t.Errorf("%v %s", out, problem)
	}

	// A new token shuts out the old one.
	renewed, err := m.NewToken()
	if err != nil {
		t.Fatal(err)
	}
	if renewed.Token == conn.Token || renewed.URL != conn.URL {
		t.Errorf("renewed as %+v, was %+v", renewed, conn)
	}
	if code := mcpStatus(t, conn.URL, conn.Token); code != http.StatusUnauthorized {
		t.Errorf("old token: %d", code)
	}
	if code := mcpStatus(t, conn.URL, renewed.Token); code == http.StatusUnauthorized {
		t.Errorf("new token: %d", code)
	}

	// The next start keeps the token, and the port if it is free.
	m.http.Close()
	m2, err := serveMCP(server, path)
	if err != nil {
		t.Fatal(err)
	}
	defer m2.http.Close()
	if again, _ := m2.Connection(); again != renewed {
		t.Errorf("restarted as %+v, was %+v", again, renewed)
	}
}

// mcpStatus is the status of a request to Harbor's MCP server with token.
func mcpStatus(t *testing.T, url, token string) int {
	t.Helper()
	req, _ := http.NewRequest(http.MethodPost, url, strings.NewReader("{}"))
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	return resp.StatusCode
}

type bearer struct{ token string }

func (b bearer) RoundTrip(r *http.Request) (*http.Response, error) {
	r = r.Clone(r.Context())
	r.Header.Set("Authorization", "Bearer "+b.token)
	return http.DefaultTransport.RoundTrip(r)
}

func TestATestSaysWhenTheLoginCheckMayBeWrong(t *testing.T) {
	m, err := service.Read(`export default {
		name: "gitea", title: "Gitea", url: "https://example.com",
		login: { url: "/login", check: async () => true },
		tools: {},
	}`)
	if err != nil {
		t.Fatal(err)
	}
	svc := engine.Service{Manifest: m}
	thrown := &browser.JSError{Message: "Error: HTTP 401"}
	for _, tc := range []struct {
		err  error
		want string
	}{
		{engine.NotSignedIn(svc), "login.check never said they were signed in"},
		{thrown, "login.check said the user was signed in"},
		{fmt.Errorf("%w to Gitea: %w", engine.ErrCannotTell, thrown), ""},
		{errors.New("the tab was closed"), ""},
	} {
		got := testHint(svc, tc.err).Error()
		if tc.want == "" && got != tc.err.Error() || tc.want != "" && !strings.Contains(got, tc.want) {
			t.Errorf("testHint(%v) = %q", tc.err, got)
		}
	}
	svc.Manifest.Login = nil
	if got := testHint(svc, thrown); got != error(thrown) {
		t.Errorf("with no login: %v", got)
	}
}
