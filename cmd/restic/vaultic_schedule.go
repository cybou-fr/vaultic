package main

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
)

// Schedules are evaluated in the computer's local time zone while Vaultic is
// running. Nothing here talks to the operating system's task scheduler.
//
// The model never builds a backlog: however many slots were missed (computer
// asleep, application closed, clock changed), at most ONE catch-up run is
// requested, and the schedule resumes from the latest slot that has passed.

const (
	vaulticScheduleManual  = "manual"
	vaulticScheduleHourly  = "hourly"
	vaulticScheduleDaily   = "daily"
	vaulticScheduleWeekly  = "weekly"
	vaulticScheduleMonthly = "monthly"

	// A run that starts later than this after its slot is a catch-up run.
	vaulticCatchUpAfter = 5 * time.Minute
)

// vaulticSchedule is stored in vaultic.json. It contains no credentials.
type vaulticSchedule struct {
	Repository string `json:"repository,omitempty"` // pinned destination; no passwords or credentials
	Kind       string `json:"kind"`
	Enabled    bool   `json:"enabled"`
	EveryHours int    `json:"every_hours,omitempty"`
	Time       string `json:"time,omitempty"`     // "HH:MM", local time
	Weekdays   []int  `json:"weekdays,omitempty"` // time.Weekday values, 0 = Sunday
	MonthDay   int    `json:"month_day,omitempty"`
	// LastSlot is the latest slot already handled (run, skipped or coalesced),
	// as an RFC 3339 instant. It anchors hourly schedules and prevents repeats.
	LastSlot string `json:"last_slot,omitempty"`
}

var vaulticFrenchWeekdays = [...]string{"dimanche", "lundi", "mardi", "mercredi", "jeudi", "vendredi", "samedi"}

func (s vaulticSchedule) Active() bool {
	return s.Enabled && s.Kind != "" && s.Kind != vaulticScheduleManual
}

func vaulticParseClock(value string) (hour, minute int, err error) {
	if _, scanErr := fmt.Sscanf(value, "%2d:%2d", &hour, &minute); scanErr != nil || len(value) != 5 || value[2] != ':' || hour < 0 || hour > 23 || minute < 0 || minute > 59 {
		return 0, 0, errors.New("Indiquez l'heure au format HH:MM (00:00 à 23:59).")
	}
	return hour, minute, nil
}

func (s vaulticSchedule) Validate() error {
	switch s.Kind {
	case "", vaulticScheduleManual:
		return nil
	case vaulticScheduleHourly:
		if s.EveryHours < 1 || s.EveryHours > 24*7 {
			return errors.New("L'intervalle doit être compris entre 1 et 168 heures.")
		}
	case vaulticScheduleDaily:
		_, _, err := vaulticParseClock(s.Time)
		return err
	case vaulticScheduleWeekly:
		if _, _, err := vaulticParseClock(s.Time); err != nil {
			return err
		}
		if len(s.Weekdays) == 0 {
			return errors.New("Choisissez au moins un jour de la semaine.")
		}
		for _, day := range s.Weekdays {
			if day < 0 || day > 6 {
				return errors.New("Jour de la semaine invalide.")
			}
		}
	case vaulticScheduleMonthly:
		if _, _, err := vaulticParseClock(s.Time); err != nil {
			return err
		}
		if s.MonthDay < 1 || s.MonthDay > 31 {
			return errors.New("Le jour du mois doit être compris entre 1 et 31.")
		}
	default:
		return errors.New("Fréquence de sauvegarde inconnue.")
	}
	return nil
}

// Describe returns the schedule in French.
func (s vaulticSchedule) Describe() string {
	switch s.Kind {
	case vaulticScheduleHourly:
		if s.EveryHours == 1 {
			return "Toutes les heures"
		}
		return fmt.Sprintf("Toutes les %d heures", s.EveryHours)
	case vaulticScheduleDaily:
		return "Chaque jour à " + s.Time
	case vaulticScheduleWeekly:
		days := append([]int(nil), s.Weekdays...)
		sort.Slice(days, func(i, j int) bool { return (days[i]+6)%7 < (days[j]+6)%7 }) // Monday first
		names := make([]string, 0, len(days))
		for _, day := range days {
			if day >= 0 && day <= 6 {
				names = append(names, vaulticFrenchWeekdays[day])
			}
		}
		return "Chaque semaine : " + strings.Join(names, ", ") + " à " + s.Time
	case vaulticScheduleMonthly:
		text := fmt.Sprintf("Chaque mois le %d à %s", s.MonthDay, s.Time)
		if s.MonthDay > 28 {
			text += " (dernier jour si le mois est plus court)"
		}
		return text
	}
	return "Manuel uniquement"
}

