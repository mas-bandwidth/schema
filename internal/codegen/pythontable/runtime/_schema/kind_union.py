"""The table-side union field kinds (docs/SPEC-TABLES.md §2.6, §3).

A union is an arm id reference, then the selected arm's header: its kind, its
length and the arm's payload. An arm is a FIELD LINE, so its payload is the
bytes a field of that type puts after its own framing prefix. The arm's length
stands in for the field's own length where the kind has one and frames the
fixed width where it has not.

An array of unions (kind 14 over element kind 15) is one union payload per
element in place, a `None` element as the single zero reference.
"""

from typing import Any

from . import InvalidValue, Refused
from . import descriptor, kinds
from .descriptor import ArmDesc, FieldDesc

# The kinds whose FIELD payload begins with its own length: an arm holds the
# bytes after that length, so the arm's L is that length.
_LENGTH_KINDS = frozenset((12, 13, 14, 16, 31, 32))


def _union_class(name: str) -> type[Any]:
    cls = descriptor._union_by_name(name)
    if cls is None:
        raise InvalidValue(name, f"the union {name} is not registered")
    return cls


def _varint_bytes(value: int) -> bytes:
    out = bytearray()
    while True:
        byte = value & 0x7F
        value >>= 7
        if value:
            out.append(byte | 0x80)
        else:
            out.append(byte)
            return bytes(out)


def _header_end(data: bytearray) -> int:
    index = 0
    while True:
        raw = data[index]
        index += 1
        if not raw & 0x80:
            return index + 1


def _arm(un: type[Any], tag: int, path: str) -> ArmDesc:
    arms: tuple[ArmDesc, ...] = un.descriptor.arms
    if type(tag) is not int or tag < 1 or tag > len(arms):
        raise InvalidValue(path, f"type {tag!r} names no arm")
    return arms[tag - 1]


def _read_union_payload(r: Any, un: type[Any], ctx: Any, allow_none: bool) -> Any:
    from .load import Reader

    reference = r.varint()
    if reference == 0:
        if not allow_none:
            raise Refused("wire_damaged", "a union field carries the empty arm")
        return un()
    arm_id = r.id_at(reference)
    arm = un.descriptor.by_id.get(arm_id)
    if arm is None:
        kind = r.byte()
        length = r.varint()
        r.take(length)
        ctx.unknown += 1
        return un()
    kind = r.byte()
    length = r.varint()
    payload = r.take(length)
    if kind != arm.kind:
        raise Refused(
            "kind_mismatch",
            f"{un.descriptor.name}.{arm.name} rides under kind {kind}, not {arm.kind}",
        )
    value = un()
    value.type = arm.tag
    if arm.field is None:
        if kind != 32:
            raise Refused(
                "kind_mismatch",
                f"{un.descriptor.name}.{arm.name} rides under kind {kind}, not 32",
            )
        if length != 0:
            raise Refused("wire_damaged", "a payload-free arm carries bytes")
        return value
    previous = ctx.current
    ctx.current = value
    try:
        loaded = _read_arm_payload(payload, arm.field, ctx)
    finally:
        ctx.current = previous
    setattr(value, arm.name, loaded)
    return value


def _read_arm_payload(payload: Any, f: FieldDesc, ctx: Any) -> Any:
    from .load import Reader

    codec = kinds._REGISTRY[f.codec]
    if f.codec == "union":
        sub = Reader(payload, 0, len(payload), ctx.ids)
        inner = _read_union_payload(sub, _union_class(f.target), ctx, True)
        if sub.pos != sub.end:
            raise Refused("wire_damaged", "bytes left inside a union arm")
        return inner
    if f.kind in _LENGTH_KINDS:
        framed = memoryview(_varint_bytes(len(payload)) + bytes(payload))
        sub = Reader(framed, 0, len(framed), ctx.ids)
        value = codec.read(sub, f, ctx)
        if sub.pos != sub.end:
            raise Refused("wire_damaged", "bytes left inside a union arm")
        return value
    sub = Reader(payload, 0, len(payload), ctx.ids)
    value = codec.read(sub, f, ctx)
    if sub.pos != sub.end:
        raise Refused("wire_damaged", "bytes left inside a union arm")
    return value


def _write_arm_header(w: Any, arm: ArmDesc, value: Any, ctx: Any) -> None:
    if arm.field is None:
        w.varint(ctx.idref(arm.arm_id))
        w.byte(arm.kind)
        w.varint(0)
        return
    f = arm.field
    ctx.idref(arm.arm_id)
    body = bytearray()
    kinds._REGISTRY[f.codec].write(_writer(body), f, value, ctx)
    if f.kind in _LENGTH_KINDS:
        w.raw(body)
        return
    end = _header_end(body)
    payload = body[end:]
    w.raw(body[:end])
    w.varint(len(payload))
    w.raw(payload)


def _writer(buf: bytearray) -> Any:
    from .save import Writer

    return Writer(buf)


def _write_union_element(w: Any, un: type[Any], value: Any, ctx: Any) -> None:
    if value.type == 0:
        w.varint(0)
        return
    arm = _arm(un, value.type, un.descriptor.name)
    payload = None if arm.field is None else getattr(value, arm.name)
    _write_arm_header(w, arm, payload, ctx)


