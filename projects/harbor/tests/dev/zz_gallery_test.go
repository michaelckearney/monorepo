// The gallery tests: one test per screen, named by what it shows, since
// harness test -e2e keeps a screenshot of the page each test and subtest
// ends on. This file is zz_ so it runs last: a few of these tests leave the
// shared app in a state later tests would trip over (a collapsed sidebar, a
// live view left open), and they create documents whose names nothing else
// uses.
package dev

import (
	"regexp"
	"testing"

	"github.com/michaelckearney/monorepo/framework/e2e"
	"github.com/mxschmitt/playwright-go"
)

// openSettings opens Settings and returns its dialog.
func openSettings(t *testing.T, page playwright.Page) playwright.Locator {
	t.Helper()
	click(t, role(page, "button", "Settings"))
	dialog := role(page, "dialog", "Settings")
	e2e.Expect(t, dialog).ToBeVisible()
	return dialog
}

// setting opens the named part of Settings.
func setting(t *testing.T, page playwright.Page, section string) playwright.Locator {
	t.Helper()
	click(t, role(page, "dialog", "Settings").GetByRole("button", playwright.LocatorGetByRoleOptions{Name: section}))
	pane := role(page, "region", section)
	e2e.Expect(t, pane).ToBeVisible()
	return pane
}

// openOpenAIForm opens OpenAI's settings form in the Providers list, from
// either state of it: Edit when it is the one in use, Set up when not.
func openOpenAIForm(t *testing.T, page playwright.Page) playwright.Locator {
	t.Helper()
	row := role(page, "region", "Providers").GetByRole("listitem").
		Filter(playwright.LocatorFilterOptions{Has: page.GetByText("OpenAI", playwright.PageGetByTextOptions{Exact: playwright.Bool(true)})})
	edit := row.GetByRole("button", playwright.LocatorGetByRoleOptions{Name: "Edit OpenAI"})
	n, err := edit.Count()
	e2e.Do(t, err)
	if n > 0 {
		e2e.Do(t, edit.Click())
	} else {
		click(t, row.GetByRole("button", playwright.LocatorGetByRoleOptions{Name: "Set up"}))
	}
	form := role(page, "form", "OpenAI")
	e2e.Expect(t, form).ToBeVisible()
	return form
}

// newDashboard opens the editor for a new dashboard, fills it in, and saves.
func newDashboard(t *testing.T, page playwright.Page, name, code string) {
	t.Helper()
	click(t, role(page, "button", "New dashboard"))
	e2e.Do(t, page.GetByLabel("Dashboard name").Fill(name))
	replaceCode(t, page, "Dashboard code", code)
	click(t, role(page, "button", "Save"))
}

// saveServiceCode saves a new service written out in full, from the editor.
func saveServiceCode(t *testing.T, page playwright.Page, source string) {
	t.Helper()
	newService(t, page)
	replaceCode(t, page, "Service code", source)
	click(t, role(page, "button", "Save"))
}

// sunnyCode is a dashboard of its own: no services, so it always shows.
const sunnyCode = `<script setup lang="ts">
import { ref } from "vue"
const n = ref<number>(0)
</script>

<template>
  <button class="sunny" @click="n++">Sunny {{ n + 1 }}</button>
</template>
`

// brokenCode compiles, and stops once the dashboard is running.
const brokenCode = `<script setup lang="ts">
throw new Error("the gallery dashboard has no data")
</script>

<template>
  <p>Gallery broken</p>
</template>
`

