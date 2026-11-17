"""Bit stream primitives for the table message form (SPEC-TABLES §3.3)."""

from __future__ import annotations

from . import LoadReport, Refused
from .load import Reader
from typing import Any, Callable


class BitReader:
    """Read low-bit-first fields from a bounded batch without overreading."""

    __slots__ = ("data", "position")

    def __init__(self, data: bytes | memoryview, position: int = 0) -> None:
        self.data = data
        self.position = position

    def get(self, width: int) -> int:
        if width < 0 or self.position + width > len(self.data) * 8:
            raise Refused("wire_damaged", "message bit stream is truncated")
        start = self.position // 8
        shift = self.position % 8
        end = (self.position + width + 7) // 8
        value = int.from_bytes(self.data[start:end], "little")
        self.position += width
        return (value >> shift) & ((1 << width) - 1)

    def align(self) -> None:
        padding = (-self.position) & 7
        if self.get(padding):
            raise Refused("wire_damaged", "message padding is nonzero")

    def raw(self, length: int) -> bytes:
        if self.position & 7 or length < 0 or length > (len(self.data) * 8 - self.position) // 8:
            raise Refused("wire_damaged", "message bytes exceed their buffer")
        start = self.position // 8
        self.position += length * 8
        return bytes(self.data[start : start + length])

    def finish(self) -> None:
        self.align()
        if self.position != len(self.data) * 8:
            raise Refused("wire_damaged", "bytes follow the message batch")


class BitWriter:
    """Write low-bit-first fields, sharing one cursor across the batch."""

    __slots__ = ("data", "position", "measure")

    def __init__(self, measure: bool = False) -> None:
        self.data = bytearray()
        self.position = 0
        self.measure = measure

    def put(self, value: int, width: int) -> None:
        if width < 0 or value < 0 or value.bit_length() > width:
            raise ValueError("value does not fit the message width")
        start = self.position // 8
        shift = self.position % 8
        end = (self.position + width + 7) // 8
        if not self.measure:
            if end > len(self.data):
                self.data.extend(bytes(end - len(self.data)))
            if end > start:
                current = int.from_bytes(self.data[start:end], "little")
                self.data[start:end] = (current | (value << shift)).to_bytes(end - start, "little")
        self.position += width

    def align(self) -> None:
        self.put(0, (-self.position) & 7)

    def raw(self, data: bytes) -> None:
        if self.position & 7:
            raise ValueError("message bytes are not aligned")
        if not self.measure:
            self.data.extend(data)
        self.position += len(data) * 8

    def finish(self) -> bytes:
        self.align()
        return bytes(self.data)


# The announced shape belongs to the remote endpoint. It is never replaced by the local
# declaration when measuring or skipping a payload from a different build.
class Shape:
    __slots__ = ("kind", "packing", "bits", "base", "minimum", "maximum", "element", "inner", "quant")

    def __init__(self, kind: int) -> None:
        self.kind = kind
        self.packing = 0
        self.bits = 0
        self.base = 0
        self.minimum = 0
        self.maximum = 0
        self.element = 0
        self.inner: Shape | None = None
        self.quant: tuple[float, float, float] | None = None


def _kind_bits(kind: int) -> int:
    if 2 <= kind <= 9:
        return 8 << ((kind - 2) % 4)
    if kind in (18, 19):
        return 128
    if 20 <= kind <= 29:
        return 8 << ((kind - 20) % 5)
    raise Refused("wire_damaged", "unknown numeric kind")


def _shape(reader: "Reader", kind: int) -> Shape:
    import math
    import struct

    shape = Shape(kind)
    if 2 <= kind <= 10 or 18 <= kind <= 29:
        shape.packing = reader.byte()
        if shape.packing == 0:
            return shape
        if kind == 10 and shape.packing == 2:
            minimum, maximum, resolution = struct.unpack("<fff", reader.take(12))
            if not all(math.isfinite(x) for x in (minimum, maximum, resolution)) or minimum >= maximum or resolution <= 0:
                raise Refused("wire_damaged", "invalid compressed float shape")
            try:
                delta = struct.unpack("<f", struct.pack("<f", maximum - minimum))[0]
                steps = struct.unpack("<f", struct.pack("<f", delta / resolution))[0]
            except OverflowError:
                raise Refused("wire_damaged", "invalid compressed float width") from None
            if not math.isfinite(delta) or not math.isfinite(steps):
                raise Refused("wire_damaged", "invalid compressed float width")
            count = math.ceil(min(max(steps, 1.0), 4294967040.0))
            shape.bits = count.bit_length()
            shape.quant = (minimum, maximum, resolution)
            return shape
        if shape.packing != 1 or kind == 10:
            raise Refused("wire_damaged", "invalid numeric packing")
        width = reader.varint()
        domain = _kind_bits(kind)
        if width > domain:
            raise Refused("wire_damaged", "message width exceeds its kind")
        shape.bits = width
        signed = 2 <= kind <= 5 or kind == 18 or 20 <= kind <= 24
        if kind in (18, 19) or 20 <= kind <= 29:
            shape.base = int.from_bytes(reader.take(16), "little", signed=signed)
        else:
            raw = reader.varint()
            shape.base = (raw >> 1) ^ -(raw & 1) if signed else raw
        low = -(1 << (domain - 1)) if signed else 0
        high = (1 << (domain - int(signed))) - 1
        if not low <= shape.base <= high:
            raise Refused("wire_damaged", "message base exceeds its kind")
        return shape
    if kind == 12:
        shape.maximum = reader.varint()
        if shape.maximum > 0x7FFFFFFF:
            raise Refused("wire_damaged", "message text bound exceeds storage")
    elif kind in (14, 16):
        if kind == 14:
            shape.minimum = reader.varint()
        shape.maximum = reader.varint()
        if shape.minimum > shape.maximum or shape.maximum > 0xFFFFFFFF:
            raise Refused("wire_damaged", "invalid message array bound")
        shape.element = reader.byte()
        if not _known_kind(shape.element) or shape.element in (14, 16):
            raise Refused("wire_damaged", "invalid message element kind")
        shape.inner = _shape(reader, shape.element)
    return shape


