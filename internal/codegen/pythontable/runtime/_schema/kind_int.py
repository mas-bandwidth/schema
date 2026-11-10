"""The integer field kinds (docs/SPEC-TABLES.md §3, kinds 2-9 and 18-19).

Python's int is arbitrary precision, so a uint64 and an int128 need no lanes:
the declared width is the byte count on the wire and the descriptor's range is
the whole contract on write.
"""

from typing import Any

from . import InvalidValue, Refused
from . import kinds
from .descriptor import FieldDesc

_WIDTH = {
    2: 1,
    3: 2,
    4: 4,
    5: 8,
    6: 1,
    7: 2,
    8: 4,
    9: 8,
    18: 16,
    19: 16,
    36: 8,
}


def _width(fd: FieldDesc) -> int:
    width = _WIDTH.get(fd.kind)
    if width is None:
        raise Refused("kind_mismatch", f"kind {fd.kind} is not an integer kind")
    return width


class _Int:
    def validate(self, fd: FieldDesc, value: Any, path: str) -> None:
        if type(value) is not int:
            raise InvalidValue(path, "not an int")
        if value < fd.lo or value > fd.hi:
            raise InvalidValue(path, f"{value} is outside [{fd.lo}, {fd.hi}]")

    def read(self, r: Any, fd: FieldDesc, ctx: Any) -> Any:
        width = _width(fd)
        raw = int.from_bytes(r.take(width), "little", signed=fd.lo < 0)
        if raw < fd.lo or raw > fd.hi:
            raise Refused("out_of_range", f"{raw} is outside [{fd.lo}, {fd.hi}]")
        return raw

    def write(self, w: Any, fd: FieldDesc, value: Any, ctx: Any) -> None:
        width = _width(fd)
        w.varint(ctx.idref(fd.field_id))
        w.byte(fd.kind)
        w.raw(int(value).to_bytes(width, "little", signed=fd.lo < 0))


kinds.register("int", _Int())
