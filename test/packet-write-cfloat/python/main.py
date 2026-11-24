"""The Python packet Write of a compressed float (SPEC §5.1 row 10,
PACKET-WIRE-ALGORITHM.md §2.5) over the shared cases in
test/packet-write-cfloat/cases.txt.

usage: main.py <generated dir> <cases.txt>

Python is an every-build leg: a refused value raises InvalidValue from the
writer in every build. A written case writes, reads back to its quantized
value (or the case's reads-back value), and, when the case pins a wire,
writes exactly those bytes. Each failure prints "FAILED: <case>: <detail>";
the last stdout line is the count tools/mustrun reads.
"""

import struct
import sys

sys.path.insert(0, sys.argv[1])

from cfloat import CFloat, InvalidValue, RatioEndpoint, Refused  # noqa: E402

failures = 0
ran = 0


def fail(name: str, detail: str) -> None:
    global failures
    sys.stderr.write(f"FAILED: {name}: {detail}\n")
    failures += 1


def value(text: str) -> float:
    if text == "NaN":
        return float("nan")
    if text == "+Inf":
        return float("inf")
    if text == "-Inf":
        return float("-inf")
    return float(text)


def bits32(v: float) -> int:
    return int(struct.unpack("<I", struct.pack("<f", v))[0])


def make(ratio: bool, x: float) -> CFloat | RatioEndpoint:
    if ratio:
        r = RatioEndpoint()
        r.ratio = x
        return r
    c = CFloat()
    c.x = x
    return c


def read(ratio: bool, wire: bytes) -> float:
    if ratio:
        return float(RatioEndpoint.read(wire).ratio)
    return float(CFloat.read(wire).x)


with open(sys.argv[2], encoding="utf-8") as cases:
    for raw in cases:
        fields = raw.split()
        if not fields or fields[0].startswith("#"):
            continue
        ran += 1
        if len(fields) < 3 or len(fields) % 2 != 1 or fields[2] not in ("refused", "written"):
            fail(fields[0], f"malformed case {raw.strip()!r}")
            continue
        name, x, verdict = fields[0], value(fields[1]), fields[2]
        # [reads-back <v>] [type RatioEndpoint] [wire <hex>], in pairs
        want = x
        ratio = False
        wire_hex = None
        malformed = False
        for key, operand in zip(fields[3::2], fields[4::2]):
            if key == "reads-back":
                want = value(operand)
            elif key == "type" and operand == "RatioEndpoint":
                ratio = True
            elif key == "wire":
                wire_hex = operand.lower()
            else:
                malformed = True
        if malformed:
            fail(name, f"malformed modifier in {raw.strip()!r}")
            continue
        try:
            wire = make(ratio, x).write()
        except InvalidValue:
            if verdict != "refused":
                fail(name, "the writer refused a lawful value")
            continue
        except Exception as exc:  # a writer refuses with InvalidValue, nothing else
            fail(name, f"the writer raised {type(exc).__name__}: {exc}")
            continue
        if verdict == "refused":
            fail(name, f"the writer accepted a value the range refuses ({wire.hex()})")
            continue
        if wire_hex is not None and wire.hex() != wire_hex:
            fail(name, f"wrote {wire.hex()}, want {wire_hex}")
            continue
        try:
            got = read(ratio, wire)
        except Refused as exc:
            fail(name, f"the reader refused the writer's bytes: {exc.reason}")
            continue
        if bits32(got) != bits32(want):
            fail(name, f"read back {got!r}, want {want!r}")

print(f"packet-write-cfloat Python: ran {ran}, failed {failures}")
if failures or ran == 0:
    sys.exit(1)
