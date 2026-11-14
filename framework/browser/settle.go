package browser

import (
	"context"
	"sync"
	"time"

	"github.com/chromedp/cdproto/cdp"
	"github.com/chromedp/cdproto/network"
)

// A page goes on working after it loads: its scripts fetch data, and renew
// a token, such as a refresh token that works once. Code that runs at the
// same moment can race the page, and lose: a site can sign you out
// everywhere when its refresh token is used twice. Settle waits for the page
// to finish first.

const (
	// quietFor is how long a tab has had no request start or end when it
	// counts as settled.
	quietFor = 500 * time.Millisecond
	// settlePatience is the longest Settle waits, for a page that never goes
	// quiet, such as one that polls.
	settlePatience = 5 * time.Second
	// longLived is the age past which a request still open counts as a
	// stream, such as a long poll, rather than as work under way.
	longLived = 10 * time.Second
)

// activity is a tab's open requests, and when one last started or ended.
type activity struct {
	mu   sync.Mutex
	open map[network.RequestID]time.Time
	last time.Time
}

// note keeps the tab's activity, from its network events. Streams that stay
// open by design, an EventSource or a WebSocket, are left out, and so are the
// pages of its frames. main is the tab's page's frame. It runs in Chromium's
// event handler.
func (a *activity) note(ev any, main cdp.FrameID) {
	now := time.Now()
	a.mu.Lock()
	defer a.mu.Unlock()
	switch e := ev.(type) {
	case *network.EventRequestWillBeSent:
		// A frame from another site loads in a process of its own, which
		// reports the end of the frame's page, not the tab.
		frame := e.Type == network.ResourceTypeDocument && e.FrameID != main
		if e.Type != network.ResourceTypeEventSource && e.Type != network.ResourceTypeWebSocket && !frame {
			a.open[e.RequestID] = now
		}
		// A new page: the last page's requests are its no more, and
		// Chromium need not say when they end. One whose body the page
		// never read, as a check that looks only at the status, would hold
		// Settle for good.
		if e.Type == network.ResourceTypeDocument && e.FrameID == main {
			clear(a.open)
			a.open[e.RequestID] = now
		}
	case *network.EventLoadingFinished:
		delete(a.open, e.RequestID)
	case *network.EventLoadingFailed:
		delete(a.open, e.RequestID)
	default:
		return
	}
	a.last = now
}

// settled reports whether no request is under way and none started or ended
// in the last quietFor. It forgets requests open longer than longLived.
func (a *activity) settled(now time.Time) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	busy := false
	for id, started := range a.open {
		if now.Sub(started) >= longLived {
			// A stream, or a request whose end Chromium never reported.
			delete(a.open, id)
		} else {
			busy = true
		}
	}
	if busy {
		return false
	}
	return now.Sub(a.last) >= quietFor
}

// Settle waits until the page has settled, or for settlePatience at most.
// A page that settled long ago returns at once.
func (t *Tab) Settle(ctx context.Context) error {
	deadline := time.Now().Add(settlePatience)
	for !t.net.settled(time.Now()) && time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-t.ctx.Done():
			return nil
		case <-time.After(100 * time.Millisecond):
		}
	}
	return nil
}
