package browser

import (
	"encoding/json"
	"strconv"
	"sync"
	"testing"
	"time"
)

// heard collects what tabs' live scripts say, in place of the Tabs service.
type heard struct {
	mu    sync.Mutex
	calls []liveCall
}

func listen(t *testing.T) *heard {
	h := &heard{}
	before := onLive
	onLive = func(_ *Tab, c liveCall) {
		h.mu.Lock()
		h.calls = append(h.calls, c)
		h.mu.Unlock()
	}
	t.Cleanup(func() { onLive = before })
	return h
}

func (h *heard) reset() {
	h.mu.Lock()
	h.calls = nil
	h.mu.Unlock()
}

func (h *heard) last(pick func(liveCall) bool) (liveCall, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for i := len(h.calls) - 1; i >= 0; i-- {
		if pick(h.calls[i]) {
			return h.calls[i], true
		}
	}
	return liveCall{}, false
}

func (h *heard) cursor() string {
	c, ok := h.last(func(c liveCall) bool { return c.Cursor != nil })
	if !ok {
		return ""
	}
	return *c.Cursor
}

// center is the middle of the element, in the page's CSS pixels; offset
// moves it right.
func center(t *testing.T, tab *Tab, selector string, offset float64) (float64, float64) {
	t.Helper()
	var p struct{ X, Y float64 }
	raw := evaluate(t, tab, `(s) => { const r = document.querySelector(s).getBoundingClientRect(); return { x: r.left + r.width / 2, y: r.top + r.height / 2 } }`, selector)
	if err := json.Unmarshal([]byte(raw), &p); err != nil {
		t.Fatal(err)
	}
	return p.X + offset, p.Y
}

func move(t *testing.T, tab *Tab, x, y float64) {
	t.Helper()
	if err := tab.Input(quick(t), InputEvent{Kind: "mouse", Type: "mouseMoved", X: x, Y: y, Button: "none"}); err != nil {
		t.Fatal(err)
	}
}

