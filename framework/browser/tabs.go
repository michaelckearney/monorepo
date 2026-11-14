package browser

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"time"
	"unicode/utf16"

	cdpbrowser "github.com/chromedp/cdproto/browser"
	"github.com/chromedp/cdproto/cdp"
	"github.com/chromedp/cdproto/domstorage"
	"github.com/chromedp/cdproto/emulation"
	"github.com/chromedp/cdproto/input"
	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/cdproto/page"
	"github.com/chromedp/cdproto/runtime"
	"github.com/chromedp/cdproto/target"
	"github.com/chromedp/chromedp"
)

// Tabs, as a browser window has them: where each is, the tabs it opened,
// and moving around in it. And a tab shown live, for LiveView: what it
// shows, streamed, and your mouse and keys sent back.

// State is where a tab is.
type State struct {
	URL          string `json:"url"`
	Title        string `json:"title"`
	Loading      bool   `json:"loading"`
	CanGoBack    bool   `json:"canGoBack"`
	CanGoForward bool   `json:"canGoForward"`
}

// State is where the tab is now.
func (t *Tab) State() State {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.state
}

// Changed is closed when State or Opened changes, or the tab closes. Call it
// again for the next change.
func (t *Tab) Changed() <-chan struct{} {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.changed
}

// signal wakes whoever waits on Changed, for t and the tabs that opened it:
// what they opened has changed too.
func (t *Tab) signal() {
	for ; t != nil; t = t.opener {
		t.mu.Lock()
		close(t.changed)
		t.changed = make(chan struct{})
		t.mu.Unlock()
	}
}

// Opened are the open tabs that t opened, directly or through another, such
// as a pop-up or a link with target=_blank, oldest first.
func (t *Tab) Opened() []*Tab {
	var all []*Tab
	var walk func(*Tab)
	walk = func(t *Tab) {
		t.mu.Lock()
		children := slices.Clone(t.children)
		t.mu.Unlock()
		for _, c := range children {
			all = append(all, c)
			walk(c)
		}
	}
	walk(t)
	slices.SortStableFunc(all, func(a, b *Tab) int { return a.opened.Compare(b.opened) })
	return all
}

// Opener is the tab that opened t, or nil for a tab the app opened.
func (t *Tab) Opener() *Tab { return t.opener }

// onBrowserEvent follows the tabs Chromium opens and closes, and its
// downloads. Every call to Chromium happens in a goroutine: one made from
// inside its event handler deadlocks.
func (b *Browser) onBrowserEvent(p *process) func(any) {
	return func(ev any) {
		switch e := ev.(type) {
		case *target.EventTargetCreated:
			if e.TargetInfo.Type == "page" && e.TargetInfo.OpenerID != "" {
				go b.attachOpened(p, e.TargetInfo.TargetID, e.TargetInfo.OpenerID)
			}
		case *target.EventTargetInfoChanged:
			p.mu.Lock()
			t := p.tabs[e.TargetInfo.TargetID]
			p.mu.Unlock()
			if t != nil {
				t.update(func(s *State) { s.URL, s.Title = e.TargetInfo.URL, e.TargetInfo.Title })
			}
		case *target.EventTargetDestroyed:
			go p.forget(e.TargetID)
		case *cdpbrowser.EventDownloadWillBegin:
			b.downloadBegins(p, e)
		case *cdpbrowser.EventDownloadProgress:
			if e.State == cdpbrowser.DownloadProgressStateCompleted || e.State == cdpbrowser.DownloadProgressStateCanceled {
				go b.downloadEnds(p, e)
			}
		}
	}
}

// attachOpened takes on a tab that a known tab opened.
func (b *Browser) attachOpened(p *process, id, opener target.ID) {
	p.mu.Lock()
	parent := p.tabs[opener]
	p.mu.Unlock()
	if parent == nil {
		return
	}
	ctx, cancel := chromedp.NewContext(p.ctx, chromedp.WithTargetID(id))
	if err := chromedp.Run(ctx); err != nil {
		cancel()
		return
	}
	t := b.newTab(ctx, cancel, parent)
	b.opened()
	if err := t.setUp(ctx); err != nil {
		cancel()
		return
	}
	p.mu.Lock()
	p.tabs[id] = t
	p.mu.Unlock()
	parent.mu.Lock()
	parent.children = append(parent.children, t)
	parent.mu.Unlock()
	parent.signal()
	// Its page may have loaded before the tab was set up to hear of it.
	t.refresh()
}

// forget drops a tab Chromium closed, which ends its context.
func (p *process) forget(id target.ID) {
	p.mu.Lock()
	t := p.tabs[id]
	delete(p.tabs, id)
	p.mu.Unlock()
	if t == nil {
		return
	}
	t.cancel()
	if p := t.opener; p != nil {
		p.mu.Lock()
		p.children = slices.DeleteFunc(p.children, func(c *Tab) bool { return c == t })
		p.mu.Unlock()
	}
	t.signal()
}

