package main

// Vaultic is built in the official restic source tree. All backup operations
// call restic's Go functions directly; no restic executable or shell is launched.
import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync/atomic"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/lang"
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
		return nil, errors.New(vaulticT("Sélectionnez au moins un dossier ou fichier"))
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
			return nil, fmt.Errorf(vaulticT("Source inaccessible : %s\nVérifiez le chemin, les permissions et le branchement du disque.\n%w"), abs, err)
		}
		if !info.IsDir() && !info.Mode().IsRegular() {
			return nil, fmt.Errorf(vaulticT("Choisissez directement un fichier ou un dossier ordinaire : %s"), abs)
		}
		if real, e := filepath.EvalSymlinks(abs); e == nil {
			abs = real
		}
		if _, err := os.Stat(abs); err != nil {
			return nil, fmt.Errorf(vaulticT("Source inaccessible : %s\nVérifiez le chemin, les permissions et le branchement du disque.\n%w"), abs, err)
		}
		if localRepo && vaulticContained(abs, repoAbs) {
			return nil, fmt.Errorf(vaulticT("Le dépôt %q ne doit pas se trouver dans une source sauvegardée (%q)"), repoAbs, abs)
		}
		cleaned = append(cleaned, abs)
	}
	return cleaned, nil
}

var vaulticSnapshotID = regexp.MustCompile(`^[a-fA-F0-9]{8,64}$`)

func vaulticRestoreTarget(target string) (string, error) {
	if target == "" {
		return "", errors.New(vaulticT("Choisissez un dossier de restauration"))
	}
	abs, err := filepath.Abs(target)
	if err != nil {
		return "", err
	}
	info, err := os.Lstat(abs)
	if err != nil {
		return "", fmt.Errorf(vaulticT("Dossier de restauration inaccessible : %w"), err)
	}
	if !info.IsDir() || info.Mode()&(os.ModeSymlink|os.ModeIrregular) != 0 {
		return "", errors.New(vaulticT("Choisissez un dossier de restauration qui n’est pas un lien"))
	}
	canonical, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return "", err
	}
	entries, err := os.ReadDir(canonical)
	if err != nil {
		return "", fmt.Errorf(vaulticT("Choisissez un dossier de restauration existant et vide : %w"), err)
	}
	if len(entries) > 0 {
		return "", errors.New(vaulticT("Pour éviter tout écrasement, choisissez un dossier de restauration vide"))
	}
	return canonical, nil
}

