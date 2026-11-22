// serialize_int128.goose -- 128-bit integers as two u64 lanes (STANDARD.md,
// uint128, int128; DESIGN.md (c)). Card serialize-goose-integers fills this
// file.

import std;
import .serialize_stream;

// bits 0..63 in lo, 64..127 in hi; Int128 is the same lanes read as two's
// complement (serialize.c serialize_uint128_t, serialize_int128_t).
struct Uint128 { lo: u64, hi: u64 }
struct Int128 { lo: u64, hi: u64 }

fn serialize_uint128_make(hi: u64, lo: u64) -> Uint128 { return Uint128 { lo: lo, hi: hi }; }
fn serialize_int128_make(hi: u64, lo: u64) -> Int128 { return Int128 { lo: lo, hi: hi }; }
// Sign-extends a 64-bit value into the 128-bit domain.
fn serialize_int128_from_int64(value: i64) -> Int128 {
    return Int128 { lo: value as! u64, hi: if value < 0 { 0xffffffffffffffff } else { 0 } };
}
fn serialize_uint128_equal(a: Uint128, b: Uint128) -> bool { return a.lo == b.lo && a.hi == b.hi; }
fn serialize_int128_equal(a: Int128, b: Int128) -> bool { return a.lo == b.lo && a.hi == b.hi; }

// -1, 0 or 1, in the unsigned domain (serialize.c serialize_u128_compare).
fn serialize_u128_compare(a: Uint128, b: Uint128) -> i64 {
    if a.hi != b.hi { return if a.hi < b.hi { -1 } else { 1 }; }
    if a.lo != b.lo { return if a.lo < b.lo { -1 } else { 1 }; }
    return 0;
}
fn serialize_uint128_compare(a: Uint128, b: Uint128) -> i64 { return serialize_u128_compare(a, b); }

// -1, 0 or 1, in the signed domain: flip the sign bit and compare unsigned,
// so no signed overflow is possible (serialize.c serialize_int128_compare).
fn serialize_int128_compare(a: Int128, b: Int128) -> i64 {
    let ua = Uint128 { lo: a.lo, hi: a.hi ^ ((1 as u64) << 63) };
    let ub = Uint128 { lo: b.lo, hi: b.hi ^ ((1 as u64) << 63) };
    return serialize_u128_compare(ua, ub);
}

// a - b in the unsigned 128-bit domain, borrowing across the lanes.
fn serialize_u128_sub(a: Uint128, b: Uint128) -> Uint128 {
    let borrow: u64 = if a.lo < b.lo { 1 } else { 0 };
    return Uint128 { lo: a.lo - b.lo, hi: a.hi - b.hi - borrow };
}
// a - b in the signed 128-bit domain (two's complement).
fn serialize_int128_sub(a: Int128, b: Int128) -> Int128 {
    let r = serialize_u128_sub(Uint128 { lo: a.lo, hi: a.hi }, Uint128 { lo: b.lo, hi: b.hi });
    return Int128 { lo: r.lo, hi: r.hi };
}
// a + b in the unsigned 128-bit domain, carrying across the lanes.
fn serialize_u128_add(a: Uint128, b: Uint128) -> Uint128 {
    let lo = a.lo + b.lo;
    let carry: u64 = if lo < a.lo { 1 } else { 0 };
    return Uint128 { lo: lo, hi: a.hi + b.hi + carry };
}
// a + b in the signed 128-bit domain (two's complement).
fn serialize_int128_add(a: Int128, b: Int128) -> Int128 {
    let r = serialize_u128_add(Uint128 { lo: a.lo, hi: a.hi }, Uint128 { lo: b.lo, hi: b.hi });
    return Int128 { lo: r.lo, hi: r.hi };
}

// Bit length, 0 for zero (serialize.c serialize_u128_bit_length).
fn serialize_u128_bit_length(v: Uint128) -> i64 {
    if v.hi != 0 { return 64 + (64 - clz(v.hi)); }
    if v.lo != 0 { return 64 - clz(v.lo); }
    return 0;
}

// bits_required at 128 bits over the unsigned domain, 0 when min == max
// (STANDARD.md, int128).
fn serialize_bits_required128(min: Uint128, max: Uint128) -> i64 {
    if serialize_uint128_equal(min, max) { return 0; }
    return serialize_u128_bit_length(serialize_u128_sub(max, min));
}

// The 32-bit group at index i, counting from least significant.
fn serialize_u128_group32(v: Uint128, i: i64) -> u32 {
    if i == 0 { return v.lo as! u32; }
    if i == 1 { return (v.lo >> 32) as! u32; }
    if i == 2 { return v.hi as! u32; }
    return (v.hi >> 32) as! u32;
}
fn serialize_u128_set_group32(v: Uint128&, i: i64, g: u32) {
    if i == 0 { v.lo |= g; }
    else if i == 1 { v.lo |= (g as u64) << 32; }
    else if i == 2 { v.hi |= g; }
    else { v.hi |= (g as u64) << 32; }
}

