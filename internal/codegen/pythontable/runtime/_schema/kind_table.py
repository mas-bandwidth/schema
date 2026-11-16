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
        return self.read_element(r, fd, ctx)

    def read_element(self, r: Any, fd: FieldDesc, ctx: Any) -> Any:
        from .load import read_body

        length = r.varint()
        payload = r.take(length)
        obj = _target(fd)()
        read_body(payload, obj, ctx)
        return obj

    def write(self, w: Any, fd: FieldDesc, value: Any, ctx: Any) -> None:
        # The field's id is numbered before its body's, so the id table is in
        # the pre-order the Go engine writes.
        w.varint(ctx.idref(fd.field_id))
        w.byte(fd.kind)
        self.write_element(w, fd, value, ctx)

    def write_element(self, w: Any, fd: FieldDesc, value: Any, ctx: Any) -> None:
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
