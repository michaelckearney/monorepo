// Package chat runs Harbor's agent: chats in which a model, through a
// provider, uses the tools of Harbor's MCP server.
package chat

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/michaelckearney/monorepo/projects/harbor/backend/provider"
)

// Model is what the agent asks for each assistant message. A provider with
// its settings is one.
type Model interface {
	Generate(ctx context.Context, req provider.Request, emit func(provider.Output)) error
}

// Config is what an Agent is made of.
type Config struct {
	DB *sql.DB
	// Tools is Harbor's MCP server, which the agent connects to in memory.
	Tools *mcp.Server
	// Instructions is what the agent is told before every chat.
	Instructions string
	// Confirm says whether to ask the user before the agent calls a tool of
	// the MCP server with this input.
	Confirm func(tool string, input map[string]any) bool
	// Model gives the model for a turn. Its error, such as that no provider
	// is set up, is what the chat shows.
	Model func() (Model, error)
}

// Agent runs chats. A chat runs one turn at a time, in the background; the
// frontend follows it in the database.
type Agent struct {
	cfg Config

	mu      sync.Mutex
	session *mcp.ClientSession
	tools   []provider.Tool
	running map[string]context.CancelFunc
}

// New returns an agent. It connects to its tools on the first turn.
func New(cfg Config) *Agent {
	// A turn that was running when Harbor quit did not finish.
	cfg.DB.Exec(`UPDATE chats SET working = 0, draft = '', error = 'Harbor quit during this turn.' WHERE working = 1`)
	return &Agent{cfg: cfg, running: map[string]context.CancelFunc{}}
}

// part is a stored part: a provider's part, and what the chat adds to it.
type part struct {
	provider.Part
	// Ask is set on a tool call that needs the user's permission.
	Ask bool `json:"ask,omitempty"`
	// Refused is set on the result of a tool call the user did not allow.
	Refused bool `json:"refused,omitempty"`
	// Context is set on a text Harbor adds to the user's message, such as
	// what they were looking at: the model gets it, the chat does not show it.
	Context bool `json:"context,omitempty"`
}

type message struct {
	Role  string `json:"role"`
	Parts []part `json:"parts"`
}

// NewChat starts an empty chat, and returns its id.
func (a *Agent) NewChat(ctx context.Context) (string, error) {
	id := rand.Text()
	_, err := a.cfg.DB.ExecContext(ctx, `INSERT INTO chats (id) VALUES (?)`, id)
	return id, err
}

// DeleteChat stops a chat and deletes it.
func (a *Agent) DeleteChat(ctx context.Context, id string) error {
	a.Stop(id)
	_, err := a.cfg.DB.ExecContext(ctx, `DELETE FROM chats WHERE id = ?`, id)
	return err
}

// Send sends the user's message, and starts the agent's turn. lookingAt
// says what the user is looking at as they send it, such as `the dashboard
// "Money"`, or is empty.
func (a *Agent) Send(id, text, lookingAt string) error {
	text = strings.TrimSpace(text)
	if text == "" {
		return errors.New("the message is empty")
	}
	return a.start(id, func(ctx context.Context) error {
		// The first message names the chat.
		title := strings.Join(strings.Fields(text), " ")
		if len([]rune(title)) > 60 {
			title = string([]rune(title)[:57]) + "..."
		}
		if _, err := a.cfg.DB.ExecContext(ctx, `UPDATE chats SET title = ? WHERE id = ? AND title = ''`, title, id); err != nil {
			return err
		}
		// A tool call the user did not answer, or that a stop cut off, gets a
		// result, so every call the model made has one.
		open, err := a.open(ctx, id)
		if err != nil {
			return err
		}
		for _, call := range open {
			reason := "It did not run: the user stopped the turn."
			if call.Ask {
				reason = "It did not run: the user sent a new message instead of answering."
			}
			if err := a.result(ctx, id, &provider.ToolResult{ID: call.ToolCall.ID, Error: reason}, false); err != nil {
				return err
			}
		}
		parts := []part{{Part: provider.Part{Text: text}}}
		if lookingAt != "" {
			// So "make this chart weekly" says which chart.
			parts = append(parts, part{Part: provider.Part{Text: "(The user is looking at " + lookingAt + ".)"}, Context: true})
		}
		return a.add(ctx, id, message{Role: "user", Parts: parts}, nil)
	})
}

