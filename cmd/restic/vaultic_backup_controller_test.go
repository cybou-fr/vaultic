package main

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestVaulticRunSchedulerFirstTickThenInterval(t *testing.T) {
	stop := make(chan struct{})
	var ticks atomic.Int32
	done := make(chan struct{})
	go func() {
		vaulticRunScheduler(stop, 20*time.Millisecond, 30*time.Millisecond, func() { ticks.Add(1) })
		close(done)
	}()
	deadline := time.Now().Add(3 * time.Second)
	for ticks.Load() < 3 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if ticks.Load() < 3 {
		t.Fatalf("scheduler ticked %d times", ticks.Load())
	}
	close(stop)
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("scheduler did not stop")
	}
	after := ticks.Load()
	time.Sleep(80 * time.Millisecond)
	if ticks.Load() != after {
		t.Fatal("scheduler kept ticking after stop")
	}
}

func TestVaulticFindProfileTitleAndSlot(t *testing.T) {
	profiles := []vaulticBackupProfile{
		{Name: "A"},
		{Name: "B", Schedule: &vaulticSchedule{Kind: vaulticScheduleDaily, Enabled: true, Time: "09:00"}},
	}
	if p, ok := vaulticFindProfile(profiles, "B"); !ok || p.Name != "B" {
		t.Fatalf("find: %+v %v", p, ok)
	}
	if _, ok := vaulticFindProfile(profiles, "C"); ok {
		t.Fatal("unknown profile")
	}
	if vaulticBackupTitle(vaulticTriggerManual) != "Sauvegarde" || vaulticBackupTitle(vaulticTriggerScheduled) != "Sauvegarde planifiée" {
		t.Fatal("operation titles")
	}
	slot := time.Date(2026, 6, 10, 9, 0, 0, 0, time.UTC)
	if !vaulticSetSlot(profiles, "B", slot) || profiles[1].Schedule.LastSlot != slot.Format(time.RFC3339) {
		t.Fatalf("slot not recorded: %+v", profiles[1].Schedule)
	}
	if vaulticSetSlot(profiles, "A", slot) {
		t.Fatal("a profile without a schedule has no slot")
	}
	if vaulticSetSlot(profiles, "C", slot) {
		t.Fatal("unknown profile")
	}
}

func TestVaulticPrepareLoadedSettingsPinsScheduleAndResolvesSources(t *testing.T) {
	abs := filepath.Join(t.TempDir(), "docs")
	s := vaulticSettings{Repository: "/repo/a", Profiles: []vaulticBackupProfile{
		{Name: "A", Sources: []string{abs, "relative-dir"}, Schedule: &vaulticSchedule{Kind: vaulticScheduleDaily, Enabled: true, Time: "09:00"}},
		{Name: "B", Schedule: &vaulticSchedule{Kind: vaulticScheduleDaily, Enabled: true, Time: "10:00", Repository: "/repo/b"}},
	}}
	vaulticPrepareLoadedSettings(&s)
	if s.Profiles[0].Schedule.Repository != "/repo/a" {
		t.Fatalf("legacy schedule must be pinned to the active repository: %q", s.Profiles[0].Schedule.Repository)
	}
	if s.Profiles[1].Schedule.Repository != "/repo/b" {
		t.Fatal("an existing destination must not change")
	}
	if s.Profiles[0].Sources[0] != abs || !filepath.IsAbs(s.Profiles[0].Sources[1]) {
		t.Fatalf("sources: %v", s.Profiles[0].Sources)
	}
}

func TestVaulticSettingsForSaveIsPortableCopyWithoutSecrets(t *testing.T) {
	saved := vaulticSettings{
		Profiles: []vaulticBackupProfile{
			{Name: "A", Sources: []string{"/data/x"}, Excludes: []string{"*.tmp"}, Schedule: &vaulticSchedule{Kind: vaulticScheduleDaily, Enabled: true, Time: "09:00", Repository: "/repo/a"}},
			{Name: "B", Sources: []string{"/b"}},
		},
		SchedulerPaused: true,
	}
	out := vaulticSettingsForSave(saved, []vaulticDepot{{Name: "d", Location: "/repo/a"}}, "/repo/a", 1)
	if out.ActiveProfile != "B" || out.Repository != "/repo/a" || !out.SchedulerPaused || len(out.Depots) != 1 || len(out.Profiles) != 2 {
		t.Fatalf("settings: %+v", out)
	}
	out.Profiles[0].Schedule.Time = "23:59"
	out.Profiles[0].Excludes[0] = "changed"
	if saved.Profiles[0].Schedule.Time != "09:00" || saved.Profiles[0].Excludes[0] != "*.tmp" {
		t.Fatal("the saved copy must be independent of live state")
	}
	raw, _ := json.Marshal(out)
	for _, forbidden := range []string{"password", "secret", "token"} {
		if strings.Contains(strings.ToLower(string(raw)), forbidden) {
			t.Fatalf("settings JSON contains %q", forbidden)
		}
	}
}
