// serialize_compressed_float.lobster — compressed_float, its precomputed form
// and the parameter derivation (STANDARD.md, Floating Point, compressed_float;
// DESIGN.md (c), (f)).
//
// The arithmetic is float32 with every step rounded, in serialize.c v2.0.0's
// order (STANDARD.md, compressed_float): the product rounds before 0.5 is
// added and that sum rounds before the floor; the decode rounds the quotient,
// the product before min is added, and the sum.
import .serialize_stream
import .serialize_bitpacker
import .serialize_float

let serialize_compressed_float_values_high = 4294967040.0

def serialize_compressed_float_is_finite(x:float) -> bool:
    return x - x == 0.0

def serialize_compressed_float_abort_params():
    serialize_abort("serialize: compressed float requires min < max and resolution > 0")

def serialize_compressed_float_abort_nonfinite():
    serialize_abort("serialize: compressed float declaration must be finite")

def serialize_compressed_float_abort_top():
    serialize_abort("serialize: compressed float declaration must decode its top integer finite (min + delta)")

// The write path's declaration check (STANDARD.md, compressed_float: "the
// decode of max_integer_value (min + delta, rounded once) finite in float32").
// Asserted on write only; a reader refuses the non-finite decode instead.
def serialize_compressed_float_check_top(delta:float, min:float):
    let top = serialize_float32_round(serialize_float32_round(min) + delta)
    if not serialize_compressed_float_is_finite(top):
        serialize_compressed_float_abort_top()

def serialize_compressed_float_abort_value():
    serialize_abort("serialize: compressed float value must be finite")

def serialize_compressed_float_abort_precomputed():
    serialize_abort("serialize: precomputed compressed float constants must be exactly what serialize_compressed_float_params derives")

// The derivation (STANDARD.md, compressed_float; serialize.c
// serialize_compressed_float_params): delta = max - min, values = delta / res
// clamped to [1, 4294967040], max_integer_value = ceil(values), bits =
// bits_required(0, max_integer_value). Each parameter is a float32. The first
// return is 0 for a good declaration, 1 for min >= max or res <= 0, and 2 for a
// non-finite delta or value count: a code, so a read can refuse it
// (IMPLEMENTING.md U7, U16) where a write or a measure aborts.
def serialize_compressed_float_derive(min:float, max:float, res:float):
    let fmin = serialize_float32_round(min)
    let fmax = serialize_float32_round(max)
    let fres = serialize_float32_round(res)
    if not (fmin < fmax) or not (fres > 0.0):
        return 1, 0, 0, 0.0
    let delta = serialize_float32_round(fmax - fmin)
    if not serialize_compressed_float_is_finite(delta):
        return 2, 0, 0, 0.0
    var values = serialize_float32_round(delta / fres)
    if not serialize_compressed_float_is_finite(values):
        return 2, 0, 0, 0.0
    if not (values >= 1.0):
        values = 1.0
    elif values > serialize_compressed_float_values_high:
        values = serialize_compressed_float_values_high
    let max_integer_value = ceiling(values)
    return 0, max_integer_value, serialize_bits_required(0, max_integer_value), delta

// serialize_compressed_float_params derives max_integer_value, bits and delta
// from a declaration; min >= max, res <= 0 and a non-finite delta or value
// count are caller error (DESIGN.md (e)).
def serialize_compressed_float_params(min:float, max:float, res:float):
    let code, max_integer_value, bits, delta = serialize_compressed_float_derive(min, max, res)
    if code == 1:
        serialize_compressed_float_abort_params()
    if code == 2:
        serialize_compressed_float_abort_nonfinite()
    return max_integer_value, bits, delta

// Precomputed constants are checked for what can be checked without the
// declaration they came from (IMPLEMENTING.md U16): max_integer_value lies in
// [1, 4294967040] (the derivation's clamp, so it is a uint32 and
// bits_required, which works in the uint32 domain, cannot mask it), bits is
// bits_required(0, max_integer_value), and delta is positive and finite.
def serialize_compressed_float_precomputed_ok(max_integer_value:int, bits:int, delta:float) -> bool:
    return max_integer_value >= 1 and max_integer_value <= 4294967040 and bits == serialize_bits_required(0, max_integer_value) and delta > 0.0 and serialize_compressed_float_is_finite(delta)

// The write and measure check (DESIGN.md (e); serialize.c's asserts in the
// precomputed entry points): bad constants abort.
def serialize_compressed_float_validate_precomputed(max_integer_value:int, bits:int, delta:float):
    if not serialize_compressed_float_precomputed_ok(max_integer_value, bits, delta):
        serialize_compressed_float_abort_precomputed()

