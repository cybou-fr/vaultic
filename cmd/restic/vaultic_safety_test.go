package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestVaulticOperationOutputRedactsCredentials(t *testing.T) {
	t.Setenv("RESTIC_REST_PASSWORD", "rest-password-fixture")
	redact := vaulticRedactOperation("vault-password-fixture")
	input := "vault-password-fixture rest-password-fixture https://user:secret@example.com/repo?token=hidden"
	want := "[masqué] [masqué] https://example.com/repo"
	if got := redact(input); got != want {
		t.Fatalf("redaction failed: %q", got)
	}
}

func TestVaulticRestoreDestinationRejectsLinks(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "folder")
	if err := os.Mkdir(target, 0700); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "link")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("cannot create a directory link: %v", err)
	}
	if _, err := vaulticRestoreTarget(link); err == nil {
		t.Fatal("accepted a linked restore destination")
	}
	if _, err := vaulticSourcesForBackup(filepath.Join(dir, "repo"), []string{link}); err == nil {
		t.Fatal("accepted a linked source")
	}
	entries, err := os.ReadDir(target)
	if err != nil || len(entries) != 0 {
		t.Fatalf("linked directory was modified: %v, %v", entries, err)
	}
}

func TestVaulticRestoreNames(t *testing.T) {
	for _, name := range []string{"", ".", "..", "parent/child", "invalid" + string(rune(0))} {
		if vaulticSafeRestoreName(name) {
			t.Fatalf("accepted invalid name %q", name)
		}
	}
	if !vaulticSafeRestoreName("rapport [1]+.txt") {
		t.Fatal("rejected a normal name containing punctuation")
	}
	if filepath.Separator == '\\' {
		for _, name := range []string{"CON", "a.", "file:stream", "back\\slash"} {
			if vaulticSafeRestoreName(name) {
				t.Fatalf("accepted invalid Windows name %q", name)
			}
		}
	}
}

func TestVaulticRedactsMalformedURL(t *testing.T) {
	redact := vaulticRedactor(nil)
	if got := redact("https://example.com/%zz?credential=value"); strings.Contains(got, "credential") {
		t.Fatal("retained address parameters after parse failure")
	}
}
