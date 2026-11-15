"""The bool field kind (docs/SPEC-TABLES.md §3, kind 1)."""

from typing import Any

from . import InvalidValue, Refused
from . import kinds
from .descriptor import FieldDesc


class _Bool:
    def validate(self, fd: FieldDesc, value: Any, path: str) -> None:
        if type(value) is not bool:
            raise InvalidValue(path, "not a bool")

    def read(self, r: Any, fd: FieldDesc, ctx: Any) -> Any:
        raw = r.byte()
        if raw > 1:
            raise Refused("wire_damaged", "a bool payload is not 0 or 1")
        return raw == 1

    def write(self, w: Any, fd: FieldDesc, value: Any, ctx: Any) -> None:
        w.varint(ctx.idref(fd.field_id))
        w.byte(fd.kind)
        w.byte(1 if value else 0)


kinds.register("bool", _Bool())
