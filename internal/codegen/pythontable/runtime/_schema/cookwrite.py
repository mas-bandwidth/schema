"""The cooked form's write side (docs/SPEC-TABLES.md §7, §7.1, §7.2, §7.6).

Cook writes the same cooked file `schema cook` writes from the same wire: the
header of §7.1, the region of §7.2 (the depth-first pre-order numbering of §3.1,
every node at align_up(offset, alignof) for its own type, every field written
piece by piece at the layout the descriptor names), and the attribution part of
§7.6. Every byte no field covers is zero. CookMeasure answers that file's exact
length.

The byte order is a parameter of the write, never a fact of the host: order 1 is
little-endian and order 2 big-endian, the two values the header's order word
carries (§7.6, §19.1).

A data cycle is refused, and a pointer the numbering does not carry refuses too:
a cook is never a partial region.
"""

import struct
from copy import copy as _copy
from types import SimpleNamespace
from typing import Any, Final, Iterator, Literal

from . import BlobRef, InvalidValue
from . import descriptor, kind_blob, kind_map
from .save import _validate, _validate_fields

MAGIC: Final = 0x4B4F4F434D484353
HEADER_BYTES: Final = 64
DIRECTORY_ENTRY: Final = 16
REGION_ALIGN_FLOOR: Final = 8
ORDER_LITTLE: Final = 1
ORDER_BIG: Final = 2

_RETIRED: Final = "the Python table runtime does not carry this construct yet (docs/PORT-PYTHON.md, THE CARD MAP)"

_WIDTH: Final = {
    2: 1,
    3: 2,
    4: 4,
    5: 8,
    6: 1,
    7: 2,
    8: 4,
    9: 8,
    18: 16,
    19: 16,
    20: 1,
    21: 2,
    22: 4,
    23: 8,
    24: 16,
    25: 1,
    26: 2,
    27: 4,
    28: 8,
    29: 16,
    36: 8,
}


class _Entry:
    __slots__ = ("cls", "value", "desc", "offset", "blob", "type_id", "blobs", "placements")

    cls: type[Any]
    value: Any
    desc: Any
    offset: int

    def __init__(self, cls: type[Any], value: Any, desc: Any) -> None:
        self.cls = cls
        self.value = value
        self.desc = desc
        self.offset = 0
        self.blob: kind_blob._BlobNode | None = None
        self.type_id = desc.type_id if desc is not None else 0
        self.blobs: list[_Entry] = []
        self.placements: dict[int, int] = {}


def _align_up(n: int, a: int) -> int:
    if a <= 1:
        return n
    return (n + a - 1) // a * a


def _width(fd: Any) -> int:
    width = _WIDTH.get(fd.kind)
    if width is None:
        raise InvalidValue(fd.name, f"kind {fd.kind} is not an integer kind")
    return width


def _put_uint(buf: bytearray, at: int, value: int, width: int, order: Literal["little", "big"]) -> None:
    buf[at : at + width] = int(value).to_bytes(width, order, signed=False)


def _put_int(buf: bytearray, at: int, value: int, width: int, order: Literal["little", "big"], signed: bool) -> None:
    buf[at : at + width] = int(value).to_bytes(width, order, signed=signed)


def _put_wide(buf: bytearray, at: int, value: int, order: Literal["little", "big"], signed: bool) -> None:
    buf[at : at + 16] = int(value).to_bytes(16, order, signed=signed)


def _optional_field(fd: Any) -> Any:
    field = _copy(fd)
    field.codec = fd.inner
    field.optional = False
    field.guards = ()
    field.at = 0
    field.pieces = tuple((offset - fd.at, size) for offset, size in fd.pieces[:-1])
    if field.codec == "array" and field.elem is not None:
        field.elem_codec = field.elem.codec
    return field


def _union_field(fd: Any) -> Any:
    return SimpleNamespace(name=fd.name, codec="union", target=fd.target, at=0)


def _text_entry(fd: Any, value: Any) -> Any:
    target = descriptor.by_name(fd.target)
    if target is None:
        raise InvalidValue(fd.name, "a text-list element is not registered")
    entry = target()
    entry.value = value
    return entry


def _map_entries(fd: Any, value: Any) -> list[Any]:
    target = descriptor.by_name(fd.target)
    if target is None:
        raise InvalidValue(fd.name, "a map entry is not registered")
    key, val = target.descriptor.fields
    entries = []
    for item in kind_map._sorted_keys(fd, value):
        entry = target()
        setattr(entry, key.name, item)
        setattr(entry, val.name, value[item])
        entries.append(entry)
    return entries


