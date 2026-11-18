# Third-party notices

## Official restic v0.19.1 — BSD-2-Clause

Original code from https://github.com/restic/restic/tree/v0.19.1,
commit `6aa3a516ce654808a1f28f9fa21e9b7c8e6e90bf`.

The Go engine, CLI and original Go test source are present under
`internal/` and `cmd/restic/`, and build directly from this repository.
This is not a wrapper and does not require a restic executable at runtime.

The **original BSD-2-Clause terms are preserved in [LICENSE.restic](LICENSE.restic)**.
Copyright (c) 2014, Alexander Neumann, and contributors.

## Fyne v2.8.2 — BSD-3-Clause

Upstream: https://github.com/fyne-io/fyne/tree/v2.8.2
License: https://github.com/fyne-io/fyne/blob/v2.8.2/LICENSE

Fyne is a Go module dependency linked into Vaultic. Manual build scripts
copy Fyne's LICENSE to the binary distribution folder.

## Dependency audit required before production binaries

This is not yet a comprehensive inventory of the transitive Go modules
included in release artifacts. Before public releases, review and ship
all relevant third-party notices and license texts.

Vaultic's own contributions remain under Apache License 2.0.
