package generated

import (
	"runtime"

	"github.com/wailsapp/wails/v3/pkg/application"
)

// MenuEvent is the Wails event a menu item sends, with the item's event name
// as its data. onMenu in the frontend's @desktop module listens for it.
const MenuEvent = "desktop:menu"

// Submenu is one of the app's own menus from desktop.yaml.
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

// Menu is the application menu: the standard menus, with the app's own
// added. A File or Help menu in desktop.yaml fills that standard place;
// any other goes between View and Window, where macOS apps put theirs.
//
// Wails has a default menu of its own, but its Help menu opens wails.io in
// the app's window.
func Menu(name, settings string, menus []Submenu) *application.Menu {
	mac := runtime.GOOS == "darwin"
	var file, help []MenuItem
	var others []Submenu
	for _, s := range menus {
		switch s.Title {
		case "File":
			file = s.Items
		case "Help":
			help = s.Items
		default:
			others = append(others, s)
		}
	}

	m := application.NewMenu()
	if mac {
		app := m.AddSubmenu(name)
		app.AddRole(application.About)
		app.AddSeparator()
		if settings != "" {
			addItem(app, MenuItem{Label: "Settings…", Key: "CmdOrCtrl+,", Event: settings})
			app.AddSeparator()
		}
		app.AddRole(application.ServicesMenu)
		app.AddSeparator()
		app.AddRole(application.Hide)
		app.AddRole(application.HideOthers)
		app.AddRole(application.UnHide)
		app.AddSeparator()
		app.AddRole(application.Quit)
	}

	f := m.AddSubmenu("File")
	addItems(f, file)
	if len(file) > 0 {
		f.AddSeparator()
	}
	if mac {
		f.AddRole(application.CloseWindow)
	} else {
		if settings != "" {
			addItem(f, MenuItem{Label: "Settings…", Key: "CmdOrCtrl+,", Event: settings})
			f.AddSeparator()
		}
		f.AddRole(application.Quit)
	}

	// Without an Edit menu, macOS has nothing to route ⌘C and ⌘V through, and
	// copy and paste do nothing in the window.
	m.AddRole(application.EditMenu)
	m.AddRole(application.ViewMenu)
	for _, s := range others {
		addItems(m.AddSubmenu(s.Title), s.Items)
	}
	m.AddRole(application.WindowMenu)
	if len(help) > 0 {
		addItems(m.AddSubmenu("Help"), help)
	}
	return m
}

func addItems(m *application.Menu, items []MenuItem) {
	for _, it := range items {
		addItem(m, it)
	}
}

func addItem(m *application.Menu, it MenuItem) {
	if it.Label == "" {
		m.AddSeparator()
		return
	}
	item := m.Add(it.Label)
	if it.Key != "" {
		item.SetAccelerator(it.Key)
	}
	event := it.Event
	item.OnClick(func(*application.Context) {
		application.Get().Event.Emit(MenuEvent, event)
	})
}
