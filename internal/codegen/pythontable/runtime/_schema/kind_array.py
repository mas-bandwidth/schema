"""The array field kind (docs/SPEC-TABLES.md §2.9, §3, kind 14).

A fixed [N]T or counted [..N]T array of a declared type by value (a table or
a packet type, docs/PORT-PYTHON.md) rides the same body with element kind 13,
and its length is held to the declaration on write and on read.

An unbounded array is a counted array whose count the data decides: kind 14,
the element kind, the count, then the elements. Nothing new rides the wire;
what the declaration drops is the bound. The element is an array element, and
each one reads and writes through its own kind module, so a later kind arrives
without an edit to this file.

The count is held to the read's resource bounds. A body shorter than its own
header, a count above the int32 storage cap, and a count the body cannot cover
refuse before a list is built, and bytes left inside L after the elements are
damage. A present plain array with a count of zero is its field's declared
default, which the writer elides, so it is a non-canonical spelling and refuses
(§4, docs/rulings.json reads-refuse-non-canonical); an optional array's
present-empty value is lawful and is not refused here. The one read returns
the whole list or raises Refused. A write refuses a list longer than the cap,
so the writer never emits a count its own read refuses.

THE SHAPE IS MEASURED (docs/PORT-PYTHON.md, THE TABLE FRAME, Bench). An
element of a fixed-width integer kind of 1 to 8 bytes, or a bool, reads and
writes in bulk: one struct call (or one slice) for the whole body, with the
range checked once over the decoded values. Every other element walks its kind
module one element at a time. Both shapes give the same value and the same
bytes; TestPythonVtUnboundedShapeBench holds them to that and times them.
"""

import struct
from typing import Any, cast

from . import InvalidValue, Refused
from . import kinds
from .descriptor import FieldDesc

_MAX_COUNT = 0x7FFFFFFF

_INT_WIDTH = {
    2: 1,
    3: 2,
    4: 4,
    5: 8,
    6: 1,
    7: 2,
    8: 4,
    9: 8,
    20: 1,
    21: 2,
    22: 4,
    23: 8,
    25: 1,
    26: 2,
    27: 4,
    28: 8,
    36: 8,
}

_SIGNED_CODE = {1: "b", 2: "h", 4: "i", 8: "q"}
_UNSIGNED_CODE = {1: "B", 2: "H", 4: "I", 8: "Q"}


def _element_kind_allowed(kind: int) -> bool:
    """The array element kinds are closed (docs/SPEC-TABLES.md §3, §4)."""
    return 1 <= kind <= 13 or kind == 15 or kind == 17 or 18 <= kind <= 30


def _check_count(path: str, count: int, cap: int = _MAX_COUNT) -> None:
    """Refuse a list the int32 storage cap cannot count (§2.9).

    The cap is an argument so a test can hold the check to a small cap without
    building a list of two billion elements.
    """
    if count > cap:
        raise InvalidValue(path, f"a list of {count} elements is past the int32 storage cap")


def _check_shape(fd: FieldDesc, count: int, path: str) -> None:
    """Refuse a write whose length a fixed [N]T or counted [..N]T array does
    not declare; an unbounded []T declares none."""
    if fd.array_kind == 1 and count != fd.bound:
        raise InvalidValue(path, f"{count} elements where {fd.bound} are declared")
    if fd.array_kind == 2 and (count < fd.array_min or (fd.bound and count > fd.bound)):
        raise InvalidValue(path, f"{count} elements outside [{fd.array_min}, {fd.bound}]")


def _int_code(element: FieldDesc) -> str | None:
    if element.codec != "int":
        return None
    width = _INT_WIDTH.get(element.kind)
    if width is None:
        return None
    return _SIGNED_CODE[width] if element.lo < 0 else _UNSIGNED_CODE[width]


def _read_walk(sub: Any, element: FieldDesc, count: int, ctx: Any) -> list[Any]:
    """The per-element shape: each element through its own kind module."""
    codec = cast(kinds.ElementKind, kinds._REGISTRY[element.codec])
    items: list[Any] = []
    append = items.append
    for _ in range(count):
        append(codec.read_element(sub, element, ctx))
    return items


def _read_bulk(sub: Any, element: FieldDesc, count: int, ctx: Any) -> list[Any] | None:
    """The bulk shape, or None for an element it does not carry."""
    if element.codec == "bool":
        raw = bytes(sub.take(count))
        if raw.translate(None, b"\x00\x01"):
            raise Refused("wire_damaged", "a bool payload is not 0 or 1")
        return [b == 1 for b in raw]
    code = _int_code(element)
    if code is None:
        return None
    size = struct.calcsize(code) * count
    chunk = sub.take(size)
    values = struct.unpack(f"<{count}{code}", chunk)
    lo = element.lo
    hi = element.hi
    if values and (min(values) < lo or max(values) > hi):
        bad = next(v for v in values if v < lo or v > hi)
        raise Refused("out_of_range", f"{bad} is outside [{lo}, {hi}]")
    return list(values)


def _write_walk(w: Any, element: FieldDesc, value: list[Any], ctx: Any) -> None:
    """The per-element shape: each element through its own kind module."""
    codec = cast(kinds.ElementKind, kinds._REGISTRY[element.codec])
    for item in value:
        codec.write_element(w, element, item, ctx)


def _write_bulk(w: Any, element: FieldDesc, value: list[Any], ctx: Any) -> bool:
    """The bulk shape; False for an element it does not carry."""
    if element.codec == "bool":
        w.raw(bytes(value))
        return True
    code = _int_code(element)
    if code is None:
        return False
    w.raw(struct.pack(f"<{len(value)}{code}", *value))
    return True


class _Array:
    def validate(self, fd: FieldDesc, value: Any, path: str) -> None:
        if not isinstance(value, list):
            raise InvalidValue(path, "not a list")
        _check_count(path, len(value))
        _check_shape(fd, len(value), path)

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
        if fd.array_kind == 1 and count != fd.bound:
            raise Refused(
                "wire_damaged",
                f"{fd.name} carries {count} elements where {fd.bound} are declared",
            )
        if fd.array_kind == 2 and (count < fd.array_min or (fd.bound and count > fd.bound)):
            raise Refused(
                "wire_damaged",
                f"{fd.name} carries {count} elements outside [{fd.array_min}, {fd.bound}]",
            )
        remaining = length - sub.pos
        if count > remaining:
            raise Refused("wire_damaged", "a count the array body cannot cover")
        element = fd.element
        if element is None:
            raise Refused("unsupported_kind", "an array carries no element descriptor")
        items = _read_bulk(sub, element, count, ctx)
        if items is None:
            items = _read_walk(sub, element, count, ctx)
        if sub.pos != length:
            raise Refused("wire_damaged", "bytes left inside an array's L after its elements")
        if count == 0 and not fd.optional:
            raise Refused(
                "non_canonical",
                f"{fd.name} rides present and empty, its declared default, which the writer elides",
            )
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
        if not _write_bulk(writer, element, value, ctx):
            _write_walk(writer, element, value, ctx)
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
