package project

import (
	"fmt"
	"regexp"
	"slices"
	"strings"

	"gopkg.in/yaml.v3"
)

// Menu is the menu field of desktop.yaml: the app's own menus, in the order
// written, and the event the Settings item sends.
//
//	menu:
//	  settings: open-settings
//	  File:
//	    - New note: { key: CmdOrCtrl+N, event: new-note }
//	    - separator
//	    - Export…: export
//
// The standard menus (the app menu, Edit, View, Window) are always there;
// these are added to them. A menu item sends its event to the frontend, where
// onMenu in @desktop receives it.
type Menu struct {
	// Settings is the event of the Settings… item, which macOS puts in the app
	// menu on ⌘, and other systems in File. Empty means no Settings item.
	Settings string
	Menus    []Submenu
}

type Submenu struct {
	Title string
	Items []MenuItem
}

// MenuItem is one item. An empty Label is a separator.
type MenuItem struct {
	Label string
	Key   string
	Event string
}

// builtinMenus are always there and filled by the system: a menu of the
// same name would be a second one beside it.
var builtinMenus = []string{"Edit", "View", "Window"}

// validEvent keeps an event name usable as a TypeScript string literal and
// readable in an error.
var validEvent = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]*$`)

// Events are every event the menu sends, in order, once each.
func (m Menu) Events() []string {
	var out []string
	add := func(e string) {
		if e != "" && !slices.Contains(out, e) {
			out = append(out, e)
		}
	}
	add(m.Settings)
	for _, s := range m.Menus {
		for _, it := range s.Items {
			add(it.Event)
		}
	}
	return out
}

func menuFail(n *yaml.Node, format string, args ...any) error {
	return fail("%s line %d: menu: %s", ConfigFile, n.Line, fmt.Sprintf(format, args...))
}

// UnmarshalYAML reads the menu from the yaml nodes themselves, because the
// order menus and items are written in is the order they appear, and a Go
// map would lose it.
func (m *Menu) UnmarshalYAML(n *yaml.Node) error {
	if n.Kind != yaml.MappingNode {
		return menuFail(n, "must be menu titles, each with a list of items, like File: [...]")
	}
	for i := 0; i < len(n.Content); i += 2 {
		k, v := n.Content[i], n.Content[i+1]
		if k.Value == "settings" {
			if v.Kind != yaml.ScalarNode || !validEvent.MatchString(v.Value) {
				return menuFail(v, "settings must be the event the Settings… item sends, like settings: open-settings")
			}
			m.Settings = v.Value
			continue
		}
		s, err := submenu(k, v)
		if err != nil {
			return err
		}
		if slices.ContainsFunc(m.Menus, func(o Submenu) bool { return o.Title == s.Title }) {
			return menuFail(k, "%s appears twice", s.Title)
		}
		m.Menus = append(m.Menus, s)
	}
	return nil
}

func submenu(k, v *yaml.Node) (Submenu, error) {
	s := Submenu{Title: strings.TrimSpace(k.Value)}
	switch {
	case s.Title == "":
		return s, menuFail(k, "a menu needs a title")
	case slices.Contains(builtinMenus, s.Title):
		return s, menuFail(k, "%s is a standard menu that is always there; give your menu another title", s.Title)
	case v.Kind != yaml.SequenceNode || len(v.Content) == 0:
		return s, menuFail(v, "%s must be a list of items, like - New note: new-note", s.Title)
	}
	for _, n := range v.Content {
		it, err := menuItem(s.Title, n)
		if err != nil {
			return s, err
		}
		s.Items = append(s.Items, it)
	}
	return s, nil
}

func menuItem(menu string, n *yaml.Node) (MenuItem, error) {
	if n.Kind == yaml.ScalarNode && n.Value == "separator" {
		return MenuItem{}, nil
	}
	if n.Kind != yaml.MappingNode || len(n.Content) != 2 {
		return MenuItem{}, menuFail(n, "each item in %s is `- Label: event`, `- Label: { event: ..., key: ... }`, or `- separator`", menu)
	}
	k, v := n.Content[0], n.Content[1]
	it := MenuItem{Label: strings.TrimSpace(k.Value)}
	if it.Label == "" {
		return it, menuFail(k, "an item in %s needs a label", menu)
	}
	switch v.Kind {
	case yaml.ScalarNode:
		it.Event = v.Value
	case yaml.MappingNode:
		for i := 0; i < len(v.Content); i += 2 {
			switch f, val := v.Content[i], v.Content[i+1]; f.Value {
			case "event":
				it.Event = val.Value
			case "key":
				it.Key = val.Value
			default:
				return it, menuFail(f, "%s: %q is not an item field; the fields are event and key", it.Label, f.Value)
			}
		}
	default:
		return it, menuFail(v, "%s must be followed by its event, like %s: my-event", it.Label, it.Label)
	}
	if !validEvent.MatchString(it.Event) {
		return it, menuFail(v, "%s: the event %q must be letters, digits, '.', '-', '_' or ':', like new-note", it.Label, it.Event)
	}
	if it.Key != "" {
		if err := checkKey(it.Key); err != nil {
			return it, menuFail(v, "%s: key %q: %v", it.Label, it.Key, err)
		}
	}
	return it, nil
}

var modifiers = []string{"cmdorctrl", "cmd", "command", "ctrl", "optionoralt", "alt", "option", "shift", "super"}

var namedKeys = regexp.MustCompile(`^(backspace|tab|return|enter|escape|left|right|up|down|space|delete|forward delete|home|end|page up|page down|plus|numlock|f([1-9]|[12][0-9]|3[0-5]))$`)

// checkKey accepts what Wails accepts, so a shortcut that would be dropped
// at runtime, with only a line in the log, is an error here instead.
func checkKey(key string) error {
	parts := strings.Split(key, "+")
	if len(parts) < 2 {
		return fmt.Errorf("a shortcut needs a modifier and a key, like CmdOrCtrl+N")
	}
	for _, m := range parts[:len(parts)-1] {
		if !slices.Contains(modifiers, strings.ToLower(m)) {
			return fmt.Errorf("%q is not a modifier; use CmdOrCtrl, Shift, OptionOrAlt, Ctrl, or Super", m)
		}
	}
	last := parts[len(parts)-1]
	if len(last) == 1 && last[0] > ' ' && last[0] < 0x7f || namedKeys.MatchString(strings.ToLower(last)) {
		return nil
	}
	return fmt.Errorf("%q is not a key; use one character, or a name such as Backspace, Enter, Up, or F5", last)
}
