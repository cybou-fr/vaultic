package main

import (
	"context"
	"encoding/json"
	"errors"
	"os"

	"github.com/restic/restic/internal/data"
	"github.com/restic/restic/internal/global"
)

// vaulticRecordLine feeds one JSON line of restic output into a report.
func vaulticRecordLine(report *vaulticBackupReport, line string) {
	var event vaulticEngineEvent
	if report != nil && json.Unmarshal([]byte(line), &event) == nil {
		report.record(event)
	}
}

// vaulticBackupExecutor builds the execution function shared by manual and
// scheduled backups. It only backs up: it never runs forget or prune.
// onLine receives every output line of the engine (it must record it into the
// report, for instance with vaulticRecordLine) and may update a progress view.
func vaulticBackupExecutor(g global.Options, sources, excludes []string, onLine func(report *vaulticBackupReport, line string)) vaulticExec {
	return func(ctx context.Context, report *vaulticBackupReport) error {
		clean, err := vaulticSourcesForBackup(g.Repo, sources)
		if err != nil {
			return err
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if onLine != nil {
			g.Term = &vaulticTerminal{line: func(line string) { onLine(report, line) }, output: nil}
		}
		host, _ := os.Hostname()
		opts := BackupOptions{Host: host, GroupBy: data.SnapshotGroupByOptions{Host: true, Path: true}}
		opts.Excludes = append([]string(nil), excludes...)
		err = runBackup(ctx, opts, g, g.Term, clean)
		// restic can format the underlying context error as plain text.
		if err != nil && ctx.Err() != nil && !errors.Is(err, ErrInvalidSourceData) {
			return ctx.Err()
		}
		return err
	}
}
