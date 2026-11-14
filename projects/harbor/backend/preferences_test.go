package backend

import (
	"errors"
	"fmt"
	"testing"

	"github.com/michaelckearney/monorepo/framework"
	"github.com/michaelckearney/monorepo/projects/harbor/backend/engine"
)

func setting(t *testing.T, p *Preferences, key string) string {
	t.Helper()
	var v string
	p.db.QueryRow(`SELECT value FROM settings WHERE key = ?`, key).Scan(&v)
	return v
}

const tokens = `{"light":{"primary":"red"},"dark":{"primary":"pink"}}`

func names(t *testing.T, p *Preferences) []string {
	t.Helper()
	rows, err := p.db.Query(`SELECT name FROM themes ORDER BY name`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var got []string
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			t.Fatal(err)
		}
		got = append(got, n)
	}
	return got
}

func TestSavesUsesAndDeletesATheme(t *testing.T) {
	p := &Preferences{db: desktop.TestDB(t)}
	if err := p.SaveTheme("", "  ", tokens); err == nil {
		t.Error("saved a theme with no name")
	}
	if err := p.SaveTheme("", "Mine", `{"light":{"primary":"red"}}`); err == nil {
		t.Error("saved a theme with no dark colours")
	}
	if err := p.SaveTheme("", "Mine", tokens); err != nil {
		t.Fatal(err)
	}
	if err := p.SaveTheme("", "Mine", tokens); err == nil {
		t.Error("saved a new theme with a name that exists")
	}
	if got := setting(t, p, "theme"); got != "Mine" {
		t.Errorf("theme in use = %q", got)
	}
	if err := p.DeleteTheme("Mine"); err != nil {
		t.Fatal(err)
	}
	var themes int
	p.db.QueryRow(`SELECT count(*) FROM themes`).Scan(&themes)
	if got := setting(t, p, "theme"); got != "" || themes != 0 {
		t.Errorf("after delete: theme %q, %d themes", got, themes)
	}
}

func TestRenamesATheme(t *testing.T) {
	p := &Preferences{db: desktop.TestDB(t)}
	if err := p.SaveTheme("", "Mine", tokens); err != nil {
		t.Fatal(err)
	}
	if err := p.SaveTheme("", "Ours", tokens); err != nil {
		t.Fatal(err)
	}
	// Renaming moves the row, and the theme setting with it.
	if err := p.SaveTheme("Ours", "Mine2", tokens); err != nil {
		t.Fatal(err)
	}
	if got := setting(t, p, "theme"); got != "Mine2" {
		t.Errorf("theme in use = %q", got)
	}
	if got := fmt.Sprint(names(t, p)); got != "[Mine Mine2]" {
		t.Errorf("themes = %s", got)
	}
	// It is an error to rename to a name another of yours has.
	if err := p.SaveTheme("Mine2", "Mine", tokens); err == nil {
		t.Error("renamed a theme to a name that exists")
	}
	if got := setting(t, p, "theme"); got != "Mine2" {
		t.Errorf("after the failed rename, theme in use = %q", got)
	}
	if got := fmt.Sprint(names(t, p)); got != "[Mine Mine2]" {
		t.Errorf("after the failed rename, themes = %s", got)
	}
	// Saving a theme without renaming it keeps it.
	if err := p.SaveTheme("Mine", "Mine", `{"light":{"primary":"blue"},"dark":{"primary":"navy"}}`); err != nil {
		t.Fatal(err)
	}
	if got := fmt.Sprint(names(t, p)); got != "[Mine Mine2]" {
		t.Errorf("themes = %s", got)
	}
}

func TestChecksModeAndPanel(t *testing.T) {
	p := &Preferences{db: desktop.TestDB(t)}
	if p.SetMode("dim") == nil || p.SetPanel("footer", 300, false) == nil || p.SetPanel("agent", 0, true) == nil {
		t.Error("took a mode or panel that is not one")
	}
	if err := p.SetMode("dark"); err != nil {
		t.Fatal(err)
	}
	if err := p.SetPanel("agent", 480, true); err != nil {
		t.Fatal(err)
	}
	if err := p.OpenedDashboard(7); err != nil {
		t.Fatal(err)
	}
	if setting(t, p, "mode") != "dark" || setting(t, p, "agent_panel") != `{"collapsed":true,"width":480}` || setting(t, p, "last_dashboard") != "7" {
		t.Error("did not save mode, panel, and dashboard")
	}
}

func TestNotesWhichSignInsAreNeeded(t *testing.T) {
	tools := &Tools{db: desktop.TestDB(t)}
	rows := func() (n int) {
		tools.db.QueryRow(`SELECT count(*) FROM sign_ins_needed`).Scan(&n)
		return n
	}
	tools.note("bank", "", &engine.SignInNeeded{Service: "bank", Heading: "Sign in to Bank", Message: "sign in to Bank first"})
	tools.note("bank", "transfer", &engine.SignInNeeded{Service: "bank", Tool: "transfer", Heading: "Confirm", Message: "confirm"})
	tools.note("bank", "list", errors.New("the page changed"))
	if rows() != 2 {
		t.Fatalf("%d rows after two sign-ins needed", rows())
	}
	// Getting through list passes the service's login, not transfer's.
	tools.note("bank", "list", nil)
	var tool string
	tools.db.QueryRow(`SELECT tool FROM sign_ins_needed`).Scan(&tool)
	if rows() != 1 || tool != "transfer" {
		t.Errorf("after a call got through: %d rows, %q", rows(), tool)
	}
}
