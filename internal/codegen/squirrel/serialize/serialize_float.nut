// serialize_float.nut: the float and double arms and the float32 narrowing of
// serialize-squirrel-float (DESIGN.md (h)). A module of the closed loader
// (DESIGN.md (f)): vargv[0] is this module's exports table and vargv[1] the
// loader's require.
//
// STANDARD.md float: the 32 bits of the IEEE-754 single-precision
// representation, no conversion; STANDARD.md double: the 64 bits, the low
// 32-bit group first. STANDARD.md, "Finite values only, every one bit exact":
// both carry FINITE values. A pattern whose exponent field is all ones (NaN
// with any payload, either sign, and both infinities) is refused on read as
// non_finite_float, tested on the RAW BITS before the holder is written, and
// a write or a measure of one is write misuse: it traps and writes nothing
// (IMPLEMENTING.md U6, U9). Every finite pattern, negative zero
// and the subnormals included, travels bit for bit.
//
// fround(x) (DESIGN.md (e)) narrows a double to the float32 Go's float32()
// gives: round to nearest, ties to even, the subnormal rounding, a finite
// double past float32's range to the signed infinity, infinities to
// themselves, and a NaN to the quiet float32 NaN of the same sign. The float
// write checks finiteness AFTER that narrowing, so a finite double above float32's range, which narrows to an
// infinity, is refused too. All of it is integer surgery on the bits of
// castf2i(x): the blob library's castf2i/casti2f reinterpret without
// conversion, and the 'f' view of a blob is never used (DESIGN.md (e)).
local _M = vargv[0];
local require = vargv[1];

local SerializeError = require("serialize_stream").SerializeError;

// floatBitsAreFinite(p) / doubleBitsAreFinite(b): the exponent field is not
// all ones (STANDARD.md, Finite values only), on the raw bits, never on a
// converted value
local floatBitsAreFinite = function(p) { return (p & 0x7F800000) != 0x7F800000; };
local doubleBitsAreFinite = function(b) { return (b & 0x7FF0000000000000) != 0x7FF0000000000000; };

// froundPattern(v): the 32-bit float32 pattern fround(v) narrows v to, as an
// unsigned 32-bit value. The narrowing is IEEE-754 round-to-nearest,
// ties-to-even on the dropped mantissa bits (DESIGN.md (e)).
local froundPattern = function(v) {
  local bits = castf2i(v);
  local sign = bits & 0x8000000000000000; // top bit, 0 or 0x8000000000000000
  local exp = (bits >>> 52) & 0x7FF;
  local frac = bits & 0xFFFFFFFFFFFFF;
  local p;
  if (exp == 0x7FF) {
    // infinity or NaN: infinities to themselves, a NaN to the quiet float32
    // NaN of the same sign. Neither is a value the format carries (STANDARD.md,
    // Finite values only): the float write refuses both, so no payload is kept.
    if (frac == 0) p = sign >>> 32 | 0x7F800000;
    else p = sign >>> 32 | 0x7FC00000;
  } else if (exp == 0) {
    // a double subnormal is below 2^-149, so it rounds to the signed zero
    p = sign >>> 32;
  } else {
    local m53 = 0x10000000000000 | frac; // the full 53-bit mantissa
    local fe = exp - 896;                 // the float32 biased exponent
    if (fe > 254) {
      // a finite double past float32's range rounds to the signed infinity
      p = sign >>> 32 | 0x7F800000;
    } else if (fe >= 1) {
      // normal float32: keep 24 bits (implicit 1 + 23), drop 29 with round to
      // nearest, ties to even; a carry out of the mantissa bumps the exponent
      local dropped = m53 & 0x1FFFFFFF;
      local m24 = m53 >>> 29;
      local m = m24;
      if (dropped > 0x10000000) m = m24 + 1;
      else if (dropped == 0x10000000 && (m24 & 1) == 1) m = m24 + 1;
      if (m == 0x1000000) { m = 0x800000; fe++; }
      if (fe > 254) p = sign >>> 32 | 0x7F800000;
      else p = sign >>> 32 | (fe << 23) | (m - 0x800000);
    } else {
      // subnormal float32: the value is m * 2^-149, rounded from m53 with the
      // same round-half-even; a round up to 2^23 becomes the smallest normal
      local t = exp - 926;
      local num;
      if (t < -52) {
        // exp < 874: only the exp == 873 boundary can round up to 2^-149
        if (exp == 873 && m53 > 0x10000000000000) num = 1; else num = 0;
      } else {
        local shift = 926 - exp;
        local dropped = m53 & ((0x1 << shift) - 1);
        local half = 0x1 << (shift - 1);
        local m24 = m53 >>> shift;
        local m = m24;
        if (dropped > half) m = m24 + 1;
        else if (dropped == half && (m24 & 1) == 1) m = m24 + 1;
        if (m >= 0x800000) num = 0x800000; else num = m;
      }
      p = sign >>> 32 | num;
    }
  }
  return p;
};

