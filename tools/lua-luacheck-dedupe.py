#!/usr/bin/env python3
"""Copy the first file of each SHA-256 from a sorted, NUL-delimited stream."""

import hashlib
import os
from pathlib import Path
import shutil
import sys


def main():
    output = Path(sys.argv[1])
    output.mkdir(parents=True, exist_ok=True)
    # Read the complete input before copying into the corpus's unique directory.
    # The caller supplies the existing find/sort order, including representative
    # basenames used by the lint configuration's narrowly scoped runtime rules.
    paths = sys.stdin.buffer.read().split(b"\0")
    seen = set()
    for raw in paths:
        if not raw:
            continue
        path = Path(os.fsdecode(raw))
        digest = hashlib.sha256()
        with path.open("rb") as source:
            for chunk in iter(lambda: source.read(1024 * 1024), b""):
                digest.update(chunk)
        key = digest.digest()
        if key in seen:
            continue
        seen.add(key)
        shutil.copyfile(path, output / f"{len(seen)}-{path.name}")
    print(
        f"luacheck-lua corpus: {len(seen)} distinct files "
        "(the runtime repeats in every unit and is checked once)"
    )


if __name__ == "__main__":
    main()
