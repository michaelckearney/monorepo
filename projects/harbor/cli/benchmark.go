package main

import (
	"bufio"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/michaelckearney/monorepo/framework"
	"github.com/michaelckearney/monorepo/projects/harbor/backend"
	"github.com/michaelckearney/monorepo/projects/harbor/backend/benchmark"
	"github.com/michaelckearney/monorepo/projects/harbor/backend/fixture"
)

func benchmarkCommand(args []string, out io.Writer) error {
	if len(args) == 0 {
		return usageError{"benchmark needs a command"}
	}
	cmd, args := args[0], args[1:]
	fs := flag.NewFlagSet("benchmark "+cmd, flag.ContinueOnError)
	asJSON := fs.Bool("json", false, "")
	switch cmd {
	case "scenarios":
		if _, err := noArgs(fs, args); err != nil {
			return err
		}
		return scenarios(out, *asJSON)
	case "list":
		running := fs.Bool("running", false, "")
		limit := fs.Int("limit", 20, "")
		if _, err := noArgs(fs, args); err != nil {
			return err
		}
		return listRuns(out, *running, *limit, *asJSON)
	case "start", "run":
		var o startOptions
		fs.Var(&o.scenarios, "scenario", "")
		fs.Var(&o.models, "model", "")
		fs.StringVar(&o.grader, "grader", "", "")
		fs.IntVar(&o.repeat, "repeat", 1, "")
		// The process start leaves the run going in.
		detached := fs.Bool("detached", false, "")
		if _, err := noArgs(fs, args); err != nil {
			return err
		}
		switch {
		case cmd == "start":
			return startDetached(out, args)
		case *detached:
			return runDetached(o)
		default:
			return runHere(out, o)
		}
	case "wait", "show", "cancel", "trial":
		evidence := fs.Bool("evidence", false, "")
		full := fs.Bool("full", false, "")
		rest, err := parse(fs, args)
		if err != nil {
			return err
		}
		if len(rest) != 1 {
			return usageError{fmt.Sprintf("benchmark %s takes one id", cmd)}
		}
		id, err := strconv.ParseInt(rest[0], 10, 64)
		if err != nil {
			return usageError{fmt.Sprintf("benchmark %s: %q is not an id", cmd, rest[0])}
		}
		switch cmd {
		case "wait":
			return wait(out, id)
		case "show":
			return show(out, id, *asJSON)
		case "cancel":
			return cancel(out, id)
		default:
			return showTrial(out, id, *asJSON, *evidence, *full)
		}
	default:
		return usageError{fmt.Sprintf("unknown benchmark command %q", cmd)}
	}
}

func noArgs(fs *flag.FlagSet, args []string) ([]string, error) {
	rest, err := parse(fs, args)
	if err == nil && len(rest) > 0 {
		err = usageError{fmt.Sprintf("%s takes no arguments, only flags; %q is not one", fs.Name(), rest[0])}
	}
	return rest, err
}

// open is the benchmarks, on the app's database. Opening them stops the
// runs whose process is gone, so what follows does not show them going.
func open() (*backend.Benchmarks, *sql.DB, error) {
	if desktop.Mode() != "dev" {
		return nil, nil, errors.New("the benchmarks are in dev builds only; run: harness cli -mode=dev benchmark …")
	}
	db := desktop.DB()
	b, err := backend.NewBenchmarks(db)
	return b, db, err
}

func scenarios(out io.Writer, asJSON bool) error {
	b, _, err := open()
	if err != nil {
		return err
	}
	all, err := b.Scenarios()
	if err != nil {
		return err
	}
	if asJSON {
		return writeJSON(out, all)
	}
	writeScenarios(out, all)
	return nil
}

func listRuns(out io.Writer, running bool, limit int, asJSON bool) error {
	_, db, err := open()
	if err != nil {
		return err
	}
	runs, err := queryRuns(db, running, limit)
	if err != nil {
		return err
	}
	if asJSON {
		return writeJSON(out, runs)
	}
	writeRuns(out, runs)
	return nil
}

type startOptions struct {
	scenarios, models list
	grader            string
	repeat            int
}

// begin starts the run in this process, filling in what o leaves out.
func begin(b *backend.Benchmarks, o startOptions) (int64, error) {
	if len(o.scenarios) == 0 {
		all, err := b.Scenarios()
		if err != nil {
			return 0, err
		}
		for _, s := range all {
			o.scenarios = append(o.scenarios, s.Name)
		}
	}
	if len(o.models) == 0 || o.grader == "" {
		model, err := b.AgentModel()
		if err != nil {
			return 0, fmt.Errorf("%v; or choose with -model and -grader", err)
		}
		if len(o.models) == 0 {
			o.models = list{model}
		}
		if o.grader == "" {
			o.grader = model
		}
	}
	return b.Start(o.scenarios, o.models, o.grader, o.repeat)
}

// runHere starts the run in this process and follows it until it ends.
// Ctrl-C, or a signal to stop, cancels it.
func runHere(out io.Writer, o startOptions) error {
	b, db, err := open()
	if err != nil {
		return err
	}
	sites, err := backend.StartFixtures()
	if err != nil {
		return err
	}
	defer sites.Stop()
	id, err := begin(b, o)
	if err != nil {
		return err
	}
	fmt.Fprintf(out, "Run %d started.\n", id)
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go func() {
		<-ctx.Done()
		b.Cancel(id)
	}()
	err = follow(context.Background(), out, b, db, id)
	// The run has ended; this is its folder and lock let go.
	b.ServiceShutdown()
	if err != nil {
		return err
	}
	return show(out, id, false)
}

