package backend

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// Preferences are how you like Harbor's window: its theme, and where you
// left it. The frontend reads them from settings and themes with live
// queries; the built-in themes are in the frontend (themes.ts).
type Preferences struct {
	db *sql.DB
}

func (p *Preferences) set(key, value string) error {
	_, err := p.db.Exec(`INSERT INTO settings (key, value) VALUES (?, ?)
		ON CONFLICT (key) DO UPDATE SET value = excluded.value`, key, value)
	return err
}

// UseTheme makes the named theme, built-in or yours, the one in use.
func (p *Preferences) UseTheme(name string) error { return p.set("theme", name) }

// SetMode sets whether the window is light, dark, or follows the system.
func (p *Preferences) SetMode(mode string) error {
	switch mode {
	case "light", "dark", "system":
		return p.set("mode", mode)
	}
	return fmt.Errorf("the mode is light, dark, or system, not %q", mode)
}

// SaveTheme saves a theme of yours, and uses it. original is the saved
// theme you are changing, or "" when adding one; renaming moves its row.
// tokens is JSON: {"light": {...}, "dark": {...}}, each CSS variables by
// name without the dashes, such as "primary": "#0e5e6f". It is an error if
// the name belongs to another theme of yours.
func (p *Preferences) SaveTheme(original, name, tokens string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return errors.New("a theme needs a name")
	}
	var t struct{ Light, Dark map[string]string }
	if err := json.Unmarshal([]byte(tokens), &t); err != nil {
		return fmt.Errorf("the theme's colours are not JSON: %w", err)
	}
	if len(t.Light) == 0 || len(t.Dark) == 0 {
		return errors.New("a theme has light and dark colours")
	}
	tx, err := p.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var exists int
	if err := tx.QueryRow(`SELECT count(*) FROM themes WHERE name = ?`, name).Scan(&exists); err != nil {
		return err
	}
	if exists > 0 && name != original {
		return errors.New("A theme with this name exists.")
	}
	if original != "" && original != name {
		if _, err := tx.Exec(`DELETE FROM themes WHERE name = ?`, original); err != nil {
			return err
		}
		if _, err := tx.Exec(`DELETE FROM settings WHERE key = 'theme' AND value = ?`, original); err != nil {
			return err
		}
	}
	if _, err := tx.Exec(`INSERT INTO themes (name, tokens) VALUES (?, ?)
		ON CONFLICT (name) DO UPDATE SET tokens = excluded.tokens`, name, tokens); err != nil {
		return err
	}
	if _, err := tx.Exec(`INSERT INTO settings (key, value) VALUES ('theme', ?)
		ON CONFLICT (key) DO UPDATE SET value = excluded.value`, name); err != nil {
		return err
	}
	return tx.Commit()
}

// DeleteTheme deletes a theme of yours. If it was in use, Harbor's own is.
func (p *Preferences) DeleteTheme(name string) error {
	tx, err := p.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`DELETE FROM themes WHERE name = ?`, name); err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM settings WHERE key = 'theme' AND value = ?`, name); err != nil {
		return err
	}
	return tx.Commit()
}

// OpenedDashboard remembers the dashboard you opened, which the window
// opens to next time.
func (p *Preferences) OpenedDashboard(id int64) error { return p.set("last_dashboard", fmt.Sprint(id)) }

// SetPanel sets how wide a side panel of the window is, in pixels, and
// whether it is collapsed to a strip, which keeps its width for when it opens
// again. The panels are the sidebar and the agent.
func (p *Preferences) SetPanel(side string, width int, collapsed bool) error {
	if side != "sidebar" && side != "agent" {
		return fmt.Errorf("the panels are the sidebar and the agent, not %q", side)
	}
	if width <= 0 {
		return fmt.Errorf("a panel is wider than %d pixels", width)
	}
	value, err := json.Marshal(map[string]any{"width": width, "collapsed": collapsed})
	if err != nil {
		return err
	}
	return p.set(side+"_panel", string(value))
}
