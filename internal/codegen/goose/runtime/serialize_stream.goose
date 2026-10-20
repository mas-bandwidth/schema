// serialize_stream.goose -- the three streams, the error latch, terminality
// and bit accounting (DESIGN.md (a), (b), (d), (e), (f)), and the bit-level
// primitives (bits, bits64, bool, uint8..uint64, align, flush), each of which
// is the latch, the gate and one call into the packer
// (src/serialize_bitpacker.goose), where every bit actually moves.

import .serialize_bitpacker;

// The latched kinds (DESIGN.md (a), errors). The first one latched sticks.
let SERIALIZE_ERROR_NONE = 0;
let SERIALIZE_ERROR_OVERFLOW = 1;           // past the end of the data or the buffer
let SERIALIZE_ERROR_VALUE_OUT_OF_RANGE = 2; // a value outside its range, every string refusal
let SERIALIZE_ERROR_ALIGN = 3;              // nonzero align padding
let SERIALIZE_ERROR_FAILED = 4;             // serialize_fail by the caller, an object returning false

// A write stream over the caller's buffer, whose length is a multiple of 8:
// the packer commits whole 64-bit words (STANDARD.md, General Conventions).
struct WriteStream {
    data: u8[:],
    num_bits: i64,
    bits_written: i64,
    word_index: i64,
    scratch: u64,
    scratch_bits: i64,
    error: i32,
}

// A read stream over `bytes` bytes of data that extends at least 8 bytes
// past them (DESIGN.md (d), the slack contract): the packer loads 64-bit
// windows and never interprets a byte past `bytes`.
struct ReadStream {
    data: const u8[:],
    num_bits: i64,
    bits_read: i64,
    error: i32,
}

// A measure stream: a conservative bound (STANDARD.md, The Measure Stream).
struct MeasureStream {
    bits_written: i64,
    error: i32,
}

// ---- lifecycle -------------------------------------------------------------

// API misuse aborts naming the rule (DESIGN.md (e)).
fn serialize_write_stream_init(buffer: u8[:]) -> WriteStream {
    if buffer.len % 8 != 0 { abort("serialize: write buffer bytes must be a multiple of 8"); }
    return WriteStream { data: buffer, num_bits: buffer.len * 8, bits_written: 0, word_index: 0,
                         scratch: 0, scratch_bits: 0, error: SERIALIZE_ERROR_NONE };
}

// data holds the `bytes` bytes to read plus at least 8 bytes of slack.
fn serialize_read_stream_init(data: const u8[:], bytes: i64) -> ReadStream {
    if bytes < 0 { abort("serialize: read bytes must not be negative"); }
    if data.len - 8 < bytes { abort("serialize: read data must extend at least 8 bytes past bytes"); }
    return ReadStream { data: data, num_bits: bytes * 8, bits_read: 0, error: SERIALIZE_ERROR_NONE };
}

// The copying entry for an exactly sized payload (STANDARD.md, Reader
// Obligations, the slack contract; serialize.c serialize_read_stream_init_padded):
// copies data into destination and zeroes the 8 slack bytes. A destination
// that cannot hold data.len + 8 copies nothing and the stream starts latched.
fn serialize_read_stream_init_padded(destination: u8[:], data: const u8[:]) -> ReadStream {
    if destination.len - 8 < data.len {
        return ReadStream { data: destination[0..0], num_bits: 0, bits_read: 0, error: SERIALIZE_ERROR_OVERFLOW };
    }
    for i in data.len { destination[i] = data[i]; }
    for i in 8 { destination[data.len + i] = 0; }
    return ReadStream { data: destination, num_bits: data.len * 8, bits_read: 0, error: SERIALIZE_ERROR_NONE };
}

fn serialize_measure_stream_init() -> MeasureStream {
    return MeasureStream { bits_written: 0, error: SERIALIZE_ERROR_NONE };
}

// Writes out the partial scratch word (STANDARD.md, General Conventions, Flush).
fn serialize_flush(s: WriteStream&) { serialize_packer_flush(s); }

// ---- the latch -------------------------------------------------------------

fn serialize_error(s: WriteStream&) -> i32 { return s.error; }
fn serialize_error(s: ReadStream&) -> i32 { return s.error; }
fn serialize_error(s: MeasureStream&) -> i32 { return s.error; }

// Latches kind unless an error is already latched (the first sticks) and
// returns false, so a caller can `return serialize_fail(s)`.
fn serialize_fail(s: WriteStream&, kind: i32 = SERIALIZE_ERROR_FAILED) -> bool {
    if kind == SERIALIZE_ERROR_NONE { abort("serialize: fail kind must not be SERIALIZE_ERROR_NONE"); }
    if s.error == SERIALIZE_ERROR_NONE { s.error = kind; }
    return false;
}
fn serialize_fail(s: ReadStream&, kind: i32 = SERIALIZE_ERROR_FAILED) -> bool {
    if kind == SERIALIZE_ERROR_NONE { abort("serialize: fail kind must not be SERIALIZE_ERROR_NONE"); }
    if s.error == SERIALIZE_ERROR_NONE { s.error = kind; }
    return false;
}
fn serialize_fail(s: MeasureStream&, kind: i32 = SERIALIZE_ERROR_FAILED) -> bool {
    if kind == SERIALIZE_ERROR_NONE { abort("serialize: fail kind must not be SERIALIZE_ERROR_NONE"); }
    if s.error == SERIALIZE_ERROR_NONE { s.error = kind; }
    return false;
}

