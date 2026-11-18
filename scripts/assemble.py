#!/usr/bin/env python3
"""Build a single Vaultic binary with official restic sources linked directly.

Requires network access at BUILD time, Git, Go 1.25.10+ and Fyne C dependencies.
No restic CLI executable is required at RUNTIME.
"""
from pathlib import Path
import os
import shutil
import subprocess
import sys

ROOT = Path(__file__).resolve().parent.parent
VERSION = "v0.19.1"
COMMIT = "6aa3a516ce654808a1f28f9fa21e9b7c8e6e90bf"
WORK = ROOT / ".vaultic-build"
UPSTREAM = WORK / "restic"

def run(*args, cwd=None):
    print("+", " ".join(str(arg) for arg in args), flush=True)
    subprocess.run([str(arg) for arg in args], cwd=cwd, check=True)

def prepare():
    if not (UPSTREAM / "go.mod").exists():
        WORK.mkdir(exist_ok=True)
        run("git", "clone", "--depth", "1", "--branch", VERSION,
            "https://github.com/restic/restic.git", UPSTREAM)
    head = subprocess.check_output(["git", "rev-parse", "HEAD"], cwd=UPSTREAM, text=True).strip()
    if head != COMMIT:
        raise RuntimeError(f"Unexpected restic commit: {head}; expected {COMMIT}. Remove {UPSTREAM} and retry.")
    target = UPSTREAM / "cmd" / "restic"
    for name in ("vaultic_gui.go", "vaultic_settings.go", "vaultic_gui_test.go"):
        shutil.copy2(ROOT / "overlay" / name, target / name)
    main = target / "main.go"
    code = main.read_text(encoding="utf-8")
    anchor = "func main() {\n"
    hook = "func main() {\n\tif len(os.Args) == 1 {\n\t\tvaulticMain()\n\t\treturn\n\t}\n"
    if hook not in code:
        if code.count(anchor) != 1:
            raise RuntimeError("restic main.go changed; cannot safely integrate the GUI")
        main.write_text(code.replace(anchor, hook, 1), encoding="utf-8")
    run("go", "mod", "edit", "-require=fyne.io/fyne/v2@v2.8.2", cwd=UPSTREAM)
    run("go", "mod", "tidy", cwd=UPSTREAM)

def build():
    out = ROOT / "dist"
    out.mkdir(exist_ok=True)
    is_windows = sys.platform == "win32"
    filename = "vaultic.exe" if is_windows else "vaultic"
    flags = ["-ldflags=-s -w -H windowsgui"] if is_windows else ["-ldflags=-s -w"]
    # -ldflags supplied as one argument; it is not interpreted by a shell.
    run("go", "build", *flags, "-o", out / filename, "./cmd/restic", cwd=UPSTREAM)
    for name in ("LICENSE", "NOTICE", "THIRD_PARTY_NOTICES.md", "portable.flag"):
        src = ROOT / name
        if src.exists():
            shutil.copy2(src, out / name)
    # Preserve upstream license texts in binary distributions.
    shutil.copy2(UPSTREAM / "LICENSE", out / "LICENSE.restic")
    fyne_module = subprocess.check_output(
        ["go", "list", "-m", "-f", "{{.Dir}}", "fyne.io/fyne/v2"],
        cwd=UPSTREAM, text=True,
    ).strip()
    shutil.copy2(Path(fyne_module) / "LICENSE", out / "LICENSE.fyne")
    print("Built:", out / filename)

if __name__ == "__main__":
    prepare()
    build()
