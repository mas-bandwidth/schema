"""The unbounded array field kind (docs/SPEC-TABLES.md §2.9, §3, kind 14).

An unbounded array is a counted array whose count the data decides: kind 14,
the element kind, the count, then the elements. Nothing new rides the wire;
what the declaration drops is the bound. The element is an array element, and
each one reads and writes through its own kind module, so a later kind arrives
without an edit to this file.

The count is held to the read's resource bounds. A body shorter than its own
header, a count above the int32 storage cap, and a count the body cannot cover
refuse before a list is built, and bytes left inside L after the elements are
damage. The one read returns the whole list or raises Refused.
"""

from typing import Any

from . import InvalidValue, Refused
from . import kinds
from .descriptor import FieldDesc

_MAX_COUNT = 0x7FFFFFFF


def _element_kind_allowed(kind: int) -> bool:
    """The array element kinds are closed (docs/SPEC-TABLES.md §3, §4)."""
    return 1 <= kind <= 13 or kind == 15 or kind == 17 or 18 <= kind <= 30


class _Array:
    def validate(self, fd: FieldDesc, value: Any, path: str) -> None:
        if not isinstance(value, list):
            raise InvalidValue(path, "not a list")

    def read(self, r: Any, fd: FieldDesc, ctx: Any) -> Any:
        from .load import Reader

        length = r.varint()
        payload = r.take(length)
        if length < 2:
            raise Refused("wire_damaged", "an array body too short for its header")
        sub = Reader(payload, 0, length, ctx.ids)
        ek = sub.byte()
        if not _element_kind_allowed(ek):
            raise Refused("wire_damaged", "an array element kind outside the closed list")
        if ek != fd.elem_kind:
            raise Refused(
                "kind_mismatch",
                f"{fd.name} rides as an array of kind {ek}, not {fd.elem_kind}",
            )
        count = sub.varint()
        if count > _MAX_COUNT:
            raise Refused(
                "count_over_extent_cap",
                f"an array count of {count} is past the int32 storage cap",
            )
        remaining = length - sub.pos
        if count > remaining:
            raise Refused("wire_damaged", "a count the array body cannot cover")
        element = fd.element
        if element is None:
            raise Refused("unsupported_kind", "an array carries no element descriptor")
        codec = kinds._REGISTRY[element.codec]
        items: list[Any] = []
        append = items.append
        for _ in range(count):
            append(codec.read_element(sub, element, ctx))
        if sub.pos != length:
            raise Refused("wire_damaged", "bytes left inside an array's L after its elements")
        return items

    def write(self, w: Any, fd: FieldDesc, value: Any, ctx: Any) -> None:
        element = fd.element
        if element is None:
            raise InvalidValue(fd.name, "an array carries no element descriptor")
        # THE FIELD'S ID IS INTERNED FIRST: the header rides before the body and
        # first-use order follows the write order (§3), so an element's own ids
        # follow their holder's.
        w.varint(ctx.idref(fd.field_id))
        w.byte(fd.kind)
        body = bytearray()
        writer = _writer(body)
        writer.byte(fd.elem_kind)
        writer.varint(len(value))
        codec = kinds._REGISTRY[element.codec]
        for item in value:
            codec.write_element(writer, element, item, ctx)
        w.varint(len(body))
        w.raw(body)

    def read_element(self, r: Any, fd: FieldDesc, ctx: Any) -> Any:
        raise Refused("unsupported_kind", "an unbounded array cannot be an element")

    def write_element(self, w: Any, fd: FieldDesc, value: Any, ctx: Any) -> None:
        raise InvalidValue(fd.name, "an unbounded array cannot be an element")


def _writer(buf: bytearray) -> Any:
    from .save import Writer

    return Writer(buf)


kinds.register("array", _Array())
