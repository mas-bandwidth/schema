// serialize_bitpacker.lobster — the packer: the 64-bit scratch word, word stores and window
// loads, the bit primitives bits and bits64, and bits_required
// (STANDARD.md, General Conventions; Bit-Level Primitives, bits; DESIGN.md (b), (c), (d), (e), (f)).
import .serialize_stream

// Packs the low `bits` bits of `value`, bits in [1,32], into the 64-bit
// LSB-first scratch word and commits full 8-byte little-endian words to
// `s.buffer` (STANDARD.md, General Conventions, bits; DESIGN.md (b), (c)).
def pack_bits(s:serialize_write_stream, value:int, bits:int):
    let v = value & serialize_mask(bits)
    s.scratch = s.scratch | serialize_shl(v, s.scratch_bits)
    let total = s.scratch_bits + bits
    if total >= 64:
        write_int64_le(s.buffer, s.word_index * 8, s.scratch)
        s.word_index += 1
        let room = 64 - s.scratch_bits
        s.scratch_bits = total - 64
        s.scratch = if s.scratch_bits == 0: 0 else: serialize_shr(v, room)
    else:
        s.scratch_bits = total
    s.bits_written += bits

// Packs the low `bits` bits of `value`, bits in [1,64]: above 32 bits the low
// 32 bits go first as a 32-bit group, then the remaining bits - 32 high bits
// (STANDARD.md, Bit-Level Primitives, bits; DESIGN.md (b)).
def pack_bits64(s:serialize_write_stream, value:int, bits:int):
    if bits <= 32:
        s.pack_bits(value, bits)
    else:
        s.pack_bits(value & 0xFFFFFFFF, 32)
        s.pack_bits(serialize_shr(value, 32), bits - 32)

// Writes out any partially filled scratch word as an 8-byte little-endian word
// (STANDARD.md, General Conventions, Flush; DESIGN.md (b)).
def flush_bits(s:serialize_write_stream):
    if s.scratch_bits != 0:
        write_int64_le(s.buffer, s.word_index * 8, s.scratch)
        s.scratch = 0
        s.scratch_bits = 0
        s.word_index += 1

// Copies `count` bytes from `data[offset..offset+count]` at a byte-aligned
// stream position, using whole 64-bit words where the run allows and reloading
// any trailing partial word into `s.scratch` (STANDARD.md, bytes; DESIGN.md (d);
// serialize.c serialize_write_bytes).
def pack_bytes(s:serialize_write_stream, data:string, offset:int, count:int):
    if (s.bits_written & 7) != 0:
        serialize_abort("serialize: byte array serialization requires byte alignment")
    // The gate is consulted once with the whole width before any byte moves,
    // so a payload that does not fit traps and writes nothing (IMPLEMENTING.md
    // U9, U10).
    if count <= 0:
        return
    if not s.write_gate(count * 8):
        return
    if s.scratch_bits != 0:
        write_int64_le(s.buffer, s.word_index * 8, s.scratch)
    let dst = s.bits_written >> 3
    var i = 0
    while i + 8 <= count:
        write_int64_le(s.buffer, dst + i, read_uint64_le(data, offset + i))
        i += 8
    while i < count:
        write_int8_le(s.buffer, dst + i, read_uint8_le(data, offset + i))
        i += 1
    s.bits_written += count * 8
    s.word_index = s.bits_written >> 6
    let tail_bits = s.bits_written & 63
    if tail_bits != 0:
        s.scratch = read_uint64_le(s.buffer, s.word_index * 8) & serialize_mask(tail_bits)
    else:
        s.scratch = 0
    s.scratch_bits = tail_bits

// Unpacks `bits` bits, bits in [1,32], by loading one 64-bit little-endian
// window at byte granularity under the 8-byte slack contract (STANDARD.md,
// General Conventions, bits, Reader Obligations; DESIGN.md (b), (c), (d)).
def unpack_bits(s:serialize_read_stream, bits:int) -> int:
    let begin = s.bits_read
    s.bits_read = begin + bits
    let window = read_uint64_le(s.data, begin >> 3)
    return serialize_shr(window, begin & 7) & serialize_mask(bits)

// Unpacks `bits` bits, bits in [1,64]: above 32 bits the low 32 bits arrive
// first, then the remaining bits - 32 high bits (STANDARD.md, bits; DESIGN.md (b)).
def unpack_bits64(s:serialize_read_stream, bits:int) -> int:
    if bits <= 32:
        return s.unpack_bits(bits)
    let lo = s.unpack_bits(32)
    let hi = s.unpack_bits(bits - 32)
    return lo | serialize_shl(hi, 32)

