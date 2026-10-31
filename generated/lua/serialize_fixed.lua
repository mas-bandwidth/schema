--[[
    serialize_fixed.lua: the fixed point Q format arm (DESIGN.md (h))

    Copyright © 2016 - 2026, Más Bandwidth LLC.

    Redistribution and use in source and binary forms, with or without modification, are permitted
    provided that the conditions of the BSD 3-Clause license are met (see the LICENSE file).

    This family owns the serializeFixed arm (DESIGN.md (a) and (h), deviation 8):
    one method covers serialize.go's SerializeFixed64 and SerializeFixed128. The
    wire rules are STANDARD.md, "fixed (Q format, ranged)": the encoding is an
    offset encoding over the RAW (scaled) bounds — raw_min = min << fraction_bits,
    raw_max = max << fraction_bits — byte identical to serializeInt64 /
    serializeInt128 of the raw value over the raw bounds. integerBits + fractionBits
    <= 64 is fixed64 with an integer holder; == 128 is fixed128 with a { lo, hi }
    holder; anything else raises serialize.go's panicFixedParams. The capacity and
    panic rules are serialize.go's fixedPointCapacity, panicFixedParams and
    panicFixedBounds (stream.go).
]]

local stream = require("serialize_stream")

local VALUE_OUT_OF_RANGE = stream.Refusal.ValueOutOfRange
local PAST_END = stream.Refusal.PastEnd

local PANIC_FIXED_PARAMS = "serialize: fixed point requires integer bits >= 1, fraction bits >= 0, "
  .. "a Q format that fits the storage width, and min <= max"
local PANIC_FIXED_BOUNDS = "serialize: fixed point bounds in whole units do not fit the Q format"

local math_type = math.type

-- The wide helper set, reached through the stream loader (DESIGN.md (h)): the
-- 128-bit arithmetic, unsigned Uint128 and signed Int128, from serialize_int128.
local int128mod = stream.family("int128", "serializeFixed")
local U = int128mod.api.Uint128
local I = int128mod.api.Int128

local function bitlen(n)
  local c = 0
  while n > 0 do
    n = n >> 1
    c = c + 1
  end
  return c
end

local function len64(n)
  if n == 0 then return 0 end
  if n < 0 then return 64 end
  return bitlen(n)
end

-- STANDARD.md, fixed: the whole-unit bounds a Q format can hold, clamped to the
-- int64 domain the bounds live in (serialize.go fixedPointCapacity). Signedness
-- follows the bounds: a negative min is signed, with the sign bit counting toward
-- integerBits; min >= 0 is unsigned.
local function fixedCapacity(integerBits, signed)
  if signed then
    if integerBits >= 65 then return math.mininteger, math.maxinteger end
    local minRep = -(1 << (integerBits - 1))
    if integerBits >= 64 then return minRep, math.maxinteger end
    return minRep, (1 << (integerBits - 1)) - 1
  end
  if integerBits >= 63 then return 0, math.maxinteger end
  return 0, (1 << integerBits) - 1
end

-- Validate the Q format and bounds; return the storage width. integerBits +
-- fractionBits <= 64 is fixed64, == 128 is fixed128, anything else is misuse
-- (serialize.go panicFixedParams). Bounds outside the format's capacity raise
-- panicFixedBounds (serialize.go fixedPointParams64/128).
-- Returns the rule's message, or nil (serialize_stream.lua, M.method:
-- write and measure raise it, a read refuses value_out_of_range; U16).
local function validateParams(integerBits, fractionBits, min, max)
  if math_type(integerBits) ~= "integer" or math_type(fractionBits) ~= "integer"
    or math_type(min) ~= "integer" or math_type(max) ~= "integer" then
    return PANIC_FIXED_PARAMS
  end
  local total = integerBits + fractionBits
  if integerBits < 1 or fractionBits < 0 or (total > 64 and total ~= 128) or min > max then
    return PANIC_FIXED_PARAMS
  end
  local minRep, maxRep = fixedCapacity(integerBits, min < 0)
  if min < minRep or max > maxRep then
    return PANIC_FIXED_BOUNDS
  end
  return nil
end

-- STANDARD.md, fixed: num raw (scaled) bounds and the bit count, in the unsigned
-- 64-bit domain (serialize.go fixedPointParams64). The raw range may be wider than
-- 2^63, so the span arithmetic wraps.
local function fixed64Params(fractionBits, min, max)
  local rawMin = min << fractionBits
  local rawMax = max << fractionBits
  local rawRange = rawMax - rawMin
  local numBits = len64(rawMax - rawMin)
  return rawMin, rawRange, numBits
end

-- STANDARD.md, fixed: the wide 128-bit counterpart (serialize.go
-- fixedPointParams128). The raw offset math runs in the unsigned 128-bit domain.
-- A degenerate range (min == max) costs zero bits on every storage width.
local function fixed128Params(fractionBits, min, max)
  local rawMin = U.lsh(I.from64(min), fractionBits)
  local rawMax = U.lsh(I.from64(max), fractionBits)
  local rawRange = U.sub(rawMax, rawMin)
  local numBits
  if min == max then
    numBits = 0
  else
    numBits = len64(max - min) + fractionBits
  end
  return rawMin, rawRange, numBits
