// serialize_compressed_float.goose -- quantized floats (STANDARD.md,
// compressed_float; DESIGN.md (a), (c)). The arithmetic is f32 throughout, in
// serialize.c's order; a non-finite value on write aborts, and a decode that
// is not finite is refused as NON_FINITE_FLOAT (STANDARD.md, compressed_float,
// The decode must be finite). A bad declaration (min, max, res, or the
// precomputed constants) aborts on a write or a measure and is refused as
// VALUE_OUT_OF_RANGE on a read, before any bit moves (U16).

import .serialize_stream;
import .serialize_float;

fn cf_finite(x: f32) -> bool {
    return x == x && x <= 3.4028235e38 && x >= -3.4028235e38;
}

fn cf_bits_required(max: u32) -> i64 {
    if max == 0 { return 0; }
    var v = max;
    var bits: i64 = 0;
    while v != 0 {
        bits += 1;
        v = v >> 1;
    }
    return bits;
}

// The largest max_integer_value: the largest float32 below 2^32, so every
// integer value is a uint32 (STANDARD.md, compressed_float; U16).
let CF_MAX_INTEGER_VALUE: u32 = 4294967040;


// The precomputed constants, checked for what can be checked without the
// declaration they came from (U16): max_integer_value in [1, 4294967040],
// bits == bits_required(0, max_integer_value), delta positive and finite.
fn cf_precomputed_ok(max_integer_value: u32, bits: i64, delta: f32) -> bool {
    return max_integer_value >= 1 && max_integer_value <= CF_MAX_INTEGER_VALUE
        && bits == cf_bits_required(max_integer_value) && delta > 0.0 && cf_finite(delta);
}

// A write's or a measure's precomputed constants: misuse aborts, naming the rule.
fn cf_check_precomputed(max_integer_value: u32, bits: i64, delta: f32) {
    if max_integer_value < 1 || max_integer_value > CF_MAX_INTEGER_VALUE {
        abort("serialize: compressed float max_integer_value must be in [1, 4294967040]");
    }
    if bits != cf_bits_required(max_integer_value) { abort("serialize: compressed float bits must match max_integer_value"); }
    if !(delta > 0.0) || !cf_finite(delta) { abort("serialize: compressed float delta must be positive and finite"); }
}

// The derivation, and whether the declaration is good: min < max, res > 0,
// and delta and delta / res finite in float32. Each is computed once.
fn cf_params(min: f32, max: f32, res: f32) -> bool, u32, i64, f32 {
    if !(min < max) || !(res > 0.0) { return false, 0, 0, 0.0; }
    let delta: f32 = max - min;
    var values: f32 = delta / res;
    if !cf_finite(delta) || !cf_finite(values) { return false, 0, 0, delta; }
    if !(values >= 1.0) {
        values = 1.0;
    } else if values > 4294967040.0 {
        values = 4294967040.0;
    }
    var max_integer_value: u32 = values as! u32;
    if max_integer_value < values {
        max_integer_value += 1;
    }
    return true, max_integer_value, cf_bits_required(max_integer_value), delta;
}

// The constants derived once (serialize.c serialize_compressed_float_params):
// max_integer_value, bits, delta (STANDARD.md, compressed_float). A bad
// declaration aborts: the helper is the caller's, like a write.
fn serialize_compressed_float_params(min: f32, max: f32, res: f32) -> u32, i64, f32 {
    if !(min < max) { abort("serialize: compressed float min must be less than max"); }
    if !(res > 0.0) { abort("serialize: compressed float res must be greater than zero"); }
    let ok, max_integer_value, bits, delta = cf_params(min, max, res);
    if !ok { abort("serialize: compressed float declaration must be finite"); }
    return max_integer_value, bits, delta;
}