def _edges(desc: Any, value: Any, out: list[tuple[Any, Any]], inactive_only: bool = False, inherited_inactive: bool = False) -> None:
    for fd in desc.fields:
        inactive = inherited_inactive or (hasattr(fd, "guard_holds") and not fd.guard_holds(value))
        if inactive and not inactive_only:
            continue
        if fd.codec == "table":
            target = descriptor.by_name(fd.target)
            if target is None:
                raise InvalidValue(fd.name, f"the table {fd.target} is not registered")
            _edges(target.descriptor, getattr(value, fd.name), out, inactive_only, inactive)
        elif fd.codec == "array" and fd.array_kind in (1, 2) and fd.elem_codec == "table":
            target = descriptor.by_name(fd.target)
            if target is None:
                raise InvalidValue(fd.name, f"the table {fd.target} is not registered")
            for item in getattr(value, fd.name):
                _edges(target.descriptor, item, out, inactive_only, inactive)
        elif fd.codec == "array" and fd.elem_codec in ("string", "text_optional"):
            continue
        elif fd.codec == "array" and fd.array_kind in (1, 2) and fd.elem_codec in ("bool", "int", "enum", "float32", "float64"):
            continue
        elif fd.codec == "keyed" and fd.elem is not None and fd.elem.codec in ("bool", "int", "enum", "float32", "float64"):
            continue
        elif fd.codec == "keyed" and fd.elem is not None and fd.elem.codec == "table":
            target = descriptor.by_name(fd.target)
            if target is None:
                raise InvalidValue(fd.name, "a keyed element is not registered")
            keyed = getattr(value, fd.name)
            values = keyed if isinstance(keyed, list) else keyed._values
            for item in values:
                _edges(target.descriptor, item, out, inactive_only, inactive)
        elif fd.codec == "optional":
            current = getattr(value, fd.name)
            if current is not None:
                field = _optional_field(fd)
                _edges(SimpleNamespace(fields=(field,)), SimpleNamespace(**{field.name: current}), out, inactive_only, inactive)
        elif fd.codec == "map":
            for item in _map_entries(fd, getattr(value, fd.name)):
                _edges(item.descriptor, item, out, inactive_only, inactive)
        elif fd.codec == "union_array":
            field = _union_field(fd)
            for item in getattr(value, fd.name):
                _edges(SimpleNamespace(fields=(field,)), SimpleNamespace(**{field.name: item}), out, inactive_only, inactive)
        elif fd.codec == "union":
            union = getattr(value, fd.name)
            if union is not None and union.type:
                arm = union.descriptor.arms[union.type - 1]
                if arm.field is not None:
                    item = getattr(union, arm.name)
                    _edges(SimpleNamespace(fields=(arm.field,)), SimpleNamespace(**{arm.field.name: item}), out, inactive_only, inactive)
        elif fd.codec in ("string_blob", "bytes_blob"):
            current = getattr(value, fd.name)
            if current is not None:
                if not inactive_only or inactive:
                    out.append((fd, current))
        elif fd.codec == "pointer":
            current = getattr(value, fd.name)
            if current is not None:
                if not inactive_only or inactive:
                    out.append((fd, current))
        elif fd.codec == "pointer_array":
            for current in getattr(value, fd.name):
                if current is not None:
                    if not inactive_only or inactive:
                        out.append((fd, current))
        elif fd.codec == "array" and fd.array_kind == 3 and fd.elem_codec in ("int", "bool", "enum", "float32", "float64", "table", "string", "text_optional"):
            if fd.elem_codec == "table":
                target = descriptor.by_name(fd.target)
                if target is None:
                    raise InvalidValue(fd.name, "a list table is not registered")
                for item in getattr(value, fd.name):
                    _edges(target.descriptor, item, out, inactive_only, inactive)
            continue
        elif fd.codec in ("bool", "int", "enum", "float32", "float64", "string", "bytes"):
            continue
        else:
            raise InvalidValue(fd.name, _RETIRED)


