// serialize_int128.lobster — uint128 and ranged int128 over two int lanes
// (STANDARD.md, Bit-Level Primitives, uint128; Integers, int128; DESIGN.md (c), (f)).
import .serialize_stream
import .serialize_bitpacker

// The two types are final here: serialize.c's serialize_uint128_t and
// serialize_int128_t, the same two 64-bit lanes { lo, hi } (bits 0..63 and
// 64..127), read unsigned and two's complement.
struct serialize_uint128:
    lo:int
    hi:int

struct serialize_int128:
    lo:int
    hi:int

def serialize_uint128_make(hi:int, lo:int) -> serialize_uint128:
    return serialize_uint128 { lo: lo, hi: hi }

def serialize_int128_make(hi:int, lo:int) -> serialize_int128:
    return serialize_int128 { lo: lo, hi: hi }

// serialize.c serialize_int128_from_int64: sign extend into the high lane
// (DESIGN.md (c), 128 bits are two int lanes).
def serialize_int128_from_int64(value:int) -> serialize_int128:
    let hi = if value < 0: -1 else: 0
    return serialize_int128 { lo: value, hi: hi }

def serialize_uint128_equal(a:serialize_uint128, b:serialize_uint128) -> bool:
    return a.lo == b.lo and a.hi == b.hi

def serialize_int128_equal(a:serialize_int128, b:serialize_int128) -> bool:
    return a.lo == b.lo and a.hi == b.hi

// Unsigned order of two 64-bit patterns: the sign-flipped compare
// (DESIGN.md (c), "Unsigned order").
def serialize_u128_compare(a:serialize_uint128, b:serialize_uint128) -> int:
    if a.hi != b.hi:
        return if serialize_ult(a.hi, b.hi): -1 else: 1
    if a.lo != b.lo:
        return if serialize_ult(a.lo, b.lo): -1 else: 1
    return 0

// Signed order: flip the high lane's sign bit, then compare unsigned
// (serialize.c serialize_int128_compare; DESIGN.md (c)).
def serialize_int128_compare(a:serialize_int128, b:serialize_int128) -> int:
    let ua = serialize_uint128_make(a.hi ^ serialize_sign_bit, a.lo)
    let ub = serialize_uint128_make(b.hi ^ serialize_sign_bit, b.lo)
    return serialize_u128_compare(ua, ub)

// a - b in the unsigned 128-bit domain, wrapping like the hardware would:
// the low lane subtracts and the borrow reaches the high lane
// (serialize.c serialize_u128_sub; DESIGN.md (c), 128 bits).
def serialize_u128_sub(a:serialize_uint128, b:serialize_uint128) -> serialize_uint128:
    let lo = a.lo - b.lo
    let borrow = if serialize_ult(a.lo, b.lo): 1 else: 0
    let hi = a.hi - b.hi - borrow
    return serialize_uint128 { lo: lo, hi: hi }

// a + b in the unsigned 128-bit domain: the low lane adds and the carry
// reaches the high lane (serialize.c serialize_u128_add; DESIGN.md (c)).
def serialize_u128_add(a:serialize_uint128, b:serialize_uint128) -> serialize_uint128:
    let lo = a.lo + b.lo
    let carry = if serialize_ult(lo, a.lo): 1 else: 0
    let hi = a.hi + b.hi + carry
    return serialize_uint128 { lo: lo, hi: hi }

// The bit length of a 128-bit unsigned value, 0 for 0
// (serialize.c serialize_u128_bit_length; DESIGN.md (c), popcount may serve).
def serialize_u128_bit_length(v:serialize_uint128) -> int:
    if v.hi != 0:
        return 64 + serialize_bit_length64(v.hi)
    return serialize_bit_length64(v.lo)

// The 32-bit group at index i, counting from least significant
// (serialize.c serialize_u128_group32; STANDARD.md, int128, "32-bit groups
// from least significant upward").
def serialize_u128_group32(v:serialize_uint128, i:int) -> int:
    if i == 0:
        return v.lo & 0xFFFFFFFF
    if i == 1:
        return serialize_shr(v.lo, 32) & 0xFFFFFFFF
    if i == 2:
        return v.hi & 0xFFFFFFFF
    return serialize_shr(v.hi, 32) & 0xFFFFFFFF

def serialize_u128_set_group32(v:serialize_uint128, i:int, g:int) -> serialize_uint128:
    if i == 0:
        return serialize_uint128 { lo: v.lo | g, hi: v.hi }
    if i == 1:
        return serialize_uint128 { lo: v.lo | serialize_shl(g, 32), hi: v.hi }
    if i == 2:
        return serialize_uint128 { lo: v.lo, hi: v.hi | g }
    return serialize_uint128 { lo: v.lo, hi: v.hi | serialize_shl(g, 32) }

// Bits required for [min, max] in the unsigned 128-bit domain
// (STANDARD.md, int128: bits_required128; DESIGN.md (c)).
def serialize_bits_required128(min:serialize_int128, max:serialize_int128) -> int:
    let span = serialize_u128_sub(
        serialize_uint128_make(max.hi, max.lo),
        serialize_uint128_make(min.hi, min.lo))
    return serialize_u128_bit_length(span)

