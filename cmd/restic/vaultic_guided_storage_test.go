package main

import (
	"strings"
	"testing"

	"github.com/restic/restic/internal/backend/all"
	"github.com/restic/restic/internal/backend/location"
)

func guidedValues(name string, extra map[string]string) map[string]string {
	v := map[string]string{"name": name, "password": "correct horse battery staple"}
	for k, val := range extra {
		v[k] = val
	}
	return v
}

// Every location built by the assistant must be accepted by the embedded
// restic parser and resolve to the expected backend.
func TestVaulticGuidedStorageAllNineBackends(t *testing.T) {
	tests := []struct {
		scheme string
		values map[string]string
		want   string
	}{
		{"local", map[string]string{"path": "/tmp/vaultic-test"}, "local:/tmp/vaultic-test"},
		{"sftp", map[string]string{"user": "backup", "host": "host.example", "path": "/vaultic"}, "sftp:backup@host.example:/vaultic"},
		{"sftp", map[string]string{"user": "backup", "host": "host.example", "port": "2222", "path": "/vaultic"}, "sftp://backup@host.example:2222//vaultic"},
		{"sftp", map[string]string{"user": "backup", "host": "host.example", "port": "2222", "path": "relative/dir"}, "sftp://backup@host.example:2222/relative/dir"},
		{"sftp", map[string]string{"user": "backup", "host": "[2001:db8::1]", "path": "/vaultic"}, "sftp://backup@[2001:db8::1]//vaultic"},
		{"rest", map[string]string{"url": "https://backup.example/repo"}, "rest:https://backup.example/repo"},
		{"rest", map[string]string{"url": "http://127.0.0.1:8000/repo"}, "rest:http://127.0.0.1:8000/repo"},
		{"gs", map[string]string{"bucket": "my-bucket", "prefix": "vaultic/primary"}, "gs:my-bucket:/vaultic/primary"},
		{"swift", map[string]string{"bucket": "my-container", "prefix": "backup"}, "swift:my-container:/backup"},
		{"rclone", map[string]string{"remote": "cloud", "prefix": "vaultic"}, "rclone:cloud:vaultic"},
		{"s3", map[string]string{"endpoint": "s3.amazonaws.com", "bucket": "b", "prefix": "vaultic"}, "s3:s3.amazonaws.com/b/vaultic"},
		{"b2", map[string]string{"bucket": "my-bucket", "prefix": "vaultic"}, "b2:my-bucket:vaultic"},
		{"azure", map[string]string{"account": "acct", "bucket": "c", "prefix": "vaultic"}, "azure:c:vaultic"},
	}
	seen := map[string]bool{}
	for _, tc := range tests {
		setup, err := vaulticBuildStorageSetup(tc.scheme, guidedValues("Test", tc.values))
		if err != nil || setup.Address != tc.want {
			t.Errorf("%s: got %q, error %v, want %q", tc.scheme, setup.Address, err, tc.want)
			continue
		}
		seen[tc.scheme] = true
		loc, err := location.Parse(all.Backends(), setup.Address)
		if err != nil || loc.Scheme != tc.scheme {
			t.Errorf("%s: engine parser gave scheme %q, error %v", tc.scheme, loc.Scheme, err)
		}
		if setup.Password != "correct horse battery staple" || strings.Contains(setup.Address, setup.Password) {
			t.Errorf("%s: restic password mishandled", tc.scheme)
		}
		// The address must round-trip through the edit form.
		scheme, back, err := vaulticParseStorageAddress(setup.Address)
		if err != nil || scheme != tc.scheme || len(back) == 0 {
			t.Errorf("%s: parse back failed: %v %v %v", tc.scheme, scheme, back, err)
		}
	}
	if len(seen) != 9 {
		t.Fatalf("expected all nine backends covered, got %v", seen)
	}
	for _, info := range vaulticBackendCatalog {
		if len(vaulticStorageFields(info.Scheme)) == 0 {
			t.Errorf("%s has no guided form", info.Scheme)
		}
		if _, ok := vaulticBackendIcons[info.Scheme]; !ok {
			t.Errorf("%s has no icon", info.Scheme)
		}
	}
}

