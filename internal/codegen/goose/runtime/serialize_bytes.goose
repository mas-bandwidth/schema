// serialize_bytes.goose -- bytes, string and wstring (STANDARD.md, bytes,
// string, wstring; DESIGN.md (a), (d), (e)). Strings are Goose's own: UTF-8
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
    guard serialize_write_check(s, bits) else { return false; }
    if bits > 0 {
        serialize_packer_write(s, value as u64, bits);
    }
    return true;
}

fn serialize_read_length(s: ReadStream&, value: i64&, buffer_size: i64) -> bool {
    if buffer_size < 1 { abort("serialize: min must not be greater than max"); }
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

// Aligns FIRST, then the count raw bytes, which are not transmitted.
fn serialize_bytes(s: WriteStream&, data: const u8[:]) -> bool {
    guard serialize_align(s) else { return false; }
    guard serialize_write_check(s, data.len * 8) else { return false; }
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

// int(length, 0, buffer_size - 1), then the bytes, which align.
fn serialize_string(s: WriteStream&, value: const u8[:], buffer_size: i64) -> bool {
    if value.len > buffer_size - 1 { abort("serialize: string length must fit buffer_size"); }
    if !serialize_valid_utf8(value) { abort("serialize: string must be valid UTF-8"); }
    guard serialize_write_length(s, value.len, buffer_size) else { return false; }
    return serialize_bytes(s, value);
}

fn serialize_string<T>(s: ReadStream&, value: T&, buffer_size: i64) -> bool {
    guard serialize_error(s) == SERIALIZE_ERROR_NONE else { return false; }
    var length: i64 = 0;
    guard serialize_read_length(s, length, buffer_size) else { return false; }
    if length == 0 {
        guard serialize_align(s) else { return false; }
        value.clear();
        return true;
    }
    guard serialize_align(s) else { return false; }
    guard serialize_read_check(s, length * 8) else { return false; }
    let start = s.bits_read >> 3;
    let payload = s.data[start..start + length];
    for i in length {
        guard payload[i] != 0 else { return serialize_fail(s, SERIALIZE_ERROR_VALUE_OUT_OF_RANGE); }
    }
    guard serialize_valid_utf8(payload) else { return serialize_fail(s, SERIALIZE_ERROR_VALUE_OUT_OF_RANGE); }
    value.clear();
    for i in length {
        value.push(payload[i]);
    }
    s.bits_read += length * 8;
    return true;
}

fn serialize_string(s: MeasureStream&, value: const u8[:], buffer_size: i64) -> bool {
    if value.len > buffer_size - 1 { abort("serialize: string length must fit buffer_size"); }
    guard serialize_measure_length(s, buffer_size) else { return false; }
    return serialize_bytes(s, value);
}

// ---- wstring (STANDARD.md, wstring) ---------------------------------------

fn serialize_utf8_unit_count(value: const u8[:]) -> i64 {
    var i: i64 = 0;
    var units: i64 = 0;
    while i < value.len {
        let lead = value[i];
        if lead < 0x80 {
            i += 1;
            units += 1;
        } else if (lead & 0xE0) == 0xC0 {
            i += 2;
            units += 1;
        } else if (lead & 0xF0) == 0xE0 {
            i += 3;
            units += 1;
        } else {
            i += 4;
            units += 2;
        }
    }
    return units;
}

fn serialize_utf8_code_point(value: const u8[:], i: i64&) -> u32 {
    let lead = value[i];
    if lead < 0x80 {
        i += 1;
        return lead;
    }
    if (lead & 0xE0) == 0xC0 {
        let cp = ((lead & 0x1F) as u32 << 6) | ((value[i + 1] & 0x3F) as u32);
        i += 2;
        return cp;
    }
    if (lead & 0xF0) == 0xE0 {
        let cp = ((lead & 0x0F) as u32 << 12) | ((value[i + 1] & 0x3F) as u32 << 6)
               | ((value[i + 2] & 0x3F) as u32);
        i += 3;
        return cp;
    }
    let cp = ((lead & 0x07) as u32 << 18) | ((value[i + 1] & 0x3F) as u32 << 12)
           | ((value[i + 2] & 0x3F) as u32 << 6) | ((value[i + 3] & 0x3F) as u32);
    i += 4;
    return cp;
}

// int(units, 0, buffer_size - 1), then each UTF-16 code unit as a 32-bit
// group, in order. NO align anywhere (STANDARD.md, wstring).
fn serialize_wstring(s: WriteStream&, value: const u8[:], buffer_size: i64) -> bool {
    if !serialize_valid_utf8(value) { abort("serialize: wstring must be valid UTF-8"); }
    let units = serialize_utf8_unit_count(value);
    if units > buffer_size - 1 { abort("serialize: wstring unit count must fit buffer_size"); }
    guard serialize_write_length(s, units, buffer_size) else { return false; }
    var i: i64 = 0;
    while i < value.len {
        let cp = serialize_utf8_code_point(value, i);
        if cp >= 0x10000 {
            let v = cp - 0x10000;
            guard serialize_bits(s, 0xD800 + (v >> 10), 32) else { return false; }
            guard serialize_bits(s, 0xDC00 + (v & 0x3FF), 32) else { return false; }
        } else {
            guard serialize_bits(s, cp, 32) else { return false; }
        }
    }
    return true;
}

fn serialize_append_utf8<T>(value: T&, cp: u32) {
    if cp < 0x80 {
        value.push(cp as! u8);
    } else if cp < 0x800 {
        value.push((0xC0 | (cp >> 6)) as! u8);
        value.push((0x80 | (cp & 0x3F)) as! u8);
    } else if cp < 0x10000 {
        value.push((0xE0 | (cp >> 12)) as! u8);
        value.push((0x80 | ((cp >> 6) & 0x3F)) as! u8);
        value.push((0x80 | (cp & 0x3F)) as! u8);
    } else {
        value.push((0xF0 | (cp >> 18)) as! u8);
        value.push((0x80 | ((cp >> 12) & 0x3F)) as! u8);
        value.push((0x80 | ((cp >> 6) & 0x3F)) as! u8);
        value.push((0x80 | (cp & 0x3F)) as! u8);
    }
}

// Validate the whole code-unit sequence before the destination is touched: no
// zero group, no group above 0xFFFF, no unpaired surrogate (STANDARD.md,
// wstring, Readers must refuse). A copy of the stream carries the peek.
fn serialize_wstring_units_valid(s: ReadStream&, units: i64) -> bool {
    var probe = s;
    var pending = false;
    for i in units {
        var c: u32 = 0;
        guard serialize_bits(probe, c, 32) else { return false; }
        if c == 0 { return false; }
        if c > 0xFFFF { return false; }
        if pending {
            if c < 0xDC00 || c > 0xDFFF { return false; }
            pending = false;
        } else if c >= 0xD800 && c <= 0xDBFF {
            pending = true;
        } else if c >= 0xDC00 && c <= 0xDFFF {
            return false;
        }
    }
    return !pending;
}

fn serialize_wstring<T>(s: ReadStream&, value: T&, buffer_size: i64) -> bool {
    guard serialize_error(s) == SERIALIZE_ERROR_NONE else { return false; }
    var units: i64 = 0;
    guard serialize_read_length(s, units, buffer_size) else { return false; }
    guard serialize_wstring_units_valid(s, units) else { return serialize_fail(s, SERIALIZE_ERROR_VALUE_OUT_OF_RANGE); }
    value.clear();
    var pending: u32 = 0;
    var have_pending = false;
    for i in units {
        var c: u32 = 0;
        assert(serialize_bits(s, c, 32));
        if have_pending {
            let cp = 0x10000 + ((pending - 0xD800) << 10) + (c - 0xDC00);
            serialize_append_utf8(value, cp);
            have_pending = false;
        } else if c >= 0xD800 && c <= 0xDBFF {
            pending = c;
            have_pending = true;
        } else {
            serialize_append_utf8(value, c);
        }
    }
    return true;
}

// The length field plus 32 bits per unit; NO align (STANDARD.md, wstring;
// The Measure Stream).
fn serialize_wstring(s: MeasureStream&, value: const u8[:], buffer_size: i64) -> bool {
    let units = serialize_utf8_unit_count(value);
    if units > buffer_size - 1 { abort("serialize: wstring unit count must fit buffer_size"); }
    guard serialize_measure_length(s, buffer_size) else { return false; }
    return serialize_measure_add(s, units * 32);
}
