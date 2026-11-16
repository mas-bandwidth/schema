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


class _Excluded(Exception):
    """The field contains a node index and cannot be retained."""


class _Capacity(Exception):
    """The complete resolved record does not fit the caller's buffer."""


class _Sink:
    __slots__ = ("data", "pos")

    def __init__(self, data: memoryview, pos: int) -> None:
        self.data = data
        self.pos = pos

    def raw(self, value: Any) -> None:
        end = self.pos + len(value)
        if end > len(self.data):
            raise _Capacity()
        self.data[self.pos:end] = value
        self.pos = end

    def byte(self, value: int) -> None:
        if self.pos >= len(self.data):
            raise _Capacity()
        self.data[self.pos] = value
        self.pos += 1

    def u32(self, value: int) -> None:
        if value > 0xFFFFFFFF:
            raise _Capacity()
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
            raise _Excluded()
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
        raise _Excluded()
    else:
        sink.raw(reader.take(reader.end - reader.pos))


def _payload(reader: Reader, sink: _Sink, kind: int, depth: int) -> None:
    width = _FIXED_WIDTH.get(kind)
    if width is not None:
        sink.raw(reader.take(width))
    elif kind == 17:
        raise _Excluded()
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


def _resolve_payload(kind: int, payload: memoryview, ids: list[int], storage: memoryview, start: int = 0) -> int:
    """Resolve one already-validated payload into storage; return its end.

    On _Capacity or _Excluded, the caller discards everything from start onward.
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
            raise _Capacity()
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


def _measure_payload(kind: int, resolved: memoryview, intern: Any) -> int:
    """Intern references and fill each frame's wire length in one post-order pass."""
    reader = Reader(resolved, 0, len(resolved), [])
    sink = _CountSink()
    _out_payload(reader, sink, kind, 0, intern, True)
    if reader.pos != reader.end:
        raise Refused("wire_damaged", "bytes left after a resolved record")
    return sink.pos


def _emit_payload(kind: int, resolved: memoryview, intern: Any, destination: memoryview, start: int = 0) -> int:
    """Emit a measured record into caller-provided output; return its end."""
    reader = Reader(resolved, 0, len(resolved), [])
    sink = _Sink(destination, start)
    _out_payload(reader, sink, kind, 0, intern, False)
    if reader.pos != reader.end:
        raise Refused("wire_damaged", "bytes left after a resolved record")
    return sink.pos