def _known_kind(kind: int) -> bool:
    return 0 <= kind <= 32 or kind == 36


class Entry:
    __slots__ = ("field_id", "kind", "shape", "key")

    def __init__(self, field_id: int, kind: int, shape: Shape, key: bytes) -> None:
        self.field_id = field_id
        self.kind = kind
        self.shape = shape
        self.key = key


class BuildVersionMismatch(Refused):
    """A strict connection refused a different announced build."""

    __slots__ = ("remote", "own")

    def __init__(self, remote: int, own: int) -> None:
        super().__init__("build_version_mismatch")
        self.remote = remote
        self.own = own


class MessageVocabulary:
    """One connection's terminal announcement state and remote vocabulary."""

    __slots__ = ("strict", "own_version", "build_version", "announced", "refused", "entries", "unknown", "resolved", "slots", "names", "variants")

    def __init__(self, own_version: int = 0, strict: bool = False) -> None:
        self.strict = strict
        self.own_version = own_version
        self.build_version = 0
        self.announced = False
        self.refused = False
        self.entries: list[Entry] = []
        self.unknown = 0
        self.resolved: dict[tuple[str, int], Any] = {}
        self.slots: dict[bytes, int] = {}
        self.names: dict[int, int] = {}
        self.variants: dict[int, dict[int, int]] = {}

    @property
    def ref_bits(self) -> int:
        return max(1, len(self.entries).bit_length())


