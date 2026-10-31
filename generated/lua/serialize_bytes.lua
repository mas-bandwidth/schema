--[[
    serialize.lua: serialize_bytes.lua

    Copyright © 2016 - 2026, Más Bandwidth LLC.

    Redistribution and use in source and binary forms, with or without modification, are permitted
    provided that the conditions of the BSD 3-Clause license are met (see the LICENSE file).

    The string family: serializeBytes and serializeString (DESIGN.md (h), card
    serialize-lua-strings). Wire rules cited by section of STANDARD.md: "bytes",
    "string". The writer is strict: it emits for every value exactly the bytes
    serialize.go v2 emits. Reads refuse every refusal
    STANDARD.md and serialize.go state, name the caller's destination untouched and
    leave the stream terminal. On a write or a measure, misuse (a string that
    does not fit, holds a NUL or is not UTF-8; an operation past the end)
    traps by IMPLEMENTING.md U9 and latches nothing. No dependence on the int
    family: the length fields are written with the stream's own bit
    primitives (serialize_int(length, 0, bufferSize-1) is just an unsigned
    field whose width is bits_required(0, bufferSize-1)).
]]

local stream = require("serialize_stream")

local Refusal = stream.Refusal
local VALUE_OUT_OF_RANGE = Refusal.ValueOutOfRange
local INVALID_UTF8 = Refusal.InvalidUtf8
local EMBEDDED_NUL = Refusal.EmbeddedNul
local PAST_END = Refusal.PastEnd

local type, error = type, error
local mathtype = math.type

-- serialize.go's panicBufferSize (serialize.go: panicBufferSize) is raised on API
-- misuse, before anything is touched, on a healthy stream and a failed one alike.
local PANIC_BUFFER_SIZE = "serialize: string buffer size must be in [1,1<<31)"

-- A declaration check returns the rule's message, or nil (serialize_stream.lua,
-- M.method: write and measure raise it; a read refuses value_out_of_range,
-- IMPLEMENTING.md U7: a negative byte count or a buffer size below 1 on read is
-- a refusal, never a raise).
local function checkBufferSize(bufferSize)
  if mathtype(bufferSize) ~= "integer" or bufferSize < 1 or bufferSize >= (1 << 31) then
    return PANIC_BUFFER_SIZE
  end
  return nil
end

local function checkBytesCount(numBytes)
  if mathtype(numBytes) ~= "integer" or numBytes < 0 then
    return "serialize: a byte count is a non-negative integer"
  end
  return nil
end

-- IMPLEMENTING.md U10: a write checks room for the whole operation, the
-- length field, the align's padding and the payload, before writing any of it.
local function room(s, fieldBits, payloadBytes)
  local at = s._bits + fieldBits
  return at + (8 - at % 8) % 8 + payloadBytes * 8 <= s._numBits
end

-- bits_required(0, max): the fewest bits that hold every value in [0, max],
-- 0 for the degenerate range [0,0] (STANDARD.md, Integers).
local function bitsRequired0toMax(max)
  if max == 0 then return 0 end
  local w = 1
  while (1 << w) <= max do w = w + 1 end
  return w
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

-- IMPLEMENTING.md U3, U4, U9: the text checks a string write and a string
-- measure both make (U6), each write misuse, so each traps (stream:_fail on a
-- write or a measure stream is the trap; nothing is latched). The writer sends
-- the caller's bytes as given, so it checks them as the reader will: the
-- length, then a NUL, then UTF-8 (Table 3-7).
local function checkText(s, value, bufferSize)
  if #value >= bufferSize then s:_fail(VALUE_OUT_OF_RANGE) end
  if value:find("\0", 1, true) then s:_fail(EMBEDDED_NUL) end
  if not validUTF8(value) then s:_fail(INVALID_UTF8) end
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
    if not room(s, 0, #value) then return s:_fail(PAST_END) end
    if not s:serializeAlign() then return false end
    return s:_writeBytes(value)
  end,
  read = function(s, numBytes)
    if not s:serializeAlign() then return false end
    local data = s:_readBytes(numBytes)
    if data == nil then return false end
    return true, data
  end,
  measure = function(s, _value, numBytes)
    if not s:serializeAlign() then return false end
    return s:_measure(numBytes * 8)
  end,
}

-- serializeString: STANDARD.md "string": the length as serialize_int(length, 0,
-- bufferSize-1), then the characters as serializeBytes (which aligns). The
-- terminator is not transmitted; the reader appends it.
methods.serializeString = {
  check = checkBufferSize,
  checkValue = function(value, _bufferSize)
    if type(value) ~= "string" then
      error("serialize: serializeString takes a string value", 3)
    end
  end,
  write = function(s, value, bufferSize)
    checkText(s, value, bufferSize)
    local w = bitsRequired0toMax(bufferSize - 1)
    if not room(s, w, #value) then return s:_fail(PAST_END) end
    if w > 0 then
      if not s:_writeBits(#value, w) then return false end
    end
    if not s:serializeAlign() then return false end
    return s:_writeBytes(value)
  end,
  -- Refusal order (STANDARD.md, string; Reader Obligations): the length past
  -- the buffer (value_out_of_range); the align's padding (nonzero_padding,
  -- before the payload length is tested against what remains); the payload
  -- past the end (past_end, tested in _readBytes before anything is taken, so
  -- a wire-claimed length never sizes an allocation the stream cannot back);
  -- then a NUL anywhere in the payload (embedded_nul) BEFORE the encoding is
  -- validated (invalid_utf8), so 02 FF 00 is embedded_nul. The bytes go back
  -- exactly as read: no normalization, no BOM stripping, no replacement.
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
    if data:find("\0", 1, true) then return s:_fail(EMBEDDED_NUL) end
    if not validUTF8(data) then return s:_fail(INVALID_UTF8) end
    return true, data
  end,
  measure = function(s, value, bufferSize)
    -- the writer's checks, by the same trap; a measure latches nothing (U6)
    checkText(s, value, bufferSize)
    local w = bitsRequired0toMax(bufferSize - 1)
    if w > 0 then
      if not s:_measure(w) then return false end
    end
    if not s:serializeAlign() then return false end
    return s:_measure(#value * 8)
  end,
}

return {
  methods = methods,
  api = {},
}
