--!strict
-- serialize_float.luau — float and double, and the float32 narrowing (DESIGN.md
-- (e) and (h); STANDARD.md, Floating Point, "float", "double" and "Finite
-- values only, every one bit exact").
--
-- serializeFloat writes the 32 bits of the IEEE-754 single-precision
-- representation as one 32-bit group; serializeDouble writes the 64 bits of
-- the IEEE-754 double as one 64-bit group, low 32 bits first
-- (STANDARD.md, double). Both carry FINITE values only: a pattern whose
-- exponent field is all ones (NaN with any payload, either infinity) is
-- refused as NonFiniteFloat on read, tested on the RAW BITS before any
-- conversion and before the destination is written. On write and measure the
-- same test, after the narrowing to float32 (a finite double above FLT_MAX
-- narrows to an infinity), is write misuse and traps, never latched
-- (IMPLEMENTING.md U9, U6). Every finite pattern, negative zero and the
-- subnormals included, is reproduced bit exact.
--
-- The float32 holder is a Luau double (DESIGN.md (e)). A finite float32 widens
-- to a double exactly, so the read goes through buffer.readf32; a write
-- narrows through buffer.writef32, Go's float32(v): round to nearest, ties to
-- even, overflow to the signed infinity.
--
-- Copyright © 2016 - 2026, Más Bandwidth LLC. BSD 3-Clause (see LICENSE).

local Stream = require("./serialize_stream")

local RAISE_FLOAT_VALUE = "serialize: serializeFloat value must be a number"
local RAISE_DOUBLE_VALUE = "serialize: serializeDouble value must be a number"

-- The write-misuse traps (IMPLEMENTING.md U9, U6): raised, never latched.
local RAISE_NONFINITE = "serialize: float and double values must be finite (after narrowing to float32 for a float)"
local RAISE_ROOM = "serialize: the write does not fit the room left in the buffer"

-- The exponent fields: all ones is NaN or an infinity (STANDARD.md, "Finite
-- values only"). EXP64 is the field within the HIGH 32-bit half of a double.
local EXP32 = 0x7f800000
local EXP64 = 0x7ff00000

-- floatBitsAreFinite is true unless the float32 pattern's exponent field is
-- all ones (STANDARD.md, "Finite values only, every one bit exact").
local function floatBitsAreFinite(u: number): boolean
	return bit32.band(u, EXP32) ~= EXP32
end

-- doubleBitsAreFinite is the same test on the high half of a double's
-- pattern, the half that holds the whole exponent field.
local function doubleBitsAreFinite(hi: number): boolean
	return bit32.band(hi, EXP64) ~= EXP64
end

-- doubleBits splits a double into the low and high 32-bit halves of its
-- IEEE-754 pattern, in the wire's order (STANDARD.md, double).
local function doubleBits(x: number): (number, number)
	local b = buffer.create(8)
	buffer.writef64(b, 0, x)
	return buffer.readu32(b, 0), buffer.readu32(b, 4)
end

-- doubleFromBits assembles the low and high halves into the double with that
-- pattern (STANDARD.md, double).
local function doubleFromBits(lo: number, hi: number): number
	local b = buffer.create(8)
	buffer.writeu32(b, 0, lo)
	buffer.writeu32(b, 4, hi)
	return buffer.readf64(b, 0)
end

-- f32frombits is the double holding the float32 with pattern u. The caller
-- has refused a non-finite u, so the widening is exact.
local function f32frombits(u: number): number
	local b = buffer.create(4)
	buffer.writeu32(b, 0, u)
	return buffer.readf32(b, 0)
end

-- f32bits is the float32 pattern of x narrowed as Go's float32(v) narrows it
-- (DESIGN.md (e)): round to nearest, ties to even, overflow to the signed
-- infinity.
local function f32bits(x: number): number
	local b = buffer.create(4)
	buffer.writef32(b, 0, x)
	return buffer.readu32(b, 0)
end

-- fround is Go's float32(v) as a double (DESIGN.md (e)): writef32 then
-- readf32.
local function fround(x: number): number
	local b = buffer.create(4)
	buffer.writef32(b, 0, x)
	return buffer.readf32(b, 0)
end

local F = {}

-- writeFloat writes holder.value as the 32 bits of its IEEE-754 single
-- pattern, one 32-bit group (STANDARD.md, float; DESIGN.md (e)). The write is
-- strict: misuse traps (IMPLEMENTING.md U9), a value that is not finite AFTER
-- the narrowing and a write that does not fit the room left included, and
-- nothing latches. A measure runs this arm over its counting primitives (U6).
-- A double with more precision than a float32 is narrowed, never refused.
function F.writeFloat(s: any, h: any): boolean
	local value = h.value
	if type(value) ~= "number" then
		error(RAISE_FLOAT_VALUE, 2)
	end
	if s:_latched() then
		return false
	end
	local u = f32bits(value)
	if not floatBitsAreFinite(u) then
		return s:_misuse(RAISE_NONFINITE)
	end
	if s:_available() < 32 then
		return s:_misuse(RAISE_ROOM)
	end
	s:_writeBits(u, 32)
	return true
end

-- readFloat reads the 32-bit group, refuses a non-finite pattern with
-- NonFiniteFloat on its raw bits, and only then stores the value; the stream
-- has already refused a latched stream and a read past the end (STANDARD.md,
-- float and "Finite values only, every one bit exact").
function F.readFloat(s: any, h: any): boolean
	local bits = s:_readBits(32)
	if not floatBitsAreFinite(bits) then
		return s:_fail(Stream.SerializeError.NonFiniteFloat)
	end
	h.value = f32frombits(bits)
	return true
end

-- writeDouble writes holder.value as the 64 bits of its IEEE-754 double
-- pattern, the low 32-bit group first, then the high one (STANDARD.md,
-- double). A value that is not finite traps (IMPLEMENTING.md U9), and the
-- whole 64-bit width is checked before any bit moves.
function F.writeDouble(s: any, h: any): boolean
	local value = h.value
	if type(value) ~= "number" then
		error(RAISE_DOUBLE_VALUE, 2)
	end
	if s:_latched() then
		return false
	end
	local lo, hi = doubleBits(value)
	if not doubleBitsAreFinite(hi) then
		return s:_misuse(RAISE_NONFINITE)
	end
	if s:_available() < 64 then
		return s:_misuse(RAISE_ROOM)
	end
	s:_writeBits(lo, 32)
	s:_writeBits(hi, 32)
	return true
end

-- readDouble reads the low 32-bit group then the high one, refuses a
-- non-finite pattern with NonFiniteFloat on its raw bits, and only then stores
-- the double with exactly those bits (STANDARD.md, double and "Finite values
-- only, every one bit exact").
function F.readDouble(s: any, h: any): boolean
	local lo = s:_readBits(32)
	local high = s:_readBits(32)
	if not doubleBitsAreFinite(high) then
		return s:_fail(Stream.SerializeError.NonFiniteFloat)
	end
	h.value = doubleFromBits(lo, high)
	return true
end

return table.freeze({
	fround = fround,
	f32bits = f32bits,
	f32frombits = f32frombits,
	floatBitsAreFinite = floatBitsAreFinite,
	writeFloat = F.writeFloat,
	readFloat = F.readFloat,
	writeDouble = F.writeDouble,
	readDouble = F.readDouble,
})
