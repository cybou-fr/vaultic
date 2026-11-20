package main

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/test"
	"github.com/restic/restic/internal/backend/gs"
	"github.com/restic/restic/internal/backend/rest"
	"github.com/restic/restic/internal/global"
	"github.com/restic/restic/internal/repository"
)

// ---------------------------------------------------------------------------
// Connection lifecycle

func TestVaulticClassifyProbe(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		password bool
		want     vaulticConnState
	}{
		{"connected", nil, true, vaulticConnConnected},
		{"reachable without password is not connected", nil, false, vaulticConnReachable},
		{"wrong restic password", repository.ErrNoKeyFound, true, vaulticConnAuthFailed},
		{"no repository", global.ErrNoRepository, true, vaulticConnUnavailable},
		{"timeout", context.DeadlineExceeded, true, vaulticConnUnavailable},
		{"cancelled", context.Canceled, true, vaulticConnNotTested},
		{"engine asked for the password", errors.New("unable to read password: " + vaulticPasswordRequiredMessage), false, vaulticConnReachable},
		{"cloud 403", errors.New("Stat: The request signature we calculated does not match: 403 Forbidden"), true, vaulticConnAuthFailed},
		{"ssh key refused", errors.New("ssh: Permission denied (publickey)"), true, vaulticConnAuthFailed},
		{"network down", errors.New("dial tcp: lookup host: no such host"), true, vaulticConnUnavailable},
	}
	for _, tc := range tests {
		got := vaulticClassifyProbe(tc.err, tc.password)
		if got.State != tc.want {
			t.Errorf("%s: got %s (%s), want %s", tc.name, got.State.Label(), got.Detail, tc.want.Label())
		}
	}
	// A password prompt must never be reported as connected when a password was given.
	if got := vaulticClassifyProbe(errors.New(vaulticPasswordRequiredMessage), true); got.State == vaulticConnReachable || got.State == vaulticConnConnected {
		t.Errorf("unexpected state for prompt with password: %s", got.State.Label())
	}
	for s := vaulticConnNotTested; s <= vaulticConnConnected; s++ {
		if vaulticConnStateFromKey(s.Key()) != s || s.Label() == "" {
			t.Errorf("state %d does not round-trip", s)
		}
	}
	if vaulticConnStateFromKey("garbage") != vaulticConnNotTested {
		t.Error("unknown key must map to not tested")
	}
}

func TestVaulticProbeTimeoutAndCancellation(t *testing.T) {
	g := global.Options{Password: "pw"}
	// An engine call that ignores its context must not block the interface.
	release := make(chan struct{})
	defer close(release)
	start := time.Now()
	res := vaulticProbeRepository(context.Background(), g, 80*time.Millisecond, func(context.Context, global.Options) error {
		<-release
		return nil
	})
	if res.State != vaulticConnUnavailable || time.Since(start) > 3*time.Second {
		t.Fatalf("timeout: state %s after %v", res.State.Label(), time.Since(start))
	}

	ctx, cancel := context.WithCancel(context.Background())
	started := make(chan struct{})
	go func() { <-started; time.Sleep(30 * time.Millisecond); cancel() }()
	res = vaulticProbeRepository(ctx, g, time.Minute, func(c context.Context, _ global.Options) error {
		close(started)
		<-c.Done()
		return c.Err()
	})
	if res.State != vaulticConnNotTested {
		t.Fatalf("cancellation: got %s (%s)", res.State.Label(), res.Detail)
	}
}

func initLocalTestRepository(t *testing.T, password string) (string, global.Options) {
	t.Helper()
	repoPath := filepath.Join(t.TempDir(), "repo")
	term := &vaulticTerminal{output: io.Discard}
	opts := global.Options{Repo: repoPath, Password: password, Backends: vaulticBackendsForSession(vaulticCloudAuth{}), Term: term, JSON: true, Verbosity: 1}
	if err := runInit(context.Background(), InitOptions{RepositoryVersion: "stable"}, opts, nil, term); err != nil {
		t.Fatalf("repository initialization: %v", err)
	}
	return repoPath, opts
}

