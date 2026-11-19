// The gallery tests for the build users get: a prod window has no developer
// bar and no fixtures, so this is where its empty states show. zz_ so it
// runs last: the chats it creates cannot then disturb any other test.
package prod

import (
	"testing"

	"github.com/michaelckearney/monorepo/framework/e2e"
	"github.com/mxschmitt/playwright-go"
)

func role(t *testing.T, page playwright.Page, role, name string) playwright.Locator {
	t.Helper()
	return page.GetByRole(playwright.AriaRole(role), playwright.PageGetByRoleOptions{Name: name, Exact: playwright.Bool(true)})
}

func click(t *testing.T, l playwright.Locator) {
	t.Helper()
	e2e.Do(t, l.Click())
}

// What the window shows before you have a dashboard.
func TestWelcome(t *testing.T) {
	page := e2e.Open(t)
	e2e.Expect(t, page.GetByRole("heading", playwright.PageGetByRoleOptions{Name: "Harbor", Level: playwright.Int(1), Exact: playwright.Bool(true)})).ToBeVisible()
	e2e.Expect(t, page.GetByRole("heading", playwright.PageGetByRoleOptions{Name: "No dashboards yet", Exact: playwright.Bool(true)})).ToBeVisible()
}

// The list of services, with none.
func TestServices(t *testing.T) {
	t.Run("empty", func(t *testing.T) {
		page := e2e.Open(t)
		click(t, role(t, page, "button", "Services"))
		e2e.Expect(t, page.GetByRole("heading", playwright.PageGetByRoleOptions{Name: "No services yet", Exact: playwright.Bool(true)})).ToBeVisible()
	})
}

// The agent's panel: a new chat with nothing beside it, and a message that
// cannot be answered until a provider is set up.
func TestChat(t *testing.T) {
	t.Run("new", func(t *testing.T) {
		page := e2e.Open(t)
		click(t, role(t, page, "button", "Services"))
		click(t, role(t, page, "button", "New chat"))
		agent := role(t, page, "complementary", "Agent panel")
		e2e.Expect(t, agent.GetByRole("heading", playwright.LocatorGetByRoleOptions{Name: "New chat"})).ToBeVisible()
		e2e.Expect(t, role(t, page, "region", "Chats").GetByText("No chats yet.")).ToBeVisible()
	})
	t.Run("noProvider", func(t *testing.T) {
		page := e2e.Open(t)
		agent := role(t, page, "complementary", "Agent panel")
		e2e.Do(t, agent.GetByLabel("Message").Fill("Which services do I have?"))
		e2e.Do(t, agent.GetByLabel("Message").Press("Enter"))
		e2e.Expect(t, agent.GetByLabel("You", playwright.LocatorGetByLabelOptions{Exact: playwright.Bool(true)})).ToHaveText("Which services do I have?")
		// Said once, under the message, in the needs-you tone with its way to
		// Settings; the footer steps aside, and no red alarms for a setup that
		// waits on the person.
		e2e.Expect(t, agent.GetByRole("alert")).ToHaveText("Harbor's agent has no provider yet. Set one up in Settings, under Providers")
		e2e.Expect(t, agent.Locator("[role=alert].need")).ToBeVisible()
		e2e.Expect(t, agent.Locator("[role=alert].text-destructive")).ToHaveCount(0)
		e2e.Expect(t, agent.GetByRole("button", playwright.LocatorGetByRoleOptions{Name: "Set up a provider"})).ToBeVisible()
		e2e.Expect(t, agent.GetByRole("status")).ToHaveCount(0)
	})
}

// The parts of Settings, as they show when nothing is set up.
func TestSettings(t *testing.T) {
	t.Run("providers", func(t *testing.T) {
		page := e2e.Open(t)
		click(t, role(t, page, "button", "Settings"))
		pane := role(t, page, "region", "Providers")
		e2e.Expect(t, pane).ToBeVisible()
		e2e.Expect(t, pane.GetByText("Harbor's agent has no provider yet.")).ToBeVisible()
		e2e.Expect(t, pane.GetByRole("button", playwright.LocatorGetByRoleOptions{Name: "Set up"}).First()).ToBeVisible()
	})
	t.Run("appearance", func(t *testing.T) {
		page := e2e.Open(t)
		click(t, role(t, page, "button", "Settings"))
		click(t, role(t, page, "dialog", "Settings").GetByRole("button", playwright.LocatorGetByRoleOptions{Name: "Appearance"}))
		e2e.Expect(t, role(t, page, "region", "Appearance")).ToBeVisible()
		e2e.Expect(t, role(t, page, "button", "Add a theme")).ToBeVisible()
	})
	t.Run("logins", func(t *testing.T) {
		page := e2e.Open(t)
		click(t, role(t, page, "button", "Settings"))
		click(t, role(t, page, "dialog", "Settings").GetByRole("button", playwright.LocatorGetByRoleOptions{Name: "Logins & passkeys"}))
		pane := role(t, page, "region", "Logins & passkeys")
		e2e.Expect(t, pane).ToBeVisible()
		e2e.Expect(t, pane.GetByText("Stored unencrypted")).ToBeVisible()
	})
	t.Run("connections", func(t *testing.T) {
		page := e2e.Open(t)
		click(t, role(t, page, "button", "Settings"))
		click(t, role(t, page, "dialog", "Settings").GetByRole("button", playwright.LocatorGetByRoleOptions{Name: "Connections"}))
		e2e.Expect(t, role(t, page, "region", "Connections").GetByText("claude mcp add --transport http harbor")).ToBeVisible()
	})
}
