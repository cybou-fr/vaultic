package main

import (
	"context"
	"net/http"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"
)

// vaulticUpdateHooks connects the update check to the application.
type vaulticUpdateHooks struct {
	Client  *http.Client
	API     string
	Current string
	// Open opens a page in the user's browser.
	Open func(url string)
}

// vaulticUpdateController is the "check for updates" control of the Settings
// page. It only runs when the user presses the button, makes one request and
// never downloads or installs anything.
type vaulticUpdateController struct {
	hooks   vaulticUpdateHooks
	Check   *widget.Button
	Open    *widget.Button
	Result  *widget.Label
	Content fyne.CanvasObject

	url string
}

func newVaulticUpdateController(hooks vaulticUpdateHooks) *vaulticUpdateController {
	if hooks.Client == nil {
		hooks.Client = &http.Client{}
	}
	c := &vaulticUpdateController{hooks: hooks}
	c.Check = widget.NewButton(vaulticT("Rechercher une mise à jour"), c.check)
	c.Open = widget.NewButton(vaulticT("Ouvrir la page des versions"), func() {
		if c.url != "" && c.hooks.Open != nil {
			c.hooks.Open(c.url)
		}
	})
	c.Open.Hide()
	c.Result = vaulticWrappedLabel(vaulticT("Vaultic ne contacte aucun serveur tant que vous n'appuyez pas sur ce bouton. La demande est anonyme et rien n'est téléchargé ni installé."))
	c.Content = container.NewVBox(c.Check, c.Result, c.Open)
	return c
}

func (c *vaulticUpdateController) check() {
	c.Check.Disable()
	c.Result.SetText(vaulticT("Recherche en cours…"))
	go func() {
		result, err := vaulticCheckForUpdate(context.Background(), c.hooks.Client, c.hooks.API, c.hooks.Current)
		fyne.Do(func() {
			c.Check.Enable()
			c.show(result, err)
		})
	}()
}

// show displays the outcome of a check.
func (c *vaulticUpdateController) show(result vaulticUpdateResult, err error) {
	c.url = ""
	c.Open.Hide()
	switch {
	case err != nil:
		c.Result.SetText(err.Error())
	case result.None:
		c.Result.SetText(vaulticT("Aucune version n'a encore été publiée."))
		c.url = result.URL
	case result.Available:
		c.Result.SetText(vaulticT("Une version plus récente est disponible : ") + result.Latest)
		c.url = result.URL
		c.Open.Show()
	default:
		c.Result.SetText(vaulticT("Vaultic est à jour (version la plus récente : ") + result.Latest + ").")
	}
}
