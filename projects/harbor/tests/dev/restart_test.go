package dev

import (
	"context"
	"testing"

	"github.com/michaelckearney/monorepo/projects/harbor/backend/engine"
)

// visit has no login check, so it can sign in to the gate.
const visit = `export default {
  name: "visit",
  title: "Visit",
  url: "URL",
  tools: {
    sign_in: { description: "Sign in.", async run() { await fetch("/login?name=quit"); return {} } },
  },
}
`

// Many sites keep a sign-in in a session cookie, which a browser forgets when
// it quits. Harbor's must not, or you sign in again each time Harbor starts.
func TestASignInOutlivesQuittingHarbor(t *testing.T) {
	srv := gate(t)
	profile := t.TempDir()
	ctx := context.Background()
	svc := gatedService(t, "quit", "/email-signin", srv.URL)

	first := engine.New(profile, &memorySecrets{}, noWindow{}, &memoryFiles{})
	call(t, first, load(t, visit, srv.URL), "sign_in", "{}")
	if ok, err := first.SignedIn(ctx, svc); err != nil || !ok {
		t.Fatalf("SignedIn before quitting = %v, %v", ok, err)
	}
	// As the app does when it quits.
	first.Close()

	second := engine.New(profile, &memorySecrets{}, noWindow{}, &memoryFiles{})
	t.Cleanup(second.Close)
	if ok, err := second.SignedIn(ctx, svc); err != nil || !ok {
		t.Errorf("SignedIn after quitting = %v, %v; the session cookie was lost", ok, err)
	}
}
