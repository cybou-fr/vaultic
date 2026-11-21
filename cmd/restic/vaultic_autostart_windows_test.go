//go:build windows

package main

import (
	"strings"
	"testing"

	"golang.org/x/sys/windows/registry"
)

// The real per-user Run key, with a value name that cannot collide with
// Vaultic's own. The value is removed at the end.
func TestVaulticWindowsRegistryAutostartRoundTrip(t *testing.T) {
	a := vaulticRegistryAutostart{value: "Vaultic-test-" + strings.ReplaceAll(t.Name(), "/", "-")}
	t.Cleanup(func() { _ = a.Set(false, "") })

	if on, err := a.Enabled(); err != nil || on {
		t.Fatalf("a new entry must not exist: %v %v", on, err)
	}
	if err := a.Set(true, ""); err == nil {
		t.Fatal("an empty executable must be refused")
	}
	exe := `C:\Program Files\Vaultic Test\vaultic.exe`
	if err := a.Set(true, exe); err != nil {
		t.Fatalf("set: %v", err)
	}
	if on, err := a.Enabled(); err != nil || !on {
		t.Fatalf("entry not created: %v %v", on, err)
	}
	key, err := registry.OpenKey(registry.CURRENT_USER, vaulticRunKeyPath, registry.QUERY_VALUE)
	if err != nil {
		t.Fatal(err)
	}
	defer key.Close()
	got, _, err := key.GetStringValue(a.value)
	if err != nil || got != `"`+exe+`" --background` {
		t.Fatalf("stored command: %q %v", got, err)
	}
	if err := a.Set(false, ""); err != nil {
		t.Fatalf("remove: %v", err)
	}
	if on, _ := a.Enabled(); on {
		t.Fatal("entry not removed")
	}
	if err := a.Set(false, ""); err != nil {
		t.Fatalf("removing twice: %v", err)
	}
}