func TestVaulticGuidedSFTPPortUsesEngineURLForm(t *testing.T) {
	address, err := vaulticBuildGuidedStorageAddress(vaulticGuidedStorageForm{Scheme: "sftp", Username: "u", Host: "h.example", Port: "2200", Path: "/repo"})
	if err != nil {
		t.Fatal(err)
	}
	loc, err := location.Parse(all.Backends(), address)
	if err != nil {
		t.Fatal(err)
	}
	_, values, _ := vaulticParseStorageAddress(address)
	if values["port"] != "2200" || values["host"] != "h.example" || values["user"] != "u" || values["path"] != "/repo" {
		t.Fatalf("port or absolute path lost by engine parser: %v (%v)", values, loc.Scheme)
	}
}

func TestVaulticGuidedStorageRejectsUnsafeInputs(t *testing.T) {
	tests := []vaulticGuidedStorageForm{
		{Scheme: "rest", Path: "http://backup.example/repo"},
		{Scheme: "rest", Path: "https://user:secret@backup.example/repo"},
		{Scheme: "rest", Path: "https://backup.example/repo?token=abc"},
		{Scheme: "rest", Path: "https://backup.example/repo#secret"},
		{Scheme: "rest", Path: "ftp://backup.example/repo"},
		{Scheme: "rest", Path: ""},
		{Scheme: "sftp", Username: "user@evil", Host: "host", Path: "/repo"},
		{Scheme: "sftp", Username: "-oProxyCommand=x", Host: "host", Path: "/repo"},
		{Scheme: "sftp", Username: "user", Host: "-oProxyCommand=calc", Path: "/repo"},
		{Scheme: "sftp", Username: "user", Host: "host name", Path: "/repo"},
		{Scheme: "sftp", Username: "user", Host: "host;rm", Path: "/repo"},
		{Scheme: "sftp", Username: "user", Host: "host", Path: "/repo/../../etc"},
		{Scheme: "sftp", Username: "user", Host: "host", Path: "~/repo"},
		{Scheme: "sftp", Username: "user", Host: "host", Path: "/"},
		{Scheme: "sftp", Username: "user", Host: "host", Path: "/repo", Port: "0"},
		{Scheme: "sftp", Username: "user", Host: "host", Path: "/repo", Port: "70000"},
		{Scheme: "sftp", Username: "user", Host: "host", Path: "/repo", Port: "22abc"},
		{Scheme: "sftp", Username: "user", Host: "[not-ip]", Path: "/repo"},
		{Scheme: "gs", Bucket: "bucket", Prefix: "../escape"},
		{Scheme: "gs", Bucket: "bucket", Prefix: "a\\b"},
		{Scheme: "gs", Bucket: "a/b"},
		{Scheme: "swift", Bucket: "container", Prefix: "folder//sub"},
		{Scheme: "swift", Bucket: ""},
		{Scheme: "rclone", Remote: "remote:other", Prefix: "repo"},
		{Scheme: "rclone", Remote: "--config=/etc/passwd", Prefix: "repo"},
		{Scheme: "rclone", Remote: "ok", Prefix: "a/../b"},
		{Scheme: "local", Path: ""},
		{Scheme: "local", Path: "x\ny"},
		{Scheme: "local", Path: "https://example/x"},
		{Scheme: "unknown", Path: "/repo"},
	}
	for _, form := range tests {
		address, err := vaulticBuildGuidedStorageAddress(form)
		if err == nil {
			t.Errorf("unsafe %s input %+v accepted as %q", form.Scheme, form, address)
		}
	}
}

