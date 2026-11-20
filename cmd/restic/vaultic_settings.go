package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/restic/restic/internal/backend/all"
	"github.com/restic/restic/internal/backend/location"
)

type vaulticDepot struct {
	Name     string `json:"name"`
	Location string `json:"location"`
	// Non-secret connection history, shown in the repository list.
	LastVerified      string `json:"last_verified,omitempty"` // RFC 3339, UTC
	LastVerifiedState string `json:"last_verified_state,omitempty"`
}

type vaulticSettings struct {
	Repository    string                 `json:"repository"`
	Sources       []string               `json:"sources"`
	Depots        []vaulticDepot         `json:"depots,omitempty"`
	Profiles      []vaulticBackupProfile `json:"backup_profiles,omitempty"`
	ActiveProfile string                 `json:"active_backup_profile,omitempty"`
	// Scheduling is paused as a whole (kept across restarts) and notification
	// categories are chosen by the user. Neither contains credentials.
	SchedulerPaused bool                `json:"scheduler_paused,omitempty"`
	Notifications   *vaulticNotifyPrefs `json:"notifications,omitempty"`
}

type vaulticBackupProfile struct {
	Name     string           `json:"name"`
	Sources  []string         `json:"sources"`
	Excludes []string         `json:"excludes,omitempty"`
	Schedule *vaulticSchedule `json:"schedule,omitempty"`
}

func (s *vaulticSettings) migrateBackupProfiles() int {
	if len(s.Profiles) == 0 {
		s.Profiles = []vaulticBackupProfile{{Name: "Par défaut", Sources: s.Sources}}
	}
	for i, profile := range s.Profiles {
		if profile.Name == s.ActiveProfile {
			return i
		}
	}
	return 0
}

func vaulticValidateDepot(value string) error {
	if strings.TrimSpace(strings.TrimPrefix(value, "local:")) == "" {
		return fmt.Errorf("indiquez l’emplacement du dépôt")
	}
	if !vaulticLocalRepository(value) && !strings.HasPrefix(value, "local:") {
		scheme, endpoint, _ := strings.Cut(value, ":")
		if strings.ContainsAny(endpoint, "?#") {
			return fmt.Errorf("retirez les paramètres et jetons de l’adresse du dépôt")
		}
		if strings.HasPrefix(endpoint, "//") {
			endpoint = scheme + ":" + endpoint
		}
		if strings.Contains(endpoint, "://") {
			parsed, err := url.Parse(endpoint)
			if err != nil {
				return fmt.Errorf("utilisez une adresse sans identifiants intégrés")
			}
			if _, hasPassword := parsed.User.Password(); parsed.User != nil && (scheme != "sftp" || hasPassword) {
				return fmt.Errorf("utilisez une adresse sans identifiants intégrés")
			}
			if scheme == "rest" && (parsed.Host == "" || (parsed.Scheme != "https" && parsed.Scheme != "http")) {
				return fmt.Errorf("indiquez une adresse REST HTTP ou HTTPS valide")
			}
		} else if scheme == "rest" {
			return fmt.Errorf("indiquez une adresse REST HTTP ou HTTPS valide")
		} else if host, _, _ := strings.Cut(endpoint, "/"); scheme != "sftp" && strings.Contains(host, "@") {
			return fmt.Errorf("utilisez une adresse sans identifiants intégrés")
		}
	}
	if _, err := location.Parse(all.Backends(), value); err != nil {
		return fmt.Errorf("emplacement ou type de dépôt non valide")
	}
	return nil
}

func vaulticPortablePath(path string) string {
	exe, err := os.Executable()
	if err != nil || !filepath.IsAbs(path) {
		return path
	}
	base := filepath.Dir(exe)
	if _, err := os.Stat(filepath.Join(base, "portable.flag")); err != nil {
		return path
	}
	if relative, err := filepath.Rel(base, path); err == nil && vaulticContained(base, path) {
		return relative
	}
	return path
}

func (s *vaulticSettings) migrateDepots() {
	if len(s.Depots) == 0 && vaulticValidateDepot(s.Repository) == nil {
		s.Depots = []vaulticDepot{{Name: "Dépôt existant", Location: s.Repository}}
	}
	active := ""
	valid := s.Depots[:0]
	for _, depot := range s.Depots {
		if depot.Name == "" || vaulticValidateDepot(depot.Location) != nil {
			continue
		}
		valid = append(valid, depot)
		if depot.Location == s.Repository {
			active = depot.Location
		}
	}
	s.Depots = valid
	if active == "" && len(valid) > 0 {
		active = valid[0].Location
	}
	s.Repository = active
}

func vaulticSettingsPath() string {
	if exe, err := os.Executable(); err == nil {
		dir := filepath.Dir(exe)
		if _, err := os.Stat(filepath.Join(dir, "portable.flag")); err == nil {
			return filepath.Join(dir, "vaultic.json")
		}
	}
	dir, err := os.UserConfigDir()
	if err != nil {
		dir = "."
	}
	return filepath.Join(dir, "Vaultic", "settings.json")
}

func vaulticLoadSettings() (vaulticSettings, error) {
	var s vaulticSettings
	path := vaulticSettingsPath()
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return s, nil
	}
	if err != nil {
		return s, err
	}
	if !info.Mode().IsRegular() {
		return s, errors.New("Le fichier de préférences doit être un fichier ordinaire")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return s, err
	}
	if err := json.Unmarshal(data, &s); err != nil {
		return vaulticSettings{}, fmt.Errorf("Préférences illisibles : %w", err)
	}
	s.migrateDepots()
	return s, nil
}

func vaulticSaveSettings(s vaulticSettings) error {
	path := vaulticSettingsPath()
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	if info, err := os.Lstat(path); err == nil {
		if !info.Mode().IsRegular() {
			return errors.New("Le fichier de préférences doit être un fichier ordinaire")
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if strings.TrimSpace(s.Repository) == "" {
		s.Repository = ""
	}
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".vaultic-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if err = f.Chmod(0600); err != nil {
		_ = f.Close()
		return err
	}
	if _, err = f.Write(data); err != nil {
		_ = f.Close()
		return err
	}
	if err = f.Sync(); err != nil {
		_ = f.Close()
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	return os.Rename(f.Name(), path)
}
