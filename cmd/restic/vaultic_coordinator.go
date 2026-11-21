package main

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/restic/restic/internal/repository"
)

const (
	vaulticRetryDelay = 5 * time.Minute // wait before looking at a busy engine again
	vaulticRetryMax   = 3               // attempts per slot, then the slot is skipped and recorded
)

// vaulticJob describes one backup run, manual or scheduled.
type vaulticJob struct {
	Profile    string
	Repository string
	Trigger    string
	Slot       time.Time // scheduled runs: the slot being served
	CatchUp    bool
	// Redact masks the restic password and session credentials in any text
	// that is stored or shown. It must be set by the caller.
	Redact func(string) string
}

// vaulticExec performs the backup through the embedded restic engine. It must
// feed report with the engine's JSON events and honour ctx.
type vaulticExec func(ctx context.Context, report *vaulticBackupReport) error

type vaulticActiveRun struct {
	Job     vaulticJob
	Started time.Time
}

// vaulticCoordinatorEnv connects the coordinator to the rest of the program;
// every dependency is a function so tests can control time and the engine.
type vaulticCoordinatorEnv struct {
	Now            func() time.Time
	Location       func() *time.Location
	Profiles       func() []vaulticBackupProfile
	Repository     func() string
	HasCredentials func() bool
	Paused         func() bool
	// SaveSlot persists the latest handled slot of a profile's schedule.
	SaveSlot func(profile string, slot time.Time) error
	// SaveSlotWorker is used by the backup worker. GUI callers can marshal to
	// the UI thread and wait for durable persistence without deadlocking Tick.
	// If omitted, SaveSlot is used (for headless tests).
	SaveSlotWorker func(profile string, slot time.Time) error
	// Start launches a run asynchronously through the single engine and
	// reports whether it was accepted.
	Start func(job vaulticJob) bool
}

type vaulticSlotState struct {
	Slot     time.Time
	Attempts int
	RetryAt  time.Time
	Handled  time.Time // latest slot already consumed in this session, even if persisting it failed
	Overdue  bool      // a skipped/overdue entry was already recorded for this waiting period
}

// vaulticCoordinator is the only place where backups begin and end. Manual and
// scheduled runs go through the same gate (one restic operation at a time),
// the same execution path, the same history and the same notifications.
type vaulticCoordinator struct {
	busy     *atomic.Bool // shared with every other engine operation
	env      vaulticCoordinatorEnv
	history  *vaulticHistoryStore
	notifier *vaulticNotifier

	mu     sync.Mutex
	active *vaulticActiveRun
	cancel context.CancelFunc
	done   chan struct{}
	slots  map[string]*vaulticSlotState
	// lastReport is the (redacted) text report of the latest run.
	lastReport string
	seq        atomic.Uint64
	// OnChange is called (from any goroutine) after a run starts or ends.
	OnChange func()
	// OnPersistenceError receives errors that should be surfaced in the GUI.
	OnPersistenceError func(error)
	persistProblem     string
}

func newVaulticCoordinator(busy *atomic.Bool, env vaulticCoordinatorEnv, history *vaulticHistoryStore, notifier *vaulticNotifier) *vaulticCoordinator {
	return &vaulticCoordinator{busy: busy, env: env, history: history, notifier: notifier, slots: map[string]*vaulticSlotState{}}
}

// TryAcquire takes the engine gate. Whoever acquires it must call RunAcquired
// (or release the gate on every path).
func (c *vaulticCoordinator) TryAcquire() bool { return c.busy.CompareAndSwap(false, true) }

// Active describes the run in progress, if any.
func (c *vaulticCoordinator) Active() (vaulticActiveRun, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.active == nil {
		return vaulticActiveRun{}, false
	}
	return *c.active, true
}

// LastReport returns the text report of the latest finished run.
func (c *vaulticCoordinator) LastReport() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.lastReport
}

// Cancel asks the running backup to stop.
func (c *vaulticCoordinator) Cancel() {
	c.mu.Lock()
	cancel := c.cancel
	c.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}

// Shutdown cancels the running backup and waits for it to finish cleanly. It
// reports whether the engine is idle afterwards.
func (c *vaulticCoordinator) Shutdown(timeout time.Duration) bool {
	c.mu.Lock()
	cancel, done := c.cancel, c.done
	c.mu.Unlock()
	if cancel == nil {
		return true
	}
	cancel()
	select {
	case <-done:
		return true
	case <-time.After(timeout):
		return false
	}
}

func (c *vaulticCoordinator) newID(at time.Time) string {
	return fmt.Sprintf("%d-%d", at.UnixMilli(), c.seq.Add(1))
}

func (c *vaulticCoordinator) changed() {
	if c.OnChange != nil {
		c.OnChange()
	}
}

