package main

import (
	"fmt"
	"time"
)

// A backup is "too old" when the last successful one is more than twice the
// schedule's longest gap behind. This is only a warning: it never starts a
// backup by itself and is never shown as a success.

// vaulticScheduleInterval is the longest gap between two slots of a schedule.
func vaulticScheduleInterval(s vaulticSchedule) time.Duration {
	switch s.Kind {
	case vaulticScheduleHourly:
		return time.Duration(max(s.EveryHours, 1)) * time.Hour
	case vaulticScheduleDaily:
		return 24 * time.Hour
	case vaulticScheduleWeekly:
		days := map[int]bool{}
		for _, d := range s.Weekdays {
			days[((d%7)+7)%7] = true
		}
		if len(days) <= 1 {
			return 7 * 24 * time.Hour
		}
		longest := 0
		for d := range days {
			gap := 1
			for !days[(d+gap)%7] {
				gap++
			}
			longest = max(longest, gap)
		}
		return time.Duration(longest) * 24 * time.Hour
	case vaulticScheduleMonthly:
		return 31 * 24 * time.Hour
	}
	return 0
}

// vaulticStaleness describes whether the protection of a schedule is overdue.
type vaulticStaleness struct {
	Stale bool
	Age   time.Duration // time since the reference moment
	Never bool          // no successful backup is known: the age runs from the schedule's anchor
}

// vaulticCheckStale compares the last successful backup (zero when none) with
// the schedule. Without a success, the schedule's anchor (its latest handled
// slot) is the reference, so a schedule that never worked is reported too.
func vaulticCheckStale(s vaulticSchedule, lastSuccess, now time.Time) vaulticStaleness {
	var result vaulticStaleness
	if !s.Active() || s.Validate() != nil {
		return result
	}
	interval := vaulticScheduleInterval(s)
	if interval <= 0 {
		return result
	}
	reference := lastSuccess
	if reference.IsZero() {
		anchor, ok := s.lastSlot()
		if !ok {
			return result
		}
		reference, result.Never = anchor, true
	}
	if now.Before(reference) {
		return result // the clock moved back
	}
	result.Age = now.Sub(reference)
	result.Stale = result.Age > 2*interval
	return result
}

// vaulticFormatAge writes an age in French, coarsely.
func vaulticFormatAge(d time.Duration) string {
	switch {
	case d >= 48*time.Hour:
		return fmt.Sprintf(vaulticT("%d jours"), int(d/(24*time.Hour)))
	case d >= 2*time.Hour:
		return fmt.Sprintf(vaulticT("%d heures"), int(d/time.Hour))
	}
	return fmt.Sprintf(vaulticT("%d minutes"), max(int(d/time.Minute), 1))
}

// vaulticStaleMessage is the warning shown for a stale schedule.
func vaulticStaleMessage(profile string, st vaulticStaleness) string {
	if st.Never {
		return fmt.Sprintf(vaulticT("Aucune sauvegarde réussie du profil « %s » depuis %s, alors qu'une planification est active."), profile, vaulticFormatAge(st.Age))
	}
	return fmt.Sprintf(vaulticT("La dernière sauvegarde réussie du profil « %s » date d'il y a %s : plus de deux fois l'intervalle prévu."), profile, vaulticFormatAge(st.Age))
}
