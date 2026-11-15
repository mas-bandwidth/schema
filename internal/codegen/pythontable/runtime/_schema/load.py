"""Form-1 load: the whole value or Refused (docs/SPEC-TABLES.md §3, §3.1).

One reader over the file's id table, one body walk over the descriptors, and
one flat node table. A field the reader cannot name is skipped and counted;
damage anywhere refuses the whole file and returns nothing.
"""

from typing import Any, TypeVar

from . import InvalidValue, LoadReport, Refused
from . import descriptor, kinds
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
        self.pending: list[tuple[Any, Any, int]] = []
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
    try:
        _read_body(reader, obj, ctx, True)
        if reader.pos != reader.end:
            raise Refused("wire_damaged", "bytes between the root terminator and the id table")
    except Refused as err:
        if err.reason != "wire_damaged":
            # The engine judges the whole file's framing before its decode
            # compares a byte with a declaration, so damage anywhere outranks
            # a kind mismatch or a range met first; the walk runs only on this
            # refusal path.
            _check_file(view, ids)
        raise
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
        if field_id in seen:
            raise Refused("wire_damaged", "a body carries one field twice")
        seen.add(field_id)
        field = descriptor_.by_id.get(field_id)
        if field is None:
            if field_id in _RESERVED_IDS:
                raise Refused("wire_damaged", "a reserved announcement id out of place")
            _skip(reader, kind)
            ctx.unknown += 1
            continue
        if kind != field.kind:
            raise Refused(
                "kind_mismatch",
                f"{descriptor_.name}.{field.name} rides under kind {kind}, not {field.kind}",
            )
        value = kinds._REGISTRY[field.codec].read(reader, field, ctx)
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
        if isinstance(owner, dict):
            owner[name] = value
        else:
            setattr(owner, name, value)


def _skip(reader: Reader, kind: int, depth: int = 0, nonfinite: list[bool] | None = None) -> None:
    """Step past one self-framed payload of kind, judging every byte of it.

    A value this reader cannot name is skipped, never trusted: it is held to
    the same wire-alone rules the Go engine's wire check applies to every
    payload before its decode (internal/tablewire/wirecheck.go, payload), so a
    skipped value the engine refuses is refused here with the same reason.

    On the decode's own skip `nonfinite` is None and a non-finite float is
    refused at once; the refusal path's file walk passes a list, records the
    float and reads on, as the engine's wire check does, so damage later in
    the file still outranks it.
    """
    width = _FIXED_WIDTH.get(kind)
    if width is not None:
        _check_scalar(reader.take(width), kind, nonfinite)
        return
    if kind == 17:
        reader.varint()
        return
    if kind == 30:
        if reader.varint() > len(reader.ids):
            raise Refused("wire_damaged", "a variant reference past the id table")
        return
    if kind == 15:
        reference = reader.varint()
        if reference == 0:
            return
        if reference > len(reader.ids):
            raise Refused("wire_damaged", "an arm reference past the id table")
        arm_kind = reader.byte()
        payload = reader.take(reader.varint())
        if arm_kind in _STRUCTURAL:
            if depth >= _MAX_DEPTH:
                raise Refused("wire_damaged", "a union arm nested past the binary bound")
            depth += 1
        _check_content(payload, arm_kind, reader.ids, depth, nonfinite)
        return
    if kind in (12, 13, 14, 16, 31, 32):
        _check_content(reader.take(reader.varint()), kind, reader.ids, depth, nonfinite)
        return
    raise Refused("wire_damaged", f"kind {kind} is a kind no reader can skip")


_MAX_DEPTH = 128
_STRUCTURAL = frozenset((13, 14, 15, 16))
_ARRAY_ELEMENT_KINDS = frozenset((*range(1, 14), 15, 17, *range(18, 31)))
_KEYED_ELEMENT_KINDS = frozenset((*range(1, 12), 13, *range(18, 31)))
_EXTENT_CAP = 0x7FFFFFFF
_RESERVED_IDS = frozenset((0xFFFFFFFFFFFFFFFE, 0xFFFFFFFFFFFFFFFD))
_RESERVED_TYPE_IDS = _RESERVED_IDS | frozenset((NODE_WIRE_ID,))


def _check_content(
    data: memoryview, kind: int, ids: list[int], depth: int = 0, nonfinite: list[bool] | None = None
) -> None:
    """Judge a payload its length already frames: it must be consumed exactly.

    The port of the engine's wire check over a field's, an arm's or a keyed
    slot's L (internal/tablewire/wirecheck.go, content).
    """
    width = _FIXED_WIDTH.get(kind)
    if width is not None:
        if len(data) != width:
            raise Refused("wire_damaged", "an L that is not its kind's width")
        _check_scalar(data, kind, nonfinite)
        return
    if kind == 12:
        _check_text(data)
        return
    if kind == 31:
        return
    if kind == 32:
        if len(data):
            raise Refused("wire_damaged", "kind 32 with a nonzero L")
        return
    if kind in (13, 14, 16):
        if depth >= _MAX_DEPTH:
            raise Refused("wire_damaged", "a framed payload nested past the binary bound")
        reader = Reader(data, 0, len(data), ids)
        if kind == 13:
            _check_body(reader, depth + 1, nonfinite=nonfinite)
        elif kind == 14:
            _check_array(reader, depth + 1, nonfinite)
        else:
            _check_keyed(reader, depth + 1, nonfinite)
        return
    if kind in (15, 17, 30):
        reader = Reader(data, 0, len(data), ids)
        _skip(reader, kind, depth, nonfinite)
        if reader.pos != reader.end:
            raise Refused("wire_damaged", "bytes left inside an arm after its payload")
        return
    raise Refused("wire_damaged", f"kind {kind} is a kind no reader can skip")


