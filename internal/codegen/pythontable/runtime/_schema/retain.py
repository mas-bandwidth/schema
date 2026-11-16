"""Bounded resolved fields for form-1 retention (SPEC-TABLES.md §6.6).

A retained payload replaces every wire-id reference with its eight-byte id;
framed structural payloads carry resolved and measured wire lengths. The caller owns
the output buffer. A record containing any node index is excluded as a whole.
The normal file reader must validate the complete wire before this walk.
"""

from typing import Any

from . import Refused
from .load import Reader, _FIXED_WIDTH

_MAX_DEPTH = 128
_RESERVED = frozenset((0, 0xFFFFFFFFFFFFFFFF, 0xFFFFFFFFFFFFFFFE, 0xFFFFFFFFFFFFFFFD))


class Excluded(Exception):
    """The field contains a node index and cannot be retained."""


class Capacity(Exception):
    """The complete resolved record does not fit the caller's buffer."""


class _Sink:
    __slots__ = ("data", "pos")

    def __init__(self, data: memoryview, pos: int) -> None:
        self.data = data
        self.pos = pos

    def raw(self, value: Any) -> None:
        end = self.pos + len(value)
        if end > len(self.data):
            raise Capacity()
        self.data[self.pos:end] = value
        self.pos = end

    def byte(self, value: int) -> None:
        if self.pos >= len(self.data):
            raise Capacity()
        self.data[self.pos] = value
        self.pos += 1

    def u32(self, value: int) -> None:
        if value > 0xFFFFFFFF:
            raise Capacity()
        self.raw(value.to_bytes(4, "little"))

    def varint(self, value: int) -> None:
        while value >= 128:
            self.byte((value & 127) | 128)
            value >>= 7
        self.byte(value)


def _ref(reader: Reader, sink: _Sink, zero: bool) -> int:
    ref = reader.varint()
    if ref == 0:
        if not zero:
            raise Refused("wire_damaged", "a retained reference is zero")
        sink.raw(bytes(8))
        return 0
    identity = reader.id_at(ref)
    if identity in _RESERVED:
        raise Refused("wire_damaged", "a retained reference names a reserved id")
    sink.raw(identity.to_bytes(8, "little"))
    return identity


def _framed(reader: Reader, sink: _Sink, kind: int, depth: int) -> None:
    length = reader.varint()
    data = reader.take(length)
    sub = Reader(data, 0, len(data), reader.ids)
    at = sink.pos
    sink.u32(0)
    sink.u32(0)  # wire length is filled by the save's post-order measure
    start = sink.pos
    _content(sub, sink, kind, depth + 1)
    if sub.pos != sub.end:
        raise Refused("wire_damaged", "bytes left inside a retained frame")
    sink.data[at:at + 4] = (sink.pos - start).to_bytes(4, "little")


def _content(reader: Reader, sink: _Sink, kind: int, depth: int) -> None:
    if depth > _MAX_DEPTH:
        raise Refused("wire_damaged", "a retained field exceeds the nesting bound")
    if kind == 13:
        while _ref(reader, sink, True):
            child_kind = reader.byte()
            sink.byte(child_kind)
            _payload(reader, sink, child_kind, depth)
    elif kind == 14 or kind == 16:
        element = reader.byte()
        count = reader.varint()
        if kind == 14 and element == 17:
            raise Excluded()
        sink.byte(element)
        sink.varint(count)
        for _ in range(count):
            if kind == 16:
                _ref(reader, sink, False)
                _framed(reader, sink, element, depth)
            else:
                _payload(reader, sink, element, depth)
    elif kind == 15 or kind == 30:
        _payload(reader, sink, kind, depth)
    elif kind == 17:
        raise Excluded()
    else:
        sink.raw(reader.take(reader.end - reader.pos))


def _payload(reader: Reader, sink: _Sink, kind: int, depth: int) -> None:
    width = _FIXED_WIDTH.get(kind)
    if width is not None:
        sink.raw(reader.take(width))
    elif kind == 17:
        raise Excluded()
    elif kind == 30:
        _ref(reader, sink, True)
    elif kind == 15:
        if _ref(reader, sink, True):
            arm_kind = reader.byte()
            sink.byte(arm_kind)
            _framed(reader, sink, arm_kind, depth)
    elif kind in (13, 14, 16):
        _framed(reader, sink, kind, depth)
    elif kind in (12, 31, 32):
        length = reader.varint()
        sink.varint(length)
        sink.raw(reader.take(length))
    else:
        raise Refused("wire_damaged", "a retained payload has an unknown kind")


