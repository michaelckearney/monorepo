package main

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/michaelckearney/monorepo/projects/harbor/backend/benchmark"
	"github.com/michaelckearney/monorepo/projects/harbor/backend/provider"
)

// runRow is a run, with its trials counted.
type runRow struct {
	ID        int64   `json:"id"`
	Grader    string  `json:"grader"`
	Status    string  `json:"status"`
	StartedAt string  `json:"startedAt"`
	EndedAt   *string `json:"endedAt"`
	Trials    int     `json:"trials"`
	// Finished counts the trials that are no longer waiting or running.
	Finished int `json:"finished"`
	// Mean is the mean score of the trials graded, and nil with none.
	Mean *float64 `json:"mean"`
	// Now is the trial going, as "scenario on model, trial n".
	Now string `json:"now,omitempty"`
}

// trialRow is a trial, without its result.
type trialRow struct {
	ID           int64    `json:"id"`
	Run          int64    `json:"run"`
	Scenario     string   `json:"scenario"`
	Model        string   `json:"model"`
	N            int      `json:"n"`
	Status       string   `json:"status"`
	Score        *float64 `json:"score"`
	Seconds      *float64 `json:"seconds"`
	InputTokens  *int64   `json:"inputTokens"`
	OutputTokens *int64   `json:"outputTokens"`
	Error        string   `json:"error,omitempty"`
}

const runColumns = `r.id, r.grader, r.status, r.started_at, r.ended_at,
	(SELECT COUNT(*) FROM benchmark_trials t WHERE t.run_id = r.id),
	(SELECT COUNT(*) FROM benchmark_trials t WHERE t.run_id = r.id AND t.status NOT IN ('waiting', 'running')),
	(SELECT AVG(score) FROM benchmark_trials t WHERE t.run_id = r.id AND t.score IS NOT NULL),
	COALESCE((SELECT t.scenario || ' on ' || t.model || ', trial ' || t.n FROM benchmark_trials t
		WHERE t.run_id = r.id AND t.status = 'running' AND r.ended_at IS NULL), '')`

func scanRun(row interface{ Scan(...any) error }) (runRow, error) {
	var r runRow
	err := row.Scan(&r.ID, &r.Grader, &r.Status, &r.StartedAt, &r.EndedAt, &r.Trials, &r.Finished, &r.Mean, &r.Now)
	return r, err
}

func queryRuns(db *sql.DB, running bool, limit int) ([]runRow, error) {
	where := ""
	if running {
		where = "WHERE r.ended_at IS NULL"
	}
	rows, err := db.Query(`SELECT `+runColumns+` FROM benchmark_runs r `+where+` ORDER BY r.id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	runs := []runRow{}
	for rows.Next() {
		r, err := scanRun(rows)
		if err != nil {
			return nil, err
		}
		runs = append(runs, r)
	}
	return runs, rows.Err()
}

func queryRun(db *sql.DB, id int64) (runRow, error) {
	r, err := scanRun(db.QueryRow(`SELECT `+runColumns+` FROM benchmark_runs r WHERE r.id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return r, fmt.Errorf("there is no run %d", id)
	}
	return r, err
}

const trialColumns = `id, run_id, scenario, model, n, status, score, seconds, input_tokens, output_tokens, error`

func scanTrial(row interface{ Scan(...any) error }, more ...any) (trialRow, error) {
	var t trialRow
	err := row.Scan(append([]any{&t.ID, &t.Run, &t.Scenario, &t.Model, &t.N, &t.Status, &t.Score, &t.Seconds, &t.InputTokens, &t.OutputTokens, &t.Error}, more...)...)
	return t, err
}

func queryTrials(db *sql.DB, run int64) ([]trialRow, error) {
	rows, err := db.Query(`SELECT `+trialColumns+` FROM benchmark_trials WHERE run_id = ? ORDER BY id`, run)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	trials := []trialRow{}
	for rows.Next() {
		t, err := scanTrial(rows)
		if err != nil {
			return nil, err
		}
		trials = append(trials, t)
	}
	return trials, rows.Err()
}

