package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/restic/restic/internal/global"
)

const (
	sentinelRestic = "R3stic-Sentinel-Pass"
	sentinelCloud  = "Cl0ud/Sentinel+Secret"
)

func assertNoSecrets(t *testing.T, where, text string, secrets ...string) {
	t.Helper()
	for _, secret := range secrets {
		if secret != "" && strings.Contains(text, secret) {
			t.Errorf("%s leaks %q: %s", where, secret, text)
		}
	}
}

// Engine and server errors sometimes echo what was sent. Whatever shape the
// echo takes, the text shown to the user must not contain the secrets.
func TestVaulticProbeDetailsAreRedactedWhateverTheEchoLooksLike(t *testing.T) {
	auth := vaulticCloudAuth{Scheme: "rest", AccessID: "alice", Secret: sentinelCloud}
	basic := base64.StdEncoding.EncodeToString([]byte("alice:" + sentinelCloud))
	jsonEscaped, _ := json.Marshal(sentinelCloud)
	echoes := map[string]string{
		"plain":            "login failed for " + sentinelCloud,
		"restic password":  "bad key " + sentinelRestic,
		"url userinfo":     "Get https://alice:" + url.QueryEscape(sentinelCloud) + "@host.example/repo?token=" + sentinelCloud + ": connection refused",
		"query escaped":    "dial tcp: password=" + url.QueryEscape(sentinelCloud),
		"path escaped":     "GET /" + url.PathEscape(sentinelCloud) + " 500",
		"json body":        `{"error":"denied","echo":` + string(jsonEscaped) + `}`,
		"basic header":     "Authorization: Basic " + basic + " rejected (401)",
		"mixed multi-line": "first\n" + sentinelRestic + "\nthen " + sentinelCloud + "\n",
	}
	for name, message := range echoes {
		g := global.Options{Password: sentinelRestic}
		res := vaulticProbeRedacted(context.Background(), g, 5*time.Second, auth, func(context.Context, global.Options) error {
			return errors.New(message)
		})
		assertNoSecrets(t, name, res.Detail, sentinelRestic, sentinelCloud, url.QueryEscape(sentinelCloud), url.PathEscape(sentinelCloud), basic, string(jsonEscaped[1:len(jsonEscaped)-1]))
		if strings.Contains(res.Detail, "token=") {
			t.Errorf("%s: URL query survived: %s", name, res.Detail)
		}
		if res.Detail == "" {
			t.Errorf("%s: no diagnostic at all", name)
		}
	}
}

// A real server that reflects the whole request back in an error page.
func TestVaulticEchoingServersDoNotLeakThroughTheEngine(t *testing.T) {
	echo := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprintf(w, "echo %s %s auth=%s body=%s", r.Method, r.URL.String(), r.Header.Get("Authorization"), body)
	}))
	defer echo.Close()

	rest := vaulticCloudAuth{Scheme: "rest", AccessID: "alice", Secret: sentinelCloud}
	g := global.Options{
		Repo: "rest:" + echo.URL + "/repo", Password: sentinelRestic, Backends: vaulticBackendsForSession(rest),
		Term: &vaulticTerminal{output: io.Discard}, JSON: true, Verbosity: 1,
	}
	res := vaulticProbeRedacted(context.Background(), g, 8*time.Second, rest, vaulticVerifyDepot)
	assertNoSecrets(t, "rest detail", res.Detail, sentinelRestic, sentinelCloud, url.QueryEscape(sentinelCloud))

	swiftAuth := vaulticCloudAuth{
		Scheme: "swift", AuthURL: echo.URL + "/v3", AccessID: "alice", Secret: sentinelCloud,
		UserDomain: "Default", Project: "demo", ProjectDomain: "Default",
	}
	g = swiftOptions(swiftAuth, swiftRepoAddress)
	g.Password = sentinelRestic
	res = vaulticProbeRedacted(context.Background(), g, 8*time.Second, swiftAuth, vaulticVerifyDepot)
	assertNoSecrets(t, "swift detail", res.Detail, sentinelRestic, sentinelCloud, url.QueryEscape(sentinelCloud))
	if res.Detail == "" || res.State == vaulticConnConnected {
		t.Fatalf("unexpected result: %s %q", res.State.Label(), res.Detail)
	}
}