def _number(value: Any) -> tuple[list[_Entry], dict[int, _Entry]]:
    root = _Entry(value.__class__, value, value.descriptor)
    entries = [root]
    index: dict[int, _Entry] = {id(value): root}
    open_ids: set[int] = {id(value)}
    blob_index: dict[int, _Entry] = {}
    first: list[tuple[Any, Any]] = []
    _edges(value.descriptor, value, first)
    stack: list[tuple[_Entry, list[tuple[Any, Any]], int]] = [(root, first, 0)]
    while stack:
        current, edges, next_edge = stack[-1]
        if next_edge >= len(edges):
            open_ids.discard(id(current.value))
            stack.pop()
            continue
        stack[-1] = (current, edges, next_edge + 1)
        fd, target_value = edges[next_edge]
        if fd.codec in ("string_blob", "bytes_blob"):
            # Only an explicit BlobRef names a shared in-memory node.
            identity = id(target_value) if isinstance(target_value, BlobRef) else None
            previous = blob_index.get(identity) if identity is not None else None
            if previous is not None:
                # Verify the wrapper is legal for this slot as well.
                kind_blob._encode(fd, target_value, fd.name)
                current.blobs.append(previous)
                continue
            body = kind_blob._encode(fd, target_value, fd.name)
            if len(body) > 0xFFFFFFFF:
                raise InvalidValue(fd.name, "a blob exceeds the cooked uint32 length limit")
            entry = _Entry(type(target_value), target_value, None)
            entry.blob = kind_blob._BlobNode(fd.codec == "string_blob", body)
            entry.type_id = value.descriptor.string_blob_type_id if entry.blob.string else value.descriptor.bytes_blob_type_id
            entries.append(entry)
            current.blobs.append(entry)
            if identity is not None:
                blob_index[identity] = entry
                index[identity] = entry
            continue
        key = id(target_value)
        if key in open_ids:
            raise InvalidValue(fd.name, "a pointer cycle has no cooked form")
        if key in index:
            continue
        target_cls = descriptor.by_name(fd.target)
        if target_cls is None:
            raise InvalidValue(fd.name, f"the table {fd.target} is not registered")
        entry = _Entry(target_cls, target_value, target_cls.descriptor)
        entries.append(entry)
        index[key] = entry
        open_ids.add(key)
        more: list[tuple[Any, Any]] = []
        _edges(target_cls.descriptor, target_value, more)
        stack.append((entry, more, 0))
    return entries, index


def _field_has_extent(fd: Any, seen: set[str]) -> bool:
    if (fd.codec == "string" and len(fd.pieces) == 1) or (fd.codec == "optional" and fd.inner == "string" and len(fd.pieces) == 2):
        return True
    if fd.codec == "map" or (fd.codec == "optional" and fd.inner == "map") or (fd.codec in ("array", "union_array", "pointer_array") and fd.array_kind == 3):
        return True
    if fd.codec in ("union", "union_array"):
        key = "union:" + fd.target
        if key in seen:
            return False
        seen.add(key)
        union_cls = descriptor._union_by_name(fd.target)
        return union_cls is not None and any(
            arm.field is not None and _field_has_extent(arm.field, seen)
            for arm in union_cls.descriptor.arms
        )
    if fd.codec in ("table", "array", "keyed", "optional") and fd.target:
        target = descriptor.by_name(fd.target)
        return target is not None and _has_extent(target.descriptor, seen)
    return False


def _has_extent(desc: Any, seen: set[str]) -> bool:
    key = "table:" + desc.name
    if key in seen:
        return False
    seen.add(key)
    return any(_field_has_extent(fd, seen) for fd in desc.fields)


