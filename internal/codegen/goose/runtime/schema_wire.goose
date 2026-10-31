// schema_wire.goose — the packet wire's bit moves (docs/PORT-GOOSE.md, THE
// PACKET WIRE): serialize.goose's serialize_bits, serialize_bits64 and
// serialize_bool for the generated codec, the same checks in the same order
// with the same refusals over the same state machine, written so that they
// inline into every call site. A writer carries the packer's state in a
// WireW local its entry owns (wire_open, wire_close), so the state lives in
// registers rather than behind the stream pointer that every byte store may
// alias; a reader moves the stream itself. The generated writers and readers move every
// plain bit field through these; everything else (fixed, int128, float,
// compressed float, bytes, text, align) goes to serialize.goose itself.
//
// Why they exist: each field's width is a constant at the call site, so once
// the move is inlined the count checks fold away, the mask and the shifts
// fold, and the 8-byte window is one store or one load (the slice gives
// Goose's bounds analysis the window, so the byte stores and loads inside it
// carry no checks and the C compiler merges them). serialize.goose's own
// entries take the width as a value and stay calls.
//
// SPDX-License-Identifier: NONE — emitted into your output; the schema
// compiler is AGPL-3.0, its output is not.
namespace schema_rt;

import .serialize_stream;

// WireW is a writer's packer state while a generated Write<T> runs: the
// stream's scratch, scratch_bits, bits_written and word_index.
struct WireW { scratch: u64, filled: i64, written: i64, word: i64 }

inline fn wire_open(s: WriteStream&) -> WireW {
    return WireW { scratch: s.scratch, filled: s.scratch_bits, written: s.bits_written, word: s.word_index };
}

// wire_reopen reads the state back after a runtime call moved the stream.
inline fn wire_reopen(s: WriteStream&, l: WireW&) {
    l.scratch = s.scratch;
    l.filled = s.scratch_bits;
    l.written = s.bits_written;
    l.word = s.word_index;
}

// wire_close puts the state back into the stream.
inline fn wire_close(s: WriteStream&, l: WireW&) {
    s.scratch = l.scratch;
    s.scratch_bits = l.filled;
    s.bits_written = l.written;
    s.word_index = l.word;
}

// wire_put packs the low `bits` bits of value, bits in [1,63]: the capacity
// check (an overrun puts the state back and aborts, as serialize.goose's
// write check does), then serialize_packer_write's state machine, whose
// scratch holds no bit above the filled count.
inline fn wire_put(s: WriteStream&, l: WireW&, value: u64, bits: i64) {
    if bits > s.num_bits - l.written {
        wire_close(s, l);
        serialize_write_past_end(s, bits);
    }
    let filled = l.filled;
    l.scratch = l.scratch | (value << (filled as! u64));
    l.written += bits;
    let total = filled + bits;
    if total < 64 {
        l.filled = total;
        return;
    }
    let at = l.word * 8;
    let window = s.data[at..at + 8];
    let word = l.scratch;
    window[0] = word as! u8;
    window[1] = (word >> 8) as! u8;
    window[2] = (word >> 16) as! u8;
    window[3] = (word >> 24) as! u8;
    window[4] = (word >> 32) as! u8;
    window[5] = (word >> 40) as! u8;
    window[6] = (word >> 48) as! u8;
    window[7] = (word >> 56) as! u8;
    l.word += 1;
    let spill = total - 64;
    l.filled = spill;
    l.scratch = if spill == 0 { 0 } else { value >> ((bits - spill) as! u64) };
}

// wire_fits is serialize_bits_write_check: a count outside [1, width] or a
// value wider than its count is write misuse, and aborts (U8, U9).
inline fn wire_fits(value: u64, bits: i64, width: i64) {
    if bits < 1 || bits > width {
        abort(if width == 32 { "serialize: bits must be in [1,32]" } else { "serialize: bits must be in [1,64]" });
    }
    if bits != 64 && value >> (bits as! u64) != 0 { abort("serialize: value must fit in bits"); }
}

// wire_ready is serialize_read_check: the latch first, then the room left
// (bit_index + n > 8 * bytes, written so it cannot overflow).
inline fn wire_ready(s: ReadStream&, bits: i64) -> bool {
    guard s.error == SERIALIZE_ERROR_NONE else { return false; }
    guard bits <= s.num_bits - s.bits_read else { return serialize_fail(s, SERIALIZE_ERROR_PAST_END); }
    return true;
}

// wire_take unpacks `bits` bits, bits in [1,56], the read check passed: one
// 8-byte window always holds them (bit_offset is at most 7), and the slack
// contract puts that window inside data (serialize_packer_read).
inline fn wire_take(s: ReadStream&, bits: i64) -> u64 {
    let begin = s.bits_read;
    let at = begin >> 3;
    let window = s.data[at..at + 8];
    let b0: u64 = window[0];
    let word = b0 | ((window[1] as u64) << 8) | ((window[2] as u64) << 16) | ((window[3] as u64) << 24)
        | ((window[4] as u64) << 32) | ((window[5] as u64) << 40) | ((window[6] as u64) << 48)
        | ((window[7] as u64) << 56);
    s.bits_read = begin + bits;
    return (word >> ((begin & 7) as! u64)) & (((1 as u64) << (bits as! u64)) - 1);
}

// wire_bits is serialize_bits: bits in [1,32], value < 2^bits (a write that
// breaks either aborts, as serialize_bits does; U8, U9).
inline fn wire_bits(s: WriteStream&, l: WireW&, value: u32, bits: i64) -> bool {
    wire_fits(value, bits, 32);
    wire_put(s, l, value, bits);
    return true;
}

inline fn wire_bits(s: ReadStream&, value: u32&, bits: i64) -> bool {
    guard bits >= 1 && bits <= 32 else { return serialize_fail(s, SERIALIZE_ERROR_VALUE_OUT_OF_RANGE); }
    guard wire_ready(s, bits) else { return false; }
    value = wire_take(s, bits) as! u32;
    return true;
}

// wire_bits64 is serialize_bits64: bits in [1,64]. A full 64-bit write and a
// read wider than one window (57 to 64 bits) are serialize.goose's own.
inline fn wire_bits64(s: WriteStream&, l: WireW&, value: u64, bits: i64) -> bool {
    if bits == 64 {
        wire_close(s, l);
        let ok = serialize_bits64(s, value, bits);
        wire_reopen(s, l);
        return ok;
    }
    wire_fits(value, bits, 64);
    wire_put(s, l, value, bits);
    return true;
}

inline fn wire_bits64(s: ReadStream&, value: u64&, bits: i64) -> bool {
    if bits > 56 { return serialize_bits64(s, value, bits); }
    guard bits >= 1 else { return serialize_fail(s, SERIALIZE_ERROR_VALUE_OUT_OF_RANGE); }
    guard wire_ready(s, bits) else { return false; }
    value = wire_take(s, bits);
    return true;
}

// wire_bool is serialize_bool: one bit, 1 for true.
inline fn wire_bool(s: WriteStream&, l: WireW&, value: bool) -> bool {
    wire_put(s, l, if value { 1 } else { 0 }, 1);
    return true;
}

inline fn wire_bool(s: ReadStream&, value: bool&) -> bool {
    guard wire_ready(s, 1) else { return false; }
    value = wire_take(s, 1) != 0;
    return true;
}
