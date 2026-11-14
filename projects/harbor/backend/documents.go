package backend

import (
	"database/sql"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/michaelckearney/monorepo/projects/harbor/backend/dashboard"
	"github.com/michaelckearney/monorepo/projects/harbor/backend/service"
	"github.com/michaelckearney/monorepo/projects/harbor/frontend/src/kit"
)

// Documents saves and deletes services and dashboards. The frontend reads
// them with live queries.
type Documents struct {
	db    *sql.DB
	icons *Icons
}

var (
	//go:embed starters/service.ts
	serviceStarter string
	//go:embed starters/dashboard.vue
	dashboardStarter string
)

// ServiceStarter is the file a new service starts from.
func (d *Documents) ServiceStarter() string { return serviceStarter }

// DashboardStarter is the file a new dashboard starts from.
func (d *Documents) DashboardStarter() string { return dashboardStarter }

// SaveService checks source and saves it, as a new service when id is 0. It
// returns the service's id. A file with problems is not saved, and the error
// lists them.
func (d *Documents) SaveService(id int64, source string) (int64, error) {
	m, err := readManifest(source)
	if err != nil {
		return 0, err
	}
	if id == 0 {
		err = d.db.QueryRow(`
			INSERT INTO services (name, source, manifest) VALUES (?, ?, ?)
			RETURNING id`, m.Name, source, m.JSON).Scan(&id)
	} else {
		err = updated(d.db.Exec(`
			UPDATE services SET name = ?, source = ?, manifest = ?, manifest_error = NULL, updated_at = CURRENT_TIMESTAMP
			WHERE id = ?`, m.Name, source, m.JSON, id))
	}
	if isUnique(err) {
		return 0, fmt.Errorf("there is already a service named %s", m.Name)
	}
	if err != nil {
		return 0, err
	}
	// Its URL may be on a site with no icon yet.
	d.icons.request()
	return id, nil
}

// DeleteService deletes a service.
func (d *Documents) DeleteService(id int64) error {
	return updated(d.db.Exec(`DELETE FROM services WHERE id = ?`, id))
}

// SaveDashboard checks source and saves it, as a new dashboard when id is 0.
// It returns the dashboard's id. A file that does not compile is not saved,
// and the error lists its problems.
func (d *Documents) SaveDashboard(id int64, name, source string) (int64, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return 0, errors.New("a dashboard needs a name")
	}
	c, err := d.compile(source)
	if err != nil {
		return 0, err
	}
	if id == 0 {
		err = d.db.QueryRow(`
			INSERT INTO dashboards (name, source) VALUES (?, ?)
			RETURNING id`, name, source).Scan(&id)
	} else {
		err = updated(d.db.Exec(`
			UPDATE dashboards SET name = ?, source = ?, updated_at = CURRENT_TIMESTAMP
			WHERE id = ?`, name, source, id))
	}
	if isUnique(err) {
		return 0, fmt.Errorf("there is already a dashboard named %s", name)
	}
	if err != nil {
		return 0, err
	}
	return id, d.useServices(id, c)
}

// CompiledDashboard compiles a saved dashboard to show it. It compiles each
// time, so what it shows always matches the Vue that runs it.
func (d *Documents) CompiledDashboard(id int64) (dashboard.Compiled, error) {
	var source string
	err := d.db.QueryRow(`SELECT source FROM dashboards WHERE id = ?`, id).Scan(&source)
	if errors.Is(err, sql.ErrNoRows) {
		return dashboard.Compiled{}, errors.New("it was deleted, so there is nothing to show")
	}
	if err != nil {
		return dashboard.Compiled{}, err
	}
	c, err := d.compile(source)
	if err != nil {
		return c, err
	}
	// A migration may have changed the file without compiling it.
	return c, d.useServices(id, c)
}

