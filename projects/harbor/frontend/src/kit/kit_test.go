package kit_test

import (
	"testing"

	"github.com/michaelckearney/monorepo/projects/harbor/backend/dashboard"
	"github.com/michaelckearney/monorepo/projects/harbor/frontend/src/kit"
)

// Every component describes itself, so search can find it, and shows how
// it's used, so the agent can copy it.
func TestEveryComponentDescribesItself(t *testing.T) {
	if len(kit.All()) < 35 {
		t.Fatalf("the kit has %d components; the embed found too few", len(kit.All()))
	}
	for _, c := range kit.All() {
		if c.Description == "" {
			t.Errorf("%s has no description", c.Name)
		}
		if len(c.Keywords) < 5 {
			t.Errorf("%s has %d keywords; give it the words someone would use when they want it", c.Name, len(c.Keywords))
		}
		if len(c.Examples) == 0 {
			t.Errorf("%s has no example", c.Name)
		}
	}
}

// Each example is something a dashboard can say: it compiles as one.
func TestEveryExampleCompiles(t *testing.T) {
	for _, c := range kit.All() {
		for i, e := range c.Examples {
			if problems := dashboard.Check(kit.Dashboard(e)); problems != nil {
				t.Errorf("%s example %d: %q", c.Name, i+1, problems)
			}
		}
	}
}

// What people ask for, and what the agent should find first.
func TestSearchFindsWhatPeopleAskFor(t *testing.T) {
	for query, want := range map[string]string{
		"my emails":                         "InboxWidget",
		"messages from the doctor's office": "InboxWidget",
		"slack mentions":                    "InboxWidget",
		"upcoming appointments":             "AgendaWidget",
		"what's coming up this week":        "AgendaWidget",
		"homework due":                      "TasksWidget",
		"my open issues":                    "TasksWidget",
		"kanban board of issues":            "BoardWidget",
		"recent bank transactions":          "LedgerWidget",
		"disk space":                        "MetersWidget",
		"budget by category":                "MetersWidget",
		"photos from my album":              "GalleryWidget",
		"electricity usage over time":       "ChartWidget",
		"latest paycheck":                   "MetricWidget",
		"time off left":                     "StatsWidget",
		"packages on the way":               "StatusWidget",
		"router details":                    "DetailsWidget",
		"a dropdown to pick a project":      "Select",
		"build my own widget":               "Widget",
	} {
		got := kit.Search(query, 3)
		if len(got) == 0 || got[0].Name != want {
			t.Errorf("Search(%q) = %v, want %s first", query, got, want)
		}
	}
}

func TestLikeSuggestsCloseNames(t *testing.T) {
	for name, want := range map[string]string{"Inbox": "InboxWidget", "Stat": "StatsWidget", "Gird": "Grid"} {
		found := false
		for _, n := range kit.Like(name) {
			found = found || n == want
		}
		if !found {
			t.Errorf("Like(%q) = %v, want it to include %s", name, kit.Like(name), want)
		}
	}
}
