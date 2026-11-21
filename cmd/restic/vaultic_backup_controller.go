package main

import (
	"context"
	"fmt"
	"sync/atomic"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/widget"

	"github.com/restic/restic/internal/global"
)

const (
	vaulticSchedulerFirstTick = 8 * time.Second
	vaulticSchedulerInterval  = 15 * time.Second
)

// vaulticRunScheduler calls tick once after first and then every interval until
// stop is closed. It blocks, so callers run it on its own goroutine.
func vaulticRunScheduler(stop <-chan struct{}, first, interval time.Duration, tick func()) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	firstTimer := time.NewTimer(first)
	defer firstTimer.Stop()
	for {
		select {
		case <-stop:
			return
		case <-firstTimer.C:
		case <-ticker.C:
		}
		tick()
	}
}

// vaulticFindProfile returns the profile with the given name.
func vaulticFindProfile(profiles []vaulticBackupProfile, name string) (vaulticBackupProfile, bool) {
	for _, candidate := range profiles {
		if candidate.Name == name {
			return candidate, true
		}
	}
	return vaulticBackupProfile{}, false
}

// vaulticBackupTitle is the operation title of a run.
func vaulticBackupTitle(trigger string) string {
	if trigger == vaulticTriggerManual {
		return "Sauvegarde"
	}
	return "Sauvegarde planifiée"
}

// vaulticSetSlot records slot as the latest handled slot of the profile's
// schedule. The in-memory anchor advances even when saving fails, so the same
// run is never launched again every tick; the caller surfaces the save error.
func vaulticSetSlot(profiles []vaulticBackupProfile, profile string, slot time.Time) bool {
	for i := range profiles {
		if profiles[i].Name == profile && profiles[i].Schedule != nil {
			profiles[i].Schedule.LastSlot = slot.Format(time.RFC3339)
			return true
		}
	}
	return false
}

// vaulticBackupHooks connects the backup controller to the application.
type vaulticBackupHooks struct {
	Window   fyne.Window
	App      fyne.App
	Settings *vaulticSettings
	// Persist saves the settings (profiles, depots, scheduler state).
	Persist func() error
	// Repository, Password and CloudAuth describe the active repository.
	Repository func() string
	Password   func() string
	CloudAuth  func(repo string) vaulticCloudAuth
	// ProfileName is the profile selected for a manual backup.
	ProfileName func() string

	History     *vaulticHistoryStore
	NotifyPrefs func() vaulticNotifyPrefs

	Runner *vaulticEngineRunner
	Panel  *vaulticStatusPanel
	Busy   *atomic.Bool

	SetBusy       func(on bool)
	Log           func(string)
	CloseNow      func()
	Completed     func(title string)
	UpdateActions func()
	// RefreshAutomation redraws the Automatisation tab.
	RefreshAutomation func()
}

// vaulticBackupController runs manual and scheduled backups through the
// coordinator: one engine operation at a time, one history, one set of
// notifications.
type vaulticBackupController struct {
	hooks vaulticBackupHooks

	Coord        *vaulticCoordinator
	BackupButton *widget.Button
	ReportButton *widget.Button
	lastReport   string
	stop         chan struct{}
	stopped      bool
}

func newVaulticBackupController(hooks vaulticBackupHooks) *vaulticBackupController {
	c := &vaulticBackupController{hooks: hooks, stop: make(chan struct{})}
	c.BackupButton = widget.NewButton("Sauvegarder maintenant", func() {
		c.Launch(vaulticTriggerManual, hooks.ProfileName(), time.Time{}, false)
	})
	c.BackupButton.Importance = widget.HighImportance
	c.ReportButton = widget.NewButton("Dernier rapport…", c.ShowReport)
	c.Coord = newVaulticCoordinator(hooks.Busy, vaulticCoordinatorEnv{
		Now:      time.Now,
		Location: func() *time.Location { return time.Local },
		Profiles: func() []vaulticBackupProfile {
			out := make([]vaulticBackupProfile, len(hooks.Settings.Profiles))
			for i, p := range hooks.Settings.Profiles {
				out[i] = p
				out[i].Schedule = vaulticCloneSchedule(p.Schedule)
			}
			return out
		},
		Repository:     hooks.Repository,
		HasCredentials: func() bool { return hooks.Password() != "" },
		Paused:         func() bool { return hooks.Settings.SchedulerPaused },
		SaveSlot:       c.saveSlot,
		// UI-owned settings change on the Fyne thread. A worker waits until
		// persistence finishes, so the slot is durable before the gate opens.
		SaveSlotWorker: func(profile string, slot time.Time) error {
			var saveErr error
			fyne.DoAndWait(func() { saveErr = c.saveSlot(profile, slot) })
			return saveErr
		},
		Start: func(job vaulticJob) bool { return c.Launch(job.Trigger, job.Profile, job.Slot, job.CatchUp) },
	}, hooks.History, newVaulticNotifier(
		hooks.NotifyPrefs,
		func(title, body string) {
			fyne.Do(func() { hooks.App.SendNotification(&fyne.Notification{Title: title, Content: body}) })
		},
		time.Now,
	))
	c.Coord.OnChange = func() {
		fyne.Do(func() {
			hooks.RefreshAutomation()
			hooks.UpdateActions()
		})
	}
	c.Coord.OnPersistenceError = func(err error) {
		fyne.Do(func() {
			hooks.Log("Erreur de sauvegarde des métadonnées : " + err.Error())
			hooks.Panel.Status.SetText("Attention : historique ou planification non enregistré(e).")
		})
	}
	return c
}

