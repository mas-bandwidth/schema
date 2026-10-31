// serialize_fixed.lobster — Q format fixed point at three storage widths
// (STANDARD.md, Integers, fixed (Q format, ranged); DESIGN.md (c), (f)).
//
// The encoding is an offset encoding over the RAW (scaled) bounds:
// raw_min = min << fraction_bits, raw_max = max << fraction_bits, and
// raw_value - raw_min is written in the bit length of raw_max - raw_min, in
// 32-bit groups from least significant upward (STANDARD.md, fixed).
//
// This card lands without serialize-lobster-integers, so the unsigned 128-bit
// pair arithmetic the wide path needs (DESIGN.md (c), "128 bits") lives here,
// written over the { lo, hi } lanes the int128 family defines. The bytes are
// serialize.c v2.0.0's.
import .serialize_stream
import .serialize_bitpacker
import .serialize_int128

// ---------------------------------------------------------------------------
// API misuse (DESIGN.md (e)): a bad Q format, min > max, or whole-unit bounds
// that do not fit the Q format. On a write or a measure these abort; on a read
// they refuse value_out_of_range before any bit moves (IMPLEMENTING.md U7,
// U16).
// ---------------------------------------------------------------------------

// The rules a declaration can break, by the code serialize_fixed_declaration
// returns (0, a good declaration, has none).
let serialize_fixed_messages = [
    "",
    "serialize: fixed point requires integer bits >= 1, fraction bits >= 0, a Q format that fits the storage width, and min <= max",
    "serialize: fixed point bounds in whole units do not fit the Q format"
]

// A Q format is legal when its storage width is one STANDARD.md's fixed
// section carries (8, 16, 32, 64 or 128, always signed), it lies within this
// arm's storage, integer_bits is at least 1, and min <= max (a degenerate
// range is legal). The whole-unit bounds must also fit integer_bits, the sign
// bit counted. Returns the code of the rule a bad declaration breaks, 0 for a
// good one: a code, not a string, so the read path allocates nothing.
def serialize_fixed_declaration(integer_bits:int, fraction_bits:int, min:int, max:int, min_storage:int, max_storage:int) -> int:
    let storage = integer_bits + fraction_bits
    if integer_bits < 1 or fraction_bits < 0 or storage < min_storage or storage > max_storage         or not (storage == 8 or storage == 16 or storage == 32 or storage == 64 or storage == 128):
        return 1
    if min > max:
        return 1
    if integer_bits < 64:
        let low = -serialize_shl(1, integer_bits - 1)
        let high = serialize_shl(1, integer_bits - 1) - 1
        if min < low or max > high:
            return 2
    return 0

// The write and measure check: a bad declaration aborts.
def serialize_fixed_validate(integer_bits:int, fraction_bits:int, min:int, max:int, min_storage:int, max_storage:int):
    let code = serialize_fixed_declaration(integer_bits, fraction_bits, min, max, min_storage, max_storage)
    if code != 0:
        serialize_abort(serialize_fixed_messages[code])

// ---------------------------------------------------------------------------
// the unsigned 128-bit pair (DESIGN.md (c); serialize.c's serialize_u128_*)
// ---------------------------------------------------------------------------

def serialize_fixed_from_int64(value:int) -> serialize_int128:
    return serialize_int128 { lo: value, hi: if value < 0: -1 else: 0 }

// Logical left shift of an unsigned pair, n in [0,128], wrapping.
def serialize_fixed_shl128(u:serialize_int128, n:int) -> serialize_int128:
    if n <= 0:
        return u
    if n < 64:
        return serialize_int128 {
            lo: serialize_shl(u.lo, n),
            hi: serialize_shl(u.hi, n) | serialize_shr(u.lo, 64 - n)
        }
    if n < 128:
        return serialize_int128 { lo: 0, hi: serialize_shl(u.lo, n - 64) }
    return serialize_int128 { lo: 0, hi: 0 }

// a - b in the unsigned 128-bit domain, wrapping.
def serialize_fixed_sub128(a:serialize_int128, b:serialize_int128) -> serialize_int128:
    let lo = a.lo - b.lo
    let borrow = if serialize_ult(a.lo, b.lo): 1 else: 0
    return serialize_int128 { lo: lo, hi: a.hi - b.hi - borrow }

// a + b in the unsigned 128-bit domain, wrapping.
def serialize_fixed_add128(a:serialize_int128, b:serialize_int128) -> serialize_int128:
    let lo = a.lo + b.lo
    let carry = if serialize_ult(lo, a.lo): 1 else: 0
    return serialize_int128 { lo: lo, hi: a.hi + b.hi + carry }

