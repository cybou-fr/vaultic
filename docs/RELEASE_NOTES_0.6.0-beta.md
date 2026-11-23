# Vaultic 0.6.0-beta

First public beta. Vaultic is a desktop backup application (Windows first) with
an embedded restic 0.19.1 engine: encrypted, deduplicated, incremental backups
to a local folder or to cloud storage, with a graphical interface in French and
English.

**This is a beta.** Try it on data that also exists elsewhere, and do not
depend on it as your only backup yet. Please report problems with the log
files (Settings → Diagnostic → "Open the logs folder"; they never contain
passwords).

## What it does

- Backup profiles (folders and files, exclusions with a preview), manual backup
  with live progress and cancellation.
- Storage: local folder, SFTP, REST server, Amazon S3 and compatible, Backblaze
  B2, Google Cloud Storage, Azure Blob, OpenStack Swift (Keystone v3), rclone;
  connection test that tells a wrong password from an unreachable server.
- Snapshots: list, details, compare, explore and restore (whole or selected
  files), repository integrity check, storage analysis.
- Retention: forget old snapshots with a mandatory preview, a confirmation and a
  separate prune step.
- Automation: schedules (hourly, daily, weekly, monthly), tray icon, close to
  tray, optional autostart, stale-backup warning. Backups run only while
  Vaultic is running; there is no Windows service.
- Passwords are kept in memory for the session; remembering them in the
  Windows Credential Manager is opt-in. Nothing secret is written to settings
  or logs.
- Single instance per settings file; rotating log files; manual "check for
  updates" (never automatic, downloads nothing).
- Windows installer (per user, no administrator rights) and a portable zip.

## What was tested

- Unit and interface tests, race detector, `go vet`, `govulncheck` (no
  reachable vulnerability).
- End-to-end, byte-for-byte restore tests, including Cyrillic paths and wrong
  passwords.
- Real servers (emulators): S3 (SeaweedFS), REST (rest-server), Azure
  (Azurite); backup, restore, check, wrong password and wrong key.
- A 40-minute soak run: 807 backups, 201 exact restores, 80 prune and check
  cycles, no memory, goroutine or handle growth.
- Installer and portable zip on a Windows 11 development PC.

## Known limitations

- **Not tested against the real services**: Amazon S3, Backblaze B2, Google
  Cloud Storage, Swift, Azure, SFTP and rclone have not been run with real
  accounts. Google Cloud Storage and Swift have no emulator test at all.
- **No clean-machine test yet**: Windows 10, a machine without a graphics
  driver, other locales, upgrading over an older version and the installer
  wizard on screen are untested.
- The executable is **not signed**: Windows SmartScreen will warn.
- The connection test cannot reach an endpoint that uses a private certificate
  authority.
- The scheduler works only while Vaultic runs; sleep/wake and multi-day runs
  have not been verified.
- Linux and macOS: the code compiles for them in part but there are no builds,
  and nothing has been run there.
- No key management yet (add, change or remove a repository password).
- The update check sees only full releases, not betas.

## Files

- `vaultic-0.6.0-beta-windows-amd64-setup.exe`: installer.
- `vaultic-0.6.0-beta-windows-amd64.zip`: program, settings in the user profile.
- `vaultic-0.6.0-beta-windows-amd64-portable.zip`: settings next to the program.
- `SHA256SUMS.txt`: checksums.
