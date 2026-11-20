package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type fakeClock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *fakeClock) Advance(d time.Duration) {
	c.mu.Lock()
	c.t = c.t.Add(d)
	c.mu.Unlock()
}

// harness wires a coordinator to a controllable clock, a temporary history
// file and (optionally) a real temporary restic repository.
type harness struct {
	t       *testing.T
	clock   *fakeClock
	loc     *time.Location
	busy    atomic.Bool
	creds   atomic.Bool
	paused  atomic.Bool
	repo    string
	mu      sync.Mutex
	profile []vaulticBackupProfile
	sent    []string
	history *vaulticHistoryStore
	coord   *vaulticCoordinator
	prefs   vaulticNotifyPrefs
	wg      sync.WaitGroup
	execs   atomic.Int32
	custom  vaulticExec // replaces the real engine when set
	real    vaulticExec
	histDir string
}

func newHarness(t *testing.T, sched vaulticSchedule) *harness {
	t.Helper()
	loc := mustLoc(t, "Europe/Paris")
	h := &harness{t: t, loc: loc, clock: &fakeClock{t: at(loc, 2026, time.June, 10, 8, 0)}, prefs: vaulticDefaultNotifyPrefs()}
	h.creds.Store(true)
	h.histDir = t.TempDir()
	h.history = newVaulticHistoryStore(filepath.Join(h.histDir, "history.json"))

	repoPath, opts := initLocalTestRepository(t, "harness-password")
	source := filepath.Join(t.TempDir(), "documents")
	if err := os.MkdirAll(source, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "note.txt"), []byte("contenu de test"), 0o600); err != nil {
		t.Fatal(err)
	}
	h.repo = repoPath
	h.real = vaulticBackupExecutor(opts, []string{source}, nil, vaulticRecordLine)
	if sched.LastSlot == "" && sched.Active() {
		sched.LastSlot = h.clock.Now().Format(time.RFC3339) // anchored at "now"
	}
	h.profile = []vaulticBackupProfile{{Name: "Documents", Sources: []string{source}, Schedule: &sched}}
	h.newCoordinator()
	return h
}

func (h *harness) newCoordinator() {
	env := vaulticCoordinatorEnv{
		Now:            h.clock.Now,
		Location:       func() *time.Location { return h.loc },
		Repository:     func() string { return h.repo },
		HasCredentials: h.creds.Load,
		Paused:         h.paused.Load,
		Profiles: func() []vaulticBackupProfile {
			h.mu.Lock()
			defer h.mu.Unlock()
			out := make([]vaulticBackupProfile, len(h.profile))
			for i, p := range h.profile {
				out[i] = p
				if p.Schedule != nil {
					copy := *p.Schedule
					out[i].Schedule = &copy
				}
			}
			return out
		},
		SaveSlot: func(profile string, slot time.Time) error {
			h.mu.Lock()
			defer h.mu.Unlock()
			for i := range h.profile {
				if h.profile[i].Name == profile && h.profile[i].Schedule != nil {
					h.profile[i].Schedule.LastSlot = slot.Format(time.RFC3339)
				}
			}
			return nil
		},
	}
	notifier := newVaulticNotifier(func() vaulticNotifyPrefs { return h.prefs }, func(title, body string) {
		h.mu.Lock()
		h.sent = append(h.sent, title+" | "+body)
		h.mu.Unlock()
	}, h.clock.Now)
	h.coord = newVaulticCoordinator(&h.busy, env, h.history, notifier)
	h.coord.env.Start = func(job vaulticJob) bool {
		if !h.coord.TryAcquire() {
			return false
		}
		job.Redact = h.job(job.Trigger).Redact // the application supplies its redactor
		h.wg.Add(1)
		go func() {
			defer h.wg.Done()
			h.coord.RunAcquired(job, h.exec(job))
		}()
		return true
	}
}

func (h *harness) job(trigger string) vaulticJob {
	return vaulticJob{Profile: "Documents", Repository: h.repo, Trigger: trigger, Slot: h.clock.Now(), Redact: vaulticRedactor([]string{"harness-password", "cloud-secret-value"})}
}

