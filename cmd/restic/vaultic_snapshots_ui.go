package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"github.com/restic/restic/internal/global"
)

const vaulticNoSnapshotSelected = "Sélectionnez un instantané pour le parcourir ou le restaurer."

// vaulticSnapshotsHooks connects the snapshot page to the application shell.
type vaulticSnapshotsHooks struct {
	Window           fyne.Window
	App              fyne.App
	ActiveRepository func() string
	UpdateActions    func()
	// Run runs an engine operation through the single gate.
	Run func(title string, fn func(context.Context, global.Options) error)
	// RunWithDepot runs fn through the gate and then done on the UI thread.
	RunWithDepot func(title string, fn func(context.Context, global.Options) error, done func() error)
}

// vaulticSnapshotsController owns the snapshot list of the active repository,
// the selection, the per-snapshot size cache and the controls of the page.
type vaulticSnapshotsController struct {
	hooks     vaulticSnapshotsHooks
	snapshots []vaulticSnapshot
	metrics   map[string]vaulticSnapshotMetrics
	selected  int
	loaded    bool

	list       *widget.List
	emptyLabel *widget.Label
	details    *widget.Label

	Empty                  *fyne.Container
	DetailsScroll          *container.Scroll
	CountValue             *canvas.Text
	RecentValue            *widget.Label
	Refresh, Explorer      *widget.Button
	DetailsButton, Compare *widget.Button
	Restore                *widget.Button
}

func newVaulticSnapshotsController(hooks vaulticSnapshotsHooks) *vaulticSnapshotsController {
	c := &vaulticSnapshotsController{hooks: hooks, metrics: map[string]vaulticSnapshotMetrics{}, selected: -1}
	c.CountValue = vaulticHeading("—", 24)
	c.RecentValue = widget.NewLabel("Aucun instantané consulté")
	c.RecentValue.Wrapping = fyne.TextWrapWord
	c.list = widget.NewList(
		func() int { return len(c.snapshots) },
		vaulticTimelineTemplate,
		func(id widget.ListItemID, obj fyne.CanvasObject) {
			if id < 0 || id >= len(c.snapshots) {
				return
			}
			var previous *vaulticSnapshot
			if id > 0 {
				previous = &c.snapshots[id-1]
			}
			var metrics *vaulticSnapshotMetrics
			if saved, ok := c.metrics[c.snapshots[id].ID]; ok {
				metrics = &saved
			}
			vaulticUpdateTimelineRow(obj, c.snapshots[id], previous, metrics)
		},
	)
	c.emptyLabel = vaulticWrappedLabel("Actualisez l’historique pour afficher les instantanés du dépôt.")
	c.Empty = vaulticEmptyState(c.emptyLabel)
	c.details = vaulticWrappedLabel(vaulticNoSnapshotSelected)
	c.DetailsScroll = container.NewVScroll(c.details)
	c.DetailsScroll.SetMinSize(fyne.NewSize(0, 64))
	c.list.OnSelected = func(id widget.ListItemID) {
		if id >= 0 && id < len(c.snapshots) {
			c.selected = id
			c.showDetails(c.snapshots[id])
		}
		hooks.UpdateActions()
	}
	c.list.OnUnselected = func(id widget.ListItemID) {
		if c.selected == id {
			c.selected = -1
			c.details.SetText(vaulticNoSnapshotSelected)
			hooks.UpdateActions()
		}
	}
	c.Refresh = widget.NewButton("Actualiser les instantanés", c.reload)
	c.Explorer = widget.NewButton("Parcourir les fichiers…", c.browse)
	c.DetailsButton = widget.NewButtonWithIcon("Taille et fichiers…", theme.InfoIcon(), c.measure)
	c.Compare = widget.NewButtonWithIcon("Comparer…", theme.SearchIcon(), c.compare)
	c.Restore = widget.NewButton("Restaurer le tout…", c.restore)
	c.Restore.Importance = widget.HighImportance
	return c
}

// vaulticSnapshotDetailText is the text shown for the selected snapshot.
func vaulticSnapshotDetailText(sn vaulticSnapshot, metrics *vaulticSnapshotMetrics) string {
	text := fmt.Sprintf("Instantané : %s\nDate : %s\nMachine : %s\nSources :\n%s", sn.ID, sn.Time.Local().Format("02/01/2006 à 15:04"), sn.Hostname, strings.Join(sn.Paths, "\n"))
	if metrics != nil {
		text += "\n\n" + vaulticFormatSnapshotMetrics(*metrics)
	}
	return text
}

// vaulticLatestSnapshotTime returns the most recent snapshot time of a list.
func vaulticLatestSnapshotTime(list []vaulticSnapshot) (time.Time, bool) {
	if len(list) == 0 {
		return time.Time{}, false
	}
	latest := list[0].Time
	for _, candidate := range list[1:] {
		if candidate.Time.After(latest) {
			latest = candidate.Time
		}
	}
	return latest, true
}

