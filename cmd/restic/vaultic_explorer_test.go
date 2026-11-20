package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/restic/restic/internal/backend/all"
	"github.com/restic/restic/internal/global"
)

func TestVaulticSelectionFilter(t *testing.T) {
	f := vaulticSelectionFilter([]vaulticSnapshotNode{
		{Path: "/alpha/report[1].txt", Type: "file"},
		{Path: "/beta/other.txt", Type: "file"},
	})
	for _, x := range []struct {
		path       string
		dir        bool
		selectItem bool
		descend    bool
	}{
		{"/alpha", true, false, true},
		{"/alpha/report[1].txt", false, true, false},
		{"/alpha/report1.txt", false, false, false},
		{"/alpha/report[1].txt2", false, false, false},
		{"/beta", true, false, true},
		{"/beta/other.txt", false, true, false},
		{"/betamax", true, false, false},
	} {
		selected, descend := f(x.path, x.dir)
		if selected != x.selectItem || descend != x.descend {
			t.Errorf("file %q: got (%v,%v)", x.path, selected, descend)
		}
	}
	d := vaulticSelectionFilter([]vaulticSnapshotNode{
		{Path: "/alpha/my-dir", Type: "dir"},
		{Path: "/alpha/my-dir/nested/file", Type: "file"},
	})
	for _, x := range []struct {
		path       string
		dir        bool
		selectItem bool
		descend    bool
	}{
		{"/alpha", true, false, true},
		{"/alpha/my-dir", true, true, true},
		{"/alpha/my-dir/file", false, true, false},
		{"/alpha/my-dir/nested", true, true, true},
		{"/alpha/my-dir2/file", false, false, false},
	} {
		selected, descend := d(x.path, x.dir)
		if selected != x.selectItem || descend != x.descend {
			t.Errorf("folder %q: got (%v,%v)", x.path, selected, descend)
		}
	}
}

func TestVaulticSelectiveRestoreRoundTrip(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "source")
	repo := filepath.Join(root, "repository")
	target := filepath.Join(root, "restored")
	if err := os.MkdirAll(source, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(target, 0700); err != nil {
		t.Fatal(err)
	}
	want := []byte("vaultic exact restore\n")
	if err := os.WriteFile(filepath.Join(source, "report[1].txt"), want, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "other.txt"), []byte("not selected"), 0600); err != nil {
		t.Fatal(err)
	}
	linkErr := os.Symlink("other.txt", filepath.Join(source, "linked.txt"))
	group := filepath.Join(source, "group (docs)")
	if err := os.Mkdir(group, 0700); err != nil {
		t.Fatal(err)
	}
	wantNested := []byte("selected directory contents\n")
	if err := os.WriteFile(filepath.Join(group, "inside+.txt"), wantNested, 0600); err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	term := &vaulticTerminal{output: &output}
	opts := global.Options{Repo: repo, Password: "vaultic-test-password", Backends: all.Backends(), Term: term, JSON: true, Verbosity: 1}
	ctx := context.Background()
	if err := runInit(ctx, InitOptions{RepositoryVersion: "stable"}, opts, nil, term); err != nil {
		t.Fatal(err)
	}
	if err := runBackup(ctx, BackupOptions{Host: "vaultic-test"}, opts, term, []string{source}); err != nil {
		t.Fatal(err)
	}
	output.Reset()
	if err := runSnapshots(ctx, SnapshotOptions{}, opts, nil, term); err != nil {
		t.Fatal(err)
	}
	var snapshots []vaulticSnapshot
	if err := json.Unmarshal(output.Bytes(), &snapshots); err != nil {
		t.Fatal(err)
	}
	if len(snapshots) != 1 {
		t.Fatalf("expected 1 snapshot, got %d", len(snapshots))
	}
	id := snapshots[0].ID
	if linkErr == nil {
		emptyTarget := t.TempDir()
		if err := vaulticRestoreSnapshot(ctx, opts, id, nil, emptyTarget); err == nil {
			t.Fatal("accepted a whole-snapshot restore containing a symbolic link")
		}
		entries, err := os.ReadDir(emptyTarget)
		if err != nil || len(entries) != 0 {
			t.Fatalf("preflight modified the destination: %v, %v", entries, err)
		}
	} else {
		t.Logf("symbolic link fixture unavailable: %v", linkErr)
	}
	var selected *vaulticSnapshotNode
	var descend func(string) error
	descend = func(dir string) error {
		nodes, err := vaulticListSnapshotDirectory(ctx, opts, id, dir)
		if err != nil {
			return err
		}
		for _, n := range nodes {
			if n.Type == "file" && n.Name == "report[1].txt" {
				cp := n
				selected = &cp
				return nil
			}
			if n.Type == "dir" {
				if err := descend(n.Path); err != nil {
					return err
				}
				if selected != nil {
					return nil
				}
			}
		}
		return nil
	}
	if err := descend("/"); err != nil {
		t.Fatal(err)
	}
	if selected == nil {
		t.Fatal("selected file absent")
	}
	exact, truncated, err := vaulticSearchSnapshot(ctx, opts, id, "REPORT[", 10)
	if err != nil || truncated || len(exact) != 1 || exact[0].Path != selected.Path {
		t.Fatalf("literal case-insensitive search: %v, truncated=%v, err=%v", exact, truncated, err)
	}
	nested, truncated, err := vaulticSearchSnapshot(ctx, opts, id, "/group (docs)", 1)
	if err != nil || !truncated || len(nested) != 1 || nested[0].Type != "dir" {
		t.Fatalf("bounded path search: %v, truncated=%v, err=%v", nested, truncated, err)
	}
	items := []vaulticSnapshotNode{exact[0], nested[0]}
	if err := vaulticRestoreSelected(ctx, opts, id, items, target); err != nil {
		t.Fatal(err)
	}
	if err := vaulticRestoreSelected(ctx, opts, id, items, target); err == nil {
		t.Fatal("restored into nonempty target")
	}
	files := make(map[string][]byte)
	if err := filepath.WalkDir(target, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() {
			content, err := os.ReadFile(p)
			if err != nil {
				return err
			}
			files[d.Name()] = content
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if len(files) != 2 || !bytes.Equal(files["report[1].txt"], want) || !bytes.Equal(files["inside+.txt"], wantNested) {
		t.Fatalf("restored selection differs: %v", files)
	}
}