// Answer answers the agent's request to run a tool, and goes on with the
// turn: the tool runs if allowed is true.
func (a *Agent) Answer(id, callID string, allowed bool) error {
	return a.start(id, func(ctx context.Context) error {
		open, err := a.open(ctx, id)
		if err != nil {
			return err
		}
		if len(open) == 0 || !open[0].Ask || open[0].ToolCall.ID != callID {
			return errors.New("the agent is not asking about that tool call")
		}
		if !allowed {
			return a.result(ctx, id, &provider.ToolResult{ID: callID, Error: "The user did not allow this tool call."}, true)
		}
		return a.call(ctx, id, open[0].ToolCall)
	})
}

// Stop ends a chat's turn, if it is on one.
func (a *Agent) Stop(id string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if cancel, ok := a.running[id]; ok {
		cancel()
	}
}

// Close stops every turn.
func (a *Agent) Close() {
	a.mu.Lock()
	defer a.mu.Unlock()
	for _, cancel := range a.running {
		cancel()
	}
	if a.session != nil {
		a.session.Close()
	}
}

// start marks the chat as working, and runs first, then the turn, in the
// background.
func (a *Agent) start(id string, first func(context.Context) error) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if _, busy := a.running[id]; busy {
		return errors.New("the agent is still on its last turn")
	}
	res, err := a.cfg.DB.Exec(`UPDATE chats SET working = 1, error = '', draft = '' WHERE id = ?`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return errors.New("the chat was deleted")
	}
	ctx, cancel := context.WithCancel(context.Background())
	a.running[id] = cancel
	go func() {
		failure := first(ctx)
		if failure == nil {
			failure = a.turn(ctx, id)
		}
		if ctx.Err() != nil {
			failure = errors.New("stopped")
		}
		a.mu.Lock()
		delete(a.running, id)
		a.mu.Unlock()
		cancel()
		problem := ""
		if failure != nil {
			problem = failure.Error()
		}
		a.cfg.DB.Exec(`UPDATE chats SET working = 0, draft = '', error = ? WHERE id = ?`, problem, id)
	}()
	return nil
}

// turn runs the agent until it has replied, or needs the user.
func (a *Agent) turn(ctx context.Context, id string) error {
	for {
		open, err := a.open(ctx, id)
		if err != nil {
			return err
		}
		if len(open) > 0 {
			if open[0].Ask {
				return nil
			}
			if err := a.call(ctx, id, open[0].ToolCall); err != nil {
				return err
			}
			continue
		}
		messages, err := a.messages(ctx, id)
		if err != nil {
			return err
		}
		if messages[len(messages)-1].Role == "assistant" {
			return nil
		}
		if err := a.generate(ctx, id, messages); err != nil {
			return err
		}
	}
}

// draftEvery is how often the reply so far is written while it streams in.
const draftEvery = 100 * time.Millisecond

// generate asks the model for the next assistant message, and stores it.
func (a *Agent) generate(ctx context.Context, id string, messages []message) error {
	model, err := a.cfg.Model()
	if err != nil {
		return err
	}
	tools, err := a.connect(ctx)
	if err != nil {
		return err
	}
	req := provider.Request{Instructions: a.cfg.Instructions, Tools: tools}
	for _, m := range messages {
		pm := provider.Message{Role: m.Role}
		for _, p := range m.Parts {
			pm.Parts = append(pm.Parts, p.Part)
		}
		req.Messages = append(req.Messages, pm)
	}

	var text strings.Builder
	var calls []part
	var usage json.RawMessage
	var written time.Time
	err = model.Generate(ctx, req, func(out provider.Output) {
		switch {
		case out.Text != "":
			text.WriteString(out.Text)
			if time.Since(written) >= draftEvery {
				a.cfg.DB.Exec(`UPDATE chats SET draft = ? WHERE id = ?`, text.String(), id)
				written = time.Now()
			}
		case out.ToolCall != nil:
			calls = append(calls, part{Part: provider.Part{ToolCall: out.ToolCall}, Ask: a.cfg.Confirm(out.ToolCall.Name, out.ToolCall.Input)})
		case out.Done:
			usage = out.Usage
		}
	})
	if err != nil {
		return err
	}
	reply := message{Role: "assistant"}
	if t := strings.TrimSpace(text.String()); t != "" {
		reply.Parts = append(reply.Parts, part{Part: provider.Part{Text: t}})
	}
	reply.Parts = append(reply.Parts, calls...)
	if len(reply.Parts) == 0 {
		return errors.New("the model sent an empty reply")
	}
	return a.add(ctx, id, reply, usage)
}

// call runs a tool call, and stores its result.
func (a *Agent) call(ctx context.Context, id string, c *provider.ToolCall) error {
	if _, err := a.connect(ctx); err != nil {
		return err
	}
	res, err := a.session.CallTool(ctx, &mcp.CallToolParams{Name: c.Name, Arguments: c.Input})
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return a.result(ctx, id, &provider.ToolResult{ID: c.ID, Error: err.Error()}, false)
	}
	return a.result(ctx, id, ToolResult(c.ID, res), false)
}

