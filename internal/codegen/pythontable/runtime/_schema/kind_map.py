"""The map field kind (docs/SPEC-TABLES.md §2.8, kind 14 over element kind 13).

A map is an array of generated `{ key, value }` entry tables in ascending key
order. The runtime holds the order in the writer and checks it in the reader:
no conforming writer emits a repeated or a descending key, so either is damage
(docs/SPEC-TABLES.md §2.8, §4).
"""

from typing import Any

from . import InvalidValue, Refused
from . import descriptor, kinds
from .descriptor import FieldDesc


def _entry(fd: FieldDesc) -> type[Any]:
    cls = descriptor.by_name(fd.target)
    if cls is None:
        raise InvalidValue(fd.name, f"the table {fd.target} is not registered")
    return cls


def _entry_fields(fd: FieldDesc) -> tuple[FieldDesc, FieldDesc]:
    """The entry's key and value, in the declaration order the entry carries."""
    fields = _entry(fd).descriptor.fields
    return fields[0], fields[1]


def _comparable(field: FieldDesc, key: Any) -> Any:
    if field.codec == "string":
        return key.encode("utf-8")
    return key


def _compare(fd: FieldDesc, a: Any, b: Any) -> int:
    """The map's total key order: text by its UTF-8 bytes, integers by value."""
    key_field, _ = _entry_fields(fd)
    x = _comparable(key_field, a)
    y = _comparable(key_field, b)
    if x < y:
        return -1
    if x > y:
        return 1
    return 0


def _sorted_keys(fd: FieldDesc, mapping: dict[Any, Any]) -> list[Any]:
    """The writer's order (docs/SPEC-TABLES.md §2.8): ascending, no key twice."""
    key_field, _ = _entry_fields(fd)
    return sorted(mapping, key=lambda key: _comparable(key_field, key))


class _Map:
    def validate(self, fd: FieldDesc, value: Any, path: str) -> None:
        from .save import _validate_map

        _validate_map(fd, value, path, set())

    def read(self, r: Any, fd: FieldDesc, ctx: Any) -> Any:
        from .load import Reader, read_body

        entry_cls = _entry(fd)
        key_field, value_field = _entry_fields(fd)
        length = r.varint()
        payload = r.take(length)
        sub = Reader(payload, 0, len(payload), ctx.ids)
        elem_kind = sub.byte()
        if elem_kind != 13:
            raise Refused(
                "kind_mismatch",
                f"{fd.name} holds map entries under kind {elem_kind}, not 13",
            )
        count = sub.varint()
        if count > 0x7FFFFFFF:
            raise Refused(
                "count_over_extent_cap",
                f"{fd.name} holds {count} entries, past the int32 storage cap",
            )
        out: dict[Any, Any] = {}
        last: Any = None
        landed = False
        for _ in range(count):
            n = sub.varint()
            body = sub.take(n)
            entry = entry_cls()
            before = len(ctx.pending)
            read_body(body, entry, ctx)
            key = getattr(entry, key_field.name)
            if landed and _compare(fd, last, key) >= 0:
                raise Refused(
                    "wire_damaged",
                    f"{fd.name} is not in ascending key order",
                )
            last = key
            landed = True
            if value_field.codec == "pointer":
                for i in range(before, len(ctx.pending)):
                    owner, name, index = ctx.pending[i]
                    if owner is entry and name == value_field.name:
                        ctx.pending[i] = (out, key, index)
            out[key] = getattr(entry, value_field.name)
        if sub.pos != sub.end:
            raise Refused(
                "wire_damaged",
                f"{fd.name} has bytes left inside its map body",
            )
        return out

    def write(self, w: Any, fd: FieldDesc, value: Any, ctx: Any) -> None:
        from .save import Writer, write_body

        entry_cls = _entry(fd)
        key_field, value_field = _entry_fields(fd)
        field_ref = ctx.idref(fd.field_id)
        keys = _sorted_keys(fd, value)
        body = bytearray()
        body_writer = Writer(body)
        body_writer.byte(13)
        body_writer.varint(len(keys))
        for key in keys:
            entry = entry_cls()
            setattr(entry, key_field.name, key)
            setattr(entry, value_field.name, value[key])
            entry_body = bytearray()
            write_body(Writer(entry_body), entry, ctx, False)
            body_writer.varint(len(entry_body))
            body_writer.raw(entry_body)
        w.varint(field_ref)
        w.byte(fd.kind)
        w.varint(len(body))
        w.raw(body)


kinds.register("map", _Map())
