// serialize_compressed_float.lobster — compressed_float, its precomputed form
// and the parameter derivation (STANDARD.md, Floating Point, compressed_float;
// DESIGN.md (c), (f)).
//
// The arithmetic is float32 with every step rounded, in serialize.c v1.11.2's
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

def serialize_compressed_float_abort_value():
    serialize_abort("serialize: compressed float value must be finite")

def serialize_compressed_float_abort_precomputed():
    serialize_abort("serialize: precomputed compressed float constants must be exactly what serialize_compressed_float_params derives")

// serialize_compressed_float_params derives max_integer_value, bits and delta
// from a declaration (STANDARD.md, compressed_float; serialize.c
// serialize_compressed_float_params): delta = max - min, values = delta / res
// clamped to [1, 4294967040], max_integer_value = ceil(values), bits =
// bits_required(0, max_integer_value). Each parameter is a float32, and min >=
// max or res <= 0 is caller error (DESIGN.md (e)).
def serialize_compressed_float_params(min:float, max:float, res:float):
    let fmin = serialize_float32_round(min)
    let fmax = serialize_float32_round(max)
    let fres = serialize_float32_round(res)
    if not (fmin < fmax) or not (fres > 0.0):
        serialize_compressed_float_abort_params()
    let delta = serialize_float32_round(fmax - fmin)
    if not serialize_compressed_float_is_finite(delta):
        serialize_compressed_float_abort_nonfinite()
    var values = serialize_float32_round(delta / fres)
    if not serialize_compressed_float_is_finite(values):
        serialize_compressed_float_abort_nonfinite()
    if not (values >= 1.0):
        values = 1.0
    elif values > serialize_compressed_float_values_high:
        values = serialize_compressed_float_values_high
    let max_integer_value = ceiling(values)
    return max_integer_value, serialize_bits_required(0, max_integer_value), delta

// Precomputed constants must be exactly what the derivation produces
// (DESIGN.md (e); serialize.c's asserts in the precomputed entry points).
def serialize_compressed_float_validate_precomputed(max_integer_value:int, bits:int, delta:float):
    if max_integer_value < 1 or bits != serialize_bits_required(0, max_integer_value) or not (delta > 0.0) or not serialize_compressed_float_is_finite(delta):
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
    if s.failed:
        return value, false
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
        return value, s.fail()
    let normalized = serialize_float32_round(float(integer_value) / float(max_integer_value))
    let scaled = serialize_float32_round(normalized * delta)
    return serialize_float32_round(scaled + fmin), true

def serialize_compressed_float(s:serialize_write_stream, value:float, min:float, max:float, res:float):
    let max_integer_value, bits, delta = serialize_compressed_float_params(min, max, res)
    let fvalue = serialize_float32_round(value)
    if not serialize_compressed_float_is_finite(fvalue):
        serialize_compressed_float_abort_value()
    let _, ok = serialize_compressed_float_write_precomputed(s, fvalue, max_integer_value, bits, delta, min)
    return value, ok

def serialize_compressed_float(s:serialize_read_stream, value:float, min:float, max:float, res:float):
    let max_integer_value, bits, delta = serialize_compressed_float_params(min, max, res)
    let decoded, ok = serialize_compressed_float_read_precomputed(s, value, max_integer_value, bits, delta, min)
    return decoded, ok

def serialize_compressed_float(s:serialize_measure_stream, value:float, min:float, max:float, res:float):
    let max_integer_value, bits, delta = serialize_compressed_float_params(min, max, res)
    if s.failed:
        return value, false
    s.measure_bits(bits)
    return value, true

def serialize_compressed_float_precomputed(s:serialize_write_stream, value:float, max_integer_value:int, bits:int, delta:float, min:float):
    serialize_compressed_float_validate_precomputed(max_integer_value, bits, delta)
    let fvalue = serialize_float32_round(value)
    if not serialize_compressed_float_is_finite(fvalue):
        serialize_compressed_float_abort_value()
    let _, ok = serialize_compressed_float_write_precomputed(s, fvalue, max_integer_value, bits, delta, min)
    return value, ok

def serialize_compressed_float_precomputed(s:serialize_read_stream, value:float, max_integer_value:int, bits:int, delta:float, min:float):
    serialize_compressed_float_validate_precomputed(max_integer_value, bits, delta)
    let decoded, ok = serialize_compressed_float_read_precomputed(s, value, max_integer_value, bits, delta, min)
    return decoded, ok

def serialize_compressed_float_precomputed(s:serialize_measure_stream, value:float, max_integer_value:int, bits:int, delta:float, min:float):
    serialize_compressed_float_validate_precomputed(max_integer_value, bits, delta)
    if s.failed:
        return value, false
    s.measure_bits(bits)
    return value, true
