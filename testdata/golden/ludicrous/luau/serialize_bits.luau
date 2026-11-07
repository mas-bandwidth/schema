--!strict
-- serialize_bits.luau — the bits family (DESIGN.md (a), (b), (c), (g) and
-- (h)): serializeBits, serializeBits64, bool and the fixed-width unsigned
-- helpers uint8/16/32/64, plus bitsRequired, bitsRequired64 and the zig-zag
-- helpers, wire compatible with serialize (STANDARD.md, format version 2.0)
-- and serialize.go v2.0.0 write_stream.go, read_stream.go and measure_stream.go.
--
-- The writer is STRICT: it emits exactly serialize.go's bytes. Write misuse
-- (bits outside their range, a value too wide for its bits, a write that does
-- not fit the room left) traps on write and measure, naming the rule, writes
-- nothing for that call and latches nothing (IMPLEMENTING.md U9, U6); on read
-- the same misuse refuses ValueOutOfRange (IMPLEMENTING.md U7). Reads refuse
-- where serialize.go refuses; a refusal leaves the caller's destination
-- untouched and the stream terminal (STANDARD.md, Reader Obligations).
--
-- The number model (DESIGN.md (c)): a value of 32 bits or fewer is a number,
-- a 64-bit value an integer (the native `integer` type), and the holder
-- convention (DESIGN.md (a)) is `{ value = ... }`.
--
-- Copyright © 2016 - 2026, Más Bandwidth LLC. BSD 3-Clause (see LICENSE).

-- The refusal reason the stream latches (DESIGN.md (a); STANDARD.md, Refusal reasons).
local PAST_END = "past_end"

-- The trap for an operation that does not fit the room left in a write
-- buffer (IMPLEMENTING.md U9, U10): write misuse, never a latched past_end.
local RAISE_ROOM = "serialize: the write does not fit the room left in the buffer"

-- Raise messages for API misuse: serialize.go's panic strings (serialize.go
-- v2.0.0 serialize.go; DESIGN.md (g)), and the value-width rule of
-- IMPLEMENTING.md U8 (this port is always checked, so a value too wide for
-- its bits is caught on write, never masked).
local RAISE_BITS_RANGE = "serialize: bits must be in [1,32]"
local RAISE_BITS_RANGE64 = "serialize: bits must be in [1,64]"
local RAISE_VALUE_BITS = "serialize: serializeBits value must fit in bits (an integer in [0,2^bits))"
local RAISE_VALUE_BITS64 = "serialize: serializeBits64 value must fit in bits"
local RAISE_INTEGER_BITS64 = "serialize: serializeBits64 value must be an integer"
local RAISE_INTEGER_UINT64 = "serialize: serializeUint64 value must be an integer"
local RAISE_UINT8 = "serialize: serializeUint8 value must be an integer in [0,2^8)"
local RAISE_UINT16 = "serialize: serializeUint16 value must be an integer in [0,2^16)"
local RAISE_UINT32 = "serialize: serializeUint32 value must be an integer in [0,2^32)"

-- POW2[n] is 2^n for n in [1,32], the exclusive top of an n-bit value.
local POW2: { number } = {}
for n = 1, 32 do
	POW2[n] = 2 ^ n
end

-- fits is true when value is an integer number in [0, top) (U8).
local function fits(value: any, top: number): boolean
	return type(value) == "number" and value >= 0 and value < top and value // 1 == value
end

local M = {}

-- ---------------------------------------------------------------------------
-- The dispatched arms: serializeBits (DISPATCH, DESIGN.md (h)), the whole
-- operation in serialize.go's order — misuse first, refuse on a latched
-- stream, the bounds check of the whole width, then the bits (serialize.go
-- write_stream.go SerializeBits / read_stream.go SerializeBits / measure_stream.go
-- SerializeBits). The bit count is a declaration: an integer in its range, a
-- fraction or a NaN included (IMPLEMENTING.md U16). Misuse raises on write and
-- measure and refuses ValueOutOfRange on read (s:_misuse, U7).

function M.writeBits(s: any, h: any, bits: number): boolean
	if type(bits) ~= "number" or not (bits >= 1 and bits <= 32) or bits // 1 ~= bits then
		return s:_misuse(RAISE_BITS_RANGE)
	end
	local value = h.value
	if not fits(value, POW2[bits]) then
		return s:_misuse(RAISE_VALUE_BITS)
	end
	if s:_latched() then
		return false
	end
	if s:_available() < bits then
		return s:_misuse(RAISE_ROOM)
	end
	s:_writeBits(value, bits)
	return true
end

function M.readBits(s: any, h: any, bits: number): boolean
	if type(bits) ~= "number" or not (bits >= 1 and bits <= 32) or bits // 1 ~= bits then
		return s:_misuse(RAISE_BITS_RANGE)
	end
	if s:_latched() then
		return false
	end
	if s:_available() < bits then
		return s:_fail(PAST_END)
	end
	h.value = s:_readBits(bits)
	return true
end

