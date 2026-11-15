"""The cooked form's read side (docs/SPEC-TABLES.md §7, §7.1, §7.2, §7.4).

Open is match-and-point: it reads the header and the base's placement and
nothing else, so it is O(1) in the file's size. Check runs `schema cook-check`'s
two passes over the attribution and the region: the directory linearly, then
every node in directory order. No payload is decoded and no reference is
followed, so a forged aliasing or cycle costs the scan nothing.

Every refusal answers its reason name: not_a_cook, foreign_order,
wrong_build_version, reserved_not_zero, bad_alignment, truncated,
unaligned_base, bad_layout.
"""

from typing import Any, Final, Literal

from . import Refused
from . import descriptor

MAGIC: Final = 0x4B4F4F434D484353
HEADER_BYTES: Final = 64
MAX_ALIGN: Final = 64
DIRECTORY_ENTRY: Final = 16
NOT_MATERIALIZED: Final = 0xFFFFFFFFFFFFFFFF
ORDER_LITTLE: Final = 1
ORDER_BIG: Final = 2

_RETIRED: Final = "the Python table runtime does not carry this construct yet (docs/PORT-PYTHON.md, THE CARD MAP)"


class CookHandle:
    """An opened cook: the region, its directory and the base's placement."""

    __slots__ = (
        "root",
        "data",
        "order",
        "data_offset",
        "data_length",
        "attrib_offset",
        "attrib_length",
        "alignment",
        "directory",
    )

    root: type[Any]
    data: memoryview
    order: Literal["little", "big"]
    data_offset: int
    data_length: int
    attrib_offset: int
    attrib_length: int
    alignment: int
    directory: list[tuple[int, int]]

    def __init__(
        self,
        root: type[Any],
        data: memoryview,
        order: Literal["little", "big"],
        data_offset: int,
        data_length: int,
        attrib_length: int,
        alignment: int,
    ) -> None:
        self.root = root
        self.data = data
        self.order = order
        self.data_offset = data_offset
        self.data_length = data_length
        self.attrib_offset = data_offset + data_length
        self.attrib_length = attrib_length
        self.alignment = alignment
        self.directory = []


def _buffer(data: Any) -> memoryview:
    if isinstance(data, bytes):
        return memoryview(data)
    if isinstance(data, bytearray):
        return memoryview(data)
    if isinstance(data, memoryview):
        if data.format != "B":
            raise Refused("invalid_buffer", "a memoryview must have format B")
        return data
    raise Refused("invalid_buffer", "a read takes bytes, bytearray or a B memoryview")


def _u64(view: memoryview, at: int, order: Literal["little", "big"]) -> int:
    return int.from_bytes(view[at : at + 8], order, signed=False)


def _i64(view: memoryview, at: int, order: Literal["little", "big"]) -> int:
    return int.from_bytes(view[at : at + 8], order, signed=True)


def _align_up(n: int, a: int) -> int:
    if a <= 1:
        return n
    return (n + a - 1) // a * a


def _open(cls: type[Any], data: Any, base_offset: int) -> CookHandle:
    view = _buffer(data)
    root = cls.descriptor
    if len(view) < HEADER_BYTES:
        raise Refused("truncated")
    order: Literal["little", "big"]
    if int.from_bytes(view[0:8], "little") == MAGIC:
        order = "little"
        word = ORDER_LITTLE
    elif int.from_bytes(view[0:8], "big") == MAGIC:
        raise Refused("foreign_order")
    else:
        raise Refused("not_a_cook")
    if _u64(view, 16, order) != word:
        raise Refused("not_a_cook")
    build = _u64(view, 8, order)
    if build != root.build_version:
        raise Refused("wrong_build_version")
    reserved0 = _u64(view, 48, order)
    reserved1 = _u64(view, 56, order)
    if reserved0 != 0 or reserved1 != 0:
        raise Refused("reserved_not_zero")
    data_length = _u64(view, 24, order)
    attrib_length = _u64(view, 32, order)
    alignment = _u64(view, 40, order)
    if (
        alignment < 8
        or alignment > MAX_ALIGN
        or alignment & (alignment - 1) != 0
        or root.align < 1
        or alignment % root.align != 0
    ):
        raise Refused("bad_alignment")
    data_offset = _align_up(HEADER_BYTES, alignment)
    if len(view) < data_offset:
        raise Refused("truncated")
    if data_length > len(view) - data_offset:
        raise Refused("truncated")
    if attrib_length != len(view) - data_offset - data_length:
        raise Refused("truncated")
    if data_length < root.size:
        raise Refused("truncated")
    if base_offset % alignment != 0:
        raise Refused("unaligned_base")
    return CookHandle(cls, view, order, data_offset, data_length, attrib_length, alignment)


