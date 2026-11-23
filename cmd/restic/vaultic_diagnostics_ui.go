package main

import (
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"
)

// vaulticDiagnosticsHooks connects the Diagnostic section to the application.
type vaulticDiagnosticsHooks struct {
	Dir string
	// Open shows a folder in the system's file manager.
	Open func(dir string)
}

// vaulticDiagnosticsController is the Diagnostic section of the Settings page:
// where the log files are and a button to open that folder, so that a user can
// attach them to a problem report.
type vaulticDiagnosticsController struct {
	hooks   vaulticDiagnosticsHooks
	Open    *widget.Button
	Path    *widget.Label
	Content fyne.CanvasObject
}

func newVaulticDiagnosticsController(hooks vaulticDiagnosticsHooks) *vaulticDiagnosticsController {
	c := &vaulticDiagnosticsController{hooks: hooks}
	c.Open = widget.NewButton(vaulticT("Ouvrir le dossier des journaux"), func() {
		// Create the folder first: it does not exist before the first line is written.
		_ = os.MkdirAll(hooks.Dir, 0o700)
		if hooks.Open != nil {
			hooks.Open(hooks.Dir)
		}
	})
	c.Path = vaulticWrappedLabel(vaulticT("Journaux : ") + hooks.Dir)
	note := vaulticWrappedLabel(vaulticT("Les journaux contiennent l'activité de Vaultic (jamais les mots de passe) et sont limités à 5 fichiers de 1 Mio. Joignez-les à un rapport de problème."))
	c.Content = container.NewVBox(c.Open, c.Path, note)
	return c
}

// vaulticFileURL turns a folder path into a file:// URL that the system opens
// in its file manager.
func vaulticFileURL(dir string) *url.URL {
	path := filepath.ToSlash(dir)
	if !strings.HasPrefix(path, "/") {
		path = "/" + path // C:/Users/... becomes /C:/Users/...
	}
	return &url.URL{Scheme: "file", Path: path}
}
