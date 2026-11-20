package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/restic/restic/internal/backend/swift"
	"github.com/restic/restic/internal/global"
)

// mockKeystone is a minimal Keystone v3 + Swift endpoint. It records every
// authentication request so tests can prove which credentials were sent.
type mockKeystone struct {
	*httptest.Server
	mu        sync.Mutex
	requests  []string
	user      string // expected user name (password mode)
	password  string
	appID     string // expected application credential ID
	appSecret string
	container int // status for the container request (default 204)
	hang      chan struct{}
}

func newMockKeystone(t *testing.T) *mockKeystone {
	t.Helper()
	m := &mockKeystone{container: http.StatusNoContent}
	m.Server = httptest.NewServer(http.HandlerFunc(m.handle))
	t.Cleanup(m.Server.Close)
	return m
}

func (m *mockKeystone) handle(w http.ResponseWriter, r *http.Request) {
	if m.hang != nil {
		select {
		case <-m.hang:
		case <-r.Context().Done():
			return
		}
	}
	if r.Method == http.MethodPost && r.URL.Path == "/v3/auth/tokens" {
		body, _ := io.ReadAll(r.Body)
		m.mu.Lock()
		m.requests = append(m.requests, string(body))
		m.mu.Unlock()
		if !m.accepts(string(body)) {
			http.Error(w, `{"error":{"code":401,"message":"The request you have made requires authentication."}}`, http.StatusUnauthorized)
			return
		}
		w.Header().Set("X-Subject-Token", "mock-token")
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]any{"token": map[string]any{
			"expires_at": time.Now().Add(time.Hour).UTC().Format("2006-01-02T15:04:05.000000Z"),
			"catalog": []any{map[string]any{"type": "object-store", "endpoints": []any{map[string]any{
				"interface": "public", "region": "R1", "url": m.URL + "/swift/v1/AUTH_project",
			}}}},
		}})
		return
	}
	if r.Header.Get("X-Auth-Token") != "mock-token" {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	if strings.Trim(r.URL.Path, "/") == "swift/v1/AUTH_project/vault" {
		w.Header().Set("X-Container-Object-Count", "0")
		w.Header().Set("X-Container-Bytes-Used", "0")
		w.WriteHeader(m.container) // the container itself
		return
	}
	http.NotFound(w, r) // objects, including the restic config file, do not exist
}

func (m *mockKeystone) accepts(body string) bool {
	if m.appID != "" {
		return strings.Contains(body, `"id":"`+m.appID+`"`) && strings.Contains(body, `"secret":"`+m.appSecret+`"`)
	}
	return strings.Contains(body, `"name":"`+m.user+`"`) && strings.Contains(body, `"password":"`+m.password+`"`)
}

func (m *mockKeystone) lastRequest(t *testing.T) string {
	t.Helper()
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.requests) == 0 {
		t.Fatal("no authentication request received")
	}
	return m.requests[len(m.requests)-1]
}

func swiftOptions(auth vaulticCloudAuth, repo string) global.Options {
	return global.Options{
		Repo: repo, Password: "restic-password", Backends: vaulticBackendsForSession(auth),
		Term: &vaulticTerminal{output: io.Discard}, JSON: true, Verbosity: 1,
	}
}

func swiftProbe(auth vaulticCloudAuth, timeout time.Duration) vaulticProbeResult {
	return vaulticProbeRepository(context.Background(), swiftOptions(auth, "swift:vault:/repo"), timeout, vaulticVerifyDepot)
}

func swiftPasswordAuth(m *mockKeystone, user, password string) vaulticCloudAuth {
	return vaulticCloudAuth{
		Scheme: "swift", AuthURL: m.URL + "/v3", AccessID: user, Secret: password,
		UserDomain: "Default", Project: "demo", ProjectDomain: "Default", Region: "R1",
	}
}