// float32FromBits(i): the double holding the float32 pattern i, widened by bit
// surgery (DESIGN.md (e), "A float32 read widens the 32 bits exactly"). Every
// finite pattern, negative zero and the subnormals included, is preserved for
// a later fround to reproduce.
local float32FromBits = function(i) {
  local sign = (i & 0x80000000) << 32;
  local e = (i >>> 23) & 0xFF;
  local m = i & 0x7FFFFF;
  local db;
  if (e == 0xFF) {
    // never reached from a read, which refuses the pattern first; an
    // infinity widens to itself and a NaN to the quiet double NaN
    if (m == 0) db = sign | 0x7FF0000000000000;
    else db = sign | 0x7FF8000000000000;
  } else if (e == 0) {
    if (m == 0) db = sign;
    else {
      local k = 0;
      local t = m;
      while (t > 1) { t = t >>> 1; k++; }
      db = sign | ((874 + k) << 52) | ((m << (52 - k)) & 0xFFFFFFFFFFFFF);
    }
  } else {
    db = sign | ((e + 896) << 52) | (m << 29);
  }
  return casti2f(db);
};

// the exports DESIGN.md (h) gives this family, beside the arms
_M.fround <- function(x) { return float32FromBits(froundPattern(x)); };
_M.float32Bits <- froundPattern;
_M.float32FromBits <- float32FromBits;
_M.floatBitsAreFinite <- floatBitsAreFinite;

// the arms (DESIGN.md (h)): function(stream, ...) with the stream method's own
// arguments after the stream. Shape (DESIGN.md (g)): room for the whole width
// before any bit moves; a read assigns the holder only after every move.
local write = {
  // serializeFloat(s, h): fround(h.value) as a 32-bit group (STANDARD.md
  // float); a non-finite pattern after the narrowing traps before any bit
  // moves (IMPLEMENTING.md U9)
  serializeFloat = function(s, h) {
    local p = froundPattern(h.value);
    if (!floatBitsAreFinite(p)) return s._fail(SerializeError.NonFiniteFloat);
    if (!s._room(32)) return false;
    s._put(p, 32);
    return true;
  },
  // serializeDouble(s, h): the 64 bits, the low 32-bit group first
  // (STANDARD.md double); finite only, as serializeFloat
  serializeDouble = function(s, h) {
    local bits = castf2i(h.value);
    if (!doubleBitsAreFinite(bits)) return s._fail(SerializeError.NonFiniteFloat);
    if (!s._room(64)) return false;
    local lo = bits & 0xFFFFFFFF;
    local hi = (bits >>> 32) & 0xFFFFFFFF;
    s._put(lo, 32);
    s._put(hi, 32);
    return true;
  },
};

// a read refuses a non-finite pattern on its raw bits, before the holder is
// written (STANDARD.md, Finite values only; Reader Obligations)
local read = {
  serializeFloat = function(s, h) {
    if (!s._room(32)) return false;
    local bits = s._take(32);
    if (!floatBitsAreFinite(bits)) return s._fail(SerializeError.NonFiniteFloat);
    h.value <- float32FromBits(bits);
    return true;
  },
  serializeDouble = function(s, h) {
    if (!s._room(64)) return false;
    local lo = s._take(32);
    local hi = s._take(32);
    local bits = lo | (hi << 32);
    if (!doubleBitsAreFinite(bits)) return s._fail(SerializeError.NonFiniteFloat);
    h.value <- casti2f(bits);
    return true;
  },
};

// the measure stream charges the exact width; like a write it is handed
// finite values only, and traps on any other (IMPLEMENTING.md U6, U9)
local measure = {
  serializeFloat = function(s, h) {
    if (!floatBitsAreFinite(froundPattern(h.value))) return s._fail(SerializeError.NonFiniteFloat);
    s._put(0, 32);
    return true;
  },
  serializeDouble = function(s, h) {
    if (!doubleBitsAreFinite(castf2i(h.value))) return s._fail(SerializeError.NonFiniteFloat);
    s._put(0, 64);
    return true;
  },
};

_M.write <- write;
_M.read <- read;
_M.measure <- measure;
