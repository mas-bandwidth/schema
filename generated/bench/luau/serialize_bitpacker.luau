--!strict
-- serialize_bitpacker.luau — BitWriter and BitReader (DESIGN.md (a), (b),
-- (g) and (h)), the bitpacking bottom layer of the serialize.luau library,
-- wire compatible with serialize (STANDARD.md, format version 2.0) and
-- serialize.go v2.0.0 bitpacker.go: the same values produce the same bytes.
--
-- The writer packs bits least-significant-bit first into a 64-bit scratch
-- word; a value that crosses the 64-bit boundary is split (low 64-s bits
-- finish the word, the rest begin the next), and each full word is stored as
-- 8 little-endian bytes (STANDARD.md, General Conventions). Flush writes the
-- final partial word; the meaningful length is the bits written rounded up to
-- a byte. The reader reads the same wire, never a byte past the data's end,
-- however short the data (STANDARD.md, Reader Obligations).
--
-- The writer is STRICT: for every value it emits exactly serialize.go v2.0.0
-- emits, byte for byte; API misuse that serialize.go panics on raises the
-- same panic text (DESIGN.md (g)). Reads refuse where serialize.go refuses;
-- hostile bytes never raise. offset and count extend Go's []byte because a
-- Luau buffer has no slices: omitted they are 0 and the buffer's length
-- (DESIGN.md (h)).
--
-- Copyright © 2016 - 2026, Más Bandwidth LLC. BSD 3-Clause (see LICENSE).

-- Raise messages for API misuse: serialize.go's panic strings, raised naming
-- the rule (serialize.go serialize.go; DESIGN.md (g)).
local RAISE_BITS_RANGE = "serialize: bits must be in [1,32]"
local RAISE_WRITE_OVERFLOW = "serialize: bit writer overflow"
local RAISE_READ_OVERFLOW = "serialize: bit reader would read past the end of the buffer"
local RAISE_NOT_ALIGNED = "serialize: byte array serialization requires byte alignment"
local RAISE_BUFFER_BYTES = "serialize: bit writer buffer size must be a multiple of 8 bytes"
local RAISE_READ_DST = "serialize: bit reader destination buffer too small"

-- ---------------------------------------------------------------------------
-- BitWriter (serialize.go bitpacker.go; STANDARD.md bits, align, bytes,
-- General Conventions: 64-bit scratch, LSB-first, little-endian words).

local BitWriter = {}
BitWriter.__index = BitWriter

local function writerPack(self: any, value: number, bits: number)
	local mask = (2 ^ bits) - 1
	local v = bit32.band(value, mask)
	local vi = integer.create(v)
	local s = self._scratchBits
	self._scratch = integer.bor(self._scratch, integer.lshift(vi, integer.create(s)))
	local newS = s + bits
	if newS >= 64 then
		buffer.writeinteger(self._data, self._wordIndex * 8, self._scratch)
		self._wordIndex += 1
		self._scratch = integer.rshift(vi, integer.create(64 - s))
		self._scratchBits = newS - 64
	else
		self._scratchBits = newS
	end
	self._bitsWritten += bits
end

-- reset points the writer at a buffer and clears all write state. The buffer
-- size must be a multiple of 8 bytes: the writer stores 8-byte words
-- (STANDARD.md General Conventions; serialize.go panicBufferBytes).
function BitWriter.reset(self: any, buf: buffer)
	local n = buffer.len(buf)
	if n % 8 ~= 0 then
		error(RAISE_BUFFER_BYTES, 2)
	end
	self._data = buf
	self._numBits = n * 8
	self._scratch = 0i
	self._bitsWritten = 0
	self._wordIndex = 0
	self._scratchBits = 0
end

function BitWriter.new(buf: buffer): any
	local self = setmetatable({}, BitWriter)
	self:reset(buf)
	return self
end

-- writeBits writes the low bits bits of value, masking like Go. bits must be
-- in [1,32]; bits of value above that count are ignored. Panics if the write
-- would go past the end of the buffer (serialize.go WriteBits).
function BitWriter.writeBits(self: any, value: number, bits: number)
	if bits < 1 or bits > 32 then
		error(RAISE_BITS_RANGE, 2)
	end
	if self._bitsWritten + bits > self._numBits then
		error(RAISE_WRITE_OVERFLOW, 2)
	end
	writerPack(self, value, bits)
end

-- writeAlign pads with zero bits to the next byte boundary; an aligned stream
-- writes nothing (STANDARD.md align).
function BitWriter.writeAlign(self: any)
	local remainder = self._bitsWritten % 8
	if remainder ~= 0 then
		self:writeBits(0, 8 - remainder)
	end
