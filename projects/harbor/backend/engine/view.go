package engine

import (
	"context"
	"errors"
	"sync"

	"github.com/michaelckearney/monorepo/framework/browser"
)

// While you sign in, or look around, the service's tab is shown in Harbor's
// window: the window shows the tab, and the tabs it opens, live (Slipway's
// LiveView), with what Harbor asks you beside the page (prompt.go).

// ErrNoView is returned by the live view's methods when Harbor's window
// shows no live view of the service.
var ErrNoView = errors.New("Harbor's window does not show that service")

// view is one sign-in's live view.
type view struct {
	svc    Service
	tab    *browser.Tab // the service's tab
	ctx    context.Context
	cancel context.CancelFunc
	done   <-chan struct{} // closed when the view ends

	// turn is held while a prompt is shown, so one shows at a time.
	turn sync.Mutex

	// show shows a prompt in Harbor's window, or none for nil.
	show func(*Prompt)

	mu        sync.Mutex
	cancelled bool
	prompt    *Prompt     // the prompt shown, if any
	asked     int         // the last prompt's ID
	answer    chan Answer // where the shown prompt's answer goes
	// seen is what you typed into username and password fields here, by
	// site (saved.go).
	seen map[string]Login
}

func newView(ctx context.Context, svc Service, tab *browser.Tab, cancel context.CancelFunc, show func(*Prompt)) *view {
	return &view{svc: svc, tab: tab, ctx: ctx, cancel: cancel, done: ctx.Done(), show: show, seen: map[string]Login{}}
}

// ended reports whether you closed the view, or it was shown for as long
// as one may be.
func (v *view) ended() bool {
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.cancelled || errors.Is(v.ctx.Err(), context.DeadlineExceeded)
}

// shows reports whether t is the service's tab, or one it opened.
func (v *view) shows(t *browser.Tab) bool {
	for ; t != nil; t = t.Opener() {
		if t == v.tab {
			return true
		}
	}
	return false
}

func (e *Engine) view(name string) (*view, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	v := e.views[name]
	if v == nil {
		return nil, ErrNoView
	}
	return v, nil
}

// viewOf is the live view that shows t, if any.
func (e *Engine) viewOf(t *browser.Tab) *view {
	e.mu.Lock()
	defer e.mu.Unlock()
	for _, v := range e.views {
		if v.shows(t) {
			return v
		}
	}
	return nil
}

// Shown is what the live view of the named service shows: the service's tab,
// in which you sign in, and the prompt beside it, if any.
func (e *Engine) Shown(name string) (*browser.Tab, *Prompt, error) {
	v, err := e.view(name)
	if err != nil {
		return nil, nil, err
	}
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.prompt == nil {
		return v.tab, nil, nil
	}
	p := *v.prompt
	return v.tab, &p, nil
}

// CloseView closes the live view of the named service: a sign-in ends
// signed out, and looking around ends.
func (e *Engine) CloseView(name string) error {
	v, err := e.view(name)
	if err != nil {
		return err
	}
	v.mu.Lock()
	v.cancelled = true
	v.mu.Unlock()
	v.cancel()
	return nil
}
