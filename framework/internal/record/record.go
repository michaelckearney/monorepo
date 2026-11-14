// Package record implements `harness record`: an agent drives the app step by
// step, then desktop replays those steps into a video with screenshots and a
// report, as evidence of what the app does.
//
// The agent works live, one command at a time, against a server build of the
// app in WebKit, through Playwright CLI. Each command prints what changed on
// screen. Nothing is filmed while the agent works: a model that thinks for
// twenty seconds between steps made a video that was mostly a still screen.
// `stop` replays the steps on a freshly started app, at a steady pace, with
// the video on.
package record

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/michaelckearney/monorepo/framework/internal/generate"
	"github.com/michaelckearney/monorepo/framework/internal/project"
	"github.com/michaelckearney/monorepo/framework/internal/steps"
)

// actions change the page, and are recorded and replayed. The number is how
// many arguments each needs at least.
var actions = map[string]int{
	"click": 1, "dblclick": 1, "fill": 2, "type": 1, "press": 1, "hover": 1,
	"select": 2, "check": 1, "uncheck": 1, "drag": 2, "drop": 1, "upload": 1,
	"dialog-accept": 0, "dialog-dismiss": 0, "mousewheel": 2,
}

// readOnly commands look at the page or the app's traffic and change
// nothing, so they are passed through and not recorded.
var readOnly = map[string]bool{
	"snapshot": true, "find": true,
	"console": true, "requests": true, "request": true, "request-headers": true,
	"request-body": true, "response-headers": true, "response-body": true,
	"cookie-list": true, "cookie-get": true,
	"localstorage-list": true, "localstorage-get": true,
	"sessionstorage-list": true, "sessionstorage-get": true,
}

// Everything else playwright-cli offers is left out on purpose. eval,
// run-code, route, and the storage setters can put on screen what the app
// never did, which would make the evidence worthless. open, close, video,
// resize, emulation, and tabs belong to desktop, which fixes them so a replay
// matches what the agent saw. goto, go-back, and go-forward have no meaning
// in a desktop app, which has no address bar.

// Usage lists the commands, for `harness record` with no arguments.
func Usage() string {
	list := func(m map[string]int) string {
		var names []string
		for n := range m {
			names = append(names, n)
		}
		sort.Strings(names)
		return strings.Join(names, ", ")
	}
	var ro []string
	for n := range readOnly {
		ro = append(ro, n)
	}
	sort.Strings(ro)
	return fmt.Sprintf(`usage: harness record <command> [args]

  start [-mode=<mode>]  start the app, in desktop.yaml's default_mode unless
                        -mode names another, and show what is on screen
  stop                  replay the steps into .desktop/recordings/ with a video

  see <text>            check that text is on screen; fails if it is not
  not-see <text>        check that text is not on screen
  shot <caption>        take a screenshot, captioned by what it shows
  restart               quit and reopen the app, to show what survives

actions (use the [ref=eN] names from the screen list):
  %s
look and diagnose (not recorded):
  %s
`, list(actions), strings.Join(ro, ", "))
}

// Start builds the app in mode, or the default for "", starts it, and opens
// it in WebKit. A recording already in progress is discarded.
func Start(ctx context.Context, dir, modeName string) error {
	p, err := loadProject(dir)
	if err != nil {
		return err
	}
	mode, err := p.FindMode(modeName)
	if err != nil {
		return err
	}
	if old, err := load(p); err == nil {
		steps.Logf("discarding the recording in progress")
		discard(ctx, p, old)
	}

	bin, err := buildApp(ctx, p, mode)
	if err != nil {
		return err
	}
	c, err := newCLI(ctx, p, sessionName(p))
	if err != nil {
		return err
	}
	if err := writeConfig(p, "live"); err != nil {
		return err
	}
	port, err := steps.FreePort()
	if err != nil {
		return err
	}
	dataDir, err := steps.TempDataDir(generate.PathsFor(p))
	if err != nil {
		return err
	}
	pid, err := startServer(p, bin, port, dataDir)
	if err != nil {
		os.RemoveAll(dataDir)
		return err
	}
	s := &state{Session: c.session, Port: port, ServerPID: pid, Server: bin, DataDir: dataDir, Started: time.Now()}

	r := c.run(ctx, "open", url(port), "--config=cli.config.json")
	if r.failed {
		stopServer(pid)
		os.RemoveAll(dataDir)
		steps.PrintBlock("[playwright] ", []byte(r.out))
		return steps.Fail("could not open the app in WebKit; see the [playwright] lines above")
	}
	s.Snapshot = r.snapshot
	if err := s.save(p); err != nil {
		return err
	}
	fmt.Println("On screen:")
	fmt.Print(readSnapshot(r.snapshot))
	fmt.Println("\nAct with the [ref=eN] names, for example: harness record click e7")
	fmt.Println("Finish with: harness record stop")
	return nil
}

