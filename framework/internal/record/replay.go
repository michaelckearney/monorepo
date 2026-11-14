package record

import (
	"context"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/michaelckearney/monorepo/framework/internal/generate"
	"github.com/michaelckearney/monorepo/framework/internal/gitversion"
	"github.com/michaelckearney/monorepo/framework/internal/project"
	"github.com/michaelckearney/monorepo/framework/internal/steps"
)

// pace is the pause after each replayed step, so a person watching can see
// what happened before the next thing does.
const pace = 700 * time.Millisecond

// outcome is one replayed step's result, for the report.
type outcome struct {
	step   step
	ok     bool
	detail string
	shot   string
}

// replay runs the recorded steps on a freshly started app with the video on,
// and writes the video, screenshots, and a report to
// .desktop/recordings/<time>/.
func replay(ctx context.Context, p *project.Project, s *state) error {
	// Under .desktop/, which keeps itself out of git: evidence is attached to
	// a pull request, not committed.
	out := filepath.Join(p.DesktopDir, "recordings", time.Now().Format("2006-01-02-150405"))
	if err := os.MkdirAll(out, 0o755); err != nil {
		return err
	}

	steps.Logf("replaying %d steps on a fresh app, with video", len(s.Steps))
	c, err := newCLI(ctx, p, s.Session+"-replay")
	if err != nil {
		return err
	}
	if err := writeConfig(p, "replay"); err != nil {
		return err
	}
	port, err := steps.FreePort()
	if err != nil {
		return err
	}
	// A fresh app has fresh data: the replay must not depend on what the
	// live session saved.
	dataDir, err := steps.TempDataDir(generate.PathsFor(p))
	if err != nil {
		return err
	}
	defer os.RemoveAll(dataDir)
	pid, err := startServer(p, s.Server, port, dataDir)
	if err != nil {
		return err
	}
	defer func() { stopServer(pid) }()
	defer c.run(context.WithoutCancel(ctx), "close")

	if r := c.run(ctx, "open", url(port), "--config=cli.config.json"); r.failed {
		steps.PrintBlock("[playwright] ", []byte(r.out))
		return steps.Fail("could not open the app for the replay")
	}
	video := filepath.Join(out, "video.webm")
	c.run(ctx, "video-start", video)
	c.run(ctx, "video-show-actions", "--duration=800")
	time.Sleep(pace)

	var results []outcome
	failed := false
	shots := 0
	for i, st := range s.Steps {
		o := outcome{step: st, ok: true}
		switch st.Kind {
		case "action":
			if accept, ok := dialogAfter(s.Steps, i); ok {
				// A dialog has to be answered by a handler set before the
				// step that opens it; live, the agent answered it after.
				c.runCode(ctx, fmt.Sprintf("page.once('dialog', d => d.%s());", map[bool]string{true: "accept", false: "dismiss"}[accept]))
			}
			if strings.HasPrefix(st.Command, "dialog-") {
				break // answered by the handler set before the step before it
			}
			if r := c.runCode(ctx, st.Code); r.failed {
				o.ok, o.detail = false, r.errorText()
			}
		case "see", "not-see":
			if r := c.runCode(ctx, check(st.Kind, st.Text)); r.failed {
				o.ok, o.detail = false, fmt.Sprintf("%q was %s", st.Text, map[string]string{"see": "not on screen", "not-see": "still on screen"}[st.Kind])
			}
		case "shot":
			shots++
			name := shotName(shots, st.Text)
			if r := c.run(ctx, "screenshot", "--filename="+filepath.Join(out, name)); r.failed {
				o.ok, o.detail = false, r.errorText()
			} else {
				o.shot = name
				c.run(ctx, "video-chapter", st.Text, "--duration=1500")
			}
		case "restart":
			stopServer(pid)
			if pid, err = startServer(p, s.Server, port, dataDir); err != nil {
				o.ok, o.detail = false, err.Error()
			} else if r := c.runCode(ctx, "await page.reload();"); r.failed {
				o.ok, o.detail = false, r.errorText()
			}
		}
		results = append(results, o)
		mark := "ok  "
		if !o.ok {
			mark = "FAIL"
		}
		fmt.Printf("%s %2d %s\n", mark, i+1, describe(st))
		if !o.ok {
			fmt.Printf("        %s\n", o.detail)
			failed = true
			break
		}
		time.Sleep(pace)
	}
	c.run(ctx, "video-stop")

	report := filepath.Join(out, "report.md")
	if err := os.WriteFile(report, []byte(writeReport(p, s, results, failed)), 0o644); err != nil {
		return err
	}
	_ = os.WriteFile(filepath.Join(out, "steps.txt"), []byte(stepList(s.Steps)), 0o644)

	// Every file, with its path from the project, so an agent can put the
	// evidence in its Markdown without opening the report to find it.
	steps.Logf("video:  %s", rel(p, video))
	for _, o := range results {
		if o.shot != "" {
			steps.Logf("shot:   %s  %q", rel(p, filepath.Join(out, o.shot)), o.step.Text)
		}
	}
	steps.Logf("report: %s", rel(p, report))
	if failed {
		return steps.Fail("the replay did not match the live session; the step marked FAIL behaved differently on a fresh app")
	}
	return nil
}

