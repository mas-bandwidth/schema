// serialize_bytes.lobster — bytes and string
// (STANDARD.md, Bytes and Strings: bytes, string; DESIGN.md (d), (f)).
//
// Every write validates the whole operation's width before any bit moves, so
// misuse traps and writes nothing (IMPLEMENTING.md U9, U10). Every read gates
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

// The refusal a string payload earns, none for real text. The NUL scan runs
// over the whole payload BEFORE the UTF-8 check, so a payload that breaks
// both rules is embedded_nul (STANDARD.md, string). Reader and writer share it.
def string_payload_refusal(data:string, n:int) -> serialize_refusal:
    if string_has_nul(data, n):
        return serialize_refusal_embedded_nul
    if not string_utf8_valid(data, n):
        return serialize_refusal_invalid_utf8
    return serialize_refusal_none

// The string checks a write and a measure make (IMPLEMENTING.md U3, U4, U6,
// U9): buffer_size at least 1, the text shorter than buffer_size, no NUL, then
// well-formed UTF-8. Returns the length.
def serialize_check_string(value:string, buffer_size:int) -> int:
    if buffer_size < 1:
        serialize_abort("serialize: buffer_size must be at least 1")
    let n = length(value)
    if n >= buffer_size:
        serialize_abort("serialize: string must be shorter than buffer_size")
    if string_has_nul(value, n):
        serialize_abort("serialize: string must not hold a NUL")
    if not string_utf8_valid(value, n):
        serialize_abort("serialize: string must be well-formed UTF-8")
    return n

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
// (STANDARD.md, bytes, align; Reader Obligations). The align and its padding
// check come before the payload's length, so set padding is nonzero_padding
// even when the payload would also run past the end.
def serialize_bytes(s:serialize_read_stream, data:string, bytes:int):
    // A negative count, or a destination shorter than the count, is caller
    // misuse: refused on a read before any bit moves, never an abort
    // (IMPLEMENTING.md U7; U16, destination capacity).
    if bytes < 0 or length(data) < bytes:
        return data, s.serialize_read_misuse()
    if s.failed:
        return data, false
    if not s.serialize_align():
        return data, false
    if not s.read_gate(bytes * 8):
        return data, false
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
    if not s.serialize_align():
        return data, false
    return data, s.measure_bits(bytes * 8)

// ---------------------------------------------------------------------------
// string (STANDARD.md, string; DESIGN.md (d))
// ---------------------------------------------------------------------------

// The length as serialize_int(length, 0, buffer_size - 1), then the payload as
// serialize_bytes, which aligns (STANDARD.md, string). Bad text and a length
// past the buffer trap and write nothing (IMPLEMENTING.md U3, U4, U9).
def serialize_string(s:serialize_write_stream, value:string, buffer_size:int):
    let n = serialize_check_string(value, buffer_size)
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
// The align's padding is checked, and the claimed length is held against the
// bytes that remain (past_end), BEFORE the payload is allocated: a short
// stream claiming a huge length allocates nothing.
def serialize_string(s:serialize_read_stream, value:string, buffer_size:int):
    // buffer_size below 1 is refused on a read, never an abort (IMPLEMENTING.md U7).
    if buffer_size < 1:
        return value, s.serialize_read_misuse()
    if s.failed:
        return value, false
    var n = 0
    var ok = true
    n, ok = s.serialize_int(n, 0, buffer_size - 1)
    if not ok:
        return value, false
    if not s.serialize_align():
        return value, false
    if not s.read_gate(n * 8):
        return value, false
    var payload = ensure_size("", n, 0)
    payload, ok = s.serialize_bytes(payload, n)
    if not ok:
        return value, false
    let text_refusal = string_payload_refusal(payload, n)
    if text_refusal != serialize_refusal_none:
        return value, s.fail(text_refusal)
    return payload, true

// The exact length width plus the align worst case and the payload
// (STANDARD.md, The Measure Stream). Text the writer refuses (too long, a NUL,
// not well-formed UTF-8) traps here too (IMPLEMENTING.md U3, U4, U6).
def serialize_string(s:serialize_measure_stream, value:string, buffer_size:int):
    let n = serialize_check_string(value, buffer_size)
    var sent = 0
    var ok = true
    sent, ok = s.serialize_int(0, 0, buffer_size - 1)
    if not ok:
        return value, false
    if not s.serialize_align():
        return value, false
    return value, s.measure_bits(n * 8)

// ---------------------------------------------------------------------------
// serialize_copy_string (IMPLEMENTING.md U15; the reference's
// serialize_copy_string): on the source's bytes, take those before the first
// NUL; if there are more than dest_size - 1, keep the first dest_size - 1,
// then drop bytes from the end while the first byte left out is a UTF-8
// continuation byte (10xxxxxx), so the copy never ends inside a character and
// serialize_string with buffer_size = dest_size accepts it. A Lobster string
// is a value, so the prefix is returned and no terminator is stored.
// dest_size below 1 is write misuse, and traps (U9).
// ---------------------------------------------------------------------------

def serialize_copy_string(source:string, dest_size:int) -> string:
    if dest_size < 1:
        serialize_abort("serialize: copy_string dest_size must be at least 1")
    var n = 0
    while n < length(source) and read_uint8_le(source, n) != 0:
        n += 1
    if n > dest_size - 1:
        n = dest_size - 1
        while n > 0 and (read_uint8_le(source, n) & 0xC0) == 0x80:
            n -= 1
    return substring(source, 0, n)
