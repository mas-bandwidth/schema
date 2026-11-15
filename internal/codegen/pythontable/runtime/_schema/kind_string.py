"""The inline text field kind (docs/SPEC-TABLES.md §3, kind 12).

A `string(N)` rides as a length and N-bytes-of-UTF-8 at most. The value is
`str`; a write encodes strictly and refuses a lone surrogate, a zero byte and
a length past N, and a read refuses a payload that is not well-formed UTF-8,
carries a zero byte, or is past the field's bound.
"""

from typing import Any

from . import InvalidValue, Refused
from . import kinds
from .descriptor import FieldDesc


class _String:
    def validate(self, fd: FieldDesc, value: Any, path: str) -> None:
        if type(value) is not str:
            raise InvalidValue(path, "not a str")
        try:
            body = value.encode("utf-8")
        except UnicodeEncodeError:
            raise InvalidValue(path, "not well-formed UTF-8 text")
        if b"\x00" in body:
            raise InvalidValue(path, "text carries a zero byte")
        if fd.size > 0 and len(body) > fd.size:
            raise InvalidValue(path, f"text is longer than {fd.size} bytes")

    def read(self, r: Any, fd: FieldDesc, ctx: Any) -> Any:
        length = r.varint()
        if fd.size > 0 and length > fd.size:
            raise Refused("wire_damaged", "a text payload is past its field's bound")
        raw = bytes(r.take(length))
        try:
            text = raw.decode("utf-8")
        except UnicodeDecodeError:
            raise Refused("wire_damaged", "a text payload is not well-formed UTF-8")
        if "\x00" in text:
            raise Refused("wire_damaged", "a text payload carries a zero byte")
        return text

    def write(self, w: Any, fd: FieldDesc, value: Any, ctx: Any) -> None:
        body = value.encode("utf-8")
        w.varint(ctx.idref(fd.field_id))
        w.byte(fd.kind)
        w.varint(len(body))
        w.raw(body)


kinds.register("string", _String())
