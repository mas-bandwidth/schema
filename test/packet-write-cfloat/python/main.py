"""The Python packet Write of a compressed float (SPEC §5.1 row 10,
PACKET-WIRE-ALGORITHM.md §2.5) over the shared cases in
test/packet-write-cfloat/cases.txt.

usage: main.py <generated dir> <cases.txt>

Python is an every-build leg: a refused value raises InvalidValue from the
writer in every build. A written case writes, reads back to its quantized
value (or the case's reads-back value), and, when the case pins a wire,
writes exactly those bytes and, when it pins bits, emits exactly that many
bits before the flush. Each failure prints "FAILED: <case>: <detail>" on
stderr; each case answers "case <name> ok|FAILED: <detail>" on stdout, the
line tools/casegate reads.
"""

import struct
import sys

sys.path.insert(0, sys.argv[1])

from cfloat import CFloat, InvalidValue, RatioEndpoint, RatioPair, Refused  # noqa: E402
from cfloat._schema.serialize import WriteStream  # noqa: E402

failures = 0
ran = 0
pending: str | None = None
why: str | None = None


def fail(name: str, detail: str) -> None:
    global failures, why
    sys.stderr.write(f"FAILED: {name}: {detail}\n")
    failures += 1
    if why is None:
        why = detail


def answer() -> None:
    global pending, why
    if pending is not None:
        print(f"case {pending} ok" if why is None else f"case {pending} FAILED: {why}")
    pending, why = None, None


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


def make(kind: str, x: float) -> CFloat | RatioEndpoint | RatioPair:
    if kind == "RatioPair":
        p = RatioPair()
        p.first = x
        p.second = x
        return p
    if kind == "RatioEndpoint":
        r = RatioEndpoint()
        r.ratio = x
        return r
    c = CFloat()
    c.x = x
    return c


def read(kind: str, wire: bytes) -> list[float]:
    """Every field the record read back; a RatioPair has two, and both must
    match the case's value."""
    if kind == "RatioPair":
        p = RatioPair.read(wire)
        return [float(p.first), float(p.second)]
    if kind == "RatioEndpoint":
        return [float(RatioEndpoint.read(wire).ratio)]
    return [float(CFloat.read(wire).x)]


def bits_written(record: CFloat | RatioEndpoint | RatioPair) -> int:
    """The bits the write emits before the flush pads the last byte.

    write() returns bytes, so this runs the same generated _s_write that
    write() runs, into a stream it can ask.
    """
    stream = WriteStream(8)
    record._s_write(stream)
    return int(stream.bits_processed())


with open(sys.argv[2], encoding="utf-8") as cases:
    for raw in cases:
        fields = raw.split()
        if not fields or fields[0].startswith("#"):
            continue
        ran += 1
        answer()
        pending = fields[0]
        if len(fields) < 3 or len(fields) % 2 != 1 or fields[2] not in ("refused", "written"):
            fail(fields[0], f"malformed case {raw.strip()!r}")
            continue
        name, x, verdict = fields[0], value(fields[1]), fields[2]
        # [reads-back <v>] [type RatioEndpoint|RatioPair] [wire <hex>] [bits <n>], in pairs
        want = x
        kind = "CFloat"
        wire_hex = None
        want_bits = None
        malformed = False
        for key, operand in zip(fields[3::2], fields[4::2]):
            if key == "reads-back":
                want = value(operand)
            elif key == "type" and operand in ("RatioEndpoint", "RatioPair"):
                kind = operand
            elif key == "wire":
                wire_hex = operand.lower()
            elif key == "bits" and operand.isdigit():
                want_bits = int(operand)
            else:
                malformed = True
        if malformed:
            fail(name, f"malformed modifier in {raw.strip()!r}")
            continue
        try:
            wire = make(kind, x).write()
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
        if want_bits is not None and (got_bits := bits_written(make(kind, x))) != want_bits:
            fail(name, f"wrote {got_bits} bits before the flush, want {want_bits}")
            continue
        if wire_hex is not None and wire.hex() != wire_hex:
            fail(name, f"wrote {wire.hex()}, want {wire_hex}")
            continue
        try:
            got = read(kind, wire)
        except Refused as exc:
            fail(name, f"the reader refused the writer's bytes: {exc.reason}")
            continue
        if any(bits32(g) != bits32(want) for g in got):
            fail(name, f"read back {got!r}, want {want!r}")

answer()
print(f"packet-write-cfloat Python: ran {ran}, failed {failures}")
if failures or ran == 0:
    sys.exit(1)