def _plan_lists(desc: Any, value: Any, at: int, cursor: list[int], placements: dict[int, int], ignore_guard: bool = False, skip_validation: bool = False, inherited_inactive: bool = False) -> None:
    for fd in desc.fields:
        current = getattr(value, fd.name)
        inactive = inherited_inactive or (hasattr(fd, "guard_holds") and not fd.guard_holds(value))
        if inactive and hasattr(fd, "guard_holds") and not skip_validation:
            # A cook writes inline storage even behind a false guard. Validate
            # that slot without traversing its pointer edges; numbering still
            # follows only active fields.
            field = _copy(fd)
            field.guards = ()
            one_field: Any = SimpleNamespace(fields=(field,))
            for _ in _validate_fields(value, one_field, ""):
                pass
        if not ignore_guard and hasattr(fd, "guard_holds") and not fd.guard_holds(value):
            trial = [0, cursor[1]]
            _plan_lists(SimpleNamespace(fields=(fd,)), value, at, trial, {}, True, True, True)
            if trial[0]:
                raise InvalidValue(fd.name, "an inactive field holds an extent")
            continue
        slot = at + fd.at
        if fd.codec == "string" and len(fd.pieces) == 1:
            if current:
                raw = current.encode("utf-8")
                cursor[0] = _align_up(cursor[0], 8)
                placements[slot] = cursor[0]
                cursor[0] += _align_up(8 + len(raw) + 1, cursor[1])
        elif fd.codec == "map":
            entries = _map_entries(fd, current)
            if entries:
                cursor[0] = _align_up(cursor[0], fd.elem_align)
                start = cursor[0]
                placements[slot] = start
                cursor[0] += len(entries) * fd.elem_size
                for position, item in enumerate(entries):
                    _plan_lists(item.descriptor, item, start + position * fd.elem_size, cursor, placements, inherited_inactive=inactive)
        elif fd.codec == "pointer_array" and fd.array_mode == "list":
            if current:
                cursor[0] = _align_up(cursor[0], fd.elem_align)
                placements[slot] = cursor[0]
                cursor[0] += len(current) * fd.elem_size
        elif fd.codec == "union_array":
            if fd.array_kind == 3:
                if not current:
                    continue
                cursor[0] = _align_up(cursor[0], fd.elem_align)
                start = cursor[0]
                placements[slot] = start
                cursor[0] += len(current) * fd.elem_size
                stride = fd.elem_size
            else:
                start = slot
                stride = fd.pieces[0][1] // fd.bound
            field = _union_field(fd)
            for position, item in enumerate(current):
                _plan_lists(SimpleNamespace(fields=(field,)), SimpleNamespace(**{field.name: item}), start + position * stride, cursor, placements, inherited_inactive=inactive)
        elif fd.codec == "array" and fd.array_kind == 3:
            if fd.elem_codec not in ("int", "bool", "enum", "float32", "float64", "table", "string", "text_optional"):
                raise InvalidValue(fd.name, _RETIRED)
            if current:
                cursor[0] = _align_up(cursor[0], fd.elem_align)
                start = cursor[0]
                placements[slot] = start
                cursor[0] += len(current) * fd.elem_size
                if fd.elem_codec in ("table", "string", "text_optional"):
                    target = descriptor.by_name(fd.target)
                    if target is None:
                        raise InvalidValue(fd.name, "a list table is not registered")
                    for position, item in enumerate(current):
                        element = _text_entry(fd, item) if fd.elem_codec != "table" else item
                        _plan_lists(target.descriptor, element, start + position * fd.elem_size, cursor, placements, inherited_inactive=inactive)
        elif fd.codec == "table":
            target = descriptor.by_name(fd.target)
            if target is not None:
                _plan_lists(target.descriptor, current, slot, cursor, placements, inherited_inactive=inactive)
        elif fd.codec == "array" and fd.array_kind in (1, 2) and fd.elem_codec in ("table", "string", "text_optional"):
            target = descriptor.by_name(fd.target)
            if target is not None:
                stride = fd.pieces[0][1] // fd.bound
                for position, item in enumerate(current):
                    element = _text_entry(fd, item) if fd.elem_codec != "table" else item
                    _plan_lists(target.descriptor, element, slot + position * stride, cursor, placements, inherited_inactive=inactive)
        elif fd.codec == "keyed" and fd.elem is not None and fd.elem.codec == "table":
            target = descriptor.by_name(fd.target)
            if target is not None:
                values = current if isinstance(current, list) else current._values
                stride = fd.pieces[0][1] // fd.bound
                for position, item in enumerate(values):
                    _plan_lists(target.descriptor, item, slot + position * stride, cursor, placements, inherited_inactive=inactive)
        elif fd.codec == "optional" and current is not None:
            field = _optional_field(fd)
            _plan_lists(SimpleNamespace(fields=(field,)), SimpleNamespace(**{field.name: current}), slot, cursor, placements, False, True, inactive)
        elif fd.codec == "union" and current is not None and current.type:
            arm = current.descriptor.arms[current.type - 1]
            if arm.field is not None:
                item = getattr(current, arm.name)
                _plan_lists(SimpleNamespace(fields=(arm.field,)), SimpleNamespace(**{arm.field.name: item}), slot + current.descriptor.arm_offset, cursor, placements, inherited_inactive=inactive)



