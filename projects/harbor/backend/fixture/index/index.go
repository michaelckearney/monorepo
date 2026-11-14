// Package index is the fixtures' front page: every site, how to sign in to
// it, and the fixtures' CA certificate.
package index

import (
	"embed"
	"net/http"

	"github.com/michaelckearney/monorepo/projects/harbor/backend/fixture/hosts"
	"github.com/michaelckearney/monorepo/projects/harbor/backend/fixture/site"
)

//go:embed templates
var files embed.FS

type entry struct {
	Name, Host, What, SignIn string
}

var entries = []entry{
	{"Tidewater Bank", hosts.Bank, "A classic server-rendered bank: accounts, statements, transfers.", "Email alex@example.localhost, then the password, then a code from the TOTP secret below."},
	{"Brightline Energy", hosts.Energy, "Bills and meter usage, from an API on another subdomain.", "Email alex@example.localhost; the sign-in link arrives in Mail."},
	{"Parcel", hosts.Shop, "An online shop: orders, tracking, returns.", "Email alex@example.localhost and the password; then a code from Mail."},
	{"Clearwater Health", hosts.Health, "A patient portal for a family.", "Email alex@example.localhost and the password; choose a person; accept the terms."},
	{"Maple Grove Schools", hosts.School, "A parent portal, with grades in another company's iframe.", "District sign-in (SAML): email alex@example.localhost and the password."},
	{"Tasklane", hosts.Tasks, "Northwind's issue tracker: a single-page app on GraphQL.", "Northwind ID: email alex@example.localhost and the password."},
	{"Payday", hosts.Payroll, "Northwind's HR and payroll.", "Northwind ID; no form if you are already signed in to Tasklane."},
	{"Huddle", hosts.Chat, "Team chat.", "Email alex@example.localhost and the password, in a popup."},
	{"Shoebox", hosts.Photos, "Photo storage.", "Email alex@example.localhost and the password, then add a passkey."},
	{"Homebase", hosts.Home, "A self-hosted home server, behind a login portal.", "Email alex@example.localhost and the password, at the portal."},
	{"Homebase router", hosts.HomeRouter, "The router's admin page.", "HTTP Basic: alex@example.localhost and the password."},
	{"Mail", hosts.Mail, "Every email the sites send. No sign-in.", ""},
}

func New(caPEM []byte) http.Handler {
	pages := site.NewPages(site.Brand{Name: "Harbor fixtures", Color: "#334155"}, files, nil)
	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
		pages.Render(w, 200, "index", "", map[string]any{
			"Sites": entries, "Password": site.Password, "TOTP": "TIDEWATERBANKHARBORFIXTURESALEXQ",
		})
	})
	mux.HandleFunc("GET /ca.pem", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/x-pem-file")
		w.Write(caPEM)
	})
	return mux
}
