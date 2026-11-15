"""Form-1 save and measure (docs/SPEC-TABLES.md §3, §3.1, §9).

A write validates the whole value before it writes a byte, numbers the reachable
node graph in one depth-first pre-order walk, and writes the root body, the
flat node table and the id table. Measure runs the same walk into a counting
sink, so the two agree byte for byte by construction.
"""

from collections.abc import Iterator
from typing import Any, Final

from . import InvalidValue
from . import descriptor, kinds
from . import kind_map
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
    __slots__ = ("ids", "refs", "indices", "order", "onstack", "current")

    def __init__(self) -> None:
        self.ids: list[int] = []
        self.refs: dict[int, int] = {}
        self.indices: dict[int, int] = {}
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
    _collect(value, ctx)
    _write_form(writer)
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
            yield current, where
        elif field.codec == "map":
            yield from _validate_map_entries(field, current, where)
        else:
            kinds._REGISTRY[field.codec].validate(field, current, where)


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
    entry = descriptor.by_name(field.target)
    if entry is None:
        raise InvalidValue(field.name, f"the table {field.target} is not registered")
    value_field = entry.descriptor.fields[1]
    for key in kind_map._sorted_keys(field, value):
        current = value[key]
        where = (path, "", key)
        if value_field.codec == "pointer":
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
