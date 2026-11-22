package main

import (
	"context"
	"crypto/tls"
	"errors"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/restic/restic/internal/backend"
	"github.com/restic/restic/internal/backend/limiter"
	"github.com/restic/restic/internal/backend/location"
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
		return vaulticT("Test en cours…")
	case vaulticConnReachable:
		return vaulticT("Joignable")
	case vaulticConnAuthFailed:
		return vaulticT("Authentification refusée")
	case vaulticConnUnavailable:
		return vaulticT("Dépôt indisponible")
	case vaulticConnConnected:
		return vaulticT("Connecté")
	}
	return vaulticT("Non testé")
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

var (
	vaulticDeniedHints      = []string{"403", "forbidden", "access denied", "accessdenied", "authorizationfailure", "authorizationpermissionmismatch"}
	vaulticAuthFailureHints = []string{
		"401", "unauthorized", "authorization failed", "invalidaccesskeyid", "signaturedoesnotmatch",
		"authentication failed", "permission denied (publickey", "invalid credentials", "bad credentials",
		"unable to authenticate", "no valid credential",
	}
	vaulticNetworkHints = []string{
		"dial tcp", "no such host", "connection refused", "connection reset", "i/o timeout", "tls:", "x509",
		"network is unreachable", "no route to host", "eof", "server misbehaving", "proxyconnect",
	}
)

// vaulticClassifyProbe maps the engine's outcome to a lifecycle state.
// passwordGiven tells whether the restic password was part of the attempt.
func vaulticClassifyProbe(err error, passwordGiven bool) vaulticProbeResult {
	switch {
	case err == nil && passwordGiven:
		return vaulticProbeResult{vaulticConnConnected, vaulticT("Dépôt ouvert et mot de passe accepté.")}
	case err == nil:
		return vaulticProbeResult{vaulticConnReachable, vaulticT("Dépôt joignable.")}
	case errors.Is(err, context.Canceled):
		return vaulticProbeResult{vaulticConnNotTested, vaulticT("Test annulé.")}
	case errors.Is(err, context.DeadlineExceeded):
		return vaulticProbeResult{vaulticConnUnavailable, vaulticT("Délai dépassé : le stockage ne répond pas.")}
	case errors.Is(err, repository.ErrNoKeyFound):
		return vaulticProbeResult{vaulticConnAuthFailed, vaulticT("Dépôt joignable, mais le mot de passe restic est incorrect.")}
	case errors.Is(err, global.ErrNoRepository):
		return vaulticProbeResult{vaulticConnUnavailable, vaulticT("Stockage joignable, mais aucun dépôt restic à cet emplacement.")}
	}
	message := err.Error()
	if !passwordGiven && strings.Contains(message, vaulticPasswordRequiredMessage) {
		// The engine got as far as asking for the password: the backend opened
		// and the repository config file exists.
		return vaulticProbeResult{vaulticConnReachable, vaulticT("Dépôt joignable. Le mot de passe n'a pas encore été vérifié.")}
	}
	lower := strings.ToLower(message)
	// Network problems come first: a refused TCP connection says nothing about
	// the credentials.
	for _, hint := range vaulticNetworkHints {
		if strings.Contains(lower, hint) {
			return vaulticProbeResult{vaulticConnUnavailable, vaulticT("Erreur réseau : le serveur de stockage ou d'authentification est injoignable (") + message + ")"}
		}
	}
	for _, hint := range vaulticDeniedHints {
		if strings.Contains(lower, hint) {
			return vaulticProbeResult{vaulticConnAuthFailed, vaulticT("Accès refusé : les identifiants sont reconnus mais n'ont pas les droits sur ce conteneur ou dépôt.")}
		}
	}
	for _, hint := range vaulticAuthFailureHints {
		if strings.Contains(lower, hint) {
			return vaulticProbeResult{vaulticConnAuthFailed, vaulticT("Identifiants refusés par le stockage (clés cloud, SSH, serveur ou Keystone).")}
		}
	}
	return vaulticProbeResult{vaulticConnUnavailable, vaulticT("Dépôt inaccessible : ") + message}
}

