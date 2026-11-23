"""The announcement surface (test/conformance/README.md, `announce`;
SPEC-TABLES §3.3).

write: the Python leg's announcement writer is the compiler: the unit's
ANNOUNCEMENT is generated into the package, and importing the unit reads it
into the unit's own vocabulary with announce_read's 4096-entry cap. The
answer is those bytes, or `refused` when the unit refuses to stand its own
announcement up (FL-38).
strict / lenient: a fresh MessageVocabulary(strict=...) reads the peer's
announcement through announce_read (FL-36).
send: save_messages of one body, which writes under the sender's own
vocabulary with no announcement in hand (FL-18).
"""

import importlib
import os
import sys

from _common import ROOT, entries, read_bytes, root_class, unit_package, write_bytes
from _mutate import fresh_or_load


def _import_unit(unit):
    """Import the unit's package. Returns (package, None), or (None, the
    unit's own Refused) when the import itself refuses."""
    unitdir = os.path.join(ROOT, unit)
    names = [n for n in sorted(os.listdir(unitdir)) if not n.startswith("_") and os.path.isfile(os.path.join(unitdir, n, "__init__.py"))]
    if len(names) != 1:
        raise RuntimeError(unit + ": expected one generated package, found " + repr(names))
    if unitdir not in sys.path:
        sys.path.insert(0, unitdir)
    try:
        return importlib.import_module(names[0]), None
    except ValueError as err:
        runtime = sys.modules.get(names[0] + "._schema")
        refused = getattr(runtime, "Refused", None)
        if refused is None or not isinstance(err, refused):
            raise
        return None, err


def run(manifest, outdir):
    for row in entries(manifest, "announce"):
        case, unit, root, mode, source = row[1:6]
        if mode == "write":
            package, refusal = _import_unit(unit)
            if refusal is not None:
                write_bytes(outdir, case, b"refused\n")
                continue
            write_bytes(outdir, case, bytes(package.ANNOUNCEMENT))
        elif mode in ("strict", "lenient"):
            package = unit_package(unit)
            vocab = package.MessageVocabulary(strict=(mode == "strict"))
            try:
                package.announce_read(vocab, read_bytes(source))
            except package.Refused:
                write_bytes(outdir, case, b"refused\n")
            else:
                write_bytes(outdir, case, b"read\n")
        elif mode == "send":
            package = unit_package(unit)
            cls = root_class(unit, root)
            value = fresh_or_load(cls, source)
            try:
                data = package.save_messages(cls, [value])
            except package.InvalidValue:
                write_bytes(outdir, case, b"refused\n")
            else:
                write_bytes(outdir, case, data)
        else:
            raise ValueError(case + ": unknown announce mode " + mode)
