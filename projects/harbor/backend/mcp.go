package backend

import (
	"context"
	"database/sql"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/michaelckearney/monorepo/projects/harbor/backend/service"
	"github.com/michaelckearney/monorepo/projects/harbor/frontend/src/kit"
)

// Harbor's own MCP server: what an agent uses to read, write, and test
// services and dashboards. Harbor's agent reaches it in memory; other agents,
// over HTTP (mcphttp.go).

//go:embed mcp.md
var mcpInstructions string

func newMCPServer(docs *Documents, tools *Tools) *mcp.Server {
	s := mcp.NewServer(&mcp.Implementation{Name: "harbor", Title: "Harbor", Version: "0.1.0"},
		&mcp.ServerOptions{Instructions: mcpInstructions})
	h := mcpTools{docs: docs, tools: tools, db: docs.db}

	mcp.AddTool(s, &mcp.Tool{Name: "list_services", Description: "List the services, with their tools.",
		Annotations: readOnly}, h.listServices)
	mcp.AddTool(s, &mcp.Tool{Name: "read_service", Description: "Read a service's file.",
		Annotations: readOnly}, h.readService)
	mcp.AddTool(s, &mcp.Tool{Name: "write_service",
		Description: "Save a service's whole file. The name in the file says which service: a new name makes a new service. A file with problems is not saved, and the error lists them.",
		Annotations: writes}, h.writeService)
	mcp.AddTool(s, &mcp.Tool{Name: "delete_service", Description: "Delete a service.",
		Annotations: deletes}, h.deleteService)
	mcp.AddTool(s, &mcp.Tool{Name: "call_service_tool",
		Description: "Run one of a service's tools, in Harbor's browser, signed in as the user, and return what it returns. If the site needs the user to sign in or confirm it's them, Harbor does it with their saved login, or shows them the page and waits.",
		Annotations: &mcp.ToolAnnotations{OpenWorldHint: ptr(true)}}, h.callServiceTool)
	mcp.AddTool(s, &mcp.Tool{Name: "test_service_tool",
		Description: "Run one of a service's tools as call_service_tool does, but in the agent's own browser, where the browser_* tools run: to test a service you wrote, its sign-in included. With no tool, test the sign-in from the start: clear what the agent's browser keeps for the site, sign in, and return the service's page as it then is, its url, title, and text, whether the sign-in worked or not. If the site needs the user to sign in, Harbor does it with their saved login, or shows them the page and waits.",
		Annotations: &mcp.ToolAnnotations{OpenWorldHint: ptr(true)}}, h.testServiceTool)

	mcp.AddTool(s, &mcp.Tool{Name: "list_dashboards", Description: "List the dashboards.",
		Annotations: readOnly}, h.listDashboards)
	mcp.AddTool(s, &mcp.Tool{Name: "read_dashboard", Description: "Read a dashboard's file.",
		Annotations: readOnly}, h.readDashboard)
	mcp.AddTool(s, &mcp.Tool{Name: "write_dashboard",
		Description: "Save a dashboard's whole file under a name: a new name makes a new dashboard. A file that does not compile is not saved, and the error lists its problems.",
		Annotations: writes}, h.writeDashboard)
	mcp.AddTool(s, &mcp.Tool{Name: "delete_dashboard", Description: "Delete a dashboard.",
		Annotations: deletes}, h.deleteDashboard)
	mcp.AddTool(s, &mcp.Tool{Name: "search_kit",
		Description: `Search the kit, what a dashboard imports from "harbor", for components to show something: say what, in plain words ("messages from the doctor", "disk space", "a dropdown to pick a project"). Returns the best matches, widgets first, each with its kind and what it shows.`,
		Annotations: readOnly}, searchKit)
	mcp.AddTool(s, &mcp.Tool{Name: "read_kit",
		Description: "Read a kit component: what it shows, its props with their comments, and examples to copy.",
		Annotations: readOnly}, readKit)

	mcp.AddTool(s, &mcp.Tool{Name: "read_file",
		Description: "Read a file a service's tool returned, by the id in its output ({ file, name, type, size }): text as text, an image as an image.",
		Annotations: readOnly}, h.readFile)
	addBrowserTools(s, h)
	return s
}

