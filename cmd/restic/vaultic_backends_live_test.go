package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/restic/restic/internal/global"
)

// Live backend tests. They are skipped unless a target is configured through
// environment variables, so the normal test run never needs a network or an
// account. They work against local emulators (MinIO, Azurite, fake-gcs-server,
// rest-server) and against real test accounts; each target gets its own empty
// repository path, which the test creates and never deletes.
//
//	VAULTIC_LIVE_LOCAL     a local folder (checks the test itself, no network)
//	VAULTIC_LIVE_S3        s3:http://127.0.0.1:9000/bucket/vaultic-test
//	VAULTIC_LIVE_S3_ACCESS access key      VAULTIC_LIVE_S3_SECRET secret key
//	VAULTIC_LIVE_REST      rest:http://127.0.0.1:8000/vaultic-test
//	VAULTIC_LIVE_AZURE     azure:container:/vaultic-test
//	VAULTIC_LIVE_AZURE_ACCOUNT / VAULTIC_LIVE_AZURE_KEY
//	VAULTIC_LIVE_GS        gs:bucket:/vaultic-test
//	VAULTIC_LIVE_GS_PROJECT project id (credentials come from the environment)
//
// Credentials are passed the way the application passes them: as session
// credentials, never through global environment variables set by the test.

type liveTarget struct {
	name    string
	address string
	auth    vaulticCloudAuth
}

func liveTargets() []liveTarget {
	var targets []liveTarget
	if a := os.Getenv("VAULTIC_LIVE_LOCAL"); a != "" {
		targets = append(targets, liveTarget{"local", a, vaulticCloudAuth{}})
	}
	if a := os.Getenv("VAULTIC_LIVE_S3"); a != "" {
		targets = append(targets, liveTarget{"s3", a, vaulticCloudAuth{
			Scheme: "s3", AccessID: os.Getenv("VAULTIC_LIVE_S3_ACCESS"), Secret: os.Getenv("VAULTIC_LIVE_S3_SECRET"),
			Region: os.Getenv("VAULTIC_LIVE_S3_REGION"),
		}})
	}
	if a := os.Getenv("VAULTIC_LIVE_REST"); a != "" {
		targets = append(targets, liveTarget{"rest", a, vaulticCloudAuth{Scheme: "rest"}})
	}
	if a := os.Getenv("VAULTIC_LIVE_AZURE"); a != "" {
		targets = append(targets, liveTarget{"azure", a, vaulticCloudAuth{
			Scheme: "azure", AccountName: os.Getenv("VAULTIC_LIVE_AZURE_ACCOUNT"), Secret: os.Getenv("VAULTIC_LIVE_AZURE_KEY"),
		}})
	}
	if a := os.Getenv("VAULTIC_LIVE_GS"); a != "" {
		targets = append(targets, liveTarget{"gs", a, vaulticCloudAuth{Scheme: "gs", ProjectID: os.Getenv("VAULTIC_LIVE_GS_PROJECT")}})
	}
	return targets
}

func (l liveTarget) options(password string) global.Options {
	opts := e2eOptions(l.address, password)
	opts.Backends = vaulticBackendsForSession(l.auth)
	return opts
}

func TestVaulticLiveBackendsRoundTrip(t *testing.T) {
	targets := liveTargets()
	if len(targets) == 0 {
		t.Skip("no live backend configured (see the comment at the top of this file)")
	}
	for _, target := range targets {
		t.Run(target.name, func(t *testing.T) {
			tree := filepath.Join(t.TempDir(), "arbre-e2e")
			e2eBuildTree(t, tree)
			want := e2eSnapshotTree(t, tree)

			// A unique repository path per run keeps the target reusable.
			target.address = target.address + "-" + filepath.Base(t.TempDir())
			ctx := context.Background()
			var out bytes.Buffer
			term := &vaulticTerminal{output: &out}
			opts := target.options(e2ePassword)
			opts.Term = term
			if err := runInit(ctx, InitOptions{RepositoryVersion: "stable"}, opts, nil, term); err != nil {
				t.Fatalf("init on %s: %v", target.name, err)
			}

			// Probe: the repository opens with the right password, and a wrong
			// password is a distinct failure, never a connection.
			if res := vaulticProbeRedacted(ctx, opts, vaulticProbeTimeout, target.auth, vaulticVerifyDepot); res.State != vaulticConnConnected {
				t.Fatalf("probe with the right password: %+v", res)
			}
			wrong := target.options("mauvais-mot-de-passe")
			if res := vaulticProbeRedacted(ctx, wrong, vaulticProbeTimeout, target.auth, vaulticVerifyDepot); res.State != vaulticConnAuthFailed {
				t.Fatalf("probe with a wrong password: %+v", res)
			}

			exec := vaulticBackupExecutor(opts, []string{tree}, nil, func(report *vaulticBackupReport, line string) { vaulticRecordLine(report, line) })
			report := &vaulticBackupReport{}
			outcome, _, err := report.result(exec(ctx, report))
			if err != nil || outcome != "Réussie" {
				t.Fatalf("backup to %s: %q %v", target.name, outcome, err)
			}
			report.mu.Lock()
			snapshot := report.summary.SnapshotID
			report.mu.Unlock()

			dest := filepath.Join(t.TempDir(), "restauré")
			if err := os.MkdirAll(dest, 0o700); err != nil {
				t.Fatal(err)
			}
			if err := vaulticRestoreSnapshot(ctx, opts, snapshot, nil, dest); err != nil {
				t.Fatalf("restore from %s: %v", target.name, err)
			}
			got := e2eSnapshotTree(t, e2eFindRestoredRoot(t, dest, "arbre-e2e"))
			if diff := e2eDiff(want, got); diff != "" {
				t.Fatalf("restored tree differs on %s:\n%s", target.name, diff)
			}
			if _, err := runCheck(ctx, CheckOptions{}, opts, nil, term); err != nil {
				t.Fatalf("check on %s: %v\n%s", target.name, err, out.String())
			}
		})
	}
}
