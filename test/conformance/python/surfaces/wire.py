"""The wire surface: observe the public read/save or strict blob refusal."""

from _common import entries, read_bytes, root_class, supported, unit_package, write_bytes


def observed_wire(outdir, case, unit, root, wire):
    """Observe the public verdict without the harness's expected-answer flags."""
    cls = root_class(unit, root)
    if cls is None or not supported(cls):
        raise AssertionError(case + ": wire root is not generated and supported")
    package = unit_package(unit)
    try:
        value = cls.load(read_bytes(wire))
    except package.Refused as err:
        if err.reason != "blob_shared":
            raise AssertionError(case + ": unexpected public wire refusal: " + err.reason) from err
        # The observed strict blob refusal (§2.5) is answered `refused`
        # (ruling R13: an absence is never credited on a must-refuse row).
        # The harness judges whether this particular input owes it.
        write_bytes(outdir, case, b"refused\n")
        return
    write_bytes(outdir, case, value.save())


def run(manifest, outdir):
    for row in entries(manifest, "instance"):
        observed_wire(outdir, row[1], row[2], row[3], row[4])
