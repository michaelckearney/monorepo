package engine

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"

	"github.com/michaelckearney/monorepo/framework/browser"
	"github.com/michaelckearney/monorepo/framework/browser/passkey"
	"github.com/michaelckearney/monorepo/framework/browser/securitykey"
)

// What sites ask the browser for, which headless Chromium cannot ask you
// itself (Slipway's browser.Options): an HTTP password, and passkeys; and
// beside them, a page's dialogs, files, and downloads (pages.go).
//
// A password Harbor has is given at once, so tools run on a site that wants
// one. Otherwise, and for every passkey, Harbor asks you in the live view of
// the site's sign-in: never while no sign-in to the site is under way, so
// no page can use your passkeys without you.

// Secrets keeps what you sign in with: HTTP passwords, Harbor's own
// passkeys, and saved logins.
type Secrets interface {
	// Password is the password saved for a site's realm, if any. origin is
	// such as https://router.home.localhost:8443.
	Password(origin, realm string) (browser.Credentials, bool, error)
	SavePassword(origin, realm string, c browser.Credentials) error
	// Passkeys are Harbor's passkeys for a site, by its domain.
	Passkeys(rpID string) ([]Passkey, error)
	SavePasskey(k passkey.Key) error
	// UsePasskeyWithoutAsking sets whether Harbor uses a passkey, by its
	// ID, without asking you.
	UsePasskeyWithoutAsking(id []byte, auto bool) error
	// Login is the login saved for a site (saved.go), by its origin.
	Login(origin string) (Login, bool, error)
	SaveLogin(origin string, l Login) error
}

// Prompt is what a sign-in asks you, beside the page.
type Prompt struct {
	// ID is what your Answer names, so it does not answer a later prompt.
	ID int `json:"id"`
	// Kind is "password" or "passkey".
	Kind string `json:"kind"`
	// Site is the site asking: its host for a password, such as
	// router.home.localhost:8443; its domain for a passkey.
	Site string `json:"site"`
	// Realm is the site's name for what the password opens.
	Realm string `json:"realm,omitempty"`
	// Wrong is set when the password you gave last was wrong.
	Wrong bool `json:"wrong,omitempty"`
	// Create is set when the site asks Harbor to make a passkey, for
	// Account.
	Create  bool   `json:"create,omitempty"`
	Account string `json:"account,omitempty"`
	// Passkeys are Harbor's passkeys the site takes, to sign in with one.
	Passkeys []PasskeyChoice `json:"passkeys,omitempty"`
	// KeyOnly is set when the site takes no passkey Harbor can make, so only
	// a security key can make one.
	KeyOnly bool `json:"keyOnly,omitempty"`
	// Step is where a sign-in with a security key is (Kind
	// "security-key"): "insert" it, type its "pin", or "touch" it.
	Step string `json:"step,omitempty"`
	// Retries is how many more wrong PINs the key takes, and Problem why it
	// asks again.
	Retries int    `json:"retries,omitempty"`
	Problem string `json:"problem,omitempty"`
	// For a page's dialog (Kind "dialog"): Dialog is "alert", "confirm",
	// "prompt", or "beforeunload"; Message is the page's; Default is a
	// prompt's text to start with.
	Dialog  string `json:"dialog,omitempty"`
	Message string `json:"message,omitempty"`
	Default string `json:"default,omitempty"`
	// For a page's request for files (Kind "files"): whether it takes more
	// than one, and the kinds it takes, as an input's accept attribute.
	Multiple bool   `json:"multiple,omitempty"`
	Accept   string `json:"accept,omitempty"`
	// Name is a downloaded file's (Kind "downloaded"), now in your
	// Downloads folder as Saved.
	Name  string `json:"name,omitempty"`
	Saved string `json:"saved,omitempty"`
}

// Passkey is one of Harbor's passkeys, and whether Harbor uses it without
// asking you while it signs in to its site.
type Passkey struct {
	passkey.Key
	Auto bool
}

// PasskeyChoice is one of Harbor's passkeys, as a sign-in offers it.
type PasskeyChoice struct {
	ID      string `json:"id"` // base64url
	Account string `json:"account"`
}

// Answer is your answer to a Prompt.
type Answer struct {
	Prompt int `json:"prompt"`
	// Cancel refuses: the site gets no password, or no passkey.
	Cancel bool `json:"cancel"`
	// For a password.
	Username string `json:"username"`
	Password string `json:"password"`
	// Passkey is the ID of the passkey to sign in with. Making a passkey
	// needs only a yes.
	Passkey string `json:"passkey"`
	// WithoutAsking lets Harbor use that passkey from now on without
	// asking you, while it signs in to the site.
	WithoutAsking bool `json:"withoutAsking"`
	// SecurityKey uses a security key, rather than Harbor's passkeys.
	SecurityKey bool `json:"securityKey"`
	// PIN is the security key's PIN.
	PIN string `json:"pin"`
	// Text is what you typed into a page's prompt dialog.
	Text string `json:"text"`
	// Files are the files you chose for a page.
	Files []AnswerFile `json:"files"`
}

