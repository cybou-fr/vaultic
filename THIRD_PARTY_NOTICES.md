# Third-party notices

## restic v0.19.1 (source integration, BSD-2-Clause)

Upstream: https://github.com/restic/restic/tree/v0.19.1
Pinned upstream commit: `6aa3a516ce654808a1f28f9fa21e9b7c8e6e90bf`

**Vaultic embeds the official restic engine from source at build time.**
The combined executable contains restic code. This is not a subprocess
integration. Copyright (c) 2014, Alexander Neumann and contributors.

The full BSD-2-Clause terms and disclaimer must be included in binary
redistributions. Source: https://github.com/restic/restic/blob/v0.19.1/LICENSE

## Fyne v2.8.2 (BSD-3-Clause)

Upstream: https://github.com/fyne-io/fyne/tree/v2.8.2
Source: https://github.com/fyne-io/fyne/blob/v2.8.2/LICENSE

Fyne is linked into Vaultic; BSD-3-Clause attribution, disclaimer, and
non-endorsement conditions apply.

**Before publishing binaries**, generate a complete inventory of Go direct
and transitive dependencies from the *assembled* restic + Fyne module and
bundle the relevant license texts and attribution notices. This file by itself
is not a complete third-party license bill of materials.
