package fixture

import (
	"net/http"

	"github.com/michaelckearney/monorepo/projects/harbor/backend/fixture/apps/bank"
	"github.com/michaelckearney/monorepo/projects/harbor/backend/fixture/apps/chat"
	"github.com/michaelckearney/monorepo/projects/harbor/backend/fixture/apps/energy"
	"github.com/michaelckearney/monorepo/projects/harbor/backend/fixture/apps/health"
	"github.com/michaelckearney/monorepo/projects/harbor/backend/fixture/apps/home"
	"github.com/michaelckearney/monorepo/projects/harbor/backend/fixture/apps/payroll"
	"github.com/michaelckearney/monorepo/projects/harbor/backend/fixture/apps/photos"
	"github.com/michaelckearney/monorepo/projects/harbor/backend/fixture/apps/school"
	"github.com/michaelckearney/monorepo/projects/harbor/backend/fixture/apps/shop"
	"github.com/michaelckearney/monorepo/projects/harbor/backend/fixture/apps/tasks"
	"github.com/michaelckearney/monorepo/projects/harbor/backend/fixture/hosts"
	"github.com/michaelckearney/monorepo/projects/harbor/backend/fixture/index"
	"github.com/michaelckearney/monorepo/projects/harbor/backend/fixture/mail"
	"github.com/michaelckearney/monorepo/projects/harbor/backend/fixture/portal"
	"github.com/michaelckearney/monorepo/projects/harbor/backend/fixture/site"
)

// sites returns the handler for each host.
func sites(store *site.Store, box *mail.Box, client *http.Client, caPEM []byte) map[string]http.Handler {
	energyPages, energyAPI := energy.New(store, box)
	schoolPortal, schoolSSO, gradebook := school.New(store)
	photoApp, photoFiles := photos.New(store)
	homePortal := homebasePortal(store)
	return map[string]http.Handler{
		hosts.Index: index.New(caPEM),
		hosts.Mail:  box.Handler(),

		hosts.Bank:      bank.New(store),
		hosts.Energy:    energyPages,
		hosts.EnergyAPI: energyAPI,
		hosts.Shop:      shop.New(store, box),
		hosts.Health:    health.New(store),
		hosts.School:    schoolPortal,
		hosts.SchoolSSO: schoolSSO,
		hosts.Gradebook: gradebook,

		hosts.NorthwindID: northwindID(store),
		hosts.Tasks:       tasks.New(store, client),
		hosts.Payroll:     payroll.New(store, client),

		hosts.Chat:       chat.New(store),
		hosts.Photos:     photoApp,
		hosts.PhotoFiles: photoFiles,

		hosts.HomeLogin:  homePortal,
		hosts.Home:       homePortal.Guard(home.New(store)),
		hosts.HomeRouter: home.Router(store),
	}
}

// alex is everyone the portals know: Alex Morgan, who works at Northwind and
// runs Homebase.
func alex(groups ...string) []portal.User {
	return []portal.User{{Username: "alex", Name: "Alex Morgan", Email: "alex@example.localhost", Groups: groups, Password: site.Password}}
}

// northwindID is Northwind's single sign-on, for Tasklane and Payday.
func northwindID(store *site.Store) *portal.Portal {
	p := portal.New(portal.Options{
		Brand:  site.Brand{Name: "Northwind ID", Color: "#2b4c9b", Footer: "Northwind Traders. For staff only.", Icons: `<link rel="icon" href="/favicon.svg" type="image/svg+xml">`},
		Host:   hosts.NorthwindID,
		Domain: "northwind.localhost",
		Cookie: "northwind_session",
		Users:  alex("staff"),
		Store:  store,
	})
	p.OIDC(
		// Tasklane is a single-page app: a public client, with PKCE, that
		// asks for consent each time.
		portal.Client{ID: "tasklane", Name: "Tasklane", Consent: true, RedirectURIs: []string{hosts.URL(hosts.Tasks) + "/callback"}},
		// Payday signs in on its server, with a secret, and no consent.
		portal.Client{ID: "payday", Name: "Payday", Secret: "payday-client-secret", RedirectURIs: []string{hosts.URL(hosts.Payroll) + "/auth/callback"}},
	)
	return p
}

// homebasePortal is the login portal in front of Homebase.
func homebasePortal(store *site.Store) *portal.Portal {
	return portal.New(portal.Options{
		Brand:  site.Brand{Name: "Homebase", Color: "#3d8b6d", Dark: true, Footer: "Homebase.", Icons: `<link rel="icon" href="/favicon.svg" type="image/svg+xml">`},
		Host:   hosts.HomeLogin,
		Domain: hosts.Home,
		Cookie: "homebase_session",
		Users:  alex("family"),
		Store:  store,
	})
}
