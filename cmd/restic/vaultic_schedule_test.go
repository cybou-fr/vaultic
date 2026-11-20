package main

import (
	"testing"
	"time"
	_ "time/tzdata" // zone data for the daylight-saving tests, independent of the host
)

func mustLoc(t *testing.T, name string) *time.Location {
	t.Helper()
	loc, err := time.LoadLocation(name)
	if err != nil {
		t.Fatal(err)
	}
	return loc
}

func at(loc *time.Location, y int, m time.Month, d, h, min int) time.Time {
	return time.Date(y, m, d, h, min, 0, 0, loc)
}

func TestVaulticScheduleValidation(t *testing.T) {
	good := []vaulticSchedule{
		{}, {Kind: vaulticScheduleManual},
		{Kind: vaulticScheduleHourly, EveryHours: 1}, {Kind: vaulticScheduleHourly, EveryHours: 168},
		{Kind: vaulticScheduleDaily, Time: "00:00"}, {Kind: vaulticScheduleDaily, Time: "23:59"},
		{Kind: vaulticScheduleWeekly, Time: "20:00", Weekdays: []int{0, 6}},
		{Kind: vaulticScheduleMonthly, Time: "03:00", MonthDay: 31},
	}
	for _, s := range good {
		if err := s.Validate(); err != nil {
			t.Errorf("%+v rejected: %v", s, err)
		}
	}
	bad := []vaulticSchedule{
		{Kind: "yearly"}, {Kind: vaulticScheduleHourly}, {Kind: vaulticScheduleHourly, EveryHours: 169},
		{Kind: vaulticScheduleHourly, EveryHours: -1},
		{Kind: vaulticScheduleDaily}, {Kind: vaulticScheduleDaily, Time: "24:00"}, {Kind: vaulticScheduleDaily, Time: "7:30"},
		{Kind: vaulticScheduleDaily, Time: "12:60"}, {Kind: vaulticScheduleDaily, Time: "12-30"}, {Kind: vaulticScheduleDaily, Time: "ab:cd"},
		{Kind: vaulticScheduleWeekly, Time: "20:00"}, {Kind: vaulticScheduleWeekly, Time: "20:00", Weekdays: []int{7}},
		{Kind: vaulticScheduleMonthly, Time: "03:00"}, {Kind: vaulticScheduleMonthly, Time: "03:00", MonthDay: 32},
	}
	for _, s := range bad {
		if err := s.Validate(); err == nil {
			t.Errorf("%+v accepted", s)
		}
	}
}

func TestVaulticScheduleDescriptionsAreFrench(t *testing.T) {
	cases := map[string]vaulticSchedule{
		"Manuel uniquement":                               {},
		"Toutes les heures":                               {Kind: vaulticScheduleHourly, EveryHours: 1},
		"Toutes les 6 heures":                             {Kind: vaulticScheduleHourly, EveryHours: 6},
		"Chaque jour à 02:30":                             {Kind: vaulticScheduleDaily, Time: "02:30"},
		"Chaque semaine : lundi, jeudi, dimanche à 20:00": {Kind: vaulticScheduleWeekly, Time: "20:00", Weekdays: []int{0, 4, 1}},
		"Chaque mois le 15 à 03:00":                       {Kind: vaulticScheduleMonthly, Time: "03:00", MonthDay: 15},
		"Chaque mois le 31 à 03:00 (dernier jour si le mois est plus court)": {Kind: vaulticScheduleMonthly, Time: "03:00", MonthDay: 31},
	}
	for want, s := range cases {
		if got := s.Describe(); got != want {
			t.Errorf("got %q want %q", got, want)
		}
	}
}

func TestVaulticHourlySchedule(t *testing.T) {
	loc := mustLoc(t, "Europe/Paris")
	t0 := at(loc, 2026, time.June, 10, 8, 0)
	s := vaulticSchedule{Kind: vaulticScheduleHourly, Enabled: true, EveryHours: 6, LastSlot: t0.Format(time.RFC3339)}

	if p := vaulticPlan(s, t0.Add(5*time.Hour+59*time.Minute), loc); p.Due || !p.Next.Equal(t0.Add(6*time.Hour)) {
		t.Fatalf("before the slot: %+v", p)
	}
	p := vaulticPlan(s, t0.Add(6*time.Hour), loc)
	if !p.Due || !p.Slot.Equal(t0.Add(6*time.Hour)) || p.CatchUp || !p.Next.Equal(t0.Add(12*time.Hour)) {
		t.Fatalf("on the slot: %+v", p)
	}
	// 20 hours later: three slots passed, coalesced into the latest one.
	p = vaulticPlan(s, t0.Add(20*time.Hour), loc)
	if !p.Due || !p.Slot.Equal(t0.Add(18*time.Hour)) || !p.CatchUp || !p.Next.Equal(t0.Add(24*time.Hour)) {
		t.Fatalf("coalescing: %+v", p)
	}
	// Never anchored: counting starts now, so enabling does not run at once.
	s.LastSlot = ""
	now := t0.Add(time.Hour)
	if p := vaulticPlan(s, now, loc); p.Due || !p.Next.Equal(now.Add(6*time.Hour)) {
		t.Fatalf("unanchored: %+v", p)
	}
	// Disabled or manual: nothing is planned.
	s.Enabled = false
	if p := vaulticPlan(s, now, loc); p.Due || !p.Next.IsZero() {
		t.Fatalf("disabled: %+v", p)
	}
}

