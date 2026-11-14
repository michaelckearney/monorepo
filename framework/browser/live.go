package browser

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/chromedp/cdproto/cdp"
	"github.com/chromedp/cdproto/page"
	"github.com/chromedp/cdproto/runtime"
	"github.com/chromedp/chromedp"
)

// What LiveView needs from the page that the screencast does not show: the
// cursor the mouse would have, the text a copy took, and what is under a
// right-click, for the menu. A script in each frame of the page's process
// tells it. The script runs in a world of its own, which shares the page's
// document but not its globals, so the page can neither see it nor call it.
// A frame from another site runs in a process of its own, which the script
// does not reach: over one, the cursor is the default arrow.

// liveWorld names the script's world, and liveBinding the binding it calls.
const (
	liveWorld   = "__desktop_live"
	liveBinding = "__desktopLiveBinding"
)

// liveCall is what the script says: one field of it.
type liveCall struct {
	Cursor *string   `json:"cursor"`
	Copy   *string   `json:"copy"`
	Menu   *PageMenu `json:"menu"`
}

// PageMenu is what is under a right-click that the page left to the
// browser: a link, selected text, or a field you type in.
type PageMenu struct {
	Link      string `json:"link"`
	Selection string `json:"selection"`
	Editable  bool   `json:"editable"`
}

// onLive gets what the script says, in a goroutine of its own. It is the
// Tabs service's; tests replace it.
var onLive = func(t *Tab, c liveCall) { live.heard(t, c) }

// cursors are the CSS cursors LiveView shows; the script sends one of
// them, and any other is the default arrow.
var cursors = map[string]bool{}

func init() {
	for _, c := range []string{
		"default", "none", "context-menu", "help", "pointer", "progress", "wait", "cell", "crosshair",
		"text", "vertical-text", "alias", "copy", "move", "no-drop", "not-allowed", "grab", "grabbing",
		"all-scroll", "col-resize", "row-resize", "n-resize", "e-resize", "s-resize", "w-resize",
		"ne-resize", "nw-resize", "se-resize", "sw-resize", "ew-resize", "ns-resize", "nesw-resize",
		"nwse-resize", "zoom-in", "zoom-out",
	} {
		cursors[c] = true
	}
}

// setUpLive adds the script to every frame the tab shows from now on, and to
// those it shows already, as in a tab a page opened, which may have loaded.
func (s *scripts) setUpLive() chromedp.Action {
	return chromedp.ActionFunc(func(ctx context.Context) error {
		if err := runtime.AddBinding(liveBinding).WithExecutionContextName(liveWorld).Do(ctx); err != nil {
			return err
		}
		if _, err := page.AddScriptToEvaluateOnNewDocument(liveScript).WithWorldName(liveWorld).Do(ctx); err != nil {
			return err
		}
		tree, err := page.GetFrameTree().Do(ctx)
		if err != nil {
			return err
		}
		var frames []cdp.FrameID
		var walk func(*page.FrameTree)
		walk = func(f *page.FrameTree) {
			frames = append(frames, f.Frame.ID)
			for _, c := range f.ChildFrames {
				walk(c)
			}
		}
		walk(tree)
		for _, f := range frames {
			// A frame that went meanwhile is skipped.
			id, err := page.CreateIsolatedWorld(f).WithWorldName(liveWorld).Do(ctx)
			if err == nil {
				_, _, _ = runtime.Evaluate(liveScript).WithContextID(id).Do(ctx)
			}
		}
		return nil
	})
}

// heardLive takes a call from the script. It runs in Chromium's event
// handler.
func (t *Tab) heardLive(payload string) {
	var c liveCall
	if json.Unmarshal([]byte(payload), &c) != nil {
		return
	}
	if c.Cursor != nil && !cursors[*c.Cursor] {
		def := "default"
		c.Cursor = &def
	}
	go onLive(t, c)
}

// setFocused tells each frame whether the page has your focus, as a
// window's page is told when you switch to another window or back: the
// field you were in loses its focus, and gets it back.
func (t *Tab) setFocused(ctx context.Context, focused bool) error {
	t.scripts.mu.Lock()
	ids := make([]runtime.ExecutionContextID, 0, len(t.scripts.live))
	for id := range t.scripts.live {
		ids = append(ids, id)
	}
	t.scripts.mu.Unlock()
	expr := fmt.Sprintf("globalThis.__desktopLive?.focused(%t)", focused)
	return t.run(ctx, chromedp.ActionFunc(func(ctx context.Context) error {
		for _, id := range ids {
			_, _, _ = runtime.Evaluate(expr).WithContextID(id).Do(ctx)
		}
		return nil
	}))
}

