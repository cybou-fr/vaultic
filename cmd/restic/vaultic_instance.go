package main

import (
	"crypto/sha256"
	"encoding/hex"
	"path/filepath"
	"runtime"
	"strings"
)

// Only one Vaultic may run per settings file. Two instances would each run a
// scheduler against the same repositories and race to write the same
// settings. The second one asks the first to show its window and exits.

// vaulticInstance is the guard held by the running instance.
type vaulticInstance interface {
	// OnShow registers what to do when another launch asks this instance to
	// show itself (it may be called from any goroutine).
	OnShow(show func())
	// Release gives the guard back.
	Release()
}

// vaulticInstanceKey names the guard after the settings file, so a portable
// copy with its own settings folder is independent of an installed one.
func vaulticInstanceKey(settingsPath string) string {
	path := filepath.Clean(settingsPath)
	if abs, err := filepath.Abs(path); err == nil {
		path = abs
	}
	if runtime.GOOS == "windows" {
		path = strings.ToLower(path)
	}
	sum := sha256.Sum256([]byte(path))
	return hex.EncodeToString(sum[:])[:16]
}
