--!strict
-- serialize_bytes.luau — bytes, string and wstring (DESIGN.md (g) and (h);
-- STANDARD.md, Bytes and Strings, Reader Obligations, The vector format).
--
-- Wire compatible with serialize.go v1.15.1: the same values produce the same
-- bytes, every refusal STANDARD.md and serialize.go state is made, a refusal
-- writes nothing (the whole operation's width is checked before any bit
-- moves), reads never go past the data's end and hostile bytes never raise.
--
-- The stream's bit I/O is reached through Luau's buffer bit operations
-- (buffer.readbits/writebits, little-endian and LSB-first, the format's own
-- order) because this card's base has no serialize_bitpacker.luau. The
-- arms use only the buffer the stream already holds and its bit accounting.
--
-- Copyright © 2016 - 2026, Más Bandwidth LLC. BSD 3-Clause (see LICENSE).

local Stream = require("./serialize_stream")

local Overflow = Stream.SerializeError.Overflow
local ValueOutOfRange = Stream.SerializeError.ValueOutOfRange
local Align = Stream.SerializeError.Align

local M: { [string]: any } = {}

-- serialize_bitpacker.luau may not be on this card's base yet. When it is,
-- the stream's own bit primitives are used so the stream's scratch word and a
-- later family's writes stay coherent; while it is absent, the same bits go
-- through the buffer's own LSB-first bit operations.
local hasBitpacker = pcall(function()
	return Stream.family("bitpacker")
end)

-- bitsRequired is (min == max) ? 0 : 32 - clz(max - min) over the unsigned
-- domain; the callers here pass [0, bufferSize - 1] (STANDARD.md int (ranged)).
local function bitsRequired(min: number, max: number): number
	if min == max then
		return 0
	end
	local x = max - min
	local bits = 0
	while x > 0 do
		bits += 1
		x = math.floor(x / 2)
	end
	return bits
end

-- validateBufferSize enforces serialize.go's panicBufferSize: bufferSize is
-- in [1, 1<<31) and an integer (STANDARD.md string, wstring; DESIGN.md (a)).
local function validateBufferSize(bufferSize: any)
	if type(bufferSize) ~= "number" or bufferSize % 1 ~= 0 then
		error("serialize: string buffer size must be an integer", 3)
	end
	if bufferSize < 1 or bufferSize > 0x7fffffff then
		error("serialize: string buffer size must be in [1,1<<31)", 3)
	end
end

-- writeBitsRaw writes bits in [1,32] at the stream's bit index (the buffer's
-- own LSB-first bit order, DESIGN.md (b); STANDARD.md General Conventions).
local function writeBitsRaw(s: any, value: number, bits: number)
	if bits == 0 then
		return
	end
	if hasBitpacker then
		s:_writeBits(value, bits)
	else
		buffer.writebits(s._data, s._bits, bits, value)
		s._bits += bits
	end
end

local function readBitsRaw(s: any, bits: number): number
	if bits == 0 then
		return 0
	end
	if hasBitpacker then
		return s:_readBits(bits)
	end
	local value = buffer.readbits(s._data, s._bits, bits)
	s._bits += bits
	return value
end

-- readAlign verifies that the padding bits are zero (STANDARD.md align).
local function readAlign(s: any): boolean
	local n = s:_alignBits()
	if n == 0 then
		return true
	end
	if s._bits + n > s._numBits then
		return s:_fail(Overflow)
	end
	if readBitsRaw(s, n) ~= 0 then
		return s:_fail(Align)
	end
	return true
end

-- writeLength writes the narrow string length as serialize_int(length, 0,
-- maxLength) (STANDARD.md string; the 32-bit offset is a number, DESIGN.md
-- (c)). The caller has already checked the whole operation's width.
local function writeLength(s: any, length: number, maxLength: number)
	writeBitsRaw(s, length, bitsRequired(0, maxLength))
end

-- readLength reads that length, refusing past the end and a decoded offset
-- outside [0, maxLength] (STANDARD.md int (ranged), Reader Obligations).
local function readLength(s: any, maxLength: number): (boolean, number)
	local bits = bitsRequired(0, maxLength)
	if s._bits + bits > s._numBits then
		s:_fail(Overflow)
		return false, 0
	end
	local offset = readBitsRaw(s, bits)
	if offset > maxLength then
		s:_fail(ValueOutOfRange)
		return false, 0
	end
	return true, offset
end

