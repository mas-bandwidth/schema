// serialize_bytes.lobster — bytes, string and wstring
// (STANDARD.md, Bytes and Strings: bytes, string, wstring; DESIGN.md (d), (f)).
//
// Every write validates the whole operation's width before any bit moves, so a
// refused call latches and writes nothing (DESIGN.md (e)). Every read gates
// before any load, refuses malformed content after the payload, and leaves the
// caller's value untouched on a refusal (STANDARD.md, Reader Obligations).

import .serialize_stream
import .serialize_bitpacker
import .serialize_int

// The UTF-8 well-formedness check of Unicode Table 3-7 — overlongs,
// surrogates, values above U+10FFFF and truncated sequences all fail
// (STANDARD.md, string, "Readers must refuse malformed string payloads").
def string_utf8_valid(data:string, n:int) -> bool:
    var i = 0
    while i < n:
        let lead = read_uint8_le(data, i)
        if lead < 0x80:
            i += 1
        elif (lead & 0xE0) == 0xC0:
            if lead < 0xC2:
                return false
            if i + 1 >= n:
                return false
            if (read_uint8_le(data, i + 1) & 0xC0) != 0x80:
                return false
            i += 2
        elif (lead & 0xF0) == 0xE0:
            if i + 2 >= n:
                return false
            let b1 = read_uint8_le(data, i + 1)
            let b2 = read_uint8_le(data, i + 2)
            if (b1 & 0xC0) != 0x80 or (b2 & 0xC0) != 0x80:
                return false
            if lead == 0xE0 and b1 < 0xA0:
                return false
            if lead == 0xED and b1 >= 0xA0:
                return false
            i += 3
        elif (lead & 0xF8) == 0xF0:
            if lead > 0xF4:
                return false
            if i + 3 >= n:
                return false
            let b1 = read_uint8_le(data, i + 1)
            let b2 = read_uint8_le(data, i + 2)
            let b3 = read_uint8_le(data, i + 3)
            if (b1 & 0xC0) != 0x80 or (b2 & 0xC0) != 0x80 or (b3 & 0xC0) != 0x80:
                return false
            if lead == 0xF0 and b1 < 0x90:
                return false
            if lead == 0xF4 and b1 >= 0x90:
                return false
            i += 4
        else:
            return false
    return true

// An interior NUL is its own refusal: NUL is well-formed UTF-8, and a zero
// byte gives the payload two lengths (STANDARD.md, string).
def string_has_nul(data:string, n:int) -> bool:
    for(n) i:
        if read_uint8_le(data, i) == 0:
            return true
    return false

// The UTF-16 unit count and writer-side well-formedness: a NUL, a surrogate
// code point or a value above U+10FFFF is bad text and latches on write
// (STANDARD.md, wstring; DESIGN.md (e), write refusals).
def wstring_units(value:[int]) -> int, bool:
    var units = 0
    for(value) c:
        if c < 0 or c == 0 or c > 0x10FFFF:
            return 0, false
        if c >= 0xD800 and c <= 0xDFFF:
            return 0, false
        if c >= 0x10000:
            units += 2
        else:
            units += 1
    return units, true

// ---------------------------------------------------------------------------
// bytes (STANDARD.md, bytes; DESIGN.md (d))
// ---------------------------------------------------------------------------

// Aligns first, then writes `bytes` raw bytes; count is not written
// (STANDARD.md, bytes).
def serialize_bytes(s:serialize_write_stream, data:string, bytes:int):
    if bytes < 0:
        serialize_abort("serialize: bytes must not be negative")
    if length(data) < bytes:
        serialize_abort("serialize: bytes must not exceed the data length")
    if s.failed:
        return data, false
    let align = s.align_bits()
    // The whole operation, alignment included, is gated before a bit moves
    // (DESIGN.md (e)).
    if not s.write_gate(align + bytes * 8):
        return data, false
    if align > 0:
        s.pack_bits(0, align)
    if bytes > 0:
        s.pack_bytes(data, 0, bytes)
    return data, true

// Aligns first and verifies the padding is zero, then reads into `data`
// (STANDARD.md, bytes, align; Reader Obligations).
def serialize_bytes(s:serialize_read_stream, data:string, bytes:int):
    if bytes < 0:
        serialize_abort("serialize: bytes must not be negative")
    if length(data) < bytes:
        serialize_abort("serialize: bytes must not exceed the data length")
    if s.failed:
        return data, false
    let align = s.align_bits()
    if not s.read_gate(align + bytes * 8):
        return data, false
    if align > 0 and s.unpack_bits(align) != 0:
        return data, s.fail()
    if bytes > 0:
        s.unpack_bytes(data, 0, bytes)
    return data, true

// A measure charges the align worst case, 7 bits, then the payload
// (STANDARD.md, The Measure Stream; serialize.c serialize_measure_bytes).
def serialize_bytes(s:serialize_measure_stream, data:string, bytes:int):
    if bytes < 0:
        serialize_abort("serialize: bytes must not be negative")
    if length(data) < bytes:
        serialize_abort("serialize: bytes must not exceed the data length")
    if s.failed:
        return data, false
    if not s.serialize_align():
        return data, false
    return data, s.measure_bits(bytes * 8)

// ---------------------------------------------------------------------------
// string (STANDARD.md, string; DESIGN.md (d))
// ---------------------------------------------------------------------------

