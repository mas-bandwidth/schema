// serialize_int.lobster — ranged int (int32 domain), int64 and int_relative
// (STANDARD.md, Integers: int, int64, int_relative; DESIGN.md (c), (f)).
import .serialize_stream
import .serialize_bitpacker

// The int32 domain (STANDARD.md, int; DESIGN.md (c)): a parameter whose C type
// is int32 is checked at the call, and anything else is API misuse (abort).
let serialize_int32_min = -2147483648
let serialize_int32_max = 2147483647

def serialize_int32_fits(value:int) -> bool:
    return value >= serialize_int32_min and value <= serialize_int32_max

def serialize_check_int32(value:int, name:string):
    if not serialize_int32_fits(value):
        serialize_abort("serialize: " + name + " must be in [INT32_MIN, INT32_MAX]")

// The range check a write and a measure make, on the caller's value as passed
// (IMPLEMENTING.md U8, U9).
def serialize_check_in_range(value:int, min:int, max:int):
    if value < min or value > max:
        serialize_abort("serialize: value must be in [min, max]")

// serialize_int: bits_required(min, max) bits of value - min, the offset
// computed in the uint32 domain (STANDARD.md, int (ranged); DESIGN.md (c), (e)).
def serialize_int(s:serialize_write_stream, value:int, min:int, max:int):
    serialize_check_int32(min, "min")
    serialize_check_int32(max, "max")
    if min > max:
        serialize_abort("serialize: min must not be greater than max")
    // A value outside [min, max] traps (IMPLEMENTING.md U9).
    serialize_check_in_range(value, min, max)
    let bits = serialize_bits_required(min, max)
    // A degenerate range costs zero bits (STANDARD.md, int).
    if bits == 0:
        return value, true
    let offset = (value - min) & 0xFFFFFFFF
    if not s.write_gate(bits):
        return value, false
    s.pack_bits(offset, bits)
    return value, true

def serialize_int(s:serialize_read_stream, value:int, min:int, max:int):
    // Bounds outside int32, or min > max, are a bad declaration: on a read it
    // is refused before any bit moves, never an abort (IMPLEMENTING.md U5,
    // U7, U16).
    if not serialize_int32_fits(min) or not serialize_int32_fits(max) or min > max:
        return value, s.serialize_read_misuse()
    if s.failed:
        return value, false
    let bits = serialize_bits_required(min, max)
    if bits == 0:
        return min, true
    if not s.read_gate(bits):
        return value, false
    let offset = s.unpack_bits(bits)
    // Reject, never clamp: compare the offset against max - min in the
    // unsigned domain before forming the value (STANDARD.md, int).
    let span = (max - min) & 0xFFFFFFFF
    if offset > span:
        return value, s.fail(serialize_refusal_value_out_of_range)
    return min + offset, true

def serialize_int(s:serialize_measure_stream, value:int, min:int, max:int):
    serialize_check_int32(min, "min")
    serialize_check_int32(max, "max")
    if min > max:
        serialize_abort("serialize: min must not be greater than max")
    serialize_check_in_range(value, min, max)
    return value, s.measure_bits(serialize_bits_required(min, max))

// serialize_int64: bits_required64(min, max) bits of value - min in the
// unsigned 64-bit domain; above 32 bits the low 32 bits go first
// (STANDARD.md, int64 (ranged); DESIGN.md (c), (e)).
def serialize_int64(s:serialize_write_stream, value:int, min:int, max:int):
    if min > max:
        serialize_abort("serialize: min must not be greater than max")
    serialize_check_in_range(value, min, max)
    let bits = serialize_bits_required64(min, max)
    if bits == 0:
        return value, true
    let offset = value - min
    if not s.write_gate(bits):
        return value, false
    s.pack_bits64(offset, bits)
    return value, true

