package main

import (
	"context"
	"fmt"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"github.com/restic/restic/internal/global"
)

// vaulticProfilesHooks connects the profile and source controls to the rest of
// the application. The controller owns the selected profile, its sources and
// the widgets that edit them; persistence and engine access stay outside.
type vaulticProfilesHooks struct {
	Window        fyne.Window
	Persist       func() error // saves the settings (depots, active repository, profiles)
	Busy          func() bool
	Log           func(string)
	UpdateActions func()
	// Changed runs after the selected profile, its sources or its schedule
	// changed, so dependent views can reload.
	Changed func()
	// Renamed runs after a profile was renamed and saved.
	Renamed func(oldName, newName string)
	// RunWithDepot runs fn through the single engine gate and then done.
	RunWithDepot func(title string, fn func(context.Context, global.Options) error, done func() error)
}

type vaulticProfilesController struct {
	hooks    vaulticProfilesHooks
	settings *vaulticSettings
	index    int
	selected int // selected row of the source list, -1 when none

	list             *widget.List
	path             *widget.Label
	Select           *widget.Select
	ExclusionSummary *widget.Label
	CountValue       *canvas.Text
	Empty            *fyne.Container // shown while the profile has no source

	AddFolder, AddFile, RemoveSource                    *widget.Button
	New, Rename, Duplicate, Delete, Exclusions, Preview *widget.Button
	Section                                             fyne.CanvasObject
}

func newVaulticProfilesController(settings *vaulticSettings, index int, hooks vaulticProfilesHooks) *vaulticProfilesController {
	c := &vaulticProfilesController{hooks: hooks, settings: settings, index: index, selected: -1}
	c.CountValue = vaulticHeading(fmt.Sprintf("%d", len(c.Sources())), 24)
	c.Empty = vaulticEmptyState(vaulticWrappedLabel("Ajoutez un fichier ou un dossier pour préparer votre sauvegarde."))
	c.list = widget.NewList(
		func() int { return len(c.Sources()) },
		vaulticListLabel,
		func(id widget.ListItemID, obj fyne.CanvasObject) {
			if sources := c.Sources(); id >= 0 && id < len(sources) {
				obj.(*widget.Label).SetText(sources[id])
			}
		},
	)
	c.path = widget.NewLabel("")
	c.list.OnSelected = func(id widget.ListItemID) {
		sources := c.Sources()
		if id < 0 || id >= len(sources) {
			return
		}
		c.selected = id
		c.path.SetText(sources[id])
		hooks.UpdateActions()
	}
	c.list.OnUnselected = func(id widget.ListItemID) {
		if c.selected == id {
			c.selected = -1
			c.path.SetText("")
			hooks.UpdateActions()
		}
	}
	c.AddFolder = widget.NewButton("+ Dossier", func() {
		dialog.NewFolderOpen(func(uri fyne.ListableURI, err error) {
			if err != nil {
				dialog.ShowError(err, hooks.Window)
				return
			}
			if uri != nil {
				c.updateSources(vaulticAppendUnique(c.Sources(), uri.Path()))
			}
		}, hooks.Window).Show()
	})
	c.AddFile = widget.NewButton("+ Fichier", func() {
		dialog.NewFileOpen(func(f fyne.URIReadCloser, err error) {
			if err != nil {
				dialog.ShowError(err, hooks.Window)
				return
			}
			if f != nil {
				path := f.URI().Path()
				_ = f.Close()
				c.updateSources(vaulticAppendUnique(c.Sources(), path))
			}
		}, hooks.Window).Show()
	})
	c.RemoveSource = widget.NewButton("Retirer", func() {
		sources := c.Sources()
		if c.selected < 0 || c.selected >= len(sources) {
			return
		}
		next := append([]string(nil), sources[:c.selected]...)
		next = append(next, sources[c.selected+1:]...)
		c.updateSources(next)
	})
	c.Section = container.NewBorder(
		container.NewVBox(widget.NewLabelWithStyle("Sources", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}), container.NewHBox(c.AddFolder, c.AddFile, c.RemoveSource)),
		container.NewHScroll(c.path), nil, nil, container.NewStack(c.list, c.Empty),
	)

	c.Select = widget.NewSelect(nil, nil)
	c.ExclusionSummary = widget.NewLabel("Aucune exclusion")
	c.Select.OnChanged = func(name string) {
		for index, profile := range c.settings.Profiles {
			if profile.Name == name && index != c.index {
				c.Apply(c.settings.Profiles, index)
				return
			}
		}
	}
	c.New = widget.NewButton("Nouveau…", func() { c.editName(true) })
	c.Rename = widget.NewButton("Renommer…", func() { c.editName(false) })
	c.Duplicate = widget.NewButtonWithIcon("Dupliquer…", theme.ContentCopyIcon(), c.duplicate)
	c.Delete = widget.NewButtonWithIcon("Supprimer le profil…", theme.DeleteIcon(), c.delete)
	c.Exclusions = widget.NewButtonWithIcon("Exclusions…", theme.ContentRemoveIcon(), c.editExclusions)
	c.Preview = widget.NewButtonWithIcon("Aperçu des exclusions…", theme.SearchIcon(), c.previewExclusions)
	return c
}

