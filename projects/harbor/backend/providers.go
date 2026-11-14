package backend

import (
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"slices"
	"strings"

	"github.com/michaelckearney/monorepo/projects/harbor/backend/chat"
	"github.com/michaelckearney/monorepo/projects/harbor/backend/provider"
)

// The providers that come with Harbor, one TypeScript file each, and the
// modules in providers/shared they import.
//
//go:embed providers
var builtinProviders embed.FS

// Providers are the providers Harbor's agent can use, and what the user
// filled in for them. The frontend reads the settings with live queries.
type Providers struct {
	db  *sql.DB
	all []*provider.Provider
}

func newProviders(db *sql.DB) *Providers {
	p := &Providers{db: db}
	dir, _ := fs.Sub(builtinProviders, "providers")
	files, _ := fs.Glob(dir, "*.ts")
	for _, file := range files {
		source, _ := fs.ReadFile(dir, file)
		loaded, err := provider.Load(string(source), dir)
		if err != nil {
			// A test loads every built-in provider, so this is a bug.
			panic(fmt.Sprintf("%s: %v", file, err))
		}
		p.all = append(p.all, loaded)
	}
	// By title, as Providers lists them: OpenAI ahead of
	// openai-compatible.ts, which sorts first as a file.
	slices.SortFunc(p.all, func(a, b *provider.Provider) int { return strings.Compare(a.Title, b.Title) })
	return p
}

// List returns every provider: its name, title, and settings.
func (p *Providers) List() []provider.Manifest {
	out := make([]provider.Manifest, len(p.all))
	for i, pr := range p.all {
		out[i] = pr.Manifest
	}
	return out
}

// Save saves a provider's settings, and makes it the one the agent uses. An
// empty value is the setting's default.
func (p *Providers) Save(name string, values map[string]string) error {
	pr := p.find(name)
	if pr == nil {
		return fmt.Errorf("there is no provider named %s", name)
	}
	known := map[string]bool{}
	for _, s := range pr.Settings {
		known[s.Key] = true
	}
	tx, err := p.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`DELETE FROM provider_settings WHERE provider = ?`, name); err != nil {
		return err
	}
	for key, value := range values {
		if !known[key] {
			return fmt.Errorf("%s has no setting %s", pr.Title, key)
		}
		if value = strings.TrimSpace(value); value != "" {
			if _, err := tx.Exec(`INSERT INTO provider_settings (provider, key, value) VALUES (?, ?, ?)`, name, key, value); err != nil {
				return err
			}
		}
	}
	if _, err := tx.Exec(`INSERT INTO settings (key, value) VALUES ('provider', ?)
		ON CONFLICT (key) DO UPDATE SET value = excluded.value`, name); err != nil {
		return err
	}
	return tx.Commit()
}

func (p *Providers) find(name string) *provider.Provider {
	for _, pr := range p.all {
		if pr.Name == name {
			return pr
		}
	}
	return nil
}

var errNoProvider = errors.New("Harbor's agent has no provider yet. Set one up in Settings, under Providers")

// model is the provider the agent uses, with its settings, read again for
// each turn so a change takes effect on the next one.
func (p *Providers) model() (chat.Model, error) {
	pr, settings, err := p.current()
	if err != nil {
		return nil, err
	}
	return providerModel(pr, settings)
}

// modelAt is a model written provider/model, such as openai/gpt-5: that
// provider, with its settings, on that model. A benchmark's agents and
// grader are written this way, so one provider can grade another's model.
func (p *Providers) modelAt(at string) (chat.Model, error) {
	name, model, ok := strings.Cut(at, "/")
	if !ok || model == "" {
		return nil, fmt.Errorf("write %q as provider/model, such as openai/gpt-5", at)
	}
	pr := p.find(name)
	if pr == nil {
		return nil, fmt.Errorf("there is no provider named %s", name)
	}
	if !slices.ContainsFunc(pr.Settings, func(s provider.Setting) bool { return s.Key == "model" }) {
		return nil, fmt.Errorf("%s has no model to choose", pr.Title)
	}
	settings, err := p.settings(name)
	if err != nil {
		return nil, err
	}
	settings["model"] = model
	return providerModel(pr, settings)
}

// current is the provider the agent uses, and its settings.
func (p *Providers) current() (*provider.Provider, map[string]string, error) {
	var name string
	err := p.db.QueryRow(`SELECT value FROM settings WHERE key = 'provider'`).Scan(&name)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil, errNoProvider
	}
	if err != nil {
		return nil, nil, err
	}
	pr := p.find(name)
	if pr == nil {
		return nil, nil, errNoProvider
	}
	settings, err := p.settings(name)
	return pr, settings, err
}

// settings is what the user filled in for a provider.
func (p *Providers) settings(name string) (map[string]string, error) {
	rows, err := p.db.Query(`SELECT key, value FROM provider_settings WHERE provider = ?`, name)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	settings := map[string]string{}
	for rows.Next() {
		var key, value string
		if err := rows.Scan(&key, &value); err != nil {
			return nil, err
		}
		settings[key] = value
	}
	return settings, rows.Err()
}

// providerModel is the provider with these settings, or why it cannot run.
func providerModel(pr *provider.Provider, settings map[string]string) (chat.Model, error) {
	for _, s := range pr.Settings {
		if !s.Optional && s.Default == "" && settings[s.Key] == "" {
			return nil, fmt.Errorf("%s needs its %s. Add it in Providers", pr.Title, s.Title)
		}
	}
	return provider.Model{Provider: pr, Settings: settings}, nil
}
