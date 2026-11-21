package main

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/driver/desktop"
)

// vaulticTrayHooks are the actions offered by the notification-area icon.
type vaulticTrayHooks struct {
	Show      func() // bring the window back
	BackupNow func() // start a manual backup of the selected profile
	Quit      func() // quit Vaultic (asks first when an operation runs)
}

// vaulticTray is the notification-area icon. Vaultic stays visible there while
// it runs in the background: nothing hidden, one menu to open or quit.
type vaulticTray struct {
	app       fyne.App
	desktop   desktop.App
	installed bool
}

// newVaulticTray reports ok=false when the platform has no notification area
// that Fyne can use; callers then never hide the window.
func newVaulticTray(app fyne.App) (*vaulticTray, bool) {
	d, ok := app.(desktop.App)
	if !ok {
		return nil, false
	}
	return &vaulticTray{app: app, desktop: d}, true
}

// vaulticTrayMenu builds the menu from the hooks.
func vaulticTrayMenu(h vaulticTrayHooks) *fyne.Menu {
	return fyne.NewMenu("Vaultic",
		fyne.NewMenuItem("Ouvrir Vaultic", h.Show),
		fyne.NewMenuItem("Sauvegarder maintenant", h.BackupNow),
		fyne.NewMenuItemSeparator(),
		fyne.NewMenuItem("Quitter Vaultic", h.Quit),
	)
}

// Install shows the icon with its menu.
func (t *vaulticTray) Install(icon fyne.Resource, h vaulticTrayHooks) {
	t.desktop.SetSystemTrayIcon(icon)
	t.desktop.SetSystemTrayMenu(vaulticTrayMenu(h))
	t.installed = true
}

// Installed reports whether the icon is shown.
func (t *vaulticTray) Installed() bool { return t != nil && t.installed }