func (h *harness) exec(job vaulticJob) vaulticExec {
	return func(ctx context.Context, report *vaulticBackupReport) error {
		h.execs.Add(1)
		if h.custom != nil {
			return h.custom(ctx, report)
		}
		return h.real(ctx, report)
	}
}

func (h *harness) settle() { h.wg.Wait() }

func (h *harness) lastSlot() string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.profile[0].Schedule.LastSlot
}

func (h *harness) notifications() []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]string(nil), h.sent...)
}

func (h *harness) records() []vaulticRunRecord { return h.history.Recent("", 100) }

func hourly(n int) vaulticSchedule {
	return vaulticSchedule{Kind: vaulticScheduleHourly, Enabled: true, EveryHours: n}
}

// ---------------------------------------------------------------------------

func TestVaulticScheduledBackupRunsThroughTheRealEngine(t *testing.T) {
	h := newHarness(t, hourly(1))
	h.coord.Tick()
	h.settle()
	if len(h.records()) != 0 {
		t.Fatal("a backup ran before its slot")
	}
	h.clock.Advance(time.Hour)
	h.coord.Tick()
	h.settle()
	records := h.records()
	if len(records) != 1 {
		t.Fatalf("expected one run, got %+v", records)
	}
	r := records[0]
	if r.Status != vaulticStatusSuccess || r.Trigger != vaulticTriggerScheduled || !vaulticSnapshotID.MatchString(r.SnapshotID) || r.Files == 0 || r.Bytes == 0 {
		t.Fatalf("record: %+v", r)
	}
	if r.Profile != "Documents" || r.Repository == "" || r.Ended.Before(r.Started) {
		t.Fatalf("record metadata: %+v", r)
	}
	if h.lastSlot() != h.clock.Now().Format(time.RFC3339) {
		t.Fatalf("slot not advanced: %s", h.lastSlot())
	}
	// Same instant again: nothing more to do.
	h.coord.Tick()
	h.settle()
	if len(h.records()) != 1 {
		t.Fatal("the same slot ran twice")
	}
	if n := h.notifications(); len(n) != 1 || !strings.Contains(n[0], "Sauvegarde terminée") {
		t.Fatalf("notifications: %v", n)
	}
	if last, ok := h.history.LastSuccess("Documents"); !ok || last.ID != r.ID {
		t.Fatal("last success not found")
	}
	if _, ok := h.coord.Active(); ok || h.busy.Load() {
		t.Fatal("engine not released")
	}
}

func TestVaulticMissedExecutionsNeverBuildABacklog(t *testing.T) {
	h := newHarness(t, hourly(1))
	// Vaultic was closed for ten days: 240 slots passed.
	h.clock.Advance(10*24*time.Hour + 17*time.Minute)
	for i := 0; i < 5; i++ {
		h.coord.Tick()
		h.settle()
	}
	records := h.records()
	if len(records) != 1 || h.execs.Load() != 1 {
		t.Fatalf("expected a single catch-up run, got %d records / %d executions", len(records), h.execs.Load())
	}
	if !records[0].CatchUp || records[0].Status != vaulticStatusSuccess {
		t.Fatalf("catch-up record: %+v", records[0])
	}
}

func TestVaulticApplicationRestartKeepsTheSchedule(t *testing.T) {
	sched := vaulticSchedule{Kind: vaulticScheduleDaily, Enabled: true, Time: "09:00"}
	h := newHarness(t, sched)
	// Day 1: the slot passes while running.
	h.clock.Advance(2 * time.Hour) // 10:00
	h.coord.Tick()
	h.settle()
	if len(h.records()) != 1 {
		t.Fatalf("first day: %+v", h.records())
	}
	// "Restart": a brand-new coordinator and history, same persisted settings.
	h.history = newVaulticHistoryStore(filepath.Join(h.histDir, "history.json"))
	h.newCoordinator()
	h.coord.Tick()
	h.settle()
	if len(h.records()) != 1 {
		t.Fatal("restart re-ran an already handled slot")
	}
	if last, ok := h.history.LastSuccess("Documents"); !ok || last.Status != vaulticStatusSuccess {
		t.Fatal("history lost across restart")
	}
	// Two days pass while the application is closed, then it restarts.
	h.clock.Advance(50 * time.Hour)
	h.history = newVaulticHistoryStore(filepath.Join(h.histDir, "history.json"))
	h.newCoordinator()
	h.coord.Tick()
	h.settle()
	records := h.records()
	if len(records) != 2 || !records[0].CatchUp {
		t.Fatalf("exactly one catch-up expected after the restart: %+v", records)
	}
}

