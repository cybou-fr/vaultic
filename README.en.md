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

The native Fyne UI now uses a midnight-blue theme, muted aqua accent, vector application icon, rounded panels and eight single-purpose sidebar destinations: **Overview, Backup, Snapshots, Storage, Integrity, Activity, Repositories and Settings**. Snapshot file browsing uses the same visual language.

Dashboard figures are derived from current in-app data. When snapshots have not yet been loaded, their count is shown as unknown rather than implying the backups are verified. All restic operations remain in-process.

## Profile exclusions and storage statistics

**Backup → Exclusions…** lets you configure native restic exclude patterns for each individual backup profile, one rule per line (e.g. `**/node_modules` or `*.tmp`). The editor validates patterns and saves them with the profile. They are passed directly to the embedded Go restic engine, not a CLI subprocess.

The **Storage** page runs restic's in-process `stats` in `raw-data` mode on demand. It shows referenced blob size, referenced blob count, and snapshots analyzed. These figures are **not total on-disk repository usage**: metadata, unreferenced data and free space are excluded. The operation is cancellable.

## Safe repository/profile management and exclusion preview

On the **Repositories** page, **Edit** renames a saved repository or changes its location, and **Forget** **removes it from Vaultic's saved list**. Neither operation deletes or modifies the actual restic repository, snapshots, keys or backup files. The selected repository is reset and a fresh password is required when switching after removal.

**Backup** supports duplicating a profile (with separate source and exclude lists) or deleting its local configuration. The last profile is protected; no restic snapshots are removed.

**Preview exclusions…** scans local source paths read-only using restic's exclude pattern matching, with a cap of 25,000 visited entries and up to 80 example paths. The preview is explicitly indicative: an excluded directory represents one matched entry even when it contains many files, and `!` rules or concurrent filesystem changes can affect the real backup. It does **not** create a snapshot.

## Snapshot details and comparisons

On the **Snapshots** screen, **Taille et fichiers…** measures regular file count, directory count and the logical total file size by walking the selected snapshot. These values are not physical repository disk usage, and hardlinks are counted under each visible filename. Results are shown in the snapshot list for the current session.

**Comparer…** selects a reference snapshot (before) and compares it with the selected snapshot (after). Vaultic invokes the original restic `diff` function in process, providing added, removed and modified files, directories and referenced blob deltas. The collector retains only the first 180 path changes while processing the complete comparison. Metadata-only differences are not enabled. The operation may be cancelled; no snapshot is modified or deleted.

### Visual snapshot comparison (Before / After)

**Compare…** now opens a dedicated window with **two synchronized columns** (Before and After). Added paths appear only on the After side, removed paths only on the Before side, and modified paths on both sides. Use the **All / Added / Modified / Removed** filters (French GUI: Toutes / Ajoutés / Modifiés / Supprimés) or the path search field.

Category totals reflect the **entire native restic diff**, while only the first **180 changes per category** are retained for display (up to 540 paths), keeping memory bounded. Search is limited to retained paths, and the UI explicitly discloses that limit. Unchanged files are not listed, and metadata-only changes are not enabled. Comparing never modifies a repository or snapshot.

## Supported backup storage backends

The **Repositories → Add repository** page exposes the **nine backends registered in the embedded restic engine**: local, SFTP, REST, S3-compatible storage, Backblaze B2, Azure Blob Storage, Google Cloud Storage, OpenStack Swift and rclone. Each selection provides a sample repository address and connection requirements, and validation uses restic's original location parser.

SFTP requires an external SSH client; rclone requires an external installed and configured rclone program. All other registered backends are provided as Go libraries within the engine, though remote storage naturally requires network access and suitable credentials. Cloud credentials should be configured externally through the provider and supported environment variables; Vaultic never stores cloud API keys in settings (see the Repositories page below for in-memory session entry). The restic repository password is separate.

### Timeline and guided file restoration

The **Snapshots** page groups entries by day on a visual timeline. Size and file counts appear on the corresponding row after manual analysis.