// Do runs one command against the recording in progress.
func Do(ctx context.Context, dir, verb string, args []string) error {
	p, err := loadProject(dir)
	if err != nil {
		return err
	}
	s, err := load(p)
	if err != nil {
		return err
	}
	c, err := newCLI(ctx, p, s.Session)
	if err != nil {
		return err
	}
	typed := strings.TrimSpace(verb + " " + strings.Join(args, " "))

	switch {
	case readOnly[verb]:
		r := c.run(ctx, append([]string{verb}, args...)...)
		if verb == "snapshot" && !r.failed {
			fmt.Print(readSnapshot(r.snapshot))
			s.Snapshot = r.snapshot
			return s.save(p)
		}
		fmt.Print(r.out)
		if r.failed {
			return steps.Fail("%s failed", verb)
		}
		return nil

	case verb == "see" || verb == "not-see":
		if len(args) < 1 {
			return steps.Fail("%s needs the text to look for, for example: harness record %s \"Saved\"", verb, verb)
		}
		text := strings.Join(args, " ")
		if r := c.runCode(ctx, check(verb, text)); r.failed {
			return showScreen(ctx, c, s, p, "%q is %s", text, map[string]string{"see": "not on screen", "not-see": "still on screen"}[verb])
		}
		s.Steps = append(s.Steps, step{Command: typed, Kind: verb, Text: text})
		fmt.Printf("ok: %s %q\n", map[string]string{"see": "on screen:", "not-see": "not on screen:"}[verb], text)
		return s.save(p)

	case verb == "shot":
		if len(args) < 1 {
			return steps.Fail("shot needs a caption that says what the screenshot shows, for example: harness record shot \"saved invoice in the list\"")
		}
		caption := strings.Join(args, " ")
		n := 1
		for _, st := range s.Steps {
			if st.Kind == "shot" {
				n++
			}
		}
		file := filepath.Join(recDir(p), "live", shotName(n, caption))
		if r := c.run(ctx, "screenshot", "--filename="+file); r.failed {
			fmt.Print(r.out)
			return steps.Fail("could not take the screenshot")
		}
		s.Steps = append(s.Steps, step{Command: typed, Kind: "shot", Text: caption})
		fmt.Printf("ok: screenshot %s (the final one is taken again during the replay)\n", rel(p, file))
		return s.save(p)

	case verb == "restart":
		stopServer(s.ServerPID)
		pid, err := startServer(p, s.Server, s.Port, s.DataDir)
		if err != nil {
			return err
		}
		s.ServerPID = pid
		r := c.runCode(ctx, "await page.reload();")
		if r.failed {
			fmt.Print(r.out)
			return steps.Fail("the app restarted, but the page did not reload")
		}
		s.Steps = append(s.Steps, step{Command: typed, Kind: "restart"})
		// A reload renumbers every element, so the old names are useless and
		// the whole list is shown again.
		snap := c.run(ctx, "snapshot")
		fmt.Println("ok: the app restarted. Element names have changed. On screen:")
		fmt.Print(readSnapshot(snap.snapshot))
		s.Snapshot = snap.snapshot
		return s.save(p)

	default:
		need, ok := actions[verb]
		if !ok {
			return steps.Fail("harness record has no command %q; run `harness record` to see the commands", verb)
		}
		if len(args) < need {
			return steps.Fail("%s needs %d argument(s); run `harness record` to see the commands", verb, need)
		}
		if verb == "upload" || verb == "drop" {
			args = absFiles(args)
		}
		r := c.run(ctx, append([]string{verb}, args...)...)
		if r.failed {
			return showScreen(ctx, c, s, p, "%s failed: %s", typed, r.errorText())
		}
		s.Steps = append(s.Steps, step{Command: typed, Kind: "action", Code: r.code})
		fmt.Printf("ok: %s\n", friendly(r.code))
		if strings.Contains(r.code, "aria-ref=") {
			fmt.Println("warning: that element has no name a person can read, so the replay cannot find it again; give it visible text or an aria-label, then start again")
		}
		if r.snapshot == "" {
			r.snapshot = c.run(ctx, "snapshot").snapshot
		}
		printChanges(s.Snapshot, r.snapshot)
		s.Snapshot = r.snapshot
		return s.save(p)
	}
}

