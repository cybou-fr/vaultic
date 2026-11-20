package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
	"unicode"
)

const (
	vaulticTriggerManual    = "manual"
	vaulticTriggerScheduled = "scheduled"

	vaulticStatusSuccess   = "success"
	vaulticStatusFailure   = "failure"
	vaulticStatusCancelled = "cancelled"
	vaulticStatusSkipped   = "skipped"

	vaulticHistoryLimit   = 300 // records kept in total
	vaulticSummaryLimit   = 200 // characters of an error summary
	vaulticHistoryVersion = 1
)

// vaulticRunRecord is one line of execution history. It never contains the
// repository password, cloud credentials or raw engine output: only fixed,
// French summaries and a short, sanitized error line.
type vaulticRunRecord struct {
	ID         string    `json:"id"`
	Profile    string    `json:"profile"`
	Repository string    `json:"repository"` // display form, no credentials
	Trigger    string    `json:"trigger"`
	Status     string    `json:"status"`
	CatchUp    bool      `json:"catch_up,omitempty"`
	Started    time.Time `json:"started"`
	Ended      time.Time `json:"ended"`
	DurationMs int64     `json:"duration_ms"`
	Files      uint64    `json:"files,omitempty"`
	Bytes      uint64    `json:"bytes,omitempty"`
	SnapshotID string    `json:"snapshot_id,omitempty"`
	Summary    string    `json:"summary,omitempty"`
}

func (r vaulticRunRecord) Duration() time.Duration {
	return time.Duration(r.DurationMs) * time.Millisecond
}

func vaulticStatusLabel(status string) string {
	switch status {
	case vaulticStatusSuccess:
		return "Réussie"
	case vaulticStatusFailure:
		return "Échec"
	case vaulticStatusCancelled:
		return "Annulée"
	case vaulticStatusSkipped:
		return "Ignorée"
	}
	return "Inconnue"
}

func vaulticTriggerLabel(trigger string) string {
	if trigger == vaulticTriggerScheduled {
		return "Planifiée"
	}
	return "Manuelle"
}

var (
	vaulticControlRe = regexp.MustCompile(`[\x00-\x1f\x7f]+`)
	vaulticSpaceRe   = regexp.MustCompile(`\s+`)
)

// vaulticSanitizeSummary reduces a diagnostic message to one short line that
// is safe to store and to show: secrets masked by redact, URLs without
// credentials or queries (the redactor strips them), control characters and
// layout removed, length bounded.
func vaulticSanitizeSummary(message string, redact func(string) string) string {
	if redact != nil {
		message = redact(message)
	}
	message = vaulticControlRe.ReplaceAllString(message, " ")
	message = vaulticSpaceRe.ReplaceAllString(strings.TrimSpace(message), " ")
	message = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) || unicode.Is(unicode.Cf, r) {
			return -1
		}
		return r
	}, message)
	runes := []rune(message)
	if len(runes) > vaulticSummaryLimit {
		message = string(runes[:vaulticSummaryLimit-1]) + "…"
	}
	return message
}

// vaulticHistoryStore keeps the history in a small JSON file next to the
// settings. Writes are atomic (temporary file, flush, rename) and the file is
// bounded, so a crash never leaves a truncated history.
type vaulticHistoryStore struct {
	mu      sync.Mutex
	path    string
	limit   int
	records []vaulticRunRecord
	loaded  bool
}

type vaulticHistoryFile struct {
	Version int                `json:"version"`
	Records []vaulticRunRecord `json:"records"`
}

func vaulticHistoryPath() string {
	return filepath.Join(filepath.Dir(vaulticSettingsPath()), "vaultic-history.json")
}

func newVaulticHistoryStore(path string) *vaulticHistoryStore {
	return &vaulticHistoryStore{path: path, limit: vaulticHistoryLimit}
}

