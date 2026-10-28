// serialize_bits.nut: the bits, bool, fixed-width unsigned and align family of
// the Squirrel port (DESIGN.md (h); the family card of serialize-squirrel-bits).
// A module of the closed loader (DESIGN.md (f)): vargv[0] is this module's
// exports table and vargv[1] the loader's require.
//
// The wire is STANDARD.md, Bit-Level Primitives (bits, bool, uint128's halves,
// align), and the bytes are exactly serialize.go v1.15.1's write_stream.go /
// read_stream.go / measure_stream.go for the same operations (DESIGN.md (g)).
//
// THE NUMBER MODEL (DESIGN.md (c)): one 64-bit SQInteger (sq64). A uint64
// above 2^63 - 1 is the negative integer with the same 64 bits; an unsigned
// comparison is `(a ^ MIN) < (b ^ MIN)` with MIN = 0x8000000000000000, an
// unsigned right shift is `>>>`, and values are composed with shifts and masks
// only, so nothing ever overflows. A 128-bit value is the array [lo, hi].
local _M = vargv[0];
local require = vargv[1];

local Err = require("serialize_stream").SerializeError;

// panic text, verbatim from serialize.go's serialize.go (DESIGN.md (g): API
// misuse raises serialize.go's panic text).
local PANIC_BITS_RANGE = "serialize: bits must be in [1,32]";
local PANIC_BITS_RANGE64 = "serialize: bits must be in [1,64]";

// mask32(k): the low k bits as a mask, k in [1,32] (k == 32 is a full dword,
// where 1 << 32 would overflow).
local mask32 = function(k) { return k == 32 ? 0xffffffff : (1 << k) - 1; };

// bitlen(n): the number of bits of the unsigned 64-bit value n (stored in a
// SQInteger). A negative n has the top bit set, so its unsigned bit length is
// 64 (DESIGN.md (c): the unsigned reading).
local bitlen = function(n) {
  local b = 0;
  while (n != 0) { b++; n = n >>> 1; }
  return b;
};

// u128 helpers on [lo, hi] arrays: unsigned 128-bit comparison, subtraction
// (mod 2^128) and bit length. The lanes are SQIntegers; the unsigned
// comparison is the XOR-with-MIN trick (DESIGN.md (c)).
local u128sub = function(a, b) {
  local lo = a[0] - b[0];
  local borrow = ((a[0] ^ 0x8000000000000000) < (b[0] ^ 0x8000000000000000)) ? 1 : 0;
  return [lo, a[1] - b[1] - borrow];
};
local u128len = function(a) {
  if (a[1] != 0) return 64 + bitlen(a[1]);
  return bitlen(a[0]);
};

// THE EXPORTS (DESIGN.md (h)): range pricing and zigzag, as serialize.go's
// BitsRequired, BitsRequired64, BitsRequired128, SignedToUnsigned and
// UnsignedToSigned (serialize.go's serialize.go). min == max costs zero bits
// (STANDARD.md, int: a degenerate range costs zero bits).
_M.bitsRequired <- function(min, max) {
  if (min == max) return 0;
  return bitlen(max - min);
};
_M.bitsRequired64 <- function(min, max) {
  if (min == max) return 0;
  return bitlen(max - min);
};
_M.bitsRequired128 <- function(min, max) {
  if (min[0] == max[0] && min[1] == max[1]) return 0;
  return u128len(u128sub(max, min));
};
// signedToUnsigned / unsignedToSigned: serialize.go v1.15.1's int32 / uint32
// zig-zag (serialize.go's serialize.go: SignedToUnsigned uses uint32(n) << 1
// ^ (0 - uint32(n) >> 31), UnsignedToSigned uses (n >> 1) ^ (0 - (n & 1)).
// The operand is masked to the low 32 bits first so a negative int32's upper
// 32 bits do not leak into the result (DESIGN.md (c): uint64 is the same
// bits; the zig-zag is 32-bit).
_M.signedToUnsigned <- function(n) {
  local x = n & 0xffffffff;
  local lo = (x << 1) & 0xffffffff;
  return (x >>> 31) != 0 ? lo ^ 0xffffffff : lo;
};
_M.unsignedToSigned <- function(n) {
  local u = n & 0xffffffff;
  local r = (u & 1) != 0 ? (u >>> 1) ^ 0xffffffff : (u >>> 1);
  return (r & 0x80000000) != 0 ? r - 0x100000000 : r;
};

// THE ARMS (DESIGN.md (g), (h)). An arm is function(stream, ...) with the
// stream method's own arguments after the stream. The shape: (1) misuse
// checks raising serialize.go's panic text; (2) `if (!s._room(n)) return false;`
// for the whole width before any bit moves; (3) the moves; (4) on read, every
// refusal latched with s._fail, and only after all of them the holder assigned;
// (5) return true.

local write = {};
local read = {};
local measure = {};

