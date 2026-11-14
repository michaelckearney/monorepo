package backend

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"sync"
	"time"

	"github.com/michaelckearney/monorepo/framework"
	"github.com/michaelckearney/monorepo/projects/harbor/backend/benchmark"
	"github.com/michaelckearney/monorepo/projects/harbor/backend/chat"
)

// Benchmarks runs the benchmarks, for the developer bar's Benchmarks page
// in a dev build and for Harbor's command-line program. A run is every
// chosen scenario on every chosen model, repeated, one trial at a time in
// the background; each trial has a new database and new browsers of its
// own, deleted when it ends. The page reads runs and trials with live
// queries.
//
// The app and the command-line program share the database, and either can
// start a run, follow one, or cancel one, whichever started it. One run
// goes at a time between them. A run going has a folder in dir, named after
// it, that holds its trials and a lock its process holds until the run
// ends: a run whose lock is free is one whose process quit or was killed
// during it, and StopDeadRuns ends it.
type Benchmarks struct {
	db        *sql.DB
	dir       string
	providers *Providers
	open      benchmark.Open

	mu sync.Mutex
	// going is the run going in this process, if any; cancel ends it, and
	// ended is closed once it has.
	going  int64
	cancel context.CancelFunc
	ended  chan struct{}
}

// NewBenchmarks is the benchmarks on db, the app's database, for Harbor's
// command-line program.
func NewBenchmarks(db *sql.DB) (*Benchmarks, error) {
	return newBenchmarks(db, benchmarksDir(), newProviders(db), openTrial)
}

// benchmarksDir is where the runs going have their folders, in the data
// folder the app and the command-line program share.
func benchmarksDir() string { return filepath.Join(desktop.DataDir(), "benchmarks") }

func newBenchmarks(db *sql.DB, dir string, providers *Providers, open benchmark.Open) (*Benchmarks, error) {
	b := &Benchmarks{db: db, dir: dir, providers: providers, open: open}
	return b, b.StopDeadRuns()
}

// Scenarios are the scenarios a run can choose from, by name.
func (b *Benchmarks) Scenarios() ([]benchmark.Scenario, error) {
	return benchmark.Scenarios(benchmark.Files)
}

// AgentModel is the model Harbor's agent uses, as provider/model: what a
// run benchmarks, and grades with, when it is not told.
func (b *Benchmarks) AgentModel() (string, error) {
	pr, settings, err := b.providers.current()
	if err != nil {
		return "", err
	}
	model := settings["model"]
	if model == "" {
		for _, s := range pr.Settings {
			if s.Key == "model" {
				model = s.Default
			}
		}
	}
	if model == "" {
		return "", fmt.Errorf("%s has no model to choose", pr.Title)
	}
	return pr.Name + "/" + model, nil
}

// Start starts a run: each scenario on each of models, repeat times,
// graded by grader. The models are the provider's set up in Providers. It
// returns the run's id. The run goes until it ends, it is cancelled, or
// this process quits.
func (b *Benchmarks) Start(scenarios, models []string, grader string, repeat int) (int64, error) {
	all, err := b.Scenarios()
	if err != nil {
		return 0, err
	}
	var chosen []benchmark.Scenario
	for _, name := range scenarios {
		i := slices.IndexFunc(all, func(s benchmark.Scenario) bool { return s.Name == name })
		if i < 0 {
			return 0, fmt.Errorf("there is no scenario named %s", name)
		}
		chosen = append(chosen, all[i])
	}
	switch {
	case len(chosen) == 0:
		return 0, errors.New("choose a scenario")
	case len(models) == 0:
		return 0, errors.New("add a model to benchmark")
	case repeat < 1:
		return 0, errors.New("run each scenario at least once")
	}
	graderModel, err := b.providers.modelAt(grader)
	if err != nil {
		return 0, err
	}
	agents := map[string]chat.Model{}
	for _, m := range models {
		if agents[m], err = b.providers.modelAt(m); err != nil {
			return 0, err
		}
	}
	// A run whose process died would otherwise be the one going.
	if err := b.StopDeadRuns(); err != nil {
		return 0, err
	}

	b.mu.Lock()
	defer b.mu.Unlock()
	// The transaction holds the database's write lock from its start, so
	// no other process starts a run between the check and the insert, or
	// sees the run's folder before its lock is taken.
	tx, err := b.db.Begin()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	var going int
	if err := tx.QueryRow(`SELECT COUNT(*) FROM benchmark_runs WHERE ended_at IS NULL`).Scan(&going); err != nil {
		return 0, err
	}
	if going > 0 {
		return 0, errors.New("a run is going; wait for it, or cancel it")
	}
	res, err := tx.Exec(`INSERT INTO benchmark_runs (grader) VALUES (?)`, grader)
	if err != nil {
		return 0, err
	}
	run, _ := res.LastInsertId()
	for _, s := range chosen {
		for _, m := range models {
			for n := 1; n <= repeat; n++ {
				if _, err := tx.Exec(`INSERT INTO benchmark_trials (run_id, scenario, model, n) VALUES (?, ?, ?, ?)`, run, s.Name, m, n); err != nil {
					return 0, err
				}
			}
		}
	}
	dir := b.runDir(run)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return 0, err
	}
	held, err := lockRun(dir)
	if err == nil && held == nil {
		err = fmt.Errorf("%s is locked by another process", dir)
	}
	if err == nil {
		err = tx.Commit()
	}
	if err != nil {
		if held != nil {
			held.Close()
		}
		os.RemoveAll(dir)
		return 0, err
	}
	ctx, cancel := context.WithCancel(context.Background())
	b.going, b.cancel, b.ended = run, cancel, make(chan struct{})
	go b.run(ctx, run, held, chosen, agents, graderModel)
	return run, nil
}

