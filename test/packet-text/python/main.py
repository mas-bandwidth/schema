"""The Python driver of the packet UTF-8 sweep (test/packet-text/README.md):
one hexadecimal wire per input line ("-" is the empty wire), one verdict per
output line, "REFUSE" or "OK <bits> <payload> <bits> <canonical>".

It reads with the generated reader over a ReadStream, as every leg's driver
does, so the verdict is the field's alone and the C++ oracle's trailing-input
rule is not asked. The consumed and the written bits are printed apart, as
the C++ driver prints them, so a disagreement is a verdict line, never a
crash.
"""

import sys

sys.path.insert(0, sys.argv[1])

from packettext.Narrow import Narrow  # noqa: E402
from packettext._schema.serialize import ReadStream, WriteStream  # noqa: E402


def main() -> None:
    out = []
    for line in sys.stdin.read().split():
        wire = b"" if line == "-" else bytes.fromhex(line)
        value = Narrow()
        r = ReadStream(wire)
        if not value._s_read(r):
            out.append("REFUSE")
            continue
        w = WriteStream(256)
        try:
            value._s_validate("Narrow")
            value._s_write(w)
        except ValueError as exc:
            out.append("WRITE-REFUSED " + type(exc).__name__)
            continue
        written = w.bits_processed()
        w.flush()
        data = w.data()
        payload = value.text.encode("utf-8", "surrogatepass").hex() or "-"
        out.append("OK %d %s %d %s" % (r.bits_processed(), payload, written, data.hex() or "-"))
    sys.stdout.write("\n".join(out) + "\n")


main()