def _layout(value: Any) -> tuple[list[_Entry], dict[int, _Entry], int, int]:
    entries, index = _number(value)
    offset = 0
    align = REGION_ALIGN_FLOOR
    for entry in entries:
        node_align = 8 if entry.blob is not None else entry.desc.cook_align
        if node_align < 1:
            node_align = 1
        if node_align > align:
            align = node_align
        offset = _align_up(offset, node_align)
        entry.offset = offset
        if entry.blob is not None:
            offset += 8 + len(entry.blob.body) + int(entry.blob.string)
        elif _has_extent(entry.desc, set()):
            cursor = [offset + entry.desc.extent_start, entry.desc.region_align]
            _plan_lists(entry.desc, entry.value, offset, cursor, entry.placements)
            offset = cursor[0]
        else:
            _plan_lists(entry.desc, entry.value, offset, [0, entry.desc.region_align], {})
            offset += entry.desc.size
    # A false guard suppresses numbering, but the inline slot is still written.
    # Any non-null reference there must already name a live numbered node.
    for entry in entries:
        if entry.blob is not None:
            continue
        inactive_edges: list[tuple[Any, Any]] = []
        _edges(entry.desc, entry.value, inactive_edges, True)
        for field, target in inactive_edges:
            if field.codec in ("string_blob", "bytes_blob") and not isinstance(target, BlobRef):
                raise InvalidValue(field.name, "an inactive blob slot has no node index")
            if id(target) not in index:
                raise InvalidValue(field.name, "an inactive reference has no node index")
    return entries, index, _align_up(offset, align), align


def _write_scalar(buf: bytearray, at: int, width: int, codec: str, value: Any, signed: bool, order: Literal["little", "big"]) -> None:
    if codec in ("float32", "float64"):
        code = "f" if codec == "float32" else "d"
        raw = struct.pack(("<" if order == "little" else ">") + code, value)
        buf[at : at + width] = raw
    else:
        _put_int(buf, at, int(value), width, order, signed)