class TableRetain:
    """Caller-owned records, a bounded path index, and retained-id slots.

    Index nodes and links occupy the same fixed bytearray as the records. A
    Patricia walk tests at most one bit per key bit; no map or second retained
    data store grows with the number of records.
    """

    __slots__ = ("storage", "ids", "used", "id_used", "root", "directory", "loaded_root", "id_root")

    def __init__(self, storage: bytearray, ids: bytearray) -> None:
        if type(storage) is not bytearray or type(ids) is not bytearray or len(ids) % 24:
            raise TypeError("retention needs a bytearray and 24-byte id slots")
        self.storage = storage
        self.ids = ids
        self.used = 0
        self.id_used = 0
        self.root = 0  # offsets are stored plus one, so zero is null
        self.directory: list[Any] = []
        self.loaded_root: Any = None
        self.id_root = 0

    def reset(self) -> None:
        self.used = 0
        self.id_used = 0
        self.root = 0
        self.directory = []
        self.loaded_root = None
        self.id_root = 0

    @staticmethod
    def _word(data: memoryview, at: int) -> int:
        return int.from_bytes(data[at:at + 4], "little")

    @staticmethod
    def _set_word(data: memoryview, at: int, value: int) -> None:
        data[at:at + 4] = value.to_bytes(4, "little")

    @staticmethod
    def _key_end(data: memoryview, record: int) -> int:
        return record + 21 + 8 * TableRetain._word(data, record + 17)

    @staticmethod
    def _bit(data: memoryview, record: int, bit: int) -> int:
        at = record + 13 + bit // 8
        if at >= TableRetain._key_end(data, record):
            return 0
        return (int(data[at]) >> (bit % 8)) & 1

    @staticmethod
    def _first_difference(data: memoryview, left: int, right: int) -> int | None:
        a = left + 13
        b = right + 13
        a_end = TableRetain._key_end(data, left)
        b_end = TableRetain._key_end(data, right)
        if a_end - a != b_end - b:
            # Depth is encoded in the first eight bytes, so distinct lengths
            # necessarily differ before either key ends.
            limit = min(a_end - a, b_end - b)
        else:
            limit = a_end - a
        for offset in range(limit):
            changed = int(data[a + offset]) ^ int(data[b + offset])
            if changed:
                return offset * 8 + (changed & -changed).bit_length() - 1
        if a_end - a != b_end - b:
            raise ValueError("invalid retained path key")
        return None

    def _leaf(self, data: memoryview, record: int) -> int:
        pointer = self.root
        while pointer:
            at = pointer - 1
            tag = int(data[at + 4])
            if tag == 2:
                return at
            if tag != 3:
                raise ValueError("corrupt retained path index")
            bit = self._word(data, at + 5)
            side = self._bit(data, record, bit)
            pointer = self._word(data, at + 9 + side * 4)
        return -1

    def _id_word(self, node: int, offset: int) -> int:
        at = (node - 1) * 24 + offset
        return int.from_bytes(self.ids[at:at + 4], "little")

    def _id_set(self, node: int, offset: int, value: int) -> None:
        at = (node - 1) * 24 + offset
        self.ids[at:at + 4] = value.to_bytes(4, "little")

    def _id_key(self, node: int) -> int:
        at = (node - 1) * 24
        return int.from_bytes(self.ids[at:at + 8], "little")

    def _id_height(self, node: int) -> int:
        return self._id_word(node, 20) if node else 0

    def _id_fix(self, node: int) -> None:
        self._id_set(node, 20, 1 + max(self._id_height(self._id_word(node, 12)), self._id_height(self._id_word(node, 16))))

    def _id_rotate(self, node: int, left: bool) -> int:
        a, b = (16, 12) if left else (12, 16)
        child = self._id_word(node, a)
        self._id_set(node, a, self._id_word(child, b))
        self._id_set(child, b, node)
        self._id_fix(node)
        self._id_fix(child)
        return child

    def _id_add(self, root: int, node: int) -> int:
        if not root:
            return node
        offset = 12 if self._id_key(node) < self._id_key(root) else 16
        self._id_set(root, offset, self._id_add(self._id_word(root, offset), node))
        self._id_fix(root)
        left = self._id_word(root, 12)
        right = self._id_word(root, 16)
        balance = self._id_height(left) - self._id_height(right)
        if balance > 1:
            if self._id_key(node) > self._id_key(left):
                self._id_set(root, 12, self._id_rotate(left, True))
            return self._id_rotate(root, False)
        if balance < -1:
            if self._id_key(node) < self._id_key(right):
                self._id_set(root, 16, self._id_rotate(right, False))
            return self._id_rotate(root, True)
        return root

    def retained_id(self, identity: int) -> int:
        node = self.id_root
        while node:
            key = self._id_key(node)
            if key == identity:
                return self._id_word(node, 8)
            node = self._id_word(node, 12 if identity < key else 16)
        return 0

    def add_retained_id(self, identity: int, slot: int) -> None:
        if self.id_used >= len(self.ids) // 24:
            raise _Capacity()
        node = self.id_used + 1
        at = (node - 1) * 24
        self.ids[at:at + 8] = identity.to_bytes(8, "little")
        self._id_set(node, 8, slot)
        self._id_set(node, 12, 0)
        self._id_set(node, 16, 0)
        self._id_set(node, 20, 1)
        self.id_root = self._id_add(self.id_root, node)
        self.id_used = node

    def _id_balance(self, root: int) -> int:
        self._id_fix(root)
        left = self._id_word(root, 12)
        right = self._id_word(root, 16)
        delta = self._id_height(left) - self._id_height(right)
        if delta > 1:
            if self._id_height(self._id_word(left, 16)) > self._id_height(self._id_word(left, 12)):
                self._id_set(root, 12, self._id_rotate(left, True))
            return self._id_rotate(root, False)
        if delta < -1:
            if self._id_height(self._id_word(right, 12)) > self._id_height(self._id_word(right, 16)):
                self._id_set(root, 16, self._id_rotate(right, False))
            return self._id_rotate(root, True)
        return root

    def _id_pop_min(self, root: int) -> tuple[int, int]:
        left = self._id_word(root, 12)
        if not left:
            return self._id_word(root, 16), root
        remaining, minimum = self._id_pop_min(left)
        self._id_set(root, 12, remaining)
        return self._id_balance(root), minimum

    def _id_remove(self, root: int, identity: int) -> int:
        if not root:
            raise ValueError("retained id index is corrupt")
        key = self._id_key(root)
        if key != identity:
            offset = 12 if identity < key else 16
            self._id_set(root, offset, self._id_remove(self._id_word(root, offset), identity))
            return self._id_balance(root)
        left = self._id_word(root, 12)
        right = self._id_word(root, 16)
        if not left:
            return right
        if not right:
            return left
        remaining, replacement = self._id_pop_min(right)
        self._id_set(replacement, 12, left)
        self._id_set(replacement, 16, remaining)
        return self._id_balance(replacement)

    def truncate_ids(self, count: int) -> None:
        while self.id_used > count:
            self.id_root = self._id_remove(self.id_root, self._id_key(self.id_used))
            self.id_used -= 1

    def capture(self, node: int, steps: tuple[tuple[int, int], ...], identity: int, kind: int, payload: memoryview, ids: list[int]) -> bool:
        """Attempt one complete, already-validated field; False loses one field."""
        if node < 1 or node > 0xFFFFFFFF or len(steps) > _MAX_DEPTH or identity in _RESERVED:
            raise ValueError("invalid retained field path or id")
        start = self.used
        sink = _Sink(memoryview(self.storage), start)
        try:
            sink.u32(0)  # record bytes following this word
            sink.byte(1)  # record, followed by next record at the same path
            sink.u32(0)
            sink.u32(node)  # original directory index
            sink.u32(node)  # node index on the current save walk
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
            end = _resolve_payload(kind, payload, ids, memoryview(self.storage), body_at)
            sink.pos = end
            data = sink.data
            leaf = self._leaf(data, start)
            difference = None if leaf < 0 else self._first_difference(data, start, self._word(data, leaf + 5) - 1)
            new_leaf = -1
            new_branch = -1
            if leaf < 0 or difference is not None:
                new_leaf = sink.pos
                sink.u32(13)
                sink.byte(2)
                sink.u32(start + 1)
                sink.u32(start + 1)
                sink.u32(0)
                if difference is not None:
                    new_branch = sink.pos
                    sink.u32(13)
                    sink.byte(3)
                    sink.u32(difference)
                    sink.u32(0)
                    sink.u32(0)
        except (_Capacity, _Excluded):
            return False
        self._set_word(data, length_at, end - body_at)
        self._set_word(data, start, end - start - 4)
        if leaf < 0:
            self.root = new_leaf + 1
        elif difference is None:
            last = self._word(data, leaf + 9) - 1
            self._set_word(data, last + 5, start + 1)
            self._set_word(data, leaf + 9, start + 1)
        else:
            pointer = self.root
            parent = -1
            parent_side = 0
            while pointer:
                at = pointer - 1
                if int(data[at + 4]) != 3 or self._word(data, at + 5) >= difference:
                    break
                parent = at
                parent_side = self._bit(data, start, self._word(data, at + 5))
                pointer = self._word(data, at + 9 + parent_side * 4)
            side = self._bit(data, start, difference)
            self._set_word(data, new_branch + 9 + side * 4, new_leaf + 1)
            self._set_word(data, new_branch + 9 + (1 - side) * 4, pointer)
            if parent < 0:
                self.root = new_branch + 1
            else:
                self._set_word(data, parent + 9 + parent_side * 4, new_branch + 1)
        self.used = sink.pos
        return True

    @staticmethod
    def _query_bit(node: int, steps: tuple[tuple[int, int], ...], bit: int) -> int:
        word = bit // 32
        if word == 0:
            value = node
        elif word == 1:
            value = len(steps)
        else:
            pair = (word - 2) // 2
            if pair >= len(steps):
                return 0
            value = steps[pair][(word - 2) % 2]
        return (value >> (bit % 32)) & 1

    def records_at(self, node: int, steps: tuple[tuple[int, int], ...]) -> Any:
        """Yield one body's retained fields in capture order without a heap index."""
        data = memoryview(self.storage)
        pointer = self.root
        while pointer:
            at = pointer - 1
            tag = int(data[at + 4])
            if tag == 2:
                break
            if tag != 3:
                raise ValueError("corrupt retained path index")
            bit = self._word(data, at + 5)
            side = self._query_bit(node, steps, bit)
            pointer = self._word(data, at + 9 + side * 4)
        if not pointer:
            return
        leaf = pointer - 1
        pointer = self._word(data, leaf + 5)
        record = pointer - 1
        if self._word(data, record + 13) != node or self._word(data, record + 17) != len(steps):
            return
        cursor = record + 21
        for ordinal, index in steps:
            if self._word(data, cursor) != ordinal or self._word(data, cursor + 4) != index:
                return
            cursor += 8
        while pointer:
            record = pointer - 1
            end = record + 4 + self._word(data, record)
            cursor = self._key_end(data, record)
            identity = int.from_bytes(data[cursor:cursor + 8], "little")
            kind = int(data[cursor + 8])
            payload_size = self._word(data, cursor + 9)
            cursor += 13
            if end > self.used or cursor + payload_size != end:
                raise ValueError("corrupt retained payload length")
            yield identity, kind, data[cursor:end], record
            pointer = self._word(data, record + 5)

    def has_body(self, node: int, steps: tuple[tuple[int, int], ...]) -> bool:
        """Whether this body or a schema-bounded descendant has retained data."""
        data = memoryview(self.storage)
        for depth in range(len(steps), _MAX_DEPTH + 1):
            pointer = self.root
            while pointer:
                at = pointer - 1
                tag = int(data[at + 4])
                if tag == 2:
                    break
                if tag != 3:
                    raise ValueError("corrupt retained path index")
                bit = self._word(data, at + 5)
                word = bit // 32
                if word == 0:
                    value = node
                elif word == 1:
                    value = depth
                else:
                    pair = (word - 2) // 2
                    value = steps[pair][(word - 2) % 2] if pair < len(steps) else 0
                pointer = self._word(data, at + 9 + ((value >> (bit % 32)) & 1) * 4)
            if not pointer:
                continue
            record = self._word(data, pointer - 1 + 5) - 1
            if self._word(data, record + 13) != node or self._word(data, record + 17) != depth:
                continue
            cursor = record + 21
            matches = True
            for ordinal, index in steps:
                if self._word(data, cursor) != ordinal or self._word(data, cursor + 4) != index:
                    matches = False
                    break
                cursor += 8
            if matches:
                return True
        return False

    def begin_save(self, ctx: Any = None, directory: Any = None) -> None:
        """Clear placement marks; optionally map old node anchors to this walk.

        The normal load's directory is retained by reference, and the normal
        save's existing node-index map is reused. Rebuilding the path trie
        consumes only its existing nodes in the caller's bytearray.
        """
        self.id_used = 0
        self.id_root = 0
        data = memoryview(self.storage)
        at = 0
        free = 0
        if ctx is not None and directory is None:
            raise ValueError("node remapping needs the loaded directory")
        while at < self.used:
            length = self._word(data, at)
            end = at + 4 + length
            if end > self.used or length < 1:
                raise ValueError("corrupt retained record")
            tag = int(data[at + 4]) & 0x7F
            data[at + 4] = tag
            if ctx is not None:
                if tag in (2, 3):
                    data[at + 4] = 2
                    self._set_word(data, at + 5, free)
                    free = at + 1
                elif tag == 1:
                    self._set_word(data, at + 5, 0)
                    original = self._word(data, at + 9)
                    mapped = 1
                    if original > 1:
                        index = original - 2
                        obj = directory[index] if index < len(directory) else None
                        mapped = ctx.indices.get(id(obj), 0) if obj is not None else 0
                    self._set_word(data, at + 13, mapped)
                else:
                    raise ValueError("corrupt retained index entry")
            at = end
        if ctx is not None:
            self.root = 0
            at = 0
            while at < self.used:
                end = at + 4 + self._word(data, at)
                if int(data[at + 4]) == 1:
                    leaf = self._leaf(data, at)
                    difference = None if leaf < 0 else self._first_difference(data, at, self._word(data, leaf + 5) - 1)
                    if leaf >= 0 and difference is None:
                        last = self._word(data, leaf + 9) - 1
                        self._set_word(data, last + 5, at + 1)
                        self._set_word(data, leaf + 9, at + 1)
                    else:
                        if not free:
                            raise ValueError("retained index has too few nodes")
                        new_leaf = free - 1
                        free = self._word(data, new_leaf + 5)
                        data[new_leaf + 4] = 2
                        self._set_word(data, new_leaf + 5, at + 1)
                        self._set_word(data, new_leaf + 9, at + 1)
                        self._set_word(data, new_leaf + 13, 0)
                        if leaf < 0:
                            self.root = new_leaf + 1
                        else:
                            if not free or difference is None:
                                raise ValueError("retained index has too few branches")
                            branch = free - 1
                            free = self._word(data, branch + 5)
                            data[branch + 4] = 3
                            self._set_word(data, branch + 5, difference)
                            pointer = self.root
                            parent = -1
                            parent_side = 0
                            while pointer:
                                current = pointer - 1
                                if int(data[current + 4]) != 3 or self._word(data, current + 5) >= difference:
                                    break
                                parent = current
                                parent_side = self._bit(data, at, self._word(data, current + 5))
                                pointer = self._word(data, current + 9 + parent_side * 4)
                            side = self._bit(data, at, difference)
                            self._set_word(data, branch + 9 + side * 4, new_leaf + 1)
                            self._set_word(data, branch + 9 + (1 - side) * 4, pointer)
                            if parent < 0:
                                self.root = branch + 1
                            else:
                                self._set_word(data, parent + 9 + parent_side * 4, branch + 1)
                at = end

    def write_tail(self, node: int, steps: tuple[tuple[int, int], ...], known: frozenset[int] | tuple[int, ...], ctx: Any, writer: Any) -> int:
        """Write this body's records, returning the number lost to id capacity."""
        lost = 0
        for identity, kind, payload, record in self.records_at(node, steps):
            if not _emit_record(self, identity, kind, payload, known, ctx, writer):
                lost += 1
            self.storage[record + 4] |= 0x80
        return lost

    def unplaced(self) -> int:
        """Count records whose body was never reached by this save walk."""
        count = 0
        data = memoryview(self.storage)
        at = 0
        while at < self.used:
            length = self._word(data, at)
            end = at + 4 + length
            if end > self.used or length < 1:
                raise ValueError("corrupt retained record")
            if int(data[at + 4]) == 1:
                count += 1
            at = end
        return count

    def records(self) -> Any:
        """Yield record headers and writable payload slices in capture order."""
        data = memoryview(self.storage)
        at = 0
        while at < self.used:
            length = self._word(data, at)
            end = at + 4 + length
            if end > self.used or length < 1:
                raise ValueError("corrupt retained record")
            tag = int(data[at + 4]) & 0x7F
            if tag in (2, 3):
                if length != 13:
                    raise ValueError("corrupt retained index entry")
                at = end
                continue
            if tag != 1 or length < 30:
                raise ValueError("corrupt retained record")
            node = self._word(data, at + 9)
            depth = self._word(data, at + 17)
            cursor = at + 21
            steps = []
            for _ in range(depth):
                ordinal = self._word(data, cursor)
                index = self._word(data, cursor + 4)
                steps.append((ordinal, index))
                cursor += 8
            identity = int.from_bytes(data[cursor:cursor + 8], "little")
            kind = int(data[cursor + 8])
            payload_size = self._word(data, cursor + 9)
            cursor += 13
            if cursor + payload_size != end:
                raise ValueError("corrupt retained payload length")
            yield node, tuple(steps), identity, kind, data[cursor:end]
            at = end


