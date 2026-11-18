"""The tables bench: the Python runner.

Measures ONE thing: a representative table written and read on the TOLERANT
WIRE (docs/SPEC-TABLES.md section 3, form 1), through the generated package
and the runtime it ships beside it (docs/PORT-PYTHON.md). It is the Lua
runner's sibling (bench/tables/lua/table_main.lua) and keeps the same contract
(bench/BENCH-STANDARD.md): the committed variant corpus drives it, the golden
gate runs before the clock, a failing gate emits NO rows, and the report is 1
warmup + 7 measured runs with the median beside min/max/spread.

A fixed class in Python reads and writes only the fixed form (form 3), so
bench/tables/python/leg generates the corpus with its tables declared
variable: the form-1 bytes are the same, and gate 2 below proves it on all 64
records before any clock starts.

THIS FILE IS SHAPE-BLIND. It names the generated root once, in ROOT below, and
nothing else: no field, no pinned value, no wire size.
bench/tables/python/SHAPE-GATE.allow carries the exact count.

THE CLOCK. time.process_time is CPU time, which is what a single-threaded
interpreter loop spends. Timing lives here, in the sanctioned bench, and in no
unit test.

Output: a human table on stderr; with --csv, CSV v2 rows on stdout.
Usage (from the repository root, under a pinned interpreter):
  python -I -B bench/tables/python/table_main.py <generated dir> [--indexed]
      [--gate] [--csv] [--round K] [--iterations N] [--wire-dir D]
      [--variant-dir D]
"""

import importlib.util
import os
import platform
import struct
import sys
import time

MAX_RUNS = 7  # median of 7 (N >= 5), after 1 warmup run
NUM_VARIANTS = 64  # an OP is one record
DEFAULT_ITERATIONS = 640  # a multiple of 64; one round trip is milliseconds here
ROOT = "TableMixed"  # the one measured shape, named once

failed = False
goldens: dict[str, bytes] = {}
csv_rows: list[str] = []


def fail(name: str, what: str) -> None:
    global failed
    sys.stderr.write("FAILED: " + name + ": " + what + "\n")
    failed = True


def usage() -> None:
    sys.stderr.write("usage: table_main.py <generated dir> [--indexed] [--gate] [--csv] [--round K] "
                     "[--iterations N] [--wire-dir D] [--variant-dir D]\n")
    sys.exit(1)


def read_file(directory: str, basename: str) -> bytes | None:
    try:
        with open(os.path.join(directory, basename), "rb") as f:
            data = f.read()
    except OSError:
        sys.stderr.write("missing corpus " + directory + "/" + basename
                         + " -- run from the schema repo root (or pass --wire-dir/--variant-dir)\n")
        return None
    goldens[basename] = data
    return data


def corpus_id() -> str:
    """FNV-1a-64 over the goldens THIS RUN LOADED (section 1.6): for each file in
    sorted basename order, the basename bytes, a 0x00 byte, then the contents."""
    h = 0xcbf29ce484222325
    for name in sorted(goldens):
        for chunk in (name.encode("utf-8"), b"\0", goldens[name]):
            for byte in chunk:
                h = ((h ^ byte) * 0x100000001b3) & 0xFFFFFFFFFFFFFFFF
    return "%016x" % h


