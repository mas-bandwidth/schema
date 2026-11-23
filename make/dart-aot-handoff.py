"""Package or verify a same-run Dart conformance build from the checkout root."""

import argparse
import hashlib
import json
import os
from pathlib import Path
import shutil
import subprocess
import tarfile
import tempfile


BUILD_PATHS = tuple(
    Path("build") / name
    for name in (
        "conformance-dart",
        "tables-generated-dart",
        "tables-generated-dart-framing",
        "tables-generated-dart-scalars",
    )
)


def command(*args):
    return subprocess.check_output(args, stderr=subprocess.STDOUT, text=True).strip()


def fingerprint(paths, root):
    result = {}
    for path in paths:
        if not path.exists():
            raise ValueError(f"missing input: {path}")
        for member in path.rglob("*") if path.is_dir() else (path,):
            if member.is_symlink():
                raise ValueError(f"unexpected symlink: {member}")
            if member.is_file() and member.name != ".stamp":
                with member.open("rb") as stream:
                    result[str(member.relative_to(root))] = hashlib.file_digest(stream, "sha256").hexdigest()
    return result


def identity(dart):
    executable = shutil.which(dart)
    if executable is None:
        raise ValueError(f"Dart executable is unavailable: {dart}")
    sdk = Path(executable).resolve().parent.parent
    if command("git", "status", "--porcelain", "--untracked-files=no"):
        raise ValueError("tracked Dart handoff checkout must be clean")
    run_id = os.environ.get("GITHUB_RUN_ID")
    if not run_id or not run_id.isdecimal():
        raise ValueError("GITHUB_RUN_ID must name the workflow run")
    return {
        "run_id": run_id,
        "tree": command("git", "rev-parse", "HEAD^{tree}"),
        "sdk_version": command(dart, "--version"),
        "sdk_inputs": fingerprint((sdk / "bin", sdk / "lib", sdk / "version"), sdk),
        "host": [command("uname", "-sm"), command("getconf", "GNU_LIBC_VERSION")],
        "go": command("go", "version"),
    }


def produce(archive, dart, verify_packets):
    if any(path.exists() or path.is_symlink() for path in (*BUILD_PATHS, Path("bin/schema"))):
        raise ValueError("Dart handoff producer requires a fresh compiler, generated tree, and executable")
    info = identity(dart)
    targets = ["build/conformance-dart"]
    if verify_packets:
        targets.append("_verify-dart-packets")
    subprocess.run(["make", f"-j{command('nproc')}", *targets, f"DART={dart}"], check=True)
    if info != identity(dart):
        raise ValueError("producer identity changed during the Dart build")
    with tempfile.TemporaryDirectory(dir=archive.parent) as tmp:
        directory = Path(tmp)
        for path in BUILD_PATHS:
            destination = directory / path
            destination.parent.mkdir(exist_ok=True)
            if path.is_dir():
                shutil.copytree(path, destination, symlinks=True, ignore=shutil.ignore_patterns(".stamp"))
            else:
                shutil.copy2(path, destination)
        info["outputs"] = fingerprint((directory / path for path in BUILD_PATHS), directory)
        (directory / "receipt.json").write_text(json.dumps(info, sort_keys=True) + "\n")
        temporary = directory / "handoff.tar.gz"
        with tarfile.open(temporary, "w:gz", compresslevel=1) as bundle:
            bundle.add(directory / "receipt.json", arcname="receipt.json")
            for path in BUILD_PATHS:
                bundle.add(directory / path, arcname=str(path))
        temporary.replace(archive)


def unpack(archive, directory):
    root = directory.resolve()
    with tarfile.open(archive, "r:gz") as bundle:
        seen = set()
        for member in bundle:
            path = Path(member.name)
            allowed = path == Path("receipt.json") or any(
                path == base or base in path.parents for base in BUILD_PATHS
            )
            if path.is_absolute() or ".." in path.parts or not allowed or path in seen or path.name == ".stamp":
                raise ValueError(f"unexpected artifact path: {path}")
            seen.add(path)
            target = root / path
            if member.isdir():
                target.mkdir(parents=True, exist_ok=True)
            elif member.isfile():
                target.parent.mkdir(parents=True, exist_ok=True)
                with bundle.extractfile(member) as stream, target.open("xb") as output:
                    shutil.copyfileobj(stream, output)
                target.chmod(member.mode & 0o777)
            else:
                raise ValueError(f"unsupported artifact member type: {path}")


def consume(archive, dart):
    if any(path.exists() or path.is_symlink() for path in BUILD_PATHS):
        raise ValueError("Dart handoff destination is not empty")
    expected = identity(dart)
    Path("build").mkdir(exist_ok=True)
    with tempfile.TemporaryDirectory(dir="build") as tmp:
        directory = Path(tmp)
        unpack(archive, directory)
        receipt = json.loads((directory / "receipt.json").read_text())
        outputs = receipt.pop("outputs")
        if receipt != expected:
            raise ValueError("Dart handoff producer run, checkout, SDK, or runtime does not match")
        if outputs != fingerprint((directory / path for path in BUILD_PATHS), directory):
            raise ValueError("Dart handoff output digest or inventory does not match")
        if not os.access(directory / BUILD_PATHS[0], os.X_OK):
            raise ValueError("Dart handoff executable has lost its execute permission")
        for path in BUILD_PATHS:
            (directory / path).replace(path)


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("operation", choices=("produce", "consume"))
    parser.add_argument("artifact", type=Path)
    parser.add_argument("dart", default="dart", nargs="?")
    parser.add_argument("--verify-packets", action="store_true", help="also run the producer’s ordinary packet checks")
    args = parser.parse_args()
    try:
        if args.operation == "produce":
            produce(args.artifact, args.dart, args.verify_packets)
        elif args.verify_packets:
            parser.error("--verify-packets applies only to produce")
        else:
            consume(args.artifact, args.dart)
    except (OSError, ValueError, KeyError, tarfile.TarError, subprocess.CalledProcessError) as exc:
        parser.exit(1, f"Dart AOT handoff: {exc}\n")
