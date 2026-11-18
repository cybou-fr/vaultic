package main

import (
    "os"
    "path/filepath"
    "testing"
)

func TestVaulticRepositoryMustNotBeInsideSource(t *testing.T) {
    root := t.TempDir()
    source := filepath.Join(root, "documents")
    repo := filepath.Join(source, "backup")
    if err := os.MkdirAll(repo, 0700); err != nil { t.Fatal(err) }
    if _, err := vaulticSourcesForBackup(repo, []string{source}); err == nil {
        t.Fatal("expected protection against recursive repository backup")
    }
}

func TestVaulticRestoreRequiresEmptyDirectory(t *testing.T) {
    root := t.TempDir()
    if _, err := vaulticRestoreTarget(root); err != nil { t.Fatal(err) }
    if err := os.WriteFile(filepath.Join(root, "important.txt"), []byte("safe"), 0600); err != nil {
        t.Fatal(err)
    }
    if _, err := vaulticRestoreTarget(root); err == nil {
        t.Fatal("expected refusal of nonempty restore destination")
    }
}

func TestVaulticSourceValid(t *testing.T) {
    root := t.TempDir()
    source := filepath.Join(root, "documents")
    if err := os.Mkdir(source, 0700); err != nil { t.Fatal(err) }
    got, err := vaulticSourcesForBackup(filepath.Join(root, "repo"), []string{source})
    if err != nil || len(got) != 1 { t.Fatalf("sources=%v err=%v", got, err) }
    // macOS temp paths can be aliases (/var -> /private/var).
    canonical, err := filepath.EvalSymlinks(source)
    if err != nil { t.Fatal(err) }
    if got[0] != canonical { t.Fatalf("canonical source=%q, want %q", got[0], canonical) }
}

func TestVaulticRelativeRepositoryUsesExecutableDirectory(t *testing.T) {
    exe, err := os.Executable()
    if err != nil { t.Fatal(err) }
    got := vaulticResolvedRepository("vault")
    want := filepath.Join(filepath.Dir(exe), "vault")
    if got != want { t.Fatalf("resolved repository=%q, want %q",got,want) }
    if got := vaulticResolvedRepository("s3:bucket/prefix"); got != "s3:bucket/prefix" {
        t.Fatalf("remote URL must not be rewritten: %q",got)
    }
}
