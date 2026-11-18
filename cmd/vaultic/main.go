package main

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/widget"

	"github.com/cybou-fr/vaultic/internal/restic"
	"github.com/cybou-fr/vaultic/internal/settings"
)

func main() {
	a := app.NewWithID("fr.cybou.vaultic")
	w := a.NewWindow("Vaultic")
	w.Resize(fyne.NewSize(980, 720))

	saved := settings.Load()
	sources := append([]string(nil), saved.Sources...)
	snapshots := []restic.Snapshot{}
	selectedSource := -1
	selectedSnapshot := -1

	binary := widget.NewEntry()
	binary.SetPlaceHolder("restic ou chemin complet vers restic.exe")
	binary.SetText(saved.ResticBinary)

	repo := widget.NewEntry()
	repo.SetPlaceHolder("Ex. : E:\\Backup\\restic ou /media/ssd/backup")
	repo.SetText(saved.Repository)

	password := widget.NewPasswordEntry()
	password.SetPlaceHolder("Mot de passe restic — non enregistré")

	status := widget.NewLabel("Prêt")
	status.Wrapping = fyne.TextWrapWord
	progress := widget.NewProgressBar()
	progress.SetValue(0)

	logView := widget.NewMultiLineEntry()
	logView.Wrapping = fyne.TextWrapWord
	logView.SetMinRowsVisible(5)
	logView.Disable()
	logData := ""
	appendLog := func(s string) {
		if strings.TrimSpace(s) == "" {
			return
		}
		logData += time.Now().Format("15:04:05") + "  " + s + "\n"
		if len(logData) > 16000 {
			logData = logData[len(logData)-12000:]
		}
		logView.SetText(logData)
	}

	save := func() {
		if err := settings.Save(settings.Settings{
			ResticBinary: strings.TrimSpace(binary.Text),
			Repository:   strings.TrimSpace(repo.Text),
			Sources:      append([]string(nil), sources...),
		}); err != nil {
			appendLog("Impossible d'enregistrer les paramètres : " + err.Error())
		}
	}
	currentClient := func() restic.Client {
		return restic.Client{
			Binary:     strings.TrimSpace(binary.Text),
			Repository: strings.TrimSpace(repo.Text),
			Password:   password.Text,
		}
	}

	var busy atomic.Bool
	var cancelOp context.CancelFunc
	cancelButton := widget.NewButton("Arrêter", func() {
		if cancelOp != nil {
			cancelOp()
			status.SetText("Arrêt de l'opération…")
		}
	})
	cancelButton.Disable()
	actionButtons := []*widget.Button{}

	setBusy := func(v bool) {
		if v {
			cancelButton.Enable()
			for _, btn := range actionButtons {
				btn.Disable()
			}
		} else {
			cancelButton.Disable()
			for _, btn := range actionButtons {
				btn.Enable()
			}
		}
	}

	updateEvent := func(e restic.Event) {
		fyne.Do(func() {
			if e.Progress != nil {
				p := e.Progress
				switch p.MessageType {
				case "status":
					if p.PercentDone >= 0 && p.PercentDone <= 1 {
						progress.SetValue(p.PercentDone)
					}
					status.SetText(fmt.Sprintf("Sauvegarde : %.1f %% — %d / %d fichiers", 100*p.PercentDone, p.FilesDone, p.TotalFiles))
				case "summary":
					progress.SetValue(1)
					appendLog("Instantané créé : " + p.SnapshotID)
					if p.ErrorCount > 0 {
						appendLog(fmt.Sprintf("Attention : %d erreurs pendant la sauvegarde", p.ErrorCount))
					}
				case "error":
					appendLog("restic a signalé une erreur de traitement d'un fichier")
				}
			}
			if e.Line != "" {
				appendLog(e.Line)
			}
		})
	}

	run := func(name string, fn func(context.Context) error) {
		if !busy.CompareAndSwap(false, true) {
			dialog.ShowInformation("Opération en cours", "Attendez la fin de l'opération en cours ou arrêtez-la.", w)
			return
		}
		save()
		ctx, cancel := context.WithCancel(context.Background())
		cancelOp = cancel
		setBusy(true)
		progress.SetValue(0)
		status.SetText(name + "…")
		appendLog("Début : " + name)
		go func() {
			err := fn(ctx)
			fyne.Do(func() {
				cancel()
				cancelOp = nil
				busy.Store(false)
				setBusy(false)
				if err != nil {
					status.SetText("Erreur : " + name)
					appendLog("Erreur : " + err.Error())
					dialog.ShowError(err, w)
					return
				}
				status.SetText(name + " — terminé")
				appendLog("Terminé : " + name)
			})
		}()
	}

	browseRepo := widget.NewButton("Parcourir…", func() {
		d := dialog.NewFolderOpen(func(uri fyne.ListableURI, err error) {
			if err != nil {
				dialog.ShowError(err, w)
				return
			}
			if uri != nil {
				repo.SetText(uri.Path())
				save()
			}
		}, w)
		d.Show()
	})
	initButton := widget.NewButton("Initialiser le dépôt", func() {
		c := currentClient()
		run("Initialisation du dépôt", func(ctx context.Context) error { return c.Init(ctx, updateEvent) })
	})
	versionButton := widget.NewButton("Vérifier restic", func() {
		c := currentClient()
		run("Vérification de restic", func(ctx context.Context) error {
			v, err := c.Version(ctx)
			if err == nil {
				updateEvent(restic.Event{Line: v})
			}
			return err
		})
	})

	sourcesList := widget.NewList(
		func() int { return len(sources) },
		func() fyne.CanvasObject { return widget.NewLabel("") },
		func(id widget.ListItemID, obj fyne.CanvasObject) {
			if id >= 0 && id < len(sources) {
				obj.(*widget.Label).SetText(sources[id])
			}
		},
	)
	sourcesList.OnSelected = func(id widget.ListItemID) { selectedSource = id }

	addFolder := widget.NewButton("+ Dossier", func() {
		dialog.NewFolderOpen(func(uri fyne.ListableURI, err error) {
			if err != nil {
				dialog.ShowError(err, w)
				return
			}
			if uri != nil {
				sources = appendUnique(sources, uri.Path())
				sourcesList.Refresh()
				save()
			}
		}, w).Show()
	})
	addFile := widget.NewButton("+ Fichier", func() {
		dialog.NewFileOpen(func(file fyne.URIReadCloser, err error) {
			if err != nil {
				dialog.ShowError(err, w)
				return
			}
			if file != nil {
				path := file.URI().Path()
				_ = file.Close()
				sources = appendUnique(sources, path)
				sourcesList.Refresh()
				save()
			}
		}, w).Show()
	})
	removeSource := widget.NewButton("Retirer de la liste", func() {
		if selectedSource < 0 || selectedSource >= len(sources) {
			return
		}
		sources = append(sources[:selectedSource], sources[selectedSource+1:]...)
		selectedSource = -1
		sourcesList.UnselectAll()
		sourcesList.Refresh()
		save()
	})
	backupButton := widget.NewButton("Lancer la sauvegarde", func() {
		c := currentClient()
		selected := append([]string(nil), sources...)
		run("Sauvegarde", func(ctx context.Context) error {
			return c.Backup(ctx, selected, updateEvent)
		})
	})
	backupButton.Importance = widget.HighImportance

	sourceSection := container.NewBorder(
		container.NewVBox(widget.NewLabelWithStyle("Éléments à sauvegarder", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
			container.NewHBox(addFolder, addFile, removeSource)),
		nil, nil, nil, sourcesList,
	)

	// Snapshot history is read-only. Restores are never allowed into nonempty folders.
	snapshotList := widget.NewList(
		func() int { return len(snapshots) },
		func() fyne.CanvasObject { return widget.NewLabel("") },
		func(id widget.ListItemID, obj fyne.CanvasObject) {
			if id >= 0 && id < len(snapshots) {
				sn := snapshots[id]
				label := sn.Time.Local().Format("02/01/2006 15:04") + "  •  " + sn.ShortID + "  •  " + sn.Hostname
				obj.(*widget.Label).SetText(label)
			}
		},
	)
	snapshotDetails := widget.NewLabel("Sélectionnez un instantané pour afficher ses chemins et restaurer ses fichiers.")
	snapshotDetails.Wrapping = fyne.TextWrapWord
	snapshotList.OnSelected = func(id widget.ListItemID) {
		selectedSnapshot = id
		if id >= 0 && id < len(snapshots) {
			sn := snapshots[id]
			snapshotDetails.SetText(fmt.Sprintf("Instantané : %s\nOrdinateur : %s\nSources :\n%s", sn.ID, sn.Hostname, strings.Join(sn.Paths, "\n")))
		}
	}
	refreshSnapshots := widget.NewButton("Actualiser", func() {
		c := currentClient()
		run("Chargement des instantanés", func(ctx context.Context) error {
			list, err := c.Snapshots(ctx)
			if err != nil {
				return err
			}
			fyne.Do(func() {
				snapshots = list
				selectedSnapshot = -1
				snapshotList.UnselectAll()
				snapshotList.Refresh()
				snapshotDetails.SetText(fmt.Sprintf("%d instantané(s). Sélectionnez-en un pour le restaurer.", len(list)))
			})
			return nil
		})
	})
	restoreButton := widget.NewButton("Restaurer l'instantané sélectionné…", func() {
		if selectedSnapshot < 0 || selectedSnapshot >= len(snapshots) {
			dialog.ShowInformation("Aucun instantané sélectionné", "Sélectionnez un instantané dans la liste.", w)
			return
		}
		id := snapshots[selectedSnapshot].ID
		c := currentClient()
		dialog.NewFolderOpen(func(uri fyne.ListableURI, err error) {
			if err != nil {
				dialog.ShowError(err, w)
				return
			}
			if uri == nil {
				return
			}
			target := uri.Path()
			dialog.ShowConfirm("Confirmer la restauration",
				"L'instantané "+id[:min(12, len(id))]+" sera restauré dans le dossier :\n"+target+
					"\n\nLe dossier doit être vide. Les fichiers existants ne sont pas écrasés.",
				func(ok bool) {
					if !ok {
						return
					}
					run("Restauration", func(ctx context.Context) error { return c.Restore(ctx, id, target, updateEvent) })
				}, w)
		}, w).Show()
	})
	restoreButton.Importance = widget.HighImportance

	checkButton := widget.NewButton("Vérifier l'intégrité du dépôt", func() {
		c := currentClient()
		run("Vérification d'intégrité", func(ctx context.Context) error { return c.Check(ctx, updateEvent) })
	})

	actionButtons = []*widget.Button{browseRepo, initButton, versionButton, addFolder, addFile, removeSource, backupButton, refreshSnapshots, restoreButton, checkButton}

	credentials := widget.NewForm(
		widget.NewFormItem("restic", binary),
		widget.NewFormItem("Dépôt", container.NewBorder(nil, nil, nil, browseRepo, repo)),
		widget.NewFormItem("Mot de passe", password),
	)
	sharedTop := container.NewVBox(
		widget.NewLabelWithStyle("Vaultic", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
		widget.NewLabel("Sauvegardes chiffrées au format restic"),
		credentials,
		container.NewHBox(versionButton, initButton),
		widget.NewSeparator(),
	)

	backupTab := container.NewBorder(
		widget.NewLabel("Sélectionnez les dossiers ou fichiers à sauvegarder."),
		container.NewVBox(widget.NewSeparator(), backupButton), nil, nil,
		sourceSection,
	)
	snapshotsTab := container.NewBorder(
		container.NewVBox(widget.NewLabel("Historique des instantanés"), refreshSnapshots),
		container.NewVBox(widget.NewSeparator(), snapshotDetails, restoreButton), nil, nil,
		snapshotList,
	)
	verifyTab := container.NewVBox(
		widget.NewLabelWithStyle("Vérification d'intégrité", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
		widget.NewLabel("restic check vérifie la structure du dépôt. Cette opération ne supprime rien."),
		checkButton,
		layout.NewSpacer(),
	)
	tabs := container.NewAppTabs(
		container.NewTabItem("Sauvegarde", backupTab),
		container.NewTabItem("Instantanés et restauration", snapshotsTab),
		container.NewTabItem("Vérification", verifyTab),
	)
	tabs.SetTabLocation(container.TabLocationTop)

	activity := container.NewVBox(
		widget.NewSeparator(),
		container.NewHBox(widget.NewLabelWithStyle("État", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}), layout.NewSpacer(), cancelButton),
		status, progress,
		widget.NewLabel("Journal des opérations"),
	)
	bottom := container.NewBorder(activity, nil, nil, nil, container.NewVScroll(logView))
	split := container.NewVSplit(tabs, bottom)
	split.Offset = 0.65
	main := container.NewBorder(sharedTop, nil, nil, nil, split)
	w.SetContent(main)
	w.SetCloseIntercept(func() {
		if busy.Load() {
			dialog.ShowInformation("Opération en cours", "Arrêtez l'opération en cours avant de fermer l'application.", w)
			return
		}
		save()
		w.SetCloseIntercept(nil)
		w.Close()
	})
	appendLog("Le mot de passe n'est pas enregistré. Indiquez le chemin de restic ou ajoutez-le au PATH.")
	w.ShowAndRun()
}

func appendUnique(existing []string, path string) []string {
	path = filepath.Clean(path)
	for _, old := range existing {
		if old == path {
			return existing
		}
	}
	return append(existing, path)
}
