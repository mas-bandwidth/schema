--[[
    serialize.lua: serialize_bytes.lua

    Copyright © 2016 - 2026, Más Bandwidth LLC.

    Redistribution and use in source and binary forms, with or without modification, are permitted
    provided that the conditions of the BSD 3-Clause license are met (see the LICENSE file).

    The string family: serializeBytes, serializeString and serializeWideString
    (DESIGN.md (h), card serialize-lua-strings). Wire rules cited by section of
    STANDARD.md: "bytes", "string", "wstring". The writer is strict: it emits for
    every value exactly the bytes serialize.go v1.15.1 emits, and where that
    writer's WriteStream refuses (ValueOutOfRange, Overflow) this port latches the
    same kind and writes nothing for that call. Reads refuse every refusal
    STANDARD.md and serialize.go state, name the caller's destination untouched and
    leave the stream terminal. No dependence on the int family: the length fields
    are written with the stream's own bit primitives (serialize_int(length, 0,
    bufferSize-1) is just an unsigned field whose width is bits_required(0,
    bufferSize-1)).
]]

local stream = require("serialize_stream")

local SerializeError = stream.SerializeError
local OVERFLOW = SerializeError.Overflow
local VALUE_OUT_OF_RANGE = SerializeError.ValueOutOfRange

local type, error, table_concat, string_format = type, error, table.concat, string.format
local mathtype = math.type

-- serialize.go's panicBufferSize (serialize.go: panicBufferSize) is raised on API
-- misuse, before anything is touched, on a healthy stream and a failed one alike.
local PANIC_BUFFER_SIZE = "serialize: string buffer size must be in [1,1<<31)"

local function checkBufferSize(bufferSize)
  if mathtype(bufferSize) ~= "integer" or bufferSize < 1 or bufferSize >= (1 << 31) then
    error(PANIC_BUFFER_SIZE, 3)
  end
end

local function checkBytesCount(numBytes)
  if mathtype(numBytes) ~= "integer" or numBytes < 0 then
    error("serialize: a byte count is a non-negative integer", 3)
  end
end

-- bits_required(0, max): the fewest bits that hold every value in [0, max],
-- 0 for the degenerate range [0,0] (STANDARD.md, Integers).
local function bitsRequired0toMax(max)
  if max == 0 then return 0 end
  local w = 1
  while (1 << w) <= max do w = w + 1 end
  return w
end

-- UTF-8 helpers. The write side trusts its input (the writer contract), so
-- utf8Next decodes the bytes it is given; the read side validates first.

local function utf8Next(s, i)
  local b = s:byte(i)
  if b < 0x80 then
    return b, i + 1
  elseif b < 0xE0 then
    return ((b & 0x1F) << 6) | (s:byte(i + 1) & 0x3F), i + 2
  elseif b < 0xF0 then
    return ((b & 0x0F) << 12) | ((s:byte(i + 1) & 0x3F) << 6)
      | (s:byte(i + 2) & 0x3F), i + 3
  else
    return ((b & 0x07) << 18) | ((s:byte(i + 1) & 0x3F) << 12)
      | ((s:byte(i + 2) & 0x3F) << 6) | (s:byte(i + 3) & 0x3F), i + 4
  end
end

local function u8encode(cp)
  if cp < 0x80 then
    return string.char(cp)
  elseif cp < 0x800 then
    return string.char(0xC0 | (cp >> 6), 0x80 | (cp & 0x3F))
  elseif cp < 0x10000 then
    return string.char(0xE0 | (cp >> 12), 0x80 | ((cp >> 6) & 0x3F),
      0x80 | (cp & 0x3F))
  else
    return string.char(0xF0 | (cp >> 18), 0x80 | ((cp >> 12) & 0x3F),
      0x80 | ((cp >> 6) & 0x3F), 0x80 | (cp & 0x3F))
  end
end

-- validUTF8 implements Unicode Table 3-7 (STANDARD.md, string): overlong forms,
-- encoded surrogates, code points above 10FFFF and truncated sequences are all
-- refused.
local function validUTF8(s)
  local n = #s
  local i = 1
  while i <= n do
    local b = s:byte(i)
    if b < 0x80 then
      i = i + 1
    elseif b < 0xC2 then
      return false
    elseif b < 0xE0 then
      if i + 1 > n then return false end
      local c = s:byte(i + 1)
      if c < 0x80 or c > 0xBF then return false end
      i = i + 2
    elseif b < 0xF0 then
      if i + 2 > n then return false end
      local c1, c2 = s:byte(i + 1), s:byte(i + 2)
      if c1 < 0x80 or c1 > 0xBF or c2 < 0x80 or c2 > 0xBF then return false end
      if b == 0xE0 and c1 <= 0x9F then return false end
      if b == 0xED and c1 >= 0xA0 then return false end
      i = i + 3
    elseif b < 0xF5 then
      if i + 3 > n then return false end
      local c1, c2, c3 = s:byte(i + 1), s:byte(i + 2), s:byte(i + 3)
      if c1 < 0x80 or c1 > 0xBF or c2 < 0x80 or c2 > 0xBF
        or c3 < 0x80 or c3 > 0xBF then return false end
      if b == 0xF0 and c1 <= 0x8F then return false end
      if b == 0xF4 and c1 >= 0x90 then return false end
      i = i + 4
    else
      return false
    end
  end
  return true
end

-- The number of UTF-16 code units the string is encoded as (serialize.go,
-- stream.go utf16Length): one per basic-plane code point, two per astral one.
local function utf16Length(s)
  local n = 0
  local i = 1
  while i <= #s do
    local cp, nxt = utf8Next(s, i)
    n = n + 1
    if cp >= 0x10000 then n = n + 1 end
    i = nxt
  end
  return n
end

local methods = {}

