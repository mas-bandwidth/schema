"""Read a wire strictly and report its verdict and whole value."""

from _common import answering, no_codec, entries, root_class, write_bytes
from _json_common import answer, raise_if_unsupported, read_wire


def run(manifest, outdir):
    for row in entries(manifest, "report"):
        with answering(row[1]):
            case, unit, root, wire = row[1:5]
            cls = root_class(unit, root)
            if cls is None:
                no_codec(outdir, case)
                continue
            try:
                value = read_wire(cls, wire)
            except ValueError as error:
                raise_if_unsupported(error)
                write_bytes(outdir, case, b"refused\n")
                continue
            answer(outdir, case, cls, value)
