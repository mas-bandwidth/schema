--!strict
-- serialize_float.luau — float and double, and the float32 narrowing (DESIGN.md
-- (e) and (h); STANDARD.md, Floating Point, "float", "double" and
-- "Bit transparency — both directions").
--
-- serializeFloat writes the 32 bits of the IEEE-754 single-precision
-- representation as one 32-bit group; serializeDouble writes the 64 bits of
-- the IEEE-754 double as one 64-bit group, low 32 bits first
-- (STANDARD.md, double). Both directions are bit transparent: every NaN
-- payload, signalling NaN, infinity, negative zero and denormal is reproduced
-- exactly, and no reader canonicalizes or refuses a pattern (STANDARD.md,
-- "Bit transparency — both directions").
--
-- The float32 holder is a Luau double (DESIGN.md (e)). A read goes from the
-- wire's 32 bits to the double by bit surgery, never through a hardware
-- f32-to-f64 conversion, so a signalling NaN survives. A write of a double
-- that a read produced (or any NaN whose low 29 mantissa bits are zero) moves
-- its bits back by the inverse surgery; every other double narrows through
-- buffer.writef32, Go's float32(v).
--
-- Copyright © 2016 - 2026, Más Bandwidth LLC. BSD 3-Clause (see LICENSE).

local Stream = require("./serialize_stream")

local RAISE_FLOAT_VALUE = "serialize: serializeFloat value must be a number"
local RAISE_DOUBLE_VALUE = "serialize: serializeDouble value must be a number"

local TWO32 = 4294967296
local TWO31 = 2147483648
local TWO29 = 536870912
local TWO23 = 8388608
local EXP32 = 0x7f800000
local EXP64_FIELD = 2047
local EXP32_FIELD = 255

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

-- f32frombits is the read half of float's bit transparency (DESIGN.md (e);
-- STANDARD.md, "Bit transparency — both directions"): sign, rebiased
-- exponent and the 23 mantissa bits shifted left 29, assembled as two u32
-- halves and read back as a double. Subnormals are normalized exactly and a
-- NaN payload is kept without quieting it.
local function f32frombits(u: number): number
	local sign = if u >= TWO31 then 1 else 0
	local exp = math.floor(u / TWO23) % 256
	local mant = u % TWO23
	local expfield: number
	local mant52: number
	if exp == 0 then
		if mant == 0 then
			expfield = 0
			mant52 = 0
		else
			-- Subnormal: normalize mant = 2^k + frac, value = 2^(k-149) * 1.frac.
			local k = 0
			while 2 ^ (k + 1) <= mant do
				k += 1
			end
			expfield = k + 874
			mant52 = (mant - 2 ^ k) * 2 ^ (52 - k)
		end
	elseif exp == EXP32_FIELD then
		expfield = EXP64_FIELD
		mant52 = mant * TWO29
	else
		expfield = exp + 896
		mant52 = mant * TWO29
	end
	local lo = mant52 % TWO32
	local hi = sign * TWO31 + expfield * 1048576 + math.floor(mant52 / TWO32)
	return doubleFromBits(lo, hi)
end

-- f32bits is the write half of float's bit transparency (DESIGN.md (e);
-- STANDARD.md, "Bit transparency — both directions"). A NaN a read produced
-- carries its payload in the top 23 double mantissa bits with the low 29
-- clear: it is narrowed by the inverse surgery, so a signalling NaN and its
-- payload survive. Every other double goes through buffer.writef32, Go's
-- float32(v): round to nearest, ties to even, overflow to the signed
-- infinity, a NaN as Go converts it.
local function f32bits(x: number): number
	local lo, hi = doubleBits(x)
	local exp = math.floor(hi / 1048576) % 2048
	local mant52 = (hi % 1048576) * TWO32 + lo
	if exp == EXP64_FIELD and mant52 ~= 0 and mant52 % TWO29 == 0 then
		local sign = if hi >= TWO31 then 1 else 0
		return sign * TWO31 + EXP32 + mant52 / TWO29
	end
	local b = buffer.create(4)
	buffer.writef32(b, 0, x)
	return buffer.readu32(b, 0)
end

-- fround is Go's float32(v) as a double (DESIGN.md (e)): writef32 then
-- readf32. A NaN is converted as the hardware converts it, as Go's
-- float32(v) does; it is not the bit-transparent path, which only the
-- serializeFloat writer takes.
local function fround(x: number): number
	local b = buffer.create(4)
	buffer.writef32(b, 0, x)
	return buffer.readf32(b, 0)
end

local F = {}

-- writeFloat writes holder.value as the 32 bits of its IEEE-754 single
-- pattern, one 32-bit group (STANDARD.md, float; DESIGN.md (e)). The write is
-- strict: misuse raises, a latched stream or a full buffer refuses without
-- writing, and a double with more precision than a float32 is narrowed, never
-- refused.
function F.writeFloat(s: any, h: any): boolean
	local value = h.value
	if type(value) ~= "number" then
		error(RAISE_FLOAT_VALUE, 2)
	end
	if s:_latched() then
		return false
	end
	if s:_available() < 32 then
		return s:_fail(Stream.SerializeError.Overflow)
	end
	s:_writeBits(f32bits(value), 32)
	return true
end

-- readFloat reads the 32-bit group and stores the bit-transparent double; the
-- stream has already refused a latched stream and a read past the end
-- (STANDARD.md, float and "Bit transparency — both directions").
function F.readFloat(s: any, h: any): boolean
	local u = s:_readBits(32)
	h.value = f32frombits(u)
	return true
end

-- writeDouble writes holder.value as the 64 bits of its IEEE-754 double
-- pattern, the low 32-bit group first, then the high one (STANDARD.md,
-- double). The whole 64-bit width is checked before any bit moves.
function F.writeDouble(s: any, h: any): boolean
	local value = h.value
	if type(value) ~= "number" then
		error(RAISE_DOUBLE_VALUE, 2)
	end
	if s:_latched() then
		return false
	end
	if s:_available() < 64 then
		return s:_fail(Stream.SerializeError.Overflow)
	end
	local lo, hi = doubleBits(value)
	s:_writeBits(lo, 32)
	s:_writeBits(hi, 32)
	return true
end

-- readDouble reads the low 32-bit group then the high one and stores the
-- double with exactly those bits (STANDARD.md, double and "Bit transparency —
-- both directions").
function F.readDouble(s: any, h: any): boolean
	local lo = s:_readBits(32)
	local hi = s:_readBits(32)
	h.value = doubleFromBits(lo, hi)
	return true
end

return table.freeze({
	fround = fround,
	f32bits = f32bits,
	f32frombits = f32frombits,
	writeFloat = F.writeFloat,
	readFloat = F.readFloat,
	writeDouble = F.writeDouble,
	readDouble = F.readDouble,
})
