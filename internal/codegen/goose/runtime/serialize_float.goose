// serialize_float.goose -- float and double as their bit patterns (STANDARD.md,
// float, double; DESIGN.md (c), the float-bits route: bytes_of over a
// one-element array out, from_bytes back in, measured on the bench).

import .serialize_stream;

// STANDARD.md, float/double: the bits go through bytes_of, never arithmetic
// (DESIGN.md (c), Float bits).
fn serialize_f32_bits(value: f32) -> u32 {
    let a: f32[1] = [value];
    let b = bytes_of(a);
    return b[0] | (b[1] as u32 << 8) | (b[2] as u32 << 16) | (b[3] as u32 << 24);
}

fn serialize_f32_from_bits(bits: u32) -> f32 {
    let image: u8[5] = [4, bits as! u8, (bits >> 8) as! u8, (bits >> 16) as! u8, (bits >> 24) as! u8];
    let v, ok = from_bytes<f32[>..]>(image);
    assert(ok);
    return v[0];
}

fn serialize_f64_bits(value: f64) -> u64 {
    let a: f64[1] = [value];
    let b = bytes_of(a);
    return b[0] | (b[1] as u64 << 8) | (b[2] as u64 << 16) | (b[3] as u64 << 24)
        | (b[4] as u64 << 32) | (b[5] as u64 << 40) | (b[6] as u64 << 48) | (b[7] as u64 << 56);
}

fn serialize_f64_from_bits(bits: u64) -> f64 {
    let image: u8[9] = [8, bits as! u8, (bits >> 8) as! u8, (bits >> 16) as! u8, (bits >> 24) as! u8,
                        (bits >> 32) as! u8, (bits >> 40) as! u8, (bits >> 48) as! u8, (bits >> 56) as! u8];
    let v, ok = from_bytes<f64[>..]>(image);
    assert(ok);
    return v[0];
}

// The finite tests, the C++ float_bits_are_finite and double_bits_are_finite:
// the exponent field is all ones for NaN (any payload, either sign) and the
// infinities, and for nothing else. Tested on the BITS, never the value, so
// no conversion can quiet a signaling NaN before the test sees it
// (STANDARD.md, Finite values only, every one bit exact).
fn serialize_float_bits_are_finite(bits: u32) -> bool { return (bits & 0x7F800000) != 0x7F800000; }
fn serialize_double_bits_are_finite(bits: u64) -> bool {
    return (bits & 0x7FF0000000000000) != 0x7FF0000000000000;
}

// float: the 32 bits of the IEEE 754 binary32 pattern (STANDARD.md, float).
// Finite values only: a writer or a measure handed NaN or an infinity aborts
// (write misuse, U6, U9) and writes nothing, and a reader refuses a non-finite
// pattern as NON_FINITE_FLOAT BEFORE the destination is written. -0 and the
// subnormals are finite and keep their exact bits.
fn serialize_float(s: WriteStream&, value: f32) -> bool {
    let pattern = serialize_f32_bits(value);
    guard serialize_float_bits_are_finite(pattern) else { abort("serialize: float must be finite"); }
    return serialize_bits(s, pattern, 32);
}
fn serialize_float(s: ReadStream&, value: f32&) -> bool {
    var bits: u32 = 0;
    guard serialize_bits(s, bits, 32) else { return false; }
    guard serialize_float_bits_are_finite(bits) else { return serialize_fail(s, SERIALIZE_ERROR_NON_FINITE_FLOAT); }
    value = serialize_f32_from_bits(bits);
    return true;
}
fn serialize_float(s: MeasureStream&, value: f32) -> bool {
    guard serialize_float_bits_are_finite(serialize_f32_bits(value)) else { abort("serialize: float must be finite"); }
    return serialize_bits(s, 0, 32);
}

// double: the 64 bits of the binary64 pattern, low 32 first (STANDARD.md,
// double), finite only exactly as float.
fn serialize_double(s: WriteStream&, value: f64) -> bool {
    let pattern = serialize_f64_bits(value);
    guard serialize_double_bits_are_finite(pattern) else { abort("serialize: double must be finite"); }
    return serialize_uint64(s, pattern);
}
fn serialize_double(s: ReadStream&, value: f64&) -> bool {
    var bits: u64 = 0;
    guard serialize_uint64(s, bits) else { return false; }
    guard serialize_double_bits_are_finite(bits) else { return serialize_fail(s, SERIALIZE_ERROR_NON_FINITE_FLOAT); }
    value = serialize_f64_from_bits(bits);
    return true;
}
fn serialize_double(s: MeasureStream&, value: f64) -> bool {
    guard serialize_double_bits_are_finite(serialize_f64_bits(value)) else { abort("serialize: double must be finite"); }
    return serialize_uint64(s, 0);
}
