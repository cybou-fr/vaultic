//go:build windows

package main

import (
	"errors"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Windows Credential Manager through advapi32. Credentials are generic,
// persisted for this user on this computer only (never roamed), and encrypted
// by Windows with the user's logon secret.

var (
	advapi32       = windows.NewLazySystemDLL("advapi32.dll")
	procCredWrite  = advapi32.NewProc("CredWriteW")
	procCredRead   = advapi32.NewProc("CredReadW")
	procCredDelete = advapi32.NewProc("CredDeleteW")
	procCredFree   = advapi32.NewProc("CredFree")
)

const (
	credTypeGeneric         = 1
	credPersistLocalMachine = 2
	errorNotFound           = syscall.Errno(1168) // ERROR_NOT_FOUND
)

// credential mirrors the CREDENTIALW structure.
type credential struct {
	Flags              uint32
	Type               uint32
	TargetName         *uint16
	Comment            *uint16
	LastWritten        windows.Filetime
	CredentialBlobSize uint32
	CredentialBlob     *byte
	Persist            uint32
	AttributeCount     uint32
	Attributes         uintptr
	TargetAlias        *uint16
	UserName           *uint16
}

type vaulticWindowsSecretStore struct{}

func newVaulticSystemSecretStore() vaulticSecretStore { return vaulticWindowsSecretStore{} }

func (vaulticWindowsSecretStore) Name() string {
	return vaulticT("Gestionnaire d'identifiants Windows")
}

func (vaulticWindowsSecretStore) Available() bool {
	return procCredWrite.Find() == nil && procCredRead.Find() == nil && procCredDelete.Find() == nil
}

func (vaulticWindowsSecretStore) Get(key string) (string, error) {
	target, err := windows.UTF16PtrFromString(key)
	if err != nil {
		return "", err
	}
	var cred *credential
	r, _, callErr := procCredRead.Call(uintptr(unsafe.Pointer(target)), credTypeGeneric, 0, uintptr(unsafe.Pointer(&cred)))
	if r == 0 {
		if errors.Is(callErr, errorNotFound) {
			return "", errSecretNotFound
		}
		return "", callErr
	}
	defer procCredFree.Call(uintptr(unsafe.Pointer(cred)))
	blob := unsafe.Slice(cred.CredentialBlob, cred.CredentialBlobSize)
	return string(append([]byte(nil), blob...)), nil
}

func (vaulticWindowsSecretStore) Set(key, secret string) error {
	target, err := windows.UTF16PtrFromString(key)
	if err != nil {
		return err
	}
	comment, _ := windows.UTF16PtrFromString(vaulticSecretLabel)
	user, _ := windows.UTF16PtrFromString("Vaultic")
	blob := []byte(secret)
	cred := credential{
		Type: credTypeGeneric, TargetName: target, Comment: comment, UserName: user,
		CredentialBlobSize: uint32(len(blob)), Persist: credPersistLocalMachine,
	}
	if len(blob) > 0 {
		cred.CredentialBlob = &blob[0]
	}
	r, _, callErr := procCredWrite.Call(uintptr(unsafe.Pointer(&cred)), 0)
	for i := range blob { // do not leave a second copy of the secret around
		blob[i] = 0
	}
	if r == 0 {
		return callErr
	}
	return nil
}

func (vaulticWindowsSecretStore) Delete(key string) error {
	target, err := windows.UTF16PtrFromString(key)
	if err != nil {
		return err
	}
	r, _, callErr := procCredDelete.Call(uintptr(unsafe.Pointer(target)), credTypeGeneric, 0)
	if r == 0 && !errors.Is(callErr, errorNotFound) {
		return callErr
	}
	return nil
}
