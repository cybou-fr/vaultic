package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"fyne.io/fyne/v2/test"
)

func TestVaulticVersionComparison(t *testing.T) {
	cases := []struct {
		candidate, current string
		newer              bool
	}{
		{"v0.6.0", "0.5.0", true}, {"0.5.1", "0.5.0", true}, {"v1.0.0", "0.99.99", true},
		{"v0.5.0", "0.5.0", false}, {"v0.4.9", "0.5.0", false}, {"0.5.0-rc1", "0.5.0", false},
		{"v0.10.0", "0.9.0", true}, // numbers, not text
		{"v0.6.0", "dev", false}, {"latest", "0.5.0", false}, {"", "0.5.0", false}, {"1.2.3.4", "0.5.0", false},
	}
	for _, c := range cases {
		if got := vaulticNewerVersion(c.candidate, c.current); got != c.newer {
			t.Errorf("%q vs %q: got %v want %v", c.candidate, c.current, got, c.newer)
		}
	}
	if line := vaulticVersionLine(); !strings.Contains(line, vaulticVersion) {
		t.Fatalf("version line: %q", line)
	}
}

func updateServer(t *testing.T, status int, body string, hits *atomic.Int32) *httptest.Server {
	t.Helper()
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		if r.Header.Get("User-Agent") != "Vaultic-update-check" || r.Header.Get("Cookie") != "" || r.Header.Get("Authorization") != "" {
			t.Errorf("the request must be anonymous: %v", r.Header)
		}
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(s.Close)
	return s
}

func TestVaulticUpdateCheckOutcomes(t *testing.T) {
	var hits atomic.Int32
	newer := updateServer(t, 200, `{"tag_name":"v0.6.0","html_url":"https://github.com/cybou-fr/vaultic/releases/tag/v0.6.0"}`, &hits)
	if r, err := vaulticCheckForUpdate(context.Background(), newer.Client(), newer.URL, "0.5.0"); err != nil || !r.Available || r.Latest != "v0.6.0" || !strings.HasPrefix(r.URL, "https://github.com/") {
		t.Fatalf("newer release: %+v %v", r, err)
	}
	same := updateServer(t, 200, `{"tag_name":"v0.5.0","html_url":"https://github.com/cybou-fr/vaultic/releases/tag/v0.5.0"}`, &hits)
	if r, err := vaulticCheckForUpdate(context.Background(), same.Client(), same.URL, "0.5.0"); err != nil || r.Available {
		t.Fatalf("same version is up to date: %+v %v", r, err)
	}
	none := updateServer(t, 404, `{"message":"Not Found"}`, &hits)
	if r, err := vaulticCheckForUpdate(context.Background(), none.Client(), none.URL, "0.5.0"); err != nil || !r.None {
		t.Fatalf("no release yet: %+v %v", r, err)
	}
	limited := updateServer(t, 403, `rate limit`, &hits)
	if _, err := vaulticCheckForUpdate(context.Background(), limited.Client(), limited.URL, "0.5.0"); err == nil {
		t.Fatal("a rate limit must be reported")
	}
	broken := updateServer(t, 200, `<html>not json`, &hits)
	if _, err := vaulticCheckForUpdate(context.Background(), broken.Client(), broken.URL, "0.5.0"); err == nil {
		t.Fatal("an unreadable answer must be reported")
	}
	odd := updateServer(t, 500, ``, &hits)
	if _, err := vaulticCheckForUpdate(context.Background(), odd.Client(), odd.URL, "0.5.0"); err == nil {
		t.Fatal("a server error must be reported")
	}
	// The answer cannot choose where the user is sent.
	evil := updateServer(t, 200, `{"tag_name":"v9.0.0","html_url":"https://evil.example/download"}`, &hits)
	if r, _ := vaulticCheckForUpdate(context.Background(), evil.Client(), evil.URL, "0.5.0"); r.URL != vaulticReleasesPage {
		t.Fatalf("an address outside github.com must be ignored: %q", r.URL)
	}
	draft := updateServer(t, 200, `{"tag_name":"v9.0.0","draft":true}`, &hits)
	if r, _ := vaulticCheckForUpdate(context.Background(), draft.Client(), draft.URL, "0.5.0"); !r.None {
		t.Fatalf("a draft is not a release: %+v", r)
	}
	if _, err := vaulticCheckForUpdate(context.Background(), http.DefaultClient, "http://127.0.0.1:1/unreachable", "0.5.0"); err == nil {
		t.Fatal("an unreachable server must be reported")
	}
}

func TestVaulticUpdateControllerNeverContactsTheServerOnItsOwn(t *testing.T) {
	test.NewTempApp(t)
	var hits atomic.Int32
	s := updateServer(t, 200, `{"tag_name":"v0.6.0","html_url":"https://github.com/cybou-fr/vaultic/releases/tag/v0.6.0"}`, &hits)
	var opened string
	c := newVaulticUpdateController(vaulticUpdateHooks{Client: s.Client(), API: s.URL, Current: "0.5.0", Open: func(u string) { opened = u }})
	if hits.Load() != 0 || c.Open.Visible() {
		t.Fatal("nothing may be requested before the button is pressed")
	}
	c.show(vaulticUpdateResult{Latest: "v0.6.0", URL: "https://github.com/cybou-fr/vaultic/releases/tag/v0.6.0", Available: true}, nil)
	if !c.Open.Visible() || !strings.Contains(c.Result.Text, "v0.6.0") {
		t.Fatalf("an available update is shown: %q", c.Result.Text)
	}
	c.Open.OnTapped()
	if !strings.HasPrefix(opened, "https://github.com/") {
		t.Fatalf("the page is opened only on request: %q", opened)
	}
	c.show(vaulticUpdateResult{Latest: "v0.5.0"}, nil)
	if c.Open.Visible() || !strings.Contains(c.Result.Text, "à jour") {
		t.Fatalf("up to date: %q", c.Result.Text)
	}
}