fn serialize_is_writing(s: WriteStream&) -> bool { return true; }
fn serialize_is_writing(s: ReadStream&) -> bool { return false; }
fn serialize_is_writing(s: MeasureStream&) -> bool { return false; }
fn serialize_is_reading(s: WriteStream&) -> bool { return false; }
fn serialize_is_reading(s: ReadStream&) -> bool { return true; }
fn serialize_is_reading(s: MeasureStream&) -> bool { return false; }

// ---- the gates every family arm calls first (DESIGN.md (e)) ---------------

// Every read consults the latch first, zero-bit reads included; a read past
// the end latches Overflow (STANDARD.md, Reader Obligations: the test is
// bit_index + n > 8 * bytes, written so it cannot overflow).
fn serialize_read_check(s: ReadStream&, bits: i64) -> bool {
    guard s.error == SERIALIZE_ERROR_NONE else { return false; }
    guard bits <= s.num_bits - s.bits_read else { return serialize_fail(s, SERIALIZE_ERROR_OVERFLOW); }
    return true;
}

// A latched write stream writes nothing; a write past the end of the buffer
// latches Overflow and writes nothing (DESIGN.md (e)).
fn serialize_write_check(s: WriteStream&, bits: i64) -> bool {
    guard s.error == SERIALIZE_ERROR_NONE else { return false; }
    guard bits <= s.num_bits - s.bits_written else { return serialize_fail(s, SERIALIZE_ERROR_OVERFLOW); }
    return true;
}

// A measure refuses nothing (STANDARD.md, The Measure Stream); only a
// caller's serialize_fail latches it.
fn serialize_measure_add(s: MeasureStream&, bits: i64) -> bool {
    guard s.error == SERIALIZE_ERROR_NONE else { return false; }
    s.bits_written += bits;
    return true;
}

// ---- accounting ------------------------------------------------------------

fn serialize_bits_processed(s: WriteStream&) -> i64 { return s.bits_written; }
fn serialize_bits_processed(s: ReadStream&) -> i64 { return s.bits_read; }
fn serialize_bits_processed(s: MeasureStream&) -> i64 { return s.bits_written; }
// The meaningful length, rounded up to a byte (STANDARD.md, General Conventions).
fn serialize_bytes_processed(s: WriteStream&) -> i64 { return (s.bits_written + 7) / 8; }
fn serialize_bytes_processed(s: ReadStream&) -> i64 { return (s.bits_read + 7) / 8; }
fn serialize_bytes_processed(s: MeasureStream&) -> i64 { return (s.bits_written + 7) / 8; }
fn serialize_bits_available(s: WriteStream&) -> i64 { return s.num_bits - s.bits_written; }
fn serialize_bits_remaining(s: ReadStream&) -> i64 { return s.num_bits - s.bits_read; }
// (8 - (bit_index % 8)) % 8 (STANDARD.md, General Conventions, Bit index and alignment).
fn serialize_align_bits(s: WriteStream&) -> i64 { return (8 - s.bits_written % 8) % 8; }
fn serialize_align_bits(s: ReadStream&) -> i64 { return (8 - s.bits_read % 8) % 8; }

// ---- bit-level primitives (STANDARD.md, Bit-Level Primitives) -------------

// bits: [1,32], value < 2^bits (STANDARD.md, bits). A bits count outside the
// range aborts (API misuse, before the latch is consulted); a value too wide
// latches ValueOutOfRange (DESIGN.md (e)). The shift is guarded: Goose masks
// shift counts, so `value >> 32` on a u32 would be `value` (DESIGN.md (c)).
fn serialize_bits(s: WriteStream&, value: u32, bits: i64) -> bool {
    if bits < 1 || bits > 32 { abort("serialize: bits must be in [1,32]"); }
    guard s.error == SERIALIZE_ERROR_NONE else { return false; }
    guard bits == 32 || value >> (bits as u32) == 0 else { return serialize_fail(s, SERIALIZE_ERROR_VALUE_OUT_OF_RANGE); }
    guard serialize_write_check(s, bits) else { return false; }
    serialize_packer_write(s, value, bits);
    return true;
}
fn serialize_bits(s: ReadStream&, value: u32&, bits: i64) -> bool {
    if bits < 1 || bits > 32 { abort("serialize: bits must be in [1,32]"); }
    guard serialize_read_check(s, bits) else { return false; }
    value = serialize_packer_read(s, bits) as! u32;
    return true;
}
fn serialize_bits(s: MeasureStream&, value: u32, bits: i64) -> bool {
    if bits < 1 || bits > 32 { abort("serialize: bits must be in [1,32]"); }
    return serialize_measure_add(s, bits);
}

