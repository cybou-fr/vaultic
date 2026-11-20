package main

import (
	"strings"
	"testing"
)

func TestVaulticSwiftAuthURLHardening(t *testing.T) {
	valid := map[string]string{
		"https://auth.example/v3":              "https://auth.example/v3",
		"https://auth.example/v3/":             "https://auth.example/v3",
		"HTTPS://Auth.Example:5000/v3":         "https://Auth.Example:5000/v3",
		"https://auth.example/identity/v3":     "https://auth.example/identity/v3",
		"https://[2001:db8::1]:5000/v3":        "https://[2001:db8::1]:5000/v3",
		"https://10.0.0.5/v3":                  "https://10.0.0.5/v3",
		"http://localhost:5000/v3":             "http://localhost:5000/v3",
		"http://127.0.0.1:5000/v3":             "http://127.0.0.1:5000/v3",
		"http://[::1]:5000/v3":                 "http://[::1]:5000/v3",
		"https://a-b.c-d.example.org:65535/v3": "https://a-b.c-d.example.org:65535/v3",
	}
	for in, want := range valid {
		got, err := vaulticSwiftAuthURL(in)
		if err != nil || got != want {
			t.Errorf("%q: got %q, %v; want %q", in, got, err, want)
		}
	}
	invalid := []string{
		"", " ", " https://auth.example/v3", "https://auth.example/v3 ",
		"https://auth.example/v3\n", "https://auth.example/v3\x00", "https://auth.example/v3\r\nHost: evil",
		"https://user@auth.example/v3", "https://user:pw@auth.example/v3", "https://@auth.example/v3",
		"https://auth.example@evil.example/v3",
		"https://auth.example/v3?", "https://auth.example/v3?x=1", "https://auth.example/v3#", "https://auth.example/v3#frag",
		"http://auth.example/v3", "http://10.0.0.5/v3", "ftp://auth.example/v3", "file:///v3", "javascript:alert(1)//v3",
		"//auth.example/v3", "auth.example/v3", "https:auth.example/v3", "https:///v3", "https://",
		"https://auth.example", "https://auth.example/", "https://auth.example/v2.0", "https://auth.example/v3x",
		"https://auth.example/v3/extra", "https://auth.example/api/v3-old", "https://v3.example/",
		"https://auth.example//v3", "https://auth.example/./v3", "https://auth.example/a/../v3",
		"https://auth.example/%2e%2e/v3", "https://auth.example/v3%2f", "https://auth.example/%76%33",
		"https://auth.example:0/v3", "https://auth.example:99999/v3", "https://auth.example:abc/v3",
		"https://auth.example:/v3", "https://auth.example:080808/v3",
		"https://auth example/v3", "https://auth_example/v3", "https://-auth.example/v3", "https://auth..example/v3",
		"https://auth.example\\evil/v3", "https://auth.example/v3\\", "https://[::1/v3", "https://[nothex]/v3",
		"https://\u0430uth.example/v3", // Cyrillic "a": must be punycode
		"https://" + strings.Repeat("a", 64) + ".example/v3",
		"https://auth.example/" + strings.Repeat("a", 600) + "/v3",
	}
	for _, in := range invalid {
		if got, err := vaulticSwiftAuthURL(in); err == nil {
			t.Errorf("%q accepted as %q", in, got)
		}
	}
}

func TestVaulticSwiftAuthURLIsStoredNormalized(t *testing.T) {
	var store vaulticCloudSessions
	auth := vaulticCloudAuth{Scheme: "swift", AuthURL: "HTTPS://Auth.Example/v3/", AccessID: "u", Secret: "p", UserDomain: "D", Project: "P", ProjectDomain: "D"}
	if err := store.set("swift:c:/r", auth); err != nil {
		t.Fatal(err)
	}
	if got := store.get("swift:c:/r").AuthURL; got != "https://Auth.Example/v3" {
		t.Fatalf("stored %q", got)
	}
	auth.AuthURL = "https://auth.example/v3?x=1"
	if err := store.set("swift:c:/r2", auth); err == nil {
		t.Fatal("unsafe URL stored")
	}
	// The error message must not echo secrets or the unsafe URL's credentials.
	auth.AuthURL = "https://user:hunter2@auth.example/v3"
	err := store.set("swift:c:/r3", auth)
	if err == nil || strings.Contains(err.Error(), "hunter2") {
		t.Fatalf("error echoes embedded credentials: %v", err)
	}
}

// REST URLs receive the same host and port scrutiny.
func TestVaulticRESTURLHostAndPortChecks(t *testing.T) {
	for _, bad := range []string{
		"https://host:99999/r", "https://host:0/r", "https://ho st/r", "https://host:/r", "https://-host/r", "https://host\\x/r",
	} {
		if _, err := vaulticBuildGuidedStorageAddress(vaulticGuidedStorageForm{Scheme: "rest", Path: bad}); err == nil {
			t.Errorf("REST URL %q accepted", bad)
		}
	}
	if _, err := vaulticBuildGuidedStorageAddress(vaulticGuidedStorageForm{Scheme: "rest", Path: "https://host.example:8443/repo"}); err != nil {
		t.Errorf("valid REST URL rejected: %v", err)
	}
}