end

-- The 128-bit group split (STANDARD.md, int128 and fixed): bits <= 32 is a
-- single group, otherwise full 32-bit groups from the bottom with the final group
-- carrying the remainder. Written through the 64-bit stream primitive.
local function writeGroups128(s, offset, numBits)
  if numBits <= 64 then
    return s:_writeBits(offset.lo, numBits)
  end
  if not s:_writeBits(offset.lo, 64) then return false end
  return s:_writeBits(offset.hi, numBits - 64)
end

local function readGroups128(s, numBits)
  if numBits <= 64 then
    local v = s:_readBits(numBits)
    if v == nil then return nil end
    return { lo = v, hi = 0 }
  end
  local lo = s:_readBits(64)
  if lo == nil then return nil end
  local hi = s:_readBits(numBits - 64)
  if hi == nil then return nil end
  return { lo = lo, hi = hi }
end

local fixed = {
  -- DESIGN.md (h): check runs before the latch check, on every stream.
  check = validateParams,
  checkValue = function(value, integerBits, fractionBits)
    if integerBits + fractionBits <= 64 then
      if math_type(value) ~= "integer" then
        error("serialize: serializeFixed takes an integer value", 0)
      end
    else
      if type(value) ~= "table"
        or math_type(value.lo) ~= "integer" or math_type(value.hi) ~= "integer" then
        error("serialize: serializeFixed takes a { lo, hi } value", 0)
      end
    end
  end,
  -- STANDARD.md, fixed (Q format, ranged): raw_value - raw_min in the bits the
  -- raw range needs. A value outside [raw_min, raw_max] traps (IMPLEMENTING.md U9)
  -- (serialize.go WriteStream.SerializeFixed64/128).
  write = function(s, value, integerBits, fractionBits, min, max)
    if integerBits + fractionBits <= 64 then
      local rawMin, rawRange, numBits = fixed64Params(fractionBits, min, max)
      local offset = value - rawMin
      if math.ult(rawRange, offset) then
        return s:_fail(VALUE_OUT_OF_RANGE)
      end
      if numBits == 0 then return true end
      return s:_writeBits(offset, numBits)
    end
    local rawMin, rawRange, numBits = fixed128Params(fractionBits, min, max)
    local offset = U.sub({ lo = value.lo, hi = value.hi }, rawMin)
    if U.cmp(offset, rawRange) > 0 then
      return s:_fail(VALUE_OUT_OF_RANGE)
    end
    if numBits == 0 then return true end
    if s._bits + numBits > s._numBits then
      return s:_fail(PAST_END)
    end
    return writeGroups128(s, offset, numBits)
  end,
  -- STANDARD.md, fixed: the reader checks the decoded offset against raw_max -
  -- raw_min in the unsigned domain before forming the value — reject, never
  -- clamp (serialize.go ReadStream.SerializeFixed64/128).
  read = function(s, integerBits, fractionBits, min, max)
    if integerBits + fractionBits <= 64 then
      local rawMin, rawRange, numBits = fixed64Params(fractionBits, min, max)
      if numBits == 0 then return true, rawMin end
      local offset = s:_readBits(numBits)
      if offset == nil then return false end
      if math.ult(rawRange, offset) then
        return s:_fail(VALUE_OUT_OF_RANGE)
      end
      return true, rawMin + offset
    end
    local rawMin, rawRange, numBits = fixed128Params(fractionBits, min, max)
    if numBits == 0 then return true, { lo = rawMin.lo, hi = rawMin.hi } end
    local offset = readGroups128(s, numBits)
    if offset == nil then return false end
    if U.cmp(offset, rawRange) > 0 then
      return s:_fail(VALUE_OUT_OF_RANGE)
    end
    local value = U.add(offset, rawMin)
    return true, { lo = value.lo, hi = value.hi }
  end,
  measure = function(s, value, integerBits, fractionBits, min, max)
    if integerBits + fractionBits <= 64 then
      local rawMin, rawRange, numBits = fixed64Params(fractionBits, min, max)
      local offset = value - rawMin
      if math.ult(rawRange, offset) then
        return s:_fail(VALUE_OUT_OF_RANGE)
      end
      return s:_measure(numBits)
    end
    local rawMin, rawRange, numBits = fixed128Params(fractionBits, min, max)
    local offset = U.sub({ lo = value.lo, hi = value.hi }, rawMin)
    if U.cmp(offset, rawRange) > 0 then
      return s:_fail(VALUE_OUT_OF_RANGE)
    end
    return s:_measure(numBits)
  end,
}

local M = {}
M.methods = { serializeFixed = fixed }
M.api = {}
return M