// vaulticProbeRepository opens the repository with a timeout. Without a
// password it stops after checking that the repository exists; with a password
// it must decrypt a key. Cancelling ctx returns promptly even if a backend
// ignores the context.
func vaulticProbeRepository(ctx context.Context, g global.Options, timeout time.Duration, open func(context.Context, global.Options) error) vaulticProbeResult {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	passwordGiven := g.Password != ""
	if g.Backends != nil {
		g.Backends = vaulticClosingRegistry(g.Backends)
	}
	done := make(chan error, 1)
	go func() { done <- open(ctx, g) }()
	select {
	case err := <-done:
		return vaulticClassifyProbe(err, passwordGiven)
	case <-ctx.Done():
		return vaulticClassifyProbe(ctx.Err(), passwordGiven)
	}
}

// The engine hands every backend a shared-style HTTP transport that nothing
// ever closes: after a connection test its idle keep-alive connections (and the
// goroutines serving them) would linger for 90 seconds. For tests, each HTTP
// based backend gets a private transport that is closed together with the
// backend, including when opening fails or is cancelled.
var vaulticHTTPBackends = map[string]bool{"rest": true, "s3": true, "b2": true, "azure": true, "gs": true, "swift": true}

type vaulticIdleCloser interface{ CloseIdleConnections() }

type vaulticClosingFactory struct{ location.Factory }

// private builds a transport with the same settings as the engine's default
// (environment proxy, timeouts, HTTP/2) but without its request watchdog
// wrapper, which hides CloseIdleConnections. Requests are bounded by the
// probe's own context instead.
func (f vaulticClosingFactory) private() (http.RoundTripper, vaulticIdleCloser) {
	tr := &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		DialContext:           (&net.Dialer{Timeout: 30 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          10,
		IdleConnTimeout:       30 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
		TLSClientConfig:       &tls.Config{},
	}
	return tr, tr
}

func (f vaulticClosingFactory) wrap(be backend.Backend, closer vaulticIdleCloser, err error) (backend.Backend, error) {
	if err != nil {
		if closer != nil {
			closer.CloseIdleConnections()
		}
		return nil, err
	}
	if closer == nil {
		return be, nil
	}
	return vaulticClosingBackend{Backend: be, closer: closer}, nil
}

func (f vaulticClosingFactory) Create(ctx context.Context, cfg interface{}, rt http.RoundTripper, lim limiter.Limiter, onError func(string, ...interface{})) (backend.Backend, error) {
	own, closer := f.private()
	if own == nil {
		own = rt
	}
	be, err := f.Factory.Create(ctx, cfg, own, lim, onError)
	return f.wrap(be, closer, err)
}

func (f vaulticClosingFactory) Open(ctx context.Context, cfg interface{}, rt http.RoundTripper, lim limiter.Limiter, onError func(string, ...interface{})) (backend.Backend, error) {
	own, closer := f.private()
	if own == nil {
		own = rt
	}
	be, err := f.Factory.Open(ctx, cfg, own, lim, onError)
	return f.wrap(be, closer, err)
}

type vaulticClosingBackend struct {
	backend.Backend
	closer vaulticIdleCloser
}

func (b vaulticClosingBackend) Close() error {
	err := b.Backend.Close()
	b.closer.CloseIdleConnections()
	return err
}

func vaulticClosingRegistry(src *location.Registry) *location.Registry {
	registry := location.NewRegistry()
	for _, info := range vaulticBackendCatalog {
		factory := src.Lookup(info.Scheme)
		if factory == nil {
			continue
		}
		if vaulticHTTPBackends[info.Scheme] {
			factory = vaulticClosingFactory{Factory: factory}
		}
		registry.Register(factory)
	}
	return registry
}

// vaulticProbeRedacted is vaulticProbeRepository with every user-facing detail
// scrubbed of the restic password, the session credentials and URL secrets:
// engine and server error messages sometimes echo what was sent.
func vaulticProbeRedacted(ctx context.Context, g global.Options, timeout time.Duration, auth vaulticCloudAuth, open func(context.Context, global.Options) error) vaulticProbeResult {
	res := vaulticProbeRepository(ctx, g, timeout, open)
	res.Detail = vaulticRedactCloudSecrets(vaulticRedactOperation(g.Password), auth)(res.Detail)
	return res
}
