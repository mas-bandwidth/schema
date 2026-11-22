// serialize_stream.lobster — the three streams: the types, the latch,
// terminality and bit accounting (DESIGN.md (a), (c), (e)), the number model
// helpers every family uses (DESIGN.md (c)), the fixed-width aliases of bits
// (STANDARD.md, Bit-Level Primitives, bits: "Fixed-width helpers are aliases
// for exactly this") and the abort every API misuse ends in.
//
// Nothing here packs a bit: the packing primitives pack_bits, pack_bits64,
// unpack_bits, unpack_bits64 and flush_bits are the bitpacker family's
// (src/serialize_bitpacker.lobster, DESIGN.md (f)).

// ---------------------------------------------------------------------------
// abort: the trap (IMPLEMENTING.md U9). Every write and measure misuse, and
// every stub, ends here: it prints the rule and exits 70. Lobster has no
// unchecked build, so it traps in every run. Never reached from data: a read
// refusal latches, it never aborts (U7).
// ---------------------------------------------------------------------------

def serialize_abort(message:string):
    print(message)
    set_exit_code(70)
    return from program

// The body of every stub until its family card lands (DESIGN.md (f)).
def serialize_stub(name:string):
    serialize_abort("serialize: " + name + " not implemented")

// ---------------------------------------------------------------------------
// the number model (DESIGN.md (c)): Lobster's int is signed 64-bit; a u64 is
// its two's-complement bit pattern. `>>` is arithmetic and a shift count is
// taken mod 64, so every right shift is masked and every count is guarded.
// ---------------------------------------------------------------------------

let serialize_sign_bit = 0x8000000000000000

// Unsigned a < b and a <= b: flip the sign bit, then compare signed.
def serialize_ult(a:int, b:int) -> bool:
    return (a ^ serialize_sign_bit) < (b ^ serialize_sign_bit)

def serialize_ule(a:int, b:int) -> bool:
    return (a ^ serialize_sign_bit) <= (b ^ serialize_sign_bit)

// The low `bits` bits set, bits in [0,64].
def serialize_mask(bits:int) -> int:
    if bits >= 64:
        return -1
    return (1 << bits) - 1

// Logical right shift, n in [0,64]: the arithmetic shift, then the mask.
def serialize_shr(v:int, n:int) -> int:
    if n == 0:
        return v
    if n >= 64:
        return 0
    return (v >> n) & ((1 << (64 - n)) - 1)

// Left shift, n in [0,64]: 64 shifts everything out (Lobster's 1 << 64 is 1).
def serialize_shl(v:int, n:int) -> int:
    if n >= 64:
        return 0
    return v << n

// ---------------------------------------------------------------------------
// the streams (DESIGN.md (a), (e))
// ---------------------------------------------------------------------------

// The format version this port implements (STANDARD.md, Format version).
let serialize_format_version = "2.0"

// ---------------------------------------------------------------------------
// refusal reasons (STANDARD.md, Refusal reasons): the closed list, spelled by
// serialize_refusal_name exactly as STANDARD.md spells them. A stream latches
// the FIRST refusal's reason; a later refusal on a failed stream is its echo
// and keeps it. `rejected` (standard 2.1) is the caller's own refusal, appended
// last so no other member moves (IMPLEMENTING.md U2).
// ---------------------------------------------------------------------------

enum serialize_refusal:
    serialize_refusal_none
    serialize_refusal_past_end
    serialize_refusal_value_out_of_range
    serialize_refusal_nonzero_padding
    serialize_refusal_trailing_data
    serialize_refusal_non_finite_float
    serialize_refusal_invalid_utf8
    serialize_refusal_embedded_nul
    serialize_refusal_non_canonical
    serialize_refusal_rejected

let serialize_refusal_names = [
    "none",
    "past_end",
    "value_out_of_range",
    "nonzero_padding",
    "trailing_data",
    "non_finite_float",
    "invalid_utf8",
    "embedded_nul",
    "non_canonical",
    "rejected"
]

// True for a member of the closed list other than none.
def serialize_refusal_is_reason(refusal:serialize_refusal) -> bool:
    let r = int(refusal)
    return r > 0 and r < length(serialize_refusal_names)

// The family-wide name of a reason ("past_end"); "none" for none
// (IMPLEMENTING.md U14). A value outside the closed list is the caller
// refusing without saying why, which a stream records as rejected, so it is
// named "rejected" here too, never a trap.
def serialize_refusal_name(refusal:serialize_refusal) -> string:
    if refusal == serialize_refusal_none:
        return "none"
    if not serialize_refusal_is_reason(refusal):
        return "rejected"
    return serialize_refusal_names[int(refusal)]

