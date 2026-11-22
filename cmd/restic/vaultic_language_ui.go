package main

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/widget"
)

// Choices of the language selector. The first label is bilingual on purpose:
// it must be readable whichever language is active.
const (
	vaulticLangChoiceAuto = "Automatique / Automatic"
	vaulticLangChoiceFR   = "Français"
	vaulticLangChoiceEN   = "English"
)

// vaulticLanguageChoiceFor maps a saved setting to a selector label.
func vaulticLanguageChoiceFor(setting string) string {
	switch setting {
	case vaulticLangFR:
		return vaulticLangChoiceFR
	case vaulticLangEN:
		return vaulticLangChoiceEN
	}
	return vaulticLangChoiceAuto
}

// vaulticLanguageSettingFor maps a selector label to the saved setting.
func vaulticLanguageSettingFor(choice string) string {
	switch choice {
	case vaulticLangChoiceFR:
		return vaulticLangFR
	case vaulticLangChoiceEN:
		return vaulticLangEN
	}
	return vaulticLangAuto
}

// vaulticLanguageHooks connects the language selector to the application.
type vaulticLanguageHooks struct {
	Window   fyne.Window
	Settings *vaulticSettings
	Persist  func() error
}

// vaulticLanguageController is the language selector of the Settings page.
// The interface is built once at start-up, so a change takes effect after
// Vaultic is restarted.
type vaulticLanguageController struct {
	hooks   vaulticLanguageHooks
	Select  *widget.Select
	Content fyne.CanvasObject
	syncing bool
}

func newVaulticLanguageController(hooks vaulticLanguageHooks) *vaulticLanguageController {
	c := &vaulticLanguageController{hooks: hooks}
	c.Select = widget.NewSelect([]string{vaulticLangChoiceAuto, vaulticLangChoiceFR, vaulticLangChoiceEN}, c.changed)
	c.syncing = true
	c.Select.SetSelected(vaulticLanguageChoiceFor(hooks.Settings.Language))
	c.syncing = false
	c.Content = container.NewVBox(
		c.Select,
		vaulticWrappedLabel(vaulticT("Le changement de langue s'applique au prochain démarrage de Vaultic.")),
	)
	return c
}

func (c *vaulticLanguageController) changed(choice string) {
	if c.syncing {
		return
	}
	previous := c.hooks.Settings.Language
	c.hooks.Settings.Language = vaulticLanguageSettingFor(choice)
	if err := c.hooks.Persist(); err != nil {
		c.hooks.Settings.Language = previous
		dialog.ShowError(err, c.hooks.Window)
		c.syncing = true
		c.Select.SetSelected(vaulticLanguageChoiceFor(previous))
		c.syncing = false
		return
	}
	dialog.ShowInformation(vaulticT("Langue"), vaulticT("Redémarrez Vaultic pour appliquer la nouvelle langue."), c.hooks.Window)
}
