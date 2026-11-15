"""Form-1 load: the whole value or Refused (docs/SPEC-TABLES.md §3, §3.1).

One reader over the file's id table, one body walk over the descriptors, and
one flat node table. A field the reader cannot name is skipped and counted;
damage anywhere refuses the whole file and returns nothing.

SKIPPED IS NOT UNREAD (§4, §6.5). A field, a node record or a payload this
reader cannot name is walked by the same wire-alone rules the engine's wire
check applies to every byte (internal/tablewire/wirecheck.go): its framing,
its text, its bool bytes, its floats, its element kinds, its references
against the id table, and its pointers, which join the one pointer graph that
is judged over every slot, known or not.
"""

from typing import Any, TypeVar

from . import InvalidValue, LoadReport, Refused
from . import descriptor, kinds
from . import kind_bool, kind_bytes, kind_enum, kind_int, kind_pointer, kind_string, kind_table, kind_unsupported
from .descriptor import FieldDesc, TableDesc

_T = TypeVar("_T")

FORM = 1
NODE_WIRE_ID = 0xFFFFFFFFFFFFFFFF
# the two reserved announcement ids, out of place in a file body (§3.3)
BUILD_VERSION_WIRE_ID = 0xFFFFFFFFFFFFFFFE
MESSAGE_VOCABULARY_WIRE_ID = 0xFFFFFFFFFFFFFFFD
# the reserved blob record type ids, fnv1a64("bytes") and fnv1a64("string")
# (§2.5): every reader knows them, and a blob record's body is its bytes
BYTES_WIRE_TYPE_ID = 0x2F2EC0474F1C4FE4
STRING_WIRE_TYPE_ID = 0x704BE0D8FAAFFC58
ID_BOUND = 4096
# THE BINARY NESTING BOUND (§4): a framed payload is one level deeper than the
# payload holding it
MAX_DEPTH = 128
# a collection count above the int32 storage cap
COUNT_CAP = 0x7FFFFFFF

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

# THE ELEMENT KINDS ARE CLOSED (§3, §4), judged under any count, 0 included:
# an array's (bool to table, union, pointer, the wide and fixed-point scalars
# and enum) and a keyed body's (bool to f64, table, the wide and fixed-point
# scalars and enum).
_ARRAY_ELEMENT_KINDS = frozenset(list(range(1, 14)) + [15, 17] + list(range(18, 30)) + [30])
_KEYED_ELEMENT_KINDS = frozenset(list(range(1, 12)) + [13] + list(range(18, 30)) + [30])
# the kinds whose payload is a framed container: a level when an arm holds one
_STRUCTURAL_KINDS = frozenset((13, 14, 15, 16))


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
    __slots__ = (
        "ids",
        "unknown",
        "pending",
        "current",
        "root",
        "nodes",
        "types",
        "saw_node",
        "depth",
        "edges",
        "children",
        "blobs",
        "placeable",
        "variable",
    )

    def __init__(self, ids: list[int]) -> None:
        self.ids = ids
        self.unknown = 0
        # every known pointer slot: its owner, its field and its node index
        self.pending: list[tuple[Any, FieldDesc, int]] = []
        self.current: Any = None
        self.root: Any = None
        # one entry per node record: the decoded table, or None for a blob or
        # a record this reader cannot place; types holds each record's type id
        self.nodes: list[Any] = []
        self.types: list[int] = []
        self.saw_node = False
        # the framed level of the body being walked: 0 in the root body and in
        # a node record
        self.depth = 0
        # THE POINTER GRAPH (§6.5, "THE GRAPH, DEFINED ONCE"): edges is the
        # node indices of the node being walked, known slots and skipped ones
        # alike, in wire order; children[0] is the root's and children[k] is
        # record k's; blobs marks the blob records
        self.edges: list[int] = []
        self.children: list[list[int]] = [self.edges]
        self.blobs: list[bool] = []
        self.placeable: frozenset[int] = frozenset()
        self.variable = False


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
    if ctx.depth >= MAX_DEPTH:
        raise Refused("wire_damaged", "a framed payload nested past the binary bound")
    ctx.depth += 1
    _read_body(reader, obj, ctx, False)
    ctx.depth -= 1
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
        if field_id in seen:
            raise Refused("wire_damaged", "a body carries one field twice")
        seen.add(field_id)
        field = descriptor_.by_id.get(field_id)
        if field is None:
            _unknown_field(reader, field_id, kind, ctx, root)
            continue
        if kind != field.kind:
            raise Refused(
                "kind_mismatch",
                f"{descriptor_.name}.{field.name} rides under kind {kind}, not {field.kind}",
            )
        value = kinds._REGISTRY[field.codec].read(reader, field, ctx)
        setattr(obj, field.name, value)
    ctx.current = previous


