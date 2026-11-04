"""The streams, the refusal list and the helpers of serialize.py.

The wire is STANDARD.md, format version 2.0. Every name here that the package
exports is listed in serialize/__init__.py; the rest is internal.

The shape of the API is the family's: one serialize function per message,
written once against the Stream surface and run against a WriteStream, a
ReadStream or a MeasureStream. A scalar travels in a Ref, so the same call
stores through it on read and takes the value from it on write.

Two mechanisms, never mixed (IMPLEMENTING.md U7, U9):

* A READ refusal is a value. Every read returns False and latches the first
  refusal's reason, which ReadStream.refusal() reports from the closed list in
  Refusal. A reader never raises on any input, and caller misuse on read (a
  bad declaration, a buffer size below 1) refuses as value_out_of_range.
* WRITE and MEASURE misuse raises WriteMisuse, a ValueError, at the call and
  before any bit of the operation is written. Python has no unchecked build, so
  this port is a checked build in every run. Misuse never latches: a write or a
  measure stream has no refusal state.

The bit accumulator of a writer is a Python int of unbounded width: values are
ORed in above the bits already held, and each whole 64-bit word below is moved
to an append-only bytearray. Nothing is a fixed word buffer, a ring or a
reused slot, so the layout carries no index arithmetic beyond that append.
"""

from __future__ import annotations

import math
import struct
from typing import Callable, Final, Generic, TypeGuard, TypeVar

T = TypeVar("T")

FORMAT_VERSION: Final = "2.0"


class Ref(Generic[T]):
    """A holder a serialize call stores through on read and reads from on write.

    A language necessity (IMPLEMENTING.md U15): a Python scalar cannot be
    passed by reference, and the unified serialize function needs one.
    """

    __slots__ = ("value",)

    def __init__(self, value: T) -> None:
        self.value = value

    def __repr__(self) -> str:
        return f"Ref({self.value!r})"


class Refusal:
    """The closed list of refusal reasons (STANDARD.md, "Refusal reasons")."""

    PAST_END: Final = "past_end"
    VALUE_OUT_OF_RANGE: Final = "value_out_of_range"
    NONZERO_PADDING: Final = "nonzero_padding"
    TRAILING_DATA: Final = "trailing_data"
    NON_FINITE_FLOAT: Final = "non_finite_float"
    INVALID_UTF8: Final = "invalid_utf8"
    EMBEDDED_NUL: Final = "embedded_nul"
    NON_CANONICAL: Final = "non_canonical"
    REJECTED: Final = "rejected"


PAST_END: Final = Refusal.PAST_END
VALUE_OUT_OF_RANGE: Final = Refusal.VALUE_OUT_OF_RANGE
NONZERO_PADDING: Final = Refusal.NONZERO_PADDING
TRAILING_DATA: Final = Refusal.TRAILING_DATA
NON_FINITE_FLOAT: Final = Refusal.NON_FINITE_FLOAT
INVALID_UTF8: Final = Refusal.INVALID_UTF8
EMBEDDED_NUL: Final = Refusal.EMBEDDED_NUL
NON_CANONICAL: Final = Refusal.NON_CANONICAL
REJECTED: Final = Refusal.REJECTED

REASONS: Final = frozenset(
    (
        PAST_END,
        VALUE_OUT_OF_RANGE,
        NONZERO_PADDING,
        TRAILING_DATA,
        NON_FINITE_FLOAT,
        INVALID_UTF8,
        EMBEDDED_NUL,
        NON_CANONICAL,
        REJECTED,
    )
)


def refusal_name(reason: str | None) -> str:
    """The family spelling of a refusal (IMPLEMENTING.md U14).

    "none" for no refusal, the reason itself for one of the closed list, and
    "" for anything else, which names no reason.
    """
    if reason is None:
        return "none"
    return reason if reason in REASONS else ""


class WriteMisuse(ValueError):
    """Write or measure misuse (IMPLEMENTING.md U9): this port's trap.

    Raised at the call, before any bit of the operation is written, for a value
    outside its range or bit width, a non-finite float, text that is not
    well-formed UTF-8, holds a NUL or does not fit its buffer, a bad
    declaration, a write buffer whose size is not a multiple of 8 bytes, an
    operation that does not fit the room left, and reject() on a writer or a
    measure. Never a refusal: a write or a measure stream latches nothing.
    """


# ---------------------------------------------------------------------------
# numbers
# ---------------------------------------------------------------------------

INT32_MIN: Final = -(1 << 31)
INT32_MAX: Final = (1 << 31) - 1
INT64_MIN: Final = -(1 << 63)
INT64_MAX: Final = (1 << 63) - 1
INT128_MIN: Final = -(1 << 127)
INT128_MAX: Final = (1 << 127) - 1
UINT128_LIMIT: Final = 1 << 128
M64: Final = (1 << 64) - 1

# int_relative (STANDARD.md, "int_relative"): the domain and the tiers.
RELATIVE_MAX: Final = INT32_MAX
LAST_BOUNDED_MAX: Final = 69914
# (flag count, low, high) for the bounded tiers, in ladder order
_TIERS: Final = ((2, 2, 6), (3, 7, 23), (4, 24, 280), (5, 281, 4377), (6, 4378, 69914))

# the largest float32 below 2^32 (STANDARD.md, "compressed_float")
COMPRESSED_FLOAT_VALUES_MAX: Final = 4294967040.0

_F32: Final = struct.Struct("<f")
_U32: Final = struct.Struct("<I")
_F64: Final = struct.Struct("<d")
_U64: Final = struct.Struct("<Q")


