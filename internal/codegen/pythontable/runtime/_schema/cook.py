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

import sys
from bisect import bisect_left as _bisect_left
from types import SimpleNamespace
from typing import Any, Final, Iterator, Literal

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
        "node_base",
        "node_end",
        "node_record_end",
        "ranges",
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
    node_base: int
    node_end: int
    node_record_end: int
    ranges: list[tuple[int, int]]

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
        self.node_base = 0
        self.node_end = 0
        self.node_record_end = 0
        self.ranges = []


def _buffer(data: Any) -> memoryview:
    if isinstance(data, bytes):
        return memoryview(data)
    if isinstance(data, bytearray):
        return memoryview(data)
    if isinstance(data, memoryview):
        if data.format != "B" or data.ndim != 1 or not data.contiguous:
            raise Refused("invalid_buffer", "a memoryview must be a contiguous one-dimensional B view")
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
    if "cook" not in cls.descriptor.forms:
        raise Refused("cook_form_not_carried", f"{cls.descriptor.name} does not carry cook")
    view = _buffer(data)
    root = cls.descriptor
    if len(view) < HEADER_BYTES:
        raise Refused("truncated")
    order: Literal["little", "big"]
    if int.from_bytes(view[0:8], sys.byteorder) == MAGIC:
        order = "little" if sys.byteorder == "little" else "big"
        word = ORDER_LITTLE if order == "little" else ORDER_BIG
    elif int.from_bytes(view[0:8], "big" if sys.byteorder == "little" else "little") == MAGIC:
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
        root = h.root.descriptor
        is_blob = type_id in (root.bytes_blob_type_id, root.string_blob_type_id)
        if is_blob:
            size, align = 8, 8
        else:
            cls = _node_class(type_id)
            size, align = cls.descriptor.size, cls.descriptor.cook_align
        if i == 0 and (offset != 0 or type_id != h.root.descriptor.type_id):
            raise Refused("bad_layout", "the root is not at the region's base or has the wrong type")
        if offset < previous:
            raise Refused("bad_layout", "directory offsets do not ascend")
        if offset % align != 0:
            raise Refused("bad_layout", "a directory entry is not aligned for its type")
        if offset + size > h.data_length:
            raise Refused("bad_layout", "a node leaves the region")
        previous = offset + size