func TestVaulticStorageFieldValidationIsInline(t *testing.T) {
	bad := []struct{ scheme, key, value string }{
		{"sftp", "host", "-oBad"}, {"sftp", "port", "99999"}, {"sftp", "user", "a b"}, {"sftp", "path", ".."},
		{"rest", "url", "http://remote.example/x"}, {"gs", "bucket", "a/b"}, {"gs", "prefix", "../x"},
		{"rclone", "remote", "a:b"}, {"local", "path", ""}, {"swift", "bucket", ""}, {"gs", "project", "bad project!"},
		{"s3", "endpoint", "https://x/y"},
	}
	for _, tc := range bad {
		if err := vaulticValidateStorageField(tc.scheme, tc.key, tc.value); err == nil {
			t.Errorf("%s.%s=%q accepted", tc.scheme, tc.key, tc.value)
		}
	}
	good := []struct{ scheme, key, value string }{
		{"sftp", "host", "backup.example.org"}, {"sftp", "port", ""}, {"sftp", "port", "2222"}, {"rest", "url", "https://x.example/r"},
		{"gs", "project", "my-project-123"}, {"gs", "prefix", ""}, {"rest", "username", ""},
	}
	for _, tc := range good {
		if err := vaulticValidateStorageField(tc.scheme, tc.key, tc.value); err != nil {
			t.Errorf("%s.%s=%q rejected: %v", tc.scheme, tc.key, tc.value, err)
		}
	}
}

func TestVaulticStorageSetupRejectsInvalidInput(t *testing.T) {
	cases := []struct {
		name   string
		scheme string
		values map[string]string
	}{
		{"no name", "local", map[string]string{"name": "", "password": "x", "path": "/r"}},
		{"no password", "local", map[string]string{"name": "n", "password": "", "path": "/r"}},
		{"unknown backend", "ftp", guidedValues("n", map[string]string{"path": "/r"})},
		{"rest half credentials", "rest", guidedValues("n", map[string]string{"url": "https://h.example/r", "username": "u"})},
		{"s3 half credentials", "s3", guidedValues("n", map[string]string{"endpoint": "s3.amazonaws.com", "bucket": "b", "access": "id"})},
		{"azure key without account", "azure", guidedValues("n", map[string]string{"bucket": "c", "secret": "k"})},
		{"query in prefix", "b2", guidedValues("n", map[string]string{"bucket": "b", "prefix": "x?y=1"})},
	}
	for _, tc := range cases {
		if _, err := vaulticBuildStorageSetup(tc.scheme, tc.values); err == nil {
			t.Errorf("%s: accepted", tc.name)
		}
	}
}

func TestVaulticGuidedStorageKeepsSecretsOutOfLocations(t *testing.T) {
	setup, err := vaulticBuildStorageSetup("rest", guidedValues("REST", map[string]string{
		"url": "https://backup.example/snapshots", "username": "alice", "secret": "s3cr3t-rest-pass",
	}))
	if err != nil {
		t.Fatal(err)
	}
	if setup.Address != "rest:https://backup.example/snapshots" {
		t.Fatalf("unexpected address %q", setup.Address)
	}
	for _, secret := range []string{"s3cr3t-rest-pass", "alice", setup.Password} {
		if strings.Contains(setup.Address, secret) || strings.Contains(vaulticSafeLocation(setup.Address), secret) {
			t.Fatalf("secret %q leaked into location", secret)
		}
	}
	if setup.Auth.Secret != "s3cr3t-rest-pass" || setup.Auth.AccessID != "alice" {
		t.Fatal("REST credentials must be carried in the session auth only")
	}
}

func TestVaulticSafeLocationStripsCredentials(t *testing.T) {
	tests := map[string]string{
		"rest:https://user:pw@host.example/repo?token=abc#frag": "rest:https://host.example/repo",
		"sftp://alice:pw@host:22//repo":                         "sftp://alice@host:22//repo",
		"sftp:alice@host:/repo":                                 "sftp:alice@host:/repo",
		"gs:bucket:/x?secret=1":                                 "gs:bucket:/x",
	}
	for in, want := range tests {
		got := vaulticSafeLocation(in)
		if got != want {
			t.Errorf("%q: got %q want %q", in, got, want)
		}
		if strings.Contains(got, "pw") || strings.Contains(got, "token") || strings.Contains(got, "secret") {
			t.Errorf("leak in %q", got)
		}
	}
}
