// Package services holds the Wails services that desktop's own optional
// packages add, such as package browser's: the generated main package adds
// them to the backend's. A package registers its service when it is
// imported, so an app that does not import it does not link it.
package services

import (
	"sync"

	"github.com/wailsapp/wails/v3/pkg/application"
)

var (
	mu    sync.Mutex
	added []application.Service
)

// Add registers a service. Call it from an init function.
func Add(s application.Service) {
	mu.Lock()
	defer mu.Unlock()
	added = append(added, s)
}

// All are the services registered.
func All() []application.Service {
	mu.Lock()
	defer mu.Unlock()
	return append([]application.Service{}, added...)
}
