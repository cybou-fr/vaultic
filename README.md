# Vaultic (Go + Fyne)

A French-first, cross-platform desktop GUI for the **official restic** backup engine. The application does not implement its own repository format. Repositories are compatible with the restic CLI and other compatible tools.

**MVP 0.1 source release**, not a signed production build.

**Language:** the current desktop UI is in French (`fr-FR`). English localization (`en`) is planned. Technical documentation is maintained in English for international contributors; a [French introduction](README.fr.md) is available. Vaultic is developed in France.

Vaultic is licensed under **Apache-2.0**; see [LICENSE](LICENSE), [NOTICE](NOTICE), and [THIRD_PARTY_NOTICES.md](THIRD_PARTY_NOTICES.md).

## Features

- Configure a repository (local path, removable SSD, or a restic-supported remote repository).
- Initialize an encrypted repository.
- Choose folders and files to back up, show backup JSON progress, and cancel operations.
- List snapshots and restore a chosen snapshot to an **empty** folder.
- Run a read-only repository consistency check (`restic check`).
- Save nonsecret paths/config. Keep the password in RAM only, never in settings.
- Portable settings: keep `portable.flag` next to the executable. Settings are saved next to that executable.
- Single active operation at a time; no shell interpolation.

## Requirements

- Go 1.23+ (to build); a C compiler and platform graphics headers/libraries for Fyne.
- restic CLI installed separately (tested interface against docs for restic 0.19.1). https://restic.net/
- Fyne v2.8.2 (resolved automatically by `go mod download`).

## Build / run

```bash
go mod tidy
go run ./cmd/vaultic
# Linux / macOS desktop:
./scripts/build.sh
# Windows PowerShell:
./scripts/build.ps1
```

You can change the **restic** field in the app to the full path to `restic` / `restic.exe`. If `restic` is on your PATH, leave it as `restic`. For a standalone portable Windows folder, copy `restic.exe` into the same folder and leave the binary field as `restic`; the app auto-detects the adjacent executable. The app deliberately does not auto-download third-party executables.

If you put the app on an external SSD, keep `portable.flag` beside its executable. You can type `vault` as a **relative local repository path**, which the app resolves relative to its own executable (useful when the SSD drive letter changes). Select SSD paths for your data and encrypted repository. **Do not put the repository inside a folder being backed up.**

For macOS signed `.app` packaging and Windows installers, packaging/signing must be done on the target build system. Fyne can also target Android and iOS, but **this MVP does not run restic on mobile**: it invokes a desktop restic executable.

## Testing

```bash
go test ./internal/restic ./internal/settings
```

The core test suite uses a fake restic command, so it does **not** validate the full backup/restore workflow against an actual repository. For real-world readiness, run an end-to-end test with restic on each target OS and verify restored data.

## Security notes

- Passwords are never written to disk or shell command arguments by the app. The password is transmitted to the restic child process using the standard `RESTIC_PASSWORD` environment variable. Like other child-process environment variables, it may be readable by sufficiently privileged local software. This MVP does not yet use an OS keychain.
- Restic data is encrypted by restic. Do not lose the repository password; without it, restoration is impossible.
- Restore is blocked into nonempty folders. Existing files should never be used as a destination in this MVP.
- `check` examines repository structure; a deeper integrity audit may require `restic check --read-data`.
- No scheduling, retention rules, repository deletion, pruning, password storage, automatic updates, or cloud-account wizard in v0.1.
- For remote backends, provide any necessary provider credentials through your normal restic-compatible environment/configuration. Provider credentials are never collected by this GUI.

## Code layout

- `cmd/vaultic` — Fyne UI
- `internal/restic` — shell-free restic orchestration and JSON events
- `internal/settings` — nonsecret portable/application settings
- `scripts` — local build scripts

Vaultic is an independent project, not an official restic product.

**Development note:** when using `go run`, the executable lives in Go's build cache. Use an absolute repository path when developing; relative paths are intended for a built portable executable.

## License and attribution

Vaultic source code is released under Apache License 2.0. Copyright 2026
Vaultic contributors. This does not change the licenses of restic, Fyne, or
their dependencies. The bundled legal texts cover only direct components;
review all transitive Go dependencies before publishing binary distributions.
For redistributed applications include `LICENSE`, `NOTICE`, and
`THIRD_PARTY_NOTICES.md`.