// Selected is the selected row of the source list, -1 when none.
func (c *vaulticProfilesController) Selected() int { return c.selected }

// Index is the position of the selected profile in the settings.
func (c *vaulticProfilesController) Index() int { return c.index }

// Current returns the selected profile.
func (c *vaulticProfilesController) Current() vaulticBackupProfile {
	return c.settings.Profiles[c.index]
}

// Sources returns the sources of the selected profile.
func (c *vaulticProfilesController) Sources() []string {
	return c.settings.Profiles[c.index].Sources
}

// Buttons lists the controls whose enabled state follows the application state.
func (c *vaulticProfilesController) Buttons() []*widget.Button {
	return []*widget.Button{c.AddFolder, c.AddFile, c.RemoveSource, c.Preview}
}

func (c *vaulticProfilesController) refreshSources() {
	c.list.Refresh()
	c.CountValue.Text = fmt.Sprintf("%d", len(c.Sources()))
	c.CountValue.Refresh()
	c.hooks.UpdateActions()
}

// Refresh redraws the profile selector and the exclusion summary.
func (c *vaulticProfilesController) Refresh() {
	if c.hooks.Changed != nil {
		defer c.hooks.Changed()
	}
	names := make([]string, len(c.settings.Profiles))
	for i, profile := range c.settings.Profiles {
		names[i] = profile.Name
	}
	c.Select.SetOptions(names)
	if c.Select.Selected != names[c.index] {
		c.Select.SetSelected(names[c.index])
	}
	if count := len(c.settings.Profiles[c.index].Excludes); count > 0 {
		c.ExclusionSummary.SetText(fmt.Sprintf("%d exclusion(s)", count))
	} else {
		c.ExclusionSummary.SetText("Aucune exclusion")
	}
}

func (c *vaulticProfilesController) updateSources(next []string) {
	previous := c.Sources()
	c.settings.Profiles[c.index].Sources = next
	if err := c.hooks.Persist(); err != nil {
		c.settings.Profiles[c.index].Sources = previous
		dialog.ShowError(err, c.hooks.Window)
		return
	}
	c.list.UnselectAll()
	c.refreshSources()
}

// Apply stores next as the profile list with index selected. On a save error
// the previous state is restored, the error is shown and false is returned.
func (c *vaulticProfilesController) Apply(next []vaulticBackupProfile, index int) bool {
	previousProfiles, previousIndex := c.settings.Profiles, c.index
	c.settings.Profiles, c.index = next, index
	if err := c.hooks.Persist(); err != nil {
		c.settings.Profiles, c.index = previousProfiles, previousIndex
		c.Refresh()
		dialog.ShowError(err, c.hooks.Window)
		return false
	}
	c.list.UnselectAll()
	c.Refresh()
	c.refreshSources()
	return true
}

func (c *vaulticProfilesController) editName(create bool) {
	w := c.hooks.Window
	title := "Renommer le profil"
	name := widget.NewEntry()
	if create {
		title = "Nouveau profil"
	} else {
		name.SetText(c.Current().Name)
	}
	form := dialog.NewForm(title, "Enregistrer", "Annuler", []*widget.FormItem{
		widget.NewFormItem("Nom", name),
	}, func(ok bool) {
		if !ok {
			return
		}
		label := strings.TrimSpace(name.Text)
		if label == "" || len([]rune(label)) > 40 {
			dialog.ShowInformation("Nom du profil", "Saisissez un nom de 1 à 40 caractères.", w)
			return
		}
		for i, profile := range c.settings.Profiles {
			if strings.EqualFold(profile.Name, label) && (create || i != c.index) {
				dialog.ShowInformation("Nom déjà utilisé", "Choisissez un autre nom pour ce profil.", w)
				return
			}
		}
		next := append([]vaulticBackupProfile(nil), c.settings.Profiles...)
		index := c.index
		if create {
			index = len(next)
			next = append(next, vaulticBackupProfile{Name: label})
			c.Apply(next, index)
			return
		}
		oldName := next[index].Name
		next[index].Name = label
		if c.Apply(next, index) && oldName != label && c.hooks.Renamed != nil {
			c.hooks.Renamed(oldName, label)
		}
	}, w)
	form.Show()
	w.Canvas().Focus(name)
}