// setUp follows what happens in a new tab: its network, its script
// contexts, where it is, and what its pages ask for.
func (t *Tab) setUp(ctx context.Context) error {
	t.net.open = map[network.RequestID]time.Time{}
	t.scripts.contexts = map[runtime.ExecutionContextID]string{}
	t.scripts.pages = map[runtime.ExecutionContextID]cdp.FrameID{}
	t.scripts.live = map[runtime.ExecutionContextID]bool{}
	main := cdp.FrameID(t.id)
	chromedp.ListenTarget(t.ctx, func(ev any) {
		t.net.note(ev, main)
		t.requests.note(ev, main, &t.seq)
		t.saved.note(ev, &t.seq)
		t.scripts.note(t, ev)
		switch e := ev.(type) {
		case *page.EventFrameNavigated:
			if e.Frame.ParentID == "" {
				go t.refresh()
			}
		case *page.EventNavigatedWithinDocument:
			if e.FrameID == main {
				go t.refresh()
			}
		case *page.EventFrameStartedLoading:
			if e.FrameID == main {
				t.update(func(s *State) { s.Loading = true })
			}
		case *page.EventFrameStoppedLoading:
			if e.FrameID == main {
				t.update(func(s *State) { s.Loading = false })
				go t.refresh()
			}
		case *page.EventJavascriptDialogOpening:
			t.dialogShown(true)
			go t.dialog(e)
		case *page.EventJavascriptDialogClosed:
			t.dialogShown(false)
		case *page.EventFileChooserOpened:
			go t.chooseFiles(e)
		}
	})
	go func() {
		<-t.ctx.Done()
		t.signal()
	}()
	actions := []chromedp.Action{network.Enable(), domstorage.Enable(), t.scripts.setUp(), t.scripts.setUpLive()}
	if t.browser.opts.ChooseFiles != nil {
		// Otherwise headless Chromium shows nothing, and the page gets no
		// files.
		actions = append(actions, page.SetInterceptFileChooserDialog(true))
	}
	if err := t.run(ctx, actions...); err != nil {
		return err
	}
	register(t)
	return nil
}

// update changes the tab's state, and says so if it changed.
func (t *Tab) update(fn func(*State)) {
	t.mu.Lock()
	before := t.state
	fn(&t.state)
	same := t.state == before
	t.mu.Unlock()
	if !same {
		t.signal()
	}
}

// refresh reads where the tab is: its address and title, as Chromium has
// them, and whether it can go back and forward. Chromium tells of a new
// address (onBrowserEvent), but not always of the title a page loads with.
func (t *Tab) refresh() {
	current, entries, err := t.history(t.ctx)
	if err != nil {
		return
	}
	var info *target.Info
	err = t.run(t.ctx, chromedp.ActionFunc(func(ctx context.Context) error {
		var err error
		info, err = target.GetTargetInfo().WithTargetID(t.id).Do(cdp.WithExecutor(ctx, chromedp.FromContext(ctx).Browser))
		return err
	}))
	if err != nil {
		return
	}
	t.update(func(s *State) {
		s.URL, s.Title = info.URL, info.Title
		s.CanGoBack = current > 0
		s.CanGoForward = current < int64(len(entries))-1
	})
}

func (t *Tab) history(ctx context.Context) (int64, []*page.NavigationEntry, error) {
	var current int64
	var entries []*page.NavigationEntry
	err := t.run(ctx, chromedp.ActionFunc(func(ctx context.Context) error {
		var err error
		current, entries, err = page.GetNavigationHistory().Do(ctx)
		return err
	}))
	if err == nil && (current < 0 || current >= int64(len(entries))) {
		err = errors.New("Chromium gave the tab's history without its place in it")
	}
	return current, entries, err
}

// Back starts going back a page, as a browser's back button does. Settle
// waits for the page.
func (t *Tab) Back(ctx context.Context) error { return t.step(ctx, -1) }

// Forward starts going forward a page.
func (t *Tab) Forward(ctx context.Context) error { return t.step(ctx, 1) }

func (t *Tab) step(ctx context.Context, by int64) error {
	current, entries, err := t.history(ctx)
	if err != nil {
		return err
	}
	to := current + by
	if to < 0 || to >= int64(len(entries)) {
		if by < 0 {
			return errors.New("the tab has no page to go back to")
		}
		return errors.New("the tab has no page to go forward to")
	}
	return t.run(ctx, page.NavigateToHistoryEntry(entries[to].ID))
}

// Reload starts loading the page again.
func (t *Tab) Reload(ctx context.Context) error {
	return t.run(ctx, page.Reload())
}

