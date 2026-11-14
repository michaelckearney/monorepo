// Package dev drives the app built in dev mode from WebKit. Each e2e run
// starts with a new database: the migrations, with the dev fixtures
// (database/dev/), which hold a service for each site in backend/fixture and
// six dashboards. The tests share one running app, so each makes documents
// with names of its own.
package dev

import (
	"regexp"
	"strings"
	"testing"

	"github.com/michaelckearney/monorepo/framework/e2e"
	"github.com/mxschmitt/playwright-go"
)

func role(page playwright.Page, role, name string) playwright.Locator {
	return page.GetByRole(playwright.AriaRole(role), playwright.PageGetByRoleOptions{Name: name, Exact: playwright.Bool(true)})
}

func sidebar(page playwright.Page, section string) playwright.Locator {
	return role(page, "region", section).GetByRole("listitem")
}

func sidebarItem(page playwright.Page, section, name string) playwright.Locator {
	return role(page, "region", section).GetByRole("button", playwright.LocatorGetByRoleOptions{Name: name, Exact: playwright.Bool(true)})
}

// serviceItem opens the Services page, from the sidebar, and finds the service
// titled name there.
func serviceItem(t *testing.T, page playwright.Page, name string) playwright.Locator {
	t.Helper()
	click(t, role(page, "button", "Services"))
	return sidebarItem(page, "Services", name)
}

// newService opens the editor for a new service, from the Services page.
func newService(t *testing.T, page playwright.Page) {
	t.Helper()
	click(t, role(page, "button", "Services"))
	click(t, role(page, "button", "New service"))
}

func heading(page playwright.Page, name string) playwright.Locator {
	return page.GetByRole("heading", playwright.PageGetByRoleOptions{Name: name, Level: playwright.Int(1), Exact: playwright.Bool(true)})
}

func click(t *testing.T, l playwright.Locator) {
	t.Helper()
	e2e.Do(t, l.Click())
}

// replaceCode replaces everything in the code editor. InsertText types the
// text as one edit, so the editor's auto-closing brackets and indenting
// leave it alone.
func replaceCode(t *testing.T, page playwright.Page, label, text string) {
	t.Helper()
	click(t, page.GetByLabel(label))
	e2e.Do(t, page.Keyboard().Press("ControlOrMeta+a"))
	e2e.Do(t, page.Keyboard().InsertText(text))
}

func serviceFile(name, title string) string {
	return strings.NewReplacer("NAME", name, "TITLE", title).Replace(`import { defineService } from "harbor"

export default defineService({
  name: "NAME",
  title: "TITLE",
  url: "https://NAME.example.com",
  tools: {
    list_things: {
      description: "List the things.",
      annotations: { readOnlyHint: true },
      async run() { return { things: [] } },
    },
    delete_thing: {
      description: "Delete a thing.",
      async run() { return {} },
    },
  },
})
`)
}

func TestListsTheSeededServicesAndDashboards(t *testing.T) {
	page := e2e.Open(t)
	// A dashboard may say a service it uses needs you.
	var dashboards []*regexp.Regexp
	for _, name := range []string{"Family", "Home", "Issue board", "Money", "This week", "Today at work"} {
		dashboards = append(dashboards, regexp.MustCompile("^"+name+`(\(|$)`))
	}
	e2e.Expect(t, sidebar(page, "Dashboards")).ToHaveText(dashboards)
	// The window opens to the first dashboard.
	e2e.Expect(t, heading(page, "Family")).ToBeVisible()
	click(t, role(page, "button", "Services"))
	var titles []*regexp.Regexp
	for _, title := range []string{
		"Brightline Energy", "Clearwater Health", "Homebase", "Homebase router", "Huddle", "Maple Grove Schools",
		"Parcel", "Payday", "Shoebox", "Tasklane", "Tidewater Bank",
	} {
		titles = append(titles, regexp.MustCompile("^"+regexp.QuoteMeta(title)+`\s*https?://`))
	}
	e2e.Expect(t, sidebar(page, "Services")).ToHaveText(titles)
}

// A widget of several services, as This week's agenda is, asks in itself for
// each sign-in it needs, in one sentence that names each service.
func TestAWidgetOfSeveralServicesAsksForEachSignIn(t *testing.T) {
	page := e2e.Open(t)
	// Its name in the sidebar may say a service it uses needs you.
	click(t, role(page, "region", "Dashboards").GetByRole("button", playwright.LocatorGetByRoleOptions{Name: regexp.MustCompile(`^This week`)}))
	week := role(page, "region", "The next seven days")
	e2e.Expect(t, week.GetByText("Sign in to Brightline Energy, Clearwater Health, Maple Grove Schools, and Payday to see this.")).ToBeVisible()
	for _, title := range []string{"Clearwater Health", "Maple Grove Schools", "Brightline Energy", "Payday"} {
		// Each name signs in; the service's icon above only names it.
		e2e.Expect(t, week.GetByRole("paragraph").GetByRole("button", playwright.LocatorGetByRoleOptions{Name: title, Exact: playwright.Bool(true)})).ToBeVisible()
		e2e.Expect(t, week.GetByRole("list", playwright.LocatorGetByRoleOptions{Name: "Where this is from"}).GetByRole("img", playwright.LocatorGetByRoleOptions{Name: title, Exact: playwright.Bool(true)})).ToBeVisible()
	}
	e2e.Expect(t, page.GetByText("Not shown:")).ToHaveCount(0)
}

