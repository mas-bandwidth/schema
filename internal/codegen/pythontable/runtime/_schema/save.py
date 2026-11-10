"""Form-1 save and measure (docs/SPEC-TABLES.md §3, §3.1, §9).

A write validates the whole value before it writes a byte, numbers the reachable
node graph in one depth-first pre-order walk, and writes the root body, the
flat node table and the id table. Measure runs the same walk into a counting
sink, so the two agree byte for byte by construction.
"""

from typing import Any

from . import InvalidValue
from . import descriptor, kinds
from . import kind_bool, kind_int, kind_pointer, kind_table, kind_unsupported
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


def _encode(value: Any, writer: Writer) -> int:
    _validate(value, value.descriptor, "", set())
    ctx = Ctx()
    ctx.indices[id(value)] = 1
    ctx.onstack.add(id(value))
    _collect(value, ctx)
    writer.byte(FORM)
    write_body(writer, value, ctx, True)
    for table_id in ctx.ids:
        writer.raw(table_id.to_bytes(8, "little"))
    writer.raw(len(ctx.ids).to_bytes(8, "little"))
    return writer.count


def _path(parent: str, name: str) -> str:
    return f"{parent}.{name}" if parent else name


def _validate(value: Any, desc: TableDesc, path: str, seen: set[int]) -> None:
    key = id(value)
    if key in seen:
        return
    seen.add(key)
    for field in desc.fields:
        current = getattr(value, field.name)
        where = _path(path, field.name)
        if field.codec == "pointer":
            if current is None:
                continue
            target = descriptor.by_name(field.target)
            if target is None or not isinstance(current, target):
                raise InvalidValue(where, f"not a {field.target} or None")
            _validate(current, current.descriptor, where, seen)
        elif field.codec == "table":
            target = descriptor.by_name(field.target)
            if target is None or not isinstance(current, target):
                raise InvalidValue(where, f"not a {field.target}")
            _validate(current, current.descriptor, where, seen)
        else:
            kinds.REGISTRY[field.codec].validate(field, current, where)


def _collect(value: Any, ctx: Ctx) -> None:
    desc: TableDesc = value.descriptor
    for field in desc.fields:
        current = getattr(value, field.name)
        if field.codec == "pointer":
            if current is None:
                continue
            key = id(current)
            if key in ctx.indices:
                if key in ctx.onstack:
                    raise InvalidValue(
                        _path(desc.name, field.name), "a pointer cycle has no wire form"
                    )
                continue
            ctx.indices[key] = len(ctx.order) + 2
            ctx.order.append(current)
            ctx.onstack.add(key)
            _collect(current, ctx)
            ctx.onstack.discard(key)
        elif field.codec == "table":
            if field.is_default(current):
                continue
            _collect(current, ctx)


def write_body(writer: Writer, value: Any, ctx: Ctx, root: bool) -> None:
    desc: TableDesc = value.descriptor
    previous = ctx.current
    ctx.current = value
    for field in desc.fields:
        current = getattr(value, field.name)
        if field.is_default(current):
            continue
        kinds.REGISTRY[field.codec].write(writer, field, current, ctx)
    if root and ctx.order:
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
    writer.byte(0)
    ctx.current = previous
