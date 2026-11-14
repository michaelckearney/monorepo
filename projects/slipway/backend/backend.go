// Package backend is the Slipway app's Go half. It asks three things, and
// keeps little of its own:
//
//   - the server's API (`harness serve`), for the bots' threads and what they
//     are doing, live
//   - GitHub's API, with your token, for issues and pull requests, and to
//     reply, so a reply comes from your account and not the bots' GitHub App
//   - harness run, in a clone of each project the app keeps itself, to run
//     any pull request on this computer
//
// A dev build asks the made-up server and GitHub in package fixture instead.
package backend

import (
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/michaelckearney/monorepo/framework"
	"github.com/michaelckearney/monorepo/projects/slipway/backend/fixture"
)

// Services lists what the frontend can call, each an import from "@backend".
func Services() []desktop.Service {
	var services []desktop.Service
	if desktop.Mode() == "dev" {
		// The dev build's server and GitHub are the fixtures, which its
		// database starts pointed at (database/dev/).
		f, err := fixture.Start()
		if err != nil {
			log.Fatalf("the fixtures: %v", err)
		}
		transport = f.Transport(http.DefaultTransport)
		services = append(services, desktop.NewService(&Fixtures{f}))
	}
	db := desktop.DB()
	settings := &Settings{db: db}
	server := &Server{db: db, settings: settings}
	github := &GitHub{settings: settings}
	go server.follow()
	return append(services,
		desktop.NewService(settings),
		desktop.NewService(server),
		desktop.NewService(github),
		desktop.NewService(&Local{settings: settings, runs: map[string]*localRun{}}),
		desktop.NewService(&Updates{github: github}),
	)
}

// Fixtures stops the fixtures when a dev build quits.
type Fixtures struct{ f *fixture.Fixture }

// ServiceShutdown stops the fixtures.
func (f *Fixtures) ServiceShutdown() error {
	f.f.Stop()
	return nil
}

// transport carries every request to the server and to GitHub. A dev build's
// sends them to the fixtures.
var transport http.RoundTripper = http.DefaultTransport

// httpClient is a client on transport; 0 is no timeout, for a stream.
func httpClient(timeout time.Duration) *http.Client {
	return &http.Client{Timeout: timeout, Transport: transport}
}

// Settings are the app's preferences.
type Settings struct{ db *sql.DB }

// Server is the server's API, such as http://server:7420.
func (s *Settings) Server() (string, error) { return s.get("server") }

// SetServer changes the server's API. The live stream follows it.
func (s *Settings) SetServer(url string) error {
	return s.set("server", strings.TrimRight(strings.TrimSpace(url), "/"))
}

// SetToken changes the GitHub token the app asks GitHub with, as you. The
// frontend can set it but not read it back.
func (s *Settings) SetToken(token string) error {
	return s.set("github_token", strings.TrimSpace(token))
}

// preferences are the settings the window keeps for itself, which it reads
// with a live query: the theme and mode, the side panels' widths, and the
// sidebar's folded sections.
var preferences = map[string]bool{"theme": true, "mode": true, "left_panel": true, "right_panel": true, "folded": true}

// SetPreference keeps one of the window's own settings.
func (s *Settings) SetPreference(key, value string) error {
	if !preferences[key] {
		return fmt.Errorf("%q is not a preference", key)
	}
	return s.set(key, value)
}

// A theme you added is kept in settings as a row "theme:<name>" holding its
// colours, beside the window's other preferences; the built-in themes are in
// the frontend (@ui/themes). The frontend reads them and the theme in use with
// a live query, and the shared Appearance part asks the app to set the mode,
// use a theme, and add or delete one — the mode and the theme in use are kept
// by SetPreference, and the two below keep and remove a theme's colours.

// SaveTheme saves a theme of yours, and uses it. original is the saved theme
// you are changing, or "" when adding one; renaming moves its row. tokens is
// JSON: {"light": {...}, "dark": {...}}, each CSS variables by name without
// the dashes, such as "primary": "#0e5e6f". It is an error if the name belongs
// to another theme of yours.
func (s *Settings) SaveTheme(original, name, tokens string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return errors.New("a theme needs a name")
	}
	var t struct{ Light, Dark map[string]string }
	if err := json.Unmarshal([]byte(tokens), &t); err != nil {
		return fmt.Errorf("the theme's colours are not JSON: %w", err)
	}
	if len(t.Light) == 0 || len(t.Dark) == 0 {
		return errors.New("a theme has light and dark colours")
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	key := "theme:" + name
	var exists int
	if err := tx.QueryRow(`SELECT count(*) FROM settings WHERE key = ?`, key).Scan(&exists); err != nil {
		return err
	}
	if exists > 0 && name != original {
		return errors.New("A theme with this name exists.")
	}
	if original != "" && original != name {
		if _, err := tx.Exec(`DELETE FROM settings WHERE key = ?`, "theme:"+original); err != nil {
			return err
		}
		if _, err := tx.Exec(`DELETE FROM settings WHERE key = 'theme' AND value = ?`, original); err != nil {
			return err
		}
	}
	if _, err := tx.Exec(`INSERT INTO settings (key, value) VALUES (?, ?)
		ON CONFLICT (key) DO UPDATE SET value = excluded.value`, key, tokens); err != nil {
		return err
	}
	if _, err := tx.Exec(`INSERT INTO settings (key, value) VALUES ('theme', ?)
		ON CONFLICT (key) DO UPDATE SET value = excluded.value`, name); err != nil {
		return err
	}
	return tx.Commit()
}

