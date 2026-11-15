"""The enum field kind (docs/SPEC-TABLES.md §3, §5, kind 30).

An enum value is not its ordinal on the wire: kind 30's payload is the
reference to the VARIANT NAME's id in the file's id table, and the zero
reference is the enum's None. The variant ids are built at import from the
declaration's wire names, so a variant renamed under `was` keeps the old name's
hash and an old writer's value still names it. The descriptor builds the two
lookups the codec uses once from them, wire_of (value to wire id) and value_of
(wire id to value), so the codec never re-derives a hash and never scans the
variants (the measured choice, docs/PORT-PYTHON.md, THE TABLE FRAME). A
reference past the id table is damage; one this declaration cannot name reads
as None and counts one `unknown`.

A DEPRECATED VARIANT (§2.15) keeps its id and its ordinal: a read that meets it
yields the field's declared default and is never counted unknown, and a strict
write of a value holding it is refused, naming the field and the variant.
"""

from typing import Any

from . import InvalidValue, Refused
from . import kinds
from .descriptor import FieldDesc


class _Enum:
    def validate(self, fd: FieldDesc, value: Any, path: str) -> None:
        if type(value) is not int:
            raise InvalidValue(path, "not an int")
        if value != 0 and value not in fd.wire_of:
            raise InvalidValue(path, f"{value} names no variant")
        if value in fd.deprecated:
            raise InvalidValue(path, f"{value} names a deprecated variant; a strict write refuses it")

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
        if value in fd.deprecated:
            return fd.default
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
