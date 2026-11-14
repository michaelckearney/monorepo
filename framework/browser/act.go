package browser

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// Using a page as a person does, by its elements: reading it as an
// accessibility tree, and clicking, typing, and pressing keys. An element is
// named by a ref, such as e12, from Snapshot or Find. Each action is made of
// the events a person's mouse and keyboard make, sent with Input, so the
// page cannot tell it from you: a page opens a file picker, or a pop-up,
// only for a click a person made.

//go:embed snapshot.js
var snapshotJS string

// Snapshot is a page as an agent reads it.
type Snapshot struct {
	URL   string `json:"url"`
	Title string `json:"title"`
	// Text is the page's accessibility tree, one element a line, as
	// Playwright MCP's browser_snapshot gives it, each with its ref:
	// `button "Sign in" [ref=e12]`.
	Text string `json:"snapshot"`
	// Truncated says Text was cut to the limit.
	Truncated bool `json:"truncated"`
}

// Snapshot reads the page, up to limit characters, or all of it for 0. An
// element keeps its ref for as long as it is on the page, so a later
// snapshot names it the same; a ref from an earlier snapshot of an element
// gone since names nothing. Settle first, to read the page once it has
// loaded what it shows.
func (t *Tab) Snapshot(ctx context.Context, limit int) (Snapshot, error) {
	var s Snapshot
	raw, err := t.Evaluate(ctx, snapshotJS, limit)
	if err != nil {
		return s, err
	}
	return s, json.Unmarshal(raw, &s)
}

// ErrNotFound is the error when Find finds no element.
var ErrNotFound = errors.New("no element matches")

// Find is the ref of the first element that matches selector, a CSS
// selector, for an element a Snapshot does not name, such as one an app
// marked with an attribute of its own.
func (t *Tab) Find(ctx context.Context, selector string) (string, error) {
	raw, err := t.Evaluate(ctx, `(selector) => {
		const el = document.querySelector(selector);
		if (!el) return "";
		const refs = (window.__desktopRefs ??= { next: 1, of: new WeakMap(), to: new Map() });
		let ref = refs.of.get(el);
		if (!ref) {
			ref = "e" + refs.next++;
			refs.of.set(el, ref);
		}
		refs.to.set(ref, new WeakRef(el));
		return ref;
	}`, selector)
	if err != nil {
		return "", err
	}
	var ref string
	if err := json.Unmarshal(raw, &ref); err != nil {
		return "", err
	}
	if ref == "" {
		return "", fmt.Errorf("%w %s", ErrNotFound, selector)
	}
	return ref, nil
}

// onElement is fn, called with the element ref names and args. fn is
// written inside it, so its errors can name ref.
func onElement(fn string) string {
	return `(ref, ...args) => {
		const el = window.__desktopRefs?.to.get(ref)?.deref();
		if (!el || !el.isConnected) {
			throw new Error("there is no element " + ref + " on the page now: take a new snapshot");
		}
		return (` + fn + `)(el, ...args);
	}`
}

// EvaluateOn is Evaluate, with the element ref names as fn's first argument,
// before args.
func (t *Tab) EvaluateOn(ctx context.Context, ref, fn string, args ...any) (json.RawMessage, error) {
	return t.Evaluate(ctx, onElement(fn), append([]any{ref}, args...)...)
}

// middle scrolls the element into view, and finds its middle, where a click
// goes. An element under another, such as a cookie banner, would give the
// click to that one.
const middle = `(el) => {
	el.scrollIntoView({ block: "center", inline: "center" });
	const r = el.getBoundingClientRect();
	const x = r.left + r.width / 2, y = r.top + r.height / 2;
	const hit = document.elementFromPoint(x, y);
	if (hit && !el.contains(hit) && hit.control !== el && !(hit.shadowRoot && hit.contains(el))) {
		const what = hit.tagName.toLowerCase() + (hit.id ? "#" + hit.id : "") + (hit.innerText ? " (" + hit.innerText.trim().slice(0, 40) + ")" : "");
		throw new Error(ref + " is under another element, " + what + ": close what covers it first");
	}
	return { x, y };
}`

// click clicks the element's middle, count times in a row: 2 is a
// double-click.
func (t *Tab) click(ctx context.Context, ref string, count int64) error {
	raw, err := t.EvaluateOn(ctx, ref, middle)
	if err != nil {
		return err
	}
	var at struct{ X, Y float64 }
	if err := json.Unmarshal(raw, &at); err != nil {
		return err
	}
	events := []InputEvent{{Kind: "mouse", Type: "mouseMoved", X: at.X, Y: at.Y}}
	for n := int64(1); n <= count; n++ {
		events = append(events,
			InputEvent{Kind: "mouse", Type: "mousePressed", X: at.X, Y: at.Y, Button: "left", ClickCount: n},
			InputEvent{Kind: "mouse", Type: "mouseReleased", X: at.X, Y: at.Y, Button: "left", ClickCount: n})
	}
	return t.send(ctx, events...)
}

// Click clicks the element, in its middle.
func (t *Tab) Click(ctx context.Context, ref string) error { return t.click(ctx, ref, 1) }

// DoubleClick double-clicks the element.
func (t *Tab) DoubleClick(ctx context.Context, ref string) error { return t.click(ctx, ref, 2) }

