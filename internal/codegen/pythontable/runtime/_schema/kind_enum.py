"""The enum field kind (docs/SPEC-TABLES.md §3, kind 30; §5).

An enum rides under its own wire kind and its payload is the reference to the
variant NAME's id in the file's id table. The descriptor carries the variants
as (value, wire id) pairs and, built once from them at import, the two lookups
the codec uses: wire_of (value to wire id) and value_of (wire id to value), so
the codec never re-derives a hash and never scans the variants (the measured
choice, docs/PORT-PYTHON.md, THE TABLE FRAME). The value is an int, as
docs/PORT-PYTHON.md's value mapping says; None is 0.
"""

from typing import Any

from . import InvalidValue, Refused
from . import kinds
from .descriptor import FieldDesc


class _Enum:
    def validate(self, fd: FieldDesc, value: Any, path: str) -> None:
        if type(value) is not int:
            raise InvalidValue(path, "not an int")
        if value < fd.lo or value > fd.hi:
            raise InvalidValue(path, f"{value} is outside [{fd.lo}, {fd.hi}]")
        if value != 0 and value not in fd.wire_of:
            raise InvalidValue(path, f"{value} names no variant of {fd.target or fd.name}")

    def read(self, r: Any, fd: FieldDesc, ctx: Any) -> Any:
        reference = r.varint()
        if reference == 0:
            return 0
        if reference > len(ctx.ids):
            raise Refused("wire_damaged", "a variant reference is past the id table")
        value = fd.value_of.get(ctx.ids[reference - 1])
        if value is None:
            ctx.unknown += 1
            return 0
        return value

    def write(self, w: Any, fd: FieldDesc, value: Any, ctx: Any) -> None:
        # the save validated the whole value first: a nonzero value names a
        # variant, so the lookup cannot miss.
        w.varint(ctx.idref(fd.field_id))
        w.byte(fd.kind)
        if value != 0:
            wire_id = fd.wire_of[value]
            w.varint(ctx.idref(wire_id))
            return
        w.varint(0)


kinds.register("enum", _Enum())