// AnswerFile is a file you chose for a page.
type AnswerFile struct {
	Name string `json:"name"`
	Data []byte `json:"data"` // base64 in JSON
}

// asker answers the browser for the engine.
type asker struct{ e *Engine }

func (a asker) password(ctx context.Context, r browser.AuthRequest) *browser.Credentials {
	if r.Proxy {
		return nil
	}
	saved := func() *browser.Credentials {
		if r.Retry {
			return nil
		}
		c, ok, err := a.e.secrets.Password(r.Origin, r.Realm)
		if err != nil || !ok {
			return nil
		}
		return &c
	}
	if c := saved(); c != nil {
		return c
	}
	v := a.e.viewFor(ctx, r.Origin)
	if v == nil {
		// The page shows the site's own refusal, and the service shows as
		// signed out.
		return nil
	}
	v.turn.Lock()
	defer v.turn.Unlock()
	// The prompt before this one may have been for the same password.
	if c := saved(); c != nil {
		return c
	}
	u, _ := url.Parse(r.Origin)
	answer, err := v.ask(ctx, Prompt{Kind: "password", Site: u.Host, Realm: r.Realm, Wrong: r.Retry})
	if err != nil || answer.Cancel {
		return nil
	}
	c := browser.Credentials{Username: answer.Username, Password: answer.Password}
	// A password that turns out wrong is asked for again, and replaced.
	_ = a.e.secrets.SavePassword(r.Origin, r.Realm, c)
	return &c
}

func notAllowed(message string) error {
	return &browser.DOMError{Name: "NotAllowedError", Message: message}
}

func (a asker) passkey(ctx context.Context, r browser.PasskeyRequest) (json.RawMessage, error) {
	if r.Create {
		return a.create(ctx, r.Options)
	}
	return a.get(ctx, r.Options)
}

func (a asker) create(ctx context.Context, options json.RawMessage) (json.RawMessage, error) {
	c, err := passkey.ParseCreation(options)
	if err != nil {
		return nil, notAllowed(err.Error())
	}
	v := a.e.viewFor(ctx, c.Origin)
	if v == nil {
		return nil, notAllowed("Harbor uses passkeys only while you sign in, in Harbor's window")
	}
	v.turn.Lock()
	defer v.turn.Unlock()
	answer, err := v.ask(ctx, Prompt{Kind: "passkey", Site: c.RPID, Create: true, Account: c.User.Name, KeyOnly: !c.CanMake()})
	if err != nil || answer.Cancel {
		return nil, notAllowed("you did not allow the passkey")
	}
	if answer.SecurityKey {
		return a.e.withSecurityKey(ctx, v, c.RPID, func(ctx context.Context, dev securitykey.Device, ask securitykey.Asker) (json.RawMessage, error) {
			return securitykey.Create(ctx, dev, c, ask)
		})
	}
	keys, err := a.e.secrets.Passkeys(c.RPID)
	if err != nil {
		return nil, notAllowed(err.Error())
	}
	for _, k := range keys {
		for _, id := range c.Exclude {
			if bytes.Equal(k.ID, id) {
				return nil, &browser.DOMError{Name: "InvalidStateError", Message: "Harbor already has a passkey for this account"}
			}
		}
	}
	key, response, err := passkey.Create(c)
	if err != nil {
		return nil, notAllowed(err.Error())
	}
	if err := a.e.secrets.SavePasskey(key); err != nil {
		return nil, notAllowed(fmt.Sprintf("Harbor could not save the passkey: %v", err))
	}
	return response, nil
}

