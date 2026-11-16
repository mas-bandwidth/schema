#!/usr/bin/env python3
"""Prove the wire fuzzer rejects a reader that admits invalid framing."""

import json
from pathlib import Path
import re
import shlex
import struct
import subprocess
import sys


def run(args, *, expect=0):
    print(shlex.join(map(str, args)), flush=True)
    result = subprocess.run(args, text=True, stdout=subprocess.PIPE, stderr=subprocess.STDOUT)
    print(result.stdout, end="", flush=True)
    if expect is not None and result.returncode != expect:
        raise RuntimeError(f"command exited {result.returncode}, expected {expect}")
    return result


def identity(name):
    value = 0xCBF29CE484222325
    for byte in name.encode():
        value = ((value ^ byte) * 0x100000001B3) & 0xFFFFFFFFFFFFFFFF
    return value


def file_wire(body, names):
    ids = [identity(name) for name in names]
    return bytes([1, *body, 0]) + struct.pack("<" + "Q" * (len(ids) + 1), *ids, len(ids))


def main():
    name, cxx, flags, serialize = sys.argv[1:]
    scratch = Path("build") / ("wire-fuzz-nc-" + name)
    scratch.mkdir(parents=True, exist_ok=True)
    source = Path("internal/codegen/cpptable/strictwalk.go")
    if name == "length":
        wire = file_wire([1, 14, 3, 4, 0, 42], ["future"])
        edits = [("return r.offset == r.size; // bytes left inside an array's L after its elements (L6)",
                  "return true; // NEGATIVE CONTROL: bytes after the counted payload are admitted")]
    elif name == "arm-width":
        wire = file_wire([1, 15, 2, 4, 3, 0, 0, 0], ["future", "future_arm"])
        edits = [("if ( n != width ) { return false; } // an L that is not its kind's width (H9)",
                  "if ( false && n != width ) { return false; } // NEGATIVE CONTROL: the arm width is unchecked")]
    elif name == "arm-terminator":
        wire = file_wire([1, 15, 2, 13, 2, 0, 42], ["future", "future_arm"])
        edits = [("if ( ref == 0 ) { return r.offset == n; } // the terminator is the body's last byte (L3)",
                  "if ( ref == 0 ) { return true; } // NEGATIVE CONTROL: a body may end before its extent")]
    elif name == "index":
        wire = file_wire([1, 17, 2], ["future"])
        source = Path("internal/codegen/cpptable/arena.go")
        # The graph checks this bound in counting, topological and reach passes.
        # Drop the same invariant in all three without touching out-of-range memory.
        edits = [(f"if ( index - 1 >= (uint64_t) {lane}->count ) {{ {lane}->bad = true; return false; }}",
                  f"if ( index - 1 >= (uint64_t) {lane}->count ) {{ return true; }} // NEGATIVE CONTROL: an unknown pointer's index is ignored")
                 for lane in ("c", "k", "r")]
    else:
        raise RuntimeError(f"unknown control {name}")
    vector = scratch / "input.bin"
    vector.write_bytes(wire)
    harness = ["./build/conformance-harness", "wire-fuzz", "--replay", str(vector),
               "--unit", "featurewitnesses", "--root", "ArrayRoot", "--failed", str(scratch / "failed.bin")]
    # The ordinary reader must agree before the sabotage is allowed to count.
    healthy = run([*harness, "--driver", "./build/wire-fuzz-cpp"])
    (scratch / "healthy.log").write_text(healthy.stdout)
    original = source.read_text()
    planted = original
    for old, new in edits:
        if planted.count(old) != 1:
            raise RuntimeError(f"{name} needs exactly one source guard: {old}")
        planted = planted.replace(old, new, 1)
    replacement = scratch / (source.stem + ".go.txt")
    replacement.write_text(planted)
    overlay = scratch / "overlay.json"
    overlay.write_text(json.dumps({"Replace": {str(source.resolve()): str(replacement.resolve())}}))
    compiler = scratch / "schema"
    run(["go", "build", "-overlay", str(overlay), "-o", str(compiler), "./cmd/schema"])
    generated = scratch / "features"
    run([str(compiler), "generate", "--lang", "cpp", "--out", str(generated),
         "testdata/conformance/feature-witnesses/full/Features.schema"])
    lines = []
    for line in Path("test/tables/wire_fuzz_main.cpp").read_text().splitlines():
        if line.startswith('#include "') and '"FeaturesTable.h"' not in line:
            continue
        if re.match(r"^    (FIXED|NOFILE|VARIABLE|MESSAGE|RETAIN)\(", line) and '"featurewitnesses"' not in line:
            continue
        lines.append(line)
    driver_source = scratch / "main.cpp"
    driver_source.write_text("\n".join(lines) + "\n")
    driver = scratch / "leg"
    run([*shlex.split(cxx), *shlex.split(flags), "-O1", "-I" + str(generated),
         "-Itest/tables", "-I" + serialize, str(driver_source),
         str(generated / "FeaturesTable.cpp"), "-o", str(driver)])
    broken = run([*harness, "--driver", str(driver)], expect=None)
    (scratch / "log").write_text(broken.stdout)
    if broken.returncode == 0 or "read acceptance differs: the leg says true, the oracle says false" not in broken.stdout:
        raise RuntimeError(f"{name}: the planted reader was not caught accepting the refused input")
    print(f"negative control: removing the {name} guard makes the reader accept and the wire fuzzer RED")


if __name__ == "__main__":
    try:
        main()
    except (OSError, RuntimeError) as error:
        print(f"NEGATIVE CONTROL FAILED: {error}", file=sys.stderr)
        sys.exit(1)
