"""The refusal probe (internal/codegen/python, wire_test.go): examples.

Every write of a value outside its contract raises InvalidValue naming the
field path, and returns no byte; every hostile read raises Refused naming
its reason, with no value. Prints one line per case, "<case> <path or
reason>", for the Go side to hold against its table.
"""

import math
import os
import sys

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))

from example import Clauses, Constants, InvalidValue, Refused, Types, Wire  # noqa: E402

here = os.path.dirname(os.path.abspath(__file__))


def golden(name: str) -> bytes:
    with open(os.path.join(here, name + ".bin"), "rb") as f:
        return f.read()


def write(case: str, v: object) -> None:
    try:
        data = v.write()  # type: ignore[attr-defined]
        print(case, "WROTE", data.hex())
    except InvalidValue as e:
        print(case, e.path)


def read(case: str, cls: type, data: object) -> None:
    try:
        cls.read(data)  # type: ignore[attr-defined]
        print(case, "READ")
    except Refused as e:
        print(case, e.reason)


def set_bits(data: bytes, pos: int, value: int, n: int) -> bytes:
    b = bytearray(data)
    for i in range(n):
        bit = pos + i
        if (value >> i) & 1:
            b[bit // 8] |= 1 << (bit % 8)
        else:
            b[bit // 8] &= ~(1 << (bit % 8)) & 0xFF
    return bytes(b)


def td() -> Wire.TestData:
    return Wire.TestData()


v = td()
v.a = 101
write("ranged-int-above-max", v)
v = td()
v.g = 1  # type: ignore[assignment]
write("int-for-bool", v)
v = td()
v.float_value = 1  # type: ignore[assignment]
write("int-for-float", v)
v = td()
v.float_value = math.nan
write("float32-nan", v)
v = td()
v.float_value = 1e39
write("float32-overflow", v)
v = td()
v.double_value = math.inf
write("float64-infinity", v)
v = td()
v.compressed_float_value = -math.inf
write("compressed-infinity", v)
v = td()
v.text = "a\x00b"
write("string-nul", v)
v = td()
v.text = "\ud800"
write("string-lone-surrogate", v)
v = td()
v.text = "x" * 256
write("string-too-long", v)
v = td()
v.fixed_bytes = [0] * 16
write("fixed-array-length", v)
v = td()
v.items = [0] * 17
write("counted-array-length", v)
v = td()
v.items = [0, 256]
write("array-element-range", v)
v = td()
v.int8_value = 128
write("int8-above-range", v)
v = td()
v.uint64_value = -1
write("uint64-negative", v)
v = td()
v.d = 256
write("bits-too-wide", v)
sc = Types.ShipCreate()
sc.position = Types.Vec3()  # type: ignore[assignment]
write("nested-wrong-class", sc)
sc = Types.ShipCreate()
sc.ship_type = 6
write("enum-above-max", sc)
pa = Wire.ProbeArray()
pa.samples[1].weapon = 16
write("nested-enum-path", pa)
pc = Wire.ProbeCollider()
pc.shape.type = 3
write("union-tag-above-max", pc)
bl = Wire.Block()
bl.data = b"x" * (Constants.MaxBlockSize + 1)
write("bytes-too-long", bl)
bl = Wire.Block()
bl.data = memoryview(b"abcd").cast("I")  # type: ignore[assignment]
write("bytes-wide-memoryview", bl)
bl = Wire.Block()
bl.data = memoryview(b"abcd")  # type: ignore[assignment]
write("bytes-memoryview", bl)

read("const-mismatch", Wire.ProbeHeader, set_bits(golden("probe_header"), 0, 0xAC, 8))
read("reserved-nonzero", Wire.ProbeHeader, set_bits(golden("probe_header"), 12, 1, 1))
read("enum-above-max-read", Types.ShipCreate, set_bits(golden("shipcreate_flags"), 0, 7, 3))
chat = golden("chat")
read("string-invalid-utf8", Wire.Chat, chat[:-11] + b"\xff" + chat[-10:])
read("string-embedded-nul", Wire.Chat, chat[:-11] + b"\x00" + chat[-10:])
read("truncated", Wire.Chat, chat[:-1])
read("trailing-byte", Wire.Chat, chat + b"\x00")
read("empty", Wire.Chat, b"")
st = Clauses.Strs()
st.lead, st.tail = 21, 5
read("align-padding-nonzero", Clauses.Strs, set_bits(st.write(), 12, 1, 1))
read("input-str", Wire.Chat, "not bytes")
read("input-wide-memoryview", Wire.Chat, memoryview(chat[: len(chat) // 4 * 4]).cast("I"))
read("input-bytearray", Wire.Chat, bytearray(chat))
read("input-memoryview", Wire.Chat, memoryview(chat))
