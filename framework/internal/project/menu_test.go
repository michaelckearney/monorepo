package project

import (
	"reflect"
	"strings"
	"testing"
)

func TestMenuKeepsOrder(t *testing.T) {
	p, err := Load(writeProject(t, `name: Demo
menu:
  Notes:
    - New note: { key: CmdOrCtrl+N, event: new-note }
    - separator
    - Export…: export
  settings: open-settings
  File:
    - Import…: { event: import }
    - Export again: export
`))
	if err != nil {
		t.Fatal(err)
	}
	want := Menu{
		Settings: "open-settings",
		Menus: []Submenu{
			{Title: "Notes", Items: []MenuItem{
				{Label: "New note", Key: "CmdOrCtrl+N", Event: "new-note"},
				{},
				{Label: "Export…", Event: "export"},
			}},
			{Title: "File", Items: []MenuItem{
				{Label: "Import…", Event: "import"},
				{Label: "Export again", Event: "export"},
			}},
		},
	}
	if !reflect.DeepEqual(p.Config.Menu, want) {
		t.Fatalf("menu:\n got %+v\nwant %+v", p.Config.Menu, want)
	}
	if got := p.Config.Menu.Events(); !reflect.DeepEqual(got, []string{"open-settings", "new-note", "export", "import"}) {
		t.Errorf("events: %v", got)
	}
}

func TestMenuErrors(t *testing.T) {
	for menu, want := range map[string]string{
		"menu: [File]":                                            "line 2: menu: must be menu titles",
		"menu:\n  Edit:\n    - Go: go":                            "Edit is a standard menu",
		"menu:\n  File: go":                                       "File must be a list of items",
		"menu:\n  File:\n    - Go":                                "each item in File is",
		"menu:\n  File:\n    - Go: two words":                     `the event "two words" must be`,
		"menu:\n  File:\n    - Go: { event: go, shortcut: x }":    `"shortcut" is not an item field`,
		"menu:\n  File:\n    - Go: { key: N, event: go }":         "needs a modifier and a key",
		"menu:\n  File:\n    - Go: { key: Hyper+N, event: go }":   `"Hyper" is not a modifier`,
		"menu:\n  File:\n    - Go: { key: Cmd+North, event: go }": `"North" is not a key`,
		"menu:\n  settings: [a]":                                  "settings must be the event",
		"menu:\n  File:\n    - A: a\n  File:\n    - B: b":         "File appears twice",
	} {
		_, err := Load(writeProject(t, "name: Demo\n"+menu+"\n"))
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%q: got %v, want %q", menu, err, want)
		}
		if err != nil && strings.Count(err.Error(), ConfigFile) != 1 {
			t.Errorf("%q: %v names the file more than once", menu, err)
		}
	}
}

func TestCheckKey(t *testing.T) {
	for _, k := range []string{"CmdOrCtrl+N", "cmd+shift+backspace", "OptionOrAlt+F12", "Ctrl+,", "Shift+Page Down", "CmdOrCtrl+Plus"} {
		if err := checkKey(k); err != nil {
			t.Errorf("%s: %v", k, err)
		}
	}
	for _, k := range []string{"N", "Cmd+", "Cmd+NN", "Cmd+F36", "Win+N"} {
		if checkKey(k) == nil {
			t.Errorf("%s was accepted", k)
		}
	}
}