func (c *vaulticSnapshotsController) showDetails(sn vaulticSnapshot) {
	var metrics *vaulticSnapshotMetrics
	if saved, ok := c.metrics[sn.ID]; ok {
		metrics = &saved
	}
	c.details.SetText(vaulticSnapshotDetailText(sn, metrics))
}

// Count returns the number of loaded snapshots.
func (c *vaulticSnapshotsController) Count() int { return len(c.snapshots) }

// Selected returns the selected snapshot.
func (c *vaulticSnapshotsController) Selected() (vaulticSnapshot, bool) {
	if c.selected < 0 || c.selected >= len(c.snapshots) {
		return vaulticSnapshot{}, false
	}
	return c.snapshots[c.selected], true
}

func (c *vaulticSnapshotsController) resetSelection() {
	c.selected = -1
	c.list.UnselectAll()
	c.details.SetText(vaulticNoSnapshotSelected)
	c.hooks.UpdateActions()
}

// Clear forgets the loaded snapshots, for example after switching repository.
func (c *vaulticSnapshotsController) Clear() {
	c.snapshots = nil
	c.loaded = false
	c.resetSelection()
	c.list.Refresh()
	c.CountValue.Text = "—"
	c.CountValue.Refresh()
	c.RecentValue.SetText("Aucun instantané consulté")
	c.metrics = map[string]vaulticSnapshotMetrics{}
}

// UpdateState enables the controls for the current application state.
func (c *vaulticSnapshotsController) UpdateState(busy bool) {
	if len(c.snapshots) == 0 {
		c.Empty.Show()
	} else {
		c.Empty.Hide()
	}
	if c.loaded {
		c.emptyLabel.SetText("Ce dépôt ne contient aucun instantané.")
	} else {
		c.emptyLabel.SetText("Actualisez l’historique pour afficher les instantanés du dépôt.")
	}
	canUse := !busy && c.selected >= 0
	vaulticSetButtonEnabled(c.Explorer, canUse)
	vaulticSetButtonEnabled(c.Restore, canUse)
	vaulticSetButtonEnabled(c.DetailsButton, canUse)
	vaulticSetButtonEnabled(c.Compare, canUse && len(c.snapshots) > 1)
}

func (c *vaulticSnapshotsController) setList(list []vaulticSnapshot) {
	c.snapshots = list
	c.loaded = true
	c.resetSelection()
	c.list.Refresh()
	c.CountValue.Text = fmt.Sprintf("%d", len(list))
	c.CountValue.Refresh()
	if latest, ok := vaulticLatestSnapshotTime(list); ok {
		c.RecentValue.SetText("Dernier instantané : " + latest.Local().Format("02/01/2006 à 15:04"))
	} else {
		c.RecentValue.SetText("Aucun instantané dans ce dépôt")
	}
}

func (c *vaulticSnapshotsController) reload() {
	c.hooks.Run("Lecture des instantanés", func(ctx context.Context, g global.Options) error {
		var out bytes.Buffer
		term := g.Term.(*vaulticTerminal)
		term.output = &out
		if err := runSnapshots(ctx, SnapshotOptions{}, g, nil, term); err != nil {
			return err
		}
		var list []vaulticSnapshot
		if err := json.Unmarshal(out.Bytes(), &list); err != nil {
			return fmt.Errorf("Décodage des instantanés : %w", err)
		}
		sort.SliceStable(list, func(i, j int) bool { return list[i].Time.After(list[j].Time) })
		fyne.Do(func() { c.setList(list) })
		return nil
	})
}

func (c *vaulticSnapshotsController) browse() {
	w := c.hooks.Window
	sn, ok := c.Selected()
	if !ok {
		dialog.ShowInformation("Sélection requise", "Sélectionnez un instantané.", w)
		return
	}
	repository := vaulticResolvedRepository(c.hooks.ActiveRepository())
	vaulticOpenExplorer(c.hooks.App, sn, func(title string, operation func(context.Context, global.Options) error) {
		if repository != vaulticResolvedRepository(c.hooks.ActiveRepository()) {
			dialog.ShowInformation("Dépôt modifié", "Actualisez les instantanés du dépôt actif avant de les parcourir.", w)
			return
		}
		c.hooks.Run(title, operation)
	})
}

