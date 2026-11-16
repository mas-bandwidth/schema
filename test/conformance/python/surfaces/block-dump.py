"""The block-dump surface: the canonical row dump, read through descriptors."""

from _common import absent, entries, read_bytes, unit_package, write_bytes

from _blocks import block_root, open_raw


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
        handle, dump = open_raw(package, root, read_bytes(path))
        if handle is None:
            absent(outdir, name, dump)
            continue
        write_bytes(outdir, name, dump().encode("utf-8"))
