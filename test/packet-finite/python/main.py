"""The Python packet finite-float gate (SPEC §4.3, serialize format 2.0).

Imports the generated finite package (test/packet-finite/python/Finite.schema)
and its vendored runtime from the directory given as the first argument (the
negative controls pass their sabotaged copy), then checks every way a bare
float reaches the Python packet wire: a float64 on a byte boundary and one
behind a bool, a float32 between them, two float64s back to back, and the
quantized float32.

The rule: a NaN (any sign, any payload) or an infinity is refused on write as
InvalidValue naming the field, and on read by its raw bits as Refused with the
reason non_finite_float. A finite double that rounds past the largest float32
is refused in a float32 field. Negative zero, the subnormals and the largest
finite values ride bit exact. The quantized float32 rides its step index, not
the pattern; its write refuses a non-finite value too.

Each failing vector prints "FAILED: <vector>: <detail>" to stderr. The
negative controls compare the set of failing vector names against the
.expected lists beside this file. Prints OK and exits 0, or exits 1.
"""

import struct
import sys
from typing import Callable

sys.path.insert(0, sys.argv[1])

from finite import Finite, InvalidValue, Pair, Refused  # noqa: E402
from finite._schema.serialize import Ref, WriteStream  # noqa: E402

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


# The non-finite patterns: quiet and signalling NaN of either sign, a NaN with
# a payload, and both infinities. Each has an all-ones exponent field.
NON_FINITE_32 = [
    ("quiet-nan", 0x7FC00000), ("signalling-nan", 0x7F800001),
    ("negative-nan", 0xFFC00000), ("payload-nan", 0x7FC0DEAD),
    ("positive-inf", 0x7F800000), ("negative-inf", 0xFF800000),
]
NON_FINITE_64 = [
    ("quiet-nan", 0x7FF8000000000000), ("signalling-nan", 0x7FF0000000000001),
    ("negative-nan", 0xFFF8000000000000), ("payload-nan", 0x7FF800000000DEAD),
    ("positive-inf", 0x7FF0000000000000), ("negative-inf", 0xFFF0000000000000),
]
# The finite edge patterns that must ride bit exact: no writer normalizes one.
FINITE_32 = [
    ("negative-zero", 0x80000000), ("min-subnormal", 0x00000001),
    ("max-subnormal", 0x007FFFFF), ("negative-subnormal", 0x80000001),
    ("max-finite", 0x7F7FFFFF), ("negative-max-finite", 0xFF7FFFFF),
]
FINITE_64 = [
    ("negative-zero", 0x8000000000000000), ("min-subnormal", 0x0000000000000001),
    ("max-subnormal", 0x000FFFFFFFFFFFFF), ("negative-subnormal", 0x8000000000000001),
    ("max-finite", 0x7FEFFFFFFFFFFFFF), ("negative-max-finite", 0xFFEFFFFFFFFFFFFF),
]

QUANTIZED_BITS = 10  # ceil(10 / 0.01) = 1000 steps


def finite() -> Finite:
    """A Finite whose every float is a plain finite value; the vectors
    overwrite one position."""
    v = Finite()
    v.aligned, v.single, v.flag, v.unaligned, v.quantized = 1.5, 2.5, True, 3.5, 5.0
    return v


def raw_base() -> dict[str, int]:
    """The raw layout of finite() above."""
    return {
        "aligned": bits64(1.5), "single": bits32(2.5),
        "unaligned": bits64(3.5), "quantized": 500,
    }


def raw_finite(r: dict[str, int]) -> bytes:
    """Finite's wire laid out from raw patterns, bypassing every float entry
    point, so a reader can be handed a pattern no conforming writer emits."""
    s = WriteStream(64)
    s.serialize_bits(Ref(r["aligned"]), 64)
    s.serialize_bits(Ref(r["single"]), 32)
    s.serialize_bool(Ref(True))
    s.serialize_bits(Ref(r["unaligned"]), 64)
    s.serialize_bits(Ref(r["quantized"]), QUANTIZED_BITS)
    s.flush()
    return s.data()


def raw_pair(a: int, b: int) -> bytes:
    s = WriteStream(16)
    s.serialize_bits(Ref(a), 64)
    s.serialize_bits(Ref(b), 64)
    s.flush()
    return s.data()


def check_write_refused(name: str, value: Finite | Pair, path: str) -> None:
    try:
        wire = value.write()
    except InvalidValue as exc:
        if exc.path != path:
            fail(name, f"write refused at {exc.path}, want {path}")
        return
    except Exception as exc:  # a writer refuses with InvalidValue, nothing else
        fail(name, f"write raised {type(exc).__name__}: {exc}")
        return
    fail(name, f"write accepted a non-finite float ({wire.hex()})")