// bits64: [1,64]; above 32 the low 32 bits go first, which LSB-first packing
// of the whole value produces by construction (STANDARD.md, bits).
fn serialize_bits64(s: WriteStream&, value: u64, bits: i64) -> bool {
    if bits < 1 || bits > 64 { abort("serialize: bits must be in [1,64]"); }
    guard s.error == SERIALIZE_ERROR_NONE else { return false; }
    guard bits == 64 || value >> (bits as u64) == 0 else { return serialize_fail(s, SERIALIZE_ERROR_VALUE_OUT_OF_RANGE); }
    guard serialize_write_check(s, bits) else { return false; }
    serialize_packer_write(s, value, bits);
    return true;
}
fn serialize_bits64(s: ReadStream&, value: u64&, bits: i64) -> bool {
    if bits < 1 || bits > 64 { abort("serialize: bits must be in [1,64]"); }
    guard serialize_read_check(s, bits) else { return false; }
    value = serialize_packer_read(s, bits);
    return true;
}
fn serialize_bits64(s: MeasureStream&, value: u64, bits: i64) -> bool {
    if bits < 1 || bits > 64 { abort("serialize: bits must be in [1,64]"); }
    return serialize_measure_add(s, bits);
}

// bool: one bit, 1 for true (STANDARD.md, bool).
fn serialize_bool(s: WriteStream&, value: bool) -> bool {
    guard serialize_write_check(s, 1) else { return false; }
    serialize_packer_write(s, if value { 1 } else { 0 }, 1);
    return true;
}
fn serialize_bool(s: ReadStream&, value: bool&) -> bool {
    guard serialize_read_check(s, 1) else { return false; }
    value = serialize_packer_read(s, 1) != 0;
    return true;
}
fn serialize_bool(s: MeasureStream&, value: bool) -> bool { return serialize_measure_add(s, 1); }

// uint8..uint64: exactly bits(value, width) (STANDARD.md, bits, the
// fixed-width helpers table); uint64 is the low 32 then the high 32.
fn serialize_uint8(s: WriteStream&, value: u8) -> bool {
    guard serialize_write_check(s, 8) else { return false; }
    serialize_packer_write(s, value, 8);
    return true;
}
fn serialize_uint8(s: ReadStream&, value: u8&) -> bool {
    guard serialize_read_check(s, 8) else { return false; }
    value = serialize_packer_read(s, 8) as! u8;
    return true;
}
fn serialize_uint8(s: MeasureStream&, value: u8) -> bool { return serialize_measure_add(s, 8); }

fn serialize_uint16(s: WriteStream&, value: u16) -> bool {
    guard serialize_write_check(s, 16) else { return false; }
    serialize_packer_write(s, value, 16);
    return true;
}
fn serialize_uint16(s: ReadStream&, value: u16&) -> bool {
    guard serialize_read_check(s, 16) else { return false; }
    value = serialize_packer_read(s, 16) as! u16;
    return true;
}
fn serialize_uint16(s: MeasureStream&, value: u16) -> bool { return serialize_measure_add(s, 16); }

fn serialize_uint32(s: WriteStream&, value: u32) -> bool {
    guard serialize_write_check(s, 32) else { return false; }
    serialize_packer_write(s, value, 32);
    return true;
}
fn serialize_uint32(s: ReadStream&, value: u32&) -> bool {
    guard serialize_read_check(s, 32) else { return false; }
    value = serialize_packer_read(s, 32) as! u32;
    return true;
}
fn serialize_uint32(s: MeasureStream&, value: u32) -> bool { return serialize_measure_add(s, 32); }

fn serialize_uint64(s: WriteStream&, value: u64) -> bool {
    guard serialize_write_check(s, 64) else { return false; }
    serialize_packer_write(s, value, 64);
    return true;
}
fn serialize_uint64(s: ReadStream&, value: u64&) -> bool {
    guard serialize_read_check(s, 64) else { return false; }
    value = serialize_packer_read(s, 64);
    return true;
}
fn serialize_uint64(s: MeasureStream&, value: u64) -> bool { return serialize_measure_add(s, 64); }

// align: zero bits to the next byte boundary, nothing when aligned; the
// reader refuses nonzero padding with Align, and a latched stream refuses
// even an aligned align (STANDARD.md, align; Reader Obligations). The measure
// charges the worst case, 7 bits (STANDARD.md, The Measure Stream).
fn serialize_align(s: WriteStream&) -> bool {
    guard s.error == SERIALIZE_ERROR_NONE else { return false; }
    let n = serialize_align_bits(s);
    if n == 0 { return true; }
    guard serialize_write_check(s, n) else { return false; }
    serialize_packer_write(s, 0, n);
    return true;
}
fn serialize_align(s: ReadStream&) -> bool {
    guard s.error == SERIALIZE_ERROR_NONE else { return false; }
    let n = serialize_align_bits(s);
    if n == 0 { return true; }
    guard serialize_read_check(s, n) else { return false; }
    guard serialize_packer_read(s, n) == 0 else { return serialize_fail(s, SERIALIZE_ERROR_ALIGN); }
    return true;
}
fn serialize_align(s: MeasureStream&) -> bool { return serialize_measure_add(s, 7); }
