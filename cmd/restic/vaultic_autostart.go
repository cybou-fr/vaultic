package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Starting Vaultic with the user's session is a per-user entry that the user
// can see and remove in the system's own startup list. It is not a service
// and not a task of the operating system's scheduler: Vaultic itself keeps
// evaluating schedules while it runs, minimised to the notification area.

// vaulticBackgroundFlag makes Vaultic start without showing its window.
const vaulticBackgroundFlag = "--background"

// vaulticAutostart reads and changes the per-user startup entry.
type vaulticAutostart interface {
	Name() string
	Enabled() (bool, error)
	Set(enabled bool, executable string) error
}

// vaulticAutostartCommand is the command line stored in the startup entry.
func vaulticAutostartCommand(executable string) string {
	return `"` + executable + `" ` + vaulticBackgroundFlag
}

// vaulticDesktopEntry is the XDG autostart file used on Linux.
func vaulticDesktopEntry(executable string) string {
	return "[Desktop Entry]\nType=Application\nName=Vaultic\nComment=Sauvegardes chiffrées\n" +
		"Exec=" + strings.ReplaceAll(executable, " ", `\ `) + " " + vaulticBackgroundFlag + "\n" +
		"X-GNOME-Autostart-enabled=true\nTerminal=false\n"
}

// vaulticLaunchAgent is the property list used on macOS.
func vaulticLaunchAgent(executable string) string {
	return `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>Label</key><string>fr.cybou.vaultic</string>
	<key>ProgramArguments</key>
	<array><string>` + executable + `</string><string>` + vaulticBackgroundFlag + `</string></array>
	<key>RunAtLoad</key><true/>
</dict>
</plist>
`
}

// vaulticFileAutostart keeps the startup entry as a file (Linux, macOS).
type vaulticFileAutostart struct {
	label   string
	path    string
	content func(executable string) string
}

func (a vaulticFileAutostart) Name() string { return a.label }

func (a vaulticFileAutostart) Enabled() (bool, error) {
	_, err := os.Stat(a.path)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	return err == nil, err
}

func (a vaulticFileAutostart) Set(enabled bool, executable string) error {
	if !enabled {
		if err := os.Remove(a.path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		return nil
	}
	if executable == "" {
		return errors.New("chemin de l'exécutable inconnu")
	}
	if err := os.MkdirAll(filepath.Dir(a.path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(a.path, []byte(a.content(executable)), 0o644)
}

// vaulticAutostartFor returns the file based startup entry of an OS, rooted
// in home. Windows uses the registry instead (see the Windows file).
func vaulticAutostartFor(goos, home string) (vaulticAutostart, error) {
	switch goos {
	case "linux":
		return vaulticFileAutostart{label: "Démarrage automatique de la session (XDG)", path: filepath.Join(home, ".config", "autostart", "vaultic.desktop"), content: vaulticDesktopEntry}, nil
	case "darwin":
		return vaulticFileAutostart{label: "Éléments d'ouverture de la session (LaunchAgent)", path: filepath.Join(home, "Library", "LaunchAgents", "fr.cybou.vaultic.plist"), content: vaulticLaunchAgent}, nil
	}
	return nil, fmt.Errorf("démarrage automatique non pris en charge sur %s", goos)
}