def _validate_union(value: Any, name: str, path: str, seen: set[int]) -> None:
    un = _union_class(name)
    if not isinstance(value, un):
        raise InvalidValue(path, f"not a {name}")
    tag = value.type
    if type(tag) is not int or tag < 0 or tag > len(un.descriptor.arms):
        raise InvalidValue(path, f"type {tag!r} names no arm")
    if tag == 0:
        return
    arm = un.descriptor.arms[tag - 1]
    if arm.field is None:
        return
    _validate_arm_value(getattr(value, arm.name), arm.field, path, seen)


def _validate_union_array(value: Any, f: FieldDesc, path: str, seen: set[int]) -> None:
    if not isinstance(value, list):
        raise InvalidValue(path, "not a list")
    if f.array_fixed:
        if len(value) != f.bound:
            raise InvalidValue(path, f"a fixed array of {f.bound} holds {len(value)}")
    elif len(value) < f.array_min or len(value) > f.bound:
        raise InvalidValue(path, f"a counted array holds {len(value)}, outside [{f.array_min}, {f.bound}]")
    for index, element in enumerate(value):
        _validate_union(element, f.target, f"{path}[{index}]", seen)


def _validate_arm_value(value: Any, f: FieldDesc, path: str, seen: set[int]) -> None:
    from .save import _validate as validate_table

    if f.codec == "pointer":
        if value is None:
            return
        target = descriptor.by_name(f.target)
        if target is None or not isinstance(value, target):
            raise InvalidValue(path, f"not a {f.target} or None")
        validate_table(value, value.descriptor, path, seen)
        return
    if f.codec == "table":
        target = descriptor.by_name(f.target)
        if target is None or not isinstance(value, target):
            raise InvalidValue(path, f"not a {f.target}")
        validate_table(value, value.descriptor, path, seen)
        return
    if f.codec == "union":
        _validate_union(value, f.target, path, seen)
        return
    if f.codec == "union_array":
        _validate_union_array(value, f, path, seen)
        return
    kinds._REGISTRY[f.codec].validate(f, value, path)


def _collect_union(value: Any, ctx: Any) -> None:
    tag = value.type
    if tag == 0:
        return
    arms = type(value).descriptor.arms
    arm = arms[tag - 1]
    if arm.field is None:
        return
    _collect_arm_value(getattr(value, arm.name), arm.field, ctx)


def _collect_union_array(value: Any, ctx: Any) -> None:
    for element in value:
        _collect_union(element, ctx)


def _collect_arm_value(value: Any, f: FieldDesc, ctx: Any) -> None:
    from .save import _collect as collect_table

    if f.codec == "pointer":
        if value is None:
            return
        key = id(value)
        if key in ctx.indices:
            if key in ctx.onstack:
                raise InvalidValue(f.name, "a pointer cycle has no wire form")
            return
        ctx.indices[key] = len(ctx.order) + 2
        ctx.order.append(value)
        ctx.onstack.add(key)
        collect_table(value, ctx)
        ctx.onstack.discard(key)
        return
    if f.codec == "table":
        collect_table(value, ctx)
        return
    if f.codec == "union":
        _collect_union(value, ctx)
        return
    if f.codec == "union_array":
        _collect_union_array(value, ctx)


class _Union:
    def validate(self, fd: FieldDesc, value: Any, path: str) -> None:
        _validate_union(value, fd.target, path, set())

    def read(self, r: Any, fd: FieldDesc, ctx: Any) -> Any:
        return _read_union_payload(r, _union_class(fd.target), ctx, False)

    def write(self, w: Any, fd: FieldDesc, value: Any, ctx: Any) -> None:
        w.varint(ctx.idref(fd.field_id))
        w.byte(fd.kind)
        if value.type == 0:
            w.varint(0)
            return
        arm = _arm(_union_class(fd.target), value.type, fd.name)
        payload = None if arm.field is None else getattr(value, arm.name)
        _write_arm_header(w, arm, payload, ctx)


class _UnionArray:
    def validate(self, fd: FieldDesc, value: Any, path: str) -> None:
        _validate_union_array(value, fd, path, set())

    def read(self, r: Any, fd: FieldDesc, ctx: Any) -> Any:
        from .load import Reader

        length = r.varint()
        payload = r.take(length)
        sub = Reader(payload, 0, len(payload), ctx.ids)
        element_kind = sub.byte()
        if element_kind != 15:
            raise Refused(
                "kind_mismatch",
                f"{fd.name} carries element kind {element_kind}, not 15",
            )
        count = sub.varint()
        if count > fd.bound:
            raise Refused("out_of_range", f"an array of {count} is past its bound {fd.bound}")
        un = _union_class(fd.target)
        if fd.array_fixed:
            values = [un() for _ in range(fd.bound)]
        else:
            if count < fd.array_min:
                raise Refused("out_of_range", f"an array of {count} is below its minimum {fd.array_min}")
            values = []
        for index in range(count):
            value = _read_union_payload(sub, un, ctx, True)
            if fd.array_fixed:
                values[index] = value
            else:
                values.append(value)
        if sub.pos != sub.end:
            raise Refused("wire_damaged", "bytes left inside an array of unions")
        return values

    def write(self, w: Any, fd: FieldDesc, value: Any, ctx: Any) -> None:
        reference = ctx.idref(fd.field_id)
        body = bytearray()
        bw = _writer(body)
        bw.byte(15)
        bw.varint(len(value))
        un = _union_class(fd.target)
        for element in value:
            _write_union_element(bw, un, element, ctx)
        w.varint(reference)
        w.byte(fd.kind)
        w.varint(len(body))
        w.raw(body)


kinds.register("union", _Union())
kinds.register("union_array", _UnionArray())
