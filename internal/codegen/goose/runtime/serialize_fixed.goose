// serialize_fixed.goose -- Q-format fixed point (STANDARD.md, fixed;
// DESIGN.md (a), (c)). integer_bits + fraction_bits equals the storage width,
// min and max are whole units; a bad Q format or min > max aborts.

import .serialize_stream;
import .serialize_int128;

// ---- 128-bit unsigned arithmetic over the Int128 lanes ---------------------
// STANDARD.md, fixed: the raw range lives in the unsigned domain of the
// storage width, exactly as int128's bounds do.

fn fixed_u128_from_i64(value: i64) -> Int128 {
    let hi: u64 = if value < 0 { 0xffffffffffffffff } else { 0 };
    return Int128 { lo: value as! u64, hi: hi };
}

fn fixed_u128_sub(a: Int128, b: Int128) -> Int128 {
    let lo = a.lo - b.lo;
    let borrow: u64 = if a.lo < b.lo { 1 } else { 0 };
    return Int128 { lo: lo, hi: a.hi - b.hi - borrow };
}

fn fixed_u128_add(a: Int128, b: Int128) -> Int128 {
    let lo = a.lo + b.lo;
    let carry: u64 = if lo < a.lo { 1 } else { 0 };
    return Int128 { lo: lo, hi: a.hi + b.hi + carry };
}

fn fixed_u128_compare(a: Int128, b: Int128) -> i64 {
    if a.hi != b.hi { return if a.hi < b.hi { -1 } else { 1 }; }
    if a.lo != b.lo { return if a.lo < b.lo { -1 } else { 1 }; }
    return 0;
}

fn fixed_bit_length64(value: u64) -> i64 {
    var v = value;
    var bits: i64 = 0;
    while v != 0 {
        bits += 1;
        v = v >> 1;
    }
    return bits;
}

fn fixed_u128_bit_length(value: Int128) -> i64 {
    if value.hi != 0 { return 64 + fixed_bit_length64(value.hi); }
    return fixed_bit_length64(value.lo);
}

// min << fraction_bits in the 128-bit domain (serialize.c serialize_raw_bound).
fn fixed_raw_bound(whole: i64, fraction_bits: i64) -> Int128 {
    var u = fixed_u128_from_i64(whole);
    for i in fraction_bits {
        let carry = u.lo >> 63;
        u = Int128 { lo: u.lo << 1, hi: (u.hi << 1) | carry };
    }
    return u;
}

fn fixed_u128_group32(value: Int128, group: i64) -> u32 {
    if group == 0 { return (value.lo & 0xffffffff) as! u32; }
    if group == 1 { return (value.lo >> 32) as! u32; }
    if group == 2 { return (value.hi & 0xffffffff) as! u32; }
    return (value.hi >> 32) as! u32;
}

fn fixed_u128_set_group32(value: Int128&, group: i64, g: u32) {
    let wide: u64 = g;
    if group == 0 { value.lo = value.lo | wide; }
    else if group == 1 { value.lo = value.lo | (wide << 32); }
    else if group == 2 { value.hi = value.hi | wide; }
    else { value.hi = value.hi | (wide << 32); }
}

// STANDARD.md, fixed: raw_value - raw_min in `bits` bits, split into 32-bit
// groups from least significant upward exactly as serialize_bits splits wide
// values.
fn fixed_write_bits(s: WriteStream&, value: Int128, bits: i64) -> bool {
    var written: i64 = 0;
    var group: i64 = 0;
    while written < bits {
        var chunk = bits - written;
        if chunk > 32 { chunk = 32; }
        guard serialize_bits(s, fixed_u128_group32(value, group), chunk) else { return false; }
        written += chunk;
        group += 1;
    }
    return true;
}