// Writes `bits` bits of an unsigned 128-bit value as 32-bit groups from least
// significant upward: bits <= 32 is one group, otherwise full groups with the
// final group carrying the remainder (STANDARD.md, int128; DESIGN.md (b)).
// The whole width is gated by the caller before the first group moves.
def serialize_write_u128_bits(s:serialize_write_stream, value:serialize_uint128, bits:int):
    var written = 0
    var group = 0
    while written < bits:
        var chunk = bits - written
        if chunk > 32:
            chunk = 32
        s.pack_bits(serialize_u128_group32(value, group), chunk)
        written += chunk
        group += 1

// Reads `bits` bits into a 128-bit value, group by group from least
// significant upward (STANDARD.md, int128; DESIGN.md (b)). The whole width is
// gated by the caller.
def serialize_read_u128_bits(s:serialize_read_stream, bits:int) -> serialize_uint128:
    var read = 0
    var group = 0
    var value = serialize_uint128_make(0, 0)
    while read < bits:
        var chunk = bits - read
        if chunk > 32:
            chunk = 32
        value = serialize_u128_set_group32(value, group, s.unpack_bits(chunk))
        read += chunk
        group += 1
    return value

// serialize_uint128: always 128 bits, the low 64-bit half first, each half
// exactly serialize_bits(half, 64) (STANDARD.md, uint128; DESIGN.md (b)).
// The whole width is gated before any bit moves, so a full buffer traps and
// writes nothing for this call (IMPLEMENTING.md U9, U10).
def serialize_uint128(s:serialize_write_stream, value:serialize_uint128):
    if not s.write_gate(128):
        return value, false
    s.pack_bits64(value.lo, 64)
    s.pack_bits64(value.hi, 64)
    return value, true

def serialize_uint128(s:serialize_read_stream, value:serialize_uint128):
    if not s.read_gate(128):
        return value, false
    let lo = s.unpack_bits64(64)
    let hi = s.unpack_bits64(64)
    return serialize_uint128 { lo: lo, hi: hi }, true

def serialize_uint128(s:serialize_measure_stream, value:serialize_uint128):
    return value, s.measure_bits(128)

// The range check a write and a measure make (IMPLEMENTING.md U8, U9).
def serialize_check_int128_in_range(value:serialize_int128, min:serialize_int128, max:serialize_int128):
    if serialize_int128_compare(value, min) < 0 or serialize_int128_compare(value, max) > 0:
        serialize_abort("serialize: value must be in [min, max]")

// serialize_int128: the offset value - min in the unsigned 128-bit domain,
// written in 32-bit groups from least significant upward (STANDARD.md,
// int128; DESIGN.md (b), (c), (e)).
def serialize_int128(s:serialize_write_stream, value:serialize_int128, min:serialize_int128, max:serialize_int128):
    // min > max is API misuse: abort before any state is consulted
    // (STANDARD.md, int; DESIGN.md (e), "min > max").
    if serialize_int128_compare(min, max) > 0:
        serialize_abort("serialize: min must not be greater than max")
    // A value outside [min, max] traps (IMPLEMENTING.md U9).
    serialize_check_int128_in_range(value, min, max)
    let umin = serialize_uint128_make(min.hi, min.lo)
    let umax = serialize_uint128_make(max.hi, max.lo)
    let uvalue = serialize_uint128_make(value.hi, value.lo)
    let span = serialize_u128_sub(umax, umin)
    let bits = serialize_u128_bit_length(span)
    // A degenerate range costs zero bits (STANDARD.md, int; int128).
    if bits == 0:
        return value, true
    let offset = serialize_u128_sub(uvalue, umin)
    if not s.write_gate(bits):
        return value, false
    serialize_write_u128_bits(s, offset, bits)
    return value, true

def serialize_int128(s:serialize_read_stream, value:serialize_int128, min:serialize_int128, max:serialize_int128):
    // min > max is a bad declaration: refused on a read, never an abort
    // (IMPLEMENTING.md U7, U16).
    if serialize_int128_compare(min, max) > 0:
        return value, s.serialize_read_misuse()
    if s.failed:
        return value, false
    let umin = serialize_uint128_make(min.hi, min.lo)
    let umax = serialize_uint128_make(max.hi, max.lo)
    let span = serialize_u128_sub(umax, umin)
    let bits = serialize_u128_bit_length(span)
    if bits == 0:
        return min, true
    if not s.read_gate(bits):
        return value, false
    let offset = serialize_read_u128_bits(s, bits)
    // Reject, never clamp: the decoded offset must be at most the span
    // (STANDARD.md, int128; DESIGN.md (e)).
    if serialize_u128_compare(offset, span) > 0:
        return value, s.fail(serialize_refusal_value_out_of_range)
    let result = serialize_u128_add(umin, offset)
    return serialize_int128 { lo: result.lo, hi: result.hi }, true

def serialize_int128(s:serialize_measure_stream, value:serialize_int128, min:serialize_int128, max:serialize_int128):
    if serialize_int128_compare(min, max) > 0:
        serialize_abort("serialize: min must not be greater than max")
    serialize_check_int128_in_range(value, min, max)
    return value, s.measure_bits(serialize_bits_required128(min, max))
