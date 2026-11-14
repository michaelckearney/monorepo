package benchmark

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/michaelckearney/monorepo/framework/browser"
	"github.com/michaelckearney/monorepo/framework/browser/totp"
	"github.com/michaelckearney/monorepo/projects/harbor/backend/engine"
	"github.com/michaelckearney/monorepo/projects/harbor/backend/fixture"
)

// The user's part in a sign-in. When Harbor would show a live view, the
// run signs in itself, in that tab, as the user would: it types in the
// fixtures' email and password, Tidewater's TOTP code, and the codes the
// sites email to mail.localhost; follows emailed sign-in links; and clicks
// through "Sign in with", consent, and "is this your device?" pages. Harbor
// closes a service's sign-in when its check passes; the one browser_sign_in
// shows has no check, and the run clicks Done, as the user would, once the
// site is back with nothing left to do.

const (
	email    = "alex@example.localhost"
	password = "password123"
	// tidewaterTOTP is Tidewater Bank's TOTP secret (the fixtures'
	// README).
	tidewaterTOTP = "TIDEWATERBANKHARBORFIXTURESALEXQ"
)

const (
	// signInSteps is how many actions a sign-in may take before the run
	// gives up on it, as a user would after a page that goes nowhere.
	signInSteps = 15
	// signInPatience is how long a sign-in may take before the run gives
	// up and closes the live view, rather than wait Harbor's five minutes.
	signInPatience = 90 * time.Second
	// codePatience is how long an emailed code may take to arrive.
	codePatience = 20 * time.Second
)

// SignIn is one live view the run answered.
type SignIn struct {
	// Browser is "user" or "agent".
	Browser string   `json:"browser"`
	Service string   `json:"service"`
	Heading string   `json:"heading"`
	Steps   []string `json:"steps"`
	// Outcome is "signed in" when Harbor closed the view, its check passed,
	// or why the run gave up.
	Outcome string `json:"outcome"`
}

// signIns is the user in a trial: the Window of both browsers.
type signIns struct {
	h Harbor

	mu  sync.Mutex
	all []*SignIn
	// prompts are what Harbor asked beside a live view, such as whether to
	// use a passkey. The run does not answer them.
	prompts []string
	wg      sync.WaitGroup
}

func (s *signIns) window(agent bool) engine.Window { return liveView{s, agent} }

// done waits for every sign-in to end, and returns them.
func (s *signIns) done() ([]SignIn, []string) {
	s.wg.Wait()
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]SignIn, len(s.all))
	for i, si := range s.all {
		out[i] = *si
	}
	return out, append([]string{}, s.prompts...)
}

type liveView struct {
	s     *signIns
	agent bool
}

func (v liveView) Open(service, _, heading string, browse bool) (func(), error) {
	ctx, cancel := context.WithCancel(context.Background())
	site, watched := strings.CutPrefix(service, engine.SiteView)
	if browse && !watched {
		// Looking around is the user's own, and ends when they are done.
		cancel()
		return func() {}, nil
	}
	who := "user"
	if v.agent {
		who = "agent"
	}
	si := &SignIn{Browser: who, Service: service, Heading: heading}
	v.s.mu.Lock()
	v.s.all = append(v.s.all, si)
	v.s.mu.Unlock()
	e := v.s.h.Engine(v.agent)
	v.s.wg.Add(1)
	go func() {
		defer v.s.wg.Done()
		outcome := signIn(ctx, e, service, site, func(step string) {
			v.s.mu.Lock()
			si.Steps = append(si.Steps, step)
			v.s.mu.Unlock()
		})
		v.s.mu.Lock()
		si.Outcome = outcome
		v.s.mu.Unlock()
	}()
	// Harbor closes the view when its check passes.
	return cancel, nil
}

func (v liveView) Prompt(service string, p *engine.Prompt) error {
	if p == nil {
		return nil
	}
	raw, _ := json.Marshal(p)
	v.s.mu.Lock()
	v.s.prompts = append(v.s.prompts, service+": "+string(raw))
	v.s.mu.Unlock()
	return nil
}

// doneAfter is how many steps in a row with nothing to do, on the site
// browser_sign_in showed, before the run clicks Done: a page that is still
// loading has nothing to do for a moment too.
const doneAfter = 3

