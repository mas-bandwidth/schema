"""Golden-gated Python packet bench over the committed 64-record corpus."""
import argparse
import os
import statistics
import sys
import time

sys.path.insert(0, os.path.abspath("generated/bench/python"))
from bench import BenchMixed


def corpus_id(files):
    h = 0xCBF29CE484222325
    for name in sorted(files):
        for b in name.encode() + b"\0" + files[name]:
            h = ((h ^ b) * 0x100000001B3) & 0xFFFFFFFFFFFFFFFF
    return f"{h:016x}"


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--csv", action="store_true")
    parser.add_argument("--gate", action="store_true")
    parser.add_argument("--quick", action="store_true")
    parser.add_argument("--round", type=int)
    parser.add_argument("--iterations", type=int)
    parser.add_argument("--wire-dir", default="testdata/wire")
    parser.add_argument("--variant-dir", default="bench/corpus/variants")
    args = parser.parse_args()
    if args.round is not None and args.round < 0:
        parser.error("--round must be nonnegative")
    iters = args.iterations if args.iterations is not None else (1280 if args.quick else 12800)
    if iters <= 0 or iters > 2147483584 or iters % 64:
        parser.error("--iterations must be a positive multiple of 64 up to 2147483584")
    with open(os.path.join(args.wire_dir, "bench_mixed.bin"), "rb") as f:
        golden = f.read()
    with open(os.path.join(args.variant_dir, "bench_mixed.variants.bin"), "rb") as f:
        packed = f.read()
    if not golden or not packed or len(packed) % 64:
        raise ValueError("invalid variant corpus")
    size = len(packed) // 64
    variants = [packed[i * size : (i + 1) * size] for i in range(64)]
    if variants[0] != golden:
        raise ValueError("variant 0 differs from the C++ pin")
    values = [BenchMixed.read(data) for data in variants]
    for i, value in enumerate(values):
        if value.write() != variants[i]:
            raise ValueError(f"variant {i} round-trip differs")
    identifier = corpus_id({"bench_mixed.bin": golden, "bench_mixed.variants.bin": packed})
    if args.gate:
        print("OK (gate only, corpus_id " + identifier + ")", file=sys.stderr)
        return
    runs = 1 if args.round is not None else (3 if args.quick else 7)
    rows = []
    sink = 0
    for path in ("write", "round_trip"):
        rates = []
        for run in range(runs + 1):
            start = time.perf_counter()
            for i in range(iters):
                idx = i % 64
                value = values[idx] if path == "write" else BenchMixed.read(variants[idx])
                sink += len(value.write())
            elapsed = time.perf_counter() - start
            if run:
                rates.append(iters / elapsed)
        rate = statistics.median(rates)
        rows.append(f"python,bench_mixed,{path},{iters},{size},{runs},{rate:.0f},{min(rates):.0f},{max(rates):.0f},{rate*size/1048576:.2f},{(max(rates)-min(rates))/rate*100:.2f},{identifier},gen,pkg,always,default,unknown")
    if args.csv:
        print("lang,bench,path,iters,bytes_per_op,runs,median_msgs_per_sec,min_msgs_per_sec,max_msgs_per_sec,median_mb_per_sec,spread_pct,corpus_id,family,linkage,checks,opt,inline")
        print("\n".join(rows))
    else:
        print("\n".join(rows), file=sys.stderr)
    print("sink:", sink, file=sys.stderr)


if __name__ == "__main__":
    main()
