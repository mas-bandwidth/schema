--[[
    serialize.lua: serialize_stream.lua

    Copyright © 2016 - 2026, Más Bandwidth LLC.

    Redistribution and use in source and binary forms, with or without modification, are permitted
    provided that the conditions of the BSD 3-Clause license are met (see the LICENSE file).

    The three streams: WriteStream, ReadStream and MeasureStream. This file owns
    the latch, terminality, the bit accounting, align, object, and the dispatch of
    every other serialize method into its family module (DESIGN.md (g), (h)).
    No family's logic lives here: a family module is required LAZILY, on the
    first call that needs it, and while its file is absent its methods raise
    "serialize: <name> not implemented".

    Wire rules cited by section of STANDARD.md (vendored verbatim). The design
    page is DESIGN.md; its sections are cited as DESIGN.md (x).
]]

local M = {}

-- The error kinds (DESIGN.md (a)): serialize.go's ErrOverflow,
-- ErrValueOutOfRange and ErrAlign, in serialize.js's spelling. A healthy
-- stream's error() is nil.
local SerializeError = {
  Overflow = "overflow",
  ValueOutOfRange = "value_out_of_range",
  Align = "align",
}
M.SerializeError = SerializeError

local OVERFLOW = SerializeError.Overflow
local VALUE_OUT_OF_RANGE = SerializeError.ValueOutOfRange
local ALIGN = SerializeError.Align

-- API misuse raises, naming the rule, where serialize.go panics (DESIGN.md (g)).
-- These messages are serialize.go's own panic strings.
local PANIC_BUFFER_BYTES = "serialize: bit writer buffer size must be a multiple of 8 bytes"
local PANIC_NOT_ALIGNED = "serialize: byte array serialization requires byte alignment"
local PANIC_PRIMITIVE_BITS = "serialize: a stream primitive takes bits in [1,64]"

local type, error, require, setmetatable, mathtype = type, error, require, setmetatable, math.type
local package = package

--------------------------------------------------------------------------------
-- THE LAZY FAMILY LOADER (DESIGN.md (h)).
--
-- A family "<f>" is the module serialize_<f>, required by basename on the
-- consumer's package.path (DESIGN.md (f)). It is looked for the way require
-- looks (package.loaded, package.preload, then package.path; package.cpath is
-- empty by the loading rule), and while it is found nowhere the method that
-- needs it raises "serialize: <name> not implemented". An error raised inside
-- a family file that IS present propagates unchanged: absence is decided by
-- the search, never by catching an error.
--------------------------------------------------------------------------------

local families = {}

local function notImplemented(name)
  error("serialize: " .. name .. " not implemented", 0)
end

local function family(fam, name)
  local mod = families[fam]
  if mod ~= nil then
    return mod
  end
  local modname = "serialize_" .. fam
  if package.loaded[modname] == nil and package.preload[modname] == nil
    and package.searchpath(modname, package.path) == nil then
    notImplemented(name)
  end
  mod = require(modname)
  if type(mod) ~= "table" then
    error("serialize: " .. modname .. " must return its module table", 0)
  end
  families[fam] = mod
  return mod
end
M.family = family

-- M.lazy(fam, name, sub): an entry-level function (DESIGN.md (a)) that resolves
-- to family fam's api[name] (or api[sub][name]) on its first call.
function M.lazy(fam, name, sub, label)
  label = label or name
  local fn
  return function(...)
    if fn == nil then
      local api = family(fam, label).api
      if sub ~= nil then
        api = api and api[sub]
      end
      fn = api and api[name]
      if fn == nil then
        notImplemented(label)
      end
    end
    return fn(...)
  end
end

local function arm(fam, name)
  local mod = family(fam, name)
  local m = mod.methods and mod.methods[name]
  if m == nil then
    notImplemented(name)
  end
  return m
end

-- The arm contract (DESIGN.md (h)): an arm returns true on success and false
-- only after it has latched an error on the stream. Anything else is a bug in
-- the family, raised by name rather than left to corrupt the latch.
local function settle(stream, name, ok)
  if ok == true then
    if stream._err ~= nil then
      error("serialize: " .. name .. " returned true on a failed stream", 0)
    end
    return true
  elseif ok == false then
    if stream._err == nil then
      error("serialize: " .. name .. " returned false without latching an error", 0)
    end
    return false
  end
  error("serialize: " .. name .. " arm must return a boolean", 0)
end

local function holderMisuse(name)
  error("serialize: " .. name .. " takes a holder table { value = ... }", 3)
end

