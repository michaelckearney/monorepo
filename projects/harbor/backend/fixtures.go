package backend

import (
	"path/filepath"

	"github.com/michaelckearney/monorepo/framework"
	"github.com/michaelckearney/monorepo/projects/harbor/backend/fixture"
)

// Fixtures is the fixture sites, which a dev build serves while it runs
// (package fixture). It stops them when the app quits.
type Fixtures struct{ sites *fixture.Fixtures }

// StartFixtures serves the fixture sites in this process, with their store
// in the data folder. Start them before anything that reaches them: a
// browser or a client made before reaches them only on port 8443.
func StartFixtures() (*fixture.Fixtures, error) {
	return fixture.Start(filepath.Join(desktop.DataDir(), "fixtures"))
}

// ServiceShutdown stops the fixture sites when the app quits.
func (f *Fixtures) ServiceShutdown() error {
	f.sites.Stop()
	return nil
}
