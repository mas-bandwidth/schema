"""The text writer in a comma-decimal process locale (test/conformance/README.md,
`json-write-into`; FL-14, FL-15).

The process locale is set through locale.setlocale(LC_ALL, ...) to the
harness's SCHEMA_COMMA_LOCALE; a refusal fails the surface. The Python text
writer, to_json, returns a str and takes no caller buffer, so there is no tail
past the text to inspect (FL-15 has no buffer to hold here): the answer is
the text it returned.
"""

import locale
import os
import sys

from _common import entries, read_bytes, root_class, write_bytes


def run(manifest, outdir):
    name = os.environ.get("SCHEMA_COMMA_LOCALE")
    if not name:
        sys.stderr.write("json-write-into: SCHEMA_COMMA_LOCALE is not set by the harness\n")
        sys.exit(1)
    try:
        locale.setlocale(locale.LC_ALL, name)
    except locale.Error as err:
        sys.stderr.write("json-write-into: setlocale(LC_ALL, " + name + ") refused: " + str(err) + "\n")
        sys.exit(1)
    if locale.localeconv()["decimal_point"] != ",":
        sys.stderr.write("json-write-into: " + name + " does not set a comma decimal point\n")
        sys.exit(1)
    for row in entries(manifest, "json-write-into"):
        case, unit, root, wire = row[1:5]
        cls = root_class(unit, root)
        if cls is None:
            raise RuntimeError(case + ": unit " + unit + " root " + root + " is not generated")
        value = cls.load(read_bytes(wire))
        write_bytes(outdir, case, value.to_json().encode("utf-8"))
