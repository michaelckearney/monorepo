package chat

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/michaelckearney/monorepo/framework"
	"github.com/michaelckearney/monorepo/projects/harbor/backend/provider"
)

// scripted is a model that saves a note, then says what happened, a word at
// a time. It keeps the last request it was given.
type scripted struct {
	last *provider.Request
}

func (s *scripted) Generate(_ context.Context, req provider.Request, emit func(provider.Output)) error {
	*s.last = req
	last := req.Messages[len(req.Messages)-1].Parts[0]
	if last.ToolResult == nil {
		emit(provider.Output{ToolCall: &provider.ToolCall{ID: "call-1", Name: "save_note", Input: map[string]any{"text": "milk"}}})
		emit(provider.Output{Done: true})
		return nil
	}
	reply := "Saved the note."
	if last.ToolResult.Error != "" {
		reply = "I did not save it."
	}
	for _, word := range strings.SplitAfter(reply, " ") {
		emit(provider.Output{Text: word})
	}
	emit(provider.Output{Done: true, Usage: json.RawMessage(`{"tokens":5}`)})
	return nil
}

type saveOutput struct {
	Saved bool `json:"saved"`
}

func notes(saved *atomic.Value) *mcp.Server {
	s := mcp.NewServer(&mcp.Implementation{Name: "notes"}, nil)
	mcp.AddTool(s, &mcp.Tool{Name: "save_note", Description: "Save a note."},
		func(_ context.Context, _ *mcp.CallToolRequest, in struct {
			Text string `json:"text"`
		}) (*mcp.CallToolResult, saveOutput, error) {
			saved.Store(in.Text)
			return nil, saveOutput{true}, nil
		})
	return s
}

type chatRow struct {
	Title, Draft, Error string
	Working             bool
}

