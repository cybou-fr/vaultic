package main

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"github.com/restic/restic/internal/global"
)

// Page builders. Each one only lays out widgets that vaulticMain created: they
// own no state and call no engine code, so they can be built and tested apart
// from the application shell.

// Indexes of the pages in the navigation, shared by the page list and the
// shortcuts of the overview.
const (
	vaulticPageOverview = iota
	vaulticPageBackup
	vaulticPageSnapshots
	vaulticPageStorage
	vaulticPageIntegrity
	vaulticPageActivity
	vaulticPageRepositories
	vaulticPageSettings
)

type vaulticOverviewParts struct {
	SourceCount, SnapshotCount fyne.CanvasObject
	Repository                 fyne.CanvasObject
	RecentRun                  fyne.CanvasObject
	RecentSnapshot             fyne.CanvasObject
	StartBackup, Setup         *widget.Button
	ViewSnapshots, ViewStorage *widget.Button
}

func vaulticOverviewPage(p vaulticOverviewParts) *container.Scroll {
	snapshotOverview := vaulticSurface(container.NewVBox(
		vaulticHeading("Dernière activité", 19),
		p.RecentSnapshot,
		p.ViewSnapshots,
	))
	statGrid := container.NewVBox(
		container.NewGridWithColumns(2,
			vaulticStatCard("SOURCES CONFIGURÉES", p.SourceCount),
			vaulticStatCard("INSTANTANÉS CONSULTÉS", p.SnapshotCount),
		),
		container.NewGridWithColumns(2,
			vaulticStatCard("DÉPÔT ACTIF", p.Repository),
			vaulticStatCard("DERNIÈRE OPÉRATION RÉUSSIE", p.RecentRun),
		),
	)
	return container.NewVScroll(container.NewPadded(container.NewVBox(
		vaulticHeading("Vue d’ensemble", 26),
		container.NewHBox(p.StartBackup, p.Setup, p.ViewStorage), statGrid, snapshotOverview,
	)))
}

type vaulticRepositoriesParts struct {
	SavedList                   fyne.CanvasObject
	ActiveRepository            fyne.CanvasObject
	Password                    fyne.CanvasObject
	Remember                    fyne.CanvasObject
	Verify, EditAuth, ClearAuth fyne.CanvasObject
	Setup                       fyne.CanvasObject
}

func vaulticRepositoriesPage(p vaulticRepositoriesParts) *container.Scroll {
	savedRepositories := vaulticSurface(container.NewVBox(
		vaulticHeading("Dépôts enregistrés", 20),
		p.SavedList,
		vaulticWrappedLabel("« Oublier » retire seulement le dépôt de Vaultic : aucune sauvegarde n'est supprimée, localement ou à distance."),
	))
	activeConfiguration := vaulticSurface(container.NewVBox(
		vaulticHeading("Dépôt actif", 20),
		p.ActiveRepository,
		vaulticWrappedLabel("Saisissez le mot de passe restic du dépôt sélectionné, puis vérifiez la connexion. Sans mot de passe, seul l'accès au dépôt est testé."),
		p.Password,
		p.Remember,
		p.Verify,
		p.EditAuth,
		p.ClearAuth,
	))
	return container.NewScroll(container.NewPadded(container.NewVBox(
		vaulticHeading("Dépôts", 26),
		savedRepositories,
		activeConfiguration,
		p.Setup,
	)))
}

type vaulticBackupParts struct {
	Profile                         fyne.CanvasObject // profile drop-down
	NewProfile, RenameProfile       fyne.CanvasObject
	DuplicateProfile, DeleteProfile fyne.CanvasObject
	Exclusions, PreviewExclusions   fyne.CanvasObject
	ExclusionSummary                fyne.CanvasObject
	Sources                         fyne.CanvasObject // already framed source section
	BackupNow, LastReport           fyne.CanvasObject
}

