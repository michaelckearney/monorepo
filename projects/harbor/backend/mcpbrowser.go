package backend

import (
	"context"
	"encoding/json"
	"slices"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/michaelckearney/monorepo/projects/harbor/backend/engine"
)

// The agent's browser tools, after Playwright MCP's: the same names and
// inputs, so a model that knows those knows these. They run in the agent's
// browser (tools.go), to find out how a site works and to build a service
// for it, never to do the user's errands: those are a service's tools.

func addBrowserTools(s *mcp.Server, h mcpTools) {
	agents := " In the agent's own browser, a profile apart from the user's: use it to build or fix a service, never to answer the user, which is what services' tools are for."
	// What every action returns besides the page: events.go.
	did := " With it, events: what the action did, the pages it went to, the site's requests, and the cookies and localStorage or sessionStorage keys it added, changed, or removed, by name."
	acts := &mcp.ToolAnnotations{DestructiveHint: ptr(false), OpenWorldHint: ptr(true)}
	looks := &mcp.ToolAnnotations{ReadOnlyHint: true, OpenWorldHint: ptr(true)}

	mcp.AddTool(s, &mcp.Tool{Name: "browser_navigate",
		Description: "Go to a URL. Returns the page's snapshot." + did + agents, Annotations: acts}, h.navigate)
	mcp.AddTool(s, &mcp.Tool{Name: "browser_navigate_back",
		Description: "Go back to the previous page. Returns the page's snapshot." + did, Annotations: acts}, h.back)
	mcp.AddTool(s, &mcp.Tool{Name: "browser_snapshot",
		Description: "The page's accessibility snapshot: one element a line, with the ref that actions name it by. Better than a screenshot for acting on the page.",
		Annotations: looks}, h.snapshot)
	mcp.AddTool(s, &mcp.Tool{Name: "browser_take_screenshot",
		Description: "A picture of the page. You cannot act on it; use browser_snapshot for that.", Annotations: looks}, h.screenshot)
	mcp.AddTool(s, &mcp.Tool{Name: "browser_click",
		Description: "Click an element on the page. Returns the page's snapshot after." + did, Annotations: acts}, h.click)
	mcp.AddTool(s, &mcp.Tool{Name: "browser_type",
		Description: "Type text into a field, in place of what it has. Returns the page's snapshot after." + did, Annotations: acts}, h.typeText)
	mcp.AddTool(s, &mcp.Tool{Name: "browser_fill_form",
		Description: "Fill in several fields at once. Returns the page's snapshot after." + did, Annotations: acts}, h.fillForm)
	mcp.AddTool(s, &mcp.Tool{Name: "browser_select_option",
		Description: "Pick options in a <select>. Returns the page's snapshot after." + did, Annotations: acts}, h.selectOption)
	mcp.AddTool(s, &mcp.Tool{Name: "browser_press_key",
		Description: "Press a key on the keyboard. Returns the page's snapshot after." + did, Annotations: acts}, h.pressKey)
	mcp.AddTool(s, &mcp.Tool{Name: "browser_wait_for",
		Description: "Wait for text to appear or go, or for a time, up to 30 seconds. Returns the page's snapshot after." + did,
		Annotations: looks}, h.waitFor)
	mcp.AddTool(s, &mcp.Tool{Name: "browser_handle_dialog",
		Description: "Answer the dialog the page has open: an alert, a confirm, a prompt, or \"leave this page?\". Returns the page's snapshot after." + did, Annotations: acts}, h.handleDialog)
	mcp.AddTool(s, &mcp.Tool{Name: "browser_evaluate",
		Description: "Run a JavaScript function in the page, and return what it returns. It gets the element, if ref names one, and last the context object a tool's run gets ({ cookies }). Use it to try out what a tool's run or a login.check will do, and to read what a site keeps, such as Object.keys(localStorage). 30 s limit. Returns events, too: what the code did, as an action's.",
		Annotations: acts}, h.evaluate)
	mcp.AddTool(s, &mcp.Tool{Name: "browser_network_requests",
		Description: "The requests the agent's tab made, the newest 100, each with the page that made it, earlier pages' too: how to find a site's own APIs. Images, styles, scripts, fonts, and media are left out unless includeStatic is set.",
		Annotations: looks}, h.requests)
	mcp.AddTool(s, &mcp.Tool{Name: "browser_network_request",
		Description: "One request, by its id from browser_network_requests: its headers, the body it sent, and the response's body, while the browser still has it.",
		Annotations: looks}, h.request)
	mcp.AddTool(s, &mcp.Tool{Name: "browser_sign_in",
		Description: "Have the user sign in to a site in the agent's browser, and learn how its sign-in works. Harbor shows the user the site, they sign in and click Done, and this returns what the sign-in did: the pages it went through, the requests, and the cookies and localStorage or sessionStorage keys it added, by name. Your browser is then signed in, to explore the site. Use it before you write a service's login, and build login.check on what it returns.",
		Annotations: acts}, h.signIn)
	mcp.AddTool(s, &mcp.Tool{Name: "browser_clear_data",
		Description: "Remove what the agent's browser keeps for a site: its cookies and storage, so the next visit is signed out. With no site, everything. Use it to test a service's sign-in from the start. The user's own browser is not touched.",
		Annotations: &mcp.ToolAnnotations{DestructiveHint: ptr(true), IdempotentHint: true}}, h.clearData)
}