def _is_int(x: object) -> TypeGuard[int]:
    return isinstance(x, int) and not isinstance(x, bool)


def bits_required(min: int, max: int) -> int:
    """The bits a ranged field over [min, max] costs: 0 for min == max.

    The bit length of max - min, which is the family's
    32 - count_leading_zeros(max - min) at every width. min <= max.
    """
    return (max - min).bit_length()


def float32_round(value: float) -> float:
    """value rounded to float32, round to nearest, ties to even.

    A language necessity (IMPLEMENTING.md U15): Python has no float32 type.
    A value whose rounding overflows float32 returns the infinity of its sign.
    Every float32 operation of compressed_float is the float64 operation then
    this rounding, which is exact: a float64 result rounded to float32 is the
    correctly rounded float32 result for +, -, * and / of float32 operands,
    because float64 carries more than 2 * 24 + 2 bits.
    """
    try:
        return float(_F32.unpack(_F32.pack(value))[0])
    except OverflowError:
        return math.copysign(math.inf, value)


def float32_bits(value: float) -> int:
    """The 32 bits of value as a float32 (after float32_round)."""
    bits: int = _U32.unpack(_F32.pack(float32_round(value)))[0]
    return bits


def float32_from_bits(bits: int) -> float:
    """The float whose float32 pattern is bits. Exact for every finite pattern."""
    return float(_F32.unpack(_U32.pack(bits))[0])


def float64_bits(value: float) -> int:
    """The 64 bits of value as a float64."""
    bits: int = _U64.unpack(_F64.pack(value))[0]
    return bits


def float64_from_bits(bits: int) -> float:
    """The float whose float64 pattern is bits."""
    return float(_F64.unpack(_U64.pack(bits))[0])


def float_bits_are_finite(bits: int) -> bool:
    """A float32 pattern whose exponent field is not all ones."""
    return (bits & 0x7F800000) != 0x7F800000


def double_bits_are_finite(bits: int) -> bool:
    """A float64 pattern whose exponent field is not all ones."""
    return (bits & 0x7FF0000000000000) != 0x7FF0000000000000


# ---------------------------------------------------------------------------
# declarations: each returns None for one that is not a declaration of the
# format, so a reader can refuse it as a value and a writer can raise
# ---------------------------------------------------------------------------


def _int_bounds(min: object, max: object, lo: int, hi: int) -> bool:
    return _is_int(min) and _is_int(max) and lo <= min <= max <= hi


def compressed_float_params(min: float, max: float, res: float) -> tuple[int, int, float] | None:
    """(max_integer_value, bits, delta) for a compressed_float declaration.

    min, max and res round to float32 first, as the float parameter type of
    the family does. None for a declaration with no wire meaning: min not
    below max, res not positive, or delta or delta / res not finite in
    float32.
    """
    if not (isinstance(min, (int, float)) and isinstance(max, (int, float)) and isinstance(res, (int, float))):
        return None
    lo = float32_round(min)
    hi = float32_round(max)
    r = float32_round(res)
    if not (lo < hi) or not (r > 0.0):
        return None
    delta = float32_round(hi - lo)
    values = float32_round(delta / r)
    if not math.isfinite(delta) or not math.isfinite(values):
        return None
    if not (values >= 1.0):
        values = 1.0
    elif values > COMPRESSED_FLOAT_VALUES_MAX:
        values = COMPRESSED_FLOAT_VALUES_MAX
    max_integer_value = math.ceil(values)
    return max_integer_value, max_integer_value.bit_length(), delta


def _precomputed_ok(max_integer_value: object, bits: object, delta: object, min: object) -> bool:
    if not (_is_int(max_integer_value) and _is_int(bits)):
        return False
    if not (isinstance(delta, (int, float)) and isinstance(min, (int, float))):
        return False
    if not 1 <= max_integer_value <= 4294967040:
        return False
    if bits != max_integer_value.bit_length():
        return False
    d = float32_round(delta)
    return d > 0.0 and math.isfinite(d) and math.isfinite(float32_round(min))


def _fixed_params(integer_bits: object, fraction_bits: object, min: object, max: object) -> tuple[int, int, int] | None:
    """(bits, raw_min, raw_span) for a fixed point declaration, or None.

    The storage width integer_bits + fraction_bits is 8, 16, 32, 64 or 128,
    the sign bit counting toward integer_bits; min and max are whole units with
    min <= max, within int64 for storage of 64 and 128 bits, and within the Q
    format's whole-unit capacity, read as signed exactly when min < 0.
    """
    if not (_is_int(integer_bits) and _is_int(fraction_bits) and _is_int(min) and _is_int(max)):
        return None
    ib, fb, lo, hi = integer_bits, fraction_bits, min, max
    if ib < 1 or fb < 0 or ib + fb not in (8, 16, 32, 64, 128):
        return None
    if lo > hi:
        return None
    if ib + fb >= 64 and not (INT64_MIN <= lo and hi <= INT64_MAX):
        return None
    if lo < 0:
        if lo < -(1 << (ib - 1)) or hi > (1 << (ib - 1)) - 1:
            return None
    elif hi > (1 << ib) - 1:
        return None
    raw_min = lo << fb
    raw_span = (hi - lo) << fb
    return raw_span.bit_length(), raw_min, raw_span


def _relative_tier(difference: int) -> int:
    """The index into _TIERS of the bounded tier a difference takes, -1 for the
    one-bit tier, and len(_TIERS) for the absolute tier."""
    if difference == 1:
        return -1
    for i, (_flags, lo, hi) in enumerate(_TIERS):
        if lo <= difference <= hi:
            return i
    return len(_TIERS)


