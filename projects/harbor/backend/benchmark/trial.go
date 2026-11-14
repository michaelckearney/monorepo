package benchmark

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io/fs"
	"path"
	"strings"
	"sync"
	"time"

	"github.com/michaelckearney/monorepo/projects/harbor/backend/chat"
	"github.com/michaelckearney/monorepo/projects/harbor/backend/engine"
	"github.com/michaelckearney/monorepo/projects/harbor/backend/provider"
)

// Trial is one scenario, run once on one model.
type Trial struct {
	Scenario string `json:"scenario"`
	Model    string `json:"model"`
	// N counts the scenario's repeats on the model, from 1.
	N int `json:"n"`
	// Seconds is how long the agent's turn took.
	Seconds float64 `json:"seconds"`
	// Error is why the turn stopped, if it failed.
	Error      string    `json:"error,omitempty"`
	Usage      Usage     `json:"usage"`
	Transcript []Message `json:"transcript"`
	Evidence   Evidence  `json:"evidence"`
	// Grade is nil when the trial was not graded, and NotGraded says why.
	Grade     *Grade `json:"grade,omitempty"`
	NotGraded string `json:"notGraded,omitempty"`
}

// Message is one message of the transcript, as the chat stores it.
type Message struct {
	Role  string          `json:"role"`
	Parts []provider.Part `json:"parts"`
}

// Usage is what the agent's model used: the grader's is not counted.
type Usage struct {
	Calls        int `json:"calls"`
	InputTokens  int `json:"inputTokens"`
	OutputTokens int `json:"outputTokens"`
}

// Harbor is Harbor's backend, put together for one trial on the trial's
// own database and data folder. Package backend makes it with Open: this
// package cannot import backend, which serves it.
type Harbor interface {
	SaveService(source string) error
	SaveDashboard(name, source string) error
	// SignIn signs the user's browser in to the service's site.
	SignIn(ctx context.Context, service string) (bool, error)
	NewChat(ctx context.Context) (string, error)
	Send(chatID, text string) error
	Stop(chatID string)
	// Engine is the user's browser, or the agent's.
	Engine(agent bool) *engine.Engine
	// Close stops the agent's turns and both browsers.
	Close()
}

// Open makes a trial's Harbor: window shows both browsers' live views,
// prompted is told each tool call Harbor would have asked the user about,
// and the agent runs on agent.
type Open func(db *sql.DB, dir string, window func(agent bool) engine.Window, prompted func(tool string, input map[string]any), agent chat.Model) Harbor

// Config is how a trial runs.
type Config struct {
	Open Open
	// Dir is the trial's own folder: its browsers' profiles.
	Dir string
	// DB is the trial's own database, with Harbor's tables and nothing in
	// them.
	DB *sql.DB
	// Files are the scenarios' folder, which setups name their files from.
	Files fs.FS
	// Agent is the model under test, and ModelName its name.
	Agent     chat.Model
	ModelName string
	Grader    chat.Model
	// TurnTimeout is how long the agent's turn may take.
	TurnTimeout time.Duration
	// Progress, if set, is told the chat so far, and what the agent's model
	// has used, once the turn starts and whenever a message is added, so
	// the trial can be followed as it goes.
	Progress func(transcript []Message, u Usage) error
}

// TurnTimeout is how long a turn may take in a run from the Benchmarks
// page.
const TurnTimeout = 20 * time.Minute