func TestVaulticSwiftAuthValidation(t *testing.T) {
	good := vaulticCloudAuth{Scheme: "swift", AuthURL: "https://auth.example/v3", AccessID: "u", Secret: "p", UserDomain: "Default", Project: "x", ProjectDomain: "Default"}
	if err := good.validate(); err != nil {
		t.Fatal(err)
	}
	app := vaulticCloudAuth{Scheme: "swift", AuthURL: "https://auth.example/v3", AccessID: "cred-id", Secret: "s", AppCredential: true}
	if err := app.validate(); err != nil {
		t.Fatalf("application credential needs no project: %v", err)
	}
	if err := (vaulticCloudAuth{Scheme: "swift"}).validate(); err != nil {
		t.Fatalf("empty swift auth means native environment: %v", err)
	}
	base := good
	mutate := map[string]func(a *vaulticCloudAuth){
		"keystone v2":      func(a *vaulticCloudAuth) { a.AuthURL = "https://auth.example/v2.0" },
		"plain http":       func(a *vaulticCloudAuth) { a.AuthURL = "http://auth.example/v3" },
		"url credentials":  func(a *vaulticCloudAuth) { a.AuthURL = "https://u:p@auth.example/v3" },
		"url query":        func(a *vaulticCloudAuth) { a.AuthURL = "https://auth.example/v3?x=1" },
		"ftp":              func(a *vaulticCloudAuth) { a.AuthURL = "ftp://auth.example/v3" },
		"missing password": func(a *vaulticCloudAuth) { a.Secret = "" },
		"missing user":     func(a *vaulticCloudAuth) { a.AccessID = "" },
		"missing project":  func(a *vaulticCloudAuth) { a.Project = "" },
		"missing domain":   func(a *vaulticCloudAuth) { a.UserDomain = "" },
		"missing url":      func(a *vaulticCloudAuth) { a.AuthURL = "" },
	}
	for name, change := range mutate {
		auth := base
		change(&auth)
		if err := auth.validate(); err == nil {
			t.Errorf("%s accepted", name)
		}
	}
	if _, err := vaulticSwiftAuthURL("http://127.0.0.1:5000/v3"); err != nil {
		t.Errorf("loopback http must be allowed for local test servers: %v", err)
	}
}

func TestVaulticSwiftFormBuildsSessionCredentials(t *testing.T) {
	setup, err := vaulticBuildStorageSetup("swift", guidedValues("Swift", map[string]string{
		"bucket": "vault", "prefix": "repo", "authurl": "https://auth.example/v3", "authmode": vaulticSwiftPasswordChoice,
		"username": "alice", "secret": "swift-secret-pw", "userdomain": "Default", "project": "demo", "projectdomain": "Default", "region": "GRA",
	}))
	if err != nil {
		t.Fatal(err)
	}
	if setup.Address != "swift:vault:/repo" {
		t.Fatalf("address %q", setup.Address)
	}
	for _, secret := range []string{"swift-secret-pw", "alice", "auth.example", setup.Password} {
		if strings.Contains(setup.Address, secret) || strings.Contains(vaulticSafeLocation(setup.Address), secret) {
			t.Errorf("%q leaked in address", secret)
		}
	}
	if !setup.Auth.hasValues() || setup.Auth.AppCredential || setup.Auth.Project != "demo" {
		t.Fatalf("auth: %+v", setup.Auth)
	}
	// Native mode: blank Keystone fields keep an address-only setup.
	native, err := vaulticBuildStorageSetup("swift", guidedValues("Swift", map[string]string{"bucket": "vault", "userdomain": "Default", "projectdomain": "Default"}))
	if err != nil || native.Auth.hasValues() {
		t.Fatalf("native mode: %v %+v", err, native.Auth)
	}
	if _, err := vaulticBuildStorageSetup("swift", guidedValues("Swift", map[string]string{"bucket": "vault", "username": "u", "secret": "p"})); err == nil {
		t.Fatal("credentials without Keystone URL accepted")
	}
	app, err := vaulticBuildStorageSetup("swift", guidedValues("Swift", map[string]string{
		"bucket": "vault", "authurl": "https://auth.example/v3", "authmode": vaulticSwiftAppCredChoice, "username": "id123", "secret": "s",
	}))
	if err != nil || !app.Auth.AppCredential {
		t.Fatalf("application credential: %v %+v", err, app.Auth)
	}
}

