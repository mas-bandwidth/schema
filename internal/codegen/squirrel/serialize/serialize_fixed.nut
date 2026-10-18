// serialize_fixed.nut: the fixed point (Q format, ranged) family, the family
// card of serialize-squirrel-fixed (DESIGN.md (h)). A module of the closed
// loader (DESIGN.md (f)): vargv[0] is this module's exports table and vargv[1]
// the loader's require.
//
// STANDARD.md, "fixed (Q format, ranged)": a value is a raw integer in a
// storage type of exactly integer_bits + fraction_bits bits (the sign bit
// counts toward integer_bits), the bounds are whole real units, and the wire
// is the offset encoding over the RAW (scaled) bounds:
//
//   raw_min = min << fraction_bits
//   raw_max = max << fraction_bits
//   bits    = bit length of ( raw_max - raw_min )
//
// For storage of 64 bits or fewer the bytes are identical to
// serializeInt64(raw_value, raw_min, raw_max); the wide form is exactly 128
// bits of storage, with integer_bits + fraction_bits == 128. A degenerate
// range (min == max) costs zero bits on every storage width.
//
// THE NUMBER MODEL (DESIGN.md (c)): one 64-bit SQInteger, uint64 is the same
// bits, an unsigned compare is `(a ^ MIN64) < (b ^ MIN64)`, an unsigned right
// shift is `>>>`, every shift count stays in [0,63], and a 64-bit difference
// that can overflow is the two 32-bit lanes with an explicit borrow. A 128-bit
// value is [lo, hi], and the two-lane helpers come from serialize_int128.nut
// (DESIGN.md (h)).
local _M = vargv[0];
local require = vargv[1];

local streams = require("serialize_stream");
local int128 = require("serialize_int128");
local SerializeError = streams.SerializeError;

// serialize.go v1.15.1's panic texts, verbatim (DESIGN.md (g): API misuse
// raises naming the same rule)
local PANIC_PARAMS = "serialize: fixed point requires integer bits >= 1, fraction bits >= 0, a Q format that fits the storage width, and min <= max";
local PANIC_BOUNDS = "serialize: fixed point bounds in whole units do not fit the Q format";

local MIN64 = 0x8000000000000000;
local MAX64 = 0x7FFFFFFFFFFFFFFF;
local MASK32 = 0xFFFFFFFF;

// ucmp64(x, y): -1/0/1, x and y compared as unsigned 64-bit values (DESIGN.md
// (c))
local ucmp64 = function(x, y) {
  local ax = x ^ MIN64;
  local ay = y ^ MIN64;
  if (ax < ay) return -1;
  if (ax > ay) return 1;
  return 0;
};

// bitlen64(x): the number of bits needed to represent the unsigned 64-bit
// value x (0 for zero, 64 when the top bit is set), as serialize.go's
// BitsRequired64 (DESIGN.md (c))
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

// sub64(a, b): a - b, wrapping modulo 2^64, composed from the 32-bit lanes
// with an explicit borrow (DESIGN.md (c): a difference that can overflow is
// never left to signed arithmetic)
local sub64 = function(a, b) {
  local alo = a & MASK32;
  local blo = b & MASK32;
  local lo = alo - blo;
  local borrow = alo < blo ? 1 : 0;
  local ahi = (a >>> 32) & MASK32;
  local bhi = (b >>> 32) & MASK32;
  local hi = ahi - bhi - borrow;
  return ((hi & MASK32) << 32) | (lo & MASK32);
};

// add64(a, b): a + b, wrapping modulo 2^64 (DESIGN.md (c))
local add64 = function(a, b) {
  local alo = a & MASK32;
  local blo = b & MASK32;
  local lo = alo + blo;
  local carry = ucmp64(alo, lo & MASK32) > 0 ? 1 : 0;
  local hi = ((a >>> 32) & MASK32) + ((b >>> 32) & MASK32) + carry;
  return ((hi & MASK32) << 32) | (lo & MASK32);
};