fn serialize_compressed_float(s: WriteStream&, value: f32, min: f32, max: f32, res: f32) -> bool {
    let max_integer_value, bits, delta = serialize_compressed_float_params(min, max, res);
    return serialize_compressed_float_precomputed(s, value, max_integer_value, bits, delta, min);
}
fn serialize_compressed_float(s: ReadStream&, value: f32&, min: f32, max: f32, res: f32) -> bool {
    let ok, max_integer_value, bits, delta = cf_params(min, max, res);
    guard ok else { return serialize_fail(s, SERIALIZE_ERROR_VALUE_OUT_OF_RANGE); }
    return serialize_compressed_float_precomputed(s, value, max_integer_value, bits, delta, min);
}
fn serialize_compressed_float(s: MeasureStream&, value: f32, min: f32, max: f32, res: f32) -> bool {
    let max_integer_value, bits, delta = serialize_compressed_float_params(min, max, res);
    return serialize_compressed_float_precomputed(s, value, max_integer_value, bits, delta, min);
}

// A write's or a measure's checks, in order: the constants, the value, and
// the declaration's top decode, min + delta rounded once, which must be
// finite (STANDARD.md, compressed_float): checked on the write side only, a
// reader refuses its non-finite decode at runtime. A non-finite value aborts
// and is never clamped (U9).
fn cf_check_write(value: f32, max_integer_value: u32, bits: i64, delta: f32, min: f32) {
    cf_check_precomputed(max_integer_value, bits, delta);
    if !cf_finite(value) { abort("serialize: compressed float value must be finite"); }
    let top: f32 = min + delta;
    if !cf_finite(top) { abort("serialize: compressed float declaration must decode its top to a finite value"); }
}

// The same wire from the precomputed constants (mas-bandwidth/schema#82).
fn serialize_compressed_float_precomputed(s: WriteStream&, value: f32, max_integer_value: u32, bits: i64, delta: f32, min: f32) -> bool {
    cf_check_write(value, max_integer_value, bits, delta, min);

    var normalized: f32 = (value - min) / delta;
    if !(normalized >= 0.0) {
        normalized = 0.0;
    } else if !(normalized <= 1.0) {
        normalized = 1.0;
    }

    let scaled: f32 = normalized * max_integer_value;
    var integer_value: u32 = (scaled + 0.5) as! u32;
    if integer_value > max_integer_value {
        integer_value = max_integer_value;
    }
    return serialize_bits(s, integer_value, bits);
}

fn serialize_compressed_float_precomputed(s: ReadStream&, value: f32&, max_integer_value: u32, bits: i64, delta: f32, min: f32) -> bool {
    guard cf_precomputed_ok(max_integer_value, bits, delta) else { return serialize_fail(s, SERIALIZE_ERROR_VALUE_OUT_OF_RANGE); }

    var integer_value: u32 = 0;
    guard serialize_bits(s, integer_value, bits) else { return false; }
    if integer_value > max_integer_value {
        return serialize_fail(s, SERIALIZE_ERROR_VALUE_OUT_OF_RANGE);
    }

    let normalized: f32 = (integer_value as f32) / max_integer_value;
    let scaled: f32 = normalized * delta;
    let decoded: f32 = scaled + min;
    // Every operand is finite and still the add can round past FLT_MAX: the
    // decode is refused, the destination untouched (STANDARD.md,
    // compressed_float, The decode must be finite).
    guard serialize_float_bits_are_finite(serialize_f32_bits(decoded)) else {
        return serialize_fail(s, SERIALIZE_ERROR_NON_FINITE_FLOAT);
    }
    value = decoded;
    return true;
}

// The measure makes every check the writer makes (U6), so a mismatched
// declaration never measures short (audit ledger L, 2026-10-27) and a
// non-finite value aborts there too.
fn serialize_compressed_float_precomputed(s: MeasureStream&, value: f32, max_integer_value: u32, bits: i64, delta: f32, min: f32) -> bool {
    cf_check_write(value, max_integer_value, bits, delta, min);
    return serialize_measure_add(s, bits);
}
