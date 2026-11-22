// serialize_stream.goose -- the three streams, the error latch, terminality
// and bit accounting (DESIGN.md (a), (b), (d), (e), (f)), and the bit-level
// primitives (bits, bits64, bool, uint8..uint64, align, flush), each of which
// is the latch, the gate and one call into the packer
// (src/serialize_bitpacker.goose), where every bit actually moves.

import std;
import .serialize_bitpacker;

// The latched kinds (DESIGN.md (a), errors), a READ stream's only: write
// and measure misuse never latches (U9). The first one latched sticks:
// the later refusals of a failed stream are echoes and keep it. Kinds 1 to 9
// are the refusal reasons, the closed list STANDARD.md defines under "Refusal
// reasons", in the C++ library's serialize::Refusal order; serialize_refusal_name
// spells each one as the corpus does. REJECTED (2.1) is the caller's own
// refusal: serialize_reject, serialize_fail with no reason, or a nested object
// whose serialize returns false with nothing latched (STANDARD.md, rejected).
// The latch is the reference's tla/ReadStreamLatch.tla: refused exactly when a
// reason is recorded, the reason never changes once recorded, no read
// succeeds after a refusal.
let SERIALIZE_ERROR_NONE = 0;
let SERIALIZE_ERROR_PAST_END = 1;           // past_end: past the end of the data
let SERIALIZE_ERROR_VALUE_OUT_OF_RANGE = 2; // value_out_of_range: outside a range or domain
let SERIALIZE_ERROR_NONZERO_PADDING = 3;    // nonzero_padding: align padding, or the end's final byte
let SERIALIZE_ERROR_TRAILING_DATA = 4;      // trailing_data: a whole byte or more after the end
let SERIALIZE_ERROR_NON_FINITE_FLOAT = 5;   // non_finite_float: NaN or an infinity
let SERIALIZE_ERROR_INVALID_UTF8 = 6;       // invalid_utf8: a string payload that is not UTF-8
let SERIALIZE_ERROR_EMBEDDED_NUL = 7;       // embedded_nul: a zero byte in a string payload
let SERIALIZE_ERROR_NON_CANONICAL = 8;      // non_canonical: an encoding no writer produces
let SERIALIZE_ERROR_REJECTED = 9;           // rejected: well formed bytes, the caller refused the value

// The family-wide name of a latched kind, exactly as STANDARD.md and the
// corpus spell it ("past_end", "rejected"); "none" for NONE (U14). Anything
// else is "unknown".
fn serialize_refusal_name(kind: i32) -> const u8[:] {
    if kind == SERIALIZE_ERROR_NONE { return "none"; }
    if kind == SERIALIZE_ERROR_PAST_END { return "past_end"; }
    if kind == SERIALIZE_ERROR_VALUE_OUT_OF_RANGE { return "value_out_of_range"; }
    if kind == SERIALIZE_ERROR_NONZERO_PADDING { return "nonzero_padding"; }
    if kind == SERIALIZE_ERROR_TRAILING_DATA { return "trailing_data"; }
    if kind == SERIALIZE_ERROR_NON_FINITE_FLOAT { return "non_finite_float"; }
    if kind == SERIALIZE_ERROR_INVALID_UTF8 { return "invalid_utf8"; }
    if kind == SERIALIZE_ERROR_EMBEDDED_NUL { return "embedded_nul"; }
    if kind == SERIALIZE_ERROR_NON_CANONICAL { return "non_canonical"; }
    if kind == SERIALIZE_ERROR_REJECTED { return "rejected"; }
    return "unknown";
}