func TestVaulticLocalRepositoryLifecycle(t *testing.T) {
	_, opts := initLocalTestRepository(t, "vaultic-test-password")
	ctx := context.Background()

	if res := vaulticProbeRepository(ctx, opts, 30*time.Second, vaulticVerifyDepot); res.State != vaulticConnConnected {
		t.Fatalf("existing repository: %s (%s)", res.State.Label(), res.Detail)
	}

	wrong := opts
	wrong.Password = "not-the-password"
	if res := vaulticProbeRepository(ctx, wrong, 30*time.Second, vaulticVerifyDepot); res.State != vaulticConnAuthFailed {
		t.Fatalf("wrong password: %s (%s)", res.State.Label(), res.Detail)
	}

	// Reachability without a password proves existence only.
	noPassword := opts
	noPassword.Password = ""
	if res := vaulticProbeRepository(ctx, noPassword, 30*time.Second, vaulticVerifyDepot); res.State != vaulticConnReachable {
		t.Fatalf("no password: %s (%s)", res.State.Label(), res.Detail)
	}

	empty := opts
	empty.Repo = t.TempDir()
	if res := vaulticProbeRepository(ctx, empty, 30*time.Second, vaulticVerifyDepot); res.State != vaulticConnUnavailable {
		t.Fatalf("directory without repository: %s (%s)", res.State.Label(), res.Detail)
	}
	missing := opts
	missing.Repo = filepath.Join(t.TempDir(), "absent")
	if res := vaulticProbeRepository(ctx, missing, 30*time.Second, vaulticVerifyDepot); res.State != vaulticConnUnavailable {
		t.Fatalf("missing directory: %s (%s)", res.State.Label(), res.Detail)
	}
	// Testing never creates or alters anything.
	if _, err := os.Stat(missing.Repo); !os.IsNotExist(err) {
		t.Fatal("a connection test created the repository")
	}
}

// ---------------------------------------------------------------------------
// Mock REST server: authentication failures and per-repository credentials.

func newMockRESTServer(t *testing.T, user, password string) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		u, p, ok := r.BasicAuth()
		if !ok || u != user || p != password {
			w.Header().Set("WWW-Authenticate", `Basic realm="restic"`)
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		http.NotFound(w, r) // authenticated, but no repository here
	}))
	t.Cleanup(server.Close)
	return server, &calls
}

func TestVaulticRESTAuthenticationFailureVersusMissingRepository(t *testing.T) {
	server, _ := newMockRESTServer(t, "alice", "right-pass")
	address := "rest:" + server.URL + "/repo"
	if err := vaulticValidateDepot(address); err != nil {
		t.Fatal(err)
	}
	probe := func(auth vaulticCloudAuth) vaulticProbeResult {
		g := global.Options{
			Repo: address, Password: "restic-password", Backends: vaulticBackendsForSession(auth),
			Term: &vaulticTerminal{output: io.Discard}, JSON: true, Verbosity: 1,
		}
		return vaulticProbeRepository(context.Background(), g, 20*time.Second, vaulticVerifyDepot)
	}
	if res := probe(vaulticCloudAuth{Scheme: "rest", AccessID: "alice", Secret: "wrong-pass"}); res.State != vaulticConnAuthFailed {
		t.Fatalf("wrong server credentials: %s (%s)", res.State.Label(), res.Detail)
	}
	res := probe(vaulticCloudAuth{Scheme: "rest", AccessID: "alice", Secret: "right-pass"})
	if res.State != vaulticConnUnavailable || !strings.Contains(res.Detail, "aucun dépôt") {
		t.Fatalf("authenticated but empty server must be 'unavailable / no repository': %s (%s)", res.State.Label(), res.Detail)
	}
	if strings.Contains(res.Detail, "right-pass") {
		t.Fatal("password leaked in detail")
	}
}

