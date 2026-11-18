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
    if err != nil || len(got) != 1 || got[0] != source { t.Fatalf("sources=%v err=%v", got, err) }
}