def serialize_int64(s:serialize_read_stream, value:int, min:int, max:int):
    if min > max:
        return value, s.serialize_read_misuse()
    if s.failed:
        return value, false
    let bits = serialize_bits_required64(min, max)
    if bits == 0:
        return min, true
    if not s.read_gate(bits):
        return value, false
    let offset = if bits <= 32: s.unpack_bits(bits) else: s.unpack_bits64(bits)
    // Compare in the unsigned domain: max - min can exceed the int64 range
    // (STANDARD.md, int64; DESIGN.md (c), "Unsigned order").
    let span = max - min
    if serialize_ult(span, offset):
        return value, s.fail(serialize_refusal_value_out_of_range)
    // The 64-bit int carries every value exactly (STANDARD.md, Wide integers
    // are exact): no step of the reconstruction passes through a float.
    let result = min + offset
    return result, true

def serialize_int64(s:serialize_measure_stream, value:int, min:int, max:int):
    if min > max:
        serialize_abort("serialize: min must not be greater than max")
    serialize_check_in_range(value, min, max)
    return value, s.measure_bits(serialize_bits_required64(min, max))

def serialize_check_int_relative_current(current:int):
    if current > serialize_int32_max:
        serialize_abort("serialize: int_relative current must be in [0, INT32_MAX]")

// The largest difference a bounded tier carries (STANDARD.md, int_relative):
// the absolute tier carrying one is a second encoding of that value.
let serialize_int_relative_bounded_max = 69914

// The ladder's total width for a difference, including its flags
// (STANDARD.md, int_relative; DESIGN.md (e)).
def serialize_int_relative_bits(difference:int) -> int:
    if difference == 1:
        return 1
    if difference <= 6:
        return 2 + 3
    if difference <= 23:
        return 3 + 5
    if difference <= 280:
        return 4 + 9
    if difference <= 4377:
        return 5 + 13
    if difference <= 69914:
        return 6 + 17
    return 6 + 32

// serialize_int_relative: the ladder of one-bit tier flags and the first tier
// the difference fits; the final tier carries current's raw unsigned 32 bits
// (STANDARD.md, int_relative; DESIGN.md (e)). The whole tier is gated before a
// flag moves, so a full buffer traps and writes nothing for this call.
def serialize_int_relative(s:serialize_write_stream, previous:int, current:int):
    if previous < 0 or previous > serialize_int32_max:
        serialize_abort("serialize: int_relative previous must be in [0, INT32_MAX]")
    // serialize.c asserts current > previous on write (ABORT CASES).
    if current <= previous:
        serialize_abort("serialize: int_relative current must be greater than previous")
    // A current past the non-negative int32 domain traps (IMPLEMENTING.md U9).
    serialize_check_int_relative_current(current)
    let difference = (current - previous) & 0xFFFFFFFF
    let total = serialize_int_relative_bits(difference)
    if not s.write_gate(total):
        return current, false
    var flag = false
    var ok = true
    if difference == 1:
        flag, ok = s.serialize_bool(true)
        return current, ok
    flag, ok = s.serialize_bool(false)
    if not ok: return current, false
    if difference <= 6:
        flag, ok = s.serialize_bool(true)
        if not ok: return current, false
        var d = 0
        d, ok = s.serialize_int(difference, 2, 6)
        return current, ok
    flag, ok = s.serialize_bool(false)
    if not ok: return current, false
    if difference <= 23:
        flag, ok = s.serialize_bool(true)
        if not ok: return current, false
        var d = 0
        d, ok = s.serialize_int(difference, 7, 23)
        return current, ok
    flag, ok = s.serialize_bool(false)
    if not ok: return current, false
    if difference <= 280:
        flag, ok = s.serialize_bool(true)
        if not ok: return current, false
        var d = 0
        d, ok = s.serialize_int(difference, 24, 280)
        return current, ok
    flag, ok = s.serialize_bool(false)
    if not ok: return current, false
    if difference <= 4377:
        flag, ok = s.serialize_bool(true)
        if not ok: return current, false
        var d = 0
        d, ok = s.serialize_int(difference, 281, 4377)
        return current, ok
    flag, ok = s.serialize_bool(false)
    if not ok: return current, false
    if difference <= 69914:
        flag, ok = s.serialize_bool(true)
        if not ok: return current, false
        var d = 0
        d, ok = s.serialize_int(difference, 4378, 69914)
        return current, ok
    flag, ok = s.serialize_bool(false)
    if not ok: return current, false
    // The absolute tier transmits current, not the difference, over 32 raw
    // unsigned bits (STANDARD.md, int_relative).
    var raw = 0
    raw, ok = s.serialize_bits(current, 32)
    return current, ok

