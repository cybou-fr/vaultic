package main

import (
	"context"
	"strings"
	"testing"
	"time"

	"fyne.io/fyne/v2/test"

	"github.com/restic/restic/internal/global"
)

func TestVaulticMaintenanceControllerShowResetAndState(t *testing.T) {
	test.NewTempApp(t)
	var titles []string
	c := newVaulticMaintenanceController(vaulticMaintenanceHooks{
		Run: func(title string, fn func(context.Context, global.Options) error) { titles = append(titles, title) },
	})
	if c.Volume.Text != "—" || c.Updated.Text != vaulticNoStorageAnalysis || len(c.Buttons()) != 2 {
		t.Fatal("initial state")
	}
	c.Check.OnTapped()
	c.Analyze.OnTapped()
	if len(titles) != 2 || titles[0] != "Vérification" || titles[1] != "Analyse du stockage" {
		t.Fatalf("operations started: %v", titles)
	}
	when := time.Date(2026, 6, 10, 14, 3, 0, 0, time.Local)
	c.Show(vaulticStorageStatistics{TotalSize: 2048, TotalBlobCount: 12, SnapshotsCount: 3}, when)
	if c.Blobs.Text != "12" || c.Snapshots.Text != "3" || c.Volume.Text == "—" {
		t.Fatalf("figures: %q %q %q", c.Volume.Text, c.Blobs.Text, c.Snapshots.Text)
	}
	if !strings.Contains(c.Updated.Text, "10/06/2026 à 14:03") {
		t.Fatalf("analysis time: %q", c.Updated.Text)
	}
	c.Reset()
	if c.Volume.Text != "—" || c.Blobs.Text != "—" || c.Snapshots.Text != "—" || c.Updated.Text != vaulticNoStorageAnalysis {
		t.Fatal("Reset must forget the analysis")
	}
	c.UpdateState(false, false)
	if !c.Analyze.Disabled() {
		t.Fatal("no repository: nothing to analyse")
	}
	c.UpdateState(false, true)
	if c.Analyze.Disabled() {
		t.Fatal("analysis available with a repository")
	}
	c.UpdateState(true, true)
	if !c.Analyze.Disabled() {
		t.Fatal("disabled while busy")
	}
}

type closerFixture struct {
	c             *vaulticWindowCloser
	busy, closing bool
	canCancel     bool
	closed, begun int
	asked         []func(bool)
}

func newCloserFixture() *closerFixture {
	f := &closerFixture{canCancel: true}
	f.c = &vaulticWindowCloser{
		Busy: func() bool { return f.busy }, Closing: func() bool { return f.closing },
		BeginClose: func() bool { f.begun++; return f.canCancel },
		CloseNow:   func() { f.closed++ },
		Confirm:    func(title, message string, answer func(bool)) { f.asked = append(f.asked, answer) },
	}
	return f
}

func TestVaulticWindowCloserClosesImmediatelyWhenIdle(t *testing.T) {
	f := newCloserFixture()
	f.c.Intercept()
	if f.closed != 1 || len(f.asked) != 0 {
		t.Fatalf("idle close: closed=%d asked=%d", f.closed, len(f.asked))
	}
}

func TestVaulticWindowCloserAsksWhileBusyAndCancelsOnYes(t *testing.T) {
	f := newCloserFixture()
	f.busy = true
	f.c.Intercept()
	f.c.Intercept() // a second click while the question is open does nothing
	if len(f.asked) != 1 || f.closed != 0 {
		t.Fatalf("must ask once: asked=%d closed=%d", len(f.asked), f.closed)
	}
	f.asked[0](false)
	if f.begun != 0 || f.closed != 0 {
		t.Fatal("answering no keeps the application open")
	}
	f.c.Intercept()
	f.asked[1](true)
	if f.begun != 1 || f.closed != 0 {
		t.Fatalf("yes cancels the operation and waits: begun=%d closed=%d", f.begun, f.closed)
	}
	f.canCancel = false
	f.c.Intercept()
	f.asked[2](true)
	if f.closed != 1 {
		t.Fatal("when nothing can be cancelled the window closes at once")
	}
}

func TestVaulticWindowCloserIgnoresClicksWhileClosing(t *testing.T) {
	f := newCloserFixture()
	f.busy, f.closing = true, true
	f.c.Intercept()
	if f.closed != 0 || len(f.asked) != 0 {
		t.Fatal("a close already in progress must be left alone")
	}
}
