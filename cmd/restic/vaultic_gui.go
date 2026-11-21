package main

// Vaultic is built in the official restic source tree. All backup operations
// call restic's Go functions directly; no restic executable or shell is launched.
import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"github.com/restic/restic/internal/global"
	"github.com/restic/restic/internal/ui"
)

type vaulticSnapshot struct {
	ID       string    `json:"id"`
	ShortID  string    `json:"short_id"`
	Hostname string    `json:"hostname"`
	Time     time.Time `json:"time"`
	Paths    []string  `json:"paths"`
}

type vaulticTerminal struct {
	line   func(string)
	output io.Writer
}

var _ ui.Terminal = (*vaulticTerminal)(nil)

func (t *vaulticTerminal) Print(s string) {
	if t.line != nil {
		t.line(strings.TrimSpace(s))
	}
}
func (t *vaulticTerminal) Error(s string) {
	if t.line != nil {
		t.line(strings.TrimSpace(s))
	}
}
func (t *vaulticTerminal) SetStatus(lines []string) {}
func (t *vaulticTerminal) CanUpdateStatus() bool    { return false }
func (t *vaulticTerminal) InputRaw() io.ReadCloser  { return io.NopCloser(strings.NewReader("")) }
func (t *vaulticTerminal) InputIsTerminal() bool    { return false }
func (t *vaulticTerminal) ReadPassword(ctx context.Context, prompt string) (string, error) {
	return "", errors.New(vaulticPasswordRequiredMessage)
}
func (t *vaulticTerminal) OutputWriter() io.Writer {
	if t.output != nil {
		return t.output
	}
	return io.Discard
}
func (t *vaulticTerminal) OutputRaw() io.Writer   { return t.OutputWriter() }
func (t *vaulticTerminal) OutputIsTerminal() bool { return false }

func vaulticLocalRepository(repo string) bool {
	if strings.HasPrefix(repo, "local:") {
		return true
	}
	if strings.Contains(repo, "://") {
		return false
	}
	if len(repo) >= 2 && repo[1] == ':' {
		return (repo[0] >= 'A' && repo[0] <= 'Z') || (repo[0] >= 'a' && repo[0] <= 'z')
	}
	return !strings.Contains(repo, ":")
}

// Resolve relative local repositories against the application executable.
// This keeps a portable SSD installation stable when its drive letter changes.
func vaulticResolvedRepository(repo string) string {
	repo = strings.TrimSpace(repo)
	repo = strings.TrimPrefix(repo, "local:")
	if repo == "" || !vaulticLocalRepository(repo) || filepath.IsAbs(repo) {
		return repo
	}
	exe, err := os.Executable()
	if err != nil {
		return repo
	}
	return filepath.Join(filepath.Dir(exe), repo)
}

func vaulticContained(parent, child string) bool {
	rel, err := filepath.Rel(parent, child)
	return err == nil && (rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))))
}

func vaulticSourcesForBackup(repo string, sources []string) ([]string, error) {
	if len(sources) == 0 {
		return nil, errors.New("Sélectionnez au moins un dossier ou fichier")
	}
	cleaned := make([]string, 0, len(sources))
	localRepo := vaulticLocalRepository(repo)
	repoAbs := ""
	if localRepo {
		var err error
		repoAbs, err = filepath.Abs(vaulticResolvedRepository(repo))
		if err != nil {
			return nil, err
		}
		if real, e := filepath.EvalSymlinks(repoAbs); e == nil {
			repoAbs = real
		}
	}
	for _, source := range sources {
		abs, err := filepath.Abs(source)
		if err != nil {
			return nil, err
		}
		info, err := os.Lstat(abs)
		if err != nil {
			return nil, fmt.Errorf("Source inaccessible : %s\nVérifiez le chemin, les permissions et le branchement du disque.\n%w", abs, err)
		}
		if !info.IsDir() && !info.Mode().IsRegular() {
			return nil, fmt.Errorf("Choisissez directement un fichier ou un dossier ordinaire : %s", abs)
		}
		if real, e := filepath.EvalSymlinks(abs); e == nil {
			abs = real
		}
		if _, err := os.Stat(abs); err != nil {
			return nil, fmt.Errorf("Source inaccessible : %s\nVérifiez le chemin, les permissions et le branchement du disque.\n%w", abs, err)
		}
		if localRepo && vaulticContained(abs, repoAbs) {
			return nil, fmt.Errorf("Le dépôt %q ne doit pas se trouver dans une source sauvegardée (%q)", repoAbs, abs)
		}
		cleaned = append(cleaned, abs)
	}
	return cleaned, nil
}

