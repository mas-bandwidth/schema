"""Form-1 load: the whole value or Refused (docs/SPEC-TABLES.md §3, §3.1, §6.5).

<<<<<<< HEAD
One wire-alone framing and pointer-graph check over every byte of the file,
followed by one declaration-aware body and node-table decode. A field or node
record the reader cannot name is skipped and counted; damage anywhere refuses
the whole file and leaves the caller's report untouched.
=======
One reader over the file's id table, one body walk over the descriptors, and
one flat node table. A field the reader cannot name is skipped and counted;
damage anywhere refuses the whole file and returns nothing.

SKIPPED IS NOT UNREAD (§4, §6.5). A field, a node record or a payload this
reader cannot name is walked by the same wire-alone rules the engine's wire
check applies to every byte (internal/tablewire/wirecheck.go): its framing,
its text, its bool bytes, its floats, its element kinds, its references
against the id table, and its pointers, which join the one pointer graph that
is judged over every slot, known or not.
>>>>>>> 5043610a8358c251defbef6d002d940708010d41
"""

from typing import Any, TypeVar

from . import InvalidValue, LoadReport, Refused
from . import descriptor, kinds
from . import kind_blob
from .descriptor import FieldDesc, TableDesc

_T = TypeVar("_T")

FORM = 1
NODE_WIRE_ID = 0xFFFFFFFFFFFFFFFF
# the two reserved announcement ids, out of place in a file body (§3.3)
_BUILD_VERSION_WIRE_ID = 0xFFFFFFFFFFFFFFFE
_MESSAGE_VOCABULARY_WIRE_ID = 0xFFFFFFFFFFFFFFFD
# the reserved blob record type ids, fnv1a64("bytes") and fnv1a64("string")
# (§2.5): every reader knows them, and a blob record's body is its bytes
_BYTES_WIRE_TYPE_ID = 0x2F2EC0474F1C4FE4
_STRING_WIRE_TYPE_ID = 0x704BE0D8FAAFFC58
ID_BOUND = 4096
# THE BINARY NESTING BOUND (§4): a framed payload is one level deeper than the
# payload holding it
_MAX_DEPTH = 128
# a collection count above the int32 storage cap
_COUNT_CAP = 0x7FFFFFFF

_BUILD_VERSION_WIRE_ID = 0xFFFFFFFFFFFFFFFE
_MESSAGE_VOCABULARY_WIRE_ID = 0xFFFFFFFFFFFFFFFD
_MAX_DEPTH = 128
_MAX_COUNT = 0x7FFFFFFF
_MAX_BLOB_BYTES = 0xFFFFFFFF

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

<<<<<<< HEAD
_ARRAY_ELEMENT_KINDS: frozenset[int] = frozenset(
    {*range(1, 14), 15, 17, *range(18, 31)}
)
_KEYED_ELEMENT_KINDS: frozenset[int] = frozenset(
    {*range(1, 12), 13, *range(18, 31)}
)


def _fnv1a64(name: str) -> int:
    h = 0xCBF29CE484222325
    for b in name.encode("ascii"):
        h ^= b
        h = (h * 0x00000100000001B3) & 0xFFFFFFFFFFFFFFFF
    return h


_BYTES_WIRE_TYPE_ID = _fnv1a64("bytes")
_STRING_WIRE_TYPE_ID = _fnv1a64("string")
=======
# THE ELEMENT KINDS ARE CLOSED (§3, §4), judged under any count, 0 included:
# an array's (bool to table, union, pointer, the wide and fixed-point scalars
# and enum) and a keyed body's (bool to f64, table, the wide and fixed-point
# scalars and enum).
_ARRAY_ELEMENT_KINDS = frozenset(list(range(1, 14)) + [15, 17] + list(range(18, 30)) + [30])
_KEYED_ELEMENT_KINDS = frozenset(list(range(1, 12)) + [13] + list(range(18, 30)) + [30])
# the kinds whose payload is a framed container: a level when an arm holds one
_STRUCTURAL_KINDS = frozenset((13, 14, 15, 16))
>>>>>>> 5043610a8358c251defbef6d002d940708010d41


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
<<<<<<< HEAD
        "current",
        "root",
        "nodes",
        "record_types",
        "declared_ids",
        "variable",
        "saw_node",
