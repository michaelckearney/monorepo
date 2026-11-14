package benchmark

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"

	"github.com/michaelckearney/monorepo/projects/harbor/backend/chat"
	"github.com/michaelckearney/monorepo/projects/harbor/backend/provider"
)

// Grading, as in the old Harbor (automations' pkg/benchmarks): the grader
// scores each criterion from 0 to 100, and Go weighs them into the trial's
// score, so the overall number is arithmetic, not the model's opinion.

// Grade is the grader's report on a trial.
type Grade struct {
	// Score is the criteria's scores, weighted, from 0 to 100.
	Score    float64  `json:"score"`
	Summary  string   `json:"summary"`
	Criteria []Scored `json:"criteria"`
	// Usage is the grader's last try's; Tries is how many it took.
	Usage json.RawMessage `json:"usage,omitempty"`
	Tries int             `json:"tries,omitempty"`
}

// Scored is one criterion's score.
type Scored struct {
	ID        string   `json:"id"`
	Title     string   `json:"title"`
	Weight    float64  `json:"weight"`
	Score     float64  `json:"score"`
	Rationale string   `json:"rationale"`
	Evidence  []string `json:"evidence"`
	Issues    []string `json:"issues"`
}

const graderInstructions = `You grade one trial of a benchmark of Harbor's agent. Harbor connects to websites through services: TypeScript files whose tools run in the user's browser, signed in as them. Harbor's agent makes and fixes services, and dashboards that show what services' tools return.

In the trial, the user sent the agent one message, on test sites that stand in for real ones. You get the message, what existed before it, the agent's transcript, what it saved, and what the saved services' tools returned when the run called them: signed in, and again after the user's browser was signed out of every site. A sign-in in the trial was done by the run, standing in for the user; the agent only caused it.

Score each criterion on its own, from 0 to 100:

100: done, fully and correctly.
75: done, with a small flaw the user would hardly notice.
50: partly done, or done with a flaw the user would notice.
25: attempted, but mostly wrong.
0: not done, or wrong.

Use a score between these when the trial falls between them. Judge what the agent made and what it did, not what it says it did: where its reply and the evidence disagree, the evidence wins. Quote the evidence you rely on. Do not give an overall score; Harbor computes it from the criteria.

Answer by calling report, once, with the whole report.`

// reportSchema is the report tool's input: the criteria, keyed by id, in
// the scenario's order, each with its evidence and issues before its
// score, and the summary last, the order to think in. Built from a Go map,
// the keys came out sorted.
//
// The server does not hold the call to it: the tool is not strict. Held to
// it as structured output, the report came back well formed, but the
// grammar allows any whitespace between JSON tokens, and Qwen, two
// versions on two vLLM builds, wrote spaces for an hour (vLLM issue
// #38696). A strict tool call on vLLM still came back with lists as
// strings. So Go checks the report, and a wrong one is sent back with
// what is wrong (grade).
func reportSchema(criteria []Criterion) json.RawMessage {
	texts := object{{"type", "array"}, {"items", object{{"type", "string"}}}}
	scored := object{}
	ids := make([]string, len(criteria))
	for i, c := range criteria {
		ids[i] = c.ID
		scored = append(scored, field{c.ID, object{
			{"type", "object"},
			{"description", c.Title},
			{"additionalProperties", false},
			{"required", []string{"evidence", "issues", "rationale", "score"}},
			{"properties", object{
				{"evidence", texts},
				{"issues", texts},
				{"rationale", object{{"type", "string"}}},
				{"score", object{{"type", "number"}, {"minimum", 0}, {"maximum", 100}}},
			}},
		}})
	}
	schema, _ := json.Marshal(object{
		{"type", "object"},
		{"additionalProperties", false},
		{"required", []string{"criteria", "summary"}},
		{"properties", object{
			{"criteria", object{
				{"type", "object"},
				{"additionalProperties", false},
				{"required", ids},
				{"properties", scored},
			}},
			{"summary", object{{"type", "string"}, {"description", "What the agent did, and how well, in two or three sentences."}}},
		}},
	})
	return schema
}

// object is a JSON object that keeps its fields in order.
type object []field

type field struct {
	key   string
	value any
}

func (o object) MarshalJSON() ([]byte, error) {
	var b strings.Builder
	b.WriteByte('{')
	for i, f := range o {
		if i > 0 {
			b.WriteByte(',')
		}
		k, err := json.Marshal(f.key)
		if err != nil {
			return nil, err
		}
		v, err := json.Marshal(f.value)
		if err != nil {
			return nil, err
		}
		b.Write(k)
		b.WriteByte(':')
		b.Write(v)
	}
	b.WriteByte('}')
	return []byte(b.String()), nil
}

