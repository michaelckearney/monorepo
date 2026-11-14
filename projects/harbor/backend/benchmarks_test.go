package backend

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/michaelckearney/monorepo/framework"
	"github.com/michaelckearney/monorepo/projects/harbor/backend/benchmark"
	"github.com/michaelckearney/monorepo/projects/harbor/backend/chat"
	"github.com/michaelckearney/monorepo/projects/harbor/backend/fixture"
	"github.com/michaelckearney/monorepo/projects/harbor/backend/provider"
)

// scripted is a model that gives its replies in turn, and records what it
// was asked.
type scripted struct {
	replies [][]provider.Output
	asked   []provider.Request
}

func (s *scripted) Generate(_ context.Context, req provider.Request, emit func(provider.Output)) error {
	s.asked = append(s.asked, req)
	if len(s.replies) == 0 {
		return fmt.Errorf("the script ran out")
	}
	for _, out := range s.replies[0] {
		emit(out)
	}
	s.replies = s.replies[1:]
	return nil
}

var _ chat.Model = (*scripted)(nil)

// fixtureService is one of the dev fixture services, which work on the fixtures.
func fixtureService(t *testing.T, name string) string {
	t.Helper()
	source, err := os.ReadFile(filepath.Join("..", "database", "dev", "assets", "services", name+".ts"))
	if err != nil {
		t.Fatal(err)
	}
	return string(source)
}