func TestAServicePageListsItsTools(t *testing.T) {
	page := e2e.Open(t)
	click(t, serviceItem(t, page, "Tidewater Bank"))
	e2e.Expect(t, heading(page, "Tidewater Bank")).ToBeVisible()
	e2e.Expect(t, page.GetByText("Sign in at https://tidewater.localhost:8443/login")).ToBeVisible()
	tools := role(page, "region", "Tools").GetByRole("listitem")
	e2e.Expect(t, tools).ToHaveCount(5)
	e2e.Expect(t, tools.First()).ToContainText("list_accounts")
	e2e.Expect(t, tools.First()).ToContainText("read-only")
	e2e.Expect(t, tools.Last()).ToContainText("transfer")
	e2e.Expect(t, tools.Last()).ToContainText("destructive")
}

func TestMakesEditsAndDeletesAService(t *testing.T) {
	page := e2e.Open(t)
	newService(t, page)
	e2e.Expect(t, heading(page, "New service")).ToBeVisible()
	e2e.Expect(t, page.GetByLabel("Service code")).ToContainText("defineService")

	replaceCode(t, page, "Service code", serviceFile("acme", "Acme"))
	click(t, role(page, "button", "Save"))

	e2e.Expect(t, heading(page, "Acme")).ToBeVisible()
	tools := role(page, "region", "Tools").GetByRole("listitem")
	e2e.Expect(t, tools).ToHaveText([]*regexp.Regexp{
		regexp.MustCompile(`list_things\s*read-only`),
		regexp.MustCompile(`delete_thing\s*destructive`),
	})
	click(t, serviceItem(t, page, "Acme"))

	// Editing starts from the saved file; the list follows the new title.
	click(t, role(page, "button", "Edit"))
	e2e.Expect(t, page.GetByLabel("Service code")).ToContainText(`title: "Acme"`)
	replaceCode(t, page, "Service code", serviceFile("acme", "Acme Corp"))
	e2e.Do(t, page.Keyboard().Press("ControlOrMeta+s"))
	e2e.Expect(t, heading(page, "Acme Corp")).ToBeVisible()
	click(t, serviceItem(t, page, "Acme Corp"))

	click(t, role(page, "button", "Delete Acme Corp"))
	click(t, role(page, "button", "Yes, delete"))
	e2e.Expect(t, heading(page, "Services")).ToBeVisible()
	e2e.Expect(t, sidebarItem(page, "Services", "Acme Corp")).ToHaveCount(0)
}

func TestDoesNotSaveAServiceWithProblems(t *testing.T) {
	page := e2e.Open(t)
	newService(t, page)
	broken := regexp.MustCompile(`  url: .*\n`).ReplaceAllString(serviceFile("broken", "Broken"), "")
	replaceCode(t, page, "Service code", broken)
	click(t, role(page, "button", "Save"))

	e2e.Expect(t, page.GetByRole("alert")).ToHaveText("url: is required")
	e2e.Expect(t, heading(page, "New service")).ToBeVisible()

	replaceCode(t, page, "Service code", serviceFile("tidewater", "Another Tidewater"))
	click(t, role(page, "button", "Save"))
	e2e.Expect(t, page.GetByRole("alert")).ToHaveText("there is already a service named tidewater")

	click(t, role(page, "button", "Cancel"))
	e2e.Expect(t, serviceItem(t, page, "Broken")).ToHaveCount(0)
}

func TestMakesAndChecksADashboard(t *testing.T) {
	page := e2e.Open(t)
	click(t, role(page, "button", "New dashboard"))
	e2e.Do(t, page.GetByLabel("Dashboard name").Fill("Weather"))

	replaceCode(t, page, "Dashboard code", "<template>\n  <p>{{ sunny </p>\n</template>\n")
	click(t, role(page, "button", "Save"))
	e2e.Expect(t, page.GetByRole("alert")).ToContainText("line 2:")

	replaceCode(t, page, "Dashboard code", `<script setup lang="ts">
import { ref } from "vue"
const n = ref<number>(1)
</script>

<template>
  <button class="sunny" @click="n++">Sunny {{ n }}</button>
</template>

<style scoped>
.sunny { color: rgb(255, 0, 0); }
</style>
`)
	click(t, role(page, "button", "Save"))
	e2e.Expect(t, heading(page, "Weather")).ToBeVisible()
	e2e.Expect(t, sidebarItem(page, "Dashboards", "Weather")).ToBeVisible()

	// The dashboard runs, and its scoped style applies.
	shown := role(page, "region", "Dashboard").GetByRole("button")
	e2e.Expect(t, shown).ToHaveText("Sunny 1")
	click(t, shown)
	e2e.Expect(t, shown).ToHaveText("Sunny 2")
	color, err := shown.Evaluate("(el) => getComputedStyle(el).color", nil)
	e2e.Do(t, err)
	if color != "rgb(255, 0, 0)" {
		t.Errorf("the scoped style did not apply: color is %v", color)
	}
}