// vaulticBackupPage returns the page and the scroll container of the
// Automatisation tab, which needs focus-following.
func vaulticBackupPage(p vaulticBackupParts, automationContent fyne.CanvasObject) (page fyne.CanvasObject, automationScroll *container.Scroll) {
	automationScroll = container.NewScroll(container.NewPadded(automationContent))
	tabs := container.NewAppTabs(
		container.NewTabItemWithIcon("Lancement", theme.MediaPlayIcon(), container.NewVScroll(container.NewBorder(
			container.NewPadded(container.NewVBox(
				container.NewHBox(p.DuplicateProfile, p.DeleteProfile),
				container.NewHBox(p.Exclusions, p.PreviewExclusions),
				p.ExclusionSummary,
			)),
			container.NewPadded(vaulticSurface(container.NewVBox(
				vaulticWrappedLabel("Le dépôt doit se trouver en dehors des dossiers sauvegardés."),
				container.NewGridWithColumns(2, p.BackupNow, p.LastReport),
			))),
			nil, nil,
			container.NewPadded(vaulticSurface(p.Sources)),
		))),
		container.NewTabItemWithIcon("Automatisation", theme.HistoryIcon(), automationScroll),
	)
	page = container.NewBorder(
		container.NewPadded(container.NewVBox(
			vaulticHeading("Sauvegarde", 26),
			widget.NewForm(widget.NewFormItem("Profil", container.NewBorder(nil, nil, nil,
				container.NewHBox(p.NewProfile, p.RenameProfile), p.Profile))),
		)),
		nil, nil, nil,
		tabs,
	)
	return page, automationScroll
}

type vaulticSnapshotParts struct {
	Refresh                fyne.CanvasObject
	Details                fyne.CanvasObject // scrollable details text
	DetailsButton, Compare fyne.CanvasObject
	Explorer, Restore      fyne.CanvasObject
	List, Empty            fyne.CanvasObject
}

func vaulticSnapshotPage(p vaulticSnapshotParts) fyne.CanvasObject {
	return container.NewBorder(
		container.NewPadded(container.NewVBox(
			vaulticPageHeading("Chronologie des instantanés", "Parcourez les versions par date, comparez les changements et restaurez vos fichiers."),
			p.Refresh,
		)),
		container.NewPadded(vaulticSurface(container.NewVBox(
			p.Details,
			container.NewGridWithColumns(2, p.DetailsButton, p.Compare),
			container.NewGridWithColumns(2, p.Explorer, p.Restore),
		))),
		nil, nil,
		container.NewPadded(vaulticSurface(container.NewStack(p.List, p.Empty))),
	)
}

type vaulticStorageParts struct {
	Analyze                  fyne.CanvasObject
	Updated                  fyne.CanvasObject
	Volume, Blobs, Snapshots fyne.CanvasObject
}

func vaulticStoragePage(p vaulticStorageParts) *container.Scroll {
	return container.NewVScroll(container.NewPadded(container.NewVBox(
		vaulticPageHeading("Stockage", "Mesurez les données réellement référencées par les instantanés."),
		vaulticSurface(container.NewVBox(
			vaulticWrappedLabel("Cette analyse lit les métadonnées du dépôt. Elle peut prendre du temps sur les dépôts volumineux et peut être annulée."),
			p.Analyze,
			p.Updated,
		)),
		container.NewGridWithColumns(2,
			vaulticStatCard("VOLUME DES BLOBS", p.Volume),
			vaulticStatCard("BLOBS RÉFÉRENCÉS", p.Blobs),
		),
		vaulticStatCard("INSTANTANÉS ANALYSÉS", p.Snapshots),
		vaulticSurface(vaulticWrappedLabel("Mesure restic raw-data : taille des blocs référencés, hors métadonnées du dépôt, fichiers non référencés et espace disque libre. Ce n'est pas la taille physique complète du dépôt.")),
	)))
}

func vaulticIntegrityPage(check fyne.CanvasObject) *container.Scroll {
	description := widget.NewLabel("La vérification contrôle la structure du dépôt. Elle ne supprime pas vos sauvegardes.")
	description.Wrapping = fyne.TextWrapWord
	return container.NewVScroll(container.NewPadded(container.NewVBox(
		vaulticHeading("Intégrité", 26),
		vaulticSurface(container.NewVBox(
			vaulticHeading("Contrôle du dépôt", 20),
			description,
			check,
		)),
	)))
}

