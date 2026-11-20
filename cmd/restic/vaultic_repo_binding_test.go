package main

import (
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestVaulticHistoryNeverAttributesAnotherRepositorySuccess(t *testing.T) {
	h := newVaulticHistoryStore(filepath.Join(t.TempDir(), "history.json"))
	for _, r := range []vaulticRunRecord{
		{ID: "1", Profile: "Documents", Repository: "local:/external/drive-a", Status: vaulticStatusSuccess, SnapshotID: "aaaa1111", Started: time.Now(), Ended: time.Now()},
		{ID: "2", Profile: "Documents", Repository: "local:/external/drive-b", Status: vaulticStatusFailure, Started: time.Now(), Ended: time.Now()},
		{ID: "3", Profile: "Documents", Repository: "local:/external/drive-b", Status: vaulticStatusSuccess, SnapshotID: "bbbb2222", Started: time.Now(), Ended: time.Now()},
	} {
		if err := h.Append(r); err != nil {
			t.Fatal(err)
		}
	}
	if r, ok := h.LastSuccessForRepository("Documents", "local:/external/drive-a"); !ok || r.SnapshotID != "aaaa1111" {
		t.Fatalf("drive a: %+v %t", r, ok)
	}
	if r, ok := h.LastSuccessForRepository("Documents", "local:/external/drive-b"); !ok || r.SnapshotID != "bbbb2222" {
		t.Fatalf("drive b: %+v %t", r, ok)
	}
	if _, ok := h.LastSuccessForRepository("Documents", "local:/external/drive-c"); ok {
		t.Fatal("success from another repository was incorrectly attributed")
	}
	recent := h.RecentForRepository("Documents", "local:/external/drive-b", 10)
	if len(recent) != 2 || recent[0].ID != "3" || recent[1].ID != "2" {
		t.Fatalf("repository history: %+v", recent)
	}
	if len(h.RecentForRepository("Documents", "", 10)) != 0 {
		t.Fatal("unspecified repository must have no attributed history")
	}
}

func TestVaulticAutomationSummaryUsesPinnedRepository(t *testing.T) {
	f := newAutomationFixture(t)
	if err := f.history.Append(vaulticRunRecord{ID: "old", Profile: "Documents", Repository: "local:/repo", Status: vaulticStatusSuccess, SnapshotID: "abcd1111"}); err != nil {
		t.Fatal(err)
	}
	f.profile.Schedule = &vaulticSchedule{Kind: vaulticScheduleDaily, Enabled: true, Time: "09:00", Repository: "local:/different-repo"}
	sum := vaulticSummarize(f.ui.hooks)
	if !strings.Contains(sum.LastSuccess, "Aucune sauvegarde") {
		t.Fatalf("old repository falsely shown as protected: %q", sum.LastSuccess)
	}
	if !strings.Contains(sum.Warning, "n'est pas actif") {
		t.Fatalf("wrong target warning: %q", sum.Warning)
	}
	f.profile.Schedule.Repository = "local:/repo"
	sum = vaulticSummarize(f.ui.hooks)
	if !strings.Contains(sum.LastSuccess, "abcd1111") {
		t.Fatalf("current repository success missing: %q", sum.LastSuccess)
	}
}

func TestVaulticPinnedScheduleDoesNotBackupActiveOtherRepository(t *testing.T) {
	h := newHarness(t, hourly(1))
	h.profile[0].Schedule.Repository = "local:/a-different-target"
	h.clock.Advance(time.Hour)
	h.coord.Tick()
	h.settle()
	if h.execs.Load() != 0 {
		t.Fatal("scheduled backup ran against the wrong active repository")
	}
	if !h.coord.Overdue("Documents") {
		t.Fatal("pending target must be displayed as overdue")
	}
	h.profile[0].Schedule.Repository = h.repo
	h.coord.Tick()
	h.settle()
	if h.execs.Load() != 1 {
		t.Fatalf("pinned target was not backed up after activation: %d", h.execs.Load())
	}
}

func TestVaulticCoordinatorReportsSlotPersistenceFailure(t *testing.T) {
	h := newHarness(t, hourly(1))
	failures := make(chan error, 1)
	h.coord.OnPersistenceError = func(err error) { failures <- err }
	h.coord.env.SaveSlotWorker = func(string, time.Time) error { return errors.New("disk full") }
	h.clock.Advance(time.Hour)
	h.coord.Tick()
	h.settle()
	select {
	case err := <-failures:
		if !strings.Contains(err.Error(), "disk full") {
			t.Fatalf("wrong persistence error: %v", err)
		}
	default:
		t.Fatal("persistence failure was silently ignored")
	}
}

func TestVaulticScheduleRepositoryIsNonSecretPersistentMetadata(t *testing.T) {
	s := vaulticSchedule{Kind: vaulticScheduleDaily, Enabled: true, Time: "09:00", Repository: "local:/backup"}
	raw, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "\"repository\":\"local:/backup\"") {
		t.Fatalf("schedule destination lost: %s", raw)
	}
	if strings.Contains(string(raw), "password") {
		t.Fatal("schedule unexpectedly contains password")
	}
}
