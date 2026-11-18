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
	binary.SetPlaceHolder("restic или полный путь к restic.exe")
	binary.SetText(saved.ResticBinary)

	repo := widget.NewEntry()
	repo.SetPlaceHolder("Например: E:\\Backup\\restic или /media/ssd/backup")
	repo.SetText(saved.Repository)

	password := widget.NewPasswordEntry()
	password.SetPlaceHolder("Пароль restic — не сохраняется")

	status := widget.NewLabel("Готов к работе")
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
			appendLog("Не удалось сохранить настройки: " + err.Error())
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
	cancelButton := widget.NewButton("Остановить", func() {
		if cancelOp != nil {
			cancelOp()
			status.SetText("Останавливаем операцию…")
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
					status.SetText(fmt.Sprintf("Резервное копирование: %.1f%% — %d / %d файлов", 100*p.PercentDone, p.FilesDone, p.TotalFiles))
				case "summary":
					progress.SetValue(1)
					appendLog("Снимок создан: " + p.SnapshotID)
					if p.ErrorCount > 0 {
						appendLog(fmt.Sprintf("Внимание: %d ошибок при backup", p.ErrorCount))
					}
				case "error":
					appendLog("restic сообщил об ошибке при обработке файла")
				}
			}
			if e.Line != "" {
				appendLog(e.Line)
			}
		})
	}

	run := func(name string, fn func(context.Context) error) {
		if !busy.CompareAndSwap(false, true) {
			dialog.ShowInformation("Операция выполняется", "Дождитесь завершения текущей операции или остановите её.", w)
			return
		}
		save()
		ctx, cancel := context.WithCancel(context.Background())
		cancelOp = cancel
		setBusy(true)
		progress.SetValue(0)
		status.SetText(name + "…")
		appendLog("Начало: " + name)
		go func() {
			err := fn(ctx)
			fyne.Do(func() {
				cancel()
				cancelOp = nil
				busy.Store(false)
				setBusy(false)
				if err != nil {
					status.SetText("Ошибка: " + name)
					appendLog("Ошибка: " + err.Error())
					dialog.ShowError(err, w)
					return
				}
				status.SetText(name + " — выполнено")
				appendLog("Завершено: " + name)
			})
		}()
	}

	browseRepo := widget.NewButton("Обзор…", func() {
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
	initButton := widget.NewButton("Создать хранилище", func() {
		c := currentClient()
		run("Создание хранилища", func(ctx context.Context) error { return c.Init(ctx, updateEvent) })
	})
	versionButton := widget.NewButton("Проверить restic", func() {
		c := currentClient()
		run("Проверка restic", func(ctx context.Context) error {
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

	addFolder := widget.NewButton("+ Папка", func() {
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
	addFile := widget.NewButton("+ Файл", func() {
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
	removeSource := widget.NewButton("Удалить из списка", func() {
		if selectedSource < 0 || selectedSource >= len(sources) {
			return
		}
		sources = append(sources[:selectedSource], sources[selectedSource+1:]...)
		selectedSource = -1
		sourcesList.UnselectAll()
		sourcesList.Refresh()
		save()
	})
	backupButton := widget.NewButton("Начать резервное копирование", func() {
		c := currentClient()
		selected := append([]string(nil), sources...)
		run("Резервное копирование", func(ctx context.Context) error {
			return c.Backup(ctx, selected, updateEvent)
		})
	})
	backupButton.Importance = widget.HighImportance

	sourceSection := container.NewBorder(
		container.NewVBox(widget.NewLabelWithStyle("Что сохранить", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
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
				label := sn.Time.Local().Format("02.01.2006 15:04") + "  •  " + sn.ShortID + "  •  " + sn.Hostname
				obj.(*widget.Label).SetText(label)
			}
		},
	)
	snapshotDetails := widget.NewLabel("Выберите снимок, чтобы посмотреть пути и восстановить файлы.")
	snapshotDetails.Wrapping = fyne.TextWrapWord
	snapshotList.OnSelected = func(id widget.ListItemID) {
		selectedSnapshot = id
		if id >= 0 && id < len(snapshots) {
			sn := snapshots[id]
			snapshotDetails.SetText(fmt.Sprintf("Снимок: %s\nКомпьютер: %s\nИсточники:\n%s", sn.ID, sn.Hostname, strings.Join(sn.Paths, "\n")))
		}
	}
	refreshSnapshots := widget.NewButton("Обновить список", func() {
		c := currentClient()
		run("Загрузка снимков", func(ctx context.Context) error {
			list, err := c.Snapshots(ctx)
			if err != nil {
				return err
			}
			fyne.Do(func() {
				snapshots = list
				selectedSnapshot = -1
				snapshotList.UnselectAll()
				snapshotList.Refresh()
				snapshotDetails.SetText(fmt.Sprintf("Снимков: %d. Выберите снимок для восстановления.", len(list)))
			})
			return nil
		})
	})
	restoreButton := widget.NewButton("Восстановить выбранный снимок…", func() {
		if selectedSnapshot < 0 || selectedSnapshot >= len(snapshots) {
			dialog.ShowInformation("Снимок не выбран", "Выберите снимок из списка.", w)
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
			dialog.ShowConfirm("Подтвердить восстановление",
				"Снимок "+id[:min(12, len(id))]+" будет восстановлен в папку:\n"+target+
					"\n\nПапка должна быть пустой. Существующие файлы не перезаписываются.",
				func(ok bool) {
					if !ok {
						return
					}
					run("Восстановление", func(ctx context.Context) error { return c.Restore(ctx, id, target, updateEvent) })
				}, w)
		}, w).Show()
	})
	restoreButton.Importance = widget.HighImportance

	checkButton := widget.NewButton("Проверить целостность хранилища", func() {
		c := currentClient()
		run("Проверка целостности", func(ctx context.Context) error { return c.Check(ctx, updateEvent) })
	})

	actionButtons = []*widget.Button{browseRepo, initButton, versionButton, addFolder, addFile, removeSource, backupButton, refreshSnapshots, restoreButton, checkButton}

	credentials := widget.NewForm(
		widget.NewFormItem("restic", binary),
		widget.NewFormItem("Хранилище", container.NewBorder(nil, nil, nil, browseRepo, repo)),
		widget.NewFormItem("Пароль", password),
	)
	sharedTop := container.NewVBox(
		widget.NewLabelWithStyle("Vaultic", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
		widget.NewLabel("Зашифрованные резервные копии с открытым форматом restic"),
		credentials,
		container.NewHBox(versionButton, initButton),
		widget.NewSeparator(),
	)

	backupTab := container.NewBorder(
		widget.NewLabel("Выберите папки или файлы для резервного копирования."),
		container.NewVBox(widget.NewSeparator(), backupButton), nil, nil,
		sourceSection,
	)
	snapshotsTab := container.NewBorder(
		container.NewVBox(widget.NewLabel("История снимков"), refreshSnapshots),
		container.NewVBox(widget.NewSeparator(), snapshotDetails, restoreButton), nil, nil,
		snapshotList,
	)
	verifyTab := container.NewVBox(
		widget.NewLabelWithStyle("Проверка целостности", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
		widget.NewLabel("restic check проверяет структуру хранилища. Эта операция ничего не удаляет."),
		checkButton,
		layout.NewSpacer(),
	)
	tabs := container.NewAppTabs(
		container.NewTabItem("Копирование", backupTab),
		container.NewTabItem("Снимки и восстановление", snapshotsTab),
		container.NewTabItem("Проверка", verifyTab),
	)
	tabs.SetTabLocation(container.TabLocationTop)

	activity := container.NewVBox(
		widget.NewSeparator(),
		container.NewHBox(widget.NewLabelWithStyle("Состояние", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}), layout.NewSpacer(), cancelButton),
		status, progress,
		widget.NewLabel("Журнал операций"),
	)
	bottom := container.NewBorder(activity, nil, nil, nil, container.NewVScroll(logView))
	split := container.NewVSplit(tabs, bottom)
	split.Offset = 0.65
	main := container.NewBorder(sharedTop, nil, nil, nil, split)
	w.SetContent(main)
	w.SetCloseIntercept(func() {
		if busy.Load() {
			dialog.ShowInformation("Идёт операция", "Сначала остановите текущую операцию, чтобы корректно закрыть приложение.", w)
			return
		}
		save()
		w.SetCloseIntercept(nil)
		w.Close()
	})
	appendLog("Пароль не сохраняется. Укажите путь к restic или установите его в PATH.")
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
