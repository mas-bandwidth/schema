"""Form-1 save and measure (docs/SPEC-TABLES.md §3, §3.1, §9).

A write validates the whole value before it writes a byte, numbers the reachable
node graph in one depth-first pre-order walk, and writes the root body, the
flat node table and the id table. Measure runs the same walk into a counting
sink, so the two agree byte for byte by construction.
"""

from typing import Any, Iterator

from . import InvalidValue
from . import descriptor, kinds
from . import kind_array, kind_blob, kind_map, kind_optional, kind_unsupported
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


def _encode(value: Any, writer: Writer) -> int:
    _validate(value, value.descriptor, "", set())
    ctx = Ctx()
    ctx.indices[id(value)] = 1
    ctx.onstack.add(id(value))
    _collect_root(value, ctx)
    _write_form(writer)
    write_body(writer, value, ctx, True)
    for table_id in ctx.ids:
        writer.raw(table_id.to_bytes(8, "little"))
    writer.raw(len(ctx.ids).to_bytes(8, "little"))
    return writer.count


def _write_form(writer: Writer) -> None:
    writer.byte(FORM)


def _collect_root(value: Any, ctx: Ctx) -> None:
    """Number the whole reachable graph from the root, the by-value tables in
    place, and flatten each body's blob-slot numbers in the order the bodies
    ride."""
    root_blobs: list[int] = []
    ctx.node_blobs[1] = root_blobs
    _collect(value, ctx, root_blobs)
    ctx.blob_slots = list(root_blobs)
    for position, node in enumerate(ctx.order):
        if not isinstance(node, kind_blob._BlobNode):
            ctx.blob_slots.extend(ctx.node_blobs[position + 2])


def _path(parent: str, name: str) -> str:
    return f"{parent}.{name}" if parent else name


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
        if field.codec == "optional":
            if current is None:
                continue
            if field.inner == "array":
                kind_optional._validate_array(field, current, where)
                if field.elem is not None and field.elem.codec == "table":
                    for index, item in enumerate(current):
                        yield item, f"{where}[{index}]"
            elif field.inner == "table":
                target = descriptor.by_name(field.target)
                if target is None or not isinstance(current, target):
                    raise InvalidValue(where, f"not a {field.target}")
                yield current, where
            else:
                kinds._REGISTRY[field.inner].validate(field, current, where)
        elif field.codec == "pointer":
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
            yield current, where
        elif field.codec == "map":
            yield from _validate_map_entries(field, current, where)
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
                    yield item, cell
                else:
                    kinds._REGISTRY[element.codec].validate(element, item, cell)
        else:
            kind = kinds._REGISTRY[field.codec]
            kind.validate(field, current, where)
            # a container kind that holds tables by value names them, and
            # each is validated as a field's table is
            for table, table_path in _held_tables(kind, field, current, where):
                yield table, table_path


def _validate_map_entries(
    field: FieldDesc, value: Any, path: str
) -> Iterator[tuple[Any, str]]:
    """Checks a map's keys and values and yields each table it holds."""
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


# A path on the numbering walk is built only when a refusal needs it: each
# edge carries (parent, field name, subscript), and _render spells the chain
# from the root as every other write refusal does (list_nodes[1].children[0]).
_NO_SUB = object()
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


def _collect(value: Any, ctx: Ctx, blobs: list[int]) -> None:
    """Numbers the reachable nodes in depth-first pre-order (§3.1).

    A node already numbered is written by its index. A node reached again while
    its own descent is still open (it is on the stack) is a cycle, which has no
    wire form: the refusal names the path from the root of the edge that closes
    it. The walk is iterative, so a pointer chain longer than the interpreter's
    recursion limit is numbered in one stack of its own.
    """
    _collect_walk(_collect_edges(value, ctx, blobs, None), ctx)


def _collect_map(field: FieldDesc, value: dict[Any, Any], ctx: Ctx, blobs: list[int]) -> None:
    _collect_walk(_collect_map_edges(field, value, None, ctx, blobs), ctx)


