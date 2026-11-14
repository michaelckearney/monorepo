package benchmark

import (
	"context"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/michaelckearney/monorepo/projects/harbor/backend/provider"
)

func TestEveryScenarioReads(t *testing.T) {
	scenarios, err := Scenarios(Files)
	if err != nil {
		t.Fatal(err)
	}
	if len(scenarios) == 0 {
		t.Fatal("no scenarios")
	}
}

func TestAScenarioWithProblemsSaysEachOne(t *testing.T) {
	_, err := Scenarios(fstest.MapFS{"bad.yaml": {Data: []byte(`
prompt: ""
setup:
  services: [setup/missing.ts]
criteria:
  - id: Data
    title: Data
    weight: 0
    description: ""
`)}})
	for _, want := range []string{"no prompt", "not lower-case", "needs a title and a description", "weight above 0", "setup/missing.ts"} {
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("no %q in %v", want, err)
		}
	}
	if _, err := Scenarios(fstest.MapFS{"bad.yaml": {Data: []byte("prompt: hi\nexpect: {}\n")}}); err == nil || !strings.Contains(err.Error(), "expect") {
		t.Errorf("an unknown field: %v", err)
	}
}

var criteria = []Criterion{
	{ID: "data", Title: "Data", Weight: 3, Description: "d"},
	{ID: "scope", Title: "Scope", Weight: 1, Description: "s"},
}

func TestTheScoreIsTheCriteriasWeighed(t *testing.T) {
	g, err := readReport([]byte(`{"criteria": {
		"scope": {"evidence": [], "issues": ["x"], "rationale": "r", "score": 0},
		"data": {"evidence": ["q"], "issues": [], "rationale": "r", "score": 100}
	}, "summary": "ok"}`), criteria)
	if err != nil {
		t.Fatal(err)
	}
	if g.Score != 75 || g.Criteria[0].ID != "data" || g.Criteria[1].Issues[0] != "x" {
		t.Errorf("grade = %+v", g)
	}
}

func TestABadReportLeavesTheTrialNotGraded(t *testing.T) {
	for name, report := range map[string]string{
		"not JSON":  `{"criteria": `,
		"missing":   `{"criteria": {"data": {"score": 50}}}`,
		"unknown":   `{"criteria": {"data": {"score": 50}, "scope": {"score": 1}, "style": {"score": 1}}}`,
		"too high":  `{"criteria": {"data": {"score": 150}, "scope": {"score": 1}}}`,
		"no number": `{"criteria": {"data": {}, "scope": {"score": 1}}}`,
	} {
		if _, err := readReport([]byte(report), criteria); err == nil {
			t.Errorf("%s: a report was taken", name)
		}
	}
}

// The schema asks for the report in the order to think in: each criterion
// in the scenario's order, its score after its evidence, the summary last.
func TestTheReportIsAskedForInOrder(t *testing.T) {
	want := `{"type":"object","additionalProperties":false,"required":["criteria","summary"],"properties":{"criteria":{"type":"object","additionalProperties":false,"required":["data","scope"],"properties":{` +
		`"data":{"type":"object","description":"Data","additionalProperties":false,"required":["evidence","issues","rationale","score"],"properties":{"evidence":{"type":"array","items":{"type":"string"}},"issues":{"type":"array","items":{"type":"string"}},"rationale":{"type":"string"},"score":{"type":"number","minimum":0,"maximum":100}}},` +
		`"scope":{"type":"object","description":"Scope","additionalProperties":false,"required":["evidence","issues","rationale","score"],"properties":{"evidence":{"type":"array","items":{"type":"string"}},"issues":{"type":"array","items":{"type":"string"}},"rationale":{"type":"string"},"score":{"type":"number","minimum":0,"maximum":100}}}}},` +
		`"summary":{"type":"string","description":"What the agent did, and how well, in two or three sentences."}}}`
	if got := string(reportSchema(criteria)); got != want {
		t.Errorf("schema:\n%s\nwant\n%s", got, want)
	}
}

// replies is a grader that gives its replies in turn, and records what it
// was asked.
type replies struct {
	left  [][]provider.Output
	asked []provider.Request
}

func (r *replies) Generate(_ context.Context, req provider.Request, emit func(provider.Output)) error {
	r.asked = append(r.asked, req)
	for _, out := range r.left[0] {
		emit(out)
	}
	r.left = r.left[1:]
	return nil
}

func reportCall(score any) []provider.Output {
	return []provider.Output{{ToolCall: &provider.ToolCall{ID: "r", Name: "report", Input: map[string]any{
		"criteria": map[string]any{
			"data":  map[string]any{"evidence": []any{}, "issues": []any{}, "rationale": "r", "score": score},
			"scope": map[string]any{"evidence": []any{}, "issues": []any{}, "rationale": "r", "score": 100},
		},
		"summary": "ok",
	}}}, {Done: true}}
}

// A reply with no report call is asked again for one; three wrong reports
// leave the trial not graded, with the last.
func TestTheGraderGetsThreeTries(t *testing.T) {
	s := Scenario{Criteria: criteria}
	r := &replies{left: [][]provider.Output{{{Text: "It did well."}, {Done: true}}, reportCall(100)}}
	g, err := grade(t.Context(), r, s, &Trial{})
	if err != nil || g.Score != 100 || g.Tries != 2 {
		t.Fatalf("grade = %+v, %v", g, err)
	}
	if again := r.asked[1].Messages; len(again) != 3 || again[1].Parts[0].Text != "It did well." || !strings.Contains(again[2].Parts[0].Text, "without calling report") {
		t.Errorf("asked again with %+v", again)
	}

	r = &replies{left: [][]provider.Output{reportCall(150), reportCall(150), reportCall(150)}}
	if _, err := grade(t.Context(), r, s, &Trial{}); err == nil || !strings.Contains(err.Error(), "not from 0 to 100, after 3 tries. It sent last: ") {
		t.Errorf("three wrong reports: %v", err)
	}
	if len(r.asked) != 3 {
		t.Errorf("asked %d times", len(r.asked))
	}
}
