package main

import (
	"strings"
	"testing"
	"time"

	"fyne.io/fyne/v2/test"
)

func newSnapshotsFixture(t *testing.T) (*vaulticSnapshotsController, *int) {
	t.Helper()
	test.NewTempApp(t)
	w := test.NewWindow(nil)
	t.Cleanup(w.Close)
	updates := 0
	c := newVaulticSnapshotsController(vaulticSnapshotsHooks{
		Window: w, App: test.NewApp(), ActiveRepository: func() string { return "local:/repo" },
		UpdateActions: func() { updates++ },
	})
	return c, &updates
}

func sampleSnapshots() []vaulticSnapshot {
	base := time.Date(2026, 6, 1, 10, 0, 0, 0, time.UTC)
	return []vaulticSnapshot{
		{ID: "bbbb2222bbbb2222", ShortID: "bbbb2222", Hostname: "pc", Time: base.Add(24 * time.Hour), Paths: []string{"/docs"}},
		{ID: "aaaa1111aaaa1111", ShortID: "aaaa1111", Hostname: "pc", Time: base, Paths: []string{"/docs", "/photos"}},
	}
}

func TestVaulticSnapshotsControllerLoadsSelectsAndClears(t *testing.T) {
	c, updates := newSnapshotsFixture(t)
	c.UpdateState(false)
	if !c.Empty.Visible() || c.Explorer.Disabled() == false {
		t.Fatal("nothing selectable before loading")
	}
	c.setList(sampleSnapshots())
	if c.Count() != 2 || c.CountValue.Text != "2" || !strings.HasPrefix(c.RecentValue.Text, "Dernier instantané") {
		t.Fatalf("after load: %d %q %q", c.Count(), c.CountValue.Text, c.RecentValue.Text)
	}
	c.list.Select(1)
	sn, ok := c.Selected()
	if !ok || sn.ID != "aaaa1111aaaa1111" || *updates == 0 {
		t.Fatalf("selection: %+v %v", sn, ok)
	}
	if !strings.Contains(c.details.Text, "aaaa1111aaaa1111") || !strings.Contains(c.details.Text, "/photos") {
		t.Fatalf("details text: %q", c.details.Text)
	}
	c.UpdateState(false)
	if c.Explorer.Disabled() || c.Restore.Disabled() || c.Compare.Disabled() || c.Empty.Visible() {
		t.Fatal("controls must be enabled with a selection")
	}
	c.UpdateState(true)
	if !c.Restore.Disabled() {
		t.Fatal("controls must be disabled while an operation runs")
	}
	c.Clear()
	if _, ok := c.Selected(); ok || c.Count() != 0 || c.CountValue.Text != "—" || c.details.Text != vaulticNoSnapshotSelected {
		t.Fatal("Clear must forget list, selection and details")
	}
}

func TestVaulticSnapshotsControllerEmptyRepositoryMessage(t *testing.T) {
	c, _ := newSnapshotsFixture(t)
	c.setList(nil)
	c.UpdateState(false)
	if c.emptyLabel.Text != "Ce dépôt ne contient aucun instantané." || !c.Empty.Visible() || c.RecentValue.Text != "Aucun instantané dans ce dépôt" {
		t.Fatalf("empty repository: %q %q", c.emptyLabel.Text, c.RecentValue.Text)
	}
}

func TestVaulticSnapshotHelpers(t *testing.T) {
	snaps := sampleSnapshots()
	if latest, ok := vaulticLatestSnapshotTime(snaps); !ok || !latest.Equal(snaps[0].Time) {
		t.Fatalf("latest: %v %v", latest, ok)
	}
	if _, ok := vaulticLatestSnapshotTime(nil); ok {
		t.Fatal("empty list has no latest snapshot")
	}
	if text := vaulticSnapshotDetailText(snaps[0], nil); strings.Contains(text, "\n\n") {
		t.Fatalf("details without metrics must not add a metrics block: %q", text)
	}
}
