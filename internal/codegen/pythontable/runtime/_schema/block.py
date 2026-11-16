"""The Python table runtime's BLOCK FORM, the READ half
(docs/SPEC-TABLES.md §19.2, §19.3).

Nothing declares the form: every fixed table has one, and a consumer that
reads a block calls BlockOpen. The bytes are memory another build wrote, so
Open checks the prologue and every array's (offset_of, count, stride) once,
points at them, and copies nothing. A Python bytes object has no address, so
the base's alignment is the caller's byte offset into whatever buffer the
bytes are a view of: a non-zero, non-multiple-of-64 offset is unaligned_base.

The descriptors are the mechanism: the generated table carries its projection
and row layouts as data (built once at import from the compiler's own model),
and Dump reads every field through them and nothing else.
"""

import builtins
import sys
from typing import Any

from . import Refused, descriptor

MAGIC = 0x4B4C42414D484353
PROLOGUE = 24
BLOCK_ALIGN = 64

# §19.1: the prologue's byte_order word is THE PRODUCER'S OWN order, 1 little
# and 2 big. Python's sys.byteorder is this build's own, and every read below
# uses it, so a big-endian host decodes a big-endian block with no case.
BYTE_ORDER = 1 if sys.byteorder == "little" else 2
_MAGIC_BYTES = MAGIC.to_bytes(8, sys.byteorder)
_FOREIGN_MAGIC_BYTES = _MAGIC_BYTES[::-1]


class ArrayView:
    """One opened out-of-line array: its triple and the row it points at."""

    __slots__ = ("desc", "offset", "count", "stride", "record")

    desc: descriptor.BlockArray
    offset: int
    count: int
    stride: int
    record: descriptor.BlockRecord

    def __init__(
        self,
        desc: descriptor.BlockArray,
        offset: int,
        count: int,
        stride: int,
        record: descriptor.BlockRecord,
    ) -> None:
        self.desc = desc
        self.offset = offset
        self.count = count
        self.stride = stride
        self.record = record


class Block:
    """An opened block, or one a builder produced: a projection and its arrays."""

    __slots__ = ("name", "bytes", "build_version", "projection", "arrays", "used", "base_offset")

    name: str
    bytes: builtins.bytes | bytearray | memoryview
    build_version: int
    projection: descriptor.BlockRecord
    arrays: tuple[ArrayView, ...]
    used: int
    base_offset: int

    def __init__(
        self,
        name: str,
        data: builtins.bytes | bytearray | memoryview,
        build_version: int,
        projection: descriptor.BlockRecord,
        arrays: tuple[ArrayView, ...],
        used: int,
        base_offset: int,
    ) -> None:
        self.name = name
        self.bytes = data
        self.build_version = build_version
        self.projection = projection
        self.arrays = arrays
        self.used = used
        self.base_offset = base_offset


def _align_up(value: int, alignment: int) -> int:
    return (value + alignment - 1) // alignment * alignment


def _u(view: memoryview, at: int, width: int) -> int:
    return int.from_bytes(view[at : at + width], sys.byteorder, signed=False)


def _s(view: memoryview, at: int, width: int) -> int:
    return int.from_bytes(view[at : at + width], sys.byteorder, signed=True)


def _stride_align(stride: int) -> int:
    align = 1
    while align < 16 and stride % (align * 2) == 0:
        align *= 2
    return align


def _rows_of(name: str) -> descriptor.BlockRecord:
    cls = descriptor.by_name(name)
    if cls is None:
        raise Refused("bad_layout", f"the array element {name} is not registered")
    row = getattr(cls, "block_row", None)
    if not isinstance(row, descriptor.BlockRecord):
        raise Refused("bad_layout", f"the array element {name} carries no row layout")
    return row


def _view_of(data: builtins.bytes | bytearray | memoryview) -> memoryview:
    try:
        view = memoryview(data)
    except TypeError:
        raise Refused("truncated", "the block is not a bytes-like object") from None
    if view.format != "B" or not view.contiguous:
        raise Refused("truncated", "the block is not a B-format contiguous buffer")
    return view