var vaulticSnapshotID = regexp.MustCompile(`^[a-fA-F0-9]{8,64}$`)

func vaulticRestoreTarget(target string) (string, error) {
	if target == "" {
		return "", errors.New("Choisissez un dossier de restauration")
	}
	abs, err := filepath.Abs(target)
	if err != nil {
		return "", err
	}
	info, err := os.Lstat(abs)
	if err != nil {
		return "", fmt.Errorf("Dossier de restauration inaccessible : %w", err)
	}
	if !info.IsDir() || info.Mode()&(os.ModeSymlink|os.ModeIrregular) != 0 {
		return "", errors.New("Choisissez un dossier de restauration qui n’est pas un lien")
	}
	canonical, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return "", err
	}
	entries, err := os.ReadDir(canonical)
	if err != nil {
		return "", fmt.Errorf("Choisissez un dossier de restauration existant et vide : %w", err)
	}
	if len(entries) > 0 {
		return "", errors.New("Pour éviter tout écrasement, choisissez un dossier de restauration vide")
	}
	return canonical, nil
}

func vaulticMain() {
	a := app.NewWithID("fr.cybou.vaultic")
	a.SetIcon(vaulticAppIcon())
	a.Settings().SetTheme(&vaulticVisualTheme{})
	w := a.NewWindow("Vaultic — sauvegardes chiffrées")
	w.SetMaster()

	saved, settingsErr := vaulticLoadSettings()
	initialProfile := saved.migrateBackupProfiles()
	// Migrate existing schedules once: their old implicit destination was
	// the active repository. Subsequent changes must not silently retarget it.
	for i := range saved.Profiles {
		if schedule := saved.Profiles[i].Schedule; schedule != nil && schedule.Repository == "" {
			schedule.Repository = saved.Repository
		}
	}
	for _, profile := range saved.Profiles {
		for i, source := range profile.Sources {
			if !filepath.IsAbs(source) {
				profile.Sources[i] = vaulticResolvedRepository(source)
			}
		}
	}
	var snapshots *vaulticSnapshotsController
	var profiles *vaulticProfilesController
	recentRunValue := widget.NewLabelWithStyle("Aucune", fyne.TextAlignLeading, fyne.TextStyle{Bold: true})
	recentRunValue.Truncation = fyne.TextTruncateEllipsis
	storageVolume := vaulticHeading("—", 24)
	storageBlobs := vaulticHeading("—", 24)
	storageSnapshots := vaulticHeading("—", 24)
	storageUpdated := vaulticWrappedLabel("Aucune analyse effectuée pour ce dépôt.")

	var repos *vaulticRepositoriesController

	var runner *vaulticEngineRunner
	panel := newVaulticStatusPanel(func() { runner.RequestCancel() })
	status := panel.Status
	logView := newVaulticLogView()
	appendLog := logView.Append
	persist := func(depots []vaulticDepot, active string) error {
		if settingsErr != nil {
			return settingsErr
		}
		stored := make([]vaulticBackupProfile, len(saved.Profiles))
		for i, profile := range saved.Profiles {
			storedSources := make([]string, len(profile.Sources))
			for j, source := range profile.Sources {
				storedSources[j] = vaulticPortablePath(source)
			}
			stored[i] = vaulticBackupProfile{
				Name: profile.Name, Sources: storedSources, Excludes: append([]string(nil), profile.Excludes...),
				Schedule: vaulticCloneSchedule(profile.Schedule),
			}
		}
		return vaulticSaveSettings(vaulticSettings{
			Repository: active, Depots: depots, Sources: stored[profiles.Index()].Sources,
			Profiles: stored, ActiveProfile: stored[profiles.Index()].Name,
			SchedulerPaused: saved.SchedulerPaused, Notifications: saved.Notifications,
		})
	}
	save := func() {
		if err := persist(saved.Depots, repos.Active()); err != nil {
			appendLog("Erreur d'enregistrement des préférences : " + err.Error())
		}
	}

	var busy atomic.Bool
	historyStore := newVaulticHistoryStore(vaulticHistoryPath())
	if err := historyStore.Load(); err != nil {
		appendLog(err.Error())
	}
	var coord *vaulticCoordinator
	var automationUI *vaulticAutomationUI
	var notifyPrefs atomic.Pointer[vaulticNotifyPrefs]
	{
		prefs := vaulticDefaultNotifyPrefs()
		if saved.Notifications != nil {
			prefs = *saved.Notifications
		}
		notifyPrefs.Store(&prefs)
	}
	stopScheduler := make(chan struct{})
	var stopSchedulerOnce sync.Once
	closePrompt := false
	var stopFocusFollow func()
	closeWindow := func() {
		stopSchedulerOnce.Do(func() { close(stopScheduler) })
		if coord != nil {
			coord.Shutdown(15 * time.Second)
		}
		if stopFocusFollow != nil {
			stopFocusFollow()
		}
		save()
		if repos != nil {
			repos.Wipe()
		}
		w.SetCloseIntercept(nil)
		w.Close()
	}
	var actionButtons []*widget.Button
	var updateActions func()
	var windowContent *fyne.Container
	var lastBackupReport string
	showBackupReport := func() {
		content := container.NewVScroll(vaulticWrappedLabel(lastBackupReport))
		content.SetMinSize(fyne.NewSize(340, 170))
		reportDialog := dialog.NewCustom("Rapport de sauvegarde", "Fermer", content, w)
		reportDialog.Resize(fyne.NewSize(490, 300))
		reportDialog.Show()
	}
	setBusy := func(on bool) {
		if on {
			panel.Busy()
		} else {
			panel.Idle()
		}
		if repos != nil {
			repos.SetBusy(on)
		}
		for _, b := range actionButtons {
			if on {
				b.Disable()
			} else {
				b.Enable()
			}
		}
		updateActions()
		windowContent.Refresh()
	}

	runner = newVaulticEngineRunner(vaulticRunnerHooks{
		Window:        w,
		Busy:          &busy,
		Panel:         panel,
		Log:           appendLog,
		SetBusy:       setBusy,
		CloseNow:      closeWindow,
		CloudAuth:     func(repo string) vaulticCloudAuth { return repos.Sessions.get(repo) },
		Completed:     recentRunValue.SetText,
		RefreshLayout: func() { windowContent.Refresh() },
	})
	run := func(title string, fn func(context.Context, global.Options) error) {
		runner.Run(title, repos.Active(), repos.Password.Text, vaulticRunOptions{}, fn, nil)
	}

	repos = newVaulticRepositoriesController(&saved, vaulticRepositoriesHooks{
		Window:        w,
		Persist:       persist,
		Busy:          busy.Load,
		Log:           appendLog,
		Status:        status.SetText,
		UpdateActions: func() { updateActions() },
		Run:           runner.Run,
		CancelRun:     func() { runner.RequestCancel() },
		Activated: func() {
			snapshots.Clear()
			storageVolume.Text = "—"
			storageVolume.Refresh()
			storageBlobs.Text = "—"
			storageBlobs.Refresh()
			storageSnapshots.Text = "—"
			storageSnapshots.Refresh()
			storageUpdated.SetText("Aucune analyse effectuée pour ce dépôt.")
		},
	})

	profiles = newVaulticProfilesController(&saved, initialProfile, vaulticProfilesHooks{
		Window:        w,
		Persist:       func() error { return persist(saved.Depots, repos.Active()) },
		Busy:          busy.Load,
		Log:           appendLog,
		UpdateActions: func() { updateActions() },
		Changed: func() {
			if automationUI != nil {
				automationUI.Load()
			}
		},
		Renamed: func(oldName, newName string) {
			if err := historyStore.RenameProfile(oldName, newName); err != nil {
				appendLog("Historique : " + err.Error())
			}
			if automationUI != nil {
				automationUI.Refresh()
			}
		},
		RunWithDepot: func(title string, fn func(context.Context, global.Options) error, done func() error) {
			runner.Run(title, repos.Active(), repos.Password.Text, vaulticRunOptions{}, fn, done)
		},
	})
	// Every backup, manual or scheduled, goes through the coordinator: one
	// engine operation at a time, one history, one set of notifications.
	launchBackup := func(trigger, profileName string, slot time.Time, catchUp bool) bool {
		var profile vaulticBackupProfile
		found := false
		for _, candidate := range saved.Profiles {
			if candidate.Name == profileName {
				profile, found = candidate, true
			}
		}
		manual := trigger == vaulticTriggerManual
		if !found || len(profile.Sources) == 0 {
			return false
		}
		if repos.Active() == "" || repos.Password.Text == "" {
			if manual {
				dialog.ShowInformation("Informations manquantes", "Indiquez un dépôt et son mot de passe.", w)
			}
			return false
		}
		if !coord.TryAcquire() {
			if manual {
				dialog.ShowInformation("Opération en cours", "Une seule opération est autorisée à la fois.", w)
			}
			return false
		}
		repo, password := repos.Active(), repos.Password.Text
		cloudAuth := repos.Sessions.get(repo)
		redact := vaulticRedactCloudSecrets(vaulticRedactOperation(password), cloudAuth)
		title := "Sauvegarde"
		if !manual {
			title = "Sauvegarde planifiée"
		}
		runner.SetCancel(coord.Cancel)
		panel.Reserve()
		setBusy(true)
		panel.Progress.SetValue(0)
		status.SetText(title + "…")
		appendLog("Début : " + title + " — profil « " + profileName + " »")
		gopts := global.Options{
			Repo: vaulticResolvedRepository(repo), Password: password, Backends: vaulticBackendsForSession(cloudAuth),
			JSON: true, Verbosity: 1,
		}
		exec := vaulticBackupExecutor(gopts, append([]string(nil), profile.Sources...), append([]string(nil), profile.Excludes...),
			func(report *vaulticBackupReport, line string) {
				runner.Handle(context.Background(), title, line, report, redact)
			})
		job := vaulticJob{Profile: profileName, Repository: repo, Trigger: trigger, Slot: slot, CatchUp: catchUp, Redact: redact}
		go func() {
			record := coord.RunAcquired(job, exec)
			fyne.Do(func() {
				runner.SetCancel(nil)
				lastBackupReport = coord.LastReport()
				status.SetText(title + " · " + vaulticStatusLabel(record.Status))
				appendLog(title + " · " + lastBackupReport)
				if record.Status == vaulticStatusSuccess {
					recentRunValue.SetText(title)
				}
				setBusy(false)
				if runner.Closing() {
					closeWindow()
					return
				}
				if manual {
					showBackupReport()
				}
				if automationUI != nil {
					automationUI.Refresh()
				}
			})
		}()
		return true
	}
	backupButton := widget.NewButton("Sauvegarder maintenant", func() {
		launchBackup(vaulticTriggerManual, profiles.Current().Name, time.Time{}, false)
	})
	// UI-owned settings are changed on the Fyne thread. A worker waits until
	// persistence finishes, so the slot is durable before releasing the gate.
	saveScheduleSlot := func(profile string, slot time.Time) error {
		for i := range saved.Profiles {
			if saved.Profiles[i].Name == profile && saved.Profiles[i].Schedule != nil {
				// Advance the in-memory anchor even on a disk error: the same
				// run must not be launched every 15 seconds in this session.
				// Persistence failures are surfaced by the coordinator.
				saved.Profiles[i].Schedule.LastSlot = slot.Format(time.RFC3339)
				if err := persist(saved.Depots, repos.Active()); err != nil {
					return err
				}
				return nil
			}
		}
		return fmt.Errorf("Profil planifié introuvable : %s", profile)
	}
	coord = newVaulticCoordinator(&busy, vaulticCoordinatorEnv{
		Now:      time.Now,
		Location: func() *time.Location { return time.Local },
		Profiles: func() []vaulticBackupProfile {
			out := make([]vaulticBackupProfile, len(saved.Profiles))
			for i, p := range saved.Profiles {
				out[i] = p
				out[i].Schedule = vaulticCloneSchedule(p.Schedule)
			}
			return out
		},
		Repository:     func() string { return repos.Active() },
		HasCredentials: func() bool { return repos.Password.Text != "" },
		Paused:         func() bool { return saved.SchedulerPaused },
		SaveSlot:       saveScheduleSlot,
		SaveSlotWorker: func(profile string, slot time.Time) error {
			var saveErr error
			fyne.DoAndWait(func() { saveErr = saveScheduleSlot(profile, slot) })
			return saveErr
		},
		Start: func(job vaulticJob) bool { return launchBackup(job.Trigger, job.Profile, job.Slot, job.CatchUp) },
	}, historyStore, newVaulticNotifier(
		func() vaulticNotifyPrefs { return *notifyPrefs.Load() },
		func(title, body string) {
			fyne.Do(func() { a.SendNotification(&fyne.Notification{Title: title, Content: body}) })
		},
		time.Now,
	))
	coord.OnChange = func() {
		fyne.Do(func() {
			if automationUI != nil {
				automationUI.Refresh()
			}
			updateActions()
		})
	}
	coord.OnPersistenceError = func(err error) {
		fyne.Do(func() {
			appendLog("Erreur de sauvegarde des métadonnées : " + err.Error())
			status.SetText("Attention : historique ou planification non enregistré(e).")
		})
	}
	go func() {
		ticker := time.NewTicker(15 * time.Second)
		defer ticker.Stop()
		first := time.After(8 * time.Second)
		for {
			select {
			case <-stopScheduler:
				return
			case <-first:
			case <-ticker.C:
			}
			fyne.Do(func() {
				coord.Tick()
				if automationUI != nil {
					automationUI.Refresh()
				}
			})
		}
	}()
	backupReportButton := widget.NewButton("Dernier rapport…", showBackupReport)
	backupButton.Importance = widget.HighImportance

	snapshots = newVaulticSnapshotsController(vaulticSnapshotsHooks{
		Window:           w,
		App:              a,
		ActiveRepository: func() string { return repos.Active() },
		UpdateActions:    func() { updateActions() },
		Run:              func(title string, fn func(context.Context, global.Options) error) { run(title, fn) },
		RunWithDepot: func(title string, fn func(context.Context, global.Options) error, done func() error) {
			runner.Run(title, repos.Active(), repos.Password.Text, vaulticRunOptions{}, fn, done)
		},
	})
	checkButton := widget.NewButton("Vérifier l'intégrité", func() {
		run("Vérification", func(ctx context.Context, g global.Options) error {
			_, err := runCheck(ctx, CheckOptions{}, g, nil, g.Term)
			return err
		})
	})
	storageButton := widget.NewButtonWithIcon("Analyser le stockage", theme.SearchIcon(), func() {
		run("Analyse du stockage", func(ctx context.Context, g global.Options) error {
			metrics, err := vaulticCollectStorageStatistics(ctx, g)
			if err != nil {
				return err
			}
			fyne.Do(func() {
				if ctx.Err() != nil {
					return
				}
				storageVolume.Text = vaulticBytes(metrics.TotalSize)
				storageVolume.Refresh()
				storageBlobs.Text = fmt.Sprintf("%d", metrics.TotalBlobCount)
				storageBlobs.Refresh()
				storageSnapshots.Text = fmt.Sprintf("%d", metrics.SnapshotsCount)
				storageSnapshots.Refresh()
				storageUpdated.SetText("Dernière analyse : " + time.Now().Format("02/01/2006 à 15:04"))
			})
			return nil
		})
	})

	actionButtons = []*widget.Button{repos.Verify, repos.EditAuth, repos.ClearAuth, profiles.AddFolder, profiles.AddFile, snapshots.Refresh, checkButton, storageButton, profiles.New, profiles.Rename, profiles.Duplicate, profiles.Delete, profiles.Exclusions, profiles.Preview}
	updateActions = func() {
		if len(profiles.Sources()) == 0 {
			profiles.Empty.Show()
		} else {
			profiles.Empty.Hide()
		}
		snapshots.UpdateState(busy.Load())
		vaulticSetButtonEnabled(backupButton, !busy.Load() && len(profiles.Sources()) > 0)
		vaulticSetButtonEnabled(backupReportButton, !busy.Load() && lastBackupReport != "")
		vaulticSetButtonEnabled(profiles.Exclusions, !busy.Load())
		vaulticSetButtonEnabled(profiles.Preview, !busy.Load() && len(profiles.Sources()) > 0 && len(profiles.Current().Excludes) > 0)
		vaulticSetButtonEnabled(profiles.Duplicate, !busy.Load())
		vaulticSetButtonEnabled(profiles.Delete, !busy.Load() && len(saved.Profiles) > 1)
		vaulticSetButtonEnabled(storageButton, !busy.Load() && repos.Active() != "")
		vaulticSetButtonEnabled(profiles.RemoveSource, !busy.Load() && profiles.Selected() >= 0)
		repos.UpdateState(busy.Load())
		if busy.Load() {
			profiles.Select.Disable()
		} else {
			profiles.Select.Enable()
		}
	}
	// Every page has one purpose. Operations still invoke the original Go
	// restic engine; this shell only changes presentation and navigation.
	startBackup := widget.NewButtonWithIcon("Préparer une sauvegarde", theme.MediaPlayIcon(), nil)
	startBackup.Importance = widget.HighImportance
	setupButton := widget.NewButtonWithIcon("Configurer le dépôt", theme.SettingsIcon(), nil)
	viewSnapshots := widget.NewButtonWithIcon("Voir les instantanés", theme.HistoryIcon(), nil)
	viewStorage := widget.NewButtonWithIcon("Espace de stockage", theme.StorageIcon(), nil)
	overviewPage := vaulticOverviewPage(vaulticOverviewParts{
		SourceCount: profiles.CountValue, SnapshotCount: snapshots.CountValue, Repository: repos.Value,
		RecentRun: recentRunValue, RecentSnapshot: snapshots.RecentValue,
		StartBackup: startBackup, Setup: setupButton, ViewSnapshots: viewSnapshots, ViewStorage: viewStorage,
	})
	repositoriesPage := vaulticRepositoriesPage(vaulticRepositoriesParts{
		SavedList: repos.ListBox, ActiveRepository: repos.Value, Password: repos.Password,
		Verify: repos.Verify, EditAuth: repos.EditAuth, ClearAuth: repos.ClearAuth,
		Setup: repos.Setup.Content,
	})
	automationUI = newVaulticAutomationUI(vaulticAutomationHooks{
		Profile: func() vaulticBackupProfile { return profiles.Current() },
		SetSchedule: func(schedule vaulticSchedule) error {
			next, err := vaulticApplySchedule(saved.Profiles, profiles.Index(), schedule, repos.Active(), time.Now())
			if err != nil {
				return err
			}
			if !profiles.Apply(next, profiles.Index()) {
				return errors.New("Enregistrement impossible : consultez le message affiché.")
			}
			appendLog("Planification du profil « " + next[profiles.Index()].Name + " » : " + schedule.Describe())
			return nil
		},
		Paused: func() bool { return saved.SchedulerPaused },
		SetPaused: func(paused bool) {
			saved.SchedulerPaused = paused
			if !paused {
				coord.Resume()
			} // nothing missed during a pause is made up afterwards
			if err := persist(saved.Depots, repos.Active()); err != nil {
				appendLog("Erreur d'enregistrement des préférences : " + err.Error())
			}
			appendLog(map[bool]string{true: "Planification mise en pause.", false: "Planification reprise."}[paused])
		},
		Notify: func() vaulticNotifyPrefs { return *notifyPrefs.Load() },
		SetNotify: func(prefs vaulticNotifyPrefs) {
			notifyPrefs.Store(&prefs)
			saved.Notifications = &prefs
			if err := persist(saved.Depots, repos.Active()); err != nil {
				appendLog("Erreur d'enregistrement des préférences : " + err.Error())
			}
		},
		Coordinator: coord, History: historyStore, Now: time.Now,
		ActiveRepository: func() string { return repos.Active() },
		Credentials:      func() bool { return repos.Password.Text != "" },
		Location:         func() *time.Location { return time.Local },
	})
	backupPage, automationScroll := vaulticBackupPage(vaulticBackupParts{
		Profile: profiles.Select, NewProfile: profiles.New, RenameProfile: profiles.Rename,
		DuplicateProfile: profiles.Duplicate, DeleteProfile: profiles.Delete,
		Exclusions: profiles.Exclusions, PreviewExclusions: profiles.Preview, ExclusionSummary: profiles.ExclusionSummary,
		Sources: profiles.Section, BackupNow: backupButton, LastReport: backupReportButton,
	}, automationUI.Content)
	snapshotPage := vaulticSnapshotPage(vaulticSnapshotParts{
		Refresh: snapshots.Refresh, Details: snapshots.DetailsScroll,
		DetailsButton: snapshots.DetailsButton, Compare: snapshots.Compare,
		Explorer: snapshots.Explorer, Restore: snapshots.Restore,
		List: snapshots.list, Empty: snapshots.Empty,
	})
	storagePage := vaulticStoragePage(vaulticStorageParts{
		Analyze: storageButton, Updated: storageUpdated,
		Volume: storageVolume, Blobs: storageBlobs, Snapshots: storageSnapshots,
	})
	checkPage := vaulticIntegrityPage(checkButton)
	journalPage := vaulticActivityPage(logView.Entry, logView.Clear)
	settingsPage := vaulticSettingsPage()

	pages := []fyne.CanvasObject{overviewPage, backupPage, snapshotPage, storagePage, checkPage, journalPage, repositoriesPage, settingsPage}
	stopFocusFollow = vaulticKeepFocusVisible(w, []*container.Scroll{overviewPage, storagePage, checkPage, repositoriesPage, settingsPage, automationScroll})
	nav := vaulticNewNavigation(pages)
	navigate := nav.Navigate
	setupButton.OnTapped = func() { navigate(vaulticPageRepositories) }
	startBackup.OnTapped = func() { navigate(vaulticPageBackup) }
	viewSnapshots.OnTapped = func() { navigate(vaulticPageSnapshots) }
	viewStorage.OnTapped = func() { navigate(vaulticPageStorage) }
	statusBar := panel.Bar()
	if repos.Active() == "" {
		navigate(vaulticPageRepositories)
	} else {
		navigate(vaulticPageOverview)
	}
	windowContent = container.NewBorder(nil, container.NewPadded(statusBar), nav.Sidebar, nil, nav.Pages)
	repos.Refresh()
	profiles.Refresh()
	w.SetContent(windowContent)
	w.SetCloseIntercept(func() {
		if runner.Closing() || closePrompt {
			return
		}
		if busy.Load() {
			closePrompt = true
			dialog.ShowConfirm("Quitter Vaultic", "Annuler l’opération et fermer Vaultic après son arrêt ?", func(ok bool) {
				closePrompt = false
				if !ok {
					return
				}
				if !runner.BeginClose() {
					closeWindow()
				}
			}, w)
			return
		}
		closeWindow()
	})
	vaulticShowWindow(w, fyne.NewSize(1180, 760), fyne.NewSize(640, 365))
	if settingsErr != nil {
		dialog.ShowError(fmt.Errorf("Enregistrement des préférences désactivé pour cette session : %w", settingsErr), w)
	}
	a.Run()
}

func vaulticVerifyDepot(ctx context.Context, g global.Options) error {
	g.NoCache = true
	printer := ui.NewProgressPrinter(g.JSON, g.Verbosity, g.Term)
	repo, err := global.OpenRepository(ctx, g, printer)
	if err != nil {
		return err
	}
	return repo.Close()
}

func vaulticAppendUnique(paths []string, path string) []string {
	path = filepath.Clean(path)
	for _, old := range paths {
		if old == path {
			return paths
		}
	}
	return append(paths, path)
}