def copy_string(source: str, dest_size: int) -> str:
    """Bounds text for serialize_string with buffer_size = dest_size.

    IMPLEMENTING.md U15: the UTF-8 bytes before the first NUL; past
    dest_size - 1 of them, the first dest_size - 1, then bytes dropped from
    the end while the first byte left out is a continuation byte. Returns that
    prefix as a str. dest_size below 1, and a source the strict codec cannot
    encode (a lone surrogate), are write misuse.
    """
    if not _is_int(dest_size) or dest_size < 1:
        raise WriteMisuse("copy_string: dest_size must be an integer of at least 1")
    if not isinstance(source, str):
        raise WriteMisuse("copy_string: source must be a str")
    try:
        data = source.encode("utf-8")
    except UnicodeEncodeError as e:
        raise WriteMisuse("copy_string: source is not encodable as UTF-8 (a lone surrogate)") from e
    nul = data.find(b"\x00")
    if nul >= 0:
        data = data[:nul]
    limit = dest_size - 1
    if len(data) > limit:
        n = limit
        while n > 0 and (data[n] & 0xC0) == 0x80:
            n -= 1
        data = data[:n]
    return data.decode("utf-8")


# ---------------------------------------------------------------------------
# write and measure encodings: each validates the caller's value and
# declaration and returns what goes on the wire, or raises WriteMisuse
# ---------------------------------------------------------------------------


def _misuse(message: str) -> WriteMisuse:
    return WriteMisuse(message)


def _enc_bits(value: object, bits: object) -> int:
    if not _is_int(bits) or not 1 <= bits <= 64:
        raise _misuse(f"serialize_bits: bits must be an integer in [1, 64], got {bits!r}")
    if not _is_int(value) or not 0 <= value < (1 << bits):
        raise _misuse(f"serialize_bits: {value!r} does not fit {bits} bits")
    return value


def _enc_ranged(value: object, min: object, max: object, lo: int, hi: int, what: str) -> tuple[int, int]:
    if not (_is_int(min) and _is_int(max) and lo <= min <= max <= hi):
        raise _misuse(f"{what}: min and max must be integers with {lo} <= min <= max <= {hi}, got {min!r}, {max!r}")
    if not _is_int(value) or not min <= value <= max:
        raise _misuse(f"{what}: {value!r} is outside [{min}, {max}]")
    return value - min, bits_required(min, max)


def _enc_fixed(value: object, integer_bits: object, fraction_bits: object, min: object, max: object) -> tuple[int, int]:
    params = _fixed_params(integer_bits, fraction_bits, min, max)
    if params is None:
        raise _misuse(
            f"serialize_fixed: ({integer_bits!r}, {fraction_bits!r}, {min!r}, {max!r}) is not a fixed point declaration"
        )
    bits, raw_min, raw_span = params
    if not _is_int(value) or not raw_min <= value <= raw_min + raw_span:
        raise _misuse(f"serialize_fixed: raw value {value!r} is outside [{raw_min}, {raw_min + raw_span}]")
    return value - raw_min, bits


def _enc_relative(previous: object, current: object) -> tuple[int, int]:
    """(payload, bits) of an int_relative, flags included, LSB first."""
    if not _is_int(previous) or not _is_int(current):
        raise _misuse("serialize_int_relative: previous and current must be integers")
    if not 0 <= previous < current <= RELATIVE_MAX:
        raise _misuse(
            f"serialize_int_relative: need 0 <= previous < current <= {RELATIVE_MAX}, got {previous!r}, {current!r}"
        )
    difference: int = current - previous
    tier = _relative_tier(difference)
    if tier < 0:
        return 1, 1
    if tier == len(_TIERS):
        # six zero flags, then current as 32 raw bits
        return current << 6, 6 + 32
    flags, lo, hi = _TIERS[tier]
    # flags - 1 zero flags, then a one flag, then the payload
    return (1 << (flags - 1)) | ((difference - lo) << flags), flags + bits_required(lo, hi)


def _enc_float(value: object) -> int:
    if not isinstance(value, (int, float)) or isinstance(value, bool):
        raise _misuse(f"serialize_float: {value!r} is not a number")
    try:
        bits: int = _U32.unpack(_F32.pack(value))[0]
    except OverflowError:
        raise _misuse(f"serialize_float: {value!r} narrows to an infinity in float32") from None
    if not float_bits_are_finite(bits):
        raise _misuse(f"serialize_float: {value!r} is not finite")
    return bits


def _enc_double(value: object) -> int:
    if not isinstance(value, (int, float)) or isinstance(value, bool):
        raise _misuse(f"serialize_double: {value!r} is not a number")
    try:
        bits = float64_bits(float(value))
    except OverflowError:
        raise _misuse(f"serialize_double: {value!r} does not fit a float64") from None
    if not double_bits_are_finite(bits):
        raise _misuse(f"serialize_double: {value!r} is not finite")
    return bits


def _cf_declaration(min: float, max: float, res: float) -> tuple[int, int, float, float]:
    params = compressed_float_params(min, max, res)
    if params is None:
        raise _misuse(f"serialize_compressed_float: ({min!r}, {max!r}, {res!r}) is not a compressed float declaration")
    max_integer_value, bits, delta = params
    return max_integer_value, bits, delta, float32_round(min)


def _cf_precomputed(max_integer_value: int, bits: int, delta: float, min: float) -> tuple[float, float]:
    if not _precomputed_ok(max_integer_value, bits, delta, min):
        raise _misuse("serialize_compressed_float_precomputed: the constants are not a compressed float declaration's")
    return float32_round(delta), float32_round(min)