// The parts of Settings, each as it shows on opening: Providers with a
// provider in use, Appearance with the themes, Logins empty and then with a
// saved login, and Connections with its address and token.
func TestSettings(t *testing.T) {
	t.Run("providers", func(t *testing.T) {
		page := e2e.Open(t)
		openSettings(t, page)
		// Setup is what leaves a provider in use, and each run starts with
		// none: set one up when this run's app has not been set up by the
		// chat's test.
		row := role(page, "region", "Providers").GetByRole("listitem").
			Filter(playwright.LocatorFilterOptions{Has: page.GetByText("OpenAI", playwright.PageGetByTextOptions{Exact: playwright.Bool(true)})})
		setUp := row.GetByRole("button", playwright.LocatorGetByRoleOptions{Name: "Set up"})
		n, err := setUp.Count()
		e2e.Do(t, err)
		if n > 0 {
			e2e.Do(t, setUp.Click())
			form := role(page, "form", "OpenAI")
			e2e.Do(t, form.GetByLabel("API key").Fill("sk-test"))
			click(t, form.GetByRole("button", playwright.LocatorGetByRoleOptions{Name: "Save and use"}))
		}
		e2e.Expect(t, row.GetByText("In use")).ToBeVisible()
	})
	t.Run("appearance", func(t *testing.T) {
		page := e2e.Open(t)
		openSettings(t, page)
		setting(t, page, "Appearance")
		e2e.Expect(t, role(page, "button", "Add a theme")).ToBeVisible()
		e2e.Expect(t, page.GetByRole("radiogroup", playwright.PageGetByRoleOptions{Name: "Mode"})).ToBeVisible()
	})
	t.Run("logins", func(t *testing.T) {
		page := e2e.Open(t)
		openSettings(t, page)
		pane := setting(t, page, "Logins & passkeys")
		e2e.Expect(t, pane.GetByText("Stored unencrypted")).ToBeVisible()
		e2e.Expect(t, pane.GetByRole("region", playwright.LocatorGetByRoleOptions{Name: "Saved logins"})).ToHaveCount(0)
		e2e.Expect(t, pane.GetByRole("heading", playwright.LocatorGetByRoleOptions{Name: "No logins yet", Exact: playwright.Bool(true)})).ToBeVisible()
		// The pane's own button, not the header's `+`, which shares the name.
		click(t, pane.GetByRole("button", playwright.LocatorGetByRoleOptions{Name: "Add a login", Exact: playwright.Bool(true)}).
			Filter(playwright.LocatorFilterOptions{HasText: "Add a login"}))
		e2e.Expect(t, role(page, "form", "Save a login")).ToBeVisible()
		// Back to the pane, so this test's gallery shot is the empty state.
		click(t, role(page, "button", "Back"))
		e2e.Expect(t, pane.GetByRole("heading", playwright.LocatorGetByRoleOptions{Name: "No logins yet", Exact: playwright.Bool(true)})).ToBeVisible()
	})
	t.Run("loginsFull", func(t *testing.T) {
		page := e2e.Open(t)
		openSettings(t, page)
		pane := setting(t, page, "Logins & passkeys")
		click(t, pane.Locator("header").GetByRole("button", playwright.LocatorGetByRoleOptions{Name: "Add a login", Exact: playwright.Bool(true)}))
		form := role(page, "form", "Save a login")
		e2e.Do(t, form.GetByLabel("Site").Fill("https://gallery.example.com"))
		e2e.Do(t, form.GetByLabel("Username").Fill("alex"))
		e2e.Do(t, form.GetByLabel("Password").Fill("letmein"))
		e2e.Do(t, form.GetByLabel("Authenticator secret").Fill("JBSWY3DPEHPK3PXP"))
		click(t, form.GetByRole("button", playwright.LocatorGetByRoleOptions{Name: "Save"}))
		list := role(page, "region", "Saved logins")
		e2e.Expect(t, list).ToBeVisible()
		e2e.Expect(t, list).ToContainText("gallery.example.com")
		e2e.Expect(t, list).ToContainText("alex")
		e2e.Expect(t, list.GetByLabel("With authenticator")).ToBeVisible()
	})
	t.Run("connections", func(t *testing.T) {
		page := e2e.Open(t)
		openSettings(t, page)
		pane := setting(t, page, "Connections")
		e2e.Expect(t, pane.GetByText("claude mcp add --transport http harbor")).ToBeVisible()
	})
	t.Run("providerForm", func(t *testing.T) {
		page := e2e.Open(t)
		openSettings(t, page)
		form := openOpenAIForm(t, page)
		e2e.Expect(t, form.GetByLabel("Model")).ToHaveAttribute("placeholder", "gpt-5")
	})
	t.Run("loginForm", func(t *testing.T) {
		page := e2e.Open(t)
		openSettings(t, page)
		setting(t, page, "Logins & passkeys")
		click(t, role(page, "button", "Add a login"))
		e2e.Expect(t, role(page, "form", "Save a login")).ToBeVisible()
	})
	t.Run("themeEditor", func(t *testing.T) {
		page := e2e.Open(t)
		openSettings(t, page)
		setting(t, page, "Appearance")
		click(t, role(page, "button", "Add a theme"))
		e2e.Expect(t, page.GetByLabel("Theme name")).ToBeVisible()
	})
}