From the **Before / After** comparison window, select a changed path and click **Ouvrir pour restaurer…** to open the appropriate snapshot in the file explorer. Removed files open the *Before* snapshot; additions and modifications open the *After* snapshot. The file is preselected when its type permits restoration. The user must still choose an empty destination and confirm the restore. Existing files are never deliberately overwritten.

## Repositories page: guided setup for all nine storage types

The **Repositories** page has a single purpose: choose, configure and verify a repository.

1. **Storage type**: nine cards (local, SFTP, REST, S3, Backblaze B2, Azure Blob, Google Cloud, OpenStack Swift, rclone) with keyboard navigation (Tab, arrows, Enter or Space).
2. **Location**: a form specific to each type, with inline validation and a preview of the restic address it produces. The SSH port is a dedicated field (`sftp://user@host:port//path` form). Addresses are checked by the real restic parser, and dangerous values (host starting with `-`, `..`, URL parameters, credentials inside the URL) are refused.
3. **Name and restic password**: the repository encryption password stays separate and is never saved.

Buttons: **Test connection**, **Open existing repository**, **Create new repository**. The step-by-step S3, B2 and Azure assistants remain available.

### Connection states

*Not tested*, *Testing*, *Reachable*, *Authentication failed*, *Repository unavailable* and *Connected*. **Reachable** only means the storage answered and a restic repository exists: without the password Vaultic never reports "Connected". A wrong restic password, refused storage credentials, insufficient rights, a network error and a missing repository each produce distinct messages. Every test has a 45 s timeout and can be cancelled.

### Credentials (session only)

- REST (server user and password), Google Cloud (project ID), S3, B2, Azure and **OpenStack Swift** (Keystone v3: URL, region, domains, project, user and password, or an Application Credential) can be entered in Vaultic.
- They stay **in memory**, isolated per repository, with no global environment variable and no subprocess, and never appear in the address, `vaultic.json` or logs. They are wiped when Vaultic closes.
- For Swift, credentials entered here fully replace the system `OS_*` variables; empty fields mean system authentication.
- Google Cloud uses Application Default Credentials only; SFTP uses your SSH agent/key; rclone uses its own configuration.

### Repository list

Each saved repository shows its name, provider, location (without credentials), last verification, and the actions **Connect**, **Edit**, **Test** and **Forget**. **Forget** only removes the entry from Vaultic: no backup is deleted, locally or remotely.

### Known limitations

- Cloud connections (S3, B2, Azure, GCS, Swift, rclone, SFTP) have not yet been validated against real services; REST and Swift are tested with mock servers and local storage with real temporary repositories.
- The restic engine **creates the Swift container** when it does not exist on open, including during a test.
- Swift: Keystone v3 only; Application Credentials by ID. The Keystone URL must be HTTPS (HTTP only to localhost), end with `/v3` and contain no credentials, query or fragment; it is normalized before being kept in memory.
- Google Cloud: no key entry in Vaultic (ADC only).

## Backup automation

Under **Backup → Automation** (*Automatisation*), every profile has its own schedule: *manual only*, *every N hours*, *daily at a chosen time*, *weekly* (chosen weekdays) or *monthly* (chosen day; 29–31 becomes the last day of a shorter month). Times use the computer's local time zone. Secondary options (weekdays, day of the month, notifications) are under **Advanced options**.

The page shows the frequency, the **next planned run**, the **last successful backup**, current activity, recent history and a **pause / resume scheduling** button.

**How it works**

