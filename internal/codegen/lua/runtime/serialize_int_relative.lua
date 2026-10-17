--[[
    serialize.lua: serialize_int_relative.lua

    Copyright © 2016 - 2026, Más Bandwidth LLC.

    Redistribution and use in source and binary forms, with or without modification, are permitted
    provided that the conditions of the BSD 3-Clause license are met (see the LICENSE file).

    The int_relative arm: the tiered encoding of a strictly increasing sequence
    over the non-negative int32 domain (STANDARD.md, int_relative; DESIGN.md
    (g), (h)). The writer takes the first tier the difference fits; the reader
    accepts any tier whose reconstruction lies in the domain and is strictly
    greater than previous. previous is the caller's own state and never arrives
    off the wire, so a previous outside the domain is API misuse and raises
    serialize.go's panicIntRelativeDomain.
]]

local M = {}

local mathtype = math.type
local PANIC_INT_RELATIVE_DOMAIN = "serialize: int relative previous must be in [0,2147483647]"
local PANIC_INT_RELATIVE_CURRENT = "serialize: int relative current must be in [-2147483648,2147483647]"

-- The int_relative domain is the non-negative int32 range (STANDARD.md int_relative).
-- The operation's API type is the signed int32 (DESIGN.md (c)): a current inside
-- that type but outside the domain latches ValueOutOfRange in the write arm.
local INT32_MIN = -2147483648
local RANGE_MAX = 2147483647

-- The bounded difference buckets (serialize.go stream.go intRelativeBuckets). Each
-- costs one signal bit plus the bits required for its [min,max] range; differences
-- past the last bucket fall back to an absolute 32-bit value.
local BUCKETS = {
  { min = 2, max = 6, bits = 3 },
  { min = 7, max = 23, bits = 5 },
  { min = 24, max = 280, bits = 9 },
  { min = 281, max = 4377, bits = 13 },
  { min = 4378, max = 69914, bits = 17 },
}

local function checkDomain(v, msg, min)
  if mathtype(v) ~= "integer" or v < (min or 0) or v > RANGE_MAX then
    error(msg, 0)
  end
end

-- The reconstruction rule (STANDARD.md int_relative: every tier's reconstruction
-- is checked, refuses a value outside the domain or not greater than previous).
local function result(stream, previous, reconstructed)
  if reconstructed > RANGE_MAX or reconstructed <= previous then
    return stream:_fail("value_out_of_range")
  end
  return true, reconstructed
end

local methods = {}

methods.check = function(previous)
  checkDomain(previous, PANIC_INT_RELATIVE_DOMAIN)
end

-- The value check is over the Go int32 type, not the operation's non-negative
-- domain (DESIGN.md (c), (h)): a negative current is inside the type and the
-- write/measure arm latches it as ValueOutOfRange, as serialize.go does.
methods.checkValue = function(previous, current)
  checkDomain(current, PANIC_INT_RELATIVE_CURRENT, INT32_MIN)
end

-- Writer (STANDARD.md int_relative: "The writer takes the first tier the difference
-- fits, and that encoding is the canonical one"; the domain and the previous < current
-- contract; DESIGN.md (g) strict writer, (h) arm shape).
methods.write = function(stream, previous, current)
  if previous >= current then
    return stream:_fail("value_out_of_range")
  end
  local difference = current - previous
  if not stream:_writeBits(difference == 1 and 1 or 0, 1) then return false end
  if difference == 1 then return true end
  for _, b in ipairs(BUCKETS) do
    local inBucket = difference <= b.max
    if not stream:_writeBits(inBucket and 1 or 0, 1) then return false end
    if inBucket then
      return stream:_writeBits(difference - b.min, b.bits)
    end
  end
  return stream:_writeBits(current, 32)
end

-- Reader (STANDARD.md int_relative: the ladder, the absolute tier's unsigned 32 raw
-- bits, "Every tier's reconstruction must be checked", the non-canonical-tier rule,
-- and Reader Obligations "Reading past the end must fail" / terminality; DESIGN.md
-- (h) arm shape).
methods.read = function(stream, previous)
  local flag = stream:_readBits(1)
  if flag == nil then return false end
  if flag == 1 then
    return result(stream, previous, previous + 1)
  end
  for _, b in ipairs(BUCKETS) do
    flag = stream:_readBits(1)
    if flag == nil then return false end
    if flag == 1 then
      local offset = stream:_readBits(b.bits)
      if offset == nil then return false end
      if offset > b.max - b.min then
        return stream:_fail("value_out_of_range")
      end
      return result(stream, previous, previous + b.min + offset)
    end
  end
  local v = stream:_readBits(32)
  if v == nil then return false end
  return result(stream, previous, v)
end

-- Measure (STANDARD.md, The Measure Stream: exact width for non-alignment operations;
-- serialize.go measure_stream.go SerializeIntRelative; DESIGN.md (h) arm shape).
methods.measure = function(stream, previous, current)
  if previous >= current then
    return stream:_fail("value_out_of_range")
  end
  local difference = current - previous
  local bits = 1
  if difference ~= 1 then
    local matched = false
    for _, b in ipairs(BUCKETS) do
      bits = bits + 1
      if difference <= b.max then
        bits = bits + b.bits
        matched = true
        break
      end
    end
    if not matched then bits = bits + 32 end
  end
  return stream:_measure(bits)
end

M.methods = {
  serializeIntRelative = methods,
}

return M
