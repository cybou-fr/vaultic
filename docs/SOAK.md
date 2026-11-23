# Soak test

`TestVaulticSoakBackupRestoreRetention` (`cmd/restic/vaultic_soak_test.go`)
runs the engine calls the application makes, in one process, for a chosen time:
backup of a changing folder (edits, additions, deletions, an 8 MiB file every
25 cycles), restore of a random remembered snapshot compared byte for byte,
and every 10 cycles forget of the oldest snapshots beyond 8, prune and `check`.
It samples heap, goroutines and open handles, and fails on a wrong restore or
on unbounded growth.

```
VAULTIC_SOAK_MINUTES=40 go test ./cmd/restic -run VaulticSoak -v -timeout 90m
```

## Result (Windows, local repository, 40 minutes)

807 backups, 201 restores, 80 forgets, 80 prunes, 80 checks, no failure.

| | start (cycle 5) | end |
|---|---|---|
| heap after GC | 12 MB | 4 MB, flat throughout |
| goroutines | 11 | 36, flat at 30-40 after the first minute |
| open handles | 232 | 267, in a few steps (+35 in 40 minutes) |

Goroutines are higher than at start because restic delays the cleanup of its
lock contexts by one minute (`UnlockCancelDelay`); the test waits for that grace
period before judging, and they stay bounded.

## Limits

- This is accelerated, in-process, on a local disk: ~1 cycle every 3 s. It does
  not cover the GUI, the tray, the scheduler's timers, sleep/wake, network
  interruptions or a real day-long run.
- Handles rose by about 35 in the first 40 minutes and stepped rarely; it did
  not grow linearly, but a multi-day run has not been done.