- The scheduler only works **while the Vaultic process runs**. There is no Windows service, no system task scheduler entry and no hidden process: in the background Vaultic stays visible in the notification area (menu: Open, Back up now, Quit).
- **Remembered password (optional)**: the "Remember the password on this computer" check box (Repositories page) puts the repository password in the system credential store (Windows Credential Manager; Linux `secret-tool`/libsecret; macOS not supported yet). Vaultic only writes the "remembered" flag to `vaultic.json`, never the password. Unticking the box, or forgetting the repository, erases the password from the system. Without the box nothing changes: the password is typed at every launch.
- **Background (Settings)**: "Closing the window minimises Vaultic to the notification area" and "Start Vaultic with my session" (per-user Windows startup entry, XDG file on Linux, LaunchAgent on macOS; started with `--background`, window hidden).
- **Snapshot retention ("Conservation" tab)**: per-profile rules (last N, one per day, week, month, year). **Nothing is ever deleted automatically**: not after a backup and not by the scheduler. "Preview" lists exactly the snapshots that would be deleted (and the kept ones with the reason) without changing anything; "Delete the listed snapshots…" asks for confirmation, re-checks the repository right before, and deletes only the previewed IDs, only those of the profile (same machine, same sources). The newest snapshot is always kept. Disk space is only given back by the separate "Free disk space…" step (prune). Every deletion is written to the history.
- **Language (Settings)**: "Automatic", "Français" or "English". The choice is saved in `vaultic.json` (key `language`) and applies at the next start. Messages are written in French, which is also their key; the English catalog is `cmd/restic/vaultic_i18n_en.go`. A test checks that every message has a translation, that the format verbs (`%s`, `%d`) and line breaks match, and that no French text remains on the main screens in English. To add a message: wrap it in `vaulticT("…")`, then complete the catalog (`VAULTIC_I18N_DUMP=file.txt go test ./cmd/restic -run EnglishCatalogCovers` lists what is missing). Known limit: some restic engine error messages and messages built by concatenation are still partly French.
- **Version and updates (Settings)**: the compiled version and commit are shown in About. "Check for updates" sends **one anonymous request** to GitHub's releases API, only when you press the button; nothing is downloaded or installed. Release procedure: `docs/RELEASE.md` (`py scripts/package.py` builds the standard and portable archives and their SHA-256 sums).
- **Stale backup alert**: when the last successful backup is more than twice the scheduled interval old, a warning appears in the Automation tab and a notification (at most one per day) is sent. It never starts a backup.
- Each schedule is bound to the repository selected when the schedule is saved. Switching the active repository never redirects the scheduled backup. The bound repository must be active and its **password must have been typed** (it is never saved), otherwise the backup remains overdue until the correct repository and credentials are available.
- **No backlog**: when several slots were missed (application closed, computer asleep, clock changed) a single catch-up backup runs. Resuming after a pause catches nothing up.
- **One restic operation at a time**: manual backups, scheduled backups and other operations share one engine. If it is busy, the scheduled backup is deferred by 5 minutes, at most 3 times, then **skipped and recorded** until the next slot. There is no silent retry after a failure.
- A backup counts as successful only if restic confirms a snapshot ID and reported no unreadable file.
- Daylight-saving changes, time-zone changes and clock changes are handled: a slot fires once per calendar day, and a clock set back simply restarts the countdown.
- Vaultic **never** runs `forget` or `prune` and never deletes snapshots automatically.

**History**: `vaultic-history.json` (next to `vaultic.json`, hence portable) keeps the last 300 runs: profile, repository (without credentials), manual/scheduled trigger, status (success, failure, cancelled, skipped), duration, files and bytes, snapshot ID and a short **sanitized** error summary (secrets masked, one line). No password, token or raw diagnostic message is stored. Writes are atomic.

Desktop **notifications**: backup completed, failed, skipped, overdue — each category can be turned off. The same event is notified once and bursts are limited.

**Limitations**: a schedule does not protect your files if Vaultic is closed or the password has not been typed; each schedule uses a fixed destination, and the active repository must match it; history and last-success indicators are scoped to that repository; notifications depend on system support; no retention policy in this version.

## Manual tests

```bash
python3 scripts/test-local.py
```

The Vaultic tests create disposable repositories. Original upstream Go test source is checked in, but upstream binary test fixtures are not; full upstream tests may therefore require additional fixtures.

**NO CI:** Do not add GitHub Actions or other CI. All builds and tests are opt-in local commands.

**Status:** Development software; verify backup and restore manually on each platform before relying on it for important data. French UI is the default; English UI localization is planned.
