"""The block-foreign surface: a file made foreign to itself by reversing its magic."""

from _common import answering, no_codec, entries, read_bytes, unit_package, write_bytes

from _blocks import block_root, foreign


def run(manifest, outdir):
    for row in entries(manifest, "block"):
        with answering(row[1]):
            name, unit, path = row[1], row[2], row[3]
            package = unit_package(unit)
            if package is None:
                no_codec(outdir, name)
                continue
            root = block_root(name)
            if root is None:
                no_codec(outdir, name)
                continue
            from _blocks import open_block

            handle = open_block(package, root, foreign(read_bytes(path)))
            write_bytes(outdir, name, b"open\n" if handle is not None else b"refuse\n")