def _enc_compressed(value: object, max_integer_value: int, delta: float, min32: float) -> int:
    """The quantized integer: float32 arithmetic, two roundings, the clamp."""
    if not math.isfinite(float32_round(min32 + delta)):
        raise _misuse("serialize_compressed_float: the declaration's top, min + delta, is not finite in float32")
    if not isinstance(value, (int, float)) or isinstance(value, bool):
        raise _misuse(f"serialize_compressed_float: {value!r} is not a number")
    value32 = float32_round(value)
    if not math.isfinite(value32):
        raise _misuse(f"serialize_compressed_float: {value!r} is not finite")
    normalized = float32_round(float32_round(value32 - min32) / delta)
    if not (normalized >= 0.0):
        normalized = 0.0
    elif not (normalized <= 1.0):
        normalized = 1.0
    scaled = float32_round(normalized * float(max_integer_value))
    integer = math.floor(float32_round(scaled + 0.5))
    if integer > max_integer_value:
        integer = max_integer_value
    return integer


def _enc_bytes(value: object, count: object) -> bytes:
    if not _is_int(count) or count < 0:
        raise _misuse(f"serialize_bytes: count must be a non-negative integer, got {count!r}")
    if not isinstance(value, (bytes, bytearray, memoryview)):
        raise _misuse("serialize_bytes: the value must be bytes, a bytearray or a memoryview")
    data = bytes(value)
    if len(data) != count:
        raise _misuse(f"serialize_bytes: the value holds {len(data)} bytes, the count is {count}")
    return data


def _buffer_size_ok(buffer_size: object) -> bool:
    return _is_int(buffer_size) and 1 <= buffer_size <= INT32_MAX + 1


def _enc_string(value: object, buffer_size: object) -> bytes:
    if not (_is_int(buffer_size) and 1 <= buffer_size <= INT32_MAX + 1):
        raise _misuse(f"serialize_string: buffer_size must be an integer in [1, 2^31], got {buffer_size!r}")
    if not isinstance(value, str):
        raise _misuse("serialize_string: the value must be a str")
    if "\x00" in value:
        raise _misuse("serialize_string: the value holds a NUL")
    try:
        data = value.encode("utf-8")
    except UnicodeEncodeError:
        raise _misuse("serialize_string: the value is not encodable as UTF-8 (a lone surrogate)") from None
    if len(data) > buffer_size - 1:
        raise _misuse(f"serialize_string: {len(data)} bytes do not fit buffer_size {buffer_size}")
    return data


# ---------------------------------------------------------------------------
# the streams
# ---------------------------------------------------------------------------


class Stream:
    """The surface a serialize function is written against.

    WriteStream, ReadStream and MeasureStream implement every operation; a
    serialize function takes a Stream and returns the bool each call returns,
    so it reads, writes and measures with one body.
    """

    __slots__ = ()

    is_writing: bool = False
    is_reading: bool = False

    def serialize_bits(self, ref: Ref[int], bits: int) -> bool:
        raise NotImplementedError

    def serialize_uint8(self, ref: Ref[int]) -> bool:
        return self.serialize_bits(ref, 8)

    def serialize_uint16(self, ref: Ref[int]) -> bool:
        return self.serialize_bits(ref, 16)

    def serialize_uint32(self, ref: Ref[int]) -> bool:
        return self.serialize_bits(ref, 32)

    def serialize_uint64(self, ref: Ref[int]) -> bool:
        return self.serialize_bits(ref, 64)

    def serialize_bool(self, ref: Ref[bool]) -> bool:
        raise NotImplementedError

    def serialize_int(self, ref: Ref[int], min: int, max: int) -> bool:
        raise NotImplementedError

    def serialize_int64(self, ref: Ref[int], min: int, max: int) -> bool:
        raise NotImplementedError

    def serialize_int128(self, ref: Ref[int], min: int, max: int) -> bool:
        raise NotImplementedError

    def serialize_uint128(self, ref: Ref[int]) -> bool:
        raise NotImplementedError

    def serialize_fixed(self, ref: Ref[int], integer_bits: int, fraction_bits: int, min: int, max: int) -> bool:
        raise NotImplementedError

    def serialize_int_relative(self, previous: int, ref: Ref[int]) -> bool:
        raise NotImplementedError

    def serialize_float(self, ref: Ref[float]) -> bool:
        raise NotImplementedError

    def serialize_double(self, ref: Ref[float]) -> bool:
        raise NotImplementedError

    def serialize_compressed_float(self, ref: Ref[float], min: float, max: float, res: float) -> bool:
        raise NotImplementedError

    def serialize_compressed_float_precomputed(
        self, ref: Ref[float], max_integer_value: int, bits: int, delta: float, min: float
    ) -> bool:
        raise NotImplementedError

    def serialize_bytes(self, ref: Ref[bytes], count: int) -> bool:
        raise NotImplementedError

    def serialize_string(self, ref: Ref[str], buffer_size: int) -> bool:
        raise NotImplementedError

    def serialize_align(self) -> bool:
        raise NotImplementedError

    def serialize_end(self) -> bool:
        raise NotImplementedError

    def serialize_object(self, serialize: Callable[[Stream], bool]) -> bool:
        raise NotImplementedError

    def reject(self) -> bool:
        raise NotImplementedError

    def fail(self, reason: str | None = None) -> bool:
        raise NotImplementedError

    def bits_processed(self) -> int:
        raise NotImplementedError

    def bytes_processed(self) -> int:
        return (self.bits_processed() + 7) >> 3

    def align_bits(self) -> int:
        raise NotImplementedError