func (c *vaulticCoordinator) persistenceError(err error) {
	if err == nil {
		return
	}
	c.mu.Lock()
	c.persistProblem = err.Error()
	c.mu.Unlock()
	if c.OnPersistenceError != nil {
		c.OnPersistenceError(err)
	}
	c.changed()
}

// PersistenceProblem returns the last unresolved failure to save the schedule
// or the history ("" when everything was saved). It stays until a later run
// is saved completely, so it cannot be hidden by unrelated status messages.
func (c *vaulticCoordinator) PersistenceProblem() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.persistProblem
}

// Execute acquires the gate and runs the job to completion. A scheduled job
// that finds the engine busy is recorded as skipped; a manual one is simply
// refused (the caller tells the user).
func (c *vaulticCoordinator) Execute(job vaulticJob, exec vaulticExec) (vaulticRunRecord, bool) {
	if !c.TryAcquire() {
		if job.Trigger == vaulticTriggerScheduled {
			return c.recordSkipped(job, "Le moteur de sauvegarde était occupé."), false
		}
		return vaulticRunRecord{}, false
	}
	return c.RunAcquired(job, exec), true
}

// RunAcquired runs a job while holding the gate and always releases it.
// Cleanup happens on every path, including a panic inside the engine.
func (c *vaulticCoordinator) RunAcquired(job vaulticJob, exec vaulticExec) (record vaulticRunRecord) {
	started := c.env.Now()
	if job.Redact == nil {
		job.Redact = vaulticRedactor(nil) // at least strips URL credentials and queries
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	c.mu.Lock()
	c.active, c.cancel, c.done = &vaulticActiveRun{Job: job, Started: started}, cancel, done
	c.mu.Unlock()
	c.changed()

	report := &vaulticBackupReport{started: started, profile: job.Profile}
	var err error
	func() {
		defer func() {
			if r := recover(); r != nil {
				err = errors.New("Erreur interne pendant la sauvegarde.")
			}
		}()
		err = exec(ctx, report)
	}()
	cancelled := ctx.Err() != nil
	if cancelled && err != nil && !errors.Is(err, ErrInvalidSourceData) {
		err = context.Canceled
	}
	ended := c.env.Now()
	persistFailed := false
	_, details, resultErr := report.result(err)
	record = c.buildRecord(job, report, resultErr, started, ended)
	c.mu.Lock()
	c.lastReport = job.Redact(details)
	c.mu.Unlock()

	cancel()
	c.mu.Lock()
	c.active, c.cancel, c.done = nil, nil, nil
	c.mu.Unlock()
	// Persist before releasing the gate, so the next run sees this one.
	if job.Trigger == vaulticTriggerScheduled {
		saveSlot := c.env.SaveSlotWorker
		if saveSlot == nil {
			saveSlot = c.env.SaveSlot
		}
		if saveSlot != nil {
			if e := saveSlot(job.Profile, job.Slot); e != nil {
				persistFailed = true
				c.persistenceError(fmt.Errorf("créneau de sauvegarde non enregistré : %w", e))
			}
		}
	}
	if e := c.history.Append(record); e != nil {
		persistFailed = true
		c.persistenceError(fmt.Errorf("historique non enregistré : %w", e))
	}
	c.notifyRecord(record)
	c.mu.Lock()
	if !persistFailed {
		c.persistProblem = ""
	}
	if job.Trigger == vaulticTriggerScheduled {
		st := c.slotState(job.Profile)
		if job.Slot.After(st.Handled) {
			st.Handled = job.Slot
		}
	}
	if st := c.slots[job.Profile]; st != nil {
		st.Overdue = false
	}
	c.mu.Unlock()
	c.busy.Store(false)
	close(done)
	c.changed()
	return record
}

func (c *vaulticCoordinator) buildRecord(job vaulticJob, report *vaulticBackupReport, err error, started, ended time.Time) vaulticRunRecord {
	report.mu.Lock()
	summary := report.summary
	status := report.status
	report.mu.Unlock()
	record := vaulticRunRecord{
		ID: c.newID(started), Profile: job.Profile, Repository: vaulticSafeLocation(job.Repository),
		Trigger: job.Trigger, CatchUp: job.CatchUp, Started: started, Ended: ended,
		DurationMs: ended.Sub(started).Milliseconds(),
	}
	if record.DurationMs < 0 {
		record.DurationMs = 0
	}
	if summary.MessageType == "summary" {
		record.Files, record.Bytes = summary.FilesProcessed, summary.BytesProcessed
	} else if status.MessageType == "status" {
		record.Files, record.Bytes = status.FilesDone, status.BytesDone
	}
	switch {
	case err == nil:
		// result() only returns nil when restic confirmed a snapshot.
		record.Status, record.SnapshotID = vaulticStatusSuccess, summary.SnapshotID
	case errors.Is(err, context.Canceled):
		record.Status, record.Summary = vaulticStatusCancelled, "Interrompue avant la fin ; aucun instantané confirmé."
	default:
		record.Status = vaulticStatusFailure
		if vaulticSnapshotID.MatchString(summary.SnapshotID) {
			record.SnapshotID = summary.SnapshotID // incomplete: a snapshot exists but some files failed
		}
		record.Summary = c.failureSummary(job, err)
	}
	return record
}

func (c *vaulticCoordinator) failureSummary(job vaulticJob, err error) string {
	switch {
	case errors.Is(err, repository.ErrNoKeyFound):
		return "Mot de passe du dépôt incorrect ou clé introuvable."
	case errors.Is(err, context.DeadlineExceeded):
		return "Délai dépassé."
	}
	return vaulticSanitizeSummary(err.Error(), job.Redact)
}

func (c *vaulticCoordinator) recordSkipped(job vaulticJob, reason string) vaulticRunRecord {
	now := c.env.Now()
	record := vaulticRunRecord{
		ID: c.newID(now), Profile: job.Profile, Repository: vaulticSafeLocation(job.Repository),
		Trigger: job.Trigger, CatchUp: job.CatchUp, Started: now, Ended: now,
		Status: vaulticStatusSkipped, Summary: reason,
	}
	if e := c.history.Append(record); e != nil {
		c.persistenceError(fmt.Errorf("historique non enregistré : %w", e))
	}
	c.notifyRecord(record)
	c.changed()
	return record
}

func (c *vaulticCoordinator) notifyRecord(r vaulticRunRecord) {
	switch r.Status {
	case vaulticStatusSuccess:
		c.notifier.Notify(vaulticNotifyCompleted, r.ID, "Sauvegarde terminée",
			fmt.Sprintf("Profil « %s » : instantané %s enregistré.", r.Profile, r.SnapshotID))
	case vaulticStatusFailure:
		c.notifier.Notify(vaulticNotifyFailed, r.ID, "Sauvegarde en échec",
			fmt.Sprintf("Profil « %s » : %s", r.Profile, r.Summary))
	case vaulticStatusSkipped:
		c.notifier.Notify(vaulticNotifySkipped, r.ID, "Sauvegarde ignorée",
			fmt.Sprintf("Profil « %s » : %s", r.Profile, r.Summary))
	}
}

func (c *vaulticCoordinator) slotState(profile string) *vaulticSlotState {
	st := c.slots[profile]
	if st == nil {
		st = &vaulticSlotState{}
		c.slots[profile] = st
	}
	return st
}

// Overdue reports whether a due backup of the profile is waiting (for example
// for the repository password) — it is never shown as completed.
func (c *vaulticCoordinator) Overdue(profile string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	st := c.slots[profile]
	return st != nil && st.Overdue
}

// Resume re-anchors every active schedule to now, so that resuming after a
// pause never triggers a catch-up of what was missed while paused.
func (c *vaulticCoordinator) Resume() {
	now := c.env.Now()
	for _, profile := range c.env.Profiles() {
		if profile.Schedule != nil && profile.Schedule.Active() && c.env.SaveSlot != nil {
			c.persistenceError(c.env.SaveSlot(profile.Name, now))
		}
	}
	c.mu.Lock()
	c.slots = map[string]*vaulticSlotState{}
	c.mu.Unlock()
}

// Tick is the scheduler. The caller invokes it periodically (and after any
// wake-up) from a single goroutine. It starts at most one backup per call and
// never queues a backlog.
func (c *vaulticCoordinator) Tick() {
	if c.env.Paused() {
		return
	}
	now, loc := c.env.Now(), c.env.Location()
	for _, profile := range c.env.Profiles() {
		if profile.Schedule == nil || !profile.Schedule.Active() {
			continue
		}
		c.warnIfStale(profile, now)
		if c.tickProfile(profile, now, loc) {
			return // one engine operation at a time
		}
	}
}

// warnIfStale notifies, at most once a day per profile, that the last
// successful backup is much older than the schedule says. It only warns.
func (c *vaulticCoordinator) warnIfStale(profile vaulticBackupProfile, now time.Time) {
	var last time.Time
	if rec, ok := c.history.LastSuccessForRepository(profile.Name, profile.Schedule.Repository); ok {
		last = rec.Ended
	}
	st := vaulticCheckStale(*profile.Schedule, last, now)
	if !st.Stale {
		return
	}
	c.notifier.Notify(vaulticNotifyOverdue, "stale|"+profile.Name+"|"+now.Format("2006-01-02"),
		"Sauvegarde ancienne", vaulticStaleMessage(profile.Name, st))
}

// tickProfile returns true when it started a run.
func (c *vaulticCoordinator) tickProfile(profile vaulticBackupProfile, now time.Time, loc *time.Location) bool {
	plan := vaulticPlan(*profile.Schedule, now, loc)
	if plan.ClockBack {
		c.mu.Lock()
		c.slotState(profile.Name).Handled = time.Time{}
		c.mu.Unlock()
	}
	if plan.ClockBack && c.env.SaveSlot != nil {
		c.persistenceError(c.env.SaveSlot(profile.Name, now)) // clock moved back
		return false
	}
	c.mu.Lock()
	st := c.slotState(profile.Name)
	if plan.Due && !plan.Slot.After(st.Handled) {
		plan.Due = false // consumed earlier in this session although saving it failed
	}
	if !plan.Due {
		st.Attempts, st.RetryAt = 0, time.Time{}
		c.mu.Unlock()
		return false
	}
	if !st.Slot.Equal(plan.Slot) {
		st.Slot, st.Attempts, st.RetryAt = plan.Slot, 0, time.Time{}
	}
	sameProfileRunning := c.active != nil && c.active.Job.Profile == profile.Name
	c.mu.Unlock()
	if sameProfileRunning {
		return false
	}

	targetRepo := profile.Schedule.Repository
	if targetRepo == "" {
		targetRepo = c.env.Repository()
	} // legacy schedules and test harnesses
	job := vaulticJob{Profile: profile.Name, Repository: targetRepo, Trigger: vaulticTriggerScheduled, Slot: plan.Slot, CatchUp: plan.CatchUp}
	finishSkipped := func(reason string) {
		c.recordSkipped(job, reason)
		c.mu.Lock()
		if plan.Slot.After(st.Handled) {
			st.Handled = plan.Slot
		}
		c.mu.Unlock()
		if c.env.SaveSlot != nil {
			c.persistenceError(c.env.SaveSlot(profile.Name, plan.Slot))
		}
	}
	if len(profile.Sources) == 0 {
		finishSkipped("Aucune source n'est sélectionnée pour ce profil.")
		return false
	}
	if job.Repository == "" || job.Repository != c.env.Repository() || !c.env.HasCredentials() {
		c.mu.Lock()
		first := !st.Overdue
		st.Overdue = true
		c.mu.Unlock()
		if first { // one entry and one notification per waiting period
			reason := "Mot de passe du dépôt non disponible : la sauvegarde planifiée attend."
			if job.Repository == "" {
				reason = "Aucun dépôt de destination : la sauvegarde planifiée attend."
			} else if job.Repository != c.env.Repository() {
				reason = "Activez le dépôt prévu pour ce profil avant la sauvegarde planifiée."
			}
			rec := c.recordSkippedQuiet(job, reason)
			c.notifier.Notify(vaulticNotifyOverdue, profile.Name+"|"+plan.Slot.UTC().Format(time.RFC3339), "Sauvegarde en retard",
				fmt.Sprintf("Profil « %s » : %s", profile.Name, rec.Summary))
		}
		return false
	}
	if c.busy.Load() {
		// Defined retry policy: look again every vaulticRetryDelay, at most
		// vaulticRetryMax times; then the slot is skipped, recorded, notified
		// and the schedule moves on. Nothing is retried silently or forever.
		c.mu.Lock()
		giveUp := false
		switch {
		case st.Attempts == 0:
			st.Attempts, st.RetryAt = 1, now.Add(vaulticRetryDelay)
		case now.Before(st.RetryAt):
		default:
			st.Attempts++
			if st.Attempts >= vaulticRetryMax {
				st.Attempts, giveUp = 0, true
			} else {
				st.RetryAt = now.Add(vaulticRetryDelay)
			}
		}
		c.mu.Unlock()
		if giveUp {
			finishSkipped(fmt.Sprintf("Le moteur de sauvegarde est resté occupé (%d tentatives).", vaulticRetryMax))
		}
		return false
	}
	if !c.env.Start(job) {
		return false // refused (engine just became busy): looked at again next tick
	}
	return true
}

// recordSkippedQuiet records a skipped run without a "skipped" notification
// (the overdue notification covers it).
func (c *vaulticCoordinator) recordSkippedQuiet(job vaulticJob, reason string) vaulticRunRecord {
	now := c.env.Now()
	record := vaulticRunRecord{
		ID: c.newID(now), Profile: job.Profile, Repository: vaulticSafeLocation(job.Repository),
		Trigger: job.Trigger, CatchUp: job.CatchUp, Started: now, Ended: now,
		Status: vaulticStatusSkipped, Summary: reason,
	}
	if e := c.history.Append(record); e != nil {
		c.persistenceError(fmt.Errorf("historique non enregistré : %w", e))
	}
	c.changed()
	return record
}
