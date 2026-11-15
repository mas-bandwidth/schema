"""The float field kinds (docs/SPEC-TABLES.md §3, kinds 10 and 11).

A float is finite on the wire: the bytes are the IEEE-754 bit pattern, with no
canonicalisation, and every read and write refuses a NaN or an infinity as
`non_finite_float` (docs/SPEC-TABLES.md §3.3). A float32 field holds the
Python float that is exactly that float32; a write rounds as Go's `float32(v)`
does.
"""

import math
import struct
from typing import Any

from . import InvalidValue, Refused
from . import kinds
from .descriptor import FieldDesc

_WIDTH: dict[int, int] = {10: 4, 11: 8}
_FORMAT: dict[int, str] = {10: "<f", 11: "<d"}


class _Float:
    def validate(self, fd: FieldDesc, value: Any, path: str) -> None:
        if type(value) is not float:
            raise InvalidValue(path, "not a float")
        if not math.isfinite(value):
            raise InvalidValue(path, "a float value is finite")
        if fd.kind == 10:
            try:
                struct.pack("<f", value)
            except OverflowError:
                raise InvalidValue(path, "past the float32 range") from None

    def read(self, r: Any, fd: FieldDesc, ctx: Any) -> Any:
        raw = float(struct.unpack(_FORMAT[fd.kind], bytes(r.take(_WIDTH[fd.kind])))[0])
        if not math.isfinite(raw):
            raise Refused("non_finite_float", "a float value is finite")
        return raw

    def write(self, w: Any, fd: FieldDesc, value: Any, ctx: Any) -> None:
        w.varint(ctx.idref(fd.field_id))
        w.byte(fd.kind)
        w.raw(struct.pack(_FORMAT[fd.kind], value))


kinds.register("float", _Float())