def _write_record(
    buf: bytearray,
    desc: Any,
    value: Any,
    base: int,
    index: dict[int, _Entry],
    order: Literal["little", "big"],
    blobs: Iterator[_Entry],
    placements: dict[int, int],
    inherited_inactive: bool = False,
) -> None:
    fast = getattr(type(value), "_s_cook_write", None)
    if callable(fast):
        fast(buf, value, base, order)
        return
    for fd in desc.fields:
        inactive = inherited_inactive or (hasattr(fd, "guard_holds") and not fd.guard_holds(value))
        at = base + fd.at
        current = getattr(value, fd.name)
        if fd.codec == "bool":
            buf[at] = 1 if current else 0
        elif fd.codec in ("int", "enum", "float32", "float64"):
            width = fd.pieces[0][1]
            if width == 16:
                _put_wide(buf, at, int(current), order, fd.lo < 0)
            else:
                _write_scalar(buf, at, width, fd.codec, current, fd.lo < 0, order)
        elif fd.codec in ("string", "bytes"):
            raw = current.encode("utf-8") if fd.codec == "string" else bytes(current)
            if fd.codec == "string" and len(fd.pieces) == 1:
                if raw:
                    start = HEADER_BYTES + placements[at - HEADER_BYTES]
                    _put_int(buf, at, start - at, 8, order, True)
                    _put_uint(buf, start, len(raw), 4, order)
                    buf[start + 8 : start + 8 + len(raw)] = raw
            else:
                buf[at : at + len(raw)] = raw
                _put_int(buf, base + fd.pieces[1][0], len(raw), 4, order, True)
        elif fd.codec == "table":
            target = descriptor.by_name(fd.target)
            if target is None:
                raise InvalidValue(fd.name, f"the table {fd.target} is not registered")
            _write_record(buf, target.descriptor, current, at, index, order, blobs, placements, inactive)
        elif fd.codec == "array" and fd.array_kind in (1, 2) and fd.elem_codec in ("bool", "int", "enum", "float32", "float64"):
            stride = fd.pieces[0][1] // fd.bound
            for position, item in enumerate(current):
                _write_scalar(buf, at + position * stride, stride, fd.elem_codec, item, fd.lo < 0, order)
            if fd.array_kind == 2:
                _put_int(buf, base + fd.pieces[1][0], len(current), 4, order, True)
        elif fd.codec == "keyed" and fd.elem is not None and fd.elem.codec in ("bool", "int", "enum", "float32", "float64"):
            values = current if isinstance(current, list) else current._values
            stride = fd.pieces[0][1] // fd.bound
            for position, item in enumerate(values):
                _write_scalar(buf, at + position * stride, stride, fd.elem.codec, item, fd.elem.lo < 0, order)
        elif fd.codec == "array" and fd.array_kind in (1, 2) and fd.elem_codec in ("table", "string", "text_optional"):
            target = descriptor.by_name(fd.target)
            if target is None:
                raise InvalidValue(fd.name, f"the table {fd.target} is not registered")
            stride = fd.pieces[0][1] // fd.bound
            for position, item in enumerate(current):
                element = _text_entry(fd, item) if fd.elem_codec != "table" else item
                _write_record(buf, target.descriptor, element, at + position * stride, index, order, blobs, placements, inactive)
            if fd.array_kind == 2:
                _put_int(buf, base + fd.pieces[1][0], len(current), 4, order, True)
        elif fd.codec == "keyed" and fd.elem is not None and fd.elem.codec == "table":
            target = descriptor.by_name(fd.target)
            if target is None:
                raise InvalidValue(fd.name, "a keyed element is not registered")
            values = current if isinstance(current, list) else current._values
            stride = fd.pieces[0][1] // fd.bound
            for position, item in enumerate(values):
                _write_record(buf, target.descriptor, item, at + position * stride, index, order, blobs, placements, inactive)
        elif fd.codec == "optional":
            if current is not None:
                field = _optional_field(fd)
                _write_record(buf, SimpleNamespace(fields=(field,)), SimpleNamespace(**{field.name: current}), at, index, order, blobs, placements, inactive)
                buf[base + fd.pieces[-1][0]] = 1
        elif fd.codec == "map":
            entries = _map_entries(fd, current)
            if entries:
                start = HEADER_BYTES + placements[at - HEADER_BYTES]
                _put_int(buf, at, start - at, 8, order, True)
                _put_int(buf, at + 8, len(entries), 4, order, True)
                for position, item in enumerate(entries):
                    _write_record(buf, item.descriptor, item, start + position * fd.elem_size, index, order, blobs, placements, inactive)
        elif fd.codec == "array" and fd.array_kind == 3 and fd.elem_codec in ("int", "bool", "enum", "float32", "float64", "table", "string", "text_optional"):
            if current:
                start = HEADER_BYTES + placements[at - HEADER_BYTES]
                _put_int(buf, at, start - at, 8, order, True)
                _put_int(buf, at + 8, len(current), 4, order, True)
                for position, item in enumerate(current):
                    element_at = start + position * fd.elem_size
                    if fd.elem_codec in ("table", "string", "text_optional"):
                        target = descriptor.by_name(fd.target)
                        if target is None:
                            raise InvalidValue(fd.name, "a list table is not registered")
                        element = _text_entry(fd, item) if fd.elem_codec != "table" else item
                        _write_record(buf, target.descriptor, element, element_at, index, order, blobs, placements, inactive)
                    else:
                        _write_scalar(buf, element_at, fd.elem_size, fd.elem_codec, item, fd.elem.lo < 0 if fd.elem is not None else fd.lo < 0, order)
        elif fd.codec == "union_array":
            field = _union_field(fd)
            if fd.array_kind == 3:
                if not current:
                    continue
                start = HEADER_BYTES + placements[at - HEADER_BYTES]
                stride = fd.elem_size
                _put_int(buf, at, start - at, 8, order, True)
                _put_int(buf, at + 8, len(current), 4, order, True)
            else:
                start = at
                stride = fd.pieces[0][1] // fd.bound
                if fd.array_kind == 2:
                    _put_int(buf, base + fd.pieces[1][0], len(current), 4, order, True)
            for position, item in enumerate(current):
                _write_record(buf, SimpleNamespace(fields=(field,)), SimpleNamespace(**{field.name: item}), start + position * stride, index, order, blobs, placements, inactive)
        elif fd.codec == "union":
            union_cls = descriptor._union_by_name(fd.target)
            if union_cls is None:
                raise InvalidValue(fd.name, "a union is not registered")
            ud = union_cls.descriptor
            tag = 0 if current is None else current.type
            _put_uint(buf, at, tag, ud.tag_size, order)
            if tag:
                arm = ud.arms[tag - 1]
                field = arm.field
                if field is not None:
                    item = getattr(current, arm.name)
                    _write_record(buf, SimpleNamespace(fields=(field,)), SimpleNamespace(**{field.name: item}), at + ud.arm_offset, index, order, blobs, placements, inactive)
        elif fd.codec in ("string_blob", "bytes_blob"):
            if current is not None:
                if inactive:
                    blob_entry = index.get(id(current)) if isinstance(current, BlobRef) else None
                    if blob_entry is None:
                        raise InvalidValue(fd.name, "an inactive blob slot has no node index")
                else:
                    blob_entry = next(blobs)
                _put_int(buf, at, HEADER_BYTES + blob_entry.offset - at, 8, order, True)
        elif fd.codec == "pointer_array":
            if fd.array_mode == "list":
                if not current:
                    continue
                start = HEADER_BYTES + placements[at - HEADER_BYTES]
                _put_int(buf, at, start - at, 8, order, True)
                _put_int(buf, at + 8, len(current), 4, order, True)
            else:
                start = at
                if fd.array_mode == "counted":
                    _put_int(buf, base + fd.pieces[1][0], len(current), 4, order, True)
            for position, item in enumerate(current):
                if item is None:
                    continue
                entry = index.get(id(item))
                if entry is None:
                    raise InvalidValue(fd.name, "the pointer target has no node index")
                slot = start + position * 8
                _put_int(buf, slot, HEADER_BYTES + entry.offset - slot, 8, order, True)
        elif fd.codec == "pointer":
            if current is None:
                _put_int(buf, at, 0, 8, order, True)
                continue
            entry = index.get(id(current))
            if entry is None:
                raise InvalidValue(fd.name, "the pointer target has no node index")
            _put_int(buf, at, HEADER_BYTES + entry.offset - at, 8, order, True)
        else:
            raise InvalidValue(fd.name, _RETIRED)


