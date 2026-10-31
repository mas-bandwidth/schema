// serialize_int.goose -- ranged integers and int_relative (STANDARD.md, int,
// int64, int_relative; DESIGN.md (a), (c), (e)). Card serialize-goose-integers
// fills this file.

import std;
import .serialize_stream;

// Bits needed for a value in [min,max] in the unsigned domain; 0 when
// min == max (STANDARD.md, int: 32 - count_leading_zeros(max - min), written
// as 64 - clz because the u32 span is widened). Unsigned subtraction wraps,
// so [INT32_MIN, INT32_MAX] is exact.
fn serialize_bits_required(min: u32, max: u32) -> i64 {
    if min == max { return 0; }
    let span: u64 = max - min;
    return 64 - clz(span);
}

// The same rule at 64 bits (STANDARD.md, int).
fn serialize_bits_required64(min: u64, max: u64) -> i64 {
    if min == max { return 0; }
    return 64 - clz(max - min);
}

// int: the offset value - min in bits_required(min, max) bits. On a write
// or a measure min > max and a value outside [min,max] abort (write misuse,
// U6, U9). On a read min > max is the caller's
// bad declaration, refused as VALUE_OUT_OF_RANGE before any bit moves (U16).
// The bounds are i32, so U5's int32 rule holds by type (STANDARD.md, int;
// DESIGN.md (a), (e)).
fn serialize_int(s: WriteStream&, value: i32, min: i32, max: i32) -> bool {
    if min > max { abort("serialize: min must not be greater than max"); }
    if value < min || value > max { abort("serialize: value must be in [min, max]"); }
    let bits = serialize_bits_required(min as! u32, max as! u32);
    if bits == 0 { return true; }
    let offset = (value as! u32) - (min as! u32);
    return serialize_bits(s, offset, bits);
}
fn serialize_int(s: ReadStream&, value: i32&, min: i32, max: i32) -> bool {
    guard min <= max else { return serialize_fail(s, SERIALIZE_ERROR_VALUE_OUT_OF_RANGE); }
    let bits = serialize_bits_required(min as! u32, max as! u32);
    if bits == 0 {
        guard serialize_read_check(s, 0) else { return false; }
        value = min;
        return true;
    }
    var offset: u32 = 0;
    guard serialize_bits(s, offset, bits) else { return false; }
    if offset > (max as! u32) - (min as! u32) {
        return serialize_fail(s, SERIALIZE_ERROR_VALUE_OUT_OF_RANGE);
    }
    value = ((min as! u32) + offset) as! i32;
    return true;
}
fn serialize_int(s: MeasureStream&, value: i32, min: i32, max: i32) -> bool {
    if min > max { abort("serialize: min must not be greater than max"); }
    if value < min || value > max { abort("serialize: value must be in [min, max]"); }
    return serialize_measure_add(s, serialize_bits_required(min as! u32, max as! u32));
}

// int64: the same over 64 bits, low 32 first above 32 bits (STANDARD.md,
// int64; DESIGN.md (c), (e)). The u64 span can exceed the signed domain.
fn serialize_int64(s: WriteStream&, value: i64, min: i64, max: i64) -> bool {
    if min > max { abort("serialize: min must not be greater than max"); }
    if value < min || value > max { abort("serialize: value must be in [min, max]"); }
    let bits = serialize_bits_required64(min as! u64, max as! u64);
    if bits == 0 { return true; }
    let offset = (value as! u64) - (min as! u64);
    return serialize_bits64(s, offset, bits);
}
fn serialize_int64(s: ReadStream&, value: i64&, min: i64, max: i64) -> bool {
    guard min <= max else { return serialize_fail(s, SERIALIZE_ERROR_VALUE_OUT_OF_RANGE); }
    let bits = serialize_bits_required64(min as! u64, max as! u64);
    if bits == 0 {
        guard serialize_read_check(s, 0) else { return false; }
        value = min;
        return true;
    }
    var offset: u64 = 0;
    guard serialize_bits64(s, offset, bits) else { return false; }
    if offset > (max as! u64) - (min as! u64) {
        return serialize_fail(s, SERIALIZE_ERROR_VALUE_OUT_OF_RANGE);
    }
    value = ((min as! u64) + offset) as! i64;
    return true;
}
fn serialize_int64(s: MeasureStream&, value: i64, min: i64, max: i64) -> bool {
    if min > max { abort("serialize: min must not be greater than max"); }
    if value < min || value > max { abort("serialize: value must be in [min, max]"); }
    return serialize_measure_add(s, serialize_bits_required64(min as! u64, max as! u64));
}

