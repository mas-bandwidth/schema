"""Verify this recorded pass and its corpus; never run a benchmark."""

import argparse
import csv
import datetime
import hashlib
import json
import math
from pathlib import Path
import statistics
import subprocess


def require(condition, message):
    if not condition:
        raise SystemExit(message)


def sha(path):
    return hashlib.sha256(path.read_bytes()).hexdigest()


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--binary-root", type=Path)
    args = parser.parse_args()
    here = Path(__file__).resolve().parent
    root = here.parents[3]
    metadata = json.loads((here / "metadata.json").read_text())
    provenance = json.loads((here / "provenance.json").read_text())
    summary = json.loads((here / "summary.json").read_text())
    record_bytes = provenance["corpus"]["bytes_per_record"]
    record_count = provenance["corpus"]["records"]
    for name, expected in provenance["evidence_files_sha256"].items():
        require(sha(here / name) == expected, f"changed evidence: {name}")

    variants = ("before", "hash")
    expected_files = {f"round-{i}-{v}.csv" for i in range(7) for v in variants}
    require({p.name for p in here.glob("round-*.csv")} == expected_files,
            "missing or unexpected round files")
    require(metadata["rounds"] == 7 and metadata["iterations"] == 400000,
            "changed pass dimensions")
    identities = {
        "lang": "cs", "bench": "bench_table", "iters": "400000",
        "bytes_per_op": str(record_bytes), "runs": "1", "spread_pct": "0.00",
        "corpus_id": "b51387f36d9b59c4", "family": "table", "linkage": "asm",
        "checks": "contract", "opt": "default", "inline": "unknown",
    }
    rows = []
    for i in range(7):
        for variant in variants:
            name = f"round-{i}-{variant}.csv"
            with (here / name).open(newline="") as stream:
                data = list(csv.DictReader(stream))
            require(len(data) == 2 and [r["path"] for r in data] ==
                    ["write", "round_trip"], f"incomplete operations: {name}")
            for row in data:
                require(len(row) == 17 and all(row[k] == v for k, v in identities.items()),
                        f"changed identity: {name}")
                rate = float(row["median_msgs_per_sec"])
                require(math.isfinite(rate) and rate > 0 and rate ==
                        float(row["min_msgs_per_sec"]) == float(row["max_msgs_per_sec"]),
                        f"invalid single-sample rate: {name}")
                require(400000 / rate >= 0.2, f"sample under 200 ms: {name}")
                require(abs(float(row["median_mb_per_sec"]) - rate * record_bytes / 1024**2)
                        < 0.011, f"byte rate mismatch: {name}")
                rows.append(dict(row, variant=variant, round=i))
    require(len(rows) == 28, "expected 28 data rows")

    for operation in ("write", "round_trip"):
        for variant in variants:
            costs = [1e6 / float(r["median_msgs_per_sec"]) for r in rows
                     if r["path"] == operation and r["variant"] == variant]
            median = statistics.median(costs)
            metrics = {"best_us": min(costs), "median_us": median,
                       "spread_pct": 100 * (max(costs) - min(costs)) / median}
            for key, value in metrics.items():
                require(math.isclose(value, summary[operation][variant][key],
                                     rel_tol=1e-12, abs_tol=1e-12),
                        f"aggregate mismatch: {operation}/{variant}/{key}")
            require(metrics["spread_pct"] <= 15, "spread exceeds acceptance bound")
        ratio = (100 * summary[operation]["hash"]["median_us"] /
                 summary[operation]["before"]["median_us"])
        require(math.isclose(ratio, summary[operation]["candidate_cost_pct"], rel_tol=1e-12),
                f"ratio mismatch: {operation}")

    load = json.loads((here / "load.json").read_text())
    expected_order = [(i, v) for i in range(7) for v in
                      (variants if i % 2 == 0 else variants[::-1])]
    require([(r["round"], r["variant"]) for r in load] == expected_order,
            "pair order mismatch")
    timestamps = [metadata["start"]] + [r["time"] for r in load] + [metadata["end"]]
    times = [datetime.datetime.fromisoformat(s) for s in timestamps]
    require(times == sorted(times) and len(set(times)) == len(times),
            "invalid measurement chronology")

    # Read the measured revisions, so a later runner change cannot rewrite this evidence.
    def recorded(revision, path):
        return subprocess.check_output(["git", "show", f"{revision}:{path}"], cwd=root)

    for path, expected in provenance["unchanged_inputs_sha256"].items():
        for revision in (metadata["before_revision"], metadata["candidate_revision"]):
            require(hashlib.sha256(recorded(revision, path)).hexdigest() == expected,
                    f"input differs between measured revisions: {path}")
    for variant, key in (("before", "before_revision"), ("hash", "candidate_revision")):
        generated = recorded(metadata[key], "generated/bench/tables/cs/BenchTableTable.cs")
        require(hashlib.sha256(generated).hexdigest() ==
                provenance["generated_table_source_sha256"][variant],
                f"generated source hash mismatch: {variant}")
    golden = recorded(metadata["before_revision"], "testdata/wire/bench_table.bin")
    packed = recorded(metadata["before_revision"], "bench/corpus/variants/bench_table.variants.bin")
    require(len(golden) == record_bytes and len(packed) == record_count * record_bytes and
            packed[:record_bytes] == golden,
            "corpus dimensions or golden mismatch")
    require(len({packed[i * record_bytes:(i + 1) * record_bytes]
                 for i in range(record_count)}) == record_count,
            "corpus records are not distinct")
    corpus = {"bench_table.bin": golden, "bench_table.variants.bin": packed}
    value = 0xcbf29ce484222325
    for name, data in sorted(corpus.items()):
        for byte in name.encode("ascii") + b"\0" + data:
            value = ((value ^ byte) * 0x100000001b3) & 0xffffffffffffffff
    require(f"{value:016x}" == identities["corpus_id"], "full corpus ID mismatch")

    for variant in variants:
        files = provenance["binary_files_sha256"][variant]
        require(files["schematablesbench.dll"] == metadata["binaries"][variant],
                f"recorded DLL hash mismatch: {variant}")
        if args.binary_root is not None:
            for name, expected in files.items():
                require(sha(args.binary_root / f"csharp-{variant}" / name) == expected,
                        f"binary hash mismatch: {variant}/{name}")
    print("PASS: 28 rows, seven alternating pairs, identity, corpus, durations, spreads and aggregates")
    if args.binary_root is not None:
        print("PASS: both frozen DLLs and their dependency/runtime configuration hashes")
    else:
        print("Frozen binaries not supplied; recorded hashes cross-checked only")


if __name__ == "__main__":
    main()
