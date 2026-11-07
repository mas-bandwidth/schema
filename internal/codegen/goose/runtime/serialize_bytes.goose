// serialize_bytes.goose -- bytes and string (STANDARD.md, bytes, string;
// DESIGN.md (a), (d), (e)). Format 2.0 removed wstring: text is UTF-8 and only
// UTF-8. Strings are Goose's own: UTF-8
// bytes. Writes take a const u8[:]; a string read takes any u8 builder
// (u8[>..] or u8[..k]) by reference, cleared and filled only on success.
// Card serialize-goose-strings fills this file.

import .serialize_stream;
import .serialize_bitpacker;

// ---- length field (STANDARD.md, string, step 1) ---------------------------
//
// The length is serialize_int(length, 0, buffer_size - 1). The ranged-int
// card's entry point is not in this checkout yet, so the two arms below do the
// same arithmetic for min == 0: serialization is the offset in
// bits_required(0, max) bits, LSB-first, and 0 when max == 0 (STANDARD.md,
// int). They are byte-for-byte serialize_int's (DESIGN.md (a)).

fn serialize_length_bits(max: i64) -> i64 {
    var v = max as u64;
    var bits: i64 = 0;
    while v != 0 {
        bits += 1;
        v = v >> 1;
    }
    return bits;
}

fn serialize_write_length(s: WriteStream&, value: i64, buffer_size: i64) -> bool {
    if buffer_size < 1 { abort("serialize: min must not be greater than max"); }
    if value < 0 || value > buffer_size - 1 { abort("serialize: string length must fit buffer_size"); }
    let bits = serialize_length_bits(buffer_size - 1);
    serialize_write_check(s, bits);
    if bits > 0 {
        serialize_packer_write(s, value as u64, bits);
    }
    return true;
}

fn serialize_read_length(s: ReadStream&, value: i64&, buffer_size: i64) -> bool {
    guard buffer_size >= 1 else { return serialize_fail(s, SERIALIZE_ERROR_VALUE_OUT_OF_RANGE); }
    let bits = serialize_length_bits(buffer_size - 1);
    guard serialize_read_check(s, bits) else { return false; }
    var v: u64 = 0;
    if bits > 0 {
        v = serialize_packer_read(s, bits);
    }
    guard v <= (buffer_size - 1) as u64 else { return serialize_fail(s, SERIALIZE_ERROR_VALUE_OUT_OF_RANGE); }
    value = v as! i64;
    return true;
}

fn serialize_measure_length(s: MeasureStream&, buffer_size: i64) -> bool {
    if buffer_size < 1 { abort("serialize: min must not be greater than max"); }
    return serialize_measure_add(s, serialize_length_bits(buffer_size - 1));
}

// ---- UTF-8 validation (STANDARD.md, string, Readers must refuse) ----------

// Unicode Table 3-7: shortest forms only, no surrogates, nothing above
// U+10FFFF, no truncated sequence, no stray continuation byte.
fn serialize_valid_utf8(data: const u8[:]) -> bool {
    var i: i64 = 0;
    while i < data.len {
        let lead = data[i];
        if lead < 0x80 {
            i += 1;
        } else if (lead & 0xE0) == 0xC0 {
            if lead < 0xC2 { return false; }
            if i + 1 >= data.len { return false; }
            if (data[i + 1] & 0xC0) != 0x80 { return false; }
            i += 2;
        } else if (lead & 0xF0) == 0xE0 {
            if i + 2 >= data.len { return false; }
            let b1 = data[i + 1];
            let b2 = data[i + 2];
            if (b1 & 0xC0) != 0x80 || (b2 & 0xC0) != 0x80 { return false; }
            if lead == 0xE0 && b1 < 0xA0 { return false; }
            if lead == 0xED && b1 >= 0xA0 { return false; }
            i += 3;
        } else if (lead & 0xF8) == 0xF0 {
            if lead > 0xF4 { return false; }
            if i + 3 >= data.len { return false; }
            let b1 = data[i + 1];
            let b2 = data[i + 2];
            let b3 = data[i + 3];
            if (b1 & 0xC0) != 0x80 || (b2 & 0xC0) != 0x80 || (b3 & 0xC0) != 0x80 { return false; }
            if lead == 0xF0 && b1 < 0x90 { return false; }
            if lead == 0xF4 && b1 >= 0x90 { return false; }
            i += 4;
        } else {
            return false;
        }
    }
    return true;
}

// ---- bytes (STANDARD.md, bytes; DESIGN.md (d)) ----------------------------

// Aligns FIRST, then the count raw bytes, which are not transmitted. The
// writer checks room for the align and the payload together before writing
// either, and aborts when they do not fit (U9, U10).
fn serialize_bytes(s: WriteStream&, data: const u8[:]) -> bool {
    serialize_write_check(s, serialize_align_bits(s) + data.len * 8);
    serialize_align(s);
    serialize_packer_write_bytes(s, data);
    return true;
}

fn serialize_bytes(s: ReadStream&, data: u8[:]) -> bool {
    guard serialize_align(s) else { return false; }
    guard serialize_read_check(s, data.len * 8) else { return false; }
    serialize_packer_read_bytes(s, data);
    return true;
}

// An align charges its worst case, seven bits (STANDARD.md, The Measure Stream).
fn serialize_bytes(s: MeasureStream&, data: const u8[:]) -> bool {
    guard serialize_align(s) else { return false; }
    return serialize_measure_add(s, data.len * 8);
}

