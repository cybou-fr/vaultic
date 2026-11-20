package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/restic/restic/internal/backend/all"
	"github.com/restic/restic/internal/global"
)

func TestVaulticStorageStatisticsNativeEngine(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "source")
	repo := filepath.Join(root, "repo")
	if err := os.MkdirAll(source, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "preuve.txt"), []byte("vaultic stockage\n"), 0600); err != nil {
		t.Fatal(err)
	}
	terminal := &vaulticTerminal{}
	opts := global.Options{
		Repo: repo, Password: "vaultic-storage-test",
		Backends: all.Backends(), Term: terminal, JSON: true, Verbosity: 1,
	}
	ctx := context.Background()
	if err := runInit(ctx, InitOptions{RepositoryVersion: "stable"}, opts, nil, terminal); err != nil {
		t.Fatal(err)
	}
	if err := runBackup(ctx, BackupOptions{Host: "vaultic-storage"}, opts, terminal, []string{source}); err != nil {
		t.Fatal(err)
	}
	stats, err := vaulticCollectStorageStatistics(ctx, opts)
	if err != nil {
		t.Fatal(err)
	}
	if stats.SnapshotsCount != 1 || stats.TotalBlobCount == 0 || stats.TotalSize == 0 {
		t.Fatalf("statistiques natives incorrectes : %+v", stats)
	}
}
