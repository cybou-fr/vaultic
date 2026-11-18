#!/usr/bin/env python3
"""Opt-in, local-only Vaultic integration tests. NO CI.

The tests use disposable temporary repositories. Never run against user data.
"""
from pathlib import Path
import subprocess

ROOT = Path(__file__).resolve().parent.parent

if __name__ == "__main__":
    if not (ROOT / "cmd/restic/vaultic_gui_test.go").is_file():
        raise RuntimeError("Full Vaultic source files missing")
    subprocess.run(
        ["go", "test", "./cmd/restic", "-run", "^TestVaultic", "-count=1", "-v"],
        cwd=ROOT, check=True,
    )