=======
        "blob_pending",
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
        "nonfinite",
>>>>>>> 5043610a8358c251defbef6d002d940708010d41
    )

    def __init__(self, ids: list[int], variable: bool = True) -> None:
        self.ids = ids
        self.unknown = 0
<<<<<<< HEAD
        self.pending: list[tuple[Any, Any, bool, int, int]] = []
=======
        # every known pointer slot: its owner, its field and its node index; a
        # map's pointer value is rewritten to the map, (its key, the field)
        self.pending: list[tuple[Any, Any, int]] = []
        # every known byte buffer slot: its owner, its field and its node index
        self.blob_pending: list[tuple[Any, FieldDesc, int]] = []
>>>>>>> 5043610a8358c251defbef6d002d940708010d41
        self.current: Any = None
        self.root: Any = None
        # one entry per node record: the decoded table, or None for a blob or
        # a record this reader cannot place; types holds each record's type id
        self.nodes: list[Any] = []
<<<<<<< HEAD
        self.record_types: list[int] = []
        self.declared_ids: frozenset[int] = descriptor._declared_type_ids()
        self.variable = variable
=======
        self.types: list[int] = []
>>>>>>> 5043610a8358c251defbef6d002d940708010d41
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
        # None in the decode, which refuses a non-finite float at once; the
        # refusal path's file walk (_check_file) records one here and reads on
        self.nonfinite: list[bool] | None = None


class _WireState:
    __slots__ = ("ids", "edges", "non_finite", "node_table", "has_node_table")

    def __init__(self, ids: list[int]) -> None:
        self.ids = ids
        self.edges: list[int] = []
        self.non_finite = ""
        self.node_table: memoryview | None = None
        self.has_node_table = False


def _text_valid(buf: memoryview) -> bool:
    raw = bytes(buf)
    if b"\x00" in raw:
        return False
    try:
        raw.decode("utf-8")
    except UnicodeDecodeError:
        return False
    return True


def _scalar_check(buf: memoryview, kind: int, state: _WireState) -> None:
    if kind == 1:
        if int(buf[0]) > 1:
            raise Refused("wire_damaged", "a bool byte other than 0 or 1")
    elif kind == 10:
        bits32 = int.from_bytes(buf, "little")
        if (bits32 & 0x7F800000) == 0x7F800000 and not state.non_finite:
            state.non_finite = "a float the wire carries"
    elif kind == 11:
        bits64 = int.from_bytes(buf, "little")
        if (bits64 & 0x7FF0000000000000) == 0x7FF0000000000000 and not state.non_finite:
            state.non_finite = "a float the wire carries"


def _walk_payload(r: Reader, kind: int, depth: int, state: _WireState) -> None:
    width = _FIXED_WIDTH.get(kind)
    if width is not None:
        _scalar_check(r.take(width), kind, state)
        return
    if kind == 17:
        state.edges.append(r.varint())
        return
    if kind == 30:
        ref = r.varint()
        if ref > len(state.ids):
            raise Refused("wire_damaged", "an enum variant reference past the id table")
        return
    if kind == 15:
        arm_ref = r.varint()
        if arm_ref == 0:
            return
        if arm_ref > len(state.ids):
            raise Refused("wire_damaged", "an arm reference past the id table")
        arm_kind = r.byte()
        arm_len = r.varint()
        arm_slice = r.take(arm_len)
        next_depth = depth
        if arm_kind in (13, 14, 15, 16):
            if depth >= _MAX_DEPTH:
                raise Refused("wire_damaged", "a union arm nested past the binary bound")
            next_depth = depth + 1
        _walk_content(arm_slice, arm_kind, next_depth, state)
        return
    if kind in (12, 13, 14, 16, 31, 32):
        sub_len = r.varint()
        sub_slice = r.take(sub_len)
        _walk_content(sub_slice, kind, depth, state)
        return
    raise Refused("wire_damaged", f"a field carries the unknown kind {kind}")