type ref struct {
	Element string `json:"element,omitempty" jsonschema:"what the element is, as you would tell the user"`
	Ref     string `json:"ref" jsonschema:"the element's ref, from the page's snapshot"`
}

func page(p engine.Page, err error) (*mcp.CallToolResult, engine.Page, error) {
	return nil, p, err
}

func (h mcpTools) navigate(ctx context.Context, _ *mcp.CallToolRequest, in struct {
	URL string `json:"url" jsonschema:"a full URL, such as https://example.com/inbox"`
}) (*mcp.CallToolResult, engine.Page, error) {
	return page(h.tools.agent().Navigate(ctx, in.URL))
}

func (h mcpTools) back(ctx context.Context, _ *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, engine.Page, error) {
	return page(h.tools.agent().Back(ctx))
}

func (h mcpTools) snapshot(ctx context.Context, _ *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, engine.Page, error) {
	return page(h.tools.agent().Snapshot(ctx))
}

func (h mcpTools) screenshot(ctx context.Context, _ *mcp.CallToolRequest, in struct {
	FullPage bool `json:"fullPage,omitempty" jsonschema:"the whole page, not only what the window shows"`
}) (*mcp.CallToolResult, any, error) {
	shot, err := h.tools.agent().Screenshot(ctx, in.FullPage)
	if err != nil {
		return nil, nil, err
	}
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.ImageContent{Data: shot, MIMEType: "image/jpeg"}}}, nil, nil
}

func (h mcpTools) click(ctx context.Context, _ *mcp.CallToolRequest, in struct {
	ref
	DoubleClick bool `json:"doubleClick,omitempty"`
}) (*mcp.CallToolResult, engine.Page, error) {
	return page(h.tools.agent().Click(ctx, in.Ref, in.DoubleClick))
}

func (h mcpTools) typeText(ctx context.Context, _ *mcp.CallToolRequest, in struct {
	ref
	Text   string `json:"text"`
	Submit bool   `json:"submit,omitempty" jsonschema:"press Enter after"`
}) (*mcp.CallToolResult, engine.Page, error) {
	return page(h.tools.agent().Type(ctx, in.Ref, in.Text, in.Submit))
}

func (h mcpTools) fillForm(ctx context.Context, _ *mcp.CallToolRequest, in struct {
	Fields []struct {
		Name string `json:"name,omitempty" jsonschema:"the field's name, as you would tell the user"`
		engine.Field
	} `json:"fields" jsonschema:"each field: its ref; its type, textbox, checkbox, radio, combobox, or slider; and its value: text, true or false for a checkbox or a radio, or an option's text"`
}) (*mcp.CallToolResult, engine.Page, error) {
	fields := make([]engine.Field, len(in.Fields))
	for i, f := range in.Fields {
		fields[i] = f.Field
	}
	return page(h.tools.agent().FillForm(ctx, fields))
}

func (h mcpTools) selectOption(ctx context.Context, _ *mcp.CallToolRequest, in struct {
	ref
	Values []string `json:"values" jsonschema:"the options to pick, by value or by their text"`
}) (*mcp.CallToolResult, engine.Page, error) {
	return page(h.tools.agent().SelectOption(ctx, in.Ref, in.Values))
}

func (h mcpTools) pressKey(ctx context.Context, _ *mcp.CallToolRequest, in struct {
	Key string `json:"key" jsonschema:"a key, such as Enter, Tab, Escape, ArrowDown, or a; with modifiers, such as Control+a"`
}) (*mcp.CallToolResult, engine.Page, error) {
	return page(h.tools.agent().PressKey(ctx, in.Key))
}

func (h mcpTools) waitFor(ctx context.Context, _ *mcp.CallToolRequest, in struct {
	Text     string  `json:"text,omitempty" jsonschema:"wait until this text is on the page"`
	TextGone string  `json:"textGone,omitempty" jsonschema:"wait until this text is gone from the page"`
	Time     float64 `json:"time,omitempty" jsonschema:"seconds to wait"`
}) (*mcp.CallToolResult, engine.Page, error) {
	return page(h.tools.agent().WaitFor(ctx, in.Text, in.TextGone, in.Time))
}

