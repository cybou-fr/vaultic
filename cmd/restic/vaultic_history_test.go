package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func sampleRecord(i int, status string) vaulticRunRecord {
	started := time.Date(2026, 6, 10, 8, 0, 0, 0, time.UTC).Add(time.Duration(i) * time.Minute)
	return vaulticRunRecord{
		ID: fmt.Sprintf("r%d", i), Profile: "Documents", Repository: "local:/x", Trigger: vaulticTriggerScheduled,
		Status: status, Started: started, Ended: started.Add(time.Second), DurationMs: 1000,
	}
}

func TestVaulticHistoryPersistsAcrossRestarts(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sub", "history.json")
	h := newVaulticHistoryStore(path)
	for i := 0; i < 5; i++ {
		rec := sampleRecord(i, vaulticStatusSuccess)
		rec.SnapshotID = fmt.Sprintf("snap%d", i)
		if err := h.Append(rec); err != nil {
			t.Fatal(err)
		}
	}
	reopened := newVaulticHistoryStore(path)
	if err := reopened.Load(); err != nil {
		t.Fatal(err)
	}
	recent := reopened.Recent("Documents", 3)
	if len(recent) != 3 || recent[0].ID != "r4" || recent[2].ID != "r2" {
		t.Fatalf("newest first: %+v", recent)
	}
	if last, ok := reopened.LastSuccess("Documents"); !ok || last.SnapshotID != "snap4" {
		t.Fatalf("last success: %+v", last)
	}
	if reopened.Recent("Autre", 5) != nil {
		t.Fatal("history leaked across profiles")
	}
	// Renaming a profile keeps its history.
	if err := reopened.RenameProfile("Documents", "Photos"); err != nil {
		t.Fatal(err)
	}
	if len(newVaulticHistoryStore(path).Recent("Photos", 10)) != 5 {
		t.Fatal("rename not persisted")
	}
	if info, err := os.Stat(path); err != nil || info.Mode().Perm()&0o077 != 0 && os.PathSeparator == '/' {
		t.Fatalf("history file permissions: %v %v", info, err)
	}
}

func TestVaulticHistoryIsBoundedAndWritesAreAtomic(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "history.json")
	h := newVaulticHistoryStore(path)
	h.limit = 20
	for i := 0; i < 55; i++ {
		if err := h.Append(sampleRecord(i, vaulticStatusSuccess)); err != nil {
			t.Fatal(err)
		}
	}
	if h.Len() != 20 {
		t.Fatalf("history not bounded: %d", h.Len())
	}
	if got := h.Recent("", 1)[0].ID; got != "r54" {
		t.Fatalf("newest record lost: %s", got)
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 {
		var names []string
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Fatalf("temporary files left behind: %v", names)
	}
	var file vaulticHistoryFile
	raw, _ := os.ReadFile(path)
	if err := json.Unmarshal(raw, &file); err != nil || len(file.Records) != 20 || file.Version != vaulticHistoryVersion {
		t.Fatalf("file content: %v %d", err, len(file.Records))
	}
	// Concurrent appends never corrupt the file.
	var wg sync.WaitGroup
	for i := 0; i < 30; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); _ = h.Append(sampleRecord(100+i, vaulticStatusFailure)) }()
	}
	wg.Wait()
	raw, _ = os.ReadFile(path)
	if err := json.Unmarshal(raw, &file); err != nil || len(file.Records) != 20 {
		t.Fatalf("concurrent writes corrupted the history: %v", err)
	}
}