def _capture_file(cls: type[Any], wire: memoryview, retain: TableRetain, ids: list[int] | None = None) -> tuple[int, int]:
    """Capture unknown fields from a file already accepted by the plain reader.

    The count pair is (retained, lost). The caller must run the ordinary file
    read first, so this walk cannot weaken any framing or value refusal.
    """
    from . import descriptor
    from .load import Ctx, NODE_WIRE_ID, _BYTES_WIRE_TYPE_ID, _STRING_WIRE_TYPE_ID, _body_end, _id_table, _placeable, _skip_payload

    if ids is None:
        ids = _id_table(wire)
    skip = Ctx(ids)
    class _DiscardEdges(list[int]):
        def append(self, value: int) -> None:
            pass
    skip.edges = _DiscardEdges()
    placeable = _placeable(cls)
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

    def array(raw: memoryview, field: Any, node: int, steps: tuple[tuple[int, int], ...], ordinal: int | None = None, unframed: bool = False) -> None:
        data = raw if unframed else framed(raw)
        ordinal = field.ordinal if ordinal is None else ordinal
        r = Reader(data, 0, len(data), ids)
        kind = r.byte()
        count = r.varint()
        if field.codec == "map":
            target = field.map_entry or field.target
            for index in range(count):
                nested(r.take(r.varint()), target, node, steps + ((ordinal, index),))
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
                    nested(content, element.target, node, steps + ((ordinal, field.keys.index(key)),))
                elif element is not None and element.codec == "enum":
                    item = Reader(content, 0, len(content), ids)
                    ref = item.varint()
                    if ref and item.id_at(ref) not in element.variant_ids:
                        counts[1] += 1
            return
        element = field.elem if field.codec == "optional" else field.element
        for index in range(count):
            if element is not None and element.codec == "table":
                nested(r.take(r.varint()), element.target, node, steps + ((ordinal, index),))
            elif element is not None and element.codec == "enum":
                ref = r.varint()
                if ref and r.id_at(ref) not in element.variant_ids:
                    counts[1] += 1
            else:
                _skip_payload(r, kind, skip, 0)

    def union(reader: Reader, target: str, ordinal: int, node: int, steps: tuple[tuple[int, int], ...]) -> None:
        ref = reader.varint()
        if not ref:
            return
        arm_id = reader.id_at(ref)
        arm_kind = reader.byte()
        payload = reader.take(reader.varint())
        union_class = descriptor._union_by_name(target)
        arm = None if union_class is None else union_class.descriptor.by_id.get(arm_id)
        if arm is None:
            counts[1] += 1
            return
        if arm.field is None:
            return
        child_steps = steps + ((ordinal, arm.tag),)
        if arm.field.codec == "table" and arm_kind == 13:
            nested(payload, arm.field.target, node, child_steps)
        elif arm.field.codec == "union" and arm_kind == 15:
            union(Reader(payload, 0, len(payload), ids), arm.field.target, ordinal, node, child_steps)
        elif arm.field.codec in ("array", "map", "keyed") and arm_kind in (14, 16):
            array_ordinal = ordinal if arm.field.codec == "array" else arm.field.ordinal
            array(payload, arm.field, node, child_steps, array_ordinal, True)
        elif arm.field.codec == "union_array" and arm_kind == 14:
            array_reader = Reader(payload, 0, len(payload), ids)
            array_reader.byte()
            count = array_reader.varint()
            for index in range(count):
                union(array_reader, arm.field.target, ordinal, node, child_steps + ((ordinal, index),))
        elif arm.field.codec == "enum" and arm_kind == 30:
            enum_reader = Reader(payload, 0, len(payload), ids)
            enum_ref = enum_reader.varint()
            if enum_ref and enum_reader.id_at(enum_ref) not in arm.field.variant_ids:
                counts[1] += 1

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
            union(Reader(raw, 0, len(raw), ids), field.target, field.ordinal, node, steps)
        elif codec == "union_array":
            data = framed(raw)
            reader = Reader(data, 0, len(data), ids)
            reader.byte()
            count = reader.varint()
            for index in range(count):
                union(reader, field.target, field.ordinal, node, steps + ((field.ordinal, index),))

    def nodes(data: memoryview) -> None:
        r = Reader(data, 0, len(data), ids)
        count = r.varint()
        for index in range(count):
            identity = r.id_at(r.varint())
            payload = r.take(r.varint())
            if identity in (_BYTES_WIRE_TYPE_ID, _STRING_WIRE_TYPE_ID):
                continue
            target = descriptor.by_id(identity) if identity in placeable else None
            if target is None:
                counts[1] += 1
            else:
                body(payload, target.descriptor, index + 2, ())

    body(wire[1:_body_end(wire)], cls.descriptor, 1, ())
    return counts[0], counts[1]


