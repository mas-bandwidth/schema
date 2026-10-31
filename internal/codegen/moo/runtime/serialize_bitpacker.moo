--!strict
-- serialize_bitpacker.moo -- BitWriter and BitReader (DESIGN.md, "Bit order"),
-- the bottom layer of serialize.moo, wire compatible with serialize
-- (STANDARD.md, format version 2.0) and serialize.go bitpacker.go.
--
-- STANDARD.md packs bits least-significant-bit first into a 64-bit scratch
-- word and stores each full word as 8 little-endian bytes. moo has no 64-bit
-- integer, so this writer packs into a 32-bit scratch and stores 4
-- little-endian bytes at a time: because the order is LSB first and the words
-- are little-endian, the bytes are identical, a 64-bit word being exactly its
-- two 32-bit halves, low half first. Flush writes the partial word and zeroes
-- the rest of its 8-byte word, as the 64-bit store does. The reader reads byte
-- by byte and never touches a byte past the data's end.
--
-- Misuse that serialize.go panics on raises the same text. Values are numbers
-- in [0, 2^32); bit counts are in [1, 32].
--
-- Copyright © 2016 - 2026, Más Bandwidth LLC. BSD 3-Clause (see LICENSE).

local RAISE_BITS_RANGE = "serialize: bits must be in [1,32]"
local RAISE_WRITE_OVERFLOW = "serialize: bit writer overflow"
local RAISE_READ_OVERFLOW = "serialize: bit reader would read past the end of the buffer"
local RAISE_NOT_ALIGNED = "serialize: byte array serialization requires byte alignment"
local RAISE_BUFFER_BYTES = "serialize: bit writer buffer size must be a multiple of 8 bytes"
local RAISE_READ_DST = "serialize: bit reader destination buffer too small"

local POW2: { number } = {}
local MASK: { number } = {}
for i = 0, 32 do
	POW2[i] = 2 ^ i
	MASK[i] = if i == 32 then 4294967295 else 2 ^ i - 1
end

local function isBits(bits: any): boolean
	return type(bits) == "number" and bits >= 1 and bits <= 32 and bits == math.floor(bits)
end

-- ---------------------------------------------------------------------------
-- BitWriter

local BitWriter: any = {}
BitWriter.__index = BitWriter

function BitWriter.reset(self: any, buf: buffer)
	local n = buffer.len(buf)
	if n % 8 ~= 0 then
		error(RAISE_BUFFER_BYTES, 2)
	end
	self._data = buf
	self._numBits = n * 8
	self._scratch = 0
	self._scratchBits = 0
	self._word = 0 -- the index of the next 32-bit word to store
	self._bitsWritten = 0
end

function BitWriter.new(buf: buffer): any
	local self = setmetatable({}, BitWriter)
	self:reset(buf)
	return self
end

-- _pack writes the low bits of value, already masked to bits in [1, 32] and
-- already bounds checked.
function BitWriter._pack(self: any, value: number, bits: number)
	local s = self._scratchBits
	if s + bits < 32 then
		self._scratch += value * POW2[s]
		self._scratchBits = s + bits
	else
		local first = 32 - s
		buffer.writeu32(self._data, self._word * 4, self._scratch + bit32.band(value, MASK[first]) * POW2[s])
		self._word += 1
		self._scratch = if first == 32 then 0 else bit32.rshift(value, first)
		self._scratchBits = s + bits - 32
	end
	self._bitsWritten += bits
end

-- writeBits writes the low bits bits of value, masking like Go; bits in [1,32].
function BitWriter.writeBits(self: any, value: number, bits: number)
	if not isBits(bits) then
		error(RAISE_BITS_RANGE, 2)
	end
	if self._bitsWritten + bits > self._numBits then
		error(RAISE_WRITE_OVERFLOW, 2)
	end
	self:_pack(bit32.band(value, MASK[bits]), bits)
end

-- writeAlign pads with zero bits to the next byte boundary (STANDARD.md align).
function BitWriter.writeAlign(self: any)
	local remainder = self._bitsWritten % 8
	if remainder ~= 0 then
		self:writeBits(0, 8 - remainder)
	end
end

-- _copy writes count bytes of src from offset into an aligned stream, already
-- checked: head bytes through the scratch to a 32-bit boundary, the middle by
-- buffer.copy (the scratch is empty there), the tail through the scratch.
function BitWriter._copy(self: any, src: buffer, offset: number, count: number)
	local i = 0
	while i < count and self._scratchBits ~= 0 do
		self:_pack(buffer.readu8(src, offset + i), 8)
		i += 1
	end
	local words = (count - i) // 4
	if words > 0 then
		buffer.copy(self._data, self._word * 4, src, offset + i, words * 4)
		self._word += words
		self._bitsWritten += words * 32
		i += words * 4
	end
	while i < count do
		self:_pack(buffer.readu8(src, offset + i), 8)
		i += 1
	end
end

