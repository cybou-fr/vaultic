package main

import (
	"context"
	"io"
	"strings"
	"testing"
	"time"

	"fyne.io/fyne/v2/test"
	"github.com/restic/restic/internal/global"
)

const swiftRepoAddress = "swift:vault:/repo"

func swiftAppAuth(m *mockKeystone, id, secret string) vaulticCloudAuth {
	return vaulticCloudAuth{Scheme: "swift", AuthURL: m.URL + "/v3", AccessID: id, Secret: secret, AppCredential: true}
}

// newSwiftRepository initializes a real restic repository inside the mock.
func newSwiftRepository(t *testing.T) *mockKeystone {
	t.Helper()
	m := newMockKeystone(t).withObjects()
	m.user, m.password = "alice", "right-pw"
	m.appID, m.appSecret = "app-id-1", "app-secret-1"
	term := &vaulticTerminal{output: io.Discard}
	opts := swiftOptions(swiftPasswordAuth(m, "alice", "right-pw"), swiftRepoAddress)
	opts.Term = term
	if err := runInit(context.Background(), InitOptions{RepositoryVersion: "stable"}, opts, nil, term); err != nil {
		t.Fatalf("repository initialization through Keystone and Swift: %v", err)
	}
	return m
}

func swiftState(t *testing.T, auth vaulticCloudAuth, resticPassword string) vaulticProbeResult {
	t.Helper()
	g := swiftOptions(auth, swiftRepoAddress)
	g.Password = resticPassword
	return vaulticProbeRepository(context.Background(), g, 30*time.Second, vaulticVerifyDepot)
}

func TestVaulticSwiftRepositoryCreateReopenAndSwitchMode(t *testing.T) {
	m := newSwiftRepository(t)
	if len(m.objects) < 2 {
		t.Fatalf("repository files were not written: %d objects", len(m.objects))
	}
	pw := swiftPasswordAuth(m, "alice", "right-pw")
	app := swiftAppAuth(m, "app-id-1", "app-secret-1")

	// Reopen with each authentication mode.
	for name, auth := range map[string]vaulticCloudAuth{"password": pw, "application credential": app} {
		if res := swiftState(t, auth, "restic-password"); res.State != vaulticConnConnected {
			t.Fatalf("%s: %s (%s)", name, res.State.Label(), res.Detail)
		}
	}
	// Reachable without the restic password; the wrong restic password is not an authentication failure of the storage.
	if res := swiftState(t, pw, ""); res.State != vaulticConnReachable {
		t.Fatalf("no restic password: %s (%s)", res.State.Label(), res.Detail)
	}
	if res := swiftState(t, pw, "wrong-restic-password"); res.State != vaulticConnAuthFailed || !strings.Contains(res.Detail, "mot de passe restic") {
		t.Fatalf("wrong restic password: %s (%s)", res.State.Label(), res.Detail)
	}
	// Wrong storage credentials are reported as storage credentials, not as the restic password.
	res := swiftState(t, swiftAppAuth(m, "app-id-1", "wrong"), "restic-password")
	if res.State != vaulticConnAuthFailed || strings.Contains(res.Detail, "mot de passe restic") {
		t.Fatalf("wrong application secret: %s (%s)", res.State.Label(), res.Detail)
	}
}

func TestVaulticSwiftSessionCredentialReplacement(t *testing.T) {
	m := newSwiftRepository(t)
	var store vaulticCloudSessions
	good := swiftPasswordAuth(m, "alice", "right-pw")
	bad := swiftPasswordAuth(m, "alice", "stale-pw")

	if err := store.set(swiftRepoAddress, bad); err != nil {
		t.Fatal(err)
	}
	if res := swiftState(t, store.get(swiftRepoAddress), "restic-password"); res.State != vaulticConnAuthFailed {
		t.Fatalf("stale credentials: %s (%s)", res.State.Label(), res.Detail)
	}
	// Replacing the session entry takes effect immediately and completely.
	if err := store.set(swiftRepoAddress, good); err != nil {
		t.Fatal(err)
	}
	if res := swiftState(t, store.get(swiftRepoAddress), "restic-password"); res.State != vaulticConnConnected {
		t.Fatalf("replaced credentials: %s (%s)", res.State.Label(), res.Detail)
	}
	// Switching to application credentials leaves nothing of the password mode behind.
	if err := store.set(swiftRepoAddress, swiftAppAuth(m, "app-id-1", "app-secret-1")); err != nil {
		t.Fatal(err)
	}
	got := store.get(swiftRepoAddress)
	if !got.AppCredential || got.UserDomain != "" || got.Project != "" || strings.Contains(got.Secret, "right-pw") {
		t.Fatalf("remnants of the previous mode: %+v", got)
	}
	if res := swiftState(t, got, "restic-password"); res.State != vaulticConnConnected {
		t.Fatalf("application credential: %s (%s)", res.State.Label(), res.Detail)
	}
	// Forgetting returns to native authentication, which has no credentials here.
	store.forget(swiftRepoAddress)
	t.Setenv("OS_AUTH_URL", "")
	if res := swiftState(t, store.get(swiftRepoAddress), "restic-password"); res.State == vaulticConnConnected {
		t.Fatal("repository opened after the session credentials were forgotten")
	}
}

