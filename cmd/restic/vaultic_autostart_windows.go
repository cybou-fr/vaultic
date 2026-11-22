//go:build windows

package main

import (
	"errors"

	"golang.org/x/sys/windows/registry"
)

const (
	vaulticRunKeyPath = `Software\Microsoft\Windows\CurrentVersion\Run`
	vaulticRunValue   = "Vaultic"
)

// vaulticRegistryAutostart uses the per-user Run key: visible in Windows'
// "Applications de démarrage" settings, removable there, and needing no
// administrator rights.
type vaulticRegistryAutostart struct{ value string }

func newVaulticSystemAutostart() vaulticAutostart {
	return vaulticRegistryAutostart{value: vaulticRunValue}
}

func (vaulticRegistryAutostart) Name() string {
	return vaulticT("Applications de démarrage de Windows")
}

func (a vaulticRegistryAutostart) Enabled() (bool, error) {
	key, err := registry.OpenKey(registry.CURRENT_USER, vaulticRunKeyPath, registry.QUERY_VALUE)
	if err != nil {
		if errors.Is(err, registry.ErrNotExist) {
			return false, nil
		}
		return false, err
	}
	defer key.Close()
	if _, _, err := key.GetStringValue(a.value); err != nil {
		if errors.Is(err, registry.ErrNotExist) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

func (a vaulticRegistryAutostart) Set(enabled bool, executable string) error {
	key, _, err := registry.CreateKey(registry.CURRENT_USER, vaulticRunKeyPath, registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer key.Close()
	if !enabled {
		if err := key.DeleteValue(a.value); err != nil && !errors.Is(err, registry.ErrNotExist) {
			return err
		}
		return nil
	}
	if executable == "" {
		return errors.New(vaulticT("chemin de l'exécutable inconnu"))
	}
	return key.SetStringValue(a.value, vaulticAutostartCommand(executable))
}
