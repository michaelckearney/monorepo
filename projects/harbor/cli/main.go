// Command cli is Harbor's command-line program, for developing Harbor:
// for now, its benchmarks. Slipway builds and runs it, on the data folder
// `harness run` uses, so it shares the running app's database:
//
//	harness cli -mode=dev benchmark list
//
// It writes what people and agents read to stdout, as plain text, or as
// JSON with -json, and its errors to stderr.
package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
)

const usage = `usage: harness cli -mode=dev benchmark <command>

commands:
  scenarios           the scenarios a run can choose from
  list                the runs, newest first, and the trial each one going is on
                      -running lists only the run going; -limit=N, 20 by default
  start               start a run in the background, print its id, and return
                      -scenario=a,b  the scenarios; all of them by default
                      -model=p/m,…   the models to benchmark, each provider/model,
                                     on the provider as it is set up in the app;
                                     the agent's model by default
                      -grader=p/m    the model that grades; the agent's by default
                      -repeat=N      how many times each scenario runs on each model
  run                 as start, then follow the run until it ends; Ctrl-C cancels it
  wait <run>          follow a run until it ends, then show it; Ctrl-C stops waiting
  show <run>          a run's trials, and the means of a scenario run more than once
  trial <trial>       a trial's grade and transcript, as Markdown; a trial going
                      shows its transcript so far
                      -evidence adds what the grader judged; -full keeps every
                      tool result whole
  cancel <run>        cancel a run, whichever process it is going in

show, list, scenarios, and trial take -json, for the data as JSON.

A run goes in the process that started it: start's runs in a process of
their own, run's in run's. One run goes at a time, in the app or here. A run's
process serves the fixture sites itself.
`

// usageError is a mistake in the command line: the usage follows it.
type usageError struct{ msg string }

func (e usageError) Error() string { return e.msg }

func main() {
	code := 0
	if err := run(os.Args[1:], os.Stdout); err != nil {
		var ue usageError
		if errors.As(err, &ue) {
			fmt.Fprintf(os.Stderr, "%s\n\n%s", ue.msg, usage)
			code = 2
		} else {
			fmt.Fprintf(os.Stderr, "harbor: %v\n", err)
			code = 1
		}
	}
	os.Exit(code)
}

func run(args []string, out io.Writer) error {
	if len(args) == 0 || args[0] == "-h" || args[0] == "--help" || args[0] == "help" {
		fmt.Fprint(out, usage)
		return nil
	}
	if args[0] != "benchmark" {
		return usageError{fmt.Sprintf("unknown command %q; the commands are benchmark's", args[0])}
	}
	return benchmarkCommand(args[1:], out)
}

// parse parses fs's flags wherever they are among args, as an agent tends
// to put them after the arguments, and returns the arguments.
func parse(fs *flag.FlagSet, args []string) ([]string, error) {
	fs.SetOutput(io.Discard)
	var rest []string
	for {
		if err := fs.Parse(args); err != nil {
			return nil, usageError{fmt.Sprintf("%s: %v", fs.Name(), err)}
		}
		args = fs.Args()
		if len(args) == 0 {
			return rest, nil
		}
		rest = append(rest, args[0])
		args = args[1:]
	}
}

// list is a flag of values, given comma-separated or more than once.
type list []string

func (l *list) String() string { return strings.Join(*l, ",") }

func (l *list) Set(v string) error {
	for _, s := range strings.Split(v, ",") {
		if s = strings.TrimSpace(s); s != "" {
			*l = append(*l, s)
		}
	}
	return nil
}
