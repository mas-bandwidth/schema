// serialize_compressed_float.goose -- quantized floats (STANDARD.md,
// compressed_float; DESIGN.md (a), (c)). The arithmetic is f32 throughout, in
// serialize.c's order; a non-finite value on write aborts.

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

// The constants derived once (serialize.c serialize_compressed_float_params):
// max_integer_value, bits, delta (STANDARD.md, compressed_float).
fn serialize_compressed_float_params(min: f32, max: f32, res: f32) -> u32, i64, f32 {
    if !(min < max) { abort("serialize: compressed float min must be less than max"); }
    if !(res > 0.0) { abort("serialize: compressed float res must be greater than zero"); }
    let delta: f32 = max - min;
    var values: f32 = delta / res;
    if !cf_finite(delta) || !cf_finite(values) {
        abort("serialize: compressed float declaration must be finite");
    }
    if !(values >= 1.0) {
        values = 1.0;
    } else if values > 4294967040.0 {
        values = 4294967040.0;
    }
    var max_integer_value: u32 = values as! u32;
    if (max_integer_value as f32) < values {
        max_integer_value += 1;
    }
    return max_integer_value, cf_bits_required(max_integer_value), delta;
}

fn serialize_compressed_float(s: WriteStream&, value: f32, min: f32, max: f32, res: f32) -> bool {
    let max_integer_value, bits, delta = serialize_compressed_float_params(min, max, res);
    return serialize_compressed_float_precomputed(s, value, max_integer_value, bits, delta, min);
}
fn serialize_compressed_float(s: ReadStream&, value: f32&, min: f32, max: f32, res: f32) -> bool {
    let max_integer_value, bits, delta = serialize_compressed_float_params(min, max, res);
    return serialize_compressed_float_precomputed(s, value, max_integer_value, bits, delta, min);
}
fn serialize_compressed_float(s: MeasureStream&, value: f32, min: f32, max: f32, res: f32) -> bool {
    let max_integer_value, bits, delta = serialize_compressed_float_params(min, max, res);
    return serialize_measure_add(s, bits);
}

// The same wire from the precomputed constants (mas-bandwidth/schema#82).
fn serialize_compressed_float_precomputed(s: WriteStream&, value: f32, max_integer_value: u32, bits: i64, delta: f32, min: f32) -> bool {
    if max_integer_value < 1 { abort("serialize: compressed float max_integer_value must be at least 1"); }
    if bits != cf_bits_required(max_integer_value) { abort("serialize: compressed float bits must match max_integer_value"); }
    if !(delta > 0.0) || !cf_finite(delta) { abort("serialize: compressed float delta must be positive and finite"); }
    if !cf_finite(value) { abort("serialize: compressed float value must be finite"); }

    var normalized: f32 = (value - min) / delta;
    if !(normalized >= 0.0) {
        normalized = 0.0;
    } else if !(normalized <= 1.0) {
        normalized = 1.0;
    }

    let scaled: f32 = normalized * (max_integer_value as f32);
    var integer_value: u32 = (scaled + 0.5) as! u32;
    if integer_value > max_integer_value {
        integer_value = max_integer_value;
    }
    return serialize_bits(s, integer_value, bits);
}

fn serialize_compressed_float_precomputed(s: ReadStream&, value: f32&, max_integer_value: u32, bits: i64, delta: f32, min: f32) -> bool {
    if max_integer_value < 1 { abort("serialize: compressed float max_integer_value must be at least 1"); }
    if bits != cf_bits_required(max_integer_value) { abort("serialize: compressed float bits must match max_integer_value"); }
    if !(delta > 0.0) || !cf_finite(delta) { abort("serialize: compressed float delta must be positive and finite"); }

    var integer_value: u32 = 0;
    guard serialize_bits(s, integer_value, bits) else { return false; }
    if integer_value > max_integer_value {
        return serialize_fail(s, SERIALIZE_ERROR_VALUE_OUT_OF_RANGE);
    }

    let normalized: f32 = (integer_value as f32) / (max_integer_value as f32);
    let scaled: f32 = normalized * delta;
    value = scaled + min;
    return true;
}

fn serialize_compressed_float_precomputed(s: MeasureStream&, value: f32, max_integer_value: u32, bits: i64, delta: f32, min: f32) -> bool {
    if bits < 1 || bits > 32 { abort("serialize: compressed float bits must be in [1,32]"); }
    return serialize_measure_add(s, bits);
}
