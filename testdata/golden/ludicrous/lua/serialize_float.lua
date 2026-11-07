--[[
    serialize.lua: serialize_float.lua

    Copyright © 2016 - 2026, Más Bandwidth LLC.

    Redistribution and use in source and binary forms, with or without modification, are permitted
    provided that the conditions of the BSD 3-Clause license are met (see the LICENSE file).

    float and double (STANDARD.md, "Floating Point"): the 32 and 64 bits of the
    IEEE-754 patterns, FINITE ONLY (STANDARD.md, "Finite values only, every one
    bit exact"). A pattern whose exponent field is all ones (NaN with any
    payload, either infinity) is refused on read as non_finite_float, tested on
    the RAW BITS before any conversion, and the destination is left unwritten.
    A non-finite value is write misuse: the writer and the measure trap on it
    (IMPLEMENTING.md U9, U6), tested on the bits after the float32 narrowing,
    so a finite double above FLT_MAX that narrows to infinity traps too. Negative zero and every subnormal are
    finite and reproduced exactly. float32 rounding (DESIGN.md (e)) is Go's
    float32() conversion: round to nearest, ties to even, subnormal rounding,
    overflow to signed infinity.
]]

local stream = require("serialize_stream")

local NON_FINITE_FLOAT = stream.Refusal.NonFiniteFloat

local M = {}

local error, math, string = error, math, string

-- STANDARD.md, "Finite values only": the exponent field all ones.
local FLOAT_EXPONENT = 0x7F800000
local DOUBLE_EXPONENT = 0x7FF0000000000000

local function floatBitsAreFinite(bits)
  return (bits & FLOAT_EXPONENT) ~= FLOAT_EXPONENT
end

local function doubleBitsAreFinite(bits)
  return (bits & DOUBLE_EXPONENT) ~= DOUBLE_EXPONENT
end

-- number of bits of a non-negative integer (0 for 0); only used on the rare
-- subnormal paths, so a loop is fine
local function bitlen(x)
  local n = 0
  while x > 0 do
    n = n + 1
    x = x >> 1
  end
  return n
end

-- The double's 64-bit pattern as an integer.
local function doubleBits(v)
  return string.unpack("<i8", string.pack("<d", v))
end

-- roundDivByPow2(sig, d): sig / 2^d rounded to the nearest integer, ties to even
-- (d >= 1). Used only on the rare subnormal path.
local function roundDivByPow2(sig, d)
  if d >= 54 then return 0 end
  local q = sig >> d
  local r = sig & ((1 << d) - 1)
  local half = 1 << (d - 1)
  if r == half then
    if (q & 1) == 1 then q = q + 1 end
  elseif r > half then
    q = q + 1
  end
  return q
end