// A write stream over the caller's buffer, whose length is a multiple of 8:
// the packer commits whole 64-bit words (STANDARD.md, General Conventions).
// It has no refusal state: write misuse aborts and records nothing (U9).
struct WriteStream {
    data: u8[:],
    num_bits: i64,
    bits_written: i64,
    word_index: i64,
    scratch: u64,
    scratch_bits: i64,
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
// It refuses nothing and has no refusal state (U6).
struct MeasureStream {
    bits_written: i64,
}

// ---- lifecycle -------------------------------------------------------------

// Write misuse aborts naming the rule (DESIGN.md (e); U1, U9).
fn serialize_write_stream_init(buffer: u8[:]) -> WriteStream {
    if buffer.len % 8 != 0 { abort("serialize: write buffer bytes must be a multiple of 8"); }
    return WriteStream { data: buffer, num_bits: buffer.len * 8, bits_written: 0, word_index: 0,
                         scratch: 0, scratch_bits: 0 };
}

// data holds the `bytes` bytes to read plus at least 8 bytes of slack. A
// negative `bytes`, or data without its slack, is caller misuse on a read: the
// stream starts refused as VALUE_OUT_OF_RANGE, never an abort (U7).
fn serialize_read_stream_init(data: const u8[:], bytes: i64) -> ReadStream {
    if bytes < 0 || data.len - 8 < bytes {
        return ReadStream { data: data[0..0], num_bits: 0, bits_read: 0, error: SERIALIZE_ERROR_VALUE_OUT_OF_RANGE };
    }
    return ReadStream { data: data, num_bits: bytes * 8, bits_read: 0, error: SERIALIZE_ERROR_NONE };
}

// The copying entry for an exactly sized payload (STANDARD.md, Reader
// Obligations, the slack contract; serialize.c serialize_read_stream_init_padded):
// copies data into destination and zeroes the 8 slack bytes. A destination
// that cannot hold data.len + 8 copies nothing and the stream starts latched.
fn serialize_read_stream_init_padded(destination: u8[:], data: const u8[:]) -> ReadStream {
    if destination.len - 8 < data.len {
        return ReadStream { data: destination[0..0], num_bits: 0, bits_read: 0, error: SERIALIZE_ERROR_PAST_END };
    }
    for i in data.len { destination[i] = data[i]; }
    for i in 8 { destination[data.len + i] = 0; }
    return ReadStream { data: destination, num_bits: data.len * 8, bits_read: 0, error: SERIALIZE_ERROR_NONE };
}

fn serialize_measure_stream_init() -> MeasureStream {
    return MeasureStream { bits_written: 0 };
}

// Writes out the partial scratch word (STANDARD.md, General Conventions, Flush).
// Required: the buffer's contents are unspecified until it runs (U11).
fn serialize_flush(s: WriteStream&) { serialize_packer_flush(s); }

// ---- the latch -------------------------------------------------------------

// A read stream's latched reason. A write or a measure records nothing
// (U6, U9), so on one it is always NONE: it exists so a function generic over
// the stream (serialize_object) can ask.
fn serialize_error(s: WriteStream&) -> i32 { return SERIALIZE_ERROR_NONE; }
fn serialize_error(s: ReadStream&) -> i32 { return s.error; }
fn serialize_error(s: MeasureStream&) -> i32 { return SERIALIZE_ERROR_NONE; }

// The reason a fail records: a kind on the closed list as given, and REJECTED
// for NONE or anything outside it, so a refused stream never reports none
// (U14; a refused stream that reported none would be relabelled past_end by
// the next read).
fn serialize_fail_kind(kind: i32) -> i32 {
    if kind < SERIALIZE_ERROR_PAST_END || kind > SERIALIZE_ERROR_REJECTED { return SERIALIZE_ERROR_REJECTED; }
    return kind;
}

// On a read, latches kind unless an error is already latched (the first
// sticks) and returns false, so a caller can `return serialize_fail(s)`. On a
// write or a measure it returns false and records nothing: no reason, no
// later failure (U6, U9). It never aborts, on any stream (U9, U14).
fn serialize_fail(s: WriteStream&, kind: i32 = SERIALIZE_ERROR_REJECTED) -> bool { return false; }
fn serialize_fail(s: ReadStream&, kind: i32 = SERIALIZE_ERROR_REJECTED) -> bool {
    if s.error == SERIALIZE_ERROR_NONE { s.error = serialize_fail_kind(kind); }
    return false;
}
fn serialize_fail(s: MeasureStream&, kind: i32 = SERIALIZE_ERROR_REJECTED) -> bool { return false; }

// The caller's own refusal (U2; STANDARD.md, rejected): the bytes were well
// formed, and the caller refuses the value they carry. On a read it latches
// REJECTED through the same latch as a wire refusal, so the first reason
// wins with no special case, and returns false, so a caller can
// `return serialize_reject(s)`. Reject is read only: on a write or a measure
// it is caller misuse and aborts, as all write misuse does (U9).
fn serialize_reject(s: WriteStream&) -> bool { abort("serialize: reject is read only"); }
fn serialize_reject(s: ReadStream&) -> bool { return serialize_fail(s, SERIALIZE_ERROR_REJECTED); }
fn serialize_reject(s: MeasureStream&) -> bool { abort("serialize: reject is read only"); }

fn serialize_is_writing(s: WriteStream&) -> bool { return true; }
fn serialize_is_writing(s: ReadStream&) -> bool { return false; }
fn serialize_is_writing(s: MeasureStream&) -> bool { return false; }
fn serialize_is_reading(s: WriteStream&) -> bool { return false; }
fn serialize_is_reading(s: ReadStream&) -> bool { return true; }
fn serialize_is_reading(s: MeasureStream&) -> bool { return false; }

// ---- the gates every family arm calls first (DESIGN.md (e)) ---------------

// Every read consults the latch first, zero-bit reads included; a read past
// the end latches PAST_END (STANDARD.md, Reader Obligations: the test is
// bit_index + n > 8 * bytes, written so it cannot overflow).
fn serialize_read_check(s: ReadStream&, bits: i64) -> bool {
    guard s.error == SERIALIZE_ERROR_NONE else { return false; }
    guard bits <= s.num_bits - s.bits_read else { return serialize_fail(s, SERIALIZE_ERROR_PAST_END); }
    return true;
}

// A write that does not fit the room left is write misuse: it aborts before
// any bit of the operation is written, naming the operation's whole width
// (U9, U10; DESIGN.md (e)). Never a latched past_end: that is a reader's.
fn serialize_write_check(s: WriteStream&, bits: i64) {
    if bits > s.num_bits - s.bits_written { serialize_write_past_end(s, bits); }
}

// The abort, out of line so the check above stays one compare.
fn serialize_write_past_end(s: WriteStream&, bits: i64) {
    var message: u8[>..] = [];
    format(message, "serialize: write does not fit the buffer: ", bits, " bits, ", s.num_bits - s.bits_written, " left");
    abort(message);
}

// A measure refuses nothing (STANDARD.md, The Measure Stream; U6): it
// latches nothing, so it adds and accepts.
fn serialize_measure_add(s: MeasureStream&, bits: i64) -> bool {
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

// bits: [1,32], value < 2^bits (STANDARD.md, bits). On a write or a measure
// a bits count outside the range, or a value too wide for it, aborts (write
// misuse; U6, U8, U9). On a read the count is the caller's
// declaration, refused as VALUE_OUT_OF_RANGE before any bit moves (U16). The
// shift is guarded: Goose masks shift counts, so `value >> 32` on a u32 would
// be `value` (DESIGN.md (c)).
fn serialize_bits_count_check(bits: i64, width: i64) {
    if bits < 1 || bits > width {
        abort(if width == 32 { "serialize: bits must be in [1,32]" } else { "serialize: bits must be in [1,64]" });
    }
}

fn serialize_bits_write_check(value: u64, bits: i64, width: i64) {
    serialize_bits_count_check(bits, width);
    if bits != 64 && value >> (bits as u64) != 0 { abort("serialize: value must fit in bits"); }
}

fn serialize_bits(s: WriteStream&, value: u32, bits: i64) -> bool {
    serialize_bits_write_check(value, bits, 32);
    serialize_write_check(s, bits);
    serialize_packer_write(s, value, bits);
    return true;
}
fn serialize_bits(s: ReadStream&, value: u32&, bits: i64) -> bool {
    guard bits >= 1 && bits <= 32 else { return serialize_fail(s, SERIALIZE_ERROR_VALUE_OUT_OF_RANGE); }
    guard serialize_read_check(s, bits) else { return false; }
    value = serialize_packer_read(s, bits) as! u32;
    return true;
}
fn serialize_bits(s: MeasureStream&, value: u32, bits: i64) -> bool {
    serialize_bits_write_check(value, bits, 32);
    return serialize_measure_add(s, bits);
}

// bits64: [1,64]; above 32 the low 32 bits go first, which LSB-first packing
// of the whole value produces by construction (STANDARD.md, bits).
fn serialize_bits64(s: WriteStream&, value: u64, bits: i64) -> bool {
    serialize_bits_write_check(value, bits, 64);
    serialize_write_check(s, bits);
    serialize_packer_write(s, value, bits);
    return true;
}
fn serialize_bits64(s: ReadStream&, value: u64&, bits: i64) -> bool {
    guard bits >= 1 && bits <= 64 else { return serialize_fail(s, SERIALIZE_ERROR_VALUE_OUT_OF_RANGE); }
    guard serialize_read_check(s, bits) else { return false; }
    value = serialize_packer_read(s, bits);
    return true;
}
fn serialize_bits64(s: MeasureStream&, value: u64, bits: i64) -> bool {
    serialize_bits_write_check(value, bits, 64);
    return serialize_measure_add(s, bits);
}

// bool: one bit, 1 for true (STANDARD.md, bool).
fn serialize_bool(s: WriteStream&, value: bool) -> bool {
    serialize_write_check(s, 1);
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
    serialize_write_check(s, 8);
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
    serialize_write_check(s, 16);
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
    serialize_write_check(s, 32);
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
    serialize_write_check(s, 64);
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
// reader refuses nonzero padding as NONZERO_PADDING, and a latched stream refuses
// even an aligned align (STANDARD.md, align; Reader Obligations). The measure
// charges the worst case, 7 bits (STANDARD.md, The Measure Stream).
fn serialize_align(s: WriteStream&) -> bool {
    let n = serialize_align_bits(s);
    if n == 0 { return true; }
    serialize_write_check(s, n);
    serialize_packer_write(s, 0, n);
    return true;
}
fn serialize_align(s: ReadStream&) -> bool {
    guard s.error == SERIALIZE_ERROR_NONE else { return false; }
    let n = serialize_align_bits(s);
    if n == 0 { return true; }
    guard serialize_read_check(s, n) else { return false; }
    guard serialize_packer_read(s, n) == 0 else { return serialize_fail(s, SERIALIZE_ERROR_NONZERO_PADDING); }
    return true;
}
fn serialize_align(s: MeasureStream&) -> bool { return serialize_measure_add(s, 7); }

// end: the end of the object (STANDARD.md, end). The write and measure halves
// emit nothing, cost nothing and return true (U12). The read
// half consults the latch first, like every read; then refuses a whole byte or
// more left in the stream as TRAILING_DATA, even a zero byte; then reads the 0
// to 7 bits left in the final byte and refuses a set one as NONZERO_PADDING.
// Trailing data is checked first, so a whole byte left over is TRAILING_DATA
// whatever its padding holds. An accepted end leaves the cursor at the end of
// the stream. A packet reader calls it after the top-level object.
fn serialize_end(s: WriteStream&) -> bool { return true; }
fn serialize_end(s: ReadStream&) -> bool {
    guard s.error == SERIALIZE_ERROR_NONE else { return false; }
    let remaining = s.num_bits - s.bits_read;
    guard remaining < 8 else { return serialize_fail(s, SERIALIZE_ERROR_TRAILING_DATA); }
    // Fewer than 8 bits remain here, so this is all of them: the final
    // byte's padding.
    let padding_bits = remaining % 8;
    if padding_bits == 0 { return true; }
    guard serialize_packer_read(s, padding_bits) == 0 else { return serialize_fail(s, SERIALIZE_ERROR_NONZERO_PADDING); }
    return true;
}
fn serialize_end(s: MeasureStream&) -> bool { return true; }
