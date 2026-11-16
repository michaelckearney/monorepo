// Command desktop is the desktop framework, as the harness runs it: the
// verbs test, record, run, build, cli, and modes, for a project in its layout (see
// the README), or test for the framework's own code.
//
// It speaks the harness's contract itself, rather than importing it, so a
// project builds with go and npm alone: the harness is how it is developed,
// never what it needs.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"

	"github.com/michaelckearney/monorepo/framework/internal/build"
	"github.com/michaelckearney/monorepo/framework/internal/cli"
	"github.com/michaelckearney/monorepo/framework/internal/pack"
	"github.com/michaelckearney/monorepo/framework/internal/project"
	"github.com/michaelckearney/monorepo/framework/internal/record"
	"github.com/michaelckearney/monorepo/framework/internal/run"
	"github.com/michaelckearney/monorepo/framework/internal/test"
)

// result is what a verb tells the harness besides its exit code.
type result struct {
	Full      bool     `json:"full,omitempty"`
	Artifacts []string `json:"artifacts,omitempty"`
}

// call is the verb as the harness gave it.
type call struct {
	verb    string
	args    []string
	project string // the project's folder, or "" for the framework itself
	cwd     string // where the harness ran
	version string
}

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: desktop <verb> [args...], run by the harness: harness test, record, run, build, cli, or modes")
		os.Exit(2)
	}
	c := call{verb: os.Args[1], args: os.Args[2:], project: os.Getenv("HARNESS_PROJECT"),
		cwd: os.Getenv("HARNESS_CWD"), version: os.Getenv("HARNESS_VERSION")}
	if c.cwd == "" {
		c.cwd, _ = os.Getwd()
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	var r result
	var err error
	code := 0
	switch {
	case c.verb == "test" && c.project == "":
		r, err = selfTest(ctx, c)
	case c.project == "":
		usageError(fmt.Sprintf("harness %s needs a project", c.verb))
	case c.verb == "test":
		ph := testFlags(c)
		r.Full = ph.Full()
		err = test.Run(ctx, c.project, ph)
	case c.verb == "run":
		err = run.Run(ctx, c.project, modeFlag(c))
	case c.verb == "build":
		var targets []string
		// What was built is packed even when a platform failed, so the
		// builds that worked can be used.
		targets, err = build.Run(ctx, c.project, strings.TrimPrefix(c.version, "v"))
		if len(targets) > 0 {
			assets, perr := pack.Pack(c.project, targets)
			if err == nil {
				err = perr
			}
			for _, a := range assets {
				rel, _ := filepath.Rel(c.project, a)
				r.Artifacts = append(r.Artifacts, rel)
			}
		}
	case c.verb == "record":
		switch {
		case len(c.args) == 0:
			fmt.Print(record.Usage())
		case c.args[0] == "start":
			c.args = c.args[1:]
			err = record.Start(ctx, c.project, modeFlag(c))
		case c.args[0] == "stop":
			if len(c.args) > 1 {
				usageError("harness record stop takes no arguments")
			}
			err = record.Stop(ctx, c.project)
		default:
			err = record.Do(ctx, c.project, c.args[0], c.args[1:])
		}
	case c.verb == "modes":
		if len(c.args) > 0 {
			usageError("harness modes takes no arguments")
		}
		err = modes(c.project)
	case c.verb == "cli":
		mode, rest := cliMode(c.args)
		code, err = cli.Run(ctx, c.project, mode, rest)
	default:
		usageError(fmt.Sprintf("the desktop framework has no verb %q", c.verb))
	}
	if werr := write(r); werr != nil && err == nil {
		err = werr
	}
	if err != nil {
		var perr *project.Error
		if !errors.As(err, &perr) {
			err = fmt.Errorf("unexpected error: %w", err)
		}
		fmt.Fprintf(os.Stderr, "desktop: %v\n", err)
		os.Exit(1)
	}
	os.Exit(code)
}

// modes prints the project's modes as JSON, for Slipway: the default
// first, each with the app's name in it.
func modes(dir string) error {
	p, err := project.Load(dir)
	if err != nil {
		return err
	}
	type mode struct {
		Mode string `json:"mode"`
		Name string `json:"name"`
	}
	out := struct {
		Default string `json:"default"`
		Modes   []mode `json:"modes"`
	}{Default: p.Config.DefaultMode, Modes: []mode{}}
	for _, name := range p.Config.ModeNames() {
		out.Modes = append(out.Modes, mode{Mode: name, Name: p.Config.Mode(name).Name})
	}
	data, err := json.Marshal(out)
	if err != nil {
		return err
	}
	fmt.Println(string(data))
	return nil
}