def _unknown_field(reader: Reader, field_id: int, kind: int, ctx: Ctx, root: bool) -> None:
    """A field id the body's declaration does not name: the node table, a
    reserved id out of place, or an unknown field, which is walked by the wire
    rules and counted."""
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
        return
    if field_id == BUILD_VERSION_WIRE_ID or field_id == MESSAGE_VOCABULARY_WIRE_ID:
        raise Refused("wire_damaged", "a reserved announcement id in a file body")
    _skip(reader, kind, ctx)
    ctx.unknown += 1


def _placeable(root: type[Any]) -> frozenset[int]:
    """THE TYPE IDS THIS ROOT CAN PLACE (§3.1): every table a pointer below
    the root targets, through by-value tables and pointers alike."""
    cached: frozenset[int] | None = getattr(root, "_s_placeable", None)
    if cached is not None:
        return cached
    out: set[int] = set()
    visited: set[str] = set()
    stack = [root]
    while stack:
        cls = stack.pop()
        desc: TableDesc = cls.descriptor
        if desc.name in visited:
            continue
        visited.add(desc.name)
        for field in desc.fields:
            if not field.target:
                continue
            target = descriptor.by_name(field.target)
            if target is None:
                continue
            if field.pointer or field.codec == "pointer":
                out.add(target.descriptor.type_id)
            stack.append(target)
    answer = frozenset(out)
    setattr(root, "_s_placeable", answer)
    return answer


def _read_node_table(payload: memoryview, ctx: Ctx) -> None:
    root_cls = type(ctx.root)
    ctx.placeable = _placeable(root_cls)
    ctx.variable = bool(ctx.root.descriptor.variable)
    reader = Reader(payload, 0, len(payload), ctx.ids)
    count = reader.varint()
    if count > COUNT_CAP:
        raise Refused("count_over_extent_cap", "a node count above the int32 storage cap")
    root_edges = ctx.edges
    nodes: list[Any] = []
    types: list[int] = []
    for _ in range(count):
        type_reference = reader.varint()
        if type_reference == 0:
            raise Refused("wire_damaged", "a node record has no type id")
        type_id = reader.id_at(type_reference)
        length = reader.varint()
        body = reader.take(length)
        edges: list[int] = []
        ctx.children.append(edges)
        types.append(type_id)
        if type_id == BYTES_WIRE_TYPE_ID or type_id == STRING_WIRE_TYPE_ID:
            # A BLOB RECORD IS ITS BYTES (§2.5, §3.1); this build places none
            if type_id == STRING_WIRE_TYPE_ID:
                _check_text(body, "a string blob record that is not well-formed text")
            ctx.blobs.append(True)
            nodes.append(None)
            if ctx.variable:
                ctx.unknown += 1
            continue
        ctx.blobs.append(False)
        cls = descriptor.by_id(type_id) if type_id in ctx.placeable else None
        ctx.edges = edges
        saved_depth = ctx.depth
        ctx.depth = 0
        if cls is None:
            # a record this reader cannot place keeps its index, and its body
            # is walked by the wire rules
            _skip_body(body, ctx, 0)
            nodes.append(None)
            if ctx.variable:
                ctx.unknown += 1
        else:
            node = cls()
            nodes.append(node)
            sub = Reader(body, 0, len(body), ctx.ids)
            _read_body(sub, node, ctx, False)
            if sub.pos != sub.end:
                raise Refused("wire_damaged", "bytes left inside a node record")
        ctx.depth = saved_depth
        ctx.edges = root_edges
    if reader.pos != reader.end:
        raise Refused("wire_damaged", "bytes left inside the node table")
    ctx.nodes = nodes
    ctx.types = types