func (s vaulticSchedule) lastSlot() (time.Time, bool) {
	if s.LastSlot == "" {
		return time.Time{}, false
	}
	t, err := time.Parse(time.RFC3339, s.LastSlot)
	return t, err == nil
}

func daysInMonth(year int, month time.Month, loc *time.Location) int {
	return time.Date(year, month+1, 0, 12, 0, 0, 0, loc).Day()
}

// vaulticNextSlot returns the first slot strictly after the given instant.
// Wall-clock schedules use the calendar day in loc: on a spring-forward day a
// time that does not exist is shifted by the platform to a valid instant, and
// on a fall-back day an ambiguous time fires once, never twice.
func vaulticNextSlot(s vaulticSchedule, after time.Time, loc *time.Location) (time.Time, bool) {
	if !s.Active() || s.Validate() != nil {
		return time.Time{}, false
	}
	after = after.In(loc)
	switch s.Kind {
	case vaulticScheduleHourly:
		// Absolute durations: unaffected by daylight-saving changes.
		return after.Add(time.Duration(s.EveryHours) * time.Hour), true
	}
	hour, minute, _ := vaulticParseClock(s.Time)
	y, m, d := after.Date()
	for offset := -1; offset <= 62; offset++ {
		day := time.Date(y, m, d+offset, 12, 0, 0, 0, loc)
		dy, dm, dd := day.Date()
		match := false
		switch s.Kind {
		case vaulticScheduleDaily:
			match = true
		case vaulticScheduleWeekly:
			for _, wd := range s.Weekdays {
				if int(day.Weekday()) == wd {
					match = true
				}
			}
		case vaulticScheduleMonthly:
			target := min(s.MonthDay, daysInMonth(dy, dm, loc))
			match = dd == target
		}
		if !match {
			continue
		}
		candidate := time.Date(dy, dm, dd, hour, minute, 0, 0, loc)
		if candidate.After(after) {
			return candidate, true
		}
	}
	return time.Time{}, false
}

// vaulticSchedulePlan is the scheduler's verdict at a given instant.
type vaulticSchedulePlan struct {
	Due       bool      // a slot has passed and has not been handled
	Slot      time.Time // the latest slot that has passed (coalesced)
	Late      time.Duration
	CatchUp   bool      // the run would start well after its slot
	Next      time.Time // next planned slot after now (zero when none)
	ClockBack bool      // the stored anchor lies in the future: the clock moved back
	Anchor    time.Time // anchor used (LastSlot, or now when never anchored)
}

// vaulticPlan evaluates a schedule. A never-anchored schedule starts counting
// at now, so enabling a schedule never triggers an immediate backup.
func vaulticPlan(s vaulticSchedule, now time.Time, loc *time.Location) vaulticSchedulePlan {
	var plan vaulticSchedulePlan
	if !s.Active() || s.Validate() != nil {
		return plan
	}
	anchor, ok := s.lastSlot()
	if !ok {
		anchor = now
	}
	if anchor.After(now) {
		// The computer's clock went backwards: re-anchor instead of waiting
		// for a slot that is now far in the future.
		plan.ClockBack = true
		anchor = now
	}
	plan.Anchor = anchor
	first, ok := vaulticNextSlot(s, anchor, loc)
	if !ok {
		return plan
	}
	if first.After(now) {
		plan.Next = first
		return plan
	}
	// One or more slots have passed: coalesce them into the latest one.
	latest := first
	if s.Kind == vaulticScheduleHourly {
		step := time.Duration(s.EveryHours) * time.Hour
		latest = first.Add(now.Sub(first) / step * step)
	} else {
		for i := 0; i < 5000; i++ {
			next, ok := vaulticNextSlot(s, latest, loc)
			if !ok || next.After(now) {
				break
			}
			latest = next
		}
	}
	plan.Due = true
	plan.Slot = latest
	plan.Late = now.Sub(latest)
	plan.CatchUp = plan.Late > vaulticCatchUpAfter
	if next, ok := vaulticNextSlot(s, latest, loc); ok {
		plan.Next = next
	}
	return plan
}

// vaulticCloneSchedule returns an independent copy (nil stays nil).
func vaulticCloneSchedule(s *vaulticSchedule) *vaulticSchedule {
	if s == nil {
		return nil
	}
	clone := *s
	clone.Weekdays = append([]int(nil), s.Weekdays...)
	return &clone
}
