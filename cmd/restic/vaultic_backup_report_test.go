package main

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestVaulticBackupReportRequiresConfirmedSnapshot(t *testing.T) {
	report := vaulticBackupReport{started: time.Now(), profile: "Documents"}
	if outcome, _, err := report.result(nil); outcome != "Échec" || err == nil {
		t.Fatalf("unconfirmed backup reported %q, err=%v", outcome, err)
	}
	report.record(vaulticEngineEvent{MessageType: "summary", SnapshotID: "12345678", FilesProcessed: 4, BytesProcessed: 2048})
	if outcome, details, err := report.result(nil); outcome != "Réussie" || err != nil || !strings.Contains(details, "12345678") || !strings.Contains(details, "2,0 Kio") {
		t.Fatalf("confirmed backup report: %q, %q, %v", outcome, details, err)
	}
	report.record(vaulticEngineEvent{MessageType: "error"})
	if outcome, _, err := report.result(nil); outcome != "Incomplète" || err == nil {
		t.Fatalf("native error event was ignored: %q, %v", outcome, err)
	}
	if outcome, details, err := report.result(ErrInvalidSourceData); outcome != "Incomplète" || err == nil || !strings.Contains(details, "12345678") || !strings.Contains(details, "moteur : 1") {
		t.Fatalf("partial backup report: %q, %q, %v", outcome, details, err)
	}
	if outcome, _, err := report.result(context.Canceled); outcome != "Annulée" || err == nil {
		t.Fatalf("cancelled backup: %q, %v", outcome, err)
	}
}