// ---------------------------------------------------------------------------
// Credential isolation

func TestVaulticCredentialsAreIsolatedPerRepository(t *testing.T) {
	t.Setenv("RESTIC_REST_USERNAME", "ambient-user")
	t.Setenv("RESTIC_REST_PASSWORD", "ambient-pass")
	t.Setenv("GOOGLE_PROJECT_ID", "ambient-project")
	var store vaulticCloudSessions
	repoA, repoB := "rest:https://a.example/repo", "rest:https://b.example/repo"
	if err := store.set(repoA, vaulticCloudAuth{Scheme: "rest", AccessID: "userA", Secret: "secretA"}); err != nil {
		t.Fatal(err)
	}
	if err := store.set(repoB, vaulticCloudAuth{Scheme: "rest", AccessID: "userB", Secret: "secretB"}); err != nil {
		t.Fatal(err)
	}
	if err := store.set("gs:bucket:/x", vaulticCloudAuth{Scheme: "gs", ProjectID: "project-x"}); err != nil {
		t.Fatal(err)
	}
	if store.get(repoA).Secret != "secretA" || store.get(repoB).Secret != "secretB" || store.get("rest:https://c.example/repo").hasSecret() {
		t.Fatal("credentials crossed between repositories")
	}
	// A scheme mismatch is refused.
	if err := store.set("gs:bucket:/x", vaulticCloudAuth{Scheme: "rest", AccessID: "u", Secret: "p"}); err == nil {
		t.Fatal("scheme mismatch accepted")
	}
	// gs never takes keys here (native ADC only).
	if err := (vaulticCloudAuth{Scheme: "gs", Secret: "key"}).validate(); err == nil {
		t.Fatal("gs must not accept pasted keys")
	}

	// Each backend registry only carries its own credentials, concurrently.
	var wg sync.WaitGroup
	for _, tc := range []struct{ repo, user, pass string }{{repoA, "userA", "secretA"}, {repoB, "userB", "secretB"}} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			factory := vaulticBackendsForSession(store.get(tc.repo)).Lookup("rest").(vaulticPrivateFactory)
			base := rest.NewConfig()
			base.URL, _ = url.Parse(tc.repo[5:])
			out, err := factory.apply(&base)
			if err != nil {
				t.Error(err)
				return
			}
			got := out.(*rest.Config).URL.User
			pw, _ := got.Password()
			if got.Username() != tc.user || pw != tc.pass {
				t.Errorf("%s got %s:%s", tc.repo, got.Username(), pw)
			}
			if base.URL.User != nil {
				t.Error("shared configuration was mutated")
			}
		}()
	}
	wg.Wait()

	// gs project is injected without environment changes.
	gsFactory := vaulticBackendsForSession(store.get("gs:bucket:/x")).Lookup("gs").(vaulticPrivateFactory)
	gsBase := gs.NewConfig()
	gsOut, err := gsFactory.apply(&gsBase)
	if err != nil || gsOut.(*gs.Config).ProjectID != "project-x" || gsBase.ProjectID != "" {
		t.Fatalf("gs project: %v %v", err, gsOut)
	}
	if os.Getenv("RESTIC_REST_USERNAME") != "ambient-user" || os.Getenv("GOOGLE_PROJECT_ID") != "ambient-project" {
		t.Fatal("process environment was mutated")
	}
	// Backends without credentials stay the untouched originals.
	if _, wrapped := vaulticBackendsForSession(store.get(repoA)).Lookup("sftp").(vaulticPrivateFactory); wrapped {
		t.Fatal("unrelated backend was wrapped")
	}

	// Session end wipes everything.
	store.clearAll()
	for _, repo := range []string{repoA, repoB, "gs:bucket:/x"} {
		if got := store.get(repo); got.hasSecret() || got.ProjectID != "" {
			t.Fatalf("credentials survived clearAll for %s", repo)
		}
	}
}

