package main

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/restic/restic/internal/global"
	"github.com/restic/restic/internal/repository"
)

// vaulticConnState is the explicit lifecycle of a repository connection.
// "Reachable" only means the storage answered and a restic repository exists
// there; it is never reported as connected until the repository password has
// been accepted by the engine.
type vaulticConnState int

const (
	vaulticConnNotTested vaulticConnState = iota
	vaulticConnTesting
	vaulticConnReachable
	vaulticConnAuthFailed
	vaulticConnUnavailable
	vaulticConnConnected
)

func (s vaulticConnState) Label() string {
	switch s {
	case vaulticConnTesting:
		return "Test en cours…"
	case vaulticConnReachable:
		return "Joignable"
	case vaulticConnAuthFailed:
		return "Authentification refusée"
	case vaulticConnUnavailable:
		return "Dépôt indisponible"
	case vaulticConnConnected:
		return "Connecté"
	}
	return "Non testé"
}

func (s vaulticConnState) Key() string {
	return [...]string{"not_tested", "testing", "reachable", "auth_failed", "unavailable", "connected"}[s]
}

func vaulticConnStateFromKey(key string) vaulticConnState {
	for s := vaulticConnNotTested; s <= vaulticConnConnected; s++ {
		if s.Key() == key {
			return s
		}
	}
	return vaulticConnNotTested
}

type vaulticProbeResult struct {
	State  vaulticConnState
	Detail string
}

const (
	vaulticPasswordRequiredMessage = "Le mot de passe doit être fourni dans l'interface Vaultic"
	vaulticProbeTimeout            = 45 * time.Second
)

var vaulticAuthFailureHints = []string{
	"access denied", "accessdenied", "403", "401", "unauthorized", "forbidden", "invalidaccesskeyid",
	"signaturedoesnotmatch", "authentication failed", "authorizationfailure", "permission denied (publickey",
	"invalid credentials", "bad credentials", "unable to authenticate", "no valid credential",
}

// vaulticClassifyProbe maps the engine's outcome to a lifecycle state.
// passwordGiven tells whether the restic password was part of the attempt.
func vaulticClassifyProbe(err error, passwordGiven bool) vaulticProbeResult {
	switch {
	case err == nil && passwordGiven:
		return vaulticProbeResult{vaulticConnConnected, "Dépôt ouvert et mot de passe accepté."}
	case err == nil:
		return vaulticProbeResult{vaulticConnReachable, "Dépôt joignable."}
	case errors.Is(err, context.Canceled):
		return vaulticProbeResult{vaulticConnNotTested, "Test annulé."}
	case errors.Is(err, context.DeadlineExceeded):
		return vaulticProbeResult{vaulticConnUnavailable, "Délai dépassé : le stockage ne répond pas."}
	case errors.Is(err, repository.ErrNoKeyFound):
		return vaulticProbeResult{vaulticConnAuthFailed, "Dépôt joignable, mais le mot de passe restic est incorrect."}
	case errors.Is(err, global.ErrNoRepository):
		return vaulticProbeResult{vaulticConnUnavailable, "Stockage joignable, mais aucun dépôt restic à cet emplacement."}
	}
	message := err.Error()
	if !passwordGiven && strings.Contains(message, vaulticPasswordRequiredMessage) {
		// The engine got as far as asking for the password: the backend opened
		// and the repository config file exists.
		return vaulticProbeResult{vaulticConnReachable, "Dépôt joignable. Le mot de passe n'a pas encore été vérifié."}
	}
	lower := strings.ToLower(message)
	for _, hint := range vaulticAuthFailureHints {
		if strings.Contains(lower, hint) {
			return vaulticProbeResult{vaulticConnAuthFailed, "Le stockage a refusé les identifiants (clés cloud, SSH ou serveur)."}
		}
	}
	return vaulticProbeResult{vaulticConnUnavailable, "Dépôt inaccessible : " + message}
}

// vaulticProbeRepository opens the repository with a timeout. Without a
// password it stops after checking that the repository exists; with a password
// it must decrypt a key. Cancelling ctx returns promptly even if a backend
// ignores the context.
func vaulticProbeRepository(ctx context.Context, g global.Options, timeout time.Duration, open func(context.Context, global.Options) error) vaulticProbeResult {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	passwordGiven := g.Password != ""
	done := make(chan error, 1)
	go func() { done <- open(ctx, g) }()
	select {
	case err := <-done:
		return vaulticClassifyProbe(err, passwordGiven)
	case <-ctx.Done():
		return vaulticClassifyProbe(ctx.Err(), passwordGiven)
	}
}