// signIn signs in in the live view of the service, until ctx ends, when
// Harbor closes the view, and returns how it ended. If it cannot, it
// closes the view, which ends the sign-in signed out. For the view
// browser_sign_in shows, site is the site's host, and the run clicks Done
// once it is back there with nothing to do.
func signIn(ctx context.Context, e *engine.Engine, service, site string, step func(string)) string {
	ctx, cancel := context.WithTimeout(ctx, signInPatience)
	defer cancel()
	since, err := lastMail(ctx)
	if err != nil {
		e.CloseView(service)
		return "gave up: " + err.Error()
	}
	clicked := map[string]bool{}
	idle := 0
	for n := 0; ; n++ {
		if ctx.Err() != nil {
			if errors.Is(ctx.Err(), context.DeadlineExceeded) {
				e.CloseView(service)
				return "gave up: it took longer than " + signInPatience.String()
			}
			return "signed in"
		}
		if n == signInSteps {
			e.CloseView(service)
			return fmt.Sprintf("gave up after %d steps", signInSteps)
		}
		tab, err := shownTab(e, service)
		if errors.Is(err, engine.ErrNoView) {
			return "signed in"
		}
		if err != nil {
			e.CloseView(service)
			return "gave up: " + err.Error()
		}
		did, err := signInStep(ctx, tab, &since, clicked)
		if err != nil && ctx.Err() == nil {
			step("error: " + err.Error())
		}
		if did != "" {
			step(did)
		}
		if idle++; did != "" || err != nil || site == "" || host(tab.State().URL) != site {
			idle = 0
		}
		if idle == doneAfter {
			step("clicked Done on " + site)
			e.CloseView(service)
			return "signed in, then clicked Done"
		}
		// The page moves on, or Harbor's check passes, in its own time.
		select {
		case <-ctx.Done():
		case <-time.After(time.Second):
		}
	}
}

// shownTab is the tab the live view shows: as Harbor's window does, the
// newest tab the service's tab opened, such as a sign-in pop-up, or that
// tab.
func shownTab(e *engine.Engine, service string) (*browser.Tab, error) {
	tab, _, err := e.Shown(service)
	if err != nil {
		return nil, err
	}
	if opened := tab.Opened(); len(opened) > 0 {
		tab = opened[len(opened)-1]
	}
	return tab, nil
}

// page is what a sign-in step needs to know of the page.
type page struct {
	URL     string        `json:"url"`
	Fields  []pageControl `json:"fields"`
	Buttons []pageControl `json:"buttons"`
}

// pageControl is an empty field the user would fill in, or a button.
type pageControl struct {
	// Mark finds it again, as data-harbor-sign-in.
	Mark int `json:"mark"`
	// Kind is a field's: username, password, or code.
	Kind string `json:"kind,omitempty"`
	Text string `json:"text,omitempty"`
}

// ref is the control's ref, for Slipway's actions.
func (c pageControl) ref(ctx context.Context, tab *browser.Tab) (string, error) {
	return tab.Find(ctx, fmt.Sprintf(`[data-harbor-sign-in="%d"]`, c.Mark))
}

// readPage marks the page's empty sign-in fields and its buttons.
const readPage = `() => {
  const shown = (el) => el.checkVisibility({ visibilityProperty: true }) && el.getBoundingClientRect().width > 0 && !el.disabled
  let mark = 0
  const marked = (el) => {
    el.setAttribute("data-harbor-sign-in", ++mark)
    return mark
  }
  const kind = (el) => {
    if (el.type === "password") return "password"
    const about = [el.autocomplete, el.name, el.id, el.type, el.placeholder, ...[...(el.labels ?? [])].map((l) => l.textContent)].join(" ")
    if (/one-time-code|otp|code|token/i.test(about)) return "code"
    if (/user|email|login|account/i.test(about)) return "username"
    return ""
  }
  const fields = [...document.querySelectorAll("input, textarea")]
    .filter((el) => shown(el) && !el.readOnly && el.value === "" && kind(el))
    .map((el) => ({ mark: marked(el), kind: kind(el) }))
  const buttons = [...document.querySelectorAll("button, input[type=submit], input[type=button], a[role=button], [role=button]")]
    .filter(shown)
    .map((el) => ({ mark: marked(el), text: (el.innerText || el.value || el.getAttribute("aria-label") || "").trim() }))
  return { url: location.href, fields, buttons }
}`

// moveOn is the text of a button that moves a sign-in on: "Sign in with
// Northwind ID", consent's "Accept", the bank's "Yes, continue to my
// accounts". Signing out, cancelling, and declining do not.
var moveOn = regexp.MustCompile(`(?i)^(sign in|log in|continue|next|accept|allow|yes|verify|submit|i agree|agree)\b`)

