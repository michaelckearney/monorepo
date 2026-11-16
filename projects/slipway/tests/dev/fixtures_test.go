// Package dev drives the app built in dev mode from WebKit. Its server and
// GitHub are the fixtures (backend/fixture), which the dev database starts
// pointed at (database/dev/). The tests share one running app, so each
// writes what is its own.
package dev

import (
	"regexp"
	"testing"
	"time"

	"github.com/michaelckearney/monorepo/framework/e2e"
	"github.com/mxschmitt/playwright-go"
)

func button(page playwright.Page, name string) playwright.Locator {
	return page.GetByRole("button", playwright.PageGetByRoleOptions{Name: name})
}

func TestTheSidebarShowsTheFixtures(t *testing.T) {
	page := e2e.Open(t)
	for _, name := range []string{"harbor", "slipway", "framework", "Harbor's README still describes the old layout", "Indent the New issue buttons"} {
		e2e.Expect(t, button(page, name).First()).ToBeVisible()
	}
	e2e.Expect(t, button(page, "Settings")).ToBeVisible()
}

func TestTheOutputWaitsUntilYouOpenIt(t *testing.T) {
	page := e2e.Open(t)
	e2e.Expect(t, button(page, "Show the app's output")).ToBeVisible()
	e2e.Expect(t, button(page, "Run on this computer")).ToBeHidden()
}

func TestTheHeadingShowsTheWholeTitleOnHover(t *testing.T) {
	page := e2e.Open(t)
	e2e.Do(t, button(page, "Harbor's README still describes the old layout").Click())
	heading := page.GetByRole("heading", playwright.PageGetByRoleOptions{Name: "Harbor's README still describes the old layout"})
	e2e.Expect(t, heading).ToHaveAttribute("title", "#11 Harbor's README still describes the old layout")
}

func TestAReplyReachesTheIssueAndStartsATurn(t *testing.T) {
	page := e2e.Open(t)
	e2e.Do(t, button(page, "Harbor's README still describes the old layout").Click())
	reply := "Thanks, merging it at " + time.Now().Format(time.RFC3339Nano)
	e2e.Do(t, page.GetByRole("textbox", playwright.PageGetByRoleOptions{Name: "Reply on #11"}).Fill(reply))
	e2e.Do(t, button(page, "Comment").Click())
	e2e.Expect(t, page.GetByText(reply)).ToBeVisible()
	e2e.Expect(t, page.GetByText("Nothing has changed since my last turn").First()).ToBeVisible()
}

func TestANewIssueOpens(t *testing.T) {
	page := e2e.Open(t)
	e2e.Do(t, button(page, "New issue").First().Click())
	title := "Add a log view " + time.Now().Format("150405.000")
	e2e.Do(t, page.GetByRole("textbox", playwright.PageGetByRoleOptions{Name: "Title"}).Fill(title))
	e2e.Do(t, button(page, "Open the issue").Click())
	e2e.Expect(t, page.GetByRole("heading", playwright.PageGetByRoleOptions{Name: title})).ToBeVisible()
}

func TestAnAttachedImageShows(t *testing.T) {
	page := e2e.Open(t)
	e2e.Do(t, button(page, "Indent the New issue buttons").Click())
	image := page.GetByRole("img", playwright.PageGetByRoleOptions{Name: "The sidebar, each New issue at its project's indent"})
	e2e.Expect(t, image).ToHaveAttribute("src", regexp.MustCompile(`^data:image/png;base64,`))
}

func TestSettingsTakeAToken(t *testing.T) {
	page := e2e.Open(t)
	e2e.Do(t, button(page, "Settings").Click())
	e2e.Do(t, button(page, "GitHub").Click())
	field := page.GetByRole("textbox", playwright.PageGetByRoleOptions{Name: "GitHub token"})
	e2e.Expect(t, field).ToHaveAttribute("placeholder", "Signed in as alex-morgan")
	e2e.Expect(t, field).ToHaveValue("")
}

// exactButton finds a button by its whole accessible name, so a name that is
// part of another ("harbor", "main") cannot match the wrong one.
func exactButton(page playwright.Page, name string) playwright.Locator {
	return page.GetByRole("button", playwright.PageGetByRoleOptions{Name: name, Exact: playwright.Bool(true)})
}

// pick selects an item of the made-up monorepo from the sidebar by its title.
func pick(t *testing.T, page playwright.Page, title string) {
	t.Helper()
	e2e.Do(t, button(page, title).Click())
}

// Text shown at the end of each subtest below is a screenshot's caption, so
// these tests reach every screen and state of the app for the gallery. They
// share the one running app with the tests above; the only setting any of
// them changes is the server, and TestServer puts it back.