-- A measure makes every check a write makes, except room, by the same trap
-- (IMPLEMENTING.md U6, U9): the value's width included.
function M.measureBits(s: any, h: any, bits: number): boolean
	if type(bits) ~= "number" or not (bits >= 1 and bits <= 32) or bits // 1 ~= bits then
		return s:_misuse(RAISE_BITS_RANGE)
	end
	if not fits(h.value, POW2[bits]) then
		return s:_misuse(RAISE_VALUE_BITS)
	end
	return s:_measure(bits)
end

-- serializeBits64: the same, in [1,64]; past 32 bits the low 32 bits first,
-- then the high bits - 32 (serialize.go write_stream.go SerializeBits64,
-- STANDARD.md bits). The value is an integer (DESIGN.md (c)).
function M.writeBits64(s: any, h: any, bits: number): boolean
	if type(bits) ~= "number" or not (bits >= 1 and bits <= 64) or bits // 1 ~= bits then
		return s:_misuse(RAISE_BITS_RANGE64)
	end
	local value = h.value
	if typeof(value) ~= "integer" then
		return s:_misuse(RAISE_INTEGER_BITS64)
	end
	if bits < 64 and integer.rshift(value, integer.create(bits)) ~= 0i then
		return s:_misuse(RAISE_VALUE_BITS64)
	end
	if s:_latched() then
		return false
	end
	if s:_available() < bits then
		return s:_misuse(RAISE_ROOM)
	end
	s:_writeBits64(value, bits)
	return true
end

function M.readBits64(s: any, h: any, bits: number): boolean
	if type(bits) ~= "number" or not (bits >= 1 and bits <= 64) or bits // 1 ~= bits then
		return s:_misuse(RAISE_BITS_RANGE64)
	end
	if s:_latched() then
		return false
	end
	if s:_available() < bits then
		return s:_fail(PAST_END)
	end
	h.value = s:_readBits64(bits)
	return true
end

function M.measureBits64(s: any, h: any, bits: number): boolean
	if type(bits) ~= "number" or not (bits >= 1 and bits <= 64) or bits // 1 ~= bits then
		return s:_misuse(RAISE_BITS_RANGE64)
	end
	local value = h.value
	if typeof(value) ~= "integer" then
		return s:_misuse(RAISE_INTEGER_BITS64)
	end
	if bits < 64 and integer.rshift(value, integer.create(bits)) ~= 0i then
		return s:_misuse(RAISE_VALUE_BITS64)
	end
	return s:_measure(bits)
end

-- ---------------------------------------------------------------------------
-- The fixed-width write arms (FIXED, DESIGN.md (h)): the write arm is the
-- family's, whole — the value's width (U8), refuse on a latched stream, the
-- bounds check of the whole width, then the bits. The read arms are reached
-- only after the stream's read gate has refused on a latched stream and on
-- running past the end, so they read exactly the width and assign the holder
-- last.

function M.writeBool(s: any, h: any): boolean
	if s:_latched() then
		return false
	end
	if s:_available() < 1 then
		return s:_misuse(RAISE_ROOM)
	end
	s:_writeBits(h.value and 1 or 0, 1)
	return true
end

function M.readBool(s: any, h: any): boolean
	h.value = s:_readBits(1) ~= 0
	return true
end

function M.writeUint8(s: any, h: any): boolean
	if not fits(h.value, 256) then
		return s:_misuse(RAISE_UINT8)
	end
	if s:_latched() then
		return false
	end
	if s:_available() < 8 then
		return s:_misuse(RAISE_ROOM)
	end
	s:_writeBits(h.value, 8)
	return true
end

function M.readUint8(s: any, h: any): boolean
	h.value = s:_readBits(8)
	return true
end

function M.writeUint16(s: any, h: any): boolean
	if not fits(h.value, 65536) then
		return s:_misuse(RAISE_UINT16)
	end
	if s:_latched() then
		return false
	end
	if s:_available() < 16 then
		return s:_misuse(RAISE_ROOM)
	end
	s:_writeBits(h.value, 16)
	return true
end

function M.readUint16(s: any, h: any): boolean
	h.value = s:_readBits(16)
	return true
end

function M.writeUint32(s: any, h: any): boolean
	if not fits(h.value, 4294967296) then
		return s:_misuse(RAISE_UINT32)
	end
	if s:_latched() then
		return false
	end
	if s:_available() < 32 then
		return s:_misuse(RAISE_ROOM)
	end
	s:_writeBits(h.value, 32)
	return true
end

function M.readUint32(s: any, h: any): boolean
	h.value = s:_readBits(32)
	return true
end

-- writeUint64 takes the bit pattern as an integer (DESIGN.md (c)); a number
-- raises serialize's own text, never the integer library's (audit 2026-10-27).
function M.writeUint64(s: any, h: any): boolean
	if typeof(h.value) ~= "integer" then
		return s:_misuse(RAISE_INTEGER_UINT64)
	end
	if s:_latched() then
		return false
	end
	if s:_available() < 64 then
		return s:_misuse(RAISE_ROOM)
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
