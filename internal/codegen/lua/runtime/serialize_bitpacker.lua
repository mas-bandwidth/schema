--[[
    serialize.lua: serialize_bitpacker.lua

    Copyright © 2016 - 2026, Más Bandwidth LLC.

    Redistribution and use in source and binary forms, with or without modification, are permitted
    provided that the conditions of the BSD 3-Clause license are met (see the LICENSE file).

    The bit writer and bit reader: the unchecked layer under the streams, exactly
    as serialize.go's bitpacker.go and serialize.js's src/bitpacker.js. A value is
    packed least-significant-bit first into a 64-bit scratch word; a value that
    crosses the 64-bit boundary is split, its low 64-s bits completing the word
    and the rest beginning the next; each full word is stored as 8 little-endian
    bytes. This module owns no latch: API misuse raises, naming serialize.go's
    panic rule (DESIGN.md (g), "The writer is STRICT"; the bitpacker table in (a)).

    Wire rules are cited by section of STANDARD.md (vendored verbatim); the design
    page is DESIGN.md.
]]

local M = {}

local type, error, math, string, table, setmetatable =
  type, error, math, string, table, setmetatable

-- serialize.go v1.15.1 serialize.go's panic strings, for the rules this layer
-- raises (DESIGN.md (a), (g)).
local PANIC_BITS_RANGE = "serialize: bits must be in [1,32]"
local PANIC_WRITE_OVERFLOW = "serialize: bit writer overflow"
local PANIC_READ_OVERFLOW = "serialize: bit reader would read past the end of the buffer"
local PANIC_NOT_ALIGNED = "serialize: byte array serialization requires byte alignment"
local PANIC_BUFFER_BYTES = "serialize: bit writer buffer size must be a multiple of 8 bytes"

local function checkBits(bits)
  if math.type(bits) ~= "integer" or bits < 1 or bits > 32 then
    error(PANIC_BITS_RANGE, 3)
  end
end

local function checkCapacity(capacity)
  if math.type(capacity) ~= "integer" or capacity < 0 or capacity % 8 ~= 0 then
    error(PANIC_BUFFER_BYTES, 3)
  end
end

--------------------------------------------------------------------------------
-- BitWriter. STANDARD.md, General Conventions: a 64-bit scratch word, bits
-- packed LSB first, a crossing value split, each full word stored as 8
-- little-endian bytes, and Flush writes the final partial word. The output
-- pieces are joined once by table.concat (DESIGN.md (d)).
--------------------------------------------------------------------------------

local BitWriter = {}
BitWriter.__index = BitWriter

function BitWriter:reset(capacity)
  checkCapacity(capacity)
  self._numBits = capacity * 8
  self._bitsWritten = 0
  self._scratch = 0
  self._scratchBits = 0
  self._wordIndex = 0
  self._pieces = {}
end