// startDetached starts the run in a new process of its own, which keeps
// going when this one, and the shell it ran in, have ended, and says which
// run it is.
func startDetached(out io.Writer, args []string) error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	cmd := exec.Command(exe, append([]string{"benchmark", "run", "-detached"}, args...)...)
	// A session of its own, so Ctrl-C in this terminal, or a harness ending
	// this command's process group, does not reach it.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	pipe, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	line, _ := bufio.NewReader(pipe).ReadString('\n')
	line = strings.TrimSpace(line)
	if id, ok := strings.CutPrefix(line, "started "); ok {
		cmd.Process.Release()
		fmt.Fprintf(out, "Run %s started. Follow it with: harness cli -mode=dev benchmark wait %s\n", id, id)
		return nil
	}
	cmd.Wait()
	if msg, ok := strings.CutPrefix(line, "failed "); ok {
		return errors.New(msg)
	}
	return fmt.Errorf("the run's process ended before it started the run")
}

// runDetached is the process start leaves the run going in. It tells start
// which run it is, or why there is none, on one line, and then writes
// nothing: start has stopped reading, and a write to its pipe would kill
// this process. Its stderr is nowhere already.
func runDetached(o startOptions) error {
	b, db, err := open()
	var id int64
	if err == nil {
		var sites *fixture.Fixtures
		if sites, err = backend.StartFixtures(); err == nil {
			defer sites.Stop()
			id, err = begin(b, o)
		}
	}
	if err != nil {
		fmt.Printf("failed %s\n", strings.ReplaceAll(err.Error(), "\n", " "))
		return nil
	}
	fmt.Printf("started %d\n", id)
	// Its stdout is start's pipe. Left as it is, nothing writes to it.
	devNull, err := os.OpenFile(os.DevNull, os.O_WRONLY, 0)
	if err != nil {
		b.Cancel(id)
		b.ServiceShutdown()
		return nil
	}
	os.Stdout = devNull
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM, syscall.SIGHUP)
	defer stop()
	ended := make(chan struct{})
	go func() {
		defer close(ended)
		for {
			var done bool
			if err := db.QueryRow(`SELECT ended_at IS NOT NULL FROM benchmark_runs WHERE id = ?`, id).Scan(&done); err != nil || done {
				return
			}
			time.Sleep(time.Second)
		}
	}()
	select {
	case <-ended:
	case <-ctx.Done():
		b.Cancel(id)
	}
	b.ServiceShutdown()
	return nil
}

// follow writes each trial of run as it changes, until the run ends, and
// stops if ctx ends first.
func follow(ctx context.Context, out io.Writer, b *backend.Benchmarks, db *sql.DB, run int64) error {
	seen := map[int64]string{}
	for i := 0; ; i++ {
		trials, err := queryTrials(db, run)
		if err != nil {
			return err
		}
		for _, t := range trials {
			if label := trialState(t); seen[t.ID] != label {
				seen[t.ID] = label
				fmt.Fprintln(out, progressLine(t))
			}
		}
		var ended bool
		if err := db.QueryRow(`SELECT ended_at IS NOT NULL FROM benchmark_runs WHERE id = ?`, run).Scan(&ended); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return fmt.Errorf("there is no run %d", run)
			}
			return err
		}
		if ended {
			return nil
		}
		// Another process's run ends only when it does; this one notices if
		// that process is gone.
		if i%10 == 9 {
			if err := b.StopDeadRuns(); err != nil {
				return err
			}
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(500 * time.Millisecond):
		}
	}
}

func wait(out io.Writer, run int64) error {
	b, db, err := open()
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := follow(ctx, out, b, db, run); err != nil {
		if ctx.Err() != nil {
			return fmt.Errorf("stopped waiting; run %d goes on", run)
		}
		return err
	}
	return show(out, run, false)
}

func show(out io.Writer, id int64, asJSON bool) error {
	_, db, err := open()
	if err != nil {
		return err
	}
	runs, err := queryRun(db, id)
	if err != nil {
		return err
	}
	trials, err := queryTrials(db, id)
	if err != nil {
		return err
	}
	if asJSON {
		return writeJSON(out, struct {
			Run    runRow     `json:"run"`
			Trials []trialRow `json:"trials"`
		}{runs, trials})
	}
	writeRun(out, runs, trials)
	return nil
}

func cancel(out io.Writer, id int64) error {
	b, db, err := open()
	if err != nil {
		return err
	}
	r, err := queryRun(db, id)
	if err != nil {
		return err
	}
	if r.Status != "running" {
		return fmt.Errorf("run %d is not going; it is %s", id, r.Status)
	}
	if err := b.Cancel(id); err != nil {
		return err
	}
	fmt.Fprintf(out, "Run %d is cancelled; its trial going stops, and the rest do not start.\n", id)
	return nil
}

func showTrial(out io.Writer, id int64, asJSON, evidence, full bool) error {
	_, db, err := open()
	if err != nil {
		return err
	}
	t, result, transcript, err := queryTrial(db, id)
	if err != nil {
		return err
	}
	if asJSON {
		return writeJSON(out, struct {
			trialRow
			Result     json.RawMessage `json:"result"`
			Transcript json.RawMessage `json:"transcript"`
		}{t, json.RawMessage(orNull(result)), json.RawMessage(orNull(transcript))})
	}
	var r *benchmark.Trial
	if result != "" {
		r = &benchmark.Trial{}
		if err := json.Unmarshal([]byte(result), r); err != nil {
			return err
		}
	}
	var partial []benchmark.Message
	if transcript != "" {
		if err := json.Unmarshal([]byte(transcript), &partial); err != nil {
			return err
		}
	}
	writeTrial(out, t, r, partial, evidence, full)
	return nil
}

func orNull(s string) string {
	if s == "" {
		return "null"
	}
	return s
}

func writeJSON(out io.Writer, v any) error {
	enc := json.NewEncoder(out)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}
