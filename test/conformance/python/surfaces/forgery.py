"""The block forgery battery (testdata/conformance/tables/FORMAT.md)."""

from _common import absent, entries, read_bytes, unit_package, write_bytes

from _blocks import block_root, open_block, place


def subject_root(manifest):
    out = {}
    for row in entries(manifest, "block"):
        out[row[1]] = (row[2], block_root(row[1]))
    return out


def run(manifest, outdir):
    roots = subject_root(manifest)
    for row in entries(manifest, "forgery"):
        name, kind, subject = row[1], row[2], row[3]
        if kind != "block":
            continue
        unit, root = roots[subject]
        package = unit_package(unit)
        if package is None:
            absent(outdir, name, "unit not generated for python")
            continue
        data = read_bytes(row[4])
        extent = int(row[5])
        pointer = row[6]
        if pointer == "null":
            handle = open_block(package, root, b"")
        else:
            handle = open_block(package, root, place(data, extent), int(pointer))
        write_bytes(outdir, name, b"open\n" if handle is not None else b"refuse\n")
