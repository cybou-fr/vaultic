//go:build !windows

package main

import (
	"errors"
	"os"
	"runtime"
)

func newVaulticSystemAutostart() vaulticAutostart {
	home, err := os.UserHomeDir()
	if err == nil {
		if a, err := vaulticAutostartFor(runtime.GOOS, home); err == nil {
			return a
		}
	}
	return vaulticNoAutostart{}
}

type vaulticNoAutostart struct{}

func (vaulticNoAutostart) Name() string           { return vaulticT("Démarrage automatique indisponible") }
func (vaulticNoAutostart) Enabled() (bool, error) { return false, nil }
func (vaulticNoAutostart) Set(bool, string) error {
	return errors.New(vaulticT("démarrage automatique non pris en charge ici"))
}
