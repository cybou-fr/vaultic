package main

import (
	"strings"
	"testing"
	"time"
)

func TestVaulticScheduleIntervalIsTheLongestGap(t *testing.T) {
	cases := []struct {
		name string
		s    vaulticSchedule
		want time.Duration
	}{
		{"hourly 6", vaulticSchedule{Kind: vaulticScheduleHourly, EveryHours: 6}, 6 * time.Hour},
		{"daily", vaulticSchedule{Kind: vaulticScheduleDaily}, 24 * time.Hour},
		{"weekly one day", vaulticSchedule{Kind: vaulticScheduleWeekly, Weekdays: []int{1}}, 7 * 24 * time.Hour},
		{"weekly mon+thu", vaulticSchedule{Kind: vaulticScheduleWeekly, Weekdays: []int{1, 4}}, 4 * 24 * time.Hour},
		{"weekly every weekday", vaulticSchedule{Kind: vaulticScheduleWeekly, Weekdays: []int{1, 2, 3, 4, 5}}, 3 * 24 * time.Hour},
		{"monthly", vaulticSchedule{Kind: vaulticScheduleMonthly}, 31 * 24 * time.Hour},
		{"manual", vaulticSchedule{Kind: vaulticScheduleManual}, 0},
	}
	for _, c := range cases {
		if got := vaulticScheduleInterval(c.s); got != c.want {
			t.Errorf("%s: got %v want %v", c.name, got, c.want)
		}
	}
}

func TestVaulticStalenessUsesTwiceTheIntervalAndTheAnchorWhenNeverSucceeded(t *testing.T) {
	now := time.Date(2026, 6, 10, 12, 0, 0, 0, time.UTC)
	daily := vaulticSchedule{Kind: vaulticScheduleDaily, Enabled: true, Time: "09:00", LastSlot: now.Add(-100 * time.Hour).Format(time.RFC3339)}

	if st := vaulticCheckStale(daily, now.Add(-47*time.Hour), now); st.Stale {
		t.Fatal("47 hours is within twice a daily interval")
	}
	st := vaulticCheckStale(daily, now.Add(-49*time.Hour), now)
	if !st.Stale || st.Never || st.Age != 49*time.Hour {
		t.Fatalf("49 hours must be stale: %+v", st)
	}
	st = vaulticCheckStale(daily, time.Time{}, now) // never succeeded: anchor 100 h ago
	if !st.Stale || !st.Never {
		t.Fatalf("a schedule that never worked must be reported: %+v", st)
	}
	fresh := daily
	fresh.LastSlot = now.Add(-3 * time.Hour).Format(time.RFC3339)
	if st := vaulticCheckStale(fresh, time.Time{}, now); st.Stale {
		t.Fatal("a recently created schedule is not stale yet")
	}
	noAnchor := daily
	noAnchor.LastSlot = ""
	if st := vaulticCheckStale(noAnchor, time.Time{}, now); st.Stale {
		t.Fatal("without any reference nothing can be called stale")
	}
	disabled := daily
	disabled.Enabled = false
	if st := vaulticCheckStale(disabled, now.Add(-1000*time.Hour), now); st.Stale {
		t.Fatal("a disabled schedule never warns")
	}
	if st := vaulticCheckStale(daily, now.Add(time.Hour), now); st.Stale {
		t.Fatal("a clock that moved back must not warn")
	}
}

func TestVaulticStaleMessagesAreReadable(t *testing.T) {
	if got := vaulticFormatAge(90 * time.Minute); got != "90 minutes" {
		t.Fatalf("minutes: %q", got)
	}
	if got := vaulticFormatAge(5 * time.Hour); got != "5 heures" {
		t.Fatalf("hours: %q", got)
	}
	if got := vaulticFormatAge(72 * time.Hour); got != "3 jours" {
		t.Fatalf("days: %q", got)
	}
	m := vaulticStaleMessage("Documents", vaulticStaleness{Stale: true, Age: 72 * time.Hour})
	if !strings.Contains(m, "Documents") || !strings.Contains(m, "3 jours") {
		t.Fatalf("message: %s", m)
	}
	if m := vaulticStaleMessage("Documents", vaulticStaleness{Stale: true, Never: true, Age: 72 * time.Hour}); !strings.Contains(m, "Aucune sauvegarde réussie") {
		t.Fatalf("never message: %s", m)
	}
}

// The scheduler warns about stale protection once a day, only through the
// overdue notification category, and never starts or fakes a backup.
func TestVaulticSchedulerWarnsOncePerDayAboutStaleProtection(t *testing.T) {
	h := newHarness(t, vaulticSchedule{Kind: vaulticScheduleDaily, Enabled: true, Time: "09:00"})
	h.creds.Store(false) // nothing can run: only the warning is under test
	h.clock.Advance(5 * 24 * time.Hour)
	h.coord.Tick()
	h.coord.Tick()
	h.settle()
	stale := 0
	for _, n := range h.notifications() {
		if strings.Contains(n, "Sauvegarde ancienne") {
			stale++
		}
	}
	if stale != 1 {
		t.Fatalf("expected one stale warning, got %d: %v", stale, h.notifications())
	}
	h.clock.Advance(24 * time.Hour)
	h.coord.Tick()
	h.settle()
	count := 0
	for _, n := range h.notifications() {
		if strings.Contains(n, "Sauvegarde ancienne") {
			count++
		}
	}
	if count != 2 {
		t.Fatalf("a new day gives a new warning, got %d", count)
	}
	for _, r := range h.records() {
		if r.Status == vaulticStatusSuccess {
			t.Fatal("a stale warning must never create a success")
		}
	}
}

func TestVaulticAutomationSummaryShowsTheStaleWarning(t *testing.T) {
	f := newAutomationFixture(t)
	f.profile.Schedule = &vaulticSchedule{Kind: vaulticScheduleDaily, Enabled: true, Time: "09:00", Repository: "local:/repo",
		LastSlot: f.now.Add(-200 * time.Hour).Format(time.RFC3339)}
	_ = f.history.Append(vaulticRunRecord{ID: "old", Profile: "Documents", Repository: "local:/repo", Status: vaulticStatusSuccess,
		SnapshotID: "abcd1111", Ended: f.now.Add(-100 * time.Hour)})
	sum := vaulticSummarize(f.ui.hooks)
	if !strings.Contains(sum.Warning, "plus de deux fois l'intervalle") {
		t.Fatalf("warning: %q", sum.Warning)
	}
}
