"""The pointer field kind (docs/SPEC-TABLES.md §3.1, kind 17).

A pointer rides as an index into the file's one flat node table. A reader
defers the index until the table is known good, so a damaged numbering refuses
the whole load rather than resolving part of it.
"""

from typing import Any

from . import InvalidValue
from . import descriptor, kinds
from .descriptor import FieldDesc


def _target(fd: FieldDesc) -> type[Any]:
    cls = descriptor.by_name(fd.target)
    if cls is None:
        raise InvalidValue(fd.name, f"the table {fd.target} is not registered")
    return cls


class _Pointer:
    def validate(self, fd: FieldDesc, value: Any, path: str) -> None:
        if value is not None and not isinstance(value, _target(fd)):
            raise InvalidValue(path, f"not a {fd.target} or None")

    def read(self, r: Any, fd: FieldDesc, ctx: Any) -> Any:
        ctx.pending.append((ctx.current, fd.name, r.varint()))
        return None

    def write(self, w: Any, fd: FieldDesc, value: Any, ctx: Any) -> None:
        index = ctx.indices.get(id(value))
        if index is None:
            raise InvalidValue(fd.name, "the pointer target has no node index")
        w.varint(ctx.idref(fd.field_id))
        w.byte(fd.kind)
        w.varint(index)


kinds.register("pointer", _Pointer())
