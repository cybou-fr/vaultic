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
		vaulticHeading(vaulticT("Dernière activité"), 19),
		p.RecentSnapshot,
		p.ViewSnapshots,
	))
	statGrid := container.NewVBox(
		container.NewGridWithColumns(2,
			vaulticStatCard(vaulticT("SOURCES CONFIGURÉES"), p.SourceCount),
			vaulticStatCard(vaulticT("INSTANTANÉS CONSULTÉS"), p.SnapshotCount),
		),
		container.NewGridWithColumns(2,
			vaulticStatCard(vaulticT("DÉPÔT ACTIF"), p.Repository),
			vaulticStatCard(vaulticT("DERNIÈRE OPÉRATION RÉUSSIE"), p.RecentRun),
		),
	)
	return container.NewVScroll(container.NewPadded(container.NewVBox(
		vaulticHeading(vaulticT("Vue d’ensemble"), 26),
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
		vaulticHeading(vaulticT("Dépôts enregistrés"), 20),
		p.SavedList,
		vaulticWrappedLabel(vaulticT("« Oublier » retire seulement le dépôt de Vaultic : aucune sauvegarde n'est supprimée, localement ou à distance.")),
	))
	activeConfiguration := vaulticSurface(container.NewVBox(
		vaulticHeading(vaulticT("Dépôt actif"), 20),
		p.ActiveRepository,
		vaulticWrappedLabel(vaulticT("Saisissez le mot de passe restic du dépôt sélectionné, puis vérifiez la connexion. Sans mot de passe, seul l'accès au dépôt est testé.")),
		p.Password,
		p.Remember,
		p.Verify,
		p.EditAuth,
		p.ClearAuth,
	))
	return container.NewScroll(container.NewPadded(container.NewVBox(
		vaulticHeading(vaulticT("Dépôts"), 26),
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
	Retention                       fyne.CanvasObject
}

// vaulticBackupPage returns the page and the scroll container of the
// Automatisation tab, which needs focus-following.
func vaulticBackupPage(p vaulticBackupParts, automationContent fyne.CanvasObject) (page fyne.CanvasObject, scrolls []*container.Scroll) {
	automationScroll := container.NewScroll(container.NewPadded(automationContent))
	retentionScroll := container.NewScroll(container.NewPadded(p.Retention))
	tabs := container.NewAppTabs(
		container.NewTabItemWithIcon(vaulticT("Lancement"), theme.MediaPlayIcon(), container.NewVScroll(container.NewBorder(
			container.NewPadded(container.NewVBox(
				container.NewHBox(p.DuplicateProfile, p.DeleteProfile),
				container.NewHBox(p.Exclusions, p.PreviewExclusions),
				p.ExclusionSummary,
			)),
			container.NewPadded(vaulticSurface(container.NewVBox(
				vaulticWrappedLabel(vaulticT("Le dépôt doit se trouver en dehors des dossiers sauvegardés.")),
				container.NewGridWithColumns(2, p.BackupNow, p.LastReport),
			))),
			nil, nil,
			container.NewPadded(vaulticSurface(p.Sources)),
		))),
		container.NewTabItemWithIcon(vaulticT("Automatisation"), theme.HistoryIcon(), automationScroll),
		container.NewTabItemWithIcon(vaulticT("Conservation"), theme.DeleteIcon(), retentionScroll),
	)
	page = container.NewBorder(
		container.NewPadded(container.NewVBox(
			vaulticHeading(vaulticT("Sauvegarde"), 26),
			widget.NewForm(widget.NewFormItem(vaulticT("Profil"), container.NewBorder(nil, nil, nil,
				container.NewHBox(p.NewProfile, p.RenameProfile), p.Profile))),
		)),
		nil, nil, nil,
		tabs,
	)
	return page, []*container.Scroll{automationScroll, retentionScroll}
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
			vaulticPageHeading(vaulticT("Chronologie des instantanés"), vaulticT("Parcourez les versions par date, comparez les changements et restaurez vos fichiers.")),
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
		vaulticPageHeading(vaulticT("Stockage"), vaulticT("Mesurez les données réellement référencées par les instantanés.")),
		vaulticSurface(container.NewVBox(
			vaulticWrappedLabel(vaulticT("Cette analyse lit les métadonnées du dépôt. Elle peut prendre du temps sur les dépôts volumineux et peut être annulée.")),
			p.Analyze,
			p.Updated,
		)),
		container.NewGridWithColumns(2,
			vaulticStatCard(vaulticT("VOLUME DES BLOBS"), p.Volume),
			vaulticStatCard(vaulticT("BLOBS RÉFÉRENCÉS"), p.Blobs),
		),
		vaulticStatCard(vaulticT("INSTANTANÉS ANALYSÉS"), p.Snapshots),
		vaulticSurface(vaulticWrappedLabel(vaulticT("Mesure restic raw-data : taille des blocs référencés, hors métadonnées du dépôt, fichiers non référencés et espace disque libre. Ce n'est pas la taille physique complète du dépôt."))),
	)))
}

