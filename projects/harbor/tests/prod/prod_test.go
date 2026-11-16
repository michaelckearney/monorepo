// Package prod drives the app built in prod mode, the one users get, from
// WebKit. It starts from the shared migrations alone: no fixtures.
package prod

import (
	"testing"

	"github.com/michaelckearney/monorepo/framework/e2e"
	"github.com/mxschmitt/playwright-go"
)

// A prod build has no developer bar and none of the dev fixtures.
func TestProdHasNoDeveloperBarOrFixtures(t *testing.T) {
	page := e2e.Open(t)
	e2e.Do(t, page.GetByRole("button", playwright.PageGetByRoleOptions{Name: "Services", Exact: playwright.Bool(true)}).Click())
	e2e.Expect(t, page.GetByRole("heading", playwright.PageGetByRoleOptions{Name: "No services yet", Exact: playwright.Bool(true)})).ToBeVisible()
	e2e.Expect(t, page.GetByRole("navigation", playwright.PageGetByRoleOptions{Name: "Developer"})).ToHaveCount(0)
	e2e.Expect(t, page.GetByRole("button", playwright.PageGetByRoleOptions{Name: "Tools", Exact: playwright.Bool(true)})).ToHaveCount(0)
}
