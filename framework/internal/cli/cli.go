// Package cli implements `harness cli`: build the project's command-line
// program, the main package in cli/, and run it with the data folder,
// dependencies, and migrations the app has under `harness run`.
//
// It shares harness run's data folder, so the program sees what the running
// app sees, and the app's live queries show what the program writes.
package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/michaelckearney/monorepo/framework/internal/appstate"
	"github.com/michaelckearney/monorepo/framework/internal/deps"
	"github.com/michaelckearney/monorepo/framework/internal/generate"
	"github.com/michaelckearney/monorepo/framework/internal/project"
	"github.com/michaelckearney/monorepo/framework/internal/steps"
)

// Run builds cli/ in mode, "" for the default, and runs it with args from
// the folder desktop was run in. It returns the program's exit code.
func Run(ctx context.Context, dir, mode string, args []string) (int, error) {
	if _, err := exec.LookPath("go"); err != nil {
		return 0, steps.Fail("go is not on PATH; install Go 1.26 or later from https://go.dev/dl/")
	}
	root, err := project.Find(dir)
	if err != nil {
		return 0, err
	}
	p, err := project.Load(root)
	if err != nil {
		return 0, err
	}
	m, err := p.FindMode(mode)
	if err != nil {
		return 0, err
	}
	if files, _ := filepath.Glob(filepath.Join(p.CLIDir, "*.go")); len(files) == 0 {
		return 0, steps.Fail("cli/ has no Go files; harness cli runs the program in cli/, a main package of the project's module")
	}

	migrations, err := generate.CLIMigrations(p, m.Mode)
	if err != nil {
		return 0, err
	}
	paths := generate.PathsFor(p)
	if err := deps.Ensure(ctx, p, paths.DepsDir); err != nil {
		return 0, err
	}
	bin := filepath.Join(paths.BinDir, m.Mode, "cli")
	if err := build(ctx, p, m, bin); err != nil {
		return 0, err
	}

	cwd, err := os.Getwd()
	if err != nil {
		return 0, err
	}
	cmd := exec.Command(bin, args...)
	cmd.Dir = cwd
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	cmd.Env = append(os.Environ(), steps.AppEnv(paths, filepath.Join(paths.DataDir, m.Mode))...)
	cmd.Env = append(cmd.Env, appstate.EnvMigrations+"="+migrations)
	// Ctrl-C in a terminal reaches the program as well as desktop, but a
	// signal sent to desktop alone, as a harness stopping a command does,
	// would leave the program running without it.
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(sigs)
	if err := cmd.Start(); err != nil {
		return 0, steps.Fail("could not start cli/: %v", err)
	}
	done := make(chan struct{})
	defer close(done)
	go func() {
		for {
			select {
			case s := <-sigs:
				_ = cmd.Process.Signal(s)
			case <-done:
				return
			}
		}
	}()
	err = cmd.Wait()
	var exit *exec.ExitError
	switch {
	case err == nil:
		return 0, nil
	case errors.As(err, &exit):
		if ws, ok := exit.Sys().(syscall.WaitStatus); ok && ws.Signaled() {
			return 128 + int(ws.Signal()), nil
		}
		return exit.ExitCode(), nil
	default:
		return 0, err
	}
}

// build compiles cli/ as a server build, as go test does: the program has
// no window, and a window build would load GTK and WebKit, which a headless
// machine does not have.
func build(ctx context.Context, p *project.Project, m project.Mode, bin string) error {
	if err := os.MkdirAll(filepath.Dir(bin), 0o755); err != nil {
		return err
	}
	cmd := exec.CommandContext(ctx, "go", "build", "-buildvcs=false", "-o", bin,
		"-tags", "server,"+generate.ModeTag(m.Mode),
		fmt.Sprintf("-ldflags=-X 'github.com/michaelckearney/monorepo/framework/internal/appstate.Mode=%s'", m.Mode),
		"./cli")
	cmd.Dir = p.Root
	env, err := steps.ProjectGoEnv(p)
	if err != nil {
		return err
	}
	cmd.Env = env
	if out, err := cmd.CombinedOutput(); err != nil {
		steps.PrintBlock("[go] ", out)
		return steps.Fail("cli/ did not compile; fix the errors above")
	}
	return nil
}
