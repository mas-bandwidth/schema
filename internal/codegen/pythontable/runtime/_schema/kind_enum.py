"""The enum field kind (docs/SPEC-TABLES.md §3, §5, kind 30).

An enum value is not its ordinal on the wire: kind 30's payload is the
reference to the VARIANT NAME's id, and the zero reference is the enum's None.
The variant ids are built at import from the declaration's wire names, so a
variant renamed under `was` keeps the old name's hash and an old writer's value
still names it. A reference this declaration cannot name reads as None and
counts one `unknown`.

A DEPRECATED VARIANT (§2.15) keeps its id and its ordinal: a read that meets it
yields the field's declared default and is never counted unknown, and a strict
write of a value holding it is refused, naming the field and the variant.
"""

from typing import Any

from . import InvalidValue, Refused
from . import kinds
from .descriptor import FieldDesc


def _check(fd: FieldDesc, value: Any, path: str) -> None:
    if type(value) is not int:
        raise InvalidValue(path, "not an int")
    if value < 0 or value > len(fd.variant_ids):
        raise InvalidValue(path, f"{value} names no variant")
    if value in fd.deprecated:
        raise InvalidValue(path, f"{value} names a deprecated variant; a strict write refuses it")


class _Enum:
    def validate(self, fd: FieldDesc, value: Any, path: str) -> None:
        _check(fd, value, path)

    def read(self, r: Any, fd: FieldDesc, ctx: Any) -> Any:
        reference = r.varint()
        if reference == 0:
            return 0
        name_id = r.id_at(reference)
        for ordinal, variant_id in enumerate(fd.variant_ids, 1):
            if variant_id == name_id:
                if ordinal in fd.deprecated:
                    return fd.default
                return ordinal
        ctx.unknown += 1
        return 0

    def write(self, w: Any, fd: FieldDesc, value: Any, ctx: Any) -> None:
        _check(fd, value, fd.name)
        w.varint(ctx.idref(fd.field_id))
        w.byte(fd.kind)
        if value == 0:
            w.varint(0)
            return
        w.varint(ctx.idref(fd.variant_ids[value - 1]))


kinds.register("enum", _Enum())