// The write-side quantization (STANDARD.md, compressed_float; serialize.c
// serialize_write_compressed_float_precomputed): clamp (value - min) / delta
// to [0,1], multiply by max_integer_value, add 0.5, floor, then clamp the
// integer to max_integer_value. The whole width is gated before any bit moves.
def serialize_compressed_float_write_precomputed(s:serialize_write_stream, value:float, max_integer_value:int, bits:int, delta:float, min:float):
    let fmin = serialize_float32_round(min)
    let fvalue = serialize_float32_round(value)
    var normalized = serialize_float32_round(serialize_float32_round(fvalue - fmin) / delta)
    if not (normalized >= 0.0):
        normalized = 0.0
    elif not (normalized <= 1.0):
        normalized = 1.0
    let scaled = serialize_float32_round(normalized * serialize_float32_round(float(max_integer_value)))
    var integer_value = floor(serialize_float32_round(scaled + 0.5))
    if integer_value > max_integer_value:
        integer_value = max_integer_value
    if not s.write_gate(bits):
        return value, false
    s.pack_bits(integer_value, bits)
    return value, true

// The read-side reconstruction (STANDARD.md, compressed_float; serialize.c
// serialize_read_compressed_float_precomputed): reject an integer greater than
// max_integer_value, then divide, multiply and add, every step float32.
def serialize_compressed_float_read_precomputed(s:serialize_read_stream, value:float, max_integer_value:int, bits:int, delta:float, min:float):
    let fmin = serialize_float32_round(min)
    if s.failed:
        return value, false
    if not s.read_gate(bits):
        return value, false
    let integer_value = s.unpack_bits(bits)
    if integer_value > max_integer_value:
        return value, s.fail(serialize_refusal_value_out_of_range)
    let normalized = serialize_float32_round(float(integer_value) / float(max_integer_value))
    let scaled = serialize_float32_round(normalized * delta)
    let decoded = serialize_float32_round(scaled + fmin)
    // The decode must be finite: every operand is finite and the integer is in
    // range, and still the final add can round past FLT_MAX. A non-finite
    // decode is refused whatever the declaration, destination untouched
    // (STANDARD.md, compressed_float, "The decode must be finite").
    let decoded_bits = serialize_float32_bits(decoded)
    if not serialize_float32_bits_are_finite(decoded_bits):
        return value, s.fail(serialize_refusal_non_finite_float)
    return decoded, true

def serialize_compressed_float(s:serialize_write_stream, value:float, min:float, max:float, res:float):
    let max_integer_value, bits, delta = serialize_compressed_float_params(min, max, res)
    serialize_compressed_float_check_top(delta, min)
    let fvalue = serialize_float32_round(value)
    if not serialize_compressed_float_is_finite(fvalue):
        serialize_compressed_float_abort_value()
    let _, ok = serialize_compressed_float_write_precomputed(s, fvalue, max_integer_value, bits, delta, min)
    return value, ok

def serialize_compressed_float(s:serialize_read_stream, value:float, min:float, max:float, res:float):
    // A bad declaration on a read is refused, never an abort (IMPLEMENTING.md U7, U16).
    let code, max_integer_value, bits, delta = serialize_compressed_float_derive(min, max, res)
    if code != 0:
        return value, s.serialize_read_misuse()
    let decoded, ok = serialize_compressed_float_read_precomputed(s, value, max_integer_value, bits, delta, min)
    return decoded, ok

// A measure makes every check the write makes (IMPLEMENTING.md U6).
def serialize_compressed_float(s:serialize_measure_stream, value:float, min:float, max:float, res:float):
    let _, bits, delta = serialize_compressed_float_params(min, max, res)
    serialize_compressed_float_check_top(delta, min)
    if not serialize_compressed_float_is_finite(serialize_float32_round(value)):
        serialize_compressed_float_abort_value()
    s.measure_bits(bits)
    return value, true

def serialize_compressed_float_precomputed(s:serialize_write_stream, value:float, max_integer_value:int, bits:int, delta:float, min:float):
    serialize_compressed_float_validate_precomputed(max_integer_value, bits, delta)
    serialize_compressed_float_check_top(delta, min)
    let fvalue = serialize_float32_round(value)
    if not serialize_compressed_float_is_finite(fvalue):
        serialize_compressed_float_abort_value()
    let _, ok = serialize_compressed_float_write_precomputed(s, fvalue, max_integer_value, bits, delta, min)
    return value, ok

def serialize_compressed_float_precomputed(s:serialize_read_stream, value:float, max_integer_value:int, bits:int, delta:float, min:float):
    // Bad constants on a read are refused, never an abort (IMPLEMENTING.md U7, U16).
    if not serialize_compressed_float_precomputed_ok(max_integer_value, bits, delta):
        return value, s.serialize_read_misuse()
    let decoded, ok = serialize_compressed_float_read_precomputed(s, value, max_integer_value, bits, delta, min)
    return decoded, ok

def serialize_compressed_float_precomputed(s:serialize_measure_stream, value:float, max_integer_value:int, bits:int, delta:float, min:float):
    serialize_compressed_float_validate_precomputed(max_integer_value, bits, delta)
    serialize_compressed_float_check_top(delta, min)
    if not serialize_compressed_float_is_finite(serialize_float32_round(value)):
        serialize_compressed_float_abort_value()
    s.measure_bits(bits)
    return value, true