def _graph(ctx: Ctx) -> None:
    """Judge the pointer graph over every slot, known or skipped: an index
    past the node table, one blob record named from two slots, a cycle (a
    pointer to the root is one, its descent open for the whole walk) and a
    record no pointer reaches."""
    children = ctx.children
    n = len(children)
    names = [0] * n
    for out in children:
        for index in out:
            if index == 0:
                continue
            if index - 1 >= n:
                raise Refused("wire_damaged", "a pointer index past the node table")
            names[index - 1] += 1
    for k, blob in enumerate(ctx.blobs):
        if blob and names[k + 1] > 1:
            raise Refused("blob_shared", "one blob record named from two slots")
    # 0 unvisited, 1 open, 2 closed
    mark = [0] * n
    stack: list[list[int]] = [[0, 0]]
    mark[0] = 1
    while stack:
        top = stack[-1]
        out = children[top[0]]
        if top[1] == len(out):
            mark[top[0]] = 2
            stack.pop()
            continue
        index = out[top[1]]
        top[1] += 1
        if index == 0:
            continue
        target = index - 1
        if mark[target] == 1:
            raise Refused("wire_damaged", "a pointer to a node whose descent is still open: a cycle")
        if mark[target] == 2:
            continue
        mark[target] = 1
        stack.append([target, 0])
    for k in range(1, n):
        if mark[k] == 0:
            raise Refused("wire_damaged", "a node record no pointer reaches")


def _resolve(ctx: Ctx) -> None:
    if len(ctx.children) > 1 or ctx.edges:
        _graph(ctx)
    for owner, field, index in ctx.pending:
        value: Any = None
        if index != 0:
            # the graph admitted the index: it names a record, never the root
            node = ctx.nodes[index - 2]
            if node is None:
                type_id = ctx.types[index - 2]
                if (
                    type_id == BYTES_WIRE_TYPE_ID
                    or type_id == STRING_WIRE_TYPE_ID
                    or descriptor.by_id(type_id) is not None
                ):
                    raise Refused(
                        "wire_damaged",
                        f"a pointer whose target is not the type its slot requires: {owner.descriptor.name}.{field.name}",
                    )
                # a record of a type this reader does not declare reads null
            else:
                target = descriptor.by_name(field.target)
                if target is None or type(node) is not target:
                    raise Refused(
                        "wire_damaged",
                        f"a pointer whose target is not the type its slot requires: {owner.descriptor.name}.{field.name}",
                    )
                value = node
        setattr(owner, field.name, value)


# THE SKIP WALK: the engine's declaration-free wire check (wirecheck.go) over
# one payload this reader does not name.


def _check_text(data: memoryview, why: str) -> None:
    raw = bytes(data)
    if b"\x00" in raw:
        raise Refused("wire_damaged", why)
    try:
        raw.decode("utf-8")
    except UnicodeDecodeError:
        raise Refused("wire_damaged", why) from None


def _scalar(data: memoryview, kind: int) -> None:
    if kind == 1:
        if data[0] > 1:
            raise Refused("wire_damaged", "a bool byte other than 0 or 1")
    elif kind == 10:
        if (int.from_bytes(data, "little") >> 23) & 0xFF == 0xFF:
            raise Refused("non_finite_float", "a float the wire carries")
    elif kind == 11:
        if (int.from_bytes(data, "little") >> 52) & 0x7FF == 0x7FF:
            raise Refused("non_finite_float", "a float the wire carries")


def _skip(reader: Reader, kind: int, ctx: Ctx) -> None:
    """Walk one SELF-FRAMED payload of kind at the reader: what follows an
    unknown field's kind byte."""
    _skip_payload(reader, kind, ctx, ctx.depth)


def _skip_payload(reader: Reader, kind: int, ctx: Ctx, depth: int) -> None:
    width = _FIXED_WIDTH.get(kind)
    if width is not None:
        if width > reader.end - reader.pos:
            raise Refused("wire_damaged", "a fixed-width payload cut short")
        _scalar(reader.take(width), kind)
        return
    if kind == 17:
        ctx.edges.append(reader.varint())
        return
    if kind == 30:
        if reader.varint() > len(ctx.ids):
            raise Refused("wire_damaged", "a variant reference past the id table")
        return
    if kind == 15:
        _skip_union(reader, ctx, depth)
        return
    if kind in (12, 13, 14, 16, 31, 32):
        length = reader.varint()
        _skip_content(reader.take(length), kind, ctx, depth)
        return
    raise Refused("wire_damaged", f"kind {kind} is a kind no reader can skip")


