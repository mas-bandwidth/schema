"""The float field kinds (docs/SPEC-TABLES.md §3, kinds 10-11).

A float rides as its IEEE-754 bit pattern, little-endian, with no
canonicalisation. Every value is finite: a NaN or an infinity is refused on
write and, by its raw bits, on read. A COMPRESSED float (a declared min, max
and resolution) is held to its range on both sides and lands -0 as +0.
"""

import math
import struct
from typing import Any

from . import InvalidValue, Refused
from . import kinds
from .descriptor import FieldDesc

_WIDTH = {"float32": 4, "float64": 8}


def _pack(fd: FieldDesc, value: float) -> bytes:
    try:
        if fd.codec == "float32":
            return struct.pack("<f", value)
        return struct.pack("<d", value)
    except (OverflowError, struct.error):
        raise InvalidValue(fd.name, "a value a float32 cannot hold") from None


def _unpack(fd: FieldDesc, raw: bytes) -> float:
    if fd.codec == "float32":
        return float(struct.unpack("<f", raw)[0])
    return float(struct.unpack("<d", raw)[0])


class _Float:
    def validate(self, fd: FieldDesc, value: Any, path: str) -> None:
        if type(value) is not float:
            raise InvalidValue(path, "not a float")
        if not math.isfinite(value):
            raise InvalidValue(path, "not a finite float")
        stored = value
        if fd.codec == "float32":
            # THE RANGE IS HELD ON THE VALUE THE WIRE CARRIES: a float32 is
            # rounded first, so a value inside the range whose float32 lies
            # outside it (0.1 under max = 0.1) is refused here rather than
            # written as bytes every reader refuses.
            stored = _unpack(fd, _pack(fd, value))
        if fd.float_range and (stored < fd.fmin or stored > fd.fmax):
            raise InvalidValue(path, f"{value} is outside [{fd.fmin}, {fd.fmax}]")

    def read(self, r: Any, fd: FieldDesc, ctx: Any) -> Any:
        value = _unpack(fd, r.take(_WIDTH[fd.codec]))
        if not math.isfinite(value):
            raise Refused("non_finite_float", f"{fd.name} is not a finite float")
        if fd.float_range:
            if value < fd.fmin or value > fd.fmax:
                raise Refused("out_of_range", f"{value} is outside [{fd.fmin}, {fd.fmax}]")
            if value == 0.0:
                return 0.0
        return value

    def write(self, w: Any, fd: FieldDesc, value: Any, ctx: Any) -> None:
        w.varint(ctx.idref(fd.field_id))
        w.byte(fd.kind)
        self.write_value(w, fd, value, ctx)

    def write_value(self, w: Any, fd: FieldDesc, value: Any, ctx: Any) -> None:
        if fd.float_range and value == 0.0:
            value = 0.0
        w.raw(_pack(fd, value))


kinds.register("float32", _Float())
kinds.register("float64", _Float())
