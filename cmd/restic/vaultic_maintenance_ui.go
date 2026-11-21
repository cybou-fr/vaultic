package main

import (
	"context"
	"fmt"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"github.com/restic/restic/internal/global"
)

const vaulticNoStorageAnalysis = "Aucune analyse effectuée pour ce dépôt."

// vaulticMaintenanceHooks connects the integrity and storage pages.
type vaulticMaintenanceHooks struct {
	// Run runs an engine operation through the single gate.
	Run func(title string, fn func(context.Context, global.Options) error)
}

// vaulticMaintenanceController owns the read-only maintenance actions: the
// integrity check and the storage analysis. Neither deletes anything.
type vaulticMaintenanceController struct {
	hooks vaulticMaintenanceHooks

	Check, Analyze *widget.Button
	Volume, Blobs  *canvas.Text
	Snapshots      *canvas.Text
	Updated        *widget.Label
}

func newVaulticMaintenanceController(hooks vaulticMaintenanceHooks) *vaulticMaintenanceController {
	c := &vaulticMaintenanceController{
		hooks:     hooks,
		Volume:    vaulticHeading("—", 24),
		Blobs:     vaulticHeading("—", 24),
		Snapshots: vaulticHeading("—", 24),
		Updated:   vaulticWrappedLabel(vaulticNoStorageAnalysis),
	}
	c.Check = widget.NewButton("Vérifier l'intégrité", func() {
		hooks.Run("Vérification", func(ctx context.Context, g global.Options) error {
			_, err := runCheck(ctx, CheckOptions{}, g, nil, g.Term)
			return err
		})
	})
	c.Analyze = widget.NewButtonWithIcon("Analyser le stockage", theme.SearchIcon(), func() {
		hooks.Run("Analyse du stockage", func(ctx context.Context, g global.Options) error {
			metrics, err := vaulticCollectStorageStatistics(ctx, g)
			if err != nil {
				return err
			}
			fyne.Do(func() {
				if ctx.Err() != nil {
					return
				}
				c.Show(metrics, time.Now())
			})
			return nil
		})
	})
	return c
}

// Buttons lists the actions disabled while an operation runs.
func (c *vaulticMaintenanceController) Buttons() []*widget.Button {
	return []*widget.Button{c.Check, c.Analyze}
}

// vaulticStorageFigures formats the three figures of a storage analysis.
func vaulticStorageFigures(m vaulticStorageStatistics) (volume, blobs, snapshots string) {
	return vaulticBytes(m.TotalSize), fmt.Sprintf("%d", m.TotalBlobCount), fmt.Sprintf("%d", m.SnapshotsCount)
}

// Show displays the result of an analysis finished at when.
func (c *vaulticMaintenanceController) Show(m vaulticStorageStatistics, when time.Time) {
	volume, blobs, snapshots := vaulticStorageFigures(m)
	c.Volume.Text, c.Blobs.Text, c.Snapshots.Text = volume, blobs, snapshots
	c.Volume.Refresh()
	c.Blobs.Refresh()
	c.Snapshots.Refresh()
	c.Updated.SetText("Dernière analyse : " + when.Format("02/01/2006 à 15:04"))
}

// Reset forgets the last analysis, for example after switching repository.
func (c *vaulticMaintenanceController) Reset() {
	for _, v := range []*canvas.Text{c.Volume, c.Blobs, c.Snapshots} {
		v.Text = "—"
		v.Refresh()
	}
	c.Updated.SetText(vaulticNoStorageAnalysis)
}

// UpdateState enables the storage analysis only with an active repository.
func (c *vaulticMaintenanceController) UpdateState(busy, hasRepository bool) {
	vaulticSetButtonEnabled(c.Analyze, !busy && hasRepository)
}

// vaulticWindowCloser decides what closing the window means: close at once
// when idle, otherwise ask, cancel the running operation and close once it
// stopped.
type vaulticWindowCloser struct {
	Busy       func() bool
	Closing    func() bool
	BeginClose func() bool // cancels the operation; false when nothing runs
	CloseNow   func()
	// Confirm asks the user and reports the answer.
	Confirm func(title, message string, answer func(bool))
	// HideToTray hides the window and reports true when the user chose to keep
	// Vaultic running in the notification area. Nil means never.
	HideToTray func() bool

	prompting bool
}

// Intercept is the window's close handler: it may hide the window instead of
// quitting.
func (c *vaulticWindowCloser) Intercept() {
	if c.HideToTray != nil && !c.Closing() && c.HideToTray() {
		return
	}
	c.Quit()
}

// Quit leaves Vaultic: at once when idle, otherwise after asking and
// cancelling the running operation.
func (c *vaulticWindowCloser) Quit() {
	if c.Closing() || c.prompting {
		return
	}
	if !c.Busy() {
		c.CloseNow()
		return
	}
	c.prompting = true
	c.Confirm("Quitter Vaultic", "Annuler l’opération et fermer Vaultic après son arrêt ?", func(ok bool) {
		c.prompting = false
		if !ok {
			return
		}
		if !c.BeginClose() {
			c.CloseNow()
		}
	})
}

// vaulticConfirm adapts the Fyne confirmation dialog.
func vaulticConfirm(w fyne.Window) func(title, message string, answer func(bool)) {
	return func(title, message string, answer func(bool)) {
		dialog.ShowConfirm(title, message, answer, w)
	}
}