def announce_read(vocab: MessageVocabulary, data: bytes | bytearray | memoryview, *, max_bytes: int = 65536, max_entries: int = 4096) -> None:
    """Resolve the first announcement once; any refusal is terminal."""
    from .descriptor import TableDesc
    from .load import Ctx, _body_end, _check_id_order, _check_node_table, _id_table, _read_node_table, _resolve, _skip, buffer_view

    if vocab.announced or vocab.refused:
        raise Refused("second_announcement")
    vocab.refused = True
    view = buffer_view(data)
    if not view:
        raise Refused("wire_damaged", "announcement has no form byte")
    if view[0] != 1:
        raise Refused("message_form_as_file" if view[0] == 2 else "newer_form")
    ids = _id_table(view)
    _check_id_order(view, ids, announcement=True)
    reader = Reader(view, 1, _body_end(view), ids)
    ctx = Ctx(ids)
    ctx.nonfinite = []

    class AnnouncementRoot:
        descriptor = TableDesc("_announcement", 0, True, ())

    ctx.root = AnnouncementRoot()
    seen: set[int] = set()
    raw_vocab: memoryview | None = None
    version_seen = False
    unknown = 0
    oversize = False
    while True:
        ref = reader.varint()
        if not ref:
            break
        field_id = reader.id_at(ref)
        kind = reader.byte()
        if field_id in seen:
            raise Refused("wire_damaged", "announcement field repeated")
        seen.add(field_id)
        if field_id == 0xFFFFFFFFFFFFFFFE:
            if kind != 9:
                raise Refused("wire_damaged", "announcement build version has wrong kind")
            vocab.build_version = int.from_bytes(reader.take(8), "little")
            version_seen = True
        elif field_id == 0xFFFFFFFFFFFFFFFD:
            if kind != 14:
                raise Refused("wire_damaged", "announcement vocabulary has wrong kind")
            field = reader.take(reader.varint())
            content = Reader(field, 0, len(field), ids)
            if content.byte() != 6:
                raise Refused("wire_damaged", "announcement vocabulary is not bytes")
            length = content.varint()
            if length != content.end - content.pos:
                raise Refused("wire_damaged", "announcement vocabulary has invalid length")
            oversize = length > max_bytes
            raw_vocab = content.take(length)
        else:
            if field_id == 0xFFFFFFFFFFFFFFFF:
                if kind != 12:
                    raise Refused("wire_damaged", "announcement node table has wrong kind")
                payload = reader.take(reader.varint())
                check = Ctx(ids)
                check.nonfinite = ctx.nonfinite
                _check_node_table(payload, check)
                _read_node_table(payload, ctx)
            else:
                _skip(reader, kind, ctx)
            unknown += 1
    if reader.pos != reader.end or not version_seen or raw_vocab is None:
        raise Refused("wire_damaged", "incomplete announcement")
    _resolve(ctx)
    if ctx.nonfinite:
        raise Refused("non_finite_float")
    if oversize:
        raise Refused("vocabulary_too_large")
    if vocab.strict and vocab.build_version != vocab.own_version:
        raise BuildVersionMismatch(vocab.build_version, vocab.own_version)
    source = Reader(raw_vocab, 0, len(raw_vocab), [])
    entries: list[Entry] = []
    triples: set[bytes] = set()
    node_seen = False
    while source.pos < source.end:
        if len(entries) >= max_entries:
            raise Refused("vocabulary_too_large")
        start = source.pos
        field_id = int.from_bytes(source.take(8), "little")
        kind = source.byte()
        if field_id in (0xFFFFFFFFFFFFFFFE, 0xFFFFFFFFFFFFFFFD) or not _known_kind(kind):
            raise Refused("wire_damaged", "invalid message vocabulary entry")
        if field_id == 0xFFFFFFFFFFFFFFFF:
            if node_seen:
                raise Refused("wire_damaged", "node vocabulary entry repeated")
            node_seen = True
        shape = _shape(source, kind)
        key = bytes(raw_vocab[start:source.pos])
        if key in triples:
            raise Refused("wire_damaged", "message vocabulary entry repeated")
        triples.add(key)
        entries.append(Entry(field_id, kind, shape, key))
    vocab.entries = entries
    vocab.slots = {entry.key: i + 1 for i, entry in enumerate(entries)}
    vocab.names = {entry.field_id: i + 1 for i, entry in enumerate(entries) if entry.kind == 0}
    vocab.unknown = unknown
    vocab.announced = True
    vocab.refused = False


def _reference(reader: BitReader, vocab: MessageVocabulary) -> Entry | None:
    reference = reader.get(vocab.ref_bits)
    if reference == 0:
        return None
    if reference > len(vocab.entries):
        raise Refused("wire_damaged", "message reference exceeds vocabulary")
    entry = vocab.entries[reference - 1]
    if entry.field_id >= 0xFFFFFFFFFFFFFFFD:
        raise Refused("wire_damaged", "reserved message reference")
    return entry


def _numeric_width(shape: Shape) -> int:
    if shape.kind == 1:
        return 1
    if shape.kind == 36:
        return 64
    return shape.bits if shape.packing == 1 else _kind_bits(shape.kind)


def _write_scalar(writer: BitWriter, shape: Shape, value: object) -> None:
    from . import InvalidValue
    if shape.kind == 1:
        if type(value) is not bool:
            raise InvalidValue("", "message bool is not bool")
        writer.put(int(value), 1)
        return
    if shape.kind in (10, 11):
        import math
        import struct
        if type(value) is not float or not math.isfinite(value):
            raise InvalidValue("", "message float is not finite")
        if shape.packing == 2:
            assert shape.quant is not None
            minimum, maximum, resolution = shape.quant
            v = _f32(value)
            if not minimum <= v <= maximum:
                raise InvalidValue("", "compressed float exceeds its declared range")
            delta = _f32(maximum - minimum)
            count = math.ceil(min(max(_f32(delta / resolution), 1.0), 4294967040.0))
            normalized = min(max(_f32(_f32(v - minimum) / delta), 0.0), 1.0)
            index = min(math.floor(_f32(_f32(normalized * _f32(float(count))) + 0.5)), count)
            writer.put(index, shape.bits)
        else:
            packed = struct.pack("<f" if shape.kind == 10 else "<d", value)
            writer.put(int.from_bytes(packed, "little"), len(packed) * 8)
        return
    if not (2 <= shape.kind <= 9 or 18 <= shape.kind <= 29 or shape.kind == 36):
        raise InvalidValue("", "message kind is not implemented")
    if type(value) is not int:
        raise InvalidValue("", "message integer is not int")
    width = _numeric_width(shape)
    raw = value - shape.base if shape.packing == 1 else value & ((1 << width) - 1)
    if raw < 0 or raw.bit_length() > width:
        raise InvalidValue("", "message value is outside its announced width")
    writer.put(raw, width)


