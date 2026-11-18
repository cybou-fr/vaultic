#!/usr/bin/env python3
"""Build Vaultic from SOURCE ALREADY IN THIS REPOSITORY. No restic checkout.

No CI. Requires Go >= 1.25.10 and native dependencies required by Fyne.
Go modules may be downloaded normally; no upstream restic source is fetched.
"""
from pathlib import Path
import shutil
import subprocess
import sys

ROOT = Path(__file__).resolve().parent.parent

def run(*args):
    print("+", " ".join(str(v) for v in args), flush=True)
    subprocess.run([str(a) for a in args], cwd=ROOT, check=True)

def build():
    for required in ("go.mod", "cmd/restic/main.go", "cmd/restic/vaultic_gui.go",
                     "internal/repository", "LICENSE.restic"):
        if not (ROOT / required).exists():
            raise RuntimeError(f"Missing source: {required}. Clone the full Vaultic repository.")
    output = ROOT / "dist"
    output.mkdir(exist_ok=True)
    binary = output / ("vaultic.exe" if sys.platform == "win32" else "vaultic")
    run("go", "mod", "tidy")
    ldflags = "-s -w -H windowsgui" if sys.platform == "win32" else "-s -w"
    run("go", "build", "-ldflags", ldflags, "-o", binary, "./cmd/restic")
    for name in ("LICENSE", "LICENSE.restic", "NOTICE", "THIRD_PARTY_NOTICES.md", "portable.flag"):
        shutil.copy2(ROOT / name, output / name)
    fyne_dir = subprocess.check_output(
        ["go", "list", "-m", "-f", "{{.Dir}}", "fyne.io/fyne/v2"],
        cwd=ROOT, text=True,
    ).strip()
    shutil.copy2(Path(fyne_dir) / "LICENSE", output / "LICENSE.fyne")
    print(f"Vaultic binary: {binary}")

if __name__ == "__main__":
    build()