// ToolResult is what the model is given for an MCP tool's result: its
// structured output, or else its text; or its text as the error.
func ToolResult(id string, res *mcp.CallToolResult) *provider.ToolResult {
	var text []string
	var images []provider.Image
	for _, content := range res.Content {
		switch c := content.(type) {
		case *mcp.TextContent:
			text = append(text, c.Text)
		case *mcp.ImageContent:
			images = append(images, provider.Image{MIMEType: c.MIMEType, Data: base64.StdEncoding.EncodeToString(c.Data)})
		}
	}
	switch {
	case res.IsError:
		return &provider.ToolResult{ID: id, Error: strings.Join(text, "\n")}
	case res.StructuredContent == nil:
		return &provider.ToolResult{ID: id, Output: strings.Join(text, "\n"), Images: images}
	}
	return &provider.ToolResult{ID: id, Output: res.StructuredContent, Images: images}
}

func (a *Agent) result(ctx context.Context, id string, r *provider.ToolResult, refused bool) error {
	return a.add(ctx, id, message{Role: "tool", Parts: []part{{Part: provider.Part{ToolResult: r}, Refused: refused}}}, nil)
}

func (a *Agent) add(ctx context.Context, id string, m message, usage json.RawMessage) error {
	parts, err := json.Marshal(m.Parts)
	if err != nil {
		return err
	}
	var u any
	if len(usage) > 0 {
		u = string(usage)
	}
	tx, err := a.cfg.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`INSERT INTO messages (chat_id, role, parts, usage) VALUES (?, ?, ?, ?)`, id, m.Role, string(parts), u); err != nil {
		return err
	}
	// The stored message replaces the draft in the same change, so the reply
	// never shows twice or not at all.
	if _, err := tx.Exec(`UPDATE chats SET draft = '', updated_at = strftime('%Y-%m-%d %H:%M:%f', 'now') WHERE id = ?`, id); err != nil {
		return err
	}
	return tx.Commit()
}

func (a *Agent) messages(ctx context.Context, id string) ([]message, error) {
	rows, err := a.cfg.DB.QueryContext(ctx, `SELECT role, parts FROM messages WHERE chat_id = ? ORDER BY seq`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []message
	for rows.Next() {
		var m message
		var parts string
		if err := rows.Scan(&m.Role, &parts); err != nil {
			return nil, err
		}
		if err := json.Unmarshal([]byte(parts), &m.Parts); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// open returns the tool calls of the last assistant message that have no
// result yet, in order.
func (a *Agent) open(ctx context.Context, id string) ([]part, error) {
	messages, err := a.messages(ctx, id)
	if err != nil {
		return nil, err
	}
	last := -1
	for i, m := range messages {
		if m.Role == "assistant" {
			last = i
		}
	}
	if last < 0 {
		return nil, nil
	}
	done := map[string]bool{}
	for _, m := range messages[last+1:] {
		for _, p := range m.Parts {
			if p.ToolResult != nil {
				done[p.ToolResult.ID] = true
			}
		}
	}
	var open []part
	for _, p := range messages[last].Parts {
		if p.ToolCall != nil && !done[p.ToolCall.ID] {
			open = append(open, p)
		}
	}
	return open, nil
}

// connect connects to Harbor's MCP server, once, and returns its tools.
func (a *Agent) connect(ctx context.Context) ([]provider.Tool, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.session != nil {
		return a.tools, nil
	}
	client, server := mcp.NewInMemoryTransports()
	// The server side lives as long as Harbor does, not as long as this turn.
	if _, err := a.cfg.Tools.Connect(context.Background(), server, nil); err != nil {
		return nil, fmt.Errorf("could not connect to Harbor's tools: %w", err)
	}
	session, err := mcp.NewClient(&mcp.Implementation{Name: "harbor-agent"}, nil).Connect(context.Background(), client, nil)
	if err != nil {
		return nil, fmt.Errorf("could not connect to Harbor's tools: %w", err)
	}
	var tools []provider.Tool
	for tool, err := range session.Tools(ctx, nil) {
		if err != nil {
			session.Close()
			return nil, fmt.Errorf("could not list Harbor's tools: %w", err)
		}
		schema, err := json.Marshal(tool.InputSchema)
		if err != nil {
			session.Close()
			return nil, err
		}
		tools = append(tools, provider.Tool{Name: tool.Name, Description: tool.Description, InputSchema: schema})
	}
	a.session, a.tools = session, tools
	return tools, nil
}