func TestVaulticSecretRedaction(t *testing.T) {
	auth := vaulticCloudAuth{Scheme: "rest", AccessID: "alice", Secret: "very-secret-server-pass"}
	redact := vaulticRedactCloudSecrets(vaulticRedactOperation("restic-repo-pass"), auth)
	message := redact("GET https://alice:very-secret-server-pass@host/repo?token=zzz failed; restic-repo-pass; very-secret-server-pass")
	for _, leaked := range []string{"very-secret-server-pass", "restic-repo-pass", "token=zzz", "alice:"} {
		if strings.Contains(message, leaked) {
			t.Errorf("%q leaked in %q", leaked, message)
		}
	}
}

// ---------------------------------------------------------------------------
// Settings persistence

func TestVaulticSettingsPersistVerificationWithoutSecrets(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("APPDATA", dir)
	t.Setenv("XDG_CONFIG_HOME", dir)
	t.Setenv("HOME", dir)
	repo := "sftp://backup@host.example:2222//vaultic"
	if err := vaulticValidateDepot(repo); err != nil {
		t.Fatalf("sftp URL with port and user must be a valid saved location: %v", err)
	}
	for _, bad := range []string{"sftp://backup:pw@host.example//vaultic", "rest:https://u:pw@h.example/r", "rest:https://h.example/r?token=1", "gs:b:/x#frag"} {
		if vaulticValidateDepot(bad) == nil {
			t.Errorf("%q accepted", bad)
		}
	}
	depots := []vaulticDepot{{Name: "Serveur", Location: repo}, {Name: "Disque", Location: filepath.Join(dir, "repo")}}
	stamp := time.Date(2026, 11, 20, 14, 3, 0, 0, time.UTC)
	depots = vaulticRecordVerification(depots, repo, vaulticConnAuthFailed, stamp)
	if depots[1].LastVerified != "" {
		t.Fatal("verification recorded on the wrong repository")
	}
	settings := vaulticSettings{Repository: repo, Depots: depots, Profiles: []vaulticBackupProfile{{Name: "Par défaut"}}}
	if err := vaulticSaveSettings(settings); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(vaulticSettingsPath())
	if err != nil {
		t.Fatal(err)
	}
	// Secrets only ever live in the session; build a real setup with sentinels
	// and prove none of them can reach the settings file.
	setup, err := vaulticBuildStorageSetup("rest", guidedValues("REST", map[string]string{
		"url": "https://backup.example/r", "username": "sentinel-user", "secret": "sentinel-secret",
	}))
	if err != nil {
		t.Fatal(err)
	}
	settings.Depots = append(settings.Depots, vaulticDepot{Name: setup.Name, Location: setup.Address})
	if err := vaulticSaveSettings(settings); err != nil {
		t.Fatal(err)
	}
	raw, err = os.ReadFile(vaulticSettingsPath())
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"sentinel-secret", "sentinel-user", setup.Password} {
		if strings.Contains(string(raw), forbidden) {
			t.Errorf("settings file contains %q", forbidden)
		}
	}
	settings.Depots = settings.Depots[:2]
	if err := vaulticSaveSettings(settings); err != nil {
		t.Fatal(err)
	}
	var decoded map[string]any
	if json.Unmarshal(raw, &decoded) != nil {
		t.Fatal("settings are not valid JSON")
	}
	loaded, err := vaulticLoadSettings()
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded.Depots) != 2 || loaded.Depots[0].LastVerifiedState != "auth_failed" || loaded.Depots[0].LastVerified != stamp.Format(time.RFC3339) {
		t.Fatalf("verification not persisted: %+v", loaded.Depots)
	}
	if !strings.Contains(vaulticVerificationSummary(loaded.Depots[0]), "Authentification refusée") || vaulticVerificationSummary(loaded.Depots[1]) != "Jamais vérifié" {
		t.Fatalf("summaries: %q / %q", vaulticVerificationSummary(loaded.Depots[0]), vaulticVerificationSummary(loaded.Depots[1]))
	}
}

