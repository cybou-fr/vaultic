package main

import (
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/test"
)

type automationFixture struct {
	t        *testing.T
	profile  vaulticBackupProfile
	saved    []vaulticSchedule
	paused   bool
	prefs    vaulticNotifyPrefs
	creds    bool
	history  *vaulticHistoryStore
	coord    *vaulticCoordinator
	now      time.Time
	loc      *time.Location
	ui       *vaulticAutomationUI
	busy     atomic.Bool
	setErr   error
	resumed  int
	notified int
}

func newAutomationFixture(t *testing.T) *automationFixture {
	t.Helper()
	test.NewTempApp(t)
	loc := mustLoc(t, "Europe/Paris")
	f := &automationFixture{t: t, loc: loc, now: at(loc, 2026, time.June, 10, 14, 3), prefs: vaulticDefaultNotifyPrefs(), creds: true}
	f.profile = vaulticBackupProfile{Name: "Documents", Sources: []string{"/x"}}
	f.history = newVaulticHistoryStore(filepath.Join(t.TempDir(), "h.json"))
	env := vaulticCoordinatorEnv{
		Now: func() time.Time { return f.now }, Location: func() *time.Location { return loc },
		Profiles:       func() []vaulticBackupProfile { return []vaulticBackupProfile{f.profile} },
		Repository:     func() string { return "local:/repo" },
		HasCredentials: func() bool { return f.creds }, Paused: func() bool { return f.paused },
		SaveSlot: func(string, time.Time) error { return nil }, Start: func(vaulticJob) bool { return false },
	}
	f.coord = newVaulticCoordinator(&f.busy, env, f.history, newVaulticNotifier(func() vaulticNotifyPrefs { return f.prefs }, func(string, string) { f.notified++ }, func() time.Time { return f.now }))
	f.ui = newVaulticAutomationUI(vaulticAutomationHooks{
		Profile: func() vaulticBackupProfile { return f.profile },
		SetSchedule: func(s vaulticSchedule) error {
			if f.setErr != nil {
				return f.setErr
			}
			f.saved = append(f.saved, s)
			if s.Kind == vaulticScheduleManual {
				f.profile.Schedule = nil
			} else {
				f.profile.Schedule = &s
			}
			return nil
		},
		Paused: func() bool { return f.paused }, SetPaused: func(p bool) { f.paused = p; f.resumed++ },
		Notify: func() vaulticNotifyPrefs { return f.prefs }, SetNotify: func(p vaulticNotifyPrefs) { f.prefs = p },
		Coordinator: f.coord, History: f.history, Now: func() time.Time { return f.now },
		ActiveRepository: func() string { return "local:/repo" },
		Credentials: func() bool { return f.creds }, Location: func() *time.Location { return loc },
	})
	return f
}

func TestVaulticAutomationControlsFollowTheFrequency(t *testing.T) {
	f := newAutomationFixture(t)
	if f.ui.frequency.Selected != vaulticFreqManual || f.ui.hoursRow.Visible() || f.ui.clockRow.Visible() || !f.ui.enabled.Disabled() {
		t.Fatal("manual profile must show no schedule controls")
	}
	cases := map[string][4]bool{ // hours, clock, weekdays, month day
		vaulticFreqHourly: {true, false, false, false}, vaulticFreqDaily: {false, true, false, false},
		vaulticFreqWeekly: {false, true, true, false}, vaulticFreqMonthly: {false, true, false, true},
		vaulticFreqManual: {false, false, false, false},
	}
	for label, want := range cases {
		f.ui.frequency.SetSelected(label)
		got := [4]bool{f.ui.hoursRow.Visible(), f.ui.clockRow.Visible(), f.ui.weekRow.Visible(), f.ui.monthRow.Visible()}
		if got != want {
			t.Errorf("%s: visible rows %v, want %v", label, got, want)
		}
	}
}

