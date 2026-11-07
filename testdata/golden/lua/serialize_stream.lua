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

-- THE INTEGER WIDTH GUARD. This library's number model is Lua 5.4's 64-bit
-- integer (DESIGN.md (c)): uint32 and uint64 fields, the 8-byte window loads and
-- the 128-bit halves all assume it. An interpreter built with LUA_32BITS has a
-- 32-bit integer, on which hostile bytes would decode as wrong values or raise
-- from string.unpack, so the library refuses to load there, by name.
local PANIC_INTEGER_WIDTH = "serialize: this library requires 64-bit Lua integers "
  .. "(math.maxinteger is 2^63 - 1); an interpreter built with LUA_32BITS is not supported"

function M.integerWidthError(maxinteger)
  if maxinteger ~= 0x7fffffffffffffff then
    return PANIC_INTEGER_WIDTH
  end
  return nil
end

do
  local e = M.integerWidthError(math.maxinteger)
  if e ~= nil then error(e, 0) end
end

-- The refusal reasons (STANDARD.md, "Refusal reasons"): the closed list,
-- spelled exactly as the standard spells them. A failed stream's error() is
-- the FIRST refusal's reason; later refusals on that stream are its echoes and
-- keep it. A healthy stream's error() is nil. These are a READER's reasons
-- only (IMPLEMENTING.md U9): a write or a measure stream never latches one, so
-- its error() is always nil. Write misuse traps instead (trapMisuse, below).
local Refusal = {
  PastEnd = "past_end",
  ValueOutOfRange = "value_out_of_range",
  NonzeroPadding = "nonzero_padding",
  TrailingData = "trailing_data",
  NonFiniteFloat = "non_finite_float",
  InvalidUtf8 = "invalid_utf8",
  EmbeddedNul = "embedded_nul",
  NonCanonical = "non_canonical",
  Rejected = "rejected",
}
M.Refusal = Refusal

-- The closed list as a set: a caller's refusal with a kind outside it is
-- latched as `rejected` (STANDARD.md, "Refusal reasons").
local REASONS = {}
for _, r in pairs(Refusal) do REASONS[r] = true end

-- IMPLEMENTING.md U14: the refusal-name function. A stream's error() is nil or
-- a reason from the closed list; its name is the reason itself, and "none" for
-- no refusal, the family's spellings. Anything else is caller error.
function M.refusalName(reason)
  if reason == nil then return "none" end
  if REASONS[reason] then return reason end
  error("serialize: refusalName takes nil or a reason from serialize.Refusal", 2)
end

local VALUE_OUT_OF_RANGE = Refusal.ValueOutOfRange
local PAST_END = Refusal.PastEnd
local NONZERO_PADDING = Refusal.NonzeroPadding
local TRAILING_DATA = Refusal.TrailingData
local REJECTED = Refusal.Rejected

-- API misuse raises, naming the rule, where serialize.go panics (DESIGN.md (g)).
-- These messages are serialize.go's own panic strings.
local PANIC_BUFFER_BYTES = "serialize: bit writer buffer size must be a multiple of 8 bytes"
local PANIC_NOT_ALIGNED = "serialize: byte array serialization requires byte alignment"
local PANIC_PRIMITIVE_BITS = "serialize: a stream primitive takes bits in [1,64]"
local PANIC_REJECT_READ_ONLY = "serialize: reject is read only; a write or measure has nothing to refuse"

-- THE TRAP (IMPLEMENTING.md U9, "One write-misuse mechanism: the trap"). This
-- port has no unchecked build, so it is a checked build always and traps in
-- every run: Lua's error(), raised at the call, before any bit of the
-- operation is written. Every write and measure misuse comes here: a value
-- outside its range or bit width, a float, double or compressed_float that is
-- not finite, text that is not well-formed UTF-8, holds a NUL or does not fit
-- buffer_size - 1, and (write only) an operation that does not fit the room
-- left. The message names the reader's reason for the same fault, but nothing
-- is recorded: write misuse never latches, never returns false, and a later
-- write emits exactly what it would have without it.
local function trapMisuse(_self, kind)
  error("serialize: write misuse (IMPLEMENTING.md U9): " .. tostring(kind), 0)