func TestVaulticEditSavedDepot(t *testing.T) {
	depots := []vaulticDepot{
		{Name: "A", Location: "gs:one:/x", LastVerified: "2026-01-01T00:00:00Z", LastVerifiedState: "connected"},
		{Name: "B", Location: "gs:two:/x"},
	}
	out, err := vaulticEditSavedDepot(depots, "gs:one:/x", "A bis", "gs:one:/x")
	if err != nil || out[0].Name != "A bis" || out[0].LastVerified == "" {
		t.Fatalf("rename must keep history: %+v %v", out, err)
	}
	out, err = vaulticEditSavedDepot(depots, "gs:one:/x", "A", "gs:three:/x")
	if err != nil || out[0].Location != "gs:three:/x" || out[0].LastVerified != "" {
		t.Fatalf("relocation must reset history: %+v %v", out, err)
	}
	if depots[0].Location != "gs:one:/x" {
		t.Fatal("input slice mutated")
	}
	for _, bad := range []struct{ old, name, loc string }{
		{"gs:one:/x", "B", "gs:one:/x"}, {"gs:one:/x", "A", "gs:two:/x"}, {"gs:one:/x", "", "gs:one:/x"},
		{"gs:one:/x", "A", "rest:https://u:p@h/r"}, {"gs:zzz:/x", "A", "gs:one:/x"},
	} {
		if _, err := vaulticEditSavedDepot(depots, bad.old, bad.name, bad.loc); err == nil {
			t.Errorf("edit %+v accepted", bad)
		}
	}
}

func TestVaulticForgettingNeverTouchesRepositoryData(t *testing.T) {
	repoPath, opts := initLocalTestRepository(t, "pw")
	before, _ := os.ReadDir(repoPath)
	depots := []vaulticDepot{{Name: "Local", Location: repoPath}}
	next, active, err := vaulticForgetSavedDepot(depots, repoPath, repoPath)
	if err != nil || len(next) != 0 || active != "" {
		t.Fatalf("forget: %v %v %v", next, active, err)
	}
	after, _ := os.ReadDir(repoPath)
	if len(before) != len(after) {
		t.Fatal("forgetting altered the repository directory")
	}
	if res := vaulticProbeRepository(context.Background(), opts, 30*time.Second, vaulticVerifyDepot); res.State != vaulticConnConnected {
		t.Fatalf("repository no longer opens after forgetting: %s", res.State.Label())
	}
}

// ---------------------------------------------------------------------------
// Interface