def resolve_payload(kind: int, payload: memoryview, ids: list[int], storage: memoryview, start: int = 0) -> int:
    """Resolve one already-validated payload into storage; return its end.

    On Capacity or Excluded, the caller discards everything from start onward.
    No input length can grow the caller's storage.
    """
    if start < 0 or start > len(storage):
        raise ValueError("start is outside the retention buffer")
    reader = Reader(payload, 0, len(payload), ids)
    sink = _Sink(storage, start)
    _payload(reader, sink, kind, 0)
    if reader.pos != reader.end:
        raise Refused("wire_damaged", "bytes left after a retained payload")
    return sink.pos


class _CountSink:
    __slots__ = ("pos",)

    def __init__(self) -> None:
        self.pos = 0

    def byte(self, value: int) -> None:
        self.pos += 1

    def raw(self, value: Any) -> None:
        self.pos += len(value)

    def varint(self, value: int) -> None:
        self.pos += max(1, (value.bit_length() + 6) // 7)


def _out_ref(reader: Reader, sink: Any, intern: Any) -> int:
    identity = int.from_bytes(reader.take(8), "little")
    sink.varint(0 if identity == 0 else intern(identity))
    return identity


def _out_frame(reader: Reader, sink: Any, kind: int, depth: int, intern: Any, measure: bool) -> None:
    size = int.from_bytes(reader.take(4), "little")
    length_at = reader.pos
    measured = int.from_bytes(reader.take(4), "little")
    data = reader.take(size)
    sub = Reader(data, 0, len(data), [])
    if measure:
        count = _CountSink()
        _out_content(sub, count, kind, depth + 1, intern, True)
        measured = count.pos
        if measured > 0xFFFFFFFF:
            raise Capacity()
        reader.data[length_at:length_at + 4] = measured.to_bytes(4, "little")
        sink.varint(measured)
        sink.pos += measured
    else:
        sink.varint(measured)
        _out_content(sub, sink, kind, depth + 1, intern, False)
    if sub.pos != sub.end:
        raise Refused("wire_damaged", "bytes left inside a resolved frame")


def _out_content(reader: Reader, sink: Any, kind: int, depth: int, intern: Any, measure: bool) -> None:
    if depth > _MAX_DEPTH:
        raise Refused("wire_damaged", "a retained field exceeds the nesting bound")
    if kind == 13:
        while _out_ref(reader, sink, intern):
            child_kind = reader.byte()
            sink.byte(child_kind)
            _out_payload(reader, sink, child_kind, depth, intern, measure)
    elif kind == 14 or kind == 16:
        element = reader.byte()
        count = reader.varint()
        sink.byte(element)
        sink.varint(count)
        for _ in range(count):
            if kind == 16:
                _out_ref(reader, sink, intern)
                _out_frame(reader, sink, element, depth, intern, measure)
            else:
                _out_payload(reader, sink, element, depth, intern, measure)
    elif kind == 15 or kind == 30:
        _out_payload(reader, sink, kind, depth, intern, measure)
    else:
        sink.raw(reader.take(reader.end - reader.pos))


def _out_payload(reader: Reader, sink: Any, kind: int, depth: int, intern: Any, measure: bool) -> None:
    width = _FIXED_WIDTH.get(kind)
    if width is not None:
        sink.raw(reader.take(width))
    elif kind == 30:
        _out_ref(reader, sink, intern)
    elif kind == 15:
        if _out_ref(reader, sink, intern):
            arm_kind = reader.byte()
            sink.byte(arm_kind)
            _out_frame(reader, sink, arm_kind, depth, intern, measure)
    elif kind in (13, 14, 16):
        _out_frame(reader, sink, kind, depth, intern, measure)
    elif kind in (12, 31, 32):
        length = reader.varint()
        sink.varint(length)
        sink.raw(reader.take(length))
    else:
        raise Refused("wire_damaged", "a retained record has an unknown kind")


def measure_payload(kind: int, resolved: memoryview, intern: Any) -> int:
    """Intern references and fill each frame's wire length in one post-order pass."""
    reader = Reader(resolved, 0, len(resolved), [])
    sink = _CountSink()
    _out_payload(reader, sink, kind, 0, intern, True)
    if reader.pos != reader.end:
        raise Refused("wire_damaged", "bytes left after a resolved record")
    return sink.pos


def emit_payload(kind: int, resolved: memoryview, intern: Any, destination: memoryview, start: int = 0) -> int:
    """Emit a measured record into caller-provided output; return its end."""
    reader = Reader(resolved, 0, len(resolved), [])
    sink = _Sink(destination, start)
    _out_payload(reader, sink, kind, 0, intern, False)
    if reader.pos != reader.end:
        raise Refused("wire_damaged", "bytes left after a resolved record")
    return sink.pos


class TableRetain:
    """Caller-owned record bytes and retained-id slots for one loaded value.

    Both stores have fixed capacity. A failed capture rolls back its entire
    record; a later load clears the used lengths, not the caller's buffers.
    """

    __slots__ = ("storage", "ids", "used", "id_used")

    def __init__(self, storage: bytearray, ids: list[tuple[int, int] | None]) -> None:
        if type(storage) is not bytearray or type(ids) is not list:
            raise TypeError("retention needs a bytearray and a list of id slots")
        self.storage = storage
        self.ids = ids
        self.used = 0
        self.id_used = 0

    def reset(self) -> None:
        self.used = 0
        self.id_used = 0

    def capture(self, node: int, steps: tuple[tuple[int, int], ...], identity: int, kind: int, payload: memoryview, ids: list[int]) -> bool:
        """Attempt one complete, already-validated field; False loses one field."""
        if node < 1 or node > 0xFFFFFFFF or len(steps) > _MAX_DEPTH or identity in _RESERVED:
            raise ValueError("invalid retained field path or id")
        start = self.used
        sink = _Sink(memoryview(self.storage), start)
        try:
            sink.u32(0)  # record bytes following this word
            sink.u32(node)
            sink.u32(len(steps))
            for ordinal, index in steps:
                if ordinal < 0 or index < 0:
                    raise ValueError("negative retained path component")
                sink.u32(ordinal)
                sink.u32(index)
            sink.raw(identity.to_bytes(8, "little"))
            sink.byte(kind)
            length_at = sink.pos
            sink.u32(0)
            body_at = sink.pos
            end = resolve_payload(kind, payload, ids, memoryview(self.storage), body_at)
            sink.data[length_at:length_at + 4] = (end - body_at).to_bytes(4, "little")
            sink.data[start:start + 4] = (end - start - 4).to_bytes(4, "little")
        except (Capacity, Excluded):
            return False
        self.used = end
        return True

    def records(self) -> Any:
        """Yield record headers and writable payload slices in capture order."""
        data = memoryview(self.storage)
        at = 0
        while at < self.used:
            length = int.from_bytes(data[at:at + 4], "little")
            end = at + 4 + length
            if end > self.used or length < 21:
                raise ValueError("corrupt retained record")
            node = int.from_bytes(data[at + 4:at + 8], "little")
            depth = int.from_bytes(data[at + 8:at + 12], "little")
            cursor = at + 12
            steps = []
            for _ in range(depth):
                ordinal = int.from_bytes(data[cursor:cursor + 4], "little")
                index = int.from_bytes(data[cursor + 4:cursor + 8], "little")
                steps.append((ordinal, index))
                cursor += 8
            identity = int.from_bytes(data[cursor:cursor + 8], "little")
            kind = int(data[cursor + 8])
            payload_size = int.from_bytes(data[cursor + 9:cursor + 13], "little")
            cursor += 13
            if cursor + payload_size != end:
                raise ValueError("corrupt retained payload length")
            yield node, tuple(steps), identity, kind, data[cursor:end]
            at = end


def capture_file(cls: type[Any], wire: memoryview, retain: TableRetain) -> tuple[int, int]:
    """Capture unknown fields from a file already accepted by the plain reader.

    The count pair is (retained, lost). The caller must run the ordinary file
    read first, so this walk cannot weaken any framing or value refusal.
    """
    from . import descriptor
    from .load import Ctx, NODE_WIRE_ID, _BYTES_WIRE_TYPE_ID, _STRING_WIRE_TYPE_ID, _body_end, _id_table, _skip_payload

    ids = _id_table(wire)
    skip = Ctx(ids)
    counts = [0, 0]
    retain.reset()

    def body(data: memoryview, desc: Any, node: int, steps: tuple[tuple[int, int], ...]) -> None:
        r = Reader(data, 0, len(data), ids)
        while True:
            ref = r.varint()
            if not ref:
                return
            identity = r.id_at(ref)
            kind = r.byte()
            if identity == NODE_WIRE_ID:
                nodes(r.take(r.varint()))
                continue
            start = r.pos
            _skip_payload(r, kind, skip, 0)
            raw = data[start:r.pos]
            field = desc.by_id.get(identity)
            if field is None:
                if retain.capture(node, steps, identity, kind, raw, ids):
                    counts[0] += 1
                else:
                    counts[1] += 1
                continue
            if kind == field.kind:
                value(raw, field, node, steps)

    def framed(raw: memoryview) -> memoryview:
        r = Reader(raw, 0, len(raw), ids)
        return r.take(r.varint())

    def nested(data: memoryview, target: str, node: int, steps: tuple[tuple[int, int], ...]) -> None:
        target_class = descriptor.by_name(target)
        if target_class is not None:
            body(data, target_class.descriptor, node, steps)

    def array(raw: memoryview, field: Any, node: int, steps: tuple[tuple[int, int], ...]) -> None:
        data = framed(raw)
        r = Reader(data, 0, len(data), ids)
        kind = r.byte()
        count = r.varint()
        if field.codec == "map":
            target = field.map_entry or field.target
            for index in range(count):
                nested(r.take(r.varint()), target, node, steps + ((field.ordinal, index),))
            return
        if field.codec == "keyed":
            element = field.elem
            for _ in range(count):
                key = r.id_at(r.varint())
                content = r.take(r.varint())
                if key not in field.keys:
                    counts[1] += 1
                    continue
                if element is not None and element.codec == "table":
                    nested(content, element.target, node, steps + ((field.ordinal, field.keys.index(key)),))
            return
        element = field.elem if field.codec == "optional" else field.element
        for index in range(count):
            if element is not None and element.codec == "table":
                nested(r.take(r.varint()), element.target, node, steps + ((field.ordinal, index),))
            else:
                _skip_payload(r, kind, skip, 0)

    def value(raw: memoryview, field: Any, node: int, steps: tuple[tuple[int, int], ...]) -> None:
        codec = field.inner if field.codec == "optional" else field.codec
        if codec == "table":
            nested(framed(raw), field.target, node, steps + ((field.ordinal, 0),))
        elif codec in ("array", "map", "keyed"):
            array(raw, field, node, steps)
        elif codec == "enum":
            r = Reader(raw, 0, len(raw), ids)
            ref = r.varint()
            if ref and r.id_at(ref) not in field.variant_ids:
                counts[1] += 1
        elif codec == "union":
            r = Reader(raw, 0, len(raw), ids)
            ref = r.varint()
            if not ref:
                return
            arm_id = r.id_at(ref)
            arm_kind = r.byte()
            payload = r.take(r.varint())
            union_class = descriptor._union_by_name(field.target)
            arm = None if union_class is None else union_class.descriptor.by_id.get(arm_id)
            if arm is None:
                counts[1] += 1
            elif arm.field is not None and arm.field.codec == "table" and arm_kind == 13:
                nested(payload, arm.field.target, node, steps + ((field.ordinal, arm.tag),))

    def nodes(data: memoryview) -> None:
        r = Reader(data, 0, len(data), ids)
        count = r.varint()
        for index in range(count):
            identity = r.id_at(r.varint())
            payload = r.take(r.varint())
            if identity in (_BYTES_WIRE_TYPE_ID, _STRING_WIRE_TYPE_ID):
                continue
            target = descriptor.by_id(identity)
            if target is None:
                counts[1] += 1
            else:
                body(payload, target.descriptor, index + 2, ())

    body(wire[1:_body_end(wire)], cls.descriptor, 1, ())
    return counts[0], counts[1]
