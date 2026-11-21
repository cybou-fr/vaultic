//go:build windows

package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/sys/windows"
)

// lockFile opens path with no sharing, like an application that keeps a file
// open exclusively (a mail database, a running virtual machine). The handle is
// released by the returned function.
func lockFile(t *testing.T, path string) func() {
	t.Helper()
	name, err := windows.UTF16PtrFromString(path)
	if err != nil {
		t.Fatal(err)
	}
	handle, err := windows.CreateFile(name, windows.GENERIC_READ|windows.GENERIC_WRITE, 0, nil, windows.OPEN_EXISTING, windows.FILE_ATTRIBUTE_NORMAL, 0)
	if err != nil {
		t.Fatalf("cannot lock %s: %v", path, err)
	}
	return func() { _ = windows.CloseHandle(handle) }
}

// A file that another program holds open exclusively cannot be read. The
// backup must say so: never "Réussie", and every other file must still be
// saved and restorable.
func TestVaulticE2EWindowsLockedFileMakesTheBackupIncompleteNotSuccessful(t *testing.T) {
	root := t.TempDir()
	tree := filepath.Join(root, "arbre-e2e")
	e2eBuildTree(t, tree)
	locked := filepath.Join(tree, "base-ouverte.db")
	if err := os.WriteFile(locked, []byte("données d'une application en cours d'exécution"), 0o600); err != nil {
		t.Fatal(err)
	}
	// What is expected in the snapshot: everything except the locked file.
	var want []string
	for _, line := range e2eSnapshotTree(t, tree) {
		if !strings.HasPrefix(line, "base-ouverte.db:") {
			want = append(want, line)
		}
	}
	unlock := lockFile(t, locked)

	repo, _ := initLocalTestRepository(t, e2ePassword)
	snapshot, outcome, err := e2eBackup(t, repo, e2ePassword, []string{tree})
	unlock()
	if outcome == "Réussie" || err == nil {
		t.Fatalf("a locked file must not give a clean success: outcome=%q err=%v", outcome, err)
	}
	if outcome != "Incomplète" {
		t.Fatalf("expected an incomplete backup, got %q (%v)", outcome, err)
	}
	if !vaulticSnapshotID.MatchString(snapshot) {
		t.Fatalf("restic still saves a snapshot of the readable files, got %q", snapshot)
	}
	restored := e2eRestore(t, repo, e2ePassword, snapshot)
	got := e2eSnapshotTree(t, e2eFindRestoredRoot(t, restored, "arbre-e2e"))
	if diff := e2eDiff(want, got); diff != "" {
		t.Fatalf("the readable files must be restored exactly:\n%s", diff)
	}
}

// Paths longer than the historic 260 character limit of Windows must back up
// and restore like any other path.
func TestVaulticE2EWindowsLongPathsRoundTrip(t *testing.T) {
	root := t.TempDir()
	tree := filepath.Join(root, "arbre-e2e")
	deep := tree
	for i := 0; i < 12; i++ {
		deep = filepath.Join(deep, strings.Repeat("d", 25)+string(rune('a'+i)))
	}
	if len(deep) < 300 {
		t.Fatalf("test path too short to exercise long paths: %d", len(deep))
	}
	long := `\\?\` + deep
	if err := os.MkdirAll(long, 0o700); err != nil {
		t.Skipf("this system cannot create long paths: %v", err)
	}
	content := []byte("fichier dans un chemin de plus de 300 caractères")
	if err := os.WriteFile(filepath.Join(long, "profond-long.txt"), content, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(tree, "court.txt"), []byte("court"), 0o600); err != nil {
		t.Fatal(err)
	}

	repo, _ := initLocalTestRepository(t, e2ePassword)
	snapshot, outcome, err := e2eBackup(t, repo, e2ePassword, []string{tree})
	if err != nil || outcome != "Réussie" {
		t.Fatalf("backup of long paths: outcome=%q err=%v", outcome, err)
	}
	dest := e2eRestore(t, repo, e2ePassword, snapshot)
	restoredRoot := e2eFindRestoredRoot(t, dest, "arbre-e2e")
	rel, _ := filepath.Rel(tree, deep)
	got, err := os.ReadFile(`\\?\` + filepath.Join(restoredRoot, rel, "profond-long.txt"))
	if err != nil {
		t.Fatalf("the long file was not restored: %v", err)
	}
	if string(got) != string(content) {
		t.Fatal("long path content differs after restore")
	}
}
