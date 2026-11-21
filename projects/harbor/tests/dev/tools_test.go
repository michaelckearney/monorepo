package dev

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/michaelckearney/monorepo/framework/e2e"
	"github.com/mxschmitt/playwright-go"
)

// shop is a website with one page of JSON, for the app's browser to call.
// /emailed is the link it emails you to sign in with.
func shop(t *testing.T) *httptest.Server {
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, "<title>Shop</title><h1>Shop</h1>") })
	mux.HandleFunc("/emailed", func(w http.ResponseWriter, r *http.Request) {
		http.SetCookie(w, &http.Cookie{Name: "session", Value: "yes", Path: "/", HttpOnly: true})
		fmt.Fprint(w, "<title>Shop</title><p>Signed in</p>")
	})
	mux.HandleFunc("/api/stock", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, `{"item":%q,"count":3}`, r.URL.Query().Get("item"))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

const shopService = `import { defineService } from "harbor"

export default defineService({
  name: "NAME",
  title: "TITLE",
  url: "URL",
  LOGIN
  tools: {
    stock: {
      description: "How many of an item are in stock.",
      annotations: { readOnlyHint: true },
      inputSchema: { type: "object", required: ["item"], properties: { item: { type: "string" } } },
      async run({ item }) {
        return await (await fetch("/api/stock?item=" + encodeURIComponent(item))).json()
      },
    },
  },
})
`

func saveService(t *testing.T, page playwright.Page, name, title, url, login string) {
	t.Helper()
	src := strings.NewReplacer("NAME", name, "TITLE", title, "URL", url, "LOGIN", login).Replace(shopService)
	newService(t, page)
	replaceCode(t, page, "Service code", src)
	click(t, role(page, "button", "Save"))
	e2e.Expect(t, heading(page, title)).ToBeVisible()
}

func TestRunsAToolInTheBrowser(t *testing.T) {
	srv := shop(t)
	page := e2e.Open(t)
	saveService(t, page, "shop", "Shop", srv.URL, "")

	// The tool takes an input, so it has no Run in its row: it runs from
	// beside its box, and that is its only Run.
	e2e.Expect(t, role(page, "button", "Run stock")).ToHaveCount(0)
	form := page.GetByRole("form", playwright.PageGetByRoleOptions{Name: "Run stock"})
	e2e.Expect(t, form.GetByRole("button", playwright.LocatorGetByRoleOptions{Name: "Run", Exact: playwright.Bool(true)})).ToHaveCount(1)
	e2e.Expect(t, form.GetByLabel("Input for stock")).ToHaveValue("{\n  \"item\": \"\"\n}")
	e2e.Do(t, form.GetByLabel("Input for stock").Fill(`{"item": "kettle"}`))
	e2e.Do(t, form.GetByRole("button", playwright.LocatorGetByRoleOptions{Name: "Run", Exact: playwright.Bool(true)}).Click())
	e2e.Expect(t, form.GetByLabel("Output of stock")).ToHaveText("{\n  \"item\": \"kettle\",\n  \"count\": 3\n}")

	// Input the schema forbids never reaches the site.
	e2e.Do(t, form.GetByLabel("Input for stock").Fill(`{}`))
	e2e.Do(t, form.GetByRole("button", playwright.LocatorGetByRoleOptions{Name: "Run", Exact: playwright.Bool(true)}).Click())
	e2e.Expect(t, form.GetByRole("alert")).ToContainText("does not match stock's inputSchema")
}

func TestASignedOutServiceSaysSo(t *testing.T) {
	srv := shop(t)
	page := e2e.Open(t)
	saveService(t, page, "locked-shop", "Locked shop", srv.URL,
		`login: { url: "/login", check: async ({ cookies }) => cookies.some((c) => c.name === "session") },`)

	e2e.Expect(t, role(page, "status", "Sign-in")).ToContainText("Signed out")
	e2e.Expect(t, role(page, "status", "Sign-in").GetByRole("button", playwright.LocatorGetByRoleOptions{Name: "Sign in"})).ToBeVisible()

	form := page.GetByRole("form", playwright.PageGetByRoleOptions{Name: "Run stock"})
	e2e.Do(t, form.GetByLabel("Input for stock").Fill(`{"item": "kettle"}`))
	e2e.Do(t, form.GetByRole("button", playwright.LocatorGetByRoleOptions{Name: "Run", Exact: playwright.Bool(true)}).Click())
	// Running a tool while signed out shows the login page; cancelling ends
	// the run.
	view := page.GetByRole("dialog", playwright.PageGetByRoleOptions{Name: "Sign in to Locked shop"})
	e2e.Do(t, view.GetByRole("button", playwright.LocatorGetByRoleOptions{Name: "Cancel"}).Click())
	e2e.Expect(t, view).Not().ToBeVisible()
	e2e.Expect(t, form.GetByRole("alert")).ToContainText("sign in to Locked shop first")
}

func TestSignsInThroughTheLiveViewPanel(t *testing.T) {
	srv := shop(t)
	page := e2e.Open(t)
	saveService(t, page, "emailed-shop", "Emailed shop", srv.URL,
		`login: { url: "/login", check: async ({ cookies }) => cookies.some((c) => c.name === "session") },`)

	click(t, role(page, "status", "Sign-in").GetByRole("button", playwright.LocatorGetByRoleOptions{Name: "Sign in"}))
	view := page.GetByRole("dialog", playwright.PageGetByRoleOptions{Name: "Sign in to Emailed shop"})
	e2e.Expect(t, view).ToBeVisible()
	// The page shows: the view draws the tab's frames on a canvas, which is
	// transparent until the first one.
	canvas, err := view.GetByLabel("The page", playwright.LocatorGetByLabelOptions{Exact: playwright.Bool(true)}).ElementHandle()
	e2e.Do(t, err)
	_, err = page.WaitForFunction(`(c) => c.getContext("2d").getImageData(0, 0, c.width, c.height).data.some((v, i) => i % 4 === 3 && v > 0)`, canvas)
	e2e.Do(t, err)

	// The address bar shows where the tab is, and goes where you type, such
	// as a link a site emailed you.
	address := view.GetByLabel("Address", playwright.LocatorGetByLabelOptions{Exact: playwright.Bool(true)})
	e2e.Expect(t, address).ToHaveValue(srv.URL + "/login")
	e2e.Do(t, address.Fill(srv.URL+"/emailed"))
	e2e.Do(t, address.Press("Enter"))
	e2e.Expect(t, view).Not().ToBeVisible()
	e2e.Expect(t, role(page, "status", "Sign-in")).ToContainText("Signed in")
	e2e.Expect(t, page.GetByText("Sign in at "+srv.URL+"/login")).Not().ToBeVisible()
}
