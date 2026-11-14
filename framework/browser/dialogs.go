package browser

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"

	cdpbrowser "github.com/chromedp/cdproto/browser"
	"github.com/chromedp/cdproto/cdp"
	"github.com/chromedp/cdproto/dom"
	"github.com/chromedp/cdproto/page"
	"github.com/chromedp/cdproto/target"
	"github.com/chromedp/chromedp"
)

// What a browser shows of its own, beside the page, which headless Chromium
// cannot: a page's dialogs, its file picker, and its downloads. Without an
// answer, a page that shows a dialog stops until it gets one.

// Dialog is a page's alert, confirm, or prompt, or its "leave this page?".
type Dialog struct {
	// Kind is "alert", "confirm", "prompt", or "beforeunload".
	Kind    string
	Message string
	// Default is a prompt's text to start with.
	Default string
	// URL is the page's.
	URL string
}

// dialogIsUp is closed while the page has a dialog open.
func (t *Tab) dialogIsUp() <-chan struct{} {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.dialogUp
}

// dialogIsGone is closed while the page has no dialog open.
func (t *Tab) dialogIsGone() <-chan struct{} {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.dialogGone
}

// dialogShown notes that a dialog opened, or closed. It runs in Chromium's
// event handler.
func (t *Tab) dialogShown(open bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	switch {
	case open && !t.dialogOpen:
		close(t.dialogUp)
		t.dialogGone = make(chan struct{})
	case !open && t.dialogOpen:
		t.dialogUp = make(chan struct{})
		close(t.dialogGone)
	}
	t.dialogOpen = open
}

// dialog asks the Dialog hook, and gives the page the answer. A dialog that
// goes first, as when the page navigates, ends the hook's ctx.
func (t *Tab) dialog(e *page.EventJavascriptDialogOpening) {
	accept, text := false, ""
	if hook := t.browser.opts.Dialog; hook != nil {
		ctx, cancel := context.WithCancel(t.ctx)
		defer cancel()
		listen, stop := context.WithCancel(t.ctx)
		defer stop()
		chromedp.ListenTarget(listen, func(ev any) {
			if _, ok := ev.(*page.EventJavascriptDialogClosed); ok {
				cancel()
			}
		})
		accept, text = hook(ctx, t, Dialog{Kind: string(e.Type), Message: e.Message, Default: e.DefaultPrompt, URL: e.URL})
		if ctx.Err() != nil {
			return
		}
	}
	_ = t.run(t.ctx, page.HandleJavaScriptDialog(accept).WithPromptText(text))
}

// FileRequest is a page's <input type=file>, asking for files.
type FileRequest struct {
	// Multiple is set when the page takes more than one.
	Multiple bool
	// Accept is the input's accept attribute, such as "image/*,.pdf", or
	// empty for any file.
	Accept string
}

// chooseFiles asks the ChooseFiles hook, and gives the input the files.
func (t *Tab) chooseFiles(e *page.EventFileChooserOpened) {
	r := FileRequest{Multiple: e.Mode == page.FileChooserOpenedModeSelectMultiple}
	var node *cdp.Node
	err := t.run(t.ctx, chromedp.ActionFunc(func(ctx context.Context) error {
		var err error
		node, err = dom.DescribeNode().WithBackendNodeID(e.BackendNodeID).Do(ctx)
		return err
	}))
	if err == nil {
		r.Accept = node.AttributeValue("accept")
	}
	paths, err := t.browser.opts.ChooseFiles(t.ctx, t, r)
	if err != nil || len(paths) == 0 {
		return
	}
	if !r.Multiple {
		paths = paths[:1]
	}
	_ = t.run(t.ctx, dom.SetFileInputFiles(paths).WithBackendNodeID(e.BackendNodeID))
}

// Download is a file a page downloaded.
type Download struct {
	// URL is where it came from.
	URL string
	// Name is the file's name, as the site gave it.
	Name string
	// Path is the file, in a folder of Desktop's.
	Path string
}

// download is one under way: the tab it began in, and the file.
type download struct {
	tab *Tab
	Download
}

// allowDownloads sends downloads to the downloads folder, emptied first, and
// has Chromium say when one starts and ends; or, with no Downloaded hook,
// refuses them.
func (b *Browser) allowDownloads(ctx context.Context, profile string) error {
	dir := filepath.Join(profile, "Desktop downloads")
	if err := os.RemoveAll(dir); err != nil {
		return err
	}
	behavior := cdpbrowser.SetDownloadBehavior(cdpbrowser.SetDownloadBehaviorBehaviorDeny)
	if b.opts.Downloaded != nil {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return err
		}
		// Each file is named by its GUID, so two with one name do not
		// overwrite each other.
		behavior = cdpbrowser.SetDownloadBehavior(cdpbrowser.SetDownloadBehaviorBehaviorAllowAndName).
			WithDownloadPath(dir).WithEventsEnabled(true)
	}
	return chromedp.Run(ctx, chromedp.ActionFunc(func(ctx context.Context) error {
		return behavior.Do(cdp.WithExecutor(ctx, chromedp.FromContext(ctx).Browser))
	}))
}

// downloadBegins notes a download, and the tab whose page or frame began it.
// It runs in Chromium's event handler.
func (b *Browser) downloadBegins(p *process, e *cdpbrowser.EventDownloadWillBegin) {
	p.mu.Lock()
	defer p.mu.Unlock()
	t := p.tabs[target.ID(e.FrameID)]
	if t == nil {
		for _, tab := range p.tabs {
			tab.scripts.mu.Lock()
			found := false
			for _, frame := range tab.scripts.pages {
				found = found || frame == e.FrameID
			}
			tab.scripts.mu.Unlock()
			if found {
				t = tab
				break
			}
		}
	}
	if t == nil {
		return
	}
	dir := filepath.Join(b.dir(), "Desktop downloads")
	p.downloads[e.GUID] = download{t, Download{URL: e.URL, Name: safeName(e.SuggestedFilename), Path: filepath.Join(dir, e.GUID)}}
}

// downloadEnds gives a finished download to the Downloaded hook.
func (b *Browser) downloadEnds(p *process, e *cdpbrowser.EventDownloadProgress) {
	p.mu.Lock()
	d, ok := p.downloads[e.GUID]
	delete(p.downloads, e.GUID)
	p.mu.Unlock()
	if !ok {
		return
	}
	if e.State != cdpbrowser.DownloadProgressStateCompleted {
		_ = os.Remove(d.Path)
		return
	}
	b.opts.Downloaded(d.tab, d.Download)
}

// safeName is a site's name for a file, as one file name: a site can name a
// download anything, such as ../../.bashrc.
func safeName(name string) string {
	name = filepath.Base(strings.ReplaceAll(name, "\\", "/"))
	if slices.Contains([]string{"", ".", "..", "/"}, name) {
		return "download"
	}
	return name
}