// start starts loading url, without waiting for it, as an address bar does.
func (t *Tab) start(ctx context.Context, url string) error {
	return t.run(ctx, chromedp.ActionFunc(func(ctx context.Context) error {
		_, _, errorText, _, err := page.Navigate(url).Do(ctx)
		if err == nil && errorText != "" {
			err = fmt.Errorf("could not open %s: %s", url, errorText)
		}
		return err
	}))
}

// Screenshot is a JPEG of what the tab shows: its window, or the whole page
// if full is set. The tab comes to the front, as headless Chromium paints
// only the tab there.
func (t *Tab) Screenshot(ctx context.Context, full bool) ([]byte, error) {
	if err := t.toFront(ctx); err != nil {
		return nil, err
	}
	var shot []byte
	err := t.run(ctx, chromedp.ActionFunc(func(ctx context.Context) error {
		capture := page.CaptureScreenshot().WithFormat(page.CaptureScreenshotFormatJpeg).WithQuality(80)
		if full {
			_, _, _, _, _, content, err := page.GetLayoutMetrics().Do(ctx)
			if err != nil {
				return err
			}
			capture = capture.WithCaptureBeyondViewport(true).
				WithClip(&page.Viewport{Width: content.Width, Height: content.Height, Scale: 1})
		}
		var err error
		shot, err = capture.Do(ctx)
		return err
	}))
	return shot, err
}

// InputEvent is a mouse, key, or text event for a tab, as LiveView sends it.
// X and Y are CSS pixels of the page; Modifiers is Chromium's bit field (Alt
// 1, Ctrl 2, Meta 4, Shift 8).
type InputEvent struct {
	// Kind is "mouse", "key", "text", or "composition": text is typed as
	// one piece, as a paste is; composition is text an input method is
	// still composing, which the next text commits.
	Kind string `json:"kind"`
	// Type is, for a mouse, mousePressed, mouseReleased, mouseMoved, or
	// mouseWheel; for a key, keyDown, rawKeyDown, or keyUp.
	Type       string  `json:"type"`
	X          float64 `json:"x"`
	Y          float64 `json:"y"`
	DeltaX     float64 `json:"deltaX"`
	DeltaY     float64 `json:"deltaY"`
	Button     string  `json:"button"`
	ClickCount int64   `json:"clickCount"`
	Modifiers  int64   `json:"modifiers"`
	Key        string  `json:"key"`
	Code       string  `json:"code"`
	Text       string  `json:"text"`
	// KeyCode is the key's code in the browser, KeyboardEvent.keyCode.
	KeyCode  int64    `json:"keyCode"`
	Commands []string `json:"commands"`
}

// toFront brings the tab to the front, unless the browser put it there
// last. A page in a tab behind others has no focus: it does not focus its
// fields, and Chromium holds its passkey requests until it does.
func (t *Tab) toFront(ctx context.Context) error {
	b := t.browser
	b.frontMu.Lock()
	defer b.frontMu.Unlock()
	if b.front == t {
		return nil
	}
	if err := t.run(ctx, page.BringToFront()); err != nil {
		return err
	}
	b.front = t
	return nil
}

// Input sends ev to the tab, as if you used it: in front, as a tab you use
// is. It returns once the page has taken it, or once it opened a dialog,
// such as a click on a button that calls alert. While a dialog is open, it
// is ErrDialogOpen.
func (t *Tab) Input(ctx context.Context, ev InputEvent) error { return t.send(ctx, ev) }

func (t *Tab) input(ctx context.Context, ev InputEvent) error {
	select {
	case <-t.dialogIsUp():
		return ErrDialogOpen
	default:
	}
	if err := t.toFront(ctx); err != nil {
		return err
	}
	var action chromedp.Action
	switch ev.Kind {
	case "mouse":
		m := input.DispatchMouseEvent(input.MouseType(ev.Type), ev.X, ev.Y).WithModifiers(input.Modifier(ev.Modifiers))
		if ev.Type == string(input.MouseWheel) {
			m = m.WithDeltaX(ev.DeltaX).WithDeltaY(ev.DeltaY)
		} else if ev.Button != "" {
			m = m.WithButton(input.MouseButton(ev.Button)).WithClickCount(ev.ClickCount)
		}
		action = m
	case "key":
		// Not as the native key code too: a Mac numbers its keys another
		// way, and Chromium passed held Option's 18 on to macOS, which
		// opened System Information.
		k := input.DispatchKeyEvent(input.KeyType(ev.Type)).WithKey(ev.Key).WithCode(ev.Code).
			WithWindowsVirtualKeyCode(ev.KeyCode).WithModifiers(input.Modifier(ev.Modifiers))
		if ev.Text != "" {
			k = k.WithText(ev.Text).WithUnmodifiedText(ev.Text)
		}
		if len(ev.Commands) > 0 {
			k = k.WithCommands(ev.Commands)
		}
		action = k
	case "text":
		action = input.InsertText(ev.Text)
	case "composition":
		// Text an input method has not committed yet, such as the accent a
		// dead key starts, shown in the field as it is typed; text commits it.
		end := int64(len(utf16.Encode([]rune(ev.Text))))
		action = input.ImeSetComposition(ev.Text, end, end)
	default:
		return fmt.Errorf("%q is not an input kind; use mouse, key, text, or composition", ev.Kind)
	}
	// Chromium sometimes never answers an input, as while the tab goes to
	// another page; then every input after it, which waits its turn,
	// would wait for good.
	ctx, cancel := context.WithTimeout(ctx, inputPatience)
	defer cancel()
	up := t.dialogIsUp()
	done := make(chan error, 1)
	go func() { done <- t.run(ctx, action) }()
	select {
	case err := <-done:
		return err
	case <-up:
		// A click that opens an alert: Chromium answers the click only once
		// the alert is answered, and the page took it.
		return errInputOpenedDialog
	}
}

