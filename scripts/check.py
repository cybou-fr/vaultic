#!/usr/bin/env python3
"""One local command that checks Vaultic before a commit. NO CI.

    py scripts/check.py            format, vet, tests, race detector, build
    py scripts/check.py --quick    format, vet and a fast subset of the tests
    py scripts/check.py --no-race  everything except the race detector
    py scripts/check.py --no-build everything except building dist/

Formatting is checked on the file content with line endings normalised, so a
Windows checkout with CRLF endings (core.autocrlf) is judged like the commit
that Git will store. Only Vaultic's own Go files are checked; the restic code
that Vaultic embeds is left as it is upstream.
"""
import argparse
import glob
import os
import shutil
import subprocess
import sys
import time
from pathlib import Path

ROOT = Path(__file__).resolve().parent.parent

# Vaultic's own sources: everything named vaultic_*.go plus the entry point.
OWN_GLOBS = ("cmd/restic/vaultic_*.go", "cmd/restic/main.go")

# A subset that runs in well under a minute (no real repositories).
QUICK_TESTS = "^TestVaultic(Schedule|Stale|Retention(Validation|Only|Never|Buckets)|Confirm|Parse|Secret|Autostart|Background|Tray|WindowCloser|Settings|Coordinator)"


def find_gcc_dirs():
    """Directories that hold a C compiler on Windows (Fyne needs cgo)."""
    if sys.platform != "win32" or shutil.which("gcc"):
        return []
    patterns = (r"C:\Qt\Tools\mingw*\bin", r"C:\msys64\ucrt64\bin", r"C:\msys64\mingw64\bin", r"C:\mingw64\bin")
    return [p for pattern in patterns for p in sorted(glob.glob(pattern)) if (Path(p) / "gcc.exe").exists()][:1]


def environment():
    env = dict(os.environ)
    extra = find_gcc_dirs()
    if extra:
        env["PATH"] = os.pathsep.join(extra + [env.get("PATH", "")])
    return env


def run(name, args, env, results):
    print(f"\n== {name}\n+ {' '.join(args)}", flush=True)
    start = time.time()
    code = subprocess.run(args, cwd=ROOT, env=env).returncode
    results.append((name, code == 0, time.time() - start))
    return code == 0


def check_format(env, results):
    """gofmt on normalised content of Vaultic's own files."""
    print("\n== gofmt (line endings normalised)", flush=True)
    start = time.time()
    files = sorted({Path(f) for pattern in OWN_GLOBS for f in glob.glob(str(ROOT / pattern))})
    bad = []
    for file in files:
        raw = Path(file).read_bytes().replace(b"\r\n", b"\n")
        proc = subprocess.run(["gofmt", "-l"], input=raw, capture_output=True, env=env)
        if proc.returncode != 0:
            print(proc.stderr.decode(errors="replace"))
            bad.append(file)
        elif proc.stdout.strip():
            bad.append(file)
    for file in bad:
        print("not formatted:", file.relative_to(ROOT).as_posix())
    print(f"{len(files)} files checked, {len(bad)} to format (run: gofmt -w <file>)")
    results.append(("gofmt", not bad, time.time() - start))
    return not bad


def main():
    parser = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    parser.add_argument("--quick", action="store_true", help="format, vet and a fast subset of the tests")
    parser.add_argument("--no-race", action="store_true", help="skip the race detector")
    parser.add_argument("--no-build", action="store_true", help="skip building dist/")
    args = parser.parse_args()

    env = environment()
    results = []
    check_format(env, results)
    run("go vet", ["go", "vet", "./cmd/restic"], env, results)
    if args.quick:
        run("tests (quick subset)", ["go", "test", "./cmd/restic", "-run", QUICK_TESTS, "-count=1"], env, results)
    else:
        run("tests", ["go", "test", "./cmd/restic", "-run", "^TestVaultic", "-count=1"], env, results)
        if not args.no_race:
            run("race detector", ["go", "test", "-race", "./cmd/restic", "-run", "^TestVaultic", "-count=1"], env, results)
        if not args.no_build:
            run("build", [sys.executable, "scripts/build.py"], env, results)

    print("\n== summary")
    for name, ok, seconds in results:
        print(f"{'ok  ' if ok else 'FAIL'} {name:<24} {seconds:6.1f}s")
    failed = [name for name, ok, _ in results if not ok]
    if failed:
        print("\nFailed:", ", ".join(failed))
        return 1
    print("\nAll checks passed.")
    return 0


if __name__ == "__main__":
    sys.exit(main())