// signInStep does the next thing the user would on the page, and says what
// it did, or "" for nothing.
func signInStep(ctx context.Context, tab *browser.Tab, since *int, clicked map[string]bool) (string, error) {
	raw, err := tab.Evaluate(ctx, readPage)
	if err != nil {
		return "", err
	}
	var p page
	if err := json.Unmarshal(raw, &p); err != nil {
		return "", err
	}
	if len(p.Fields) > 0 {
		var did []string
		for _, f := range p.Fields {
			text := email
			switch f.Kind {
			case "password":
				text = password
			case "code":
				if text, err = code(ctx, p.URL, since); err != nil {
					return strings.Join(did, ", "), err
				}
			}
			ref, err := f.ref(ctx, tab)
			if err == nil {
				err = tab.Type(ctx, ref, text)
			}
			if err != nil {
				return strings.Join(did, ", "), err
			}
			did = append(did, "typed the "+f.Kind)
		}
		// Enter submits the form, as the user would.
		if err := tab.Press(ctx, "Enter"); err != nil {
			return strings.Join(did, ", "), err
		}
		return strings.Join(did, ", ") + ", pressed Enter on " + host(p.URL), nil
	}
	for _, b := range p.Buttons {
		key := p.URL + " " + b.Text
		if !moveOn.MatchString(b.Text) || clicked[key] {
			continue
		}
		clicked[key] = true
		ref, err := b.ref(ctx, tab)
		if err == nil {
			err = tab.Click(ctx, ref)
		}
		return fmt.Sprintf("clicked %q on %s", b.Text, host(p.URL)), err
	}
	// A page that says to check your email: follow the link it sent.
	if link, ok, err := mailedLink(ctx, since); err != nil || ok {
		if err != nil {
			return "", err
		}
		return "followed the emailed link to " + host(link), tab.Navigate(ctx, link)
	}
	return "", nil
}

func host(u string) string {
	if p, err := url.Parse(u); err == nil {
		return p.Host
	}
	return u
}

// code is the code a page asks for: Tidewater's from its authenticator
// app, and every other site's from the newest email it sent.
func code(ctx context.Context, page string, since *int) (string, error) {
	if strings.HasPrefix(host(page), "tidewater.localhost") {
		return totp.Code(tidewaterTOTP, time.Now())
	}
	ctx, cancel := context.WithTimeout(ctx, codePatience)
	defer cancel()
	sixDigits := regexp.MustCompile(`\b\d{6}\b`)
	for {
		msgs, err := mail(ctx)
		if err != nil {
			return "", err
		}
		for _, m := range msgs {
			if m.ID <= *since {
				continue
			}
			if c := sixDigits.FindString(m.Subject + "\n" + m.Text); c != "" {
				*since = m.ID
				return c, nil
			}
		}
		select {
		case <-ctx.Done():
			return "", errors.New("no code came to " + email)
		case <-time.After(time.Second):
		}
	}
}

// mailedLink is the sign-in link in an email that came since, if any.
func mailedLink(ctx context.Context, since *int) (string, bool, error) {
	msgs, err := mail(ctx)
	if err != nil {
		return "", false, err
	}
	link := regexp.MustCompile(`https://[^\s<>"]+`)
	for _, m := range msgs {
		if m.ID <= *since {
			continue
		}
		if l := link.FindString(m.Text); l != "" && regexp.MustCompile(`(?i)sign.?in|log.?in`).MatchString(m.Subject) {
			*since = m.ID
			return l, true, nil
		}
	}
	return "", false, nil
}

type message struct {
	ID      int    `json:"id"`
	Subject string `json:"subject"`
	Text    string `json:"text"`
}

// fixtures reaches the fixture sites from Go, where this process serves
// them, and trusts their certificates.
func fixtures() *http.Client {
	return &http.Client{Timeout: 10 * time.Second, Transport: fixture.Transport(http.DefaultTransport.(*http.Transport))}
}

// mail is every email sent to Alex, newest first.
func mail(ctx context.Context) ([]message, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://mail.localhost:8443/api/messages?to="+url.QueryEscape(email), nil)
	if err != nil {
		return nil, err
	}
	res, err := fixtures().Do(req)
	if err != nil {
		return nil, fmt.Errorf("reading the fixtures' mail: %w", err)
	}
	defer res.Body.Close()
	var msgs []message
	if err := json.NewDecoder(res.Body).Decode(&msgs); err != nil {
		return nil, fmt.Errorf("reading the fixtures' mail: %w", err)
	}
	return msgs, nil
}

// lastMail is the newest email's ID, so that a sign-in uses only the codes
// sent during it.
func lastMail(ctx context.Context) (int, error) {
	msgs, err := mail(ctx)
	if err != nil {
		return 0, err
	}
	last := 0
	for _, m := range msgs {
		last = max(last, m.ID)
	}
	return last, nil
}
