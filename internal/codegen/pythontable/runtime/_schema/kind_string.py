"""The string field kind (docs/SPEC-TABLES.md §3, kind 12).

Text is UTF-8 on the wire, encoded and decoded strictly, bounded by N BYTES
and never carrying a zero byte. A lone surrogate and a NUL are refused on
write; ill-formed text is damage on read.
"""

from typing import Any

from . import InvalidValue, Refused
from . import kinds
from .descriptor import FieldDesc


class _String:
    def validate(self, fd: FieldDesc, value: Any, path: str) -> None:
        if type(value) is not str:
            raise InvalidValue(path, "not a string")
        try:
            payload = value.encode("utf-8")
        except UnicodeEncodeError:
            raise InvalidValue(path, "ill-formed UTF-8 text") from None
        if b"\x00" in payload:
            raise InvalidValue(path, "text carries a zero byte")
        if len(payload) > fd.bound:
            raise InvalidValue(path, f"{len(payload)} bytes is past the bound {fd.bound}")

    def read(self, r: Any, fd: FieldDesc, ctx: Any) -> Any:
        payload = bytes(r.take(r.varint()))
        if len(payload) > fd.bound:
            raise Refused("length_over_bound", f"{len(payload)} bytes is past the bound {fd.bound}")
        if b"\x00" in payload:
            raise Refused("wire_damaged", "text carries a zero byte")
        try:
            return payload.decode("utf-8")
        except UnicodeDecodeError:
            raise Refused("wire_damaged", "ill-formed UTF-8 text") from None

    def write(self, w: Any, fd: FieldDesc, value: Any, ctx: Any) -> None:
        w.varint(ctx.idref(fd.field_id))
        w.byte(fd.kind)
        self.write_value(w, fd, value, ctx)

    def write_value(self, w: Any, fd: FieldDesc, value: Any, ctx: Any) -> None:
        payload = value.encode("utf-8")
        w.varint(len(payload))
        w.raw(payload)


kinds.register("string", _String())