// Stop ends the live session and replays its steps into a new folder under
// .desktop/recordings/.
func Stop(ctx context.Context, dir string) error {
	p, err := loadProject(dir)
	if err != nil {
		return err
	}
	s, err := load(p)
	if err != nil {
		return err
	}
	discard(ctx, p, s)
	if len(s.Steps) == 0 {
		steps.Logf("nothing was recorded")
		return nil
	}
	return replay(ctx, p, s)
}

// check is the Playwright code for see and not-see. Text matches a part of
// an element's text, ignoring case, as a person scanning the screen would.
func check(verb, text string) string {
	q, _ := json.Marshal(text)
	state := "visible"
	if verb == "not-see" {
		state = "hidden"
	}
	return fmt.Sprintf("await page.getByText(%s).first().waitFor({ state: %q, timeout: 5000 });", q, state)
}

// showScreen prints a failure and everything on screen, so the next attempt
// can use a name that is there.
func showScreen(ctx context.Context, c cli, s *state, p *project.Project, format string, args ...any) error {
	r := c.run(ctx, "snapshot")
	fmt.Println("On screen now:")
	fmt.Print(readSnapshot(r.snapshot))
	if r.snapshot != "" {
		s.Snapshot = r.snapshot
		_ = s.save(p)
	}
	return steps.Fail(format+"; nothing was recorded for this step", args...)
}

func loadProject(dir string) (*project.Project, error) {
	if err := steps.CheckTools(); err != nil {
		return nil, err
	}
	root, err := project.Find(dir)
	if err != nil {
		return nil, err
	}
	return project.Load(root)
}

func buildApp(ctx context.Context, p *project.Project, mode project.Mode) (string, error) {
	if err := steps.Packages(ctx, p); err != nil {
		return "", err
	}
	wails, err := steps.Wails3(ctx)
	if err != nil {
		return "", err
	}
	paths, err := steps.Prepare(ctx, p, wails)
	if err != nil {
		return "", err
	}
	return steps.BuildServer(ctx, p, paths, mode)
}

func newCLI(ctx context.Context, p *project.Project, session string) (cli, error) {
	bin, err := playwrightCLI(ctx)
	if err != nil {
		return cli{}, err
	}
	if err := os.MkdirAll(recDir(p), 0o755); err != nil {
		return cli{}, err
	}
	return cli{bin: bin, session: session, dir: recDir(p)}, nil
}

func sessionName(p *project.Project) string {
	return "desktop-" + project.Slug(p.Config.Name)
}

func url(port int) string { return fmt.Sprintf("http://127.0.0.1:%d/", port) }

// writeConfig fixes the browser the same way the e2e tests do: WebKit, the
// window size from desktop.yaml, light mode, and a fixed locale and zone.
func writeConfig(p *project.Project, outputDir string) error {
	cfg := map[string]any{
		"browser": map[string]any{
			"browserName": "webkit",
			"isolated":    true,
			"contextOptions": map[string]any{
				"viewport":    map[string]int{"width": p.Config.Window.Width, "height": p.Config.Window.Height},
				"colorScheme": "light",
				"locale":      "en-US",
				"timezoneId":  "UTC",
			},
		},
		"outputDir": outputDir,
	}
	raw, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(recDir(p), "cli.config.json"), raw, 0o644)
}

// startServer starts the app's server build in its own process group and
// leaves it running after desktop exits: the next `harness record` command is
// a new process that needs the same app.
func startServer(p *project.Project, bin string, port int, dataDir string) (int, error) {
	logf, err := os.OpenFile(filepath.Join(recDir(p), "app.log"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return 0, err
	}
	defer logf.Close()
	cmd := exec.Command(bin)
	cmd.Dir = p.Root
	cmd.Env = append(os.Environ(), "WAILS_SERVER_HOST=127.0.0.1", "WAILS_SERVER_PORT="+strconv.Itoa(port))
	cmd.Env = append(cmd.Env, steps.AppEnv(generate.PathsFor(p), dataDir)...)
	cmd.Stdout, cmd.Stderr = logf, logf
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		return 0, steps.Fail("could not start the app: %v", err)
	}
	pid := cmd.Process.Pid
	_ = cmd.Process.Release()
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		if syscall.Kill(pid, 0) != nil {
			return 0, steps.Fail("the app exited as it started; see .desktop/record/app.log")
		}
		if c, err := dial(port); err == nil {
			c.Close()
			return pid, nil
		}
		time.Sleep(100 * time.Millisecond)
	}
	stopServer(pid)
	return 0, steps.Fail("the app did not start within 30 seconds; see .desktop/record/app.log")
}

