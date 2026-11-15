"""The array field kind (docs/SPEC-TABLES.md §3, kind 14).

An array of a declared type is a kind-14 body whose element kind is 13: each
element is its own length-prefixed table body. That is how a packet type by
value in an array rides the form-1 table wire (docs/PORT-PYTHON.md, THE TABLE
FRAME), so the element is read and written by the same body walk a nested table
uses. An array of any other element kind is outside this cut and refuses by
name.
"""

from typing import Any

from . import InvalidValue, Refused
from . import descriptor, kinds
from .descriptor import FieldDesc


def _target(fd: FieldDesc) -> type[Any]:
    cls = descriptor.by_name(fd.target)
    if cls is None:
        raise InvalidValue(fd.name, f"the table {fd.target} is not registered")
    return cls


class _Array:
    def validate(self, fd: FieldDesc, value: Any, path: str) -> None:
        if type(value) is not list:
            raise InvalidValue(path, "not a list")
        length = len(value)
        if fd.array_kind == 1 and length != fd.bound:
            raise InvalidValue(path, f"{length} elements where {fd.bound} are declared")
        if fd.array_kind == 2 and (length < fd.array_min or (fd.bound and length > fd.bound)):
            raise InvalidValue(path, f"{length} elements outside [{fd.array_min}, {fd.bound}]")
        target = _target(fd)
        from .save import _validate

        for index, element in enumerate(value):
            where = f"{path}[{index}]"
            if not isinstance(element, target):
                raise InvalidValue(where, f"not a {fd.target}")
            _validate(element, element.descriptor, where, set())

    def read(self, r: Any, fd: FieldDesc, ctx: Any) -> Any:
        from .load import Reader, read_body

        length = r.varint()
        body = r.take(length)
        reader = Reader(body, 0, len(body), ctx.ids)
        elem_kind = reader.byte()
        if elem_kind != fd.elem_kind:
            raise Refused(
                "kind_mismatch",
                f"{fd.name} carries elements under kind {elem_kind}, not {fd.elem_kind}",
            )
        count = reader.varint()
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
        cls = _target(fd)
        out: list[Any] = []
        for _ in range(count):
            element_length = reader.varint()
            payload = reader.take(element_length)
            element = cls()
            read_body(payload, element, ctx)
            out.append(element)
        if reader.pos != reader.end:
            raise Refused("wire_damaged", f"bytes left inside {fd.name}'s array body")
        return out

    def write(self, w: Any, fd: FieldDesc, value: Any, ctx: Any) -> None:
        from .save import Writer, write_body

        # The field's id is interned before the body's, as the nested table's
        # is (docs/SPEC-TABLES.md §3).
        w.varint(ctx.idref(fd.field_id))
        w.byte(fd.kind)
        body = bytearray()
        elements = Writer(body)
        elements.byte(fd.elem_kind)
        elements.varint(len(value))
        for element in value:
            payload = bytearray()
            write_body(Writer(payload), element, ctx, False)
            elements.varint(len(payload))
            elements.raw(payload)
        w.varint(len(body))
        w.raw(body)


kinds.register("array", _Array())
