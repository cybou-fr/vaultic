package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func snapAt(id string, t time.Time, host string, paths ...string) vaulticSnapshot {
	full := strings.Repeat(id, 64)[:64]
	return vaulticSnapshot{ID: full, ShortID: full[:8], Hostname: host, Time: t, Paths: paths}
}

func TestVaulticRetentionValidationAndDescription(t *testing.T) {
	if err := (vaulticRetention{}).Validate(); err == nil {
		t.Fatal("an empty policy would delete everything and must be refused")
	}
	if err := (vaulticRetention{KeepDaily: -1}).Validate(); err == nil {
		t.Fatal("negative counts must be refused")
	}
	if err := (vaulticRetention{KeepLast: 5000}).Validate(); err == nil {
		t.Fatal("absurd counts must be refused")
	}
	if err := vaulticRecommendedRetention().Validate(); err != nil {
		t.Fatal(err)
	}
	// Whatever the rules say, the newest snapshot is always kept.
	if got := (vaulticRetention{KeepMonthly: 6}).Effective().Last; got != 1 {
		t.Fatalf("the newest snapshot must always be kept, Last=%d", got)
	}
	if d := vaulticRecommendedRetention().Describe(); !strings.Contains(d, "3 derniers") || !strings.Contains(d, "12 mois") {
		t.Fatalf("description: %s", d)
	}
}

func TestVaulticRetentionOnlyConsidersTheProfilesOwnSnapshots(t *testing.T) {
	base := time.Date(2026, 6, 10, 12, 0, 0, 0, time.UTC)
	src := []string{filepath.Clean("/data/docs")}
	var all []vaulticSnapshot
	for i := 0; i < 6; i++ {
		all = append(all, snapAt(fmt.Sprintf("%x", i+1), base.Add(time.Duration(i)*time.Hour), "pc", src...))
	}
	all = append(all,
		snapAt("a", base.Add(10*time.Hour), "autre-pc", src...),                 // other host
		snapAt("b", base.Add(11*time.Hour), "pc", "/data/photos"),               // other sources
		snapAt("c", base.Add(-100*time.Hour), "pc", "/data/docs", "/data/more"), // other source set
	)
	p, err := vaulticPreviewRetention(all, "Documents", "local:/repo", "PC", src, vaulticRetention{KeepLast: 2}, base)
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Keep) != 2 || len(p.Remove) != 4 || p.Others != 3 {
		t.Fatalf("keep=%d remove=%d others=%d", len(p.Keep), len(p.Remove), p.Others)
	}
	for _, item := range p.Remove {
		for _, foreign := range []string{"a", "b", "c"} {
			if item.ID == strings.Repeat(foreign, 64) {
				t.Fatalf("a snapshot of another profile/host/source was listed for removal: %s", item.ShortID)
			}
		}
	}
	if !p.Keep[0].Time.After(p.Keep[1].Time) || p.Keep[0].Time != base.Add(5*time.Hour) {
		t.Fatalf("the two newest snapshots must be kept: %+v", p.Keep)
	}
	if len(p.Keep[0].Reasons) == 0 {
		t.Fatal("kept snapshots must say why")
	}
}

func TestVaulticRetentionNeverRemovesTheOnlySnapshot(t *testing.T) {
	base := time.Date(2026, 6, 10, 12, 0, 0, 0, time.UTC)
	src := []string{"/data/docs"}
	all := []vaulticSnapshot{snapAt("1", base, "pc", src...)}
	p, err := vaulticPreviewRetention(all, "D", "r", "pc", src, vaulticRetention{KeepYearly: 1}, base)
	if err != nil || len(p.Remove) != 0 || len(p.Keep) != 1 {
		t.Fatalf("a single snapshot must stay: %+v %v", p, err)
	}
	if _, err := vaulticPreviewRetention(all, "D", "r", "pc", src, vaulticRetention{}, base); err == nil {
		t.Fatal("an empty policy must not produce a preview")
	}
}

