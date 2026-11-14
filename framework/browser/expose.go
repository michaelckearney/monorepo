package browser

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

	"github.com/chromedp/cdproto/cdp"
	"github.com/chromedp/cdproto/page"
	"github.com/chromedp/cdproto/runtime"
	"github.com/chromedp/chromedp"
)

// A tab's script contexts: the page's, each frame's, and those extensions
// make beside them, each with the origin of its site. Go functions the app
// exposes to the page are called from them, and any script on the page can
// call one, so the browser never takes the page's word for which site it
// is: it asks Chromium which site the calling script belongs to.

// scripts are a tab's script contexts, and the functions it exposes.
type scripts struct {
	mu       sync.Mutex
	contexts map[runtime.ExecutionContextID]string // script context to origin
	// pages are the contexts that are a frame's page, rather than one an
	// extension made beside it, and the frame each is: those frames.go
	// runs code in.
	pages map[runtime.ExecutionContextID]cdp.FrameID
	// live are the contexts of LiveView's script, in its own world
	// (live.go).
	live    map[runtime.ExecutionContextID]bool
	exposed map[string]ExposedFunc
}

// ExposedFunc answers a page's call to a function Expose added. origin is
// the site of the script that called, such as https://example.com, as
// Chromium knows it; arg is the value the page passed.
type ExposedFunc func(ctx context.Context, origin string, arg json.RawMessage) (any, error)

// bindingPrefix names the DevTools binding behind each exposed function.
const bindingPrefix = "__desktopBinding_"

// setUp turns on the events note reads.
func (s *scripts) setUp() chromedp.Action {
	return runtime.Enable()
}

// note keeps the tab's script contexts, and answers calls to exposed
// functions. It runs in Chromium's event handler.
func (s *scripts) note(t *Tab, ev any) {
	switch e := ev.(type) {
	case *runtime.EventExecutionContextCreated:
		var aux struct {
			IsDefault bool        `json:"isDefault"`
			FrameID   cdp.FrameID `json:"frameId"`
		}
		_ = json.Unmarshal(e.Context.AuxData, &aux)
		s.mu.Lock()
		s.contexts[e.Context.ID] = e.Context.Origin
		if aux.IsDefault {
			s.pages[e.Context.ID] = aux.FrameID
		} else if e.Context.Name == liveWorld {
			s.live[e.Context.ID] = true
		}
		s.mu.Unlock()
	case *runtime.EventExecutionContextDestroyed:
		s.mu.Lock()
		delete(s.contexts, e.ExecutionContextID)
		delete(s.pages, e.ExecutionContextID)
		delete(s.live, e.ExecutionContextID)
		s.mu.Unlock()
	case *runtime.EventExecutionContextsCleared:
		s.mu.Lock()
		clear(s.contexts)
		clear(s.pages)
		clear(s.live)
		s.mu.Unlock()
	case *runtime.EventBindingCalled:
		if e.Name == liveBinding {
			s.mu.Lock()
			ours := s.live[e.ExecutionContextID]
			s.mu.Unlock()
			if ours {
				t.heardLive(e.Payload)
			}
			return
		}
		s.mu.Lock()
		fn := s.exposed[e.Name]
		s.mu.Unlock()
		if fn != nil {
			// Calls to Chromium from inside its event handler deadlock.
			go t.answer(fn, e.ExecutionContextID, e.Payload)
		}
	}
}

func (s *scripts) origin(id runtime.ExecutionContextID) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.contexts[id]
}

// Expose adds window[name] to the page, and to every page the tab loads from
// now on, and to the frames in each that run in its process. The page calls
// it with a value JSON can carry, and gets a promise: of what fn returns, or
// rejected with an Error of fn's error's message.
func (t *Tab) Expose(ctx context.Context, name string, fn ExposedFunc) error {
	s := &t.scripts
	binding := bindingPrefix + name
	t.scripts.mu.Lock()
	if t.scripts.exposed == nil {
		t.scripts.exposed = map[string]ExposedFunc{}
	}
	t.scripts.exposed[binding] = fn
	t.scripts.mu.Unlock()
	script := fmt.Sprintf(exposeScript, binding, name)
	return t.run(ctx, runtime.AddBinding(binding), chromedp.ActionFunc(func(ctx context.Context) error {
		_, err := page.AddScriptToEvaluateOnNewDocument(script).Do(ctx)
		return err
	}), s.inPages(script))
}

// inPages runs script in each page and frame the tab shows now, as
// AddScriptToEvaluateOnNewDocument does in those it shows later. A page that
// goes meanwhile is skipped.
func (s *scripts) inPages(script string) chromedp.Action {
	return chromedp.ActionFunc(func(ctx context.Context) error {
		s.mu.Lock()
		ids := make([]runtime.ExecutionContextID, 0, len(s.pages))
		for id := range s.pages {
			ids = append(ids, id)
		}
		s.mu.Unlock()
		for _, id := range ids {
			_, _, _ = runtime.Evaluate(script).WithContextID(id).Do(ctx)
		}
		return nil
	})
}

// exposeScript wraps the binding, which only sends a string, in a function
// that returns a promise of the answer.
const exposeScript = `(() => {
	const binding = globalThis[%[1]q];
	const replies = (globalThis.__desktopReplies ??= {});
	let next = 0;
	globalThis[%[2]q] = (arg) => new Promise((resolve, reject) => {
		const id = %[2]q + ":" + ++next;
		replies[id] = (ok, value) => {
			delete replies[id];
			ok ? resolve(value) : reject(new Error(value));
		};
		binding(JSON.stringify({ id, arg: arg === undefined ? null : arg }));
	});
})()`

// answer runs fn for one call, and gives the page its answer, in the script
// context that called.
func (t *Tab) answer(fn ExposedFunc, id runtime.ExecutionContextID, payload string) {
	var call struct {
		ID  string          `json:"id"`
		Arg json.RawMessage `json:"arg"`
	}
	if json.Unmarshal([]byte(payload), &call) != nil {
		return
	}
	ok, value := true, any(nil)
	if v, err := fn(t.ctx, t.scripts.origin(id), call.Arg); err != nil {
		ok, value = false, err.Error()
	} else {
		value = v
	}
	reply, err := json.Marshal([]any{call.ID, ok, value})
	if err != nil {
		reply, _ = json.Marshal([]any{call.ID, false, "the answer is not JSON: " + err.Error()})
	}
	expr := fmt.Sprintf("((id, ok, value) => globalThis.__desktopReplies?.[id]?.(ok, value))(...%s)", reply)
	_ = t.run(t.ctx, chromedp.ActionFunc(func(ctx context.Context) error {
		_, _, err := runtime.Evaluate(expr).WithContextID(id).Do(ctx)
		return err
	}))
}
