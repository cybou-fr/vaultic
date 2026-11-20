package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)

// Two repositories, one profile: a schedule pinned to repository B must never
// write into repository A, even while A is the active repository.
func TestVaulticValidationTwoRepositoriesOneProfileNeverCrossTargets(t *testing.T) {
	h := newHarness(t, hourly(1))
	repoA := h.repo
	repoBPath, optsB := initLocalTestRepository(t, "other-password")
	source := h.profile[0].Sources
	execB := vaulticBackupExecutor(optsB, source, nil, vaulticRecordLine)

	var targets []string
	h.coord.env.Start = func(job vaulticJob) bool {
		if !h.coord.TryAcquire() {
			return false
		}
		job.Redact = h.job(job.Trigger).Redact
		h.mu.Lock()
		targets = append(targets, job.Repository)
		h.mu.Unlock()
		exec := h.real
		if job.Repository == repoBPath {
			exec = execB
		}
		h.wg.Add(1)
		go func() {
			defer h.wg.Done()
			h.coord.RunAcquired(job, func(ctx context.Context, r *vaulticBackupReport) error { h.execs.Add(1); return exec(ctx, r) })
		}()
		return true
	}

	h.profile[0].Schedule.Repository = repoBPath // pinned to B while A is active
	h.clock.Advance(time.Hour)
	for i := 0; i < 5; i++ {
		h.coord.Tick()
		h.settle()
		h.clock.Advance(15 * time.Second)
	}
	if h.execs.Load() != 0 || len(targets) != 0 {
		t.Fatalf("scheduled backup started while the wrong repository was active: %v", targets)
	}
	if !h.coord.Overdue("Documents") {
		t.Fatal("waiting state not reported")
	}

	// The user activates B: the run goes to B only.
	h.repo = repoBPath
	h.coord.Tick()
	h.settle()
	if len(targets) != 1 || targets[0] != repoBPath {
		t.Fatalf("wrong target: %v", targets)
	}
	recs := h.records()
	var success *vaulticRunRecord
	for i := range recs {
		if recs[i].Status == vaulticStatusSuccess {
			success = &recs[i]
		}
	}
	if success == nil || success.Repository != vaulticSafeLocation(repoBPath) {
		t.Fatalf("record not attributed to repository B: %+v", recs)
	}
	if _, ok := h.history.LastSuccessForRepository("Documents", repoA); ok {
		t.Fatal("repository A must not show a success it never received")
	}
	if _, ok := h.history.LastSuccessForRepository("Documents", repoBPath); !ok {
		t.Fatal("repository B success missing")
	}
	if entries, _ := os.ReadDir(filepath.Join(repoA, "snapshots")); len(entries) != 0 {
		t.Fatalf("repository A received %d snapshot(s)", len(entries))
	}
	if entries, _ := os.ReadDir(filepath.Join(repoBPath, "snapshots")); len(entries) != 1 {
		t.Fatalf("repository B should hold exactly one snapshot, has %d", len(entries))
	}
}

// A failing settings write must be reported and must never cause the same slot
// to be launched again on every 15-second tick.
func TestVaulticValidationSlotSaveFailureReportedWithoutExecutionLoop(t *testing.T) {
	h := newHarness(t, hourly(1))
	var reported atomic.Int32
	h.coord.OnPersistenceError = func(error) { reported.Add(1) }
	fail := func(string, time.Time) error { return errors.New("disque plein") }
	h.coord.env.SaveSlot = fail
	h.coord.env.SaveSlotWorker = fail
	h.custom = func(ctx context.Context, r *vaulticBackupReport) error { return h.real(ctx, r) }

	h.clock.Advance(time.Hour)
	for i := 0; i < 40; i++ { // ten minutes of 15 s ticks
		h.coord.Tick()
		h.settle()
		h.clock.Advance(15 * time.Second)
	}
	if got := h.execs.Load(); got != 1 {
		t.Fatalf("slot re-executed %d times after a failed settings write", got)
	}
	if reported.Load() == 0 {
		t.Fatal("persistence failure was not surfaced")
	}
	// The next real slot still runs.
	h.clock.Advance(time.Hour)
	h.coord.Tick()
	h.settle()
	if got := h.execs.Load(); got != 2 {
		t.Fatalf("next slot should run exactly once more, total %d", got)
	}
}

func TestVaulticValidationSkippedSlotWithFailingSaveIsRecordedOnce(t *testing.T) {
	h := newHarness(t, hourly(1))
	h.profile[0].Sources = nil
	fail := func(string, time.Time) error { return errors.New("lecture seule") }
	h.coord.env.SaveSlot = fail
	h.coord.env.SaveSlotWorker = fail
	h.clock.Advance(time.Hour)
	for i := 0; i < 20; i++ {
		h.coord.Tick()
		h.settle()
		h.clock.Advance(15 * time.Second)
	}
	if n := len(h.records()); n != 1 {
		t.Fatalf("skipped slot recorded %d times", n)
	}
}

// Missing cloud credentials: the repository is a cloud location but no secret
// is available, so nothing runs and nothing is reported as a success.
func TestVaulticValidationMissingCloudCredentialsNeverReportSuccess(t *testing.T) {
	h := newHarness(t, hourly(1))
	cloud := "s3:s3.example.invalid/bucket"
	h.repo = cloud
	h.profile[0].Schedule.Repository = cloud
	h.creds.Store(false)
	h.clock.Advance(time.Hour)
	for i := 0; i < 10; i++ {
		h.coord.Tick()
		h.settle()
		h.clock.Advance(15 * time.Second)
	}
	if h.execs.Load() != 0 {
		t.Fatal("a backup started without credentials")
	}
	if !h.coord.Overdue("Documents") {
		t.Fatal("waiting state must be visible")
	}
	for _, r := range h.records() {
		if r.Status == vaulticStatusSuccess {
			t.Fatalf("false success: %+v", r)
		}
	}
	if _, ok := h.history.LastSuccessForRepository("Documents", cloud); ok {
		t.Fatal("cloud repository shown as protected")
	}
	// An unreachable cloud repository with credentials fails visibly.
	h.creds.Store(true)
	h.custom = func(ctx context.Context, r *vaulticBackupReport) error { return errors.New("dial tcp: no such host") }
	h.coord.Tick()
	h.settle()
	last := h.records()[0]
	if last.Status != vaulticStatusFailure {
		t.Fatalf("unreachable cloud must fail, got %+v", last)
	}
}
