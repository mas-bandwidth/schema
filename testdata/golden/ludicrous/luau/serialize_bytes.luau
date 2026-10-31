--!strict
-- serialize_bytes.luau — bytes and string (DESIGN.md (g) and (h);
-- STANDARD.md, Bytes and Strings, Reader Obligations, The vector format).
--
-- Wire compatible with serialize.go v2.0.0: the same values produce the same
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

local PastEnd = Stream.SerializeError.PastEnd
local ValueOutOfRange = Stream.SerializeError.ValueOutOfRange
local InvalidUtf8 = Stream.SerializeError.InvalidUtf8
local EmbeddedNul = Stream.SerializeError.EmbeddedNul

-- The write-misuse traps (IMPLEMENTING.md U9, U6, U3, U4): text a reader
-- would refuse, text longer than buffer_size - 1 and an operation that does
-- not fit the room left raise on write and measure (the measure has no room
-- to check), never latch.
local RAISE_RANGE = "serialize: serializeString value must be at most buffer size - 1 bytes"
local RAISE_ROOM = "serialize: the write does not fit the room left in the buffer"
local RAISE_TEXT: { [string]: string } = {
	[EmbeddedNul] = "serialize: serializeString value must not hold a NUL (embedded_nul)",
	[InvalidUtf8] = "serialize: serializeString value must be well-formed UTF-8 (invalid_utf8)",
}

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

-- bufferSizeMisuse applies serialize.go's panicBufferSize: bufferSize is in
-- [1, 1<<31) and an integer (STANDARD.md string; DESIGN.md (a)). It returns
-- the rule broken, or nil; the arm raises it on write and measure and refuses
-- ValueOutOfRange on read (IMPLEMENTING.md U7, U16).
local function bufferSizeMisuse(bufferSize: any): string?
	if type(bufferSize) ~= "number" or bufferSize % 1 ~= 0 then
		return "serialize: string buffer size must be an integer"
	end
	if bufferSize < 1 or bufferSize > 0x7fffffff then
		return "serialize: string buffer size must be in [1,1<<31)"
	end
	return nil
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

-- readAlign is the stream's own align (STANDARD.md align): bytes and string
-- verify their padding through the one align every read shares, so the
-- padding rule lives in one place.
local function readAlign(s: any): boolean
	return s:serializeAlign()
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
		s:_fail(PastEnd)
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
-- an invalid sequence is U+FFFD and consumes one byte (DESIGN.md (g)).
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

-- textRefusal is the reason a string payload is refused for, or nil
-- (STANDARD.md string): a zero byte anywhere is EmbeddedNul, scanned over the
-- whole payload BEFORE the encoding is checked, so a payload that breaks both
-- rules is EmbeddedNul; then a payload that is not well-formed UTF-8 is
-- InvalidUtf8.
local function textRefusal(value: string): string?
	if string.find(value, "\0", 1, true) ~= nil then
		return EmbeddedNul
	end
	if not utf8Valid(value) then
		return InvalidUtf8
	end
	return nil
end

-- copyString is serialize_copy_string (IMPLEMENTING.md U15; the reference's
-- serialize_copy_string): the bytes of source before its first NUL, at most
-- destSize - 1 of them, so the copy fits a string field of buffer size
-- destSize. A Luau string is a value, so the copy is returned. A cut never
-- splits a UTF-8 character: it backs off over continuation bytes, so a
-- well-formed source gives a well-formed copy. destSize is caller's
-- declaration and raises unless it is an integer of at least 1.
function M.copyString(source: string, destSize: number): string
	if type(source) ~= "string" then
		error("serialize: copyString source must be a string", 3)
	end
	if type(destSize) ~= "number" or destSize % 1 ~= 0 or destSize < 1 then
		error("serialize: copyString destination size must be an integer >= 1", 3)
	end
	local n = #source
	local nul = string.find(source, "\0", 1, true)
	if nul ~= nil then
		n = nul - 1
	end
	if n > destSize - 1 then
		n = destSize - 1
		while n > 0 do
			local nextByte = string.byte(source, n + 1) :: number
			if nextByte < 0x80 or nextByte > 0xBF then
				break
			end
			n -= 1
		end
	end
	return string.sub(source, 1, n)
end

-- ---------------------------------------------------------------------------
-- bytes (STANDARD.md bytes; DESIGN.md (g): aligns first, count not written).

-- bytesRange is the optional counted form serializeBytes(data[, offset, count])
-- (the reference's serialize_bytes( stream, data, count ), with moo's offset):
-- count bytes of data from offset, the whole buffer by default. The count is
-- the caller's declaration and is never written (STANDARD.md bytes). It
-- returns the range, or nil when data is not a buffer or the range is not
-- integers inside it (IMPLEMENTING.md U7, U16): the caller's misuse, a trap on
-- write and measure and a ValueOutOfRange refusal on read.
local RAISE_BYTES_DATA = "serialize: serializeBytes data must be a buffer"
local RAISE_BYTES_RANGE = "serialize: serializeBytes offset and count must be integers inside the buffer"

