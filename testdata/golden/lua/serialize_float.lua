--[[
    serialize.lua: serialize_float.lua

    Copyright © 2016 - 2026, Más Bandwidth LLC.

    Redistribution and use in source and binary forms, with or without modification, are permitted
    provided that the conditions of the BSD 3-Clause license are met (see the LICENSE file).

    float and double (STANDARD.md, "Floating Point"): the 32 and 64 bits of the
    IEEE-754 patterns, bit transparent both ways — every NaN payload, signalling
    NaN, infinity, negative zero and denormal is legal and reproduced exactly,
    and a reader never canonicalizes or refuses a pattern. float32 rounding
    (DESIGN.md (e)) is Go's float32() conversion: round to nearest, ties to
    even, subnormal rounding, overflow to signed infinity, infinities to
    themselves, and NaN as the conversion instruction produces.
]]

local M = {}

local type, error, math, string = type, error, math, string

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

-- roundDivByPow2(M, d): M / 2^d rounded to the nearest integer, ties to even
-- (d >= 1). Used only on the rare subnormal path.
local function roundDivByPow2(M, d)
  if d >= 54 then return 0 end
  local q = M >> d
  local r = M & ((1 << d) - 1)
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

  -- infinity and NaN: bit transparency (STANDARD.md, float). A NaN that is
  -- exactly a float32 moves its bits; any other NaN becomes the quiet NaN with
  -- its sign and its top 22 payload bits, as Go's conversion instruction makes.
  if e == 0x7FF then
    local fm
    if m == 0 then
      fm = 0
    elseif (m & 0x1FFFFFFF) == 0 then
      fm = m >> 29
    else
      fm = (m >> 29) | 0x400000
    end
    return (s << 31) | 0x7F800000 | fm
  end

  -- zero, sign included (STANDARD.md, "Bit transparency": negative zero is legal)
  if e == 0 and m == 0 then
    return s << 31
  end

  -- value magnitude = M * 2^p, with the leading bit of M at position k
  local M, p, k
  if e == 0 then
    M = m
    p = -1074
    k = bitlen(m) - 1
  else
    M = (1 << 52) | m
    p = e - 1075
    k = 52
  end

  -- normalize M to a 24-bit significand t at the MSB, rounding to nearest,
  -- ties to even on the discarded bits; value = t * 2^q (DESIGN.md (e))
  local shift = k - 23
  local t, q
  if shift > 0 then
    t = M >> shift
    local r = M & ((1 << shift) - 1)
    local half = 1 << (shift - 1)
    if r == half then
      if (t & 1) == 1 then t = t + 1 end
    elseif r > half then
      t = t + 1
    end
    q = p + shift
  else
    t = M << (-shift)
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

  -- subnormal single: the exact value M*2^p rounded straight onto the 2^-149
  -- grid, ties to even, in one rounding from the exact M (DESIGN.md (e)).
  -- Rounding the pre-rounded 24-bit t instead would double-round and pick the
  -- wrong neighbor (JOB.md attempt 2). d = (p+149) negation > 0.
  local m23 = roundDivByPow2(M, -p - 149)
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

-- DESIGN.md (c): a float holder value is a Lua float (1.0, never 1); an
-- integer is API misuse and raises.
local function checkFloat(value)
  if math.type(value) ~= "float" then
    error("serialize: a float holder value is a Lua float (math.type == \"float\")", 3)
  end
end

M.methods = {
  -- STANDARD.md, float: the 32 bits of the IEEE-754 single pattern, one 32-bit
  -- group. No conversion beyond Go's float32 rounding of the double.
  serializeFloat = {
    checkValue = function(value) checkFloat(value) end,
    write = function(stream, value)
      return stream:_writeBits(float32Bits(value), 32)
    end,
    read = function(stream)
      local bits = stream:_readBits(32)
      if bits == nil then return false end
      return true, float32FromBits(bits)
    end,
    measure = function(stream)
      return stream:_measure(32)
    end,
  },
  -- STANDARD.md, double: the 64 bits, one 64-bit group (low 32 first).
  serializeDouble = {
    checkValue = function(value) checkFloat(value) end,
    write = function(stream, value)
      return stream:_writeBits(doubleBits(value), 64)
    end,
    read = function(stream)
      local bits = stream:_readBits(64)
      if bits == nil then return false end
      return true, string.unpack("<d", string.pack("<i8", bits))
    end,
    measure = function(stream)
      return stream:_measure(64)
    end,
  },
}

return M