// Unsigned comparison of two 128-bit pairs, -1, 0 or 1.
def serialize_fixed_compare128(a:serialize_int128, b:serialize_int128) -> int:
    if a.hi != b.hi:
        return if serialize_ult(a.hi, b.hi): -1 else: 1
    if a.lo != b.lo:
        return if serialize_ult(a.lo, b.lo): -1 else: 1
    return 0

// Bit length of an unsigned 128-bit pair; 0 for 0.
def serialize_fixed_bit_length128(v:serialize_int128) -> int:
    if v.hi != 0:
        return 64 + serialize_bit_length64(v.hi)
    return serialize_bit_length64(v.lo)

// raw_bound: min/max are WHOLE units, the raw bound is whole << fraction_bits
// (STANDARD.md, fixed; serialize.c serialize_raw_bound).
def serialize_fixed_raw_bound(whole:int, fraction_bits:int) -> serialize_int128:
    return serialize_fixed_shl128(serialize_fixed_from_int64(whole), fraction_bits)

// The 32-bit group at index i, counting from least significant (STANDARD.md,
// fixed: the same splitting rule as serialize_bits).
def serialize_fixed_group32(v:serialize_int128, i:int) -> int:
    switch i:
        case 0: return v.lo & 0xFFFFFFFF
        case 1: return serialize_shr(v.lo, 32)
        case 2: return v.hi & 0xFFFFFFFF
        default: return serialize_shr(v.hi, 32)

// Writes `bits` bits of an unsigned 128-bit value as 32-bit groups from least
// significant upward; the caller has already gated the whole width.
def serialize_fixed_write_u128(s:serialize_write_stream, value:serialize_int128, bits:int):
    var written = 0
    var group = 0
    while written < bits:
        var chunk = bits - written
        if chunk > 32:
            chunk = 32
        s.pack_bits(serialize_fixed_group32(value, group), chunk)
        written += chunk
        group += 1

// Reads `bits` bits into an unsigned 128-bit value; the caller has already
// gated the whole width and no byte outside the data is loaded.
def serialize_fixed_read_u128(s:serialize_read_stream, bits:int) -> serialize_int128:
    var lo = 0
    var hi = 0
    var read = 0
    var group = 0
    while read < bits:
        var chunk = bits - read
        if chunk > 32:
            chunk = 32
        let g = s.unpack_bits(chunk)
        switch group:
            case 0: lo = lo | g
            case 1: lo = lo | serialize_shl(g, 32)
            case 2: hi = hi | g
            default: hi = hi | serialize_shl(g, 32)
        read += chunk
        group += 1
    return serialize_int128 { lo: lo, hi: hi }

// The range check a write and a measure make (IMPLEMENTING.md U6, U9): a raw
// value outside [raw_min, raw_max] traps. Returns the field's width.
def serialize_fixed_check(raw:serialize_int128, fraction_bits:int, min:int, max:int) -> int:
    let raw_min = serialize_fixed_raw_bound(min, fraction_bits)
    let raw_max = serialize_fixed_raw_bound(max, fraction_bits)
    let span = serialize_fixed_sub128(raw_max, raw_min)
    let offset = serialize_fixed_sub128(raw, raw_min)
    if serialize_fixed_compare128(span, offset) < 0:
        serialize_abort("serialize: value must be in [min, max]")
    return serialize_fixed_bit_length128(span)

// The write core: an offset encoding over the raw bounds (STANDARD.md, fixed;
// serialize.c serialize_write_fixed_core). A value outside the raw span
// traps (IMPLEMENTING.md U9).
def serialize_fixed_write_core(s:serialize_write_stream, raw:serialize_int128, fraction_bits:int, min:int, max:int) -> bool:
    let bits = serialize_fixed_check(raw, fraction_bits, min, max)
    if bits == 0:
        return true
    let raw_min = serialize_fixed_raw_bound(min, fraction_bits)
    let offset = serialize_fixed_sub128(raw, raw_min)
    if not s.write_gate(bits):
        return false
    serialize_fixed_write_u128(s, offset, bits)
    return true