var (
	readOnly = &mcp.ToolAnnotations{ReadOnlyHint: true}
	writes   = &mcp.ToolAnnotations{DestructiveHint: ptr(false)}
	deletes  = &mcp.ToolAnnotations{DestructiveHint: ptr(true)}
)

func ptr[T any](v T) *T { return &v }

type mcpTools struct {
	docs  *Documents
	tools *Tools
	db    *sql.DB
}

type named struct {
	Name string `json:"name" jsonschema:"the service's or the dashboard's name"`
}

type file struct {
	Source string `json:"source" jsonschema:"the whole file"`
}

// What a save returns says in words that it saved, and echoes what Harbor
// read from the file. The model is never shown a result's schema: a bare
// "created": false, for a save that replaced a file, was taken for a
// failure 43 times in one benchmark trial, which deleted and rewrote the
// service each time looking for an error that was not there.
type savedService struct {
	Saved    string   `json:"saved"`
	Replaced bool     `json:"replaced"`
	Login    bool     `json:"login"`
	Tools    []string `json:"tools"`
}

type savedDashboard struct {
	Saved    string `json:"saved"`
	Replaced bool   `json:"replaced"`
}

// said is a result that is only a sentence.
func said(format string, a ...any) *mcp.CallToolResult {
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: fmt.Sprintf(format, a...)}}}
}

type serviceSummary struct {
	Name        string        `json:"name"`
	Title       string        `json:"title"`
	Description string        `json:"description,omitempty"`
	URL         string        `json:"url"`
	Login       bool          `json:"login" jsonschema:"whether it has a login check"`
	Tools       []toolSummary `json:"tools"`
}

type toolSummary struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	ReadOnly    bool   `json:"readOnly,omitempty"`
}

func (h mcpTools) listServices(ctx context.Context, _ *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, struct {
	Services []serviceSummary `json:"services"`
}, error) {
	var out struct {
		Services []serviceSummary `json:"services"`
	}
	out.Services = []serviceSummary{}
	// A service whose source would not read has no tools to list.
	rows, err := h.db.QueryContext(ctx, `SELECT manifest FROM services WHERE manifest IS NOT NULL ORDER BY name`)
	if err != nil {
		return nil, out, err
	}
	defer rows.Close()
	for rows.Next() {
		var raw string
		if err := rows.Scan(&raw); err != nil {
			return nil, out, err
		}
		var m service.Manifest
		if err := json.Unmarshal([]byte(raw), &m); err != nil {
			return nil, out, err
		}
		s := serviceSummary{Name: m.Name, Title: m.Title, Description: m.Description, URL: m.URL, Login: m.Login != nil, Tools: []toolSummary{}}
		for _, t := range m.Tools {
			readOnly := t.Annotations != nil && t.Annotations.ReadOnlyHint != nil && *t.Annotations.ReadOnlyHint
			s.Tools = append(s.Tools, toolSummary{Name: t.Name, Description: t.Description, ReadOnly: readOnly})
		}
		out.Services = append(out.Services, s)
	}
	return nil, out, rows.Err()
}

func (h mcpTools) readService(ctx context.Context, _ *mcp.CallToolRequest, in named) (*mcp.CallToolResult, file, error) {
	var f file
	err := h.db.QueryRowContext(ctx, `SELECT source FROM services WHERE name = ?`, in.Name).Scan(&f.Source)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, f, fmt.Errorf("there is no service named %s", in.Name)
	}
	return nil, f, err
}

func (h mcpTools) writeService(ctx context.Context, _ *mcp.CallToolRequest, in file) (*mcp.CallToolResult, savedService, error) {
	m, err := service.Read(in.Source)
	if err != nil {
		return nil, savedService{}, notSaved(err)
	}
	id, err := h.idOf(ctx, "services", m.Name)
	if err != nil {
		return nil, savedService{}, err
	}
	if _, err := h.docs.SaveService(id, in.Source); err != nil {
		return nil, savedService{}, notSaved(err)
	}
	out := savedService{Saved: "service " + m.Name, Replaced: id != 0, Login: m.Login != nil, Tools: []string{}}
	for _, t := range m.Tools {
		out.Tools = append(out.Tools, t.Name)
	}
	return nil, out, nil
}

