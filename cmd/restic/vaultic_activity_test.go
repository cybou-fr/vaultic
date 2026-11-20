package main

import (
	"strings"
	"testing"
	"time"

	"fyne.io/fyne/v2/test"
)

func TestVaulticAppendLogTextTimestampsAndTrims(t *testing.T) {
	now := time.Date(2026, 6, 10, 14, 3, 9, 0, time.UTC)
	if got := vaulticAppendLogText("", "   ", now); got != "" {
		t.Fatalf("blank message must be ignored: %q", got)
	}
	got := vaulticAppendLogText("", "Début : Vérification", now)
	if got != "14:03:09  Début : Vérification\n" {
		t.Fatalf("line format: %q", got)
	}
	long := strings.Repeat("x", vaulticLogLimit)
	trimmed := vaulticAppendLogText(long, "dernier", now)
	if len(trimmed) != vaulticLogKeep || !strings.HasSuffix(trimmed, "dernier\n") {
		t.Fatalf("trimmed log: len=%d", len(trimmed))
	}
}

func TestVaulticLogViewAppendAndClear(t *testing.T) {
	test.NewTempApp(t)
	l := newVaulticLogView()
	l.Append("un")
	l.Append("")
	l.Append("deux")
	if strings.Count(l.Entry.Text, "\n") != 2 {
		t.Fatalf("log text: %q", l.Entry.Text)
	}
	l.Clear()
	if l.Entry.Text != "" {
		t.Fatal("Clear must empty the log")
	}
}

func TestVaulticInterpretEventBackupStatus(t *testing.T) {
	e := vaulticEngineEvent{MessageType: "status", PercentDone: 0.5, TotalFiles: 10, FilesDone: 5, TotalBytes: 2048, BytesDone: 1024, SecondsElapsed: 2}
	v := vaulticInterpretEvent(e, "Sauvegarde", true)
	if !v.HasProgress || v.Progress != 0.5 || !v.Suppress || v.Log != "" {
		t.Fatalf("view: %+v", v)
	}
	if !strings.HasPrefix(v.Status, "5 / 10 fichiers") || !strings.Contains(v.Status, "/s") {
		t.Fatalf("status text: %q", v.Status)
	}
}

func TestVaulticInterpretEventProgressBoundsAndRestore(t *testing.T) {
	if v := vaulticInterpretEvent(vaulticEngineEvent{MessageType: "status", PercentDone: 1.5, TotalBytes: 10}, "x", false); v.HasProgress {
		t.Fatal("percent above 1 must not move the bar")
	}
	if v := vaulticInterpretEvent(vaulticEngineEvent{MessageType: "status", PercentDone: 0.2}, "x", false); v.HasProgress {
		t.Fatal("unknown total must not move the bar")
	}
	v := vaulticInterpretEvent(vaulticEngineEvent{MessageType: "status", TotalFiles: 4, FilesRestored: 1}, "Restauration", false)
	if v.Status != "Restauration en cours · 1 / 4 fichiers" {
		t.Fatalf("restore status: %q", v.Status)
	}
}

func TestVaulticInterpretEventSummaryAndError(t *testing.T) {
	if v := vaulticInterpretEvent(vaulticEngineEvent{MessageType: "summary", SnapshotID: "abcd1234"}, "Sauvegarde", true); !v.Suppress {
		t.Fatal("the backup report owns the backup summary")
	}
	v := vaulticInterpretEvent(vaulticEngineEvent{MessageType: "summary", SnapshotID: "abcd1234", FilesProcessed: 3, BytesProcessed: 99}, "Op", false)
	if v.Log != "Instantané abcd1234 · 3 fichier(s) traité(s), 99 octets" || v.Suppress {
		t.Fatalf("snapshot summary: %+v", v)
	}
	v = vaulticInterpretEvent(vaulticEngineEvent{MessageType: "summary", TotalFiles: 7, FilesRestored: 5, BytesRestored: 12}, "Op", false)
	if v.Log != "5 élément(s) restauré(s) sur 7 · 12 octets" {
		t.Fatalf("restore summary: %+v", v)
	}
	if v := vaulticInterpretEvent(vaulticEngineEvent{MessageType: "summary"}, "Op", false); !v.Suppress {
		t.Fatal("an empty summary is not logged")
	}
	e := vaulticEngineEvent{MessageType: "error"}
	e.Error.Message = "fichier illisible"
	if v := vaulticInterpretEvent(e, "Op", false); v.Log != "fichier illisible" {
		t.Fatalf("error message: %+v", v)
	}
	if v := vaulticInterpretEvent(vaulticEngineEvent{MessageType: "error"}, "Op", false); v.Log != "" || v.Suppress {
		t.Fatal("an error without a message keeps the original line")
	}
	if v := vaulticInterpretEvent(vaulticEngineEvent{MessageType: "verbose_status"}, "Op", false); v.Log != "" || v.Suppress || v.HasProgress {
		t.Fatalf("unknown events pass through: %+v", v)
	}
}

func TestVaulticStatusPanelBusyIdleAndProgress(t *testing.T) {
	test.NewTempApp(t)
	cancelled := 0
	p := newVaulticStatusPanel(func() { cancelled++ })
	if p.Cancel.Visible() || p.Progress.Visible() || p.Status.Text != "Prêt" {
		t.Fatal("idle by default")
	}
	p.Busy()
	if !p.Cancel.Visible() || p.Cancel.Disabled() {
		t.Fatal("cancel must be usable while busy")
	}
	if !p.SetProgress(0.3) || p.SetProgress(0.6) {
		t.Fatal("the bar appears once")
	}
	if p.Progress.Value != 0.6 {
		t.Fatalf("progress value %v", p.Progress.Value)
	}
	p.Cancel.OnTapped()
	if cancelled != 1 {
		t.Fatal("cancel callback not called")
	}
	p.Idle()
	if p.Cancel.Visible() || p.Progress.Visible() {
		t.Fatal("Idle must hide cancel and progress")
	}
	if p.Bar() == nil {
		t.Fatal("status bar missing")
	}
}