// queryTrial is a trial, and its result as JSON: "" until it is done, and
// for one that failed.
// queryTrial is a trial, its result, once it is done, and its chat so far,
// while it goes or where it got to if it was cut short.
func queryTrial(db *sql.DB, id int64) (trialRow, string, string, error) {
	var result, transcript sql.NullString
	t, err := scanTrial(db.QueryRow(`SELECT `+trialColumns+`, result, transcript FROM benchmark_trials WHERE id = ?`, id), &result, &transcript)
	if errors.Is(err, sql.ErrNoRows) {
		return t, "", "", fmt.Errorf("there is no trial %d", id)
	}
	return t, result.String, transcript.String, err
}

// trialState is a trial's status, as the Benchmarks page says it.
func trialState(t trialRow) string {
	switch t.Status {
	case "done":
		if t.Score == nil {
			return "not graded"
		}
		return "graded"
	case "running":
		return "running"
	default:
		return t.Status
	}
}

func (t trialRow) name() string {
	return fmt.Sprintf("%s on %s, trial %d", t.Scenario, t.Model, t.N)
}

// progressLine is a trial as it changes, while a run is followed.
func progressLine(t trialRow) string {
	line := fmt.Sprintf("trial %d, %s: %s", t.ID, t.name(), trialState(t))
	if t.Score != nil {
		line += ", " + number(t.Score)
	}
	if t.Seconds != nil {
		line += fmt.Sprintf(" in %ss", number(t.Seconds))
	}
	if t.Error != "" {
		line += ": " + oneLine(t.Error, 200)
	}
	return line
}

func writeScenarios(out io.Writer, all []benchmark.Scenario) {
	w := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "SCENARIO\tCRITERIA\tPROMPT")
	for _, s := range all {
		fmt.Fprintf(w, "%s\t%d\t%s\n", s.Name, len(s.Criteria), oneLine(s.Prompt, 80))
	}
	w.Flush()
}

func writeRuns(out io.Writer, runs []runRow) {
	if len(runs) == 0 {
		fmt.Fprintln(out, "No runs.")
		return
	}
	w := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "RUN\tSTATUS\tSTARTED\tGRADER\tFINISHED\tMEAN\tNOW")
	for _, r := range runs {
		fmt.Fprintf(w, "%d\t%s\t%s\t%s\t%d of %d\t%s\t%s\n", r.ID, r.Status, local(r.StartedAt), r.Grader, r.Finished, r.Trials, number(r.Mean), r.Now)
	}
	w.Flush()
}

// writeRun is a run: its trials, and the means of each scenario on each
// model run more than once.
func writeRun(out io.Writer, r runRow, trials []trialRow) {
	fmt.Fprintf(out, "Run %d: %s, graded by %s, started %s; %d of %d trials finished", r.ID, r.Status, r.Grader, local(r.StartedAt), r.Finished, r.Trials)
	if r.Mean != nil {
		fmt.Fprintf(out, ", mean score %s", number(r.Mean))
	}
	fmt.Fprint(out, ".\nScores are from 0 to 100. Tokens and seconds are the agent's. See a trial with: harness cli -mode=dev benchmark trial <trial>\n\n")
	w := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "TRIAL\tSCENARIO\tMODEL\tN\tSTATUS\tSCORE\tTOKENS IN\tTOKENS OUT\tSECONDS")
	for _, t := range trials {
		fmt.Fprintf(w, "%d\t%s\t%s\t%d\t%s\t%s\t%s\t%s\t%s\n", t.ID, t.Scenario, t.Model, t.N, trialState(t),
			number(t.Score), count(t.InputTokens), count(t.OutputTokens), number(t.Seconds))
	}
	w.Flush()
	for _, t := range trials {
		if t.Error != "" {
			fmt.Fprintf(out, "\nTrial %d: %s\n", t.ID, oneLine(t.Error, 300))
		}
	}

	means := summarise(trials)
	if len(means) == 0 {
		return
	}
	fmt.Fprint(out, "\nOver the trials graded:\n\n")
	w = tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "SCENARIO\tMODEL\tGRADED\tMEAN\tLOWEST")
	for _, m := range means {
		fmt.Fprintf(w, "%s\t%s\t%d of %d\t%s\t%s\n", m.scenario, m.model, m.graded, m.trials, number(m.mean), number(m.lowest))
	}
	w.Flush()
}

type mean struct {
	scenario, model string
	trials, graded  int
	mean, lowest    *float64
}