func (c *vaulticSnapshotsController) measure() {
	w := c.hooks.Window
	sn, ok := c.Selected()
	if !ok {
		return
	}
	if cached, ok := c.metrics[sn.ID]; ok {
		dialog.ShowInformation("Détails de l'instantané", vaulticFormatSnapshotMetrics(cached), w)
		return
	}
	var metrics vaulticSnapshotMetrics
	c.hooks.RunWithDepot("Analyse de l'instantané",
		func(ctx context.Context, g global.Options) error {
			var err error
			metrics, err = vaulticMeasureSnapshot(ctx, g, sn.ID)
			return err
		}, func() error {
			c.metrics[sn.ID] = metrics
			c.list.Refresh()
			if current, ok := c.Selected(); ok && current.ID == sn.ID {
				c.details.SetText(vaulticSnapshotDetailText(sn, &metrics))
			}
			dialog.ShowInformation("Détails de l'instantané", vaulticFormatSnapshotMetrics(metrics), w)
			return nil
		})
}

func (c *vaulticSnapshotsController) compare() {
	w := c.hooks.Window
	target, ok := c.Selected()
	if !ok || len(c.snapshots) < 2 {
		return
	}
	options, ids, initial := vaulticComparisonChoices(c.snapshots, target)
	compareSelect := widget.NewSelect(options, nil)
	compareSelect.SetSelected(initial)
	helper := vaulticWrappedLabel("Sélectionnez l'instantané de référence (avant). Les différences seront calculées vers l'instantané actuellement sélectionné (après).")
	content := container.NewVBox(
		helper,
		widget.NewLabel("Instantané sélectionné : "+target.Time.Local().Format("02/01/2006 15:04")+"  ·  "+target.ShortID),
		widget.NewLabel("Instantané de référence :"),
		compareSelect,
	)
	dialog.NewCustomConfirm("Comparer deux instantanés", "Comparer", "Annuler", content, func(ok bool) {
		if !ok {
			return
		}
		sourceID := ids[compareSelect.Selected]
		if sourceID == "" || sourceID == target.ID {
			return
		}
		var comparison vaulticSnapshotComparison
		originalRepo := vaulticResolvedRepository(c.hooks.ActiveRepository())
		knownSnapshots := append([]vaulticSnapshot(nil), c.snapshots...)
		c.hooks.RunWithDepot("Comparaison des instantanés",
			func(ctx context.Context, g global.Options) error {
				var err error
				comparison, err = vaulticCompareSnapshots(ctx, g, sourceID, target.ID)
				return err
			}, func() error {
				vaulticShowVisualComparison(c.hooks.App, comparison, func(snapshotID, selectedPath string) {
					if originalRepo != vaulticResolvedRepository(c.hooks.ActiveRepository()) {
						dialog.ShowInformation("Dépôt modifié", "Revenez au dépôt d'origine avant de restaurer cet élément.", w)
						return
					}
					var sourceSnapshot vaulticSnapshot
					found := false
					for _, sn := range knownSnapshots {
						if sn.ID == snapshotID {
							sourceSnapshot, found = sn, true
							break
						}
					}
					if !found {
						dialog.ShowInformation("Instantané introuvable", "Actualisez la liste des instantanés.", w)
						return
					}
					vaulticOpenExplorerAtPath(c.hooks.App, sourceSnapshot, func(title string, operation func(context.Context, global.Options) error) {
						if originalRepo != vaulticResolvedRepository(c.hooks.ActiveRepository()) {
							dialog.ShowInformation("Dépôt modifié", "Le dépôt a changé. Fermez cet explorateur puis rechargez l'historique.", w)
							return
						}
						c.hooks.Run(title, operation)
					}, selectedPath)
				})
				return nil
			})
	}, w).Show()
}

func (c *vaulticSnapshotsController) restore() {
	w := c.hooks.Window
	sn, ok := c.Selected()
	if !ok {
		dialog.ShowInformation("Sélection requise", "Sélectionnez d'abord un instantané.", w)
		return
	}
	id := sn.ID
	if !vaulticSnapshotID.MatchString(id) {
		dialog.ShowError(errors.New("Identifiant d'instantané non valide"), w)
		return
	}
	dialog.NewFolderOpen(func(uri fyne.ListableURI, err error) {
		if err != nil {
			dialog.ShowError(err, w)
			return
		}
		if uri == nil {
			return
		}
		dest, err := vaulticRestoreTarget(uri.Path())
		if err != nil {
			dialog.ShowError(err, w)
			return
		}
		dialog.ShowConfirm("Confirmer la restauration",
			"Restaurer l'instantané "+id[:min(len(id), 12)]+" dans :\n"+dest+"\n\nLe dossier doit être vide. Aucun fichier existant ne sera écrasé.",
			func(ok bool) {
				if !ok {
					return
				}
				// Recheck immediately before starting the restoration.
				c.hooks.Run("Restauration", func(ctx context.Context, g global.Options) error {
					return vaulticRestoreSnapshot(ctx, g, id, nil, dest)
				})
			}, w)
	}, w).Show()
}