class WriteStream(Stream):
    """Writes bitpacked data into a buffer of capacity bytes.

    capacity is a multiple of 8 (IMPLEMENTING.md U1). An operation that does
    not fit the room left raises WriteMisuse before any bit of it is written
    (U9, U10). flush() is required before data() (U11).
    """

    __slots__ = ("_capacity_bits", "_out", "_acc", "_held", "_bits")

    is_writing = True
    is_reading = False

    def __init__(self, capacity: int) -> None:
        if not _is_int(capacity) or capacity < 0 or capacity % 8 != 0:
            raise WriteMisuse(f"WriteStream: capacity must be a non-negative multiple of 8 bytes, got {capacity!r}")
        self._capacity_bits = capacity * 8
        self._out = bytearray()
        self._acc = 0
        self._held = 0
        self._bits = 0

    def _room(self, n: int) -> None:
        if self._bits + n > self._capacity_bits:
            raise WriteMisuse(
                f"WriteStream: {n} bits do not fit the {self._capacity_bits - self._bits} bits left in the buffer"
            )

    def _put(self, value: int, n: int) -> None:
        acc = self._acc | (value << self._held)
        held = self._held + n
        if held >= 64:
            out = self._out
            while held >= 64:
                out += (acc & M64).to_bytes(8, "little")
                acc >>= 64
                held -= 64
        self._acc = acc
        self._held = held
        self._bits += n

    def _put_aligned_bytes(self, data: bytes) -> None:
        # the stream is aligned here, so the held bits are whole bytes
        if self._held:
            self._out += self._acc.to_bytes(self._held >> 3, "little")
            self._acc = 0
            self._held = 0
        self._out += data
        self._bits += 8 * len(data)

    def serialize_bits(self, ref: Ref[int], bits: int) -> bool:
        value = _enc_bits(ref.value, bits)
        self._room(bits)
        self._put(value, bits)
        return True

    def serialize_bool(self, ref: Ref[bool]) -> bool:
        value = ref.value
        if not isinstance(value, bool):
            raise WriteMisuse(f"serialize_bool: {value!r} is not a bool")
        self._room(1)
        self._put(1 if value else 0, 1)
        return True

    def serialize_int(self, ref: Ref[int], min: int, max: int) -> bool:
        offset, n = _enc_ranged(ref.value, min, max, INT32_MIN, INT32_MAX, "serialize_int")
        self._room(n)
        self._put(offset, n)
        return True

    def serialize_int64(self, ref: Ref[int], min: int, max: int) -> bool:
        offset, n = _enc_ranged(ref.value, min, max, INT64_MIN, INT64_MAX, "serialize_int64")
        self._room(n)
        self._put(offset, n)
        return True

    def serialize_int128(self, ref: Ref[int], min: int, max: int) -> bool:
        offset, n = _enc_ranged(ref.value, min, max, INT128_MIN, INT128_MAX, "serialize_int128")
        self._room(n)
        self._put(offset, n)
        return True

    def serialize_uint128(self, ref: Ref[int]) -> bool:
        value = ref.value
        if not _is_int(value) or not 0 <= value < UINT128_LIMIT:
            raise WriteMisuse(f"serialize_uint128: {value!r} does not fit 128 bits")
        self._room(128)
        self._put(value, 128)
        return True

    def serialize_fixed(self, ref: Ref[int], integer_bits: int, fraction_bits: int, min: int, max: int) -> bool:
        offset, n = _enc_fixed(ref.value, integer_bits, fraction_bits, min, max)
        self._room(n)
        self._put(offset, n)
        return True

    def serialize_int_relative(self, previous: int, ref: Ref[int]) -> bool:
        payload, n = _enc_relative(previous, ref.value)
        self._room(n)
        self._put(payload, n)
        return True

    def serialize_float(self, ref: Ref[float]) -> bool:
        bits = _enc_float(ref.value)
        self._room(32)
        self._put(bits, 32)
        return True

    def serialize_double(self, ref: Ref[float]) -> bool:
        bits = _enc_double(ref.value)
        self._room(64)
        self._put(bits, 64)
        return True

    def serialize_compressed_float(self, ref: Ref[float], min: float, max: float, res: float) -> bool:
        max_integer_value, bits, delta, min32 = _cf_declaration(min, max, res)
        integer = _enc_compressed(ref.value, max_integer_value, delta, min32)
        self._room(bits)
        self._put(integer, bits)
        return True

    def serialize_compressed_float_precomputed(
        self, ref: Ref[float], max_integer_value: int, bits: int, delta: float, min: float
    ) -> bool:
        delta32, min32 = _cf_precomputed(max_integer_value, bits, delta, min)
        integer = _enc_compressed(ref.value, max_integer_value, delta32, min32)
        self._room(bits)
        self._put(integer, bits)
        return True

    def serialize_bytes(self, ref: Ref[bytes], count: int) -> bool:
        data = _enc_bytes(ref.value, count)
        pad = (8 - (self._bits & 7)) & 7
        self._room(pad + 8 * count)
        self._put(0, pad)
        self._put_aligned_bytes(data)
        return True

    def serialize_string(self, ref: Ref[str], buffer_size: int) -> bool:
        data = _enc_string(ref.value, buffer_size)
        n = bits_required(0, buffer_size - 1)
        pad = (8 - ((self._bits + n) & 7)) & 7
        self._room(n + pad + 8 * len(data))
        self._put(len(data), n + pad)
        self._put_aligned_bytes(data)
        return True

    def serialize_align(self) -> bool:
        pad = (8 - (self._bits & 7)) & 7
        self._room(pad)
        self._put(0, pad)
        return True

    def serialize_end(self) -> bool:
        return True

    def serialize_object(self, serialize: Callable[[Stream], bool]) -> bool:
        return serialize(self) is True

    def reject(self) -> bool:
        raise WriteMisuse("reject: the caller's refusal is read only; a WriteStream has nothing to refuse")

    def fail(self, reason: str | None = None) -> bool:
        return False

    def flush(self) -> None:
        """Completes the stream (U11). Every writer calls it before data().

        The held bits reach data() either way, so it moves nothing; it is the
        family's completion point, and no write follows it.
        """

    def data(self) -> bytes:
        """The stream: bytes_processed() bytes, the final byte's unused bits zero."""
        tail = (self._held + 7) >> 3
        if tail:
            return bytes(self._out) + self._acc.to_bytes(tail, "little")
        return bytes(self._out)

    def bits_processed(self) -> int:
        return self._bits

    def bits_available(self) -> int:
        return self._capacity_bits - self._bits

    def align_bits(self) -> int:
        return (8 - (self._bits & 7)) & 7


