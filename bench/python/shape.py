"""The Python leg's code-shape measurement (docs/PORT-PYTHON.md, D1).

usage: python -I shape.py <generated dir> [iterations]

D1 asks the generator to keep whichever of two shapes measures faster: the
straight-line code it emits (stream methods bound to locals, one guarded call
per field), or a descriptor walk (one generic loop over a table of field
descriptors). This runs both over the same serialize.py streams and the same
representative types of examples/ (Types.Input: five float32s and eight
bools; Wire.ProbeBits: wide bit fields; Wire.Test: ranged ints), with the same
per-field guards, and prints microseconds per write and per read. It is a
measurement, not a test: it uses the clock, so it is a functional run (make
bench-python-shape), never part of make test.
"""

import sys
import time
from collections.abc import Callable
from typing import Any

sys.path.insert(0, sys.argv[1])

from example import Types, Wire  # noqa: E402
from example._schema import InvalidValue  # noqa: E402
from example._schema.serialize import ReadStream, Ref, WriteMisuse, WriteStream  # noqa: E402

ITERATIONS = int(sys.argv[2]) if len(sys.argv) > 2 else 20000

# A descriptor is (field, serialize.py operation, its arguments, guard). The
# guard is the same rule the straight-line code checks before the call.
Desc = tuple[tuple[str, str, tuple[int, ...], str], ...]

INPUT: Desc = tuple(
    [(n, "float", (), "float") for n in ("stick_x", "stick_y", "throttle", "yaw", "pitch")]
    + [(n, "bool", (), "") for n in ("fire", "alt_fire", "boost", "brake", "aim", "lock_on", "zoom", "ping")]
)
PROBE_BITS: Desc = (
    ("small", "bits", (9,), ""), ("boundary", "bits", (33,), ""), ("wide", "bits", (64,), ""),
    ("sensor", "int64", (0, 4294967295), ""), ("nonce", "int128", (0, 18446744073709551615), ""),
)
TEST: Desc = (
    ("test_a", "bits", (16,), ""), ("test_b", "int", (0, 1000), ""),
    ("test_c", "int", (0, 1000), ""), ("test_d", "int", (0, 1000), ""),
)


def walk_write(v: object, desc: Desc, capacity: int) -> bytes:
    w = WriteStream(capacity)
    R: Ref[Any] = Ref(None)
    at = ""
    try:
        for name, op, args, guard in desc:
            at = name
            x = getattr(v, name)
            if guard == "float" and type(x) is not float:
                raise WriteMisuse("a float field holds a float")
            R.value = x
            getattr(w, "serialize_" + op)(R, *args)
    except WriteMisuse as e:
        raise InvalidValue(at, str(e)) from None
    w.flush()
    return w.data()


def walk_read(cls: Callable[[], object], data: bytes, desc: Desc) -> object:
    r = ReadStream(data)
    v = cls()
    R: Ref[Any] = Ref(None)
    for name, op, args, _ in desc:
        if not getattr(r, "serialize_" + op)(R, *args):
            raise ValueError(r.refusal())
        setattr(v, name, R.value)
    if not r.serialize_end():
        raise ValueError(r.refusal())
    return v


def per_op(fn: Callable[[], object]) -> float:
    fn()
    start = time.perf_counter()
    for _ in range(ITERATIONS):
        fn()
    return (time.perf_counter() - start) / ITERATIONS * 1e6


def measure(name: str, v: Any, desc: Desc) -> None:
    cls = type(v)
    capacity = (cls.MAX_BYTES + 7) & ~7
    data = v.write()
    assert walk_write(v, desc, capacity) == data, f"{name}: the walk writes other bytes"
    assert walk_read(cls, data, desc) == cls.read(data), f"{name}: the walk reads another value"
    sw = per_op(lambda: v.write())
    ww = per_op(lambda: walk_write(v, desc, capacity))
    sr = per_op(lambda: cls.read(data))
    wr = per_op(lambda: walk_read(cls, data, desc))
    print(f"{name}: write straight {sw:.2f} us, walk {ww:.2f} us ({ww / sw:.2f}x); "
          f"read straight {sr:.2f} us, walk {wr:.2f} us ({wr / sr:.2f}x)")


inp = Types.Input()
inp.stick_x, inp.stick_y, inp.throttle, inp.yaw, inp.pitch = 0.25, -0.5, 1.0, 0.125, -0.75
inp.fire, inp.boost, inp.zoom = True, True, True
measure("Types.Input", inp, INPUT)

pb = Wire.ProbeBits()
pb.small, pb.boundary, pb.wide, pb.sensor, pb.nonce = 0x1FF, 0x1FFFFFFFF, 0xFEDCBA9876543210, 4294967295, 2**64 - 1
measure("Wire.ProbeBits", pb, PROBE_BITS)

t = Wire.Test()
t.test_a, t.test_b, t.test_c, t.test_d = 65535, 1000, 500, 0
measure("Wire.Test", t, TEST)
print(f"python {sys.version.split()[0]}, {ITERATIONS} iterations each")
