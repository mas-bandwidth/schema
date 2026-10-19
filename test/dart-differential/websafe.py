#!/usr/bin/env python3
"""Fold the schema-internal 64-bit literals of a generated Dart tree.

This is not a fix and it edits no tracked file: the target copies the generated
corpus to build/ first and folds that scratch copy ON BOTH LANES, so the Dart
VM and dart2js run identical source whose constants fit the web runtime's
32-bit operators. The unmodified tree is still the compile-refusal lane.

An ID/literal above 2^53 is folded to its low 31 bits so dart2js accepts the
tree. A declared RANGE BOUND is not an id: folding it to 31 bits would clamp
every value in 2^31..2^53 and hide the very lane this file exists to build.
Bounds are therefore folded to +-(2^53-1), the widest exact web int, so an
int64/uint64 field carries 2^32..2^53 and reads back unclamped on both
runtimes. Bounds are recognised only on a clamp line, a _jsonIntField call or
a clamp() call; every other literal keeps the low-31 rule.
"""
import glob
import re
import sys

SAFE = 2**53 - 1
PAT = re.compile(r"(?<![\w.])-?\d{15,}(?![\w.])")


def fold(m):
    n = int(m.group(0))
    if abs(n) <= SAFE:
        return m.group(0)
    return str(n & 0x7FFFFFFF)


def fold_bound(m):
    n = int(m.group(0))
    if abs(n) <= SAFE:
        return m.group(0)
    return str(SAFE if n > 0 else -SAFE)


def is_bound(code):
    return "report.clamped++" in code or "_jsonIntField(" in code or "clamp(" in code


def fold_line(line):
    code, sep, comment = line.partition("//")
    fn = fold_bound if is_bound(code) else fold
    return PAT.sub(fn, code) + sep + comment


def main(root):
    for path in glob.glob(root + "/**/*.dart", recursive=True):
        with open(path) as f:
            lines = f.readlines()
        with open(path, "w") as f:
            f.write("".join(fold_line(line) for line in lines))


if __name__ == "__main__":
    main(sys.argv[1])
