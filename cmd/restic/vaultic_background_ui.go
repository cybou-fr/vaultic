package main

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/widget"
)

// vaulticBackgroundHooks connects the background options to the application.
type vaulticBackgroundHooks struct {
	Window        fyne.Window
	Settings      *vaulticSettings
	Persist       func() error
	Autostart     vaulticAutostart
	Executable    func() (string, error)
	TrayAvailable bool
	Log           func(string)
}

// vaulticBackgroundController owns the two background options: minimise to the
// notification area when the window is closed, and start with the session.
// Scheduled backups run only while the Vaultic process runs; these options make
// that process easier to keep running, visibly.
type vaulticBackgroundController struct {
	hooks vaulticBackgroundHooks

	CloseToTray *widget.Check
	Autostart   *widget.Check
	Content     fyne.CanvasObject

	syncing bool
}

func newVaulticBackgroundController(hooks vaulticBackgroundHooks) *vaulticBackgroundController {
	c := &vaulticBackgroundController{hooks: hooks}
	c.CloseToTray = widget.NewCheck(vaulticT("Fermer la fenêtre réduit Vaultic dans la zone de notification"), c.onCloseToTray)
	c.Autostart = widget.NewCheck(vaulticT("Démarrer Vaultic avec ma session (réduit dans la zone de notification)"), c.onAutostart)
	info := vaulticWrappedLabel(vaulticT("Les sauvegardes planifiées ne s'exécutent que pendant que Vaultic tourne. " +
		"Le processus reste visible dans la zone de notification ; « Quitter Vaultic » y arrête tout. " +
		"Aucun service système ni tâche du planificateur de l'ordinateur n'est créé."))
	c.Content = container.NewVBox(c.CloseToTray, c.Autostart, info)
	if !hooks.TrayAvailable {
		c.CloseToTray.Disable()
		c.Autostart.Disable()
		c.Content.(*fyne.Container).Add(vaulticWrappedLabel(vaulticT("Aucune zone de notification n'est disponible sur cet ordinateur : ces options sont désactivées.")))
	}
	c.Sync()
	return c
}

// Sync shows the current state: the saved choice and the system's startup list.
func (c *vaulticBackgroundController) Sync() {
	c.syncing = true
	defer func() { c.syncing = false }()
	c.CloseToTray.SetChecked(c.hooks.Settings.CloseToTray && c.hooks.TrayAvailable)
	enabled, err := c.hooks.Autostart.Enabled()
	if err != nil {
		c.Autostart.Disable()
		c.hooks.Log(vaulticT("Démarrage automatique : état illisible (") + err.Error() + ").")
		return
	}
	c.Autostart.SetChecked(enabled)
}

// ShouldHideOnClose reports whether closing the window hides it instead.
func (c *vaulticBackgroundController) ShouldHideOnClose() bool {
	return c.hooks.TrayAvailable && c.hooks.Settings.CloseToTray
}

func (c *vaulticBackgroundController) onCloseToTray(on bool) {
	if c.syncing {
		return
	}
	previous := c.hooks.Settings.CloseToTray
	c.hooks.Settings.CloseToTray = on
	if err := c.hooks.Persist(); err != nil {
		c.hooks.Settings.CloseToTray = previous
		dialog.ShowError(err, c.hooks.Window)
		c.Sync()
	}
}

func (c *vaulticBackgroundController) onAutostart(on bool) {
	if c.syncing {
		return
	}
	executable := ""
	if on {
		var err error
		if executable, err = c.hooks.Executable(); err != nil {
			dialog.ShowError(err, c.hooks.Window)
			c.Sync()
			return
		}
	}
	if err := c.hooks.Autostart.Set(on, executable); err != nil {
		dialog.ShowError(err, c.hooks.Window)
		c.Sync()
		return
	}
	if on {
		c.hooks.Log(vaulticT("Démarrage automatique activé (") + c.hooks.Autostart.Name() + ").")
	} else {
		c.hooks.Log(vaulticT("Démarrage automatique désactivé."))
	}
}