def _walk_content(buf: memoryview, kind: int, depth: int, state: _WireState) -> None:
    width = _FIXED_WIDTH.get(kind)
    if width is not None:
        if len(buf) != width:
            raise Refused("wire_damaged", "an arm payload width does not match its kind")
        _scalar_check(buf, kind, state)
        return
    if kind == 12:
        if not _text_valid(buf):
            raise Refused("wire_damaged", "ill-formed text on wire")
        return
    if kind == 31:
        return
    if kind == 32:
        if len(buf) != 0:
            raise Refused("wire_damaged", "a void payload has nonzero length")
        return
    if kind in (13, 14, 16):
        if depth >= _MAX_DEPTH:
            raise Refused("wire_damaged", "framed body nested past the binary bound")
        if kind == 13:
            _walk_body(buf, False, depth + 1, state)
        elif kind == 14:
            _walk_array(buf, depth + 1, state)
        else:
            _walk_keyed(buf, depth + 1, state)
        return
    if kind in (15, 17, 30):
        sub_r = Reader(buf, 0, len(buf), state.ids)
        _walk_payload(sub_r, kind, depth, state)
        if sub_r.pos != sub_r.end:
            raise Refused("wire_damaged", "bytes left inside an arm after its payload")
        return
    raise Refused("wire_damaged", f"an arm carries the unknown kind {kind}")


def _walk_array(buf: memoryview, depth: int, state: _WireState) -> None:
    if len(buf) < 2:
        raise Refused("wire_damaged", "an array body too short for its header")
    sub_r = Reader(buf, 0, len(buf), state.ids)
    ek = sub_r.byte()
    count = sub_r.varint()
    if count > _MAX_COUNT:
        raise Refused("count_over_extent_cap", "an array count exceeds int32 cap")
    if ek not in _ARRAY_ELEMENT_KINDS:
        raise Refused("wire_damaged", "an array element kind outside the closed list")
    for _ in range(count):
        _walk_payload(sub_r, ek, depth, state)
    if sub_r.pos != sub_r.end:
        raise Refused("wire_damaged", "bytes left inside an array after its elements")


def _walk_keyed(buf: memoryview, depth: int, state: _WireState) -> None:
    if len(buf) < 2:
        raise Refused("wire_damaged", "a keyed body too short for its header")
    sub_r = Reader(buf, 0, len(buf), state.ids)
    ek = sub_r.byte()
    count = sub_r.varint()
    if count > _MAX_COUNT:
        raise Refused("count_over_extent_cap", "a keyed count exceeds int32 cap")
    if ek not in _KEYED_ELEMENT_KINDS:
        raise Refused("wire_damaged", "a keyed element kind outside the closed list")
    seen_keys: set[int] = set()
    for _ in range(count):
        key = sub_r.varint()
        if key == 0 or key > len(state.ids):
            raise Refused("wire_damaged", "a keyed slot key reference names no id")
        if key in seen_keys:
            raise Refused("wire_damaged", "the same keyed slot twice in one body")
        seen_keys.add(key)
        slot_len = sub_r.varint()
        slot_slice = sub_r.take(slot_len)
        _walk_content(slot_slice, ek, depth, state)
    if sub_r.pos != sub_r.end:
        raise Refused("wire_damaged", "bytes left inside a keyed body after its slots")


def _walk_body(buf: memoryview, root: bool, depth: int, state: _WireState) -> None:
    r = Reader(buf, 0, len(buf), state.ids)
    seen_fields: set[int] = set()
    while True:
        ref = r.varint()
        if ref == 0:
            break
        fid = r.id_at(ref)
        if fid in seen_fields:
            raise Refused("wire_damaged", "a body carries one field twice")
        seen_fields.add(fid)
        if fid in (_BUILD_VERSION_WIRE_ID, _MESSAGE_VOCABULARY_WIRE_ID):
            raise Refused("wire_damaged", "a reserved announcement id in a file body")
        kind = r.byte()
        if fid == NODE_WIRE_ID:
            if not root:
                raise Refused("wire_damaged", "the node table is only valid in the root")
            if kind != 12:
                raise Refused("wire_damaged", "the node table is not a kind-12 payload")
            nt_len = r.varint()
            state.node_table = r.take(nt_len)
            state.has_node_table = True
            continue
        _walk_payload(r, kind, depth, state)
    if r.pos != r.end:
        raise Refused("wire_damaged", "bytes between the body terminator and its end")


