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
