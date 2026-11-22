#!/usr/bin/env python3
"""Build Vaultic and package it for distribution. NO CI.

    py scripts/package.py                  build, then write dist/release/*
    py scripts/package.py --skip-build     package the existing dist/ build
    py scripts/package.py --sign-command "signtool sign /fd sha256 /f cert.pfx /p secret /tr http://timestamp.example {file}"

Output, in dist/release/:
    vaultic-<version>-<os>-<arch>.zip            installed-style package (settings in the user profile)
    vaultic-<version>-<os>-<arch>-portable.zip   portable package (settings next to the program)
    SHA256SUMS.txt                               checksums of both archives

The version comes from VAULTIC_VERSION. Signing is optional and only runs when
--sign-command is given; the command is run on the executable before it is
archived, with {file} replaced by its path. The signing step is not exercised
by the repository's own checks, because it needs a code-signing certificate.
"""
import argparse
import hashlib
import platform
import shutil
import subprocess
import sys
import zipfile
from pathlib import Path

ROOT = Path(__file__).resolve().parent.parent
sys.path.insert(0, str(Path(__file__).resolve().parent))
import build  # noqa: E402  (scripts/build.py)

NOTICES = ("LICENSE", "LICENSE.restic", "NOTICE", "THIRD_PARTY_NOTICES.md")
READMES = ("README.md", "README.fr.md", "README.en.md")


def target():
    system = {"Windows": "windows", "Linux": "linux", "Darwin": "darwin"}.get(platform.system(), platform.system().lower())
    machine = platform.machine().lower()
    arch = {"amd64": "amd64", "x86_64": "amd64", "arm64": "arm64", "aarch64": "arm64"}.get(machine, machine)
    return system, arch


def sha256(path):
    h = hashlib.sha256()
    with open(path, "rb") as f:
        for block in iter(lambda: f.read(1 << 20), b""):
            h.update(block)
    return h.hexdigest()


def make_zip(path, folder, files, portable):
    """files: list of (source, name inside the archive)."""
    with zipfile.ZipFile(path, "w", zipfile.ZIP_DEFLATED) as z:
        for source, name in files:
            z.write(source, f"{folder}/{name}")
        if portable:
            z.writestr(f"{folder}/portable.flag",
                       "This empty marker file enables portable settings next to the executable.\n"
                       "Do not store passwords here.\n")


def verify_zip(path, portable, exe_name):
    with zipfile.ZipFile(path) as z:
        bad = z.testzip()
        if bad:
            raise RuntimeError(f"{path.name}: corrupt member {bad}")
        names = [n.split("/", 1)[1] for n in z.namelist()]
    if exe_name not in names:
        raise RuntimeError(f"{path.name}: {exe_name} missing")
    if ("portable.flag" in names) != portable:
        raise RuntimeError(f"{path.name}: portable.flag must be {'present' if portable else 'absent'}")
    for required in NOTICES:
        if required not in names:
            raise RuntimeError(f"{path.name}: {required} missing")


def main():
    parser = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    parser.add_argument("--skip-build", action="store_true")
    parser.add_argument("--sign-command", help="command run on the executable; {file} is replaced by its path")
    args = parser.parse_args()

    if not args.skip_build:
        build.build()
    dist = ROOT / "dist"
    system, arch = target()
    exe_name = "vaultic.exe" if system == "windows" else "vaultic"
    exe = dist / exe_name
    if not exe.is_file():
        raise SystemExit(f"{exe} not found: build first")
    if args.sign_command:
        command = args.sign_command.replace("{file}", str(exe))
        print("signing the executable", flush=True)  # the command may hold secrets: never print it
        subprocess.run(command, shell=True, check=True)

    version = (ROOT / "VAULTIC_VERSION").read_text(encoding="utf-8").strip()
    folder = f"vaultic-{version}"
    out = dist / "release"
    shutil.rmtree(out, ignore_errors=True)
    out.mkdir(parents=True)

    files = [(exe, exe_name)] + [(ROOT / n, n) for n in NOTICES + READMES if (ROOT / n).is_file()]
    archives = []
    for portable in (False, True):
        name = f"vaultic-{version}-{system}-{arch}{'-portable' if portable else ''}.zip"
        path = out / name
        make_zip(path, folder, files, portable)
        verify_zip(path, portable, exe_name)
        archives.append(path)

    (out / "SHA256SUMS.txt").write_text("".join(f"{sha256(p)}  {p.name}\n" for p in archives), encoding="utf-8")
    print(f"Vaultic {version} ({system}/{arch})")
    for p in archives:
        print(f"  {p.name}  {p.stat().st_size / 1e6:.1f} MB")
    print(f"  checksums: {out / 'SHA256SUMS.txt'}")


if __name__ == "__main__":
    main()