func vaulticIntegrityPage(check fyne.CanvasObject) *container.Scroll {
	description := widget.NewLabel(vaulticT("La vérification contrôle la structure du dépôt. Elle ne supprime pas vos sauvegardes."))
	description.Wrapping = fyne.TextWrapWord
	return container.NewVScroll(container.NewPadded(container.NewVBox(
		vaulticHeading(vaulticT("Intégrité"), 26),
		vaulticSurface(container.NewVBox(
			vaulticHeading(vaulticT("Contrôle du dépôt"), 20),
			description,
			check,
		)),
	)))
}

func vaulticActivityPage(logs fyne.CanvasObject, clear func()) fyne.CanvasObject {
	return container.NewBorder(
		container.NewPadded(vaulticHeading(vaulticT("Activité"), 26)),
		nil, nil, nil, container.NewPadded(vaulticSurface(container.NewBorder(
			container.NewHBox(widget.NewLabel(vaulticT("Événements de cette session")), layout.NewSpacer(), widget.NewButtonWithIcon(vaulticT("Effacer"), theme.ContentClearIcon(), clear)),
			nil, nil, nil, container.NewVScroll(logs),
		))),
	)
}

func vaulticSettingsPage(background, language, update, diagnostics fyne.CanvasObject) *container.Scroll {
	return container.NewVScroll(container.NewPadded(container.NewVBox(
		vaulticHeading(vaulticT("Paramètres"), 26),
		vaulticSurface(container.NewVBox(
			vaulticHeading(vaulticT("Langue")+" / Language", 19),
			language,
		)),
		vaulticSurface(container.NewVBox(
			vaulticHeading(vaulticT("Arrière-plan"), 19),
			background,
		)),
		vaulticSurface(container.NewVBox(
			vaulticHeading(vaulticT("Diagnostic"), 19),
			diagnostics,
		)),
		vaulticSurface(container.NewVBox(
			vaulticHeading(vaulticT("À propos de Vaultic"), 19),
			vaulticWrappedLabel(vaulticT("Les dépôts Vaultic utilisent le format restic.")),
			vaulticWrappedLabel(vaulticT("Licence Apache-2.0 (Vaultic), BSD-2-Clause (restic).")),
			widget.NewLabel("Vaultic "+vaulticVersionLine()),
			widget.NewLabel(vaulticT("Auteur : ")+vaulticAuthor),
			widget.NewLabel(vaulticT("Moteur intégré — ")+global.Version),
			update,
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

// Rebuilt by vaulticLocalizeTables.
var vaulticNavLabels = []string{vaulticL("Vue d'ensemble"), vaulticL("Sauvegarde"), vaulticL("Instantanés"), vaulticL("Stockage"), vaulticL("Intégrité"), vaulticL("Activité"), vaulticL("Dépôts"), vaulticL("Paramètres")}

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
	brand := container.NewHBox(container.NewGridWrap(fyne.NewSize(40, 40), widget.NewIcon(vaulticAppIcon())), container.NewCenter(vaulticHeading(vaulticT("Vaultic"), 24)))
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
	title := widget.NewLabelWithStyle(vaulticT("ÉTAT"), fyne.TextAlignLeading, fyne.TextStyle{Bold: true})
	return container.NewVBox(
		widget.NewSeparator(),
		progress,
		container.NewBorder(nil, nil, title, cancel, status),
	)
}
