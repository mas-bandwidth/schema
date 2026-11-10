"""Form-1 load: the whole value or Refused (docs/SPEC-TABLES.md §3, §3.1).

One reader over the file's id table, one body walk over the descriptors, and
one flat node table. A field the reader cannot name is skipped and counted;
damage anywhere refuses the whole file and returns nothing.
"""

from typing import Any, TypeVar

from . import InvalidValue, LoadReport, Refused
from . import descriptor, kinds
from . import kind_bool, kind_int, kind_pointer, kind_table, kind_unsupported
from .descriptor import FieldDesc, TableDesc

_T = TypeVar("_T")

FORM = 1
NODE_WIRE_ID = 0xFFFFFFFFFFFFFFFF
ID_BOUND = 4096

_FIXED_WIDTH = {
    1: 1,
    2: 1,
    3: 2,
    4: 4,
    5: 8,
    6: 1,
    7: 2,
    8: 4,
    9: 8,
    10: 4,
    11: 8,
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


def buffer_view(data: Any) -> memoryview:
    if isinstance(data, bytes):
        return memoryview(data)
    if isinstance(data, bytearray):
        return memoryview(data)
    if isinstance(data, memoryview):
        if data.format != "B":
            raise Refused("invalid_buffer", "a memoryview must have format B")
        return data
    raise Refused("invalid_buffer", "a read takes bytes, bytearray or a B memoryview")


class Reader:
    __slots__ = ("data", "pos", "end", "ids")

    data: memoryview

    def __init__(self, data: memoryview, pos: int, end: int, ids: list[int]) -> None:
        self.data = data
        self.pos = pos
        self.end = end
        self.ids = ids

    def byte(self) -> int:
        if self.pos >= self.end:
            raise Refused("wire_damaged", "a body ended before its framing")
        value = int(self.data[self.pos])
        self.pos += 1
        return value

    def take(self, n: int) -> memoryview:
        if n < 0 or self.pos + n > self.end:
            raise Refused("wire_damaged", "a length runs past its body")
        start = self.pos
        self.pos += n
        return self.data[start : start + n]

    def varint(self) -> int:
        value = 0
        shift = 0
        count = 0
        while True:
            raw = self.byte()
            count += 1
            if count > 10:
                raise Refused("wire_damaged", "a reference is longer than ten bytes")
            if count == 10 and (raw & 0x7F) > 1:
                raise Refused("wire_damaged", "a reference crosses the 64th bit")
            value |= (raw & 0x7F) << shift
            if not raw & 0x80:
                if count > 1 and raw == 0:
                    raise Refused("wire_damaged", "a non-canonical reference")
                return value
            shift += 7

    def id_at(self, reference: int) -> int:
        if reference < 1 or reference > len(self.ids):
            raise Refused("wire_damaged", "a reference is past the id table")
        return self.ids[reference - 1]


class Ctx:
    __slots__ = ("ids", "unknown", "pending", "current", "root", "nodes", "saw_node")

    def __init__(self, ids: list[int]) -> None:
        self.ids = ids
        self.unknown = 0
        self.pending: list[tuple[Any, str, int]] = []
        self.current: Any = None
        self.root: Any = None
        self.nodes: list[Any] = []
        self.saw_node = False


def load_table(cls: type[_T], data: Any, report: LoadReport | None = None) -> _T:
    view = buffer_view(data)
    ids = _id_table(view)
    ctx = Ctx(ids)
    obj = cls()
    ctx.root = obj
    reader = Reader(view, 1, _body_end(view), ids)
    _read_body(reader, obj, ctx, True)
    if reader.pos != reader.end:
        raise Refused("wire_damaged", "bytes between the root terminator and the id table")
    _resolve(ctx)
    if report is not None:
        report.unknown = ctx.unknown
        report.widened = 0
        report.retained = 0
        report.retain_lost = 0
    return obj


def _body_end(view: memoryview) -> int:
    count = int.from_bytes(view[len(view) - 8 :], "little")
    return len(view) - 8 - 8 * count


def _id_table(view: memoryview) -> list[int]:
    n = len(view)
    if n < 10:
        raise Refused("wire_damaged", "a file is shorter than its framing")
    form = int(view[0])
    if form != FORM:
        if form == 2:
            raise Refused("message_form_as_file", "a message form is not a table file")
        if form == 3:
            raise Refused("fixed_form_as_file", "a fixed form is not this reader's file")
        raise Refused("newer_form", f"form {form} is not a form this build carries")
    count = int.from_bytes(view[n - 8 :], "little")
    if count > ID_BOUND:
        raise Refused("wire_damaged", f"an id table of {count} entries is past the bound")
    start = n - 8 - 8 * count
    if start < 2:
        raise Refused("wire_damaged", "an id table leaves no room for a root body")
    ids: list[int] = []
    seen: set[int] = set()
    for index in range(count):
        offset = start + 8 * index
        table_id = int.from_bytes(view[offset : offset + 8], "little")
        if table_id in seen:
            raise Refused("wire_damaged", "an id table carries one id twice")
        seen.add(table_id)
        ids.append(table_id)
    return ids


def read_body(data: Any, obj: Any, ctx: Ctx) -> None:
    """Read one nested body whose bytes are exactly data."""
    reader = Reader(data, 0, len(data), ctx.ids)
    _read_body(reader, obj, ctx, False)
    if reader.pos != reader.end:
        raise Refused("wire_damaged", "bytes left inside a body")


def _read_body(reader: Reader, obj: Any, ctx: Ctx, root: bool) -> None:
    descriptor_: TableDesc = obj.descriptor
    previous = ctx.current
    ctx.current = obj
    seen: set[int] = set()
    while True:
        reference = reader.varint()
        if reference == 0:
            break
        field_id = reader.id_at(reference)
        kind = reader.byte()
        if field_id == NODE_WIRE_ID:
            if not root:
                raise Refused("wire_damaged", "the node table is not in the root body")
            if ctx.saw_node:
                raise Refused("wire_damaged", "the node table rides twice")
            if kind != 12:
                raise Refused("wire_damaged", "the node table is not a kind-12 payload")
            length = reader.varint()
            _read_node_table(reader.take(length), ctx)
            ctx.saw_node = True
            continue
        field = descriptor_.by_id.get(field_id)
        if field is None:
            _skip(reader, kind)
            ctx.unknown += 1
            continue
        if field.field_id in seen:
            raise Refused("wire_damaged", "a body carries one field twice")
        seen.add(field.field_id)
        if kind != field.kind:
            raise Refused(
                "kind_mismatch",
                f"{descriptor_.name}.{field.name} rides under kind {kind}, not {field.kind}",
            )
        value = kinds.REGISTRY[field.codec].read(reader, field, ctx)
        setattr(obj, field.name, value)
    ctx.current = previous


def _read_node_table(payload: memoryview, ctx: Ctx) -> None:
    reader = Reader(payload, 0, len(payload), ctx.ids)
    count = reader.varint()
    nodes: list[Any] = []
    for _ in range(count):
        type_reference = reader.varint()
        if type_reference == 0:
            raise Refused("wire_damaged", "a node record has no type id")
        type_id = reader.id_at(type_reference)
        cls = descriptor.by_id(type_id)
        if cls is None:
            raise Refused("wire_damaged", f"a node names the unknown type id {type_id:#x}")
        length = reader.varint()
        body = reader.take(length)
        node = cls()
        nodes.append(node)
        _read_body(Reader(body, 0, len(body), ctx.ids), node, ctx, False)
    if reader.pos != reader.end:
        raise Refused("wire_damaged", "bytes left inside the node table")
    ctx.nodes = nodes


def _resolve(ctx: Ctx) -> None:
    for owner, name, index in ctx.pending:
        value: Any
        if index == 0:
            value = None
        elif index == 1:
            value = ctx.root
        elif 2 <= index <= len(ctx.nodes) + 1:
            value = ctx.nodes[index - 2]
        else:
            raise Refused("wire_damaged", "a node index is past the node table")
        setattr(owner, name, value)


def _skip(reader: Reader, kind: int) -> None:
    width = _FIXED_WIDTH.get(kind)
    if width is not None:
        reader.take(width)
        return
    if kind in (17, 30):
        reader.varint()
        return
    if kind in (12, 13, 14, 16, 31, 32):
        length = reader.varint()
        reader.take(length)
        return
    if kind == 15:
        arm = reader.varint()
        if arm != 0:
            reader.byte()
            length = reader.varint()
            reader.take(length)
        return
    raise Refused("wire_damaged", f"kind {kind} is not in this build's closed set")