// Run runs a scenario once: setup, the user's message, the agent's turn,
// the evidence, and the grade. Its error is the run's own failure, such
// as a setup that does not work, or ctx ending; the agent's failures are
// in the trial.
func Run(ctx context.Context, s Scenario, n int, c Config) (*Trial, error) {
	t := &Trial{Scenario: s.Name, Model: c.ModelName, N: n}
	var prompts []Prompt
	var mu sync.Mutex
	user := &signIns{}
	// The user allows everything, and the evidence says what Harbor would
	// have asked.
	h := c.Open(c.DB, c.Dir, user.window, func(tool string, input map[string]any) {
		mu.Lock()
		prompts = append(prompts, Prompt{Tool: tool, Input: input})
		mu.Unlock()
	}, c.Agent)
	user.h = h
	defer h.Close()

	if err := setUp(ctx, h, s, c.Files); err != nil {
		return nil, fmt.Errorf("setting up %s: %w", s.Name, err)
	}

	chatID, err := h.NewChat(ctx)
	if err != nil {
		return nil, err
	}
	start := time.Now()
	if err := h.Send(chatID, s.Prompt); err != nil {
		return nil, err
	}
	shown := -1
	progress := func() error {
		if c.Progress == nil {
			return nil
		}
		var n int
		if err := c.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM messages WHERE chat_id = ?`, chatID).Scan(&n); err != nil || n == shown {
			return err
		}
		msgs, u, err := transcript(ctx, c.DB, chatID)
		if err != nil {
			return err
		}
		shown = n
		return c.Progress(msgs, u)
	}
	t.Error, err = wait(ctx, h, c.DB, chatID, c.TurnTimeout, progress)
	if err != nil {
		return nil, err
	}
	t.Seconds = time.Since(start).Seconds()
	if t.Transcript, t.Usage, err = transcript(ctx, c.DB, chatID); err != nil {
		return nil, err
	}

	if t.Evidence, err = gather(ctx, h, c.DB); err != nil {
		return nil, fmt.Errorf("gathering the evidence: %w", err)
	}
	mu.Lock()
	t.Evidence.Prompts = prompts
	mu.Unlock()
	t.Evidence.SignIns, t.Evidence.Asked = user.done()

	if t.Grade, err = grade(ctx, c.Grader, s, t); err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		t.NotGraded = err.Error()
	}
	return t, nil
}

// setUp saves the setup's services and dashboards, and signs in to its
// sites.
func setUp(ctx context.Context, h Harbor, s Scenario, files fs.FS) error {
	for _, f := range s.Setup.Services {
		source, err := fs.ReadFile(files, f)
		if err != nil {
			return err
		}
		if err := h.SaveService(string(source)); err != nil {
			return fmt.Errorf("%s: %w", f, err)
		}
	}
	for _, f := range s.Setup.Dashboards {
		source, err := fs.ReadFile(files, f)
		if err != nil {
			return err
		}
		name := strings.TrimSuffix(path.Base(f), path.Ext(f))
		if err := h.SaveDashboard(name, string(source)); err != nil {
			return fmt.Errorf("%s: %w", f, err)
		}
	}
	for _, name := range s.Setup.SignedIn {
		ctx, cancel := context.WithTimeout(ctx, callPatience)
		ok, err := h.SignIn(ctx, name)
		cancel()
		if err != nil {
			return fmt.Errorf("signing in to %s: %w", name, err)
		}
		if !ok {
			return fmt.Errorf("could not sign in to %s", name)
		}
	}
	return nil
}

// wait waits for the agent's turn to end, and returns its error, if it
// failed. A turn that takes longer than timeout is stopped. progress runs
// each time it looks, the last time once the turn has ended.
func wait(ctx context.Context, h Harbor, db *sql.DB, chatID string, timeout time.Duration, progress func() error) (string, error) {
	deadline := time.Now().Add(timeout)
	stopped := false
	for {
		var working bool
		var failure string
		if err := db.QueryRowContext(ctx, `SELECT working, error FROM chats WHERE id = ?`, chatID).Scan(&working, &failure); err != nil {
			return "", err
		}
		if err := progress(); err != nil {
			h.Stop(chatID)
			return "", err
		}
		if !working {
			if stopped {
				failure = fmt.Sprintf("the turn took longer than %s, so the run stopped it", timeout)
			}
			return failure, nil
		}
		if !stopped && time.Now().After(deadline) {
			h.Stop(chatID)
			stopped = true
		}
		select {
		case <-ctx.Done():
			h.Stop(chatID)
			return "", ctx.Err()
		case <-time.After(500 * time.Millisecond):
		}
	}
}

// transcript is the chat's messages, and what the agent's model used.
// Usage is counted from OpenAI's names for it, the one provider Harbor has.
func transcript(ctx context.Context, db *sql.DB, chatID string) ([]Message, Usage, error) {
	var u Usage
	rows, err := db.QueryContext(ctx, `SELECT role, parts, usage FROM messages WHERE chat_id = ? ORDER BY seq`, chatID)
	if err != nil {
		return nil, u, err
	}
	defer rows.Close()
	var out []Message
	for rows.Next() {
		var m Message
		var parts string
		var usage sql.NullString
		if err := rows.Scan(&m.Role, &parts, &usage); err != nil {
			return nil, u, err
		}
		if err := json.Unmarshal([]byte(parts), &m.Parts); err != nil {
			return nil, u, err
		}
		out = append(out, m)
		if m.Role != "assistant" {
			continue
		}
		u.Calls++
		var counts struct {
			Input  int `json:"input_tokens"`
			Output int `json:"output_tokens"`
		}
		if usage.Valid && json.Unmarshal([]byte(usage.String), &counts) == nil {
			u.InputTokens += counts.Input
			u.OutputTokens += counts.Output
		}
	}
	return out, u, rows.Err()
}