def _check_wire(
    root_body: memoryview, ids: list[int]
) -> tuple[list[tuple[int, memoryview]], str]:
    state = _WireState(ids)
    _walk_body(root_body, True, 0, state)
    children: list[list[int]] = [state.edges]
    blobs: list[bool] = []
    records: list[tuple[int, memoryview]] = []
    if state.has_node_table and state.node_table is not None:
        nt_r = Reader(state.node_table, 0, len(state.node_table), ids)
        declared_count = nt_r.varint()
        while nt_r.pos < nt_r.end:
            type_ref = nt_r.varint()
            if type_ref == 0:
                raise Refused("wire_damaged", "a node record has no type id")
            type_id = nt_r.id_at(type_ref)
            if type_id in (
                NODE_WIRE_ID,
                _BUILD_VERSION_WIRE_ID,
                _MESSAGE_VOCABULARY_WIRE_ID,
            ):
                raise Refused(
                    "wire_damaged", "a reserved id used as a node record type"
                )
            rec_len = nt_r.varint()
            rec_body = nt_r.take(rec_len)
            records.append((type_id, rec_body))
        if len(records) != declared_count:
            raise Refused("wire_damaged", "node table record count mismatch")
        for type_id, rec_body in records:
            if type_id in (_BYTES_WIRE_TYPE_ID, _STRING_WIRE_TYPE_ID):
                if len(rec_body) > _MAX_BLOB_BYTES:
                    raise Refused("blob_over_size_cap", "a blob record past the size cap")
                if type_id == _STRING_WIRE_TYPE_ID and not _text_valid(rec_body):
                    raise Refused(
                        "wire_damaged", "a string blob record that is not well-formed text"
                    )
                blobs.append(True)
                children.append([])
            else:
                state.edges = []
                _walk_body(rec_body, False, 0, state)
                blobs.append(False)
                children.append(state.edges)

    n = len(children)
    names = [0] * n
    for out in children:
        for index in out:
            if index == 0:
                continue
            if index - 1 >= n:
                raise Refused("wire_damaged", "a pointer index past the node table")
            names[index - 1] += 1
    for k, is_blob in enumerate(blobs):
        if is_blob and names[k + 1] > 1:
            raise Refused("blob_shared", "one blob record named from two slots")

    mark = [0] * n
    stack: list[list[int]] = [[0, 0]]
    mark[0] = 1
    while stack:
        top = stack[-1]
        node_i = top[0]
        next_i = top[1]
        out = children[node_i]
        if next_i == len(out):
            mark[node_i] = 2
            stack.pop()
            continue
        index = out[next_i]
        top[1] = next_i + 1
        if index == 0:
            continue
        target_i = index - 1
        m_state = mark[target_i]
        if m_state == 1:
            raise Refused(
                "wire_damaged",
                "a pointer to a node whose descent is still open: a cycle",
            )
        if m_state == 2:
            continue
        mark[target_i] = 1
        stack.append([target_i, 0])

    for k in range(1, n):
        if mark[k] == 0:
            raise Refused("wire_damaged", "a node record no pointer reaches")

    return records, state.non_finite


def _open_file(
    view: memoryview,
) -> tuple[list[int], list[tuple[int, memoryview]], str]:
    ids = _id_table(view)
    end = _body_end(view)
    records, non_finite = _check_wire(view[1:end], ids)
    return ids, records, non_finite


