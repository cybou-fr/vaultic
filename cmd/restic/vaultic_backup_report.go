package main

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"
)

type vaulticEngineEvent struct {
	MessageType    string  `json:"message_type"`
	PercentDone    float64 `json:"percent_done"`
	FilesDone      uint64  `json:"files_done"`
	FilesRestored  uint64  `json:"files_restored"`
	TotalFiles     uint64  `json:"total_files"`
	TotalBytes     uint64  `json:"total_bytes"`
	BytesDone      uint64  `json:"bytes_done"`
	SecondsElapsed uint64  `json:"seconds_elapsed"`
	ErrorCount     uint64  `json:"error_count"`
	FilesProcessed uint64  `json:"total_files_processed"`
	BytesProcessed uint64  `json:"total_bytes_processed"`
	BytesRestored  uint64  `json:"bytes_restored"`
	SnapshotID     string  `json:"snapshot_id"`
	TotalDuration  float64 `json:"total_duration"`
	Error          struct {
		Message string `json:"message"`
	} `json:"error"`
}

type vaulticBackupReport struct {
	mu             sync.Mutex
	started        time.Time
	profile        string
	status         vaulticEngineEvent
	summary        vaulticEngineEvent
	reportedErrors uint64
}

func (r *vaulticBackupReport) record(event vaulticEngineEvent) {
	r.mu.Lock()
	defer r.mu.Unlock()
	switch event.MessageType {
	case "status":
		r.status = event
	case "summary":
		r.summary = event
	case "error":
		r.reportedErrors++
	}
}

func (r *vaulticBackupReport) result(err error) (outcome, details string, resultErr error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	snapshot := r.summary.SnapshotID
	if err == nil && !vaulticSnapshotID.MatchString(snapshot) {
		err = errors.New(vaulticT("Aucun identifiant d’instantané confirmé par le moteur."))
	}
	errorCount := max(r.reportedErrors, r.status.ErrorCount)
	if err == nil && errorCount > 0 {
		err = ErrInvalidSourceData
	}
	switch {
	case err == nil:
		outcome = vaulticT("Réussie")
	case errors.Is(err, context.Canceled):
		outcome = vaulticT("Annulée")
	case errors.Is(err, ErrInvalidSourceData):
		outcome = vaulticT("Incomplète")
		err = errors.New(vaulticT("Au moins un fichier source n’a pas pu être lu. Consultez le journal pour les détails."))
	default:
		outcome = vaulticT("Échec")
	}
	duration := time.Since(r.started)
	if r.summary.TotalDuration > 0 {
		duration = time.Duration(r.summary.TotalDuration * float64(time.Second))
	}
	lines := []string{outcome, vaulticT("Profil : ") + r.profile, vaulticT("Durée : ") + duration.Round(time.Millisecond).String()}
	if r.summary.MessageType == "summary" {
		lines = append(lines, fmt.Sprintf(vaulticT("Fichiers traités : %d\nDonnées traitées : %s"), r.summary.FilesProcessed, vaulticBytes(r.summary.BytesProcessed)))
	} else if r.status.MessageType == "status" {
		lines = append(lines, fmt.Sprintf(vaulticT("Dernier relevé : %d fichier(s), %s"), r.status.FilesDone, vaulticBytes(r.status.BytesDone)))
	}
	lines = append(lines, fmt.Sprintf(vaulticT("Erreurs signalées par le moteur : %d"), errorCount))
	if snapshot != "" {
		lines = append(lines, vaulticT("Instantané : ")+snapshot)
	}
	if err != nil && !errors.Is(err, context.Canceled) {
		lines = append(lines, err.Error())
	}
	return outcome, strings.Join(lines, "\n"), err
}

func vaulticBytes(count uint64) string {
	units := [...]string{vaulticT("o"), vaulticT("Kio"), vaulticT("Mio"), vaulticT("Gio"), vaulticT("Tio"), vaulticT("Pio"), vaulticT("Eio")}
	value := float64(count)
	unit := 0
	for value >= 1024 && unit < len(units)-1 {
		value /= 1024
		unit++
	}
	if unit == 0 {
		return fmt.Sprintf(vaulticT("%d o"), count)
	}
	text := fmt.Sprintf(vaulticT("%.1f %s"), value, units[unit])
	if vaulticLanguage() == vaulticLangFR {
		text = strings.ReplaceAll(text, ".", ",") // French decimal comma
	}
	return text
}
