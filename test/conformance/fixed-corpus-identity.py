#!/usr/bin/env python3
"""Keep ordinary nightly consumers on the downloaded C++ fixed-corpus bytes."""

import argparse
import hashlib
import json
from pathlib import Path
import sys


CORPORA = ("fixedform-corpus", "fixedform-bench-corpus")
REQUIRED = (
    "fixedform-corpus/manifest.txt",
    "fixedform-bench-corpus/bench_fixed.bin",
    "fixedform-bench-corpus/bench_fixed.oracle.json",
)


def inventory(root):
    files = {}
    for corpus in CORPORA:
        directory = root / corpus
        if not directory.is_dir():
            raise ValueError(f"missing shared directory: {corpus}")
        for path in sorted(directory.rglob("*")):
            relative = path.relative_to(root).as_posix()
            # Make's stamps and this compiled producer are not corpus inputs.
            if relative in (f"{corpus}/.stamp", "fixedform-bench-corpus/corpus"):
                continue
            if path.is_symlink():
                raise ValueError(f"shared input is a symlink: {relative}")
            if path.is_dir():
                continue
            if not path.is_file():
                raise ValueError(f"shared input is not a file: {relative}")
            content = path.read_bytes()
            files[relative] = {"sha256": hashlib.sha256(content).hexdigest(), "size": len(content)}
    for name in REQUIRED:
        if name not in files or files[name]["size"] == 0:
            raise ValueError(f"missing or empty shared input: {name}")
    if not any(name.startswith("fixedform-corpus/") and name.endswith(".bin") for name in files):
        raise ValueError("shared fixedform-corpus has no binary inputs")
    return files


def compare_inventory(expected, actual, label):
    missing = sorted(expected.keys() - actual.keys())
    extra = sorted(actual.keys() - expected.keys())
    changed = sorted(name for name in expected.keys() & actual.keys() if expected[name] != actual[name])
    if missing or extra or changed:
        raise ValueError(f"{label} identity mismatch: missing={missing}, extra={extra}, changed={changed}")


def save(receipt, record):
    receipt.parent.mkdir(parents=True, exist_ok=True)
    receipt.write_text(json.dumps(record, sort_keys=True, indent=2) + "\n")


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("mode", choices=("snapshot", "verify"))
    parser.add_argument("reference", type=Path)
    parser.add_argument("receipt", type=Path)
    parser.add_argument("--consumer", type=Path)
    parser.add_argument("--phase", default="overlay")
    args = parser.parse_args()
    record = {"version": 1, "files": {}, "checks": [], "status": "fail"}
    try:
        if args.mode == "snapshot":
            record["files"] = inventory(args.reference)
            record["status"] = "snapshot"
        else:
            if args.consumer is None:
                raise ValueError("verify requires --consumer")
            record = json.loads(args.receipt.read_text())
            if record.get("version") != 1 or not record.get("files"):
                raise ValueError("missing shared corpus snapshot")
            expected = record["files"]
            compare_inventory(expected, inventory(args.reference), "downloaded reference")
            compare_inventory(expected, inventory(args.consumer), "consumer")
            for name in expected:
                if (args.reference / name).read_bytes() != (args.consumer / name).read_bytes():
                    raise ValueError(f"consumer bytes differ: {name}")
            if any(check["status"] == "fail" for check in record["checks"]):
                raise ValueError("an earlier shared corpus identity check failed")
            record["checks"].append({"phase": args.phase, "status": "pass"})
            record["status"] = "pass"
        save(args.receipt, record)
        print(f"fixed corpus identity: {args.mode} {args.phase} ({len(record['files'])} files)")
        return 0
    except (OSError, ValueError) as error:
        record["checks"].append({"phase": args.phase, "status": "fail", "error": str(error)})
        record["status"] = "fail"
        save(args.receipt, record)
        print(f"fixed corpus identity: {error}", file=sys.stderr)
        return 1


if __name__ == "__main__":
    sys.exit(main())
