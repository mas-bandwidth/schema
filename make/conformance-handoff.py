"""Package or verify a same-run conformance build from the checkout root."""

import argparse
import hashlib
import json
import os
from pathlib import Path
import shutil
import subprocess
import tarfile
import tempfile


DART_PATHS = tuple(
    Path("build") / name
    for name in (
        "conformance-dart",
        "tables-generated-dart",
        "tables-generated-dart-framing",
        "tables-generated-dart-scalars",
    )
)


CPP_PATHS = (Path("build/conformance-cpp"), Path("build/schema_test_cook"))
CPP_BUILD_TREE = (Path("build/tables-generated"), Path("build/conformance-cpp-objs"))


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


def runtime_digest(name):
    path = Path(command("c++", f"-print-file-name={name}"))
    with path.open("rb") as stream:
        return hashlib.file_digest(stream, "sha256").hexdigest()


def identity(leg, dart):
    if command("git", "status", "--porcelain", "--untracked-files=no"):
        raise ValueError("tracked conformance handoff checkout must be clean")
    run_id = os.environ.get("GITHUB_RUN_ID")
    if not run_id or not run_id.isdecimal():
        raise ValueError("GITHUB_RUN_ID must name the workflow run")
    common = {
        "run_id": run_id,
        "tree": command("git", "rev-parse", "HEAD^{tree}"),
        "host": [command("uname", "-sm"), command("getconf", "GNU_LIBC_VERSION")],
        "go": command("go", "version"),
    }
    if leg == "cpp":
        sibling = Path("../serialize")
        if command("git", "-C", str(sibling), "status", "--porcelain", "--untracked-files=no"):
            raise ValueError("tracked C++ runtime checkout must be clean")
        common.update(
            leg=leg,
            compiler=command("c++", "--version"),
            runtime_tree=command("git", "-C", str(sibling), "rev-parse", "HEAD^{tree}"),
            runtime_headers=fingerprint(sorted(sibling.glob("*.h")), sibling),
            linker_runtime=[
                runtime_digest(name)
                for name in ("libstdc++.so.6", "libgcc_s.so.1")
            ],
        )
    else:
        executable = shutil.which(dart)
        if executable is None:
            raise ValueError(f"Dart executable is unavailable: {dart}")
        sdk = Path(executable).resolve().parent.parent
        common.update(
            sdk_version=command(dart, "--version"),
            sdk_inputs=fingerprint((sdk / "bin", sdk / "lib", sdk / "version"), sdk),
        )
    return common


def produce(archive, leg, dart, verify_packets, prebuilt_compiler):
    paths = CPP_PATHS if leg == "cpp" else DART_PATHS
    scratch = CPP_BUILD_TREE if leg == "cpp" else ()
    if any(path.exists() or path.is_symlink() for path in (*paths, *scratch)):
        raise ValueError("conformance handoff producer requires a fresh generated tree and executable")
    compiler = Path("bin/schema")
    if prebuilt_compiler:
        if leg != "cpp":
            raise ValueError("a prebuilt compiler applies only to the C++ handoff")
        if not compiler.is_file() or not os.access(compiler, os.X_OK):
            raise ValueError("prebuilt compiler is missing or not executable")
    elif compiler.exists() or compiler.is_symlink():
        raise ValueError("conformance handoff producer requires a fresh compiler")
    info = identity(leg, dart)
    targets = list(map(str, paths)) if leg == "cpp" else ["build/conformance-dart"]
    if verify_packets:
        targets.append("_verify-dart-packets")
    variables = ["CXX=c++"] if leg == "cpp" else [f"DART={dart}"]
    jobs = min(4, int(command("nproc"))) if leg == "cpp" else int(command("nproc"))
    subprocess.run(["make", f"-j{jobs}", *targets, *variables], check=True)
    if info != identity(leg, dart):
        raise ValueError("producer identity changed during the conformance build")
    with tempfile.TemporaryDirectory(dir=archive.parent) as tmp:
        directory = Path(tmp)
        for path in paths:
            destination = directory / path
            destination.parent.mkdir(exist_ok=True)
            if path.is_dir():
                shutil.copytree(path, destination, symlinks=True, ignore=shutil.ignore_patterns(".stamp"))
            else:
                shutil.copy2(path, destination)
        info["outputs"] = fingerprint((directory / path for path in paths), directory)
        (directory / "receipt.json").write_text(json.dumps(info, sort_keys=True) + "\n")
        temporary = directory / "handoff.tar.gz"
        with tarfile.open(temporary, "w:gz", compresslevel=1) as bundle:
            bundle.add(directory / "receipt.json", arcname="receipt.json")
            for path in paths:
                bundle.add(directory / path, arcname=str(path))
        temporary.replace(archive)


def unpack(archive, directory, paths):
    root = directory.resolve()
    with tarfile.open(archive, "r:gz") as bundle:
        seen = set()
        for member in bundle:
            path = Path(member.name)
            allowed = path == Path("receipt.json") or any(
                path == base or base in path.parents for base in paths
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


def consume(archive, leg, dart):
    paths = CPP_PATHS if leg == "cpp" else DART_PATHS
    if any(path.exists() or path.is_symlink() for path in paths):
        raise ValueError("conformance handoff destination is not empty")
    expected = identity(leg, dart)
    Path("build").mkdir(exist_ok=True)
    with tempfile.TemporaryDirectory(dir="build") as tmp:
        directory = Path(tmp)
        unpack(archive, directory, paths)
        receipt = json.loads((directory / "receipt.json").read_text())
        outputs = receipt.pop("outputs")
        if receipt != expected:
            raise ValueError("conformance handoff producer run, checkout, SDK, or runtime does not match")
        if outputs != fingerprint((directory / path for path in paths), directory):
            raise ValueError("conformance handoff output digest or inventory does not match")
        for executable in CPP_PATHS if leg == "cpp" else DART_PATHS[:1]:
            if not os.access(directory / executable, os.X_OK):
                raise ValueError(f"handoff executable has lost its execute permission: {executable}")
        for path in paths:
            (directory / path).replace(path)


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("operation", choices=("produce", "consume"))
    parser.add_argument("artifact", type=Path)
    parser.add_argument("dart", default="dart", nargs="?")
    parser.add_argument("--leg", choices=("dart", "cpp"), default="dart")
    parser.add_argument("--verify-packets", action="store_true", help="also run the producer’s ordinary packet checks")
    parser.add_argument("--prebuilt-compiler", action="store_true", help="use a separately verified compiler restored at bin/schema (C++ only)")
    args = parser.parse_args()
    try:
        if args.verify_packets and args.leg != "dart":
            parser.error("--verify-packets applies only to Dart")
        if args.prebuilt_compiler and (args.operation != "produce" or args.leg != "cpp"):
            parser.error("--prebuilt-compiler applies only to C++ produce")
        if args.operation == "produce":
            produce(args.artifact, args.leg, args.dart, args.verify_packets, args.prebuilt_compiler)
        elif args.verify_packets:
            parser.error("--verify-packets applies only to produce")
        else:
            consume(args.artifact, args.leg, args.dart)
    except (OSError, ValueError, KeyError, tarfile.TarError, subprocess.CalledProcessError) as exc:
        parser.exit(1, f"conformance handoff: {exc}\n")
