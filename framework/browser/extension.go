package browser

import (
	"context"
	"crypto/sha256"
	"embed"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/chromedp/cdproto/runtime"
	"github.com/chromedp/chromedp"
)

// Headless Chromium cannot show its password box or its passkey dialog: a
// site that asks for an HTTP password gets an error page, and one that asks
// for a passkey waits for good. Desktop's extension takes those requests
// instead, through two of Chrome's extension APIs, and passes them to the
// HTTPAuth and Passkey hooks:
//
//   - webRequest.onAuthRequired, with the webRequestAuthProvider
//     permission, for HTTP passwords. Chrome gives the password only to
//     the site that asked. Answering over the DevTools protocol instead
//     (Fetch.authRequired) pauses every request of every tab.
//   - webAuthenticationProxy, for passkeys. Chrome first checks that the
//     passkey's site is the page's own, and adds the page's true origin;
//     a mistake in that check would let one site use another's passkey.
//
// The extension's page, bridge.html, has the listeners. The browser opens it
// in a tab of its own and adds desktopAsk to it, which the page calls with
// each request; the browser answers by calling the page's desktopAnswer.

//go:embed extension
var extension embed.FS

// extensionPatience is how long the browser waits for Chromium to load the
// extension when it starts.
const extensionPatience = 10 * time.Second

// AuthRequest is a site's request for an HTTP password.
type AuthRequest struct {
	// URL is what was requested; Origin is the site that asked, such as
	// https://router.home.localhost:8443.
	URL    string
	Origin string
	Realm  string
	// Retry is set when the password last given for this request was
	// wrong.
	Retry bool
	// Proxy is set when a proxy asked, not the site.
	Proxy bool
}

// Credentials are a username and password.
type Credentials struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

// PasskeyRequest is a page's request to make a passkey (Create) or to use
// one.
type PasskeyRequest struct {
	Create bool
	// Origin is the page's origin, as Chrome says it, such as
	// https://example.com.
	Origin string
	// Options are the page's publicKey options, as JSON, as
	// PublicKeyCredential's parseCreationOptionsFromJSON and
	// parseRequestOptionsFromJSON take them. Chrome adds the page's true
	// origin, as extensions.remoteDesktopClientOverride.
	Options json.RawMessage
}

// DOMError is the error a page's navigator.credentials call throws, such as
// a NotAllowedError.
type DOMError struct {
	Name    string `json:"name"`
	Message string `json:"message"`
}

func (e *DOMError) Error() string { return e.Name + ": " + e.Message }

// extensionID is the extension's ID, which Chrome makes from the key in its
// manifest: the first 16 bytes of the key's SHA-256, in hex, with the
// digits 0 to f written a to p.
func extensionID() (string, error) {
	raw, err := extension.ReadFile("extension/manifest.json")
	if err != nil {
		return "", err
	}
	var m struct{ Key string }
	if err := json.Unmarshal(raw, &m); err != nil {
		return "", err
	}
	der, err := base64.StdEncoding.DecodeString(m.Key)
	if err != nil {
		return "", fmt.Errorf("the extension's key: %w", err)
	}
	sum := sha256.Sum256(der)
	id := make([]byte, 32)
	for i, b := range sum[:16] {
		id[2*i] = 'a' + b>>4
		id[2*i+1] = 'a' + b&0xf
	}
	return string(id), nil
}

// writeExtension writes the extension into the profile, for Chromium to
// load, replacing whatever an older Desktop wrote there.
func writeExtension(profile string) (string, error) {
	dir := filepath.Join(profile, "Desktop extension")
	if strings.Contains(dir, ",") {
		// Chromium's --load-extension takes a list of folders, split at
		// commas.
		return "", fmt.Errorf("Chromium cannot load an extension from %s: the path has a comma", dir)
	}
	if err := os.RemoveAll(dir); err != nil {
		return "", err
	}
	sub, err := fs.Sub(extension, "extension")
	if err != nil {
		return "", err
	}
	return dir, os.CopyFS(dir, sub)
}

// question is what the bridge asks.
type question struct {
	ID        int    `json:"id"`
	Kind      string `json:"kind"` // "password", "passkey", or "canceled"
	RequestID string `json:"requestId"`
	// For a password.
	URL     string `json:"url"`
	Host    string `json:"host"`
	Port    int    `json:"port"`
	Realm   string `json:"realm"`
	IsProxy bool   `json:"isProxy"`
	// For a passkey.
	Create  bool            `json:"create"`
	Options json.RawMessage `json:"options"`
}

// bridge is the running Chromium's extension page, and the questions it is
// waiting on.
type bridge struct {
	ctx  context.Context
	opts Options

	mu sync.Mutex
	// asking are the passkey questions not answered yet, by the
	// request's ID, so a page that gives up can end its question.
	asking map[string]context.CancelFunc
	// answered are the password requests given a password, so asking
	// again means it was wrong.
	answered map[string]bool
}