// add128(a, b): a + b, wrapping modulo 2^128 (DESIGN.md (c))
local add128 = function(a, b) {
  local lo = a[0] + b[0];
  local carry = ucmp64(a[0], lo) > 0 ? 1 : 0;
  local hi = a[1] + b[1] + carry;
  return [lo, hi];
};

// shl64(x, n): x << n for n in [0,63], composed from the lanes so the shift
// count never leaves [0,63] (DESIGN.md (c))
local shl64 = function(x, n) {
  if (n == 0) return x;
  local alo = x & MASK32;
  local ahi = (x >>> 32) & MASK32;
  if (n >= 32) return (((alo << (n - 32)) & MASK32) << 32);
  local lo = (alo << n) & MASK32;
  local hi = ((ahi << n) | (alo >>> (32 - n))) & MASK32;
  return (hi << 32) | lo;
};

// fixedPointCapacity(integerBits, signed): the whole-unit bounds a Q format
// can represent, clamped to the int64 domain the bounds live in; the
// signedness follows the bounds, as serialize.go's fixedPointCapacity
// (STANDARD.md, fixed; the sign bit counts toward integer_bits)
local fixedPointCapacity = function(integerBits, signed) {
  if (signed) {
    if (integerBits >= 65) return [MIN64, MAX64];
    local minRepresentable = integerBits == 64 ? MIN64 : -(1 << (integerBits - 1));
    if (integerBits >= 64) return [minRepresentable, MAX64];
    return [minRepresentable, (1 << (integerBits - 1)) - 1];
  }
  if (integerBits >= 63) return [0, MAX64];
  return [0, (1 << integerBits) - 1];
};

// fixedPointParams64(integerBits, fractionBits, min, max): serialize.go v1.15.1's
// fixedPointParams64 (DESIGN.md (g); STANDARD.md, fixed). It raises
// panicFixedParams when the Q format or the bounds are misuse, and
// panicFixedBounds when a whole-unit bound does not fit the format. The raw
// bounds are whole-unit bounds shifted left by fractionBits in the unsigned
// domain, where a negative bound wraps two's complement, and the width is the
// bit length of the raw range (zero for a degenerate range, on every width).
local fixedPointParams64 = function(integerBits, fractionBits, min, max) {
  if (integerBits < 1 || fractionBits < 0 || integerBits + fractionBits > 64 || min > max) {
    throw PANIC_PARAMS;
  }
  local cap = fixedPointCapacity(integerBits, min < 0);
  if (min < cap[0] || max > cap[1]) throw PANIC_BOUNDS;
  local rawMin = shl64(min, fractionBits);
  local rawMax = shl64(max, fractionBits);
  local rawRange = sub64(rawMax, rawMin);
  local numBits = rawMin == rawMax ? 0 : bitlen64(rawRange);
  return { rawMin = rawMin, rawRange = rawRange, numBits = numBits };
};

// fixedPointParams128(integerBits, fractionBits, min, max): serialize.go
// v1.15.1's fixedPointParams128 (DESIGN.md (g); STANDARD.md, fixed). Storage is
// exactly 128 bits, so integerBits + fractionBits must equal 128. The raw
// offset math runs in the unsigned 128-bit domain; the width is the whole-unit
// range's bit length plus fractionBits (shifting a range left adds exactly
// that many bits), except a degenerate range, which costs zero bits on every
// storage width (serialize.go's serialize#54 note; STANDARD.md, fixed).
local fixedPointParams128 = function(integerBits, fractionBits, min, max) {
  if (integerBits < 1 || fractionBits < 0 || integerBits + fractionBits != 128 || min > max) {
    throw PANIC_PARAMS;
  }
  local cap = fixedPointCapacity(integerBits, min < 0);
  if (min < cap[0] || max > cap[1]) throw PANIC_BOUNDS;
  local rawMin = int128.lsh(int128.int128From64(min), fractionBits);
  local rawMax = int128.lsh(int128.int128From64(max), fractionBits);
  local rawRange = int128.sub(rawMax, rawMin);
  local numBits;
  if (min == max) {
    numBits = 0;
  } else {
    numBits = bitlen64(sub64(max, min)) + fractionBits;
  }
  return { rawMin = rawMin, rawRange = rawRange, numBits = numBits };
};

