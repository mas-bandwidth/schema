"""Form-1 save and measure (docs/SPEC-TABLES.md §3, §3.1, §9).

A write validates the whole value before it writes a byte, numbers the reachable
node graph in one depth-first pre-order walk, and writes the root body, the
flat node table and the id table. Measure runs the same walk into a counting
sink, so the two agree byte for byte by construction.
"""

<<<<<<< HEAD
from collections.abc import Iterator
from typing import Any, Final
=======
from typing import Any, Iterator
>>>>>>> 5043610a8358c251defbef6d002d940708010d41

from . import InvalidValue
from . import descriptor, kinds
from . import kind_array, kind_blob, kind_map, kind_unsupported
from .descriptor import FieldDesc, TableDesc

FORM = 1
NODE_WIRE_ID = 0xFFFFFFFFFFFFFFFF


class Writer:
    __slots__ = ("buf", "count")

    def __init__(self, buf: bytearray | None) -> None:
        self.buf = buf
        self.count = 0

    def byte(self, value: int) -> None:
        self.count += 1
        if self.buf is not None:
            self.buf.append(value & 0xFF)

    def raw(self, data: Any) -> None:
        self.count += len(data)
        if self.buf is not None:
            self.buf.extend(data)

    def varint(self, value: int) -> None:
        if value < 0:
            raise InvalidValue("", "a reference is negative")
        while True:
            byte = value & 0x7F
            value >>= 7
            if value:
                self.byte(byte | 0x80)
            else:
                self.byte(byte)
                return


class Ctx:
    __slots__ = ("ids", "refs", "indices", "node_blobs", "blob_slots", "blob_next", "order", "onstack", "current")

    def __init__(self) -> None:
        self.ids: list[int] = []
        self.refs: dict[int, int] = {}
        self.indices: dict[int, int] = {}
        # Each node body's blob record numbers, in the order its write walk
        # meets the slots (node 1 is the root), then flattened into the order
        # the bodies are written. A by-value table reached twice is two visits
        # with two records each, never keyed by the owner's identity.
        self.node_blobs: dict[int, list[int]] = {}
        self.blob_slots: list[int] = []
        self.blob_next = 0
        self.order: list[Any] = []
        self.onstack: set[int] = set()
        self.current: Any = None

    def idref(self, value: int) -> int:
        reference = self.refs.get(value)
        if reference is None:
            self.ids.append(value)
            reference = len(self.ids)
            self.refs[value] = reference
        return reference


def save_table(value: Any) -> bytes:
    sink = bytearray()
    _encode(value, Writer(sink))
    return bytes(sink)


def measure_table(value: Any) -> int:
    return _encode(value, Writer(None))


def _write_form(writer: Writer) -> None:
    writer.byte(FORM)


def _encode(value: Any, writer: Writer) -> int:
    _validate(value, value.descriptor, "", set())
    ctx = Ctx()
    ctx.indices[id(value)] = 1
    ctx.onstack.add(id(value))
<<<<<<< HEAD
    _collect(value, ctx)
    _write_form(writer)
=======
    root_blobs: list[int] = []
    ctx.node_blobs[1] = root_blobs
    _collect(value, ctx, root_blobs)
    ctx.blob_slots = list(root_blobs)
    for position, node in enumerate(ctx.order):
        if not isinstance(node, kind_blob._BlobNode):
            ctx.blob_slots.extend(ctx.node_blobs[position + 2])
    writer.byte(FORM)
>>>>>>> 5043610a8358c251defbef6d002d940708010d41
    write_body(writer, value, ctx, True)
    for table_id in ctx.ids:
        writer.raw(table_id.to_bytes(8, "little"))
    writer.raw(len(ctx.ids).to_bytes(8, "little"))
    return writer.count


def _path(parent: str, name: str) -> str:
    return f"{parent}.{name}" if parent else name


# THE WALKS ARE ITERATIVE (docs/SPEC-TABLES.md §3.1: nothing recurses over the
# pointer graph). Each table on the walk is one generator of the edges out of
# it, kept on an explicit stack: the driver resumes the top generator, and an
# edge to a table not yet seen pushes that table's generator, so the order is
# the recursive walk's depth-first pre-order and a chain of any length costs
# one stack entry per open table and no interpreter frame. A map's entries
# are expanded inside the generator of the table that holds the map (a map
# inside a map nests generators to the depth of the declaration, never of the
# data).