abstract class serialize_stream:
    // The latch: once true every operation fails, consuming nothing and
    // writing no destination (STANDARD.md, Reader Obligations, "Failure is
    // terminal"). Set only through fail().
    failed:bool = false

    // Why the stream failed: the FIRST refusal's reason, none until then
    // (STANDARD.md, Refusal reasons). Set only through fail().
    refusal:serialize_refusal = serialize_refusal_none

    def ok() -> bool:
        return not failed

    def error() -> bool:
        return failed

    // The first refusal's name, as STANDARD.md spells it ("none" before one).
    def refusal_name() -> string:
        return serialize_refusal_name(refusal)

    // serialize_read_fail: latch with a reason, return false. Only the first
    // reason is kept; later ones are echoes. A refusal with no reason, or with
    // a value outside the closed list, records rejected: a refused stream
    // never reports none (IMPLEMENTING.md U14). The write and measure streams
    // override this: there it returns false and records nothing (U6, U9).
    def fail(reason:serialize_refusal) -> bool:
        if not failed:
            refusal = if serialize_refusal_is_reason(reason): reason else: serialize_refusal_rejected
        failed = true
        return false

// A write stream over a caller-owned buffer (DESIGN.md (d)): the words are
// stored into the caller's string in place, so the caller reads its own buffer
// back, bytes_processed() bytes of it, after flush(). No copy. A write stream
// has no refusal state: write misuse traps (IMPLEMENTING.md U9), and fail
// returns false and records nothing, so it never fails.
class serialize_write_stream : serialize_stream
    buffer:string
    num_bits:int
    bits_written:int = 0
    word_index:int = 0
    scratch:int = 0
    scratch_bits:int = 0

    def fail(why:serialize_refusal) -> bool: return false

// A read stream over caller data (DESIGN.md (d)). data must extend 8 bytes
// past `bytes` (the slack contract, STANDARD.md Reader Obligations, "the
// eight-byte slack contract"): the reader loads 64-bit windows at byte
// granularity and never interprets a byte at or past `bytes`.
class serialize_read_stream : serialize_stream
    data:string
    num_bits:int
    bits_read:int = 0

// A measure stream: a conservative bound, never a refusal (STANDARD.md, The
// Measure Stream). It makes every check a write makes, except room, by the
// same trap; its fail returns false and latches nothing, so an object that
// refuses answers false and never poisons it (IMPLEMENTING.md U6, U9).
class serialize_measure_stream : serialize_stream
    bits_written:int = 0

    def fail(reason:serialize_refusal) -> bool: return false

// serialize_write_stream_init. The buffer's length is the capacity and must be
// a multiple of 8 bytes: the writer stores whole 64-bit words.
constructor serialize_write_stream(buffer:string):
    if length(buffer) % 8 != 0:
        serialize_abort("serialize: write buffer bytes must be a multiple of 8")
    return serialize_write_stream { buffer: buffer, num_bits: length(buffer) * 8 }

// serialize_read_stream_init: the fast path, in place, under the slack
// contract. A caller that breaks the contract (a negative byte count, or data
// that does not extend 8 bytes past it) gets a stream refused at
// initialization as value_out_of_range over no bits, decided by its own
// arguments, never later by data. A reader never traps (IMPLEMENTING.md U7).
constructor serialize_read_stream(data:string, bytes:int):
    if bytes < 0 or length(data) < bytes + 8:
        return serialize_read_stream { data: data, num_bits: 0, failed: true, refusal: serialize_refusal_value_out_of_range }
    return serialize_read_stream { data: data, num_bits: bytes * 8 }

// serialize_read_stream_init_padded: copies exact-length data into a fresh
// string with 8 zero bytes of slack, then reads the copy.
def serialize_read_stream_padded(data:string) -> serialize_read_stream:
    let bytes = length(data)
    let padded = ensure_size(string_with_capacity(bytes + 8), bytes + 8, 0)
    write_substring(padded, 0, data, false)
    return serialize_read_stream(padded, bytes)

constructor serialize_measure_stream():
    return serialize_measure_stream {}

// ---------------------------------------------------------------------------
// direction (DESIGN.md (a): one spelling for three streams needs it)
// ---------------------------------------------------------------------------

def is_writing(s::serialize_write_stream) -> bool: return true
def is_reading(s::serialize_write_stream) -> bool: return false
def is_writing(s::serialize_read_stream) -> bool: return false
def is_reading(s::serialize_read_stream) -> bool: return true
def is_writing(s::serialize_measure_stream) -> bool: return false
def is_reading(s::serialize_measure_stream) -> bool: return false