// wait waits for the chat's turn to end.
func wait(t *testing.T, db *sql.DB, id string) chatRow {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		var c chatRow
		if err := db.QueryRow(`SELECT title, draft, error, working FROM chats WHERE id = ?`, id).
			Scan(&c.Title, &c.Draft, &c.Error, &c.Working); err != nil {
			t.Fatal(err)
		}
		if !c.Working {
			return c
		}
		if time.Now().After(deadline) {
			t.Fatal("the turn did not end")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// stored returns the chat's messages as stored, one line each.
func stored(t *testing.T, db *sql.DB, id string) string {
	t.Helper()
	// The frontend reads parts as JSON text; a BLOB would not parse.
	var blobs int
	db.QueryRow(`SELECT count(*) FROM messages WHERE typeof(parts) != 'text' OR typeof(usage) NOT IN ('text', 'null')`).Scan(&blobs)
	if blobs > 0 {
		t.Errorf("%d messages are not stored as text", blobs)
	}
	rows, err := db.Query(`SELECT role, parts FROM messages WHERE chat_id = ? ORDER BY seq`, id)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var lines []string
	for rows.Next() {
		var role, parts string
		rows.Scan(&role, &parts)
		lines = append(lines, role+" "+parts)
	}
	return strings.Join(lines, "\n")
}

type setup struct {
	db    *sql.DB
	agent *Agent
	saved *atomic.Value
	last  *provider.Request
	id    string
}

// newSetup makes an agent with the notes tools, and a chat. A nil model is
// the scripted one.
func newSetup(t *testing.T, model func() (Model, error)) setup {
	s := setup{db: desktop.TestDB(t), saved: &atomic.Value{}, last: &provider.Request{}}
	if model == nil {
		model = func() (Model, error) { return &scripted{last: s.last}, nil }
	}
	s.agent = New(Config{
		DB:           s.db,
		Tools:        notes(s.saved),
		Instructions: "Save notes.",
		Confirm:      func(tool string, _ map[string]any) bool { return tool == "save_note" },
		Model:        model,
	})
	t.Cleanup(s.agent.Close)
	var err error
	if s.id, err = s.agent.NewChat(t.Context()); err != nil {
		t.Fatal(err)
	}
	return s
}

func TestTheModelIsToldWhatTheUserIsLookingAt(t *testing.T) {
	s := newSetup(t, nil)
	if err := s.agent.Send(s.id, "Make this chart weekly", `the dashboard "Money"`); err != nil {
		t.Fatal(err)
	}
	wait(t, s.db, s.id)
	want := `user [{"text":"Make this chart weekly"},{"text":"(The user is looking at the dashboard \"Money\".)","context":true}]`
	if got := stored(t, s.db, s.id); !strings.HasPrefix(got, want+"\n") {
		t.Errorf("messages:\n%s\nwant them to start:\n%s", got, want)
	}
	if parts := s.last.Messages[0].Parts; len(parts) != 2 || parts[1].Text != `(The user is looking at the dashboard "Money".)` {
		t.Errorf("the model got %+v", parts)
	}
}

func TestATurnAsksBeforeItSaves(t *testing.T) {
	s := newSetup(t, nil)
	if err := s.agent.Send(s.id, "  Remember   to buy milk  ", ""); err != nil {
		t.Fatal(err)
	}
	c := wait(t, s.db, s.id)
	if c.Title != "Remember to buy milk" || c.Error != "" || s.saved.Load() != nil {
		t.Fatalf("after the first turn: %+v, saved %v", c, s.saved.Load())
	}
	want := `user [{"text":"Remember   to buy milk"}]
assistant [{"toolCall":{"id":"call-1","name":"save_note","input":{"text":"milk"}},"ask":true}]`
	if got := stored(t, s.db, s.id); got != want {
		t.Fatalf("messages:\n%s\nwant:\n%s", got, want)
	}
	if s.last.Instructions != "Save notes." || len(s.last.Tools) != 1 || s.last.Tools[0].Name != "save_note" {
		t.Errorf("request: %+v", s.last)
	}

	if err := s.agent.Answer(s.id, "call-2", true); err != nil {
		t.Fatal(err)
	}
	if c := wait(t, s.db, s.id); c.Error != "the agent is not asking about that tool call" {
		t.Errorf("answering another call: %+v", c)
	}

	if err := s.agent.Answer(s.id, "call-1", true); err != nil {
		t.Fatal(err)
	}
	c = wait(t, s.db, s.id)
	if c.Error != "" || c.Draft != "" || s.saved.Load() != "milk" {
		t.Fatalf("after the answer: %+v, saved %v", c, s.saved.Load())
	}
	want += `
tool [{"toolResult":{"id":"call-1","output":{"saved":true}}}]
assistant [{"text":"Saved the note."}]`
	if got := stored(t, s.db, s.id); got != want {
		t.Errorf("messages:\n%s\nwant:\n%s", got, want)
	}
	var usage string
	s.db.QueryRow(`SELECT usage FROM messages WHERE chat_id = ? ORDER BY seq DESC LIMIT 1`, s.id).Scan(&usage)
	if usage != `{"tokens":5}` {
		t.Errorf("usage = %s", usage)
	}
}

func TestATurnGoesOnWhenYouSayNo(t *testing.T) {
	s := newSetup(t, nil)
	s.agent.Send(s.id, "Remember milk", "")
	wait(t, s.db, s.id)
	if err := s.agent.Answer(s.id, "call-1", false); err != nil {
		t.Fatal(err)
	}
	if c := wait(t, s.db, s.id); c.Error != "" || s.saved.Load() != nil {
		t.Fatalf("%+v, saved %v", c, s.saved.Load())
	}
	got := stored(t, s.db, s.id)
	if !strings.HasSuffix(got, `tool [{"toolResult":{"id":"call-1","error":"The user did not allow this tool call."},"refused":true}]
assistant [{"text":"I did not save it."}]`) {
		t.Errorf("messages:\n%s", got)
	}
}

func TestANewMessageClosesAQuestion(t *testing.T) {
	s := newSetup(t, nil)
	s.agent.Send(s.id, "Remember milk", "")
	wait(t, s.db, s.id)
	s.agent.Send(s.id, "Never mind", "")
	wait(t, s.db, s.id)
	got := stored(t, s.db, s.id)
	want := `tool [{"toolResult":{"id":"call-1","error":"It did not run: the user sent a new message instead of answering."}}]
user [{"text":"Never mind"}]`
	if !strings.Contains(got, want) {
		t.Errorf("messages:\n%s", got)
	}
	// The model was asked again, with a result for every call it made.
	if n := len(s.last.Messages); n != 4 || s.last.Messages[2].Parts[0].ToolResult == nil {
		t.Errorf("request: %+v", s.last.Messages)
	}
}

func TestATurnWithNoModelSaysWhy(t *testing.T) {
	s := newSetup(t, func() (Model, error) { return nil, errors.New("Harbor's agent has no provider") })
	if err := s.agent.Send(s.id, "hello", ""); err != nil {
		t.Fatal(err)
	}
	if c := wait(t, s.db, s.id); c.Error != "Harbor's agent has no provider" {
		t.Errorf("error = %q", c.Error)
	}
	// The chat still shows what was sent.
	if got := stored(t, s.db, s.id); got != `user [{"text":"hello"}]` {
		t.Errorf("messages: %s", got)
	}
}

// waiting is a model that streams some text, then waits to be stopped.
type waiting struct{}

func (waiting) Generate(ctx context.Context, _ provider.Request, emit func(provider.Output)) error {
	emit(provider.Output{Text: "Thinking about"})
	<-ctx.Done()
	return ctx.Err()
}

func TestStopEndsATurn(t *testing.T) {
	s := newSetup(t, func() (Model, error) { return waiting{}, nil })
	s.agent.Send(s.id, "hello", "")
	deadline := time.Now().Add(5 * time.Second)
	for {
		var draft string
		s.db.QueryRow(`SELECT draft FROM chats WHERE id = ?`, s.id).Scan(&draft)
		if draft == "Thinking about" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the draft was not written")
		}
		time.Sleep(10 * time.Millisecond)
	}
	if err := s.agent.Send(s.id, "again", ""); err == nil || err.Error() != "the agent is still on its last turn" {
		t.Errorf("a second message: %v", err)
	}
	s.agent.Stop(s.id)
	if c := wait(t, s.db, s.id); c.Error != "stopped" || c.Draft != "" {
		t.Errorf("%+v", c)
	}
}

func TestATurnCutOffByQuittingSaysSo(t *testing.T) {
	s := newSetup(t, nil)
	s.db.Exec(`UPDATE chats SET working = 1, draft = 'Half a' WHERE id = ?`, s.id)
	New(Config{DB: s.db})
	if c := wait(t, s.db, s.id); c.Error != "Harbor quit during this turn." || c.Draft != "" {
		t.Errorf("%+v", c)
	}
}

func TestAToolsPicturesGoToTheModel(t *testing.T) {
	r := ToolResult("c1", &mcp.CallToolResult{Content: []mcp.Content{&mcp.ImageContent{Data: []byte("JPEG"), MIMEType: "image/jpeg"}}})
	if len(r.Images) != 1 || r.Images[0].MIMEType != "image/jpeg" || r.Images[0].Data != "SlBFRw==" {
		t.Errorf("images = %+v", r.Images)
	}
}