// Whatever the user types into the form, the validation messages must never
// repeat a secret back.
func TestVaulticFormErrorsDoNotEchoSecrets(t *testing.T) {
	cases := []struct {
		scheme string
		values map[string]string
	}{
		{"rest", map[string]string{"url": "https://alice:" + sentinelCloud + "@host.example/r"}},
		{"rest", map[string]string{"url": "https://host.example/r?token=" + sentinelCloud}},
		{"swift", map[string]string{"bucket": "c", "authurl": "https://alice:" + sentinelCloud + "@auth.example/v3", "username": "u", "secret": sentinelCloud}},
		{"swift", map[string]string{"bucket": "c", "authurl": "http://auth.example/v3?pw=" + sentinelCloud, "username": "u", "secret": sentinelCloud}},
		{"swift", map[string]string{"bucket": "c", "username": "u", "secret": sentinelCloud}},
		{"s3", map[string]string{"endpoint": "https://x/" + sentinelCloud, "bucket": "b"}},
		{"b2", map[string]string{"bucket": "my-bucket", "prefix": "a?" + sentinelCloud, "access": "id"}},
		{"azure", map[string]string{"bucket": "c", "secret": sentinelCloud}},
		{"sftp", map[string]string{"host": "h", "user": "u:" + sentinelCloud, "path": "/r"}},
		{"gs", map[string]string{"bucket": "b/" + sentinelCloud}},
	}
	for _, tc := range cases {
		_, err := vaulticBuildStorageSetup(tc.scheme, guidedValues("n", tc.values))
		if err == nil {
			t.Errorf("%s %v: accepted", tc.scheme, tc.values)
			continue
		}
		assertNoSecrets(t, tc.scheme+" validation message", err.Error(), sentinelCloud, sentinelRestic)
		for key, value := range tc.values {
			if err := vaulticValidateStorageField(tc.scheme, key, value); err != nil {
				assertNoSecrets(t, tc.scheme+"."+key+" inline message", err.Error(), sentinelCloud)
			}
		}
	}
}

// Fill every secret field of every backend with sentinels and prove that no
// persisted representation can contain them.
func TestVaulticNoSecretsInSettingsForAnyBackend(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("APPDATA", dir)
	t.Setenv("XDG_CONFIG_HOME", dir)
	t.Setenv("HOME", dir)
	forms := map[string]map[string]string{
		"local":  {"path": filepath.Join(dir, "repo")},
		"sftp":   {"user": "backup", "host": "h.example", "path": "/r"},
		"rest":   {"url": "https://h.example/r", "username": "alice", "secret": sentinelCloud},
		"s3":     {"endpoint": "s3.example", "bucket": "bkt", "access": "AKIASENTINEL", "secret": sentinelCloud},
		"b2":     {"bucket": "my-bucket", "access": "B2SENTINELID", "secret": sentinelCloud},
		"azure":  {"account": "acct", "bucket": "c", "secret": sentinelCloud},
		"gs":     {"bucket": "b", "project": "proj-sentinel-1"},
		"swift":  {"bucket": "c", "authurl": "https://auth.example/v3", "username": "swift-user-sentinel", "secret": sentinelCloud, "userdomain": "Default", "project": "demo", "projectdomain": "Default"},
		"rclone": {"remote": "cloud"},
	}
	var depots []vaulticDepot
	var store vaulticCloudSessions
	var combined strings.Builder
	for scheme, values := range forms {
		v := guidedValues("Dépôt "+scheme, values)
		v["password"] = sentinelRestic
		setup, err := vaulticBuildStorageSetup(scheme, v)
		if err != nil {
			t.Fatalf("%s: %v", scheme, err)
		}
		if authHasValuesForTest(setup.Auth) {
			if err := store.set(setup.Address, setup.Auth); err != nil {
				t.Fatalf("%s: %v", scheme, err)
			}
		}
		depots = append(depots, vaulticDepot{Name: setup.Name, Location: setup.Address})
		combined.WriteString(vaulticSafeLocation(setup.Address) + "\n")
	}
	settings := vaulticSettings{Repository: depots[0].Location, Depots: depots, Profiles: []vaulticBackupProfile{{Name: "Par défaut"}}}
	if err := vaulticSaveSettings(settings); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(vaulticSettingsPath())
	if err != nil {
		t.Fatal(err)
	}
	if len(depots) != 9 {
		t.Fatalf("expected nine backends, got %d", len(depots))
	}
	combined.Write(raw)
	for _, secret := range []string{sentinelRestic, sentinelCloud, "AKIASENTINEL", "B2SENTINELID", "swift-user-sentinel", "alice", "auth.example"} {
		assertNoSecrets(t, "settings and displayed locations", combined.String(), secret)
	}
	// The session store, not the settings file, holds them, and only until cleared.
	if !store.get(depots[0].Location).hasValues() && !store.get(depots[2].Location).hasValues() && !store.get(depots[7].Location).hasValues() {
		t.Log("session store content not inspected for local only setups")
	}
	store.clearAll()
	for _, depot := range depots {
		if store.get(depot.Location).hasValues() {
			t.Fatalf("session credentials survived for %s", depot.Name)
		}
	}
}

func authHasValuesForTest(a vaulticCloudAuth) bool { return a.hasValues() }

// Credentials are held as strings, so "clearing" means dropping every reference:
// nothing about them may remain reachable from the session store afterwards.
func TestVaulticSessionStoreForgetsEverything(t *testing.T) {
	var store vaulticCloudSessions
	for _, repo := range []string{"rest:https://a.example/r", "rest:https://b.example/r"} {
		if err := store.set(repo, vaulticCloudAuth{Scheme: "rest", AccessID: "u", Secret: sentinelCloud}); err != nil {
			t.Fatal(err)
		}
	}
	store.forget("rest:https://a.example/r")
	if store.get("rest:https://a.example/r").hasValues() || !store.get("rest:https://b.example/r").hasValues() {
		t.Fatal("forget must remove only the named repository")
	}
	store.clearAll()
	if len(store.entries) != 0 {
		t.Fatalf("%d entries left", len(store.entries))
	}
}