def _scan_record(
    h: CookHandle,
    region: memoryview,
    desc: Any,
    base: int,
    offsets: list[int],
) -> None:
    for start, end in desc.padding:
        if any(region[base + start : base + end]):
            raise Refused("bad_layout", "a record padding byte is not zero")
    for fd in desc.fields:
        at = base + fd.at
        if fd.codec in ("bool", "int", "enum", "float32", "float64"):
            continue
        if fd.codec == "string" and len(fd.pieces) == 1:
            delta = _i64(region, at, h.order)
            if delta:
                start = at + delta
                if start < h.node_record_end or start % 8 or start + 8 > h.node_end:
                    raise Refused("bad_layout", "a text blob header leaves its extent or is unaligned")
                length = int.from_bytes(region[start : start + 4], h.order)
                end = start + 8 + length + 1
                if not length or end > h.node_end or region[end - 1] != 0:
                    raise Refused("bad_layout", "a text blob has invalid length or terminator")
                if any(start < other_end and other_start < end for other_start, other_end in h.ranges):
                    raise Refused("bad_layout", "text blobs overlap")
                h.ranges.append((start, end))
            continue
        if fd.codec in ("string", "bytes"):
            count_at = base + fd.pieces[1][0]
            used = int.from_bytes(region[count_at : count_at + 4], h.order, signed=True)
            if used < 0 or used > fd.size:
                raise Refused("bad_layout", "a text length leaves its buffer")
            storage_end = at + fd.pieces[0][1]
            if any(region[at + used : storage_end]) or any(region[storage_end:count_at]):
                raise Refused("bad_layout", "a text tail or padding byte is not zero")
            continue
        if fd.codec == "table":
            target = descriptor.by_name(fd.target)
            if target is None:
                raise Refused("bad_layout", f"the table {fd.target} is not registered")
            _scan_record(h, region, target.descriptor, at, offsets)
            continue
        if fd.codec == "array" and fd.array_kind in (1, 2) and fd.elem_codec in ("bool", "int", "enum", "float32", "float64"):
            stride = fd.pieces[0][1] // fd.bound
            if fd.array_kind == 2:
                count_at = base + fd.pieces[1][0]
                count = int.from_bytes(region[count_at : count_at + 4], h.order, signed=True)
                if count < 0 or count > fd.bound:
                    raise Refused("bad_layout", "an array count exceeds its bound")
                if any(region[at + count * stride : at + fd.bound * stride]) or any(region[at + fd.bound * stride : count_at]):
                    raise Refused("bad_layout", "a dead array slot or padding is not zero")
            continue
        if fd.codec == "keyed" and fd.elem is not None and fd.elem.codec in ("bool", "int", "enum", "float32", "float64"):
            continue
        if fd.codec == "array" and fd.array_kind in (1, 2) and fd.elem_codec in ("table", "string", "text_optional"):
            target = descriptor.by_name(fd.target)
            if target is None:
                raise Refused("bad_layout", f"the table {fd.target} is not registered")
            count = fd.bound
            stride = fd.pieces[0][1] // fd.bound
            if fd.array_kind == 2:
                count_at = base + fd.pieces[1][0]
                count = int.from_bytes(region[count_at : count_at + 4], h.order, signed=True)
                if count < 0 or count > fd.bound:
                    raise Refused("bad_layout", "an array count exceeds its bound")
                if any(region[at + count * stride : at + fd.bound * stride]):
                    raise Refused("bad_layout", "a dead array slot is not zero")
                if any(region[at + fd.bound * stride : count_at]):
                    raise Refused("bad_layout", "array padding is not zero")
            for position in range(count):
                _scan_record(h, region, target.descriptor, at + position * stride, offsets)
            continue
        if fd.codec == "keyed" and fd.elem is not None and fd.elem.codec == "table":
            target = descriptor.by_name(fd.target)
            if target is None:
                raise Refused("bad_layout", "a keyed element is not registered")
            stride = fd.pieces[0][1] // fd.bound
            for position in range(fd.bound):
                _scan_record(h, region, target.descriptor, at + position * stride, offsets)
            continue
        if fd.codec == "optional":
            present_at = base + fd.pieces[-1][0]
            present = region[present_at]
            if present not in (0, 1):
                raise Refused("bad_layout", "an optional presence byte is not 0 or 1")
            if not present:
                if any(region[at : present_at]):
                    raise Refused("bad_layout", "an absent optional has nonzero payload")
            else:
                field = _optional_field(fd)
                _scan_record(h, region, SimpleNamespace(fields=(field,), padding=()), at, offsets)
                end = max((offset + size for offset, size in field.pieces), default=0)
                if any(region[at + end : present_at]):
                    raise Refused("bad_layout", "optional padding is not zero")
            continue
        if fd.codec == "union_array" and fd.array_kind in (1, 2):
            count = fd.bound
            stride = fd.pieces[0][1] // fd.bound
            if fd.array_kind == 2:
                count_at = base + fd.pieces[1][0]
                count = int.from_bytes(region[count_at : count_at + 4], h.order, signed=True)
                if count < 0 or count > fd.bound:
                    raise Refused("bad_layout", "a union array count exceeds its bound")
                if any(region[at + count * stride : at + fd.bound * stride]):
                    raise Refused("bad_layout", "a dead union array slot is not zero")
            field = SimpleNamespace(name=fd.name, codec="union", target=fd.target, at=0)
            for position in range(count):
                _scan_record(h, region, SimpleNamespace(fields=(field,), padding=()), at + position * stride, offsets)
            continue
        if fd.codec == "map" or (fd.codec == "union_array" and fd.array_kind == 3) or (fd.codec == "array" and fd.array_kind == 3 and fd.elem_codec in ("bool", "int", "enum", "float32", "float64", "table", "string", "text_optional")):
            delta = _i64(region, at, h.order)
            count = int.from_bytes(region[at + 8 : at + 12], h.order, signed=True)
            if count < 0 or (delta == 0) != (count == 0):
                raise Refused("bad_layout", "a list has an invalid count or reference")
            if any(region[at + 12 : at + 16]):
                raise Refused("bad_layout", "a list slot has nonzero padding")
            if count:
                start = at + delta
                end = start + count * fd.elem_size
                if start < h.node_record_end or end > h.node_end or start % fd.elem_align:
                    raise Refused("bad_layout", "a list leaves its extent or is unaligned")
                if any(start < other_end and other_start < end for other_start, other_end in h.ranges):
                    raise Refused("bad_layout", "lists overlap")
                h.ranges.append((start, end))
                if fd.codec == "union_array":
                    field = SimpleNamespace(name=fd.name, codec="union", target=fd.target, at=0)
                    for position in range(count):
                        _scan_record(h, region, SimpleNamespace(fields=(field,), padding=()), start + position * fd.elem_size, offsets)
                elif fd.codec == "map" or fd.elem_codec in ("table", "string", "text_optional"):
                    target = descriptor.by_name(fd.target)
                    if target is None:
                        raise Refused("bad_layout", "a list or map element is not registered")
                    previous: Any = None
                    for position in range(count):
                        element_at = start + position * fd.elem_size
                        _scan_record(h, region, target.descriptor, element_at, offsets)
                        if fd.codec == "map":
                            key = target.descriptor.fields[0]
                            key_at = element_at + key.at
                            if key.codec == "string":
                                if len(key.pieces) == 1:
                                    delta = _i64(region, key_at, h.order)
                                    if delta:
                                        text_start = key_at + delta
                                        used = int.from_bytes(region[text_start : text_start + 4], h.order)
                                        value: Any = bytes(region[text_start + 8 : text_start + 8 + used])
                                    else:
                                        value = b""
                                else:
                                    used_at = element_at + key.pieces[1][0]
                                    used = int.from_bytes(region[used_at : used_at + 4], h.order, signed=True)
                                    value = bytes(region[key_at : key_at + used])
                            elif key.codec in ("int", "enum", "bool"):
                                value = int.from_bytes(region[key_at : key_at + key.pieces[0][1]], h.order, signed=key.lo < 0)
                            else:
                                raise Refused("bad_layout", "a map key kind is unsupported")
                            if position and previous >= value:
                                raise Refused("bad_layout", "map keys do not ascend")
                            previous = value
            continue
        if fd.codec == "pointer_array":
            if fd.array_mode == "list":
                delta = _i64(region, at, h.order)
                count = int.from_bytes(region[at + 8 : at + 12], h.order, signed=True)
                if count < 0 or (delta == 0) != (count == 0) or any(region[at + 12 : at + 16]):
                    raise Refused("bad_layout", "a pointer list has an invalid slot")
                start = at + delta
                end = start + count * 8
                if count:
                    if start < h.node_record_end or end > h.node_end or start % fd.elem_align:
                        raise Refused("bad_layout", "a pointer list leaves its extent or is unaligned")
                    if any(start < other_end and other_start < end for other_start, other_end in h.ranges):
                        raise Refused("bad_layout", "lists overlap")
                    h.ranges.append((start, end))
            else:
                start = at
                count = fd.bound
                if fd.array_mode == "counted":
                    count_at = base + fd.pieces[1][0]
                    count = int.from_bytes(region[count_at : count_at + 4], h.order, signed=True)
                    if count < 0 or count > fd.bound:
                        raise Refused("bad_layout", "a pointer array count exceeds its bound")
                    if any(region[start + count * 8 : count_at]):
                        raise Refused("bad_layout", "a dead pointer slot or padding is not zero")
            field = SimpleNamespace(codec="pointer", name=fd.name, target=fd.target, at=0)
            for position in range(count):
                _scan_record(h, region, SimpleNamespace(fields=(field,), padding=()), start + position * 8, offsets)
            continue
        if fd.codec == "union":
            union_cls = descriptor._union_by_name(fd.target)
            if union_cls is None:
                raise Refused("bad_layout", "a union is not registered")
            ud = union_cls.descriptor
            tag = int.from_bytes(region[at : at + ud.tag_size], h.order)
            if tag > len(ud.arms):
                raise Refused("bad_layout", "a union tag names no arm")
            arm = ud.arms[tag - 1] if tag else None
            arm_size = arm.size if arm is not None else 0
            arm_at = at + ud.arm_offset
            if any(region[at + ud.tag_size : arm_at]) or any(region[arm_at + arm_size : at + ud.size]):
                raise Refused("bad_layout", "inactive union storage is not zero")
            if arm is not None and arm.field is not None:
                field = arm.field
                _scan_record(h, region, SimpleNamespace(fields=(field,), padding=()), arm_at, offsets)
            continue
        if fd.codec in ("string_blob", "bytes_blob"):
            delta = _i64(region, at, h.order)
            if delta:
                found = _directory_find(h, offsets, at + delta)
                expected = h.root.descriptor.string_blob_type_id if fd.codec == "string_blob" else h.root.descriptor.bytes_blob_type_id
                if found is None or found[1] != expected:
                    raise Refused("bad_layout", "a blob reference names no matching node")
            continue
        if fd.codec == "pointer":
            delta = _i64(region, at, h.order)
            if delta == 0:
                continue
            target_at = at + delta
            found = _directory_find(h, offsets, target_at)
            if found is None:
                raise Refused("bad_layout", "a reference names no directory entry")
            target_id = found[1]
            target = descriptor.by_name(fd.target)
            if target is None or target.descriptor.type_id != target_id:
                raise Refused("bad_layout", "a reference names another node type")
            continue
        raise Refused("bad_layout", _RETIRED)