def BlockOpen(
    T: Any,
    name: str,
    data: builtins.bytes | bytearray | memoryview,
    build_version: int | None = None,
    base_offset: int = 0,
) -> Block:
    """Open one block, or raise Refused naming the first failing clause."""
    view = _view_of(data)
    if build_version is None:
        build_version = getattr(T, "BUILD_VERSION", None)
    cls = getattr(T, name, None)
    layout: descriptor.BlockTable | None = getattr(cls, "block", None) if cls is not None else None
    if layout is None:
        raise Refused("not_a_cook", f"the table {name} has no block form")
    if len(view) < layout.projection.size:
        raise Refused("truncated", f"the block is {len(view)} bytes, shorter than its projection")
    if bytes(view[0:8]) != _MAGIC_BYTES:
        if bytes(view[0:8]) == _FOREIGN_MAGIC_BYTES:
            raise Refused("foreign_order", "the block was written in the other byte order")
        raise Refused("not_a_cook", "the block's magic is not a schema block")
    if _u(view, 16, 8) != BYTE_ORDER:
        raise Refused("not_a_cook", "the block's byte order word disagrees with this build")
    if build_version is None or _u(view, 8, 8) != build_version:
        raise Refused("wrong_build_version", "the block was written by another build")

    used = layout.projection.size
    arrays: list[ArrayView] = []
    for a in layout.arrays:
        offset_of = _u(view, a.offset, 8)
        count = _u(view, a.offset + 8, 4)
        stride = _u(view, a.offset + 12, 4)
        if stride != a.stride:
            raise Refused("bad_layout", f"{a.name} has pitch {stride}, this build's is {a.stride}")
        if count > a.maximum:
            raise Refused("bad_layout", f"{a.name} has {count} rows, past its maximum {a.maximum}")
        elem_align = _stride_align(stride)
        start_align = max(BLOCK_ALIGN, elem_align)
        if offset_of < layout.projection.size or offset_of % start_align != 0:
            raise Refused("bad_layout", f"{a.name} starts at {offset_of}, outside or unaligned")
        if offset_of > len(view):
            raise Refused("bad_layout", f"{a.name} starts at {offset_of}, past the block")
        rows = count * stride
        if rows > len(view) - offset_of:
            raise Refused("bad_layout", f"{a.name}'s rows leave the block")
        ending = offset_of + rows
        if ending > used:
            used = ending
        arrays.append(ArrayView(a, offset_of, count, stride, _rows_of(a.element)))

    padding = (BLOCK_ALIGN - (used % BLOCK_ALIGN)) % BLOCK_ALIGN
    if padding > len(view) - used:
        raise Refused("truncated", "the bytes stop inside the used extent's padding")
    used += padding
    if base_offset % BLOCK_ALIGN != 0:
        raise Refused("unaligned_base", f"the base is {base_offset} bytes past an aligned one")
    return Block(name, view, build_version, layout.projection, tuple(arrays), used, base_offset)


Open = BlockOpen


def _join(prefix: str, name: str) -> str:
    if prefix == "":
        return name
    return prefix + "." + name


def _cook_text(view: memoryview, at: int, used: int) -> str:
    out = ['"']
    for i in range(used):
        c = view[at + i]
        if 0x20 <= c < 0x7F and c != 0x22 and c != 0x5C:
            out.append(chr(c))
        else:
            out.append("\\x%02x" % c)
    out.append('" len=%d' % used)
    return "".join(out)


def _dump_scalar(view: memoryview, at: int, kind: int, width: int) -> str:
    if kind == 1:
        return "true" if view[at] != 0 else "false"
    if kind == 10:
        return "0x%08x" % _u(view, at, 4)
    if kind == 11:
        return "0x%016x" % _u(view, at, 8)
    if 2 <= kind <= 5:
        return str(_s(view, at, width))
    return str(_u(view, at, width))


def _dump_record(
    out: list[str],
    view: memoryview,
    base: int,
    record: descriptor.BlockRecord,
    prefix: str,
) -> None:
    for f in record.fields:
        if f.out_of_line:
            continue
        name = _join(prefix, f.name)
        if f.counted:
            used = _s(view, base + f.count_offset, 4)
            if used < 0 or used > f.bound:
                raise Refused(
                    "bad_layout",
                    f"{record.name}.{f.name} carries a used length of {used}, outside [ 0, {f.bound} ]",
                )
            out.append("  " + name + " = " + _cook_text(view, base + f.offset, used))
        else:
            slots = f.bound if f.array else 1
            for s in range(slots):
                at = base + f.offset + s * f.elem_size
                slot = name + "[" + str(s) + "]" if f.array else name
                if f.target:
                    _dump_record(out, view, at, _rows_of(f.target), slot)
                else:
                    out.append("  " + slot + " = " + _dump_scalar(view, at, f.kind, f.elem_size))
        if f.optional:
            present = view[base + f.present_offset] != 0
            out.append("  " + name + "#present = " + ("true" if present else "false"))


def Dump(handle: Block) -> str:
    """The canonical row dump of an opened block (FORMAT.md), descriptor-only."""
    view = _view_of(handle.bytes)
    out = ["projection " + handle.name + " @0"]
    _dump_record(out, view, 0, handle.projection, "")
    for a in handle.arrays:
        out.append(
            "array %s %s @%d count=%d stride=%d"
            % (a.desc.name, a.record.name, a.offset, a.count, a.stride)
        )
        for r in range(a.count):
            at = a.offset + r * a.stride
            out.append("row %d @%d" % (r, at))
            _dump_record(out, view, at, a.record, "")
    return "\n".join(out) + "\n"