// ErrDialogOpen is Input's error while the page has a dialog open: the page
// takes nothing until the dialog is answered, through Options.Dialog.
var ErrDialogOpen = errors.New("the page has a dialog open: answer it first")

// errInputOpenedDialog is the input that opened a dialog: it went in.
var errInputOpenedDialog = errors.New("the input opened a dialog")

// send sends events to the tab in turn, as Input does each. Once one opens
// a dialog, send returns, as the page waits for the dialog's answer, and
// sends the rest once it is answered: a person lets go of the mouse after
// the alert a press opened, though the caller has moved on.
func (t *Tab) send(ctx context.Context, events ...InputEvent) error {
	select {
	case <-t.dialogIsUp():
		return ErrDialogOpen
	default:
	}
	select {
	case <-t.sending:
	case <-ctx.Done():
		return ctx.Err()
	}
	result := make(chan error, 1)
	waiting := make(chan struct{})
	go func() {
		defer func() { t.sending <- struct{}{} }()
		signalled := false
		for _, ev := range events {
			err := t.input(ctx, ev)
			if errors.Is(err, errInputOpenedDialog) {
				ctx = context.WithoutCancel(ctx)
				select {
				case <-t.dialogIsGone():
					// The Dialog hook answered it already.
				default:
					if !signalled {
						signalled = true
						close(waiting)
					}
				}
				select {
				case <-t.dialogIsGone():
					continue
				case <-t.ctx.Done():
					result <- nil
					return
				}
			}
			if err != nil {
				result <- err
				return
			}
		}
		result <- nil
	}()
	select {
	case err := <-result:
		return err
	case <-waiting:
		return nil
	}
}

// inputPatience is how long Input waits for Chromium to take an input.
const inputPatience = 5 * time.Second

// Frame is one image of what a tab shows: a JPEG, base64, and the size of the
// page in CSS pixels, which Input's coordinates are in.
type Frame struct {
	JPEG   string  `json:"jpeg"`
	Width  float64 `json:"width"`
	Height float64 `json:"height"`
}

// screencast lays the page out at width by height CSS pixels, drawn at
// scale, brings the tab to the front, and calls fn with each frame it
// paints, until ctx ends or the tab closes. Then the page goes back to the
// window's size. Headless Chromium paints only the front tab: one behind the
// others sends nothing. fn runs in Chromium's event handler, so it must not
// block.
func (t *Tab) screencast(ctx context.Context, width, height, scale float64, fn func(Frame)) error {
	listen, cancel := context.WithCancel(t.ctx)
	defer cancel()
	stop := context.AfterFunc(ctx, cancel)
	defer stop()
	chromedp.ListenTarget(listen, func(ev any) {
		e, ok := ev.(*page.EventScreencastFrame)
		if !ok {
			return
		}
		fn(Frame{JPEG: e.Data, Width: e.Metadata.DeviceWidth, Height: e.Metadata.DeviceHeight})
		// Chromium sends the next frame once this one is acknowledged.
		go chromedp.Run(listen, page.ScreencastFrameAck(e.SessionID))
	})
	// Chromium sends a frame only when the page changes; starting again
	// makes it send the one it shows now.
	if err := t.toFront(ctx); err != nil {
		return err
	}
	err := t.run(ctx,
		emulation.SetDeviceMetricsOverride(int64(width), int64(height), scale, false),
		page.StopScreencast(),
		page.StartScreencast().WithFormat(page.ScreencastFormatJpeg).WithQuality(90))
	if err != nil {
		return err
	}
	<-listen.Done()
	stopCtx, done := context.WithTimeout(t.ctx, time.Second)
	defer done()
	_ = chromedp.Run(stopCtx, page.StopScreencast(), emulation.ClearDeviceMetricsOverride())
	if ctx.Err() != nil {
		return ctx.Err()
	}
	return errors.New("the tab was closed")
}
