package generated

import (
	"path/filepath"
	"sync"

	"github.com/wailsapp/wails/v3/pkg/application"

	"github.com/michaelckearney/monorepo/framework"
	"github.com/michaelckearney/monorepo/framework/internal/appstate"
	"github.com/michaelckearney/monorepo/framework/internal/live"
	"github.com/michaelckearney/monorepo/framework/internal/services"
)

// Services are desktop's own services, which the generated main package adds
// to the backend's: Live, and those of desktop's packages the backend
// imports, such as package browser.
func Services() []application.Service {
	return append([]application.Service{application.NewService(&Live{})}, services.All()...)
}

// Live is the backend of useQuery in the frontend's @desktop module. The
// frontend calls it by name, so its package path, type, and method names are
// part of the contract with that module.
type Live struct{}

var engine = sync.OnceValues(func() (*live.Engine, error) {
	// Opening the app's database first applies its migrations, so a live
	// query never sees the tables before them.
	desktop.DB()
	path := filepath.Join(desktop.DataDir(), appstate.DBFile)
	return live.Start(path, func(u live.Update) {
		// A value, not a pointer: Wails sends what it is given, and the
		// frontend reads the fields.
		application.Get().Event.Emit(live.Event, u)
	})
})

// Subscribe starts a live query and returns its first result. client
// identifies one page load of one window; id, one query in it.
func (Live) Subscribe(client, id, query string, args []any) (live.Snapshot, error) {
	e, err := engine()
	if err != nil {
		return live.Snapshot{}, err
	}
	return e.Subscribe(client, id, query, args)
}

// Unsubscribe ends a live query.
func (Live) Unsubscribe(client, id string) {
	if e, err := engine(); err == nil {
		e.Unsubscribe(client, id)
	}
}

// Keep says the client is still there. False means its queries were dropped
// and it must subscribe again.
func (Live) Keep(client string) bool {
	e, err := engine()
	return err == nil && e.Keep(client)
}