def _directory_find(h: CookHandle, offsets: list[int], target: int) -> tuple[int, int] | None:
    position = _bisect_left(offsets, target)
    if position == len(offsets) or offsets[position] != target:
        return None
    return position, h.directory[position][1]


def check_cook(cls: type[Any], data: Any, base_offset: int = 0) -> CookHandle:
    """Open a cook and run cook-check's two passes over its region."""
    h = _open(cls, data, base_offset)
    if h.data_length % h.alignment != 0:
        raise Refused("bad_layout", "the data part is not a whole number of alignments")
    directory = _directory(h)
    _check_directory(h, directory)
    h.directory = directory
    offsets = [offset for offset, _ in directory]
    region = h.data[h.data_offset : h.data_offset + h.data_length]
    for i, entry in enumerate(directory):
        offset, type_id = entry
        h.node_base = offset
        h.node_end = directory[i + 1][0] if i + 1 < len(directory) else h.data_length
        h.ranges = []
        if type_id in (h.root.descriptor.bytes_blob_type_id, h.root.descriptor.string_blob_type_id):
            end = directory[i + 1][0] if i + 1 < len(directory) else h.data_length
            length = int.from_bytes(region[offset : offset + 4], h.order)
            needed = 8 + length + int(type_id == h.root.descriptor.string_blob_type_id)
            if needed > end - offset:
                raise Refused("bad_layout", "a blob length leaves its extent")
            if any(region[offset + 4 : offset + 8]) or any(region[offset + needed : end]):
                raise Refused("bad_layout", "blob padding is not zero")
            if type_id == h.root.descriptor.string_blob_type_id and region[offset + 8 + length] != 0:
                raise Refused("bad_layout", "a string blob is not terminated")
        else:
            cls2 = _node_class(type_id)
            h.node_record_end = offset + cls2.descriptor.size
            _scan_record(h, region, cls2.descriptor, offset, offsets)
            cursor = h.node_record_end
            for start, end in sorted(h.ranges):
                if any(region[cursor:start]):
                    raise Refused("bad_layout", "extent padding is not zero")
                cursor = end
            if any(region[cursor:h.node_end]):
                raise Refused("bad_layout", "extent slack is not zero")
    h.directory = directory
    return h