// graderTries is how many reports the grader may send before the trial is
// not graded: a wrong one goes back to it with what is wrong.
const graderTries = 3

// grade asks the grader to score the trial. An error leaves the trial not
// graded: it is the grader's failure, not the agent's.
func grade(ctx context.Context, grader chat.Model, s Scenario, t *Trial) (*Grade, error) {
	report := provider.Tool{Name: "report", Description: "Report your grade of the trial.", InputSchema: reportSchema(s.Criteria)}
	messages := []provider.Message{{Role: "user", Parts: []provider.Part{{Text: graderMessage(s, t)}}}}
	for try := 1; ; try++ {
		var usage json.RawMessage
		var text strings.Builder
		var call *provider.ToolCall
		err := grader.Generate(ctx, provider.Request{Instructions: graderInstructions, Messages: messages, Tools: []provider.Tool{report}}, func(out provider.Output) {
			switch {
			case out.Text != "":
				text.WriteString(out.Text)
			case out.ToolCall != nil && call == nil:
				call = out.ToolCall
			case out.Done:
				usage = out.Usage
			}
		})
		if err != nil {
			return nil, err
		}
		g, sent, problem := readCall(call, text.String(), s.Criteria)
		if problem == nil {
			g.Usage, g.Tries = usage, try
			return g, nil
		}
		// What it sent goes with the error, to tell the grader's mistake
		// from Harbor's.
		if try == graderTries {
			return nil, fmt.Errorf("%w, after %d tries. It sent last: %s", problem, graderTries, cut(sent, 2000))
		}
		reply := provider.Message{Role: "assistant", Parts: []provider.Part{}}
		if text.Len() > 0 {
			reply.Parts = append(reply.Parts, provider.Part{Text: text.String()})
		}
		if call == nil {
			messages = append(messages, reply, provider.Message{Role: "user", Parts: []provider.Part{{Text: problem.Error() + ". Call report with the whole report."}}})
			continue
		}
		reply.Parts = append(reply.Parts, provider.Part{ToolCall: call})
		messages = append(messages, reply, provider.Message{Role: "tool", Parts: []provider.Part{{ToolResult: &provider.ToolResult{ID: call.ID, Error: problem.Error() + ". Call report again with the whole report, fixed."}}}})
	}
}

// readCall is the grade a reply's report call gives, and what it sent.
func readCall(call *provider.ToolCall, text string, criteria []Criterion) (*Grade, string, error) {
	if call == nil {
		return nil, text, errors.New("the grader answered without calling report")
	}
	input, err := json.Marshal(call.Input)
	if err != nil {
		return nil, "", err
	}
	if call.Name != "report" {
		return nil, string(input), fmt.Errorf("the grader called %s, not report", call.Name)
	}
	g, err := readReport(input, criteria)
	return g, string(input), err
}

// readReport checks the grader's report: every criterion scored, from 0 to
// 100, and no others.
func readReport(report []byte, criteria []Criterion) (*Grade, error) {
	var r struct {
		Criteria map[string]struct {
			Evidence  []string `json:"evidence"`
			Issues    []string `json:"issues"`
			Rationale string   `json:"rationale"`
			Score     *float64 `json:"score"`
		} `json:"criteria"`
		Summary string `json:"summary"`
	}
	if err := json.Unmarshal(report, &r); err != nil {
		return nil, fmt.Errorf("the grader's report is malformed: %w", err)
	}
	known := map[string]bool{}
	for _, c := range criteria {
		known[c.ID] = true
	}
	for id := range r.Criteria {
		if !known[id] {
			return nil, fmt.Errorf("the grader scored %q, which is not a criterion", id)
		}
	}
	// In the scenario's order, so reports line up.
	g := &Grade{Summary: strings.TrimSpace(r.Summary)}
	for _, c := range criteria {
		item, ok := r.Criteria[c.ID]
		switch {
		case !ok:
			return nil, fmt.Errorf("the grader did not score %s", c.ID)
		case item.Score == nil || *item.Score < 0 || *item.Score > 100 || math.IsNaN(*item.Score):
			return nil, fmt.Errorf("the grader's score for %s is not from 0 to 100", c.ID)
		}
		g.Criteria = append(g.Criteria, Scored{ID: c.ID, Title: c.Title, Weight: c.Weight, Score: *item.Score,
			Rationale: strings.TrimSpace(item.Rationale), Evidence: item.Evidence, Issues: item.Issues})
	}
	g.Score = weighted(g.Criteria)
	return g, nil
}

