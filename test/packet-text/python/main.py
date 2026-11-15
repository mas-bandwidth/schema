"""The Python driver of the packet UTF-8 sweep (test/packet-text/README.md):
one hexadecimal wire per input line ("-" is the empty wire), one verdict per
output line, "REFUSE" or "OK <bits> <payload> <bits> <canonical>".

It reads with the generated reader over a ReadStream, as every leg's driver
does, so the verdict is the field's alone and the C++ oracle's trailing-input
rule is not asked; the consumed bits must equal the bits the write wrote.
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
        value._s_validate("Narrow")
        value._s_write(w)
        bits = w.bits_processed()
        w.flush()
        data = w.data()
        if r.bits_processed() != bits:
            raise SystemExit("the read consumed %d bits, the write wrote %d" % (r.bits_processed(), bits))
        payload = value.text.encode("utf-8").hex() or "-"
        out.append("OK %d %s %d %s" % (bits, payload, bits, data.hex() or "-"))
    sys.stdout.write("\n".join(out) + "\n")


main()
