package main

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"fyne.io/fyne/v2/test"

	"github.com/restic/restic/internal/global"
)

func TestVaulticFindSavedDepotComparesResolvedLocalPaths(t *testing.T) {
	depots := []vaulticDepot{{Name: "A", Location: "/data/repo-a"}, {Name: "B", Location: "s3:s3.example.invalid/bucket"}}
	if d, ok := vaulticFindSavedDepot(depots, "/data/repo-a"); !ok || d.Name != "A" {
		t.Fatalf("exact match: %+v %v", d, ok)
	}
	if d, ok := vaulticFindSavedDepot(depots, "s3:s3.example.invalid/bucket"); !ok || d.Name != "B" {
		t.Fatalf("remote match: %+v %v", d, ok)
	}
	if _, ok := vaulticFindSavedDepot(depots, "/data/other"); ok {
		t.Fatal("unknown location must not match")
	}
}

func TestVaulticNormalizeAddressAndSessionAuthSupport(t *testing.T) {
	if got := vaulticNormalizeAddress("s3:s3.example.invalid/bucket"); got != "s3:s3.example.invalid/bucket" {
		t.Fatalf("remote address changed: %q", got)
	}
	if got := vaulticNormalizeAddress("relative/./repo"); got != filepath.Join("relative", "repo") {
		t.Fatalf("relative local path: %q", got)
	}
	for _, address := range []string{"s3:s3.example.invalid/b", "b2:bucket:path", "azure:container:/", "rest:https://example.invalid/", "gs:bucket:/", "swift:container:/"} {
		if !vaulticSupportsSessionAuth(address) {
			t.Fatalf("%s must accept session credentials", address)
		}
	}
	for _, address := range []string{"", "/local/path", "sftp:user@host:/repo"} {
		if vaulticSupportsSessionAuth(address) {
			t.Fatalf("%q has no session credentials editor", address)
		}
	}
}

type reposFixture struct {
	c           *vaulticRepositoriesController
	settings    *vaulticSettings
	persists    []string
	updates     int
	status      string
	logs        []string
	activated   int
	failPersist error
}

func newReposFixture(t *testing.T) *reposFixture {
	t.Helper()
	test.NewTempApp(t)
	f := &reposFixture{settings: &vaulticSettings{Repository: "/data/repo-a", Depots: []vaulticDepot{
		{Name: "A", Location: "/data/repo-a"}, {Name: "B", Location: "/data/repo-b"},
	}}}
	w := test.NewWindow(nil)
	t.Cleanup(w.Close)
	f.c = newVaulticRepositoriesController(f.settings, vaulticRepositoriesHooks{
		Window: w,
		Persist: func(depots []vaulticDepot, active string) error {
			if f.failPersist != nil {
				return f.failPersist
			}
			f.persists = append(f.persists, active)
			return nil
		},
		Busy: func() bool { return false }, Log: func(s string) { f.logs = append(f.logs, s) },
		Status: func(s string) { f.status = s }, UpdateActions: func() { f.updates++ },
		Run: func(string, string, string, vaulticRunOptions, func(context.Context, global.Options) error, func() error) {
			t.Fatal("no engine operation expected")
		},
		CancelRun: func() {}, Activated: func() { f.activated++ },
	})
	return f
}

func TestVaulticRepositoriesControllerActivateWipeAndRefresh(t *testing.T) {
	f := newReposFixture(t)
	if f.c.Active() != "/data/repo-a" || f.c.Value.Text != "/data/repo-a" {
		t.Fatalf("initial active repository: %q %q", f.c.Active(), f.c.Value.Text)
	}
	f.c.Refresh()
	if len(f.c.ListBox.Objects) != 2 || len(f.c.rowButtons) == 0 || f.updates == 0 {
		t.Fatalf("list not built: %d rows, %d buttons", len(f.c.ListBox.Objects), len(f.c.rowButtons))
	}
	f.c.Activate("/data/repo-b", "pw")
	if f.c.Active() != "/data/repo-b" || f.c.Password.Text != "pw" || f.activated != 1 || f.c.Value.Text != "/data/repo-b" {
		t.Fatalf("activate: %q %q %d", f.c.Active(), f.c.Password.Text, f.activated)
	}
	f.c.Sessions.set("/data/repo-b", vaulticCloudAuth{})
	f.c.Wipe()
	if f.c.Password.Text != "" {
		t.Fatal("Wipe must clear the password")
	}
	f.c.Activate("", "")
	if f.c.Value.Text != "Aucun dépôt configuré" {
		t.Fatalf("empty label: %q", f.c.Value.Text)
	}
}

func TestVaulticRepositoriesControllerSelectingASavedRepository(t *testing.T) {
	f := newReposFixture(t)
	f.c.connectSaved(f.settings.Depots[1])
	if f.c.Active() != "/data/repo-b" || len(f.persists) != 1 || f.persists[0] != "/data/repo-b" {
		t.Fatalf("selection must be saved: active %q persisted %v", f.c.Active(), f.persists)
	}
	if f.c.Password.Text != "" || f.status == "" {
		t.Fatalf("password must start empty and the user be guided: %q", f.status)
	}
	f.failPersist = errors.New("disque plein")
	f.c.connectSaved(f.settings.Depots[0])
	if f.c.Active() != "/data/repo-b" {
		t.Fatal("a failed save must not switch the repository")
	}
}

func TestVaulticRepositoriesControllerStateFollowsBusyAndBackend(t *testing.T) {
	f := newReposFixture(t)
	f.c.Refresh()
	f.c.UpdateState(false)
	if f.c.Verify.Disabled() || !f.c.EditAuth.Disabled() {
		t.Fatal("a local repository can be verified but has no credentials editor")
	}
	f.c.UpdateState(true)
	if !f.c.Verify.Disabled() {
		t.Fatal("controls must be disabled while busy")
	}
	f.c.Activate("s3:s3.example.invalid/bucket", "")
	f.c.UpdateState(false)
	if f.c.EditAuth.Disabled() {
		t.Fatal("an S3 repository needs the credentials editor")
	}
	if !f.c.ClearAuth.Disabled() {
		t.Fatal("no stored credentials: nothing to clear")
	}
	if len(f.c.Buttons()) != 3 {
		t.Fatal("three controls are disabled during operations")
	}
	f.c.SetBusy(true)
	if !f.c.Password.Disabled() {
		t.Fatal("password entry must be locked while busy")
	}
	f.c.SetBusy(false)
	if f.c.Password.Disabled() {
		t.Fatal("password entry must be unlocked afterwards")
	}
}
