package main

import (
	"errors"
	"fmt"
	"time"
)

// vaulticApplySchedule returns a copy of profiles in which the profile at index
// carries the given schedule. It is pure: the caller persists the result.
//
// The active repository becomes the schedule's explicit destination, so
// switching repositories later never silently redirects scheduled backups. An
// unchanged schedule keeps its slot; a changed one starts counting from now
// instead of replaying an old destination's schedule.
func vaulticApplySchedule(profiles []vaulticBackupProfile, index int, schedule vaulticSchedule, activeRepository string, now time.Time) ([]vaulticBackupProfile, error) {
	if index < 0 || index >= len(profiles) {
		return nil, errors.New("Profil introuvable.")
	}
	next := append([]vaulticBackupProfile(nil), profiles...)
	old := next[index].Schedule
	if schedule.Kind == vaulticScheduleManual {
		next[index].Schedule = nil
		return next, nil
	}
	if activeRepository != "" {
		schedule.Repository = activeRepository
	} else if old != nil {
		schedule.Repository = old.Repository
	}
	if schedule.Enabled && schedule.Repository == "" {
		return nil, errors.New("Sélectionnez un dépôt dans Dépôts avant d'activer la planification.")
	}
	if err := vaulticValidateDepot(schedule.Repository); schedule.Enabled && err != nil {
		return nil, errors.New("Le dépôt de destination est invalide.")
	}
	if old != nil && old.Repository == schedule.Repository && old.Kind == schedule.Kind &&
		old.Enabled == schedule.Enabled && old.EveryHours == schedule.EveryHours &&
		old.Time == schedule.Time && old.MonthDay == schedule.MonthDay && fmt.Sprint(old.Weekdays) == fmt.Sprint(schedule.Weekdays) {
		schedule.LastSlot = old.LastSlot
	} else if schedule.Enabled {
		schedule.LastSlot = now.Format(time.RFC3339)
	}
	next[index].Schedule = &schedule
	return next, nil
}
