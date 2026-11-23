"""Refuse a cook with its magic reversed to the foreign byte order."""

from _common import answering, no_codec, entries, read_bytes, unit_package, write_bytes
from _blocks import foreign
from _cooks import open_cook


def run(manifest, outdir):
    for row in entries(manifest, "cook"):
        with answering(row[1]):
            name, unit, root, path = row[1:5]
            package = unit_package(unit)
            if package is None:
                no_codec(outdir, name)
                continue
            handle = open_cook(package, root, foreign(read_bytes(path)))
            write_bytes(outdir, name, b"open\n" if handle is not None else b"refuse\n")
