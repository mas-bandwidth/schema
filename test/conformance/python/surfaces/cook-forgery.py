"""The cooked header forgery battery."""

from _common import answering, no_codec, entries, read_bytes, unit_package, write_bytes
from _blocks import place
from _cooks import open_cook


def run(manifest, outdir):
    roots = {row[1]: (row[2], row[3]) for row in entries(manifest, "cook")}
    for row in entries(manifest, "forgery"):
        with answering(row[1]):
            name, kind, subject = row[1:4]
            if kind != "cook":
                continue
            unit, root = roots[subject]
            package = unit_package(unit)
            if package is None:
                no_codec(outdir, name)
                continue
            data = read_bytes(row[4])
            extent, pointer = int(row[5]), row[6]
            handle = open_cook(package, root, b"") if pointer == "null" else open_cook(
                package, root, place(data, extent), int(pointer)
            )
            write_bytes(outdir, name, b"open\n" if handle is not None else b"refuse\n")