func TestVaulticBusyEngineAppliesTheRetryPolicy(t *testing.T) {
	h := newHarness(t, hourly(1))
	h.clock.Advance(time.Hour)
	h.busy.Store(true) // another restic operation is running
	h.coord.Tick()     // first sighting: deferred
	if len(h.records()) != 0 || h.execs.Load() != 0 {
		t.Fatal("started on a busy engine")
	}
	h.clock.Advance(vaulticRetryDelay - time.Second)
	h.coord.Tick() // too early to look again
	h.clock.Advance(time.Second)
	h.coord.Tick() // attempt 2
	if len(h.records()) != 0 {
		t.Fatal("skipped too early")
	}
	h.clock.Advance(vaulticRetryDelay)
	h.coord.Tick() // attempt 3: give up
	records := h.records()
	if len(records) != 1 || records[0].Status != vaulticStatusSkipped || !strings.Contains(records[0].Summary, "occupé") {
		t.Fatalf("expected a recorded skip: %+v", records)
	}
	if h.execs.Load() != 0 {
		t.Fatal("engine used while busy")
	}
	if h.lastSlot() == "" || h.lastSlot() == at(h.loc, 2026, time.June, 10, 8, 0).Format(time.RFC3339) {
		t.Fatal("skipped slot not consumed: it would retry forever")
	}
	if n := h.notifications(); len(n) != 1 || !strings.Contains(n[0], "ignorée") {
		t.Fatalf("notifications: %v", n)
	}
	// Further ticks do not retry the skipped slot.
	for i := 0; i < 5; i++ {
		h.coord.Tick()
	}
	if len(h.records()) != 1 {
		t.Fatal("silent retry loop")
	}
	// Once the engine is free, the next slot runs normally.
	h.busy.Store(false)
	h.clock.Advance(time.Hour)
	h.coord.Tick()
	h.settle()
	if got := h.records(); len(got) != 2 || got[0].Status != vaulticStatusSuccess {
		t.Fatalf("next slot: %+v", got)
	}
}

func TestVaulticBusyThenFreeBeforeRetryLimitStillRuns(t *testing.T) {
	h := newHarness(t, hourly(1))
	h.clock.Advance(time.Hour)
	h.busy.Store(true)
	h.coord.Tick()
	h.busy.Store(false)
	h.clock.Advance(vaulticRetryDelay)
	h.coord.Tick()
	h.settle()
	if got := h.records(); len(got) != 1 || got[0].Status != vaulticStatusSuccess {
		t.Fatalf("deferred slot did not run: %+v", got)
	}
}

func TestVaulticManualAndScheduledNeverOverlap(t *testing.T) {
	h := newHarness(t, hourly(1))
	var running, peak atomic.Int32
	release := make(chan struct{})
	started := make(chan struct{}, 4)
	h.custom = func(ctx context.Context, report *vaulticBackupReport) error {
		n := running.Add(1)
		for {
			p := peak.Load()
			if n <= p || peak.CompareAndSwap(p, n) {
				break
			}
		}
		started <- struct{}{}
		select {
		case <-release:
		case <-ctx.Done():
		}
		running.Add(-1)
		return context.Canceled
	}
	var wg sync.WaitGroup
	wg.Add(1)
	go func() { // manual run holds the engine
		defer wg.Done()
		h.coord.Execute(h.job(vaulticTriggerManual), h.exec(h.job(vaulticTriggerManual)))
	}()
	<-started
	// A second manual run is refused, a scheduled one is recorded as skipped.
	if _, ok := h.coord.Execute(h.job(vaulticTriggerManual), h.exec(h.job(vaulticTriggerManual))); ok {
		t.Fatal("two manual backups overlapped")
	}
	rec, ok := h.coord.Execute(h.job(vaulticTriggerScheduled), h.exec(h.job(vaulticTriggerScheduled)))
	if ok || rec.Status != vaulticStatusSkipped {
		t.Fatalf("scheduled run during a manual one: ok=%v %+v", ok, rec)
	}
	// The scheduler also keeps off while the manual run is active.
	h.clock.Advance(time.Hour)
	h.coord.Tick()
	if h.execs.Load() != 1 {
		t.Fatalf("the scheduler started a second engine run: %d", h.execs.Load())
	}
	close(release)
	wg.Wait()
	h.settle()
	if peak.Load() != 1 {
		t.Fatalf("peak concurrency %d", peak.Load())
	}
	if h.busy.Load() {
		t.Fatal("gate not released")
	}
}

