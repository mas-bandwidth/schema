// serialize_float.goose -- float and double as their bit patterns (STANDARD.md,
// float, double; DESIGN.md (c), the float-bits route: bytes_of over a
// one-element array out, from_bytes back in, measured on the bench).

import .serialize_stream;

// STANDARD.md, float/double: the bits go through bytes_of, never arithmetic
// (DESIGN.md (c), Float bits).
fn serialize_f32_bits(value: f32) -> u32 {
    let a: f32[1] = [value];
    let b = bytes_of(a);
    return b[0] as u32 | (b[1] as u32 << 8) | (b[2] as u32 << 16) | (b[3] as u32 << 24);
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
    return b[0] as u64 | (b[1] as u64 << 8) | (b[2] as u64 << 16) | (b[3] as u64 << 24)
        | (b[4] as u64 << 32) | (b[5] as u64 << 40) | (b[6] as u64 << 48) | (b[7] as u64 << 56);
}

fn serialize_f64_from_bits(bits: u64) -> f64 {
    let image: u8[9] = [8, bits as! u8, (bits >> 8) as! u8, (bits >> 16) as! u8, (bits >> 24) as! u8,
                        (bits >> 32) as! u8, (bits >> 40) as! u8, (bits >> 48) as! u8, (bits >> 56) as! u8];
    let v, ok = from_bytes<f64[>..]>(image);
    assert(ok);
    return v[0];
}

// float: the 32 bits of the IEEE 754 binary32 pattern (STANDARD.md, float).
fn serialize_float(s: WriteStream&, value: f32) -> bool {
    return serialize_bits(s, serialize_f32_bits(value), 32);
}
fn serialize_float(s: ReadStream&, value: f32&) -> bool {
    var bits: u32 = 0;
    guard serialize_bits(s, bits, 32) else { return false; }
    value = serialize_f32_from_bits(bits);
    return true;
}
fn serialize_float(s: MeasureStream&, value: f32) -> bool {
    return serialize_bits(s, 0, 32);
}

// double: the 64 bits of the binary64 pattern, low 32 first (STANDARD.md, double).
fn serialize_double(s: WriteStream&, value: f64) -> bool {
    return serialize_uint64(s, serialize_f64_bits(value));
}
fn serialize_double(s: ReadStream&, value: f64&) -> bool {
    var bits: u64 = 0;
    guard serialize_uint64(s, bits) else { return false; }
    value = serialize_f64_from_bits(bits);
    return true;
}
fn serialize_double(s: MeasureStream&, value: f64) -> bool {
    return serialize_uint64(s, 0);
}
