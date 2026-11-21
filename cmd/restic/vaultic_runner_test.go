package main

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"fyne.io/fyne/v2/test"

	"github.com/restic/restic/internal/global"
	"github.com/restic/restic/internal/repository"
)

type runnerFixture struct {
	r         *vaulticEngineRunner
	panel     *vaulticStatusPanel
	busy      atomic.Bool
	mu        sync.Mutex
	logs      []string
	busyCalls []bool
	completed []string
	closed    atomic.Int32
}

func newRunnerFixture(t *testing.T) *runnerFixture {
	t.Helper()
	test.NewTempApp(t)
	f := &runnerFixture{}
	w := test.NewWindow(nil)
	t.Cleanup(w.Close)
	f.panel = newVaulticStatusPanel(func() { f.r.RequestCancel() })
	f.r = newVaulticEngineRunner(vaulticRunnerHooks{
		Window: w, Busy: &f.busy, Panel: f.panel,
		Log:           func(s string) { f.mu.Lock(); f.logs = append(f.logs, s); f.mu.Unlock() },
		SetBusy:       func(on bool) { f.mu.Lock(); f.busyCalls = append(f.busyCalls, on); f.mu.Unlock() },
		CloseNow:      func() { f.closed.Add(1) },
		CloudAuth:     func(string) vaulticCloudAuth { return vaulticCloudAuth{} },
		Completed:     func(title string) { f.mu.Lock(); f.completed = append(f.completed, title); f.mu.Unlock() },
		RefreshLayout: func() {},
	})
	return f
}

// waitLog waits until the runner logged a line starting with prefix. The runner
// logs last, after every status update, so the state can be read safely then.
func (f *runnerFixture) waitLog(t *testing.T, prefix string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		f.mu.Lock()
		for _, l := range f.logs {
			if strings.HasPrefix(l, prefix) {
				f.mu.Unlock()
				return
			}
		}
		f.mu.Unlock()
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("no log line starting with %q", prefix)
}

func (f *runnerFixture) waitIdle(t *testing.T) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for f.busy.Load() && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if f.busy.Load() {
		t.Fatal("the operation never ended")
	}
}

func (f *runnerFixture) snapshot() (logs []string, busy []bool, completed []string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.logs...), append([]bool(nil), f.busyCalls...), append([]string(nil), f.completed...)
}

func TestVaulticRunnerSuccessRunsFinishAndReports(t *testing.T) {
	f := newRunnerFixture(t)
	repo := t.TempDir()
	var gotRepo, gotPassword string
	finished := false
	f.r.Run("Vérification", repo, "secret", vaulticRunOptions{}, func(ctx context.Context, g global.Options) error {
		gotRepo, gotPassword = g.Repo, g.Password
		return nil
	}, func() error { finished = true; return nil })
	f.waitLog(t, "Terminé")
	logs, busy, completed := f.snapshot()
	if gotRepo != vaulticResolvedRepository(repo) || gotPassword != "secret" || !finished {
		t.Fatalf("engine options/finish: %q %q %v", gotRepo, gotPassword, finished)
	}
	if f.panel.Status.Text != "Vérification — terminé" || len(completed) != 1 || completed[0] != "Vérification" {
		t.Fatalf("status %q completed %v", f.panel.Status.Text, completed)
	}
	if len(busy) != 2 || !busy[0] || busy[1] {
		t.Fatalf("busy transitions: %v", busy)
	}
	if len(logs) < 2 || logs[0] != "Début : Vérification" || logs[len(logs)-1] != "Terminé : Vérification" {
		t.Fatalf("logs: %v", logs)
	}
	if f.busy.Load() || f.r.Running() {
		t.Fatal("gate or cancel left set")
	}
}

func TestVaulticRunnerRefusesMissingInformationAndBusyGate(t *testing.T) {
	f := newRunnerFixture(t)
	ran := false
	fn := func(context.Context, global.Options) error { ran = true; return nil }
	f.r.Run("x", "", "pw", vaulticRunOptions{}, fn, nil)
	f.r.Run("x", t.TempDir(), "", vaulticRunOptions{}, fn, nil)
	if ran || f.busy.Load() {
		t.Fatal("missing repository or password must not start the engine")
	}
	f.busy.Store(true)
	f.r.Run("x", t.TempDir(), "pw", vaulticRunOptions{}, fn, nil)
	if ran {
		t.Fatal("a second operation must not start while the gate is taken")
	}
	f.busy.Store(false)
	f.r.Run("Test", t.TempDir(), "", vaulticRunOptions{AllowNoPassword: true}, fn, nil)
	f.waitIdle(t)
	if !ran {
		t.Fatal("a reachability test may run without a password")
	}
}

