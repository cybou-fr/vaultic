package restic

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func fakeRestic(t *testing.T) (string, string) {
	t.Helper()
	if os.PathSeparator == '\\' {
		t.Skip("POSIX fake executable")
	}
	dir := t.TempDir()
	bin := filepath.Join(dir, "restic")
	// Deliberately test that arguments are passed as separate strings and
	// the password is available to restic but not inserted into argv.
	script := `#!/bin/sh
case "$1" in
version) echo 'restic 0.19.1'; exit 0;;
--repo) shift 2;;
*) exit 8;;
esac
case "$1" in
init) test "$RESTIC_PASSWORD" = 's3cret' || exit 9; echo 'created';;
backup) test "$RESTIC_PASSWORD" = 's3cret' || exit 9; echo '{"message_type":"status","percent_done":0.5,"files_done":2}'; echo '{"message_type":"summary","snapshot_id":"aabbccddeeff0011"}';;
snapshots) printf '[{"id":"aabbccddeeff00112233","short_id":"aabbccdd","hostname":"test","paths":["/tmp/a"]}]';;
restore) echo 'restored';;
check) echo 'no errors';;
*) exit 7;;
esac
`
	if err := os.WriteFile(bin, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	return bin, dir
}

func TestFlow(t *testing.T) {
	bin, dir := fakeRestic(t)
	c := Client{Binary: bin, Repository: filepath.Join(dir, "repo"), Password: "s3cret"}
	v, err := c.Version(context.Background())
	if err != nil || !strings.Contains(v, "0.19.1") {
		t.Fatalf("version: %q %v", v, err)
	}
	if err := c.Init(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	source := filepath.Join(dir, "source folder")
	if err := os.Mkdir(source, 0700); err != nil {
		t.Fatal(err)
	}
	count := 0
	if err := c.Backup(context.Background(), []string{source}, func(e Event) {
		if e.Progress != nil {
			count++
		}
	}); err != nil {
		t.Fatal(err)
	}
	if count != 2 {
		t.Fatalf("expected two JSON events, got %d", count)
	}
	got, err := c.Snapshots(context.Background())
	if err != nil || len(got) != 1 || got[0].ShortID != "aabbccdd" {
		t.Fatalf("snapshots: %+v %v", got, err)
	}
	if err := c.Check(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	if err := c.Restore(context.Background(), got[0].ID, filepath.Join(dir, "restored"), nil); err != nil {
		t.Fatal(err)
	}
}

func TestSafetyChecks(t *testing.T) {
	bin, dir := fakeRestic(t)
	parent := filepath.Join(dir, "data")
	repo := filepath.Join(parent, "repo")
	if err := os.MkdirAll(repo, 0700); err != nil {
		t.Fatal(err)
	}
	c := Client{Binary: bin, Repository: repo, Password: "s3cret"}
	if err := c.Backup(context.Background(), []string{parent}, nil); err == nil {
		t.Fatal("recursive repository backup should be rejected")
	}
	if err := c.Backup(context.Background(), nil, nil); err == nil {
		t.Fatal("empty sources should be rejected")
	}
	target := filepath.Join(dir, "restoretarget")
	if err := os.Mkdir(target, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(target, "existing"), []byte("important"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := c.Restore(context.Background(), "aabbccdd", target, nil); err == nil {
		t.Fatal("restore to nonempty folder should be rejected")
	}
	if err := c.Restore(context.Background(), "--help", filepath.Join(dir, "other"), nil); err == nil {
		t.Fatal("malicious snapshot id should be rejected")
	}
}

func TestValidate(t *testing.T) {
	if err := (Client{}).Validate(); err == nil {
		t.Fatal("missing repo accepted")
	}
	if err := (Client{Repository: "repo"}).Validate(); err == nil {
		t.Fatal("missing password accepted")
	}
}
