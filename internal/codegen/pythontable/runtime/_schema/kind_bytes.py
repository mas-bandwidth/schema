"""The bytes field kind (docs/SPEC-TABLES.md §2.5, §3).

A `bytes(N)` rides as an array of u8: kind 14, the element kind, the count and
the bytes. A read returns `bytes`; a write takes `bytes`, `bytearray` or a
`"B"`-format `memoryview`. The declared size is the byte bound, and a value at
its declared default is elided by the field walk before this module is reached.
"""

from typing import Any

from . import InvalidValue, Refused
from . import kinds
from .descriptor import FieldDesc

_U8 = 6


def _blob(value: Any) -> bytes | None:
    if isinstance(value, bytes):
        return value
    if isinstance(value, bytearray):
        return bytes(value)
    if isinstance(value, memoryview):
        if value.format != "B":
            return None
        return value.tobytes()
    return None


class _Bytes:
    def validate(self, fd: FieldDesc, value: Any, path: str) -> None:
        raw = _blob(value)
        if raw is None:
            raise InvalidValue(path, "not bytes, bytearray or a B memoryview")
        if fd.size > 0 and len(raw) > fd.size:
            raise InvalidValue(path, f"length {len(raw)} is past its bound {fd.size}")

    def read(self, r: Any, fd: FieldDesc, ctx: Any) -> Any:
        from .load import Reader

        length = r.varint()
        body = r.take(length)
        sub = Reader(body, 0, len(body), r.ids)
        if sub.byte() != _U8:
            raise Refused("kind_mismatch", "a byte buffer's element kind is not u8")
        count = sub.varint()
        if fd.size > 0 and count > fd.size:
            raise Refused("length_over_bound", f"length {count} is past its bound {fd.size}")
        data = bytes(sub.take(count))
        if sub.pos != sub.end:
            raise Refused("wire_damaged", "bytes left inside a byte buffer")
        return data

    def write(self, w: Any, fd: FieldDesc, value: Any, ctx: Any) -> None:
        from .save import Writer

        raw = _blob(value)
        if raw is None:
            raise InvalidValue(fd.name, "not bytes, bytearray or a B memoryview")
        inner = bytearray()
        body = Writer(inner)
        body.byte(_U8)
        body.varint(len(raw))
        body.raw(raw)
        w.varint(ctx.idref(fd.field_id))
        w.byte(fd.kind)
        w.varint(len(inner))
        w.raw(inner)


kinds.register("bytes", _Bytes())
