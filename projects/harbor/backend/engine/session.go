package engine

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sync/atomic"
	"time"

	"github.com/michaelckearney/monorepo/framework/browser"
	"github.com/michaelckearney/monorepo/projects/harbor/backend/service"
)

// A session is one service's tab, and what runs in it. Calls to one service
// take turns: the tab, and whatever page it is on, is shared.
type session struct {
	turn chan struct{} // holds one token; take it to use the tab
	tab  *browser.Tab
	// signingIn is the tab while a login.run runs in it: the only time its
	// page may have Harbor fill in a saved login, or use a passkey you let
	// it use without asking.
	signingIn atomic.Pointer[browser.Tab]
}

func newSession() *session {
	s := &session{turn: make(chan struct{}, 1)}
	s.turn <- struct{}{}
	return s
}

// take waits for the session's turn, or for ctx to end.
func (s *session) take(ctx context.Context) (release func(), err error) {
	select {
	case <-s.turn:
		return func() { s.turn <- struct{}{} }, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// tabFor returns the session's tab, opening one at the service's URL if it
// has none or the last one was closed, set up for saved logins (saved.go).
// The caller has the session's turn.
func (e *Engine) tabFor(ctx context.Context, s *session, svc Service) (*browser.Tab, error) {
	if s.tab != nil {
		select {
		case <-s.tab.Done():
		default:
			return s.tab, nil
		}
	}
	tab, err := e.browser.Open(ctx, svc.Manifest.URL)
	if err != nil {
		return nil, err
	}
	if err := e.setUpLogins(ctx, tab, s, svc.Manifest.Name); err != nil {
		tab.Close()
		return nil, err
	}
	s.tab = tab
	return tab, nil
}

// compiled is a service's file as it runs in a page, and its hash.
type compiled struct {
	js, hash string
}

func compile(svc Service) (compiled, error) {
	js, err := service.Compile(svc.Source)
	if err != nil {
		return compiled{}, fmt.Errorf("%s does not compile: %w", svc.Manifest.Name, err)
	}
	sum := sha256.Sum256([]byte(js))
	return compiled{js: js, hash: hex.EncodeToString(sum[:8])}, nil
}

// load puts the compiled service in the page as window.__harbor, unless the
// page has this version already.
func load(ctx context.Context, tab *browser.Tab, c compiled) error {
	_, err := tab.Evaluate(ctx, `(js, hash) => {
		(0, eval)(js);
		window.__harbor = { hash, service: `+service.Global+`.default };
	}`, c.js, c.hash)
	return err
}

// invoke calls the function at path in the service, such as
// "tools.list_issues.run", with args. A page that lost the service, because
// it navigated or is a new tab, has it loaded again first.
func invoke(ctx context.Context, tab *browser.Tab, c compiled, path string, args ...any) (json.RawMessage, error) {
	value, _, err := invokeWithFiles(ctx, tab, c, path, args...)
	return value, err
}

// call is invoke's half in the page. Files in args, which Harbor sends as
// { __harborFile: { name, type, data } }, arrive as File objects; files in
// what the function returns leave as { __harborFile: n }, with their bytes,
// in base64, beside the value (files.go).
const call = `async (hash, path, args) => {
	if (!window.__harbor || window.__harbor.hash !== hash) {
		return { missing: true };
	}
	const plain = (v) => v !== null && typeof v === "object" && Object.getPrototypeOf(v) === Object.prototype;
	const toFile = (v) => {
		if (Array.isArray(v)) return v.map(toFile);
		if (!plain(v)) return v;
		if (v.__harborFile) {
			const bytes = Uint8Array.from(atob(v.__harborFile.data), (c) => c.charCodeAt(0));
			return new File([bytes], v.__harborFile.name, { type: v.__harborFile.type });
		}
		return Object.fromEntries(Object.entries(v).map(([k, x]) => [k, toFile(x)]));
	};
	const files = [];
	const fromFile = async (v) => {
		if (v instanceof Blob) {
			const bytes = new Uint8Array(await v.arrayBuffer());
			let binary = "";
			for (let i = 0; i < bytes.length; i += 0x8000) {
				binary += String.fromCharCode(...bytes.subarray(i, i + 0x8000));
			}
			files.push({ name: v.name ?? "", type: v.type, data: btoa(binary) });
			return { __harborFile: files.length - 1 };
		}
		if (Array.isArray(v)) return Promise.all(v.map(fromFile));
		if (!plain(v)) return v;
		const out = {};
		for (const [k, x] of Object.entries(v)) out[k] = await fromFile(x);
		return out;
	};
	const fn = path.split(".").reduce((o, k) => o[k], window.__harbor.service);
	const value = await fromFile(await fn(...toFile(args)));
	return { value, files };
}`

// pageFile is a file a function returned, as it leaves the page.
type pageFile struct {
	Name string `json:"name"`
	Type string `json:"type"`
	Data []byte `json:"data"` // base64 in JSON
}

func invokeWithFiles(ctx context.Context, tab *browser.Tab, c compiled, path string, args ...any) (json.RawMessage, []pageFile, error) {
	if args == nil {
		args = []any{}
	}
	for range 2 {
		raw, err := tab.Evaluate(ctx, call, c.hash, path, args)
		if err != nil {
			return nil, nil, err
		}
		var out struct {
			Missing bool            `json:"missing"`
			Value   json.RawMessage `json:"value"`
			Files   []pageFile      `json:"files"`
		}
		if err := json.Unmarshal(raw, &out); err != nil {
			return nil, nil, err
		}
		if !out.Missing {
			if out.Value == nil {
				return json.RawMessage("null"), out.Files, nil
			}
			return out.Value, out.Files, nil
		}
		if err := load(ctx, tab, c); err != nil {
			return nil, nil, err
		}
	}
	return nil, nil, fmt.Errorf("the page lost the service as soon as it was loaded")
}

// Cookie is what check and run see of a cookie in their context object.
// HttpOnly cookies are listed without their value: they are HttpOnly so the
// page's own scripts cannot read them, and passing the value in would undo
// that.
type Cookie struct {
	Name     string  `json:"name"`
	Value    *string `json:"value,omitempty"`
	Domain   string  `json:"domain"`
	Path     string  `json:"path"`
	Expires  *string `json:"expires"` // RFC 3339, or null for a session cookie
	Expired  bool    `json:"expired"`
	HTTPOnly bool    `json:"httpOnly"`
	Secure   bool    `json:"secure"`
}

// context is the last argument of login.check and of a tool's run.
type jsContext struct {
	Cookies []Cookie `json:"cookies"`
}

func pageContext(ctx context.Context, tab *browser.Tab, svc Service, now time.Time) (jsContext, error) {
	return siteContext(ctx, tab, svc.Manifest.URL, now)
}

// siteContext is the context object with the cookies of the site at address.
func siteContext(ctx context.Context, tab *browser.Tab, address string, now time.Time) (jsContext, error) {
	got, err := tab.Cookies(ctx, address)
	if err != nil {
		return jsContext{}, err
	}
	return jsContext{Cookies: cookies(got, now)}, nil
}

func cookies(got []browser.Cookie, now time.Time) []Cookie {
	out := make([]Cookie, len(got))
	for i, c := range got {
		k := Cookie{Name: c.Name, Domain: c.Domain, Path: c.Path, HTTPOnly: c.HTTPOnly, Secure: c.Secure}
		if !c.HTTPOnly {
			v := c.Value
			k.Value = &v
		}
		if c.Expires > 0 {
			at := time.Unix(int64(c.Expires), 0).UTC()
			s := at.Format(time.RFC3339)
			k.Expires = &s
			k.Expired = at.Before(now)
		}
		out[i] = k
	}
	return out
}