// write gives the harness the verb's result.
func write(r result) error {
	path := os.Getenv("HARNESS_RESULT")
	if path == "" {
		return nil
	}
	data, err := json.Marshal(r)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o600)
}

// selfTest is test on the framework's own code: vet, then its Go tests.
// With -short or -run, it is not the whole of them.
func selfTest(ctx context.Context, c call) (result, error) {
	dir, _ := os.Getwd()
	steps, full := selfSteps(c.args)
	for _, step := range steps {
		fmt.Fprintf(os.Stderr, "desktop: go %s\n", strings.Join(step, " "))
		cmd := exec.CommandContext(ctx, "go", step...)
		cmd.Dir = dir
		// Without cgo, on Wails' purego backend, as the apps are built: with
		// cgo, Wails' packages need GTK and WebKit headers a CI runner lacks.
		cmd.Env = append(os.Environ(), "CGO_ENABLED=0", "GOFLAGS=-tags=purego")
		cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
		if err := cmd.Run(); err != nil {
			return result{Full: full}, &project.Error{Msg: "go " + strings.Join(step, " ") + " failed"}
		}
	}
	return result{Full: full}, nil
}

// selfSteps are the go commands of selfTest. -lint, -unit, and -e2e pick
// phases, as they do for a project: vet is the framework's lint, its Go tests
// its unit tests, and it has no e2e tests of its own. The other arguments are
// go test's.
func selfSteps(args []string) ([][]string, bool) {
	var lint, unit, e2e bool
	var rest []string
	for _, a := range args {
		switch a {
		case "-lint", "--lint":
			lint = true
		case "-unit", "--unit":
			unit = true
		case "-e2e", "--e2e":
			e2e = true
		default:
			rest = append(rest, a)
		}
	}
	if !lint && !unit && !e2e {
		lint, unit = true, true
	}
	var steps [][]string
	if lint {
		steps = append(steps, []string{"vet", "./..."})
	}
	if unit {
		steps = append(steps, append(append([]string{"test"}, rest...), "./..."))
	}
	return steps, len(args) == 0
}

// testFlags parses harness test's flags and paths. The flags may come
// before or after the paths; the paths are from where the harness ran.
func testFlags(c call) test.Phases {
	var ph test.Phases
	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.BoolVar(&ph.Lint, "lint", false, "")
	fs.BoolVar(&ph.Unit, "unit", false, "")
	fs.BoolVar(&ph.E2E, "e2e", false, "")
	fs.StringVar(&ph.Run, "run", "", "")
	args := c.args
	for len(args) > 0 {
		if err := fs.Parse(args); err != nil {
			usageError("harness test: " + err.Error())
		}
		args = fs.Args()
		if len(args) > 0 {
			ph.Paths = append(ph.Paths, projectPath(c, args[0]))
			args = args[1:]
		}
	}
	if ph.Run != "" {
		if _, err := regexp.Compile(ph.Run); err != nil {
			usageError("harness test: -run: " + err.Error())
		}
	}
	return ph
}

// projectPath makes a path from where the harness ran into one inside the
// project, which is where the framework works.
func projectPath(c call, arg string) string {
	abs := arg
	if !filepath.IsAbs(arg) {
		abs = filepath.Join(c.cwd, arg)
	}
	rel, err := filepath.Rel(c.project, abs)
	if err != nil || strings.HasPrefix(rel, "..") {
		return arg
	}
	return rel
}

// modeFlag parses the -mode a verb takes, its only flag. "" is the default
// mode.
func modeFlag(c call) string {
	var mode string
	fs := flag.NewFlagSet(c.verb, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.StringVar(&mode, "mode", "", "")
	if err := fs.Parse(c.args); err != nil {
		usageError("harness " + c.verb + ": " + err.Error())
	}
	if fs.NArg() > 0 {
		usageError(fmt.Sprintf("harness %s: unexpected argument %q; the only flag is -mode=<mode>", c.verb, fs.Arg(0)))
	}
	return mode
}

// cliMode takes the -mode harness cli may have before the program's own
// arguments, which are passed on whatever they look like. A -- ends the
// framework's arguments.
func cliMode(args []string) (string, []string) {
	var mode string
	switch {
	case len(args) > 0 && (strings.HasPrefix(args[0], "-mode=") || strings.HasPrefix(args[0], "--mode=")):
		_, mode, _ = strings.Cut(args[0], "=")
		args = args[1:]
	case len(args) > 1 && (args[0] == "-mode" || args[0] == "--mode"):
		mode, args = args[1], args[2:]
	}
	if len(args) > 0 && args[0] == "--" {
		args = args[1:]
	}
	return mode, args
}

func usageError(msg string) {
	fmt.Fprintln(os.Stderr, msg)
	os.Exit(2)
}
