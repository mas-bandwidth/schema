// serialize_float.lobster — float (float32 on the wire) and double, and the
// float32 routes (STANDARD.md, Floating Point: float, double; DESIGN.md (c), (f)).
//
// float writes the 32 bits of the IEEE-754 single-precision representation as
// one 32-bit group; double writes the 64 bits of the double as one 64-bit
// group. Both directions are bit transparent (STANDARD.md, Floating Point,
// "Bit transparency — both directions"): every NaN payload, signaling NaN,
// infinity, negative zero and denormal is reproduced exactly.
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

// serialize_float32_bits: the 32 bits of the IEEE-754 single-precision
// representation (STANDARD.md, Floating Point, float; DESIGN.md (c)).
//
// A NaN is the one value the hardware float32 conversion is not transparent
// for: converting a signaling NaN to float32 quiets it. A NaN read by
// serialize_float32_from_bits carries its payload in the top 23 mantissa bits
// with the low 29 clear, so the inverse surgery below reproduces the transmitted
// pattern exactly, signaling bit included (STANDARD.md, "Bit transparency —
// both directions"). Every other value goes through the scratch string.
def serialize_float32_bits(x:float) -> int:
    let u64 = serialize_float64_bits(x)
    let exponent = (u64 >> 52) & 0x7FF
    let mantissa = u64 & 0xFFFFFFFFFFFFF
    if exponent == 0x7FF and mantissa != 0:
        var payload = serialize_shr(mantissa, 29)
        if payload == 0:
            payload = 0x400000
        let sign = serialize_shr(u64, 63) & 1
        return (sign << 31) | 0x7F800000 | payload
    write_float32_le(serialize_float_scratch, 0, x)
    return read_uint32_le(serialize_float_scratch, 0)

// serialize_float32_from_bits: the float32 pattern as a double, by bit surgery
// rather than a hardware conversion, so a NaN payload survives a read
// (STANDARD.md, "Bit transparency — both directions"; DESIGN.md (c)).
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
// The whole width is gated before any bit moves (DESIGN.md (e)).
def serialize_float(s:serialize_write_stream, value:float):
    if s.failed:
        return value, false
    if not s.write_gate(32):
        return value, false
    s.pack_bits(serialize_float32_bits(value), 32)
    return value, true

// serialize_float on the read stream: the 32 wire bits become the value
// through serialize_float32_from_bits, so no read converts or canonicalizes
// (STANDARD.md, "Bit transparency — both directions").
def serialize_float(s:serialize_read_stream, value:float):
    if s.failed:
        return value, false
    if not s.read_gate(32):
        return value, false
    return serialize_float32_from_bits(s.unpack_bits(32)), true

def serialize_float(s:serialize_measure_stream, value:float):
    if s.failed:
        return value, false
    s.measure_bits(32)
    return value, true

// serialize_double on the write stream: one 64-bit group (STANDARD.md, double).
def serialize_double(s:serialize_write_stream, value:float):
    if s.failed:
        return value, false
    if not s.write_gate(64):
        return value, false
    s.pack_bits64(serialize_float64_bits(value), 64)
    return value, true

// serialize_double on the read stream: the 64 bits are the double's pattern,
// with no conversion (STANDARD.md, "Bit transparency — both directions").
def serialize_double(s:serialize_read_stream, value:float):
    if s.failed:
        return value, false
    if not s.read_gate(64):
        return value, false
    return serialize_float64_from_bits(s.unpack_bits64(64)), true

def serialize_double(s:serialize_measure_stream, value:float):
    if s.failed:
        return value, false
    s.measure_bits(64)
    return value, true
