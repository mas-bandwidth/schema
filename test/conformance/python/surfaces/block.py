"""The block surface (docs/SPEC-TABLES.md §19): each image opens."""

from _common import absent, entries, read_bytes, unit_package, write_bytes

from _blocks import BLOCK_ROOTS, block_root, open_block


def run(manifest, outdir):
    for row in entries(manifest, "block"):
        name, unit, path = row[1], row[2], row[3]
        package = unit_package(unit)
        if package is None:
            absent(outdir, name, "unit not generated for python")
            continue
        root = block_root(name)
        if root is None:
            absent(outdir, name, "no block root for " + name)
            continue
        handle = open_block(package, root, read_bytes(path))
        write_bytes(outdir, name, b"open\n" if handle is not None else b"refuse\n")


__all__ = ["BLOCK_ROOTS", "run"]