def _read_scalar(reader: BitReader, shape: Shape) -> object:
    if shape.kind == 1:
        return bool(reader.get(1))
    if shape.kind in (10, 11):
        import math
        import struct
        if shape.packing == 2:
            assert shape.quant is not None
            minimum, maximum, resolution = shape.quant
            delta = _f32(maximum - minimum)
            count = math.ceil(min(max(_f32(delta / resolution), 1.0), 4294967040.0))
            index = reader.get(shape.bits)
            if index > count:
                raise Refused("wire_damaged", "compressed float index exceeds count")
            fraction = _f32(_f32(float(index)) / _f32(float(count)))
            value = _f32(_f32(fraction * delta) + minimum)
        else:
            width = 32 if shape.kind == 10 else 64
            raw = reader.get(width).to_bytes(width // 8, "little")
            value = struct.unpack("<f" if width == 32 else "<d", raw)[0]
        if not math.isfinite(value):
            raise Refused("non_finite_float")
        return value
    if not (2 <= shape.kind <= 9 or 18 <= shape.kind <= 29 or shape.kind == 36):
        raise Refused("kind_mismatch", "message kind is not implemented")
    width = _numeric_width(shape)
    value = reader.get(width)
    if shape.packing == 1:
        return value + shape.base
    signed = 2 <= shape.kind <= 5 or shape.kind == 18 or 20 <= shape.kind <= 24 or shape.kind == 36
    return value - (1 << width) if signed and width and value & (1 << (width - 1)) else value


def _write_body(writer: BitWriter, value: object, vocab: MessageVocabulary, keys: dict[tuple[str, str], bytes]) -> None:
    from typing import Any
    obj: Any = value
    for field in obj.descriptor.fields:
        current = getattr(obj, field.name)
        if field.is_default(current) and not getattr(field, "required", False):
            continue
        key = keys.get((obj.descriptor.name, field.name))
        slot = vocab.slots.get(key) if key is not None else None
        if slot is None:
            raise ValueError("message field is absent from its own vocabulary")
        writer.put(slot, vocab.ref_bits)
        _write_value(writer, field, current, vocab.entries[slot - 1].shape, vocab, keys)
    writer.put(0, vocab.ref_bits)


def _read_body(reader: BitReader, cls: type[object], vocab: MessageVocabulary, own_version: int, keys: dict[tuple[str, str], bytes], stats: list[int], depth: int = 0) -> object:
    from typing import Any
    if depth > 128:
        raise Refused("wire_damaged", "message nesting exceeds 128")
    obj: Any = cls()
    seen: set[int] = set()
    last_ordinal = -1
    while True:
        entry = _reference(reader, vocab)
        if entry is None:
            for field in obj.descriptor.fields:
                if getattr(field, "required", False) and field.field_id not in seen:
                    raise Refused("missing_required", field.name)
            return obj
        if entry.field_id in seen:
            raise Refused("wire_damaged", "message field repeated")
        seen.add(entry.field_id)
        field = vocab.resolved.get((obj.descriptor.name, entry.field_id))
        if field is None:
            if vocab.build_version == own_version:
                raise Refused("wire_damaged", "same-build message names an undeclared field")
            _skip_value(reader, entry.shape, vocab, depth)
            stats[0] += 1
            continue
        own_key = keys.get((obj.descriptor.name, field.name))
        if entry.kind != field.kind:
            if not _widens(entry.kind, field.kind):
                raise Refused("kind_mismatch", "message field kind changed")
            stats[1] += 1
        same_shape = vocab.build_version == own_version and own_key == entry.key
        if same_shape:
            if field.ordinal <= last_ordinal:
                raise Refused("wire_damaged", "message fields are out of order")
            last_ordinal = field.ordinal
        substituted = stats[2]
        value = _read_value(reader, field, entry.shape, vocab, own_version, keys, stats, depth, True, getattr(obj, field.name), same_shape)
        if type(value) is int and (value < field.lo or value > field.hi):
            raise Refused("out_of_range", field.name)
        if type(value) is float and getattr(field, "float_range", False):
            if value < field.fmin or value > field.fmax:
                raise Refused("out_of_range", field.name)
        if same_shape and stats[2] == substituted and field.is_default(value) and not getattr(field, "required", False):
            raise Refused("wire_damaged", "message field is explicitly at its default")
        setattr(obj, field.name, value)


def _encode_messages(root: type[object], values: list[object], vocab: MessageVocabulary, keys: dict[tuple[str, str], bytes], roots: frozenset[str], measure: bool, fast: Callable[[BitWriter, Any], None] | None = None) -> BitWriter:
    from . import InvalidValue
    from .descriptor import by_name
    from .save import _validate
    if not 1 <= len(values) <= 256:
        raise InvalidValue("", "batch_too_large")
    root_name = getattr(getattr(root, "descriptor", None), "name", "")
    if not vocab.announced or root_name not in roots or by_name(root_name) is not root:
        raise InvalidValue("", "message root or vocabulary is invalid")
    for value in values:
        if type(value) is not root:
            raise InvalidValue("", "batch contains another root")
        _validate(value, getattr(value, "descriptor"), "", set())
    writer = BitWriter(measure)
    writer.put(2, 8)
    writer.put(len(values) - 1, 8)
    for value in values:
        if fast is None:
            _write_body(writer, value, vocab, keys)
        else:
            fast(writer, value)
    writer.align()
    return writer


def save_messages(root: type[object], values: list[object], vocab: MessageVocabulary, keys: dict[tuple[str, str], bytes], roots: frozenset[str], fast: Callable[[BitWriter, Any], None] | None = None) -> bytes:
    return bytes(_encode_messages(root, values, vocab, keys, roots, False, fast).data)


def measure_messages(root: type[object], values: list[object], vocab: MessageVocabulary, keys: dict[tuple[str, str], bytes], roots: frozenset[str], fast: Callable[[BitWriter, Any], None] | None = None) -> int:
    return _encode_messages(root, values, vocab, keys, roots, True, fast).position // 8


def load_messages(root: type[object], vocab: MessageVocabulary, data: bytes | bytearray | memoryview, own_version: int, keys: dict[tuple[str, str], bytes], roots: frozenset[str], capacity: int = 256, report: "LoadReport | None" = None) -> list[object]:
    from .descriptor import by_name
    from .load import buffer_view

    root_name = getattr(getattr(root, "descriptor", None), "name", "")
    if root_name not in roots or by_name(root_name) is not root:
        raise Refused("invalid_root")
    view = buffer_view(data)
    if not view:
        raise Refused("wire_damaged", "empty batch")
    if view[0] != 2:
        raise Refused("newer_form")
    if not vocab.announced:
        raise Refused("no_vocabulary")
    if len(view) < 2:
        raise Refused("wire_damaged", "missing batch count")
    count = view[1] + 1
    if count > capacity:
        raise Refused("batch_too_large")
    scan = BitReader(view, 16)
    pending: list[str] = []
    for _ in range(count):
        _scan_body(scan, vocab, 0, pending)
    scan.finish()
    if pending:
        raise Refused(pending[0])
    reader = BitReader(view, 16)
    # Unknown/widened are public counters; substitutions are internal evidence
    # that the decoded default was not what the sender actually wrote.
    stats = [0, 0, 0]
    values = [_read_body(reader, root, vocab, own_version, keys, stats) for _ in range(count)]
    reader.finish()
    if report is not None:
        report.unknown = stats[0]
        report.widened = stats[1]
        report.retained = 0
        report.retain_lost = 0
    return values


def _name_slot(vocab: MessageVocabulary, field_id: int) -> int:
    slot = vocab.names.get(field_id)
    if slot is None:
        raise ValueError("message vocabulary is missing a name")
    return slot


def _write_value(writer: BitWriter, field: object, value: object, shape: Shape, vocab: MessageVocabulary, keys: dict[tuple[str, str], bytes]) -> None:
    from typing import Any
    from . import InvalidValue
    fd: Any = field
    v: Any = value
    if shape.kind == 13:
        _write_body(writer, v, vocab, keys)
    elif shape.kind == 12:
        raw = v.encode("utf-8")
        if len(raw) > shape.maximum or b"\0" in raw:
            raise InvalidValue(fd.name, "message text is invalid")
        writer.put(len(raw), shape.maximum.bit_length())
        writer.align()
        writer.raw(raw)
    elif shape.kind == 14:
        if not shape.minimum <= len(v) <= shape.maximum or shape.inner is None:
            raise InvalidValue(fd.name, "message array count exceeds its bound")
        writer.put(len(v) - shape.minimum, (shape.maximum - shape.minimum).bit_length())
        if shape.element == 6:
            writer.align()
        if fd.codec == "bytes" and shape.element == 6 and shape.inner.packing == 0:
            writer.raw(bytes(v))
            return
        element = fd.elem if fd.elem is not None else fd.element if fd.element is not None else fd
        for item in v:
            _write_value(writer, element, item, shape.inner, vocab, keys)
    elif shape.kind == 30:
        slot = 0 if v == 0 else _name_slot(vocab, fd.variant_ids[v - 1])
        writer.put(slot, vocab.ref_bits)
    elif shape.kind == 15:
        if v.type == 0:
            writer.put(0, vocab.ref_bits)
            return
        arm = v.descriptor.arms[v.type - 1]
        key = keys.get((v.descriptor.name, arm.name))
        arm_slot = vocab.slots.get(key) if key is not None else None
        if arm_slot is None:
            raise ValueError("message union arm is absent from its vocabulary")
        writer.put(arm_slot, vocab.ref_bits)
        if arm.field is not None:
            _write_value(writer, arm.field, getattr(v, arm.name), vocab.entries[arm_slot - 1].shape, vocab, keys)
    elif shape.kind == 16:
        if fd.elem is None or shape.inner is None:
            raise InvalidValue(fd.name, "message keyed element is missing")
        values = v if fd.keyed_list else v._values
        present = [(key, value) for key, value in zip(fd.keys, values) if not fd.elem.is_default(value)]
        if len(present) > shape.maximum:
            raise InvalidValue(fd.name, "message keyed count exceeds its bound")
        writer.put(len(present), shape.maximum.bit_length())
        for key, item in present:
            writer.put(_name_slot(vocab, key), vocab.ref_bits)
            _write_value(writer, fd.elem, item, shape.inner, vocab, keys)
    else:
        _write_scalar(writer, shape, v)


def _read_value(reader: BitReader, field: object, shape: Shape, vocab: MessageVocabulary, own_version: int, keys: dict[tuple[str, str], bytes], stats: list[int], depth: int, in_field: bool = False, existing: object = None, same_shape: bool = False) -> object:
    from typing import Any, cast
    from . import descriptor
    fd: Any = field
    if shape.kind == 13:
        cls = descriptor.by_name(fd.target)
        if cls is None:
            raise Refused("kind_mismatch", "message table target is not registered")
        return _read_body(reader, cls, vocab, own_version, keys, stats, depth + 1)
    if shape.kind == 12:
        length = reader.get(shape.maximum.bit_length())
        if length > shape.maximum:
            raise Refused("wire_damaged", "message text exceeds announced bound")
        if length > fd.size:
            raise Refused("length_over_bound", fd.name)
        reader.align()
        raw = reader.raw(length)
        if b"\0" in raw:
            raise Refused("wire_damaged", "message text contains NUL")
        try:
            return raw.decode("utf-8")
        except UnicodeDecodeError:
            raise Refused("wire_damaged", "message text is not UTF-8") from None
    if shape.kind == 14:
        count = shape.minimum + reader.get((shape.maximum - shape.minimum).bit_length())
        if count > shape.maximum:
            raise Refused("wire_damaged", "message count exceeds announced bound")
        bound = fd.size if fd.codec == "bytes" else fd.bound
        if count > bound:
            raise Refused("length_over_bound" if fd.codec == "bytes" else "count_over_bound", fd.name)
        if shape.element == 6:
            reader.align()
        if shape.inner is None:
            raise Refused("wire_damaged", "message element shape is absent")
        if fd.codec == "bytes" and shape.element == 6 and shape.inner.packing == 0:
            return reader.raw(count)
        element = fd.elem if fd.elem is not None else fd.element if fd.element is not None else fd
        if element is not fd and shape.element != element.kind:
            if not _widens(shape.element, element.kind):
                raise Refused("kind_mismatch", "message array element kind changed")
            stats[1] += 1
        if fd.array_kind == 2 and count < fd.array_min:
            raise Refused("count_over_bound", fd.name)
        values = [_read_value(reader, element, shape.inner, vocab, own_version, keys, stats, depth + 1) for _ in range(count)]
        if fd.array_kind == 1 and count < fd.bound:
            for _ in range(fd.bound - count):
                values.append(descriptor.new_by_name(element.target) if element.codec == "table" else element.default)
        return bytes(cast(list[int], values)) if fd.codec == "bytes" else values
    if shape.kind == 30:
        entry = _reference(reader, vocab)
        if entry is None:
            return 0
        if entry.kind != 0:
            raise Refused("wire_damaged", "message enum name has a payload")
        ordinal = vocab.variants.get(id(fd), {}).get(entry.field_id)
        if ordinal is not None:
            if ordinal in fd.deprecated:
                stats[2] += 1
                return fd.default
            return ordinal
        stats[0] += 1
        stats[2] += 1
        return 0
    if shape.kind == 15:
        cls = descriptor._union_by_name(fd.target)
        if cls is None:
            raise Refused("kind_mismatch", "message union target is not registered")
        result: Any = cls()
        entry = _reference(reader, vocab)
        if entry is None:
            if in_field:
                raise Refused("wire_damaged", "a union field explicitly carries None")
            return result
        if entry.kind == 0:
            raise Refused("wire_damaged", "message union arm has no framing")
        arm = result.descriptor.by_id.get(entry.field_id)
        if arm is None:
            _skip_value(reader, entry.shape, vocab, depth + 1)
            stats[0] += 1
            stats[2] += 1
            return result
        if entry.kind != arm.kind:
            if not _widens(entry.kind, arm.kind):
                raise Refused("kind_mismatch", "message union arm kind changed")
            stats[1] += 1
        result.type = arm.tag
        if arm.field is not None:
            setattr(result, arm.name, _read_value(reader, arm.field, entry.shape, vocab, own_version, keys, stats, depth + 1))
        return result
    if shape.kind == 16:
        if shape.inner is None or fd.elem is None or existing is None:
            raise Refused("wire_damaged", "message keyed element is missing")
        if shape.element != fd.elem.kind:
            raise Refused("kind_mismatch", "message keyed element kind changed")
        count = reader.get(shape.maximum.bit_length())
        if count > shape.maximum:
            raise Refused("wire_damaged", "message keyed count exceeds its bound")
        output: Any = existing
        slots = output if fd.keyed_list else output._values
        seen: set[int] = set()
        last_index = -1
        for _ in range(count):
            key = _reference(reader, vocab)
            if key is None or key.kind != 0 or key.field_id in seen:
                raise Refused("wire_damaged", "invalid or repeated message key")
            seen.add(key.field_id)
            if key.field_id not in fd.keys:
                _skip_value(reader, shape.inner, vocab, depth + 1)
                stats[0] += 1
                stats[2] += 1
                continue
            index = fd.keys.index(key.field_id)
            if same_shape and index <= last_index:
                raise Refused("wire_damaged", "message keyed slots are out of order")
            last_index = index
            substituted = stats[2]
            item = _read_value(reader, fd.elem, shape.inner, vocab, own_version, keys, stats, depth + 1)
            if same_shape and stats[2] == substituted and fd.elem.is_default(item):
                raise Refused("wire_damaged", "message keyed slot is explicitly at its default")
            slots[index] = item
        return output
    value = _read_scalar(reader, shape)
    if type(value) is int and fd.codec != "bytes" and (value < fd.lo or value > fd.hi):
        raise Refused("out_of_range", fd.name)
    if type(value) is float and getattr(fd, "float_range", False):
        if value < fd.fmin or value > fd.fmax:
            raise Refused("out_of_range", fd.name)
    return value


def _widens(source: int, target: int) -> bool:
    if target in (3, 4, 5):
        return 2 <= source < target
    if target == 18:
        return 2 <= source <= 5
    if target in (7, 8, 9):
        return 6 <= source < target
    if target == 19:
        return 6 <= source <= 9
    return source == 10 and target == 11


def resolve_vocabulary(vocab: MessageVocabulary, local: MessageVocabulary, records: dict[str, type[Any]]) -> None:
    """Refuse known incompatible kinds before the first body is decoded."""
    by_id: dict[int, set[tuple[int, int]]] = {}
    for entry in local.entries:
        by_id.setdefault(entry.field_id, set()).add((entry.kind, entry.shape.element))
    for entry in vocab.entries:
        known = by_id.get(entry.field_id)
        if known is None:
            continue
        def compatible(candidate: tuple[int, int]) -> bool:
            kind, element = candidate
            if entry.kind != kind:
                return _widens(entry.kind, kind)
            if entry.kind in (14, 16) and entry.shape.element != element:
                return entry.kind == 14 and _widens(entry.shape.element, element)
            return True
        if not any(compatible(candidate) for candidate in known):
            vocab.entries = []
            vocab.slots.clear()
            vocab.names.clear()
            vocab.resolved.clear()
            vocab.variants.clear()
            vocab.announced = False
            vocab.refused = True
            raise Refused("kind_mismatch", "announcement field has an incompatible kind")
    from .descriptor import _union_by_name

    resolved: dict[tuple[str, int], Any] = {}
    variants: dict[int, dict[int, int]] = {}
    visited: set[int] = set()

    def collect(field: Any) -> None:
        if id(field) in visited:
            return
        visited.add(id(field))
        if field.variant_ids:
            variants[id(field)] = {value: index + 1 for index, value in enumerate(field.variant_ids)}
        for child in (field.elem, field.element):
            if child is not None:
                collect(child)
        union = _union_by_name(field.target) if field.target else None
        if union is not None:
            for arm in union.descriptor.arms:
                if arm.field is not None:
                    collect(arm.field)

    for name, cls in records.items():
        for field in cls.descriptor.fields:
            resolved[(name, field.field_id)] = field
            collect(field)
    vocab.resolved = resolved
    vocab.variants = variants


def _skip_value(reader: BitReader, shape: Shape, vocab: MessageVocabulary, depth: int, in_field: bool = False, pending: list[str] | None = None) -> None:
    import math
    import struct

    if depth > 128:
        raise Refused("wire_damaged", "message nesting exceeds 128")
    kind = shape.kind
    if kind in (0, 32):
        return
    if kind == 1 or 2 <= kind <= 9 or 18 <= kind <= 29 or kind == 36:
        value = _read_scalar(reader, shape)
        if type(value) is int:
            width = 64 if kind == 36 else _kind_bits(kind)
            signed = 2 <= kind <= 5 or kind == 18 or 20 <= kind <= 24 or kind == 36
            low = -(1 << (width - 1)) if signed else 0
            high = (1 << (width - int(signed))) - 1
            if not low <= value <= high:
                if pending is None:
                    raise Refused("out_of_range", "message integer exceeds its storage domain")
                pending.append("out_of_range")
        return
    if kind in (10, 11):
        if shape.packing == 2:
            assert shape.quant is not None
            qmin, qmax, qres = shape.quant
            delta = struct.unpack("<f", struct.pack("<f", qmax - qmin))[0]
            ratio = struct.unpack("<f", struct.pack("<f", delta / qres))[0]
            count = math.ceil(min(max(ratio, 1.0), 4294967040.0))
            if reader.get(shape.bits) > count:
                raise Refused("wire_damaged", "compressed float index exceeds count")
        else:
            width = 32 if kind == 10 else 64
            raw = reader.get(width).to_bytes(width // 8, "little")
            value = struct.unpack("<f" if kind == 10 else "<d", raw)[0]
            if not math.isfinite(value):
                if pending is None:
                    raise Refused("non_finite_float")
                pending.append("non_finite_float")
        return
    if kind == 12:
        length = reader.get(shape.maximum.bit_length())
        if length > shape.maximum:
            raise Refused("wire_damaged", "text length exceeds announcement")
        reader.align()
        raw = reader.raw(length)
        try:
            if b"\0" in raw:
                raise UnicodeError("NUL")
            raw.decode("utf-8")
        except UnicodeError:
            raise Refused("wire_damaged", "invalid message text") from None
        return
    if kind == 13:
        seen: set[int] = set()
        while True:
            entry = _reference(reader, vocab)
            if entry is None:
                return
            if entry.field_id in seen:
                raise Refused("wire_damaged", "nested message repeats a field")
            seen.add(entry.field_id)
            _skip_value(reader, entry.shape, vocab, depth + 1, True, pending)
    if kind in (15, 30):
        entry = _reference(reader, vocab)
        if entry is None:
            if kind == 15 and in_field:
                raise Refused("wire_damaged", "a union field explicitly carries None")
            return
        if (kind == 30 and entry.kind != 0) or (kind == 15 and entry.kind == 0):
            raise Refused("wire_damaged", "message name has the wrong framing")
        if kind == 15:
            _skip_value(reader, entry.shape, vocab, depth + 1, False, pending)
        return
    if kind in (14, 16):
        minimum = shape.minimum if kind == 14 else 0
        count = minimum + reader.get((shape.maximum - minimum).bit_length())
        if count > shape.maximum or shape.inner is None:
            raise Refused("wire_damaged", "invalid message array count")
        if kind == 14 and shape.element == 6:
            reader.align()
        inner = shape.inner
        if kind == 14 and inner.kind in (0, 32):
            return
        if kind == 14 and (inner.kind == 1 or inner.kind == 36 or (2 <= inner.kind <= 9 or 18 <= inner.kind <= 29) and inner.packing == 0):
            width = 1 if inner.kind == 1 else 64 if inner.kind == 36 else _kind_bits(inner.kind)
            bits = count * width
            if bits > len(reader.data) * 8 - reader.position:
                raise Refused("wire_damaged", "message array exceeds the remaining bits")
            reader.position += bits
            return
        if kind == 14 and inner.packing == 1 and inner.bits == 0:
            _skip_value(reader, inner, vocab, depth + 1, False, pending)
            return
        seen_keys: set[int] = set()
        for _ in range(count):
            start = reader.position
            if kind == 16:
                key = _reference(reader, vocab)
                if key is None or key.kind != 0 or key.field_id in seen_keys:
                    raise Refused("wire_damaged", "invalid or repeated message key")
                seen_keys.add(key.field_id)
            _skip_value(reader, inner, vocab, depth + 1, False, pending)
            if reader.position == start:
                raise Refused("wire_damaged", "message element consumes no bits")
        return
    if kind == 31:
        reader.align()
        length = reader.get(32)
        if length > (len(reader.data) * 8 - reader.position) // 8:
            raise Refused("wire_damaged", "escape exceeds message batch")
        reader.position += length * 8
        return
    raise Refused("wire_damaged", "invalid message body kind")


def _f32(value: float) -> float:
    import struct
    return float(struct.unpack("<f", struct.pack("<f", value))[0])


def _scan_body(reader: BitReader, vocab: MessageVocabulary, depth: int, pending: list[str]) -> None:
    if depth > 128:
        raise Refused("wire_damaged", "message nesting exceeds 128")
    seen: set[int] = set()
    while True:
        entry = _reference(reader, vocab)
        if entry is None:
            return
        if entry.field_id in seen:
            raise Refused("wire_damaged", "message field repeated")
        seen.add(entry.field_id)
        _skip_value(reader, entry.shape, vocab, depth, True, pending)