-- STANDARD.md, bits: write the low bits of value; bits in [1,32]. Above that
-- count the value's bits are ignored, as serialize.go's uint32 parameter does.
function BitWriter:writeBits(value, bits)
  checkBits(bits)
  if math.type(value) ~= "integer" then
    error("serialize: bit writer value must be an integer", 2)
  end
  -- serialize.go's WriteBits panics panicWriteOverflow, before writing.
  if self._bitsWritten + bits > self._numBits then
    error(PANIC_WRITE_OVERFLOW, 2)
  end
  local mask = bits == 32 and 0xffffffff or ((1 << bits) - 1)
  value = value & mask

  local s = self._scratchBits
  self._scratch = self._scratch | (value << s)
  local newScratchBits = s + bits
  if newScratchBits >= 64 then
    self._pieces[#self._pieces + 1] = string.pack("<i8", self._scratch)
    self._wordIndex = self._wordIndex + 1
    -- the bits that spilled past 64 begin the next scratch word
    self._scratch = value >> (64 - s)
    self._scratchBits = newScratchBits - 64
  else
    self._scratchBits = newScratchBits
  end
  self._bitsWritten = self._bitsWritten + bits
end

-- STANDARD.md, align: zero bits to the next byte boundary; nothing when
-- already aligned.
function BitWriter:writeAlign()
  local remainderBits = self._bitsWritten % 8
  if remainderBits ~= 0 then
    self:writeBits(0, 8 - remainderBits)
  end
end

-- STANDARD.md, bytes: a byte block, aligned only. The head and tail go through
-- the scratch; the aligned middle is copied whole as a piece. serialize.go's
-- writeBytes panics panicNotAligned and panicWriteOverflow.
function BitWriter:writeBytes(s)
  if type(s) ~= "string" then
    error("serialize: bit writer bytes must be a Lua string", 2)
  end
  if self._bitsWritten % 8 ~= 0 then
    error(PANIC_NOT_ALIGNED, 2)
  end
  local n = #s
  if self._bitsWritten + n * 8 > self._numBits then
    error(PANIC_WRITE_OVERFLOW, 2)
  end

  local headBytes = (8 - (self._bitsWritten % 64) // 8) % 8
  if headBytes > n then headBytes = n end
  for i = 1, headBytes do
    self:writeBits(string.byte(s, i), 8)
  end
  if headBytes == n then return end

  local numWords = (n - headBytes) // 8
  if numWords > 0 then
    self._pieces[#self._pieces + 1] = string.sub(s, headBytes + 1, headBytes + numWords * 8)
    self._bitsWritten = self._bitsWritten + numWords * 64
    self._wordIndex = self._wordIndex + numWords
  end

  for i = headBytes + numWords * 8 + 1, n do
    self:writeBits(string.byte(s, i), 8)
  end
end

-- STANDARD.md, General Conventions, "Flush": store the partial scratch word.
function BitWriter:flushBits()
  if self._scratchBits ~= 0 then
    self._pieces[#self._pieces + 1] = string.pack("<i8", self._scratch)
    self._scratch = 0
    self._scratchBits = 0
    self._wordIndex = self._wordIndex + 1
  end
end

-- STANDARD.md, General Conventions, "Bit index and alignment".
function BitWriter:alignBits()
  return (8 - self._bitsWritten % 8) % 8
end

function BitWriter:bitsWritten()
  return self._bitsWritten
end

function BitWriter:bitsAvailable()
  return self._numBits - self._bitsWritten
end

-- STANDARD.md, General Conventions, "Flush": the meaningful length is the
-- number of bytes actually required, rounded up to a byte.
function BitWriter:bytesWritten()
  return (self._bitsWritten + 7) // 8
end

function BitWriter:data()
  local joined = table.concat(self._pieces)
  local n = self:bytesWritten()
  if #joined < n then
    joined = joined .. string.rep("\0", n - #joined)
  end
  return string.sub(joined, 1, n)
end

--------------------------------------------------------------------------------
-- BitReader. STANDARD.md, Reader Obligations: the bounds test precedes every
-- load. The data is kept with 16 zero bytes appended (DESIGN.md (d)), so every
-- window load inside the bounds is one string.unpack and padding is loaded,
-- never interpreted.
--------------------------------------------------------------------------------

local BitReader = {}
BitReader.__index = BitReader

local PADDING = string.rep("\0", 16)

function BitReader:reset(data)
  if type(data) ~= "string" then
    error("serialize: BitReader reads a Lua string", 3)
  end
  self._data = data
  self._padded = data .. PADDING
  self._numBits = #data * 8
  self._bitsRead = 0
end

-- STANDARD.md, Reader Obligations, "Reading past the end must fail": the test
-- is bit_index + n > 8 * bytes, made before any bit is consumed.
function BitReader:wouldReadPastEnd(bits)
  return self._bitsRead + bits > self._numBits
end

-- STANDARD.md, bits: bits in [1,32], LSB first. serialize.go's ReadBits panics
-- panicBitsRange and panicReadOverflow.
function BitReader:readBits(bits)
  checkBits(bits)
  if self._bitsRead + bits > self._numBits then
    error(PANIC_READ_OVERFLOW, 2)
  end
  local byteIndex = self._bitsRead // 8
  local window = string.unpack("<I8", self._padded, byteIndex + 1)
  local value = (window >> (self._bitsRead & 7)) & ((1 << bits) - 1)
  self._bitsRead = self._bitsRead + bits
  return value
end

-- STANDARD.md, align: readers verify the padding is zero, else refuse.
function BitReader:readAlign()
  local remainderBits = self._bitsRead % 8
  if remainderBits ~= 0 then
    if self:readBits(8 - remainderBits) ~= 0 then
      return false
    end
  end
  return true
end

-- STANDARD.md, bytes: aligned only; a fresh Lua string, never an alias.
function BitReader:readBytes(n)
  if self._bitsRead % 8 ~= 0 then
    error(PANIC_NOT_ALIGNED, 2)
  end
  if math.type(n) ~= "integer" or n < 0 then
    error("serialize: a byte count is a non-negative integer", 2)
  end
  -- compare in bytes, so a huge count cannot wrap the bound
  if n > (self._numBits - self._bitsRead) // 8 then
    error(PANIC_READ_OVERFLOW, 2)
  end
  local offset = self._bitsRead // 8
  self._bitsRead = self._bitsRead + n * 8
  return string.sub(self._data, offset + 1, offset + n)
end

function BitReader:alignBits()
  return (8 - self._bitsRead % 8) % 8
end

function BitReader:bitsRead()
  return self._bitsRead
end

function BitReader:bitsRemaining()
  return self._numBits - self._bitsRead
end

M.api = {
  BitWriter = {
    new = function(capacity)
      local w = setmetatable({}, BitWriter)
      w:reset(capacity)
      return w
    end,
  },
  BitReader = {
    new = function(data)
      local r = setmetatable({}, BitReader)
      r:reset(data)
      return r
    end,
  },
}

return M