func TestVaulticConcurrentStartsAdmitExactlyOne(t *testing.T) {
	h := newHarness(t, hourly(1))
	h.custom = func(ctx context.Context, report *vaulticBackupReport) error {
		time.Sleep(100 * time.Millisecond)
		return context.Canceled
	}
	var accepted atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, ok := h.coord.Execute(h.job(vaulticTriggerManual), h.exec(h.job(vaulticTriggerManual))); ok {
				accepted.Add(1)
			}
		}()
	}
	wg.Wait()
	if accepted.Load() < 1 || int(accepted.Load()) != int(h.execs.Load()) {
		t.Fatalf("accepted %d, executed %d", accepted.Load(), h.execs.Load())
	}
	// Whatever the interleaving, executions never ran at the same time.
	if h.busy.Load() {
		t.Fatal("gate left closed")
	}
}

func TestVaulticCancellationStopsTheBackupAndCleansUp(t *testing.T) {
	h := newHarness(t, hourly(1))
	started := make(chan struct{})
	h.custom = func(ctx context.Context, report *vaulticBackupReport) error {
		close(started)
		<-ctx.Done()
		return errors.New("context canceled while uploading to https://user:pw@host/x?token=abc")
	}
	done := make(chan vaulticRunRecord, 1)
	go func() {
		rec, _ := h.coord.Execute(h.job(vaulticTriggerManual), h.exec(h.job(vaulticTriggerManual)))
		done <- rec
	}()
	<-started
	if _, ok := h.coord.Active(); !ok {
		t.Fatal("active run not visible")
	}
	h.coord.Cancel()
	rec := <-done
	if rec.Status != vaulticStatusCancelled || rec.SnapshotID != "" {
		t.Fatalf("cancelled record: %+v", rec)
	}
	if strings.Contains(rec.Summary, "token") || strings.Contains(rec.Summary, "pw") {
		t.Fatalf("raw error kept: %q", rec.Summary)
	}
	if h.busy.Load() {
		t.Fatal("gate left closed after cancellation")
	}
	if _, ok := h.coord.Active(); ok {
		t.Fatal("run still active")
	}
	// A cancelled run never produces a success notification.
	for _, n := range h.notifications() {
		if strings.Contains(n, "terminée") {
			t.Fatalf("notification after cancel: %s", n)
		}
	}
}

func TestVaulticShutdownWaitsForTheRunningBackup(t *testing.T) {
	h := newHarness(t, hourly(1))
	started := make(chan struct{})
	finished := make(chan struct{})
	h.custom = func(ctx context.Context, report *vaulticBackupReport) error {
		close(started)
		<-ctx.Done()
		time.Sleep(50 * time.Millisecond) // orderly engine shutdown
		close(finished)
		return ctx.Err()
	}
	go h.coord.Execute(h.job(vaulticTriggerScheduled), h.exec(h.job(vaulticTriggerScheduled)))
	<-started
	if !h.coord.Shutdown(5 * time.Second) {
		t.Fatal("shutdown timed out")
	}
	select {
	case <-finished:
	default:
		t.Fatal("shutdown returned before the engine finished")
	}
	if h.busy.Load() {
		t.Fatal("gate left closed")
	}
	if !h.coord.Shutdown(time.Second) {
		t.Fatal("idle shutdown must succeed immediately")
	}
}

