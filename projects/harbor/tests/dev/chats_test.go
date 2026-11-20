package dev

import (
	"testing"

	"github.com/michaelckearney/monorepo/framework/e2e"
	"github.com/mxschmitt/playwright-go"
)

var exact = playwright.PageGetByLabelOptions{Exact: playwright.Bool(true)}

// A real model is not part of the e2e tests: the chat's own tests run turns
// with a scripted model, and the OpenAI provider's run against a fake API.
// This checks the chat in the app, up to the model, and setting up a
// provider.
func TestStartsAChatAndSetsUpAProvider(t *testing.T) {
	page := e2e.Open(t)
	// The agent is beside the dashboard the window opens to.
	agent := role(page, "complementary", "Agent panel")
	click(t, role(page, "button", "New chat"))
	e2e.Expect(t, agent.GetByRole("heading", playwright.LocatorGetByRoleOptions{Name: "New chat"})).ToBeVisible()
	e2e.Expect(t, agent.GetByRole("status")).ToContainText("Harbor's agent has no provider yet.")

	e2e.Do(t, agent.GetByLabel("Message").Fill("Which services do I have?"))
	e2e.Do(t, agent.GetByLabel("Message").Press("Enter"))

	e2e.Expect(t, agent.GetByLabel("You", playwright.LocatorGetByLabelOptions{Exact: playwright.Bool(true)})).ToHaveText("Which services do I have?")
	e2e.Expect(t, agent.GetByRole("heading", playwright.LocatorGetByRoleOptions{Name: "Which services do I have?"})).ToBeVisible()
	e2e.Expect(t, sidebarItem(page, "Chats", "Which services do I have?")).ToBeVisible()
	// The answer under the message says it once, in the needs-you tone, the
	// button right there instead of instructions, and the footer steps aside.
	e2e.Expect(t, agent.GetByRole("alert")).ToHaveText("Harbor's agent has no provider yet.")
	e2e.Expect(t, agent.Locator("[role=alert].need")).ToBeVisible()
	e2e.Expect(t, agent.Locator("[role=alert].text-destructive")).ToHaveCount(0)
	e2e.Expect(t, agent.GetByRole("status")).ToHaveCount(0)

	click(t, role(page, "button", "Set up a provider"))
	e2e.Expect(t, role(page, "dialog", "Settings")).ToBeVisible()
	row := role(page, "region", "Providers").GetByRole("listitem").Filter(playwright.LocatorFilterOptions{Has: page.GetByText("OpenAI", playwright.PageGetByTextOptions{Exact: playwright.Bool(true)})})
	click(t, row.GetByRole("button", playwright.LocatorGetByRoleOptions{Name: "Set up"}))
	openai := role(page, "form", "OpenAI")
	e2e.Expect(t, openai.GetByLabel("Model")).ToHaveAttribute("placeholder", "gpt-5")
	e2e.Do(t, openai.GetByLabel("API key").Fill("sk-test"))
	click(t, openai.GetByRole("button", playwright.LocatorGetByRoleOptions{Name: "Save and use"}))
	// Saving goes back to the list.
	e2e.Expect(t, row.GetByText("In use")).ToBeVisible()
	e2e.Do(t, page.Keyboard().Press("Escape"))
	// The chat's error from before stays, but it no longer says to set one up.
	e2e.Expect(t, agent.GetByRole("status")).ToHaveCount(0)

	click(t, sidebarItem(page, "Chats", "Which services do I have?"))
	e2e.Expect(t, page.GetByLabel("You", exact)).ToHaveText("Which services do I have?")
	e2e.Expect(t, page.GetByText("Set up a provider")).ToHaveCount(0)

	// Settings says how to reach Harbor from other agents.
	click(t, role(page, "button", "Settings"))
	click(t, role(page, "dialog", "Settings").GetByRole("button", playwright.LocatorGetByRoleOptions{Name: "Connections"}))
	e2e.Expect(t, role(page, "region", "Connections").GetByText("claude mcp add --transport http harbor")).ToBeVisible()
}
