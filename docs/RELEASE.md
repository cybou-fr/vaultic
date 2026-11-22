# Releasing Vaultic

No CI is used: every step is run by hand on a developer machine.

## 1. Prepare

1. Set the version in `VAULTIC_VERSION` (for example `0.6.0`) and commit it.
2. `py scripts/check.py` must pass (format, vet, tests, race detector, build).
3. `govulncheck ./cmd/restic` must report no vulnerability that affects the
   code (`go install golang.org/x/vuln/cmd/govulncheck@latest` installs it).
   Fix findings with `go get <module>@<fixed version>` and `go mod tidy`, then
   repeat step 2.

## 2. Build and package

```powershell
py scripts/package.py
```

This builds `dist/vaultic.exe` (the version and commit are compiled in and
shown in Settings → About) and writes to `dist/release/`:

- `vaultic-<version>-<os>-<arch>.zip`: the program, licences and READMEs.
  Settings live in the user's profile.
- `vaultic-<version>-<os>-<arch>-portable.zip`: the same plus `portable.flag`,
  so settings live next to the program (for an external disk).
- `SHA256SUMS.txt`: checksums of both archives.

The script re-opens each archive and checks that the executable and licences
are present and that only the portable archive carries `portable.flag`.

## 3. Windows installer (not verified)

`installer/vaultic.iss` is an Inno Setup script for a per-user installer (no
administrator rights; it never deletes settings or stored passwords when
uninstalling). Inno Setup is not installed on the development machine, so the
script has **never been built or run**. Before publishing an installer:

1. Install Inno Setup, run `iscc installer\vaultic.iss`.
2. Install and uninstall it on a clean Windows machine and confirm that
   Vaultic starts, the Start Menu entry works and settings survive an
   uninstall.

## 4. Signing (not verified)

Unsigned Windows programs show SmartScreen warnings. With a code-signing
certificate, sign the executable during packaging:

```powershell
py scripts/package.py --sign-command "signtool sign /fd sha256 /f cert.pfx /p <password> /tr <timestamp url> /td sha256 {file}"
```

The command is run on `dist/vaultic.exe` before it is archived and is never
printed (it may contain secrets). This path has not been exercised because no
certificate is available.

## 5. Publish

Create a GitHub release whose tag is `v<version>` (for example `v0.6.0`) and
attach the archives and `SHA256SUMS.txt`. The "Check for updates" button in
Settings compares the latest release tag with the running version; it only
runs when the user presses it and never downloads anything.

## Platforms

| Platform | State |
|----------|-------|
| Windows amd64 | Built, tested, packaged. |
| Linux amd64 | Not built. Needs Go and the Fyne development packages (`libgl1-mesa-dev`, `xorg-dev`). Secret storage uses `secret-tool`; autostart uses an XDG file. |
| macOS | Not built. Secret storage is not wired (the password stays in memory); autostart uses a LaunchAgent. |

Linux and macOS code paths compile in isolation (the platform files are
cross-checked) but have never run.
