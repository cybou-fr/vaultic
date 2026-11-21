//go:build linux

package main

import (
	"bytes"
	"context"
	"errors"
	"os/exec"
	"strings"
	"time"
)

// libsecret through the secret-tool program (GNOME Keyring, KWallet's Secret
// Service). The secret travels on standard input, never as an argument.

type vaulticLinuxSecretStore struct{}

func newVaulticSystemSecretStore() vaulticSecretStore { return vaulticLinuxSecretStore{} }

func (vaulticLinuxSecretStore) Name() string { return "Trousseau libsecret" }

func (vaulticLinuxSecretStore) Available() bool {
	_, err := exec.LookPath("secret-tool")
	return err == nil
}

func runSecretTool(stdin string, args []string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "secret-tool", args...)
	if stdin != "" {
		cmd.Stdin = strings.NewReader(stdin)
	}
	var out bytes.Buffer
	cmd.Stdout = &out
	if err := cmd.Run(); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

func (vaulticLinuxSecretStore) Get(key string) (string, error) {
	out, err := runSecretTool("", vaulticSecretToolArgs("lookup", key))
	if err != nil || len(out) == 0 {
		var exit *exec.ExitError
		if err == nil || errors.As(err, &exit) {
			return "", errSecretNotFound
		}
		return "", err
	}
	return string(out), nil
}

func (vaulticLinuxSecretStore) Set(key, secret string) error {
	_, err := runSecretTool(secret, vaulticSecretToolArgs("store", key))
	return err
}

func (vaulticLinuxSecretStore) Delete(key string) error {
	_, err := runSecretTool("", vaulticSecretToolArgs("clear", key))
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		return nil // nothing to clear
	}
	return err
}
