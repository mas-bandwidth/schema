// serialize_float.lobster — float (float32 on the wire) and double, and the
// float32 routes (STANDARD.md, Floating Point: float, double; DESIGN.md (c), (f)).
//
// float writes the 32 bits of the IEEE-754 single-precision representation as
// one 32-bit group; double writes the 64 bits of the double as one 64-bit
// group. Both carry FINITE values only (STANDARD.md, Floating Point, "Finite
// values only, every one bit exact"): a pattern whose exponent field is all
// ones (NaN with any payload, either infinity) is refused as non_finite_float
// on read, tested on the raw wire bits before the destination is written, and
// is write misuse on a write or a measure, which traps (IMPLEMENTING.md U9). Every finite pattern, negative zero and
// the subnormals included, is reproduced bit for bit.
import .serialize_stream

// The float32 scratch string the bit routes use (DESIGN.md (c), float32): one
// 16-byte string, allocated once and reused.
let serialize_float_scratch = ensure_size("", 16, 0)

// A float<32> holder: Lobster's native route to a rounded float32 (DESIGN.md
// (c), "round to f32: store into a float<32> struct field, read it back").
struct serialize_float32_holder:
    value:float<32>

// serialize_float32_round: round a double to float32 and widen the rounded
// value back to a double (DESIGN.md (c)). compressed_float uses this after
// every float32 operation.
def serialize_float32_round(x:float) -> float:
    let holder = serialize_float32_holder { value: x }
    return holder.value

// The finite tests, on raw bits (STANDARD.md, "Finite values only"): the
// exponent field all ones is NaN or an infinity.
def serialize_float32_bits_are_finite(bits:int) -> bool:
    return (bits & 0x7F800000) != 0x7F800000

def serialize_float64_bits_are_finite(bits:int) -> bool:
    return (bits & 0x7FF0000000000000) != 0x7FF0000000000000

// The finite checks a write and a measure make (IMPLEMENTING.md U6, U9).
def serialize_check_float32_finite(pattern:int):
    if not serialize_float32_bits_are_finite(pattern):
        serialize_abort("serialize: float value must be finite")

def serialize_check_float64_finite(pattern:int):
    if not serialize_float64_bits_are_finite(pattern):
        serialize_abort("serialize: double value must be finite")

// serialize_float32_bits: the 32 bits of the IEEE-754 single-precision
// representation, through the scratch string (STANDARD.md, Floating Point,
// float; DESIGN.md (c)). A finite double above FLT_MAX narrows to an
// infinity, which the writer then refuses.
def serialize_float32_bits(x:float) -> int:
    write_float32_le(serialize_float_scratch, 0, x)
    return read_uint32_le(serialize_float_scratch, 0)

// serialize_float32_from_bits: the float32 pattern as a double, by bit surgery
// rather than a hardware conversion, so every finite pattern, the subnormals
// included, widens exactly (DESIGN.md (c)).
def serialize_float32_from_bits(u:int) -> float:
    let sign = serialize_shr(u, 31) & 1
    let exponent = serialize_shr(u, 23) & 0xFF
    let mantissa = u & 0x7FFFFF
    var exponent64 = 0
    var mantissa64 = 0
    if exponent == 0:
        if mantissa != 0:
            // Subnormal: normalize mantissa = 2^k + frac; the value is
            // 2^(k-149) * 1.frac, so the double exponent field is k + 874.
            var k = 0
            while serialize_shl(1, k + 1) <= mantissa:
                k += 1
            exponent64 = k + 874
            mantissa64 = serialize_shl(mantissa - serialize_shl(1, k), 52 - k)
    else:
        exponent64 = exponent + 896
        mantissa64 = serialize_shl(mantissa, 29)
    if exponent == 0xFF:
        exponent64 = 0x7FF
        mantissa64 = serialize_shl(mantissa, 29)
    let lo = mantissa64 & 0xFFFFFFFF
    let hi = (sign << 31) | (exponent64 << 20) | serialize_shr(mantissa64, 32)
    write_int32_le(serialize_float_scratch, 0, lo)
    write_int32_le(serialize_float_scratch, 4, hi)
    return read_float64_le(serialize_float_scratch, 0)

// serialize_float64_bits: the 64 bits of the IEEE-754 double, through the
// scratch string (DESIGN.md (c)).
def serialize_float64_bits(x:float) -> int:
    write_float64_le(serialize_float_scratch, 0, x)
    return read_uint64_le(serialize_float_scratch, 0)

// serialize_float64_from_bits: the double with exactly those 64 bits; reading
// float64 from the scratch is bit preserving (STANDARD.md, Floating Point,
// double; DESIGN.md (c)).
def serialize_float64_from_bits(u:int) -> float:
    write_int64_le(serialize_float_scratch, 0, u)
    return read_float64_le(serialize_float_scratch, 0)

// serialize_float on the write stream: a 32-bit group (STANDARD.md, float).
// A writer is handed finite values only, as it is handed an int inside its
// range: a NaN, an infinity, or a finite double that narrows past FLT_MAX
// traps, tested on the bits after the narrowing (IMPLEMENTING.md, The check
// model; U9). The whole width is gated before any bit moves.
def serialize_float(s:serialize_write_stream, value:float):
    let pattern = serialize_float32_bits(value)
    serialize_check_float32_finite(pattern)
    if not s.write_gate(32):
        return value, false
    s.pack_bits(pattern, 32)
    return value, true

// serialize_float on the read stream: the 32 wire bits are tested for a
// finite pattern BEFORE the destination is written, then become the value
// through serialize_float32_from_bits, so no read converts or canonicalizes
// (STANDARD.md, "Finite values only, every one bit exact").
def serialize_float(s:serialize_read_stream, value:float):
    if s.failed:
        return value, false
    if not s.read_gate(32):
        return value, false
    let bits = s.unpack_bits(32)
    if not serialize_float32_bits_are_finite(bits):
        return value, s.fail(serialize_refusal_non_finite_float)
    return serialize_float32_from_bits(bits), true

def serialize_float(s:serialize_measure_stream, value:float):
    serialize_check_float32_finite(serialize_float32_bits(value))
    s.measure_bits(32)
    return value, true

// serialize_double on the write stream: one 64-bit group (STANDARD.md,
// double). A NaN or an infinity traps (IMPLEMENTING.md U9).
def serialize_double(s:serialize_write_stream, value:float):
    let pattern = serialize_float64_bits(value)
    serialize_check_float64_finite(pattern)
    if not s.write_gate(64):
        return value, false
    s.pack_bits64(pattern, 64)
    return value, true

// serialize_double on the read stream: the 64 bits are tested for a finite
// pattern before the destination is written, and are then the double's
// pattern with no conversion (STANDARD.md, "Finite values only").
def serialize_double(s:serialize_read_stream, value:float):
    if s.failed:
        return value, false
    if not s.read_gate(64):
        return value, false
    let bits = s.unpack_bits64(64)
    if not serialize_float64_bits_are_finite(bits):
        return value, s.fail(serialize_refusal_non_finite_float)
    return serialize_float64_from_bits(bits), true

def serialize_double(s:serialize_measure_stream, value:float):
    serialize_check_float64_finite(serialize_float64_bits(value))
    s.measure_bits(64)
    return value, true