func TestVaulticRetentionBucketsKeepOnePerDay(t *testing.T) {
	base := time.Date(2026, 6, 10, 23, 0, 0, 0, time.UTC)
	src := []string{"/data/docs"}
	var all []vaulticSnapshot
	for day := 0; day < 5; day++ { // two snapshots a day for five days
		all = append(all, snapAt(fmt.Sprintf("%x", day*2+1), base.AddDate(0, 0, -day).Add(-10*time.Hour), "pc", src...))
		all = append(all, snapAt(fmt.Sprintf("%x", day*2+2), base.AddDate(0, 0, -day), "pc", src...))
	}
	p, err := vaulticPreviewRetention(all, "D", "r", "pc", src, vaulticRetention{KeepDaily: 3}, base)
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Keep) != 3 || len(p.Remove) != 7 {
		t.Fatalf("daily 3 of ten snapshots: keep=%d remove=%d", len(p.Keep), len(p.Remove))
	}
}

func TestVaulticConfirmRetentionAcceptsOnlyAFreshUnchangedSubset(t *testing.T) {
	base := time.Date(2026, 6, 10, 12, 0, 0, 0, time.UTC)
	src := []string{"/data/docs"}
	var all []vaulticSnapshot
	for i := 0; i < 6; i++ {
		all = append(all, snapAt(fmt.Sprintf("%x", i+1), base.Add(time.Duration(i)*time.Hour), "pc", src...))
	}
	policy := vaulticRetention{KeepLast: 2}
	p, err := vaulticPreviewRetention(all, "D", "r", "pc", src, policy, base)
	if err != nil {
		t.Fatal(err)
	}
	if ids, err := vaulticConfirmRetention(p, all, "pc", src, base.Add(time.Minute)); err != nil || len(ids) != 4 {
		t.Fatalf("an unchanged repository confirms exactly the preview: %v %v", ids, err)
	}
	// A newer backup only makes more old snapshots removable: still a subset.
	newer := append([]vaulticSnapshot{snapAt("f", base.Add(9*time.Hour), "pc", src...)}, all...)
	if ids, err := vaulticConfirmRetention(p, newer, "pc", src, base.Add(time.Minute)); err != nil || len(ids) != 4 {
		t.Fatalf("a newer backup must not block or widen the deletion: %v %v", ids, err)
	}
	// Too old.
	if _, err := vaulticConfirmRetention(p, all, "pc", src, base.Add(vaulticRetentionPreviewTTL+time.Minute)); err == nil {
		t.Fatal("an old preview must be refused")
	}
	// Tampered: an extra ID smuggled into the list to delete.
	forged := p
	forged.Remove = append(append([]vaulticRetentionItem(nil), p.Remove...), p.Keep[0])
	if _, err := vaulticConfirmRetention(forged, all, "pc", src, base.Add(time.Minute)); err == nil {
		t.Fatal("a modified preview must be refused")
	}
	// The newest snapshots disappeared: an old one that was to be removed is now needed.
	changed := all[:3] // only the 3 oldest remain
	if _, err := vaulticConfirmRetention(p, changed, "pc", src, base.Add(time.Minute)); err == nil {
		t.Fatal("a snapshot that must now be kept must stop the deletion")
	}
	// Everything already deleted.
	if _, err := vaulticConfirmRetention(p, all[4:], "pc", src, base.Add(time.Minute)); err == nil {
		t.Fatal("nothing left to delete must be reported")
	}
}