local function bytesRange(data: any, offset: any, count: any): (number?, number?, string?)
	if type(data) ~= "buffer" then
		return nil, nil, RAISE_BYTES_DATA
	end
	local len = buffer.len(data)
	if offset == nil and count == nil then
		-- the whole-buffer form, the hot one: nothing to validate
		return 0, len, nil
	end
	local off = if offset == nil then 0 else offset
	if type(off) ~= "number" or off % 1 ~= 0 or off < 0 or off > len then
		return nil, nil, RAISE_BYTES_RANGE
	end
	local cnt = if count == nil then len - off else count
	if type(cnt) ~= "number" or cnt % 1 ~= 0 or cnt < 0 or cnt > len - off then
		return nil, nil, RAISE_BYTES_RANGE
	end
	return off, cnt, nil
end

function M.writeBytes(s: any, data: any, offset: any, count: any): boolean
	local off, n, bad = bytesRange(data, offset, count)
	if off == nil or n == nil then
		return s:_misuse(bad :: string)
	end
	if s:_latched() then
		return false
	end
	local align = s:_alignBits()
	if s._bits + align + n * 8 > s._numBits then
		return s:_misuse(RAISE_ROOM)
	end
	writeBitsRaw(s, 0, align)
	if n > 0 then
		if hasBitpacker then
			s:_writeBytes(data, off, n)
		else
			buffer.copy(s._data, s._bits // 8, data, off, n)
			s._bits += n * 8
		end
	end
	return true
end

function M.readBytes(s: any, data: any, offset: any, count: any): boolean
	local off, n, bad = bytesRange(data, offset, count)
	if off == nil or n == nil then
		return s:_misuse(bad :: string)
	end
	if s:_latched() then
		return false
	end
	if not readAlign(s) then
		return false
	end
	if s._bits + n * 8 > s._numBits then
		return s:_fail(PastEnd)
	end
	if n > 0 then
		if hasBitpacker then
			s:_readBytes(data, off, n)
		else
			buffer.copy(data, off, s._data, s._bits // 8, n)
			s._bits += n * 8
		end
	end
	return true
end

function M.measureBytes(s: any, data: any, offset: any, count: any): boolean
	local off, n, bad = bytesRange(data, offset, count)
	if off == nil or n == nil then
		return s:_misuse(bad :: string)
	end
	if s:_latched() then
		return false
	end
	if not s:_measure(7) then
		return false
	end
	return s:_measure(n * 8)
end

-- ---------------------------------------------------------------------------
-- string (STANDARD.md string; DESIGN.md (g): length, align, bytes; the reader
-- refuses a zero byte with EmbeddedNul, then malformed UTF-8 with InvalidUtf8,
-- and the writer and the measure trap on the same payloads, naming the same
-- reasons, and latch nothing: a writer is handed well-formed UTF-8 with no
-- zero byte; IMPLEMENTING.md U3, U4, U9).

function M.writeString(s: any, holder: any, bufferSize: any): boolean
	if type(holder) ~= "table" then
		return s:_misuse("serialize: serializeString holder must be a table")
	end
	local badSize = bufferSizeMisuse(bufferSize)
	if badSize ~= nil then
		return s:_misuse(badSize)
	end
	if type(holder.value) ~= "string" then
		return s:_misuse("serialize: serializeString value must be a string")
	end
	if s:_latched() then
		return false
	end
	local value = holder.value
	local length = #value
	local maxLength = bufferSize - 1
	if length >= bufferSize then
		return s:_misuse(RAISE_RANGE)
	end
	local refusal = textRefusal(value)
	if refusal ~= nil then
		return s:_misuse(RAISE_TEXT[refusal])
	end
	local lengthBits = bitsRequired(0, maxLength)
	local align = (8 - (s._bits + lengthBits) % 8) % 8
	if s._bits + lengthBits + align + length * 8 > s._numBits then
		return s:_misuse(RAISE_ROOM)
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
		return s:_misuse("serialize: serializeString holder must be a table")
	end
	local badSize = bufferSizeMisuse(bufferSize)
	if badSize ~= nil then
		return s:_misuse(badSize)
	end
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
	-- the claimed length is checked against the bytes left BEFORE anything
	-- the size of that claim is allocated
	if s._bits + length * 8 > s._numBits then
		return s:_fail(PastEnd)
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
	local refusal = textRefusal(value)
	if refusal ~= nil then
		return s:_fail(refusal)
	end
	holder.value = value
	return true
end

function M.measureString(s: any, holder: any, bufferSize: any): boolean
	if type(holder) ~= "table" then
		return s:_misuse("serialize: serializeString holder must be a table")
	end
	local badSize = bufferSizeMisuse(bufferSize)
	if badSize ~= nil then
		return s:_misuse(badSize)
	end
	if type(holder.value) ~= "string" then
		return s:_misuse("serialize: serializeString value must be a string")
	end
	if s:_latched() then
		return false
	end
	local length = #holder.value
	if length >= bufferSize then
		return s:_misuse(RAISE_RANGE)
	end
	-- the measure traps on the text the writer traps on, by the same
	-- trap (IMPLEMENTING.md U3, U6, U9)
	local refusal = textRefusal(holder.value)
	if refusal ~= nil then
		return s:_misuse(RAISE_TEXT[refusal])
	end
	if not s:_measure(bitsRequired(0, bufferSize - 1)) then
		return false
	end
	if not s:_measure(7) then
		return false
	end
	return s:_measure(length * 8)
end

return table.freeze(M)