def _skip_content(data: memoryview, kind: int, ctx: Ctx, depth: int) -> None:
    """Walk a payload already framed by a length; it must be consumed exactly."""
    width = _FIXED_WIDTH.get(kind)
    if width is not None:
        if len(data) != width:
            raise Refused("wire_damaged", "an L that is not its kind's width")
        _scalar(data, kind)
        return
    if kind == 12:
        _check_text(data, "ill-formed text")
        return
    if kind == 31:
        return  # opaque: the one kind whose meaning a reader may not know (§3)
    if kind == 32:
        if len(data) != 0:
            raise Refused("wire_damaged", "kind 32 with a nonzero L")
        return
    if kind in (13, 14, 16):
        if depth >= MAX_DEPTH:
            raise Refused("wire_damaged", "a framed payload nested past the binary bound")
        if kind == 13:
            _skip_body(data, ctx, depth + 1)
        elif kind == 14:
            _skip_array(data, ctx, depth + 1)
        else:
            _skip_keyed(data, ctx, depth + 1)
        return
    if kind in (15, 17, 30):
        reader = Reader(data, 0, len(data), ctx.ids)
        _skip_payload(reader, kind, ctx, depth)
        if reader.pos != reader.end:
            raise Refused("wire_damaged", "bytes left inside an arm after its payload")
        return
    raise Refused("wire_damaged", f"kind {kind} is a kind no reader can skip")


def _skip_body(data: memoryview, ctx: Ctx, depth: int) -> None:
    """Walk one table body that ends with its terminator as its last byte."""
    reader = Reader(data, 0, len(data), ctx.ids)
    seen: set[int] = set()
    while True:
        reference = reader.varint()
        if reference == 0:
            if reader.pos != reader.end:
                raise Refused("wire_damaged", "bytes after a body's terminator inside its L")
            return
        field_id = reader.id_at(reference)
        if field_id in seen:
            raise Refused("wire_damaged", "the same field id twice in one body")
        seen.add(field_id)
        kind = reader.byte()
        if field_id == BUILD_VERSION_WIRE_ID or field_id == MESSAGE_VOCABULARY_WIRE_ID:
            raise Refused("wire_damaged", "a reserved announcement id out of place")
        if field_id == NODE_WIRE_ID:
            raise Refused("wire_damaged", "the node table's reserved id in a nested body")
        _skip_payload(reader, kind, ctx, depth)


def _array_header(data: memoryview, ctx: Ctx) -> tuple[int, int, Reader]:
    # THE HEADER LIES INSIDE ITS OWN L (§4)
    if len(data) < 2:
        raise Refused("wire_damaged", "an array, keyed or map body too short for its header")
    reader = Reader(data, 1, len(data), ctx.ids)
    count = reader.varint()
    if count > COUNT_CAP:
        raise Refused("count_over_extent_cap", "a collection count above the int32 storage cap")
    return int(data[0]), count, reader


def _skip_array(data: memoryview, ctx: Ctx, depth: int) -> None:
    element, count, reader = _array_header(data, ctx)
    if element not in _ARRAY_ELEMENT_KINDS:
        raise Refused("wire_damaged", "an array element kind outside the closed list")
    for _ in range(count):
        _skip_payload(reader, element, ctx, depth)
    if reader.pos != reader.end:
        raise Refused("wire_damaged", "bytes left inside an array's L after its elements")


def _skip_keyed(data: memoryview, ctx: Ctx, depth: int) -> None:
    element, count, reader = _array_header(data, ctx)
    if element not in _KEYED_ELEMENT_KINDS:
        raise Refused("wire_damaged", "a keyed element kind outside the closed list")
    seen: set[int] = set()
    for _ in range(count):
        key = reader.varint()
        if key == 0 or key > len(ctx.ids):
            raise Refused("wire_damaged", "a keyed slot whose key reference names no id")
        if key in seen:
            raise Refused("wire_damaged", "the same keyed slot twice in one keyed body")
        seen.add(key)
        length = reader.varint()
        _skip_content(reader.take(length), element, ctx, depth)
    if reader.pos != reader.end:
        raise Refused("wire_damaged", "bytes left inside a keyed array's L after its slots")


def _skip_union(reader: Reader, ctx: Ctx, depth: int) -> None:
    reference = reader.varint()
    if reference == 0:
        return
    if reference > len(ctx.ids):
        raise Refused("wire_damaged", "an arm reference past the id table")
    kind = reader.byte()
    length = reader.varint()
    payload = reader.take(length)
    # A UNION IS A LEVEL WHEN ITS ARM IS STRUCTURAL (§4)
    if kind in _STRUCTURAL_KINDS:
        if depth >= MAX_DEPTH:
            raise Refused("wire_damaged", "a union arm nested past the binary bound")
        depth += 1
    _skip_content(payload, kind, ctx, depth)