// The real engine on a real repository: only the previewed snapshots of the
// profile disappear, everything else restores byte for byte.
func TestVaulticRetentionDeletesOnlyThePreviewedSnapshotsOnARealRepository(t *testing.T) {
	root := t.TempDir()
	dirA := filepath.Join(root, "profil-a")
	dirB := filepath.Join(root, "profil-b")
	for _, d := range []string{dirA, dirB} {
		if err := os.MkdirAll(d, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	repo, _ := initLocalTestRepository(t, e2ePassword)
	opts := e2eOptions(repo, e2ePassword)
	ctx := context.Background()

	var idsA []string
	for i := 0; i < 5; i++ {
		if err := os.WriteFile(filepath.Join(dirA, "fichier.txt"), []byte(fmt.Sprintf("version %d de A", i)), 0o600); err != nil {
			t.Fatal(err)
		}
		id, outcome, err := e2eBackup(t, repo, e2ePassword, []string{dirA})
		if err != nil || outcome != "Réussie" {
			t.Fatalf("backup A%d: %q %v", i, outcome, err)
		}
		idsA = append(idsA, id)
	}
	var idsB []string
	for i := 0; i < 2; i++ {
		if err := os.WriteFile(filepath.Join(dirB, "autre.txt"), []byte(fmt.Sprintf("version %d de B", i)), 0o600); err != nil {
			t.Fatal(err)
		}
		id, _, err := e2eBackup(t, repo, e2ePassword, []string{dirB})
		if err != nil {
			t.Fatal(err)
		}
		idsB = append(idsB, id)
	}

	host, _ := os.Hostname()
	sourcesA, err := vaulticSourcesForBackup(repo, []string{dirA})
	if err != nil {
		t.Fatal(err)
	}
	all, err := vaulticListSnapshots(ctx, opts)
	if err != nil || len(all) != 7 {
		t.Fatalf("listing: %d snapshots, %v", len(all), err)
	}
	now := time.Now()
	preview, err := vaulticPreviewRetention(all, "A", repo, host, sourcesA, vaulticRetention{KeepLast: 2}, now)
	if err != nil {
		t.Fatal(err)
	}
	if len(preview.Keep) != 2 || len(preview.Remove) != 3 || preview.Others != 2 {
		t.Fatalf("preview: keep=%d remove=%d others=%d", len(preview.Keep), len(preview.Remove), preview.Others)
	}
	ids, err := vaulticConfirmRetention(preview, all, host, sourcesA, now)
	if err != nil {
		t.Fatal(err)
	}
	if err := vaulticForgetSnapshots(ctx, opts, ids); err != nil {
		t.Fatalf("forget: %v", err)
	}
	if err := vaulticPruneRepository(ctx, opts); err != nil {
		t.Fatalf("prune: %v", err)
	}

	after, err := vaulticListSnapshots(ctx, opts)
	if err != nil || len(after) != 4 {
		t.Fatalf("after: %d snapshots, %v", len(after), err)
	}
	present := map[string]bool{}
	for _, sn := range after {
		present[sn.ID] = true
	}
	for i, id := range idsA {
		want := i >= 3 // the two newest of A
		if present[id] != want {
			t.Fatalf("A snapshot %d: present=%v want %v", i, present[id], want)
		}
	}
	for _, id := range idsB {
		if !present[id] {
			t.Fatal("a snapshot of another profile was deleted")
		}
	}
	// What remains restores exactly, and the repository is consistent.
	for i, c := range []struct {
		id, name, content string
	}{{idsA[4], "profil-a", "version 4 de A"}, {idsA[3], "profil-a", "version 3 de A"}, {idsB[1], "profil-b", "version 1 de B"}} {
		restored := e2eRestore(t, repo, e2ePassword, c.id)
		root := e2eFindRestoredRoot(t, restored, c.name)
		entries, _ := os.ReadDir(root)
		if len(entries) != 1 {
			t.Fatalf("case %d: restored %d files", i, len(entries))
		}
		got, _ := os.ReadFile(filepath.Join(root, entries[0].Name()))
		if string(got) != c.content {
			t.Fatalf("case %d: restored %q want %q", i, got, c.content)
		}
	}
	term := &vaulticTerminal{output: new(strings.Builder)}
	opts.Term = term
	if _, err := runCheck(ctx, CheckOptions{}, opts, nil, term); err != nil {
		t.Fatalf("check after retention: %v", err)
	}
	// Forgetting needs complete IDs: a prefix or "latest" is refused before any engine call.
	for _, bad := range []string{"latest", idsA[4][:8], "", "../etc"} {
		if err := vaulticForgetSnapshots(ctx, opts, []string{bad}); err == nil {
			t.Fatalf("%q must be refused", bad)
		}
	}
}
