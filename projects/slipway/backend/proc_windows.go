package backend

import "os/exec"

// Windows has no process groups to signal: a run is stopped outright.
func group(cmd *exec.Cmd) {}

func interrupt(cmd *exec.Cmd) { cmd.Process.Kill() }

func kill(cmd *exec.Cmd) { cmd.Process.Kill() }
