#!/usr/bin/env python3
"""Fold the schema-internal 64-bit literals of a generated Dart tree.

This is not a fix and it edits no tracked file: the target copies the generated
corpus to build/ first and folds that scratch copy ON BOTH LANES, so the Dart
VM and dart2js run identical source whose constants fit the web runtime.

Two folds, because the literals play two roles. A declared CLAMP BOUND must
stay wide enough for the lane's values: int64's [-2^63, 2^63-1] folds to the
widest exact web int, +/- (2^53 - 1), so 2^32..2^53 read back unclamped on
both runtimes instead of being cut to a 31-bit bound. An ID or hash literal is
only compared for equality, so it keeps the old low-31-bit fold, which also
avoids collisions between distinct ids. The unmodified tree is still the
compile-refusal lane.
"""
import glob
import re
import sys

PAT = re.compile(r"(?<![\w.])-?\d{15,}(?![\w.])")
CLAMP = re.compile(r"(?:\bvalue\s*[<>]|_u64(?:Below|Above)\s*\()")
WEB_MAX = 2**53 - 1


def fold(m, bound):
    n = int(m.group(0))
    if abs(n) <= 2**53:
        return m.group(0)
    if bound:
        return str(WEB_MAX if n > 0 else -WEB_MAX)
    return str(n & 0x7FFFFFFF)


def fold_line(line):
    code, sep, comment = line.partition("//")
    bound = bool(CLAMP.search(code))
    return PAT.sub(lambda m: fold(m, bound), code) + sep + comment


def main(root):
    for path in glob.glob(root + "/**/*.dart", recursive=True):
        with open(path) as f:
            lines = f.readlines()
        with open(path, "w") as f:
            f.write("".join(fold_line(line) for line in lines))


if __name__ == "__main__":
    main(sys.argv[1])