// Cancel ends the run, whichever process it is going in: its trial stops,
// and those waiting do not start.
func (b *Benchmarks) Cancel(run int64) error {
	// The run's process sees this, as the page does.
	if _, err := b.db.Exec(`UPDATE benchmark_runs SET status = 'cancelled' WHERE id = ? AND status = 'running' AND ended_at IS NULL`, run); err != nil {
		return err
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.going == run {
		b.cancel()
	}
	return nil
}

// ServiceShutdown cancels the run going in this process, if any, and
// returns once it has ended, when the app or the command-line program
// quits.
func (b *Benchmarks) ServiceShutdown() error {
	b.mu.Lock()
	ended := b.ended
	if b.cancel != nil {
		b.cancel()
	}
	b.mu.Unlock()
	if ended != nil {
		<-ended
	}
	return nil
}

// StopDeadRuns ends the runs whose process quit or was killed during them,
// and deletes their folders.
func (b *Benchmarks) StopDeadRuns() error {
	// The write lock, all through: Start makes a run's folder and takes its
	// lock inside a transaction, so no folder here is one whose lock is
	// about to be taken.
	tx, err := b.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	rows, err := tx.Query(`SELECT id FROM benchmark_runs WHERE ended_at IS NULL`)
	if err != nil {
		return err
	}
	var open []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		open = append(open, id)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}

	entries, err := os.ReadDir(b.dir)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	alive := map[int64]bool{}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		dir := filepath.Join(b.dir, e.Name())
		held, err := lockRun(dir)
		if errors.Is(err, fs.ErrNotExist) {
			// Its run ended, and deleted it, since the folder was listed.
			continue
		}
		if err != nil {
			return err
		}
		if held == nil {
			if id, err := strconv.ParseInt(e.Name(), 10, 64); err == nil {
				alive[id] = true
			}
			continue
		}
		// Nothing holds it, so nothing is using the folder: a dead run's, or
		// what is left of one that ended.
		err = os.RemoveAll(dir)
		held.Close()
		if err != nil {
			return err
		}
	}
	for _, id := range open {
		if alive[id] {
			continue
		}
		if _, err := tx.Exec(`UPDATE benchmark_trials SET status = 'stopped' WHERE run_id = ? AND status IN ('waiting', 'running')`, id); err != nil {
			return err
		}
		if _, err := tx.Exec(`UPDATE benchmark_runs SET status = CASE status WHEN 'running' THEN 'stopped' ELSE status END,
			ended_at = strftime('%Y-%m-%d %H:%M:%f', 'now') WHERE id = ?`, id); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (b *Benchmarks) runDir(run int64) string {
	return filepath.Join(b.dir, strconv.FormatInt(run, 10))
}

// run runs the run's trials, in order, holding the run's lock until it
// has ended.
func (b *Benchmarks) run(ctx context.Context, run int64, held *os.File, scenarios []benchmark.Scenario, agents map[string]chat.Model, grader chat.Model) {
	b.mu.Lock()
	ended := b.ended
	b.mu.Unlock()
	defer func() {
		os.RemoveAll(b.runDir(run))
		held.Close()
		b.mu.Lock()
		b.cancel()
		b.going, b.cancel, b.ended = 0, nil, nil
		b.mu.Unlock()
		close(ended)
	}()
	go b.watchCancel(ctx, run)

	type trial struct {
		id       int64
		scenario string
		model    string
		n        int
	}
	var trials []trial
	rows, err := b.db.Query(`SELECT id, scenario, model, n FROM benchmark_trials WHERE run_id = ? ORDER BY id`, run)
	if err == nil {
		for rows.Next() {
			var t trial
			if err = rows.Scan(&t.id, &t.scenario, &t.model, &t.n); err != nil {
				break
			}
			trials = append(trials, t)
		}
		rows.Close()
	}
	for _, t := range trials {
		if err != nil || ctx.Err() != nil {
			break
		}
		if _, err = b.db.Exec(`UPDATE benchmark_trials SET status = 'running' WHERE id = ?`, t.id); err != nil {
			break
		}
		i := slices.IndexFunc(scenarios, func(s benchmark.Scenario) bool { return s.Name == t.scenario })
		result, failure := b.trial(ctx, run, t.id, scenarios[i], t.n, t.model, agents[t.model], grader)
		switch {
		case ctx.Err() != nil:
			_, err = b.db.Exec(`UPDATE benchmark_trials SET status = 'cancelled' WHERE id = ?`, t.id)
		case failure != nil:
			_, err = b.db.Exec(`UPDATE benchmark_trials SET status = 'failed', error = ? WHERE id = ?`, failure.Error(), t.id)
		default:
			err = b.save(t.id, result)
		}
	}
	status := "done"
	if ctx.Err() != nil {
		status = "cancelled"
	}
	b.db.Exec(`UPDATE benchmark_trials SET status = 'cancelled' WHERE run_id = ? AND status IN ('waiting', 'running')`, run)
	b.db.Exec(`UPDATE benchmark_runs SET status = ?, ended_at = strftime('%Y-%m-%d %H:%M:%f', 'now') WHERE id = ?`, status, run)
}

// cancelPoll is how often a run looks for a Cancel from another process.
const cancelPoll = 500 * time.Millisecond

// watchCancel cancels the run when another process cancels it, until it
// ends.
func (b *Benchmarks) watchCancel(ctx context.Context, run int64) {
	tick := time.NewTicker(cancelPoll)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
			var status string
			if err := b.db.QueryRow(`SELECT status FROM benchmark_runs WHERE id = ?`, run).Scan(&status); err == nil && status == "cancelled" {
				b.mu.Lock()
				if b.going == run {
					b.cancel()
				}
				b.mu.Unlock()
				return
			}
		}
	}
}