func (h mcpTools) deleteService(ctx context.Context, _ *mcp.CallToolRequest, in named) (*mcp.CallToolResult, any, error) {
	id, err := h.idOf(ctx, "services", in.Name)
	if err == nil && id == 0 {
		err = fmt.Errorf("there is no service named %s", in.Name)
	}
	if err == nil {
		err = h.docs.DeleteService(id)
	}
	if err != nil {
		return nil, nil, err
	}
	return said("Deleted the service %s.", in.Name), nil, nil
}

type toolCall struct {
	Service string         `json:"service"`
	Tool    string         `json:"tool"`
	Input   map[string]any `json:"input,omitempty" jsonschema:"the tool's input, which its inputSchema describes"`
}

// A service tool's output is the result itself: the agent wraps every
// result in {"output": ...}, and wrapping it here too gave the model
// {"output": {"output": ...}}.
func (h mcpTools) callServiceTool(ctx context.Context, _ *mcp.CallToolRequest, in toolCall) (*mcp.CallToolResult, map[string]any, error) {
	input, err := json.Marshal(in.Input)
	if err != nil {
		return nil, nil, err
	}
	if in.Input == nil {
		input = []byte("{}")
	}
	raw, err := h.tools.Call(ctx, in.Service, in.Tool, string(input))
	if err != nil {
		return nil, nil, err
	}
	var out map[string]any
	return nil, out, json.Unmarshal([]byte(raw), &out)
}

type testCall struct {
	Service string         `json:"service"`
	Tool    string         `json:"tool,omitempty" jsonschema:"the tool; leave it out to only sign in"`
	Input   map[string]any `json:"input,omitempty" jsonschema:"the tool's input, which its inputSchema describes"`
}

func (h mcpTools) testServiceTool(ctx context.Context, _ *mcp.CallToolRequest, in testCall) (*mcp.CallToolResult, map[string]any, error) {
	input, err := json.Marshal(in.Input)
	if err != nil {
		return nil, nil, err
	}
	if in.Input == nil {
		input = []byte("{}")
	}
	raw, err := h.tools.Test(ctx, in.Service, in.Tool, string(input))
	if err != nil {
		return nil, nil, err
	}
	var out map[string]any
	return nil, out, json.Unmarshal([]byte(raw), &out)
}

func (h mcpTools) listDashboards(ctx context.Context, _ *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, struct {
	Dashboards []string `json:"dashboards"`
}, error) {
	var out struct {
		Dashboards []string `json:"dashboards"`
	}
	out.Dashboards = []string{}
	rows, err := h.db.QueryContext(ctx, `SELECT name FROM dashboards ORDER BY name COLLATE NOCASE`)
	if err != nil {
		return nil, out, err
	}
	defer rows.Close()
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, out, err
		}
		out.Dashboards = append(out.Dashboards, name)
	}
	return nil, out, rows.Err()
}

func (h mcpTools) readDashboard(ctx context.Context, _ *mcp.CallToolRequest, in named) (*mcp.CallToolResult, file, error) {
	var f file
	err := h.db.QueryRowContext(ctx, `SELECT source FROM dashboards WHERE name = ?`, in.Name).Scan(&f.Source)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, f, fmt.Errorf("there is no dashboard named %s", in.Name)
	}
	return nil, f, err
}

type dashboardFile struct {
	Name   string `json:"name" jsonschema:"the dashboard's name, as the sidebar shows it"`
	Source string `json:"source" jsonschema:"the whole Vue file"`
}

