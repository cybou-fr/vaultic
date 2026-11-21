package main

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func TestVaulticRememberPasswordRequiresAPasswordThenStoresItSafely(t *testing.T) {
	f := newReposFixture(t)
	f.c.Refresh()
	f.c.Remember.SetChecked(true) // no password typed yet
	if f.c.Remember.Checked || len(f.store.secrets) != 0 {
		t.Fatal("remembering without a password must be refused")
	}
	f.c.Password.SetText("s3cret-é-Пароль")
	f.c.Remember.SetChecked(true)
	key := vaulticSecretKey("/data/repo-a")
	if got, err := f.store.Get(key); err != nil || got != "s3cret-é-Пароль" {
		t.Fatalf("the password must be in the credential store: %q %v", got, err)
	}
	depot, _ := vaulticFindSavedDepot(f.settings.Depots, "/data/repo-a")
	if !depot.RememberPassword || !f.c.Remember.Checked {
		t.Fatal("the choice must be recorded and shown")
	}
	// The settings file only gets the flag, never the password.
	raw, _ := json.Marshal(f.settings)
	if strings.Contains(string(raw), "s3cret") || !strings.Contains(string(raw), `"remember_password":true`) {
		t.Fatalf("settings JSON: %s", raw)
	}
	for _, line := range f.logs {
		if strings.Contains(line, "s3cret") {
			t.Fatalf("the password leaked into the log: %q", line)
		}
	}
	f.c.Remember.SetChecked(false)
	if _, err := f.store.Get(key); !errors.Is(err, errSecretNotFound) {
		t.Fatal("unticking must erase the stored password")
	}
	if depot, _ := vaulticFindSavedDepot(f.settings.Depots, "/data/repo-a"); depot.RememberPassword {
		t.Fatal("the flag must be cleared")
	}
}

func TestVaulticRememberedPasswordIsLoadedOnActivationOnly(t *testing.T) {
	f := newReposFixture(t)
	f.settings.Depots[1].RememberPassword = true
	_ = f.store.Set(vaulticSecretKey("/data/repo-b"), "remembered")
	f.c.Activate("/data/repo-b", "")
	if f.c.Password.Text != "remembered" {
		t.Fatalf("remembered password not loaded: %q", f.c.Password.Text)
	}
	if !f.c.Remember.Checked {
		t.Fatal("the check box must reflect the choice")
	}
	f.c.Activate("/data/repo-b", "typed")
	if f.c.Password.Text != "typed" {
		t.Fatal("a typed password must not be replaced by the stored one")
	}
	f.c.Activate("/data/repo-a", "")
	if f.c.Password.Text != "" {
		t.Fatal("a repository without the choice must start with an empty password")
	}
}

func TestVaulticRememberedPasswordMissingFromTheStoreClearsTheChoice(t *testing.T) {
	f := newReposFixture(t)
	f.settings.Depots[0].RememberPassword = true // flag set, credential deleted by the user
	f.c.LoadRemembered()
	if f.c.Password.Text != "" || f.settings.Depots[0].RememberPassword || f.c.Remember.Checked {
		t.Fatal("a vanished credential must clear the choice")
	}
	if len(f.logs) == 0 || !strings.Contains(f.logs[len(f.logs)-1], "n'existe plus") {
		t.Fatalf("the user must be told: %v", f.logs)
	}
}

func TestVaulticRememberPasswordFailuresLeaveNothingHalfDone(t *testing.T) {
	f := newReposFixture(t)
	f.c.Password.SetText("pw")
	f.store.failSet = errors.New("accès refusé")
	f.c.Remember.SetChecked(true)
	if f.c.Remember.Checked || f.settings.Depots[0].RememberPassword || len(f.store.secrets) != 0 {
		t.Fatal("a failed store write must not record the choice")
	}
	f.store.failSet = nil
	f.failPersist = errors.New("disque plein")
	f.c.Remember.SetChecked(true)
	if f.c.Remember.Checked || len(f.store.secrets) != 0 {
		t.Fatal("when the flag cannot be saved the secret must be removed again")
	}
}

func TestVaulticUnavailableCredentialStoreKeepsTheOldBehaviour(t *testing.T) {
	f := newReposFixture(t)
	f.store.unavailable = true
	f.c.Refresh()
	f.c.UpdateState(false)
	if !f.c.Remember.Disabled() {
		t.Fatal("no credential store: the option must be disabled")
	}
	f.settings.Depots[0].RememberPassword = true
	f.c.LoadRemembered()
	if f.c.Password.Text != "" || len(f.logs) == 0 {
		t.Fatal("an unavailable store leaves the password empty and says so")
	}
}

func TestVaulticForgettingARepositoryAndEditingItsAddressKeepSecretsConsistent(t *testing.T) {
	f := newReposFixture(t)
	f.settings.Depots[0].RememberPassword = true
	_ = f.store.Set(vaulticSecretKey("/data/repo-a"), "pw-a")
	f.c.moveRemembered("/data/repo-a", "/data/repo-a-moved")
	if _, err := f.store.Get(vaulticSecretKey("/data/repo-a")); !errors.Is(err, errSecretNotFound) {
		t.Fatal("the old credential must be removed")
	}
	if got, err := f.store.Get(vaulticSecretKey("/data/repo-a-moved")); err != nil || got != "pw-a" {
		t.Fatalf("the password must follow the repository: %q %v", got, err)
	}
	// Oublier: confirmation dialogs are UI; the secret removal is the contract.
	f.settings.Depots[1].RememberPassword = true
	_ = f.store.Set(vaulticSecretKey("/data/repo-b"), "pw-b")
	f.c.forget(f.settings.Depots[1])
}
