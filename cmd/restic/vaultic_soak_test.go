package main

import (
	"context"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"runtime"
	"runtime/pprof"
	"strconv"
	"strings"
	"testing"
	"time"
)

// Soak test: many backup / restore / retention / check cycles in one process,
// watching for leaks (memory, goroutines, open handles) and for any snapshot
// that stops restoring exactly. It is skipped unless VAULTIC_SOAK_MINUTES is
// set, e.g.  VAULTIC_SOAK_MINUTES=30 go test ./cmd/restic -run VaulticSoak -v -timeout 2h
//
// It exercises the same in-process engine calls the application makes for a
// scheduled backup, so a leak here is a leak in a Vaultic that stays running
// for days in the tray.

type soakSample struct {
	cycle      int
	heapMB     float64
	sysMB      float64
	goroutines int
	handles    int
}

func soakMeasure(cycle int) soakSample {
	runtime.GC()
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	return soakSample{cycle, float64(m.HeapAlloc) / 1e6, float64(m.Sys) / 1e6, runtime.NumGoroutine(), soakHandleCount()}
}

func (s soakSample) String() string {
	return fmt.Sprintf("cycle %4d  heap %6.1f MB  sys %6.1f MB  goroutines %3d  handles %4d", s.cycle, s.heapMB, s.sysMB, s.goroutines, s.handles)
}

// soakMutate changes a working tree the way a real folder changes between two
// backups: some files edited, some added, some removed, an occasional big file.
func soakMutate(t *testing.T, rng *rand.Rand, tree string, cycle int) {
	t.Helper()
	entries, err := os.ReadDir(tree)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if rng.Intn(10) == 0 {
			if rng.Intn(2) == 0 {
				_ = os.Remove(filepath.Join(tree, e.Name()))
			} else {
				soakWrite(t, rng, filepath.Join(tree, e.Name()), 1+rng.Intn(200_000))
			}
		}
	}
	for i := 0; i < 1+rng.Intn(6); i++ {
		soakWrite(t, rng, filepath.Join(tree, fmt.Sprintf("c%04d-%d.dat", cycle, i)), 1+rng.Intn(300_000))
	}
	if cycle%25 == 0 {
		soakWrite(t, rng, filepath.Join(tree, fmt.Sprintf("gros-%04d.bin", cycle)), 8<<20)
	}
}

func soakWrite(t *testing.T, rng *rand.Rand, path string, size int) {
	t.Helper()
	buf := make([]byte, size)
	rng.Read(buf)
	if err := os.WriteFile(path, buf, 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestVaulticSoakBackupRestoreRetention(t *testing.T) {
	minutes, _ := strconv.Atoi(os.Getenv("VAULTIC_SOAK_MINUTES"))
	if minutes <= 0 {
		t.Skip("set VAULTIC_SOAK_MINUTES to run the soak test")
	}
	deadline := time.Now().Add(time.Duration(minutes) * time.Minute)
	rng := rand.New(rand.NewSource(20260611))
	tree := filepath.Join(t.TempDir(), "arbre-soak")
	if err := os.MkdirAll(tree, 0o700); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 40; i++ {
		soakWrite(t, rng, filepath.Join(tree, fmt.Sprintf("base-%02d.dat", i)), 1+rng.Intn(400_000))
	}
	repo, g := initLocalTestRepository(t, e2ePassword)
	ctx := context.Background()

	remembered := map[string][]string{} // snapshot ID -> expected tree listing
	var samples []soakSample
	baseline := soakSample{}
	var backups, restores, forgets, prunes, checks int

	for cycle := 1; time.Now().Before(deadline); cycle++ {
		soakMutate(t, rng, tree, cycle)
		id, outcome, err := e2eBackup(t, repo, e2ePassword, []string{tree})
		if err != nil || outcome != "Réussie" {
			t.Fatalf("cycle %d: backup %q: %v", cycle, outcome, err)
		}
		backups++
		remembered[id] = e2eSnapshotTree(t, tree)

		if cycle%4 == 0 { // restore a random remembered snapshot and compare
			var pick string
			n := rng.Intn(len(remembered))
			for k := range remembered {
				if n == 0 {
					pick = k
					break
				}
				n--
			}
			restored := e2eRestore(t, repo, e2ePassword, pick)
			got := e2eSnapshotTree(t, e2eFindRestoredRoot(t, restored, "arbre-soak"))
			if diff := e2eDiff(remembered[pick], got); diff != "" {
				t.Fatalf("cycle %d: snapshot %s no longer restores exactly:\n%s", cycle, pick, diff)
			}
			restores++
			_ = os.RemoveAll(restored)
		}

		if cycle%10 == 0 { // retention: forget the oldest beyond 8, prune, check
			list, err := vaulticListSnapshots(ctx, g)
			if err != nil {
				t.Fatalf("cycle %d: list: %v", cycle, err)
			}
			if len(list) > 8 {
				var drop []string
				for _, s := range list[8:] {
					drop = append(drop, s.ID)
					delete(remembered, s.ID)
				}
				if err := vaulticForgetSnapshots(ctx, g, drop); err != nil {
					t.Fatalf("cycle %d: forget: %v", cycle, err)
				}
				forgets++
				if err := vaulticPruneRepository(ctx, g); err != nil {
					t.Fatalf("cycle %d: prune: %v", cycle, err)
				}
				prunes++
			}
			if _, err := runCheck(ctx, CheckOptions{}, g, nil, g.Term); err != nil {
				t.Fatalf("cycle %d: check: %v", cycle, err)
			}
			checks++
		}

		if cycle == 5 {
			baseline = soakMeasure(cycle)
			t.Log("baseline   ", baseline)
		}
		if cycle%20 == 0 {
			s := soakMeasure(cycle)
			samples = append(samples, s)
			t.Log(s)
		}
	}

	// Restic delays the cleanup of its lock contexts by up to a minute after a
	// command ends (UnlockCancelDelay), so goroutines are judged only after
	// that grace period.
	settle := time.Now().Add(100 * time.Second)
	final := soakMeasure(-1)
	for final.goroutines > baseline.goroutines+25 && time.Now().Before(settle) {
		time.Sleep(5 * time.Second)
		final = soakMeasure(-1)
	}
	t.Logf("final       %s", final)
	t.Logf("operations: %d backups, %d restores, %d forgets, %d prunes, %d checks", backups, restores, forgets, prunes, checks)
	if baseline.cycle == 0 {
		t.Skip("run too short to have a baseline")
	}
	// Leak thresholds are generous: they catch unbounded growth, not noise.
	if final.goroutines > baseline.goroutines+25 {
		var dump strings.Builder
		_ = pprof.Lookup("goroutine").WriteTo(&dump, 1)
		t.Logf("goroutine profile:\n%s", dump.String())
		t.Errorf("goroutines grew from %d to %d", baseline.goroutines, final.goroutines)
	}
	if baseline.handles > 0 && final.handles > baseline.handles+50 {
		t.Errorf("open handles grew from %d to %d", baseline.handles, final.handles)
	}
	if final.heapMB > baseline.heapMB*3+100 {
		t.Errorf("heap grew from %.1f MB to %.1f MB", baseline.heapMB, final.heapMB)
	}
}