def open_cook(cls: type[Any], data: Any, base_offset: int = 0) -> CookHandle:
    """Open a cook: the header and the base's placement, and nothing else."""
    return _open(cls, data, base_offset)


def _directory(h: CookHandle) -> list[tuple[int, int]]:
    if h.attrib_length == 0:
        raise Refused("bad_layout", "a cook with no attribution part cannot be checked")
    if h.attrib_length % DIRECTORY_ENTRY != 0:
        raise Refused("bad_layout", "the attribution part is not whole entries")
    out: list[tuple[int, int]] = []
    for i in range(h.attrib_length // DIRECTORY_ENTRY):
        at = h.attrib_offset + i * DIRECTORY_ENTRY
        out.append((_u64(h.data, at, h.order), _u64(h.data, at + 8, h.order)))
    return out


def _node_class(type_id: int) -> type[Any]:
    cls = descriptor.by_id(type_id)
    if cls is None:
        raise Refused("bad_layout", f"type id {type_id:#x} is no table this unit has")
    return cls


def _check_directory(h: CookHandle, directory: list[tuple[int, int]]) -> None:
    previous = 0
    for i, entry in enumerate(directory):
        offset, type_id = entry
        if offset == NOT_MATERIALIZED:
            raise Refused("bad_layout", "a directory entry is the not-materialized sentinel")
        cls = _node_class(type_id)
        d = cls.descriptor
        if i == 0 and offset != 0:
            raise Refused("bad_layout", "the root is not at the region's base")
        if offset < previous:
            raise Refused("bad_layout", "directory offsets do not ascend")
        if offset % d.cook_align != 0:
            raise Refused("bad_layout", "a directory entry is not aligned for its type")
        if offset + d.size > h.data_length:
            raise Refused("bad_layout", "a node leaves the region")
        previous = offset + d.size


def _scan_record(
    h: CookHandle,
    region: memoryview,
    desc: Any,
    base: int,
    index: int,
    by_offset: dict[int, tuple[int, int]],
    edges: list[list[int]],
) -> None:
    for fd in desc.fields:
        at = base + fd.at
        if fd.codec == "bool" or fd.codec == "int":
            continue
        if fd.codec == "table":
            target = descriptor.by_name(fd.target)
            if target is None:
                raise Refused("bad_layout", f"the table {fd.target} is not registered")
            _scan_record(h, region, target.descriptor, at, index, by_offset, edges)
            continue
        if fd.codec == "pointer":
            delta = _i64(region, at, h.order)
            if delta == 0:
                continue
            target_at = at + delta
            found = by_offset.get(target_at)
            if found is None:
                raise Refused("bad_layout", "a reference names no directory entry")
            target_id = found[1]
            target = descriptor.by_name(fd.target)
            if target is None or target.descriptor.type_id != target_id:
                raise Refused("bad_layout", "a reference names another node type")
            edges[index].append(found[0])
            continue
        raise Refused("bad_layout", _RETIRED)


def _cycle(edges: list[list[int]]) -> bool:
    mark = [0] * len(edges)
    for start in range(len(edges)):
        if mark[start] != 0:
            continue
        mark[start] = 1
        stack: list[tuple[int, int]] = [(start, 0)]
        while stack:
            node, next_edge = stack[-1]
            if next_edge >= len(edges[node]):
                mark[node] = 2
                stack.pop()
                continue
            target = edges[node][next_edge]
            stack[-1] = (node, next_edge + 1)
            if mark[target] == 1:
                return True
            if mark[target] == 2:
                continue
            mark[target] = 1
            stack.append((target, 0))
    return False


def check_cook(cls: type[Any], data: Any, base_offset: int = 0) -> CookHandle:
    """Open a cook and run cook-check's two passes over its region."""
    h = _open(cls, data, base_offset)
    if h.data_length % h.alignment != 0:
        raise Refused("bad_layout", "the data part is not a whole number of alignments")
    directory = _directory(h)
    _check_directory(h, directory)
    by_offset = {offset: (i, type_id) for i, (offset, type_id) in enumerate(directory)}
    edges: list[list[int]] = [[] for _ in directory]
    region = h.data[h.data_offset : h.data_offset + h.data_length]
    for i, entry in enumerate(directory):
        offset, type_id = entry
        cls2 = _node_class(type_id)
        _scan_record(h, region, cls2.descriptor, offset, i, by_offset, edges)
    if _cycle(edges):
        raise Refused("bad_layout", "a cook is never cyclic")
    h.directory = directory
    return h


from .cookwrite import cook, cook_measure  # noqa: E402  (the write side of the one surface)
