"""The inline byte field kind (docs/SPEC-TABLES.md §3, kind 14).

`bytes(N)` rides as an array of u8: a length and the bytes at most N. The
value is `bytes`; a write takes bytes, bytearray or a B-view memoryview and
refuses a length past N, and a read returns bytes the caller owns.
"""

from typing import Any

from . import InvalidValue, Refused
from . import kinds
from .descriptor import FieldDesc


def _body(value: Any, path: str) -> bytes:
    if isinstance(value, bytes):
        return bytes(value)
    if isinstance(value, bytearray):
        return bytes(value)
    if isinstance(value, memoryview):
        if value.format != "B":
            raise InvalidValue(path, "a memoryview must have format B")
        return value.tobytes()
    raise InvalidValue(path, "not bytes, bytearray or a B memoryview")


class _Bytes:
    def validate(self, fd: FieldDesc, value: Any, path: str) -> None:
        body = _body(value, path)
        if fd.size > 0 and len(body) > fd.size:
            raise InvalidValue(path, f"bytes are longer than {fd.size}")

    def read(self, r: Any, fd: FieldDesc, ctx: Any) -> Any:
        length = r.varint()
        if fd.size > 0 and length > fd.size:
            raise Refused("wire_damaged", "a byte payload is past its field's bound")
        return bytes(r.take(length))

    def write(self, w: Any, fd: FieldDesc, value: Any, ctx: Any) -> None:
        body = _body(value, fd.name)
        w.varint(ctx.idref(fd.field_id))
        w.byte(fd.kind)
        w.varint(len(body))
        w.raw(body)


kinds.register("bytes", _Bytes())