def _emit_record(retain: TableRetain, identity: int, kind: int, resolved: memoryview, known: frozenset[int] | tuple[int, ...], ctx: Any, writer: Any) -> bool:
    """Place one record atomically, or return False when its ids do not fit.

    `ctx` is the normal save context. The caller initializes the fixed id
    slots at the start of a measure or save and counts False as one loss.
    """
    previous_ids = len(ctx.ids)
    previous_retained = retain.id_used

    def intern(value: int) -> int:
        # The emitted tuple is sorted once at compile time; test callers may
        # also supply a fixed set. Neither grows with retained input.
        if isinstance(known, tuple):
            from bisect import bisect_left

            position = bisect_left(known, value)
            nameable = position < len(known) and known[position] == value
        else:
            nameable = value in known
        if nameable:
            existing = ctx.refs.get(value)
            if existing is not None:
                return int(existing)
            if len(ctx.ids) + retain.id_used + ctx.remaining >= 4096 and value not in ctx.reserved:
                raise _Capacity()
            return int(ctx.idref(value))
        existing = retain.retained_id(value)
        if existing:
            return existing
        if len(ctx.ids) + retain.id_used + ctx.remaining >= 4096:
            raise _Capacity()
        slot = len(ctx.ids) + retain.id_used + 1
        retain.add_retained_id(value, slot)
        return slot

    try:
        field_ref = intern(identity)
        _measure_payload(kind, resolved, intern)
    except _Capacity:
        while len(ctx.ids) > previous_ids:
            value = ctx.ids.pop()
            ctx.forget_id(value)
        retain.truncate_ids(previous_retained)
        return False
    writer.varint(field_ref)
    writer.byte(kind)
    reader = Reader(resolved, 0, len(resolved), [])
    _out_payload(reader, writer, kind, 0, intern, False)
    if reader.pos != reader.end:
        raise Refused("wire_damaged", "bytes left after a resolved record")
    return True
