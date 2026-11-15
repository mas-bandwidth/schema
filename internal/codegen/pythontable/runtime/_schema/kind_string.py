"""The string field kind (docs/SPEC-TABLES.md §3, kind 12).

A string rides as its own length and its UTF-8 bytes. The declared size is a
byte bound, not a character bound; a value at its declared default is elided by
the field walk before this module is reached, so an old writer's absent field
reads back as that default.
"""

from typing import Any

from . import InvalidValue, Refused
from . import kinds
from .descriptor import FieldDesc


def _encoded(fd: FieldDesc, value: Any, path: str) -> bytes:
    if type(value) is not str:
        raise InvalidValue(path, "not a str")
    try:
        raw = value.encode("utf-8")
    except UnicodeEncodeError:
        raise InvalidValue(path, "ill-formed UTF-8 text") from None
    if b"\x00" in raw:
        raise InvalidValue(path, "ill-formed UTF-8 text (a zero byte)")
    if fd.size > 0 and len(raw) > fd.size:
        raise InvalidValue(path, f"length {len(raw)} is past its bound {fd.size}")
    return raw


class _String:
    def validate(self, fd: FieldDesc, value: Any, path: str) -> None:
        _encoded(fd, value, path)

    def read(self, r: Any, fd: FieldDesc, ctx: Any) -> Any:
        return self.read_element(r, fd, ctx)

    def read_element(self, r: Any, fd: FieldDesc, ctx: Any) -> Any:
        length = r.varint()
        payload = bytes(r.take(length))
        if b"\x00" in payload:
            raise Refused("wire_damaged", "ill-formed text")
        try:
            text = payload.decode("utf-8")
        except UnicodeDecodeError:
            raise Refused("wire_damaged", "ill-formed text") from None
        if fd.size > 0 and length > fd.size:
            raise Refused("length_over_bound", f"length {length} is past its bound {fd.size}")
        return text

    def write(self, w: Any, fd: FieldDesc, value: Any, ctx: Any) -> None:
        w.varint(ctx.idref(fd.field_id))
        w.byte(fd.kind)
        self.write_element(w, fd, value, ctx)

    def write_element(self, w: Any, fd: FieldDesc, value: Any, ctx: Any) -> None:
        encoded = value.encode("utf-8")
        w.varint(len(encoded))
        w.raw(encoded)


kinds.register("string", _String())