// selectAll selects what a field has, so what is typed replaces it. A
// slider takes the value as it is: typing does not move one.
const selectAll = `(el, text) => {
	if (el instanceof HTMLInputElement && el.type === "range") {
		Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, "value").set.call(el, text);
		el.dispatchEvent(new Event("input", { bubbles: true }));
		el.dispatchEvent(new Event("change", { bubbles: true }));
		return "set";
	}
	if (el.select) {
		el.select();
	} else if (el.isContentEditable) {
		const range = document.createRange();
		range.selectNodeContents(el);
		getSelection().removeAllRanges();
		getSelection().addRange(range);
	} else {
		throw new Error(ref + " is not a field to type in");
	}
	return "selected";
}`

// Type clicks the field and types text in place of what it had, as one
// piece, as a paste is. Empty text clears the field. A slider is set to
// text.
func (t *Tab) Type(ctx context.Context, ref, text string) error {
	if err := t.click(ctx, ref, 1); err != nil {
		return err
	}
	raw, err := t.EvaluateOn(ctx, ref, selectAll, text)
	if err != nil || string(raw) == `"set"` {
		return err
	}
	if text == "" {
		return t.send(ctx, keyEvents(namedKeys["Backspace"], 0)...)
	}
	return t.send(ctx, InputEvent{Kind: "text", Text: text})
}

// choose picks the options of a <select>, by value or by their text.
const choose = `(el, values) => {
	if (el.tagName !== "SELECT") throw new Error(ref + " is not a <select>: click it, then click the option");
	const found = [];
	for (const o of el.options) {
		o.selected = values.includes(o.value) || values.includes(o.text.trim());
		if (o.selected) found.push(o);
	}
	if (found.length === 0) throw new Error("no option of " + ref + " is " + values.join(" or "));
	el.dispatchEvent(new Event("input", { bubbles: true }));
	el.dispatchEvent(new Event("change", { bubbles: true }));
}`

// Select picks options of a <select>, by their values or their text, and
// unpicks the rest.
func (t *Tab) Select(ctx context.Context, ref string, values ...string) error {
	_, err := t.EvaluateOn(ctx, ref, choose, values)
	return err
}

// Check ticks a checkbox or a radio button, or unticks it, by clicking it
// if it is not already so.
func (t *Tab) Check(ctx context.Context, ref string, checked bool) error {
	raw, err := t.EvaluateOn(ctx, ref, `(el) => el.checked ?? el.getAttribute("aria-checked") === "true"`)
	if err != nil {
		return err
	}
	if (string(raw) == "true") == checked {
		return nil
	}
	return t.click(ctx, ref, 1)
}

// key is one key as Chromium takes it.
type key struct {
	name, code, text string
	keyCode          int64
}

var namedKeys = map[string]key{
	"Enter": {"Enter", "Enter", "\r", 13}, "Tab": {"Tab", "Tab", "", 9}, "Escape": {"Escape", "Escape", "", 27},
	"Backspace": {"Backspace", "Backspace", "", 8}, "Delete": {"Delete", "Delete", "", 46},
	"ArrowUp": {"ArrowUp", "ArrowUp", "", 38}, "ArrowDown": {"ArrowDown", "ArrowDown", "", 40},
	"ArrowLeft": {"ArrowLeft", "ArrowLeft", "", 37}, "ArrowRight": {"ArrowRight", "ArrowRight", "", 39},
	"Home": {"Home", "Home", "", 36}, "End": {"End", "End", "", 35}, "PageUp": {"PageUp", "PageUp", "", 33},
	"PageDown": {"PageDown", "PageDown", "", 34}, "Space": {" ", "Space", " ", 32},
}

var modifierBits = map[string]int64{"Alt": 1, "Control": 2, "Meta": 4, "Shift": 8}

// keyOf is a key by its name, such as Enter or a, or zero if it has none.
func keyOf(name string) key {
	if k, ok := namedKeys[name]; ok {
		return k
	}
	if r := []rune(name); len(r) == 1 {
		k := key{name: name, text: name}
		switch c := strings.ToUpper(name)[0]; {
		case c >= 'A' && c <= 'Z':
			k.code, k.keyCode = "Key"+string(c), int64(c)
		case c >= '0' && c <= '9':
			k.code, k.keyCode = "Digit"+string(c), int64(c)
		}
		return k
	}
	return key{}
}

// keyEvents press and let go of the key, with the modifiers held.
func keyEvents(k key, modifiers int64) []InputEvent {
	text := k.text
	if modifiers&^modifierBits["Shift"] != 0 {
		// A shortcut types nothing.
		text = ""
	}
	down := "rawKeyDown"
	if text != "" {
		down = "keyDown"
	}
	return []InputEvent{
		{Kind: "key", Type: down, Key: k.name, Code: k.code, KeyCode: k.keyCode, Text: text, Modifiers: modifiers},
		{Kind: "key", Type: "keyUp", Key: k.name, Code: k.code, KeyCode: k.keyCode, Modifiers: modifiers},
	}
}

// Press presses a key in the page, where its focus is: a key by name, such
// as Enter, Tab, Escape, ArrowDown, or a, after the modifiers to hold, as in
// Control+a or Shift+Tab. The modifiers are Alt, Control, Meta, and Shift.
func (t *Tab) Press(ctx context.Context, keys string) error {
	parts := strings.Split(keys, "+")
	var modifiers int64
	for _, m := range parts[:len(parts)-1] {
		bit, ok := modifierBits[m]
		if !ok {
			return fmt.Errorf("%q is not a modifier; use Alt, Control, Meta, or Shift", m)
		}
		modifiers |= bit
	}
	k := keyOf(parts[len(parts)-1])
	if k.name == "" {
		return fmt.Errorf("%q is not a key; name one such as Enter, Tab, Escape, ArrowDown, or a", parts[len(parts)-1])
	}
	return t.send(ctx, keyEvents(k, modifiers)...)
}