// ---- string (STANDARD.md, string) -----------------------------------------

// int(length, 0, buffer_size - 1), then the bytes, which align. The writer
// and the measure are handed well-formed UTF-8 with no zero byte that fits
// buffer_size - 1 (abort otherwise; U3, U4, U9). The writer checks room for
// the whole operation, length, align and payload, before writing any of it,
// and aborts when it does not fit (U9, U10). The reader checks the declaration against the destination, then the
// claimed length against the stream, BEFORE it touches the destination, then
// scans the whole payload for a zero byte (EMBEDDED_NUL) before it validates
// the encoding (INVALID_UTF8), so a payload that breaks both rules is
// EMBEDDED_NUL (STANDARD.md, string).
fn serialize_string_check(value: const u8[:], buffer_size: i64) {
    if buffer_size < 1 { abort("serialize: min must not be greater than max"); }
    if value.len > buffer_size - 1 { abort("serialize: string length must fit buffer_size"); }
    for i in value.len {
        if value[i] == 0 { abort("serialize: string must not contain a zero byte"); }
    }
    if !serialize_valid_utf8(value) { abort("serialize: string must be valid UTF-8"); }
}

fn serialize_string(s: WriteStream&, value: const u8[:], buffer_size: i64) -> bool {
    serialize_string_check(value, buffer_size);
    let length_bits = serialize_length_bits(buffer_size - 1);
    let align = (8 - (s.bits_written + length_bits) % 8) % 8;
    serialize_write_check(s, length_bits + align + value.len * 8);
    serialize_write_length(s, value.len, buffer_size);
    return serialize_bytes(s, value);
}

// The capacity of a string read's destination (U16): a limited builder
// (u8[..k], u8[..]) has its own, which buffer_size - 1 must not exceed; a
// growable one (u8[>..], u8[>..<]) has none.
fn serialize_string_capacity(value: u8[>..]&) -> i64 { return 0x7FFFFFFFFFFFFFFF; }
fn serialize_string_capacity(value: u8[>..<]&) -> i64 { return 0x7FFFFFFFFFFFFFFF; }
fn serialize_string_capacity<T>(value: T&) -> i64 { return value.cap; }

fn serialize_string<T>(s: ReadStream&, value: T&, buffer_size: i64) -> bool {
    guard serialize_error(s) == SERIALIZE_ERROR_NONE else { return false; }
    // A destination that cannot hold buffer_size - 1 bytes is the caller's
    // bad declaration, refused before any bit moves whatever the wire holds:
    // never a capacity trap the wire chooses (U16; audit ledger H, 2026-10-27).
    guard buffer_size - 1 <= serialize_string_capacity(value) else {
        return serialize_fail(s, SERIALIZE_ERROR_VALUE_OUT_OF_RANGE);
    }
    var length: i64 = 0;
    guard serialize_read_length(s, length, buffer_size) else { return false; }
    if length == 0 {
        guard serialize_align(s) else { return false; }
        value.clear();
        return true;
    }
    guard serialize_align(s) else { return false; }
    // The claimed length against the bytes left, in bytes: length * 8 could
    // wrap for a huge buffer_size, the division cannot.
    guard length <= serialize_bits_remaining(s) / 8 else { return serialize_fail(s, SERIALIZE_ERROR_PAST_END); }
    let start = s.bits_read >> 3;
    let payload = s.data[start..start + length];
    for i in length {
        guard payload[i] != 0 else { return serialize_fail(s, SERIALIZE_ERROR_EMBEDDED_NUL); }
    }
    guard serialize_valid_utf8(payload) else { return serialize_fail(s, SERIALIZE_ERROR_INVALID_UTF8); }
    value.clear();
    for i in length {
        value.push(payload[i]);
    }
    s.bits_read += length * 8;
    return true;
}

fn serialize_string(s: MeasureStream&, value: const u8[:], buffer_size: i64) -> bool {
    serialize_string_check(value, buffer_size);
    guard serialize_measure_length(s, buffer_size) else { return false; }
    return serialize_bytes(s, value);
}

// ---- copy_string (the reference helper; U15) -------------------------------

// serialize_copy_string (IMPLEMENTING.md U15, 2.1.1): zeroes the
// destination, then copies the source's bytes before its first zero byte;
// if there are more than dest.len - 1 of them it keeps the first
// dest.len - 1, then drops bytes from the end while the first byte left out
// is a continuation byte (10xxxxxx), so a cut never ends inside a UTF-8
// sequence and serialize_string with buffer_size = dest.len accepts the
// result. The destination always ends in a zero byte. A destination of no
// bytes is write misuse and aborts (U9).
fn serialize_copy_string(dest: u8[:], source: const u8[:]) {
    if dest.len < 1 { abort("serialize: copy_string destination must hold at least one byte"); }
    for i in dest.len { dest[i] = 0; }
    var length: i64 = 0;
    while length < dest.len - 1 && length < source.len && source[length] != 0 {
        length += 1;
    }
    // The cut fell inside a sequence exactly when the first byte left out is
    // a continuation byte. A zero byte or the source's end is never one, so
    // an uncut copy never backs off.
    while length > 0 && length < source.len && (source[length] & 0xC0) == 0x80 {
        length -= 1;
    }
    for i in length { dest[i] = source[i]; }
}
