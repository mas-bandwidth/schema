"""The enum field kind (docs/SPEC-TABLES.md §3, kind 30).

An enum value is a plain int ordinal. On the wire it rides as the reference to
its VARIANT NAME's id, with 0 for None. A reference this build cannot name is
unknown: the field reads None and the load counts one.
"""

from typing import Any

from . import InvalidValue, Refused
from . import kinds
from .descriptor import FieldDesc


class _Enum:
    def validate(self, fd: FieldDesc, value: Any, path: str) -> None:
        if type(value) is not int:
            raise InvalidValue(path, "not an int")
        if value < 0 or value > fd.hi:
            raise InvalidValue(path, f"{value} is outside [0, {fd.hi}]")
        if value != 0 and value > len(fd.variants):
            raise InvalidValue(path, f"{value} names no variant")

    def read(self, r: Any, fd: FieldDesc, ctx: Any) -> Any:
        reference = r.varint()
        if reference == 0:
            return 0
        variant_id = r.id_at(reference)
        for index, (_, wire_id) in enumerate(fd.variants):
            if wire_id == variant_id:
                return index + 1
        ctx.unknown += 1
        return 0

    def write(self, w: Any, fd: FieldDesc, value: Any, ctx: Any) -> None:
        w.varint(ctx.idref(fd.field_id))
        w.byte(fd.kind)
        self.write_value(w, fd, value, ctx)

    def write_value(self, w: Any, fd: FieldDesc, value: Any, ctx: Any) -> None:
        if value == 0:
            w.varint(0)
            return
        w.varint(ctx.idref(fd.variants[value - 1][1]))


kinds.register("enum", _Enum())