def _decode_node_records(
    root_desc: TableDesc, records: list[tuple[int, memoryview]], ctx: Ctx
) -> None:
    reachable = root_desc.reachable_ids
    for type_id, _ in records:
        ctx.record_types.append(type_id)
        if type_id in reachable:
            cls = descriptor.by_id(type_id)
            if cls is None:
                ctx.unknown += 1
                ctx.nodes.append(None)
            else:
                ctx.nodes.append(cls())
        else:
            ctx.unknown += 1
            ctx.nodes.append(None)
    for i, (_, rec_body) in enumerate(records):
        node = ctx.nodes[i]
        if node is not None:
            _read_body(Reader(rec_body, 0, len(rec_body), ctx.ids), node, ctx, False)


def load_table(cls: type[_T], data: Any, report: LoadReport | None = None) -> _T:
    view = buffer_view(data)
    ids, records, non_finite = _open_file(view)
    desc: TableDesc = getattr(cls, "descriptor")
    ctx = Ctx(ids, desc.variable)
    obj = cls()
    ctx.root = obj
    if desc.variable and records:
        _decode_node_records(desc, records, ctx)
    reader = Reader(view, 1, _body_end(view), ids)
    try:
        _read_body(reader, obj, ctx, True)
        if reader.pos != reader.end:
            raise Refused("wire_damaged", "bytes between the root terminator and the id table")
    except Refused as err:
        if err.reason != "wire_damaged":
            # The engine judges the whole file's framing before its decode
            # compares a byte with a declaration (internal/tablewire/
            # wirecheck.go, checkFile), so damage anywhere outranks a kind
            # mismatch or a range met first; the walk runs only on this
            # refusal path. A walk that names the decode's own reason keeps
            # the decode's refusal, which names the field.
            try:
                _check_file(view, ids)
            except Refused as walked:
                if walked.reason != err.reason:
                    raise walked from None
        raise
    _resolve(ctx)
    if non_finite:
        raise Refused("non_finite_float", non_finite)
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


<<<<<<< HEAD
=======
def read_body(data: Any, obj: Any, ctx: Ctx) -> None:
    """Read one nested body whose bytes are exactly data."""
    reader = Reader(data, 0, len(data), ctx.ids)
    if ctx.depth >= _MAX_DEPTH:
        raise Refused("wire_damaged", "a framed payload nested past the binary bound")
    ctx.depth += 1
    _read_body(reader, obj, ctx, False)
    ctx.depth -= 1
    if reader.pos != reader.end:
        raise Refused("wire_damaged", "bytes left inside a body")


>>>>>>> 5043610a8358c251defbef6d002d940708010d41
def _read_body(reader: Reader, obj: Any, ctx: Ctx, root: bool) -> None:
    descriptor_: TableDesc = obj.descriptor
    seen: set[int] = set()
    previous = ctx.current
    ctx.current = obj
    while True:
        reference = reader.varint()
        if reference == 0:
            break
        field_id = reader.id_at(reference)
        kind = reader.byte()
<<<<<<< HEAD
        if field_id == NODE_WIRE_ID:
            if not root:
                raise Refused("wire_damaged", "the node table is only valid in the root")
            if ctx.saw_node:
                raise Refused("wire_damaged", "the node table rides twice")
            if kind != 12:
                raise Refused("wire_damaged", "the node table is not a kind-12 payload")
            length = reader.varint()
            reader.take(length)
            ctx.saw_node = True
            continue
=======
        if field_id in seen:
            raise Refused("wire_damaged", "a body carries one field twice")
        seen.add(field_id)
>>>>>>> 5043610a8358c251defbef6d002d940708010d41
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


<<<<<<< HEAD
def _resolve(ctx: Ctx) -> None:
    for holder, slot_or_name, is_seq, index, target_id in ctx.pending:
        value: Any
        if index == 0 or not ctx.variable:
            value = None
        elif index == 1:
            raise Refused("wire_damaged", "a pointer to the root")
        elif 2 <= index <= len(ctx.nodes) + 1:
            rec_type = ctx.record_types[index - 2]
            if rec_type != target_id:
                if rec_type in ctx.declared_ids:
                    raise Refused(
                        "wire_damaged",
                        "a pointer whose target is not the type its slot requires",
                    )
                value = None
            else:
                value = ctx.nodes[index - 2]
        else:
            raise Refused("wire_damaged", "a node index is past the node table")
        if is_seq:
            holder[slot_or_name] = value
        else:
            setattr(holder, slot_or_name, value)
