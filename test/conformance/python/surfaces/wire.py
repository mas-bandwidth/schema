"""The wire surface: load the wire file, save it, write the bytes."""

from _common import absent, read_bytes, root_class, supported, write_bytes


def run(manifest, outdir):
    from _common import entries

    for row in entries(manifest, "instance"):
        case, unit, root, wire = row[1], row[2], row[3], row[4]
        try:
            cls = root_class(unit, root)
            if cls is None:
                absent(outdir, case, "unit not generated for python")
                continue
            if not supported(cls):
                absent(outdir, case, "the python table runtime does not carry this construct")
                continue
            value = cls.load(read_bytes(wire))
            write_bytes(outdir, case, value.save())
        except Exception as err:  # noqa: BLE001 - a case this leg cannot answer is absent
            absent(outdir, case, err)
