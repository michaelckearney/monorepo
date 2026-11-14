package browser

import (
	"context"
	"fmt"
	"net/url"

	"github.com/chromedp/cdproto/cdp"
	"github.com/chromedp/cdproto/page"
	"github.com/chromedp/cdproto/target"
	"github.com/chromedp/chromedp"
)

// A frame from another site, such as a gradebook a school's portal shows in
// an iframe: the page cannot read it, so code runs in the frame instead.
// Chromium runs a frame from another site in a process of its own, as a
// target the tab attaches to; a frame from the page's own site, but another
// origin, runs in the page's process, in a script context of its own.

// Frame is a frame from the site at origin, such as
// https://gradebook.localhost:8443, in the page the tab shows, as a tab to
// Evaluate in. It errors if the page has no such frame, yet.
func (t *Tab) Frame(ctx context.Context, origin string) (*Tab, error) {
	want, err := url.Parse(origin)
	if err != nil {
		return nil, err
	}
	same := func(address string) bool {
		u, err := url.Parse(address)
		return err == nil && u.Scheme == want.Scheme && u.Host == want.Host
	}
	// The tab's frames: the page's, and those in its process.
	var tree *page.FrameTree
	err = t.run(ctx, chromedp.ActionFunc(func(ctx context.Context) error {
		tree, err = page.GetFrameTree().Do(ctx)
		return err
	}))
	if err != nil {
		return nil, err
	}
	ours := map[cdp.FrameID]bool{}
	var walk func(*page.FrameTree)
	walk = func(f *page.FrameTree) {
		ours[f.Frame.ID] = true
		for _, child := range f.ChildFrames {
			walk(child)
		}
	}
	walk(tree)
	// A process of its own: a target whose parent is one of the tab's
	// frames.
	var infos []*target.Info
	err = t.run(ctx, chromedp.ActionFunc(func(ctx context.Context) error {
		infos, err = target.GetTargets().Do(cdp.WithExecutor(ctx, chromedp.FromContext(ctx).Browser))
		return err
	}))
	if err != nil {
		return nil, err
	}
	for _, info := range infos {
		if info.Type == "iframe" && ours[info.ParentFrameID] && same(info.URL) {
			return t.attachFrame(ctx, info.TargetID)
		}
	}
	// The page's own process: the script context of a frame, not the
	// page's, of the frame's origin.
	t.scripts.mu.Lock()
	defer t.scripts.mu.Unlock()
	for id, frame := range t.scripts.pages {
		if frame != tree.Frame.ID && ours[frame] && same(t.scripts.contexts[id]) {
			return &Tab{id: t.id, browser: t.browser, ctx: t.ctx, cancel: func() {}, changed: make(chan struct{}), script: id}, nil
		}
	}
	return nil, fmt.Errorf("the page has no frame from %s", origin)
}

// attachFrame attaches to a frame's own target, once: the attachment lasts
// as long as the tab.
func (t *Tab) attachFrame(ctx context.Context, id target.ID) (*Tab, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if f := t.frames[id]; f != nil && f.ctx.Err() == nil {
		return f, nil
	}
	frameCtx, cancel := chromedp.NewContext(t.ctx, chromedp.WithTargetID(id))
	if err := chromedp.Run(frameCtx); err != nil {
		cancel()
		return nil, fmt.Errorf("could not reach the frame: %w", err)
	}
	if t.frames == nil {
		t.frames = map[target.ID]*Tab{}
	}
	t.frames[id] = t.browser.newTab(frameCtx, cancel, nil)
	return t.frames[id], nil
}