=======
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
    if field_id == _BUILD_VERSION_WIRE_ID or field_id == _MESSAGE_VOCABULARY_WIRE_ID:
        raise Refused("wire_damaged", "a reserved announcement id in a file body")
    _skip(reader, kind, ctx)
    ctx.unknown += 1


def _placeable(root: type[Any]) -> frozenset[int]:
    """THE TYPE IDS THIS ROOT CAN PLACE (§3.1): every table a pointer below
    the root targets, through by-value tables and pointers alike, and the
    reserved id of each byte buffer kind a *bytes or *string slot below it
    names (§2.5). A map's buffer value is not carried, so it places nothing."""
    cached: frozenset[int] | None = getattr(root, "_s_placeable", None)
    if cached is not None:
        return cached
    out: set[int] = set()
    visited: set[str] = set()
    stack: list[tuple[type[Any], bool]] = [(root, False)]
    while stack:
        cls, entry = stack.pop()
        desc: TableDesc = cls.descriptor
        if desc.name in visited:
            continue
        visited.add(desc.name)
        for field in _reached_fields(desc.fields):
            if field.codec == "bytes_blob" and not entry:
                out.add(_BYTES_WIRE_TYPE_ID)
            elif field.codec == "string_blob" and not entry:
                out.add(_STRING_WIRE_TYPE_ID)
            if not field.target:
                continue
            target = descriptor.by_name(field.target)
            if target is None:
                continue
            if field.pointer or field.codec == "pointer":
                out.add(target.descriptor.type_id)
            stack.append((target, field.codec == "map"))
    answer = frozenset(out)
    setattr(root, "_s_placeable", answer)
    return answer


def _reached_fields(fields: tuple[FieldDesc, ...]) -> list[FieldDesc]:
    """A table's fields, with each union's arm field lines in its place, so a
    pointer in an arm (at any union depth) is a pointer below the root."""
    out: list[FieldDesc] = []
    unions: list[str] = []
    for field in fields:
        out.append(field)
        if field.codec == "union" or field.codec == "union_array":
            unions.append(field.target)
    seen: set[str] = set()
    while unions:
        name = unions.pop()
        if name in seen:
            continue
        seen.add(name)
        cls = descriptor._union_by_name(name)
        if cls is None:
            continue
        for arm in cls.descriptor.arms:
            if arm.field is None:
                continue
            out.append(arm.field)
            if arm.field.codec == "union" or arm.field.codec == "union_array":
                unions.append(arm.field.target)
    return out


