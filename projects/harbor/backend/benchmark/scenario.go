// Package benchmark grades how well Harbor's agent does what users ask: a
// scenario is one user message, on the fixture sites, and a grading model
// scores what the agent did against the scenario's criteria. A dev build
// runs it from the developer bar's Benchmarks page. See
// docs/benchmarks.md.
package benchmark

import (
	"bytes"
	"embed"
	"fmt"
	"io/fs"
	"path"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"
)

// The scenarios, one YAML file each, and the files their setups name, in
// setup/.
//
//go:embed scenarios
var embedded embed.FS

// Files are the scenarios' folder.
var Files, _ = fs.Sub(embedded, "scenarios")

// Scenario is one file in scenarios/.
type Scenario struct {
	// Name is the file's name, without .yaml.
	Name     string      `yaml:"-" json:"name"`
	Prompt   string      `yaml:"prompt" json:"prompt"`
	Setup    Setup       `yaml:"setup" json:"setup"`
	Criteria []Criterion `yaml:"criteria" json:"criteria"`
}

// Setup is what exists before the message. Files are named from
// scenarios/.
type Setup struct {
	Services   []string `yaml:"services" json:"services,omitempty"`
	Dashboards []string `yaml:"dashboards" json:"dashboards,omitempty"`
	// SignedIn are services, by name, whose sites the user's browser is
	// signed in to.
	SignedIn []string `yaml:"signed_in" json:"signedIn,omitempty"`
}

// Criterion is one thing the grader scores, from 0 to 100.
type Criterion struct {
	ID          string  `yaml:"id" json:"id"`
	Title       string  `yaml:"title" json:"title"`
	Weight      float64 `yaml:"weight" json:"weight"`
	Description string  `yaml:"description" json:"description"`
}

var criterionID = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)

// Scenarios reads every scenario in fsys, by name. The error lists every
// problem in every file.
func Scenarios(fsys fs.FS) ([]Scenario, error) {
	files, err := fs.Glob(fsys, "*.yaml")
	if err != nil {
		return nil, err
	}
	var out []Scenario
	var problems []string
	for _, file := range files {
		s, err := readScenario(fsys, file)
		if err != nil {
			problems = append(problems, fmt.Sprintf("%s: %v", file, err))
			continue
		}
		out = append(out, s)
	}
	if len(problems) > 0 {
		return nil, fmt.Errorf("%s", strings.Join(problems, "\n"))
	}
	return out, nil
}

func readScenario(fsys fs.FS, file string) (Scenario, error) {
	raw, err := fs.ReadFile(fsys, file)
	if err != nil {
		return Scenario{}, err
	}
	var s Scenario
	d := yaml.NewDecoder(bytes.NewReader(raw))
	d.KnownFields(true)
	if err := d.Decode(&s); err != nil {
		return Scenario{}, err
	}
	s.Name = strings.TrimSuffix(path.Base(file), ".yaml")
	s.Prompt = strings.TrimSpace(s.Prompt)

	var problems []string
	if s.Prompt == "" {
		problems = append(problems, "it has no prompt")
	}
	if len(s.Criteria) == 0 {
		problems = append(problems, "it has no criteria")
	}
	seen := map[string]bool{}
	for i, c := range s.Criteria {
		switch {
		case !criterionID.MatchString(c.ID):
			problems = append(problems, fmt.Sprintf("criterion %d's id %q is not lower-case letters, digits, and underscores", i+1, c.ID))
		case seen[c.ID]:
			problems = append(problems, fmt.Sprintf("two criteria have the id %s", c.ID))
		}
		seen[c.ID] = true
		if strings.TrimSpace(c.Title) == "" || strings.TrimSpace(c.Description) == "" {
			problems = append(problems, fmt.Sprintf("criterion %s needs a title and a description", c.ID))
		}
		if c.Weight <= 0 {
			problems = append(problems, fmt.Sprintf("criterion %s needs a weight above 0", c.ID))
		}
	}
	for _, f := range append(append([]string{}, s.Setup.Services...), s.Setup.Dashboards...) {
		if _, err := fs.Stat(fsys, f); err != nil {
			problems = append(problems, fmt.Sprintf("its setup names %s, which is not there", f))
		}
	}
	if len(problems) > 0 {
		return Scenario{}, fmt.Errorf("%s", strings.Join(problems, "; "))
	}
	return s, nil
}