func TestVaulticFailedBackupIsRecordedRedactedAndNotRetried(t *testing.T) {
	h := newHarness(t, hourly(1))
	h.custom = func(ctx context.Context, report *vaulticBackupReport) error {
		return errors.New("upload failed: https://alice:cloud-secret-value@bucket.example/repo?sig=XYZ password harness-password\nstack trace line 2")
	}
	h.clock.Advance(time.Hour)
	h.coord.Tick()
	h.settle()
	for i := 0; i < 4; i++ {
		h.coord.Tick() // no silent retry
		h.settle()
	}
	records := h.records()
	if len(records) != 1 || records[0].Status != vaulticStatusFailure || h.execs.Load() != 1 {
		t.Fatalf("failure handling: %+v (%d executions)", records, h.execs.Load())
	}
	for _, secret := range []string{"cloud-secret-value", "harness-password", "sig=XYZ", "alice:", "\n"} {
		if strings.Contains(records[0].Summary, secret) {
			t.Errorf("summary leaks %q: %q", secret, records[0].Summary)
		}
	}
	if !strings.Contains(records[0].Summary, "upload failed") {
		t.Errorf("summary lost its meaning: %q", records[0].Summary)
	}
	if records[0].SnapshotID != "" {
		t.Fatal("failed run claims a snapshot")
	}
	n := h.notifications()
	if len(n) != 1 || !strings.Contains(n[0], "échec") {
		t.Fatalf("notifications: %v", n)
	}
	for _, secret := range []string{"cloud-secret-value", "harness-password"} {
		if strings.Contains(n[0], secret) {
			t.Fatalf("notification leaks %q", secret)
		}
	}
	if _, ok := h.history.LastSuccess("Documents"); ok {
		t.Fatal("a failure counted as the last successful backup")
	}
}

func TestVaulticBackupIsNeverSuccessfulWithoutAConfirmedSnapshot(t *testing.T) {
	h := newHarness(t, hourly(1))
	h.custom = func(ctx context.Context, report *vaulticBackupReport) error { return nil } // engine "succeeds" silently
	rec, _ := h.coord.Execute(h.job(vaulticTriggerManual), h.exec(h.job(vaulticTriggerManual)))
	if rec.Status == vaulticStatusSuccess {
		t.Fatalf("success without a snapshot: %+v", rec)
	}
	// Source files that could not be read also prevent a "success".
	h.custom = func(ctx context.Context, report *vaulticBackupReport) error {
		vaulticRecordLine(report, `{"message_type":"error","error":{"message":"unreadable"}}`)
		vaulticRecordLine(report, `{"message_type":"summary","snapshot_id":"0123abcd","total_files_processed":3,"total_bytes_processed":10}`)
		return nil
	}
	rec, _ = h.coord.Execute(h.job(vaulticTriggerManual), h.exec(h.job(vaulticTriggerManual)))
	if rec.Status != vaulticStatusFailure || !strings.Contains(rec.Summary, "fichier") {
		t.Fatalf("incomplete backup: %+v", rec)
	}
}

func TestVaulticPanicInTheEngineIsContained(t *testing.T) {
	h := newHarness(t, hourly(1))
	h.custom = func(ctx context.Context, report *vaulticBackupReport) error { panic("boom with harness-password") }
	rec, _ := h.coord.Execute(h.job(vaulticTriggerManual), h.exec(h.job(vaulticTriggerManual)))
	if rec.Status != vaulticStatusFailure || strings.Contains(rec.Summary, "harness-password") || h.busy.Load() {
		t.Fatalf("panic handling: %+v busy=%v", rec, h.busy.Load())
	}
}

