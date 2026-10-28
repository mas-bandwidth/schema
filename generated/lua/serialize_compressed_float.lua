--[[
    serialize_compressed_float.lua: the compressed float (DESIGN.md (h))

    Copyright © 2016 - 2026, Más Bandwidth LLC.

    Redistribution and use in source and binary forms, with or without modification, are permitted
    provided that the conditions of the BSD 3-Clause license are met (see the LICENSE file).

    This family owns the serializeCompressedFloat and
    serializeCompressedFloatPrecomputed arms, and the api.compressedFloatParams
    derivation. The wire rules are STANDARD.md, "compressed_float": let
    delta = max - min and values = delta / res, clamped to [1, 4294967040].
    Then max_integer_value = ceil(values) and bits = bits_required(0,
    max_integer_value). The writer clamps (value - min) / delta to [0,1],
    multiplies by max_integer_value, adds 0.5, floors, and clamps the integer
    to max_integer_value (STANDARD.md, "The integer clamp is normative"). The
    reader divides by max_integer_value, multiplies by delta, and adds min; it
    refuses an integer above max_integer_value.

    The whole of the arithmetic is float32, emulated by fround after EVERY
    step (STANDARD.md, "This arithmetic is float32", "The reader's arithmetic
    is pinned the same way"; DESIGN.md (e)). fround, the float32 rounding of a
    double, is the float card's, owned here because that file had not landed
    when this card ran; it implements the write rule followed by the read rule
    of DESIGN.md (e) (round to nearest, ties to even; overflow to infinity;
    below the float32 normal range, subnormals and signed zero).
]]

local stream = require("serialize_stream")
local bits = require("serialize_bits")

local VALUE_OUT_OF_RANGE = stream.SerializeError.ValueOutOfRange
local PANIC_FLOAT_PARAMS = "serialize: compressed float requires min < max and resolution > 0"
local PANIC_PRECOMPUTED = "serialize: precomputed compressed float constants must be exactly what CompressedFloatParams derives"

local pack = string.pack
local unpack = string.unpack
local math_type = math.type
local math_ceil = math.ceil
local math_floor = math.floor
local math_tointeger = math.tointeger

-- f32ToDouble(bits32): widen a float32 pattern to the double with the same
-- value (DESIGN.md (e), "Read"). A subnormal float32 with mantissa m32 is
-- m32 * 2^-149: the double renormalizes it, so the double exponent is
-- 874 + (position of m32's top bit) and its mantissa is the remaining 52 bits.
local function f32ToDouble(bits32)
  local s = (bits32 >> 31) & 1
  local e32 = (bits32 >> 23) & 0xFF
  local m32 = bits32 & 0x7FFFFF
  local e64, m64
  if e32 == 0 then
    if m32 == 0 then
      e64, m64 = 0, 0
    else
      local bitpos = 0
      local t = m32
      while t > 0 do t = t >> 1; bitpos = bitpos + 1 end
      bitpos = bitpos - 1
      e64 = 874 + bitpos
      m64 = (m32 << (52 - bitpos)) & 0xFFFFFFFFFFFFF
    end
  elseif e32 == 0xFF then
    e64 = 0x7FF
    m64 = m32 << 29
  else
    e64 = e32 + 896
    m64 = m32 << 29
  end
  local bits64 = (s << 63) | (e64 << 52) | m64
  return unpack("<d", pack("<i8", bits64))
end

-- fround(x): the float32 rounding of a double, as a double. Empty the write
-- rule of DESIGN.md (e): round to nearest, ties to even; magnitudes past the
-- float32 range become infinities; below the normal range, subnormals and
-- signed zero; a NaN that is not exactly a float32 becomes the quiet NaN with
-- its sign and its top 22 payload bits.
local function fround(x)
  local lo, hi = unpack("<I4I4", pack("<d", x))
  local s = (hi >> 31) & 1
  local e = (hi >> 20) & 0x7FF
  local m = ((hi & 0xFFFFF) << 32) | lo
  if e == 0x7FF then
    if m == 0 then
      if s == 1 then return -math.huge else return math.huge end
    end
    local m32
    if (m & 0x1FFFFFFF) == 0 then
      m32 = m >> 29
    else
      m32 = (m >> 30) | 0x400000
    end
    return f32ToDouble((s << 31) | (0xFF << 23) | m32)
  end
  if e == 0 then
    if s == 1 then return -0.0 else return 0.0 end
  end
  local E = e - 1023
  if E > 127 then
    if s == 1 then return -math.huge else return math.huge end
  end
  local sig = 0x10000000000000 + m
  if E >= -126 then
    -- normal float32: keep 24 significant bits, round to nearest ties to even.
    local half = 1 << 28
    local mask = 0x1FFFFFFF
    local r = sig & mask
    local q = sig >> 29
    if r > half then q = q + 1 elseif r == half then
      if (q & 1) == 1 then q = q + 1 end
    end
    if q == 0x1000000 then
      if E == 127 then
        if s == 1 then return -math.huge else return math.huge end
      end
      return f32ToDouble((s << 31) | ((E + 128) << 23))
    end
    return f32ToDouble((s << 31) | ((E + 127) << 23) | (q - 0x800000))
  else
    -- subnormal float32: value = frac * 2^-149, frac < 2^23. shift = 926 - e
    -- gives frac = round(sig >> shift). Below 2^-150 (shift > 53) the value
    -- rounds to signed zero.
    local shift = 926 - e
    if shift > 53 then
      if s == 1 then return -0.0 else return 0.0 end
    end
    local mask = (1 << shift) - 1
    local half = 1 << (shift - 1)
    local r = sig & mask
    local q = sig >> shift
    if r > half then q = q + 1 elseif r == half then
      if (q & 1) == 1 then q = q + 1 end
    end
    if q == 0x800000 then
      return f32ToDouble((s << 31) | (1 << 23))
    end
    return f32ToDouble((s << 31) | q)
  end
end

-- roundNumber(x, what): a finite float32 parameter or value. The wrong Lua
-- type and a non-finite value after float32 rounding are both API misuse and
-- raise (DESIGN.md (c), (e); STANDARD.md, "Non-finite inputs are
-- non-conforming").
local function roundNumber(x, what)
  if type(x) ~= "number" then
    error("serialize: " .. what .. " takes a number", 0)
  end
  local r = fround(x)
  if r ~= r or r == math.huge or r == -math.huge then
    error("serialize: " .. what .. " must be a finite float32", 0)
  end
  return r
end

-- derive(min, max, res): STANDARD.md, "compressed_float", over float32
-- values already rounded and validated. Returns maxIntegerValue, bits, delta.
local function derive(min, max, res)
  local delta = fround(max - min)
  local values = fround(delta / res)
  if not (values >= 1) then
    values = 1
  elseif values > 4294967040.0 then
    values = 4294967040.0
  end
  local maxInt = math_tointeger(math_ceil(values))
  return maxInt, bits.api.bitsRequired(0, maxInt), delta
end

local compressed = {
  -- serialize.go CompressedFloatParams panics: min must be < max and resolution
  -- > 0, after each is rounded to float32 (DESIGN.md (e)).
  check = function(min, max, resolution)
    local mn = roundNumber(min, "min")
    local mx = roundNumber(max, "max")
    local rs = roundNumber(resolution, "resolution")
    if not (mn < mx) or not (rs > 0) then error(PANIC_FLOAT_PARAMS, 0) end
  end,
  checkValue = function(value)
    roundNumber(value, "value")
  end,
  -- STANDARD.md, "compressed_float", the writer: the value is rounded to
  -- float32, clamped to [min,max], quantized with the two roundings around
  -- 0.5, floored, then clamped to max_integer_value.
  write = function(s, value, min, max, resolution)
    local mn = roundNumber(min, "min")
    local mx = roundNumber(max, "max")
    local rs = roundNumber(resolution, "resolution")
    local val = roundNumber(value, "value")
    local maxInt, bitsN, delta = derive(mn, mx, rs)
    local normalized = fround(fround(val - mn) / delta)
    if not (normalized >= 0) then
      normalized = 0
    elseif not (normalized <= 1) then
      normalized = 1
    end
    local product = fround(normalized * fround(maxInt))
    local sum = fround(product + 0.5)
    local iv = math_tointeger(math_floor(sum))
    if iv > maxInt then iv = maxInt end
    return s:_writeBits(iv, bitsN)
  end,
  -- STANDARD.md, "compressed_float", the reader: divide by max_integer_value,
  -- multiply by delta, add min, every step rounded. Refuse an integer above
  -- max_integer_value.
  read = function(s, min, max, resolution)
    local mn = roundNumber(min, "min")
    local mx = roundNumber(max, "max")
    local rs = roundNumber(resolution, "resolution")
    local maxInt, bitsN, delta = derive(mn, mx, rs)
    local iv = s:_readBits(bitsN)
    if iv == nil then return false end
    if iv > maxInt then return s:_fail(VALUE_OUT_OF_RANGE) end
    local q = fround(fround(iv) / fround(maxInt))
    local p = fround(q * delta)
    local v = fround(p + mn)
    return true, v
  end,
  -- serialize.go MeasureStream.SerializeCompressedFloat32: the wire width.
  measure = function(s, value, min, max, resolution)
    local mn = roundNumber(min, "min")
    local mx = roundNumber(max, "max")
    local rs = roundNumber(resolution, "resolution")
    local maxInt, bitsN, delta = derive(mn, mx, rs)
    return s:_measure(bitsN)
  end,
}

local precomputed = {
  -- serialize.go validateCompressedFloatConstants: the precomputed constants
  -- must be exactly what compressedFloatParams derives; anything else raises
  -- panicPrecomputedFloat (DESIGN.md (e)). delta must be exactly a float32.
  check = function(maxIntegerValue, bitsN, delta, min)
    local dl = roundNumber(delta, "delta")
    local mn = roundNumber(min, "min")
    if math_type(maxIntegerValue) ~= "integer" or math_type(bitsN) ~= "integer" then
      error(PANIC_PRECOMPUTED, 0)
    end
    if maxIntegerValue == 0 or bitsN ~= bits.api.bitsRequired(0, maxIntegerValue)
      or not (dl > 0) or dl - dl ~= 0 or dl ~= fround(dl) then
      error(PANIC_PRECOMPUTED, 0)
    end
  end,
  checkValue = function(value)
    roundNumber(value, "value")
  end,
  write = function(s, value, maxIntegerValue, bitsN, delta, min)
    local val = roundNumber(value, "value")
    local dl = roundNumber(delta, "delta")
    local mn = roundNumber(min, "min")
    local normalized = fround(fround(val - mn) / dl)
    if not (normalized >= 0) then
      normalized = 0
    elseif not (normalized <= 1) then
      normalized = 1
    end
    local product = fround(normalized * fround(maxIntegerValue))
    local sum = fround(product + 0.5)
    local iv = math_tointeger(math_floor(sum))
    if iv > maxIntegerValue then iv = maxIntegerValue end
    return s:_writeBits(iv, bitsN)
  end,
  read = function(s, maxIntegerValue, bitsN, delta, min)
    local dl = roundNumber(delta, "delta")
    local mn = roundNumber(min, "min")
    local iv = s:_readBits(bitsN)
    if iv == nil then return false end
    if iv > maxIntegerValue then return s:_fail(VALUE_OUT_OF_RANGE) end
    local q = fround(fround(iv) / fround(maxIntegerValue))
    local p = fround(q * dl)
    local v = fround(p + mn)
    return true, v
  end,
  measure = function(s, value, maxIntegerValue, bitsN, delta, min)
    return s:_measure(bitsN)
  end,
}

local M = {}
M.methods = {
  serializeCompressedFloat = compressed,
  serializeCompressedFloatPrecomputed = precomputed,
}
M.api = {
  -- serialize.go CompressedFloatParams. Rounded and validated exactly as the
  -- serializeCompressedFloat check; the precomputed entry points validate
  -- against this derivation.
  compressedFloatParams = function(min, max, resolution)
    local mn = roundNumber(min, "min")
    local mx = roundNumber(max, "max")
    local rs = roundNumber(resolution, "resolution")
    if not (mn < mx) or not (rs > 0) then error(PANIC_FLOAT_PARAMS, 0) end
    return derive(mn, mx, rs)
  end,
}
return M
