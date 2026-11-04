"""Packet text, the Python leg (test/packet-text/README.md): the harness's
driver protocol.

usage: python -I main.py <generated dir>, the harness's lines on stdin

Each line is one hexadecimal wire ("-" for none). Each answers "REFUSE", or
"OK <read bits> <payload hex> <written bits> <wire hex>" after re-encoding the
decoded value (the harness compares both bit counts with C++'s). A string(15)
is a str: serialize.py refuses a zero byte, then malformed UTF-8, and no
decoder substitutes replacement characters. The generated codec functions run
over serialize.py's streams directly, so the bit counts are the codec's own.
"""

import sys

sys.path.insert(0, sys.argv[1])

from packettext import InvalidValue  # noqa: E402
from packettext.Narrow import Narrow, _s_r_Narrow, _s_w_Narrow  # noqa: E402
from packettext._schema.serialize import ReadStream, Ref, WriteStream  # noqa: E402


def hexs(b: bytes) -> str:
    return b.hex() if b else "-"


out = []
for line in sys.stdin.read().split():
    wire = b"" if line == "-" else bytes.fromhex(line)
    value = Narrow()
    r = ReadStream(wire)
    if not _s_r_Narrow(r, value, Ref(None)):
        out.append("REFUSE")
        continue
    read = r.bits_processed()
    payload = hexs(value.text.encode("utf-8"))
    w = WriteStream(Narrow.MAX_BYTES + 7 & ~7)
    try:
        _s_w_Narrow(w, value, Ref(None))
    except InvalidValue:
        out.append(f"ACCEPTED-UNWRITABLE {read} {payload}")
        continue
    bits = w.bits_processed()
    w.flush()
    out.append(f"OK {read} {payload} {bits} {hexs(w.data())}")
print("\n".join(out))
