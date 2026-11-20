package main

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/widget"
	"testing"
	"time"
)

func scheduleProfiles(s *vaulticSchedule) []vaulticBackupProfile {
	return []vaulticBackupProfile{{Name: "A"}, {Name: "B", Schedule: s}}
}

func TestVaulticApplySchedulePinsActiveRepositoryAndAnchorsAtNow(t *testing.T) {
	now := time.Date(2026, 6, 10, 8, 0, 0, 0, time.UTC)
	in := scheduleProfiles(nil)
	out, err := vaulticApplySchedule(in, 1, vaulticSchedule{Kind: vaulticScheduleHourly, Enabled: true, EveryHours: 2}, "local:/repo-a", now)
	if err != nil {
		t.Fatal(err)
	}
	got := out[1].Schedule
	if got == nil || got.Repository != "local:/repo-a" || got.LastSlot != now.Format(time.RFC3339) {
		t.Fatalf("schedule: %+v", got)
	}
	if in[1].Schedule != nil {
		t.Fatal("input profiles were modified")
	}
}

func TestVaulticApplyScheduleUnchangedKeepsSlotChangedRepositoryRestarts(t *testing.T) {
	now := time.Date(2026, 6, 10, 8, 0, 0, 0, time.UTC)
	old := &vaulticSchedule{Kind: vaulticScheduleDaily, Enabled: true, Time: "09:00", Repository: "local:/repo-a", LastSlot: "2026-06-09T09:00:00Z"}
	same := vaulticSchedule{Kind: vaulticScheduleDaily, Enabled: true, Time: "09:00"}
	out, err := vaulticApplySchedule(scheduleProfiles(old), 1, same, "local:/repo-a", now)
	if err != nil || out[1].Schedule.LastSlot != old.LastSlot {
		t.Fatalf("unchanged schedule lost its slot: %+v %v", out[1].Schedule, err)
	}
	out, err = vaulticApplySchedule(scheduleProfiles(old), 1, same, "local:/repo-b", now)
	if err != nil || out[1].Schedule.Repository != "local:/repo-b" || out[1].Schedule.LastSlot != now.Format(time.RFC3339) {
		t.Fatalf("new destination must start fresh: %+v %v", out[1].Schedule, err)
	}
}

func TestVaulticApplyScheduleKeepsPreviousDestinationWithoutActiveRepository(t *testing.T) {
	old := &vaulticSchedule{Kind: vaulticScheduleDaily, Enabled: true, Time: "09:00", Repository: "local:/repo-a"}
	out, err := vaulticApplySchedule(scheduleProfiles(old), 1, vaulticSchedule{Kind: vaulticScheduleDaily, Enabled: true, Time: "10:00"}, "", time.Now())
	if err != nil || out[1].Schedule.Repository != "local:/repo-a" {
		t.Fatalf("destination lost: %+v %v", out[1].Schedule, err)
	}
}

func TestVaulticApplyScheduleRejectsMissingDestinationAndBadIndex(t *testing.T) {
	enabled := vaulticSchedule{Kind: vaulticScheduleDaily, Enabled: true, Time: "09:00"}
	if _, err := vaulticApplySchedule(scheduleProfiles(nil), 1, enabled, "", time.Now()); err == nil {
		t.Fatal("an enabled schedule without any destination must be refused")
	}
	if _, err := vaulticApplySchedule(scheduleProfiles(nil), 5, enabled, "local:/x", time.Now()); err == nil {
		t.Fatal("unknown profile must be refused")
	}
	// A disabled schedule can be stored without a destination.
	if out, err := vaulticApplySchedule(scheduleProfiles(nil), 1, vaulticSchedule{Kind: vaulticScheduleDaily, Time: "09:00"}, "", time.Now()); err != nil || out[1].Schedule == nil {
		t.Fatalf("disabled schedule: %v", err)
	}
}

func TestVaulticApplyScheduleManualRemovesTheSchedule(t *testing.T) {
	old := &vaulticSchedule{Kind: vaulticScheduleDaily, Enabled: true, Time: "09:00", Repository: "local:/repo-a"}
	out, err := vaulticApplySchedule(scheduleProfiles(old), 1, vaulticSchedule{Kind: vaulticScheduleManual}, "local:/repo-a", time.Now())
	if err != nil || out[1].Schedule != nil {
		t.Fatalf("manual must clear the schedule: %+v %v", out[1].Schedule, err)
	}
}

func TestVaulticPageBuildersAndNavigation(t *testing.T) {
	newAutomationFixture(t) // initialises a Fyne test application
	pages := []fyne.CanvasObject{vaulticSettingsPage(), vaulticSettingsPage(), vaulticSettingsPage(), vaulticSettingsPage(), vaulticSettingsPage(), vaulticSettingsPage(), vaulticSettingsPage(), vaulticSettingsPage()}
	nav := vaulticNewNavigation(pages)
	if len(nav.buttons) != len(vaulticNavLabels) || len(pages) != len(vaulticNavLabels) {
		t.Fatal("one sidebar button per page expected")
	}
	nav.Navigate(vaulticPageRepositories)
	if nav.Pages.Objects[0] != pages[vaulticPageRepositories] || nav.buttons[vaulticPageRepositories].Importance != widget.HighImportance || nav.buttons[vaulticPageOverview].Importance != widget.LowImportance {
		t.Fatal("navigation did not switch page and highlight")
	}
	nav.Navigate(99) // out of range is ignored
	if nav.Pages.Objects[0] != pages[vaulticPageRepositories] {
		t.Fatal("out-of-range navigation changed the page")
	}
}

func TestVaulticComparisonChoicesExcludeTargetAndPreselectSuggestion(t *testing.T) {
	base := time.Date(2026, 6, 1, 10, 0, 0, 0, time.UTC)
	snaps := []vaulticSnapshot{
		{ID: "id-new", ShortID: "new", Hostname: "pc", Time: base.Add(48 * time.Hour)},
		{ID: "id-mid", ShortID: "mid", Hostname: "pc", Time: base.Add(24 * time.Hour)},
		{ID: "id-old", ShortID: "old", Hostname: "pc", Time: base},
	}
	options, ids, initial := vaulticComparisonChoices(snaps, snaps[0])
	if len(options) != 2 || len(ids) != 2 {
		t.Fatalf("target must be excluded: %v", options)
	}
	for _, label := range options {
		if ids[label] == "id-new" {
			t.Fatal("target offered as its own reference")
		}
	}
	if initial == "" || ids[initial] == "" {
		t.Fatalf("no preselection: %q", initial)
	}
	if o, _, i := vaulticComparisonChoices(snaps[:1], snaps[0]); len(o) != 0 || i != "" {
		t.Fatal("a single snapshot has nothing to compare with")
	}
}
