package main

// Vaultic is built in the official restic source tree. All backup operations
// call restic's Go functions directly; no restic executable or shell is launched.
import (
	"context"
	"encoding/json"
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
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"github.com/restic/restic/internal/global"
	"github.com/restic/restic/internal/repository"
	"github.com/restic/restic/internal/restic"
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

	activeRepository := saved.Repository
	cloudSessions := &vaulticCloudSessions{}
	var setupUI *vaulticStorageSetupUI
	var allowNoPassword, inlineResult bool // consumed by the next runWithDepot call
	repoListBox := container.NewVBox()
	var repoRowButtons []*widget.Button
	repoValue := widget.NewLabel("Aucun dépôt configuré")
	repoValue.Wrapping = fyne.TextWrapWord
	updateRepositoryLabel := func(value string) {
		if strings.TrimSpace(value) == "" {
			repoValue.SetText("Aucun dépôt configuré")
		} else {
			repoValue.SetText(strings.TrimSpace(value))
		}
	}
	updateRepositoryLabel(activeRepository)
	passEntry := widget.NewPasswordEntry()
	passEntry.SetPlaceHolder("Mot de passe du dépôt — jamais enregistré")

	status := widget.NewLabel("Prêt")
	status.Wrapping = fyne.TextWrapWord
	progress := widget.NewProgressBar()
	progress.Hide()
	progressPlaceholder := canvas.NewRectangle(vaulticBackground)
	progressPlaceholder.SetMinSize(fyne.NewSize(0, progress.MinSize().Height))
	progressPlaceholder.Hide()
	progressSlot := container.NewStack(progressPlaceholder, progress)
	progressSlot.Hide()
	logs := widget.NewMultiLineEntry()
	logs.SetMinRowsVisible(4)
	logs.SetPlaceHolder("Aucun événement dans cette session.")
	logs.Wrapping = fyne.TextWrapWord
	logs.Disable()
	var logText string
	appendLog := func(msg string) {
		if strings.TrimSpace(msg) == "" {
			return
		}
		logText += time.Now().Format("15:04:05") + "  " + msg + "\n"
		if len(logText) > 16000 {
			logText = logText[len(logText)-12000:]
		}
		logs.SetText(logText)
	}
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
		if err := persist(saved.Depots, activeRepository); err != nil {
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
	var cancelOp context.CancelFunc
	closing := false
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
		passEntry.SetText("")
		if setupUI != nil {
			setupUI.pass.SetText("")
		}
		cloudSessions.clearAll()
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
	cancelButton := widget.NewButton("Arrêter", func() {
		if cancelOp != nil {
			cancelOp()
			status.SetText("Annulation…")
		}
	})
	cancelButton.Hide()
	setBusy := func(on bool) {
		if on {
			cancelButton.Enable()
			cancelButton.Show()
			passEntry.Disable()
		} else {
			cancelButton.Hide()
			progress.Hide()
			progressPlaceholder.Hide()
			progressSlot.Hide()
			passEntry.Enable()
		}
		if setupUI != nil {
			setupUI.SetBusy(on)
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

	// Called from the restic worker's goroutines; all widget access is marshaled
	// onto Fyne's UI thread. restic emits native JSON backup progress.
	onOutput := func(ctx context.Context, title, line string, report *vaulticBackupReport, redact func(string) string) {
		if line == "" {
			return
		}
		var e vaulticEngineEvent
		if json.Unmarshal([]byte(line), &e) == nil {
			if report != nil {
				report.record(e)
			}
			switch e.MessageType {
			case "status":
				fyne.Do(func() {
					if ctx.Err() != nil {
						return
					}
					if e.TotalBytes > 0 && e.PercentDone >= 0 && e.PercentDone <= 1 {
						progress.SetValue(e.PercentDone)
						if !progress.Visible() {
							progress.Show()
							progressSlot.Show()
							windowContent.Refresh()
						}
					}
					if report != nil {
						files := fmt.Sprintf("%d fichiers", e.FilesDone)
						if e.TotalFiles > 0 {
							files = fmt.Sprintf("%d / %d fichiers", e.FilesDone, e.TotalFiles)
						}
						metrics := files + " · " + vaulticBytes(e.BytesDone)
						if e.TotalBytes > 0 {
							metrics += " / " + vaulticBytes(e.TotalBytes)
						}
						if e.SecondsElapsed > 0 {
							metrics += " · " + vaulticBytes(e.BytesDone/e.SecondsElapsed) + "/s"
						}
						status.SetText(metrics)
					} else if e.TotalFiles > 0 {
						status.SetText(fmt.Sprintf("%s en cours · %d / %d fichiers", title, e.FilesDone+e.FilesRestored, e.TotalFiles))
					}
				})
				return
			case "summary":
				if report != nil {
					return
				}
				if e.SnapshotID != "" {
					line = fmt.Sprintf("Instantané %s · %d fichier(s) traité(s), %d octets", e.SnapshotID, e.FilesProcessed, e.BytesProcessed)
				} else if e.TotalFiles > 0 {
					line = fmt.Sprintf("%d élément(s) restauré(s) sur %d · %d octets", e.FilesRestored, e.TotalFiles, e.BytesRestored)
				} else {
					return
				}
			case "error":
				if e.Error.Message != "" {
					line = e.Error.Message
				}
			}
		}
		line = redact(line)
		fyne.Do(func() { appendLog(line) })
	}

	runWithDepot := func(title, repo, password string, create bool, fn func(context.Context, global.Options) error, finish func() error, report *vaulticBackupReport) {
		noPassword, inline := allowNoPassword, inlineResult
		allowNoPassword, inlineResult = false, false
		if repo == "" || (password == "" && !noPassword) {
			dialog.ShowInformation("Informations manquantes", "Indiquez un dépôt et son mot de passe.", w)
			return
		}
		if !busy.CompareAndSwap(false, true) {
			dialog.ShowInformation("Opération en cours", "Une seule opération est autorisée à la fois.", w)
			return
		}
		ctx, cancel := context.WithCancel(context.Background())
		cancelOp = cancel
		if report != nil {
			progressPlaceholder.Show()
			progressSlot.Show()
		}
		setBusy(true)
		progress.SetValue(0)
		status.SetText(title + "…")
		appendLog("Début : " + title)
		cloudAuth := cloudSessions.get(repo)
		redact := vaulticRedactCloudSecrets(vaulticRedactOperation(password), cloudAuth)
		go func() {
			terminal := &vaulticTerminal{line: func(line string) { onOutput(ctx, title, line, report, redact) }, output: io.Discard}
			gopts := global.Options{
				Repo: vaulticResolvedRepository(repo), Password: password, Backends: vaulticBackendsForSession(cloudAuth),
				Term: terminal, JSON: true, Verbosity: 1,
			}
			var err error
			if !create && !inline && vaulticLocalRepository(repo) {
				if _, statErr := os.Stat(gopts.Repo); statErr != nil {
					err = fmt.Errorf("dépôt inaccessible. Vérifiez le chemin et le branchement du disque : %w", statErr)
				}
			}
			if err == nil {
				err = fn(ctx, gopts)
			}
			fyne.Do(func() {
				cancel()
				cancelOp = nil
				busy.Store(false)
				if errors.Is(err, repository.ErrNoKeyFound) {
					err = errors.New("Mot de passe incorrect ou aucune clé utilisable dans ce dépôt.")
				}
				if closing {
					if err == nil && finish != nil {
						_ = finish()
					}
					closeWindow()
					return
				}
				if report != nil {
					var outcome string
					outcome, lastBackupReport, err = report.result(err)
					lastBackupReport = redact(lastBackupReport)
					status.SetText("Sauvegarde · " + outcome)
					appendLog("Sauvegarde · " + lastBackupReport)
					if err == nil {
						recentRunValue.SetText(title)
					}
					setBusy(false)
					showBackupReport()
					return
				}
				setBusy(false)
				if errors.Is(err, context.Canceled) {
					status.SetText(title + " — annulé")
					appendLog("Annulé : " + title)
					return
				}
				if err == nil && finish != nil {
					err = finish()
				}
				if err != nil {
					status.SetText("Erreur : " + title)
					message := redact(err.Error())
					appendLog(message)
					if inline {
						status.SetText(message)
					} else {
						dialog.ShowError(errors.New(message), w)
					}
					return
				}
				status.SetText(title + " — terminé")
				recentRunValue.SetText(title)
				appendLog("Terminé : " + title)
			})
		}()
	}
	run := func(title string, fn func(context.Context, global.Options) error) {
		runWithDepot(title, activeRepository, passEntry.Text, false, fn, nil, nil)
	}

	var activateDepot func(string, string)
	var refreshDepots func()
	var editingLocation string
	savedDepot := func(location string) (vaulticDepot, bool) {
		for _, depot := range saved.Depots {
			if depot.Location == location || vaulticResolvedRepository(depot.Location) == vaulticResolvedRepository(location) {
				return depot, true
			}
		}
		return vaulticDepot{}, false
	}
	normalizeAddress := func(address string) string {
		if vaulticLocalRepository(address) {
			localPath := strings.TrimPrefix(address, "local:")
			if filepath.IsAbs(localPath) {
				return vaulticPortablePath(localPath)
			}
			return filepath.Clean(localPath)
		}
		return address
	}
	authHasValues := func(a vaulticCloudAuth) bool {
		return a.hasValues()
	}
	// recordProbe stores the outcome of a connection test (UI thread only).
	recordProbe := func(location string, res vaulticProbeResult) {
		depot, ok := savedDepot(location)
		if !ok || res.State == vaulticConnNotTested {
			return
		}
		next := vaulticRecordVerification(saved.Depots, depot.Location, res.State, time.Now())
		if err := persist(next, activeRepository); err != nil {
			appendLog("Erreur d'enregistrement des préférences : " + err.Error())
			return
		}
		saved.Depots = next
		refreshDepots()
	}
	// probe runs on the worker goroutine; the detail text is redacted because
	// it may quote engine errors.
	probe := func(ctx context.Context, g global.Options, location string) vaulticProbeResult {
		return vaulticProbeRedacted(ctx, g, vaulticProbeTimeout, cloudSessions.get(location), vaulticVerifyDepot)
	}
	// testRepository checks a repository without changing it. Without a
	// password it can only prove reachability, never a successful connection.
	testRepository := func(location, password string, fromForm bool) {
		if busy.Load() {
			return
		}
		if fromForm {
			setupUI.SetState(vaulticProbeResult{State: vaulticConnTesting})
		}
		allowNoPassword, inlineResult = true, true
		runWithDepot("Test de la connexion", location, password, false, func(ctx context.Context, g global.Options) error {
			res := probe(ctx, g, location)
			if _, known := savedDepot(location); !known {
				cloudSessions.forget(location)
			}
			fyne.Do(func() {
				if fromForm {
					setupUI.SetState(res)
				}
				recordProbe(location, res)
			})
			switch res.State {
			case vaulticConnConnected, vaulticConnReachable:
				return nil
			case vaulticConnNotTested:
				return context.Canceled
			}
			return errors.New(res.State.Label() + " — " + res.Detail)
		}, nil, nil)
	}
	connectDepot := func(setup vaulticStorageSetup, create bool) {
		name, address := setup.Name, normalizeAddress(setup.Address)
		if _, exists := savedDepot(address); exists {
			dialog.ShowInformation("Dépôt déjà enregistré", "Utilisez un nom et un emplacement différents, ou sélectionnez le dépôt enregistré.", w)
			return
		}
		for _, depot := range saved.Depots {
			if strings.EqualFold(depot.Name, name) {
				dialog.ShowInformation("Dépôt déjà enregistré", "Utilisez un nom et un emplacement différents, ou sélectionnez le dépôt enregistré.", w)
				return
			}
		}
		if authHasValues(setup.Auth) {
			if err := cloudSessions.set(address, setup.Auth); err != nil {
				dialog.ShowError(err, w)
				return
			}
		}
		password := setup.Password
		title := "Connexion au dépôt"
		if create {
			title = "Création du dépôt"
		}
		setupUI.SetState(vaulticProbeResult{State: vaulticConnTesting})
		var outcome vaulticProbeResult
		inlineResult = true
		runWithDepot(title, address, password, create, func(ctx context.Context, g global.Options) error {
			var err error
			if create {
				createCtx, stop := context.WithTimeout(ctx, 2*vaulticProbeTimeout)
				defer stop()
				printer := ui.NewProgressPrinter(g.JSON, g.Verbosity, g.Term)
				var repo *repository.Repository
				repo, err = global.CreateRepository(createCtx, g, restic.StableRepoVersion, nil, printer)
				if err == nil {
					err = repo.Close()
				}
				if ctx.Err() == nil && createCtx.Err() != nil {
					err = createCtx.Err()
				}
				outcome = vaulticClassifyProbe(err, true)
				if err == nil {
					outcome.Detail = "Nouveau dépôt créé et ouvert."
				}
			} else {
				outcome = probe(ctx, g, address)
				switch outcome.State {
				case vaulticConnConnected:
				case vaulticConnNotTested:
					err = context.Canceled
				default:
					err = errors.New(outcome.State.Label() + " — " + outcome.Detail)
				}
			}
			if err != nil {
				cloudSessions.forget(address)
			}
			outcome.Detail = vaulticRedactCloudSecrets(vaulticRedactOperation(password), setup.Auth)(outcome.Detail)
			final := outcome
			fyne.Do(func() { setupUI.SetState(final) })
			return err
		}, func() error {
			depots := append(append([]vaulticDepot(nil), saved.Depots...), vaulticDepot{
				Name: name, Location: address,
				LastVerified: time.Now().UTC().Format(time.RFC3339), LastVerifiedState: vaulticConnConnected.Key(),
			})
			if err := persist(depots, address); err != nil {
				return err
			}
			saved.Depots = depots
			activateDepot(address, password)
			refreshDepots()
			final := outcome
			setupUI.Reset()
			setupUI.SetState(final)
			return nil
		}, nil)
	}
	saveEditedDepot := func(setup vaulticStorageSetup) {
		old, address := editingLocation, normalizeAddress(setup.Address)
		updated, err := vaulticEditSavedDepot(saved.Depots, old, setup.Name, address)
		if err != nil {
			dialog.ShowError(err, w)
			return
		}
		active := activeRepository
		if active == old {
			active = address
		}
		if err := persist(updated, active); err != nil {
			dialog.ShowError(err, w)
			return
		}
		saved.Depots = updated
		if old != address {
			cloudSessions.forget(old)
			if authHasValues(setup.Auth) {
				if err := cloudSessions.set(address, setup.Auth); err != nil {
					dialog.ShowError(err, w)
				}
			}
			if activeRepository == old {
				activateDepot(address, "")
			}
		}
		editingLocation = ""
		setupUI.Reset()
		refreshDepots()
		appendLog("Dépôt modifié dans Vaultic : " + setup.Name + " (aucune donnée distante modifiée).")
	}
	cloudWizardHook := func(scheme string) {
		if busy.Load() {
			return
		}
		vaulticShowCloudWizard(w, scheme, func(setup vaulticCloudSetup, create bool) error {
			if busy.Load() {
				return errors.New("Une opération est déjà en cours.")
			}
			if _, exists := savedDepot(setup.Address); exists {
				return errors.New("Ce nom ou emplacement est déjà enregistré dans Vaultic.")
			}
			connectDepot(vaulticStorageSetup{Name: setup.Name, Address: setup.Address, Password: setup.Password, Auth: setup.Auth}, create)
			return nil
		})
	}
	verifyButton := widget.NewButton("Vérifier la connexion", func() {
		testRepository(activeRepository, passEntry.Text, false)
	})
	cloudAuthEditorButton := widget.NewButtonWithIcon("Configurer l'accès du dépôt…", theme.SettingsIcon(), func() {
		if busy.Load() || activeRepository == "" {
			return
		}
		info, ok := vaulticBackendForAddress(activeRepository)
		if !ok {
			return
		}
		vaulticShowCloudSessionWizard(w, info.Scheme, func(auth vaulticCloudAuth) error {
			if busy.Load() {
				return errors.New("Une opération est déjà en cours.")
			}
			if err := cloudSessions.set(activeRepository, auth); err != nil {
				return err
			}
			updateActions()
			if passEntry.Text != "" {
				testRepository(activeRepository, passEntry.Text, false)
			} else {
				dialog.ShowInformation("Mot de passe requis", "Identifiants configurés pour cette session. Saisissez le mot de passe restic du dépôt puis utilisez « Vérifier la connexion ».", w)
			}
			return nil
		})
	})
	clearCloudAuthButton := widget.NewButtonWithIcon("Effacer les identifiants de la session", theme.DeleteIcon(), func() {
		if activeRepository == "" || busy.Load() {
			return
		}
		cloudSessions.forget(activeRepository)
		updateActions()
		dialog.ShowInformation("Identifiants effacés", "Les identifiants en mémoire ont été effacés pour ce dépôt. Les données restic et la configuration système ne sont pas modifiées.", w)
	})
	repositoryHooks := vaulticRepositoryHooks{
		Connect: func(depot vaulticDepot) {
			if busy.Load() {
				return
			}
			if depot.Location == activeRepository && passEntry.Text != "" {
				testRepository(depot.Location, passEntry.Text, false)
				return
			}
			if depot.Location != activeRepository {
				if err := persist(saved.Depots, depot.Location); err != nil {
					dialog.ShowError(err, w)
					return
				}
				activateDepot(depot.Location, "")
			}
			refreshDepots()
			status.SetText("Dépôt « " + depot.Name + " » sélectionné — saisissez son mot de passe puis vérifiez la connexion.")
			w.Canvas().Focus(passEntry)
		},
		Edit: func(depot vaulticDepot) {
			if busy.Load() {
				return
			}
			if err := setupUI.Edit(depot.Name, depot.Location); err != nil {
				dialog.ShowError(err, w)
				return
			}
			editingLocation = depot.Location
		},
		Test: func(depot vaulticDepot) {
			if busy.Load() {
				return
			}
			password := ""
			if depot.Location == activeRepository {
				password = passEntry.Text
			}
			testRepository(depot.Location, password, false)
		},
		Forget: func(depot vaulticDepot) {
			location, name := depot.Location, depot.Name
			dialog.ShowConfirm("Oublier le dépôt",
				"Retirer « "+name+" » de la liste Vaultic ?\n\nAucun fichier de sauvegarde ni instantané ne sera supprimé, ni localement ni à distance. Vous pourrez reconnecter le dépôt plus tard.",
				func(ok bool) {
					if !ok || busy.Load() {
						return
					}
					next, active, err := vaulticForgetSavedDepot(saved.Depots, activeRepository, location)
					if err != nil {
						dialog.ShowError(err, w)
						return
					}
					if err := persist(next, active); err != nil {
						dialog.ShowError(err, w)
						return
					}
					saved.Depots = next
					cloudSessions.forget(location)
					if activeRepository == location {
						activateDepot(active, "")
					}
					if editingLocation == location {
						editingLocation = ""
						setupUI.Reset()
					}
					refreshDepots()
					appendLog("Dépôt retiré de la liste Vaultic : " + name + " (sauvegardes non supprimées).")
				}, w)
		},
	}

	profiles = newVaulticProfilesController(&saved, initialProfile, vaulticProfilesHooks{
		Window:        w,
		Persist:       func() error { return persist(saved.Depots, activeRepository) },
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
			runWithDepot(title, activeRepository, passEntry.Text, false, fn, done, nil)
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
		if activeRepository == "" || passEntry.Text == "" {
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
		repo, password := activeRepository, passEntry.Text
		cloudAuth := cloudSessions.get(repo)
		redact := vaulticRedactCloudSecrets(vaulticRedactOperation(password), cloudAuth)
		title := "Sauvegarde"
		if !manual {
			title = "Sauvegarde planifiée"
		}
		cancelOp = coord.Cancel
		progressPlaceholder.Show()
		progressSlot.Show()
		setBusy(true)
		progress.SetValue(0)
		status.SetText(title + "…")
		appendLog("Début : " + title + " — profil « " + profileName + " »")
		gopts := global.Options{
			Repo: vaulticResolvedRepository(repo), Password: password, Backends: vaulticBackendsForSession(cloudAuth),
			JSON: true, Verbosity: 1,
		}
		exec := vaulticBackupExecutor(gopts, append([]string(nil), profile.Sources...), append([]string(nil), profile.Excludes...),
			func(report *vaulticBackupReport, line string) {
				onOutput(context.Background(), title, line, report, redact)
			})
		job := vaulticJob{Profile: profileName, Repository: repo, Trigger: trigger, Slot: slot, CatchUp: catchUp, Redact: redact}
		go func() {
			record := coord.RunAcquired(job, exec)
			fyne.Do(func() {
				cancelOp = nil
				lastBackupReport = coord.LastReport()
				status.SetText(title + " · " + vaulticStatusLabel(record.Status))
				appendLog(title + " · " + lastBackupReport)
				if record.Status == vaulticStatusSuccess {
					recentRunValue.SetText(title)
				}
				setBusy(false)
				if closing {
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
				if err := persist(saved.Depots, activeRepository); err != nil {
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
		Repository:     func() string { return activeRepository },
		HasCredentials: func() bool { return passEntry.Text != "" },
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
		ActiveRepository: func() string { return activeRepository },
		UpdateActions:    func() { updateActions() },
		Run:              func(title string, fn func(context.Context, global.Options) error) { run(title, fn) },
		RunWithDepot: func(title string, fn func(context.Context, global.Options) error, done func() error) {
			runWithDepot(title, activeRepository, passEntry.Text, false, fn, done, nil)
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

	actionButtons = []*widget.Button{verifyButton, cloudAuthEditorButton, clearCloudAuthButton, profiles.AddFolder, profiles.AddFile, snapshots.Refresh, checkButton, storageButton, profiles.New, profiles.Rename, profiles.Duplicate, profiles.Delete, profiles.Exclusions, profiles.Preview}
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
		vaulticSetButtonEnabled(storageButton, !busy.Load() && activeRepository != "")
		vaulticSetButtonEnabled(profiles.RemoveSource, !busy.Load() && profiles.Selected() >= 0)
		vaulticSetButtonEnabled(verifyButton, !busy.Load() && activeRepository != "")
		activeBackend, knownBackend := vaulticBackendForAddress(activeRepository)
		hasCloudEditor := knownBackend && (activeBackend.Scheme == "s3" || activeBackend.Scheme == "b2" || activeBackend.Scheme == "azure" || activeBackend.Scheme == "rest" || activeBackend.Scheme == "gs" || activeBackend.Scheme == "swift")
		vaulticSetButtonEnabled(cloudAuthEditorButton, !busy.Load() && hasCloudEditor)
		vaulticSetButtonEnabled(clearCloudAuthButton, !busy.Load() && activeRepository != "" && cloudSessions.get(activeRepository).hasSecret())
		if busy.Load() {
			profiles.Select.Disable()
		} else {
			profiles.Select.Enable()
		}
		for _, b := range repoRowButtons {
			vaulticSetButtonEnabled(b, !busy.Load())
		}
	}
	activateDepot = func(address, password string) {
		activeRepository = address
		passEntry.SetText(password)
		updateRepositoryLabel(address)
		snapshots.Clear()
		storageVolume.Text = "—"
		storageVolume.Refresh()
		storageBlobs.Text = "—"
		storageBlobs.Refresh()
		storageSnapshots.Text = "—"
		storageSnapshots.Refresh()
		storageUpdated.SetText("Aucune analyse effectuée pour ce dépôt.")
	}
	refreshDepots = func() {
		repoRowButtons = nil
		rows := make([]fyne.CanvasObject, 0, len(saved.Depots))
		for _, depot := range saved.Depots {
			row, buttons := vaulticRepositoryRow(depot, depot.Location == activeRepository, repositoryHooks)
			rows = append(rows, row)
			repoRowButtons = append(repoRowButtons, buttons...)
		}
		if len(rows) == 0 {
			rows = append(rows, vaulticWrappedLabel("Aucun dépôt enregistré. Ajoutez-en un ci-dessous."))
		}
		repoListBox.Objects = rows
		repoListBox.Refresh()
		updateActions()
	}
	setupUI = vaulticNewStorageSetupUI(vaulticStorageSetupHooks{
		Window: w,
		Browse: func(set func(string)) {
			dialog.NewFolderOpen(func(uri fyne.ListableURI, err error) {
				if err != nil {
					dialog.ShowError(err, w)
					return
				}
				if uri != nil {
					set(uri.Path())
				}
			}, w).Show()
		},
		Submit: func(setup vaulticStorageSetup, action string) {
			if busy.Load() {
				return
			}
			switch action {
			case vaulticActionTest:
				address := normalizeAddress(setup.Address)
				if authHasValues(setup.Auth) {
					if err := cloudSessions.set(address, setup.Auth); err != nil {
						dialog.ShowError(err, w)
						return
					}
				}
				testRepository(address, setup.Password, true)
			case vaulticActionSave:
				saveEditedDepot(setup)
			default:
				connectDepot(setup, action == vaulticActionCreate)
			}
		},
		Cancel: func() {
			if cancelOp != nil {
				cancelOp()
				status.SetText("Annulation…")
			}
		},
		Wizard:     cloudWizardHook,
		CancelEdit: func() { editingLocation = ""; setupUI.Reset() },
	})
	setupUI.SetBusy(busy.Load())

	// Every page has one purpose. Operations still invoke the original Go
	// restic engine; this shell only changes presentation and navigation.
	startBackup := widget.NewButtonWithIcon("Préparer une sauvegarde", theme.MediaPlayIcon(), nil)
	startBackup.Importance = widget.HighImportance
	setupButton := widget.NewButtonWithIcon("Configurer le dépôt", theme.SettingsIcon(), nil)
	viewSnapshots := widget.NewButtonWithIcon("Voir les instantanés", theme.HistoryIcon(), nil)
	viewStorage := widget.NewButtonWithIcon("Espace de stockage", theme.StorageIcon(), nil)
	overviewPage := vaulticOverviewPage(vaulticOverviewParts{
		SourceCount: profiles.CountValue, SnapshotCount: snapshots.CountValue, Repository: repoValue,
		RecentRun: recentRunValue, RecentSnapshot: snapshots.RecentValue,
		StartBackup: startBackup, Setup: setupButton, ViewSnapshots: viewSnapshots, ViewStorage: viewStorage,
	})
	repositoriesPage := vaulticRepositoriesPage(vaulticRepositoriesParts{
		SavedList: repoListBox, ActiveRepository: repoValue, Password: passEntry,
		Verify: verifyButton, EditAuth: cloudAuthEditorButton, ClearAuth: clearCloudAuthButton,
		Setup: setupUI.Content,
	})
	automationUI = newVaulticAutomationUI(vaulticAutomationHooks{
		Profile: func() vaulticBackupProfile { return profiles.Current() },
		SetSchedule: func(schedule vaulticSchedule) error {
			next, err := vaulticApplySchedule(saved.Profiles, profiles.Index(), schedule, activeRepository, time.Now())
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
			if err := persist(saved.Depots, activeRepository); err != nil {
				appendLog("Erreur d'enregistrement des préférences : " + err.Error())
			}
			appendLog(map[bool]string{true: "Planification mise en pause.", false: "Planification reprise."}[paused])
		},
		Notify: func() vaulticNotifyPrefs { return *notifyPrefs.Load() },
		SetNotify: func(prefs vaulticNotifyPrefs) {
			notifyPrefs.Store(&prefs)
			saved.Notifications = &prefs
			if err := persist(saved.Depots, activeRepository); err != nil {
				appendLog("Erreur d'enregistrement des préférences : " + err.Error())
			}
		},
		Coordinator: coord, History: historyStore, Now: time.Now,
		ActiveRepository: func() string { return activeRepository },
		Credentials:      func() bool { return passEntry.Text != "" },
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
	journalPage := vaulticActivityPage(logs, func() { logText = ""; logs.SetText("") })
	settingsPage := vaulticSettingsPage()

	pages := []fyne.CanvasObject{overviewPage, backupPage, snapshotPage, storagePage, checkPage, journalPage, repositoriesPage, settingsPage}
	stopFocusFollow = vaulticKeepFocusVisible(w, []*container.Scroll{overviewPage, storagePage, checkPage, repositoriesPage, settingsPage, automationScroll})
	nav := vaulticNewNavigation(pages)
	navigate := nav.Navigate
	setupButton.OnTapped = func() { navigate(vaulticPageRepositories) }
	startBackup.OnTapped = func() { navigate(vaulticPageBackup) }
	viewSnapshots.OnTapped = func() { navigate(vaulticPageSnapshots) }
	viewStorage.OnTapped = func() { navigate(vaulticPageStorage) }
	statusBar := vaulticStatusBar(progressSlot, status, cancelButton)
	if activeRepository == "" {
		navigate(vaulticPageRepositories)
	} else {
		navigate(vaulticPageOverview)
	}
	windowContent = container.NewBorder(nil, container.NewPadded(statusBar), nav.Sidebar, nil, nav.Pages)
	refreshDepots()
	profiles.Refresh()
	w.SetContent(windowContent)
	w.SetCloseIntercept(func() {
		if closing || closePrompt {
			return
		}
		if busy.Load() {
			closePrompt = true
			dialog.ShowConfirm("Quitter Vaultic", "Annuler l’opération et fermer Vaultic après son arrêt ?", func(ok bool) {
				closePrompt = false
				if !ok {
					return
				}
				if cancelOp == nil {
					closeWindow()
					return
				}
				closing = true
				cancelOp()
				status.SetText("Annulation avant fermeture…")
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