// DeleteTheme deletes a theme of yours. If it was in use, Slipway's own is.
func (s *Settings) DeleteTheme(name string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`DELETE FROM settings WHERE key = ?`, "theme:"+name); err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM settings WHERE key = 'theme' AND value = ?`, name); err != nil {
		return err
	}
	return tx.Commit()
}

// token is the GitHub token, or "".
func (s *Settings) token() string {
	if s == nil {
		return ""
	}
	t, _ := s.get("github_token")
	return t
}

func (s *Settings) get(key string) (string, error) {
	var v string
	err := s.db.QueryRow(`SELECT value FROM settings WHERE key = ?`, key).Scan(&v)
	if err == sql.ErrNoRows {
		return "", nil
	}
	return v, err
}

func (s *Settings) set(key, value string) error {
	_, err := s.db.Exec(`INSERT INTO settings (key, value) VALUES (?, ?)
		ON CONFLICT (key) DO UPDATE SET value = excluded.value`, key, value)
	return err
}

// tool finds a command. An app opened from the Finder gets launchd's PATH,
// /usr/bin:/bin:/usr/sbin:/sbin, which has neither Homebrew's tools nor a
// harness from go install, so the usual places are tried too.
func tool(name string) string {
	if p, err := exec.LookPath(name); err == nil {
		return p
	}
	home, _ := os.UserHomeDir()
	for _, dir := range []string{"/opt/homebrew/bin", "/usr/local/bin", filepath.Join(home, "go", "bin"),
		filepath.Join(home, ".local", "bin"), "/usr/bin"} {
		p := filepath.Join(dir, name)
		if st, err := os.Stat(p); err == nil && !st.IsDir() {
			return p
		}
	}
	return name
}

// env is the environment for the commands the app runs, with the places
// tool looks on PATH, so the harness can find git, go, and npm.
func env() []string {
	home, _ := os.UserHomeDir()
	extra := []string{"/opt/homebrew/bin", "/usr/local/bin", "/usr/local/go/bin", filepath.Join(home, "go", "bin"), filepath.Join(home, ".local", "bin")}
	out := os.Environ()
	for i, kv := range out {
		if strings.HasPrefix(kv, "PATH=") {
			out[i] = kv + ":" + strings.Join(extra, ":")
			return out
		}
	}
	return append(out, "PATH=/usr/bin:/bin:"+strings.Join(extra, ":"))
}

// gitEnv is env, with your GitHub token for git's requests to github.com,
// so the app's clones need no credential helper, and for gh, should the
// harness ask it. The token is in the environment, not in a command line
// that other users' ps can see.
//
// Any git configuration already in the environment is dropped: its
// GIT_CONFIG_COUNT would count the token's entry wrong, or give git a
// header the app never meant it to have.
func gitEnv(token string) []string {
	out := []string{"GIT_TERMINAL_PROMPT=0"}
	for _, kv := range env() {
		if !strings.HasPrefix(kv, "GIT_CONFIG_COUNT=") && !strings.HasPrefix(kv, "GIT_CONFIG_KEY_") && !strings.HasPrefix(kv, "GIT_CONFIG_VALUE_") &&
			!strings.HasPrefix(kv, "GH_TOKEN=") && !strings.HasPrefix(kv, "GIT_TERMINAL_PROMPT=") {
			out = append(out, kv)
		}
	}
	if token == "" {
		return out
	}
	basic := base64.StdEncoding.EncodeToString([]byte("x-access-token:" + token))
	return append(out, "GIT_CONFIG_COUNT=1", "GIT_CONFIG_KEY_0=http.https://github.com/.extraheader",
		"GIT_CONFIG_VALUE_0=Authorization: Basic "+basic, "GH_TOKEN="+token)
}