def _validate(value: Any, desc: TableDesc, path: str, seen: set[int]) -> None:
    key = id(value)
    if key in seen:
        return
    seen.add(key)
    _validate_drain([_validate_fields(value, desc, path)], seen)


def _validate_map(field: FieldDesc, value: Any, path: str, seen: set[int]) -> None:
    _validate_drain([_validate_map_entries(field, value, path)], seen)


def _validate_drain(stack: list[Iterator[tuple[Any, str]]], seen: set[int]) -> None:
    """Runs the validation walk: every table reached is checked once."""
    while stack:
        edge = next(stack[-1], None)
        if edge is None:
            stack.pop()
            continue
        child, where = edge
        key = id(child)
        if key in seen:
            continue
        seen.add(key)
        stack.append(_validate_fields(child, child.descriptor, where))


def _validate_fields(
    value: Any, desc: TableDesc, path: str
) -> Iterator[tuple[Any, str]]:
    """Checks one table's own fields and yields each table it holds."""
    for field in desc.fields:
        if not field.guard_holds(value):
            continue
        current = getattr(value, field.name)
        where = _path(path, field.name)
        if field.codec == "pointer":
            if current is None:
                continue
            target = descriptor.by_name(field.target)
            if target is None or not isinstance(current, target):
                raise InvalidValue(where, f"not a {field.target} or None")
            yield current, where
        elif field.codec == "pointer_array":
            kinds._REGISTRY["pointer_array"].validate(field, current, where)
            for index, elem in enumerate(current):
                if elem is not None:
                    yield elem, f"{where}[{index}]"
        elif field.codec == "table":
            target = descriptor.by_name(field.target)
            if target is None or not isinstance(current, target):
                raise InvalidValue(where, f"not a {field.target}")
<<<<<<< HEAD
            yield current, where
=======
            _validate(current, current.descriptor, where, seen)
        elif field.codec == "array":
            if not isinstance(current, list):
                raise InvalidValue(where, "not a list")
            kind_array._check_count(where, len(current))
            kind_array._check_shape(field, len(current), where)
            element = field.element
            if element is None:
                raise InvalidValue(where, "an array carries no element descriptor")
            for i, item in enumerate(current):
                cell = f"{where}[{i}]"
                if element.codec == "table":
                    target = descriptor.by_name(element.target)
                    if target is None or not isinstance(item, target):
                        raise InvalidValue(cell, f"not a {element.target}")
                    _validate(item, item.descriptor, cell, seen)
                else:
                    kinds._REGISTRY[element.codec].validate(element, item, cell)
>>>>>>> 5043610a8358c251defbef6d002d940708010d41
        elif field.codec == "map":
            yield from _validate_map_entries(field, current, where)
        else:
            kind = kinds._REGISTRY[field.codec]
            kind.validate(field, current, where)
            # a container kind that holds tables by value names them, and
            # each is validated as a field's table is
            for table, table_path in _held_tables(kind, field, current, where):
                _validate(table, table.descriptor, table_path, seen)


def _validate_map_entries(
    field: FieldDesc, value: Any, path: str
) -> Iterator[tuple[Any, str]]:
    """Checks a map's keys and scalar values and yields each table it holds."""
    if type(value) is not dict:
        raise InvalidValue(path, "not a dict")
    entry = descriptor.by_name(field.target)
    if entry is None:
        raise InvalidValue(path, f"the table {field.target} is not registered")
    key_field = entry.descriptor.fields[0]
    value_field = entry.descriptor.fields[1]
    for key, current in value.items():
        where = f"{path}[{key!r}]"
        kinds._REGISTRY[key_field.codec].validate(key_field, key, where)
        if value_field.codec == "string_blob" or value_field.codec == "bytes_blob":
            # A map whose VALUE is a byte buffer, `map[K]*bytes` or
            # `map[K]*string`, is not carried yet: an entry refuses by name,
            # as kind_unsupported.py refused it before the buffers landed.
            raise InvalidValue(where, kind_unsupported._RULE)
        if value_field.codec == "pointer":
            kinds._REGISTRY["pointer"].validate(value_field, current, where)
            if current is not None:
                yield current, where
        elif value_field.codec == "table":
            kinds._REGISTRY["table"].validate(value_field, current, where)
            yield current, where
        elif value_field.codec == "map":
            yield from _validate_map_entries(value_field, current, where)
        else:
            kinds._REGISTRY[value_field.codec].validate(value_field, current, where)


