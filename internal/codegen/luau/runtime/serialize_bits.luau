--!strict
-- serialize_bits.luau — the bits family (DESIGN.md (a), (b), (c), (g) and
-- (h)): serializeBits, serializeBits64, bool and the fixed-width unsigned
-- helpers uint8/16/32/64, plus bitsRequired, bitsRequired64 and the zig-zag
-- helpers, wire compatible with serialize (STANDARD.md, format version 1.1)
-- and serialize.go v1.15.1 write_stream.go, read_stream.go and measure_stream.go.
--
-- The writer is STRICT: it emits exactly serialize.go's bytes, and where
-- serialize.go's WriteStream refuses (a full buffer) this port latches the
-- same kind and writes nothing for that call; API misuse that serialize.go
-- panics on (bits outside its range) raises naming the same rule. Reads refuse
-- where serialize.go refuses; a refusal leaves the caller's destination
-- untouched and the stream terminal (STANDARD.md, Reader Obligations).
--
-- The number model (DESIGN.md (c)): a value of 32 bits or fewer is a number,
-- a 64-bit value an integer (the native `integer` type), and the holder
-- convention (DESIGN.md (a)) is `{ value = ... }`.
--
-- Copyright © 2016 - 2026, Más Bandwidth LLC. BSD 3-Clause (see LICENSE).

-- SerializeError kinds as the stream latches them (DESIGN.md (a)).
local OVERFLOW = "overflow"

-- Raise messages for API misuse: serialize.go's panic strings (serialize.go
-- v1.15.1 serialize.go; DESIGN.md (g)).
local RAISE_BITS_RANGE = "serialize: bits must be in [1,32]"
local RAISE_BITS_RANGE64 = "serialize: bits must be in [1,64]"

local M = {}

-- ---------------------------------------------------------------------------
-- The dispatched arms: serializeBits (DISPATCH, DESIGN.md (h)), the whole
-- operation in serialize.go's order — raise on misuse, refuse on a latched
-- stream, the bounds check of the whole width, then the bits (serialize.go
-- write_stream.go SerializeBits / read_stream.go SerializeBits / measure_stream.go
-- SerializeBits).

function M.writeBits(s: any, h: any, bits: number): boolean
	if bits < 1 or bits > 32 then
		error(RAISE_BITS_RANGE, 2)
	end
	if s:_latched() then
		return false
	end
	if s:_available() < bits then
		return s:_fail(OVERFLOW)
	end
	local mask = (2 ^ bits) - 1
	s:_writeBits(bit32.band(h.value, mask), bits)
	return true
end

function M.readBits(s: any, h: any, bits: number): boolean
	if bits < 1 or bits > 32 then
		error(RAISE_BITS_RANGE, 2)
	end
	if s:_latched() then
		return false
	end
	if s:_available() < bits then
		return s:_fail(OVERFLOW)
	end
	h.value = s:_readBits(bits)
	return true
end

function M.measureBits(s: any, h: any, bits: number): boolean
	if bits < 1 or bits > 32 then
		error(RAISE_BITS_RANGE, 2)
	end
	return s:_measure(bits)
end

-- serializeBits64: the same, in [1,64]; past 32 bits the low 32 bits first,
-- then the high bits - 32 (serialize.go write_stream.go SerializeBits64,
-- STANDARD.md bits).
function M.writeBits64(s: any, h: any, bits: number): boolean
	if bits < 1 or bits > 64 then
		error(RAISE_BITS_RANGE64, 2)
	end
	if s:_latched() then
		return false
	end
	if s:_available() < bits then
		return s:_fail(OVERFLOW)
	end
	s:_writeBits64(h.value, bits)
	return true
end

function M.readBits64(s: any, h: any, bits: number): boolean
	if bits < 1 or bits > 64 then
		error(RAISE_BITS_RANGE64, 2)
	end
	if s:_latched() then
		return false
	end
	if s:_available() < bits then
		return s:_fail(OVERFLOW)
	end
	h.value = s:_readBits64(bits)
	return true
end

function M.measureBits64(s: any, h: any, bits: number): boolean
	if bits < 1 or bits > 64 then
		error(RAISE_BITS_RANGE64, 2)
	end
	return s:_measure(bits)