def _read_node_table(payload: memoryview, ctx: Ctx) -> None:
    root_cls = type(ctx.root)
    ctx.placeable = _placeable(root_cls)
    ctx.variable = bool(ctx.root.descriptor.variable)
    reader = Reader(payload, 0, len(payload), ctx.ids)
    count = reader.varint()
    if count > _COUNT_CAP:
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
        blob = type_id == _BYTES_WIRE_TYPE_ID or type_id == _STRING_WIRE_TYPE_ID
        if blob and length > kind_blob._BLOB_CAP:
            raise Refused("blob_over_size_cap", "a byte buffer record is past the storage cap")
        body = reader.take(length)
        edges: list[int] = []
        ctx.children.append(edges)
        types.append(type_id)
        if blob:
            # A BLOB RECORD IS ITS BYTES (§2.5, §3.1): a known *bytes or
            # *string slot places it (_resolve). A record of a kind no slot
            # below the root can place is one this reader cannot name, and
            # counts once; one of a placeable kind that no known slot names
            # belongs to a skipped field, which counted it already.
            if type_id not in ctx.placeable and ctx.variable:
                ctx.unknown += 1
            if type_id == _STRING_WIRE_TYPE_ID:
                _check_text(body, "a string blob record that is not well-formed text")
                nodes.append(kind_blob._BlobValue(True, bytes(body).decode("utf-8")))
            else:
                nodes.append(kind_blob._BlobValue(False, bytes(body)))
            ctx.blobs.append(True)
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
    for owner, slot, index in ctx.pending:
        # a map's pointer value names its map and (key, field); any other
        # slot names its table and its field
        if isinstance(owner, dict):
            key, field = slot
            where = f"{field.name}[{key!r}]"
        else:
            key, field = None, slot
            where = f"{owner.descriptor.name}.{field.name}"
        value: Any = None
        if index != 0:
            # the graph admitted the index: it names a record, never the root
            node = ctx.nodes[index - 2]
            if node is None:
                type_id = ctx.types[index - 2]
                if descriptor.by_id(type_id) is not None:
                    raise Refused(
                        "wire_damaged",
                        f"a pointer whose target is not the type its slot requires: {where}",
                    )
                # a record of a type this reader does not declare reads null
            else:
                target = descriptor.by_name(field.target)
                if target is None or type(node) is not target:
                    raise Refused(
                        "wire_damaged",
                        f"a pointer whose target is not the type its slot requires: {where}",
                    )
                value = node
        if isinstance(owner, dict):
            owner[key] = value
        else:
            setattr(owner, field.name, value)
    for owner, field, index in ctx.blob_pending:
        if index == 0:
            setattr(owner, field.name, None)
            continue
        # the graph admitted the index and refused a record named twice
        buffer = ctx.nodes[index - 2]
        if not isinstance(buffer, kind_blob._BlobValue):
            if descriptor.by_id(ctx.types[index - 2]) is not None:
                raise Refused("wire_damaged", "a byte buffer slot names a table record")
            # a record of a type this reader does not declare reads null; it
            # counted once when the node table was read
            setattr(owner, field.name, None)
            continue
        if buffer.string != (field.codec == "string_blob"):
            raise Refused("wire_damaged", "a byte buffer slot names the other buffer kind")
        setattr(owner, field.name, buffer.value)
>>>>>>> 5043610a8358c251defbef6d002d940708010d41


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


def _scalar(data: memoryview, kind: int, ctx: Ctx) -> None:
    if kind == 1:
        if data[0] > 1:
            raise Refused("wire_damaged", "a bool byte other than 0 or 1")
    elif kind == 10:
        if (int.from_bytes(data, "little") >> 23) & 0xFF == 0xFF:
            _not_finite(ctx)
    elif kind == 11:
        if (int.from_bytes(data, "little") >> 52) & 0x7FF == 0x7FF:
            _not_finite(ctx)


def _not_finite(ctx: Ctx) -> None:
    """Refuse a non-finite float in the decode, or record it on the file walk."""
    if ctx.nonfinite is None:
        raise Refused("non_finite_float", "a float the wire carries")
    ctx.nonfinite.append(True)


def _check_file(view: memoryview, ids: list[int]) -> None:
    """Raise the wire check's damage over the whole root body, if any.

    The root body is walked as a nested one is, except that it may hold the
    node table, whose records are each a body (a blob record is its bytes). A
    non-finite float is recorded and the walk reads on, as the engine's wire
    check does, so damage later in the file outranks it; with no damage the
    decode's own refusal stands (internal/tablewire/wirecheck.go, checkFile).
    """
    ctx = Ctx(ids)
    ctx.nonfinite = []
    reader = Reader(view, 1, _body_end(view), ids)
    seen: set[int] = set()
    while True:
        reference = reader.varint()
        if reference == 0:
            break
        field_id = reader.id_at(reference)
        if field_id in seen:
            raise Refused("wire_damaged", "the same field id twice in one body")
        seen.add(field_id)
        kind = reader.byte()
        if field_id == _BUILD_VERSION_WIRE_ID or field_id == _MESSAGE_VOCABULARY_WIRE_ID:
            raise Refused("wire_damaged", "a reserved announcement id out of place")
        if field_id == NODE_WIRE_ID:
            if kind != 12:
                raise Refused("wire_damaged", "the node table under a kind other than 12")
            _check_node_table(reader.take(reader.varint()), ctx)
            continue
        _skip_payload(reader, kind, ctx, 0)
    if reader.pos != reader.end:
        raise Refused("wire_damaged", "bytes between the root terminator and the id table")