class ReadStream(Stream):
    """Reads bitpacked data from bytes, never past their length.

    The buffer contract: none. The reader takes the stream's exact bytes and
    never looks past them. A refusal is a value: the read returns False,
    leaves its Ref unwritten, and latches the first reason, which refusal()
    reports. Every later read refuses, consumes nothing and writes nothing,
    zero-bit reads included. A ReadStream is not re-initialized (U13): make a
    new one for new bytes.
    """

    __slots__ = ("_data", "_nbits", "_bit", "_err")

    is_writing = False
    is_reading = True

    def __init__(self, data: bytes | bytearray | memoryview) -> None:
        self._err: str | None = None
        if isinstance(data, (bytes, bytearray, memoryview)):
            self._data = bytes(data)
        else:
            self._data = b""
            self._err = VALUE_OUT_OF_RANGE
        self._nbits = 8 * len(self._data) if self._err is None else -1
        self._bit = 0

    def _fail(self, reason: str) -> bool:
        # the latch (IMPLEMENTING.md, "Failure"): the first reason is kept, and
        # the readable length is poisoned, so the past-end check every read
        # already makes refuses every later read that would consume a bit
        if self._err is None:
            self._err = reason
        self._nbits = -1
        return False

    def _take(self, n: int) -> int:
        """n bits at the read position, which the caller has checked are there."""
        i = self._bit
        b = i >> 3
        s = i & 7
        v = int.from_bytes(self._data[b : b + ((s + n + 7) >> 3)], "little") >> s
        self._bit = i + n
        return v & ((1 << n) - 1)

    def _check_align(self) -> bool:
        pad = (8 - (self._bit & 7)) & 7
        if pad and self._take(pad) != 0:
            return self._fail(NONZERO_PADDING)
        return True

    def _read_int32(self, min: int, max: int) -> int | None:
        """A ranged int32 field, or None after a refusal. The latch is checked."""
        if not _int_bounds(min, max, INT32_MIN, INT32_MAX):
            self._fail(VALUE_OUT_OF_RANGE)
            return None
        span = max - min
        n = span.bit_length()
        if n == 0:
            return min
        if self._bit + n > self._nbits:
            self._fail(PAST_END)
            return None
        offset = self._take(n)
        if offset > span:
            self._fail(VALUE_OUT_OF_RANGE)
            return None
        return min + offset

    def serialize_bits(self, ref: Ref[int], bits: int) -> bool:
        if self._err is not None:
            return False
        if not _is_int(bits) or not 1 <= bits <= 64:
            return self._fail(VALUE_OUT_OF_RANGE)
        if self._bit + bits > self._nbits:
            return self._fail(PAST_END)
        ref.value = self._take(bits)
        return True

    def serialize_bool(self, ref: Ref[bool]) -> bool:
        if self._err is not None:
            return False
        if self._bit + 1 > self._nbits:
            return self._fail(PAST_END)
        ref.value = self._take(1) == 1
        return True

    def serialize_int(self, ref: Ref[int], min: int, max: int) -> bool:
        if self._err is not None:
            return False
        value = self._read_int32(min, max)
        if value is None:
            return False
        ref.value = value
        return True

    def serialize_int64(self, ref: Ref[int], min: int, max: int) -> bool:
        if self._err is not None:
            return False
        if not _int_bounds(min, max, INT64_MIN, INT64_MAX):
            return self._fail(VALUE_OUT_OF_RANGE)
        span = max - min
        n = span.bit_length()
        if n == 0:
            ref.value = min
            return True
        if self._bit + n > self._nbits:
            return self._fail(PAST_END)
        offset = self._take(n)
        if offset > span:
            return self._fail(VALUE_OUT_OF_RANGE)
        ref.value = min + offset
        return True

    def serialize_int128(self, ref: Ref[int], min: int, max: int) -> bool:
        if self._err is not None:
            return False
        if not _int_bounds(min, max, INT128_MIN, INT128_MAX):
            return self._fail(VALUE_OUT_OF_RANGE)
        span = max - min
        n = span.bit_length()
        if n == 0:
            ref.value = min
            return True
        if self._bit + n > self._nbits:
            return self._fail(PAST_END)
        offset = self._take(n)
        if offset > span:
            return self._fail(VALUE_OUT_OF_RANGE)
        ref.value = min + offset
        return True

    def serialize_uint128(self, ref: Ref[int]) -> bool:
        if self._err is not None:
            return False
        if self._bit + 128 > self._nbits:
            return self._fail(PAST_END)
        ref.value = self._take(128)
        return True

    def serialize_fixed(self, ref: Ref[int], integer_bits: int, fraction_bits: int, min: int, max: int) -> bool:
        if self._err is not None:
            return False
        params = _fixed_params(integer_bits, fraction_bits, min, max)
        if params is None:
            return self._fail(VALUE_OUT_OF_RANGE)
        n, raw_min, raw_span = params
        if n == 0:
            ref.value = raw_min
            return True
        if self._bit + n > self._nbits:
            return self._fail(PAST_END)
        offset = self._take(n)
        if offset > raw_span:
            return self._fail(VALUE_OUT_OF_RANGE)
        ref.value = raw_min + offset
        return True

    def serialize_int_relative(self, previous: int, ref: Ref[int]) -> bool:
        if self._err is not None:
            return False
        if not _is_int(previous) or not 0 <= previous <= RELATIVE_MAX:
            return self._fail(VALUE_OUT_OF_RANGE)
        # the one-bit tier
        if self._bit + 1 > self._nbits:
            return self._fail(PAST_END)
        if self._take(1):
            current = previous + 1
            if current > RELATIVE_MAX or current <= previous:
                return self._fail(VALUE_OUT_OF_RANGE)
            ref.value = current
            return True
        # the bounded tiers, one flag each
        for _flags, lo, hi in _TIERS:
            if self._bit + 1 > self._nbits:
                return self._fail(PAST_END)
            if self._take(1):
                n = bits_required(lo, hi)
                if self._bit + n > self._nbits:
                    return self._fail(PAST_END)
                offset = self._take(n)
                if offset > hi - lo:
                    return self._fail(VALUE_OUT_OF_RANGE)
                current = previous + lo + offset
                if current > RELATIVE_MAX or current <= previous:
                    return self._fail(VALUE_OUT_OF_RANGE)
                ref.value = current
                return True
        # the absolute tier: current as 32 raw bits
        if self._bit + 32 > self._nbits:
            return self._fail(PAST_END)
        v = self._take(32)
        if v > RELATIVE_MAX or v <= previous:
            return self._fail(VALUE_OUT_OF_RANGE)
        if v - previous <= LAST_BOUNDED_MAX:
            return self._fail(NON_CANONICAL)
        ref.value = v
        return True

    def serialize_float(self, ref: Ref[float]) -> bool:
        if self._err is not None:
            return False
        if self._bit + 32 > self._nbits:
            return self._fail(PAST_END)
        bits = self._take(32)
        if (bits & 0x7F800000) == 0x7F800000:
            return self._fail(NON_FINITE_FLOAT)
        ref.value = float(_F32.unpack(_U32.pack(bits))[0])
        return True

    def serialize_double(self, ref: Ref[float]) -> bool:
        if self._err is not None:
            return False
        if self._bit + 64 > self._nbits:
            return self._fail(PAST_END)
        bits = self._take(64)
        if (bits & 0x7FF0000000000000) == 0x7FF0000000000000:
            return self._fail(NON_FINITE_FLOAT)
        ref.value = float(_F64.unpack(_U64.pack(bits))[0])
        return True

    def _read_compressed(self, ref: Ref[float], max_integer_value: int, n: int, delta: float, min32: float) -> bool:
        if self._bit + n > self._nbits:
            return self._fail(PAST_END)
        integer = self._take(n)
        if integer > max_integer_value:
            return self._fail(VALUE_OUT_OF_RANGE)
        normalized = float32_round(float(integer) / float(max_integer_value))
        scaled = float32_round(normalized * delta)
        decoded = float32_round(scaled + min32)
        if not math.isfinite(decoded):
            return self._fail(NON_FINITE_FLOAT)
        ref.value = decoded
        return True

    def serialize_compressed_float(self, ref: Ref[float], min: float, max: float, res: float) -> bool:
        if self._err is not None:
            return False
        params = compressed_float_params(min, max, res)
        if params is None:
            return self._fail(VALUE_OUT_OF_RANGE)
        max_integer_value, n, delta = params
        return self._read_compressed(ref, max_integer_value, n, delta, float32_round(min))

    def serialize_compressed_float_precomputed(
        self, ref: Ref[float], max_integer_value: int, bits: int, delta: float, min: float
    ) -> bool:
        if self._err is not None:
            return False
        if not _precomputed_ok(max_integer_value, bits, delta, min):
            return self._fail(VALUE_OUT_OF_RANGE)
        return self._read_compressed(ref, max_integer_value, bits, float32_round(delta), float32_round(min))

    def serialize_bytes(self, ref: Ref[bytes], count: int) -> bool:
        if self._err is not None:
            return False
        if not _is_int(count) or count < 0:
            return self._fail(VALUE_OUT_OF_RANGE)
        if not self._check_align():
            return False
        if self._bit + 8 * count > self._nbits:
            return self._fail(PAST_END)
        b = self._bit >> 3
        self._bit += 8 * count
        ref.value = self._data[b : b + count]
        return True

    def serialize_string(self, ref: Ref[str], buffer_size: int) -> bool:
        if self._err is not None:
            return False
        if not _buffer_size_ok(buffer_size):
            return self._fail(VALUE_OUT_OF_RANGE)
        length = self._read_int32(0, buffer_size - 1)
        if length is None:
            return False
        if not self._check_align():
            return False
        if self._bit + 8 * length > self._nbits:
            return self._fail(PAST_END)
        b = self._bit >> 3
        self._bit += 8 * length
        payload = self._data[b : b + length]
        if b"\x00" in payload:
            return self._fail(EMBEDDED_NUL)
        try:
            text = payload.decode("utf-8")
        except UnicodeDecodeError:
            return self._fail(INVALID_UTF8)
        ref.value = text
        return True

    def serialize_align(self) -> bool:
        if self._err is not None:
            return False
        return self._check_align()

    def serialize_end(self) -> bool:
        if self._err is not None:
            return False
        remaining = self._nbits - self._bit
        if remaining >= 8:
            return self._fail(TRAILING_DATA)
        if remaining > 0 and self._take(remaining) != 0:
            return self._fail(NONZERO_PADDING)
        return True

    def serialize_object(self, serialize: Callable[[Stream], bool]) -> bool:
        if self._err is not None:
            return False
        if serialize(self) is True and self._err is None:
            return True
        return self._fail(REJECTED)

    def reject(self) -> bool:
        """The caller's refusal: latches rejected, keeping a first reason."""
        return self._fail(REJECTED)

    def fail(self, reason: str | None = None) -> bool:
        """Refuses with a reason of the caller's; outside the list it is rejected."""
        return self._fail(reason if reason in REASONS else REJECTED)

    def refusal(self) -> str | None:
        """The first refusal's reason, or None while the stream is healthy."""
        return self._err

    def bits_processed(self) -> int:
        return self._bit

    def bits_remaining(self) -> int:
        return max(0, self._nbits - self._bit)

    def align_bits(self) -> int:
        return (8 - (self._bit & 7)) & 7