func TestVaulticDamagedHistoryDoesNotBlockBackups(t *testing.T) {
	path := filepath.Join(t.TempDir(), "history.json")
	if err := os.WriteFile(path, []byte(`{"version":1,"records":[{"id":"x"`), 0o600); err != nil {
		t.Fatal(err)
	}
	h := newVaulticHistoryStore(path)
	if err := h.Append(sampleRecord(1, vaulticStatusSuccess)); err == nil {
		t.Log("damage reported through Append")
	}
	if h.Len() != 1 {
		t.Fatalf("a damaged file must start a fresh history: %d", h.Len())
	}
	if _, err := os.Stat(path + ".damaged"); err != nil {
		t.Fatal("the damaged file was not kept aside")
	}
	// A directory or oversized file is refused rather than followed.
	dirPath := filepath.Join(t.TempDir(), "history.json")
	if err := os.Mkdir(dirPath, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := newVaulticHistoryStore(dirPath).Load(); err == nil {
		t.Fatal("a directory was accepted as history")
	}
}

func TestVaulticSanitizeSummary(t *testing.T) {
	redact := vaulticRedactor([]string{"hunter2-password", "tok_live_ABC123"})
	cases := map[string][]string{
		"login failed for hunter2-password":                        {"hunter2-password"},
		"GET https://alice:tok_live_ABC123@host.example/r?sig=abc": {"tok_live_ABC123", "alice:", "sig=abc"},
		"line one\nline two\r\n\ttabbed\x00nul\x07bell":            {"\n", "\r", "\t", "\x00", "\x07"},
		"zero​width‮flip":                                          {"​", "‮"},
	}
	for in, forbidden := range cases {
		got := vaulticSanitizeSummary(in, redact)
		for _, f := range forbidden {
			if strings.Contains(got, f) {
				t.Errorf("%q: %q still contains %q", in, got, f)
			}
		}
		if got == "" {
			t.Errorf("%q: empty summary", in)
		}
	}
	long := vaulticSanitizeSummary(strings.Repeat("é", 1000), nil)
	if len([]rune(long)) > vaulticSummaryLimit {
		t.Fatalf("summary not bounded: %d", len([]rune(long)))
	}
	// The store sanitizes even when a caller forgot to.
	path := filepath.Join(t.TempDir(), "history.json")
	h := newVaulticHistoryStore(path)
	rec := sampleRecord(1, vaulticStatusFailure)
	rec.Summary = "multi\nline\x00 " + strings.Repeat("x", 500)
	_ = h.Append(rec)
	raw, _ := os.ReadFile(path)
	var file vaulticHistoryFile
	_ = json.Unmarshal(raw, &file)
	if strings.ContainsAny(file.Records[0].Summary, "\n\x00") || len([]rune(file.Records[0].Summary)) > vaulticSummaryLimit {
		t.Fatalf("stored summary unsafe: %q", file.Records[0].Summary)
	}
}

func TestVaulticHistoryFileHoldsNoSecretFields(t *testing.T) {
	raw, _ := json.Marshal(sampleRecord(1, vaulticStatusSuccess))
	var keys map[string]any
	_ = json.Unmarshal(raw, &keys)
	for key := range keys {
		for _, banned := range []string{"password", "secret", "token", "key", "credential", "raw", "log"} {
			if strings.Contains(strings.ToLower(key), banned) {
				t.Errorf("history field %q looks sensitive", key)
			}
		}
	}
}

// ---------------------------------------------------------------------------

func TestVaulticNotificationDeduplicationAndPreferences(t *testing.T) {
	clock := &fakeClock{t: time.Date(2026, 6, 10, 8, 0, 0, 0, time.UTC)}
	prefs := vaulticDefaultNotifyPrefs()
	var sent []string
	n := newVaulticNotifier(func() vaulticNotifyPrefs { return prefs }, func(title, body string) { sent = append(sent, title) }, clock.Now)

	if !n.Notify(vaulticNotifyFailed, "run-1", "Échec", "x") {
		t.Fatal("first notification refused")
	}
	if n.Notify(vaulticNotifyFailed, "run-1", "Échec", "x") {
		t.Fatal("the same event notified twice")
	}
	if !n.Notify(vaulticNotifyFailed, "run-2", "Échec", "y") {
		t.Fatal("a different event was suppressed")
	}
	// The same key in another category is a different event.
	if !n.Notify(vaulticNotifyOverdue, "run-1", "Retard", "z") {
		t.Fatal("category must be part of the key")
	}
	// After a day the event may be reported again.
	clock.Advance(25 * time.Hour)
	if !n.Notify(vaulticNotifyFailed, "run-1", "Échec", "x") {
		t.Fatal("deduplication memory never expires")
	}

	// Categories can be switched off one by one.
	prefs.Completed = false
	if n.Notify(vaulticNotifyCompleted, "run-9", "OK", "x") {
		t.Fatal("disabled category notified")
	}
	if !n.Notify(vaulticNotifySkipped, "run-9", "Ignorée", "x") {
		t.Fatal("other categories must keep working")
	}
	prefs = vaulticNotifyPrefs{}
	for _, cat := range []string{vaulticNotifyCompleted, vaulticNotifyFailed, vaulticNotifySkipped, vaulticNotifyOverdue} {
		if n.Notify(cat, "all-off-"+cat, "t", "b") {
			t.Fatalf("%s notified with everything disabled", cat)
		}
	}
	if n.Notify("unknown", "k", "t", "b") {
		t.Fatal("unknown category notified")
	}
}

func TestVaulticNotificationBurstIsLimited(t *testing.T) {
	clock := &fakeClock{t: time.Date(2026, 6, 10, 8, 0, 0, 0, time.UTC)}
	count := 0
	n := newVaulticNotifier(vaulticDefaultNotifyPrefs, func(title, body string) { count++ }, clock.Now)
	for i := 0; i < 20; i++ {
		n.Notify(vaulticNotifyFailed, fmt.Sprintf("run-%d", i), "Échec", "x")
	}
	if count != vaulticNotifyBurst {
		t.Fatalf("burst of %d allowed, sent %d", vaulticNotifyBurst, count)
	}
	clock.Advance(vaulticNotifyWindow + time.Minute)
	if !n.Notify(vaulticNotifyFailed, "run-late", "Échec", "x") {
		t.Fatal("notifications never resume after a burst")
	}
	if (*vaulticNotifier)(nil).Notify(vaulticNotifyFailed, "k", "t", "b") {
		t.Fatal("nil notifier must be harmless")
	}
}

func TestVaulticNotificationPreferencesPersistWithoutSecrets(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("APPDATA", dir)
	t.Setenv("XDG_CONFIG_HOME", dir)
	t.Setenv("HOME", dir)
	loc := &vaulticSchedule{Kind: vaulticScheduleWeekly, Enabled: true, Time: "20:00", Weekdays: []int{1, 4}, LastSlot: "2026-06-08T18:00:00Z"}
	settings := vaulticSettings{
		Repository: "", SchedulerPaused: true,
		Notifications: &vaulticNotifyPrefs{Completed: false, Failed: true, Skipped: true, Overdue: false},
		Profiles:      []vaulticBackupProfile{{Name: "Par défaut", Sources: []string{"/x"}, Schedule: loc}},
	}
	if err := vaulticSaveSettings(settings); err != nil {
		t.Fatal(err)
	}
	loaded, err := vaulticLoadSettings()
	if err != nil {
		t.Fatal(err)
	}
	if !loaded.SchedulerPaused || loaded.Notifications == nil || loaded.Notifications.Completed || !loaded.Notifications.Failed {
		t.Fatalf("preferences lost: %+v", loaded)
	}
	got := loaded.Profiles[0].Schedule
	if got == nil || got.Kind != vaulticScheduleWeekly || got.Time != "20:00" || len(got.Weekdays) != 2 || got.LastSlot != loc.LastSlot || !got.Enabled {
		t.Fatalf("schedule lost: %+v", got)
	}
	raw, _ := os.ReadFile(vaulticSettingsPath())
	for _, banned := range []string{"password", "secret", "token"} {
		if strings.Contains(strings.ToLower(string(raw)), banned) {
			t.Errorf("settings mention %q", banned)
		}
	}
	// A duplicated profile does not inherit an enabled schedule.
	dup, _, err := vaulticDuplicateSavedProfile(loaded.Profiles, 0, "Copie")
	if err != nil || dup[1].Schedule != nil {
		t.Fatalf("duplicate inherited the schedule: %v %+v", err, dup[1].Schedule)
	}
}