end

-- ---------------------------------------------------------------------------
-- The fixed-width write arms (FIXED, DESIGN.md (h)): the write arm is the
-- family's, whole — refuse on a latched stream, the bounds check of the whole
-- width, then the bits. The read arms are reached only after the stream's read
-- gate has refused on a latched stream and on running past the end, so they
-- read exactly the width and assign the holder last.

function M.writeBool(s: any, h: any): boolean
	if s:_latched() then
		return false
	end
	if s:_available() < 1 then
		return s:_fail(OVERFLOW)
	end
	s:_writeBits(h.value and 1 or 0, 1)
	return true
end

function M.readBool(s: any, h: any): boolean
	h.value = s:_readBits(1) ~= 0
	return true
end

function M.writeUint8(s: any, h: any): boolean
	if s:_latched() then
		return false
	end
	if s:_available() < 8 then
		return s:_fail(OVERFLOW)
	end
	s:_writeBits(h.value, 8)
	return true
end

function M.readUint8(s: any, h: any): boolean
	h.value = s:_readBits(8)
	return true
end

function M.writeUint16(s: any, h: any): boolean
	if s:_latched() then
		return false
	end
	if s:_available() < 16 then
		return s:_fail(OVERFLOW)
	end
	s:_writeBits(h.value, 16)
	return true
end

function M.readUint16(s: any, h: any): boolean
	h.value = s:_readBits(16)
	return true
end

function M.writeUint32(s: any, h: any): boolean
	if s:_latched() then
		return false
	end
	if s:_available() < 32 then
		return s:_fail(OVERFLOW)
	end
	s:_writeBits(h.value, 32)
	return true
end

function M.readUint32(s: any, h: any): boolean
	h.value = s:_readBits(32)
	return true
end

function M.writeUint64(s: any, h: any): boolean
	if s:_latched() then
		return false
	end
	if s:_available() < 64 then
		return s:_fail(OVERFLOW)
	end
	s:_writeBits64(h.value, 64)
	return true
end

function M.readUint64(s: any, h: any): boolean
	h.value = s:_readBits64(64)
	return true
end

-- ---------------------------------------------------------------------------
-- The other exports (DESIGN.md (h)): bitsRequired, bitsRequired64 and the
-- zig-zag helpers (serialize.go serialize.go BitsRequired, BitsRequired64,
-- SignedToUnsigned, UnsignedToSigned).

-- bitsRequired(min, max) is the bits the range [min,max] takes (STANDARD.md
-- int (ranged)): 0 when the range is degenerate, else the bit length of
-- max - min.
function M.bitsRequired(min: number, max: number): number
	local diff = max - min
	if diff == 0 then
		return 0
	end
	return 32 - bit32.countlz(diff)
end

-- bitsRequired64(min, max) is the unsigned 64-bit bit length of max - min.
function M.bitsRequired64(min: integer, max: integer): number
	local diff = integer.sub(max, min)
	if diff == 0i then
		return 0
	end
	local hi = integer.rshift(diff, 32i)
	local function u32(x: integer): number
		local n = integer.tonumber(x)
		if n < 0 then
			return n + 4294967296
		end
		return n
	end
	if hi ~= 0i then
		return 32 + (32 - bit32.countlz(u32(hi)))
	end
	return 32 - bit32.countlz(u32(integer.band(diff, 0xffffffffi)))
end

-- signedToUnsigned / unsignedToSigned are the zig-zag pair (serialize.go
-- SignedToUnsigned, UnsignedToSigned), in the int32 domain.
function M.signedToUnsigned(n: number): number
	local u = ((n < 0) and (n + 4294967296) or n)
	return bit32.bxor(bit32.lshift(u, 1), 0 - bit32.rshift(u, 31))
end

function M.unsignedToSigned(n: number): number
	local raw = bit32.bxor(bit32.rshift(n, 1), bit32.band(0 - bit32.band(n, 1), 0xffffffff))
	if raw >= 0x80000000 then
		return raw - 4294967296
	end
	return raw
end

return table.freeze(M)
