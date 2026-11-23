#!/usr/bin/env python3
"""Package or validate the optimized Elixir agreement build for this CI run."""

import argparse
import hashlib
import json
import os
import re
import shutil
import subprocess
import tempfile
from pathlib import Path

SOURCE = Path("build/tables-generated-elixir")
STRICT_COMPILE = Path("make/elixir-strict.sh")
DRIVER = Path("test/conformance/elixir/driver_impl.ex")
MODULE = re.compile(rb"^defmodule ([A-Za-z0-9_.]+) do$", re.MULTILINE)
COMPILER_ENV = ("ELIXIR_ERL_OPTIONS", "ERL_COMPILER_OPTIONS", "ERL_FLAGS", "ERL_AFLAGS", "ERL_ZFLAGS")


def require(condition, reason):
    if not condition:
        raise ValueError(reason)


def digest(path):
    require(path.is_file() and not path.is_symlink(), f"{path} is not a regular file")
    return hashlib.sha256(path.read_bytes()).hexdigest()


def identity():
    for name in COMPILER_ENV:
        require(not os.getenv(name), f"unexpected compile override {name}")
    banner = subprocess.check_output(["elixir", "--version"], text=True)
    toolchain = re.findall(r"^Erlang/OTP \d+ \[erts-[^\]]+\]|^Elixir .+$", banner, re.MULTILINE)
    require(len(toolchain) == 2, "could not identify OTP, ERTS and Elixir")
    return {
        "tree": subprocess.check_output(["git", "rev-parse", "HEAD^{tree}"], text=True).strip(),
        "run": os.environ["GITHUB_RUN_ID"],
        "attempt": os.environ["GITHUB_RUN_ATTEMPT"],
        "toolchain": toolchain,
        "strict_compile": digest(STRICT_COMPILE),
    }


def source_files(units):
    for unit in units:
        require(re.fullmatch(r"[A-Za-z0-9][A-Za-z0-9_-]*", unit), f"invalid unit name {unit!r}")
        files = sorted((SOURCE / unit).glob("*.ex"))
        require(files, f"no generated Elixir sources for {unit}")
        yield from files


def beam_modules(source):
    return [f"Elixir.{name.decode()}.beam" for name in MODULE.findall(source.read_bytes())]


def check_directory(directory, names):
    require(directory.is_dir() and not directory.is_symlink(), f"missing directory {directory}")
    actual = {file.name for file in directory.iterdir()}
    expected = set(names)
    require(actual == expected, f"{directory}: missing {sorted(expected - actual)}, extra {sorted(actual - expected)}")


def expected_metadata(identity_info, units, shard):
    files = list(source_files(units))
    modules = [module for path in files for module in beam_modules(path)]
    require(modules and len(modules) == len(set(modules)), f"shard {shard}: empty or overlapping module roster")
    return {
        **identity_info,
        "shard": shard,
        "inputs": {str(path): digest(path) for path in files},
    }, modules


def stage_shard(directory, ebin, identity_info, units, shard):
    metadata, modules = expected_metadata(identity_info, units, shard)
    check_directory(ebin, modules)
    directory.mkdir(parents=True)
    out = directory / "ebin"
    out.mkdir()
    for module in modules:
        shutil.copyfile(ebin / module, out / module, follow_symlinks=False)
    metadata["outputs"] = {module: digest(out / module) for module in modules}
    (directory / "manifest.json").write_text(json.dumps(metadata, sort_keys=True) + "\n")


def assemble(directory, ebin, identity_info, units, shard_count):
    names = [f"elixir-agreement-{number}" for number in range(1, shard_count + 1)]
    check_directory(directory, names)
    ebin.parent.mkdir(parents=True, exist_ok=True)
    require(not ebin.exists() and not ebin.is_symlink(), f"{ebin} already exists")

    with tempfile.TemporaryDirectory(prefix=".elixir-handoff-", dir=ebin.parent) as temp:
        partial = Path(temp) / "ebin"
        partial.mkdir()
        seen = set()
        for index, name in enumerate(names):
            shard = directory / name
            check_directory(shard, ["manifest.json", "ebin"])
            manifest = shard / "manifest.json"
            require(manifest.is_file() and not manifest.is_symlink(), f"{manifest} is not a regular file")
            metadata, modules = expected_metadata(identity_info, units[index::shard_count], index + 1)
            check_directory(shard / "ebin", modules)
            require(seen.isdisjoint(modules), f"{name}: module appears in multiple shards")
            for module in modules:
                shutil.copyfile(shard / "ebin" / module, partial / module, follow_symlinks=False)
            metadata["outputs"] = {module: digest(partial / module) for module in modules}
            receipt = json.loads(manifest.read_text())
            require(isinstance(receipt, dict), f"{name}: malformed receipt")
            attempt = receipt.get("attempt")
            require(
                isinstance(attempt, str) and re.fullmatch(r"[1-9][0-9]*", attempt)
                and int(attempt) <= int(metadata["attempt"]),
                f"{name}: invalid producer attempt {attempt!r}",
            )
            # A failed-jobs-only rerun retains successful producer jobs. All
            # their identity fields except the attempt must still match.
            metadata["attempt"] = attempt
            require(receipt == metadata, f"{name}: source, build or run identity differs")
            seen.update(modules)
        require(seen.isdisjoint(beam_modules(DRIVER)), "a generated module overlaps the local driver")
        partial.rename(ebin)
    print(f"Elixir handoff: verified {len(seen)} modules from {shard_count} agreement shards")


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("mode", choices=["give", "take"])
    parser.add_argument("directory", type=Path)
    parser.add_argument("ebin", type=Path)
    parser.add_argument("shard_count", type=int)
    parser.add_argument("units", nargs="+")
    parser.add_argument("--shard", type=int)
    args = parser.parse_args()
    try:
        require(args.units == sorted(set(args.units)), "unit roster must be sorted and unique")
        require(0 < args.shard_count <= len(args.units), "invalid shard count")
        identity_info = identity()
        if args.mode == "give":
            require(args.shard is not None and 0 < args.shard <= args.shard_count, "invalid producer shard")
            stage_shard(args.directory, args.ebin, identity_info, args.units[args.shard - 1::args.shard_count], args.shard)
        else:
            require(args.shard is None, "take requires the whole shard set")
            assemble(args.directory, args.ebin, identity_info, args.units, args.shard_count)
    except (OSError, ValueError, KeyError, subprocess.CalledProcessError) as exc:
        parser.exit(1, f"Elixir handoff: {exc}\n")


if __name__ == "__main__":
    main()
