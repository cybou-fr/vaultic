# Vaultic — Encrypted backup with the restic engine built in

Vaultic is a French-first cross-platform Go/Fyne application for Windows, Linux and macOS. **The full Go application source, including restic v0.19.1 engine packages, is checked into this repository.** The build does not clone or download restic source. No separate restic executable is needed at runtime.

## Source layout

- `cmd/restic/`: restic command implementations plus `vaultic_*.go` GUI and integration.
- `internal/`: original restic engine, encrypted repository format, backends and file restoration.
- `go.mod`, `go.sum`: combined module with Fyne, using the original restic import path to preserve internal imports.
- `scripts/build.py`: local build, no upstream restic source fetch.
- `scripts/test-local.py`: explicitly invoked manual integration tests.

The restic baseline is v0.19.1, original commit `6aa3a516ce654808a1f28f9fa21e9b7c8e6e90bf`. See `LICENSE.restic` for the BSD-2-Clause license. Vaultic-authored code uses Apache-2.0.

## Build (manual)

Prerequisites: Go 1.25.10+, Python 3, and Fyne native/C graphics development libraries.

```bash
python3 scripts/build.py
```

Windows: `py scripts/build.py`. You may also run `go mod tidy && go build -o vaultic ./cmd/restic`. Go downloads required third-party **module dependencies**, not the restic source.

The no-argument executable starts the desktop GUI. Existing restic-style CLI commands are retained for diagnostics and interoperability. The repository format remains restic-compatible.

## Vaultic desktop interface

The native Fyne UI now uses a midnight-blue theme, muted aqua accent, vector application icon, rounded panels and seven single-purpose sidebar destinations: **Overview, Backup, Snapshots, Storage, Integrity, Activity and Settings**. Snapshot file browsing uses the same visual language.

Dashboard figures are derived from current in-app data. When snapshots have not yet been loaded, their count is shown as unknown rather than implying the backups are verified. All restic operations remain in-process.

## Profile exclusions and storage statistics

**Backup → Exclusions…** lets you configure native restic exclude patterns for each individual backup profile, one rule per line (e.g. `**/node_modules` or `*.tmp`). The editor validates patterns and saves them with the profile. They are passed directly to the embedded Go restic engine, not a CLI subprocess.

The **Storage** page runs restic's in-process `stats` in `raw-data` mode on demand. It shows referenced blob size, referenced blob count, and snapshots analyzed. These figures are **not total on-disk repository usage**: metadata, unreferenced data and free space are excluded. The operation is cancellable.

## Safe repository/profile management and exclusion preview

**Settings → Active repository** allows renaming the *display label* or **removing a repository from Vaultic's saved list**. Neither operation deletes or modifies the actual restic repository, snapshots, keys or backup files. The selected repository is reset and a fresh password is required when switching after removal.

**Backup** supports duplicating a profile (with separate source and exclude lists) or deleting its local configuration. The last profile is protected; no restic snapshots are removed.

**Preview exclusions…** scans local source paths read-only using restic's exclude pattern matching, with a cap of 25,000 visited entries and up to 80 example paths. The preview is explicitly indicative: an excluded directory represents one matched entry even when it contains many files, and `!` rules or concurrent filesystem changes can affect the real backup. It does **not** create a snapshot.

## Manual tests

```bash
python3 scripts/test-local.py
```

The Vaultic tests create disposable repositories. Original upstream Go test source is checked in, but upstream binary test fixtures are not; full upstream tests may therefore require additional fixtures.

**NO CI:** Do not add GitHub Actions or other CI. All builds and tests are opt-in local commands.

**Status:** Development software; verify backup and restore manually on each platform before relying on it for important data. French UI is the default; English UI localization is planned.