// A dashboard that shows, and one that stops with an error. The one that
// asks for sign-ins is TestAWidgetOfSeveralServicesAsksForEachSignIn.
func TestDashboard(t *testing.T) {
	t.Run("full", func(t *testing.T) {
		page := e2e.Open(t)
		newDashboard(t, page, "Gallery sunny", sunnyCode)
		e2e.Expect(t, heading(page, "Gallery sunny")).ToBeVisible()
		e2e.Expect(t, role(page, "region", "Dashboard").
			GetByRole("button", playwright.LocatorGetByRoleOptions{Name: "Sunny 1"})).ToBeVisible()
	})
	t.Run("error", func(t *testing.T) {
		page := e2e.Open(t)
		newDashboard(t, page, "Gallery broken", brokenCode)
		e2e.Expect(t, heading(page, "Gallery broken")).ToBeVisible()
		e2e.Expect(t, page.GetByRole("alert")).ToContainText("The dashboard stopped with an error")
		e2e.Expect(t, role(page, "button", "Ask the agent to fix it")).ToBeVisible()
	})
}

// The document editor: a new service and dashboard, and each with the
// problems that stop a save.
func TestEditor(t *testing.T) {
	t.Run("newService", func(t *testing.T) {
		page := e2e.Open(t)
		newService(t, page)
		e2e.Expect(t, heading(page, "New service")).ToBeVisible()
		e2e.Expect(t, page.GetByLabel("Service code")).ToContainText("defineService")
	})
	t.Run("serviceProblem", func(t *testing.T) {
		page := e2e.Open(t)
		newService(t, page)
		broken := regexp.MustCompile(`  url: .*\n`).ReplaceAllString(serviceFile("gallery-broken", "Gallery broken"), "")
		replaceCode(t, page, "Service code", broken)
		click(t, role(page, "button", "Save"))
		e2e.Expect(t, page.GetByRole("alert")).ToHaveText("url: is required")
	})
	t.Run("newDashboard", func(t *testing.T) {
		page := e2e.Open(t)
		click(t, role(page, "button", "New dashboard"))
		e2e.Expect(t, heading(page, "New dashboard")).ToBeVisible()
		e2e.Expect(t, page.GetByLabel("Dashboard name")).ToBeVisible()
	})
	t.Run("dashboardProblem", func(t *testing.T) {
		page := e2e.Open(t)
		click(t, role(page, "button", "New dashboard"))
		e2e.Do(t, page.GetByLabel("Dashboard name").Fill("Gallery line"))
		replaceCode(t, page, "Dashboard code", "<template>\n  <p>{{ sunny </p>\n</template>\n")
		click(t, role(page, "button", "Save"))
		e2e.Expect(t, page.GetByRole("alert")).ToContainText("line 2:")
	})
	t.Run("editService", func(t *testing.T) {
		page := e2e.Open(t)
		click(t, serviceItem(t, page, "Tidewater Bank"))
		click(t, role(page, "button", "Edit"))
		e2e.Expect(t, heading(page, "Edit service")).ToBeVisible()
		e2e.Expect(t, page.GetByLabel("Service code")).ToContainText("tidewater")
	})
	t.Run("editDashboard", func(t *testing.T) {
		page := e2e.Open(t)
		newDashboard(t, page, "Gallery edit", sunnyCode)
		click(t, role(page, "button", "Edit"))
		e2e.Expect(t, heading(page, "Edit dashboard")).ToBeVisible()
		e2e.Expect(t, page.GetByLabel("Dashboard name")).ToHaveValue("Gallery edit")
	})
}

// A new chat in the agent's panel, beside the list of services. A chat with
// messages is TestStartsAChatAndSetsUpAProvider's view of the window.
func TestChat(t *testing.T) {
	t.Run("new", func(t *testing.T) {
		page := e2e.Open(t)
		click(t, role(page, "button", "Services"))
		click(t, role(page, "button", "New chat"))
		agent := role(page, "complementary", "Agent panel")
		e2e.Expect(t, agent.GetByRole("heading", playwright.LocatorGetByRoleOptions{Name: "New chat"})).ToBeVisible()
		e2e.Expect(t, agent.GetByLabel("Message")).ToBeVisible()
	})
	t.Run("prefill", func(t *testing.T) {
		page := e2e.Open(t)
		newDashboard(t, page, "Gallery ask", brokenCode)
		click(t, role(page, "button", "Ask the agent to fix it"))
		agent := role(page, "complementary", "Agent panel")
		e2e.Expect(t, agent.GetByLabel("Message")).ToHaveValue(regexp.MustCompile(`(?s)^Fix this dashboard\.`))
	})
}

