package engine

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/michaelckearney/monorepo/framework/browser"
)

// What a page shows beside itself, as a browser window would: its dialogs,
// its file picker, and its downloads. In a live view, Harbor asks you, as a
// browser would. Anywhere else, no one is there to ask: Harbor answers a
// dialog itself, gives no files, and drops a download.

// dialog answers a page's alert, confirm, prompt, or "leave this page?".
func (a asker) dialog(ctx context.Context, tab *browser.Tab, d browser.Dialog) (bool, string) {
	v := a.e.viewOf(tab)
	if v == nil && a.e.browsing.shows(tab) {
		// The agent answers its own tab's dialogs (browse.go).
		return a.e.browsing.waitForDialog(ctx, d)
	}
	if v == nil {
		// An alert says OK, and a page that would keep Harbor from leaving
		// it lets it go: a tool navigates the tab. A question gets no.
		return d.Kind == "alert" || d.Kind == "beforeunload", ""
	}
	v.turn.Lock()
	defer v.turn.Unlock()
	answer, err := v.ask(ctx, Prompt{Kind: "dialog", Site: host(d.URL), Dialog: d.Kind, Message: d.Message, Default: d.Default})
	if err != nil || answer.Cancel {
		return false, ""
	}
	return true, answer.Text
}

// chooseFiles asks you for the files a page wants, and keeps them for the
// page until Harbor quits.
func (a asker) chooseFiles(ctx context.Context, tab *browser.Tab, r browser.FileRequest) ([]string, error) {
	v := a.e.viewOf(tab)
	if v == nil {
		return nil, errors.New("Harbor gives a page files only in its window")
	}
	v.turn.Lock()
	defer v.turn.Unlock()
	shown, _ := tab.URL(ctx)
	answer, err := v.ask(ctx, Prompt{Kind: "files", Site: host(shown), Multiple: r.Multiple, Accept: r.Accept})
	if err != nil || answer.Cancel || len(answer.Files) == 0 {
		return nil, err
	}
	dir, err := os.MkdirTemp("", "harbor-upload-")
	if err != nil {
		return nil, err
	}
	a.e.mu.Lock()
	a.e.uploads = append(a.e.uploads, dir)
	a.e.mu.Unlock()
	var paths []string
	for i, f := range answer.Files {
		// Each in a folder of its own, under the name you chose it by.
		sub := filepath.Join(dir, fmt.Sprint(i))
		path := filepath.Join(sub, filepath.Base(f.Name))
		if err := os.MkdirAll(sub, 0o700); err != nil {
			return nil, err
		}
		if err := os.WriteFile(path, f.Data, 0o600); err != nil {
			return nil, err
		}
		paths = append(paths, path)
	}
	return paths, nil
}

// downloaded puts a file a page in a live view downloaded in your Downloads
// folder, as a browser would, and says so there.
func (a asker) downloaded(tab *browser.Tab, d browser.Download) {
	v := a.e.viewOf(tab)
	if v == nil {
		_ = os.Remove(d.Path)
		return
	}
	home, err := os.UserHomeDir()
	if err != nil {
		_ = os.Remove(d.Path)
		return
	}
	saved, err := moveTo(filepath.Join(home, "Downloads"), d.Name, d.Path)
	if err != nil {
		_ = os.Remove(d.Path)
		return
	}
	v.turn.Lock()
	defer v.turn.Unlock()
	_, _ = v.ask(v.ctx, Prompt{Kind: "downloaded", Site: host(d.URL), Name: d.Name, Saved: saved})
}

// moveTo moves the file at from into dir as name, or as name (2), and so
// on, if dir has one by that name already. It returns where it went.
func moveTo(dir, name, from string) (string, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	ext := filepath.Ext(name)
	stem := strings.TrimSuffix(name, ext)
	for n := 1; ; n++ {
		to := filepath.Join(dir, name)
		if n > 1 {
			to = filepath.Join(dir, fmt.Sprintf("%s (%d)%s", stem, n, ext))
		}
		if _, err := os.Lstat(to); errors.Is(err, os.ErrNotExist) {
			if err := os.Rename(from, to); err != nil {
				return "", err
			}
			return to, nil
		}
	}
}

// host is the host in address, or address if it has none.
func host(address string) string {
	if u, err := url.Parse(address); err == nil && u.Host != "" {
		return u.Host
	}
	return address
}