def stats(rates: list[float]) -> dict[str, float]:
    rates = sorted(rates)
    n = len(rates)
    median = rates[n // 2]
    return {"median": median, "min": rates[0], "max": rates[-1],
            "spread": (rates[-1] - rates[0]) / median * 100.0}


def report(bench: str, path: str, iters: int, bytes_per_op: float, s: dict[str, float],
           csv: bool, num_runs: int) -> None:
    mbps = s["median"] * bytes_per_op / (1024.0 * 1024.0)
    sys.stderr.write("%-18s %-11s %10.4f M msg/s %10.2f MB/s   (min %.4f, max %.4f, spread %.1f%%)\n" % (
        bench, path, s["median"] / 1e6, mbps, s["min"] / 1e6, s["max"] / 1e6, s["spread"]))
    if csv:
        csv_rows.append("python,%s,%s,%d,%.17g,%d,%.0f,%.0f,%.0f,%.2f,%.2f" % (
            bench, path, iters, bytes_per_op, num_runs, s["median"], s["min"], s["max"], mbps, s["spread"]))


def flush_csv(csv: bool) -> None:
    if not csv:
        return
    if failed:
        sys.stderr.write("refusing to emit CSV rows from a failing run\n")  # section 1.5
        return
    out = sys.stdout
    out.write("lang,bench,path,iters,bytes_per_op,runs,median_msgs_per_sec,min_msgs_per_sec,max_msgs_per_sec,"
              "median_mb_per_sec,spread_pct,corpus_id,family,linkage,checks,opt,inline\n")
    ident = corpus_id()
    # family `table` (section 1.9); linkage `module` (a generated package over
    # the runtime beside it); checks `contract` (the read's wire contract is
    # unconditional, with no assert to compile out); opt `default` (CPython
    # has no operator-visible level); inline `unknown` (an interpreter has no
    # section 4.1 artifact to disassemble)
    for row in csv_rows:
        out.write(row + "," + ident + ",table,module,contract,default,unknown\n")


def load_variants(name: str, variant_dir: str, indexed: bool) -> tuple[list[bytes], float] | None:
    packed = read_file(variant_dir, name + ".variants.bin")
    if packed is None:
        return None
    if indexed:
        index = read_file(variant_dir, name + ".lengths")
        if index is None or len(index) != NUM_VARIANTS * 4:
            fail(name, "the length index is not 64 little-endian uint32 lengths")
            return None
        lengths = list(struct.unpack("<%dI" % NUM_VARIANTS, index))
    else:
        if not packed or len(packed) % NUM_VARIANTS:
            fail(name, "the unindexed corpus is not 64 equal records")
            return None
        lengths = [len(packed) // NUM_VARIANTS] * NUM_VARIANTS
    variants, offset = [], 0
    for k, size in enumerate(lengths):
        if size <= 0 or offset + size > len(packed):
            fail(name, "variant %d's length does not fit the corpus" % k)
            return None
        variants.append(packed[offset:offset + size])
        offset += size
    if offset != len(packed):
        fail(name, "the lengths do not sum to the corpus size")
        return None
    return variants, len(packed) / NUM_VARIANTS


def import_generated(generated: str) -> object:
    """The one generated package under <generated dir>, imported from its own
    directory: no module outside it can answer the import."""
    packages = [n for n in sorted(os.listdir(generated))
                if os.path.isfile(os.path.join(generated, n, "__init__.py"))]
    if len(packages) != 1:
        sys.stderr.write("expected one generated package under " + generated + "\n")
        sys.exit(1)
    path = os.path.join(generated, packages[0], "__init__.py")
    spec = importlib.util.spec_from_file_location(packages[0], path,
                                                  submodule_search_locations=[os.path.dirname(path)])
    if spec is None or spec.loader is None:
        sys.stderr.write("cannot import " + path + "\n")
        sys.exit(1)
    module = importlib.util.module_from_spec(spec)
    sys.modules[packages[0]] = module
    spec.loader.exec_module(module)
    return module


def bench_table(cls: type, name: str, golden: str, iters: int, num_runs: int, gate: bool, csv: bool,
                wire_dir: str, variant_dir: str, indexed: bool) -> None:
    global failed
    loaded = load_variants(name, variant_dir, indexed)
    if loaded is None:
        failed = True
        return
    variants, bytes_per_op = loaded

    # gate 1 (section 1.5): variant 0 IS the pinned instance.
    pinned = read_file(wire_dir, golden + ".bin")
    if pinned is None:
        failed = True
        return
    if pinned != variants[0]:
        sys.stderr.write("WIRE GOLDEN MISMATCH: %s (%d golden vs %d corpus bytes) -- refusing to bench code that "
                         "does not match the corpus\n" % (golden, len(pinned), len(variants[0])))
        failed = True
        return

    # gate 2: every variant loads, re-saves, and comes back byte-identical
    # before any clock starts.
    load = cls.load  # type: ignore[attr-defined]
    instances = []
    for k in range(NUM_VARIANTS):
        try:
            value = load(variants[k])
            again = value.save()
        except Exception as error:  # a refusal or an invalid value is the gate's answer
            fail(name, "variant %d does not load and save: %s" % (k, error))
            return
        if again != variants[k]:
            fail(name, "variant round-trip bytes differ -- refusing to bench a codec that does not reproduce "
                       "the corpus")
            return
        instances.append(value)
    if gate:
        return

    clock = time.process_time
    # WRITE: save the 64 pre-loaded instances round-robin.
    write_rates = []
    for run in range(-1, num_runs):
        start = clock()
        for n in range(iters):
            k = n % NUM_VARIANTS
            if len(instances[k].save()) != len(variants[k]):
                fail(name, "save failed in loop")
                return
        elapsed = clock() - start
        if run >= 0:
            write_rates.append(iters / elapsed)

    # ROUND-TRIP: load a variant buffer, then re-save what came out.
    trip_rates = []
    for run in range(-1, num_runs):
        start = clock()
        for n in range(iters):
            k = n % NUM_VARIANTS
            if len(load(variants[k]).save()) != len(variants[k]):
                fail(name, "re-save failed in loop")
                return
        elapsed = clock() - start
        if run >= 0:
            trip_rates.append(iters / elapsed)

    w, rt = stats(write_rates), stats(trip_rates)
    report(name, "write", iters, bytes_per_op, w, csv, num_runs)
    report(name, "round_trip", iters, bytes_per_op, rt, csv, num_runs)
    # READ is DERIVED, never measured: round-trip time minus write time. It
    # prints for continuity and is NOT a CSV row (section 2.9).
    read_time = 1.0 / rt["median"] - 1.0 / w["median"]
    if read_time > 0:
        sys.stderr.write("%-18s %-11s %10.4f M msg/s   (DERIVED: round-trip minus write, informational -- not a "
                         "measured row)\n" % (name, "read", 1e-6 / read_time))


def main() -> None:
    args = sys.argv[1:]
    if not args or args[0].startswith("--"):
        usage()
    generated = args[0]
    csv = gate = indexed = False
    iterations = 0
    num_runs = MAX_RUNS
    wire_dir, variant_dir = "testdata/wire", "bench/corpus/variants"
    i = 1
    while i < len(args):
        a = args[i]
        if a == "--indexed":
            indexed = True
        elif a == "--gate":
            gate = True
        elif a == "--csv":
            csv = True
        elif a in ("--wire-dir", "--variant-dir", "--iterations", "--round") and i + 1 < len(args):
            value = args[i + 1]
            i += 1
            if a == "--wire-dir":
                wire_dir = value
            elif a == "--variant-dir":
                variant_dir = value
            elif a == "--iterations":
                if not value.isdigit() or int(value) <= 0 or int(value) % NUM_VARIANTS:
                    sys.stderr.write("--iterations requires a positive multiple of 64\n")
                    sys.exit(1)
                iterations = int(value)
            else:
                if not value.isdigit():
                    sys.stderr.write("--round takes a non-negative integer, got '" + value + "'\n")
                    sys.exit(1)
                num_runs = 1  # section 2.4: the driver aggregates across rounds itself
        else:
            sys.stderr.write("unknown argument " + a + "\n")
            sys.exit(1)
        i += 1

    module = import_generated(generated)
    sys.stderr.write("schema tables bench (python, %s %s)\n" % (platform.python_implementation(),
                                                              platform.python_version()))
    bench_table(getattr(module, ROOT), "bench_table", "bench_table", iterations or DEFAULT_ITERATIONS, num_runs,
                gate, csv, wire_dir, variant_dir, indexed)
    flush_csv(csv)
    ident = corpus_id()
    if failed:
        sys.stderr.write("TABLES BENCH FAILED (corpus_id " + ident + ")\n")
        sys.exit(1)
    sys.stderr.write("OK (corpus_id " + ident + ")\n")


if __name__ == "__main__":
    main()