// compile compiles a dashboard, and fills in each tool it uses from its
// service's manifest. A service or a tool that is not there is a problem.
func (d *Documents) compile(source string) (dashboard.Compiled, error) {
	c, err := dashboard.Compile(source)
	if err != nil {
		return c, err
	}
	var problems []string
	manifests := map[string]*service.Manifest{}
	for i, u := range c.Uses {
		m, seen := manifests[u.Service]
		if !seen {
			if m, err = d.manifest(u.Service); err != nil {
				return c, err
			}
			manifests[u.Service] = m
			if m == nil {
				problems = append(problems, fmt.Sprintf(`useService("%s"): there is no service named %s`, u.Service, u.Service))
			}
		}
		if m == nil {
			continue
		}
		var t *service.Tool
		for j := range m.Tools {
			if m.Tools[j].Name == u.Tool {
				t = &m.Tools[j]
			}
		}
		if t == nil {
			problems = append(problems, fmt.Sprintf("%s.%s: %s has no tool %s", u.Service, u.Tool, u.Service, u.Tool))
			continue
		}
		a := t.Annotations
		c.Uses[i].Title = t.Title
		if c.Uses[i].Title == "" {
			c.Uses[i].Title = words(t.Name)
		}
		c.Uses[i].Read = a != nil && a.ReadOnlyHint != nil && *a.ReadOnlyHint
		c.Uses[i].Destructive = !c.Uses[i].Read && (a == nil || a.DestructiveHint == nil || *a.DestructiveHint)
	}
	if len(problems) > 0 {
		return c, errors.New(strings.Join(problems, "\n"))
	}
	return c, nil
}

// manifest is the manifest of the service with this name, or nil if there
// is none, or its file has problems.
func (d *Documents) manifest(name string) (*service.Manifest, error) {
	var raw sql.NullString
	err := d.db.QueryRow(`SELECT manifest FROM services WHERE name = ?`, name).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) || (err == nil && !raw.Valid) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var m service.Manifest
	if err := json.Unmarshal([]byte(raw.String), &m); err != nil {
		return nil, err
	}
	return &m, nil
}

// useServices notes which services a dashboard uses, as its file says.
func (d *Documents) useServices(id int64, c dashboard.Compiled) error {
	if _, err := d.db.Exec(`DELETE FROM dashboard_services WHERE dashboard_id = ?`, id); err != nil {
		return err
	}
	for _, u := range c.Uses {
		if _, err := d.db.Exec(`INSERT OR IGNORE INTO dashboard_services (dashboard_id, service) VALUES (?, ?)`, id, u.Service); err != nil {
			return err
		}
	}
	return nil
}

// words is a tool's name as words: "update_issue" is "Update issue".
func words(name string) string {
	w := strings.ReplaceAll(name, "_", " ")
	return strings.ToUpper(w[:1]) + w[1:]
}

// KitComponent is a kit component as the Kit page shows it: what it says of
// itself, and each example compiled as a dashboard.
type KitComponent struct {
	Name        string       `json:"name"`
	Kind        string       `json:"kind"`
	Description string       `json:"description"`
	Keywords    []string     `json:"keywords"`
	Props       string       `json:"props"`
	Examples    []KitExample `json:"examples"`
}

// KitExample is an example's template, and it compiled.
type KitExample struct {
	Source string             `json:"source"`
	Code   dashboard.Compiled `json:"code"`
}

// Kit is the dashboard kit, for the Kit page in dev mode.
func (d *Documents) Kit() ([]KitComponent, error) {
	var out []KitComponent
	for _, c := range kit.All() {
		k := KitComponent{Name: c.Name, Kind: c.Kind, Description: c.Description, Keywords: c.Keywords, Props: c.Props}
		for _, e := range c.Examples {
			code, err := dashboard.Compile(kit.Dashboard(e))
			if err != nil {
				return nil, fmt.Errorf("%s's example: %w", c.Name, err)
			}
			k.Examples = append(k.Examples, KitExample{Source: e, Code: code})
		}
		out = append(out, k)
	}
	return out, nil
}

// DeleteDashboard deletes a dashboard.
func (d *Documents) DeleteDashboard(id int64) error {
	return updated(d.db.Exec(`DELETE FROM dashboards WHERE id = ?`, id))
}

// updated turns a change to no rows into an error: the document was deleted
// while it was open.
func updated(res sql.Result, err error) error {
	if err != nil {
		return err
	}
	if n, err := res.RowsAffected(); err != nil || n == 0 {
		return errors.New("it was deleted, so there is nothing to change")
	}
	return nil
}

func isUnique(err error) bool {
	return err != nil && strings.Contains(err.Error(), "UNIQUE constraint failed")
}