// StartScheduler begins evaluating schedules while the application is open.
func (c *vaulticBackupController) StartScheduler() {
	go vaulticRunScheduler(c.stop, vaulticSchedulerFirstTick, vaulticSchedulerInterval, func() {
		fyne.Do(func() {
			c.Coord.Tick()
			c.hooks.RefreshAutomation()
		})
	})
}

// Shutdown stops the scheduler and waits for a running backup to stop.
func (c *vaulticBackupController) Shutdown() {
	if !c.stopped {
		c.stopped = true
		close(c.stop)
	}
	c.Coord.Shutdown(15 * time.Second)
}

func (c *vaulticBackupController) saveSlot(profile string, slot time.Time) error {
	if !vaulticSetSlot(c.hooks.Settings.Profiles, profile, slot) {
		return fmt.Errorf("Profil planifié introuvable : %s", profile)
	}
	return c.hooks.Persist()
}

// Report is the report of the last backup ("" before any).
func (c *vaulticBackupController) Report() string { return c.lastReport }

// ShowReport opens the report of the last backup.
func (c *vaulticBackupController) ShowReport() {
	content := container.NewVScroll(vaulticWrappedLabel(c.lastReport))
	content.SetMinSize(fyne.NewSize(340, 170))
	reportDialog := dialog.NewCustom("Rapport de sauvegarde", "Fermer", content, c.hooks.Window)
	reportDialog.Resize(fyne.NewSize(490, 300))
	reportDialog.Show()
}

// UpdateState enables the controls for the application state.
func (c *vaulticBackupController) UpdateState(busy bool, hasSources bool) {
	vaulticSetButtonEnabled(c.BackupButton, !busy && hasSources)
	vaulticSetButtonEnabled(c.ReportButton, !busy && c.lastReport != "")
}

// Launch starts a backup of the named profile and reports whether it started.
// Manual launches explain a refusal in a dialog; scheduled ones stay silent
// (the coordinator records and notifies).
func (c *vaulticBackupController) Launch(trigger, profileName string, slot time.Time, catchUp bool) bool {
	h := c.hooks
	profile, found := vaulticFindProfile(h.Settings.Profiles, profileName)
	manual := trigger == vaulticTriggerManual
	if !found || len(profile.Sources) == 0 {
		return false
	}
	repo, password := h.Repository(), h.Password()
	if repo == "" || password == "" {
		if manual {
			dialog.ShowInformation("Informations manquantes", "Indiquez un dépôt et son mot de passe.", h.Window)
		}
		return false
	}
	if !c.Coord.TryAcquire() {
		if manual {
			dialog.ShowInformation("Opération en cours", "Une seule opération est autorisée à la fois.", h.Window)
		}
		return false
	}
	cloudAuth := h.CloudAuth(repo)
	redact := vaulticRedactCloudSecrets(vaulticRedactOperation(password), cloudAuth)
	title := vaulticBackupTitle(trigger)
	h.Runner.SetCancel(c.Coord.Cancel)
	h.Panel.Reserve()
	h.SetBusy(true)
	h.Panel.Progress.SetValue(0)
	h.Panel.Status.SetText(title + "…")
	h.Log("Début : " + title + " — profil « " + profileName + " »")
	gopts := global.Options{
		Repo: vaulticResolvedRepository(repo), Password: password, Backends: vaulticBackendsForSession(cloudAuth),
		JSON: true, Verbosity: 1,
	}
	exec := vaulticBackupExecutor(gopts, append([]string(nil), profile.Sources...), append([]string(nil), profile.Excludes...),
		func(report *vaulticBackupReport, line string) {
			h.Runner.Handle(context.Background(), title, line, report, redact)
		})
	job := vaulticJob{Profile: profileName, Repository: repo, Trigger: trigger, Slot: slot, CatchUp: catchUp, Redact: redact}
	go func() {
		record := c.Coord.RunAcquired(job, exec)
		fyne.Do(func() {
			h.Runner.SetCancel(nil)
			c.lastReport = c.Coord.LastReport()
			h.Panel.Status.SetText(title + " · " + vaulticStatusLabel(record.Status))
			h.Log(title + " · " + c.lastReport)
			if record.Status == vaulticStatusSuccess {
				h.Completed(title)
			}
			h.SetBusy(false)
			if h.Runner.Closing() {
				h.CloseNow()
				return
			}
			if manual {
				c.ShowReport()
			}
			h.RefreshAutomation()
		})
	}()
	return true
}
