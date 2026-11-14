package browser

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"

	"github.com/michaelckearney/monorepo/framework/internal/services"
)

// The backend of useTab and LiveView, in the frontend's @desktop module. The
// frontend calls Tabs by name, so its package path, type, and method names
// are part of the contract with that module. Frames and changes to a tab go
// to the frontend as Wails events, so no call stays open: WebKit lets the
// app's window have only about six calls to Go open at once.

func init() { services.Add(application.NewService(live)) }

// live is the service.
var live = &Tabs{}

// The events the frontend listens for.
const (
	tabEvent    = "desktop:browser:tab"
	frameEvent  = "desktop:browser:frame"
	cursorEvent = "desktop:browser:cursor"
	menuEvent   = "desktop:browser:menu"
)

// menuName names the native menu a right-click on the page opens.
const menuName = "desktop:browser:page-menu"

// The largest scale LiveView draws a page at: a Retina screen's. More would
// only make each frame bigger.
const maxScale = 2

// registry is every open tab of every browser, by ID, for the frontend.
var registry sync.Map

// register lists t for the frontend until it closes.
func register(t *Tab) {
	registry.Store(t.ID(), t)
	go func() {
		<-t.Done()
		registry.CompareAndDelete(t.ID(), t)
	}()
}

func lookup(id string) (*Tab, error) {
	if t, ok := registry.Load(id); ok {
		return t.(*Tab), nil
	}
	return nil, errors.New("the tab is closed")
}

// TabState is a tab as useTab has it.
type TabState struct {
	ID string `json:"id"`
	State
	// Opened are the IDs of the tabs it opened, oldest first.
	Opened []string `json:"opened"`
	Closed bool     `json:"closed"`
}

func stateOf(t *Tab) TabState {
	s := TabState{ID: t.ID(), State: t.State(), Opened: []string{}}
	for _, o := range t.Opened() {
		s.Opened = append(s.Opened, o.ID())
	}
	select {
	case <-t.Done():
		s.Closed = true
	default:
	}
	return s
}

// FrameEvent is a frame of the tab LiveView shows.
type FrameEvent struct {
	Tab string `json:"tab"`
	Frame
}

// Tabs lets the frontend follow and use the browser's tabs.
type Tabs struct {
	mu sync.Mutex
	// watching counts the frontend's watchers of each tab, by ID, and
	// stops sending its changes when the last one goes.
	watching map[string]*watch
	// shown is the tab LiveView streams; stopShown stops it, and
	// shownDone is closed once it has stopped, and the tab is back to its
	// own size.
	shown     string
	stopShown context.CancelFunc
	shownDone chan struct{}
	// cursor is the one the shown tab last sent, and menu what is under
	// its last right-click, for OpenMenu.
	cursor string
	menu   *PageMenu
}

type watch struct {
	count int
	stop  context.CancelFunc
}