-- serializeBytes: STANDARD.md "bytes": aligns first, then writes `count` raw
-- bytes. The count is not transmitted; both sides know it.
methods.serializeBytes = {
  check = checkBytesCount,
  checkValue = function(value, numBytes)
    if type(value) ~= "string" then
      error("serialize: serializeBytes takes a string value", 3)
    end
    if #value ~= numBytes then
      error("serialize: serializeBytes value length must equal numBytes", 3)
    end
  end,
  write = function(s, value)
    if not s:serializeAlign() then return false end
    return s:_writeBytes(value)
  end,
  read = function(s, numBytes)
    if not s:serializeAlign() then return false end
    local data = s:_readBytes(numBytes)
    if data == nil then return false end
    return true, data
  end,
  measure = function(s, value, numBytes)
    if not s:serializeAlign() then return false end
    return s:_measure(numBytes * 8)
  end,
}

-- serializeString: STANDARD.md "string": the length as serialize_int(length, 0,
-- bufferSize-1), then the characters as serializeBytes (which aligns). The
-- terminator is not transmitted; the reader appends it.
methods.serializeString = {
  check = checkBufferSize,
  checkValue = function(value, bufferSize)
    if type(value) ~= "string" then
      error("serialize: serializeString takes a string value", 3)
    end
  end,
  write = function(s, value, bufferSize)
    if #value >= bufferSize then return s:_fail(VALUE_OUT_OF_RANGE) end
    local w = bitsRequired0toMax(bufferSize - 1)
    if w > 0 then
      if not s:_writeBits(#value, w) then return false end
    end
    if not s:serializeAlign() then return false end
    return s:_writeBytes(value)
  end,
  read = function(s, bufferSize)
    local w = bitsRequired0toMax(bufferSize - 1)
    local length
    if w == 0 then
      length = 0
    else
      length = s:_readBits(w)
      if length == nil then return false end
    end
    if length > bufferSize - 1 then return s:_fail(VALUE_OUT_OF_RANGE) end
    if not s:serializeAlign() then return false end
    local data = s:_readBytes(length)
    if data == nil then return false end
    if data:find("\0", 1, true) then return s:_fail(VALUE_OUT_OF_RANGE) end
    if not validUTF8(data) then return s:_fail(VALUE_OUT_OF_RANGE) end
    return true, data
  end,
  measure = function(s, value, bufferSize)
    local w = bitsRequired0toMax(bufferSize - 1)
    if w > 0 then
      if not s:_measure(w) then return false end
    end
    if not s:serializeAlign() then return false end
    return s:_measure(#value * 8)
  end,
}

-- serializeWideString: STANDARD.md "wstring": the length in UTF-16 code units,
-- then each code unit as a 32-bit group, with NO alignment anywhere. Astral code
-- points split into surrogate pairs on write and recombine on read.
methods.serializeWideString = {
  check = checkBufferSize,
  checkValue = function(value, bufferSize)
    if type(value) ~= "string" then
      error("serialize: serializeWideString takes a string value", 3)
    end
  end,
  write = function(s, value, bufferSize)
    local units = utf16Length(value)
    if units >= bufferSize then return s:_fail(VALUE_OUT_OF_RANGE) end
    local w = bitsRequired0toMax(bufferSize - 1)
    if w > 0 then
      if not s:_writeBits(units, w) then return false end
    end
    local i = 1
    while i <= #value do
      local cp, nxt = utf8Next(value, i)
      if cp >= 0x10000 then
        local base = cp - 0x10000
        local hi = 0xD800 + (base >> 10)
        local lo = 0xDC00 + (base & 0x3FF)
        if not s:_writeBits(hi, 32) then return false end
        if not s:_writeBits(lo, 32) then return false end
      else
        if not s:_writeBits(cp, 32) then return false end
      end
      i = nxt
    end
    return true
  end,
  read = function(s, bufferSize)
    local w = bitsRequired0toMax(bufferSize - 1)
    local length
    if w == 0 then
      length = 0
    else
      length = s:_readBits(w)
      if length == nil then return false end
    end
    if length > bufferSize - 1 then return s:_fail(VALUE_OUT_OF_RANGE) end
    -- bounds check the whole payload before reading any group (serialize.go,
    -- read_stream.go SerializeWideString)
    if length * 32 > (s._numBits - s._bits) then return s:_fail(OVERFLOW) end
    local out = {}
    local j = 1
    while j <= length do
      local unit = s:_readBits(32)
      if unit == nil then return false end
      if unit == 0 then return s:_fail(VALUE_OUT_OF_RANGE) end
      if unit > 0xFFFF or (unit >= 0xDC00 and unit <= 0xDFFF) then
        return s:_fail(VALUE_OUT_OF_RANGE)
      end
      if unit >= 0xD800 and unit <= 0xDBFF then
        if j == length then return s:_fail(VALUE_OUT_OF_RANGE) end
        local low = s:_readBits(32)
        if low == nil then return false end
        if low < 0xDC00 or low > 0xDFFF then return s:_fail(VALUE_OUT_OF_RANGE) end
        local cp = 0x10000 + ((unit - 0xD800) << 10) + (low - 0xDC00)
        out[#out + 1] = u8encode(cp)
        j = j + 2
      else
        out[#out + 1] = u8encode(unit)
        j = j + 1
      end
    end
    return true, table_concat(out)
  end,
  measure = function(s, value, bufferSize)
    local units = utf16Length(value)
    if units >= bufferSize then return s:_fail(VALUE_OUT_OF_RANGE) end
    local w = bitsRequired0toMax(bufferSize - 1)
    if w > 0 then
      if not s:_measure(w) then return false end
    end
    return s:_measure(units * 32)
  end,
}

return {
  methods = methods,
  api = {},
}
