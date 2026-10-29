--[[
    serialize_int.lua: the ranged integers int and int64 (DESIGN.md (h))

    Copyright © 2016 - 2026, Más Bandwidth LLC.

    Redistribution and use in source and binary forms, with or without modification, are permitted
    provided that the conditions of the BSD 3-Clause license are met (see the LICENSE file).

    This family owns the serializeInt (int32) and serializeInt64 arms. The wire
    rules are STANDARD.md, "int (ranged)" and "int64 (ranged)": the width is
    bits_required(min, max), the offset value - min is written in that many
    bits, and the offset and span arithmetic is carried in the UNSIGNED domain
    of the width, so a range wider than 2^31 or 2^63 is exact. A degenerate
    range min == max costs zero bits and the value comes from the range.
]]

local stream = require("serialize_stream")

local VALUE_OUT_OF_RANGE = stream.Refusal.ValueOutOfRange
local PANIC_MIN_MAX = "serialize: min must not be greater than max"

local INT32_MIN = -2147483648
local INT32_MAX = 2147483647

local math_type = math.type

-- bitlen(n): the number of significant bits of a non-negative integer.
local function bitlen(n)
  local c = 0
  while n > 0 do
    n = n >> 1
    c = c + 1
  end
  return c
end

-- len64(n): the bit length of the unsigned 64-bit pattern of n. A negative
-- integer has the top bit set, so its length is 64.
local function len64(n)
  if n == 0 then return 0 end
  if n < 0 then return 64 end
  return bitlen(n)
end

-- STANDARD.md, int: bits_required(min, max) over the unsigned 32-bit domain.
local function bitsRequired32(min, max)
  if min == max then return 0 end
  return bitlen(((max & 0xffffffff) - (min & 0xffffffff)) & 0xffffffff)
end

-- STANDARD.md, int64: bits_required64 over the unsigned 64-bit domain.
local function bitsRequired64(min, max)
  if min == max then return 0 end
  return len64(max - min)
end

-- A declaration check returns the rule's message, or nil (serialize_stream.lua,
-- M.method: write and measure raise it, a read refuses value_out_of_range;
-- IMPLEMENTING.md U5, U16).
local int = {
  check = function(min, max)
    if math_type(min) ~= "integer" or math_type(max) ~= "integer" then
      return "serialize: serializeInt takes integer bounds"
    end
    if min < INT32_MIN or min > INT32_MAX or max < INT32_MIN or max > INT32_MAX then
      return "serialize: serializeInt bounds must be in int32"
    end
    if min > max then return PANIC_MIN_MAX end
    return nil
  end,
  checkValue = function(value)
    if math_type(value) ~= "integer" or value < INT32_MIN or value > INT32_MAX then
      error("serialize: serializeInt takes an int32 value", 0)
    end
  end,
  -- STANDARD.md, int (ranged): value - min in the uint32 domain, in the bits
  -- the range needs. A value outside [min,max] traps (IMPLEMENTING.md U9)
  -- (serialize.go WriteStream.SerializeInt).
  write = function(s, value, min, max)
    if value < min or value > max then
      return s:_fail(VALUE_OUT_OF_RANGE)
    end
    local bits = bitsRequired32(min, max)
    if bits == 0 then return true end
    local offset = ((value & 0xffffffff) - (min & 0xffffffff)) & 0xffffffff
    return s:_writeBits(offset, bits)
  end,
  -- STANDARD.md, int (ranged): the reader compares the decoded offset against
  -- max - min in the unsigned domain before it forms the value (serialize.go
  -- ReadStream.SerializeInt).
  read = function(s, min, max)
    local bits = bitsRequired32(min, max)
    if bits == 0 then return true, min end
    local offset = s:_readBits(bits)
    if offset == nil then return false end
    local span = ((max & 0xffffffff) - (min & 0xffffffff)) & 0xffffffff
    if offset > span then
      return s:_fail(VALUE_OUT_OF_RANGE)
    end
    return true, offset + min
  end,
  measure = function(s, value, min, max)
    if value < min or value > max then
      return s:_fail(VALUE_OUT_OF_RANGE)
    end
    return s:_measure(bitsRequired32(min, max))
  end,
}

local int64 = {
  check = function(min, max)
    if math_type(min) ~= "integer" or math_type(max) ~= "integer" then
      return "serialize: serializeInt64 takes integer bounds"
    end
    if min > max then return PANIC_MIN_MAX end
    return nil
  end,
  checkValue = function(value)
    if math_type(value) ~= "integer" then
      error("serialize: serializeInt64 takes an integer value", 0)
    end
  end,
  -- STANDARD.md, int64 (ranged): value - min in the uint64 domain. Widths over
  -- 32 bits split low 32 first (serialize.go WriteStream.SerializeInt64).
  write = function(s, value, min, max)
    if value < min or value > max then
      return s:_fail(VALUE_OUT_OF_RANGE)
    end
    local bits = bitsRequired64(min, max)
    if bits == 0 then return true end
    return s:_writeBits(value - min, bits)
  end,
  read = function(s, min, max)
    local bits = bitsRequired64(min, max)
    if bits == 0 then return true, min end
    local offset = s:_readBits(bits)
    if offset == nil then return false end
    local span = max - min
    if math.ult(span, offset) then
      return s:_fail(VALUE_OUT_OF_RANGE)
    end
    return true, offset + min
  end,
  measure = function(s, value, min, max)
    if value < min or value > max then
      return s:_fail(VALUE_OUT_OF_RANGE)
    end
    return s:_measure(bitsRequired64(min, max))
  end,
}

local M = {}
M.methods = { serializeInt = int, serializeInt64 = int64 }
M.api = {}
return M