func vaulticActivityPage(logs fyne.CanvasObject, clear func()) fyne.CanvasObject {
	return container.NewBorder(
		container.NewPadded(vaulticHeading("Activité", 26)),
		nil, nil, nil, container.NewPadded(vaulticSurface(container.NewBorder(
			container.NewHBox(widget.NewLabel("Événements de cette session"), layout.NewSpacer(), widget.NewButtonWithIcon("Effacer", theme.ContentClearIcon(), clear)),
			nil, nil, nil, container.NewVScroll(logs),
		))),
	)
}

func vaulticSettingsPage(background fyne.CanvasObject) *container.Scroll {
	return container.NewVScroll(container.NewPadded(container.NewVBox(
		vaulticHeading("Paramètres", 26),
		vaulticSurface(container.NewVBox(
			vaulticHeading("Arrière-plan", 19),
			background,
		)),
		vaulticSurface(container.NewVBox(
			vaulticHeading("À propos de Vaultic", 19),
			vaulticWrappedLabel("Les dépôts Vaultic utilisent le format restic."),
			vaulticWrappedLabel("Licence Apache-2.0 (Vaultic), BSD-2-Clause (restic)."),
			widget.NewLabel("Moteur intégré — "+global.Version),
		)),
	)))
}

// vaulticNavigation owns the sidebar, the page stack and the navigation state.
type vaulticNavigation struct {
	Sidebar fyne.CanvasObject
	Pages   *fyne.Container // stack showing the current page
	pages   []fyne.CanvasObject
	buttons []*widget.Button
}

var vaulticNavLabels = []string{"Vue d'ensemble", "Sauvegarde", "Instantanés", "Stockage", "Intégrité", "Activité", "Dépôts", "Paramètres"}

func vaulticNewNavigation(pages []fyne.CanvasObject) *vaulticNavigation {
	n := &vaulticNavigation{pages: pages, Pages: container.NewStack(pages[0])}
	icons := []fyne.Resource{theme.HomeIcon(), theme.StorageIcon(), theme.HistoryIcon(), theme.StorageIcon(), theme.ConfirmIcon(), theme.ListIcon(), theme.StorageIcon(), theme.SettingsIcon()}
	items := make([]fyne.CanvasObject, 0, len(vaulticNavLabels))
	for i := range vaulticNavLabels {
		index := i
		button := widget.NewButtonWithIcon(vaulticNavLabels[i], icons[i], func() { n.Navigate(index) })
		button.Alignment = widget.ButtonAlignLeading
		button.Importance = widget.LowImportance
		n.buttons = append(n.buttons, button)
		items = append(items, button)
	}
	brand := container.NewHBox(container.NewGridWrap(fyne.NewSize(40, 40), widget.NewIcon(vaulticAppIcon())), container.NewCenter(vaulticHeading("Vaultic", 24)))
	n.Sidebar = container.NewStack(canvas.NewRectangle(vaulticSidebar), container.NewPadded(container.NewBorder(
		container.NewVBox(brand, widget.NewSeparator()), nil,
		nil, nil,
		container.NewVScroll(container.New(layout.NewCustomPaddedVBoxLayout(4), items...)),
	)))
	return n
}

// Navigate shows the page at index and highlights its sidebar button.
func (n *vaulticNavigation) Navigate(index int) {
	if index < 0 || index >= len(n.pages) {
		return
	}
	n.Pages.Objects = []fyne.CanvasObject{n.pages[index]}
	n.Pages.Refresh()
	for i, b := range n.buttons {
		if i == index {
			b.Importance = widget.HighImportance
		} else {
			b.Importance = widget.LowImportance
		}
		b.Refresh()
	}
}

// vaulticStatusBar is the strip under every page: progress and status text.
func vaulticStatusBar(progress, status, cancel fyne.CanvasObject) fyne.CanvasObject {
	title := widget.NewLabelWithStyle("ÉTAT", fyne.TextAlignLeading, fyne.TextStyle{Bold: true})
	return container.NewVBox(
		widget.NewSeparator(),
		progress,
		container.NewBorder(nil, nil, title, cancel, status),
	)
}