// Writes `bits` bits as 32-bit groups from least significant upward
// (STANDARD.md, int128). The caller has already gated the whole width.
fn serialize_u128_write_bits(s: WriteStream&, value: Uint128, bits: i64) {
    var written: i64 = 0;
    var group: i64 = 0;
    while written < bits {
        var chunk = bits - written;
        if chunk > 32 { chunk = 32; }
        let g: u64 = serialize_u128_group32(value, group);
        let mask: u64 = if chunk == 64 { 0xffffffffffffffff } else { ((1 as u64) << (chunk as u64)) - 1 };
        serialize_packer_write(s, g & mask, chunk);
        written += chunk;
        group += 1;
    }
}
// Reads `bits` bits back, group by group; the caller has gated the width.
fn serialize_u128_read_bits(s: ReadStream&, value: Uint128&, bits: i64) {
    var v = Uint128 { lo: 0, hi: 0 };
    var read: i64 = 0;
    var group: i64 = 0;
    while read < bits {
        var chunk = bits - read;
        if chunk > 32 { chunk = 32; }
        let g = serialize_packer_read(s, chunk) as! u32;
        serialize_u128_set_group32(v, group, g);
        read += chunk;
        group += 1;
    }
    value = v;
}

// uint128: always 128 bits, the low half first, each half as bits(half, 64)
// (STANDARD.md, uint128).
fn serialize_uint128(s: WriteStream&, value: Uint128) -> bool {
    serialize_write_check(s, 128);
    serialize_packer_write(s, value.lo, 64);
    serialize_packer_write(s, value.hi, 64);
    return true;
}
fn serialize_uint128(s: ReadStream&, value: Uint128&) -> bool {
    guard serialize_read_check(s, 128) else { return false; }
    let lo = serialize_packer_read(s, 64);
    let hi = serialize_packer_read(s, 64);
    value = Uint128 { lo: lo, hi: hi };
    return true;
}
fn serialize_uint128(s: MeasureStream&, value: Uint128) -> bool { return serialize_measure_add(s, 128); }

// int128: ranged; where the range fits 64 bits the bytes equal int64 over the
// same bounds. The span and offset are unsigned, so a range wider than 2^127
// is exact (STANDARD.md, int128; DESIGN.md (c), (e)).
fn serialize_int128(s: WriteStream&, value: Int128, min: Int128, max: Int128) -> bool {
    if serialize_int128_compare(min, max) > 0 { abort("serialize: min must not be greater than max"); }
    if serialize_int128_compare(value, min) < 0 || serialize_int128_compare(value, max) > 0 {
        abort("serialize: value must be in [min, max]");
    }
    let umin = Uint128 { lo: min.lo, hi: min.hi };
    let umax = Uint128 { lo: max.lo, hi: max.hi };
    let uvalue = Uint128 { lo: value.lo, hi: value.hi };
    let bits = serialize_bits_required128(umin, umax);
    if bits == 0 { return true; }
    let offset = serialize_u128_sub(uvalue, umin);
    serialize_write_check(s, bits);
    serialize_u128_write_bits(s, offset, bits);
    return true;
}
fn serialize_int128(s: ReadStream&, value: Int128&, min: Int128, max: Int128) -> bool {
    guard serialize_int128_compare(min, max) <= 0 else { return serialize_fail(s, SERIALIZE_ERROR_VALUE_OUT_OF_RANGE); }
    let umin = Uint128 { lo: min.lo, hi: min.hi };
    let umax = Uint128 { lo: max.lo, hi: max.hi };
    let span = serialize_u128_sub(umax, umin);
    let bits = serialize_u128_bit_length(span);
    if bits == 0 {
        guard serialize_read_check(s, 0) else { return false; }
        value = min;
        return true;
    }
    guard serialize_read_check(s, bits) else { return false; }
    var offset = Uint128 { lo: 0, hi: 0 };
    serialize_u128_read_bits(s, offset, bits);
    if serialize_u128_compare(offset, span) > 0 {
        return serialize_fail(s, SERIALIZE_ERROR_VALUE_OUT_OF_RANGE);
    }
    let result = serialize_u128_add(umin, offset);
    value = Int128 { lo: result.lo, hi: result.hi };
    return true;
}
fn serialize_int128(s: MeasureStream&, value: Int128, min: Int128, max: Int128) -> bool {
    if serialize_int128_compare(min, max) > 0 { abort("serialize: min must not be greater than max"); }
    if serialize_int128_compare(value, min) < 0 || serialize_int128_compare(value, max) > 0 {
        abort("serialize: value must be in [min, max]");
    }
    let umin = Uint128 { lo: min.lo, hi: min.hi };
    let umax = Uint128 { lo: max.lo, hi: max.hi };
    return serialize_measure_add(s, serialize_bits_required128(umin, umax));
}
