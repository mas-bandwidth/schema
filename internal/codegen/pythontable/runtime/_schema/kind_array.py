"""The fixed and counted array field kinds (docs/SPEC-TABLES.md §3, kind 14).

A body is the element kind, the count, then the elements back to back. A
fixed `[N]T` carries exactly N slots; a counted `[A..N]T` carries a count in
[A, N]. A read builds the whole list or refuses; a write validates every
element before it writes a byte.
"""

from typing import Any

from . import InvalidValue, Refused
from . import kinds
from .descriptor import FieldDesc, new_by_name

_ALLOWED = frozenset(
    list(range(1, 14)) + [15, 17] + list(range(18, 30)) + [30]
)


def _zero(fd: FieldDesc) -> Any:
    elem = fd.elem
    if elem is None:
        raise InvalidValue(fd.name, "an array carries no element descriptor")
    if elem.codec == "table":
        return new_by_name(elem.target)
    if elem.codec == "bytes":
        return b""
    return elem.default


class _Array:
    def validate(self, fd: FieldDesc, value: Any, path: str) -> None:
        if type(value) is not list:
            raise InvalidValue(path, "not a list")
        if fd.array_kind == "fixed":
            if len(value) != fd.bound:
                raise InvalidValue(path, f"a fixed array holds exactly {fd.bound} elements")
        elif len(value) < fd.array_min or len(value) > fd.bound:
            raise InvalidValue(path, f"{len(value)} elements is outside [{fd.array_min}, {fd.bound}]")
        elem = fd.elem
        if elem is None:
            raise InvalidValue(path, "an array carries no element descriptor")
        for index, item in enumerate(value):
            where = f"{path}[{index}]"
            if elem.codec == "table":
                from .descriptor import by_name

                target = by_name(elem.target)
                if target is None or type(item) is not target:
                    raise InvalidValue(where, f"not a {elem.target}")
                from .save import _validate

                _validate(item, item.descriptor, where, set())
            else:
                kinds._REGISTRY[elem.codec].validate(elem, item, where)

    def read(self, r: Any, fd: FieldDesc, ctx: Any) -> Any:
        from .load import Reader

        elem = fd.elem
        if elem is None:
            raise Refused("wire_damaged", "an array carries no element descriptor")
        body = r.take(r.varint())
        if len(body) < 2:
            raise Refused("wire_damaged", "an array body is too short for its header")
        sub = Reader(body, 0, len(body), ctx.ids)
        elem_kind = sub.byte()
        if elem_kind not in _ALLOWED:
            raise Refused("wire_damaged", f"element kind {elem_kind} is not in the closed set")
        if elem_kind != elem.kind:
            raise Refused("kind_mismatch", f"{fd.name} rides element kind {elem_kind}, not {elem.kind}")
        count = sub.varint()
        if count > fd.bound:
            raise Refused("count_over_bound", f"{count} elements is past the bound {fd.bound}")
        codec = kinds._REGISTRY[elem.codec]
        if fd.array_kind == "fixed":
            values = [_zero(fd) for _ in range(fd.bound)]
            for index in range(count):
                values[index] = codec.read(sub, elem, ctx)
        else:
            values = []
            for _ in range(count):
                values.append(codec.read(sub, elem, ctx))
        if sub.pos != len(body):
            raise Refused("wire_damaged", "bytes left inside an array body")
        return values

    def write(self, w: Any, fd: FieldDesc, value: Any, ctx: Any) -> None:
        w.varint(ctx.idref(fd.field_id))
        w.byte(fd.kind)
        self.write_value(w, fd, value, ctx)

    def write_value(self, w: Any, fd: FieldDesc, value: Any, ctx: Any) -> None:
        from .save import Writer

        elem = fd.elem
        if elem is None:
            raise InvalidValue(fd.name, "an array carries no element descriptor")
        body = bytearray()
        bw = Writer(body)
        bw.byte(elem.kind)
        count = fd.bound if fd.array_kind == "fixed" else len(value)
        bw.varint(count)
        codec = kinds._REGISTRY[elem.codec]
        for item in value:
            codec.write_value(bw, elem, item, ctx)
        w.varint(len(body))
        w.raw(body)


kinds.register("array", _Array())