func (h *vaulticHistoryStore) load() error {
	if h.loaded {
		return nil
	}
	h.loaded = true
	info, err := os.Lstat(h.path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() || info.Size() > 8<<20 {
		return errors.New("Le fichier d'historique est inattendu et a été ignoré.")
	}
	raw, err := os.ReadFile(h.path)
	if err != nil {
		return err
	}
	var file vaulticHistoryFile
	if err := json.Unmarshal(raw, &file); err != nil {
		// A damaged history must not block backups: start afresh and keep the
		// damaged file aside for inspection.
		_ = os.Rename(h.path, h.path+".damaged")
		return fmt.Errorf("Historique illisible, un nouvel historique a été créé : %w", err)
	}
	for _, r := range file.Records {
		r.Summary = vaulticSanitizeSummary(r.Summary, nil)
		h.records = append(h.records, r)
	}
	if len(h.records) > h.limit {
		h.records = h.records[len(h.records)-h.limit:]
	}
	return nil
}

// Load reads the history from disk (once).
func (h *vaulticHistoryStore) Load() error {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.load()
}

func (h *vaulticHistoryStore) saveLocked() error {
	data, err := json.MarshalIndent(vaulticHistoryFile{Version: vaulticHistoryVersion, Records: h.records}, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(h.path), 0o700); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(h.path), ".vaultic-history-*")
	if err != nil {
		return err
	}
	name := f.Name()
	defer os.Remove(name)
	if err = f.Chmod(0o600); err == nil {
		_, err = f.Write(data)
	}
	if err == nil {
		err = f.Sync()
	}
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return err
	}
	return os.Rename(name, h.path)
}

// Append adds a record, trims the history to its limit and saves atomically.
// The in-memory history is updated even when saving fails.
func (h *vaulticHistoryStore) Append(r vaulticRunRecord) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	loadErr := h.load()
	r.Summary = vaulticSanitizeSummary(r.Summary, nil)
	h.records = append(h.records, r)
	if len(h.records) > h.limit {
		h.records = append([]vaulticRunRecord(nil), h.records[len(h.records)-h.limit:]...)
	}
	if err := h.saveLocked(); err != nil {
		return err
	}
	return loadErr
}

// Recent returns up to n records for a profile (all profiles when empty),
// newest first.
func (h *vaulticHistoryStore) Recent(profile string, n int) []vaulticRunRecord {
	h.mu.Lock()
	defer h.mu.Unlock()
	_ = h.load()
	var out []vaulticRunRecord
	for i := len(h.records) - 1; i >= 0 && len(out) < n; i-- {
		if profile == "" || h.records[i].Profile == profile {
			out = append(out, h.records[i])
		}
	}
	return out
}

// LastSuccess returns the latest confirmed backup of a profile.
func (h *vaulticHistoryStore) LastSuccess(profile string) (vaulticRunRecord, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	_ = h.load()
	for i := len(h.records) - 1; i >= 0; i-- {
		if h.records[i].Profile == profile && h.records[i].Status == vaulticStatusSuccess {
			return h.records[i], true
		}
	}
	return vaulticRunRecord{}, false
}

// Len is the number of stored records.
func (h *vaulticHistoryStore) Len() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	_ = h.load()
	return len(h.records)
}

// RenameProfile keeps the history attached to a renamed profile.
func (h *vaulticHistoryStore) RenameProfile(oldName, newName string) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	_ = h.load()
	changed := false
	for i := range h.records {
		if h.records[i].Profile == oldName {
			h.records[i].Profile = newName
			changed = true
		}
	}
	if !changed {
		return nil
	}
	return h.saveLocked()
}

 
// LastSuccessForRepository only treats backups in the requested repository as
// evidence of protection. Switching the active repository must never reuse the
// success of another repository with the same profile name.
func (h *vaulticHistoryStore) LastSuccessForRepository(profile, repository string) (vaulticRunRecord, bool) {
 if repository == "" { return vaulticRunRecord{}, false }
 wanted := vaulticSafeLocation(repository)
 h.mu.Lock()
 defer h.mu.Unlock()
 _ = h.load()
 for i := len(h.records)-1; i>=0; i-- {
  r := h.records[i]
  if r.Profile == profile && r.Repository == wanted && r.Status == vaulticStatusSuccess {
   return r, true
  }
 }
 return vaulticRunRecord{}, false
}

// RecentForRepository shows only records for the requested profile and storage.
// Legacy records without a repository are intentionally not attributed.
func (h *vaulticHistoryStore) RecentForRepository(profile, repository string, n int) []vaulticRunRecord {
 if repository == "" || n <= 0 { return nil }
 wanted := vaulticSafeLocation(repository)
 h.mu.Lock()
 defer h.mu.Unlock()
 _ = h.load()
 out := make([]vaulticRunRecord,0,min(n,len(h.records)))
 for i := len(h.records)-1; i>=0 && len(out)<n; i-- {
  r:=h.records[i]
  if r.Profile==profile && r.Repository==wanted {out=append(out,r)}
 }
 return out
}
