//go:build !windows

package backend

import (
	"os/exec"
	"syscall"
)

// group puts a run in its own process group, so stopping it stops the app
// and Vite it started too.
func group(cmd *exec.Cmd) { cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true} }

// interrupt asks a run to stop, as Ctrl-C would.
func interrupt(cmd *exec.Cmd) { syscall.Kill(-cmd.Process.Pid, syscall.SIGINT) }

func kill(cmd *exec.Cmd) { syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