func TestVaulticDailyScheduleAndNextSlots(t *testing.T) {
	loc := mustLoc(t, "Europe/Paris")
	s := vaulticSchedule{Kind: vaulticScheduleDaily, Enabled: true, Time: "02:30"}
	next, ok := vaulticNextSlot(s, at(loc, 2026, time.June, 10, 1, 0), loc)
	if !ok || !next.Equal(at(loc, 2026, time.June, 10, 2, 30)) {
		t.Fatalf("same day: %v", next)
	}
	next, _ = vaulticNextSlot(s, at(loc, 2026, time.June, 10, 2, 30), loc) // strictly after
	if !next.Equal(at(loc, 2026, time.June, 11, 2, 30)) {
		t.Fatalf("strictly after: %v", next)
	}
	next, _ = vaulticNextSlot(s, at(loc, 2026, time.December, 31, 23, 0), loc)
	if !next.Equal(at(loc, 2027, time.January, 1, 2, 30)) {
		t.Fatalf("year end: %v", next)
	}
}

func TestVaulticWeeklySchedule(t *testing.T) {
	loc := mustLoc(t, "Europe/Paris")
	s := vaulticSchedule{Kind: vaulticScheduleWeekly, Enabled: true, Time: "20:00", Weekdays: []int{int(time.Monday), int(time.Thursday)}}
	// 2026-06-10 is a Wednesday.
	want := []time.Time{
		at(loc, 2026, time.June, 11, 20, 0), at(loc, 2026, time.June, 15, 20, 0),
		at(loc, 2026, time.June, 18, 20, 0), at(loc, 2026, time.June, 22, 20, 0),
	}
	cursor := at(loc, 2026, time.June, 10, 12, 0)
	for i, w := range want {
		next, ok := vaulticNextSlot(s, cursor, loc)
		if !ok || !next.Equal(w) {
			t.Fatalf("slot %d: got %v want %v", i, next, w)
		}
		cursor = next
	}
	// Later the same day on a selected weekday.
	next, _ := vaulticNextSlot(s, at(loc, 2026, time.June, 11, 8, 0), loc)
	if !next.Equal(at(loc, 2026, time.June, 11, 20, 0)) {
		t.Fatalf("same weekday: %v", next)
	}
}

func TestVaulticMonthlyScheduleClampsToShortMonths(t *testing.T) {
	loc := mustLoc(t, "Europe/Paris")
	s := vaulticSchedule{Kind: vaulticScheduleMonthly, Enabled: true, Time: "03:00", MonthDay: 31}
	want := []time.Time{
		at(loc, 2027, time.January, 31, 3, 0), at(loc, 2027, time.February, 28, 3, 0), at(loc, 2027, time.March, 31, 3, 0),
		at(loc, 2027, time.April, 30, 3, 0), at(loc, 2027, time.May, 31, 3, 0),
	}
	cursor := at(loc, 2027, time.January, 1, 0, 0)
	for _, w := range want {
		next, ok := vaulticNextSlot(s, cursor, loc)
		if !ok || !next.Equal(w) {
			t.Fatalf("got %v want %v", next, w)
		}
		cursor = next
	}
	// Leap year.
	next, _ := vaulticNextSlot(s, at(loc, 2028, time.February, 1, 0, 0), loc)
	if !next.Equal(at(loc, 2028, time.February, 29, 3, 0)) {
		t.Fatalf("leap february: %v", next)
	}
	// The 15th is never clamped.
	s.MonthDay = 15
	next, _ = vaulticNextSlot(s, at(loc, 2027, time.February, 16, 0, 0), loc)
	if !next.Equal(at(loc, 2027, time.March, 15, 3, 0)) {
		t.Fatalf("15th: %v", next)
	}
}