func TestVaulticSwiftFactoryReplacesAmbientEnvironment(t *testing.T) {
	for k, v := range map[string]string{
		"OS_USERNAME": "ambient-user", "OS_PASSWORD": "ambient-pass", "OS_AUTH_URL": "https://ambient/v3",
		"OS_USER_ID": "ambient-id", "OS_PROJECT_NAME": "ambient-project", "OS_AUTH_TOKEN": "ambient-token",
		"OS_STORAGE_URL": "https://ambient/storage", "OS_APPLICATION_CREDENTIAL_ID": "ambient-app",
	} {
		t.Setenv(k, v)
	}
	base := swift.NewConfig()
	base.Container = "vault"
	base.ApplyEnvironment("") // what the engine does before the factory runs
	if base.UserName != "ambient-user" {
		t.Fatal("test setup: environment was not applied")
	}
	factory := func(auth vaulticCloudAuth) *swift.Config {
		out, err := vaulticBackendsForSession(auth).Lookup("swift").(vaulticPrivateFactory).apply(&base)
		if err != nil {
			t.Fatal(err)
		}
		return out.(*swift.Config)
	}
	cfg := factory(vaulticCloudAuth{Scheme: "swift", AuthURL: "https://mine/v3", AccessID: "me", Secret: "mypw", UserDomain: "D", Project: "P", ProjectDomain: "PD", Region: "R"})
	if cfg.UserName != "me" || cfg.APIKey != "mypw" || cfg.AuthURL != "https://mine/v3" || cfg.Tenant != "P" || cfg.TenantDomain != "PD" || cfg.Domain != "D" || cfg.Region != "R" {
		t.Fatalf("session credentials not applied: %+v", cfg)
	}
	if cfg.UserID != "" || cfg.AuthToken.Unwrap() != "" || cfg.StorageURL != "" || cfg.ApplicationCredentialID != "" {
		t.Fatalf("ambient values leaked into the session config: %+v", cfg)
	}
	if base.UserName != "ambient-user" {
		t.Fatal("shared config mutated")
	}
	cfg = factory(vaulticCloudAuth{Scheme: "swift", AuthURL: "https://mine/v3", AccessID: "app-id", Secret: "app-secret", AppCredential: true})
	if cfg.ApplicationCredentialID != "app-id" || cfg.ApplicationCredentialSecret.Unwrap() != "app-secret" || cfg.UserName != "" || cfg.APIKey != "" {
		t.Fatalf("application credential config wrong: %+v", cfg)
	}
	// No session credentials: the native environment is left alone.
	if factory(vaulticCloudAuth{Scheme: "swift"}).UserName != "ambient-user" {
		t.Fatal("native mode must keep environment values")
	}
	if os.Getenv("OS_PASSWORD") != "ambient-pass" {
		t.Fatal("process environment mutated")
	}
}

func TestVaulticSwiftMockKeystone(t *testing.T) {
	m := newMockKeystone(t)
	m.user, m.password = "alice", "right-pw"
	t.Setenv("OS_USERNAME", "ambient-user")
	t.Setenv("OS_PASSWORD", "ambient-pass")

	res := swiftProbe(swiftPasswordAuth(m, "alice", "wrong-pw"), 20*time.Second)
	if res.State != vaulticConnAuthFailed || !strings.Contains(res.Detail, "Identifiants refusés") {
		t.Fatalf("wrong password: %s (%s)", res.State.Label(), res.Detail)
	}

	// Right password: authentication works, but there is no restic repository.
	res = swiftProbe(swiftPasswordAuth(m, "alice", "right-pw"), 20*time.Second)
	if res.State != vaulticConnUnavailable || !strings.Contains(res.Detail, "aucun dépôt") {
		t.Fatalf("authenticated, no repository: %s (%s)", res.State.Label(), res.Detail)
	}
	body := m.lastRequest(t)
	for _, want := range []string{`"alice"`, `"right-pw"`, `"demo"`, `"Default"`} {
		if !strings.Contains(body, want) {
			t.Errorf("Keystone request lacks %s: %s", want, body)
		}
	}
	if strings.Contains(body, "ambient") {
		t.Fatalf("ambient environment reached Keystone: %s", body)
	}
	if strings.Contains(res.Detail, "right-pw") {
		t.Fatal("password in detail")
	}
}

func TestVaulticSwiftApplicationCredentialAndAccessDenied(t *testing.T) {
	m := newMockKeystone(t)
	m.appID, m.appSecret = "cred-id-42", "cred-secret"
	auth := vaulticCloudAuth{Scheme: "swift", AuthURL: m.URL + "/v3", AccessID: "cred-id-42", Secret: "cred-secret", AppCredential: true}
	m.container = http.StatusForbidden
	res := swiftProbe(auth, 20*time.Second)
	body := m.lastRequest(t)
	if !strings.Contains(body, "application_credential") || !strings.Contains(body, "cred-id-42") || strings.Contains(body, `"password"`) {
		t.Fatalf("application credential request: %s", body)
	}
	if res.State != vaulticConnAuthFailed || !strings.Contains(res.Detail, "droits") {
		t.Fatalf("403 on the container must say rights are missing, not wrong password: %s (%s)", res.State.Label(), res.Detail)
	}
}

