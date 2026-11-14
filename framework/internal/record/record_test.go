package record

import (
	"strings"
	"testing"
)

func TestFriendly(t *testing.T) {
	for code, want := range map[string]string{
		"await page.getByRole('button', { name: 'Greet' }).click();": "getByRole('button', { name: 'Greet' }).click()",
		"// Press Enter\nawait page.keyboard.press('Enter')":         "keyboard.press('Enter')",
	} {
		if got := friendly(code); got != want {
			t.Errorf("friendly(%q) = %q, want %q", code, got, want)
		}
	}
}

func TestCheckQuotesText(t *testing.T) {
	got := check("see", `Say "hi" it's`)
	if !strings.Contains(got, `getByText("Say \"hi\" it's")`) || !strings.Contains(got, `"visible"`) {
		t.Errorf("see: %s", got)
	}
	if got := check("not-see", "x"); !strings.Contains(got, `"hidden"`) {
		t.Errorf("not-see: %s", got)
	}
}

func TestShotName(t *testing.T) {
	if got := shotName(3, "Saved invoice, in the list!"); got != "03-saved-invoice-in-the-list.png" {
		t.Errorf("shotName: %q", got)
	}
}

func TestRefsIncludeReloadedPages(t *testing.T) {
	for _, r := range []string{"e7", "f1e7"} {
		if !ref.MatchString(r) {
			t.Errorf("%s is not taken as an element reference", r)
		}
	}
}

func TestDialogAnsweredBeforeTheStepThatOpensIt(t *testing.T) {
	all := []step{{Command: "click e3"}, {Command: "dialog-dismiss"}}
	if accept, ok := dialogAfter(all, 0); !ok || accept {
		t.Errorf("dialogAfter: accept=%v ok=%v, want a dismiss", accept, ok)
	}
	if _, ok := dialogAfter(all, 1); ok {
		t.Error("the last step has no dialog after it")
	}
}