func TestLiveViewHearsTheCursorCopiesAndMenus(t *testing.T) {
	frame := `<a href="/x" style="display:block;height:40px">in a frame</a>`
	srv := site(t, map[string]string{
		"/": `<!doctype html><title>Live</title><body style="margin:0">
			<a id="link" href="/x" style="display:block;width:200px;height:40px">a link</a>
			<input id="field" value="hello world" style="display:block;width:200px;height:30px">
			<p id="para" style="width:600px;margin:0;font:16px sans-serif">words</p>
			<div id="grab" style="cursor:grab;width:200px;height:40px"></div>
			<div id="custom" style="cursor:url(hand.png) 4 4, move;width:200px;height:40px"></div>
			<button id="button" style="width:200px;height:40px">a button</button>
			<div id="own" style="width:200px;height:40px" oncontextmenu="event.preventDefault()">own menu</div>
			<iframe id="frame" srcdoc='` + frame + `' style="display:block;width:300px;height:60px;border:0"></iframe>
			<textarea id="area" style="display:block;width:300px;height:60px">one two three
four five</textarea>`,
	})
	h := listen(t)
	b := open(t, Options{})
	ctx := quick(t)
	tab, err := b.Open(ctx, srv.URL+"/")
	if err != nil {
		t.Fatal(err)
	}

	t.Run("cursor", func(t *testing.T) {
		for _, c := range []struct {
			selector string
			offset   float64
			want     string
		}{
			{"#link", 0, "pointer"},
			{"#field", 0, "text"},
			{"#para", -280, "text"},
			{"#para", 250, "default"},
			{"#grab", 0, "grab"},
			{"#custom", 0, "move"},
			{"#button", 0, "default"},
			{"#frame", -100, "pointer"},
			{"#area", 0, "text"},
		} {
			h.reset()
			x, y := center(t, tab, c.selector, c.offset)
			move(t, tab, x, y)
			eventually(t, c.selector+" has the "+c.want+" cursor", func() bool { return h.cursor() == c.want })
		}
	})

	t.Run("the mouse leaving clears the hover", func(t *testing.T) {
		x, y := center(t, tab, "#link", 0)
		move(t, tab, x, y)
		eventually(t, "the link is hovered", func() bool {
			return evaluate(t, tab, `() => document.querySelector("#link").matches(":hover")`) == "true"
		})
		move(t, tab, -1, -1)
		eventually(t, "the link is not hovered", func() bool {
			return evaluate(t, tab, `() => document.querySelector("#link").matches(":hover")`) == "false"
		})
	})

	t.Run("copy", func(t *testing.T) {
		h.reset()
		evaluate(t, tab, `() => { const f = document.querySelector("#field"); f.focus(); f.setSelectionRange(0, 5); }`)
		err := tab.Input(ctx, InputEvent{Kind: "key", Type: "rawKeyDown", Key: "c", Code: "KeyC", KeyCode: 67, Modifiers: metaKey, Commands: []string{"copy"}})
		if err != nil {
			t.Fatal(err)
		}
		eventually(t, "the copy is heard", func() bool {
			c, ok := h.last(func(c liveCall) bool { return c.Copy != nil })
			return ok && *c.Copy == "hello"
		})
	})

	rightClick := func(t *testing.T, selector string) {
		t.Helper()
		x, y := center(t, tab, selector, 0)
		for _, typ := range []string{"mousePressed", "mouseReleased"} {
			if err := tab.Input(ctx, InputEvent{Kind: "mouse", Type: typ, X: x, Y: y, Button: "right", ClickCount: 1}); err != nil {
				t.Fatal(err)
			}
		}
	}
	menu := func() *PageMenu {
		c, _ := h.last(func(c liveCall) bool { return c.Menu != nil })
		return c.Menu
	}

	t.Run("menu", func(t *testing.T) {
		h.reset()
		rightClick(t, "#link")
		eventually(t, "a menu for the link", func() bool { m := menu(); return m != nil && m.Link == srv.URL+"/x" })
		h.reset()
		rightClick(t, "#field")
		eventually(t, "a menu for the field", func() bool { m := menu(); return m != nil && m.Editable })
		h.reset()
		rightClick(t, "#own")
		rightClick(t, "#button")
		eventually(t, "a menu for the button", func() bool { m := menu(); return m != nil && !m.Editable && m.Link == "" })
		h.mu.Lock()
		n := 0
		for _, c := range h.calls {
			if c.Menu != nil {
				n++
			}
		}
		h.mu.Unlock()
		if n != 1 {
			t.Errorf("%d menus for a page's own menu and a button, want 1: the button's", n)
		}
	})

	t.Run("focus", func(t *testing.T) {
		evaluate(t, tab, `() => document.querySelector("#field").focus()`)
		if err := tab.setFocused(ctx, false); err != nil {
			t.Fatal(err)
		}
		if got := evaluate(t, tab, `() => document.activeElement.id`); got != `""` {
			t.Errorf("unfocused, the page's focus is on %s", got)
		}
		if err := tab.setFocused(ctx, true); err != nil {
			t.Fatal(err)
		}
		if got := evaluate(t, tab, `() => document.activeElement.id`); got != `"field"` {
			t.Errorf("focused again, the page's focus is on %s, want field", got)
		}
	})

	t.Run("macOS editing commands", func(t *testing.T) {
		// LiveView sends these for the macOS shortcuts; each must be one
		// Chromium knows. The caret starts after "five".
		for _, c := range []struct {
			command    string
			start, end int
			value      string
		}{
			{"moveWordLeft", 19, 19, ""},
			{"moveWordLeftAndModifySelection", 19, 23, ""},
			{"moveToLeftEndOfLine", 14, 14, ""},
			{"moveToLeftEndOfLineAndModifySelection", 14, 23, ""},
			{"moveToBeginningOfDocument", 0, 0, ""},
			{"moveToBeginningOfDocumentAndModifySelection", 0, 23, ""},
			{"moveToBeginningOfParagraph", 14, 14, ""},
			{"deleteWordBackward", 19, 19, "one two three\nfour "},
			{"deleteToBeginningOfLine", 14, 14, "one two three\n"},
		} {
			evaluate(t, tab, `() => { const a = document.querySelector("#area"); a.value = "one two three\nfour five"; a.focus(); a.setSelectionRange(23, 23); }`)
			if err := tab.send(ctx, keyPress("", "", 0, 0, []string{c.command})...); err != nil {
				t.Fatal(err)
			}
			var got struct {
				Start, End int
				Value      string
			}
			raw := evaluate(t, tab, `() => { const a = document.querySelector("#area"); return { start: a.selectionStart, end: a.selectionEnd, value: a.value } }`)
			if err := json.Unmarshal([]byte(raw), &got); err != nil {
				t.Fatal(err)
			}
			want := c.value
			if want == "" {
				want = "one two three\nfour five"
			}
			if got.Start != c.start || got.End != c.end || got.Value != want {
				t.Errorf("%s: selection %d-%d, value %s; want %d-%d, %s", c.command, got.Start, got.End,
					strconv.Quote(got.Value), c.start, c.end, strconv.Quote(want))
			}
		}
	})

	t.Run("every macOS editing command LiveView sends", func(t *testing.T) {
		// The caret starts inside "fo|ur", so each moves or edits something.
		for _, command := range []string{
			"selectAll", "moveToLeftEndOfLine", "moveToLeftEndOfLineAndModifySelection", "moveToRightEndOfLine",
			"moveToRightEndOfLineAndModifySelection", "moveToBeginningOfDocument",
			"moveToBeginningOfDocumentAndModifySelection", "moveToEndOfDocument",
			"moveToEndOfDocumentAndModifySelection", "moveWordLeft", "moveWordLeftAndModifySelection",
			"moveWordRight", "moveWordRightAndModifySelection", "moveToBeginningOfParagraph",
			"moveToEndOfParagraph", "deleteWordBackward", "deleteWordForward", "deleteToBeginningOfLine",
			"deleteToEndOfParagraph", "deleteBackward", "deleteForward",
		} {
			before := evaluate(t, tab, `() => { const a = document.querySelector("#area"); a.value = "one two three\nfour five"; a.focus(); a.setSelectionRange(16, 16); return [a.selectionStart, a.selectionEnd, a.value] }`)
			if err := tab.send(ctx, keyPress("", "", 0, 0, []string{command})...); err != nil {
				t.Fatal(err)
			}
			after := evaluate(t, tab, `() => { const a = document.querySelector("#area"); return [a.selectionStart, a.selectionEnd, a.value] }`)
			if after == before {
				t.Errorf("%s did nothing", command)
			}
		}
	})

	t.Run("composition", func(t *testing.T) {
		evaluate(t, tab, `() => { const f = document.querySelector("#field"); f.value = ""; f.focus(); }`)
		if err := tab.Input(ctx, InputEvent{Kind: "composition", Text: "´"}); err != nil {
			t.Fatal(err)
		}
		if got := evaluate(t, tab, `() => document.querySelector("#field").value`); got != `"´"` {
			t.Errorf("composing, the field has %s", got)
		}
		if err := tab.Input(ctx, InputEvent{Kind: "text", Text: "é"}); err != nil {
			t.Fatal(err)
		}
		time.Sleep(100 * time.Millisecond)
		if got := evaluate(t, tab, `() => document.querySelector("#field").value`); got != `"é"` {
			t.Errorf("committed, the field has %s, want é", got)
		}
	})
}