func TestVaulticSwiftNativeEnvironmentVersusSessionCredentials(t *testing.T) {
	m := newSwiftRepository(t)
	for k, v := range map[string]string{
		"OS_AUTH_URL": m.URL + "/v3", "OS_USERNAME": "alice", "OS_PASSWORD": "right-pw",
		"OS_PROJECT_NAME": "demo", "OS_USER_DOMAIN_NAME": "Default", "OS_PROJECT_DOMAIN_NAME": "Default",
	} {
		t.Setenv(k, v)
	}
	// No session credentials: native environment authentication works.
	if res := swiftState(t, vaulticCloudAuth{Scheme: "swift"}, "restic-password"); res.State != vaulticConnConnected {
		t.Fatalf("native environment: %s (%s)", res.State.Label(), res.Detail)
	}
	// Session credentials win completely, even over a working environment.
	if res := swiftState(t, swiftPasswordAuth(m, "alice", "wrong"), "restic-password"); res.State != vaulticConnAuthFailed {
		t.Fatalf("wrong session credentials must not fall back to the environment: %s (%s)", res.State.Label(), res.Detail)
	}
	// And a broken environment does not disturb correct session credentials.
	t.Setenv("OS_PASSWORD", "broken")
	if res := swiftState(t, swiftPasswordAuth(m, "alice", "right-pw"), "restic-password"); res.State != vaulticConnConnected {
		t.Fatalf("session credentials with broken environment: %s (%s)", res.State.Label(), res.Detail)
	}
}

func TestVaulticSwiftEditFormKeepsLocationAndDropsSecrets(t *testing.T) {
	test.NewTempApp(t)
	u := vaulticNewStorageSetupUI(vaulticStorageSetupHooks{})
	u.SetScheme("swift")
	u.setField("authurl", "https://auth.example/v3")
	u.setField("username", "alice")
	u.setField("secret", "typed-secret")
	// Editing a saved Swift repository loads its location only.
	if err := u.Edit("Mon Swift", "swift:vault:/team/backup"); err != nil {
		t.Fatal(err)
	}
	v := u.values()
	if v["bucket"] != "vault" || v["prefix"] != "team/backup" || v["name"] != "Mon Swift" {
		t.Fatalf("location not loaded: %v", v)
	}
	if v["secret"] != "" || v["username"] != "" || v["authurl"] != "" {
		t.Fatalf("credentials survived switching into edit mode: %v", v)
	}
	// Mode switching keeps the form coherent and builds the right credentials.
	u.setField("authurl", "https://auth.example/v3")
	u.setField("authmode", vaulticSwiftAppCredChoice)
	u.setField("appid", "app-1")
	u.setField("secret", "s")
	u.name.SetText("Mon Swift")
	u.pass.SetText("restic")
	setup, err := u.build(vaulticActionSave)
	if err != nil || !setup.Auth.AppCredential || setup.Auth.AccessID != "app-1" || setup.Address != "swift:vault:/team/backup" {
		t.Fatalf("app credential build: %v %+v", err, setup)
	}
	u.setField("authmode", vaulticSwiftPasswordChoice)
	u.setField("username", "alice")
	u.setField("project", "demo")
	setup, err = u.build(vaulticActionSave)
	if err != nil || setup.Auth.AppCredential || setup.Auth.AccessID != "alice" || setup.Auth.Project != "demo" {
		t.Fatalf("password build after switching back: %v %+v", err, setup)
	}
	// Relocating the saved entry through the edit helper resets its history.
	depots := []vaulticDepot{{Name: "Mon Swift", Location: "swift:vault:/team/backup", LastVerified: "2026-01-01T00:00:00Z", LastVerifiedState: "connected"}}
	out, err := vaulticEditSavedDepot(depots, "swift:vault:/team/backup", "Mon Swift", "swift:other:/team/backup")
	if err != nil || out[0].LastVerified != "" {
		t.Fatalf("relocation: %v %+v", err, out)
	}
	_ = global.Options{}
}

// Repeated successful probes must not leave connections or goroutines behind.
func TestVaulticSwiftProbesDoNotLeakConnections(t *testing.T) {
	m := newSwiftRepository(t)
	auth := swiftPasswordAuth(m, "alice", "right-pw")
	_ = swiftState(t, auth, "restic-password")
	// The repository initialization above used the engine's own transport; it
	// is not part of what probes are responsible for.
	time.Sleep(500 * time.Millisecond)
	openBefore := m.conns.Load()
	baseline := settledGoroutines(0, 500*time.Millisecond)
	for i := 0; i < 12; i++ {
		if res := swiftState(t, auth, "restic-password"); res.State != vaulticConnConnected {
			t.Fatalf("round %d: %s (%s)", i, res.State.Label(), res.Detail)
		}
	}
	deadline := time.Now().Add(8 * time.Second)
	for m.conns.Load() > openBefore && time.Now().Before(deadline) {
		time.Sleep(100 * time.Millisecond)
	}
	if n := m.conns.Load(); n > openBefore {
		t.Fatalf("%d HTTP connections left open after 12 completed probes (%d before)", n, openBefore)
	}
	if n := settledGoroutines(baseline+4, 8*time.Second); n > baseline+4 {
		t.Fatalf("goroutines grew from %d to %d", baseline, n)
	}
}
