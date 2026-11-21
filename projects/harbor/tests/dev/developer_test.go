package dev

import (
	"testing"

	"github.com/michaelckearney/monorepo/framework/e2e"
	"github.com/mxschmitt/playwright-go"
)

// The Tools page runs the agent's tools through Harbor's MCP server, and
// shows the result as the agent gets it.
func TestRunsOneOfTheAgentsTools(t *testing.T) {
	page := e2e.Open(t)
	click(t, role(page, "button", "Tools"))
	e2e.Expect(t, heading(page, "Tools")).ToBeVisible()

	rows := page.GetByRole("main").GetByRole("listitem")
	e2e.Expect(t, rows.Filter(playwright.LocatorFilterOptions{HasText: "delete_service"})).ToContainText("destructive")
	e2e.Expect(t, rows.Filter(playwright.LocatorFilterOptions{HasText: "list_dashboards"})).ToContainText("read-only")

	// A tool with an input has no Run in its row: it runs from beside its box.
	e2e.Expect(t, role(page, "button", "Run delete_dashboard")).ToHaveCount(0)
	form := role(page, "form", "Run delete_dashboard")
	e2e.Expect(t, form.GetByText("The agent asks you before this call.")).ToBeVisible()

	form = role(page, "form", "Run read_dashboard")
	e2e.Expect(t, form.GetByText("The agent makes this call without asking.")).ToBeVisible()
	e2e.Do(t, form.GetByLabel("Input for read_dashboard").Fill(`{"name": "Nope"}`))
	click(t, form.GetByRole("button"))
	e2e.Expect(t, form.GetByLabel("Result of read_dashboard")).ToHaveText("{\n  \"error\": \"there is no dashboard named Nope\"\n}")
	e2e.Expect(t, form.GetByText("Took ")).ToBeVisible()

	// A tool with no input keeps the row's Run, and that is its only Run.
	e2e.Expect(t, role(page, "form", "Run list_services").GetByRole("button")).ToHaveCount(0)
	click(t, role(page, "button", "Run list_services"))
	result := role(page, "form", "Run list_services")
	e2e.Expect(t, result.GetByLabel("Result of list_services")).ToContainText("tidewater")
	e2e.Expect(t, result.GetByText("Took ")).ToBeVisible()
}

// The Benchmarks page lists the scenarios for a new run, and a run needs a
// model to benchmark.
func TestABenchmarkRunNeedsAModel(t *testing.T) {
	page := e2e.Open(t)
	click(t, role(page, "button", "Benchmarks"))
	e2e.Expect(t, heading(page, "Benchmarks")).ToBeVisible()
	e2e.Expect(t, page.GetByText("No runs yet.")).ToBeVisible()

	form := role(page, "form", "New run")
	e2e.Expect(t, form.GetByRole("checkbox", playwright.LocatorGetByRoleOptions{Name: "tasklane-open-issues"})).ToBeChecked()
	e2e.Do(t, form.GetByLabel("Model 1").Fill(""))
	click(t, form.GetByRole("button", playwright.LocatorGetByRoleOptions{Name: "Start the run"}))
	e2e.Expect(t, form.GetByRole("alert")).ToHaveText("add a model to benchmark")
	e2e.Expect(t, page.GetByText("No runs yet.")).ToBeVisible()
}