func (a asker) get(ctx context.Context, options json.RawMessage) (json.RawMessage, error) {
	r, err := passkey.ParseRequest(options)
	if err != nil {
		return nil, notAllowed(err.Error())
	}
	saved, err := a.e.secrets.Passkeys(r.RPID)
	if err != nil {
		return nil, notAllowed(err.Error())
	}
	var keys []Passkey
	var choices []PasskeyChoice
	for _, k := range saved {
		if r.Takes(k.Key) {
			keys = append(keys, k)
			choices = append(choices, PasskeyChoice{ID: base64.RawURLEncoding.EncodeToString(k.ID), Account: k.UserName})
		}
	}
	v := a.e.viewFor(ctx, r.Origin)
	// A passkey you let Harbor use without asking is used while Harbor signs
	// in to its site: in a live view, or in a login.run. Never otherwise.
	if v != nil || a.e.signingInTo(ctx, r.Origin) {
		for _, k := range keys {
			if k.Auto {
				return passkey.Get(r, k.Key)
			}
		}
	}
	if v == nil {
		return nil, notAllowed("Harbor uses passkeys only while you sign in, in Harbor's window")
	}
	v.turn.Lock()
	defer v.turn.Unlock()
	// With none, the prompt says so, and you sign in another way.
	answer, err := v.ask(ctx, Prompt{Kind: "passkey", Site: r.RPID, Passkeys: choices})
	if err != nil || answer.Cancel {
		return nil, notAllowed("you did not use a passkey")
	}
	if answer.SecurityKey {
		return a.e.withSecurityKey(ctx, v, r.RPID, func(ctx context.Context, dev securitykey.Device, ask securitykey.Asker) (json.RawMessage, error) {
			return securitykey.Get(ctx, dev, r, ask)
		})
	}
	for i, c := range choices {
		if c.ID == answer.Passkey {
			if answer.WithoutAsking {
				_ = a.e.secrets.UsePasskeyWithoutAsking(keys[i].ID, true)
			}
			return passkey.Get(r, keys[i].Key)
		}
	}
	return nil, notAllowed("Harbor has no such passkey for " + r.RPID)
}

// signingInTo reports whether Harbor is running a login.run on the site at
// origin.
func (e *Engine) signingInTo(ctx context.Context, origin string) bool {
	e.mu.Lock()
	var tabs []*browser.Tab
	for _, s := range e.sessions {
		if t := s.signingIn.Load(); t != nil {
			tabs = append(tabs, t)
		}
	}
	e.mu.Unlock()
	for _, t := range tabs {
		if shown, err := t.URL(ctx); err == nil && sameOrigin(shown, origin) {
			return true
		}
	}
	return false
}

func sameOrigin(a, b string) bool {
	ua, errA := url.Parse(a)
	ub, errB := url.Parse(b)
	return errA == nil && errB == nil && ua.Scheme == ub.Scheme && ua.Host == ub.Host
}

// viewFor is the live view of the sign-in to the site at origin, if one is
// under way: the one whose service is on that site, or whose tab, or a tab
// it opened, is on it now.
func (e *Engine) viewFor(ctx context.Context, origin string) *view {
	want, err := url.Parse(origin)
	if err != nil {
		return nil
	}
	e.mu.Lock()
	views := make([]*view, 0, len(e.views))
	for _, v := range e.views {
		views = append(views, v)
	}
	e.mu.Unlock()
	for _, v := range views {
		sites := []string{v.svc.Manifest.URL}
		if l := v.svc.Manifest.Login; l != nil {
			sites = append(sites, siteURL(v.svc.Manifest, l.URL))
		}
		for _, t := range append([]*browser.Tab{v.tab}, v.tab.Opened()...) {
			if shown, err := t.URL(ctx); err == nil {
				sites = append(sites, shown)
			}
		}
		for _, s := range sites {
			if u, err := url.Parse(s); err == nil && u.Scheme == want.Scheme && u.Host == want.Host {
				return v
			}
		}
	}
	return nil
}

// offer shows p in the view until withdraw; your answer arrives on answers.
// Hold v.turn.
func (v *view) offer(p Prompt) (answers <-chan Answer, withdraw func()) {
	answer := make(chan Answer, 1)
	v.mu.Lock()
	v.asked++
	p.ID = v.asked
	v.answer = answer
	v.prompt = &p
	v.show(&p)
	v.mu.Unlock()
	return answer, func() {
		v.mu.Lock()
		defer v.mu.Unlock()
		if v.answer == answer {
			v.answer = nil
			v.prompt = nil
			v.show(nil)
		}
	}
}

// ask shows p in the view, and waits for your answer. It returns an error
// if ctx ends, as when the page gives up, or the sign-in ends first. Hold
// v.turn.
func (v *view) ask(ctx context.Context, p Prompt) (Answer, error) {
	answer, withdraw := v.offer(p)
	defer withdraw()
	select {
	case a := <-answer:
		return a, nil
	case <-ctx.Done():
		return Answer{}, ctx.Err()
	case <-v.done:
		return Answer{}, errors.New("the sign-in ended")
	}
}

// Answer answers the prompt the live view of a sign-in to the named service
// shows.
func (e *Engine) Answer(name string, a Answer) error {
	v, err := e.view(name)
	if err != nil {
		return err
	}
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.prompt == nil || v.prompt.ID != a.Prompt {
		return errors.New("that question is no longer asked")
	}
	v.answer <- a
	// Shown no more, though ask has not woken yet.
	v.prompt = nil
	v.show(nil)
	return nil
}