-- writeBytes copies count bytes of src from offset into an ALIGNED stream;
-- offset and count extend Go's []byte (a buffer has no slices) and default to
-- 0 and the buffer's length.
function BitWriter.writeBytes(self: any, src: buffer, offset: number?, count: number?)
	local off = offset or 0
	local cnt = count or buffer.len(src)
	if self._bitsWritten % 8 ~= 0 then
		error(RAISE_NOT_ALIGNED, 2)
	end
	if self._bitsWritten + cnt * 8 > self._numBits then
		error(RAISE_WRITE_OVERFLOW, 2)
	end
	self:_copy(src, off, cnt)
end

-- flushBits stores the partial word and zeroes the rest of its 8-byte word,
-- the bytes serialize.go's 64-bit store leaves (STANDARD.md, Flush). It
-- changes no state, so a second flush stores the same bytes.
function BitWriter.flushBits(self: any)
	if self._bitsWritten % 64 ~= 0 then
		buffer.writeu32(self._data, self._word * 4, self._scratch)
		if self._word % 2 == 0 then
			buffer.writeu32(self._data, self._word * 4 + 4, 0)
		end
	end
end

function BitWriter.alignBits(self: any): number
	return (8 - self._bitsWritten % 8) % 8
end

function BitWriter.bitsWritten(self: any): number
	return self._bitsWritten
end

function BitWriter.bitsAvailable(self: any): number
	return self._numBits - self._bitsWritten
end

function BitWriter.bytesWritten(self: any): number
	return (self._bitsWritten + 7) // 8
end

-- data returns the written bytes as a fresh buffer. Call flushBits first.
function BitWriter.data(self: any): buffer
	local n = self:bytesWritten()
	local out = buffer.create(n)
	if n > 0 then
		buffer.copy(out, 0, self._data, 0, n)
	end
	return out
end

-- ---------------------------------------------------------------------------
-- BitReader

local BitReader: any = {}
BitReader.__index = BitReader

function BitReader.reset(self: any, data: buffer)
	self._data = data
	self._numBits = buffer.len(data) * 8
	self._bitsRead = 0
end

function BitReader.new(data: buffer): any
	local self = setmetatable({}, BitReader)
	self:reset(data)
	return self
end

function BitReader.wouldReadPastEnd(self: any, bits: number): boolean
	return self._bitsRead + bits > self._numBits
end

-- _take reads bits in [1, 32], already checked, least-significant-bit first,
-- one byte at a time (at most five bytes).
function BitReader._take(self: any, bits: number): number
	local pos = self._bitsRead
	local data = self._data
	local result = 0
	local got = 0
	while got < bits do
		local off = pos % 8
		local take = 8 - off
		if take > bits - got then
			take = bits - got
		end
		local chunk = bit32.extract(buffer.readu8(data, pos // 8), off, take)
		result += chunk * POW2[got]
		got += take
		pos += take
	end
	self._bitsRead = pos
	return result
end

function BitReader.readBits(self: any, bits: number): number
	if not isBits(bits) then
		error(RAISE_BITS_RANGE, 2)
	end
	if self._bitsRead + bits > self._numBits then
		error(RAISE_READ_OVERFLOW, 2)
	end
	return self:_take(bits)
end

-- readAlign skips to the next byte boundary; false when the padding is not
-- zero (STANDARD.md align).
function BitReader.readAlign(self: any): boolean
	local remainder = self._bitsRead % 8
	if remainder ~= 0 then
		if self:readBits(8 - remainder) ~= 0 then
			return false
		end
	end
	return true
end

-- _copyOut reads count bytes from an aligned stream into dst at offset,
-- already checked.
function BitReader._copyOut(self: any, dst: buffer, offset: number, count: number)
	if count > 0 then
		buffer.copy(dst, offset, self._data, self._bitsRead // 8, count)
		self._bitsRead += count * 8
	end
end

-- readBytes reads count bytes from an ALIGNED stream into dst at offset.
function BitReader.readBytes(self: any, dst: buffer, offset: number?, count: number?)
	local off = offset or 0
	local cnt = count or buffer.len(dst)
	if self._bitsRead % 8 ~= 0 then
		error(RAISE_NOT_ALIGNED, 2)
	end
	if self._bitsRead + cnt * 8 > self._numBits then
		error(RAISE_READ_OVERFLOW, 2)
	end
	if off + cnt > buffer.len(dst) then
		error(RAISE_READ_DST, 2)
	end
	self:_copyOut(dst, off, cnt)
end

function BitReader.alignBits(self: any): number
	return (8 - self._bitsRead % 8) % 8
end

function BitReader.bitsRead(self: any): number
	return self._bitsRead
end

function BitReader.bitsRemaining(self: any): number
	return self._numBits - self._bitsRead
end

return table.freeze({
	BitWriter = table.freeze(BitWriter),
	BitReader = table.freeze(BitReader),
	POW2 = POW2,
	MASK = MASK,
})
