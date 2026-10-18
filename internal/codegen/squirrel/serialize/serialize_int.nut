// serialize_int.nut: the ranged 32-bit and 64-bit integers, the family card of
// serialize-squirrel-int (DESIGN.md (h)). A module of the closed loader
// (DESIGN.md (f)): vargv[0] is this module's exports table and vargv[1] the
// loader's require.
//
// THE NUMBER MODEL (DESIGN.md (c)): one 64-bit SQInteger; uint64 is the same
// bits. An unsigned comparison is `(a ^ MIN64) < (b ^ MIN64)` with MIN64 =
// 0x8000000000000000, an unsigned right shift is `>>>`, a shift count stays in
// [0,63], and nothing overflows (a 64-bit difference is computed as the
// wrapping subtraction of the bits, which is exact mod 2^64: what Go's
// `uint64(max) - uint64(min)` is).
local _M = vargv[0];
local require = vargv[1];

local streams = require("serialize_stream");
local SerializeError = streams.SerializeError;

local PANIC_MINMAX = "serialize: min must not be greater than max";
local MIN64 = 0x8000000000000000;

// bitlen64(x): the number of bits needed to represent the unsigned 64-bit
// value x (0 for zero, 64 when the top bit is set)
local bitlen64 = function(x) {
  if (x == 0) return 0;
  if (x < 0) return 64;
  local n = 1;
  local v = x;
  if (v >= 0x100000000) { n += 32; v = v >>> 32; }
  if (v >= 0x10000) { n += 16; v = v >>> 16; }
  if (v >= 0x100) { n += 8; v = v >>> 8; }
  if (v >= 0x10) { n += 4; v = v >>> 4; }
  if (v >= 0x4) { n += 2; v = v >>> 2; }
  if (v >= 0x2) { n += 1; }
  return n;
};

// bitsRequired32(min, max): the int range's width: zero when min == max, else
// the bit length of the unsigned 32-bit span (STANDARD.md int; serialize.go
// BitsRequired)
local bitsRequired32 = function(min, max) {
  if (min == max) return 0;
  return bitlen64((max - min) & 0xFFFFFFFF);
};

// bitsRequired64(min, max): the int64 range's width: zero when min == max,
// else the bit length of the unsigned 64-bit span, so a range wider than 2^63
// is exact (STANDARD.md int64; serialize.go BitsRequired64)
local bitsRequired64 = function(min, max) {
  if (min == max) return 0;
  return bitlen64(max - min);
};

local write = {};
local read = {};
local measure = {};

// serializeInt(s, h, min, max): the int (ranged) codec. value - min is an in
// the bits the range needs; a degenerate range min == max costs zero bits and
// the value is the range (STANDARD.md int). A value outside [min,max] latches
// ValueOutOfRange and writes nothing (DESIGN.md (g)).
write.serializeInt <- function(s, h, min, max) {
  if (min > max) throw PANIC_MINMAX;
  if (s._error != null) return false;
  local v = h.value;
  if (v < min || v > max) return s._fail(SerializeError.ValueOutOfRange);
  local bits = bitsRequired32(min, max);
  if (bits == 0) return true;
  if (!s._room(bits)) return false;
  s._put((v - min) & 0xFFFFFFFF, bits);
  return true;
};

read.serializeInt <- function(s, h, min, max) {
  if (min > max) throw PANIC_MINMAX;
  if (s._error != null) return false;
  local bits = bitsRequired32(min, max);
  if (bits == 0) { h.value = min; return true; }
  if (!s._room(bits)) return false;
  local unsigned = s._take(bits);
  // the reader compares the decoded offset against max - min in the unsigned
  // domain before it forms the value, which cannot overflow on hostile input
  // (STANDARD.md int)
  if (unsigned > ((max - min) & 0xFFFFFFFF)) return s._fail(SerializeError.ValueOutOfRange);
  h.value = unsigned + min;
  return true;
};

measure.serializeInt <- function(s, h, min, max) {
  if (min > max) throw PANIC_MINMAX;
  if (s._error != null) return false;
  local v = h.value;
  if (v < min || v > max) return s._fail(SerializeError.ValueOutOfRange);
  local bits = bitsRequired32(min, max);
  if (bits == 0) return true;
  if (!s._room(bits)) return false;
  s._put(0, bits);
  return true;
};

// serializeInt64(s, h, min, max): the int64 (ranged) codec. If the width is 32
// or fewer the offset is a single group; otherwise the low 32 bits are written
// first, then the remaining bits - 32 high bits (STANDARD.md int64).
write.serializeInt64 <- function(s, h, min, max) {
  if (min > max) throw PANIC_MINMAX;
  if (s._error != null) return false;
  local v = h.value;
  if (v < min || v > max) return s._fail(SerializeError.ValueOutOfRange);
  local bits = bitsRequired64(min, max);
  if (bits == 0) return true;
  if (!s._room(bits)) return false;
  local off = v - min;
  if (bits <= 32) {
    s._put(off & 0xFFFFFFFF, bits);
  } else {
    s._put(off & 0xFFFFFFFF, 32);
    s._put(off >>> 32, bits - 32);
  }
  return true;
};

read.serializeInt64 <- function(s, h, min, max) {
  if (min > max) throw PANIC_MINMAX;
  if (s._error != null) return false;
  local bits = bitsRequired64(min, max);
  if (bits == 0) { h.value = min; return true; }
  if (!s._room(bits)) return false;
  local off;
  if (bits <= 32) {
    off = s._take(bits);
  } else {
    local g0 = s._take(32);
    local g1 = s._take(bits - 32);
    off = g0 | (g1 << 32);
  }
  local span = max - min;
  if ((off ^ MIN64) > (span ^ MIN64)) return s._fail(SerializeError.ValueOutOfRange);
  h.value = off + min;
  return true;
};

measure.serializeInt64 <- function(s, h, min, max) {
  if (min > max) throw PANIC_MINMAX;
  if (s._error != null) return false;
  local v = h.value;
  if (v < min || v > max) return s._fail(SerializeError.ValueOutOfRange);
  local bits = bitsRequired64(min, max);
  if (bits == 0) return true;
  if (!s._room(bits)) return false;
  s._put(0, bits);
  return true;
};

_M.write <- write;
_M.read <- read;
_M.measure <- measure;