def check_read_refused(name: str, cls: type[Finite] | type[Pair], wire: bytes) -> None:
    try:
        cls.read(wire)
    except Refused as exc:
        if exc.reason != "non_finite_float":
            fail(name, f"read refused with {exc.reason}, want non_finite_float")
        return
    except Exception as exc:  # a reader refuses, never raises anything else
        fail(name, f"the reader raised {type(exc).__name__}: {exc}")
        return
    fail(name, "read accepted a non-finite pattern")


def check_round_trip(name: str, value: Finite | Pair, want: bytes, same: Callable[[Finite | Pair], bool]) -> None:
    try:
        wire = value.write()
    except InvalidValue as exc:
        fail(name, f"write refused a finite value: {exc}")
        return
    if wire != want:
        fail(name, f"wire {wire.hex()}, want {want.hex()}: the pattern did not ride bit exact")
        return
    try:
        got = type(value).read(wire)
    except Refused as exc:
        fail(name, f"read refused a finite pattern: {exc.reason}")
        return
    if not same(got):
        fail(name, "read back a value that is not the pattern written")


def set_attr(v: Finite | Pair, field: str, x: float) -> None:
    setattr(v, field, x)


def get_attr(v: Finite | Pair, field: str) -> float:
    x: float = getattr(v, field)
    return x


POSITIONS_32 = ["single"]
POSITIONS_64 = ["aligned", "unaligned"]

for label, pattern in NON_FINITE_32:
    for p in POSITIONS_32:
        v = finite()
        set_attr(v, p, f32(pattern))
        check_write_refused(f"{p}/write/{label}", v, f"Finite.{p}")
        r = raw_base()
        r[p] = pattern
        check_read_refused(f"{p}/read/{label}", Finite, raw_finite(r))
        vectors += 2
    q = finite()
    q.quantized = f32(pattern)
    check_write_refused(f"quantized/write/{label}", q, "Finite.quantized")
    vectors += 1

for label, pattern in NON_FINITE_64:
    for p in POSITIONS_64:
        v = finite()
        set_attr(v, p, f64(pattern))
        check_write_refused(f"{p}/write/{label}", v, f"Finite.{p}")
        r = raw_base()
        r[p] = pattern
        check_read_refused(f"{p}/read/{label}", Finite, raw_finite(r))
        vectors += 2
    pair = Pair()
    pair.first, pair.second = 1.0, f64(pattern)
    check_write_refused(f"pair/write/{label}", pair, "Pair.second")
    check_read_refused(f"pair/read/{label}", Pair, raw_pair(bits64(1.0), pattern))
    vectors += 2
    q = finite()
    q.quantized = f64(pattern)
    check_write_refused(f"quantized/write/double-{label}", q, "Finite.quantized")
    vectors += 1

# A finite double above the largest float32 that rounds past it is refused in
# a float32 field; one that rounds down to it rides as that float32.
v = finite()
v.single = 1e39
check_write_refused("single/write/narrows-to-inf", v, "Finite.single")
vectors += 1
v = finite()
v.single = f32(0x7F7FFFFF) * (1 + 2.0**-26)
r = raw_base()
r["single"] = 0x7F7FFFFF
check_round_trip("single/roundtrip/rounds-to-max-finite", v, raw_finite(r),
                 lambda got: bits32(get_attr(got, "single")) == 0x7F7FFFFF)
vectors += 1

for label, pattern in FINITE_32:
    for p in POSITIONS_32:
        v = finite()
        set_attr(v, p, f32(pattern))
        r = raw_base()
        r[p] = pattern
        check_round_trip(f"{p}/roundtrip/{label}", v, raw_finite(r),
                         lambda got, p=p, pattern=pattern: bits32(get_attr(got, p)) == pattern)
        vectors += 1

for label, pattern in FINITE_64:
    for p in POSITIONS_64:
        v = finite()
        set_attr(v, p, f64(pattern))
        r = raw_base()
        r[p] = pattern
        check_round_trip(f"{p}/roundtrip/{label}", v, raw_finite(r),
                         lambda got, p=p, pattern=pattern: bits64(get_attr(got, p)) == pattern)
        vectors += 1
    pair = Pair()
    pair.first, pair.second = f64(pattern), 1.0
    check_round_trip(f"pair/roundtrip/{label}", pair, raw_pair(pattern, bits64(1.0)),
                     lambda got, pattern=pattern: bits64(get_attr(got, "first")) == pattern)
    vectors += 1

if failures > 0:
    sys.stderr.write(f"packet finite Python: {failures} of {vectors} vectors FAILED\n")
    sys.exit(1)
print(f"OK: packet finite Python, {vectors} vectors")