// serializeBits: bits in [1,32], the low bits of the value (STANDARD.md, bits;
// serialize.go panicBitsRange). Bits above the width are masked, as serialize.go
// masks them.
write.serializeBits <- function(s, h, bits) {
  if (bits < 1 || bits > 32) throw PANIC_BITS_RANGE;
  if (!s._room(bits)) return false;
  s._put(h.value & mask32(bits), bits);
  return true;
}

// serializeBits64: bits in [1,64]; past 32 bits the low 32 bits are written
// first, then the remaining bits - 32 high bits (STANDARD.md, bits; DESIGN.md
// (b); serialize.go SerializeBits64).
write.serializeBits64 <- function(s, h, bits) {
  if (bits < 1 || bits > 64) throw PANIC_BITS_RANGE64;
  if (!s._room(bits)) return false;
  if (bits <= 32) {
    s._put(h.value & mask32(bits), bits);
  } else {
    s._put(h.value & 0xffffffff, 32);
    s._put((h.value >>> 32) & mask32(bits - 32), bits - 32);
  }
  return true;
}

// serializeBool: one bit, 1 for true and 0 for false (STANDARD.md, bool).
write.serializeBool <- function(s, h) {
  if (!s._room(1)) return false;
  s._put(h.value ? 1 : 0, 1);
  return true;
}

// the fixed-width unsigned helpers are aliases for serializeBits at that width
// (STANDARD.md, bits: the fixed-width helpers carry no range information).
write.serializeUint8 <- function(s, h) {
  if (!s._room(8)) return false;
  s._put(h.value & 0xff, 8);
  return true;
}
write.serializeUint16 <- function(s, h) {
  if (!s._room(16)) return false;
  s._put(h.value & 0xffff, 16);
  return true;
}
write.serializeUint32 <- function(s, h) {
  if (!s._room(32)) return false;
  s._put(h.value & 0xffffffff, 32);
  return true;
}
write.serializeUint64 <- function(s, h) {
  if (!s._room(64)) return false;
  s._put(h.value & 0xffffffff, 32);
  s._put((h.value >>> 32) & 0xffffffff, 32);
  return true;
}

// serializeAlign: pads with zero bits to the next byte boundary; nothing is
// written when already aligned (STANDARD.md, align).
write.serializeAlign <- function(s) {
  local n = s.alignBits();
  if (n == 0) return true;
  if (!s._room(n)) return false;
  s._put(0, n);
  return true;
}

read.serializeBits <- function(s, h, bits) {
  if (bits < 1 || bits > 32) throw PANIC_BITS_RANGE;
  if (!s._room(bits)) return false;
  h.value = s._take(bits);
  return true;
}

read.serializeBits64 <- function(s, h, bits) {
  if (bits < 1 || bits > 64) throw PANIC_BITS_RANGE64;
  if (!s._room(bits)) return false;
  if (bits <= 32) {
    h.value = s._take(bits);
  } else {
    h.value = s._take(32) | (s._take(bits - 32) << 32);
  }
  return true;
}

read.serializeBool <- function(s, h) {
  if (!s._room(1)) return false;
  h.value = s._take(1) != 0;
  return true;
}

read.serializeUint8 <- function(s, h) {
  if (!s._room(8)) return false;
  h.value = s._take(8);
  return true;
}
read.serializeUint16 <- function(s, h) {
  if (!s._room(16)) return false;
  h.value = s._take(16);
  return true;
}
read.serializeUint32 <- function(s, h) {
  if (!s._room(32)) return false;
  h.value = s._take(32);
  return true;
}
read.serializeUint64 <- function(s, h) {
  if (!s._room(64)) return false;
  h.value = s._take(32) | (s._take(32) << 32);
  return true;
}

read.serializeAlign <- function(s) {
  local n = s.alignBits();
  if (n == 0) return true;
  if (!s._room(n)) return false;
  if (s._take(n) != 0) return s._fail(Err.Align);
  return true;
}

// the measure stream charges exact width for every operation here (STANDARD.md,
// The Measure Stream); align charges the worst case of 7 bits, which
// MeasureStream.alignBits() returns (DESIGN.md (g)).
measure.serializeBits <- function(s, h, bits) {
  if (bits < 1 || bits > 32) throw PANIC_BITS_RANGE;
  s._put(0, bits);
  return true;
}
measure.serializeBits64 <- function(s, h, bits) {
  if (bits < 1 || bits > 64) throw PANIC_BITS_RANGE64;
  s._put(0, bits);
  return true;
}
measure.serializeBool <- function(s, h) { s._put(0, 1); return true; }
measure.serializeUint8 <- function(s, h) { s._put(0, 8); return true; }
measure.serializeUint16 <- function(s, h) { s._put(0, 16); return true; }
measure.serializeUint32 <- function(s, h) { s._put(0, 32); return true; }
measure.serializeUint64 <- function(s, h) { s._put(0, 64); return true; }
measure.serializeAlign <- function(s) { s._put(0, s.alignBits()); return true; }

_M.write <- write;
_M.read <- read;
_M.measure <- measure;
