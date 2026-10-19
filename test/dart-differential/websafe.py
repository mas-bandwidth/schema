#!/usr/bin/env python3
"""Fold the schema-internal 64-bit literals of a generated Dart tree.

This is not a fix and it edits no tracked file: the target copies the generated
corpus to build/ first and folds that scratch copy ON BOTH LANES, so the Dart
VM and dart2js run identical source whose constants fit the web runtime's
32-bit operators. The unmodified tree is still the compile-refusal lane.
"""
import glob
import re
import sys

PAT = re.compile(r"(?<![\w.])-?\d{15,}(?![\w.])")


def fold(m):
    n = int(m.group(0))
    if abs(n) <= 2**53:
        return m.group(0)
    return str(n & 0x7FFFFFFF)


def fold_line(line):
    code, sep, comment = line.partition("//")
    return PAT.sub(fold, code) + sep + comment


def main(root):
    for path in glob.glob(root + "/**/*.dart", recursive=True):
        with open(path) as f:
            lines = f.readlines()
        with open(path, "w") as f:
            f.write("".join(fold_line(line) for line in lines))


if __name__ == "__main__":
    main(sys.argv[1])
