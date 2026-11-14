package backend

import (
	"strings"
	"testing"

	"github.com/michaelckearney/monorepo/framework"
)

const serviceTemplate = `export default { name: "%s", title: "T", url: "https://example.com" }`

func source(name string) string { return strings.Replace(serviceTemplate, "%s", name, 1) }

func TestSaveService(t *testing.T) {
	db := desktop.TestDB(t)
	d := Documents{db: db, icons: newIcons(db)}

	id, err := d.SaveService(0, source("one"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := d.SaveService(id, source("renamed")); err != nil {
		t.Fatal(err)
	}
	var name, src, manifest, kind string
	db.QueryRow(`SELECT name, source, manifest, typeof(manifest) FROM services WHERE id = ?`, id).
		Scan(&name, &src, &manifest, &kind)
	if name != "renamed" || src != source("renamed") || !strings.Contains(manifest, `"name":"renamed"`) || kind != "text" {
		t.Errorf("saved %q %q %q, manifest as %s", name, src, manifest, kind)
	}

	if _, err := d.SaveService(0, source("renamed")); err == nil || err.Error() != "there is already a service named renamed" {
		t.Errorf("duplicate name: %v", err)
	}
	if _, err := d.SaveService(id, `export default {}`); err == nil {
		t.Error("saved a file with problems")
	}
	if err := d.DeleteService(id); err != nil {
		t.Fatal(err)
	}
	if _, err := d.SaveService(id, source("gone")); err == nil || !strings.Contains(err.Error(), "deleted") {
		t.Errorf("save after delete: %v", err)
	}
}

func TestSaveDashboard(t *testing.T) {
	db := desktop.TestDB(t)
	d := Documents{db: db, icons: newIcons(db)}

	id, err := d.SaveDashboard(0, " Inbox ", dashboardStarter)
	if err != nil {
		t.Fatal(err)
	}
	var name string
	db.QueryRow(`SELECT name FROM dashboards WHERE id = ?`, id).Scan(&name)
	if name != "Inbox" {
		t.Errorf("name = %q", name)
	}
	if _, err := d.SaveDashboard(0, "Inbox", dashboardStarter); err == nil || err.Error() != "there is already a dashboard named Inbox" {
		t.Errorf("duplicate name: %v", err)
	}
	if _, err := d.SaveDashboard(id, "  ", dashboardStarter); err == nil || err.Error() != "a dashboard needs a name" {
		t.Errorf("empty name: %v", err)
	}
	if _, err := d.SaveDashboard(id, "Inbox", "<template><p></template>"); err == nil || !strings.HasPrefix(err.Error(), "line 1: ") {
		t.Errorf("a file that does not compile: %v", err)
	}
	if c, err := d.CompiledDashboard(id); err != nil || !strings.Contains(c.JS, "module.exports") {
		t.Errorf("compiled: %v %+v", err, c)
	}
	if err := d.DeleteDashboard(id); err != nil {
		t.Fatal(err)
	}
	if err := d.DeleteDashboard(id); err == nil {
		t.Error("deleted twice")
	}
}
