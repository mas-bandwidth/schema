"""The pointer-array field kind (docs/SPEC-TABLES.md §2.1, §2.9, §3, §3.1).

A []*T, [N]*T, [..N]*T, or [Min..N]*T rides as a kind-14 array body whose
element kind is 17 (pointer) and whose elements are canonical unsigned LEB128
indices into the file's one flat node table (0 for None, 2..N+1 for heap
records).
"""

from typing import Any

from . import InvalidValue, Refused
from . import descriptor, kinds
from .descriptor import FieldDesc, TableDesc

_ELEM_KIND_POINTER = 17
_MAX_COUNT = 0x7FFFFFFF
_ARRAY_ELEMENT_KINDS: frozenset[int] = frozenset(
    {*range(1, 14), 15, 17, *range(18, 31)}
)


def _target(fd: FieldDesc) -> type[Any]:
    cls = descriptor.by_name(fd.target)
    if cls is None:
        raise InvalidValue(fd.name, f"the table {fd.target} is not registered")
    return cls


def _put_varint(buf: bytearray, value: int) -> None:
    if value < 0:
        raise InvalidValue("", "a reference is negative")
    while True:
        byte = value & 0x7F
        value >>= 7
        if value:
            buf.append(byte | 0x80)
        else:
            buf.append(byte)
            return


class _PointerArray:
    def validate(self, fd: FieldDesc, value: Any, path: str) -> None:
        if type(value) is not list:
            raise InvalidValue(path, "not a list")
        n = len(value)
        if fd.array_mode == "fixed":
            if n != fd.bound:
                raise InvalidValue(path, f"length {n} is not {fd.bound}")
        elif fd.array_mode == "counted":
            if n < fd.min_count or n > fd.bound:
                raise InvalidValue(
                    path, f"length {n} is outside [{fd.min_count}, {fd.bound}]"
                )
        elif n > _MAX_COUNT:
            raise InvalidValue(path, "length exceeds int32 storage cap")
        target = _target(fd)
        for index, elem in enumerate(value):
            if elem is None or isinstance(elem, target):
                continue
            where = f"{path}[{index}]"
            other = getattr(elem, "descriptor", None)
            if isinstance(other, TableDesc):
                raise InvalidValue(where, f"{other.name} is not a {fd.target} or None")
            raise InvalidValue(where, f"not a {fd.target} or None")

    def read(self, r: Any, fd: FieldDesc, ctx: Any) -> Any:
        length: int = r.varint()
        payload = r.take(length)
        if length < 2:
            raise Refused("wire_damaged", "an array body too short for its header")
        sub = r.__class__(payload, 0, length, r.ids)
        ek: int = sub.byte()
        count: int = sub.varint()
        if ek not in _ARRAY_ELEMENT_KINDS:
            raise Refused(
                "wire_damaged", "an array element kind outside the closed list"
            )
        if ek != _ELEM_KIND_POINTER:
            raise Refused(
                "kind_mismatch",
                f"{fd.name}: array element kind {ek} is not {_ELEM_KIND_POINTER}",
            )
        if count > _MAX_COUNT:
            raise Refused(
                "count_over_extent_cap",
                f"{fd.name}: count {count} exceeds int32 storage cap",
            )
        if fd.array_mode != "list" and count > fd.bound:
            raise Refused(
                "count_over_bound",
                f"{fd.name}: count {count} exceeds bound {fd.bound}",
            )
        alloc_len = fd.bound if fd.array_mode == "fixed" else count
        out: list[Any] = [None] * alloc_len
        for slot in range(count):
            idx: int = sub.varint()
            ctx.pending.append((out, (slot, fd), idx))
            ctx.edges.append(idx)
        if sub.pos != sub.end:
            raise Refused(
                "wire_damaged", "bytes left inside an array after its elements"
            )
        return out

    def write(self, w: Any, fd: FieldDesc, value: Any, ctx: Any) -> None:
        payload = bytearray()
        payload.append(_ELEM_KIND_POINTER)
        _put_varint(payload, len(value))
        for index, elem in enumerate(value):
            if elem is None:
                payload.append(0)
            else:
                node_idx = ctx.indices.get(id(elem))
                if node_idx is None:
                    raise InvalidValue(
                        f"{fd.name}[{index}]", "the pointer target has no node index"
                    )
                _put_varint(payload, node_idx)
        w.varint(ctx.idref(fd.field_id))
        w.byte(fd.kind)
        w.varint(len(payload))
        w.raw(payload)


kinds.register("pointer_array", _PointerArray())
