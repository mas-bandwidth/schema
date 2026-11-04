"""Packet wide, the Python leg (test/packet-wide/README.md): the harness's
driver protocol over WideText.schema.

usage: python -I main.py <generated dir>, the harness's lines on stdin

Each line is "<bound> <hex wire>": the bound (7 or 4) names WideSeven or
WideFour. Each answers "REFUSE", or "OK <read bits> <units hex> <written
bits> <wire hex>", the units four hex digits each, after re-encoding the
decoded value. A wstring(N) is a str of at most N UTF-16 code units: the read
refuses a zero unit, a unit above 0xFFFF and an unpaired surrogate.
"""

import sys

sys.path.insert(0, sys.argv[1])

from wide import InvalidValue  # noqa: E402
from wide import WideText as W  # noqa: E402
from wide._schema.serialize import ReadStream, Ref, WriteStream  # noqa: E402


def hexs(b: bytes) -> str:
    return b.hex() if b else "-"


def units(text: str) -> str:
    # surrogatepass: a sabotaged reader may hand back a lone surrogate, which
    # must reach the harness as a verdict, not a crash
    data = text.encode("utf-16-le", "surrogatepass")
    if not data:
        return "-"
    return "".join(f"{data[i] | data[i + 1] << 8:04x}" for i in range(0, len(data), 2))


out = []
lines = sys.stdin.read().splitlines()
for line in lines:
    if not line:
        continue
    bound, raw = line.split(" ")
    assert bound in ("7", "4"), "a bound of 7 or 4"
    seven = bound == "7"
    value: W.WideSeven | W.WideFour = W.WideSeven() if seven else W.WideFour()
    r = ReadStream(b"" if raw == "-" else bytes.fromhex(raw))
    ok = W._s_r_WideSeven(r, value, Ref(None)) if isinstance(value, W.WideSeven) else W._s_r_WideFour(r, value, Ref(None))
    if not ok:
        out.append("REFUSE")
        continue
    read = r.bits_processed()
    payload = units(value.text)
    w = WriteStream(type(value).MAX_BYTES + 7 & ~7)
    try:
        if isinstance(value, W.WideSeven):
            W._s_w_WideSeven(w, value, Ref(None))
        else:
            W._s_w_WideFour(w, value, Ref(None))
    except InvalidValue:
        out.append(f"ACCEPTED-UNWRITABLE {read} {payload}")
        continue
    bits = w.bits_processed()
    w.flush()
    out.append(f"OK {read} {payload} {bits} {hexs(w.data())}")
print("\n".join(out))