// ---------------------------------------------------------------------------
// accounting (STANDARD.md, General Conventions, "Bit index and alignment")
// ---------------------------------------------------------------------------

def bits_processed(s::serialize_write_stream) -> int: return bits_written
def bits_processed(s::serialize_read_stream) -> int: return bits_read
def bits_processed(s::serialize_measure_stream) -> int: return bits_written

// The meaningful length: bits rounded up to a byte (STANDARD.md, Flush).
def bytes_processed(s::serialize_write_stream) -> int: return (bits_written + 7) >> 3
def bytes_processed(s::serialize_read_stream) -> int: return (bits_read + 7) >> 3
def bytes_processed(s::serialize_measure_stream) -> int: return (bits_written + 7) >> 3

def bits_available(s::serialize_write_stream) -> int: return num_bits - bits_written
def bits_remaining(s::serialize_read_stream) -> int: return num_bits - bits_read

// (8 - bit_index % 8) % 8 (STANDARD.md, General Conventions).
def align_bits(s::serialize_write_stream) -> int: return (8 - (bits_written & 7)) & 7
def align_bits(s::serialize_read_stream) -> int: return (8 - (bits_read & 7)) & 7

// serialize_write_flush: write out the partial scratch word (STANDARD.md,
// Flush). Call once, after the final write. Flush is required: the caller's
// buffer is unspecified until it (IMPLEMENTING.md U11).
def flush(s::serialize_write_stream):
    s.flush_bits()

// ---------------------------------------------------------------------------
// the gates every operation passes before any bit moves (DESIGN.md (e))
// ---------------------------------------------------------------------------

// Write: trap when `bits` more would pass the end of the buffer
// (IMPLEMENTING.md U9, U10). Called once with the WHOLE width of the
// operation, before any bit of it moves.
def write_gate(s::serialize_write_stream, bits:int) -> bool:
    if bits_written + bits > num_bits:
        serialize_abort("serialize: write does not fit the room left in the buffer")
    return true

// Read: refuse when latched; refuse (latch) past the end, the test
// bit_index + n > 8 * bytes (STANDARD.md, Reader Obligations, "Reading past
// the end must fail").
def read_gate(s::serialize_read_stream, bits:int) -> bool:
    if failed:
        return false
    if bits_read + bits > num_bits:
        return s.fail(serialize_refusal_past_end)
    return true

// Measure: add the width; never refuses (STANDARD.md, The Measure Stream).
def measure_bits(s::serialize_measure_stream, bits:int) -> bool:
    bits_written += bits
    return true

// ---------------------------------------------------------------------------
// the fixed-width aliases of bits (STANDARD.md, bits, the helper table; bool)
// Write and measure: a value outside [0, 2^n) traps (IMPLEMENTING.md U6, U9);
// read: the gate, then the bits, then the destination; measure: the width.
// ---------------------------------------------------------------------------

// The width check a write and a measure make (IMPLEMENTING.md U8, U9).
def serialize_check_fits(value:int, bits:int):
    if bits < 64 and (value < 0 or value > serialize_mask(bits)):
        serialize_abort("serialize: value must fit in bits")

def serialize_fixed_width_write(s::serialize_write_stream, value:int, bits:int) -> bool:
    serialize_check_fits(value, bits)
    if not s.write_gate(bits):
        return false
    if bits <= 32:
        s.pack_bits(value, bits)
    else:
        s.pack_bits64(value, bits)
    return true

def serialize_bool(s::serialize_write_stream, value:bool):
    return value, s.serialize_fixed_width_write(value, 1)
def serialize_uint8(s::serialize_write_stream, value:int):
    return value, s.serialize_fixed_width_write(value, 8)
def serialize_uint16(s::serialize_write_stream, value:int):
    return value, s.serialize_fixed_width_write(value, 16)
def serialize_uint32(s::serialize_write_stream, value:int):
    return value, s.serialize_fixed_width_write(value, 32)
def serialize_uint64(s::serialize_write_stream, value:int):
    return value, s.serialize_fixed_width_write(value, 64)

def serialize_bool(s::serialize_read_stream, value:bool):
    if not s.read_gate(1):
        return value, false
    return bool(s.unpack_bits(1)), true
def serialize_uint8(s::serialize_read_stream, value:int):
    if not s.read_gate(8):
        return value, false
    return s.unpack_bits(8), true