// The length as serialize_int(length, 0, buffer_size - 1), then the payload as
// serialize_bytes, which aligns (STANDARD.md, string). Bad text and a length
// past the buffer latch and write nothing (DESIGN.md (e), write refusals).
def serialize_string(s:serialize_write_stream, value:string, buffer_size:int):
    if buffer_size < 1:
        serialize_abort("serialize: buffer_size must be at least 1")
    if s.failed:
        return value, false
    let n = length(value)
    if n >= buffer_size:
        return value, s.fail()
    if string_has_nul(value, n) or not string_utf8_valid(value, n):
        return value, s.fail()
    let length_bits = serialize_bits_required(0, buffer_size - 1)
    let align = (8 - ((s.bits_written + length_bits) & 7)) & 7
    if not s.write_gate(length_bits + align + n * 8):
        return value, false
    var sent = 0
    var ok = true
    sent, ok = s.serialize_int(n, 0, buffer_size - 1)
    if not ok:
        return value, false
    var payload = value
    payload, ok = s.serialize_bytes(value, n)
    return value, ok

// Reads the length, aligns, then the payload; refuses an interior NUL and
// malformed UTF-8 (STANDARD.md, string). A refusal returns the input value.
def serialize_string(s:serialize_read_stream, value:string, buffer_size:int):
    if buffer_size < 1:
        serialize_abort("serialize: buffer_size must be at least 1")
    if s.failed:
        return value, false
    var n = 0
    var ok = true
    n, ok = s.serialize_int(n, 0, buffer_size - 1)
    if not ok:
        return value, false
    var payload = ensure_size("", n, 0)
    payload, ok = s.serialize_bytes(payload, n)
    if not ok:
        return value, false
    if string_has_nul(payload, n) or not string_utf8_valid(payload, n):
        return value, s.fail()
    return payload, true

// The exact length width plus the align worst case and the payload
// (STANDARD.md, The Measure Stream).
def serialize_string(s:serialize_measure_stream, value:string, buffer_size:int):
    if buffer_size < 1:
        serialize_abort("serialize: buffer_size must be at least 1")
    if s.failed:
        return value, false
    let n = length(value)
    if n >= buffer_size:
        return value, s.fail()
    var sent = 0
    var ok = true
    sent, ok = s.serialize_int(0, 0, buffer_size - 1)
    if not ok:
        return value, false
    if not s.serialize_align():
        return value, false
    return value, s.measure_bits(n * 8)

// ---------------------------------------------------------------------------
// wstring (STANDARD.md, wstring; DESIGN.md (d))
// ---------------------------------------------------------------------------

// The length in UTF-16 code units, then each unit as a 32-bit group, with NO
// alignment anywhere (STANDARD.md, wstring). An astral code point is split
// into its surrogate pair, as serialize.c does on a 4-byte wchar_t platform.
def serialize_wstring(s:serialize_write_stream, value:[int], buffer_size:int):
    if buffer_size < 1:
        serialize_abort("serialize: buffer_size must be at least 1")
    if s.failed:
        return value, false
    let units, valid = wstring_units(value)
    if not valid:
        return value, s.fail()
    if units >= buffer_size:
        return value, s.fail()
    let length_bits = serialize_bits_required(0, buffer_size - 1)
    if not s.write_gate(length_bits + units * 32):
        return value, false
    var sent = 0
    var ok = true
    sent, ok = s.serialize_int(units, 0, buffer_size - 1)
    if not ok:
        return value, false
    for(value) c:
        if c >= 0x10000:
            let v = c - 0x10000
            var unit = 0
            unit, ok = s.serialize_bits(0xD800 + (v >> 10), 32)
            if not ok:
                return value, false
            unit, ok = s.serialize_bits(0xDC00 + (v & 0x3FF), 32)
            if not ok:
                return value, false
        else:
            var unit = 0
            unit, ok = s.serialize_bits(c, 32)
            if not ok:
                return value, false
    return value, true

// Reads the unit count, then each UTF-16 code unit as a 32-bit group, with no
// alignment; recombines surrogate pairs and refuses a group above 0xFFFF, a
// zero unit and an unpaired or misordered surrogate (STANDARD.md, wstring).
def serialize_wstring(s:serialize_read_stream, value:[int], buffer_size:int):
    if buffer_size < 1:
        serialize_abort("serialize: buffer_size must be at least 1")
    if s.failed:
        return value, false
    var units = 0
    var ok = true
    units, ok = s.serialize_int(units, 0, buffer_size - 1)
    if not ok:
        return value, false
    let out:[int] = []
    var i = 0
    while i < units:
        var c = 0
        c, ok = s.serialize_bits(c, 32)
        if not ok:
            return value, false
        if c == 0 or c > 0xFFFF:
            return value, s.fail()
        if c >= 0xD800 and c <= 0xDBFF:
            if i + 1 >= units:
                return value, s.fail()
            var low = 0
            low, ok = s.serialize_bits(low, 32)
            if not ok:
                return value, false
            if low < 0xDC00 or low > 0xDFFF:
                return value, s.fail()
            out.push(0x10000 + ((c - 0xD800) << 10) + (low - 0xDC00))
            i += 2
        elif c >= 0xDC00 and c <= 0xDFFF:
            return value, s.fail()
        else:
            out.push(c)
            i += 1
    return out, true

// The unit count's exact length width plus 32 bits per unit, no align
// (STANDARD.md, wstring; The Measure Stream).
def serialize_wstring(s:serialize_measure_stream, value:[int], buffer_size:int):
    if buffer_size < 1:
        serialize_abort("serialize: buffer_size must be at least 1")
    if s.failed:
        return value, false
    let units, valid = wstring_units(value)
    if not valid:
        return value, s.fail()
    if units >= buffer_size:
        return value, s.fail()
    var sent = 0
    var ok = true
    sent, ok = s.serialize_int(0, 0, buffer_size - 1)
    if not ok:
        return value, false
    return value, s.measure_bits(units * 32)
