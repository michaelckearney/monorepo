// Package desktop is the only Go package a desktop backend imports.
//
// It exists so that a backend never imports Wails directly. The Wails version
// is desktop's choice, and a backend that named Wails types would have to
// change whenever desktop moved to a newer one.
package desktop

import (
	"github.com/wailsapp/wails/v3/pkg/application"

	"github.com/michaelckearney/monorepo/framework/internal/pin"
)

// WailsVersion is the Wails release this version of desktop is built against.
const WailsVersion = pin.Wails

// Service is a value the frontend can call methods on.
type Service = application.Service

// NewService makes the exported methods of service callable from the
// frontend, through the generated `@backend` module.
//
// It has to be generic, and the argument has to be a pointer to a concrete
// type. Wails finds services by reading the code, not by running it: it
// follows the type parameter from here back to each call site. A backend that
// built its services from values typed `any` would compile and then produce no
// bindings at all.
func NewService[T any](service *T) Service {
	return application.NewService(service)
}