<<<<<<< HEAD
# A path on the numbering walk is built only when a refusal needs it: each
# edge carries (parent, field name, subscript), and _render spells the chain
# from the root as every other write refusal does (list_nodes[1].children[0]).
_NO_SUB: Final = object()
_Path = tuple[Any, str, Any] | None


def _render(path: _Path) -> str:
    segments: list[str] = []
    while path is not None:
        parent, name, sub = path
        segment = name if sub is _NO_SUB else f"{name}[{sub!r}]"
        segments.append(segment)
        path = parent
    out = ""
    for segment in reversed(segments):
        if out and not segment.startswith("["):
            out += "."
        out += segment
    return out


def _collect(value: Any, ctx: Ctx) -> None:
    """Numbers the reachable nodes in depth-first pre-order (§3.1).

    A node already numbered is written by its index. A node reached again while
    its own descent is still open (it is on the stack) is a cycle, which has no
    wire form: the refusal names the path from the root of the edge that closes
    it.
    """
    indices = ctx.indices
    order = ctx.order
    onstack = ctx.onstack
    stack: list[tuple[Iterator[tuple[Any, _Path, bool]], int]] = [
        (_collect_edges(value, None), -1)
    ]
    while stack:
        edges, open_key = stack[-1]
        edge = next(edges, None)
        if edge is None:
            stack.pop()
            if open_key >= 0:
                onstack.discard(open_key)
            continue
        child, where, is_node = edge
        if not is_node:
            stack.append((_collect_edges(child, where), -1))
            continue
        key = id(child)
        if key in indices:
            if key in onstack:
                raise InvalidValue(_render(where), "a pointer cycle has no wire form")
            continue
        indices[key] = len(order) + 2
        order.append(child)
        onstack.add(key)
        stack.append((_collect_edges(child, where), key))


def _collect_edges(value: Any, path: _Path) -> Iterator[tuple[Any, _Path, bool]]:
    """Yields one table's edges in field order: (target, path, is a node)."""
    for field in value.descriptor.fields:
        if not field.guard_holds(value):
            continue
        codec = field.codec
        if codec == "pointer":
            current = getattr(value, field.name)
            if current is not None:
                yield current, (path, field.name, _NO_SUB), True
        elif codec == "pointer_array":
            name = field.name
            for index, elem in enumerate(getattr(value, name)):
                if elem is not None:
                    yield elem, (path, name, index), True
        elif codec == "table":
            current = getattr(value, field.name)
            if not field.is_default(current):
                yield current, (path, field.name, _NO_SUB), False
        elif codec == "map":
            yield from _collect_map_edges(
                field, getattr(value, field.name), (path, field.name, _NO_SUB)
            )


def _collect_map_edges(
    field: FieldDesc, value: dict[Any, Any], path: _Path
) -> Iterator[tuple[Any, _Path, bool]]:
=======
def _collect(value: Any, ctx: Ctx, blobs: list[int]) -> None:
    desc: TableDesc = value.descriptor
    for field in desc.fields:
        current = getattr(value, field.name)
        if field.codec == "string_blob" or field.codec == "bytes_blob":
            if current is None:
                continue
            body = kind_blob._encode(field, current, _path(desc.name, field.name))
            index = len(ctx.order) + 2
            ctx.order.append(kind_blob._BlobNode(field.codec == "string_blob", body))
            blobs.append(index)
        elif field.codec == "pointer":
            if current is None:
                continue
            key = id(current)
            if key in ctx.indices:
                if key in ctx.onstack:
                    raise InvalidValue(
                        _path(desc.name, field.name), "a pointer cycle has no wire form"
                    )
                continue
            node_index = len(ctx.order) + 2
            ctx.indices[key] = node_index
            ctx.order.append(current)
            ctx.onstack.add(key)
            inner: list[int] = []
            ctx.node_blobs[node_index] = inner
            _collect(current, ctx, inner)
            ctx.onstack.discard(key)
        elif field.codec == "table":
            if field.is_default(current):
                continue
            _collect(current, ctx, blobs)
        elif field.codec == "array":
            # a table element is a by-value edge, so its blob slots are the
            # owner's, in the order the elements ride
            element = field.element
            if element is None or element.codec != "table":
                continue
            for item in current:
                _collect(item, ctx, blobs)
        elif field.codec == "map":
            _collect_map(field, current, ctx, blobs)
        else:
            kind = kinds._REGISTRY[field.codec]
            # a kind whose value can hold pointers numbers them itself, in
            # the order its bytes ride (a union's arm, kind_union.py)
            collect = getattr(kind, "collect", None)
            if collect is not None:
                collect(field, current, ctx, blobs)
                continue
            # the tables a container holds by value are walked in place, in
            # the order their bodies ride
            for table, _ in _held_tables(kind, field, current, ""):
                _collect(table, ctx, blobs)