def _require_cook(value: Any) -> None:
    if "cook" not in value.descriptor.forms:
        raise InvalidValue(value.descriptor.name, "the table does not carry cook")


def cook_measure(value: Any) -> int:
    """The cooked file's exact length, or InvalidValue for a value no writer carries."""
    _require_cook(value)
    _validate(value, value.descriptor, "", set())
    entries, _, data_length, _ = _layout(value)
    return HEADER_BYTES + data_length + len(entries) * DIRECTORY_ENTRY


def cook(value: Any, order: int = ORDER_LITTLE, capacity: int | None = None) -> bytes:
    """Write the cooked file in the caller's byte order (1 little, 2 big)."""
    _require_cook(value)
    if order != ORDER_LITTLE and order != ORDER_BIG:
        raise ValueError("the byte order is 1 (little-endian) or 2 (big-endian)")
    word: Literal["little", "big"] = "big" if order == 2 else "little"
    _validate(value, value.descriptor, "", set())
    entries, index, data_length, align = _layout(value)
    total = HEADER_BYTES + data_length + len(entries) * DIRECTORY_ENTRY
    if capacity is not None and capacity < total:
        raise InvalidValue("", "the capacity does not hold the cook")
    buf = bytearray(total)
    for entry in entries:
        at = HEADER_BYTES + entry.offset
        if entry.blob is not None:
            _put_uint(buf, at, len(entry.blob.body), 4, word)
            buf[at + 8 : at + 8 + len(entry.blob.body)] = entry.blob.body
        else:
            _write_record(buf, entry.desc, entry.value, at, index, word, iter(entry.blobs), entry.placements)
    _put_uint(buf, 0, MAGIC, 8, word)
    _put_uint(buf, 8, value.descriptor.build_version, 8, word)
    _put_uint(buf, 16, order, 8, word)
    _put_uint(buf, 24, data_length, 8, word)
    _put_uint(buf, 32, len(entries) * DIRECTORY_ENTRY, 8, word)
    _put_uint(buf, 40, align, 8, word)
    _put_uint(buf, 48, 0, 8, word)
    _put_uint(buf, 56, 0, 8, word)
    at = HEADER_BYTES + data_length
    for entry in entries:
        _put_uint(buf, at, entry.offset, 8, word)
        _put_uint(buf, at + 8, entry.type_id, 8, word)
        at += DIRECTORY_ENTRY
    return bytes(buf)