def serialize_uint16(s::serialize_read_stream, value:int):
    if not s.read_gate(16):
        return value, false
    return s.unpack_bits(16), true
def serialize_uint32(s::serialize_read_stream, value:int):
    if not s.read_gate(32):
        return value, false
    return s.unpack_bits(32), true
def serialize_uint64(s::serialize_read_stream, value:int):
    if not s.read_gate(64):
        return value, false
    return s.unpack_bits64(64), true

def serialize_bool(s::serialize_measure_stream, value:bool):
    return value, s.measure_bits(1)
def serialize_uint8(s::serialize_measure_stream, value:int):
    serialize_check_fits(value, 8)
    return value, s.measure_bits(8)
def serialize_uint16(s::serialize_measure_stream, value:int):
    serialize_check_fits(value, 16)
    return value, s.measure_bits(16)
def serialize_uint32(s::serialize_measure_stream, value:int):
    serialize_check_fits(value, 32)
    return value, s.measure_bits(32)
def serialize_uint64(s::serialize_measure_stream, value:int):
    serialize_check_fits(value, 64)
    return value, s.measure_bits(64)

// ---------------------------------------------------------------------------
// align (STANDARD.md, Bit-Level Primitives, align): zero bits to the next byte
// boundary, nothing when aligned; the reader refuses nonzero padding; the
// measure charges the worst case, 7 bits (STANDARD.md, The Measure Stream).
// Every form consults the latch first, an aligned one included (STANDARD.md,
// Reader Obligations, "Every read consults the failure state").
// ---------------------------------------------------------------------------

def serialize_align(s::serialize_write_stream) -> bool:
    let n = s.align_bits()
    if not s.write_gate(n):
        return false
    if n > 0:
        s.pack_bits(0, n)
    return true

def serialize_align(s::serialize_read_stream) -> bool:
    let n = s.align_bits()
    if not s.read_gate(n):
        return false
    if n > 0 and s.unpack_bits(n) != 0:
        return s.fail(serialize_refusal_nonzero_padding)
    return true

def serialize_align(s::serialize_measure_stream) -> bool:
    return s.measure_bits(7)

// ---------------------------------------------------------------------------
// end (STANDARD.md, end): the object ends at the bit index after its last
// field, and the stream ends there too. A writer's end emits nothing and a
// measure's costs nothing. A reader's end consults the failure state first,
// then refuses a whole byte or more left over as trailing_data (even a zero
// byte), then reads the final byte's unused bits and refuses a set one as
// nonzero_padding; trailing data is checked first, and the bits of the last
// field are never examined. An accepted end leaves the stream exactly
// consumed. A packet reader reads the end after the top-level object.
// ---------------------------------------------------------------------------

// A writer's end is a no-op returning true (IMPLEMENTING.md U12).
def serialize_end(s::serialize_write_stream) -> bool: return true

def serialize_end(s::serialize_read_stream) -> bool:
    if not s.read_gate(0):
        return false
    let remaining = s.bits_remaining()
    if remaining >= 8:
        return s.fail(serialize_refusal_trailing_data)
    // Fewer than 8 bits remain, so this is all of them: the final byte's padding.
    let padding_bits = remaining & 7
    if padding_bits > 0:
        let padding = s.unpack_bits(padding_bits)
        if padding != 0:
            return s.fail(serialize_refusal_nonzero_padding)
    return true

def serialize_end(s::serialize_measure_stream) -> bool:
    // A no-op returning true (IMPLEMENTING.md U12).
    return true

// ---------------------------------------------------------------------------
// reject: the caller's own refusal (STANDARD.md, Refusal reasons, rejected;
// IMPLEMENTING.md U2). On a read it latches rejected through the same latch as
// every wire refusal, so it is terminal and the first reason wins. On a write
// or a measure there is nothing to refuse: it is caller misuse, and it traps
// (IMPLEMENTING.md U9; this port has no unchecked build).
// ---------------------------------------------------------------------------

def reject(s::serialize_read_stream) -> bool:
    return s.fail(serialize_refusal_rejected)

def reject(s::serialize_write_stream) -> bool:
    serialize_abort("serialize: reject is read only")
    return false

def reject(s::serialize_measure_stream) -> bool:
    serialize_abort("serialize: reject is read only")
    return false

// Caller misuse on a read (a bad declaration, a negative count, a destination
// too small) refuses value_out_of_range before any bit moves, and never aborts
// (IMPLEMENTING.md U7, U16). A failed stream keeps its first reason.
def serialize_read_misuse(s::serialize_read_stream) -> bool:
    return s.fail(serialize_refusal_value_out_of_range)
