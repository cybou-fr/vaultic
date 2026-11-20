package main

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

type vaulticRepositoryHooks struct {
	Connect, Edit, Test, Forget func(vaulticDepot)
}

// vaulticRepositoryRow shows one saved repository: friendly name, provider,
// safe location (never credentials), last verification and the four actions.
// Forgetting only edits Vaultic's list; remote backups are never touched.
func vaulticRepositoryRow(depot vaulticDepot, active bool, h vaulticRepositoryHooks) (fyne.CanvasObject, []*widget.Button) {
	scheme := "local"
	if info, ok := vaulticBackendForAddress(depot.Location); ok {
		scheme = info.Scheme
	}
	icon := widget.NewIcon(vaulticBackendIcon(scheme, active))
	name := vaulticHeading(depot.Name, 16)
	provider := vaulticCaption(vaulticProviderName(depot.Location))
	head := container.NewHBox(container.NewGridWrap(fyne.NewSize(28, 28), icon), name)
	if active {
		badge := canvas.NewText("ACTIF", vaulticAccent)
		badge.TextSize, badge.TextStyle = 11, fyne.TextStyle{Bold: true}
		head.Add(badge)
	}
	location := widget.NewLabel(vaulticSafeLocation(depot.Location))
	location.Truncation = fyne.TextTruncateEllipsis
	verified := vaulticCaption("Dernière vérification : " + vaulticVerificationSummary(depot))

	connect := widget.NewButtonWithIcon("Se connecter", theme.LoginIcon(), func() { h.Connect(depot) })
	if !active {
		connect.Importance = widget.MediumImportance
	} else {
		connect.Importance = widget.HighImportance
	}
	edit := widget.NewButtonWithIcon("Modifier", theme.DocumentCreateIcon(), func() { h.Edit(depot) })
	test := widget.NewButtonWithIcon("Tester", theme.ViewRefreshIcon(), func() { h.Test(depot) })
	forget := widget.NewButtonWithIcon("Oublier", theme.DeleteIcon(), func() { h.Forget(depot) })
	forget.Importance = widget.LowImportance

	body := container.NewVBox(head, provider, location, verified, container.NewGridWrap(fyne.NewSize(112, 38), connect, edit, test, forget))
	return vaulticSurfaceRaised(body), []*widget.Button{connect, edit, test, forget}
}