end
M.trapMisuse = trapMisuse

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

-- The read arm contract (DESIGN.md (h)): an arm returns true on success and
-- false only after it has latched an error on the stream. Anything else is a
-- bug in the family, raised by name rather than left to corrupt the latch.
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

-- The write and measure settle (IMPLEMENTING.md U6, U9): misuse traps inside
-- the arm, so an arm that returns at all returns true; anything else is a bug
-- in the family, raised by name.
local function settleWrite(name, ok)
  if ok == true then return true end
  error("serialize: " .. name .. " write or measure arm must return true or trap", 0)
end

-- THE LATCH: the first reason wins; a later refusal on a failed stream is an
-- echo and keeps it (STANDARD.md, "Refusal reasons").
local function fail(self, kind)
  if self._err == nil then
    self._err = kind
  end
  return false
end

-- M.method(fam, name, holderIndex) -> { write = fn, read = fn, measure = fn }.
-- The one maker every dispatched serialize method is built by. The public
-- method takes its holder at position holderIndex (1, or 2 for
-- serializeIntRelative(previous, holder)). An arm's check(...) validates the
-- caller's declaration (the arguments other than the holder) and RETURNS a
-- message naming the rule when it is bad, nil when it is good; it never
-- raises, so each stream applies its own mechanism (IMPLEMENTING.md U16).
--
-- Write and measure, per call (DESIGN.md (g); IMPLEMENTING.md U6, U9: every
-- misuse traps, and there is no failed state to check):
--   1. the holder is a table, else raise;
--   2. the family is loaded (lazily), else raise not implemented;
--   3. a bad declaration raises its message; the arm's checkValue runs over
--      the same arguments with holder.value in the holder's place (the type
--      and domain checks of DESIGN.md (c)) and raises on misuse;
--   4. the arm receives holder.value in the holder's place, and traps
--      (trapMisuse) on a value its operation cannot carry.
--
-- Read, per call (IMPLEMENTING.md U7: a reader never traps):
--   1. the family is loaded (lazily), else raise not implemented (absent code
--      is not a runtime condition);
--   2. a failed stream returns false: nothing is consumed and the holder is
--      untouched (STANDARD.md, Reader Obligations, "Failure is terminal"), and
--      the first reason is kept;
--   3. caller misuse (a holder that is not a table, a bad declaration) refuses
--      as value_out_of_range before any bit moves (U7, U16), never a raise;
--   4. the arm receives the other arguments and returns ok, value, and the
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
        if a.check then
          local e = a.check(...)
          if e ~= nil then error(e, 2) end
        end
        if a.checkValue then a.checkValue(holder.value, ...) end
        return settleWrite(name, a.write(self, holder.value, ...))
      end,
      read = function(self, holder, ...)
        local a = get()
        if self._err ~= nil then return false end
        if type(holder) ~= "table" then return fail(self, VALUE_OUT_OF_RANGE) end
        if a.check and a.check(...) ~= nil then return fail(self, VALUE_OUT_OF_RANGE) end
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
        if a.check then
          local e = a.check(...)
          if e ~= nil then error(e, 2) end
        end
        if a.checkValue then a.checkValue(holder.value, ...) end
        return settleWrite(name, a.measure(self, holder.value, ...))
      end,
    }
  elseif holderIndex == 2 then
    return {
      write = function(self, first, holder, ...)
        if type(holder) ~= "table" then holderMisuse(name) end
        local a = get()
        if a.check then
          local e = a.check(first, ...)
          if e ~= nil then error(e, 2) end
        end
        if a.checkValue then a.checkValue(first, holder.value, ...) end
        return settleWrite(name, a.write(self, first, holder.value, ...))
      end,
      read = function(self, first, holder, ...)
        local a = get()
        if self._err ~= nil then return false end
        if type(holder) ~= "table" then return fail(self, VALUE_OUT_OF_RANGE) end
        if a.check and a.check(first, ...) ~= nil then return fail(self, VALUE_OUT_OF_RANGE) end
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
        if a.check then
          local e = a.check(first, ...)
          if e ~= nil then error(e, 2) end
        end
        if a.checkValue then a.checkValue(first, holder.value, ...) end
        return settleWrite(name, a.measure(self, first, holder.value, ...))
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

-- STANDARD.md, object: composition, no bytes of its own. An error latched
-- inside the object fails the call even when the object returns true. On a
-- READ, an object that returns false is the caller refusing (STANDARD.md,
-- "Refusal reasons"; IMPLEMENTING.md U2): it may give a kind from the closed
-- list, latched as given; with none, or one outside the list, the stream
-- latches `rejected`. Through the same latch, so a reason a primitive inside
-- the object latched first is kept (DESIGN.md (a), deviation 12). A read never
-- traps (U7): an argument that is not an object refuses as value_out_of_range.
local function readObject(self, object)
  if self._err ~= nil then return false end
  if not isObject(object) then return fail(self, VALUE_OUT_OF_RANGE) end
  local ok, kind = object:serialize(self)
  if ok ~= true then
    fail(self, REASONS[kind] and kind or REJECTED)
    return false
  end
  return self._err == nil
end

-- On a WRITE or a MEASURE there is nothing to refuse: an object that returns
-- false fails the call and records nothing (IMPLEMENTING.md U6, U9; the
-- reference latches `rejected` on a read stream only).
local function writeObject(self, object)
  if not isObject(object) then objectMisuse() end
  return object:serialize(self) == true
end

-- STANDARD.md, "Refusal reasons"; IMPLEMENTING.md U2: the caller's own refusal
-- of a value whose bytes were well formed. Read only: on a write or a measure
-- there is nothing to refuse, and calling it there is caller misuse, which
-- traps in a checked build (U9); this port is always checked, so it raises in
-- every run.
local function rejectReadOnly()
  error(PANIC_REJECT_READ_ONLY, 2)
end

-- IMPLEMENTING.md U9, U14: the public fail on a write or a measure returns
-- false and records nothing; there is nothing to refuse.
local function failNothing()
  return false
end

local function common(class)
  class.__index = class
  function class:error() return self._err end
  -- IMPLEMENTING.md U14: the refusal's name, "none" for no refusal.
  function class:refusal() return M.refusalName(self._err) end
  function class:ok() return self._err == nil end
  function class:setContext(context) self._context = context end
  function class:context() return self._context end
  -- STANDARD.md, General Conventions, "Bit index and alignment".
  function class:bitsProcessed() return self._bits end
  function class:bytesProcessed() return (self._bits + 7) // 8 end
  class._fail = trapMisuse
  class.serializeObject = writeObject
  class.fail = failNothing
  return class
end

--------------------------------------------------------------------------------
-- WriteStream (DESIGN.md (g): the writer is strict; misuse traps, U9).
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
WriteStream.reject = rejectReadOnly

local function writer(self)
  local w = self._writer
  if w == nil then
    w = bitpacker("BitWriter").BitWriter.new(self._capacity)
    self._writer = w
  end
  return w
end

-- THE WRITE PRIMITIVE (DESIGN.md (h)). bits in [1,64]; value is masked to bits.
-- The TOTAL width is checked against the capacity up front, so a write that
-- does not fit traps (U9, U10) with nothing on the wire. bits > 32 go as the
-- low 32 bits first, then the bits - 32 high bits (STANDARD.md, bits).
function WriteStream:_writeBits(value, bits)
  if mathtype(bits) ~= "integer" or bits < 1 or bits > 64 then error(PANIC_PRIMITIVE_BITS, 2) end
  if self._bits + bits > self._numBits then trapMisuse(self, PAST_END) end
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
  if self._bits % 8 ~= 0 then error(PANIC_NOT_ALIGNED, 2) end
  local n = #s
  if n == 0 then return true end
  -- compared in bytes, as serialize.go does, so a huge count cannot wrap the test
  if n > (self._numBits - self._bits) // 8 then trapMisuse(self, PAST_END) end
  writer(self):writeBytes(s)
  self._bits = self._bits + n * 8
  return true
end

-- STANDARD.md, align: zero bits to the next byte boundary; nothing when aligned.
function WriteStream:serializeAlign()
  local n = (8 - self._bits % 8) % 8
  if n == 0 then return true end
  return self:_writeBits(0, n)
end

-- STANDARD.md, end: a writer's end emits nothing. IMPLEMENTING.md U12: it is a
-- no-op returning true.
function WriteStream:serializeEnd()
  return true
end

-- STANDARD.md, General Conventions, "Flush". Ends the write, and is required:
-- the written bytes are unspecified until flush (IMPLEMENTING.md U11).
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

-- A ReadStream reads a Lua string. Anything else is caller misuse on read,
-- which never raises (IMPLEMENTING.md U7): the stream is made over no bytes
-- with value_out_of_range latched at initialization, so every read refuses.
local function readReset(self, data)
  local err = nil
  if type(data) ~= "string" then
    data = ""
    err = VALUE_OUT_OF_RANGE
  end
  self._data = data
  self._numBits = #data * 8
  self._bits = 0
  self._reader = nil
  self._err = err
end

function ReadStream:reset(data) readReset(self, data) end
function ReadStream:isWriting() return false end
function ReadStream:isReading() return true end
function ReadStream:alignBits() return (8 - self._bits % 8) % 8 end

-- stream:reject(): refuses the read as `rejected` and returns false, through
-- the latch every wire refusal takes, so a reason latched first is kept and
-- the stream is terminal from here on (IMPLEMENTING.md U2).
ReadStream._fail = fail
function ReadStream:reject()
  return fail(self, REJECTED)
end

-- stream:fail(reason): the public fail (IMPLEMENTING.md U14). A reason from
-- the closed list is latched as given; nil or anything else latches
-- `rejected`, never none. Through the one latch, so the first reason wins.
function ReadStream:fail(reason)
  return fail(self, REASONS[reason] and reason or REJECTED)
end

ReadStream.serializeObject = readObject

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
    fail(self, PAST_END)
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
    fail(self, PAST_END)
    return nil
  end
  local s = reader(self):readBytes(n)
  self._bits = self._bits + n * 8
  return s
end

-- STANDARD.md, align: readers verify the padding is zero, else nonzero_padding.
function ReadStream:serializeAlign()
  if self._err ~= nil then return false end
  local n = (8 - self._bits % 8) % 8
  if n == 0 then return true end
  local v = self:_readBits(n)
  if v == nil then return false end
  if v ~= 0 then return fail(self, NONZERO_PADDING) end
  return true
end

-- STANDARD.md, end: a packet reader reads its end after the last field of the
-- top-level object. Order (STANDARD.md, end; Reader Obligations):
--   1. a failed stream refuses with its first reason (the latch keeps it);
--   2. a whole byte or more left over is trailing_data, whatever the padding
--      holds, so `bits 3; end` over 0D 00 is trailing_data;
--   3. otherwise the 0-7 bits left are the final byte's padding: any set bit
--      is nonzero_padding. An accepted end leaves bitsProcessed() at 8 * bytes.
function ReadStream:serializeEnd()
  if self._err ~= nil then return false end
  local remaining = self._numBits - self._bits
  if remaining >= 8 then return fail(self, TRAILING_DATA) end
  if remaining == 0 then return true end
  local v = self:_readBits(remaining)
  if v == nil then return false end
  if v ~= 0 then return fail(self, NONZERO_PADDING) end
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
MeasureStream.reject = rejectReadOnly
-- IMPLEMENTING.md U6: a measure refuses nothing. It makes every check a write
-- makes (except room, which it does not have) by the same trap (U9), the
-- _fail every class carries, and latches nothing; error() on a measure is
-- always nil.

-- THE MEASURE PRIMITIVE (DESIGN.md (h)): adds bits (any non-negative integer).
function MeasureStream:_measure(bits)
  if mathtype(bits) ~= "integer" or bits < 0 then error("serialize: a measured width is a non-negative integer", 2) end
  self._bits = self._bits + bits
  return true
end

function MeasureStream:serializeAlign()
  return self:_measure(7)
end

-- STANDARD.md, end: a measure's end costs nothing (IMPLEMENTING.md U12: a
-- no-op returning true).
function MeasureStream:serializeEnd()
  return true
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
