"""The block-foreign surface: a file made foreign to itself by reversing its magic."""

from _common import absent, entries, read_bytes, unit_package, write_bytes

from _blocks import block_root, foreign


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
        from _blocks import open_block

        handle = open_block(package, root, foreign(read_bytes(path)))
        write_bytes(outdir, name, b"open\n" if handle is not None else b"refuse\n")
