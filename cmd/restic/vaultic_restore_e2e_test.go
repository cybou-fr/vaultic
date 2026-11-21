package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"io/fs"
	"math/rand"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/restic/restic/internal/global"
)

// Stage 1 of the roadmap: prove that backup and restore are correct on a tree
// that looks like real data. Everything runs in process on a local repository.

const e2ePassword = "e2e-restore-password"

// e2eEntry describes one file or folder of the test tree.
type e2eEntry struct {
	path string // slash separated, relative to the tree root
	dir  bool
	size int
	seed int64
}

// e2eEntries covers sizes around block boundaries, empty files and folders,
// Unicode names, spaces and dots, and a deep path.
func e2eEntries() []e2eEntry {
	deep := "niveau-un/niveau-deux/niveau-trois/niveau-quatre/niveau-cinq/niveau-six/niveau-sept/niveau-huit"
	return []e2eEntry{
		{path: "vide.txt", size: 0},
		{path: "un-octet.bin", size: 1, seed: 1},
		{path: "limite-4095.bin", size: 4095, seed: 2},
		{path: "limite-4096.bin", size: 4096, seed: 3},
		{path: "limite-4097.bin", size: 4097, seed: 4},
		{path: "un-mio-plus-un.bin", size: 1<<20 + 1, seed: 5},
		{path: "gros-6-mio.bin", size: 6 << 20, seed: 6},
		{path: "Документы/привет мир.txt", size: 321, seed: 7},
		{path: "Документы/Фото отпуск 2026", dir: true},
		{path: "日本語/ファイル.txt", size: 77, seed: 8},
		{path: "café ☕/crème brûlée.txt", size: 900, seed: 9},
		{path: "nom avec espaces/fichier  double  espace.txt", size: 12, seed: 10},
		{path: "points.et.extensions.multiples.tar.gz", size: 2048, seed: 11},
		{path: ".caché", size: 40, seed: 12},
		{path: "dossier-vide", dir: true},
		{path: deep + "/profond.txt", size: 5000, seed: 13},
	}
}

func e2eContent(e e2eEntry) []byte {
	buf := make([]byte, e.size)
	rand.New(rand.NewSource(e.seed + 1000)).Read(buf)
	return buf
}

func e2eBuildTree(t *testing.T, root string) {
	t.Helper()
	for _, e := range e2eEntries() {
		full := filepath.Join(root, filepath.FromSlash(e.path))
		if e.dir {
			if err := os.MkdirAll(full, 0o700); err != nil {
				t.Fatal(err)
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(full), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, e2eContent(e), 0o600); err != nil {
			t.Fatal(err)
		}
	}
}

// e2eSnapshotTree lists a tree as "path:size:sha256" lines ("path:dir" for
// folders), so two trees can be compared exactly.
func e2eSnapshotTree(t *testing.T, root string) []string {
	t.Helper()
	var lines []string
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, path)
		if rel == "." {
			return nil
		}
		rel = filepath.ToSlash(rel)
		if d.IsDir() {
			lines = append(lines, rel+":dir")
			return nil
		}
		content, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		sum := sha256.Sum256(content)
		lines = append(lines, fmt.Sprintf("%s:%d:%s", rel, len(content), hex.EncodeToString(sum[:])))
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(lines)
	return lines
}

// e2eFindRestoredRoot finds the folder called name below dest: restic restores
// a source under its absolute path.
func e2eFindRestoredRoot(t *testing.T, dest, name string) string {
	t.Helper()
	var found string
	_ = filepath.WalkDir(dest, func(path string, d fs.DirEntry, err error) error {
		if err == nil && d.IsDir() && d.Name() == name && found == "" {
			found = path
			return filepath.SkipAll
		}
		return nil
	})
	if found == "" {
		t.Fatalf("restored tree %q not found below %s", name, dest)
	}
	return found
}

func e2eDiff(want, got []string) string {
	w := map[string]bool{}
	for _, l := range want {
		w[l] = true
	}
	g := map[string]bool{}
	for _, l := range got {
		g[l] = true
	}
	var out []string
	for _, l := range want {
		if !g[l] {
			out = append(out, "manque ou différent : "+l)
		}
	}
	for _, l := range got {
		if !w[l] {
			out = append(out, "en trop : "+l)
		}
	}
	return strings.Join(out, "\n")
}

func e2eOptions(repo, password string) global.Options {
	return global.Options{
		Repo: repo, Password: password, Backends: vaulticBackendsForSession(vaulticCloudAuth{}),
		Term: &vaulticTerminal{output: io.Discard}, JSON: true, Verbosity: 1,
	}
}

// e2eBackup runs a backup through the executor Vaultic uses and returns the
// snapshot ID and the outcome label of the report.
func e2eBackup(t *testing.T, repo, password string, sources []string) (snapshotID, outcome string, err error) {
	t.Helper()
	exec := vaulticBackupExecutor(e2eOptions(repo, password), sources, nil, func(report *vaulticBackupReport, line string) { vaulticRecordLine(report, line) })
	report := &vaulticBackupReport{}
	runErr := exec(context.Background(), report)
	outcome, _, err = report.result(runErr)
	report.mu.Lock()
	snapshotID = report.summary.SnapshotID
	report.mu.Unlock()
	return snapshotID, outcome, err
}