func TestVaulticAutomationSavesEachKindOfSchedule(t *testing.T) {
	f := newAutomationFixture(t)
	f.ui.enabled.SetChecked(true)

	f.ui.frequency.SetSelected(vaulticFreqHourly)
	f.ui.hours.SetSelected("6")
	f.ui.save()
	f.ui.frequency.SetSelected(vaulticFreqDaily)
	f.ui.clock.SetText("21:30")
	f.ui.save()
	f.ui.frequency.SetSelected(vaulticFreqWeekly)
	f.ui.weekdays[time.Monday].SetChecked(true)
	f.ui.weekdays[time.Friday].SetChecked(true)
	f.ui.save()
	f.ui.frequency.SetSelected(vaulticFreqMonthly)
	f.ui.monthDay.SetSelected("31")
	f.ui.save()
	if len(f.saved) != 4 {
		t.Fatalf("saved %d schedules: %+v", len(f.saved), f.saved)
	}
	h, d, w, m := f.saved[0], f.saved[1], f.saved[2], f.saved[3]
	if h.Kind != vaulticScheduleHourly || h.EveryHours != 6 || h.Time != "" || !h.Enabled {
		t.Errorf("hourly: %+v", h)
	}
	if d.Kind != vaulticScheduleDaily || d.Time != "21:30" || d.EveryHours != 0 || len(d.Weekdays) != 0 {
		t.Errorf("daily: %+v", d)
	}
	if w.Kind != vaulticScheduleWeekly || len(w.Weekdays) < 2 || w.MonthDay != 0 {
		t.Errorf("weekly: %+v", w)
	}
	if m.Kind != vaulticScheduleMonthly || m.MonthDay != 31 || len(m.Weekdays) != 0 {
		t.Errorf("monthly: %+v", m)
	}
	for _, s := range f.saved {
		if err := s.Validate(); err != nil {
			t.Errorf("saved an invalid schedule %+v: %v", s, err)
		}
	}
	// The controls reload from the saved profile.
	f.ui.Load()
	if f.ui.frequency.Selected != vaulticFreqMonthly || f.ui.monthDay.Selected != "31" || !f.ui.enabled.Checked {
		t.Fatalf("reload: %s %s", f.ui.frequency.Selected, f.ui.monthDay.Selected)
	}
}

func TestVaulticAutomationRejectsInvalidInput(t *testing.T) {
	f := newAutomationFixture(t)
	f.ui.enabled.SetChecked(true)
	f.ui.frequency.SetSelected(vaulticFreqDaily)
	for _, bad := range []string{"", "25:00", "9:5", "abc", "12:99"} {
		f.ui.clock.SetText(bad)
		f.ui.save()
		if len(f.saved) != 0 || f.ui.message.Text == "" {
			t.Fatalf("%q: saved %d, message %q", bad, len(f.saved), f.ui.message.Text)
		}
	}
	f.ui.frequency.SetSelected(vaulticFreqWeekly)
	f.ui.clock.SetText("08:00")
	for _, day := range vaulticWeekdayOrder {
		f.ui.weekdays[day].SetChecked(false)
	}
	f.ui.save()
	if len(f.saved) != 0 || !strings.Contains(f.ui.message.Text, "jour") {
		t.Fatalf("weekly without a day: %d %q", len(f.saved), f.ui.message.Text)
	}
	f.ui.weekdays[time.Sunday].SetChecked(true)
	f.setErr = errTest("disque plein")
	f.ui.save()
	if f.ui.message.Text != "disque plein" {
		t.Fatalf("storage error not shown: %q", f.ui.message.Text)
	}
}

type errTest string

func (e errTest) Error() string { return string(e) }

