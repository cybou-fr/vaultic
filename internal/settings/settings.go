// Package settings manages portable non-secret preferences.
package settings

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

type Settings struct {
	ResticBinary string   `json:"restic_binary"`
	Repository   string   `json:"repository"`
	Sources      []string `json:"sources"`
}

// File lives near the executable in portable mode (next to portable.flag).
// Otherwise it lives in the OS user config directory.
func Path() string {
	exe, err := os.Executable()
	if err == nil {
		home := filepath.Dir(exe)
		if _, err := os.Stat(filepath.Join(home, "portable.flag")); err == nil {
			return filepath.Join(home, "vaultic.json")
		}
	}
	dir, err := os.UserConfigDir()
	if err != nil {
		dir = "."
	}
	return filepath.Join(dir, "Vaultic", "settings.json")
}

func Load() Settings {
	s := Settings{ResticBinary: "restic"}
	data, err := os.ReadFile(Path())
	if err == nil {
		_ = json.Unmarshal(data, &s)
	}
	if strings.TrimSpace(s.ResticBinary) == "" {
		s.ResticBinary = "restic"
	}
	return s
}

func Save(s Settings) error {
	path := Path()
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	// Temp + rename avoids truncated settings on an interrupted write.
	tmp, err := os.CreateTemp(filepath.Dir(path), ".settings-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if err := tmp.Chmod(0600); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}
