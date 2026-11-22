package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"fyne.io/fyne/v2/test"

	"github.com/restic/restic/internal/global"
)

func TestVaulticParseRetentionAcceptsBlankAndRejectsGarbage(t *testing.T) {
	r, err := vaulticParseRetention([5]string{"3", "", " 7 ", "", ""})
	if err != nil || r.KeepLast != 3 || r.KeepDaily != 0 || r.KeepWeekly != 7 {
		t.Fatalf("parse: %+v %v", r, err)
	}
	for _, bad := range [][5]string{{"", "", "", "", ""}, {"x", "", "", "", ""}, {"-1", "", "", "", ""}, {"1.5", "", "", "", ""}, {"99999", "", "", "", ""}} {
		if _, err := vaulticParseRetention(bad); err == nil {
			t.Fatalf("%v must be refused", bad)
		}
	}
}

func TestVaulticRetentionConfirmTextSaysWhatWillBeDeleted(t *testing.T) {
	base := time.Date(2026, 6, 10, 12, 0, 0, 0, time.Local)
	p := vaulticRetentionPreview{Profile: "Documents",
		Keep:   []vaulticRetentionItem{{ShortID: "aaaa"}, {ShortID: "bbbb"}},
		Remove: []vaulticRetentionItem{{Time: base.Add(48 * time.Hour)}, {Time: base}}}
	text := vaulticRetentionConfirmText(p)
	for _, want := range []string{"2 instantané(s)", "« Documents »", "irréversible", "10/06/2026", "12/06/2026", "2 instantané(s) seront conservés"} {
		if !strings.Contains(text, want) {
			t.Fatalf("confirmation text lacks %q:\n%s", want, text)
		}
	}
}

func TestVaulticRetentionRecordIsAHistoryEntryWithoutSecrets(t *testing.T) {
	end := time.Date(2026, 6, 10, 12, 0, 5, 0, time.UTC)
	r := vaulticRetentionRecord("Documents", "rest:https://user:pw@host.example.invalid/repo", "3 instantané(s) supprimé(s)", end.Add(-5*time.Second), end)
	if r.Trigger != vaulticTriggerRetention || r.Status != vaulticStatusSuccess || r.DurationMs != 5000 || r.Profile != "Documents" {
		t.Fatalf("record: %+v", r)
	}
	if strings.Contains(r.Repository, "pw") {
		t.Fatalf("the repository must be shown without credentials: %q", r.Repository)
	}
	if vaulticTriggerLabel(vaulticTriggerRetention) != "Conservation" {
		t.Fatal("history label")
	}
}

type retentionFixture struct {
	c        *vaulticRetentionController
	profile  vaulticBackupProfile
	repo     string
	opts     global.Options
	history  *vaulticHistoryStore
	saved    []vaulticRetention
	logs     []string
	updates  int
	finished int
}

func newRetentionFixture(t *testing.T, dir string) *retentionFixture {
	t.Helper()
	test.NewTempApp(t)
	w := test.NewWindow(nil)
	t.Cleanup(w.Close)
	f := &retentionFixture{profile: vaulticBackupProfile{Name: "Documents", Sources: []string{dir}}}
	repo, _ := initLocalTestRepository(t, e2ePassword)
	f.repo, f.opts = repo, e2eOptions(repo, e2ePassword)
	f.history = newVaulticHistoryStore(filepath.Join(t.TempDir(), "history.json"))
	f.c = newVaulticRetentionController(vaulticRetentionHooks{
		Window:  w,
		Profile: func() vaulticBackupProfile { return f.profile },
		SaveRetention: func(r vaulticRetention) error {
			f.saved = append(f.saved, r)
			f.profile.Retention = &r
			return nil
		},
		Repository: func() string { return f.repo },
		// Runs synchronously against the real repository.
		Run: func(title string, fn func(context.Context, global.Options) error, finish func() error) {
			if err := fn(context.Background(), f.opts); err != nil {
				t.Fatalf("%s: %v", title, err)
			}
			if finish != nil {
				if err := finish(); err != nil {
					t.Fatalf("%s finish: %v", title, err)
				}
			}
			f.finished++
		},
		History: f.history, Log: func(s string) { f.logs = append(f.logs, s) },
		// Like the application, the hook uses the controller through a variable that
		// is only set once the constructor returned: calling it earlier must not happen.
		UpdateActions: func() { f.updates++; f.c.UpdateState(false, true, true) },
	})
	return f
}

