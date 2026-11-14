package engine

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/michaelckearney/monorepo/framework/browser"
	"github.com/michaelckearney/monorepo/framework/browser/securitykey"
)

// withSecurityKey uses a security key for a passkey request, asking you in
// the view to plug it in, type its PIN, and touch it. You can cancel at any
// step. Hold v.turn.
func (e *Engine) withSecurityKey(ctx context.Context, v *view, site string,
	use func(context.Context, securitykey.Device, securitykey.Asker) (json.RawMessage, error)) (json.RawMessage, error) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	dev, err := plugIn(ctx, v, site, e.SecurityKeys)
	if err != nil {
		return nil, err
	}
	defer dev.Close()
	ask := &keyAsker{v: v, site: site, ctx: ctx, cancel: cancel}
	defer ask.withdraw()
	response, err := use(ctx, dev, ask)
	var status securitykey.Status
	switch {
	case err == nil:
		return response, nil
	case errors.As(err, &status) && status == securitykey.StatusCredentialExcluded:
		return nil, &browser.DOMError{Name: "InvalidStateError", Message: err.Error()}
	case ctx.Err() != nil:
		return nil, notAllowed("you did not use the security key")
	}
	return nil, notAllowed(err.Error())
}

// plugIn waits for a security key, asking you to plug one in if none is.
// With several, it uses the first.
func plugIn(ctx context.Context, v *view, site string, find func() ([]securitykey.Device, error)) (securitykey.Device, error) {
	var answers <-chan Answer
	var withdraw func()
	for {
		devs, err := find()
		if err != nil {
			return nil, notAllowed(err.Error())
		}
		if len(devs) > 0 {
			for _, d := range devs[1:] {
				d.Close()
			}
			if withdraw != nil {
				withdraw()
			}
			return devs[0], nil
		}
		if answers == nil {
			answers, withdraw = v.offer(Prompt{Kind: "security-key", Site: site, Step: "insert"})
		}
		select {
		case <-answers:
			withdraw()
			return nil, notAllowed("you did not use a security key")
		case <-ctx.Done():
			withdraw()
			return nil, ctx.Err()
		case <-v.done:
			withdraw()
			return nil, notAllowed("the sign-in ended")
		case <-time.After(500 * time.Millisecond):
		}
	}
}

// keyAsker asks you, in the view, while a security key is used.
type keyAsker struct {
	v      *view
	site   string
	ctx    context.Context // ends with the key's use
	cancel context.CancelFunc
	// touch withdraws the prompt to touch the key, while it shows.
	touch func()
}

func (k *keyAsker) PIN(ctx context.Context, retries int, problem string) (string, error) {
	k.withdraw()
	answer, err := k.v.ask(ctx, Prompt{Kind: "security-key", Site: k.site, Step: "pin", Retries: retries, Problem: problem})
	if err != nil {
		return "", err
	}
	if answer.Cancel {
		k.cancel()
		return "", errors.New("you did not type the PIN")
	}
	return answer.PIN, nil
}

// Touch shows the prompt to touch the key; Cancel there stops the key
// waiting.
func (k *keyAsker) Touch() {
	k.withdraw()
	answers, withdraw := k.v.offer(Prompt{Kind: "security-key", Site: k.site, Step: "touch"})
	k.touch = withdraw
	go func() {
		select {
		case <-answers:
			k.cancel()
		case <-k.ctx.Done():
		}
	}()
}

func (k *keyAsker) withdraw() {
	if k.touch != nil {
		k.touch()
		k.touch = nil
	}
}
