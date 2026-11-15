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

from typing import Any, Final, Literal

from . import InvalidValue
from . import descriptor
from .save import _validate

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
    __slots__ = ("cls", "value", "desc", "offset")

    cls: type[Any]
    value: Any
    desc: Any
    offset: int

    def __init__(self, cls: type[Any], value: Any, desc: Any) -> None:
        self.cls = cls
        self.value = value
        self.desc = desc
        self.offset = 0


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


def _edges(desc: Any, value: Any, out: list[tuple[Any, Any]]) -> None:
    for fd in desc.fields:
        if fd.codec == "table":
            target = descriptor.by_name(fd.target)
            if target is None:
                raise InvalidValue(fd.name, f"the table {fd.target} is not registered")
            _edges(target.descriptor, getattr(value, fd.name), out)
        elif fd.codec == "pointer":
            current = getattr(value, fd.name)
            if current is not None:
                out.append((fd, current))
        elif fd.codec == "bool" or fd.codec == "int":
            continue
        else:
            raise InvalidValue(fd.name, _RETIRED)


def _number(value: Any) -> tuple[list[_Entry], dict[int, _Entry]]:
    root = _Entry(value.__class__, value, value.descriptor)
    entries = [root]
    index: dict[int, _Entry] = {id(value): root}
    open_ids: set[int] = {id(value)}
    first: list[tuple[Any, Any]] = []
    _edges(value.descriptor, value, first)
    stack: list[tuple[Any, list[tuple[Any, Any]], int]] = [(value, first, 0)]
    while stack:
        current, edges, next_edge = stack[-1]
        if next_edge >= len(edges):
            open_ids.discard(id(current))
            stack.pop()
            continue
        stack[-1] = (current, edges, next_edge + 1)
        fd, target_value = edges[next_edge]
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
        stack.append((target_value, more, 0))
    return entries, index


def _layout(value: Any) -> tuple[list[_Entry], dict[int, _Entry], int, int]:
    entries, index = _number(value)
    offset = 0
    align = REGION_ALIGN_FLOOR
    for entry in entries:
        node_align = entry.desc.cook_align
        if node_align < 1:
            node_align = 1
        if node_align > align:
            align = node_align
        offset = _align_up(offset, node_align)
        entry.offset = offset
        offset += entry.desc.size
    return entries, index, _align_up(offset, align), align


def _write_record(
    buf: bytearray,
    desc: Any,
    value: Any,
    base: int,
    index: dict[int, _Entry],
    order: Literal["little", "big"],
) -> None:
    for fd in desc.fields:
        at = base + fd.at
        current = getattr(value, fd.name)
        if fd.codec == "bool":
            buf[at] = 1 if current else 0
        elif fd.codec == "int":
            width = _width(fd)
            if width == 16:
                _put_wide(buf, at, int(current), order, fd.lo < 0)
            else:
                _put_int(buf, at, int(current), width, order, fd.lo < 0)
        elif fd.codec == "table":
            target = descriptor.by_name(fd.target)
            if target is None:
                raise InvalidValue(fd.name, f"the table {fd.target} is not registered")
            _write_record(buf, target.descriptor, current, at, index, order)
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


def cook_measure(value: Any) -> int:
    """The cooked file's exact length, or InvalidValue for a value no writer carries."""
    _validate(value, value.descriptor, "", set())
    entries, _, data_length, _ = _layout(value)
    return HEADER_BYTES + data_length + len(entries) * DIRECTORY_ENTRY


def cook(value: Any, order: int = ORDER_LITTLE, capacity: int | None = None) -> bytes:
    """Write the cooked file in the caller's byte order (1 little, 2 big)."""
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
        _write_record(buf, entry.desc, entry.value, HEADER_BYTES + entry.offset, index, word)
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
        _put_uint(buf, at + 8, entry.desc.type_id, 8, word)
        at += DIRECTORY_ENTRY
    return bytes(buf)
