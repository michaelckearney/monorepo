package main

import (
	"flag"
	"slices"
	"strings"
	"testing"

	"github.com/michaelckearney/monorepo/projects/harbor/backend/benchmark"
	"github.com/michaelckearney/monorepo/projects/harbor/backend/provider"
)

// An agent puts flags after the arguments as often as before them.
func TestFlagsGoAnywhere(t *testing.T) {
	fs := flag.NewFlagSet("benchmark show", flag.ContinueOnError)
	asJSON := fs.Bool("json", false, "")
	var models list
	fs.Var(&models, "model", "")
	rest, err := parse(fs, []string{"4", "-json", "-model", "a/b,c/d", "-model=e/f"})
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(rest, []string{"4"}) || !*asJSON || !slices.Equal(models, list{"a/b", "c/d", "e/f"}) {
		t.Errorf("rest %q, json %v, models %q", rest, *asJSON, models)
	}
	if _, err := parse(fs, []string{"-jsn"}); err == nil {
		t.Error("an unknown flag was accepted")
	}
}

func score(v float64) *float64 { return &v }

func TestMeansAreOfRepeatedScenariosGraded(t *testing.T) {
	trials := []trialRow{
		{Scenario: "a", Model: "m", Score: score(80)},
		{Scenario: "a", Model: "m", Score: score(60)},
		{Scenario: "a", Model: "m", Status: "failed"},
		{Scenario: "b", Model: "m", Score: score(10)},
	}
	got := summarise(trials)
	if len(got) != 1 {
		t.Fatalf("summaries: %+v", got)
	}
	if m := got[0]; m.scenario != "a" || m.trials != 3 || m.graded != 2 || *m.mean != 70 || *m.lowest != 60 {
		t.Errorf("a on m: %+v, mean %v, lowest %v", m, *m.mean, *m.lowest)
	}
}

func TestProgressSaysHowATrialEnded(t *testing.T) {
	secs := 12.34
	for _, c := range []struct {
		t    trialRow
		want string
	}{
		{trialRow{ID: 3, Scenario: "s", Model: "p/m", N: 2, Status: "running"}, "trial 3, s on p/m, trial 2: running"},
		{trialRow{ID: 3, Scenario: "s", Model: "p/m", N: 2, Status: "done", Score: score(87.5), Seconds: &secs}, "trial 3, s on p/m, trial 2: graded, 87.5 in 12.3s"},
		{trialRow{ID: 3, Scenario: "s", Model: "p/m", N: 2, Status: "done"}, "trial 3, s on p/m, trial 2: not graded"},
		{trialRow{ID: 3, Scenario: "s", Model: "p/m", N: 2, Status: "failed", Error: "the setup\nfailed"}, "trial 3, s on p/m, trial 2: failed: the setup failed"},
	} {
		if got := progressLine(c.t); got != c.want {
			t.Errorf("got %q, want %q", got, c.want)
		}
	}
}

// A tool's result is shown with its call, and an image is left out.
func TestTranscriptPairsCallsWithResults(t *testing.T) {
	var b strings.Builder
	writeTranscript(&b, []benchmark.Message{
		{Role: "user", Parts: []provider.Part{{Text: "Hi"}}},
		{Role: "assistant", Parts: []provider.Part{
			{ToolCall: &provider.ToolCall{ID: "1", Name: "look", Input: map[string]any{"at": "x"}}},
			{ToolCall: &provider.ToolCall{ID: "2", Name: "save", Input: map[string]any{}}},
		}},
		{Role: "tool", Parts: []provider.Part{
			{ToolResult: &provider.ToolResult{ID: "2", Error: "no"}},
			{ToolResult: &provider.ToolResult{ID: "1", Output: strings.Repeat("y", 3000), Images: []provider.Image{{MIMEType: "image/png", Data: "AAAA"}}}},
		}},
		{Role: "assistant", Parts: []provider.Part{{Text: "Done."}}},
	}, false)
	got := b.String()
	for _, want := range []string{
		"**You:** Hi",
		"**Tool `look`**, done:",
		"Result, with 1 image(s) not shown:",
		"1002 more characters; -full shows them",
		"**Tool `save`**, failed:",
		"Error: no",
		"**Agent:** Done.",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
	if strings.Contains(got, "AAAA") || strings.Index(got, "`look`") > strings.Index(got, "`save`") {
		t.Errorf("the transcript:\n%s", got)
	}
}