func TestVaulticRunnerInlineErrorIsRedactedAndNotReportedAsSuccess(t *testing.T) {
	f := newRunnerFixture(t)
	f.r.Run("Connexion", t.TempDir(), "topsecret", vaulticRunOptions{Inline: true}, func(context.Context, global.Options) error {
		return errors.New("échec avec topsecret dans le message")
	}, nil)
	f.waitLog(t, "échec")
	_, _, completed := f.snapshot()
	if len(completed) != 0 {
		t.Fatal("a failed operation must not count as completed")
	}
	if strings.Contains(f.panel.Status.Text, "topsecret") || !strings.Contains(f.panel.Status.Text, "échec") {
		t.Fatalf("status must show the redacted error: %q", f.panel.Status.Text)
	}
}

func TestVaulticRunnerCancelAndClose(t *testing.T) {
	f := newRunnerFixture(t)
	started := make(chan struct{})
	f.r.Run("Analyse", t.TempDir(), "pw", vaulticRunOptions{}, func(ctx context.Context, g global.Options) error {
		close(started)
		<-ctx.Done()
		return ctx.Err()
	}, nil)
	<-started
	if !f.r.Running() {
		t.Fatal("running operation must be cancellable")
	}
	f.r.RequestCancel()
	f.waitLog(t, "Annulé")
	if f.panel.Status.Text != "Analyse — annulé" {
		t.Fatalf("status after cancel: %q", f.panel.Status.Text)
	}

	started2 := make(chan struct{})
	finished := false
	f.r.Run("Analyse", t.TempDir(), "pw", vaulticRunOptions{}, func(ctx context.Context, g global.Options) error {
		close(started2)
		<-ctx.Done()
		return nil
	}, func() error { finished = true; return nil })
	<-started2
	if !f.r.BeginClose() || !f.r.Closing() {
		t.Fatal("BeginClose must cancel a running operation")
	}
	deadline := time.Now().Add(5 * time.Second)
	for f.closed.Load() == 0 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if f.closed.Load() != 1 || !finished {
		t.Fatalf("window must close after the operation ended: closed=%d finished=%v", f.closed.Load(), finished)
	}
	idle := newRunnerFixture(t)
	if idle.r.BeginClose() {
		t.Fatal("nothing runs: the window can close at once")
	}
}

func TestVaulticRunnerHelpers(t *testing.T) {
	if !vaulticMissingConnectionInfo("", "pw", false) || !vaulticMissingConnectionInfo("r", "", false) || vaulticMissingConnectionInfo("r", "", true) || vaulticMissingConnectionInfo("r", "pw", false) {
		t.Fatal("missing connection info rules")
	}
	if got := vaulticFriendlyEngineError(fmt.Errorf("wrapped: %w", repository.ErrNoKeyFound)); got == nil || !strings.Contains(got.Error(), "Mot de passe incorrect") {
		t.Fatalf("no-key error: %v", got)
	}
	other := errors.New("autre")
	if vaulticFriendlyEngineError(other) != other || vaulticFriendlyEngineError(nil) != nil {
		t.Fatal("other errors pass through")
	}
	missing := filepath.Join(t.TempDir(), "absent")
	if err := vaulticCheckLocalRepositoryReachable(missing, missing); err == nil || !strings.Contains(err.Error(), "branchement") {
		t.Fatalf("unplugged disk: %v", err)
	}
	if err := vaulticCheckLocalRepositoryReachable(t.TempDir(), t.TempDir()); err != nil {
		t.Fatalf("existing path: %v", err)
	}
	if err := vaulticCheckLocalRepositoryReachable("s3:s3.example.invalid/bucket", "s3:s3.example.invalid/bucket"); err != nil {
		t.Fatalf("remote locations are not stat-ed: %v", err)
	}
}