def _check_node_table(data: memoryview, ctx: Ctx) -> None:
    reader = Reader(data, 0, len(data), ctx.ids)
    count = reader.varint()
    if count > _COUNT_CAP:
        raise Refused("count_over_extent_cap", "a node count above the int32 storage cap")
    for _ in range(count):
        type_id = reader.id_at(reader.varint())
        body = reader.take(reader.varint())
        if type_id == _STRING_WIRE_TYPE_ID:
            _check_text(body, "a string blob record that is not well-formed text")
            continue
        if type_id == _BYTES_WIRE_TYPE_ID:
            continue
        if (
            type_id == NODE_WIRE_ID
            or type_id == _BUILD_VERSION_WIRE_ID
            or type_id == _MESSAGE_VOCABULARY_WIRE_ID
        ):
            # A reserved id is damage anywhere but the body whose transport it
            # is, so it cannot be a record's type (internal/tablewire/
            # nodetable.go, scanNodeRecords).
            raise Refused("wire_damaged", "a reserved id in a record's type position")
        _skip_body(body, ctx, 0)
    if reader.pos != reader.end:
        raise Refused("wire_damaged", "bytes left inside the node table")


def _skip(reader: Reader, kind: int, ctx: Ctx) -> None:
    """Walk one SELF-FRAMED payload of kind at the reader: what follows an
    unknown field's kind byte."""
    _skip_payload(reader, kind, ctx, ctx.depth)


def _skip_payload(reader: Reader, kind: int, ctx: Ctx, depth: int) -> None:
    width = _FIXED_WIDTH.get(kind)
    if width is not None:
        if width > reader.end - reader.pos:
            raise Refused("wire_damaged", "a fixed-width payload cut short")
        _scalar(reader.take(width), kind, ctx)
        return
<<<<<<< HEAD
=======
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
>>>>>>> 5043610a8358c251defbef6d002d940708010d41
    if kind in (12, 13, 14, 16, 31, 32):
        length = reader.varint()
        _skip_content(reader.take(length), kind, ctx, depth)
        return
<<<<<<< HEAD
    if kind in (17, 30):
        reader.varint()
        return
    if kind == 15:
        arm = reader.varint()
        if arm == 0:
            return
        reader.byte()
        length = reader.varint()
        reader.take(length)
        return
    raise Refused("wire_damaged", f"a field carries the unknown kind {kind}")


def read_body(payload: memoryview, obj: Any, ctx: Ctx) -> None:
    reader = Reader(payload, 0, len(payload), ctx.ids)
    _read_body(reader, obj, ctx, False)
    if reader.pos != reader.end:
        raise Refused("wire_damaged", "bytes between a nested terminator and its end")
=======
    raise Refused("wire_damaged", f"kind {kind} is a kind no reader can skip")


def _skip_content(data: memoryview, kind: int, ctx: Ctx, depth: int) -> None:
    """Walk a payload already framed by a length; it must be consumed exactly."""
    width = _FIXED_WIDTH.get(kind)
    if width is not None:
        if len(data) != width:
            raise Refused("wire_damaged", "an L that is not its kind's width")
        _scalar(data, kind, ctx)
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
        if depth >= _MAX_DEPTH:
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
        if field_id == _BUILD_VERSION_WIRE_ID or field_id == _MESSAGE_VOCABULARY_WIRE_ID:
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
    if count > _COUNT_CAP:
        raise Refused("count_over_extent_cap", "a collection count above the int32 storage cap")
    return int(data[0]), count, reader


def _skip_array(data: memoryview, ctx: Ctx, depth: int) -> None:
    element, count, reader = _array_header(data, ctx)
    if element not in _ARRAY_ELEMENT_KINDS:
        raise Refused("wire_damaged", "a walked array's element kind outside the closed list")
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
        if depth >= _MAX_DEPTH:
            raise Refused("wire_damaged", "a union arm nested past the binary bound")
        depth += 1
    _skip_content(payload, kind, ctx, depth)
>>>>>>> 5043610a8358c251defbef6d002d940708010d41
