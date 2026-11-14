package engine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"time"

	"github.com/michaelckearney/monorepo/framework/browser"
	"github.com/michaelckearney/monorepo/framework/browser/totp"
)

// Saved logins: a site's username, password, and authenticator secret, by
// site. A login.run types them in with fill (fillFor), so Harbor
// signs in again on its own when a session expires. You save one when Harbor
// offers, after you sign in through the live view, or on the Settings page.

// Login is a saved login. TOTPSecret is the secret an authenticator app
// keeps, in base32, if the site asks for a code; Harbor makes the code.
type Login struct {
	Username   string `json:"username"`
	Password   string `json:"password"`
	TOTPSecret string `json:"totpSecret"`
}

// setUpLogins has the service's tab type in saved logins when fill asks,
// and tell Harbor what you type into login forms.
func (e *Engine) setUpLogins(ctx context.Context, tab *browser.Tab, s *session, name string) error {
	if err := tab.Expose(ctx, "harborFill", e.fillFor(tab, s)); err != nil {
		return err
	}
	if err := tab.Expose(ctx, "harborSeen", e.seenFor(name)); err != nil {
		return err
	}
	return tab.OnNewDocument(ctx, watchSeen)
}

// watchSeen is put in every page the service's tab loads: it tells Harbor
// what you type into a username or password field.
const watchSeen = `(() => {
	const isUsername = (el) => el.type === "email" || /username|email/.test(el.autocomplete) ||
		/user|email|login/i.test(el.name + " " + el.id);
	document.addEventListener("input", (e) => {
		const el = e.target;
		if (!(el instanceof HTMLInputElement) || typeof harborSeen !== "function") return;
		if (el.type === "password") harborSeen({ password: el.value });
		else if (isUsername(el)) harborSeen({ username: el.value });
	}, true);
})()`

// fillFor types a saved value into the field the page has focused, while s
// lets it: only while Harbor runs a login.run. The page's fill (the
// "harbor" module, in package service) asks for "username", "password", or
// "code". The value is typed, never given to the page's script.
func (e *Engine) fillFor(tab *browser.Tab, s *session) browser.ExposedFunc {
	return func(ctx context.Context, origin string, arg json.RawMessage) (any, error) {
		var req struct {
			What string `json:"what"`
		}
		if err := json.Unmarshal(arg, &req); err != nil {
			return nil, err
		}
		if s.signingIn.Load() == nil {
			return nil, errors.New("Harbor fills in fields only while it signs in")
		}
		host := origin
		if u, err := url.Parse(origin); err == nil && u.Host != "" {
			host = u.Host
		}
		l, ok, err := e.secrets.Login(origin)
		if err != nil {
			return nil, err
		}
		if !ok {
			return nil, fmt.Errorf("Harbor has no saved login for %s", host)
		}
		var text string
		switch req.What {
		case "username":
			text = l.Username
		case "password":
			text = l.Password
		default:
			if l.TOTPSecret == "" {
				return nil, fmt.Errorf("Harbor has no authenticator secret saved for %s", host)
			}
			if text, err = totp.Code(l.TOTPSecret, time.Now()); err != nil {
				return nil, err
			}
		}
		if err := tab.Input(ctx, browser.InputEvent{Kind: "text", Text: text}); err != nil {
			return nil, fmt.Errorf("Harbor could not type into the field: %w", err)
		}
		return nil, nil
	}
}

// seenFor keeps what you type into username and password fields in the
// live view of the service, to offer to save it.
func (e *Engine) seenFor(name string) browser.ExposedFunc {
	return func(ctx context.Context, origin string, arg json.RawMessage) (any, error) {
		var typed Login
		if err := json.Unmarshal(arg, &typed); err != nil || origin == "" {
			return nil, err
		}
		e.mu.Lock()
		v := e.views[name]
		e.mu.Unlock()
		if v == nil {
			return nil, nil
		}
		v.mu.Lock()
		defer v.mu.Unlock()
		l := v.seen[origin]
		if typed.Username != "" {
			l.Username = typed.Username
		}
		if typed.Password != "" {
			l.Password = typed.Password
		}
		v.seen[origin] = l
		return nil, nil
	}
}

// offerToSave asks, in the live view, whether to save each login you typed
// that Harbor has not saved: once you have signed in, so it is not a wrong
// password. A saved authenticator secret is kept.
func (e *Engine) offerToSave(ctx context.Context, v *view) {
	v.mu.Lock()
	seen := v.seen
	v.mu.Unlock()
	for origin, typed := range seen {
		if typed.Password == "" {
			continue
		}
		saved, _, err := e.secrets.Login(origin)
		if err != nil || (saved.Username == typed.Username && saved.Password == typed.Password) {
			continue
		}
		host := origin
		if u, err := url.Parse(origin); err == nil {
			host = u.Host
		}
		v.turn.Lock()
		answer, err := v.ask(ctx, Prompt{Kind: "save-login", Site: host, Account: typed.Username})
		v.turn.Unlock()
		if err != nil || answer.Cancel {
			continue
		}
		saved.Username, saved.Password = typed.Username, typed.Password
		_ = e.secrets.SaveLogin(origin, saved)
	}
}