func TestVaulticAutomationSummaryTexts(t *testing.T) {
	f := newAutomationFixture(t)
	sum := vaulticSummarize(f.ui.hooks)
	if sum.Schedule != "Manuel uniquement" || !strings.Contains(sum.Next, "Aucune") || !strings.Contains(sum.LastSuccess, "Aucune sauvegarde") {
		t.Fatalf("manual: %+v", sum)
	}
	f.profile.Schedule = &vaulticSchedule{Kind: vaulticScheduleDaily, Time: "02:30", Enabled: false}
	if sum = vaulticSummarize(f.ui.hooks); !strings.Contains(sum.Schedule, "désactivée") || !strings.Contains(sum.Next, "désactivée") {
		t.Fatalf("disabled: %+v", sum)
	}
	f.profile.Schedule = &vaulticSchedule{Kind: vaulticScheduleDaily, Time: "02:30", Enabled: true, LastSlot: at(f.loc, 2026, time.June, 10, 2, 30).Format(time.RFC3339)}
	sum = vaulticSummarize(f.ui.hooks)
	if sum.Schedule != "Chaque jour à 02:30" || sum.Next != "jeu. 11 juin 2026 à 02:30" {
		t.Fatalf("enabled: %+v", sum)
	}
	f.paused = true
	if sum = vaulticSummarize(f.ui.hooks); !strings.Contains(sum.Next, "pause") || !strings.Contains(sum.Schedule, "pause") || !sum.Paused {
		t.Fatalf("paused: %+v", sum)
	}
	f.paused = false

	// A success appears as the last successful backup; failures do not.
	_ = f.history.Append(vaulticRunRecord{ID: "1", Profile: "Documents", Repository: "local:/repo", Status: vaulticStatusSuccess, SnapshotID: "abcdef0123456789", Ended: at(f.loc, 2026, time.June, 10, 2, 31), Started: at(f.loc, 2026, time.June, 10, 2, 30)})
	_ = f.history.Append(vaulticRunRecord{ID: "2", Profile: "Documents", Repository: "local:/repo", Status: vaulticStatusFailure, Summary: "échec", Ended: at(f.loc, 2026, time.June, 11, 2, 31), Started: at(f.loc, 2026, time.June, 11, 2, 30)})
	sum = vaulticSummarize(f.ui.hooks)
	if !strings.Contains(sum.LastSuccess, "10 juin 2026 à 02:31") || !strings.Contains(sum.LastSuccess, "abcdef01") {
		t.Fatalf("last success: %q", sum.LastSuccess)
	}

	// Due and waiting for the password: shown as overdue, never as done.
	f.now = at(f.loc, 2026, time.June, 11, 2, 40)
	f.creds = false
	f.coord.Tick()
	sum = vaulticSummarize(f.ui.hooks)
	if !strings.Contains(sum.Next, "En retard") || !strings.Contains(sum.Warning, "mot de passe") || !strings.Contains(sum.Warning, "pas considérée comme réalisée") {
		t.Fatalf("overdue: %+v", sum)
	}
	f.ui.Refresh()
	if f.ui.warning.Hidden || !strings.Contains(f.ui.nextValue.Text, "En retard") {
		t.Fatal("overdue state not displayed")
	}
}

func TestVaulticAutomationPauseButtonAndNotificationChoices(t *testing.T) {
	f := newAutomationFixture(t)
	if f.ui.pauseButton.Text != "Mettre en pause" {
		t.Fatalf("pause label: %q", f.ui.pauseButton.Text)
	}
	test.Tap(f.ui.pauseButton)
	if !f.paused || f.ui.pauseButton.Text != "Reprendre la planification" {
		t.Fatalf("paused state: %v %q", f.paused, f.ui.pauseButton.Text)
	}
	test.Tap(f.ui.pauseButton)
	if f.paused || f.ui.pauseButton.Text != "Mettre en pause" {
		t.Fatalf("resumed state: %v %q", f.paused, f.ui.pauseButton.Text)
	}
	f.ui.notify[0].SetChecked(false)
	f.ui.notify[3].SetChecked(false)
	if f.prefs.Completed || f.prefs.Overdue || !f.prefs.Failed || !f.prefs.Skipped {
		t.Fatalf("notification choices: %+v", f.prefs)
	}
}

