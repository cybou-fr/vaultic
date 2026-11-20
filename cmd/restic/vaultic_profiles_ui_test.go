package main

import (
	"errors"
	"testing"

	"fyne.io/fyne/v2/test"
)

type profilesFixture struct {
	c        *vaulticProfilesController
	settings *vaulticSettings
	persists int
	failNext error
	updates  int
	changed  int
	renamed  [][2]string
}

func newProfilesFixture(t *testing.T) *profilesFixture {
	t.Helper()
	test.NewTempApp(t)
	f := &profilesFixture{settings: &vaulticSettings{Profiles: []vaulticBackupProfile{
		{Name: "Documents", Sources: []string{"/a", "/b"}},
		{Name: "Photos", Sources: []string{"/p"}},
	}}}
	w := test.NewWindow(nil)
	t.Cleanup(w.Close)
	f.c = newVaulticProfilesController(f.settings, 0, vaulticProfilesHooks{
		Window: w,
		Persist: func() error {
			f.persists++
			if f.failNext != nil {
				err := f.failNext
				f.failNext = nil
				return err
			}
			return nil
		},
		Busy:          func() bool { return false },
		Log:           func(string) {},
		UpdateActions: func() { f.updates++ },
		Changed:       func() { f.changed++ },
		Renamed:       func(o, n string) { f.renamed = append(f.renamed, [2]string{o, n}) },
	})
	f.c.Refresh()
	return f
}

func TestVaulticProfilesControllerSelectsAndExposesSources(t *testing.T) {
	f := newProfilesFixture(t)
	if f.c.Index() != 0 || len(f.c.Sources()) != 2 || f.c.Current().Name != "Documents" {
		t.Fatalf("initial state: %d %v", f.c.Index(), f.c.Sources())
	}
	if f.c.CountValue.Text != "2" {
		t.Fatalf("count label %q", f.c.CountValue.Text)
	}
	f.c.Select.SetSelected("Photos")
	if f.c.Index() != 1 || len(f.c.Sources()) != 1 || f.c.CountValue.Text != "1" {
		t.Fatalf("switching profile: %d %v %s", f.c.Index(), f.c.Sources(), f.c.CountValue.Text)
	}
	if f.persists == 0 {
		t.Fatal("the selected profile must be saved")
	}
}

func TestVaulticProfilesControllerRemoveSourceAndRollbackOnSaveFailure(t *testing.T) {
	f := newProfilesFixture(t)
	f.c.list.Select(0)
	if f.c.Selected() != 0 {
		t.Fatal("row not selected")
	}
	f.failNext = errors.New("disque plein")
	f.c.RemoveSource.OnTapped()
	if len(f.c.Sources()) != 2 {
		t.Fatalf("a failed save must roll the sources back: %v", f.c.Sources())
	}
	f.c.RemoveSource.OnTapped()
	if got := f.c.Sources(); len(got) != 1 || got[0] != "/b" {
		t.Fatalf("source not removed: %v", got)
	}
	if f.c.Selected() != -1 || f.c.CountValue.Text != "1" {
		t.Fatalf("selection/count after removal: %d %s", f.c.Selected(), f.c.CountValue.Text)
	}
}

func TestVaulticProfilesControllerApplyRestoresPreviousStateOnFailure(t *testing.T) {
	f := newProfilesFixture(t)
	next := append([]vaulticBackupProfile(nil), f.settings.Profiles...)
	next[0].Excludes = []string{"*.tmp"}
	f.failNext = errors.New("lecture seule")
	if f.c.Apply(next, 0) {
		t.Fatal("Apply must report the failure")
	}
	if len(f.settings.Profiles[0].Excludes) != 0 {
		t.Fatal("failed Apply left the new profiles in the settings")
	}
	if !f.c.Apply(next, 0) || f.c.ExclusionSummary.Text != "1 exclusion(s)" {
		t.Fatalf("successful Apply: %q", f.c.ExclusionSummary.Text)
	}
}

func TestVaulticProfilesControllerButtonsAreExposedForEnabling(t *testing.T) {
	f := newProfilesFixture(t)
	if len(f.c.Buttons()) != 4 || f.c.Section == nil || f.c.Empty == nil {
		t.Fatal("controls missing")
	}
	f.c.Preview.OnTapped() // no exclusions: informs the user, must not call the engine
	if f.changed == 0 {
		t.Fatal("Refresh must notify dependent views")
	}
}
