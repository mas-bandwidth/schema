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
# load and save import this module for its registration; binding them as
# modules (not their names) resolves at call time, so the hot paths pay
# no per-call import.
from . import load as _load
from . import save as _save
from .descriptor import ArmDesc, FieldDesc

# The kinds whose FIELD payload begins with its own length: an arm holds the
# bytes after that length, so the arm's L is that length.
_LENGTH_KINDS = frozenset((12, 13, 14, 16, 31, 32))


def _union_class(name: str) -> type[Any]:
    cls = descriptor._union_by_name(name)
    if cls is None:
        raise InvalidValue(name, f"the union {name} is not registered")
    return cls


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


def _read_union_payload(r: Any, un: type[Any], ctx: Any) -> Any:
    reference = r.varint()
    if reference == 0:
        # The empty arm reads as None, in a field as in an array element: the
        # Go engine reads a union field's zero reference so (and writes it so
        # for a required union). The spec names a field's arm 0 a non-canonical
        # spelling; refusing it is the cross-leg tightening's, not this leg's
        # alone.
        return un()
    arm_id = r.id_at(reference)
    arm = un.descriptor.by_id.get(arm_id)
    kind = r.byte()
    framed = r.pos
    length = r.varint()
    start = r.pos
    payload = r.take(length)
    if arm is None:
        # An arm this reader cannot name reads as None, counted unknown, and
        # its payload is judged as the engine's wire check judges any skipped
        # value (internal/tablewire/wirecheck.go, union and content).
        _load.check_content(payload, kind, ctx.ids)
        ctx.unknown += 1
        return un()
    if kind != arm.kind:
        raise Refused(
            "kind_mismatch",
            f"{un.descriptor.name}.{arm.name} rides under kind {kind}, not {arm.kind}",
        )
    value = un()
    value.type = arm.tag
    if arm.field is None:
        if length != 0:
            raise Refused("wire_damaged", "kind 32 with a nonzero L")
        return value
    previous = ctx.current
    ctx.current = value
    try:
        loaded = _read_arm_payload(r, framed, start, arm.field, ctx)
    finally:
        ctx.current = previous
    setattr(value, arm.name, loaded)
    return value


def _read_arm_payload(r: Any, framed: int, start: int, f: FieldDesc, ctx: Any) -> Any:
    """Read one arm's payload in place, out of the reader that framed it.

    A length kind's field payload begins with its own L, which is the arm's L,
    so the field codec reads from the arm's L onward; every other kind reads
    the bytes after it. Both are bounded to the arm's end, never copied.
    """
    sub = _load.Reader(r.data, framed if f.kind in _LENGTH_KINDS else start, r.pos, ctx.ids)
    if f.codec == "union":
        value = _read_union_payload(sub, _union_class(f.target), ctx)
    else:
        value = kinds._REGISTRY[f.codec].read(sub, f, ctx)
    if sub.pos != sub.end:
        raise Refused("wire_damaged", "bytes left inside an arm's L after its payload")
    return value


def _write_arm_header(w: Any, arm: ArmDesc, value: Any, ctx: Any) -> None:
    if arm.field is None:
        w.varint(ctx.idref(arm.arm_id))
        w.byte(arm.kind)
        w.varint(0)
        return
    f = arm.field
    if f.kind in _LENGTH_KINDS:
        # The arm's field line IS the arm header: the field id is the arm id,
        # and the field's own L is the arm's L, so the codec writes in place.
        kinds._REGISTRY[f.codec].write(w, f, value, ctx)
        return
    ctx.idref(arm.arm_id)
    body = bytearray()
    kinds._REGISTRY[f.codec].write(_writer(body), f, value, ctx)
    end = _header_end(body)
    payload = body[end:]
    w.raw(body[:end])
    w.varint(len(payload))
    w.raw(payload)


def _writer(buf: bytearray) -> Any:
    return _save.Writer(buf)


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
    _validate_arm_value(getattr(value, arm.name), arm.field, f"{path}.{arm.name}", seen)


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
    if f.codec == "pointer":
        if value is None:
            return
        target = descriptor.by_name(f.target)
        if target is None or not isinstance(value, target):
            raise InvalidValue(path, f"not a {f.target} or None")
        _save._validate(value, value.descriptor, path, seen)
        return
    if f.codec == "table":
        target = descriptor.by_name(f.target)
        if target is None or not isinstance(value, target):
            raise InvalidValue(path, f"not a {f.target}")
        _save._validate(value, value.descriptor, path, seen)
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
        _save._collect(value, ctx)
        ctx.onstack.discard(key)
        return
    if f.codec == "table":
        _save._collect(value, ctx)
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
        return _read_union_payload(r, _union_class(fd.target), ctx)

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
        length = r.varint()
        payload = r.take(length)
        sub = _load.Reader(payload, 0, len(payload), ctx.ids)
        element_kind = sub.byte()
        if element_kind != 15:
            raise Refused(
                "kind_mismatch",
                f"{fd.name} carries element kind {element_kind}, not 15",
            )
        count = sub.varint()
        # A count past the int32 storage cap is past every bound too; the
        # refusal path's file walk (load.check_file) names it
        # count_over_extent_cap, as the engine's wire check does.
        if count > fd.bound:
            raise Refused("count_over_bound", f"{fd.name} counts {count}, past its bound {fd.bound}")
        un = _union_class(fd.target)
        # A counted array below its declared minimum is read as it stands, as
        # the engine reads it; the minimum binds the writer (save refuses it).
        values = [un() for _ in range(fd.bound)] if fd.array_fixed else []
        for index in range(count):
            value = _read_union_payload(sub, un, ctx)
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