fn fixed_read_bits(s: ReadStream&, value: Int128&, bits: i64) -> bool {
    var read: i64 = 0;
    var group: i64 = 0;
    value = Int128 { lo: 0, hi: 0 };
    while read < bits {
        var chunk = bits - read;
        if chunk > 32 { chunk = 32; }
        var g: u32 = 0;
        guard serialize_bits(s, g, chunk) else { return false; }
        fixed_u128_set_group32(value, group, g);
        read += chunk;
        group += 1;
    }
    return true;
}

fn fixed_check_format(integer_bits: i64, fraction_bits: i64, width: i64) {
    if integer_bits < 1 || fraction_bits < 0 || integer_bits + fraction_bits != width {
        abort("serialize: integer_bits + fraction_bits must equal the storage width");
    }
}

// fixed32 carries the 8-, 16- and 32-bit storage widths (STANDARD.md, fixed).
fn fixed_check_format32(integer_bits: i64, fraction_bits: i64) {
    if integer_bits < 1 || fraction_bits < 0
        || (integer_bits + fraction_bits != 8 && integer_bits + fraction_bits != 16
            && integer_bits + fraction_bits != 32) {
        abort("serialize: integer_bits + fraction_bits must equal the storage width");
    }
}

fn fixed_write_core(s: WriteStream&, raw_value: Int128, raw_min: Int128, raw_max: Int128) -> bool {
    let span = fixed_u128_sub(raw_max, raw_min);
    let bits = fixed_u128_bit_length(span);
    let offset = fixed_u128_sub(raw_value, raw_min);
    if bits == 0 {
        guard fixed_u128_compare(raw_value, raw_min) == 0
            else { return serialize_fail(s, SERIALIZE_ERROR_VALUE_OUT_OF_RANGE); }
        guard s.error == SERIALIZE_ERROR_NONE else { return false; }
        return true;
    }
    if fixed_u128_compare(offset, span) > 0 {
        return serialize_fail(s, SERIALIZE_ERROR_VALUE_OUT_OF_RANGE);
    }
    guard serialize_write_check(s, bits) else { return false; }
    return fixed_write_bits(s, offset, bits);
}

fn fixed_read_core(s: ReadStream&, raw_value: Int128&, raw_min: Int128, raw_max: Int128) -> bool {
    guard s.error == SERIALIZE_ERROR_NONE else { return false; }
    let span = fixed_u128_sub(raw_max, raw_min);
    let bits = fixed_u128_bit_length(span);
    if bits == 0 {
        raw_value = raw_min;
        return true;
    }
    guard serialize_read_check(s, bits) else { return false; }
    let start = s.bits_read;
    var offset = Int128 { lo: 0, hi: 0 };
    guard fixed_read_bits(s, offset, bits) else { return false; }
    if fixed_u128_compare(offset, span) > 0 {
        s.bits_read = start;
        return serialize_fail(s, SERIALIZE_ERROR_VALUE_OUT_OF_RANGE);
    }
    raw_value = fixed_u128_add(raw_min, offset);
    return true;
}

fn fixed_measure_core(s: MeasureStream&, raw_min: Int128, raw_max: Int128) -> bool {
    guard serialize_measure_add(s, fixed_u128_bit_length(fixed_u128_sub(raw_max, raw_min))) else { return false; }
    return true;
}

// ---- fixed32 ---------------------------------------------------------------