-- float32Bits(v): the 32-bit IEEE-754 single pattern of double v, rounded as
-- Go's float32(v) conversion (DESIGN.md (e)). Assembles the pattern from sign,
-- exponent and mantissa with integer bit surgery, never a float conversion.
local function float32Bits(v)
  local b = doubleBits(v)
  local s = b >> 63
  local e = (b >> 52) & 0x7FF
  local m = b & 0xFFFFFFFFFFFFF

  -- infinity and NaN: the format carries neither (STANDARD.md, "Finite values
  -- only"), so the writer refuses the pattern this returns. An infinity keeps
  -- its sign; every NaN becomes the quiet NaN with its sign.
  if e == 0x7FF then
    return (s << 31) | 0x7F800000 | (m ~= 0 and 0x400000 or 0)
  end

  -- zero, sign included (STANDARD.md, "Finite values only": negative zero is finite)
  if e == 0 and m == 0 then
    return s << 31
  end

  -- value magnitude = sig * 2^p, with the leading bit of sig at position k
  local sig, p, k
  if e == 0 then
    sig = m
    p = -1074
    k = bitlen(m) - 1
  else
    sig = (1 << 52) | m
    p = e - 1075
    k = 52
  end

  -- normalize sig to a 24-bit significand t at the MSB, rounding to nearest,
  -- ties to even on the discarded bits; value = t * 2^q (DESIGN.md (e))
  local shift = k - 23
  local t, q
  if shift > 0 then
    t = sig >> shift
    local r = sig & ((1 << shift) - 1)
    local half = 1 << (shift - 1)
    if r == half then
      if (t & 1) == 1 then t = t + 1 end
    elseif r > half then
      t = t + 1
    end
    q = p + shift
  else
    t = sig << (-shift)
    q = p + shift
  end

  -- a rounding carry past 2^24 lifts the exponent
  if t == (1 << 24) then
    t = 1 << 23
    q = q + 1
  end

  local E = 23 + q
  if E > 127 then
    return (s << 31) | 0x7F800000
  end
  if E >= -126 then
    return (s << 31) | ((E + 127) << 23) | (t - (1 << 23))
  end

  -- subnormal single: the exact value sig*2^p rounded straight onto the 2^-149
  -- grid, ties to even, in one rounding from the exact sig (DESIGN.md (e)).
  -- Rounding the pre-rounded 24-bit t instead would double-round and pick the
  -- wrong neighbor (JOB.md attempt 2). d = (p+149) negation > 0.
  local m23 = roundDivByPow2(sig, -p - 149)
  if m23 >= (1 << 23) then
    return (s << 31) | (1 << 23)
  end
  if m23 == 0 then
    return s << 31
  end
  return (s << 31) | m23
end

-- float32FromBits(bits): widen a 32-bit single pattern to the double with the
-- same value by surgery (exponent rebiased, mantissa shifted left 29; a NaN
-- keeps its payload and its quiet bit) (DESIGN.md (e)).
local function float32FromBits(bits)
  local s = bits >> 31
  local e = (bits >> 23) & 0xFF
  local fm = bits & 0x7FFFFF
  local dbits
  if e == 0 then
    if fm == 0 then
      dbits = s << 63
    else
      local k = bitlen(fm) - 1
      dbits = (s << 63) | ((k + 874) << 52) | ((fm & ((1 << k) - 1)) << (52 - k))
    end
  elseif e == 0xFF then
    dbits = (s << 63) | (0x7FF << 52) | (fm << 29)
  else
    dbits = (s << 63) | ((e + 896) << 52) | (fm << 29)
  end
  return string.unpack("<d", string.pack("<i8", dbits))
end

-- Module fields (DESIGN.md (e)): the float card owns fround, float32Bits and
-- float32FromBits, exported for the compressed-float card.
M.fround = function(v) return float32FromBits(float32Bits(v)) end
M.float32Bits = float32Bits
M.float32FromBits = float32FromBits
M.floatBitsAreFinite = floatBitsAreFinite
M.doubleBitsAreFinite = doubleBitsAreFinite

-- DESIGN.md (c): a float holder value is a Lua float (1.0, never 1); an
-- integer is API misuse and raises.
local function checkFloat(value)
  if math.type(value) ~= "float" then
    error("serialize: a float holder value is a Lua float (math.type == \"float\")", 3)
  end
end

M.methods = {
  -- STANDARD.md, float: the 32 bits of the IEEE-754 single pattern, one 32-bit
  -- group, finite only. No conversion beyond Go's float32 rounding of the double.
  serializeFloat = {
    checkValue = function(value) checkFloat(value) end,
    write = function(s, value)
      local bits = float32Bits(value)
      if not floatBitsAreFinite(bits) then return s:_fail(NON_FINITE_FLOAT) end
      return s:_writeBits(bits, 32)
    end,
    read = function(s)
      local bits = s:_readBits(32)
      if bits == nil then return false end
      if not floatBitsAreFinite(bits) then return s:_fail(NON_FINITE_FLOAT) end
      return true, float32FromBits(bits)
    end,
    measure = function(s, value)
      if not floatBitsAreFinite(float32Bits(value)) then return s:_fail(NON_FINITE_FLOAT) end
      return s:_measure(32)
    end,
  },
  -- STANDARD.md, double: the 64 bits, one 64-bit group (low 32 first), finite
  -- only.
  serializeDouble = {
    checkValue = function(value) checkFloat(value) end,
    write = function(s, value)
      local bits = doubleBits(value)
      if not doubleBitsAreFinite(bits) then return s:_fail(NON_FINITE_FLOAT) end
      return s:_writeBits(bits, 64)
    end,
    read = function(s)
      local bits = s:_readBits(64)
      if bits == nil then return false end
      if not doubleBitsAreFinite(bits) then return s:_fail(NON_FINITE_FLOAT) end
      return true, string.unpack("<d", string.pack("<i8", bits))
    end,
    measure = function(s, value)
      if not doubleBitsAreFinite(doubleBits(value)) then return s:_fail(NON_FINITE_FLOAT) end
      return s:_measure(64)
    end,
  },
}

return M
