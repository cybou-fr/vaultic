package main

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"fyne.io/fyne/v2/test"
)

func newTestLog(t *testing.T) *vaulticLogFile {
	t.Helper()
	l := newVaulticLogFile(filepath.Join(t.TempDir(), "logs"))
	l.now = func() time.Time { return time.Date(2026, 6, 10, 14, 3, 9, 123_000_000, time.UTC) }
	return l
}

func readLog(t *testing.T, path string) string {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

func TestVaulticLogLineFormatAndSingleLine(t *testing.T) {
	l := newTestLog(t)
	l.Info("Début : Sauvegarde\nsecond line\r\nthird")
	l.Write("warn", "   ")                 // blank messages are ignored
	l.Write("info", "tab\there \x07 bell") // control characters are dropped
	text := readLog(t, l.Path())
	lines := strings.Split(strings.TrimRight(text, "\n"), "\n")
	if len(lines) != 2 {
		t.Fatalf("expected two lines, got %d:\n%s", len(lines), text)
	}
	if !strings.HasPrefix(lines[0], "2026-06-10 14:03:09.123 [info] Début : Sauvegarde | second line | third") {
		t.Fatalf("line format: %q", lines[0])
	}
	if strings.ContainsAny(lines[1], "\x07") {
		t.Fatalf("control character survived: %q", lines[1])
	}
}

func TestVaulticLogHidesSecrets(t *testing.T) {
	l := newTestLog(t)
	l.AddSecret("hunter2-password")
	l.AddSecret("ab") // too short to be a useful filter
	l.Info("connecting to rest:https://user:topsecretpw@host.example.invalid/repo")
	l.Info("the password hunter2-password was rejected")
	l.Info("Authorization: Bearer abc123 and key=AKIAEXAMPLE token: xyz")
	l.Info("Mot de passe : visible ?")
	text := readLog(t, l.Path())
	for _, leak := range []string{"topsecretpw", "hunter2-password", "abc123", "AKIAEXAMPLE", "xyz"} {
		if strings.Contains(text, leak) {
			t.Fatalf("the log leaked %q:\n%s", leak, text)
		}
	}
	if !strings.Contains(text, "[hidden]") || !strings.Contains(text, "host.example.invalid") {
		t.Fatalf("hosts stay readable, secrets become [hidden]:\n%s", text)
	}
}

func TestVaulticLogRotatesAndKeepsAtMostFiveFiles(t *testing.T) {
	l := newTestLog(t)
	l.maxSize = 400
	for i := 0; i < 200; i++ {
		l.Info(strings.Repeat("x", 80))
	}
	entries, err := os.ReadDir(l.Dir())
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != vaulticLogFileKeep {
		t.Fatalf("expected %d files, found %d", vaulticLogFileKeep, len(entries))
	}
	for _, e := range entries {
		info, _ := e.Info()
		if info.Size() > 400+100 {
			t.Fatalf("%s is %d bytes: rotation did not cap it", e.Name(), info.Size())
		}
	}
	if _, err := os.Stat(l.Path() + ".5"); err == nil {
		t.Fatal("older files beyond the limit must be deleted")
	}
}

func TestVaulticLogIsPrivateAndTruncatesHugeLines(t *testing.T) {
	l := newTestLog(t)
	l.Info(strings.Repeat("é", 5000))
	text := readLog(t, l.Path())
	if n := len([]rune(strings.TrimSpace(text))); n > vaulticLogLineLimit+60 {
		t.Fatalf("an ordinary line must be limited, got %d characters", n)
	}
	if runtime.GOOS != "windows" {
		info, _ := os.Stat(l.Path())
		if info.Mode().Perm() != 0o600 {
			t.Fatalf("the log must be private: %v", info.Mode())
		}
	}
}

func TestVaulticLogSurvivesConcurrentWritersAndAnUnwritableFolder(t *testing.T) {
	l := newTestLog(t)
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 50; j++ {
				l.Info("message")
			}
		}()
	}
	wg.Wait()
	if n := strings.Count(readLog(t, l.Path()), "\n"); n != 400 {
		t.Fatalf("expected 400 lines, got %d", n)
	}
	// A log that cannot be written must never break the application.
	blocked := newVaulticLogFile(filepath.Join(l.Path(), "not-a-folder"))
	blocked.Info("ignored")
}

func TestVaulticLogRecordsAPanicWithItsStackThenRepanics(t *testing.T) {
	l := newTestLog(t)
	func() {
		defer func() {
			if r := recover(); r == nil {
				t.Fatal("the panic must continue after being logged")
			}
		}()
		defer l.RecoverAndRepanic()
		panic("boom for the test")
	}()
	text := readLog(t, l.Path())
	if !strings.Contains(text, "[panic] boom for the test") || !strings.Contains(text, "TestVaulticLogRecordsAPanic") {
		t.Fatalf("the crash must be recorded with its stack:\n%s", text)
	}
}

func TestVaulticLogStartLineAndDirectoryNextToSettings(t *testing.T) {
	l := newTestLog(t)
	l.Start("fr")
	if text := readLog(t, l.Path()); !strings.Contains(text, "Vaultic ") || !strings.Contains(text, "language fr") || !strings.Contains(text, runtime.GOOS) {
		t.Fatalf("start line: %s", text)
	}
	settings := filepath.Join("C:", "Users", "x", "Vaultic", "settings.json")
	if got := vaulticLogDir(settings); filepath.Dir(got) != filepath.Dir(settings) || filepath.Base(got) != "logs" {
		t.Fatalf("log folder: %s", got)
	}
}

func TestVaulticDiagnosticsOpensTheLogFolder(t *testing.T) {
	test.NewTempApp(t)
	dir := filepath.Join(t.TempDir(), "logs")
	var opened string
	c := newVaulticDiagnosticsController(vaulticDiagnosticsHooks{Dir: dir, Open: func(d string) { opened = d }})
	if !strings.Contains(c.Path.Text, dir) {
		t.Fatalf("the path must be shown: %q", c.Path.Text)
	}
	c.Open.OnTapped()
	if opened != dir {
		t.Fatalf("the folder must be opened: %q", opened)
	}
	if info, err := os.Stat(dir); err != nil || !info.IsDir() {
		t.Fatal("the folder must exist even before the first log line")
	}
	u := vaulticFileURL(`C:\Users\x y\Vaultic\logs`)
	if runtime.GOOS == "windows" && u.String() != "file:///C:/Users/x%20y/Vaultic/logs" {
		t.Fatalf("file URL: %s", u)
	}
}