// summarise is each scenario on each model that ran more than once, in the
// order they first ran.
func summarise(trials []trialRow) []mean {
	var out []mean
	at := map[string]int{}
	for _, t := range trials {
		key := t.Scenario + "\x00" + t.Model
		i, ok := at[key]
		if !ok {
			i = len(out)
			at[key] = i
			out = append(out, mean{scenario: t.Scenario, model: t.Model})
		}
		m := &out[i]
		m.trials++
		if t.Score != nil {
			score := *t.Score
			if m.graded == 0 {
				m.mean, m.lowest = new(float64), new(float64)
				*m.lowest = score
			}
			*m.mean = (*m.mean*float64(m.graded) + score) / float64(m.graded+1)
			*m.lowest = min(*m.lowest, score)
			m.graded++
		}
	}
	repeated := out[:0]
	for _, m := range out {
		if m.trials > 1 {
			repeated = append(repeated, m)
		}
	}
	return repeated
}

// resultLimit is how much of a tool's result a trial shows without -full.
const resultLimit = 2000

// writeTrial is a trial as Markdown: what it was, its grade, its
// transcript, and with evidence, what the grader judged.
func writeTrial(out io.Writer, t trialRow, r *benchmark.Trial, partial []benchmark.Message, evidence, full bool) {
	fmt.Fprintf(out, "# Trial %d: %s\n\n", t.ID, t.name())
	facts := []string{fmt.Sprintf("Run %d", t.Run), trialState(t)}
	if t.Score != nil {
		facts = append(facts, "score "+number(t.Score))
	}
	if t.InputTokens != nil {
		facts = append(facts, fmt.Sprintf("%s tokens in, %s out", count(t.InputTokens), count(t.OutputTokens)))
	}
	if t.Seconds != nil {
		facts = append(facts, number(t.Seconds)+" seconds")
	}
	fmt.Fprintf(out, "%s.\n", strings.Join(facts, ", "))
	if r == nil {
		switch {
		case t.Error != "":
			fmt.Fprintf(out, "\nThe trial failed: %s\n", t.Error)
		case t.Status == "waiting" || t.Status == "running":
			fmt.Fprintln(out, "\nIt has no result yet.")
		default:
			fmt.Fprintln(out, "\nIt has no result.")
		}
		if len(partial) > 0 {
			fmt.Fprint(out, "\n## Transcript so far\n")
			writeTranscript(out, partial, full)
		}
		return
	}
	if r.Error != "" {
		fmt.Fprintf(out, "\nThe turn ended with an error: %s\n", r.Error)
	}

	fmt.Fprint(out, "\n## Grade\n\n")
	switch {
	case r.Grade != nil:
		fmt.Fprintf(out, "%s\n", r.Grade.Summary)
		for _, c := range r.Grade.Criteria {
			fmt.Fprintf(out, "\n### %s: %s (weight %s)\n\n%s\n", c.Title, number(&c.Score), number(&c.Weight), c.Rationale)
			if len(c.Issues) > 0 {
				fmt.Fprint(out, "\nIssues:\n\n")
				for _, issue := range c.Issues {
					fmt.Fprintf(out, "- %s\n", issue)
				}
			}
			if len(c.Evidence) > 0 {
				fmt.Fprint(out, "\nEvidence:\n\n")
				for _, e := range c.Evidence {
					fmt.Fprintf(out, "- %s\n", e)
				}
			}
		}
	case r.NotGraded != "":
		fmt.Fprintf(out, "Not graded: %s\n", r.NotGraded)
	}

	fmt.Fprint(out, "\n## Transcript\n")
	writeTranscript(out, r.Transcript, full)

	if evidence {
		writeEvidence(out, r.Evidence)
	}
}

func writeTranscript(out io.Writer, messages []benchmark.Message, full bool) {
	results := map[string]*provider.ToolResult{}
	for _, m := range messages {
		for _, p := range m.Parts {
			if p.ToolResult != nil {
				results[p.ToolResult.ID] = p.ToolResult
			}
		}
	}
	for _, m := range messages {
		for _, p := range m.Parts {
			switch {
			case p.Text != "" && m.Role == "user":
				fmt.Fprintf(out, "\n**You:** %s\n", p.Text)
			case p.Text != "":
				fmt.Fprintf(out, "\n**Agent:** %s\n", p.Text)
			case p.ToolCall != nil:
				res := results[p.ToolCall.ID]
				state := "no result"
				switch {
				case res == nil:
				case res.Error != "":
					state = "failed"
				default:
					state = "done"
				}
				fmt.Fprintf(out, "\n**Tool `%s`**, %s:\n\n", p.ToolCall.Name, state)
				fence(out, "json", indent(p.ToolCall.Input), full)
				if res == nil {
					continue
				}
				if res.Error != "" {
					fmt.Fprintf(out, "\nError: %s\n", res.Error)
					continue
				}
				fmt.Fprint(out, "\nResult")
				if len(res.Images) > 0 {
					fmt.Fprintf(out, ", with %d image(s) not shown", len(res.Images))
				}
				fmt.Fprint(out, ":\n\n")
				fence(out, "json", indent(res.Output), full)
			}
		}
	}
}

