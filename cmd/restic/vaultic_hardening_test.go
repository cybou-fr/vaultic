package main

import (
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/widget"
)

// settledGoroutines waits for background goroutines to wind down and returns
// the final count, so leaks show up as a persistent difference.
func settledGoroutines(target int, wait time.Duration) int {
	deadline := time.Now().Add(wait)
	for {
		runtime.GC()
		n := runtime.NumGoroutine()
		if n <= target || time.Now().After(deadline) {
			return n
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// Cancelled and timed-out probes must not leave goroutines or TCP connections
// behind, even when the remote end never answers.
func TestVaulticProbeCancellationDoesNotLeak(t *testing.T) {
	var open atomic.Int32
	hang := make(chan struct{})
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body) // lets the server notice a client that went away
		select {
		case <-hang:
		case <-r.Context().Done():
		}
	}))
	server.Config.ConnState = func(_ net.Conn, state http.ConnState) {
		switch state {
		case http.StateNew:
			open.Add(1)
		case http.StateClosed, http.StateHijacked:
			open.Add(-1)
		}
	}
	server.Start()
	defer server.Close()
	defer close(hang)

	cases := map[string]vaulticCloudAuth{
		"swift": {Scheme: "swift", AuthURL: server.URL + "/v3", AccessID: "u", Secret: "p", UserDomain: "Default", Project: "x", ProjectDomain: "Default"},
		"rest":  {Scheme: "rest", AccessID: "u", Secret: "p"},
	}
	repos := map[string]string{"swift": "swift:vault:/repo", "rest": "rest:" + server.URL + "/repo"}
	// Warm up lazily started runtime goroutines before measuring.
	_ = swiftProbe(cases["swift"], 100*time.Millisecond)
	time.Sleep(200 * time.Millisecond)
	baseline := settledGoroutines(0, 300*time.Millisecond)

	const rounds = 15
	for name, auth := range cases {
		for i := 0; i < rounds; i++ {
			ctx, cancel := context.WithCancel(context.Background())
			time.AfterFunc(40*time.Millisecond, cancel)
			res := vaulticProbeRepository(ctx, swiftOptions(auth, repos[name]), time.Minute, vaulticVerifyDepot)
			if res.State != vaulticConnNotTested {
				t.Fatalf("%s round %d: cancellation reported %s (%s)", name, i, res.State.Label(), res.Detail)
			}
			res = vaulticProbeRepository(context.Background(), swiftOptions(auth, repos[name]), 40*time.Millisecond, vaulticVerifyDepot)
			if res.State != vaulticConnUnavailable {
				t.Fatalf("%s round %d: timeout reported %s", name, i, res.State.Label())
			}
		}
	}
	// Give transports a chance to notice the closed requests.
	if n := settledGoroutines(baseline+4, 10*time.Second); n > baseline+4 {
		buf := make([]byte, 1<<16)
		buf = buf[:runtime.Stack(buf, true)]
		t.Fatalf("goroutines grew from %d to %d after %d cancelled probes:\n%s", baseline, n, 2*2*rounds, buf)
	}
	deadline := time.Now().Add(10 * time.Second)
	for open.Load() > 0 && time.Now().Before(deadline) {
		time.Sleep(50 * time.Millisecond)
	}
	if n := open.Load(); n > 0 {
		t.Fatalf("%d connections still open after cancelled probes", n)
	}
}

// Probes against a repository that opens normally must also be clean.
func TestVaulticProbeSuccessDoesNotLeak(t *testing.T) {
	_, opts := initLocalTestRepository(t, "pw")
	opts.Term = &vaulticTerminal{output: io.Discard}
	_ = vaulticProbeRepository(context.Background(), opts, 30*time.Second, vaulticVerifyDepot)
	baseline := settledGoroutines(0, 500*time.Millisecond)
	for i := 0; i < 10; i++ {
		if res := vaulticProbeRepository(context.Background(), opts, 30*time.Second, vaulticVerifyDepot); res.State != vaulticConnConnected {
			t.Fatalf("round %d: %s (%s)", i, res.State.Label(), res.Detail)
		}
	}
	if n := settledGoroutines(baseline+3, 10*time.Second); n > baseline+3 {
		t.Fatalf("goroutines grew from %d to %d", baseline, n)
	}
}

