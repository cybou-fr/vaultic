package main

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
	"sync"
)

// vaulticSecretStore keeps a repository password in the operating system's
// credential store, only when the user asked for it. Nothing here writes to
// Vaultic's own settings: the settings file records only the non-secret fact
// that a password was remembered.
type vaulticSecretStore interface {
	// Name describes the store for the interface ("Gestionnaire d'identifiants
	// Windows", "Trousseau libsecret").
	Name() string
	// Available reports whether the store can be used on this computer.
	Available() bool
	// Get returns the secret; errSecretNotFound when none is stored.
	Get(key string) (string, error)
	Set(key, secret string) error
	// Delete removes the secret; deleting a missing secret is not an error.
	Delete(key string) error
}

var (
	errSecretNotFound    = errors.New("aucun mot de passe mémorisé pour ce dépôt")
	errSecretUnavailable = errors.New("le magasin d'identifiants du système n'est pas disponible")
)

// vaulticSecretKey derives the credential's name from the repository address.
// A hash keeps addresses (and anything that might be in them) out of the
// system's credential list while staying stable between runs.
func vaulticSecretKey(location string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(location)))
	return "Vaultic:" + hex.EncodeToString(sum[:])[:24]
}

// vaulticSecretLabel is the readable label shown next to the credential.
const vaulticSecretLabel = "Vaultic — mot de passe d'un dépôt"

// vaulticMemorySecretStore is an in-memory store for tests and as a stand-in
// where the system offers none. It never persists anything.
type vaulticMemorySecretStore struct {
	mu          sync.Mutex
	secrets     map[string]string
	unavailable bool
	failSet     error
}

func newVaulticMemorySecretStore() *vaulticMemorySecretStore {
	return &vaulticMemorySecretStore{secrets: map[string]string{}}
}

func (m *vaulticMemorySecretStore) Name() string    { return "Mémoire de test" }
func (m *vaulticMemorySecretStore) Available() bool { return !m.unavailable }

func (m *vaulticMemorySecretStore) Get(key string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.unavailable {
		return "", errSecretUnavailable
	}
	value, ok := m.secrets[key]
	if !ok {
		return "", errSecretNotFound
	}
	return value, nil
}

func (m *vaulticMemorySecretStore) Set(key, secret string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.unavailable {
		return errSecretUnavailable
	}
	if m.failSet != nil {
		return m.failSet
	}
	m.secrets[key] = secret
	return nil
}

func (m *vaulticMemorySecretStore) Delete(key string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.unavailable {
		return errSecretUnavailable
	}
	delete(m.secrets, key)
	return nil
}

// vaulticSecretToolArgs builds the arguments of libsecret's secret-tool for
// an operation. The secret itself is never an argument: it is written to the
// command's standard input, so it cannot appear in a process list.
func vaulticSecretToolArgs(operation, key string) []string {
	switch operation {
	case "store":
		return []string{"store", "--label=" + vaulticSecretLabel, "service", "vaultic", "account", key}
	case "lookup":
		return []string{"lookup", "service", "vaultic", "account", key}
	case "clear":
		return []string{"clear", "service", "vaultic", "account", key}
	}
	return nil
}
