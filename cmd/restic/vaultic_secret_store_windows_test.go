//go:build windows

package main

import (
	"errors"
	"testing"
)

// The real Windows Credential Manager. The credential uses a name that cannot
// collide with Vaultic's own and is removed at the end.
func TestVaulticWindowsCredentialManagerRoundTrip(t *testing.T) {
	s := newVaulticSystemSecretStore()
	if !s.Available() {
		t.Skip("Windows Credential Manager is not available")
	}
	key := "Vaultic-test:" + t.Name()
	t.Cleanup(func() { _ = s.Delete(key) })

	if _, err := s.Get(key); !errors.Is(err, errSecretNotFound) {
		t.Fatalf("a new credential must not exist: %v", err)
	}
	secret := "mot de passe é ☕ Пароль 123"
	if err := s.Set(key, secret); err != nil {
		t.Fatalf("set: %v", err)
	}
	if got, err := s.Get(key); err != nil || got != secret {
		t.Fatalf("get: %q %v", got, err)
	}
	if err := s.Set(key, "autre"); err != nil {
		t.Fatalf("overwrite: %v", err)
	}
	if got, _ := s.Get(key); got != "autre" {
		t.Fatalf("overwrite did not replace the secret: %q", got)
	}
	if err := s.Delete(key); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := s.Get(key); !errors.Is(err, errSecretNotFound) {
		t.Fatalf("the credential must be gone: %v", err)
	}
	if err := s.Delete(key); err != nil {
		t.Fatalf("deleting twice: %v", err)
	}
}