// Watch returns the tab's state, and sends each change to it as an event
// until Unwatch. A closed tab says so.
func (s *Tabs) Watch(id string) TabState {
	t, err := lookup(id)
	if err != nil {
		return TabState{ID: id, Opened: []string{}, Closed: true}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.watching == nil {
		s.watching = map[string]*watch{}
	}
	if w := s.watching[id]; w != nil {
		w.count++
		return stateOf(t)
	}
	ctx, stop := context.WithCancel(context.Background())
	s.watching[id] = &watch{count: 1, stop: stop}
	changed := t.Changed()
	go func() {
		for {
			select {
			case <-changed:
			case <-ctx.Done():
				return
			}
			changed = t.Changed()
			application.Get().Event.Emit(tabEvent, stateOf(t))
			select {
			case <-t.Done():
				return
			default:
			}
		}
	}()
	return stateOf(t)
}

// Unwatch ends one Watch of the tab.
func (s *Tabs) Unwatch(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	w := s.watching[id]
	if w == nil {
		return
	}
	if w.count--; w.count == 0 {
		w.stop()
		delete(s.watching, id)
	}
}

// Show streams the tab as events, laid out at width by height CSS pixels and
// drawn at scale, until Hide, or Show of another tab: headless Chromium
// paints only one tab at a time.
func (s *Tabs) Show(id string, width, height, scale float64) error {
	t, err := lookup(id)
	if err != nil {
		return err
	}
	if width < 1 || height < 1 {
		return errors.New("LiveView has no size to show the tab at")
	}
	scale = min(max(scale, 1), maxScale)
	s.mu.Lock()
	defer s.mu.Unlock()
	// The last one's ending puts its tab back to its own size, which would
	// undo this one's size if it came after: as when LiveView is resized.
	s.stop()
	ctx, stop := context.WithCancel(context.Background())
	done := make(chan struct{})
	s.shown, s.stopShown, s.shownDone = id, stop, done
	s.cursor, s.menu = "", nil
	go func() {
		defer close(done)
		_ = t.screencast(ctx, width, height, scale, func(f Frame) {
			application.Get().Event.Emit(frameEvent, FrameEvent{Tab: id, Frame: f})
		})
	}()
	return nil
}

// Hide stops streaming the tab, if it is the one shown.
func (s *Tabs) Hide(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.shown == id {
		s.stop()
	}
}

// stop stops the tab shown, if any, and waits for it. The page gets back
// the focus Focus took, for whatever uses the tab next. s.mu is held.
func (s *Tabs) stop() {
	if s.stopShown == nil {
		return
	}
	s.stopShown()
	<-s.shownDone
	if t, err := lookup(s.shown); err == nil {
		go func() {
			ctx, cancel := context.WithTimeout(t.ctx, 2*time.Second)
			defer cancel()
			_ = t.setFocused(ctx, true)
		}()
	}
	s.shown, s.stopShown, s.shownDone = "", nil, nil
	s.cursor, s.menu = "", nil
}

// CursorEvent is the cursor the mouse has over the tab LiveView shows: a
// CSS cursor, such as pointer or text.
type CursorEvent struct {
	Tab    string `json:"tab"`
	Cursor string `json:"cursor"`
}

// heard takes what the shown tab's script says (live.go): a cursor goes to
// LiveView, a copy to your clipboard, and a right-click to LiveView, which
// opens the menu where you clicked. A tab not shown is not yours to use, so
// what it says goes nowhere: a page the app drives does not write to your
// clipboard.
func (s *Tabs) heard(t *Tab, c liveCall) {
	s.mu.Lock()
	if s.shown != t.ID() {
		s.mu.Unlock()
		return
	}
	cursorChanged := c.Cursor != nil && *c.Cursor != s.cursor
	if cursorChanged {
		s.cursor = *c.Cursor
	}
	if c.Menu != nil {
		s.menu = c.Menu
	}
	s.mu.Unlock()
	app := application.Get()
	switch {
	case cursorChanged:
		app.Event.Emit(cursorEvent, CursorEvent{Tab: t.ID(), Cursor: *c.Cursor})
	case c.Copy != nil:
		app.Clipboard.SetText(*c.Copy)
	case c.Menu != nil:
		app.Event.Emit(menuEvent, t.ID())
	}
}

// Focus tells the tab whether you are using it: LiveView's says when it
// gets your keys, and when they go elsewhere in the app.
func (s *Tabs) Focus(ctx context.Context, id string, focused bool) error {
	t, err := lookup(id)
	if err != nil {
		return err
	}
	return t.setFocused(ctx, focused)
}

// OpenMenu opens the browser's menu for the tab's last right-click, at x, y
// in the window that called: what a browser offers there, for a link,
// selected text, a field, or the page.
func (s *Tabs) OpenMenu(ctx context.Context, id string, x, y int) error {
	t, err := lookup(id)
	if err != nil {
		return err
	}
	s.mu.Lock()
	m := s.menu
	s.menu = nil
	s.mu.Unlock()
	window, ok := ctx.Value(application.WindowKey).(application.Window)
	if m == nil || !ok {
		return nil
	}
	app := application.Get()
	do := func(fn func(context.Context) error) func(*application.Context) {
		return func(*application.Context) {
			ctx, cancel := context.WithTimeout(t.ctx, inputPatience)
			defer cancel()
			_ = fn(ctx)
		}
	}
	copyText := func(text string) func(context.Context) error {
		return func(context.Context) error { app.Clipboard.SetText(text); return nil }
	}
	menu := application.NewContextMenu(menuName)
	if m.Link != "" {
		menu.Add("Copy Link Address").OnClick(do(copyText(m.Link)))
		menu.AddSeparator()
	}
	switch {
	case m.Editable:
		menu.Add("Cut").SetEnabled(m.Selection != "").OnClick(do(func(ctx context.Context) error {
			app.Clipboard.SetText(m.Selection)
			return t.send(ctx, keyPress("Backspace", "Backspace", 8, 0, nil)...)
		}))
		menu.Add("Copy").SetEnabled(m.Selection != "").OnClick(do(copyText(m.Selection)))
		text, _ := app.Clipboard.Text()
		menu.Add("Paste").SetEnabled(text != "").OnClick(do(func(ctx context.Context) error {
			text, _ := app.Clipboard.Text()
			return t.send(ctx, InputEvent{Kind: "text", Text: text})
		}))
		menu.AddSeparator()
		menu.Add("Select All").OnClick(do(func(ctx context.Context) error {
			return t.send(ctx, keyPress("a", "KeyA", 65, metaKey, []string{"selectAll"})...)
		}))
	case m.Selection != "":
		menu.Add("Copy").OnClick(do(copyText(m.Selection)))
	case m.Link == "":
		state := t.State()
		menu.Add("Back").SetEnabled(state.CanGoBack).OnClick(do(t.Back))
		menu.Add("Forward").SetEnabled(state.CanGoForward).OnClick(do(t.Forward))
		menu.Add("Reload").OnClick(do(t.Reload))
	}
	menu.Update()
	window.OpenContextMenu(&application.ContextMenuData{Id: menuName, X: x, Y: y})
	return nil
}

// metaKey is Command's bit in InputEvent.Modifiers.
const metaKey = 4

// keyPress is a key pressed and let go.
func keyPress(key, code string, keyCode, modifiers int64, commands []string) []InputEvent {
	return []InputEvent{
		{Kind: "key", Type: "rawKeyDown", Key: key, Code: code, KeyCode: keyCode, Modifiers: modifiers, Commands: commands},
		{Kind: "key", Type: "keyUp", Key: key, Code: code, KeyCode: keyCode, Modifiers: modifiers},
	}
}

// Input sends your mouse, key, or pasted text to the tab.
func (s *Tabs) Input(ctx context.Context, id string, ev InputEvent) error {
	t, err := lookup(id)
	if err != nil {
		return err
	}
	return t.Input(ctx, ev)
}

// Navigate starts loading url in the tab, as an address bar does.
func (s *Tabs) Navigate(ctx context.Context, id, url string) error {
	t, err := lookup(id)
	if err != nil {
		return err
	}
	return t.start(ctx, url)
}

// Back goes back a page.
func (s *Tabs) Back(ctx context.Context, id string) error {
	t, err := lookup(id)
	if err != nil {
		return err
	}
	return t.Back(ctx)
}

// Forward goes forward a page.
func (s *Tabs) Forward(ctx context.Context, id string) error {
	t, err := lookup(id)
	if err != nil {
		return err
	}
	return t.Forward(ctx)
}

// Reload loads the page again.
func (s *Tabs) Reload(ctx context.Context, id string) error {
	t, err := lookup(id)
	if err != nil {
		return err
	}
	return t.Reload(ctx)
}

// Close closes the tab.
func (s *Tabs) Close(id string) {
	if t, err := lookup(id); err == nil {
		t.Close()
	}
}