class MeasureStream(Stream):
    """Counts the bits a message would take from any starting bit position.

    A bound, not the packet size: every alignment-performing operation (align,
    bytes, string) costs 7 bits. It makes every check a write makes except
    the room left, raises WriteMisuse the same way, and refuses nothing (U6).
    """

    __slots__ = ("_bits",)

    is_writing = True
    is_reading = False

    def __init__(self) -> None:
        self._bits = 0

    def serialize_bits(self, ref: Ref[int], bits: int) -> bool:
        _enc_bits(ref.value, bits)
        self._bits += bits
        return True

    def serialize_bool(self, ref: Ref[bool]) -> bool:
        if not isinstance(ref.value, bool):
            raise WriteMisuse(f"serialize_bool: {ref.value!r} is not a bool")
        self._bits += 1
        return True

    def serialize_int(self, ref: Ref[int], min: int, max: int) -> bool:
        self._bits += _enc_ranged(ref.value, min, max, INT32_MIN, INT32_MAX, "serialize_int")[1]
        return True

    def serialize_int64(self, ref: Ref[int], min: int, max: int) -> bool:
        self._bits += _enc_ranged(ref.value, min, max, INT64_MIN, INT64_MAX, "serialize_int64")[1]
        return True

    def serialize_int128(self, ref: Ref[int], min: int, max: int) -> bool:
        self._bits += _enc_ranged(ref.value, min, max, INT128_MIN, INT128_MAX, "serialize_int128")[1]
        return True

    def serialize_uint128(self, ref: Ref[int]) -> bool:
        value = ref.value
        if not _is_int(value) or not 0 <= value < UINT128_LIMIT:
            raise WriteMisuse(f"serialize_uint128: {value!r} does not fit 128 bits")
        self._bits += 128
        return True

    def serialize_fixed(self, ref: Ref[int], integer_bits: int, fraction_bits: int, min: int, max: int) -> bool:
        self._bits += _enc_fixed(ref.value, integer_bits, fraction_bits, min, max)[1]
        return True

    def serialize_int_relative(self, previous: int, ref: Ref[int]) -> bool:
        self._bits += _enc_relative(previous, ref.value)[1]
        return True

    def serialize_float(self, ref: Ref[float]) -> bool:
        _enc_float(ref.value)
        self._bits += 32
        return True

    def serialize_double(self, ref: Ref[float]) -> bool:
        _enc_double(ref.value)
        self._bits += 64
        return True

    def serialize_compressed_float(self, ref: Ref[float], min: float, max: float, res: float) -> bool:
        max_integer_value, bits, delta, min32 = _cf_declaration(min, max, res)
        _enc_compressed(ref.value, max_integer_value, delta, min32)
        self._bits += bits
        return True

    def serialize_compressed_float_precomputed(
        self, ref: Ref[float], max_integer_value: int, bits: int, delta: float, min: float
    ) -> bool:
        delta32, min32 = _cf_precomputed(max_integer_value, bits, delta, min)
        _enc_compressed(ref.value, max_integer_value, delta32, min32)
        self._bits += bits
        return True

    def serialize_bytes(self, ref: Ref[bytes], count: int) -> bool:
        _enc_bytes(ref.value, count)
        self._bits += 7 + 8 * count
        return True

    def serialize_string(self, ref: Ref[str], buffer_size: int) -> bool:
        data = _enc_string(ref.value, buffer_size)
        self._bits += bits_required(0, buffer_size - 1) + 7 + 8 * len(data)
        return True

    def serialize_align(self) -> bool:
        self._bits += 7
        return True

    def serialize_end(self) -> bool:
        return True

    def serialize_object(self, serialize: Callable[[Stream], bool]) -> bool:
        return serialize(self) is True

    def reject(self) -> bool:
        raise WriteMisuse("reject: the caller's refusal is read only; a MeasureStream has nothing to refuse")

    def fail(self, reason: str | None = None) -> bool:
        return False

    def bits_processed(self) -> int:
        return self._bits

    def align_bits(self) -> int:
        return 7