// The read core: check the decoded offset against the raw span, reject never
// clamp (STANDARD.md, fixed), and leave the caller's value untouched on a
// refusal (STANDARD.md, Reader Obligations).
def serialize_fixed_read_core(s:serialize_read_stream, fraction_bits:int, min:int, max:int) -> serialize_int128, bool:
    let raw_min = serialize_fixed_raw_bound(min, fraction_bits)
    let raw_max = serialize_fixed_raw_bound(max, fraction_bits)
    let span = serialize_fixed_sub128(raw_max, raw_min)
    let bits = serialize_fixed_bit_length128(span)
    // A degenerate field reads no bits and still consults the failure state:
    // on a failed stream it is refused too (STANDARD.md, Reader Obligations,
    // "Failure is terminal").
    if bits == 0:
        return raw_min, not s.failed
    if not s.read_gate(bits):
        return raw_min, false
    let offset = serialize_fixed_read_u128(s, bits)
    if serialize_fixed_compare128(offset, span) > 0:
        return raw_min, s.fail(serialize_refusal_value_out_of_range)
    return serialize_fixed_add128(raw_min, offset), true

// ---------------------------------------------------------------------------
// fixed32: storage 8, 16 or 32 bits, over the int32 raw value
// ---------------------------------------------------------------------------

def serialize_fixed32(s:serialize_write_stream, value:int, integer_bits:int, fraction_bits:int, min:int, max:int):
    serialize_fixed_validate(integer_bits, fraction_bits, min, max, 8, 32)
    return value, serialize_fixed_write_core(s, serialize_fixed_from_int64(value), fraction_bits, min, max)

def serialize_fixed32(s:serialize_read_stream, value:int, integer_bits:int, fraction_bits:int, min:int, max:int):
    if serialize_fixed_declaration(integer_bits, fraction_bits, min, max, 8, 32) != 0:
        return value, s.serialize_read_misuse()
    let raw, ok = serialize_fixed_read_core(s, fraction_bits, min, max)
    if not ok:
        return value, false
    let low = raw.lo & 0xFFFFFFFF
    return (if low >= 0x80000000: low - 0x100000000 else: low), true

def serialize_fixed32(s:serialize_measure_stream, value:int, integer_bits:int, fraction_bits:int, min:int, max:int):
    serialize_fixed_validate(integer_bits, fraction_bits, min, max, 8, 32)
    s.measure_bits(serialize_fixed_check(serialize_fixed_from_int64(value), fraction_bits, min, max))
    return value, true

// ---------------------------------------------------------------------------
// fixed64: storage 64 bits or fewer, over the int64 raw value
// ---------------------------------------------------------------------------

def serialize_fixed64(s:serialize_write_stream, value:int, integer_bits:int, fraction_bits:int, min:int, max:int):
    serialize_fixed_validate(integer_bits, fraction_bits, min, max, 8, 64)
    return value, serialize_fixed_write_core(s, serialize_fixed_from_int64(value), fraction_bits, min, max)

def serialize_fixed64(s:serialize_read_stream, value:int, integer_bits:int, fraction_bits:int, min:int, max:int):
    if serialize_fixed_declaration(integer_bits, fraction_bits, min, max, 8, 64) != 0:
        return value, s.serialize_read_misuse()
    let raw, ok = serialize_fixed_read_core(s, fraction_bits, min, max)
    if not ok:
        return value, false
    return raw.lo, true

def serialize_fixed64(s:serialize_measure_stream, value:int, integer_bits:int, fraction_bits:int, min:int, max:int):
    serialize_fixed_validate(integer_bits, fraction_bits, min, max, 8, 64)
    s.measure_bits(serialize_fixed_check(serialize_fixed_from_int64(value), fraction_bits, min, max))
    return value, true

// ---------------------------------------------------------------------------
// fixed128: storage exactly 128 bits, over the int128 raw value
// ---------------------------------------------------------------------------

def serialize_fixed128(s:serialize_write_stream, value:serialize_int128, integer_bits:int, fraction_bits:int, min:int, max:int):
    serialize_fixed_validate(integer_bits, fraction_bits, min, max, 128, 128)
    return value, serialize_fixed_write_core(s, value, fraction_bits, min, max)

def serialize_fixed128(s:serialize_read_stream, value:serialize_int128, integer_bits:int, fraction_bits:int, min:int, max:int):
    if serialize_fixed_declaration(integer_bits, fraction_bits, min, max, 128, 128) != 0:
        return value, s.serialize_read_misuse()
    let raw, ok = serialize_fixed_read_core(s, fraction_bits, min, max)
    if not ok:
        return value, false
    return raw, true

def serialize_fixed128(s:serialize_measure_stream, value:serialize_int128, integer_bits:int, fraction_bits:int, min:int, max:int):
    serialize_fixed_validate(integer_bits, fraction_bits, min, max, 128, 128)
    s.measure_bits(serialize_fixed_check(value, fraction_bits, min, max))
    return value, true