// Copies `count` bytes from a byte-aligned stream position into
// `data[offset..offset+count]` in place, using whole 64-bit words where the
// run allows (STANDARD.md, bytes; DESIGN.md (d); serialize.c serialize_read_bytes).
def unpack_bytes(s:serialize_read_stream, data:string, offset:int, count:int):
    // An unaligned call is caller misuse: refused, never an abort, on a read
    // (IMPLEMENTING.md U7). serialize_bytes aligns first, so it never reaches this.
    if (s.bits_read & 7) != 0:
        s.serialize_read_misuse()
        return
    // Refuse a read past the end before any load, with the whole width, so no
    // byte is read outside the data and a zero-count call still tests the latch
    // (DESIGN.md (e); STANDARD.md, Reader Obligations, "Reading past the end
    // must fail", "No read touches memory past the data's end").
    if count <= 0:
        if not s.read_gate(0):
            return
        return
    if not s.read_gate(count * 8):
        return
    let src = s.bits_read >> 3
    var i = 0
    while i + 8 <= count:
        write_int64_le(data, offset + i, read_uint64_le(s.data, src + i))
        i += 8
    while i < count:
        write_int8_le(data, offset + i, read_uint8_le(s.data, src + i))
        i += 1
    s.bits_read += count * 8

// serialize_bits: bits in [1,32], value in [0, 2^bits) (STANDARD.md,
// Bit-Level Primitives, bits; DESIGN.md (a), (e), (f)).
def serialize_bits(s:serialize_write_stream, value:int, bits:int):
    if bits < 1 or bits > 32:
        serialize_abort("serialize: bits must be in [1,32]")
    serialize_check_fits(value, bits)
    if not s.write_gate(bits):
        return value, false
    s.pack_bits(value, bits)
    return value, true

def serialize_bits(s:serialize_read_stream, value:int, bits:int):
    // A bad bit count on a read is refused, never an abort (IMPLEMENTING.md U7, U16).
    if bits < 1 or bits > 32:
        return value, s.serialize_read_misuse()
    if not s.read_gate(bits):
        return value, false
    return s.unpack_bits(bits), true

def serialize_bits(s:serialize_measure_stream, value:int, bits:int):
    if bits < 1 or bits > 32:
        serialize_abort("serialize: bits must be in [1,32]")
    serialize_check_fits(value, bits)
    return value, s.measure_bits(bits)

// serialize_bits64: bits in [1,64], value a u64 bit pattern with zero bits
// at and above `bits` (STANDARD.md, Bit-Level Primitives, bits; DESIGN.md (a), (c), (e), (f)).
def serialize_bits64(s:serialize_write_stream, value:int, bits:int):
    if bits < 1 or bits > 64:
        serialize_abort("serialize: bits must be in [1,64]")
    serialize_check_fits(value, bits)
    if not s.write_gate(bits):
        return value, false
    s.pack_bits64(value, bits)
    return value, true

def serialize_bits64(s:serialize_read_stream, value:int, bits:int):
    // A bad bit count on a read is refused, never an abort (IMPLEMENTING.md U7, U16).
    if bits < 1 or bits > 64:
        return value, s.serialize_read_misuse()
    if not s.read_gate(bits):
        return value, false
    return s.unpack_bits64(bits), true

def serialize_bits64(s:serialize_measure_stream, value:int, bits:int):
    if bits < 1 or bits > 64:
        serialize_abort("serialize: bits must be in [1,64]")
    serialize_check_fits(value, bits)
    return value, s.measure_bits(bits)

// Bit length of a 64-bit unsigned pattern, 0 for 0 (serialize.c serialize_bit_length64).
def serialize_bit_length64(v:int) -> int:
    if v == 0:
        return 0
    if v < 0:
        return 64
    var n = 1
    var x = v
    if (x >> 32) != 0:
        n += 32
        x = x >> 32
    if (x >> 16) != 0:
        n += 16
        x = x >> 16
    if (x >> 8) != 0:
        n += 8
        x = x >> 8
    if (x >> 4) != 0:
        n += 4
        x = x >> 4
    if (x >> 2) != 0:
        n += 2
        x = x >> 2
    if (x >> 1) != 0:
        n += 1
    return n

// Bits required for [min, max] in the uint32 domain (STANDARD.md, int;
// serialize.c serialize_bits_required; DESIGN.md (a), (c)).
def serialize_bits_required(min:int, max:int) -> int:
    let span = (max - min) & 0xFFFFFFFF
    return serialize_bit_length64(span)

// Bits required for [min, max] over u64 patterns (STANDARD.md, int64;
// serialize.c serialize_bits_required64; DESIGN.md (a), (c)).
def serialize_bits_required64(min:int, max:int) -> int:
    if min == max:
        return 0
    return serialize_bit_length64(max - min)
