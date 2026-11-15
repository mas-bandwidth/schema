"""The byte-buffer field kinds (docs/SPEC-TABLES.md §2.5, kind 17).

A `*bytes` or `*string` field is a POINTER to a blob record: kind 17 names a
node index, and the record's body is the bytes themselves. Python's `bytes`
and `str` are values, not boxed nodes, so the save numbers ONE record per
non-null slot BY VALUE: a caller's equal strings in two slots are two records,
never one shared node. Records are numbered per slot VISIT, never by the
owning table's identity, so one table object placed in two by-value slots (or
by value and by pointer) writes a record of its own for each. The read keeps the wire's node indices, so a wire that
names one blob record from two slots is refused by name (`blob_shared`); a
`*string` record's bytes must be well-formed UTF-8 with no zero byte.
"""

from typing import Any

from . import InvalidValue
from . import kinds
from .descriptor import FieldDesc

# The two RESERVED node type ids a byte buffer's record rides under
# (docs/SPEC-TABLES.md §3.1): fnv1a64("bytes") and fnv1a64("string").
_STRING_BLOB_ID = 0x704BE0D8FAAFFC58
_BYTES_BLOB_ID = 0x2F2EC0474F1C4FE4
# The most a record's u32 length holds; a longer blob refuses the whole wire.
_BLOB_CAP = 0xFFFFFFFF


class _BlobNode:
    """One blob record the save is about to write: its shape and its bytes."""

    __slots__ = ("string", "body")

    def __init__(self, string: bool, body: bytes) -> None:
        self.string = string
        self.body = body


class _BlobValue:
    """One blob record the read placed: its shape and its Python value."""

    __slots__ = ("string", "value")

    def __init__(self, string: bool, value: Any) -> None:
        self.string = string
        self.value = value


def _encode(fd: FieldDesc, value: Any, path: str) -> bytes:
    """The record body a write puts on the wire, or InvalidValue.

    A `*string` is text: UTF-8 encoded strictly, and no zero byte (a text blob's
    content is refused on the same terms as kind 12's). A `*bytes` takes bytes,
    bytearray or a B-view memoryview; any other memoryview is refused rather
    than reinterpreted.
    """
    if fd.codec == "string_blob":
        if type(value) is not str:
            raise InvalidValue(path, "not a str")
        try:
            body = value.encode("utf-8")
        except UnicodeEncodeError:
            raise InvalidValue(path, "not well-formed UTF-8 text")
        if b"\x00" in body:
            raise InvalidValue(path, "text carries a zero byte")
        return body
    if isinstance(value, bytes):
        return bytes(value)
    if isinstance(value, bytearray):
        return bytes(value)
    if isinstance(value, memoryview):
        if value.format != "B":
            raise InvalidValue(path, "a memoryview must have format B")
        return value.tobytes()
    raise InvalidValue(path, "not bytes, bytearray or a B memoryview")


class _Blob:
    def validate(self, fd: FieldDesc, value: Any, path: str) -> None:
        if value is not None:
            _encode(fd, value, path)

    def read(self, r: Any, fd: FieldDesc, ctx: Any) -> Any:
        index = r.varint()
        ctx.blob_pending.append((ctx.current, fd, index))
        ctx.edges.append(index)
        return None

    def write(self, w: Any, fd: FieldDesc, value: Any, ctx: Any) -> None:
        # The save numbered each slot visit in write order (save.py, _collect):
        # the next number is this slot's own record.
        if ctx.blob_next >= len(ctx.blob_slots):
            raise InvalidValue(fd.name, "the byte buffer slot has no node index")
        index = ctx.blob_slots[ctx.blob_next]
        ctx.blob_next += 1
        w.varint(ctx.idref(fd.field_id))
        w.byte(fd.kind)
        w.varint(index)


kinds.register("string_blob", _Blob())
kinds.register("bytes_blob", _Blob())