-- The Go utf8 decoder's accept/reject decision and its invalid-byte advance:
-- an invalid sequence is U+FFFD and consumes one byte (DESIGN.md (g): the
-- writer walks the bytes as Go's `for range` does).
local function decodeRune(value: string, i: number): (number, number, boolean)
	local n = #value
	if i > n then
		return 0xFFFD, 1, false
	end
	local b0 = string.byte(value, i) :: number
	if b0 < 0x80 then
		return b0, 1, true
	end
	local extra: number
	local minimum: number
	local rune: number
	if b0 < 0xC2 then
		return 0xFFFD, 1, false
	elseif b0 < 0xE0 then
		extra, minimum, rune = 1, 0x80, b0 - 0xC0
	elseif b0 < 0xF0 then
		extra, minimum, rune = 2, 0x800, b0 - 0xE0
	elseif b0 < 0xF5 then
		extra, minimum, rune = 3, 0x10000, b0 - 0xF0
	else
		return 0xFFFD, 1, false
	end
	if i + extra > n then
		return 0xFFFD, 1, false
	end
	for k = 1, extra do
		local b = string.byte(value, i + k) :: number
		if b < 0x80 or b > 0xBF then
			return 0xFFFD, 1, false
		end
		rune = rune * 0x40 + (b - 0x80)
	end
	if rune < minimum or rune > 0x10FFFF or (rune >= 0xD800 and rune <= 0xDFFF) then
		return 0xFFFD, 1, false
	end
	return rune, extra + 1, true
end

-- utf8Valid is Go's utf8.Valid: every rune well formed under Unicode Table
-- 3-7, so overlong forms, surrogates and values past U+10FFFF are refused
-- (STANDARD.md string; DESIGN.md (g)).
local function utf8Valid(value: string): boolean
	local i = 1
	while i <= #value do
		local _, size, ok = decodeRune(value, i)
		if not ok then
			return false
		end
		i += size
	end
	return true
end

-- utf16Units is the wire's unit sequence: Go's `for range` code points, one
-- unit per basic-plane code point and a surrogate pair per astral code point
-- (STANDARD.md wstring; DESIGN.md (g)).
local function utf16Units(value: string): { number }
	local units = {}
	local i = 1
	while i <= #value do
		local rune, size = decodeRune(value, i)
		if rune >= 0x10000 then
			local v = rune - 0x10000
			units[#units + 1] = 0xD800 + math.floor(v / 0x400)
			units[#units + 1] = 0xDC00 + v % 0x400
		else
			units[#units + 1] = rune
		end
		i += size
	end
	return units
end

-- ---------------------------------------------------------------------------
-- bytes (STANDARD.md bytes; DESIGN.md (g): aligns first, count not written).

function M.writeBytes(s: any, data: any): boolean
	if type(data) ~= "buffer" then
		error("serialize: serializeBytes data must be a buffer", 2)
	end
	if s:_latched() then
		return false
	end
	local count = buffer.len(data)
	local align = s:_alignBits()
	if s._bits + align + count * 8 > s._numBits then
		return s:_fail(Overflow)
	end
	writeBitsRaw(s, 0, align)
	if count > 0 then
		if hasBitpacker then
			s:_writeBytes(data, 0, count)
		else
			buffer.copy(s._data, s._bits // 8, data, 0, count)
			s._bits += count * 8
		end
	end
	return true
end

function M.readBytes(s: any, data: any): boolean
	if type(data) ~= "buffer" then
		error("serialize: serializeBytes data must be a buffer", 2)
	end
	if s:_latched() then
		return false
	end
	if not readAlign(s) then
		return false
	end
	local count = buffer.len(data)
	if s._bits + count * 8 > s._numBits then
		return s:_fail(Overflow)
	end
	if count > 0 then
		if hasBitpacker then
			s:_readBytes(data, 0, count)
		else
			buffer.copy(data, 0, s._data, s._bits // 8, count)
			s._bits += count * 8
		end
	end
	return true
end

function M.measureBytes(s: any, data: any): boolean
	if type(data) ~= "buffer" then
		error("serialize: serializeBytes data must be a buffer", 2)
	end
	if s:_latched() then
		return false
	end
	if not s:_measure(7) then
		return false
	end
	return s:_measure(buffer.len(data) * 8)
end

-- ---------------------------------------------------------------------------
-- string (STANDARD.md string; DESIGN.md (g): length, align, bytes; the reader
-- refuses an interior NUL and malformed UTF-8 with ValueOutOfRange).

function M.writeString(s: any, holder: any, bufferSize: any): boolean
	if type(holder) ~= "table" then
		error("serialize: serializeString holder must be a table", 2)
	end
	validateBufferSize(bufferSize)
	if type(holder.value) ~= "string" then
		error("serialize: serializeString value must be a string", 2)
	end
	if s:_latched() then
		return false
	end
	local value = holder.value
	local length = #value
	local maxLength = bufferSize - 1
	if length >= bufferSize then
		return s:_fail(ValueOutOfRange)
	end
	local lengthBits = bitsRequired(0, maxLength)
	local align = (8 - (s._bits + lengthBits) % 8) % 8
	if s._bits + lengthBits + align + length * 8 > s._numBits then
		return s:_fail(Overflow)
	end
	writeLength(s, length, maxLength)
	writeBitsRaw(s, 0, align)
	if length > 0 then
		if hasBitpacker then
			s:_writeBytes(buffer.fromstring(value), 0, length)
		else
			buffer.writestring(s._data, s._bits // 8, value)
			s._bits += length * 8
		end
	end
	return true
end

function M.readString(s: any, holder: any, bufferSize: any): boolean
	if type(holder) ~= "table" then
		error("serialize: serializeString holder must be a table", 2)
	end
	validateBufferSize(bufferSize)
	if s:_latched() then
		return false
	end
	local ok, length = readLength(s, bufferSize - 1)
	if not ok then
		return false
	end
	if not readAlign(s) then
		return false
	end
	if s._bits + length * 8 > s._numBits then
		return s:_fail(Overflow)
	end
	local value: string
	if hasBitpacker then
		local payload = buffer.create(length)
		if length > 0 then
			s:_readBytes(payload, 0, length)
		end
		value = buffer.tostring(payload)
	else
		value = buffer.readstring(s._data, s._bits // 8, length)
		s._bits += length * 8
	end
	if string.find(value, "\0", 1, true) ~= nil then
		return s:_fail(ValueOutOfRange)
	end
	if not utf8Valid(value) then
		return s:_fail(ValueOutOfRange)
	end
	holder.value = value
	return true
end

function M.measureString(s: any, holder: any, bufferSize: any): boolean
	if type(holder) ~= "table" then
		error("serialize: serializeString holder must be a table", 2)
	end
	validateBufferSize(bufferSize)
	if type(holder.value) ~= "string" then
		error("serialize: serializeString value must be a string", 2)
	end
	if s:_latched() then
		return false
	end
	local length = #holder.value
	if length >= bufferSize then
		return s:_fail(ValueOutOfRange)
	end
	if not s:_measure(bitsRequired(0, bufferSize - 1)) then
		return false
	end
	if not s:_measure(7) then
		return false
	end
	return s:_measure(length * 8)
end

-- ---------------------------------------------------------------------------
-- wstring (STANDARD.md wstring; DESIGN.md (g): length in UTF-16 code units,
-- then each unit as 32 bits, with no alignment anywhere).

function M.writeWideString(s: any, holder: any, bufferSize: any): boolean
	if type(holder) ~= "table" then
		error("serialize: serializeWideString holder must be a table", 2)
	end
	validateBufferSize(bufferSize)
	if type(holder.value) ~= "string" then
		error("serialize: serializeWideString value must be a string", 2)
	end
	if s:_latched() then
		return false
	end
	local units = utf16Units(holder.value)
	local length = #units
	if length >= bufferSize then
		return s:_fail(ValueOutOfRange)
	end
	local lengthBits = bitsRequired(0, bufferSize - 1)
	if s._bits + lengthBits + length * 32 > s._numBits then
		return s:_fail(Overflow)
	end
	writeLength(s, length, bufferSize - 1)
	for i = 1, #units do
		writeBitsRaw(s, units[i], 32)
	end
	return true
end

function M.readWideString(s: any, holder: any, bufferSize: any): boolean
	if type(holder) ~= "table" then
		error("serialize: serializeWideString holder must be a table", 2)
	end
	validateBufferSize(bufferSize)
	if s:_latched() then
		return false
	end
	local ok, length = readLength(s, bufferSize - 1)
	if not ok then
		return false
	end
	if s._bits + length * 32 > s._numBits then
		return s:_fail(Overflow)
	end
	local parts = {}
	local remaining = length
	while remaining > 0 do
		local unit = readBitsRaw(s, 32)
		remaining -= 1
		if unit == 0 or unit > 0xFFFF or (unit >= 0xDC00 and unit <= 0xDFFF) then
			return s:_fail(ValueOutOfRange)
		end
		if unit >= 0xD800 and unit <= 0xDBFF then
			if remaining == 0 then
				return s:_fail(ValueOutOfRange)
			end
			local low = readBitsRaw(s, 32)
			remaining -= 1
			if low < 0xDC00 or low > 0xDFFF then
				return s:_fail(ValueOutOfRange)
			end
			local codepoint = 0x10000 + (unit - 0xD800) * 0x400 + (low - 0xDC00)
			parts[#parts + 1] = utf8.char(codepoint)
		else
			parts[#parts + 1] = utf8.char(unit)
		end
	end
	holder.value = table.concat(parts)
	return true
end

function M.measureWideString(s: any, holder: any, bufferSize: any): boolean
	if type(holder) ~= "table" then
		error("serialize: serializeWideString holder must be a table", 2)
	end
	validateBufferSize(bufferSize)
	if type(holder.value) ~= "string" then
		error("serialize: serializeWideString value must be a string", 2)
	end
	if s:_latched() then
		return false
	end
	local length = #utf16Units(holder.value)
	if length >= bufferSize then
		return s:_fail(ValueOutOfRange)
	end
	if not s:_measure(bitsRequired(0, bufferSize - 1)) then
		return false
	end
	return s:_measure(length * 32)
end

return table.freeze(M)
