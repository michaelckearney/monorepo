package run

import (
	"testing"

	"github.com/michaelckearney/monorepo/framework/internal/branches"
)

func TestRelevantIsWhatGoBuilds(t *testing.T) {
	for path, want := range map[string]bool{
		"backend/app.go":      true,
		"go.mod":              true,
		"go.sum":              true,
		"backend/app_test.go": false,
		"backend/.#app.go":    false,
		"backend/_old.go":     false,
		"backend/notes.md":    false,
	} {
		if got := relevant(path); got != want {
			t.Errorf("relevant(%q) = %v, want %v", path, got, want)
		}
	}
}

func TestScratchIsWhatFinderAndEditorsLeave(t *testing.T) {
	for path, want := range map[string]bool{
		"database/dev/001_init.sql":      false,
		"database/assets/chart.vue":      false,
		"database/dev/.DS_Store":         true,
		"database/dev/.001_init.sql.swp": true,
		"database/dev/001_init.sql~":     true,
		"database/dev/#001_init.sql#":    true,
		"database/dev/#":                 false,
	} {
		if got := scratch(path); got != want {
			t.Errorf("scratch(%q) = %v, want %v", path, got, want)
		}
	}
}

func TestLabel(t *testing.T) {
	pr := branches.Branch{Key: "harness-17", Name: "harness/17", PR: &branches.PR{Number: 18, Title: "fix: indent the buttons"}}
	if got := label(pr); got != "#18 fix: indent the buttons · harness/17" {
		t.Errorf("a pull request's label: %q", got)
	}
	if got := label(branches.Branch{Key: "x", Name: "feature/x"}); got != "feature/x" {
		t.Errorf("a branch's label: %q", got)
	}
}
