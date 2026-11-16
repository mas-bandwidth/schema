"""Read framing vectors strictly and report the whole accepted value."""

import os

from _common import absent, entries, root_class, write_bytes
from _json_common import answer, raise_if_unsupported, read_wire, write_wire


def run(manifest, outdir):
    for row in entries(manifest, "framing"):
        case, unit, root, wire = row[1:5]
        # The roster tests form 1 for Cfg; its original schema also declares
        # a fixed class. Generate a variable-class sibling for this wire gate.
        tree = os.environ.get("PYTHON_FRAMING_GENERATED", "build/conformance-python-framing") if unit == "tblr1" else None
        cls = root_class(unit, root, tree)
        if cls is None:
            absent(outdir, case, "unit not generated for python")
            continue
        try:
            value = read_wire(cls, wire)
        except ValueError as error:
            raise_if_unsupported(error)
            write_bytes(outdir, case, b"refused\n")
            continue
        if "no-value" in row[5:]:
            write_bytes(outdir, case + ".wire", write_wire(cls, value))
            write_bytes(outdir, case, b"read\n")
        else:
            answer(outdir, case, cls, value)
