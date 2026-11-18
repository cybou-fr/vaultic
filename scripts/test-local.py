#!/usr/bin/env python3
"""Run Vaultic's in-process restic tests manually. NO CI, NO automation.

Requires Git, Python, Go 1.25.10+ and Fyne native build dependencies.
Tests use local temporary restic repositories; never use a user's repository.
"""
from assemble import UPSTREAM, prepare, run

if __name__ == "__main__":
    prepare()
    run("go", "test", "./cmd/restic", "-run", "^TestVaultic", "-count=1", "-v", cwd=UPSTREAM)
