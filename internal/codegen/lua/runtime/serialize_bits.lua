--[[
    serialize.lua: serialize_bits.lua

    Copyright © 2016 - 2026, Más Bandwidth LLC.

    Redistribution and use in source and binary forms, with or without modification, are permitted
    provided that the conditions of the BSD 3-Clause license are met (see the LICENSE file).

    The bits family: the raw bit primitives, the fixed-width unsigned helpers and
    the zig-zag conversions. This file owns no bytes of its own; every move goes
    through the stream's bit primitives (serialize_stream.lua).

    Wire rules cited by section of STANDARD.md (vendored verbatim). The design
    page is DESIGN.md; its sections are cited as DESIGN.md (x).
]]

local type, math, error = type, math, error
local mathtype = math.type

local VALUE_OUT_OF_RANGE = require("serialize_stream").Refusal.ValueOutOfRange

-- serialize.go's panic strings, for the misuse raises (DESIGN.md (g)).
local PANIC_BITS_RANGE = "serialize: bits must be in [1,32]"
local PANIC_BITS_RANGE64 = "serialize: bits must be in [1,64]"

local function intErr(field)
  error("serialize: " .. field .. " must be an integer", 2)
end

-- A declaration check returns the rule's message, or nil (serialize_stream.lua,
-- M.method: write and measure raise it, a read refuses value_out_of_range).
local function checkBits(bits, lo, hi, panicmsg)
  if mathtype(bits) ~= "integer" or bits < lo or bits > hi then
    return panicmsg
  end
  return nil
end

-- IMPLEMENTING.md U8, "The check model": a value that does not fit its bit
-- width is caught, never masked. It traps on a write and a measure (U9), as
-- every value outside its call's range does here (DESIGN.md (c), (g); the
-- measure by the same trap, U6). (value >> bits) is logical: a negative value
-- fails it below 64 bits, and at 64 bits every pattern fits.
local function armWrite(stream, value, bits)
  if value >> bits ~= 0 then return stream:_fail(VALUE_OUT_OF_RANGE) end
  return stream:_writeBits(value, bits)
end

local function armRead(stream, bits)
  local v = stream:_readBits(bits)
  if v == nil then return false end
  return true, v
end

local function armMeasure(stream, value, bits)
  if value >> bits ~= 0 then return stream:_fail(VALUE_OUT_OF_RANGE) end
  return stream:_measure(bits)
end

local M = {}

M.methods = {
  serializeBits = {
    check = function(bits)
      return checkBits(bits, 1, 32, PANIC_BITS_RANGE)
    end,
    checkValue = function(value)
      if mathtype(value) ~= "integer" then intErr("bits value") end
      if value < 0 or value > 0xffffffff then
        error("serialize: bits value must be in [0,4294967295]", 2)
      end
    end,
    write = armWrite,
    read = armRead,
    measure = armMeasure,
  },
  serializeBits64 = {
    check = function(bits)
      return checkBits(bits, 1, 64, PANIC_BITS_RANGE64)
    end,
    checkValue = function(value)
      if mathtype(value) ~= "integer" then intErr("bits value") end
    end,
    write = armWrite,
    read = armRead,
    measure = armMeasure,
  },
  serializeBool = {
    checkValue = function(value)
      if type(value) ~= "boolean" then
        error("serialize: bool value must be a boolean", 2)
      end
    end,
    write = function(stream, value)
      return stream:_writeBits(value and 1 or 0, 1)
    end,
    read = function(stream)
      local v = stream:_readBits(1)
      if v == nil then return false end
      return true, v ~= 0
    end,
    measure = function(stream)
      return stream:_measure(1)
    end,
  },
  serializeUint8 = {
    checkValue = function(value)
      if mathtype(value) ~= "integer" then intErr("uint8 value") end
      if value < 0 or value > 255 then error("serialize: uint8 value must be in [0,255]", 2) end
    end,
    write = function(stream, value) return stream:_writeBits(value, 8) end,
    read = function(stream)
      local v = stream:_readBits(8)
      if v == nil then return false end
      return true, v
    end,
    measure = function(stream) return stream:_measure(8) end,
  },
  serializeUint16 = {
    checkValue = function(value)
      if mathtype(value) ~= "integer" then intErr("uint16 value") end
      if value < 0 or value > 65535 then error("serialize: uint16 value must be in [0,65535]", 2) end
    end,
    write = function(stream, value) return stream:_writeBits(value, 16) end,
    read = function(stream)
      local v = stream:_readBits(16)
      if v == nil then return false end
      return true, v
    end,
    measure = function(stream) return stream:_measure(16) end,
  },
  serializeUint32 = {
    checkValue = function(value)
      if mathtype(value) ~= "integer" then intErr("uint32 value") end
      if value < 0 or value > 4294967295 then error("serialize: uint32 value must be in [0,4294967295]", 2) end
    end,
    write = function(stream, value) return stream:_writeBits(value, 32) end,
    read = function(stream)
      local v = stream:_readBits(32)
      if v == nil then return false end
      return true, v
    end,
    measure = function(stream) return stream:_measure(32) end,
  },
  serializeUint64 = {
    checkValue = function(value)
      if mathtype(value) ~= "integer" then intErr("uint64 value") end
    end,
    write = function(stream, value) return stream:_writeBits(value, 64) end,
    read = function(stream)
      local v = stream:_readBits(64)
      if v == nil then return false end
      return true, v
    end,
    measure = function(stream) return stream:_measure(64) end,
  },
}

M.api = {
  -- STANDARD.md, int (ranged): bits_required over the unsigned 32-bit width.
  bitsRequired = function(min, max)
    if min == max then return 0 end
    local d = (max - min) & 0xffffffff
    local n = 0
    while d > 0 do n = n + 1; d = d >> 1 end
    return n
  end,
  -- STANDARD.md, int64 (ranged): bits_required64 over the unsigned 64-bit width.
  bitsRequired64 = function(min, max)
    if min == max then return 0 end
    local d = max - min
    if d < 0 then return 64 end
    local n = 0
    while d > 0 do n = n + 1; d = d >> 1 end
    return n
  end,
  -- serialize.go SignedToUnsigned: zig-zag on int32.
  signedToUnsigned = function(n)
    local u = n & 0xffffffff
    return ((u << 1) & 0xffffffff) ~ ((0 - (u >> 31)) & 0xffffffff)
  end,
  -- serialize.go UnsignedToSigned: the inverse, returning the signed int32.
  unsignedToSigned = function(n)
    local r = ((n >> 1) & 0xffffffff) ~ ((0 - (n & 1)) & 0xffffffff)
    if r >= 0x80000000 then r = r - 0x100000000 end
    return r
  end,
}

return M
