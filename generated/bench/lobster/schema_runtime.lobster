// schema_runtime.lobster — the Lobster target's shared runtime
// (docs/PORT-LOBSTER.md, THE RUNTIME LAYOUT). Emitted beside every generated
// module and imported as `import .schema_runtime`. It holds what every
// surface shares: the Int128 struct, the byte-buffer constructors and the
// number model's helpers (docs/PORT-LOBSTER.md, THE NUMBER MODEL). The
// type-wire codec's files (bitreader.lobster, bitwriter.lobster) join it
// under runtime/.
// SPDX-License-Identifier: NONE — emitted output, yours under terms of your choice.

namespace schema_rt

// Int128 is a 128-bit value as its two's-complement lanes: lo is bits 0..63
// and hi bits 64..127, each an int holding the lane's bit pattern. It is the
// wire's own order (low lane first, docs/SPEC-TABLES.md §3), and a struct, so
// it is copied by value and never allocates.
struct Int128:
    lo:int
    hi:int

// zeros is a fresh byte string of n zero bytes: the storage of a string(N),
// bytes(N) or [N]uint8 field, born at its maximum size (SPEC §4.7). The
// empty literal is a constant, so ensure_size returns a new string.
def zeros(n:int) -> string:
    return ensure_size("", n, 0)

// put writes bytes into a byte string at 0, in place, and returns it.
def put(buf:string, b:string) -> string:
    return write_substring(buf, 0, b, false)

// f64bits is the bit pattern of a double; f64frombits its inverse
// (docs/PORT-LOBSTER.md, FLOAT32).
def f64bits(x:float) -> int:
    return read_int64_le(write_float64_le(zeros(8), 0, x), 0)

def f64frombits(b:int) -> float:
    return read_float64_le(write_int64_le(zeros(8), 0, b), 0)

// fround rounds a double to the nearest float32 the way a float32 field's
// value is stored before it is written, Go's float32(v): write_float32_le is
// the C++ float conversion, IEEE round to nearest, ties to even
// (docs/PORT-LOBSTER.md, FLOAT32).
def fround(x:float) -> float:
    return read_float32_le(write_float32_le(zeros(4), 0, x), 0)

// f32bits is the 32 bits of a float32 value, as read_uint32_le reads them.
def f32bits(x:float) -> int:
    return read_uint32_le(write_float32_le(zeros(4), 0, x), 0)

// THE NUMBER MODEL (docs/PORT-LOBSTER.md): Lobster has one integer, int,
// signed 64-bit; >> is arithmetic and a shift count is taken mod 64.

// shr is the logical right shift of a 64-bit word: the arithmetic shift,
// then the mask that clears the copied sign bits; a count of 0 is the word
// and a count of 64 or more is 0, never the count mod 64.
def shr(a:int, n:int) -> int:
    if n <= 0:
        return a
    if n >= 64:
        return 0
    return (a >> n) & ((1 << (64 - n)) - 1)

// shl is the left shift with the same guards.
def shl(a:int, n:int) -> int:
    if n <= 0:
        return a
    if n >= 64:
        return 0
    return a << n

// ult is the unsigned order of two 64-bit words: flip the sign bit of
// each, then compare signed.
def ult(a:int, b:int) -> bool:
    return (a ^ 0x8000000000000000) < (b ^ 0x8000000000000000)