func e2eRestore(t *testing.T, repo, password, snapshotID string) string {
	t.Helper()
	dest := filepath.Join(t.TempDir(), "restauré")
	if err := os.MkdirAll(dest, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := vaulticRestoreSnapshot(context.Background(), e2eOptions(repo, password), snapshotID, nil, dest); err != nil {
		t.Fatalf("restore: %v", err)
	}
	return dest
}

func TestVaulticE2ERestoreIsByteForByte(t *testing.T) {
	root := t.TempDir()
	tree := filepath.Join(root, "arbre-e2e")
	e2eBuildTree(t, tree)
	want := e2eSnapshotTree(t, tree)

	repo, _ := initLocalTestRepository(t, e2ePassword)
	snapshot, outcome, err := e2eBackup(t, repo, e2ePassword, []string{tree})
	if err != nil || outcome != "Réussie" || !vaulticSnapshotID.MatchString(snapshot) {
		t.Fatalf("backup: outcome=%q err=%v snapshot=%q", outcome, err, snapshot)
	}
	restored := e2eRestore(t, repo, e2ePassword, snapshot)
	got := e2eSnapshotTree(t, e2eFindRestoredRoot(t, restored, "arbre-e2e"))
	if diff := e2eDiff(want, got); diff != "" {
		t.Fatalf("restored tree differs from the source:\n%s", diff)
	}

	// The repository itself must pass restic's own consistency check.
	var out bytes.Buffer
	term := &vaulticTerminal{output: &out}
	opts := e2eOptions(repo, e2ePassword)
	opts.Term = term
	if _, err := runCheck(context.Background(), CheckOptions{}, opts, nil, term); err != nil {
		t.Fatalf("check after backup: %v\n%s", err, out.String())
	}
}

func TestVaulticE2EIncrementalBackupKeepsEverySnapshotRestorable(t *testing.T) {
	root := t.TempDir()
	tree := filepath.Join(root, "arbre-e2e")
	e2eBuildTree(t, tree)
	repo, _ := initLocalTestRepository(t, e2ePassword)

	first, _, err := e2eBackup(t, repo, e2ePassword, []string{tree})
	if err != nil {
		t.Fatal(err)
	}
	wantFirst := e2eSnapshotTree(t, tree)

	// Change one file, add one, delete one, then back up again.
	if err := os.WriteFile(filepath.Join(tree, "limite-4096.bin"), []byte("modifié"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(tree, "nouveau-fichier.txt"), []byte("nouveau"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(tree, "un-octet.bin")); err != nil {
		t.Fatal(err)
	}
	wantSecond := e2eSnapshotTree(t, tree)
	second, _, err := e2eBackup(t, repo, e2ePassword, []string{tree})
	if err != nil || second == first {
		t.Fatalf("second backup: %v (ids %s / %s)", err, first, second)
	}

	cases := []struct {
		name string
		id   string
		want []string
	}{{"premier", first, wantFirst}, {"second", second, wantSecond}}
	for _, c := range cases {
		restored := e2eRestore(t, repo, e2ePassword, c.id)
		got := e2eSnapshotTree(t, e2eFindRestoredRoot(t, restored, "arbre-e2e"))
		if diff := e2eDiff(c.want, got); diff != "" {
			t.Fatalf("snapshot %s no longer restores exactly:\n%s", c.name, diff)
		}
	}
}

func TestVaulticE2EWrongPasswordNeverRestoresOrBacksUp(t *testing.T) {
	root := t.TempDir()
	tree := filepath.Join(root, "arbre-e2e")
	e2eBuildTree(t, tree)
	repo, _ := initLocalTestRepository(t, e2ePassword)
	snapshot, _, err := e2eBackup(t, repo, e2ePassword, []string{tree})
	if err != nil {
		t.Fatal(err)
	}
	if id, outcome, err := e2eBackup(t, repo, "mauvais-mot-de-passe", []string{tree}); err == nil || outcome == "Réussie" || id != "" {
		t.Fatalf("a wrong password must fail the backup: id=%q outcome=%q err=%v", id, outcome, err)
	}
	dest := filepath.Join(t.TempDir(), "refusé")
	_ = os.MkdirAll(dest, 0o700)
	if err := vaulticRestoreSnapshot(context.Background(), e2eOptions(repo, "mauvais-mot-de-passe"), snapshot, nil, dest); err == nil {
		t.Fatal("restore with a wrong password must fail")
	}
	if entries, _ := os.ReadDir(dest); len(entries) != 0 {
		t.Fatalf("nothing may be restored with a wrong password, found %d entries", len(entries))
	}
}

func TestVaulticE2ECyrillicPathsForRepositorySourceAndDestination(t *testing.T) {
	root := filepath.Join(t.TempDir(), "Мои документы")
	tree := filepath.Join(root, "источник")
	e2eBuildTree(t, tree)
	want := e2eSnapshotTree(t, tree)

	repo := filepath.Join(t.TempDir(), "резервный репозиторий")
	opts := e2eOptions(repo, e2ePassword)
	term := &vaulticTerminal{output: &bytes.Buffer{}}
	opts.Term = term
	if err := runInit(context.Background(), InitOptions{RepositoryVersion: "stable"}, opts, nil, term); err != nil {
		t.Fatalf("init in a Cyrillic path: %v", err)
	}
	snapshot, outcome, err := e2eBackup(t, repo, e2ePassword, []string{tree})
	if err != nil || outcome != "Réussie" {
		t.Fatalf("backup: %q %v", outcome, err)
	}
	restored := e2eRestore(t, repo, e2ePassword, snapshot)
	got := e2eSnapshotTree(t, e2eFindRestoredRoot(t, restored, "источник"))
	if diff := e2eDiff(want, got); diff != "" {
		t.Fatalf("Cyrillic round trip differs:\n%s", diff)
	}
}