// A whole trial on the fixture sites, with scripted models. The setup
// signs in to Tidewater (a TOTP code) and Tasklane (OIDC with consent);
// the agent saves the Parcel service, whose tools the run calls signed in,
// which signs in with an emailed code, and then signed out; and the
// grader's report is the trial's grade.
func TestABenchmarkTrial(t *testing.T) {
	// It starts the fixture sites and a browser, and takes most of a minute:
	// the rest of the unit tests take seconds. It runs in the whole of
	// harness test, and is skipped by the runs of a few tests, which pass
	// -short.
	if testing.Short() {
		t.Skip("slow: runs in the whole of harness test")
	}
	sites, err := fixture.Start(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(sites.Stop)
	files := fstest.MapFS{}
	for _, name := range []string{"tidewater", "tasklane"} {
		files[name+".ts"] = &fstest.MapFile{Data: []byte(fixtureService(t, name))}
	}
	agent := &scripted{replies: [][]provider.Output{
		{{ToolCall: &provider.ToolCall{ID: "c1", Name: "write_service", Input: map[string]any{"source": fixtureService(t, "parcel")}}}, {Done: true, Usage: []byte(`{"input_tokens":100,"output_tokens":20}`)}},
		{{Text: "I made the Parcel service."}, {Done: true, Usage: []byte(`{"input_tokens":150,"output_tokens":10}`)}},
	}}
	// The grader's first report has a list as a string, as Qwen once sent;
	// it is sent back with what is wrong, and the second is the grade.
	report := func(issues any) provider.Output {
		return provider.Output{ToolCall: &provider.ToolCall{ID: "r", Name: "report", Input: map[string]any{
			"criteria": map[string]any{
				"data":  map[string]any{"evidence": []any{}, "issues": issues, "rationale": "r", "score": 100},
				"scope": map[string]any{"evidence": []any{}, "issues": []any{}, "rationale": "r", "score": 50},
			},
			"summary": "It made the service.",
		}}}
	}
	grader := &scripted{replies: [][]provider.Output{
		{report("none"), {Done: true}},
		{report([]any{}), {Done: true}},
	}}
	s := benchmark.Scenario{
		Name:   "parcel",
		Prompt: "Make me a Parcel service.",
		Setup:  benchmark.Setup{Services: []string{"tidewater.ts", "tasklane.ts"}, SignedIn: []string{"tidewater", "tasklane"}},
		Criteria: []benchmark.Criterion{
			{ID: "data", Title: "Data", Weight: 3, Description: "d"},
			{ID: "scope", Title: "Scope", Weight: 1, Description: "s"},
		},
	}
	// The chat as it goes: it only grows, and ends as the trial's.
	var progress []int
	var last benchmark.Usage
	trial, err := benchmark.Run(t.Context(), s, 1, benchmark.Config{
		Open: openTrial, Dir: t.TempDir(), DB: desktop.TestDB(t), Files: files,
		Agent: agent, ModelName: "scripted", Grader: grader, TurnTimeout: time.Minute,
		Progress: func(transcript []benchmark.Message, u benchmark.Usage) error {
			progress = append(progress, len(transcript))
			last = u
			return nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if trial.Error != "" || len(trial.Transcript) != 4 || trial.Usage != (benchmark.Usage{Calls: 2, InputTokens: 250, OutputTokens: 30}) {
		t.Errorf("the turn: error %q, %d messages, usage %+v", trial.Error, len(trial.Transcript), trial.Usage)
	}
	if len(progress) == 0 || !slices.IsSorted(progress) || progress[len(progress)-1] != 4 || last != trial.Usage {
		t.Errorf("progress: %v messages, last usage %+v", progress, last)
	}
	var parcel *benchmark.SavedService
	for i, svc := range trial.Evidence.Services {
		if svc.Name == "parcel" {
			parcel = &trial.Evidence.Services[i]
		}
	}
	if parcel == nil {
		t.Fatalf("services = %+v", trial.Evidence.Services)
	}
	in := map[string]benchmark.Call{}
	for _, c := range parcel.SignedIn {
		in[c.Tool] = c
	}
	if c := in["list_orders"]; c.Error != "" || !strings.Contains(string(c.Output), "P1364274-046") {
		t.Errorf("list_orders, signed in = %+v; the run's sign-ins: %+v", c, trial.Evidence.SignIns)
	}
	for _, c := range parcel.SignedOut {
		if c.Error == "" || !regexp.MustCompile(`(?i)sign in`).MatchString(c.Error) {
			t.Errorf("%s, signed out = %+v", c.Tool, c)
		}
	}
	if trial.Grade == nil || trial.Grade.Score != 87.5 {
		t.Errorf("grade = %+v, not graded: %s", trial.Grade, trial.NotGraded)
	}
	// The grader saw what the tools returned.
	if trial.Grade == nil || len(grader.asked) != 2 || len(grader.asked[0].Tools) != 1 || grader.asked[0].Tools[0].Name != "report" || trial.Grade.Tries != 2 {
		t.Errorf("the grader was asked %d times; the grade: %+v", len(grader.asked), trial.Grade)
	}
	if len(grader.asked) == 2 {
		if back := grader.asked[1].Messages[2].Parts[0].ToolResult; back == nil || !strings.Contains(back.Error, "issues") {
			t.Errorf("what went back to the grader: %+v", grader.asked[1].Messages)
		}
	}
	if msg := grader.asked[0].Messages[0].Parts[0].Text; !strings.Contains(msg, "P1364274-046") || !strings.Contains(msg, "signed out of every site") {
		t.Errorf("the grader's message:\n%s", msg)
	}
}

// benchmarksDB is a database with dev's benchmark tables, which a test
// database, with only the migrations every mode has, does not have.
func benchmarksDB(t *testing.T) *sql.DB {
	t.Helper()
	db := desktop.TestDB(t)
	schema, err := os.ReadFile(filepath.Join("..", "database", "dev", "011_benchmarks.sql"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(string(schema)); err != nil {
		t.Fatal(err)
	}
	return db
}

func runStatus(t *testing.T, db *sql.DB, run int64) (string, bool, string) {
	t.Helper()
	var status, trial string
	var ended sql.NullString
	if err := db.QueryRow(`SELECT r.status, r.ended_at, t.status FROM benchmark_runs r JOIN benchmark_trials t ON t.run_id = r.id WHERE r.id = ?`, run).Scan(&status, &ended, &trial); err != nil {
		t.Fatal(err)
	}
	return status, ended.Valid, trial
}

// A run another process is going with holds its folder's lock, and is left
// alone; once that process is gone, its lock is free, and the run is
// stopped and its folder deleted.
func TestBenchmarksStopOnlyRunsWhoseProcessIsGone(t *testing.T) {
	db := benchmarksDB(t)
	dir := t.TempDir()
	for _, run := range []int64{1, 2} {
		if _, err := db.Exec(`INSERT INTO benchmark_runs (id, grader) VALUES (?, 'g/m')`, run); err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec(`INSERT INTO benchmark_trials (run_id, scenario, model, n, status) VALUES (?, 's', 'p/m', 1, 'running')`, run); err != nil {
			t.Fatal(err)
		}
	}
	// Run 1 is going in another process; run 2's process was killed, and
	// left its folder.
	going := filepath.Join(dir, "1")
	if err := os.MkdirAll(going, 0o700); err != nil {
		t.Fatal(err)
	}
	held, err := lockRun(going)
	if err != nil || held == nil {
		t.Fatalf("lockRun = %v, %v", held, err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "2", "7"), 0o700); err != nil {
		t.Fatal(err)
	}

	b, err := newBenchmarks(db, dir, newProviders(db), openTrial)
	if err != nil {
		t.Fatal(err)
	}
	if status, ended, trial := runStatus(t, db, 1); status != "running" || ended || trial != "running" {
		t.Errorf("the run going: %s, ended %v, its trial %s", status, ended, trial)
	}
	if status, ended, trial := runStatus(t, db, 2); status != "stopped" || !ended || trial != "stopped" {
		t.Errorf("the dead run: %s, ended %v, its trial %s", status, ended, trial)
	}
	if _, err := os.Stat(filepath.Join(dir, "2")); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("the dead run's folder: %v", err)
	}

	// Cancelled from here, though it goes elsewhere, and then its process
	// dies before it ends the run.
	if err := b.Cancel(1); err != nil {
		t.Fatal(err)
	}
	if status, _, _ := runStatus(t, db, 1); status != "cancelled" {
		t.Errorf("the cancelled run: %s", status)
	}
	held.Close()
	if err := b.StopDeadRuns(); err != nil {
		t.Fatal(err)
	}
	if status, ended, trial := runStatus(t, db, 1); status != "cancelled" || !ended || trial != "stopped" {
		t.Errorf("the cancelled run, once its process died: %s, ended %v, its trial %s", status, ended, trial)
	}
	if _, err := os.Stat(going); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("its folder: %v", err)
	}
}
