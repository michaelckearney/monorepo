package record

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"syscall"
	"time"

	"github.com/michaelckearney/monorepo/framework/internal/project"
	"github.com/michaelckearney/monorepo/framework/internal/steps"
)

// step is one recorded action, kept so `stop` can replay it.
type step struct {
	// Command is what the agent typed, for the report.
	Command string `json:"command"`
	Kind    string `json:"kind"` // action, see, not-see, shot, restart
	// Code is the Playwright code an action ran, with element references
	// already turned into locators by name, so it works on a fresh page.
	Code string `json:"code,omitempty"`
	Text string `json:"text,omitempty"`
}

// state is the recording in progress. It lives in .desktop/record/, because
// each `harness record` command is a separate process.
type state struct {
	Session   string `json:"session"`
	Port      int    `json:"port"`
	ServerPID int    `json:"serverPid"`
	Server    string `json:"server"`
	// DataDir is the app's data folder for this session, new at start and
	// kept across restarts, so a restart shows what the app saved.
	DataDir  string    `json:"dataDir"`
	Started  time.Time `json:"started"`
	Snapshot string    `json:"snapshot"`
	Steps    []step    `json:"steps"`
}

func recDir(p *project.Project) string {
	return filepath.Join(p.DesktopDir, "record")
}

func load(p *project.Project) (*state, error) {
	raw, err := os.ReadFile(filepath.Join(recDir(p), "state.json"))
	if errors.Is(err, os.ErrNotExist) {
		return nil, steps.Fail("no recording in progress; run: harness record start")
	}
	if err != nil {
		return nil, err
	}
	var s state
	if err := json.Unmarshal(raw, &s); err != nil {
		return nil, err
	}
	return &s, nil
}

func (s *state) save(p *project.Project) error {
	raw, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(recDir(p), "state.json"), raw, 0o644)
}

func clear(p *project.Project) error {
	return os.Remove(filepath.Join(recDir(p), "state.json"))
}

// stopServer ends the app's server process group. The server outlives the
// `harness record start` that started it, so it is found by its saved pid.
func stopServer(pid int) {
	if pid <= 0 {
		return
	}
	_ = syscall.Kill(-pid, syscall.SIGTERM)
	for i := 0; i < 30; i++ {
		if syscall.Kill(pid, 0) != nil {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	_ = syscall.Kill(-pid, syscall.SIGKILL)
}