func (h mcpTools) handleDialog(ctx context.Context, _ *mcp.CallToolRequest, in struct {
	Accept     bool   `json:"accept" jsonschema:"true for OK, false for Cancel"`
	PromptText string `json:"promptText,omitempty" jsonschema:"what to answer a prompt with"`
}) (*mcp.CallToolResult, engine.Page, error) {
	return page(h.tools.agent().HandleDialog(ctx, in.Accept, in.PromptText))
}

func (h mcpTools) evaluate(ctx context.Context, _ *mcp.CallToolRequest, in struct {
	Function string `json:"function" jsonschema:"a function, such as (context) => document.title, or (element, context) => element.textContent with a ref"`
	Element  string `json:"element,omitempty" jsonschema:"what the element is, as you would tell the user"`
	Ref      string `json:"ref,omitempty" jsonschema:"an element's ref, from the page's snapshot"`
}) (*mcp.CallToolResult, evaluated, error) {
	var out evaluated
	raw, events, err := h.tools.agent().Evaluate(ctx, in.Function, in.Ref)
	if err != nil {
		return nil, out, err
	}
	out.Events = events
	return nil, out, json.Unmarshal(raw, &out.Result)
}

type evaluated struct {
	Result any      `json:"result"`
	Events []string `json:"events,omitempty"`
}

// requestsShown is how many requests browser_network_requests lists, the
// newest: the list now goes back through the pages before.
const requestsShown = 100

type requestSummary struct {
	ID       string `json:"id"`
	Method   string `json:"method"`
	URL      string `json:"url"`
	Page     string `json:"page,omitempty"`
	Type     string `json:"type"`
	Status   int64  `json:"status"`
	MimeType string `json:"mimeType,omitempty"`
	Failed   string `json:"failed,omitempty"`
}

func (h mcpTools) requests(ctx context.Context, _ *mcp.CallToolRequest, in struct {
	IncludeStatic bool `json:"includeStatic,omitempty" jsonschema:"include images, styles, scripts, fonts, and media"`
}) (*mcp.CallToolResult, struct {
	Requests []requestSummary `json:"requests"`
	Earlier  int              `json:"earlier,omitempty" jsonschema:"how many older requests are left out"`
}, error) {
	var out struct {
		Requests []requestSummary `json:"requests"`
		Earlier  int              `json:"earlier,omitempty" jsonschema:"how many older requests are left out"`
	}
	all, err := h.tools.agent().Requests(ctx)
	if err != nil {
		return nil, out, err
	}
	out.Requests = []requestSummary{}
	for _, r := range all {
		if !in.IncludeStatic && slices.Contains(engine.StaticTypes, r.Type) {
			continue
		}
		out.Requests = append(out.Requests, requestSummary{r.ID, r.Method, r.URL, r.Page, r.Type, r.Status, r.MimeType, r.Failed})
	}
	if n := len(out.Requests) - requestsShown; n > 0 {
		out.Requests, out.Earlier = out.Requests[n:], n
	}
	return nil, out, nil
}

func (h mcpTools) request(ctx context.Context, _ *mcp.CallToolRequest, in struct {
	ID string `json:"id" jsonschema:"the request's id, from browser_network_requests"`
}) (*mcp.CallToolResult, engine.RequestDetail, error) {
	d, err := h.tools.agent().Request(ctx, in.ID)
	return nil, d, err
}

func (h mcpTools) clearData(ctx context.Context, _ *mcp.CallToolRequest, in struct {
	Site string `json:"site,omitempty" jsonschema:"the site, such as https://mail.example.com; leave it out for every site"`
}) (*mcp.CallToolResult, any, error) {
	if err := h.tools.agent().ClearData(ctx, in.Site); err != nil {
		return nil, nil, err
	}
	if in.Site == "" {
		return said("Cleared everything your browser keeps, for every site."), nil, nil
	}
	// A site that signs in through another, such as an OpenID provider,
	// signs straight back in: that site's session was not cleared.
	return said("Cleared the cookies and storage your browser keeps for %s. Other sites keep theirs, such as a sign-in provider it sends you to, so signing in again may not ask for a password.", in.Site), nil, nil
}

func (h mcpTools) signIn(ctx context.Context, _ *mcp.CallToolRequest, in struct {
	URL string `json:"url" jsonschema:"the site, such as https://mail.example.com"`
}) (*mcp.CallToolResult, engine.SignInWatched, error) {
	r, err := h.tools.agent().WatchSignIn(ctx, in.URL)
	return nil, r, err
}
