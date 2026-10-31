// serialize_int128.nut: the ranged 128-bit integer and the raw uint128, the
// family card of serialize-squirrel-int (DESIGN.md (h)). A module of the
// closed loader (DESIGN.md (f)): vargv[0] is this module's exports table and
// vargv[1] the loader's require.
//
// THE NUMBER MODEL (DESIGN.md (c)): a 128-bit value is the array [lo, hi],
// each half the bits of that half (the low 64 bits first, matching the wire
// order). A lane is one SQInteger holding uint64 bits: unsigned LAN is
// `(a ^ MIN64) < (b ^ MIN64)` with MIN64 = 0x8000000000000000, an unsigned
// right shift is `>>>`, a shift count stays in [0,63], and nothing overflows
// (a value is composed from the lanes with shifts and masks only).
local _M = vargv[0];
local require = vargv[1];

local streams = require("serialize_stream");
local SerializeError = streams.SerializeError;

local PANIC_MINMAX = "serialize: min must not be greater than max";
local MIN64 = 0x8000000000000000;

// ucmp64(x, y): -1/0/1, x and y compared as unsigned 64-bit values
local ucmp64 = function(x, y) {
  local ax = x ^ MIN64;
  local ay = y ^ MIN64;
  if (ax < ay) return -1;
  if (ax > ay) return 1;
  return 0;
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

// THE 64-BIT LANES (DESIGN.md (c): signed overflow, and a left shift that
// loses bits, are C++ undefined behaviour in the VM, so the library never
// leaves a sum, a difference or a shift that can wrap to SQInteger
// arithmetic). Each is composed from the two 32-bit halves, where nothing can
// overflow, and is exact modulo 2^64. serialize_int.nut, serialize_bits.nut
// and serialize_fixed.nut reach them through this module.
local MASK32 = 0xFFFFFFFF;

// add64(a, b): a + b, wrapping modulo 2^64
local add64 = function(a, b) {
  local lo = (a & MASK32) + (b & MASK32);
  local hi = ((a >>> 32) + (b >>> 32) + (lo >>> 32)) & MASK32;
  return (hi << 32) | (lo & MASK32);
};

// sub64(a, b): a - b, wrapping modulo 2^64, with an explicit borrow
local sub64 = function(a, b) {
  local lo = (a & MASK32) - (b & MASK32);
  local borrow = lo < 0 ? 1 : 0;
  local hi = ((a >>> 32) - (b >>> 32) - borrow) & MASK32;
  return (hi << 32) | (lo & MASK32);
};

// shl64(x, n): x << n for n in [0,63], wrapping modulo 2^64: the bits that
// would leave the word are masked off first, so the VM's shift never loses a
// bit and never shifts a negative value
local shl64 = function(x, n) {
  if (n == 0) return x;
  return (x & (0xFFFFFFFFFFFFFFFF >>> n)) << n;
};

// u128sub(a, b): a - b, wrapping modulo 2^128 (STANDARD.md int128: the
// offset is computed in the unsigned domain)
local u128sub = function(a, b) {
  local lo = sub64(a[0], b[0]);
  local br = ucmp64(a[0], b[0]) < 0 ? 1 : 0;
  local hi = sub64(sub64(a[1], b[1]), br);
  return [lo, hi];
};

// u128add(a, b): a + b, wrapping modulo 2^128
local u128add = function(a, b) {
  local lo = add64(a[0], b[0]);
  local ca = ucmp64(a[0], lo) > 0 ? 1 : 0;
  local hi = add64(add64(a[1], b[1]), ca);
  return [lo, hi];
};

// u128cmp(a, b): -1/0/1, a and b compared as unsigned 128-bit values
local u128cmp = function(a, b) {
  local h = ucmp64(a[1], b[1]);
  if (h != 0) return h;
  return ucmp64(a[0], b[0]);
};

// i128cmp(a, b): -1/0/1, a and b compared as signed 128-bit values (the high
// lane compares signed, the low lane breaks ties unsigned)
local i128cmp = function(a, b) {
  if (a[1] < b[1]) return -1;
  if (a[1] > b[1]) return 1;
  return ucmp64(a[0], b[0]);
};

// u128len(x): the number of bits needed to represent the unsigned 128-bit
// value x (0 for zero): bits_required128(min, max) = Len(max - min)
local u128len = function(x) {
  if (x[1] != 0) return 64 + bitlen64(x[1]);
  return bitlen64(x[0]);
};

// the two-lane helpers serialize_fixed.nut reuses (DESIGN.md (h)): sub, cmp,
// lsh, rsh, each on [lo, hi]
local lsh = function(x, n) {
  if (n == 0) return x;
  if (n < 64) return [shl64(x[0], n), (shl64(x[1], n) | (x[0] >>> (64 - n)))];
  return [0, shl64(x[0], n - 64)];
};

local rsh = function(x, n) {
  if (n == 0) return x;
  if (n < 64) return [((x[0] >>> n) | shl64(x[1], 64 - n)), (x[1] >>> n)];
  return [(x[1] >>> (n - 64)), 0];
};

// putGroups(s, off, numBits): write the 128-bit offset least significant 32-bit
// group first, with the final group carrying the remainder (STANDARD.md int128:
// up to four groups; the same wire bits as writing the low 64-bit half then the
// high remainder)
local putGroups = function(s, off, numBits) {
  local lo = off[0];
  local hi = off[1];
  if (numBits <= 32) {
    s._put(lo & 0xFFFFFFFF, numBits);
  } else if (numBits <= 64) {
    s._put(lo & 0xFFFFFFFF, 32);
    s._put((lo >>> 32) & 0xFFFFFFFF, numBits - 32);
  } else if (numBits <= 96) {
    s._put(lo & 0xFFFFFFFF, 32);
    s._put((lo >>> 32) & 0xFFFFFFFF, 32);
    s._put(hi & 0xFFFFFFFF, numBits - 64);
  } else {
    s._put(lo & 0xFFFFFFFF, 32);
    s._put((lo >>> 32) & 0xFFFFFFFF, 32);
    s._put(hi & 0xFFFFFFFF, 32);
    s._put((hi >>> 32) & 0xFFFFFFFF, numBits - 96);
  }
};

// takeGroups(s, numBits): read the 128-bit offset, least significant 32-bit
// group first
local takeGroups = function(s, numBits) {
  if (numBits <= 32) {
    return [s._take(numBits), 0];
  } else if (numBits <= 64) {
    local g0 = s._take(32);
    local g1 = s._take(numBits - 32);
    return [(g0 | (g1 << 32)), 0];
  } else if (numBits <= 96) {
    local g0 = s._take(32);
    local g1 = s._take(32);
    local g2 = s._take(numBits - 64);
    return [(g0 | (g1 << 32)), g2];
  } else {
    local g0 = s._take(32);
    local g1 = s._take(32);
    local g2 = s._take(32);
    local g3 = s._take(numBits - 96);
    return [(g0 | (g1 << 32)), (g2 | (g3 << 32))];
  }
};

local write = {};
local read = {};
local measure = {};

// serializeInt128(s, h, min, max): the ranged 128-bit integer. min and max are
// [lo, hi] arrays; the offset from min is computed in the unsigned 128-bit
// domain, so a range wider than 2^127 is exact (STANDARD.md int128; DESIGN.md
// (c), (g)). A degenerate range min == max costs zero bits and the value comes
// from the range (STANDARD.md int).
write.serializeInt128 <- function(s, h, min, max) {
  if (i128cmp(min, max) > 0) throw PANIC_MINMAX;
  local v = h.value;
  if (i128cmp(v, min) < 0 || i128cmp(v, max) > 0) return s._fail(SerializeError.ValueOutOfRange);
  local numBits = u128len(u128sub(max, min));
  if (numBits == 0) return true;
  if (!s._room(numBits)) return false;
  putGroups(s, u128sub(v, min), numBits);
  return true;
};

// a bad declaration (min > max) refuses ValueOutOfRange on read, never a
// raise (IMPLEMENTING.md U7, U16)
read.serializeInt128 <- function(s, h, min, max) {
  if (s._error != null) return false;
  if (i128cmp(min, max) > 0) return s._fail(SerializeError.ValueOutOfRange);
  local numBits = u128len(u128sub(max, min));
  if (numBits == 0) { h.value = [min[0], min[1]]; return true; }
  if (!s._room(numBits)) return false;
  local off = takeGroups(s, numBits);
  // reject, never clamp: the decoded offset must be at most max - min in the
  // unsigned domain (STANDARD.md int128)
  if (u128cmp(off, u128sub(max, min)) > 0) return s._fail(SerializeError.ValueOutOfRange);
  h.value = u128add(off, min);
  return true;
};

measure.serializeInt128 <- function(s, h, min, max) {
  if (i128cmp(min, max) > 0) throw PANIC_MINMAX;
  local v = h.value;
  if (i128cmp(v, min) < 0 || i128cmp(v, max) > 0) return s._fail(SerializeError.ValueOutOfRange);
  local numBits = u128len(u128sub(max, min));
  if (numBits == 0) return true;
  if (!s._room(numBits)) return false;
  s._put(0, numBits);
  return true;
};

// serializeUint128(s, h): the raw unsigned 128-bit integer, always 128 bits:
// the low 64-bit half first, then the high half (STANDARD.md uint128)
write.serializeUint128 <- function(s, h) {
  if (!s._room(128)) return false;
  local v = h.value;
  s._put(v[0] & 0xFFFFFFFF, 32);
  s._put((v[0] >>> 32) & 0xFFFFFFFF, 32);
  s._put(v[1] & 0xFFFFFFFF, 32);
  s._put((v[1] >>> 32) & 0xFFFFFFFF, 32);
  return true;
};

read.serializeUint128 <- function(s, h) {
  if (s._error != null) return false;
  if (!s._room(128)) return false;
  local g0 = s._take(32);
  local g1 = s._take(32);
  local g2 = s._take(32);
  local g3 = s._take(32);
  local lo = g0 | (g1 << 32);
  local hi = g2 | (g3 << 32);
  h.value = [lo, hi];
  return true;
};

measure.serializeUint128 <- function(s, h) {
  if (!s._room(128)) return false;
  s._put(0, 128);
  return true;
};

_M.int128From64 <- function(x) {
  local hi = x < 0 ? 0xFFFFFFFFFFFFFFFF : 0;
  return [x, hi];
};

_M.uint128From64 <- function(x) {
  return [x, 0];
};

_M.add64 <- add64;
_M.sub64 <- sub64;
_M.shl64 <- shl64;
_M.add <- u128add;
_M.sub <- u128sub;
_M.cmp <- u128cmp;
_M.lsh <- lsh;
_M.rsh <- rsh;

_M.write <- write;
_M.read <- read;
_M.measure <- measure;