// A service page with nothing to run, and one whose sign-in cannot be
// checked. The rest are TestAServicePageListsItsTools and the sign-in tests
// in tools_test.go.
func TestService(t *testing.T) {
	t.Run("noTools", func(t *testing.T) {
		page := e2e.Open(t)
		saveServiceCode(t, page, `export default {
  name: "gallery-quiet",
  title: "Gallery quiet",
  url: "https://quiet.example.com",
  tools: {},
}
`)
		e2e.Expect(t, heading(page, "Gallery quiet")).ToBeVisible()
		e2e.Expect(t, role(page, "region", "Tools")).ToContainText("This service has no tools yet.")
	})
	t.Run("signInFailed", func(t *testing.T) {
		page := e2e.Open(t)
		saveServiceCode(t, page, `export default {
  name: "gallery-dead",
  title: "Gallery dead",
  url: "http://127.0.0.1:9",
  login: { url: "/login", check: async () => { await fetch("http://127.0.0.1:9/health"); return false } },
  tools: {},
}
`)
		e2e.Expect(t, heading(page, "Gallery dead")).ToBeVisible()
		e2e.Expect(t, role(page, "status", "Sign-in").
			GetByRole("button", playwright.LocatorGetByRoleOptions{Name: "Check again"})).ToBeVisible()
	})
}

// The new run's form, filled in, with the scenarios to choose from. The
// empty page and its error are TestABenchmarkRunNeedsAModel.
func TestBenchmarks(t *testing.T) {
	t.Run("newRun", func(t *testing.T) {
		page := e2e.Open(t)
		click(t, role(page, "button", "Benchmarks"))
		click(t, role(page, "button", "New run"))
		form := role(page, "form", "New run")
		e2e.Expect(t, form.GetByRole("checkbox", playwright.LocatorGetByRoleOptions{Name: "tasklane-open-issues"})).ToBeChecked()
		e2e.Do(t, form.GetByLabel("Model 1").Fill("openai/gpt-5"))
		e2e.Do(t, form.GetByLabel("Grader").Fill("openai/gpt-5"))
		e2e.Expect(t, form.GetByLabel("Model 1")).ToHaveValue("openai/gpt-5")
	})
}

// The kit's Components page: the Widget frame, and a primitive.
func TestComponents(t *testing.T) {
	t.Run("widget", func(t *testing.T) {
		page := e2e.Open(t)
		click(t, role(page, "button", "Components"))
		e2e.Expect(t, heading(page, "Widget")).ToBeVisible()
		e2e.Expect(t, page.GetByRole("main", playwright.PageGetByRoleOptions{Name: "Widget"})).ToBeVisible()
	})
	t.Run("primitive", func(t *testing.T) {
		page := e2e.Open(t)
		click(t, role(page, "button", "Components"))
		click(t, role(page, "navigation", "Components").GetByRole("button", playwright.LocatorGetByRoleOptions{Name: "Avatar"}))
		e2e.Expect(t, heading(page, "Avatar")).ToBeVisible()
	})
}

// The sidebar collapsed to its strip, beside a dashboard.
func TestSidebar(t *testing.T) {
	t.Run("collapsed", func(t *testing.T) {
		page := e2e.Open(t)
		newDashboard(t, page, "Gallery strip", sunnyCode)
		e2e.Do(t, page.GetByRole("separator", playwright.PageGetByRoleOptions{Name: "Resize the sidebar"}).Press("Enter"))
		e2e.Expect(t, role(page, "button", "Show the sidebar")).ToBeVisible()
	})
}

// The live view of a sign-in: the page it shows you, and the address bar to
// go where you type. Last of all, because it leaves the view open over
// whatever page comes next.
func TestSignIn(t *testing.T) {
	t.Run("view", func(t *testing.T) {
		srv := shop(t)
		page := e2e.Open(t)
		show := role(page, "button", "Show the sidebar")
		n, err := show.Count()
		e2e.Do(t, err)
		if n > 0 {
			e2e.Do(t, show.Click())
		}
		saveService(t, page, "gallery-view", "Gallery view", srv.URL,
			`login: { url: "/login", check: async ({ cookies }) => cookies.some((c) => c.name === "gallery-view-session") },`)
		click(t, role(page, "status", "Sign-in").GetByRole("button", playwright.LocatorGetByRoleOptions{Name: "Sign in"}))
		view := page.GetByRole("dialog", playwright.PageGetByRoleOptions{Name: "Sign in to Gallery view"})
		e2e.Expect(t, view).ToBeVisible()
		canvas, err := view.GetByLabel("The page", playwright.LocatorGetByLabelOptions{Exact: playwright.Bool(true)}).ElementHandle()
		e2e.Do(t, err)
		_, err = page.WaitForFunction(`(c) => c.getContext("2d").getImageData(0, 0, c.width, c.height).data.some((v, i) => i % 4 === 3 && v > 0)`, canvas)
		e2e.Do(t, err)
		e2e.Expect(t, view.GetByLabel("Address", playwright.LocatorGetByLabelOptions{Exact: playwright.Bool(true)})).ToHaveValue(srv.URL + "/login")
	})
}
