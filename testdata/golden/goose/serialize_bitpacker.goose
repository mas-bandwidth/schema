// serialize_bitpacker.goose -- the 64-bit LSB-first scratch word and the
// 8-byte little-endian window reader (STANDARD.md, General Conventions,
// Bit-Level Primitives; DESIGN.md (b), (c), (d), (e), (f)). Every function is
// called by the stream (src/serialize_stream.goose) and the families only
// after the gate, so none checks the latch, the bounds or the value again.

import .serialize_stream;

// Stores one 64-bit word at data[base..base+8] as 8 little-endian bytes by
// shift (STANDARD.md, General Conventions; DESIGN.md (b), (d)). The assert
// gives Goose's bounds analysis the window length so index checks inside the
// 8 stores can fold away.
fn serialize_bitpacker_store_word(data: u8[:], base: i64, word: u64) {
    assert(base >= 0 && base + 7 < data.len);
    data[base] = word as! u8;
    data[base + 1] = (word >> 8) as! u8;
    data[base + 2] = (word >> 16) as! u8;
    data[base + 3] = (word >> 24) as! u8;
    data[base + 4] = (word >> 32) as! u8;
    data[base + 5] = (word >> 40) as! u8;
    data[base + 6] = (word >> 48) as! u8;
    data[base + 7] = (word >> 56) as! u8;
}

// Loads one 64-bit little-endian window from data[base..base+8] (STANDARD.md,
// General Conventions, Implementation Law; DESIGN.md (b), (d)). The slack
// contract guarantees data.len >= bytes + 8, so every byte position inside the
// message has a full 8-byte window inside data.
fn serialize_bitpacker_load_word(data: const u8[:], base: i64) -> u64 {
    assert(base >= 0 && base + 7 < data.len);
    let b0: u64 = data[base];
    return b0
        | ((data[base + 1] as u64) << 8)
        | ((data[base + 2] as u64) << 16)
        | ((data[base + 3] as u64) << 24)
        | ((data[base + 4] as u64) << 32)
        | ((data[base + 5] as u64) << 40)
        | ((data[base + 6] as u64) << 48)
        | ((data[base + 7] as u64) << 56);
}

// Packs the low `bits` bits of value, bits in [1,64], value < 2^bits, the
// capacity already checked; advances bits_written (STANDARD.md, General
// Conventions, bits; DESIGN.md (b), (c)). Shift counts of 0 and 64 are handled
// explicitly because Goose masks shift counts to the operand width.
fn serialize_packer_write(s: WriteStream&, value: u64, bits: i64) {
    let mask: u64 = if bits == 64 { 0xffffffffffffffff } else { ((1 as u64) << (bits as u64)) - 1 };
    let v: u64 = value & mask;
    if s.scratch_bits == 0 {
        if bits == 64 {
            serialize_bitpacker_store_word(s.data, s.word_index * 8, v);
            s.word_index += 1;
        } else {
            s.scratch = v;
            s.scratch_bits = bits;
        }
    } else {
        let shift = s.scratch_bits as u64;
        let room = 64 - s.scratch_bits;
        s.scratch = s.scratch | (v << shift);
        if bits < room {
            s.scratch_bits += bits;
        } else {
            serialize_bitpacker_store_word(s.data, s.word_index * 8, s.scratch);
            s.word_index += 1;
            let spill = bits - room;
            s.scratch_bits = spill;
            s.scratch = if spill == 0 { 0 } else { v >> (room as u64) };
        }
    }
    s.bits_written += bits;
}

// Unpacks `bits` bits, bits in [1,64], the bounds already checked; advances
// bits_read. Loads 64-bit windows inside data (the slack contract) and never
// interprets a byte at or past num_bits / 8 (STANDARD.md, General Conventions,
// bits, Reader Obligations; DESIGN.md (b), (c), (d)).
fn serialize_packer_read(s: ReadStream&, bits: i64) -> u64 {
    let begin = s.bits_read;
    let byte_index = begin >> 3;
    let bit_offset = begin & 7;
    let window = serialize_bitpacker_load_word(s.data, byte_index);
    let avail = 64 - bit_offset;
    s.bits_read = begin + bits;
    if bits <= avail {
        let shifted = if bit_offset == 0 { window } else { window >> (bit_offset as u64) };
        let mask: u64 = if bits == 64 { 0xffffffffffffffff } else { ((1 as u64) << (bits as u64)) - 1 };
        return shifted & mask;
    }
    let lo = window >> (bit_offset as u64);
    let rem = bits - avail;
    let hi_mask: u64 = ((1 as u64) << (rem as u64)) - 1;
    let next_byte: u64 = s.data[byte_index + 8];
    let hi = next_byte & hi_mask;
    return lo | (hi << (avail as u64));
}

// Writes the partial scratch word out (STANDARD.md, General Conventions, Flush;
// DESIGN.md (b)).
fn serialize_packer_flush(s: WriteStream&) {
    if s.scratch_bits != 0 {
        serialize_bitpacker_store_word(s.data, s.word_index * 8, s.scratch);
        s.scratch = 0;
        s.scratch_bits = 0;
        s.word_index += 1;
    }
}

// Copies data at a byte-aligned position, the capacity already checked
// (STANDARD.md, bytes; serialize.c serialize_write_bytes). Stores the partial
// head word if any, copies the payload straight into data at the byte cursor,
// and reloads the trailing partial word into scratch so subsequent bit writes
// pack into it seamlessly.
fn serialize_packer_write_bytes(s: WriteStream&, data: const u8[:]) {
    if s.bits_written % 8 != 0 { abort("serialize: byte array serialization requires byte alignment"); }
    if data.len == 0 { return; }
    if s.scratch_bits != 0 {
        serialize_bitpacker_store_word(s.data, s.word_index * 8, s.scratch);
    }
    let byte_start = s.bits_written >> 3;
    for i in data.len {
        s.data[byte_start + i] = data[i];
    }
    s.bits_written += data.len * 8;
    s.word_index = s.bits_written / 64;
    let tail_bits = s.bits_written % 64;
    if tail_bits != 0 {
        let word = serialize_bitpacker_load_word(s.data, s.word_index * 8);
        let mask: u64 = ((1 as u64) << (tail_bits as u64)) - 1;
        s.scratch = word & mask;
    } else {
        s.scratch = 0;
    }
    s.scratch_bits = tail_bits;
}

// Copies data.len bytes from a byte-aligned position, the bounds already
// checked (STANDARD.md, bytes; serialize.c serialize_read_bytes).
fn serialize_packer_read_bytes(s: ReadStream&, data: u8[:]) {
    if s.bits_read % 8 != 0 { abort("serialize: byte array serialization requires byte alignment"); }
    let byte_start = s.bits_read >> 3;
    for i in data.len {
        data[i] = s.data[byte_start + i];
    }
    s.bits_read += data.len * 8;
}
