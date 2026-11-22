package main

import "path/filepath"

// vaulticPrepareLoadedSettings normalises settings right after loading.
//
// Schedules created before destinations existed targeted the active
// repository implicitly; that destination is pinned once so later changes of
// the active repository never silently redirect scheduled backups. Relative
// sources are resolved against the application folder.
func vaulticPrepareLoadedSettings(s *vaulticSettings) {
	for i := range s.Profiles {
		if schedule := s.Profiles[i].Schedule; schedule != nil && schedule.Repository == "" {
			schedule.Repository = s.Repository
		}
	}
	for _, profile := range s.Profiles {
		for i, source := range profile.Sources {
			if !filepath.IsAbs(source) {
				profile.Sources[i] = vaulticResolvedRepository(source)
			}
		}
	}
}

// vaulticSettingsForSave builds what is written to disk: portable source
// paths, copies of profiles and schedules, and no credentials (settings never
// hold any). profileIndex selects the active profile.
func vaulticSettingsForSave(saved vaulticSettings, depots []vaulticDepot, active string, profileIndex int) vaulticSettings {
	stored := make([]vaulticBackupProfile, len(saved.Profiles))
	for i, profile := range saved.Profiles {
		storedSources := make([]string, len(profile.Sources))
		for j, source := range profile.Sources {
			storedSources[j] = vaulticPortablePath(source)
		}
		stored[i] = vaulticBackupProfile{
			Name: profile.Name, Sources: storedSources, Excludes: append([]string(nil), profile.Excludes...),
			Schedule: vaulticCloneSchedule(profile.Schedule), Retention: vaulticCloneRetention(profile.Retention),
		}
	}
	return vaulticSettings{
		Repository: active, Depots: depots, Sources: stored[profileIndex].Sources,
		Profiles: stored, ActiveProfile: stored[profileIndex].Name,
		SchedulerPaused: saved.SchedulerPaused, Notifications: saved.Notifications,
		CloseToTray: saved.CloseToTray,
	}
}