func vaulticMain(startHidden bool) {
	// One Vaultic per settings file: a second launch asks the first to show
	// itself and leaves.
	instance, running, instanceErr := vaulticAcquireInstance(vaulticInstanceKey(vaulticSettingsPath()))
	if running {
		return
	}
	if instance != nil {
		defer instance.Release()
	}
	fileLog := newVaulticLogFile(vaulticLogDir(vaulticSettingsPath()))
	defer fileLog.RecoverAndRepanic()
	if instanceErr != nil {
		fileLog.Write("warn", "single-instance guard unavailable: "+instanceErr.Error())
	}
	a := app.NewWithID("fr.cybou.vaultic")
	a.SetIcon(vaulticAppIcon())
	a.Settings().SetTheme(&vaulticVisualTheme{})
	w := a.NewWindow(vaulticT("Vaultic — sauvegardes chiffrées"))
	w.SetMaster()

	saved, settingsErr := vaulticLoadSettings()
	// Choose the language before any text is built.
	vaulticSetLanguage(vaulticResolveLanguage(saved.Language, string(lang.SystemLocale())))
	vaulticLocalizeTables()
	fileLog.Start(vaulticLanguage())
	w.SetTitle(vaulticT("Vaultic — sauvegardes chiffrées"))
	initialProfile := saved.migrateBackupProfiles()
	vaulticPrepareLoadedSettings(&saved)
	var snapshots *vaulticSnapshotsController
	var profiles *vaulticProfilesController
	var retention *vaulticRetentionController
	recentRunValue := widget.NewLabelWithStyle(vaulticT("Aucune"), fyne.TextAlignLeading, fyne.TextStyle{Bold: true})
	recentRunValue.Truncation = fyne.TextTruncateEllipsis
	var maintenance *vaulticMaintenanceController

	var repos *vaulticRepositoriesController

	var runner *vaulticEngineRunner
	panel := newVaulticStatusPanel(func() { runner.RequestCancel() })
	status := panel.Status
	logView := newVaulticLogView()
	appendLog := func(message string) {
		logView.Append(message)
		fileLog.Info(message)
	}
	persist := func(depots []vaulticDepot, active string) error {
		if settingsErr != nil {
			return settingsErr
		}
		return vaulticSaveSettings(vaulticSettingsForSave(saved, depots, active, profiles.Index()))
	}
	save := func() {
		if err := persist(saved.Depots, repos.Active()); err != nil {
			appendLog(vaulticT("Erreur d'enregistrement des préférences : ") + err.Error())
		}
	}

	var busy atomic.Bool
	historyStore := newVaulticHistoryStore(vaulticHistoryPath())
	if err := historyStore.Load(); err != nil {
		appendLog(err.Error())
	}
	var backup *vaulticBackupController
	var automationUI *vaulticAutomationUI
	var notifyPrefs atomic.Pointer[vaulticNotifyPrefs]
	{
		prefs := vaulticDefaultNotifyPrefs()
		if saved.Notifications != nil {
			prefs = *saved.Notifications
		}
		notifyPrefs.Store(&prefs)
	}
	var stopFocusFollow func()
	closeWindow := func() {
		if backup != nil {
			backup.Shutdown()
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
			maintenance.Reset()
			if retention != nil {
				retention.invalidate()
			}
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
			if retention != nil {
				retention.Load()
			}
		},
		Renamed: func(oldName, newName string) {
			if err := historyStore.RenameProfile(oldName, newName); err != nil {
				appendLog(vaulticT("Historique : ") + err.Error())
			}
			if automationUI != nil {
				automationUI.Refresh()
			}
		},
		RunWithDepot: func(title string, fn func(context.Context, global.Options) error, done func() error) {
			runner.Run(title, repos.Active(), repos.Password.Text, vaulticRunOptions{}, fn, done)
		},
	})
	backup = newVaulticBackupController(vaulticBackupHooks{
		Window:        w,
		App:           a,
		Settings:      &saved,
		Persist:       func() error { return persist(saved.Depots, repos.Active()) },
		Repository:    func() string { return repos.Active() },
		Password:      func() string { return repos.Password.Text },
		CloudAuth:     func(repo string) vaulticCloudAuth { return repos.Sessions.get(repo) },
		ProfileName:   func() string { return profiles.Current().Name },
		History:       historyStore,
		NotifyPrefs:   func() vaulticNotifyPrefs { return *notifyPrefs.Load() },
		Runner:        runner,
		Panel:         panel,
		Busy:          &busy,
		SetBusy:       setBusy,
		Log:           appendLog,
		CloseNow:      closeWindow,
		Completed:     recentRunValue.SetText,
		UpdateActions: func() { updateActions() },
		RefreshAutomation: func() {
			if automationUI != nil {
				automationUI.Refresh()
			}
		},
	})
	backup.StartScheduler()

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
	maintenance = newVaulticMaintenanceController(vaulticMaintenanceHooks{Run: run})

	actionButtons = []*widget.Button{repos.Verify, repos.EditAuth, repos.ClearAuth, profiles.AddFolder, profiles.AddFile, snapshots.Refresh, maintenance.Check, maintenance.Analyze, profiles.New, profiles.Rename, profiles.Duplicate, profiles.Delete, profiles.Exclusions, profiles.Preview}
	updateActions = func() {
		if len(profiles.Sources()) == 0 {
			profiles.Empty.Show()
		} else {
			profiles.Empty.Hide()
		}
		snapshots.UpdateState(busy.Load())
		backup.UpdateState(busy.Load(), len(profiles.Sources()) > 0)
		vaulticSetButtonEnabled(profiles.Exclusions, !busy.Load())
		vaulticSetButtonEnabled(profiles.Preview, !busy.Load() && len(profiles.Sources()) > 0 && len(profiles.Current().Excludes) > 0)
		vaulticSetButtonEnabled(profiles.Duplicate, !busy.Load())
		vaulticSetButtonEnabled(profiles.Delete, !busy.Load() && len(saved.Profiles) > 1)
		maintenance.UpdateState(busy.Load(), repos.Active() != "")
		retention.UpdateState(busy.Load(), repos.Active() != "", len(profiles.Sources()) > 0)
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
	startBackup := widget.NewButtonWithIcon(vaulticT("Préparer une sauvegarde"), theme.MediaPlayIcon(), nil)
	startBackup.Importance = widget.HighImportance
	setupButton := widget.NewButtonWithIcon(vaulticT("Configurer le dépôt"), theme.SettingsIcon(), nil)
	viewSnapshots := widget.NewButtonWithIcon(vaulticT("Voir les instantanés"), theme.HistoryIcon(), nil)
	viewStorage := widget.NewButtonWithIcon(vaulticT("Espace de stockage"), theme.StorageIcon(), nil)
	overviewPage := vaulticOverviewPage(vaulticOverviewParts{
		SourceCount: profiles.CountValue, SnapshotCount: snapshots.CountValue, Repository: repos.Value,
		RecentRun: recentRunValue, RecentSnapshot: snapshots.RecentValue,
		StartBackup: startBackup, Setup: setupButton, ViewSnapshots: viewSnapshots, ViewStorage: viewStorage,
	})
	repositoriesPage := vaulticRepositoriesPage(vaulticRepositoriesParts{
		SavedList: repos.ListBox, ActiveRepository: repos.Value, Password: repos.Password,
		Remember: repos.Remember,
		Verify:   repos.Verify, EditAuth: repos.EditAuth, ClearAuth: repos.ClearAuth,
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
				return errors.New(vaulticT("Enregistrement impossible : consultez le message affiché."))
			}
			appendLog(vaulticT("Planification du profil « ") + next[profiles.Index()].Name + " » : " + schedule.Describe())
			return nil
		},
		Paused: func() bool { return saved.SchedulerPaused },
		SetPaused: func(paused bool) {
			saved.SchedulerPaused = paused
			if !paused {
				backup.Coord.Resume()
			} // nothing missed during a pause is made up afterwards
			if err := persist(saved.Depots, repos.Active()); err != nil {
				appendLog(vaulticT("Erreur d'enregistrement des préférences : ") + err.Error())
			}
			appendLog(map[bool]string{true: vaulticT("Planification mise en pause."), false: vaulticT("Planification reprise.")}[paused])
		},
		Notify: func() vaulticNotifyPrefs { return *notifyPrefs.Load() },
		SetNotify: func(prefs vaulticNotifyPrefs) {
			notifyPrefs.Store(&prefs)
			saved.Notifications = &prefs
			if err := persist(saved.Depots, repos.Active()); err != nil {
				appendLog(vaulticT("Erreur d'enregistrement des préférences : ") + err.Error())
			}
		},
		Coordinator: backup.Coord, History: historyStore, Now: time.Now,
		ActiveRepository: func() string { return repos.Active() },
		Credentials:      func() bool { return repos.Password.Text != "" },
		Location:         func() *time.Location { return time.Local },
	})
	retention = newVaulticRetentionController(vaulticRetentionHooks{
		Window:  w,
		Profile: func() vaulticBackupProfile { return profiles.Current() },
		SaveRetention: func(policy vaulticRetention) error {
			next := append([]vaulticBackupProfile(nil), saved.Profiles...)
			next[profiles.Index()].Retention = &policy
			if !profiles.Apply(next, profiles.Index()) {
				return errors.New(vaulticT("Enregistrement impossible : consultez le message affiché."))
			}
			return nil
		},
		Repository: func() string { return repos.Active() },
		Run: func(title string, fn func(context.Context, global.Options) error, finish func() error) {
			runner.Run(title, repos.Active(), repos.Password.Text, vaulticRunOptions{}, fn, finish)
		},
		History:       historyStore,
		Log:           appendLog,
		UpdateActions: func() { updateActions() },
	})
	backupPage, backupScrolls := vaulticBackupPage(vaulticBackupParts{
		Profile: profiles.Select, NewProfile: profiles.New, RenameProfile: profiles.Rename,
		DuplicateProfile: profiles.Duplicate, DeleteProfile: profiles.Delete,
		Exclusions: profiles.Exclusions, PreviewExclusions: profiles.Preview, ExclusionSummary: profiles.ExclusionSummary,
		Sources: profiles.Section, BackupNow: backup.BackupButton, LastReport: backup.ReportButton,
		Retention: retention.Content,
	}, automationUI.Content)
	snapshotPage := vaulticSnapshotPage(vaulticSnapshotParts{
		Refresh: snapshots.Refresh, Details: snapshots.DetailsScroll,
		DetailsButton: snapshots.DetailsButton, Compare: snapshots.Compare,
		Explorer: snapshots.Explorer, Restore: snapshots.Restore,
		List: snapshots.list, Empty: snapshots.Empty,
	})
	storagePage := vaulticStoragePage(vaulticStorageParts{
		Analyze: maintenance.Analyze, Updated: maintenance.Updated,
		Volume: maintenance.Volume, Blobs: maintenance.Blobs, Snapshots: maintenance.Snapshots,
	})
	checkPage := vaulticIntegrityPage(maintenance.Check)
	journalPage := vaulticActivityPage(logView.Entry, logView.Clear)
	tray, trayOK := newVaulticTray(a)
	background := newVaulticBackgroundController(vaulticBackgroundHooks{
		Window: w, Settings: &saved,
		Persist:       func() error { return persist(saved.Depots, repos.Active()) },
		Autostart:     newVaulticSystemAutostart(),
		Executable:    os.Executable,
		TrayAvailable: trayOK,
		Log:           appendLog,
	})
	languageUI := newVaulticLanguageController(vaulticLanguageHooks{
		Window: w, Settings: &saved,
		Persist: func() error { return persist(saved.Depots, repos.Active()) },
	})
	updateUI := newVaulticUpdateController(vaulticUpdateHooks{
		API: vaulticReleasesAPI, Current: vaulticVersion,
		Open: func(page string) {
			if u, err := url.Parse(page); err == nil {
				_ = a.OpenURL(u)
			}
		},
	})
	diagnosticsUI := newVaulticDiagnosticsController(vaulticDiagnosticsHooks{
		Dir:  fileLog.Dir(),
		Open: func(dir string) { _ = a.OpenURL(vaulticFileURL(dir)) },
	})
	settingsPage := vaulticSettingsPage(background.Content, languageUI.Content, updateUI.Content, diagnosticsUI.Content)

	pages := []fyne.CanvasObject{overviewPage, backupPage, snapshotPage, storagePage, checkPage, journalPage, repositoriesPage, settingsPage}
	stopFocusFollow = vaulticKeepFocusVisible(w, append([]*container.Scroll{overviewPage, storagePage, checkPage, repositoriesPage, settingsPage}, backupScrolls...))
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
	repos.LoadRemembered()
	profiles.Refresh()
	w.SetContent(windowContent)
	windowShown := false
	reveal := func() {
		if !windowShown {
			windowShown = true
			vaulticShowWindow(w, fyne.NewSize(1180, 760), fyne.NewSize(640, 365))
		} else {
			w.Show()
		}
		w.RequestFocus()
	}
	trayNoticeShown := false
	closer := &vaulticWindowCloser{
		HideToTray: func() bool {
			if !background.ShouldHideOnClose() || !tray.Installed() {
				return false
			}
			w.Hide()
			if !trayNoticeShown {
				trayNoticeShown = true
				a.SendNotification(&fyne.Notification{Title: vaulticT("Vaultic continue en arrière-plan"),
					Content: vaulticT("Les sauvegardes planifiées restent actives. Pour quitter : clic droit sur l'icône, « Quitter Vaultic ».")})
			}
			return true
		},
		Busy: busy.Load, Closing: runner.Closing, BeginClose: runner.BeginClose,
		CloseNow: closeWindow, Confirm: vaulticConfirm(w),
	}
	w.SetCloseIntercept(closer.Intercept)
	if trayOK {
		tray.Install(vaulticAppIcon(), vaulticTrayHooks{
			Show: func() { fyne.Do(reveal) },
			BackupNow: func() {
				fyne.Do(func() { backup.Launch(vaulticTriggerManual, profiles.Current().Name, time.Time{}, false) })
			},
			Quit: func() { fyne.Do(closer.Quit) },
		})
	}
	if !(startHidden && tray.Installed()) {
		reveal()
	}
	if instance != nil {
		// A later launch brings this window forward, even from the tray.
		instance.OnShow(func() { fyne.Do(reveal) })
	}
	if settingsErr != nil {
		dialog.ShowError(fmt.Errorf(vaulticT("Enregistrement des préférences désactivé pour cette session : %w"), settingsErr), w)
	}
	a.Run()
	fileLog.Info("Vaultic stopped")
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