func TestVaulticSwiftNetworkFailureIsNotAuthFailure(t *testing.T) {
	m := newMockKeystone(t)
	auth := swiftPasswordAuth(m, "alice", "pw")
	m.Server.Close() // nothing listens any more
	res := swiftProbe(auth, 20*time.Second)
	if res.State != vaulticConnUnavailable || !strings.Contains(res.Detail, "réseau") {
		t.Fatalf("network failure: %s (%s)", res.State.Label(), res.Detail)
	}
}

func TestVaulticSwiftTimeoutAndCancellation(t *testing.T) {
	m := newMockKeystone(t)
	m.hang = make(chan struct{})
	defer close(m.hang)
	auth := swiftPasswordAuth(m, "alice", "pw")

	start := time.Now()
	res := swiftProbe(auth, 300*time.Millisecond)
	if res.State != vaulticConnUnavailable || !strings.Contains(res.Detail, "Délai") || time.Since(start) > 10*time.Second {
		t.Fatalf("timeout: %s (%s) after %v", res.State.Label(), res.Detail, time.Since(start))
	}

	ctx, cancel := context.WithCancel(context.Background())
	go func() { time.Sleep(150 * time.Millisecond); cancel() }()
	res = vaulticProbeRepository(ctx, swiftOptions(auth, "swift:vault:/repo"), time.Minute, vaulticVerifyDepot)
	if res.State != vaulticConnNotTested {
		t.Fatalf("cancellation: %s (%s)", res.State.Label(), res.Detail)
	}
}

func TestVaulticSwiftCredentialsIsolatedBetweenRepositories(t *testing.T) {
	a, b := newMockKeystone(t), newMockKeystone(t)
	a.user, a.password = "alice", "pw-a"
	b.user, b.password = "bob", "pw-b"
	var store vaulticCloudSessions
	if err := store.set("swift:vault:/a", swiftPasswordAuth(a, "alice", "pw-a")); err != nil {
		t.Fatal(err)
	}
	if err := store.set("swift:vault:/b", swiftPasswordAuth(b, "bob", "pw-b")); err != nil {
		t.Fatal(err)
	}
	if store.get("swift:other:/c").hasValues() {
		t.Fatal("credentials leaked to an unrelated repository")
	}
	var wg sync.WaitGroup
	for _, repo := range []string{"swift:vault:/a", "swift:vault:/b"} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			res := vaulticProbeRepository(context.Background(), swiftOptions(store.get(repo), repo), 20*time.Second, vaulticVerifyDepot)
			if !strings.Contains(res.Detail, "aucun dépôt") {
				t.Errorf("%s: %s (%s)", repo, res.State.Label(), res.Detail)
			}
		}()
	}
	wg.Wait()
	if !strings.Contains(a.lastRequest(t), `"alice"`) || strings.Contains(a.lastRequest(t), "bob") {
		t.Fatal("server A saw wrong credentials")
	}
	if !strings.Contains(b.lastRequest(t), `"bob"`) || strings.Contains(b.lastRequest(t), "alice") {
		t.Fatal("server B saw wrong credentials")
	}
	store.clearAll()
	if store.get("swift:vault:/a").hasValues() {
		t.Fatal("credentials survived the end of the session")
	}
}

func TestVaulticSwiftSecretsNeverReachLogsOrSettings(t *testing.T) {
	auth := vaulticCloudAuth{Scheme: "swift", AuthURL: "https://auth.example/v3", AccessID: "alice", Secret: "swift-super-secret"}
	redact := vaulticRedactCloudSecrets(vaulticRedactOperation("restic-pass"), auth)
	msg := redact("authenticate https://auth.example/v3: body password=swift-super-secret restic-pass")
	if strings.Contains(msg, "swift-super-secret") || strings.Contains(msg, "restic-pass") {
		t.Fatalf("secret in log line: %q", msg)
	}
	raw, _ := json.Marshal(vaulticDepot{Name: "Swift", Location: "swift:vault:/repo"})
	if strings.Contains(string(raw), "swift-super-secret") || strings.Contains(string(raw), "auth.example") {
		t.Fatalf("settings entry carries credentials: %s", raw)
	}
}
