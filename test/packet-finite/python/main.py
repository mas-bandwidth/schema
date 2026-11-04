"""The Python packet finite-float gate (SPEC §4.3, serialize format 2.0).

usage: python -I main.py <generated dir>

Loads the package generated from Finite.schema out of the directory given
(the negative controls pass their sabotaged copy), then checks every way a
bare float reaches the Python packet wire: a float32 and a float64 each
alone, a float64 on a byte and one behind a bool, both widths as list
elements, and a fixed array of float64.

The rule, through the vendored serialize.py: a NaN (any sign, any payload) or
an infinity is refused on write (InvalidValue) and on read on its raw bits
(Refused, non_finite_float). Negative zero, the subnormals and the largest
finite values ride bit exact. The quantized float32 rides its step index, not
the pattern; its write refuses a non-finite value too.

Each failing vector prints "FAILED: <vector>: <detail>" to stderr; the
negative controls compare the set of failing vector names against the
.expected list beside this file. Prints OK and exits 0, or exits 1.
"""

import struct
import sys
from collections.abc import Callable

sys.path.insert(0, sys.argv[1])

from packetfinite import Finite as F  # noqa: E402
from packetfinite import InvalidValue, Refused  # noqa: E402
from packetfinite._schema.serialize import Ref, WriteStream  # noqa: E402

failures = 0
vectors = 0


def fail(name: str, detail: str) -> None:
    global failures
    sys.stderr.write(f"FAILED: {name}: {detail}\n")
    failures += 1


def f32(bits: int) -> float:
    return float(struct.unpack("<f", struct.pack("<I", bits))[0])


def f64(bits: int) -> float:
    return float(struct.unpack("<d", struct.pack("<Q", bits))[0])


def bits32(v: float) -> int:
    return int(struct.unpack("<I", struct.pack("<f", v))[0])


def bits64(v: float) -> int:
    return int(struct.unpack("<Q", struct.pack("<d", v))[0])


NON_FINITE_32 = [
    ("quiet-nan", 0x7FC00000), ("signalling-nan", 0x7F800001), ("negative-nan", 0xFFC00000),
    ("payload-nan", 0x7FC0DEAD), ("positive-inf", 0x7F800000), ("negative-inf", 0xFF800000),
]
NON_FINITE_64 = [
    ("quiet-nan", 0x7FF8000000000000), ("signalling-nan", 0x7FF0000000000001),
    ("negative-nan", 0xFFF8000000000000), ("payload-nan", 0x7FF800000000DEAD),
    ("positive-inf", 0x7FF0000000000000), ("negative-inf", 0xFFF0000000000000),
]
FINITE_32 = [
    ("negative-zero", 0x80000000), ("min-subnormal", 0x00000001), ("max-subnormal", 0x007FFFFF),
    ("negative-subnormal", 0x80000001), ("max-finite", 0x7F7FFFFF), ("negative-max-finite", 0xFF7FFFFF),
]
FINITE_64 = [
    ("negative-zero", 0x8000000000000000), ("min-subnormal", 0x0000000000000001),
    ("max-subnormal", 0x000FFFFFFFFFFFFF), ("negative-subnormal", 0x8000000000000001),
    ("max-finite", 0x7FEFFFFFFFFFFFFF), ("negative-max-finite", 0xFFEFFFFFFFFFFFFF),
]


def finite() -> F.Finite:
    """A Finite whose every float is a plain finite value."""
    v = F.Finite()
    v.aligned, v.single, v.flag, v.unaligned = 1.5, 2.5, True, 3.5
    v.singles = [4.5]
    v.doubles = [5.5]
    v.quantized = 5.0
    return v


QUANTIZED_BITS = 10  # ceil(10 / 0.01) = 1000 steps


def raw_base() -> dict[str, int]:
    return {
        "aligned": bits64(1.5), "single": bits32(2.5), "unaligned": bits64(3.5),
        "singles": bits32(4.5), "doubles": bits64(5.5), "quantized": 500,
    }


def raw_finite(r: dict[str, int]) -> bytes:
    """Finite's wire laid out from raw patterns, bypassing every float entry point."""
    s = WriteStream(64)
    for value, bits in (
        (r["aligned"], 64), (r["single"], 32), (1, 1), (r["unaligned"], 64), (1, 3),
        (r["singles"], 32), (1, 3), (r["doubles"], 64), (r["quantized"], QUANTIZED_BITS),
    ):
        s.serialize_bits(Ref(value), bits)
    s.flush()
    return s.data()


def raw_pair(a: int, b: int) -> bytes:
    s = WriteStream(16)
    s.serialize_bits(Ref(a), 64)
    s.serialize_bits(Ref(b), 64)
    s.flush()
    return s.data()