fn serialize_fixed32(s: WriteStream&, value: i32, integer_bits: i64, fraction_bits: i64, min: i32, max: i32) -> bool {
    fixed_check_format32(integer_bits, fraction_bits);
    if min > max { abort("serialize: min must not be greater than max"); }
    return fixed_write_core(s, fixed_u128_from_i64(value as i64), fixed_raw_bound(min as i64, fraction_bits),
                            fixed_raw_bound(max as i64, fraction_bits));
}
fn serialize_fixed32(s: ReadStream&, value: i32&, integer_bits: i64, fraction_bits: i64, min: i32, max: i32) -> bool {
    fixed_check_format32(integer_bits, fraction_bits);
    if min > max { abort("serialize: min must not be greater than max"); }
    var raw = Int128 { lo: 0, hi: 0 };
    guard fixed_read_core(s, raw, fixed_raw_bound(min as i64, fraction_bits), fixed_raw_bound(max as i64, fraction_bits)) else { return false; }
    value = raw.lo as! i32;
    return true;
}
fn serialize_fixed32(s: MeasureStream&, value: i32, integer_bits: i64, fraction_bits: i64, min: i32, max: i32) -> bool {
    fixed_check_format32(integer_bits, fraction_bits);
    if min > max { abort("serialize: min must not be greater than max"); }
    return fixed_measure_core(s, fixed_raw_bound(min as i64, fraction_bits), fixed_raw_bound(max as i64, fraction_bits));
}

// ---- fixed64 ---------------------------------------------------------------

fn serialize_fixed64(s: WriteStream&, value: i64, integer_bits: i64, fraction_bits: i64, min: i64, max: i64) -> bool {
    fixed_check_format(integer_bits, fraction_bits, 64);
    if min > max { abort("serialize: min must not be greater than max"); }
    return fixed_write_core(s, fixed_u128_from_i64(value), fixed_raw_bound(min, fraction_bits), fixed_raw_bound(max, fraction_bits));
}
fn serialize_fixed64(s: ReadStream&, value: i64&, integer_bits: i64, fraction_bits: i64, min: i64, max: i64) -> bool {
    fixed_check_format(integer_bits, fraction_bits, 64);
    if min > max { abort("serialize: min must not be greater than max"); }
    var raw = Int128 { lo: 0, hi: 0 };
    guard fixed_read_core(s, raw, fixed_raw_bound(min, fraction_bits), fixed_raw_bound(max, fraction_bits)) else { return false; }
    value = raw.lo as! i64;
    return true;
}
fn serialize_fixed64(s: MeasureStream&, value: i64, integer_bits: i64, fraction_bits: i64, min: i64, max: i64) -> bool {
    fixed_check_format(integer_bits, fraction_bits, 64);
    if min > max { abort("serialize: min must not be greater than max"); }
    return fixed_measure_core(s, fixed_raw_bound(min, fraction_bits), fixed_raw_bound(max, fraction_bits));
}

// ---- fixed128 --------------------------------------------------------------
// The value is a raw 128-bit two's-complement integer; min and max are i64
// whole units, scaled to their 128-bit raw bounds (STANDARD.md, fixed).

fn serialize_fixed128(s: WriteStream&, value: Int128, integer_bits: i64, fraction_bits: i64, min: i64, max: i64) -> bool {
    fixed_check_format(integer_bits, fraction_bits, 128);
    if min > max { abort("serialize: min must not be greater than max"); }
    return fixed_write_core(s, value, fixed_raw_bound(min, fraction_bits), fixed_raw_bound(max, fraction_bits));
}
fn serialize_fixed128(s: ReadStream&, value: Int128&, integer_bits: i64, fraction_bits: i64, min: i64, max: i64) -> bool {
    fixed_check_format(integer_bits, fraction_bits, 128);
    if min > max { abort("serialize: min must not be greater than max"); }
    var raw = Int128 { lo: 0, hi: 0 };
    guard fixed_read_core(s, raw, fixed_raw_bound(min, fraction_bits), fixed_raw_bound(max, fraction_bits)) else { return false; }
    value = raw;
    return true;
}
fn serialize_fixed128(s: MeasureStream&, value: Int128, integer_bits: i64, fraction_bits: i64, min: i64, max: i64) -> bool {
    fixed_check_format(integer_bits, fraction_bits, 128);
    if min > max { abort("serialize: min must not be greater than max"); }
    return fixed_measure_core(s, fixed_raw_bound(min, fraction_bits), fixed_raw_bound(max, fraction_bits));
}
