package benchmark

import (
	"context"
	"database/sql"
	"encoding/json"
	"time"

	"github.com/michaelckearney/monorepo/projects/harbor/backend/engine"
	"github.com/michaelckearney/monorepo/projects/harbor/backend/service"
)

// Evidence is what the grader judges, besides the transcript: what the
// agent made, and what it does. It is the same for every scenario.
type Evidence struct {
	Services   []SavedService `json:"services"`
	Dashboards []Saved        `json:"dashboards"`
	// Prompts are the tool calls Harbor would have asked the user about.
	// The run allowed them all.
	Prompts []Prompt `json:"prompts"`
	// SignIns are the live views the run signed in in, for the agent's
	// browser and the user's.
	SignIns []SignIn `json:"signIns"`
	// Asked are what Harbor asked beside a live view, which the run does
	// not answer.
	Asked []string `json:"asked,omitempty"`
}

// Saved is a saved file.
type Saved struct {
	Name   string `json:"name"`
	Source string `json:"source"`
}

// SavedService is a saved service, and what its tools do.
type SavedService struct {
	Saved
	// Problem is why Harbor cannot read the file, if it cannot.
	Problem string `json:"problem,omitempty"`
	// SignedIn and SignedOut are its read-only tools with no required
	// input, called in the user's browser: signed in, then after the
	// browser was cleared.
	SignedIn  []Call `json:"signedIn"`
	SignedOut []Call `json:"signedOut"`
}

// Call is one call of a tool, and what came of it.
type Call struct {
	Tool   string          `json:"tool"`
	Output json.RawMessage `json:"output,omitempty"`
	Error  string          `json:"error,omitempty"`
}

// Prompt is a tool call Harbor would have asked the user about.
type Prompt struct {
	Tool  string         `json:"tool"`
	Input map[string]any `json:"input"`
}

// callPatience is how long a tool may take in the evidence, a sign-in
// included.
const callPatience = 2 * time.Minute

// gather collects what the agent saved, and calls its services' tools.
func gather(ctx context.Context, h Harbor, db *sql.DB) (Evidence, error) {
	var ev Evidence
	rows, err := db.QueryContext(ctx, `SELECT name, source, manifest, manifest_error FROM services ORDER BY name`)
	if err != nil {
		return ev, err
	}
	for rows.Next() {
		var s SavedService
		var manifest, problem sql.NullString
		if err := rows.Scan(&s.Name, &s.Source, &manifest, &problem); err != nil {
			rows.Close()
			return ev, err
		}
		s.Problem = problem.String
		ev.Services = append(ev.Services, s)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return ev, err
	}
	if rows, err = db.QueryContext(ctx, `SELECT name, source FROM dashboards ORDER BY name`); err != nil {
		return ev, err
	}
	for rows.Next() {
		var d Saved
		if err := rows.Scan(&d.Name, &d.Source); err != nil {
			rows.Close()
			return ev, err
		}
		ev.Dashboards = append(ev.Dashboards, d)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return ev, err
	}

	e := h.Engine(false)
	call := func(svc engine.Service, tool string, wait bool) Call {
		ctx, cancel := context.WithTimeout(ctx, callPatience)
		defer cancel()
		run := e.Call
		if !wait {
			run = e.CallWithoutWaiting
		}
		out, err := run(ctx, svc, tool, json.RawMessage(`{}`))
		c := Call{Tool: tool, Output: out}
		if err != nil {
			c.Error = err.Error()
		}
		return c
	}
	var svcs []engine.Service
	for i, s := range ev.Services {
		if s.Problem != "" {
			svcs = append(svcs, engine.Service{})
			continue
		}
		m, err := service.Read(s.Source)
		if err != nil {
			ev.Services[i].Problem = err.Error()
			svcs = append(svcs, engine.Service{})
			continue
		}
		svc := engine.Service{Manifest: m, Source: s.Source}
		svcs = append(svcs, svc)
		for _, tool := range readTools(m) {
			ev.Services[i].SignedIn = append(ev.Services[i].SignedIn, call(svc, tool, true))
		}
	}
	// Signed out of every site, as when the user's sessions end.
	if err := e.ClearData(ctx, ""); err != nil {
		return ev, err
	}
	for i, svc := range svcs {
		if svc.Source == "" {
			continue
		}
		for _, tool := range readTools(svc.Manifest) {
			ev.Services[i].SignedOut = append(ev.Services[i].SignedOut, call(svc, tool, false))
		}
	}
	return ev, nil
}

// readTools are the service's read-only tools that need no input.
func readTools(m service.Manifest) []string {
	var out []string
	for _, t := range m.Tools {
		if t.Annotations == nil || t.Annotations.ReadOnlyHint == nil || !*t.Annotations.ReadOnlyHint {
			continue
		}
		var schema struct {
			Required []string `json:"required"`
		}
		if len(t.InputSchema) > 0 {
			if err := json.Unmarshal(t.InputSchema, &schema); err != nil {
				continue
			}
		}
		if len(schema.Required) == 0 {
			out = append(out, t.Name)
		}
	}
	return out
}