// discard closes the live browser and stops the app.
func discard(ctx context.Context, p *project.Project, s *state) {
	if c, err := newCLI(ctx, p, s.Session); err == nil {
		c.run(ctx, "close")
	}
	stopServer(s.ServerPID)
	if s.DataDir != "" {
		_ = os.RemoveAll(s.DataDir)
	}
	_ = clear(p)
}

func readSnapshot(snap string) string {
	if strings.TrimSpace(snap) == "" {
		return "(the screen list is empty)\n"
	}
	if !strings.HasSuffix(snap, "\n") {
		snap += "\n"
	}
	return snap
}

// noise is what changes on screen without anything happening: element
// references are renumbered after a reload (e6 becomes f1e6), and [active]
// follows the keyboard focus.
var noise = regexp.MustCompile(` \[(ref=[a-z0-9]+|active)\]`)

// printChanges shows the lines of the screen list that appeared and went
// away. A whole list after every step filled a small model's context with
// repeats; the full list is printed only at the start and on failure.
func printChanges(before, after string) {
	if after == "" {
		return
	}
	old := map[string]bool{}
	for _, l := range strings.Split(readSnapshot(before), "\n") {
		old[noise.ReplaceAllString(strings.TrimSpace(l), "")] = true
	}
	now := map[string]bool{}
	var appeared []string
	for _, l := range strings.Split(readSnapshot(after), "\n") {
		key := noise.ReplaceAllString(strings.TrimSpace(l), "")
		now[key] = true
		if key != "" && !old[key] {
			appeared = append(appeared, strings.TrimSpace(l))
		}
	}
	var gone []string
	for k := range old {
		if k != "" && !now[k] {
			gone = append(gone, k)
		}
	}
	sort.Strings(gone)
	if len(appeared) == 0 && len(gone) == 0 {
		fmt.Println("no change on screen")
		return
	}
	for _, l := range appeared {
		fmt.Println("  appeared: " + l)
	}
	for _, l := range gone {
		fmt.Println("  gone:     " + l)
	}
}

// friendly turns "await page.getByRole('button', { name: 'Greet' }).click();"
// into "getByRole('button', { name: 'Greet' }).click()".
func friendly(code string) string {
	// Some commands lead with a comment, such as "// Press Enter".
	var lines []string
	for _, l := range strings.Split(code, "\n") {
		if l = strings.TrimSpace(l); l != "" && !strings.HasPrefix(l, "//") {
			lines = append(lines, l)
		}
	}
	code = strings.Join(lines, " ")
	code = strings.TrimPrefix(code, "await ")
	code = strings.TrimPrefix(code, "page.")
	return strings.TrimSuffix(code, ";")
}

var notSlug = regexp.MustCompile(`[^a-z0-9]+`)

func shotName(n int, caption string) string {
	slug := strings.Trim(notSlug.ReplaceAllString(strings.ToLower(caption), "-"), "-")
	if len(slug) > 40 {
		slug = strings.TrimRight(slug[:40], "-")
	}
	return fmt.Sprintf("%02d-%s.png", n, slug)
}

func rel(p *project.Project, path string) string {
	r, err := filepath.Rel(p.Root, path)
	if err != nil {
		return path
	}
	return r
}

var ref = regexp.MustCompile(`^(f\d+)?e\d+$`)

// absFiles makes file arguments absolute. playwright-cli runs from
// .desktop/record, so a path relative to where the agent stands would not be
// found there.
func absFiles(args []string) []string {
	out := make([]string, len(args))
	for i, a := range args {
		out[i] = a
		if ref.MatchString(a) {
			continue
		}
		if abs, err := filepath.Abs(a); err == nil {
			if _, err := os.Stat(abs); err == nil {
				out[i] = abs
			}
		}
	}
	return out
}