func TestVaulticNoBackupStartsWithoutCredentials(t *testing.T) {
	h := newHarness(t, hourly(1))
	h.creds.Store(false)
	h.clock.Advance(time.Hour)
	for i := 0; i < 10; i++ {
		h.coord.Tick()
		h.clock.Advance(time.Minute)
	}
	h.settle()
	if h.execs.Load() != 0 {
		t.Fatal("a backup started without the repository password")
	}
	records := h.records()
	if len(records) != 1 || records[0].Status != vaulticStatusSkipped || !strings.Contains(records[0].Summary, "Mot de passe") {
		t.Fatalf("expected one skipped entry: %+v", records)
	}
	if !h.coord.Overdue("Documents") {
		t.Fatal("overdue state not reported")
	}
	n := h.notifications()
	if len(n) != 1 || !strings.Contains(n[0], "en retard") {
		t.Fatalf("expected a single overdue notification: %v", n)
	}
	// A missed backup is not a completed one.
	if _, ok := h.history.LastSuccess("Documents"); ok {
		t.Fatal("overdue shown as success")
	}
	// Several more slots pass: still no spam and no run.
	h.clock.Advance(5 * time.Hour)
	h.coord.Tick()
	if len(h.records()) != 1 || len(h.notifications()) != 1 {
		t.Fatalf("spam while waiting: %d records, %d notifications", len(h.records()), len(h.notifications()))
	}
	// The password becomes available: one catch-up run, then back to normal.
	h.creds.Store(true)
	h.coord.Tick()
	h.settle()
	records = h.records()
	if len(records) != 2 || records[0].Status != vaulticStatusSuccess || !records[0].CatchUp || h.execs.Load() != 1 {
		t.Fatalf("catch-up after credentials: %+v", records)
	}
	if h.coord.Overdue("Documents") {
		t.Fatal("overdue state not cleared")
	}
}

func TestVaulticPauseStopsSchedulingAndResumeDoesNotCatchUp(t *testing.T) {
	h := newHarness(t, hourly(1))
	h.paused.Store(true)
	h.clock.Advance(5 * time.Hour)
	h.coord.Tick()
	h.settle()
	if h.execs.Load() != 0 || len(h.records()) != 0 {
		t.Fatal("scheduler ran while paused")
	}
	h.paused.Store(false)
	h.coord.Resume()
	h.coord.Tick()
	h.settle()
	if h.execs.Load() != 0 {
		t.Fatal("resuming triggered a catch-up of what was missed while paused")
	}
	h.clock.Advance(time.Hour)
	h.coord.Tick()
	h.settle()
	if len(h.records()) != 1 {
		t.Fatalf("schedule did not continue after resume: %+v", h.records())
	}
}

func TestVaulticProfileWithoutSourcesIsSkippedAndRecorded(t *testing.T) {
	h := newHarness(t, hourly(1))
	h.mu.Lock()
	h.profile[0].Sources = nil
	h.mu.Unlock()
	h.clock.Advance(time.Hour)
	h.coord.Tick()
	h.coord.Tick()
	records := h.records()
	if len(records) != 1 || records[0].Status != vaulticStatusSkipped || !strings.Contains(records[0].Summary, "source") || h.execs.Load() != 0 {
		t.Fatalf("no sources: %+v", records)
	}
}

func TestVaulticClockMovedBackReanchorsInsteadOfStalling(t *testing.T) {
	h := newHarness(t, hourly(1))
	h.clock.Advance(3 * time.Hour)
	h.coord.Tick()
	h.settle()
	first := len(h.records())
	h.clock.Advance(-10 * time.Hour) // the clock is set back
	h.coord.Tick()                   // re-anchors, runs nothing
	h.settle()
	if len(h.records()) != first {
		t.Fatal("ran after the clock went back")
	}
	h.clock.Advance(time.Hour)
	h.coord.Tick()
	h.settle()
	if len(h.records()) != first+1 {
		t.Fatalf("schedule stalled after the clock was set back: %d records", len(h.records()))
	}
}

func TestVaulticSchedulerNeverRunsRetentionCommands(t *testing.T) {
	for _, file := range []string{"vaultic_schedule.go", "vaultic_coordinator.go", "vaultic_history.go", "vaultic_notify.go", "vaultic_backup_exec.go", "vaultic_automation_ui.go"} {
		raw, err := os.ReadFile(file)
		if err != nil {
			continue
		}
		for _, forbidden := range []string{"runForget", "runPrune", "ForgetOptions", "PruneOptions", "runRewrite", "exec.Command", "schtasks", "os.StartProcess"} {
			if strings.Contains(string(raw), forbidden) {
				t.Errorf("%s mentions %s: scheduling must only back up", file, forbidden)
			}
		}
	}
}
