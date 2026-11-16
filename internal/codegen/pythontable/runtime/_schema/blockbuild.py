"""The Python table runtime's BLOCK FORM builder
(docs/SPEC-TABLES.md §19, §19.1).

The block form is a third projection of a fixed table: a projection record at
offset 0 of a 64-byte aligned extent, then each out-of-line array's rows in
declaration order. Begin writes what every builder writes: the three-word
prologue and one sixteen-byte (offset_of, count, stride) triple per array. It
writes no row and no padding; the tail is unspecified and rows past count
belong to the caller.

A Python bytes object has no address, so the base's alignment is a caller
argument at Open rather than a fact Begin can stamp.
"""

import sys
from typing import Any, Mapping

from . import InvalidValue, descriptor
from . import block as _block


def _align_up(value: int, alignment: int) -> int:
    return (value + alignment - 1) // alignment * alignment


def _stride_align(stride: int) -> int:
    align = 1
    while align < 16 and stride % (align * 2) == 0:
        align *= 2
    return align


def _put(buf: bytearray, at: int, width: int, value: int) -> None:
    buf[at : at + width] = int(value).to_bytes(width, sys.byteorder, signed=False)


def _table(T: Any, root: str) -> descriptor.BlockTable:
    cls = getattr(T, root, None)
    layout: descriptor.BlockTable | None = getattr(cls, "block", None) if cls is not None else None
    if layout is None:
        raise InvalidValue(root, "the table has no block form")
    return layout


def BlockMaxBytes(T: Any, root: str) -> int:
    """<Table>BlockMaxBytes: the projection plus every array at its maximum."""
    return _table(T, root).max_bytes


def Begin(T: Any, root: str, counts: Mapping[str, int] | None = None) -> _block.Block:
    """Write the prologue and the triples of one block, or raise InvalidValue."""
    layout = _table(T, root)
    version = getattr(T, "BUILD_VERSION", None)
    if version is None:
        raise InvalidValue(root, "the unit carries no build version for the block prologue")
    chosen = counts or {}
    for a in layout.arrays:
        count = chosen.get(a.name, 0)
        if type(count) is not int or count < 0 or count > a.maximum:
            raise InvalidValue(a.name, f"count {count} is outside [0, {a.maximum}]")

    offset = layout.projection.size
    starts: list[tuple[descriptor.BlockArray, int, int]] = []
    for a in layout.arrays:
        count = chosen.get(a.name, 0)
        align = max(_block.BLOCK_ALIGN, _stride_align(a.stride))
        offset = _align_up(offset, align)
        starts.append((a, offset, count))
        offset += count * a.stride
    used = _align_up(offset, _block.BLOCK_ALIGN)

    buf = bytearray(used)
    _put(buf, 0, 8, _block.MAGIC)
    _put(buf, 8, 8, version)
    _put(buf, 16, 8, _block.BYTE_ORDER)
    arrays: list[_block.ArrayView] = []
    for a, start, count in starts:
        _put(buf, a.offset, 8, start)
        _put(buf, a.offset + 8, 4, count)
        _put(buf, a.offset + 12, 4, a.stride)
        cls = descriptor.by_name(a.element)
        if cls is None:
            raise InvalidValue(a.name, f"the array element {a.element} is not registered")
        row = getattr(cls, "block_row", None)
        if not isinstance(row, descriptor.BlockRecord):
            raise InvalidValue(a.name, f"the array element {a.element} carries no row layout")
        arrays.append(_block.ArrayView(a, start, count, a.stride, row))
    return _block.Block(root, buf, version, layout.projection, tuple(arrays), used, 0)


def BlockBytes(block: _block.Block) -> int:
    """The used extent, the greatest offset_of + count * stride rounded to 64."""
    return len(block.bytes)
