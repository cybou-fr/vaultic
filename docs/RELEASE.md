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

## 3. Windows installer

`installer/vaultic.iss` is an Inno Setup script for a per-user installer (no
administrator rights; it never deletes settings or stored passwords when
uninstalling). `py scripts/package.py` builds it when Inno Setup is installed
(`vaultic-<version>-windows-amd64-setup.exe`, included in `SHA256SUMS.txt`);
`--no-installer` skips it.

Verified on the development machine (Inno Setup 7.1.1): a silent install into
a temporary folder (`/VERYSILENT /DIR=...`) put the program and licences there,
registered "Vaultic 0.5.0 / Stanislav SAVELIEV" in the user's uninstall list and
created a Start Menu shortcut; the installed program started; a silent
uninstall removed the files, the uninstall entry and the shortcut. Still to do
before publishing: try it on a clean machine (another user account or a virtual
machine) and in French.

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