def write_refused(name: str, value: F.Finite | F.Pair) -> None:
    try:
        wire = value.write()
        fail(name, f"write accepted a non-finite float ({wire.hex()})")
    except InvalidValue:
        pass


def read_refused(name: str, cls: type[F.Finite] | type[F.Pair], wire: bytes) -> None:
    try:
        cls.read(wire)
        fail(name, "read accepted a non-finite pattern")
    except Refused as e:
        if e.reason != "non_finite_float":
            fail(name, f"read refused with {e.reason}, want non_finite_float")
    except Exception as e:  # a reader refuses, never raises anything else
        fail(name, f"the reader raised {e!r}")


def round_trip(name: str, value: F.Finite, want: bytes, same: Callable[[F.Finite], bool]) -> None:
    try:
        wire = value.write()
    except InvalidValue as e:
        fail(name, f"write refused a finite value: {e}")
        return
    if wire != want:
        fail(name, f"wire {wire.hex()}, want {want.hex()}: the pattern did not ride bit exact")
        return
    try:
        got = F.Finite.read(wire)
    except Refused as e:
        fail(name, f"read refused a finite pattern: {e.reason}")
        return
    if not same(got):
        fail(name, "read back a value that is not the pattern written")


def set_single(v: F.Finite, x: float) -> None:
    v.single = x


def set_singles(v: F.Finite, x: float) -> None:
    v.singles[0] = x


def set_aligned(v: F.Finite, x: float) -> None:
    v.aligned = x


def set_unaligned(v: F.Finite, x: float) -> None:
    v.unaligned = x


def set_doubles(v: F.Finite, x: float) -> None:
    v.doubles[0] = x


POSITIONS_32: list[tuple[str, Callable[[F.Finite, float], None], Callable[[F.Finite], float]]] = [
    ("single", set_single, lambda g: g.single),
    ("singles", set_singles, lambda g: g.singles[0]),
]
POSITIONS_64: list[tuple[str, Callable[[F.Finite, float], None], Callable[[F.Finite], float]]] = [
    ("aligned", set_aligned, lambda g: g.aligned),
    ("unaligned", set_unaligned, lambda g: g.unaligned),
    ("doubles", set_doubles, lambda g: g.doubles[0]),
]

for name, pattern in NON_FINITE_32:
    for pos, setter, _ in POSITIONS_32:
        v = finite()
        setter(v, f32(pattern))
        write_refused(f"{pos}/write/{name}", v)
        r = raw_base()
        r[pos] = pattern
        read_refused(f"{pos}/read/{name}", F.Finite, raw_finite(r))
        vectors += 2
    q = finite()
    q.quantized = f32(pattern)
    write_refused(f"quantized/write/{name}", q)
    vectors += 1

for name, pattern in NON_FINITE_64:
    for pos, setter, _ in POSITIONS_64:
        v = finite()
        setter(v, f64(pattern))
        write_refused(f"{pos}/write/{name}", v)
        r = raw_base()
        r[pos] = pattern
        read_refused(f"{pos}/read/{name}", F.Finite, raw_finite(r))
        vectors += 2
    pair = F.Pair()
    pair.values = [1.0, f64(pattern)]
    write_refused(f"pair/write/{name}", pair)
    read_refused(f"pair/read/{name}", F.Pair, raw_pair(bits64(1.0), pattern))
    vectors += 2
    q = finite()
    q.quantized = f64(pattern)
    write_refused(f"quantized/write/double-{name}", q)
    vectors += 1

# a finite double above FLT_MAX narrows to an infinity in a float32 field
v = finite()
v.single = 1e39
write_refused("single/write/narrows-to-inf", v)
vectors += 1

for name, pattern in FINITE_32:
    for pos, setter, getter in POSITIONS_32:
        v = finite()
        setter(v, f32(pattern))
        r = raw_base()
        r[pos] = pattern
        round_trip(f"{pos}/roundtrip/{name}", v, raw_finite(r), lambda g, p=pattern, get=getter: bits32(get(g)) == p)
        vectors += 1

for name, pattern in FINITE_64:
    for pos, setter, getter in POSITIONS_64:
        v = finite()
        setter(v, f64(pattern))
        r = raw_base()
        r[pos] = pattern
        round_trip(f"{pos}/roundtrip/{name}", v, raw_finite(r), lambda g, p=pattern, get=getter: bits64(get(g)) == p)
        vectors += 1

if failures:
    sys.stderr.write(f"packet finite (python): {failures} of {vectors} vectors failed\n")
    sys.exit(1)
print(f"packet finite (python): {vectors} vectors, OK")