// Keyboard traversal: Tab must be able to reach every backend card, and the
// arrow keys must then move between them.
func TestVaulticTabReachesBackendCards(t *testing.T) {
	test.NewTempApp(t)
	u := vaulticNewStorageSetupUI(vaulticStorageSetupHooks{})
	// Same structure as the Dépôts page: a password entry, then the setup
	// surface inside a scroller that is shorter than its content.
	pass := widget.NewPasswordEntry()
	window := test.NewWindow(container.NewScroll(container.NewVBox(pass, u.Content)))
	defer window.Close()
	window.Resize(fyne.NewSize(700, 360))
	canvas := window.Canvas()
	seen := map[fyne.Focusable]bool{}
	for i := 0; i < 60; i++ {
		canvas.FocusNext()
		if f := canvas.Focused(); f != nil {
			seen[f] = true
		}
	}
	for _, card := range u.cards {
		if !seen[card] {
			t.Errorf("Tab never reaches the %s card", card.info.Scheme)
		}
	}
}

// Tabbing into content below the fold must scroll it into view.
func TestVaulticFocusedCardIsScrolledIntoView(t *testing.T) {
	test.NewTempApp(t)
	u := vaulticNewStorageSetupUI(vaulticStorageSetupHooks{})
	scroll := container.NewScroll(container.NewVBox(widget.NewLabel("x"), u.Content))
	window := test.NewWindow(scroll)
	defer window.Close()
	window.Resize(fyne.NewSize(700, 320))
	last := u.cards[len(u.cards)-1]
	window.Canvas().Focus(last)
	vaulticRevealFocused(window.Canvas(), []*container.Scroll{scroll})
	d := fyne.CurrentApp().Driver()
	top := d.AbsolutePositionForObject(last).Y - d.AbsolutePositionForObject(scroll).Y
	if top < 0 || top+last.Size().Height > scroll.Size().Height {
		t.Fatalf("focused card not visible: top %.0f height %.0f viewport %.0f offset %.0f", top, last.Size().Height, scroll.Size().Height, scroll.Offset.Y)
	}
	// And back up to the first control.
	window.Canvas().Focus(u.cards[0])
	vaulticRevealFocused(window.Canvas(), []*container.Scroll{scroll})
	top = d.AbsolutePositionForObject(u.cards[0]).Y - d.AbsolutePositionForObject(scroll).Y
	if top < 0 || top+u.cards[0].Size().Height > scroll.Size().Height {
		t.Fatalf("first card not visible after scrolling back: top %.0f", top)
	}
}

// The window can be as narrow as 640 px, which leaves roughly 400 px for the
// page content next to the sidebar. Nothing on the Dépôts page may demand more.
func TestVaulticRepositoriesPageFitsNarrowWindow(t *testing.T) {
	test.NewTempApp(t)
	const limit = 400
	u := vaulticNewStorageSetupUI(vaulticStorageSetupHooks{})
	for _, info := range vaulticBackendCatalog {
		u.SetScheme(info.Scheme)
		u.SetState(vaulticProbeResult{State: vaulticConnUnavailable, Detail: strings.Repeat("Erreur réseau très longue ", 8)})
		if w := u.Content.MinSize().Width; w > limit {
			t.Errorf("%s setup needs %.0f px (limit %d)", info.Scheme, w, limit)
		}
	}
	hooks := vaulticRepositoryHooks{Connect: func(vaulticDepot) {}, Edit: func(vaulticDepot) {}, Test: func(vaulticDepot) {}, Forget: func(vaulticDepot) {}}
	row, _ := vaulticRepositoryRow(vaulticDepot{Name: "Un nom de dépôt assez long pour tester", Location: "sftp://backup@un-serveur-tres-long.example.org:2222//srv/vaultic/depot", LastVerified: "2026-11-20T13:03:00Z", LastVerifiedState: "auth_failed"}, true, hooks)
	if w := row.MinSize().Width; w > limit {
		t.Errorf("repository row needs %.0f px (limit %d)", w, limit)
	}
}