def _collect_walk(
    start: Iterator[tuple[Any, _Path, bool, list[int]]], ctx: Ctx
) -> None:
    indices = ctx.indices
    order = ctx.order
    onstack = ctx.onstack
    stack: list[tuple[Iterator[tuple[Any, _Path, bool, list[int]]], int]] = [(start, -1)]
    while stack:
        edges, open_key = stack[-1]
        edge = next(edges, None)
        if edge is None:
            stack.pop()
            if open_key >= 0:
                onstack.discard(open_key)
            continue
        child, where, is_node, child_blobs = edge
        if not is_node:
            stack.append((_collect_edges(child, ctx, child_blobs, where), -1))
            continue
        key = id(child)
        if key in indices:
            if key in onstack:
                raise InvalidValue(_render(where), "a pointer cycle has no wire form")
            continue
        node_index = len(order) + 2
        indices[key] = node_index
        order.append(child)
        onstack.add(key)
        inner: list[int] = []
        ctx.node_blobs[node_index] = inner
        stack.append((_collect_edges(child, ctx, inner, where), key))


def _collect_edges(
    value: Any, ctx: Ctx, blobs: list[int], path: _Path
) -> Iterator[tuple[Any, _Path, bool, list[int]]]:
    """Yields one table's edges in field order: (target, path, is a node, the
    blob slots of the body the target's bytes belong to). Blob records are
    numbered in place, because a blob slot is a node but has no body to walk."""
    desc: TableDesc = value.descriptor
    for field in desc.fields:
        if not field.guard_holds(value):
            continue
        current = getattr(value, field.name)
        codec = field.codec
        if codec == "optional":
            if current is None:
                continue
            if field.inner == "table":
                yield current, (path, field.name, _NO_SUB), False, blobs
            elif field.inner == "array" and field.elem is not None and field.elem.codec == "table":
                # A present array's elements own their edges, in slot order;
                # an absent one owns none (docs/SPEC-TABLES.md §2.3).
                for index, item in enumerate(current):
                    yield item, (path, field.name, index), False, blobs
        elif codec == "string_blob" or codec == "bytes_blob":
            if current is None:
                continue
            body = kind_blob._encode(field, current, _path(desc.name, field.name))
            index = len(ctx.order) + 2
            ctx.order.append(kind_blob._BlobNode(field.codec == "string_blob", body))
            blobs.append(index)
            continue
        if codec == "pointer":
            if current is not None:
                yield current, (path, field.name, _NO_SUB), True, blobs
        elif codec == "pointer_array":
            name = field.name
            for index, elem in enumerate(current):
                if elem is not None:
                    yield elem, (path, name, index), True, blobs
        elif codec == "table":
            if not field.is_default(current):
                yield current, (path, field.name, _NO_SUB), False, blobs
        elif codec == "array":
            element = field.element
            if element is not None and element.codec == "table":
                for index, item in enumerate(current):
                    yield item, (path, field.name, index), False, blobs
        elif codec == "map":
            yield from _collect_map_edges(
                field, current, (path, field.name, _NO_SUB), ctx, blobs
            )
        else:
            kind = kinds._REGISTRY[codec]
            collect = getattr(kind, "collect", None)
            if collect is not None:
                collect(field, current, ctx, blobs)
                continue
            for table, _ in _held_tables(kind, field, current, ""):
                yield table, (path, field.name, _NO_SUB), False, blobs


def _held_tables(kind: Any, field: FieldDesc, value: Any, path: str) -> Iterator[tuple[Any, str]]:
    """The tables a container kind holds by value: its tables() when it has one."""
    tables = getattr(kind, "tables", None)
    if tables is None:
        return iter(())
    held: Iterator[tuple[Any, str]] = tables(field, value, path)
    return held


def _collect_map_edges(
    field: FieldDesc, value: dict[Any, Any], path: _Path, ctx: Ctx, blobs: list[int]
) -> Iterator[tuple[Any, _Path, bool, list[int]]]:
    entry = descriptor.by_name(field.target)
    if entry is None:
        raise InvalidValue(field.name, f"the table {field.target} is not registered")
    value_field = entry.descriptor.fields[1]
    for key in kind_map._sorted_keys(field, value):
        current = value[key]
        where = (path, "", key)
        if value_field.codec == "pointer":
            if current is not None:
                yield current, where, True, blobs
        elif value_field.codec == "table":
            if not value_field.is_default(current):
                yield current, where, False, blobs
        elif value_field.codec == "map":
            yield from _collect_map_edges(value_field, current, where, ctx, blobs)


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
        _write_node_table(writer, ctx)
    writer.byte(0)
    ctx.current = previous


def _write_node_table(writer: Writer, ctx: Ctx) -> None:
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