def serialize_int_relative(s:serialize_read_stream, previous:int, current:int):
    if previous < 0 or previous > serialize_int32_max:
        return current, s.serialize_read_misuse()
    if s.failed:
        return current, false
    // Every tier reconstructs into the non-wrapping 64-bit int and leaves the
    // domain and ordering check to one tail (STANDARD.md, int_relative).
    var reconstructed = 0
    var flag = false
    var ok = true
    flag, ok = s.serialize_bool(false)
    if not ok: return current, false
    if flag:
        reconstructed = previous + 1
    else:
        flag, ok = s.serialize_bool(false)
        if not ok: return current, false
        if flag:
            var d = 0
            d, ok = s.serialize_int(d, 2, 6)
            if not ok: return current, false
            reconstructed = previous + d
        else:
            flag, ok = s.serialize_bool(false)
            if not ok: return current, false
            if flag:
                var d = 0
                d, ok = s.serialize_int(d, 7, 23)
                if not ok: return current, false
                reconstructed = previous + d
            else:
                flag, ok = s.serialize_bool(false)
                if not ok: return current, false
                if flag:
                    var d = 0
                    d, ok = s.serialize_int(d, 24, 280)
                    if not ok: return current, false
                    reconstructed = previous + d
                else:
                    flag, ok = s.serialize_bool(false)
                    if not ok: return current, false
                    if flag:
                        var d = 0
                        d, ok = s.serialize_int(d, 281, 4377)
                        if not ok: return current, false
                        reconstructed = previous + d
                    else:
                        flag, ok = s.serialize_bool(false)
                        if not ok: return current, false
                        if flag:
                            var d = 0
                            d, ok = s.serialize_int(d, 4378, 69914)
                            if not ok: return current, false
                            reconstructed = previous + d
                        else:
                            // The absolute tier's 32 raw bits are UNSIGNED
                            // (STANDARD.md, int_relative).
                            var raw = 0
                            raw, ok = s.serialize_bits(raw, 32)
                            if not ok: return current, false
                            // The absolute tier is canonical too: a current
                            // in the domain, above previous and no more than
                            // 69914 above it has a bounded encoding, so this
                            // one is refused as non_canonical. The domain and
                            // ordering rule comes first, so a raw value
                            // outside it falls through to the tail's
                            // value_out_of_range; the difference is formed in
                            // the 64-bit int after that test and cannot wrap
                            // (STANDARD.md, int_relative).
                            if raw <= serialize_int32_max and raw > previous and raw - previous <= serialize_int_relative_bounded_max:
                                return current, s.fail(serialize_refusal_non_canonical)
                            reconstructed = raw
    // One tail checks the domain and the strict increase, before the
    // destination is written (STANDARD.md, int_relative; DESIGN.md (e)).
    if reconstructed > serialize_int32_max or reconstructed <= previous:
        return current, s.fail(serialize_refusal_value_out_of_range)
    return reconstructed, true

def serialize_int_relative(s:serialize_measure_stream, previous:int, current:int):
    if previous < 0 or previous > serialize_int32_max:
        serialize_abort("serialize: int_relative previous must be in [0, INT32_MAX]")
    if current <= previous:
        serialize_abort("serialize: int_relative current must be greater than previous")
    serialize_check_int_relative_current(current)
    let difference = (current - previous) & 0xFFFFFFFF
    return current, s.measure_bits(serialize_int_relative_bits(difference))