from .cookwrite import cook, cook_measure, _optional_field  # noqa: E402  (the write side of the one surface)


def dump_cook(handle: CookHandle) -> str:
    """Describe reachable records without copying their storage (SPEC-TABLES §7.5)."""
    region = handle.data[handle.data_offset : handle.data_offset + handle.data_length]
    lines: list[str] = []
    reached: dict[int, Any] = {}

    def integer(at: int, width: int, signed: bool) -> int:
        return int.from_bytes(region[at : at + width], handle.order, signed=signed)

    def emit(path: str, value: str) -> None:
        lines.append("  " + path + " = " + value)

    def text_at(at: int, length: int) -> str:
        out = '"'
        for byte in region[at : at + length]:
            if 0x20 <= byte < 0x7f and byte not in (0x22, 0x5c):
                out += chr(byte)
            else:
                out += f"\\x{byte:02x}"
        return out + f'" len={length}'

    def scalar(at: int, width: int, codec: str, signed: bool) -> str:
        if codec in ("float32", "float64"):
            raise Refused("bad_layout", "the cook dump does not define a float spelling")
        value = integer(at, width, signed)
        return ("true" if value else "false") if codec == "bool" else str(value)

    def record(desc: Any, at: int, prefix: str) -> Iterator[tuple[int, Any]]:
        for fd in desc.fields:
            name = prefix + "." + fd.name if prefix else fd.name
            slot = at + fd.at
            if fd.codec in ("bool", "int", "enum", "float32", "float64"):
                emit(name, scalar(slot, fd.pieces[0][1], fd.codec, fd.codec == "int" and fd.lo < 0))
            elif fd.codec in ("string", "bytes"):
                if fd.codec == "string" and len(fd.pieces) == 1:
                    delta = integer(slot, 8, True)
                    start = slot + delta if delta else 0
                    count = integer(start, 4, False) if delta else 0
                    emit(name, text_at(start + 8, count))
                else:
                    count = integer(at + fd.pieces[1][0], 4, True)
                    if count < 0 or count > fd.size:
                        raise Refused("bad_layout", "a text length leaves its buffer")
                    emit(name, text_at(slot, count))
            elif fd.codec == "table":
                child = descriptor.by_name(fd.target)
                if child is None:
                    raise Refused("bad_layout", "a nested table is not registered")
                yield from record(child.descriptor, slot, name)
            elif fd.codec == "array" and fd.array_kind == 3 and fd.elem_codec in ("bool", "int", "enum", "float32", "float64", "table", "string", "text_optional"):
                delta = integer(slot, 8, True)
                count = integer(slot + 8, 4, True)
                for position in range(count):
                    element_at = slot + delta + position * fd.elem_size
                    if fd.elem_codec in ("table", "string", "text_optional"):
                        child = descriptor.by_name(fd.target)
                        if child is None:
                            raise Refused("bad_layout", "a list table is not registered")
                        yield from record(child.descriptor, element_at, name + f"[{position}]")
                    else:
                        emit(name + f"[{position}]", scalar(element_at, fd.elem_size, fd.elem_codec, fd.elem_codec == "int" and fd.lo < 0))
                emit(name + "#count", str(count))
            elif fd.codec == "map":
                delta = integer(slot, 8, True)
                count = integer(slot + 8, 4, True)
                child = descriptor.by_name(fd.target)
                if child is None:
                    raise Refused("bad_layout", "a map entry is not registered")
                for position in range(count):
                    yield from record(child.descriptor, slot + delta + position * fd.elem_size, name + f"[{position}]")
                emit(name + "#count", str(count))
            elif fd.codec == "union_array":
                union_cls = descriptor._union_by_name(fd.target)
                if union_cls is None:
                    raise Refused("bad_layout", "a union is not registered")
                if fd.array_kind == 3:
                    delta = integer(slot, 8, True)
                    count = integer(slot + 8, 4, True)
                    start, stride = slot + delta, fd.elem_size
                else:
                    count = fd.bound
                    start, stride = slot, fd.pieces[0][1] // fd.bound
                for position in range(count):
                    union_at = start + position * stride
                    tag = integer(union_at, union_cls.descriptor.tag_size, False)
                    element_name = name + f"[{position}]"
                    emit(element_name + "#tag", str(tag))
                    arm = union_cls.descriptor.arms[tag - 1] if tag else None
                    if arm is not None and arm.field is not None:
                        yield from record(SimpleNamespace(fields=(arm.field,)), union_at + union_cls.descriptor.arm_offset, element_name)
                if fd.array_kind == 2:
                    emit(name + "#count", str(integer(at + fd.pieces[1][0], 4, True)))
                elif fd.array_kind == 3:
                    emit(name + "#count", str(count))
            elif fd.codec == "union":
                union_cls = descriptor._union_by_name(fd.target)
                if union_cls is None:
                    raise Refused("bad_layout", "a union is not registered")
                tag = integer(slot, union_cls.descriptor.tag_size, False)
                emit(name + "#tag", str(tag))
                arm = union_cls.descriptor.arms[tag - 1] if tag else None
                if arm is not None and arm.field is not None:
                    yield from record(SimpleNamespace(fields=(arm.field,)), slot + union_cls.descriptor.arm_offset, name)
            elif fd.codec in ("string_blob", "bytes_blob"):
                delta = integer(slot, 8, True)
                emit(name, "null" if delta == 0 else "-> @" + str(slot + delta))
            elif fd.codec == "pointer_array":
                child = descriptor.by_name(fd.target)
                if child is None:
                    raise Refused("bad_layout", "a pointer array target is not registered")
                if fd.array_mode == "list":
                    delta = integer(slot, 8, True)
                    count = integer(slot + 8, 4, True)
                    start = slot + delta
                else:
                    start = slot
                    count = fd.bound
                for position in range(count):
                    element = start + position * 8
                    delta = integer(element, 8, True)
                    emit(name + f"[{position}]", "null" if delta == 0 else "-> @" + str(element + delta))
                    if delta:
                        yield element + delta, child.descriptor
                if fd.array_mode == "counted":
                    emit(name + "#count", str(integer(at + fd.pieces[1][0], 4, True)))
                elif fd.array_mode == "list":
                    emit(name + "#count", str(count))
            elif fd.codec == "pointer":
                delta = integer(slot, 8, True)
                if delta == 0:
                    emit(name, "null")
                else:
                    target = slot + delta
                    child = descriptor.by_name(fd.target)
                    if child is None:
                        raise Refused("bad_layout", "a pointer target is not registered")
                    emit(name, "-> @" + str(target))
                    yield target, child.descriptor
            elif fd.codec == "array" and fd.array_kind in (1, 2) and fd.elem_codec in ("bool", "int", "enum", "float32", "float64"):
                stride = fd.pieces[0][1] // fd.bound
                for position in range(fd.bound):
                    emit(name + f"[{position}]", scalar(slot + position * stride, stride, fd.elem_codec, fd.elem_codec == "int" and fd.lo < 0))
                if fd.array_kind == 2:
                    emit(name + "#count", str(integer(at + fd.pieces[1][0], 4, True)))
            elif fd.codec == "array" and fd.array_kind in (1, 2) and fd.elem_codec in ("table", "string", "text_optional"):
                child = descriptor.by_name(fd.target)
                if child is None:
                    raise Refused("bad_layout", "an array element is not registered")
                stride = fd.pieces[0][1] // fd.bound
                for position in range(fd.bound):
                    yield from record(child.descriptor, slot + position * stride, name + f"[{position}]")
                if fd.array_kind == 2:
                    count = integer(at + fd.pieces[1][0], 4, True)
                    emit(name + "#count", str(count))
            elif fd.codec == "keyed" and fd.elem is not None and fd.elem.codec in ("bool", "int", "enum", "float32", "float64"):
                stride = fd.pieces[0][1] // fd.bound
                for position in range(fd.bound):
                    emit(name + f"[{position}]", scalar(slot + position * stride, stride, fd.elem.codec, fd.elem.codec == "int" and fd.elem.lo < 0))
            elif fd.codec == "keyed" and fd.elem is not None and fd.elem.codec == "table":
                child = descriptor.by_name(fd.target)
                if child is None:
                    raise Refused("bad_layout", "a keyed element is not registered")
                stride = fd.pieces[0][1] // fd.bound
                for position in range(fd.bound):
                    yield from record(child.descriptor, slot + position * stride, name + f"[{position}]")
            elif fd.codec == "optional":
                field = _optional_field(fd)
                yield from record(SimpleNamespace(fields=(field,)), slot, prefix)
                emit(name + "#present", "true" if region[at + fd.pieces[-1][0]] else "false")
            else:
                raise Refused("bad_layout", _RETIRED)

    def begin(at: int, desc: Any) -> Iterator[tuple[int, Any]] | None:
        if at in reached:
            if reached[at] is not desc:
                raise Refused("bad_layout", "one offset has two table types")
            return None
        if at < 0 or at + desc.size > len(region):
            raise Refused("bad_layout", "a record leaves the region")
        reached[at] = desc
        lines.append(f"node {len(reached) - 1} {desc.name} @{at}")
        return record(desc, at, "")

    first = begin(0, handle.root.descriptor)
    assert first is not None
    stack = [first]
    while stack:
        try:
            target, desc = next(stack[-1])
        except StopIteration:
            stack.pop()
            continue
        child = begin(target, desc)
        if child is not None:
            stack.append(child)
    return "\n".join(lines) + "\n"