// putGroups64(s, off, numBits): the 64-bit offset's low 32 bits first, then
// the remaining high bits (STANDARD.md, fixed: the same split as
// serialize_bits and serialize_int64)
local putGroups64 = function(s, off, numBits) {
  if (numBits <= 32) {
    s._put(off & MASK32, numBits);
  } else {
    s._put(off & MASK32, 32);
    s._put(off >>> 32, numBits - 32);
  }
};

// takeGroups64(s, numBits): the 64-bit offset, low 32 bits first
local takeGroups64 = function(s, numBits) {
  if (numBits <= 32) return s._take(numBits);
  local g0 = s._take(32);
  local g1 = s._take(numBits - 32);
  return g0 | (g1 << 32);
};

// putGroups128(s, off, numBits): the 128-bit offset's least significant 32-bit
// group first, with the final group carrying the remainder (STANDARD.md,
// fixed: split exactly as serialize_bits splits wide values)
local putGroups128 = function(s, off, numBits) {
  local lo = off[0];
  local hi = off[1];
  if (numBits <= 32) {
    s._put(lo & MASK32, numBits);
  } else if (numBits <= 64) {
    s._put(lo & MASK32, 32);
    s._put((lo >>> 32) & MASK32, numBits - 32);
  } else if (numBits <= 96) {
    s._put(lo & MASK32, 32);
    s._put((lo >>> 32) & MASK32, 32);
    s._put(hi & MASK32, numBits - 64);
  } else {
    s._put(lo & MASK32, 32);
    s._put((lo >>> 32) & MASK32, 32);
    s._put(hi & MASK32, 32);
    s._put((hi >>> 32) & MASK32, numBits - 96);
  }
};

// takeGroups128(s, numBits): read the 128-bit offset, least significant
// 32-bit group first
local takeGroups128 = function(s, numBits) {
  if (numBits <= 32) {
    return [s._take(numBits), 0];
  } else if (numBits <= 64) {
    local g0 = s._take(32);
    local g1 = s._take(numBits - 32);
    return [g0 | (g1 << 32), 0];
  } else if (numBits <= 96) {
    local g0 = s._take(32);
    local g1 = s._take(32);
    local g2 = s._take(numBits - 64);
    return [g0 | (g1 << 32), g2];
  } else {
    local g0 = s._take(32);
    local g1 = s._take(32);
    local g2 = s._take(32);
    local g3 = s._take(numBits - 96);
    return [g0 | (g1 << 32), g2 | (g3 << 32)];
  }
};

local write = {};
local read = {};
local measure = {};

// serializeFixed64(s, h, integerBits, fractionBits, min, max): the 64-bit
// fixed point write (STANDARD.md, fixed; serialize.go SerializeFixed64).
// Misuse raises; a raw value outside the raw bounds latches ValueOutOfRange
// and writes nothing (DESIGN.md (g)).
write.serializeFixed64 <- function(s, h, integerBits, fractionBits, min, max) {
  local p = fixedPointParams64(integerBits, fractionBits, min, max);
  if (s._error != null) return false;
  local off = sub64(h.value, p.rawMin);
  if (ucmp64(off, p.rawRange) > 0) return s._fail(SerializeError.ValueOutOfRange);
  if (!s._room(p.numBits)) return false;
  if (p.numBits > 0) putGroups64(s, off, p.numBits);
  return true;
};

