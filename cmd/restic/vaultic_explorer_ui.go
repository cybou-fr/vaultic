package main

import (
	"context"
	"fmt"
	"path"
	"sort"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/widget"

	"github.com/restic/restic/internal/global"
)

func vaulticOpenExplorer(app fyne.App, snapshot vaulticSnapshot, run func(string, func(context.Context, global.Options) error)) {
	vaulticOpenExplorerAtPath(app, snapshot, run, "")
}

// Opens the existing snapshot browser with a specific path already queued for
// selective restore. The user must still choose and confirm the destination.
func vaulticOpenExplorerAtPath(app fyne.App, snapshot vaulticSnapshot, run func(string, func(context.Context, global.Options) error), initialPath string) {
	if initialPath != "" {
		initialPath = path.Clean(initialPath)
		if vaulticValidateSnapshotPath(initialPath) != nil || initialPath == "/" {
			initialPath = ""
		}
	}
	w := app.NewWindow(vaulticT("Vaultic — Instantané ") + snapshot.ShortID)
	closed := false
	w.SetOnClosed(func() { closed = true })
	directory := "/"
	searching := false
	var nodes []vaulticSnapshotNode
	selected := -1
	picked := make(map[string]vaulticSnapshotNode)

	location := widget.NewLabel(directory)
	details := widget.NewLabel("")
	info := widget.NewLabel(vaulticT("Lecture de l’instantané…"))
	count := widget.NewLabel("")
	empty := vaulticEmptyState(vaulticWrappedLabel(vaulticT("Aucun élément à afficher.")))
	query := widget.NewEntry()
	query.SetPlaceHolder(vaulticT("Nom ou chemin dans l’instantané"))
	back := widget.NewButton(vaulticT("Dossier parent"), nil)
	open := widget.NewButton(vaulticT("Ouvrir le dossier"), nil)
	toggle := widget.NewButton(vaulticT("Ajouter à la sélection"), nil)
	clear := widget.NewButton(vaulticT("Vider la sélection"), nil)
	restore := widget.NewButton(vaulticT("Restaurer la sélection…"), nil)
	restore.Importance = widget.HighImportance

	files := widget.NewList(func() int { return len(nodes) }, vaulticListLabel,
		func(id widget.ListItemID, obj fyne.CanvasObject) {
			if id < 0 || id >= len(nodes) {
				return
			}
			node := nodes[id]
			name := node.Name
			if searching {
				name = node.Path
			}
			kind := node.Type
			switch node.Type {
			case "dir":
				kind = vaulticT("Dossier")
			case "file":
				kind = vaulticT("Fichier")
			case "symlink":
				kind = vaulticT("Lien")
			}
			line := kind + " · " + name
			if node.Size != nil {
				line += " · " + vaulticBytes(*node.Size)
			}
			if _, ok := picked[node.Path]; ok {
				line = "✓  " + line
			}
			obj.(*widget.Label).SetText(line)
		},
	)

	update := func() {
		hasRow := selected >= 0 && selected < len(nodes)
		vaulticSetButtonEnabled(open, hasRow && nodes[selected].Type == "dir")
		vaulticSetButtonEnabled(toggle, hasRow && vaulticRestorableNode(nodes[selected]))
		toggle.SetText(vaulticT("Ajouter à la sélection"))
		details.SetText("")
		if hasRow {
			node := nodes[selected]
			details.SetText(node.Path)
			if !vaulticRestorableNode(node) {
				details.SetText(node.Path + vaulticT(" · Type non restaurable"))
			}
			if _, ok := picked[node.Path]; ok {
				toggle.SetText(vaulticT("Retirer de la sélection"))
			}
		}
		count.SetText(fmt.Sprintf(vaulticT("%d élément(s) sélectionné(s)"), len(picked)))
		vaulticSetButtonEnabled(restore, len(picked) > 0)
		vaulticSetButtonEnabled(clear, len(picked) > 0)
		if searching {
			back.SetText(vaulticT("Quitter la recherche"))
		} else {
			back.SetText(vaulticT("Dossier parent"))
		}
		vaulticSetButtonEnabled(back, searching || directory != "/")
	}
	files.OnSelected = func(id widget.ListItemID) { selected = id; update() }
	files.OnUnselected = func(id widget.ListItemID) {
		if selected == id {
			selected = -1
			update()
		}
	}
	display := func(found []vaulticSnapshotNode, locationText, status string, results bool) {
		if closed {
			return
		}
		selected = -1
		files.UnselectAll()
		nodes, searching = found, results
		location.SetText(locationText)
		info.SetText(status)
		if len(nodes) == 0 {
			empty.Show()
		} else {
			empty.Hide()
			files.ScrollToTop()
		}
		files.Refresh()
		update()
	}
	load := func(next string) {
		run(vaulticT("Exploration de l’instantané"), func(ctx context.Context, opts global.Options) error {
			found, err := vaulticListSnapshotDirectory(ctx, opts, snapshot.ID, next)
			if err != nil {
				return err
			}
			fyne.Do(func() {
				if closed {
					return
				}
				directory = next
				display(found, next, fmt.Sprintf(vaulticT("%d élément(s)"), len(found)), false)
				if initialPath != "" && path.Dir(initialPath) == next {
					desired := initialPath
					initialPath = ""
					located := false
					for index, node := range found {
						if node.Path != desired {
							continue
						}
						located = true
						files.Select(index)
						if vaulticRestorableNode(node) {
							picked[node.Path] = node
							files.Refresh()
							update()
							info.SetText(vaulticT("Élément retrouvé et ajouté à la sélection. Choisissez « Restaurer la sélection… » pour continuer."))
						} else {
							info.SetText(vaulticT("Élément retrouvé mais son type n'est pas restaurable en toute sécurité."))
						}
						break
					}
					if !located {
						info.SetText(vaulticT("Chemin introuvable dans cet instantané. Parcourez les dossiers pour le rechercher."))
					}
				}
			})
			return nil
		})
	}
	search := func() {
		term := strings.TrimSpace(query.Text)
		if term == "" {
			if searching {
				load(directory)
			}
			return
		}
		run(vaulticT("Recherche dans l’instantané"), func(ctx context.Context, opts global.Options) error {
			const limit = 1000
			found, truncated, err := vaulticSearchSnapshot(ctx, opts, snapshot.ID, term, limit)
			if err != nil {
				return err
			}
			status := fmt.Sprintf(vaulticT("%d résultat(s)"), len(found))
			if truncated {
				status += vaulticT(" affichés · Affinez la recherche")
			}
			fyne.Do(func() { display(found, vaulticT("Recherche dans tout l’instantané : ")+term, status, true) })
			return nil
		})
	}
	query.OnSubmitted = func(string) { search() }
	back.OnTapped = func() {
		if searching {
			query.SetText("")
			load(directory)
		} else if directory != "/" {
			load(path.Dir(directory))
		}
	}
	open.OnTapped = func() {
		if selected >= 0 && nodes[selected].Type == "dir" {
			load(nodes[selected].Path)
		}
	}
	toggle.OnTapped = func() {
		if selected < 0 || !vaulticRestorableNode(nodes[selected]) {
			return
		}
		node := nodes[selected]
		if _, ok := picked[node.Path]; ok {
			delete(picked, node.Path)
		} else {
			picked[node.Path] = node
		}
		files.Refresh()
		update()
	}
	clear.OnTapped = func() {
		for key := range picked {
			delete(picked, key)
		}
		files.Refresh()
		update()
	}
	restore.OnTapped = func() {
		items := make([]vaulticSnapshotNode, 0, len(picked))
		paths := make([]string, 0, len(picked))
		for _, node := range picked {
			items = append(items, node)
			paths = append(paths, node.Path)
		}
		sort.Strings(paths)
		dialog.NewFolderOpen(func(uri fyne.ListableURI, err error) {
			if err != nil {
				dialog.ShowError(err, w)
				return
			}
			if uri == nil {
				return
			}
			destination, err := vaulticRestoreTarget(uri.Path())
			if err != nil {
				dialog.ShowError(err, w)
				return
			}
			summary := fmt.Sprintf(vaulticT("%d élément(s) vers :\n%s\n\nLe dossier doit être vide. Les fichiers existants ne seront pas écrasés.\n\n%s"),
				len(items), destination, strings.Join(paths, "\n"))
			body := container.NewVScroll(vaulticWrappedLabel(summary))
			body.SetMinSize(fyne.NewSize(320, 155))
			dialog.NewCustomConfirm(vaulticT("Confirmer la restauration"), vaulticT("Restaurer"), vaulticT("Annuler"), body, func(ok bool) {
				if ok {
					run(vaulticT("Restauration de la sélection"), func(ctx context.Context, opts global.Options) error {
						fyne.Do(func() {
							if !closed {
								info.SetText(vaulticT("Restauration en cours…"))
							}
						})
						err := vaulticRestoreSelected(ctx, opts, snapshot.ID, items, destination)
						status := vaulticT("Restauration terminée")
						if err != nil {
							status = vaulticT("Restauration interrompue. Consultez le journal.")
						}
						fyne.Do(func() {
							if !closed {
								info.SetText(status)
							}
						})
						return err
					})
				}
			}, w).Show()
		}, w).Show()
	}
	w.SetContent(container.NewBorder(
		container.NewPadded(container.NewVBox(
			vaulticHeading(vaulticT("Contenu de l’instantané"), 26),
			container.NewBorder(nil, nil, nil, widget.NewButton(vaulticT("Rechercher"), search), query),
			container.NewHScroll(location), info,
			container.NewHBox(back, open),
		)),
		container.NewPadded(vaulticSurface(container.NewVBox(
			container.NewHScroll(details),
			container.NewHBox(toggle, clear),
			container.NewBorder(nil, nil, nil, restore, count),
		))),
		nil, nil, container.NewPadded(vaulticSurface(container.NewStack(files, empty))),
	))
	update()
	vaulticShowWindow(w, fyne.NewSize(900, 650), fyne.NewSize(520, 450))
	initialDirectory := "/"
	if initialPath != "" {
		initialDirectory = path.Dir(initialPath)
	}
	load(initialDirectory)
}