// openBridge opens the extension's page in a tab of its own, and waits for
// it to take passkey requests. b.mu is held.
func (b *Browser) openBridge(browserCtx context.Context) error {
	id, err := extensionID()
	if err != nil {
		return err
	}
	ctx, _ := chromedp.NewContext(browserCtx)
	br := &bridge{ctx: ctx, opts: b.opts, asking: map[string]context.CancelFunc{}, answered: map[string]bool{}}
	// Calls to Chromium from inside its event handler deadlock.
	chromedp.ListenTarget(ctx, func(ev any) {
		if e, ok := ev.(*runtime.EventBindingCalled); ok && e.Name == "desktopAsk" {
			go br.handle(e.Payload)
		}
	})
	if err := chromedp.Run(ctx, runtime.AddBinding("desktopAsk")); err != nil {
		return fmt.Errorf("could not open Desktop's extension: %w", err)
	}
	// Chromium loads the extension after it starts, and refuses its page
	// (ERR_BLOCKED_BY_CLIENT) until then.
	deadline := time.Now().Add(extensionPatience)
	for {
		err := chromedp.Run(ctx, chromedp.Navigate("chrome-extension://"+id+"/bridge.html"))
		if err == nil {
			break
		}
		if !strings.Contains(err.Error(), "ERR_BLOCKED_BY_CLIENT") || time.Now().After(deadline) {
			return fmt.Errorf("could not open Desktop's extension: %w", err)
		}
		time.Sleep(100 * time.Millisecond)
	}
	var attached any
	err = chromedp.Run(ctx, chromedp.Evaluate(`window.attached`, &attached, func(p *runtime.EvaluateParams) *runtime.EvaluateParams {
		return p.WithAwaitPromise(true)
	}))
	if err != nil {
		return fmt.Errorf("could not open Desktop's extension: %w", err)
	}
	if attached != nil {
		return fmt.Errorf("Desktop's extension could not take passkey requests: %v", attached)
	}
	return nil
}

// handle answers one question from the bridge.
func (br *bridge) handle(payload string) {
	var q question
	if err := json.Unmarshal([]byte(payload), &q); err != nil {
		return
	}
	switch q.Kind {
	case "canceled":
		br.mu.Lock()
		cancel := br.asking[q.RequestID]
		br.mu.Unlock()
		if cancel != nil {
			cancel()
		}
	case "password":
		br.reply(q.ID, br.password(q))
	case "passkey":
		ctx, cancel := context.WithCancel(br.ctx)
		br.mu.Lock()
		br.asking[q.RequestID] = cancel
		br.mu.Unlock()
		answer := br.passkey(ctx, q)
		cancel()
		br.mu.Lock()
		delete(br.asking, q.RequestID)
		br.mu.Unlock()
		br.reply(q.ID, answer)
	}
}

func (br *bridge) password(q question) any {
	scheme := "https"
	if u, err := url.Parse(q.URL); err == nil && u.Scheme == "http" {
		scheme = "http"
	}
	// As a URL writes it: with no port when it is the scheme's own.
	origin := scheme + "://" + q.Host
	if !(scheme == "https" && q.Port == 443 || scheme == "http" && q.Port == 80) {
		origin += ":" + strconv.Itoa(q.Port)
	}
	br.mu.Lock()
	retry := br.answered[q.RequestID]
	br.mu.Unlock()
	if br.opts.HTTPAuth == nil {
		return map[string]any{"cancel": true}
	}
	creds := br.opts.HTTPAuth(br.ctx, AuthRequest{
		URL:    q.URL,
		Origin: origin,
		Realm:  q.Realm,
		Retry:  retry,
		Proxy:  q.IsProxy,
	})
	if creds == nil {
		return map[string]any{"cancel": true}
	}
	br.mu.Lock()
	br.answered[q.RequestID] = true
	br.mu.Unlock()
	return map[string]any{"authCredentials": creds}
}

func (br *bridge) passkey(ctx context.Context, q question) any {
	if br.opts.Passkey == nil {
		return map[string]any{"error": DOMError{"NotAllowedError", "this app does not use passkeys"}}
	}
	var page struct {
		Extensions struct {
			Override struct {
				Origin string `json:"origin"`
			} `json:"remoteDesktopClientOverride"`
		} `json:"extensions"`
	}
	_ = json.Unmarshal(q.Options, &page)
	response, err := br.opts.Passkey(ctx, PasskeyRequest{Create: q.Create, Origin: page.Extensions.Override.Origin, Options: q.Options})
	var domErr *DOMError
	switch {
	case errors.As(err, &domErr):
		return map[string]any{"error": domErr}
	case err != nil:
		return map[string]any{"error": DOMError{"NotAllowedError", err.Error()}}
	}
	return map[string]any{"responseJson": string(response)}
}

// reply gives the bridge the answer to question id.
func (br *bridge) reply(id int, answer any) {
	raw, err := json.Marshal(answer)
	if err != nil {
		return
	}
	_ = chromedp.Run(br.ctx, chromedp.Evaluate(fmt.Sprintf("desktopAnswer(%d, %s)", id, raw), nil))
}
