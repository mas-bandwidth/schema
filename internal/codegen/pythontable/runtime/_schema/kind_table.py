"""The by-value table field kind (docs/SPEC-TABLES.md §3, kind 13).

A nested table is a length-prefixed body. The codec delegates the body walk to
load.py or save.py, so a table inside an array, an arm or another table is the
same code path.
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


class _Table:
    def validate(self, fd: FieldDesc, value: Any, path: str) -> None:
        # EXACTLY the generated class: a subclass could carry another
        # descriptor or __eq__, and neither may change what is written.
        if type(value) is not _target(fd):
            raise InvalidValue(path, f"not a {fd.target}")

    def read(self, r: Any, fd: FieldDesc, ctx: Any) -> Any:
        from .load import read_body

        length = r.varint()
        payload = r.take(length)
        obj = _target(fd)()
        read_body(payload, obj, ctx)
        return obj

    def write(self, w: Any, fd: FieldDesc, value: Any, ctx: Any) -> None:
        from .save import write_body

        # AN ALL-DEFAULT NESTED TABLE ELIDES BY ITS BODY, as the engine
        # elides it (internal/tablewire/encode.go, kind 13; SPEC-TABLES §3):
        # the field's id is interned first, the body is built, and when the
        # body is its terminator alone the field is dropped and every id it
        # interned is undone, so an elided field costs nothing in the id
        # table either. Each field inside decided its own elision against its
        # DECLARED default (floats by bit pattern, compressed floats by
        # value), so the body, not an equality, is the answer.
        mark = len(ctx.ids)
        reference = ctx.idref(fd.field_id)
        body = bytearray()
        write_body(_body_writer(body), value, ctx, False)
        if len(body) <= 1:
            for interned in ctx.ids[mark:]:
                del ctx.refs[interned]
            del ctx.ids[mark:]
            return
        w.varint(reference)
        w.byte(fd.kind)
        w.varint(len(body))
        w.raw(body)

    def write_value(self, w: Any, fd: FieldDesc, value: Any, ctx: Any) -> None:
        from .save import write_body

        body = bytearray()
        bw = _body_writer(body)
        write_body(bw, value, ctx, False)
        w.varint(len(body))
        w.raw(body)


def _body_writer(buf: bytearray) -> Any:
    from .save import Writer

    return Writer(buf)


kinds.register("table", _Table())
