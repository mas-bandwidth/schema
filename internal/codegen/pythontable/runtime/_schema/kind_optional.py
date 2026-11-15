"""The optional field kind (docs/SPEC-TABLES.md §2.3).

`?T` on this wire is PRESENCE: an absent optional writes no field and reads as
None, and a present one rides the wrapped kind's framing, its default included.
The descriptor names the wrapped codec in `inner`, so this module owns the
presence gate and the inner kind owns the bytes; the paths in save.py gate the
node numbering on the same presence, so an absent value owns no node.

An optional ARRAY, `?[N]T` or `?[..N]T`, has `inner` "array": a present list
always rides as the plain array's framing (kind 14: the element kind, the
count, then the elements), a counted one at count zero included and a fixed one
whole, so present-and-empty and absent stay two values. The element descriptor
is `elem`, and each element delegates to its own codec's payload.
"""

from typing import Any

from . import InvalidValue, Refused
from . import descriptor, kind_int, kinds
from .descriptor import FieldDesc

_ELEMENT_KINDS = frozenset(list(range(1, 14)) + [15, 17] + list(range(18, 30)) + [30, 36])


def _elem(fd: FieldDesc) -> FieldDesc:
    elem = fd.elem
    if elem is None:
        raise InvalidValue(fd.name, "an optional array carries no element descriptor")
    return elem


def _zero(elem: FieldDesc) -> Any:
    if elem.codec == "table":
        return descriptor.new_by_name(elem.target)
    return elem.default


def _validate_array(fd: FieldDesc, value: Any, path: str) -> None:
    """Hold a present optional array to its shape and every element to its kind."""
    if type(value) is not list:
        raise InvalidValue(path, "not a list")
    if fd.array_kind == 1:  # [N]T
        if len(value) != fd.bound:
            raise InvalidValue(path, f"a fixed array holds exactly {fd.bound} elements, not {len(value)}")
    elif len(value) < fd.array_min or len(value) > fd.bound:
        raise InvalidValue(path, f"{len(value)} elements is outside [{fd.array_min}, {fd.bound}]")
    elem = _elem(fd)
    if elem.codec == "table":
        target = descriptor.by_name(elem.target)
        for index, item in enumerate(value):
            if target is None or not isinstance(item, target):
                raise InvalidValue(f"{path}[{index}]", f"not a {elem.target}")
        return
    codec = kinds._REGISTRY[elem.codec]
    for index, item in enumerate(value):
        codec.validate(elem, item, f"{path}[{index}]")


def _read_element(r: Any, elem: FieldDesc, ctx: Any) -> Any:
    if elem.codec == "bool":
        raw = r.byte()
        if raw > 1:
            raise Refused("wire_damaged", "a bool payload is not 0 or 1")
        return raw == 1
    return kinds._REGISTRY[elem.codec].read(r, elem, ctx)


def _write_element(w: Any, elem: FieldDesc, item: Any, ctx: Any) -> None:
    if elem.codec == "bool":
        w.byte(1 if item else 0)
        return
    if elem.codec == "int":
        w.raw(int(item).to_bytes(kind_int._width(elem), "little", signed=elem.lo < 0))
        return
    from .save import write_body

    body = bytearray()
    write_body(_writer(body), item, ctx, False)
    w.varint(len(body))
    w.raw(body)


def _writer(buf: bytearray) -> Any:
    from .save import Writer

    return Writer(buf)


def _read_array(r: Any, fd: FieldDesc, ctx: Any) -> Any:
    from .load import Reader

    elem = _elem(fd)
    body = r.take(r.varint())
    if len(body) < 2:
        raise Refused("wire_damaged", "an array body is too short for its header")
    sub = Reader(body, 0, len(body), ctx.ids)
    elem_kind = sub.byte()
    if elem_kind not in _ELEMENT_KINDS:
        raise Refused("wire_damaged", f"element kind {elem_kind} is not in the closed set")
    if elem_kind != elem.kind:
        raise Refused("kind_mismatch", f"{fd.name} rides element kind {elem_kind}, not {elem.kind}")
    count = sub.varint()
    if count > fd.bound:
        raise Refused("count_over_bound", f"{count} elements is past the bound {fd.bound}")
    values: list[Any] = []
    for _ in range(count):
        values.append(_read_element(sub, elem, ctx))
    if sub.pos != sub.end:
        raise Refused("wire_damaged", "bytes left inside an array body")
    if fd.array_kind == 1:  # [N]T
        # An older writer's shorter bound pads with the element's defaults.
        while len(values) < fd.bound:
            values.append(_zero(elem))
    return values


def _write_array(w: Any, fd: FieldDesc, value: Any, ctx: Any) -> None:
    elem = _elem(fd)
    w.varint(ctx.idref(fd.field_id))
    w.byte(fd.kind)
    body = bytearray()
    bw = _writer(body)
    bw.byte(elem.kind)
    bw.varint(len(value))
    for item in value:
        _write_element(bw, elem, item, ctx)
    w.varint(len(body))
    w.raw(body)


class _Optional:
    def _inner(self, fd: FieldDesc) -> Any:
        return kinds._REGISTRY[fd.inner]

    def validate(self, fd: FieldDesc, value: Any, path: str) -> None:
        if value is None:
            return
        if fd.inner == "array":
            _validate_array(fd, value, path)
            return
        self._inner(fd).validate(fd, value, path)

    def read(self, r: Any, fd: FieldDesc, ctx: Any) -> Any:
        if fd.inner == "array":
            return _read_array(r, fd, ctx)
        return self._inner(fd).read(r, fd, ctx)

    def write(self, w: Any, fd: FieldDesc, value: Any, ctx: Any) -> None:
        if fd.inner == "array":
            _write_array(w, fd, value, ctx)
            return
        self._inner(fd).write(w, fd, value, ctx)


kinds.register("optional", _Optional())