def _check_scalar(data: memoryview, kind: int, nonfinite: list[bool] | None = None) -> None:
    if kind == 1:
        if data[0] > 1:
            raise Refused("wire_damaged", "a bool byte other than 0 or 1")
    elif kind == 10:
        if int.from_bytes(data, "little") & 0x7F800000 == 0x7F800000:
            _not_finite(nonfinite)
    elif kind == 11:
        if int.from_bytes(data, "little") & 0x7FF0000000000000 == 0x7FF0000000000000:
            _not_finite(nonfinite)


def _not_finite(nonfinite: list[bool] | None) -> None:
    """Record a non-finite float on the file walk, or refuse it in the decode."""
    if nonfinite is None:
        raise Refused("non_finite_float", "a float the wire carries")
    nonfinite.append(True)


def _check_text(data: memoryview) -> None:
    raw = bytes(data)
    if b"\x00" in raw:
        raise Refused("wire_damaged", "ill-formed text")
    try:
        raw.decode("utf-8")
    except UnicodeDecodeError:
        raise Refused("wire_damaged", "ill-formed text") from None


def _check_file(view: memoryview, ids: list[int]) -> None:
    """Raise the wire check's damage over the whole root body, if any.

    The root body is walked as a nested one is, except that it may hold the
    node table, whose records are each a body. A non-finite float is recorded
    and the walk reads on, as the engine's wire check does; only after the
    whole body is clean is it ignored, so damage later in the file outranks
    it. With no damage, the decode's own refusal of the float stands
    (internal/tablewire/wirecheck.go, checkFile; decode.go, decodeWalk).
    """
    nonfinite: list[bool] = []
    _check_body(Reader(view, 1, _body_end(view), ids), 0, True, nonfinite)


def _check_node_table(data: memoryview, ids: list[int], nonfinite: list[bool] | None = None) -> None:
    reader = Reader(data, 0, len(data), ids)
    for _ in range(reader.varint()):
        type_id = reader.id_at(reader.varint())
        if type_id in _RESERVED_TYPE_IDS:
            # A reserved id is damage anywhere but the body whose transport it
            # is, so it cannot be a record's type (internal/tablewire/nodetable.go,
            # scanNodeRecords).
            raise Refused("wire_damaged", "a reserved id in a record's type position")
        body = reader.take(reader.varint())
        _check_body(Reader(body, 0, len(body), ids), 0, nonfinite=nonfinite)
    if reader.pos != reader.end:
        raise Refused("wire_damaged", "bytes left inside the node table")


def _check_body(reader: Reader, depth: int, root: bool = False, nonfinite: list[bool] | None = None) -> None:
    seen: set[int] = set()
    while True:
        reference = reader.varint()
        if reference == 0:
            if reader.pos != reader.end:
                raise Refused("wire_damaged", "bytes after a body's terminator inside its L")
            return
        if reference > len(reader.ids):
            raise Refused("wire_damaged", "a field reference past the id table")
        field_id = reader.ids[reference - 1]
        if field_id in seen:
            raise Refused("wire_damaged", "the same field id twice in one body")
        seen.add(field_id)
        kind = reader.byte()
        if field_id in _RESERVED_IDS:
            raise Refused("wire_damaged", "a reserved announcement id out of place")
        if field_id == NODE_WIRE_ID:
            if not root:
                raise Refused("wire_damaged", "the node table's reserved id in a nested body")
            if kind != 12:
                raise Refused("wire_damaged", "the node table under a kind other than 12")
            _check_node_table(reader.take(reader.varint()), reader.ids, nonfinite)
            continue
        _skip(reader, kind, depth, nonfinite)


def _array_header(reader: Reader) -> tuple[int, int]:
    if reader.end - reader.pos < 2:
        raise Refused("wire_damaged", "an array, keyed or map body too short for its header")
    element_kind = reader.byte()
    count = reader.varint()
    if count > _EXTENT_CAP:
        raise Refused("count_over_extent_cap", "a collection count above the int32 storage cap")
    return element_kind, count


def _check_array(reader: Reader, depth: int, nonfinite: list[bool] | None = None) -> None:
    element_kind, count = _array_header(reader)
    if element_kind not in _ARRAY_ELEMENT_KINDS:
        raise Refused("wire_damaged", "an array element kind outside the closed list")
    for _ in range(count):
        _skip(reader, element_kind, depth, nonfinite)
    if reader.pos != reader.end:
        raise Refused("wire_damaged", "bytes left inside an array's L after its elements")


def _check_keyed(reader: Reader, depth: int, nonfinite: list[bool] | None = None) -> None:
    element_kind, count = _array_header(reader)
    if element_kind not in _KEYED_ELEMENT_KINDS:
        raise Refused("wire_damaged", "a keyed element kind outside the closed list")
    seen: set[int] = set()
    for _ in range(count):
        key = reader.varint()
        if key == 0 or key > len(reader.ids):
            raise Refused("wire_damaged", "a keyed slot whose key reference names no id")
        if key in seen:
            raise Refused("wire_damaged", "the same keyed slot twice in one keyed body")
        seen.add(key)
        _check_content(reader.take(reader.varint()), element_kind, reader.ids, depth, nonfinite)
    if reader.pos != reader.end:
        raise Refused("wire_damaged", "bytes left inside a keyed array's L after its slots")