end

-- writeBytes copies count bytes of src from offset into an ALIGNED stream.
-- The writer must be byte aligned, else misuse; the whole run must fit, else
-- overflow. Bytes are bitpacked 8 at a time, byte-identical to serialize.go's
-- head/block/tail copy (STANDARD.md bytes; DESIGN.md (h)).
function BitWriter.writeBytes(self: any, src: buffer, offset: number?, count: number?)
	local off = offset or 0
	local cnt = count or buffer.len(src)
	if self._bitsWritten % 8 ~= 0 then
		error(RAISE_NOT_ALIGNED, 2)
	end
	if self._bitsWritten + cnt * 8 > self._numBits then
		error(RAISE_WRITE_OVERFLOW, 2)
	end
	for i = 0, cnt - 1 do
		writerPack(self, buffer.readu8(src, off + i), 8)
	end
end

-- flushBits writes out the partially filled scratch word (STANDARD.md General
-- Conventions, Flush). The flush ends the write.
function BitWriter.flushBits(self: any)
	if self._scratchBits ~= 0 then
		buffer.writeinteger(self._data, self._wordIndex * 8, self._scratch)
		self._scratch = 0i
		self._scratchBits = 0
		self._wordIndex += 1
	end
end

-- alignBits returns the number of align bits an align would write now, in
-- [0,7] (STANDARD.md, Bit index and alignment).
function BitWriter.alignBits(self: any): number
	return (8 - self._bitsWritten % 8) % 8
end

function BitWriter.bitsWritten(self: any): number
	return self._bitsWritten
end

function BitWriter.bitsAvailable(self: any): number
	return self._numBits - self._bitsWritten
end

-- bytesWritten is the meaningful length: bits written rounded up to a byte
-- (STANDARD.md General Conventions, Flush).
function BitWriter.bytesWritten(self: any): number
	return (self._bitsWritten + 7) // 8
end

-- data returns the written bytes as a fresh buffer (DESIGN.md (d): a Luau
-- buffer has no slices). Call flushBits first.
function BitWriter.data(self: any): buffer
	local n = self:bytesWritten()
	local out = buffer.create(n)
	if n > 0 then
		buffer.copy(out, 0, self._data, 0, n)
	end
	return out
end

-- ---------------------------------------------------------------------------
-- BitReader (serialize.go bitpacker.go; STANDARD.md Reader Obligations: no
-- read goes past the data's end, hostile bytes never raise, a refusal leaves
-- its destination untouched).

local BitReader = {}
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

-- wouldReadPastEnd returns true if reading bits more bits would read past the
-- end of the data (serialize.go WouldReadPastEnd).
function BitReader.wouldReadPastEnd(self: any, bits: number): boolean
	return self._bitsRead + bits > self._numBits
end

-- readBits reads bits in [1,32] as a number, least-significant-bit first.
-- Misuse (bits outside [1,32], past the end) raises serialize.go's panic text
-- (serialize.go ReadBits). The reader never touches a byte past the data's
-- end: the past-end test happens before any byte is loaded. One
-- buffer.readbits call reads the whole field (LSB first, the wire's own bit
-- order), so the cost no longer grows with the width.
function BitReader.readBits(self: any, bits: number): number
	if bits < 1 or bits > 32 then
		error(RAISE_BITS_RANGE, 2)
	end
	if self._bitsRead + bits > self._numBits then
		error(RAISE_READ_OVERFLOW, 2)
	end
	local start = self._bitsRead
	self._bitsRead = start + bits
	return buffer.readbits(self._data, start, bits)
end

-- readAlign reads an align: skips to the next byte boundary, refusing nonzero
-- padding bits (returns false) per STANDARD.md align.
function BitReader.readAlign(self: any): boolean
	local remainder = self._bitsRead % 8
	if remainder ~= 0 then
		local value = self:readBits(8 - remainder)
		if value ~= 0 then
			return false
		end
	end
	return true
end

-- readBytes reads count bytes from an ALIGNED stream into dst at offset. The
-- reader must be byte aligned, else misuse; the whole run must fit, else
-- overflow (serialize.go ReadBytes; DESIGN.md (h)).
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
	for i = 0, cnt - 1 do
		buffer.writeu8(dst, off + i, self:readBits(8))
	end
end

-- alignBits returns the number of align bits an align would read now, in
-- [0,7] (STANDARD.md, Bit index and alignment).
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
})