// read.serializeFixed64: reject, never clamp: the decoded offset must be at
// most raw_max - raw_min, and a read past the end fails with Overflow before
// it reads (STANDARD.md, fixed; Reader Obligations). A refusal leaves the
// holder untouched and the stream terminal (DESIGN.md (g)).
read.serializeFixed64 <- function(s, h, integerBits, fractionBits, min, max) {
  local p = fixedPointParams64(integerBits, fractionBits, min, max);
  if (s._error != null) return false;
  if (p.numBits == 0) { h.value = p.rawMin; return true; }
  if (!s._room(p.numBits)) return false;
  local off = takeGroups64(s, p.numBits);
  if (ucmp64(off, p.rawRange) > 0) return s._fail(SerializeError.ValueOutOfRange);
  h.value = add64(p.rawMin, off);
  return true;
};

// measure.serializeFixed64: the same width and the same range refusal as the
// writer, charged on the measure stream (STANDARD.md, The Measure Stream;
// serialize.go MeasureStream.SerializeFixed64)
measure.serializeFixed64 <- function(s, h, integerBits, fractionBits, min, max) {
  local p = fixedPointParams64(integerBits, fractionBits, min, max);
  if (s._error != null) return false;
  local off = sub64(h.value, p.rawMin);
  if (ucmp64(off, p.rawRange) > 0) return s._fail(SerializeError.ValueOutOfRange);
  if (!s._room(p.numBits)) return false;
  if (p.numBits > 0) s._put(0, p.numBits);
  return true;
};

// serializeFixed128(s, h, integerBits, fractionBits, min, max): the wide fixed
// point write, storage exactly 128 bits (STANDARD.md, fixed; serialize.go
// SerializeFixed128). A value is [lo, hi]; the raw offset and range run in the
// unsigned 128-bit domain, and a raw value outside the raw bounds latches
// ValueOutOfRange (DESIGN.md (g)).
write.serializeFixed128 <- function(s, h, integerBits, fractionBits, min, max) {
  local p = fixedPointParams128(integerBits, fractionBits, min, max);
  if (s._error != null) return false;
  local off = int128.sub(h.value, p.rawMin);
  if (int128.cmp(off, p.rawRange) > 0) return s._fail(SerializeError.ValueOutOfRange);
  if (!s._room(p.numBits)) return false;
  if (p.numBits > 0) putGroups128(s, off, p.numBits);
  return true;
};

// read.serializeFixed128: the decoded offset must be at most the raw range,
// rejected rather than clamped, and a read past the end is Overflow before it
// reads (STANDARD.md, fixed; Reader Obligations; DESIGN.md (g))
read.serializeFixed128 <- function(s, h, integerBits, fractionBits, min, max) {
  local p = fixedPointParams128(integerBits, fractionBits, min, max);
  if (s._error != null) return false;
  if (p.numBits == 0) { h.value = [p.rawMin[0], p.rawMin[1]]; return true; }
  if (!s._room(p.numBits)) return false;
  local off = takeGroups128(s, p.numBits);
  if (int128.cmp(off, p.rawRange) > 0) return s._fail(SerializeError.ValueOutOfRange);
  h.value = add128(p.rawMin, off);
  return true;
};

// measure.serializeFixed128: the same width and range refusal as the wide
// writer (STANDARD.md, The Measure Stream; serialize.go
// MeasureStream.SerializeFixed128)
measure.serializeFixed128 <- function(s, h, integerBits, fractionBits, min, max) {
  local p = fixedPointParams128(integerBits, fractionBits, min, max);
  if (s._error != null) return false;
  local off = int128.sub(h.value, p.rawMin);
  if (int128.cmp(off, p.rawRange) > 0) return s._fail(SerializeError.ValueOutOfRange);
  if (!s._room(p.numBits)) return false;
  if (p.numBits > 0) s._put(0, p.numBits);
  return true;
};

_M.write <- write;
_M.read <- read;
_M.measure <- measure;