func TestVaulticAutomationHistoryListShowsRecentRuns(t *testing.T) {
	f := newAutomationFixture(t)
	if len(f.ui.historyBox.Objects) != 1 {
		t.Fatal("empty history must show a message")
	}
	for i := 0; i < 12; i++ {
		status := vaulticStatusSuccess
		if i%4 == 1 {
			status = vaulticStatusFailure
		}
		if i%4 == 2 {
			status = vaulticStatusSkipped
		}
		_ = f.history.Append(vaulticRunRecord{ID: string(rune('a' + i)), Profile: "Documents", Repository: "local:/repo", Status: status, Trigger: vaulticTriggerScheduled, Started: f.now.Add(time.Duration(i) * time.Minute), Ended: f.now.Add(time.Duration(i) * time.Minute), SnapshotID: "0123456789abcdef"})
	}
	_ = f.history.Append(vaulticRunRecord{ID: "z", Profile: "Autre profil", Status: vaulticStatusSuccess})
	f.ui.Refresh()
	if len(f.ui.historyBox.Objects) != 8 {
		t.Fatalf("expected the 8 most recent runs of this profile, got %d", len(f.ui.historyBox.Objects))
	}
	if vaulticFormatDuration(90*time.Second) != "1 min 30 s" || vaulticFormatDuration(300*time.Millisecond) != "< 1 s" || vaulticFormatDuration(2*time.Hour+5*time.Minute) != "2 h 05 min" {
		t.Fatal("duration formatting")
	}
}

// Opt-in: VAULTIC_SCREENSHOT_DIR=<dir> renders the Automatisation section.
func TestVaulticCaptureAutomationScreens(t *testing.T) {
	dir := os.Getenv("VAULTIC_SCREENSHOT_DIR")
	if dir == "" {
		t.Skip("set VAULTIC_SCREENSHOT_DIR to render screenshots")
	}
	f := newAutomationFixture(t)
	fyne.CurrentApp().Settings().SetTheme(&vaulticVisualTheme{})
	f.profile.Schedule = &vaulticSchedule{Kind: vaulticScheduleWeekly, Enabled: true, Time: "20:00", Weekdays: []int{1, 4}, LastSlot: at(f.loc, 2026, time.June, 8, 20, 0).Format(time.RFC3339)}
	base := f.now.Add(-6 * 24 * time.Hour)
	records := []vaulticRunRecord{
		{ID: "1", Profile: "Documents", Repository: "local:/repo", Trigger: vaulticTriggerScheduled, Status: vaulticStatusSuccess, Files: 1284, Bytes: 734003200, SnapshotID: "4a5b6c7d8e9f0011", DurationMs: 83000},
		{ID: "2", Profile: "Documents", Repository: "local:/repo", Trigger: vaulticTriggerManual, Status: vaulticStatusSuccess, Files: 12, Bytes: 3400000, SnapshotID: "99aabbccddeeff00", DurationMs: 4200},
		{ID: "3", Profile: "Documents", Repository: "local:/repo", Trigger: vaulticTriggerScheduled, Status: vaulticStatusSkipped, Summary: "Mot de passe du dépôt non disponible : la sauvegarde planifiée attend."},
		{ID: "4", Profile: "Documents", Repository: "local:/repo", Trigger: vaulticTriggerScheduled, CatchUp: true, Status: vaulticStatusFailure, DurationMs: 2100, Summary: "Dépôt inaccessible : le disque externe n'est pas branché."},
		{ID: "5", Profile: "Documents", Repository: "local:/repo", Trigger: vaulticTriggerManual, Status: vaulticStatusCancelled, DurationMs: 9000, Summary: "Interrompue avant la fin ; aucun instantané confirmé."},
	}
	for i, r := range records {
		r.Started = base.Add(time.Duration(i) * 24 * time.Hour)
		r.Ended = r.Started.Add(r.Duration())
		_ = f.history.Append(r)
	}
	f.ui.Load()
	save := func(name string, size fyne.Size) {
		window := test.NewWindow(container.NewScroll(container.NewPadded(f.ui.Content)))
		defer window.Close()
		window.Resize(size)
		file, err := os.Create(filepath.Join(dir, name))
		if err != nil {
			t.Fatal(err)
		}
		defer file.Close()
		if err := png.Encode(file, window.Canvas().Capture()); err != nil {
			t.Fatal(err)
		}
	}
	save("automation-weekly.png", fyne.NewSize(760, 1500))
	f.ui.advanced.OpenAll()
	f.ui.Refresh()
	save("automation-advanced-narrow.png", fyne.NewSize(430, 1900))
}
