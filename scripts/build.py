#!/usr/bin/env python3
"""Build Vaultic from SOURCE ALREADY IN THIS REPOSITORY. No restic checkout.

No CI. Requires Go >= 1.25.10 and native dependencies required by Fyne.
Go modules may be downloaded normally; no upstream restic source is fetched.
"""
from pathlib import Path
import shutil
import stat
import subprocess
import sys

ROOT = Path(__file__).resolve().parent.parent

def run(*args):
    print("+", " ".join(str(v) for v in args), flush=True)
    subprocess.run([str(a) for a in args], cwd=ROOT, check=True)

def git_commit():
    """Short commit hash, with -dirty when the tree has uncommitted changes; "" outside a checkout."""
    try:
        commit = subprocess.check_output(["git", "rev-parse", "--short", "HEAD"], cwd=ROOT, text=True, stderr=subprocess.DEVNULL).strip()
        dirty = subprocess.check_output(["git", "status", "--porcelain"], cwd=ROOT, text=True, stderr=subprocess.DEVNULL).strip()
    except (OSError, subprocess.CalledProcessError):
        return ""
    return commit + ("-dirty" if dirty else "")


def build():
    for required in ("go.mod", "cmd/restic/main.go", "cmd/restic/vaultic_gui.go",
                     "internal/repository", "LICENSE.restic"):
        if not (ROOT / required).exists():
            raise RuntimeError(f"Missing source: {required}. Clone the full Vaultic repository.")
    output = ROOT / "dist"
    output.mkdir(exist_ok=True)
    binary = output / ("vaultic.exe" if sys.platform == "win32" else "vaultic")
    version = (ROOT / "VAULTIC_VERSION").read_text(encoding="utf-8").strip()
    if sys.platform == "win32":
        # Keep the version shown in Explorer's Properties in step with
        # VAULTIC_VERSION. Without windres the committed resource is used as is.
        subprocess.run([sys.executable, str(ROOT / "scripts" / "make-icon.py"), "--resources-only"], cwd=ROOT, check=False)
    commit = git_commit()
    ldflags = "-s -w -H windowsgui" if sys.platform == "win32" else "-s -w"
    ldflags += f" -X main.vaulticVersion={version} -X main.vaulticCommit={commit}"
    run("go", "build", "-mod=readonly", "-ldflags", ldflags, "-o", binary, "./cmd/restic")
    for name in ("LICENSE", "LICENSE.restic", "NOTICE", "THIRD_PARTY_NOTICES.md"):
        shutil.copy2(ROOT / name, output / name)
    # The dist folder is a portable build: this marker makes settings live next
    # to the executable. It is generated here and not tracked in the repository,
    # so running from a checkout never switches to portable mode by accident.
    (output / "portable.flag").write_text(
        "This empty marker file enables portable settings next to the executable.\n"
        "Do not store passwords here.\n", encoding="utf-8")
    fyne_dir = subprocess.check_output(
        ["go", "list", "-m", "-f", "{{.Dir}}", "fyne.io/fyne/v2"],
        cwd=ROOT, text=True,
    ).strip()
    fyne_license = output / "LICENSE.fyne"
    if sys.platform == "win32" and fyne_license.exists():
        # Older builds copied the Go module cache's read-only file mode.
        fyne_license.chmod(stat.S_IREAD | stat.S_IWRITE)
    shutil.copyfile(Path(fyne_dir) / "LICENSE", fyne_license)
    print(f"Vaultic binary: {binary}")

if __name__ == "__main__":
    build()