func TestVaulticRetentionUIPreviewApplyAndRecordOnARealRepository(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "documents")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	f := newRetentionFixture(t, dir)
	for i := 0; i < 4; i++ {
		if err := os.WriteFile(filepath.Join(dir, "f.txt"), []byte(fmt.Sprintf("v%d", i)), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, _, err := e2eBackup(t, f.repo, e2ePassword, []string{dir}); err != nil {
			t.Fatal(err)
		}
	}

	// Nothing can be deleted before a preview exists.
	f.c.UpdateState(false, true, true)
	if !f.c.Apply.Disabled() || f.c.Preview.Disabled() {
		t.Fatal("before a preview only the preview button is available")
	}
	f.c.entries[0].SetText("2")
	f.c.save()
	if len(f.saved) != 1 || f.saved[0].KeepLast != 2 {
		t.Fatalf("policy not saved: %+v", f.saved)
	}
	f.c.runPreview()
	if f.c.preview == nil || len(f.c.preview.Remove) != 2 || len(f.c.preview.Keep) != 2 {
		t.Fatalf("preview: %+v", f.c.preview)
	}
	if !strings.Contains(f.c.summary.Text, "à supprimer : 2") || !strings.Contains(f.c.details.Text, "À SUPPRIMER (2)") {
		t.Fatalf("preview display: %q / %q", f.c.summary.Text, f.c.details.Text)
	}
	f.c.UpdateState(false, true, true)
	if f.c.Apply.Disabled() {
		t.Fatal("deletion must be offered once a preview lists something")
	}

	// Changing the policy after the preview invalidates it.
	f.c.entries[0].SetText("3")
	if f.c.preview != nil {
		t.Fatal("editing the policy must discard the preview")
	}
	f.c.UpdateState(false, true, true)
	if !f.c.Apply.Disabled() {
		t.Fatal("no preview: deletion disabled again")
	}

	f.c.entries[0].SetText("2")
	f.c.runPreview()
	reviewed := *f.c.preview
	// A snapshot list at confirm time is re-read: everything is still consistent.
	f.c.apply(reviewed)
	after, err := vaulticListSnapshots(context.Background(), f.opts)
	if err != nil || len(after) != 2 {
		t.Fatalf("after deletion: %d snapshots, %v", len(after), err)
	}
	if f.c.preview != nil {
		t.Fatal("the preview is consumed by the deletion")
	}
	records := f.history.Recent("", 10)
	if len(records) != 1 || records[0].Trigger != vaulticTriggerRetention || !strings.Contains(records[0].Summary, "2 instantané(s) supprimé(s)") {
		t.Fatalf("history: %+v", records)
	}
	// The same preview cannot be replayed: nothing is left to delete.
	if _, err := vaulticConfirmRetention(reviewed, after, mustHostname(t), mustSources(t, f.repo, dir), time.Now()); err == nil {
		t.Fatal("a consumed preview must not delete anything again")
	}
	f.c.prune()
	if records := f.history.Recent("", 10); len(records) != 2 || !strings.Contains(records[0].Summary, "prune") {
		t.Fatalf("prune must be recorded: %+v", records)
	}
}

func mustHostname(t *testing.T) string {
	t.Helper()
	h, err := os.Hostname()
	if err != nil {
		t.Fatal(err)
	}
	return h
}

func mustSources(t *testing.T, repo, dir string) []string {
	t.Helper()
	s, err := vaulticSourcesForBackup(repo, []string{dir})
	if err != nil {
		t.Fatal(err)
	}
	return s
}

// Retention commands may only be reached from the retention engine and its
// screen: not from the scheduler, the backup path or anything else.
func TestVaulticSnapshotDeletionIsReachableOnlyFromTheRetentionCode(t *testing.T) {
	allowedEngine := map[string]bool{
		"vaultic_retention_engine.go": true,
		"cmd_forget.go":               true, "cmd_prune.go": true,
	}
	allowedCallers := map[string]bool{"vaultic_retention_ui.go": true, "vaultic_retention_engine.go": true}
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, file := range files {
		if strings.HasSuffix(file, "_test.go") {
			continue
		}
		raw, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		text := string(raw)
		if !allowedEngine[file] && (strings.Contains(text, "runForget(") || strings.Contains(text, "runPrune(")) {
			t.Errorf("%s calls the engine's forget/prune directly", file)
		}
		if !allowedCallers[file] && (strings.Contains(text, "vaulticForgetSnapshots(") || strings.Contains(text, "vaulticPruneRepository(")) {
			t.Errorf("%s deletes snapshots or prunes: only the retention screen may", file)
		}
	}
}