// Each state of an item's timeline and its status line.
func TestItemView(t *testing.T) {
	t.Run("pullRequestChecksPassed", func(t *testing.T) {
		page := e2e.Open(t)
		pick(t, page, "Harbor's README still describes the old layout")
		e2e.Expect(t, page.GetByText("Checks passed")).ToBeVisible()
		// The checks popover lists each check; it is a non-modal overlay the
		// screenshot does not keep open, so the picture ends on the item.
		e2e.Do(t, button(page, "Checks passed").Click())
		e2e.Expect(t, page.GetByText("check / test")).ToBeVisible()
	})
	t.Run("waitingForNextTurn", func(t *testing.T) {
		page := e2e.Open(t)
		pick(t, page, "Jump the timeline to its top")
		e2e.Expect(t, page.GetByText("3 new events for its next turn")).ToBeVisible()
	})
	t.Run("botWorking", func(t *testing.T) {
		page := e2e.Open(t)
		pick(t, page, "Indent the New issue buttons")
		e2e.Expect(t, page.GetByText("Bot working")).ToBeVisible()
	})
	t.Run("turnPromptExpanded", func(t *testing.T) {
		page := e2e.Open(t)
		pick(t, page, "Indent the New issue buttons")
		e2e.Do(t, button(page, "prompt").First().Click())
		e2e.Expect(t, page.GetByText("Start with `harness status`")).ToBeVisible()
	})
	t.Run("waitingForYou", func(t *testing.T) {
		page := e2e.Open(t)
		pick(t, page, "Add a dark theme to Harbor")
		e2e.Expect(t, page.GetByText("Waiting for you").First()).ToBeVisible()
	})
	t.Run("noThread", func(t *testing.T) {
		page := e2e.Open(t)
		pick(t, page, "Cache the framework's e2e builds")
		e2e.Expect(t, page.GetByText("No bot thread yet")).ToBeVisible()
	})
	t.Run("checksFailed", func(t *testing.T) {
		page := e2e.Open(t)
		pick(t, page, "chore: try the harness's next release")
		e2e.Expect(t, page.GetByText("Checks failed")).ToBeVisible()
	})
	t.Run("botLog", func(t *testing.T) {
		page := e2e.Open(t)
		pick(t, page, "Harbor's README still describes the old layout")
		e2e.Do(t, button(page, "The bot's whole log").Click())
		e2e.Expect(t, page.GetByText("The bot's log of #11")).ToBeVisible()
	})
	t.Run("defaultBranch", func(t *testing.T) {
		page := e2e.Open(t)
		e2e.Do(t, exactButton(page, "main").First().Click())
		e2e.Expect(t, page.GetByText("as it is on GitHub.")).ToBeVisible()
	})
}

// The app output panel, opened from its strip, and its branch picker. The
// panel's collapsed state is kept in the shared database, so once a test opens
// it the next start with it open; openOutput opens it only when it is closed.
func openOutput(t *testing.T, page playwright.Page) {
	t.Helper()
	strip := button(page, "Show the app's output")
	if n, err := strip.Count(); err == nil && n > 0 {
		e2e.Do(t, strip.Click())
	}
}

func TestOutputPanel(t *testing.T) {
	t.Run("nothingRuns", func(t *testing.T) {
		page := e2e.Open(t)
		openOutput(t, page)
		e2e.Expect(t, page.GetByText("Nothing runs here yet. Pick a branch above to run it.")).ToBeVisible()
		// The picker lists each project's default branch and its pull
		// requests; its menu is a non-modal overlay the screenshot does not
		// keep open, so the picture ends on the opened panel.
		e2e.Do(t, button(page, "Run on this computer").Click())
		e2e.Expect(t, page.GetByText("#18").First()).ToBeVisible()
	})
}

// The parts of Settings, beside GitHub which TestSettingsTakeAToken ends on.
func TestSettings(t *testing.T) {
	t.Run("server", func(t *testing.T) {
		page := e2e.Open(t)
		e2e.Do(t, exactButton(page, "Settings").Click())
		e2e.Expect(t, page.GetByText("Connected · harness fixture")).ToBeVisible()
	})
	t.Run("appearance", func(t *testing.T) {
		page := e2e.Open(t)
		e2e.Do(t, exactButton(page, "Settings").Click())
		e2e.Do(t, button(page, "Appearance").Click())
		e2e.Expect(t, exactButton(page, "Add a theme")).ToBeVisible()
	})
}

// The window when the server cannot be reached: its projects do not load, so
// the main shows the error and offers the server settings, and the sidebar
// has none. It points the app at a port nothing answers, and ends there so the
// gallery shows that state; a second page then puts the fixture's address back
// and proves the app recovers, as the tests share one app. Its picture is
// overwritten by the first page's, which is the one worth keeping.
func TestServer(t *testing.T) {
	const down = "http://127.0.0.1:1"
	const up = "http://server.fixture.localhost"
	setServer := func(page playwright.Page, url string) error {
		if err := exactButton(page, "Settings").Click(); err != nil {
			return err
		}
		field := page.GetByRole("textbox", playwright.PageGetByRoleOptions{Name: "Address", Exact: playwright.Bool(true)})
		if err := field.Fill(url); err != nil {
			return err
		}
		if err := button(page, "Save").First().Click(); err != nil {
			return err
		}
		return button(page, "Close").Click()
	}
	t.Run("unreachable", func(t *testing.T) {
		page := e2e.Open(t)
		e2e.Do(t, setServer(page, down))
		e2e.Expect(t, page.GetByText("No projects yet.")).ToBeVisible()
		e2e.Expect(t, page.GetByText("cannot reach the server")).ToBeVisible()
		e2e.Expect(t, button(page, "Server settings")).ToBeVisible()
		recovered := e2e.Open(t)
		e2e.Do(t, setServer(recovered, up))
		e2e.Expect(t, button(recovered, "harbor").First()).ToBeVisible()
	})
}
