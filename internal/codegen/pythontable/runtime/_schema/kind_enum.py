"""The enum field kind (docs/SPEC-TABLES.md §3, kind 30; §5).

An enum rides under its own wire kind and its payload is the reference to the
variant NAME's id in the file's id table. The descriptor carries the variants
as (value, wire id) pairs, so the codec never re-derives a hash. The value is
an int, as docs/PORT-PYTHON.md's value mapping says; None is 0.
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
        if value != 0 and not any(v == value for v, _ in fd.variants):
            raise InvalidValue(path, f"{value} names no variant of {fd.target or fd.name}")

    def read(self, r: Any, fd: FieldDesc, ctx: Any) -> Any:
        reference = r.varint()
        if reference == 0:
            return 0
        if reference > len(ctx.ids):
            raise Refused("wire_damaged", "a variant reference is past the id table")
        wire_id = ctx.ids[reference - 1]
        for value, variant_id in fd.variants:
            if variant_id == wire_id:
                return value
        ctx.unknown += 1
        return 0

    def write(self, w: Any, fd: FieldDesc, value: Any, ctx: Any) -> None:
        if value != 0:
            for variant, wire_id in fd.variants:
                if variant == value:
                    w.varint(ctx.idref(fd.field_id))
                    w.byte(fd.kind)
                    w.varint(ctx.idref(wire_id))
                    return
            raise InvalidValue(fd.name, f"{value} names no variant")
        w.varint(ctx.idref(fd.field_id))
        w.byte(fd.kind)
        w.varint(0)


kinds.register("enum", _Enum())
