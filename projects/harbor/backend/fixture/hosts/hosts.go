// Package hosts names every site the fixtures serve. Each is its own host
// under .localhost. Each organization has
// its own domain, so its sites are a different "site" from every other
// organization's, as on the real web: cookies, SameSite, and iframes behave
// as they would between real companies.
package hosts

// Port is the port in every URL. The fixtures listen on it when it is free,
// and on another port otherwise; Harbor's browsers and package fixture's
// Transport send these URLs there.
const Port = "8443"

const (
	Index = "fixtures.localhost"
	Mail  = "mail.localhost"

	Bank = "tidewater.localhost"

	Energy    = "brightline.localhost"
	EnergyAPI = "api.brightline.localhost"

	Shop = "parcel.localhost"

	Health = "clearwater.localhost"

	School    = "maplegrove.localhost"
	SchoolSSO = "sso.maplegrove.localhost"
	Gradebook = "gradebook.localhost"

	NorthwindID = "id.northwind.localhost"
	Tasks       = "tasks.northwind.localhost"
	Payroll     = "payroll.northwind.localhost"

	Chat = "huddle.localhost"

	Photos     = "shoebox.localhost"
	PhotoFiles = "files.shoebox.localhost"

	Home       = "home.localhost"
	HomeLogin  = "login.home.localhost"
	HomeRouter = "router.home.localhost"
)

// URL is the https URL of a host, with the port.
func URL(host string) string { return "https://" + host + ":" + Port }
