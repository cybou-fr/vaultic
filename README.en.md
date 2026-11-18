# Vaultic — Native restic-based desktop backup app

Vaultic is a Go + Fyne backup application integrating the **official restic v0.19.1 engine in-process**, built into a single executable. **Vaultic does not launch the restic CLI as a child process**, and does not invent its own repository format.

French is the default UI language; English localization is planned.

Build with `python3 scripts/assemble.py` (Windows: `py scripts/assemble.py`), requiring Git, Go 1.25.10+, Python 3, and Fyne desktop C/graphics build dependencies. At build time the script retrieves restic sources at pinned commit `6aa3a516ce654808a1f28f9fa21e9b7c8e6e90bf`, installs the GUI overlay into `cmd/restic`, and builds one native binary. The output requires no separate restic executable.

Current development state: encrypted repository initialization, backup with native progress, snapshot listing, restore to an empty directory, structural repository checks, portable nonsecret settings. Not yet qualified for production backups or releases.

The repo format, cryptography, deduplication, and backend implementations are supplied by upstream restic unchanged. Vaultic GUI code is Apache-2.0; restic remains BSD-2-Clause; Fyne remains BSD-3-Clause. See [README.md](README.md), [LICENSE](LICENSE), [NOTICE](NOTICE), and [THIRD_PARTY_NOTICES.md](THIRD_PARTY_NOTICES.md).