def _held_tables(kind: Any, field: FieldDesc, value: Any, path: str) -> Iterator[tuple[Any, str]]:
    """The tables a container kind holds by value: its tables() when it has one."""
    tables = getattr(kind, "tables", None)
    if tables is None:
        return iter(())
    held: Iterator[tuple[Any, str]] = tables(field, value, path)
    return held


def _collect_map(field: FieldDesc, value: dict[Any, Any], ctx: Ctx, blobs: list[int]) -> None:
    # A map is a by-value edge of the owner's body: a table value's blob slots
    # are the owner's, numbered in the ascending key order the map writes in;
    # a pointer value's node keeps its own.
>>>>>>> 5043610a8358c251defbef6d002d940708010d41
    entry = descriptor.by_name(field.target)
    if entry is None:
        raise InvalidValue(field.name, f"the table {field.target} is not registered")
    value_field = entry.descriptor.fields[1]
    for key in kind_map._sorted_keys(field, value):
        current = value[key]
        where = (path, "", key)
        if value_field.codec == "pointer":
<<<<<<< HEAD
            if current is not None:
                yield current, where, True
        elif value_field.codec == "table":
            if not value_field.is_default(current):
                yield current, where, False
        elif value_field.codec == "map":
            yield from _collect_map_edges(value_field, current, where)


def _write_node_table(writer: Writer, ctx: Ctx) -> None:
    writer.varint(ctx.idref(NODE_WIRE_ID))
    writer.byte(12)
    payload = bytearray()
    nodes = Writer(payload)
    nodes.varint(len(ctx.order))
    for node in ctx.order:
        nodes.varint(ctx.idref(node.descriptor.type_id))
        body = bytearray()
        write_body(Writer(body), node, ctx, False)
        nodes.varint(len(body))
        nodes.raw(body)
    writer.varint(len(payload))
    writer.raw(payload)
=======
            if current is None:
                continue
            node = id(current)
            if node in ctx.indices:
                if node in ctx.onstack:
                    raise InvalidValue(
                        _path(field.name, repr(key)),
                        "a pointer cycle has no wire form",
                    )
                continue
            node_index = len(ctx.order) + 2
            ctx.indices[node] = node_index
            ctx.order.append(current)
            ctx.onstack.add(node)
            inner: list[int] = []
            ctx.node_blobs[node_index] = inner
            _collect(current, ctx, inner)
            ctx.onstack.discard(node)
        elif value_field.codec == "table":
            if value_field.is_default(current):
                continue
            _collect(current, ctx, blobs)
        elif value_field.codec == "map":
            _collect_map(value_field, current, ctx, blobs)
>>>>>>> 5043610a8358c251defbef6d002d940708010d41


def write_body(writer: Writer, value: Any, ctx: Ctx, root: bool) -> None:
    desc: TableDesc = value.descriptor
    previous = ctx.current
    ctx.current = value
    for field in desc.fields:
        if not field.guard_holds(value):
            continue
        current = getattr(value, field.name)
        if field.is_default(current):
            continue
        kinds._REGISTRY[field.codec].write(writer, field, current, ctx)
    if root and ctx.order:
<<<<<<< HEAD
        _write_node_table(writer, ctx)
=======
        writer.varint(ctx.idref(NODE_WIRE_ID))
        writer.byte(12)
        payload = bytearray()
        nodes = Writer(payload)
        nodes.varint(len(ctx.order))
        for node in ctx.order:
            if isinstance(node, kind_blob._BlobNode):
                type_id = kind_blob._STRING_BLOB_ID if node.string else kind_blob._BYTES_BLOB_ID
                nodes.varint(ctx.idref(type_id))
                nodes.varint(len(node.body))
                nodes.raw(node.body)
                continue
            nodes.varint(ctx.idref(node.descriptor.type_id))
            body = bytearray()
            write_body(Writer(body), node, ctx, False)
            nodes.varint(len(body))
            nodes.raw(body)
        writer.varint(len(payload))
        writer.raw(payload)
>>>>>>> 5043610a8358c251defbef6d002d940708010d41
    writer.byte(0)
    ctx.current = previous