func (h mcpTools) writeDashboard(ctx context.Context, _ *mcp.CallToolRequest, in dashboardFile) (*mcp.CallToolResult, savedDashboard, error) {
	id, err := h.idOf(ctx, "dashboards", in.Name)
	if err != nil {
		return nil, savedDashboard{}, err
	}
	if _, err := h.docs.SaveDashboard(id, in.Name, in.Source); err != nil {
		return nil, savedDashboard{}, notSaved(err)
	}
	return nil, savedDashboard{Saved: "dashboard " + in.Name, Replaced: id != 0}, nil
}

type kitQuery struct {
	Query string `json:"query" jsonschema:"what to show, in plain words"`
}

// searchKit's result is the matches, or a sentence when there are none.
func searchKit(_ context.Context, _ *mcp.CallToolRequest, in kitQuery) (*mcp.CallToolResult, any, error) {
	matches := kit.Search(in.Query, 8)
	if len(matches) == 0 {
		return said("Nothing in the kit matches %q. Say what to show in other words, or build a widget of your own: the Widget frame with primitives, or Vue of your own, inside.", in.Query), nil, nil
	}
	return nil, map[string]any{"matches": matches}, nil
}

func readKit(_ context.Context, _ *mcp.CallToolRequest, in struct {
	Name string `json:"name" jsonschema:"the component's name, as search_kit gives it"`
}) (*mcp.CallToolResult, kit.Component, error) {
	c, ok := kit.Find(in.Name)
	if !ok {
		if like := kit.Like(in.Name); len(like) > 0 {
			return nil, c, fmt.Errorf("the kit has no %s; did you mean %s?", in.Name, strings.Join(like, " or "))
		}
		return nil, c, fmt.Errorf("the kit has no %s; search_kit finds what it has", in.Name)
	}
	return nil, c, nil
}

func (h mcpTools) deleteDashboard(ctx context.Context, _ *mcp.CallToolRequest, in named) (*mcp.CallToolResult, any, error) {
	id, err := h.idOf(ctx, "dashboards", in.Name)
	if err == nil && id == 0 {
		err = fmt.Errorf("there is no dashboard named %s", in.Name)
	}
	if err == nil {
		err = h.docs.DeleteDashboard(id)
	}
	if err != nil {
		return nil, nil, err
	}
	return said("Deleted the dashboard %s.", in.Name), nil, nil
}

// idOf is the id of the named service or dashboard, or 0 if there is none.
func (h mcpTools) idOf(ctx context.Context, table, name string) (int64, error) {
	var id int64
	err := h.db.QueryRowContext(ctx, `SELECT id FROM `+table+` WHERE name = ?`, name).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil
	}
	return id, err
}

func notSaved(err error) error {
	return fmt.Errorf("it was not saved:\n%w", err)
}

type fileInfo struct {
	Name string `json:"name"`
	Type string `json:"type"`
	Size int    `json:"size"`
}

func (h mcpTools) readFile(ctx context.Context, _ *mcp.CallToolRequest, in struct {
	File string `json:"file" jsonschema:"the file's id, from a tool's output"`
}) (*mcp.CallToolResult, fileInfo, error) {
	f, err := files{h.db}.File(in.File)
	if err != nil {
		return nil, fileInfo{}, err
	}
	info := fileInfo{Name: f.Name, Type: f.Type, Size: len(f.Data)}
	var content mcp.Content
	switch {
	case textType(f.Type) && utf8.Valid(f.Data):
		content = &mcp.TextContent{Text: string(f.Data)}
	case strings.HasPrefix(f.Type, "image/"):
		content = &mcp.ImageContent{Data: f.Data, MIMEType: f.Type}
	default:
		content = &mcp.EmbeddedResource{Resource: &mcp.ResourceContents{URI: "harbor://files/" + in.File, MIMEType: f.Type, Blob: f.Data}}
	}
	return &mcp.CallToolResult{Content: []mcp.Content{content}}, info, nil
}

// textType reports whether a file of the media type is text to read.
func textType(t string) bool {
	t, _, _ = strings.Cut(t, ";")
	return strings.HasPrefix(t, "text/") || slices.Contains([]string{"application/json", "application/xml", "application/javascript"}, t) ||
		strings.HasSuffix(t, "+json") || strings.HasSuffix(t, "+xml")
}
