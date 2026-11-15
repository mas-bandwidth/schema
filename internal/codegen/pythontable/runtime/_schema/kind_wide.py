"""The wide scalar field kinds (docs/SPEC-TABLES.md §3, kinds 18-29).

The 128-bit integers and the fixed-point family: int128 and uint128, and
fixed(I, F) and ufixed(I, F) at every storage width. Python's int is arbitrary
precision, so no lane arithmetic is needed: the raw value is the integer the
wire carries, low half first in little-endian bytes, and the descriptor's
declared [lo, hi] is the whole contract on write. A read refuses a raw value
outside it rather than clamping (docs/SPEC-TABLES.md §4, §13.10).
"""

from typing import Any

from . import InvalidValue, Refused
from . import kinds
from .descriptor import FieldDesc

_WIDTH = {
    18: 16,
    19: 16,
    20: 1,
    21: 2,
    22: 4,
    23: 8,
    24: 16,
    25: 1,
    26: 2,
    27: 4,
    28: 8,
    29: 16,
}


def _width(fd: FieldDesc) -> int:
    width = _WIDTH.get(fd.kind)
    if width is None:
        raise Refused("kind_mismatch", f"kind {fd.kind} is not a wide scalar kind")
    return width


def _neg(fd: FieldDesc) -> bool:
    return fd.lo < 0


class _Wide:
    def validate(self, fd: FieldDesc, value: Any, path: str) -> None:
        if type(value) is not int:
            raise InvalidValue(path, "not an int")
        if value < fd.lo or value > fd.hi:
            raise InvalidValue(path, f"{value} is outside [{fd.lo}, {fd.hi}]")

    def read(self, r: Any, fd: FieldDesc, ctx: Any) -> Any:
        width = _width(fd)
        neg = _neg(fd)
        raw = int.from_bytes(r.take(width), "little", signed=neg)
        if raw < fd.lo or raw > fd.hi:
            raise Refused("out_of_range", f"{raw} is outside [{fd.lo}, {fd.hi}]")
        return raw

    def write(self, w: Any, fd: FieldDesc, value: Any, ctx: Any) -> None:
        width = _width(fd)
        neg = _neg(fd)
        w.varint(ctx.idref(fd.field_id))
        w.byte(fd.kind)
        w.raw(int(value).to_bytes(width, "little", signed=neg))


kinds.register("wide", _Wide())