func writeEvidence(out io.Writer, e benchmark.Evidence) {
	fmt.Fprint(out, "\n## Evidence\n")
	if len(e.Services) == 0 && len(e.Dashboards) == 0 {
		fmt.Fprint(out, "\nThe agent saved nothing.\n")
	}
	for _, s := range e.Services {
		fmt.Fprintf(out, "\n### Service %s\n", s.Name)
		if s.Problem != "" {
			fmt.Fprintf(out, "\nHarbor cannot read it: %s\n", s.Problem)
		}
		for _, phase := range []struct {
			title string
			calls []benchmark.Call
		}{{"signed in", s.SignedIn}, {"signed out", s.SignedOut}} {
			for _, c := range phase.calls {
				fmt.Fprintf(out, "\n`%s`, %s:\n\n", c.Tool, phase.title)
				if c.Error != "" {
					fmt.Fprintf(out, "Error: %s\n", c.Error)
					continue
				}
				fence(out, "json", indent(c.Output), false)
			}
		}
		fmt.Fprintln(out, "\nSource:")
		fmt.Fprintln(out)
		fence(out, "ts", s.Source, true)
	}
	for _, d := range e.Dashboards {
		fmt.Fprintf(out, "\n### Dashboard %s\n\n", d.Name)
		fence(out, "vue", d.Source, true)
	}
	if len(e.Prompts) > 0 {
		fmt.Fprint(out, "\n### Tool calls Harbor would have asked about (the run allowed each)\n\n")
		fence(out, "json", indent(e.Prompts), true)
	}
	if len(e.Asked) > 0 {
		fmt.Fprint(out, "\n### What Harbor asked beside a live view, which the run does not answer\n\n")
		for _, a := range e.Asked {
			fmt.Fprintf(out, "- %s\n", a)
		}
	}
	for _, si := range e.SignIns {
		fmt.Fprintf(out, "\nSign-in to %s, in the %s's browser: %s\n", si.Service, si.Browser, si.Outcome)
		for i, step := range si.Steps {
			fmt.Fprintf(out, "%d. %s\n", i+1, step)
		}
	}
}

// fence writes text as a fenced code block, cut to resultLimit unless
// full.
func fence(out io.Writer, lang, text string, full bool) {
	if !full && len(text) > resultLimit {
		text = text[:resultLimit] + fmt.Sprintf("\n… %d more characters; -full shows them", len(text)-resultLimit)
	}
	ticks := "```"
	for strings.Contains(text, ticks) {
		ticks += "`"
	}
	fmt.Fprintf(out, "%s%s\n%s\n%s\n", ticks, lang, strings.TrimRight(text, "\n"), ticks)
}

func indent(v any) string {
	if raw, ok := v.(json.RawMessage); ok {
		var parsed any
		if json.Unmarshal(raw, &parsed) == nil {
			v = parsed
		}
	}
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return fmt.Sprint(v)
	}
	return string(b)
}

// number is a score, mean, or count of seconds, with at most one decimal,
// or – for none.
func number(v *float64) string {
	if v == nil {
		return "–"
	}
	return strconv.FormatFloat(float64(int64(*v*10+0.5))/10, 'f', -1, 64)
}

func count(v *int64) string {
	if v == nil {
		return "–"
	}
	return strconv.FormatInt(*v, 10)
}

// local is a time the database keeps, in UTC, in this machine's time zone.
func local(utc string) string {
	t, err := time.ParseInLocation("2006-01-02 15:04:05.000", utc, time.UTC)
	if err != nil {
		return utc
	}
	return t.Local().Format("2006-01-02 15:04")
}

func oneLine(s string, limit int) string {
	s = strings.Join(strings.Fields(s), " ")
	if r := []rune(s); len(r) > limit {
		return string(r[:limit-1]) + "…"
	}
	return s
}