// int_relative: the tier ladder over [0, 2^31 - 1], current > previous
// (STANDARD.md, int_relative). previous outside the domain and current not
// above previous are caller error and abort on a write or a measure, as
// serialize.c asserts; on a read a previous outside the domain is refused as
// VALUE_OUT_OF_RANGE before any bit moves (U7, U16); the
// difference is unsigned so --debug cannot trap on it. The absolute tier
// transmits current itself as 32 unsigned raw bits, and a reader refuses it
// as NON_CANONICAL when the difference has a bounded tier. The writer checks
// room for the whole ladder before writing any of it (U10).
fn serialize_int_relative(s: WriteStream&, previous: i32, current: i32) -> bool {
    if previous < 0 { abort("serialize: int_relative previous must be in [0, 2147483647]"); }
    if current <= previous { abort("serialize: int_relative current must be greater than previous"); }
    let difference = (current as! u32) - (previous as! u32);
    serialize_write_check(s, serialize_int_relative_bits(difference));
    if !serialize_bool(s, difference == 1) { return false; }
    if difference == 1 { return true; }
    if !serialize_bool(s, difference <= 6) { return false; }
    if difference <= 6 { return serialize_int(s, difference as! i32, 2, 6); }
    if !serialize_bool(s, difference <= 23) { return false; }
    if difference <= 23 { return serialize_int(s, difference as! i32, 7, 23); }
    if !serialize_bool(s, difference <= 280) { return false; }
    if difference <= 280 { return serialize_int(s, difference as! i32, 24, 280); }
    if !serialize_bool(s, difference <= 4377) { return false; }
    if difference <= 4377 { return serialize_int(s, difference as! i32, 281, 4377); }
    if !serialize_bool(s, difference <= 69914) { return false; }
    if difference <= 69914 { return serialize_int(s, difference as! i32, 4378, 69914); }
    return serialize_bits(s, current as! u32, 32);
}
fn serialize_int_relative(s: ReadStream&, previous: i32, current: i32&) -> bool {
    guard s.error == SERIALIZE_ERROR_NONE else { return false; }
    guard previous >= 0 else { return serialize_fail(s, SERIALIZE_ERROR_VALUE_OUT_OF_RANGE); }
    var flag = false;
    var difference: i32 = 0;
    var reconstructed: u32 = 0;
    var absolute = false;
    if !serialize_bool(s, flag) { return false; }
    if flag {
        reconstructed = (previous as! u32) + 1;
    } else {
        if !serialize_bool(s, flag) { return false; }
        if flag {
            if !serialize_int(s, difference, 2, 6) { return false; }
            reconstructed = (previous as! u32) + (difference as! u32);
        } else {
            if !serialize_bool(s, flag) { return false; }
            if flag {
                if !serialize_int(s, difference, 7, 23) { return false; }
                reconstructed = (previous as! u32) + (difference as! u32);
            } else {
                if !serialize_bool(s, flag) { return false; }
                if flag {
                    if !serialize_int(s, difference, 24, 280) { return false; }
                    reconstructed = (previous as! u32) + (difference as! u32);
                } else {
                    if !serialize_bool(s, flag) { return false; }
                    if flag {
                        if !serialize_int(s, difference, 281, 4377) { return false; }
                        reconstructed = (previous as! u32) + (difference as! u32);
                    } else {
                        if !serialize_bool(s, flag) { return false; }
                        if flag {
                            if !serialize_int(s, difference, 4378, 69914) { return false; }
                            reconstructed = (previous as! u32) + (difference as! u32);
                        } else {
                            if !serialize_bits(s, reconstructed, 32) { return false; }
                            absolute = true;
                        }
                    }
                }
            }
        }
    }
    if reconstructed > 0x7FFFFFFF || reconstructed <= (previous as! u32) {
        return serialize_fail(s, SERIALIZE_ERROR_VALUE_OUT_OF_RANGE);
    }
    // The absolute tier is canonical too: a current in the domain, above
    // previous and no more than 69914 above it has a bounded encoding, so the
    // absolute one is refused as NON_CANONICAL. The domain rule above runs
    // first, and the difference is taken only once current > previous holds,
    // so it cannot wrap (STANDARD.md, int_relative).
    if absolute && reconstructed - (previous as! u32) <= 69914 {
        return serialize_fail(s, SERIALIZE_ERROR_NON_CANONICAL);
    }
    current = reconstructed as! i32;
    return true;
}
fn serialize_int_relative(s: MeasureStream&, previous: i32, current: i32) -> bool {
    if previous < 0 { abort("serialize: int_relative previous must be in [0, 2147483647]"); }
    if current <= previous { abort("serialize: int_relative current must be greater than previous"); }
    return serialize_measure_add(s, serialize_int_relative_bits((current as! u32) - (previous as! u32)));
}

// The ladder's width for a difference of at least 1: the write's room check
// and the measure.
fn serialize_int_relative_bits(difference: u32) -> i64 {
    var bits: i64 = 1;
    if difference != 1 {
        bits += 1;
        if difference <= 6 {
            bits += 3;
        } else {
            bits += 1;
            if difference <= 23 {
                bits += 5;
            } else {
                bits += 1;
                if difference <= 280 {
                    bits += 9;
                } else {
                    bits += 1;
                    if difference <= 4377 {
                        bits += 13;
                    } else {
                        bits += 1;
                        if difference <= 69914 {
                            bits += 17;
                        } else {
                            bits += 32;
                        }
                    }
                }
            }
        }
    }
    return bits;
}