func TestVaulticDaylightSavingTransitions(t *testing.T) {
	loc := mustLoc(t, "Europe/Paris")
	daily := vaulticSchedule{Kind: vaulticScheduleDaily, Enabled: true, Time: "02:30"}
	// Count the slots over each transition week: exactly one per calendar day,
	// even though 02:30 does not exist on 29 March and happens twice on 25 October.
	for _, start := range []time.Time{at(loc, 2026, time.March, 27, 12, 0), at(loc, 2026, time.October, 23, 12, 0)} {
		cursor := start
		days := map[string]int{}
		for i := 0; i < 6; i++ {
			next, ok := vaulticNextSlot(daily, cursor, loc)
			if !ok || !next.After(cursor) {
				t.Fatalf("no progress from %v: %v", cursor, next)
			}
			days[next.In(loc).Format("2006-01-02")]++
			cursor = next
		}
		for day, n := range days {
			if n != 1 {
				t.Errorf("%s has %d slots", day, n)
			}
		}
		if len(days) != 6 {
			t.Errorf("days covered: %v", days)
		}
	}
	// The slot of the spring-forward day exists and is on that day.
	next, _ := vaulticNextSlot(daily, at(loc, 2026, time.March, 28, 12, 0), loc)
	if y, m, d := next.In(loc).Date(); y != 2026 || m != time.March || d != 29 {
		t.Fatalf("spring-forward slot on %v", next.In(loc))
	}

	// Hourly slots are absolute durations: 6 hours later is exactly 6 hours later.
	hourly := vaulticSchedule{Kind: vaulticScheduleHourly, Enabled: true, EveryHours: 6}
	before := at(loc, 2026, time.March, 29, 0, 0)
	next, _ = vaulticNextSlot(hourly, before, loc)
	if next.Sub(before) != 6*time.Hour {
		t.Fatalf("hourly across DST: %v", next.Sub(before))
	}
	// Weekly Sunday 02:30 across the change.
	weekly := vaulticSchedule{Kind: vaulticScheduleWeekly, Enabled: true, Time: "02:30", Weekdays: []int{0}}
	next, _ = vaulticNextSlot(weekly, at(loc, 2026, time.March, 22, 12, 0), loc)
	if next.In(loc).Day() != 29 || next.In(loc).Weekday() != time.Sunday {
		t.Fatalf("weekly: %v", next.In(loc))
	}
	// After a DST-day run, the plan does not fire a second time.
	slot := next
	daily.LastSlot = slot.Format(time.RFC3339)
	if p := vaulticPlan(daily, slot.Add(time.Hour), loc); p.Due {
		t.Fatalf("slot repeated after running: %+v", p)
	}
}

func TestVaulticTimezoneChange(t *testing.T) {
	paris, newYork := mustLoc(t, "Europe/Paris"), mustLoc(t, "America/New_York")
	s := vaulticSchedule{Kind: vaulticScheduleDaily, Enabled: true, Time: "09:00"}
	ran := at(paris, 2026, time.June, 10, 9, 0)
	s.LastSlot = ran.Format(time.RFC3339)
	// The computer moves to New York the same day: 09:00 there has not
	// happened yet when it is 07:30 in Paris... and then it passes once.
	now := at(newYork, 2026, time.June, 10, 9, 5)
	p := vaulticPlan(s, now, newYork)
	if !p.Due || !p.Slot.Equal(at(newYork, 2026, time.June, 10, 9, 0)) {
		t.Fatalf("one slot in the new zone: %+v", p)
	}
	s.LastSlot = p.Slot.Format(time.RFC3339)
	p = vaulticPlan(s, now.Add(time.Minute), newYork)
	if p.Due || !p.Next.Equal(at(newYork, 2026, time.June, 11, 9, 0)) {
		t.Fatalf("schedule continues in the new zone: %+v", p)
	}
	// Moving the other way never yields more than one due slot, however far.
	s.LastSlot = at(newYork, 2026, time.June, 1, 9, 0).Format(time.RFC3339)
	p = vaulticPlan(s, at(paris, 2026, time.June, 20, 10, 0), paris)
	if !p.Due || !p.Slot.Equal(at(paris, 2026, time.June, 20, 9, 0)) {
		t.Fatalf("coalesced after zone change: %+v", p)
	}
}

func TestVaulticClockChanges(t *testing.T) {
	loc := mustLoc(t, "Europe/Paris")
	s := vaulticSchedule{Kind: vaulticScheduleDaily, Enabled: true, Time: "02:30"}
	now := at(loc, 2026, time.June, 10, 12, 0)
	// Clock moved back: the stored slot lies in the future.
	s.LastSlot = now.Add(48 * time.Hour).Format(time.RFC3339)
	p := vaulticPlan(s, now, loc)
	if !p.ClockBack || p.Due {
		t.Fatalf("clock back: %+v", p)
	}
	// Clock moved forward by a month: a single catch-up, not thirty.
	s.LastSlot = now.Format(time.RFC3339)
	p = vaulticPlan(s, now.Add(30*24*time.Hour), loc)
	if !p.Due || !p.CatchUp || !p.Slot.Equal(at(loc, 2026, time.July, 10, 2, 30)) {
		t.Fatalf("clock forward: %+v", p)
	}
	// A corrupted anchor is treated as never anchored.
	s.LastSlot = "not a date"
	if p := vaulticPlan(s, now, loc); p.Due {
		t.Fatalf("bad anchor: %+v", p)
	}
}
