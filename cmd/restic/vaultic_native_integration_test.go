package main

import (
    "bytes"
    "context"
    "encoding/json"
    "errors"
    "io/fs"
    "os"
    "path/filepath"
    "testing"

    "github.com/restic/restic/internal/backend/all"
    "github.com/restic/restic/internal/data"
    "github.com/restic/restic/internal/global"
    "github.com/restic/restic/internal/repository"
)

// This is intentionally an in-process integration test: it invokes the same
// run* functions used by Vaultic's UI, without executing restic as a subprocess.
func TestVaulticNativeEngineRoundTrip(t *testing.T) {
    root := t.TempDir()
    repoPath := filepath.Join(root, "repository")
    sourcePath := filepath.Join(root, "source")
    restoredPath := filepath.Join(root, "restored")
    if err := os.MkdirAll(sourcePath, 0700); err != nil { t.Fatal(err) }
    if err := os.MkdirAll(restoredPath, 0700); err != nil { t.Fatal(err) }
    original := []byte("Vaultic's native restic engine round-trip test\n")
    if err := os.WriteFile(filepath.Join(sourcePath, "vaultic-proof.txt"), original, 0600); err != nil {
        t.Fatal(err)
    }

    var output bytes.Buffer
    term := &vaulticTerminal{output: &output}
    opts := global.Options{
        Repo: repoPath,
        Password: "vaultic-integration-test-password",
        Backends: all.Backends(),
        Term: term,
        JSON: true,
        Verbosity: 1,
    }
    ctx := context.Background()
    if err := runInit(ctx, InitOptions{RepositoryVersion: "stable"}, opts, nil, term); err != nil {
        t.Fatalf("native init: %v", err)
    }
    if err := vaulticVerifyDepot(ctx, opts); err != nil {
        t.Fatalf("verify depot: %v", err)
    }
    wrong := opts
    wrong.Password = "incorrect-test-password"
    if err := vaulticVerifyDepot(ctx, wrong); !errors.Is(err, repository.ErrNoKeyFound) {
        t.Fatalf("wrong password: got %v", err)
    }
    sources, err := vaulticSourcesForBackup(repoPath, []string{sourcePath})
    if err != nil { t.Fatalf("validate sources: %v", err) }
    if err := runBackup(ctx, BackupOptions{
        Host: "vaultic-native-test",
        GroupBy: data.SnapshotGroupByOptions{Host:true, Path:true},
    }, opts, term, sources); err != nil {
        t.Fatalf("native backup: %v", err)
    }
    output.Reset()
    if err := runSnapshots(ctx, SnapshotOptions{}, opts, nil, term); err != nil {
        t.Fatalf("native snapshot listing: %v", err)
    }
    var snapshots []vaulticSnapshot
    if err := json.Unmarshal(output.Bytes(), &snapshots); err != nil {
        t.Fatalf("decode snapshots: %v; raw=%s", err, output.String())
    }
    if len(snapshots) != 1 || !vaulticSnapshotID.MatchString(snapshots[0].ID) {
        t.Fatalf("expected one snapshot, got %#v", snapshots)
    }
    target, err := vaulticRestoreTarget(restoredPath)
    if err != nil { t.Fatalf("validate restore target: %v", err) }
    if err := runRestore(ctx, RestoreOptions{Target:target, InheritWindowsPermissions:true}, opts, term, []string{snapshots[0].ID}); err != nil {
        t.Fatalf("native restore: %v", err)
    }
    var restored [][]byte
    err = filepath.WalkDir(restoredPath, func(path string, entry fs.DirEntry, walkErr error) error {
        if walkErr != nil { return walkErr }
        if entry.IsDir() || entry.Name() != "vaultic-proof.txt" { return nil }
        content, err := os.ReadFile(path)
        if err != nil { return err }
        restored = append(restored, content)
        return nil
    })
    if err != nil { t.Fatalf("walk restored data: %v", err) }
    if len(restored) != 1 || !bytes.Equal(restored[0], original) {
        t.Fatalf("restored data mismatch: got %d files", len(restored))
    }
    summary, err := runCheck(ctx, CheckOptions{}, opts, nil, term)
    if err != nil { t.Fatalf("native repository check: %v", err) }
    if summary.NumErrors != 0 { t.Fatalf("repository check found %d errors", summary.NumErrors) }
}
