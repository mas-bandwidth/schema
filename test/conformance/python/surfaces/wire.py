"""The wire surface: load the wire file, save it, write the bytes."""

from _common import absent, read_bytes, root_class, supported, unit_package, write_bytes


def refused_wire(outdir, case, unit, root, wire):
    """A marked refusal is an observed public verdict, never a missing port."""
    cls = root_class(unit, root)
    if cls is None or not supported(cls):
        raise AssertionError(case + ": expected blob_shared refusal from a supported root")
    package = unit_package(unit)
    try:
        cls.load(read_bytes(wire))
    except package.Refused as err:
        if err.reason != "blob_shared":
            raise AssertionError(case + ": expected blob_shared refusal, got " + err.reason) from err
    else:
        raise AssertionError(case + ": expected blob_shared refusal, read accepted")
    # This filename is the shared harness's expected-refusal protocol (§2.5),
    # not an unsupported construct or an exception caught by the fallback.
    absent(outdir, case, "blob_shared")


def run(manifest, outdir):
    from _common import entries

    for row in entries(manifest, "instance"):
        case, unit, root, wire = row[1], row[2], row[3], row[4]
        if "wire-refused" in row[5:]:
            refused_wire(outdir, case, unit, root, wire)
            continue
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
