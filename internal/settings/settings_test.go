package settings

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSaveLoad(t *testing.T) {
	// os.UserConfigDir honours XDG_CONFIG_HOME on Linux.
	if os.PathSeparator == '\\' {
		t.Skip("uses XDG_CONFIG_HOME")
	}
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	want := Settings{ResticBinary: "restic", Repository: "/tmp/myrepo", Sources: []string{"/tmp/data"}}
	if err := Save(want); err != nil {
		t.Fatal(err)
	}
	got := Load()
	if got.Repository != want.Repository || len(got.Sources) != 1 {
		t.Fatalf("got %+v", got)
	}
	info, err := os.Stat(filepath.Join(os.Getenv("XDG_CONFIG_HOME"), "Vaultic", "settings.json"))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0600 {
		t.Fatalf("unexpected mode %v", info.Mode())
	}
}