-- M.method(fam, name, holderIndex) -> { write = fn, read = fn, measure = fn }.
-- The one maker every dispatched serialize method is built by. The public
-- method takes its holder at position holderIndex (1, or 2 for
-- serializeIntRelative(previous, holder)). Order, per call (DESIGN.md (g)):
--   1. the holder is a table, else raise;
--   2. the family is loaded (lazily), else raise not implemented;
--   3. the arm's check(...) runs over the arguments other than the holder,
--      raising on API misuse exactly where serialize.go panics, on a healthy
--      stream and a failed one alike; on write and measure the arm's
--      checkValue runs too, over the same arguments with holder.value in the
--      holder's place (the type and domain checks of DESIGN.md (c));
--   4. a failed stream returns false: nothing is consumed, nothing is written
--      and the holder is untouched (STANDARD.md, Reader Obligations, "Failure
--      is terminal": every read consults the failure state before it does
--      anything else, zero-bit reads included);
--   5. write and measure arms receive holder.value in the holder's place; a
--      read arm receives the other arguments and returns ok, value, and the
--      holder is assigned only when ok (STANDARD.md, Reader Obligations, "A
--      refused primitive read must leave its destination unwritten").
function M.method(fam, name, holderIndex)
  local m
  local function get()
    if m == nil then
      m = arm(fam, name)
    end
    return m
  end
  if holderIndex == 1 then
    return {
      write = function(self, holder, ...)
        if type(holder) ~= "table" then holderMisuse(name) end
        local a = get()
        if a.check then a.check(...) end
        if a.checkValue then a.checkValue(holder.value, ...) end
        if self._err ~= nil then return false end
        return settle(self, name, a.write(self, holder.value, ...))
      end,
      read = function(self, holder, ...)
        if type(holder) ~= "table" then holderMisuse(name) end
        local a = get()
        if a.check then a.check(...) end
        if self._err ~= nil then return false end
        local ok, value = a.read(self, ...)
        if settle(self, name, ok) then
          holder.value = value
          return true
        end
        return false
      end,
      measure = function(self, holder, ...)
        if type(holder) ~= "table" then holderMisuse(name) end
        local a = get()
        if a.check then a.check(...) end
        if a.checkValue then a.checkValue(holder.value, ...) end
        if self._err ~= nil then return false end
        return settle(self, name, a.measure(self, holder.value, ...))
      end,
    }
  elseif holderIndex == 2 then
    return {
      write = function(self, first, holder, ...)
        if type(holder) ~= "table" then holderMisuse(name) end
        local a = get()
        if a.check then a.check(first, ...) end
        if a.checkValue then a.checkValue(first, holder.value, ...) end
        if self._err ~= nil then return false end
        return settle(self, name, a.write(self, first, holder.value, ...))
      end,
      read = function(self, first, holder, ...)
        if type(holder) ~= "table" then holderMisuse(name) end
        local a = get()
        if a.check then a.check(first, ...) end
        if self._err ~= nil then return false end
        local ok, value = a.read(self, first, ...)
        if settle(self, name, ok) then
          holder.value = value
          return true
        end
        return false
      end,
      measure = function(self, first, holder, ...)
        if type(holder) ~= "table" then holderMisuse(name) end
        local a = get()
        if a.check then a.check(first, ...) end
        if a.checkValue then a.checkValue(first, holder.value, ...) end
        if self._err ~= nil then return false end
        return settle(self, name, a.measure(self, first, holder.value, ...))
      end,
    }
  end
  error("serialize: holderIndex must be 1 or 2", 2)
end

-- THE DISPATCH TABLE (DESIGN.md (h)): every serialize method a family owns,
-- the family whose file implements it, and where its holder sits.
local DISPATCH = {
  { "serializeBits", "bits", 1 },
  { "serializeBits64", "bits", 1 },
  { "serializeBool", "bits", 1 },
  { "serializeUint8", "bits", 1 },
  { "serializeUint16", "bits", 1 },
  { "serializeUint32", "bits", 1 },
  { "serializeUint64", "bits", 1 },
  { "serializeInt", "int", 1 },
  { "serializeInt64", "int", 1 },
  { "serializeInt128", "int128", 1 },
  { "serializeUint128", "int128", 1 },
  { "serializeFixed", "fixed", 1 },
  { "serializeIntRelative", "int_relative", 2 },
  { "serializeFloat", "float", 1 },
  { "serializeDouble", "float", 1 },
  { "serializeCompressedFloat", "compressed_float", 1 },
  { "serializeCompressedFloatPrecomputed", "compressed_float", 1 },
  { "serializeBytes", "bytes", 1 },
  { "serializeString", "bytes", 1 },
  { "serializeWideString", "bytes", 1 },
}
M.DISPATCH = DISPATCH

local function bitpacker(name)
  return family("bitpacker", name).api
end

local function isObject(object)
  local t = type(object)
  return (t == "table" or t == "userdata") and type(object.serialize) == "function"
end

local function objectMisuse()
  error("serialize: serializeObject takes an object with a serialize(self, stream) method", 3)
end

-- STANDARD.md, object: composition, no bytes of its own. serialize.go: a
-- failure the object returns is latched, and an error latched inside it fails
-- the call even when the object returns true. The object returns false and,
-- optionally, its own kind (any non-nil value, latched as given, as Go latches
-- any error); with none, ValueOutOfRange (DESIGN.md (a), deviation 12).
local function serializeObject(self, object)
  if not isObject(object) then objectMisuse() end
  if self._err ~= nil then return false end
  local ok, kind = object:serialize(self)
  if ok ~= true then
    if self._err == nil then
      self._err = kind or VALUE_OUT_OF_RANGE
    end
    return false
  end
  return self._err == nil
end

local function fail(self, kind)
  if self._err == nil then
    self._err = kind
  end
  return false
end

local function common(class)
  class.__index = class
  function class:error() return self._err end
  function class:ok() return self._err == nil end
  function class:setContext(context) self._context = context end
  function class:context() return self._context end
  -- STANDARD.md, General Conventions, "Bit index and alignment".
  function class:bitsProcessed() return self._bits end
  function class:bytesProcessed() return (self._bits + 7) // 8 end
  class._fail = fail
  class.serializeObject = serializeObject
  return class
end

--------------------------------------------------------------------------------
-- WriteStream (DESIGN.md (g): the writer is strict).
--------------------------------------------------------------------------------

local WriteStream = common({})

local function checkCapacity(capacity)
  if mathtype(capacity) ~= "integer" or capacity < 0 or capacity % 8 ~= 0 then
    error(PANIC_BUFFER_BYTES, 3)
  end
end

local function writeReset(self, capacity)
  checkCapacity(capacity)
  self._capacity = capacity
  self._numBits = capacity * 8
  self._bits = 0
  self._writer = nil
  self._err = nil
end

function WriteStream:reset(capacity) writeReset(self, capacity) end
function WriteStream:isWriting() return true end
function WriteStream:isReading() return false end
function WriteStream:alignBits() return (8 - self._bits % 8) % 8 end

local function writer(self)
  local w = self._writer
  if w == nil then
    w = bitpacker("BitWriter").BitWriter.new(self._capacity)
    self._writer = w
  end
  return w
end

-- THE WRITE PRIMITIVE (DESIGN.md (h)). bits in [1,64]; value is masked to bits.
-- The TOTAL width is checked against the capacity up front, so a refused wide
-- write puts nothing on the wire. bits > 32 go as the low 32 bits first, then
-- the bits - 32 high bits (STANDARD.md, bits).
function WriteStream:_writeBits(value, bits)
  if self._err ~= nil then return false end
  if mathtype(bits) ~= "integer" or bits < 1 or bits > 64 then error(PANIC_PRIMITIVE_BITS, 2) end
  if self._bits + bits > self._numBits then return fail(self, OVERFLOW) end
  local w = writer(self)
  if bits <= 32 then
    w:writeBits(value & ((1 << bits) - 1), bits)
  else
    w:writeBits(value & 0xffffffff, 32)
    w:writeBits((value >> 32) & ((1 << (bits - 32)) - 1), bits - 32)
  end
  self._bits = self._bits + bits
  return true
end

-- The byte block primitive: the stream must already be aligned (the family
-- aligns first, STANDARD.md, bytes); s is a Lua string.
function WriteStream:_writeBytes(s)
  if self._err ~= nil then return false end
  if self._bits % 8 ~= 0 then error(PANIC_NOT_ALIGNED, 2) end
  local n = #s
  if n == 0 then return true end
  -- compared in bytes, as serialize.go does, so a huge count cannot wrap the test
  if n > (self._numBits - self._bits) // 8 then return fail(self, OVERFLOW) end
  writer(self):writeBytes(s)
  self._bits = self._bits + n * 8
  return true
end

-- STANDARD.md, align: zero bits to the next byte boundary; nothing when aligned.
function WriteStream:serializeAlign()
  if self._err ~= nil then return false end
  local n = (8 - self._bits % 8) % 8
  if n == 0 then return true end
  return self:_writeBits(0, n)
end

-- STANDARD.md, General Conventions, "Flush". Ends the write.
function WriteStream:flush()
  if self._writer ~= nil then
    self._writer:flushBits()
  end
end

-- The written portion: bytesProcessed() bytes. Call flush first.
function WriteStream:data()
  if self._writer == nil then
    return ""
  end
  return self._writer:data()
end

--------------------------------------------------------------------------------
-- ReadStream (DESIGN.md (g): every refusal binds; hostile bytes never raise).
--------------------------------------------------------------------------------

local ReadStream = common({})

local function readReset(self, data)
  if type(data) ~= "string" then
    error("serialize: ReadStream reads a Lua string", 3)
  end
  self._data = data
  self._numBits = #data * 8
  self._bits = 0
  self._reader = nil
  self._err = nil
end

function ReadStream:reset(data) readReset(self, data) end
function ReadStream:isWriting() return false end
function ReadStream:isReading() return true end
function ReadStream:alignBits() return (8 - self._bits % 8) % 8 end

local function reader(self)
  local r = self._reader
  if r == nil then
    r = bitpacker("BitReader").BitReader.new(self._data)
    self._reader = r
  end
  return r
end

-- THE READ PRIMITIVE (DESIGN.md (h)): returns the integer read, or nil after
-- latching. STANDARD.md, Reader Obligations, "Reading past the end must fail":
-- the test is bit_index + n > 8 * bytes, made before any bit is consumed.
function ReadStream:_readBits(bits)
  if self._err ~= nil then return nil end
  if mathtype(bits) ~= "integer" or bits < 1 or bits > 64 then error(PANIC_PRIMITIVE_BITS, 2) end
  if self._bits + bits > self._numBits then
    fail(self, OVERFLOW)
    return nil
  end
  local r = reader(self)
  local v
  if bits <= 32 then
    v = r:readBits(bits)
  else
    local lo = r:readBits(32)
    v = lo | (r:readBits(bits - 32) << 32)
  end
  self._bits = self._bits + bits
  return v
end

-- The byte block primitive: n bytes as a Lua string, or nil after latching.
function ReadStream:_readBytes(n)
  if self._err ~= nil then return nil end
  if self._bits % 8 ~= 0 then error(PANIC_NOT_ALIGNED, 2) end
  if mathtype(n) ~= "integer" or n < 0 then error("serialize: a byte count is a non-negative integer", 2) end
  if n == 0 then return "" end
  -- compared in bytes, as serialize.go does (read_stream.go SerializeBytes), so a
  -- huge count cannot wrap the test
  if n > (self._numBits - self._bits) // 8 then
    fail(self, OVERFLOW)
    return nil
  end
  local s = reader(self):readBytes(n)
  self._bits = self._bits + n * 8
  return s
end

-- STANDARD.md, align: readers verify the padding is zero, else Align.
function ReadStream:serializeAlign()
  if self._err ~= nil then return false end
  local n = (8 - self._bits % 8) % 8
  if n == 0 then return true end
  local v = self:_readBits(n)
  if v == nil then return false end
  if v ~= 0 then return fail(self, ALIGN) end
  return true
end

--------------------------------------------------------------------------------
-- MeasureStream (STANDARD.md, The Measure Stream: a bound, not the packet size).
--------------------------------------------------------------------------------

local MeasureStream = common({})

local function measureReset(self)
  self._bits = 0
  self._err = nil
end

function MeasureStream:reset() measureReset(self) end
function MeasureStream:isWriting() return true end
function MeasureStream:isReading() return false end
-- The conservative worst case: every align is charged 7 bits.
function MeasureStream:alignBits() return 7 end

-- THE MEASURE PRIMITIVE (DESIGN.md (h)): adds bits (any non-negative integer).
function MeasureStream:_measure(bits)
  if self._err ~= nil then return false end
  if mathtype(bits) ~= "integer" or bits < 0 then error("serialize: a measured width is a non-negative integer", 2) end
  self._bits = self._bits + bits
  return true
end

function MeasureStream:serializeAlign()
  return self:_measure(7)
end

--------------------------------------------------------------------------------
-- The dispatched methods, built once per class from DISPATCH.
--------------------------------------------------------------------------------

for _, d in ipairs(DISPATCH) do
  local arms = M.method(d[2], d[1], d[3])
  WriteStream[d[1]] = arms.write
  ReadStream[d[1]] = arms.read
  MeasureStream[d[1]] = arms.measure
end

M.WriteStream = {
  -- capacity: the buffer size in bytes, a multiple of 8 (serialize.go
  -- NewWriteStream: the writer stores 8-byte words).
  new = function(capacity)
    local s = setmetatable({ _context = nil }, WriteStream)
    writeReset(s, capacity)
    return s
  end,
}

M.ReadStream = {
  -- data: the packet, a Lua string of any length.
  new = function(data)
    local s = setmetatable({ _context = nil }, ReadStream)
    readReset(s, data)
    return s
  end,
}

M.MeasureStream = {
  new = function()
    local s = setmetatable({ _context = nil }, MeasureStream)
    measureReset(s)
    return s
  end,
}

return M
