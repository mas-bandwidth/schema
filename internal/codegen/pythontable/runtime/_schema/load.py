"""Form-1 load: the whole value or Refused (docs/SPEC-TABLES.md §3, §3.1, §6.5).

One wire-alone framing and pointer-graph check over every byte of the file,
followed by one declaration-aware body and node-table decode. A field or node
record the reader cannot name is skipped and counted; damage anywhere refuses
the whole file and leaves the caller's report untouched.
"""

from typing import Any, TypeVar

from . import InvalidValue, LoadReport, Refused
from . import descriptor, kinds
from . import (
    kind_bool,
    kind_int,
    kind_pointer,
    kind_pointer_array,
    kind_table,
    kind_unsupported,
)
from .descriptor import FieldDesc, TableDesc

_T = TypeVar("_T")

FORM = 1
NODE_WIRE_ID = 0xFFFFFFFFFFFFFFFF
ID_BOUND = 4096

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
        "record_types",
        "declared_ids",
        "variable",
        "saw_node",
    )

    def __init__(self, ids: list[int], variable: bool = True) -> None:
        self.ids = ids
        self.unknown = 0
        self.pending: list[tuple[Any, Any, bool, int, int]] = []
        self.current: Any = None
        self.root: Any = None
        self.nodes: list[Any] = []
        self.record_types: list[int] = []
        self.declared_ids: frozenset[int] = descriptor._declared_type_ids()
        self.variable = variable
        self.saw_node = False


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
    _read_body(reader, obj, ctx, True)
    if reader.pos != reader.end:
        raise Refused("wire_damaged", "bytes between the root terminator and the id table")
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
        value = kinds._REGISTRY[field.codec].read(reader, field, ctx)
        setattr(obj, field.name, value)
    ctx.current = previous


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


def _skip(reader: Reader, kind: int) -> None:
    width = _FIXED_WIDTH.get(kind)
    if width is not None:
        reader.take(width)
        return
    if kind in (12, 13, 14, 16, 31, 32):
        length = reader.varint()
        reader.take(length)
        return
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
