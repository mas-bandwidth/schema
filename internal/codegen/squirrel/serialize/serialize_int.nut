// serialize_int.nut: the ranged 32-bit and 64-bit integers, the family card of
// serialize-squirrel-int (DESIGN.md (h)). A module of the closed loader
// (DESIGN.md (f)): vargv[0] is this module's exports table and vargv[1] the
// loader's require.
//
// THE NUMBER MODEL (DESIGN.md (c)): one 64-bit SQInteger; uint64 is the same
// bits. An unsigned comparison is `(a ^ MIN64) < (b ^ MIN64)` with MIN64 =
// 0x8000000000000000, an unsigned right shift is `>>>`, a shift count stays in
// [0,63], and nothing overflows: an int64 range wider than 2^63 computes its
// span, offset and value through serialize_int128.nut's 64-bit lanes, exact
// mod 2^64 (what Go's `uint64(max) - uint64(min)` is), and every narrower
// range keeps plain arithmetic, where nothing can wrap (wide64 below).
local _M = vargv[0];
local require = vargv[1];

local streams = require("serialize_stream");
local int128 = require("serialize_int128");
local SerializeError = streams.SerializeError;

local PANIC_MINMAX = "serialize: min must not be greater than max";
local PANIC_INT32 = "serialize: int bounds must be in [-2147483648, 2147483647]";
local INT32_MIN = -0x80000000;
local INT32_MAX = 0x7FFFFFFF;
local MIN64 = 0x8000000000000000;
local MAX64 = 0x7FFFFFFFFFFFFFFF;

// the 64-bit lanes (serialize_int128.nut; DESIGN.md (c)): exact modulo 2^64,
// never a signed overflow in the VM
local add64 = int128.add64;
local sub64 = int128.sub64;

// wide64(min, max): true when max - min does not fit in 63 bits, the only
// int64 ranges whose offset arithmetic can overflow a SQInteger. A range
// whose bounds share a sign never is; otherwise max - min > MAX64 exactly
// when max > MAX64 + min, a sum that cannot overflow because min < 0. Every
// other range keeps plain arithmetic: its span fits, and an offset at most
// the span added to min is at most max.
local wide64 = function(min, max) {
  return min < 0 && max >= 0 && max > MAX64 + min;
};

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
  return bitlen64(wide64(min, max) ? sub64(max, min) : max - min);
};

local write = {};
local read = {};
local measure = {};

// serializeInt(s, h, min, max): the int (ranged) codec. value - min is an in
// the bits the range needs; a degenerate range min == max costs zero bits and
// the value is the range (STANDARD.md int). A value outside [min,max] is
// write misuse: it traps and writes nothing (IMPLEMENTING.md U9; DESIGN.md (g)).
write.serializeInt <- function(s, h, min, max) {
  if (min < INT32_MIN || max > INT32_MAX) throw PANIC_INT32;
  if (min > max) throw PANIC_MINMAX;
  local v = h.value;
  if (v < min || v > max) return s._fail(SerializeError.ValueOutOfRange);
  local bits = bitsRequired32(min, max);
  if (bits == 0) return true;
  if (!s._room(bits)) return false;
  s._put((v - min) & 0xFFFFFFFF, bits);
  return true;
};

// A bad declaration on read (bounds outside int32, or min > max) is never a
// raise: it refuses ValueOutOfRange before any bit moves, after the failure
// state, so a failed stream keeps its first reason (IMPLEMENTING.md U5, U7,
// U16).
read.serializeInt <- function(s, h, min, max) {
  if (s._error != null) return false;
  if (min < INT32_MIN || max > INT32_MAX || min > max) return s._fail(SerializeError.ValueOutOfRange);
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
  if (min < INT32_MIN || max > INT32_MAX) throw PANIC_INT32;
  if (min > max) throw PANIC_MINMAX;
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
  local v = h.value;
  if (v < min || v > max) return s._fail(SerializeError.ValueOutOfRange);
  local bits = bitsRequired64(min, max);
  if (bits == 0) return true;
  if (!s._room(bits)) return false;
  local off = wide64(min, max) ? sub64(v, min) : v - min;
  if (bits <= 32) {
    s._put(off & 0xFFFFFFFF, bits);
  } else {
    s._put(off & 0xFFFFFFFF, 32);
    s._put(off >>> 32, bits - 32);
  }
  return true;
};

read.serializeInt64 <- function(s, h, min, max) {
  if (s._error != null) return false;
  if (min > max) return s._fail(SerializeError.ValueOutOfRange);
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
  local value;
  if (wide64(min, max)) {
    if ((off ^ MIN64) > (sub64(max, min) ^ MIN64)) return s._fail(SerializeError.ValueOutOfRange);
    value = add64(off, min);
  } else {
    if (off > max - min) return s._fail(SerializeError.ValueOutOfRange);
    value = off + min;
  }
  h.value = value;
  return true;
};

measure.serializeInt64 <- function(s, h, min, max) {
  if (min > max) throw PANIC_MINMAX;
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