// trial runs one trial, on a new database, in a new folder of its own in
// its run's.
func (b *Benchmarks) trial(ctx context.Context, run, id int64, s benchmark.Scenario, n int, model string, agent, grader chat.Model) (*benchmark.Trial, error) {
	dir := filepath.Join(b.runDir(run), strconv.FormatInt(id, 10))
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	defer os.RemoveAll(dir)
	db, err := desktop.NewDB(filepath.Join(dir, "app.db"))
	if err != nil {
		return nil, err
	}
	defer db.Close()
	return benchmark.Run(ctx, s, n, benchmark.Config{
		Open: b.open, Dir: dir, DB: db, Files: benchmark.Files,
		Agent: agent, ModelName: model, Grader: grader, TurnTimeout: benchmark.TurnTimeout,
		Progress: func(transcript []benchmark.Message, u benchmark.Usage) error {
			raw, err := json.Marshal(transcript)
			if err != nil {
				return err
			}
			_, err = b.db.Exec(`UPDATE benchmark_trials SET started_at = COALESCE(started_at, strftime('%Y-%m-%d %H:%M:%f', 'now')),
				transcript = ?, input_tokens = ?, output_tokens = ? WHERE id = ?`, string(raw), u.InputTokens, u.OutputTokens, id)
			return err
		},
	})
}

// save stores a trial that ran. Its transcript is in result now.
func (b *Benchmarks) save(id int64, t *benchmark.Trial) error {
	result, err := json.Marshal(t)
	if err != nil {
		return err
	}
	var score *float64
	if t.Grade != nil {
		score = &t.Grade.Score
	}
	_, err = b.db.Exec(`UPDATE benchmark_trials SET status = 'done', score = ?, seconds = ?, input_tokens = ?, output_tokens = ?, error = ?, result = ?, transcript = NULL WHERE id = ?`,
		score, t.Seconds, t.Usage.InputTokens, t.Usage.OutputTokens, t.Error, string(result), id)
	return err
}