// liveScript is the script. It runs once in each world: a frame's script
// world outlasts nothing, so a new document gets it anew.
const liveScript = `(() => {
	if (globalThis.__desktopLive) return;
	const binding = globalThis[` + "`" + liveBinding + "`" + `];
	const say = (m) => binding(JSON.stringify(m));

	const textTypes = new Set(["", "text", "search", "email", "url", "tel", "password", "number", "date",
		"datetime-local", "month", "time", "week"]);
	const editable = (el) => {
		if (!(el instanceof Element)) return false;
		if (el instanceof HTMLTextAreaElement) return !el.disabled;
		if (el instanceof HTMLInputElement) return !el.disabled && textTypes.has(el.getAttribute("type")?.toLowerCase() ?? "");
		return el instanceof HTMLElement && el.isContentEditable;
	};
	// The text under the point, if any: what an auto cursor shows a text
	// cursor over, as long as it can be selected.
	const overText = (el, x, y) => {
		if (getComputedStyle(el).userSelect === "none" || el.closest("button, select, [role=button]")) return false;
		const at = document.caretRangeFromPoint(x, y);
		const node = at?.startContainer;
		if (!node || node.nodeType !== Node.TEXT_NODE || !node.data.trim()) return false;
		const all = document.createRange();
		all.selectNodeContents(node);
		for (const r of all.getClientRects()) {
			if (x >= r.left && x <= r.right && y >= r.top && y <= r.bottom) return true;
		}
		return false;
	};
	const cursorOf = (el, x, y) => {
		if (!(el instanceof Element)) return "default";
		if (el instanceof HTMLIFrameElement || el instanceof HTMLFrameElement) return "default";
		const css = getComputedStyle(el).cursor;
		// A list, such as url(hand.png) 4 4, pointer, ends in a keyword.
		const k = css.slice(css.lastIndexOf(",") + 1).trim();
		if (k !== "auto" && !k.startsWith("url(")) return k;
		if (editable(el)) return "text";
		return overText(el, x, y) ? "text" : "default";
	};
	let lastX = -1, lastY = -1;
	const moved = (e) => {
		lastX = e.clientX;
		lastY = e.clientY;
		say({ cursor: cursorOf(e.composedPath()[0], e.clientX, e.clientY) });
	};
	for (const type of ["mousemove", "mousedown", "mouseup", "mouseover"]) {
		addEventListener(type, moved, { capture: true, passive: true });
	}
	// The page moves under a still mouse as it scrolls.
	let pending = false;
	addEventListener("scroll", () => {
		if (pending || lastX < 0) return;
		pending = true;
		requestAnimationFrame(() => {
			pending = false;
			const el = document.elementFromPoint(lastX, lastY);
			if (el) say({ cursor: cursorOf(el, lastX, lastY) });
		});
	}, { capture: true, passive: true });

	const selected = () => {
		const el = document.activeElement;
		if ((el instanceof HTMLInputElement || el instanceof HTMLTextAreaElement) && el.selectionStart !== null) {
			if (el instanceof HTMLInputElement && el.type === "password") return "";
			return el.value.slice(el.selectionStart, el.selectionEnd);
		}
		return getSelection()?.toString() ?? "";
	};
	// A copy goes to the page's own clipboard, inside Chromium: the script
	// sends it on to yours. It runs before a cut takes the text away.
	const copied = () => {
		const text = selected();
		if (text) say({ copy: text });
	};
	addEventListener("copy", copied, { capture: true });
	addEventListener("cut", copied, { capture: true });

	// A right-click that the page does not answer with a menu of its own
	// gets the browser's.
	addEventListener("contextmenu", (e) => {
		const el = e.composedPath()[0];
		const link = el instanceof Element ? el.closest("a[href]")?.href ?? "" : "";
		const menu = { link, selection: selected(), editable: editable(el) || editable(document.activeElement) };
		setTimeout(() => { if (!e.defaultPrevented) say({ menu }); });
	}, { capture: true });

	let unfocused = null;
	globalThis.__desktopLive = {
		focused(on) {
			if (!on) {
				const el = document.activeElement;
				if (document.hasFocus() && el && el !== document.body && el !== document.documentElement
					&& !(el instanceof HTMLIFrameElement) && !(el instanceof HTMLFrameElement)) {
					unfocused = el;
					el.blur();
				}
				return;
			}
			const el = unfocused;
			unfocused = null;
			if (el?.isConnected && (!document.activeElement || document.activeElement === document.body)) {
				el.focus({ preventScroll: true });
			}
		},
	};
})()`