func (c *vaulticProfilesController) duplicate() {
	w := c.hooks.Window
	from := c.index
	field := widget.NewEntry()
	field.SetText(vaulticSuggestedCopyName(c.settings.Profiles, from))
	dialog.NewForm("Dupliquer le profil", "Dupliquer", "Annuler", []*widget.FormItem{
		widget.NewFormItem("Nom de la copie", field),
	}, func(ok bool) {
		if !ok || c.hooks.Busy() || from != c.index {
			return
		}
		updated, index, err := vaulticDuplicateSavedProfile(c.settings.Profiles, from, field.Text)
		if err != nil {
			dialog.ShowError(err, w)
			return
		}
		if c.Apply(updated, index) {
			c.hooks.Log("Profil dupliqué (sources et exclusions copiées).")
		}
	}, w).Show()
	w.Canvas().Focus(field)
}

func (c *vaulticProfilesController) delete() {
	if len(c.settings.Profiles) <= 1 {
		return
	}
	w := c.hooks.Window
	target := c.index
	name := c.settings.Profiles[target].Name
	dialog.ShowConfirm("Supprimer le profil",
		"Supprimer uniquement le profil « "+name+" » de Vaultic ?\n\nLes dépôts restic et les sauvegardes existantes ne seront pas modifiés.",
		func(ok bool) {
			if !ok || c.hooks.Busy() || target != c.index {
				return
			}
			updated, index, err := vaulticDeleteSavedProfile(c.settings.Profiles, target)
			if err != nil {
				dialog.ShowError(err, w)
				return
			}
			if c.Apply(updated, index) {
				c.hooks.Log("Profil supprimé de la configuration : " + name + " (snapshots inchangés).")
			}
		}, w)
}

func (c *vaulticProfilesController) editExclusions() {
	w := c.hooks.Window
	targetProfile := c.index
	editor := widget.NewMultiLineEntry()
	editor.SetMinRowsVisible(10)
	editor.Wrapping = fyne.TextWrapWord
	editor.SetPlaceHolder("**/node_modules\n**/.cache\n*.tmp")
	editor.SetText(strings.Join(c.settings.Profiles[targetProfile].Excludes, "\n"))
	explanation := vaulticWrappedLabel("Une règle restic par ligne. Les lignes vides ou commençant par # sont ignorées. Des exceptions peuvent commencer par !. Les exclusions ne concernent que ce profil.")
	scroll := container.NewVScroll(editor)
	scroll.SetMinSize(fyne.NewSize(500, 290))
	content := container.NewBorder(explanation, nil, nil, nil, scroll)
	dialog.NewCustomConfirm("Exclusions du profil", "Enregistrer", "Annuler", content, func(ok bool) {
		if !ok {
			return
		}
		if targetProfile != c.index {
			dialog.ShowInformation("Profil modifié", "Sélectionnez à nouveau le profil avant de modifier ses exclusions.", w)
			return
		}
		rules, err := vaulticParseExclusions(editor.Text)
		if err != nil {
			dialog.ShowError(err, w)
			return
		}
		next := append([]vaulticBackupProfile(nil), c.settings.Profiles...)
		next[targetProfile].Excludes = rules
		c.Apply(next, targetProfile)
	}, w).Show()
}

func (c *vaulticProfilesController) previewExclusions() {
	w := c.hooks.Window
	sources := c.Sources()
	if len(sources) == 0 {
		dialog.ShowInformation("Sources manquantes", "Ajoutez d'abord un dossier ou un fichier au profil.", w)
		return
	}
	rules := append([]string(nil), c.Current().Excludes...)
	if len(rules) == 0 {
		dialog.ShowInformation("Aucune exclusion", "Ajoutez des règles d'exclusion pour ce profil avant de lancer un aperçu.", w)
		return
	}
	selected := append([]string(nil), sources...)
	var result vaulticExclusionPreview
	c.hooks.RunWithDepot("Aperçu des exclusions",
		func(ctx context.Context, g global.Options) error {
			cleaned, err := vaulticSourcesForBackup(g.Repo, selected)
			if err != nil {
				return err
			}
			result, err = vaulticPreviewExcludedPaths(ctx, cleaned, rules)
			return err
		},
		func() error {
			details := vaulticWrappedLabel(vaulticFormatExclusionPreview(result))
			content := container.NewVScroll(details)
			content.SetMinSize(fyne.NewSize(560, 340))
			dlg := dialog.NewCustom("Aperçu indicatif des exclusions", "Fermer", content, w)
			dlg.Resize(fyne.NewSize(640, 440))
			dlg.Show()
			return nil
		})
}