func weighted(scores []Scored) float64 {
	var sum, weights float64
	for _, s := range scores {
		sum += s.Score * s.Weight
		weights += s.Weight
	}
	return math.Round(sum/weights*10) / 10
}

// The grader's view of a long trial is cut, so it fits: tool results the
// agent read in full are cut to their start.
const (
	resultLimit = 3000
	outputLimit = 8000
)

// graderMessage is the trial, as the grader reads it.
func graderMessage(s Scenario, t *Trial) string {
	var b strings.Builder
	section := func(title string) { fmt.Fprintf(&b, "\n## %s\n\n", title) }
	asJSON := func(v any) string {
		raw, _ := json.MarshalIndent(v, "", "  ")
		return string(raw)
	}

	section("The user's message")
	b.WriteString(s.Prompt + "\n")
	section("What existed before it")
	if len(s.Setup.Services)+len(s.Setup.Dashboards)+len(s.Setup.SignedIn) == 0 {
		b.WriteString("Nothing: no services, no dashboards, and the user's browser signed in to nothing.\n")
	} else {
		b.WriteString(asJSON(s.Setup) + "\n")
	}
	section("Criteria")
	b.WriteString(asJSON(s.Criteria) + "\n")

	section("The agent's transcript")
	if t.Error != "" {
		fmt.Fprintf(&b, "The turn ended with an error: %s\n\n", t.Error)
	}
	for _, m := range t.Transcript {
		for _, p := range m.Parts {
			switch {
			case p.Text != "":
				fmt.Fprintf(&b, "[%s] %s\n\n", m.Role, p.Text)
			case p.ToolCall != nil:
				input, _ := json.Marshal(p.ToolCall.Input)
				fmt.Fprintf(&b, "[agent calls %s] %s\n\n", p.ToolCall.Name, cut(string(input), resultLimit))
			case p.ToolResult != nil:
				if p.ToolResult.Error != "" {
					fmt.Fprintf(&b, "[error] %s\n\n", cut(p.ToolResult.Error, resultLimit))
					continue
				}
				out, _ := json.Marshal(p.ToolResult.Output)
				if s, ok := p.ToolResult.Output.(string); ok {
					out = []byte(s)
				}
				fmt.Fprintf(&b, "[result] %s\n", cut(string(out), resultLimit))
				if n := len(p.ToolResult.Images); n > 0 {
					fmt.Fprintf(&b, "(and %d picture(s), not shown)\n", n)
				}
				b.WriteString("\n")
			}
		}
	}

	section("What the agent saved, and what its services return")
	if len(t.Evidence.Services) == 0 {
		b.WriteString("No services.\n\n")
	}
	for _, svc := range t.Evidence.Services {
		fmt.Fprintf(&b, "### Service %s\n\n```ts\n%s\n```\n\n", svc.Name, svc.Source)
		if svc.Problem != "" {
			fmt.Fprintf(&b, "Harbor cannot read it: %s\n\n", svc.Problem)
			continue
		}
		if len(svc.SignedIn) == 0 {
			b.WriteString("It has no read-only tool without required input, so the run called none.\n\n")
		}
		for _, phase := range []struct {
			title string
			calls []Call
		}{{"signed in", svc.SignedIn}, {"signed out of every site", svc.SignedOut}} {
			for _, c := range phase.calls {
				if c.Error != "" {
					fmt.Fprintf(&b, "%s with {}, %s: error: %s\n\n", c.Tool, phase.title, c.Error)
				} else {
					fmt.Fprintf(&b, "%s with {}, %s: %s\n\n", c.Tool, phase.title, cut(string(c.Output), outputLimit))
				}
			}
		}
	}
	if len(t.Evidence.Dashboards) == 0 {
		b.WriteString("No dashboards.\n\n")
	}
	for _, d := range t.Evidence.Dashboards {
		fmt.Fprintf(&b, "### Dashboard %s\n\n(The run cannot render a dashboard yet: judge it from its file.)\n\n```vue\n%s\n```\n\n", d.Name, d.Source)
	}

	section("Tool calls Harbor would have asked the user about")
	if len(t.Evidence.Prompts) == 0 {
		b.WriteString("None.\n")
	} else {
		b.WriteString("The run allowed each one.\n\n" + asJSON(t.Evidence.Prompts) + "\n")
	}
	section("Sign-ins the run did for the user")
	if len(t.Evidence.SignIns) == 0 {
		b.WriteString("None.\n")
	} else {
		b.WriteString(asJSON(t.Evidence.SignIns) + "\n")
	}
	return b.String()
}

func cut(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + fmt.Sprintf("… (%d more characters)", len(s)-n)
}
