package dev

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestRunsAToolInAFrameFromAnotherSite: a portal shows each student's grades
// in a frame from a gradebook on another site, which the portal's page
// cannot read; and notes in a frame from its own site, on another port.
func TestRunsAToolInAFrameFromAnotherSite(t *testing.T) {
	gradebook := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, `<!doctype html><p id="grade">Student %s: A</p>`, r.URL.Query().Get("student"))
	}))
	t.Cleanup(gradebook.Close)
	notes := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `<!doctype html><p id="note">Bring a coat</p>`)
	}))
	t.Cleanup(notes.Close)
	// The portal is localhost, the gradebook 127.0.0.1: two sites. The notes
	// are localhost too, on another port: the same site, another origin.
	notesURL := strings.Replace(notes.URL, "127.0.0.1", "localhost", 1)
	portal := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if id, ok := strings.CutPrefix(r.URL.Path, "/students/"); ok {
			fmt.Fprintf(w, `<!doctype html><p>Portal</p><iframe src="%s/embed?student=%s"></iframe>`, gradebook.URL, id)
			return
		}
		fmt.Fprintf(w, `<!doctype html><p>Portal</p><iframe src="%s/notes"></iframe>`, notesURL)
	}))
	t.Cleanup(portal.Close)
	portalURL := strings.Replace(portal.URL, "127.0.0.1", "localhost", 1)

	src := strings.NewReplacer("GRADEBOOK", gradebook.URL, "NOTES", notesURL).Replace(`export default {
  name: "portal",
  title: "Portal",
  url: "URL",
  tools: {
    grade: {
      description: "A student's grade.",
      inputSchema: { type: "object", required: ["student"], properties: { student: { type: "string" } } },
      frame: { site: "GRADEBOOK", page: (input) => "/students/" + input.student },
      async run() { return { grade: document.getElementById("grade").textContent, site: location.origin } },
    },
    note: {
      description: "The note.",
      frame: { site: "NOTES", page: "/" },
      async run() { return { note: document.getElementById("note").textContent } },
    },
  },
}
`)
	svc := load(t, src, portalURL)
	e := newEngine(t)
	for range 2 {
		got := call(t, e, svc, "grade", `{"student":"7"}`)
		if got["grade"] != "Student 7: A" || got["site"] != gradebook.URL {
			t.Errorf("grade = %v", got)
		}
	}
	if got := call(t, e, svc, "note", "{}"); got["note"] != "Bring a coat" {
		t.Errorf("note = %v", got)
	}
}
