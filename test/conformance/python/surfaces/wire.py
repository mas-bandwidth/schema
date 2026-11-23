"""The wire surface: observe the public read/save or its refusal."""

from _common import answering, entries, no_codec, read_bytes, root_class, supported, unit_package, write_bytes


def observed_wire(outdir, case, unit, root, wire):
    """Observe the public verdict without the harness's expected-answer flags."""
    cls = root_class(unit, root)
    if cls is None or not supported(cls):
        # no codec for this root: the per-case absence, never a refusal
        no_codec(outdir, case)
        return
    package = unit_package(unit)
    try:
        value = cls.load(read_bytes(wire))
    except package.Refused:
        # The harness compares the public verdict independently of its reason.
        write_bytes(outdir, case, b"refused\n")
        return
    write_bytes(outdir, case, value.save())


def run(manifest, outdir):
    for row in entries(manifest, "instance"):
        with answering(row[1]):
            observed_wire(outdir, row[1], row[2], row[3], row[4])