func TestVaulticStorageSetupScreen(t *testing.T) {
	test.NewTempApp(t)
	window := test.NewWindow(nil)
	defer window.Close()
	var got vaulticStorageSetup
	var action string
	ui := vaulticNewStorageSetupUI(vaulticStorageSetupHooks{
		Window: window,
		Submit: func(s vaulticStorageSetup, a string) { got, action = s, a },
	})
	window.SetContent(ui.Content)
	window.Resize(fyne.NewSize(900, 900))

	if len(ui.cards) != 9 {
		t.Fatalf("expected nine backend cards, got %d", len(ui.cards))
	}
	// Keyboard: focus a card, press Enter.
	window.Canvas().Focus(ui.cards[1])
	ui.cards[1].TypedKey(&fyne.KeyEvent{Name: fyne.KeyReturn})
	if ui.scheme != "sftp" || !ui.cards[1].selected || ui.cards[0].selected {
		t.Fatalf("keyboard selection failed: %s", ui.scheme)
	}
	// Arrow navigation moves the focus.
	ui.cards[1].TypedKey(&fyne.KeyEvent{Name: fyne.KeyRight})
	if window.Canvas().Focused() != ui.cards[2] {
		t.Fatal("arrow key did not move focus to the next card")
	}

	// Dynamic fields and inline validation.
	ui.setField("host", "-oProxyCommand=evil")
	ui.setField("user", "backup")
	ui.setField("path", "/srv/vaultic")
	ui.name.SetText("Serveur")
	ui.pass.SetText("pw")
	if !ui.openBtn.Disabled() || ui.control("host").(fyne.Validatable).Validate() == nil {
		t.Fatal("invalid host must disable actions and show an inline error on the field")
	}
	ui.setField("host", "host.example")
	ui.setField("port", "2222")
	if ui.problem.Text != "" || ui.openBtn.Disabled() || !strings.Contains(ui.preview.Text, "sftp://backup@host.example:2222//srv/vaultic") {
		t.Fatalf("valid form rejected: %q / %q", ui.problem.Text, ui.preview.Text)
	}
	test.Tap(ui.openBtn)
	if action != vaulticActionOpen || got.Address != "sftp://backup@host.example:2222//srv/vaultic" || got.Password != "pw" {
		t.Fatalf("submit: %s %+v", action, got)
	}

	// A test needs no password; creation and opening do.
	ui.pass.SetText("")
	if ui.testBtn.Disabled() || !ui.openBtn.Disabled() || !ui.createBtn.Disabled() {
		t.Fatal("password gating wrong")
	}
	test.Tap(ui.testBtn)
	if action != vaulticActionTest || got.Password != "" {
		t.Fatalf("test submit: %s %+v", action, got)
	}

	// Every backend renders a form.
	for _, info := range vaulticBackendCatalog {
		ui.SetScheme(info.Scheme)
		if len(ui.entries) != len(vaulticStorageFields(info.Scheme)) {
			t.Errorf("%s: form fields missing", info.Scheme)
		}
	}

	// Lifecycle states are visible.
	for s := vaulticConnNotTested; s <= vaulticConnConnected; s++ {
		ui.SetState(vaulticProbeResult{State: s, Detail: "detail"})
		if ui.badge.Text != s.Label() {
			t.Errorf("badge %q for %s", ui.badge.Text, s.Label())
		}
	}

	// Edit mode round-trips an address.
	if err := ui.Edit("Serveur", "sftp://backup@host.example:2222//srv/vaultic"); err != nil {
		t.Fatal(err)
	}
	if !ui.editing || ui.scheme != "sftp" || ui.values()["port"] != "2222" || ui.values()["path"] != "/srv/vaultic" || ui.saveBtn.Disabled() {
		t.Fatalf("edit mode: %+v", ui.values())
	}
	ui.Reset()
	if ui.editing || ui.name.Text != "" {
		t.Fatal("reset did not leave edit mode")
	}
}

func TestVaulticRepositoryRowShowsSafeInformation(t *testing.T) {
	test.NewTempApp(t)
	depot := vaulticDepot{Name: "Serveur", Location: "sftp://backup:oops@host.example:2222//vaultic"}
	var forgotten bool
	row, buttons := vaulticRepositoryRow(depot, true, vaulticRepositoryHooks{
		Connect: func(vaulticDepot) {}, Edit: func(vaulticDepot) {}, Test: func(vaulticDepot) {},
		Forget: func(vaulticDepot) { forgotten = true },
	})
	if len(buttons) != 4 {
		t.Fatalf("expected connect/edit/test/forget, got %d", len(buttons))
	}
	window := test.NewWindow(row)
	defer window.Close()
	window.Resize(fyne.NewSize(700, 300))
	texts := map[string]bool{}
	var walk func(o fyne.CanvasObject)
	walk = func(o fyne.CanvasObject) {
		if l, ok := o.(interface{ Text() string }); ok {
			texts[l.Text()] = true
		}
		if c, ok := o.(*fyne.Container); ok {
			for _, child := range c.Objects {
				walk(child)
			}
		}
	}
	walk(row)
	// The stored location contains a password: the row must never show it.
	if got := vaulticSafeLocation(depot.Location); strings.Contains(got, "oops") {
		t.Fatalf("password shown: %s", got)
	}
	test.Tap(buttons[3])
	if !forgotten {
		t.Fatal("forget hook not called")
	}
}