// dialogAfter reports whether the step after i answers a dialog, and how.
func dialogAfter(all []step, i int) (accept bool, ok bool) {
	if i+1 >= len(all) {
		return false, false
	}
	switch strings.Fields(all[i+1].Command)[0] {
	case "dialog-accept":
		return true, true
	case "dialog-dismiss":
		return false, true
	}
	return false, false
}

// describe is a step as a person reads it. Element references mean nothing
// after the session, so actions are shown by the locator they resolved to.
func describe(st step) string {
	switch st.Kind {
	case "action":
		if st.Code == "" {
			return st.Command
		}
		return friendly(st.Code)
	case "see":
		return fmt.Sprintf("see %q", st.Text)
	case "not-see":
		return fmt.Sprintf("not-see %q", st.Text)
	case "shot":
		return fmt.Sprintf("screenshot: %s", st.Text)
	default:
		return st.Command
	}
}

func writeReport(p *project.Project, s *state, results []outcome, failed bool) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# %s: recording\n\n", p.Config.Name)
	// The version is git's, which a project outside a repository has none of.
	version := ""
	if v, err := gitversion.Build(context.Background(), p.Root); err == nil {
		version = ", version " + v
	}
	fmt.Fprintf(&b, "Recorded %s%s, in WebKit at %dx%d.\n\n",
		s.Started.Format("2006-01-02 15:04"), version, p.Config.Window.Width, p.Config.Window.Height)
	if failed {
		fmt.Fprintf(&b, "**Result: failed.** The replay stopped at step %d of %d.\n\n", len(results), len(s.Steps))
	} else {
		fmt.Fprintf(&b, "**Result: every step passed** (%d steps).\n\n", len(s.Steps))
	}
	b.WriteString("| # | Step | Result |\n|---|---|---|\n")
	for i, o := range results {
		res := "ok"
		if !o.ok {
			res = "**failed:** " + strings.ReplaceAll(o.detail, "\n", " ")
		}
		fmt.Fprintf(&b, "| %d | `%s` | %s |\n", i+1, strings.ReplaceAll(describe(o.step), "|", "\\|"), res)
	}
	var shots []outcome
	for _, o := range results {
		if o.shot != "" {
			shots = append(shots, o)
		}
	}
	if len(shots) > 0 {
		b.WriteString("\n## Screenshots\n")
		for _, o := range shots {
			fmt.Fprintf(&b, "\n**%s**\n\n![%s](%s)\n", o.step.Text, o.step.Text, o.shot)
		}
	}
	b.WriteString("\nVideo: [video.webm](video.webm)\n")
	return b.String()
}

func stepList(all []step) string {
	var b strings.Builder
	for _, st := range all {
		b.WriteString(describe(st) + "\n")
	}
	return b.String()
}

func dial(port int) (net.Conn, error) {
	return net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", port), 200*time.Millisecond)
}
