//go:build !windows && !linux

package main

// No system credential store is wired for this platform yet (macOS Keychain
// is planned). Vaultic keeps working exactly as before: the password is typed
// and kept in memory only.

type vaulticNoSecretStore struct{}

func newVaulticSystemSecretStore() vaulticSecretStore { return vaulticNoSecretStore{} }

func (vaulticNoSecretStore) Name() string               { return "Aucun magasin d'identifiants" }
func (vaulticNoSecretStore) Available() bool            { return false }
func (vaulticNoSecretStore) Get(string) (string, error) { return "", errSecretUnavailable }
func (vaulticNoSecretStore) Set(string, string) error   { return errSecretUnavailable }
func (vaulticNoSecretStore) Delete(string) error        { return errSecretUnavailable }
