"""The bytes field kind (docs/SPEC-TABLES.md §3, kind 14 over element kind 6).

A byte buffer rides as an array of u8: the body length, the element kind, the
count, then the bytes. A read returns `bytes`; a write takes `bytes`,
`bytearray` or a `B`-format `memoryview`.
"""

from typing import Any

from . import InvalidValue, Refused
from . import kinds
from .descriptor import FieldDesc

_ELEM_U8 = 6
_ARRAY_ELEMENT_KINDS = frozenset(
    list(range(1, 14)) + [15, 17] + list(range(18, 30)) + [30]
)


def _payload(fd: FieldDesc, value: Any, path: str) -> bytes:
    # Exact types only: a subclass could answer bytes() or == with other
    # bytes than it holds.
    if type(value) is bytes:
        payload = value
    elif type(value) is bytearray:
        payload = bytes(value)
    elif type(value) is memoryview:
        if value.format != "B":
            raise InvalidValue(path, "a memoryview must have format B")
        payload = value.tobytes()
    else:
        raise InvalidValue(path, "not bytes, bytearray or a B memoryview")
    if len(payload) > fd.bound:
        raise InvalidValue(path, f"{len(payload)} bytes is past the bound {fd.bound}")
    return payload


class _Bytes:
    def validate(self, fd: FieldDesc, value: Any, path: str) -> None:
        _payload(fd, value, path)

    def read(self, r: Any, fd: FieldDesc, ctx: Any) -> Any:
        from .load import Reader

        body = r.take(r.varint())
        if len(body) < 2:
            raise Refused("wire_damaged", "a bytes body is too short for its header")
        sub = Reader(body, 0, len(body), ctx.ids)
        elem_kind = sub.byte()
        if elem_kind not in _ARRAY_ELEMENT_KINDS:
            raise Refused("wire_damaged", f"element kind {elem_kind} is not in the closed set")
        if elem_kind != _ELEM_U8:
            raise Refused("kind_mismatch", f"{fd.name} is not an array of u8")
        count = sub.varint()
        if count > fd.bound:
            raise Refused("length_over_bound", f"{count} bytes is past the bound {fd.bound}")
        payload = bytes(sub.take(count))
        if sub.pos != len(body):
            raise Refused("wire_damaged", "bytes left inside a bytes body")
        return payload

    def write(self, w: Any, fd: FieldDesc, value: Any, ctx: Any) -> None:
        w.varint(ctx.idref(fd.field_id))
        w.byte(fd.kind)
        self.write_value(w, fd, value, ctx)

    def write_value(self, w: Any, fd: FieldDesc, value: Any, ctx: Any) -> None:
        from .save import Writer

        payload = bytes(value)
        body = bytearray()
        bw = Writer(body)
        bw.byte(_ELEM_U8)
        bw.varint(len(payload))
        bw.raw(payload)
        w.varint(len(body))
        w.raw(body)


kinds.register("bytes", _Bytes())
