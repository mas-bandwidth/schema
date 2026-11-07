--!strict
-- serialize_fixed.luau — fixed64 and fixed128 (DESIGN.md (a), (b), (c), (g)
-- and (h); STANDARD.md "fixed (Q format, ranged)"): a fixed point value held
-- in an integer storage type of exactly integer_bits + fraction_bits bits,
-- the stored integer the real value scaled by 2^fraction_bits, min and max
-- whole-unit bounds. The encoding is an offset encoding over the RAW (scaled)
-- bounds, byte identical to serializeInt64 / serializeInt128 of the raw value
-- over the raw bounds (STANDARD.md fixed; serialize.go fixedPointParams64/128
-- and SerializeFixed64/128, v2.0.0).
--
-- This card lands without serialize_bits / serialize_int / serialize_int128
-- (they are not yet on the base branch), so the helpers this family needs —
-- bitsRequired64 and the Uint128 pair arithmetic — are implemented here,
-- inline, rather than required from those modules. The behavior is identical
-- to the design's dependency structure; the bytes are the serialize.go bytes.
--
-- Copyright © 2016 - 2026, Más Bandwidth LLC. BSD 3-Clause (see LICENSE).

-- THE INTEGER GATE (DESIGN.md (c)): 64-bit values are Luau's native integer.
-- A Luau without the integer type refuses here, at require time.
if typeof(integer) ~= "table" then
	error("serialize: this Luau has no integer type (typeof(integer) is not table); serialize.luau needs Luau 0.741 or later with the integer library (DESIGN.md (c))")
end

-- The latched refusal reasons (DESIGN.md (a); STANDARD.md, Refusal reasons),
-- as the stream spells them.
local VALUE_OUT_OF_RANGE = "value_out_of_range"
local PAST_END = "past_end"

-- The write-misuse traps (IMPLEMENTING.md U9, U6): raised, never latched.
local RAISE_RANGE = "serialize: fixed point value must be in [min,max] whole units"
local RAISE_ROOM = "serialize: the write does not fit the room left in the buffer"

-- Misuse raises: serialize.go's panic text, raised naming the rule (serialize.go
-- serialize.go panicFixedParams, panicFixedBounds; DESIGN.md (g)).
local RAISE_PARAMS = "serialize: fixed point requires integer bits >= 1, fraction bits >= 0, a Q format that fits the storage width, and min <= max"
local RAISE_BOUNDS = "serialize: fixed point bounds in whole units do not fit the Q format"

-- ---------------------------------------------------------------------------
-- The Uint128 pair and the integer helpers this family needs (DESIGN.md (c);
-- serialize.go int128.go): { lo, hi } of integer bit patterns, two's
-- complement halves, unsigned 128-bit semantics, wrapping modulo 2^128.
type U128 = { lo: integer, hi: integer }

local MIN_INT64 = integer.lshift(1i, 63i)
local MAX_INT64 = 0x7fffffffffffffffi

-- bitLen64 is the bit length of an unsigned 64-bit pattern: floor(log2(v))+1,
-- and zero for zero (serialize.go bits.Len64).
local function bitLen64(v: integer): number
	local n = 0
	while v ~= 0i do
		v = integer.rshift(v, 1i)
		n += 1
	end
	return n
end

-- bitsRequired64 is the bit count of the range [min,max] in the unsigned
-- 64-bit domain: zero when min == max, else the bit length of max - min
-- (STANDARD.md int64; serialize.go BitsRequired64).
local function bitsRequired64(min: integer, max: integer): number
	if min == max then
		return 0
	end
	return bitLen64(integer.sub(max, min))
end

-- from64 turns an int64 bit pattern into the unsigned 128-bit pair, sign
-- extending (serialize.go Int128From64).
local function from64(x: integer): U128
	return { lo = x, hi = if integer.lt(x, 0i) then -1i else 0i }
end

-- lshU shifts an unsigned 128-bit pair left by n bits, wrapping (serialize.go
-- Uint128.Lsh).
local function lshU(u: U128, n: number): U128
	if n == 0 then
		return u
	end
	if n < 64 then
		local lo = integer.lshift(u.lo, integer.create(n))
		local hi = integer.bor(integer.lshift(u.hi, integer.create(n)), integer.rshift(u.lo, integer.create(64 - n)))
		return { lo = lo, hi = hi }
	end
	if n < 128 then
		return { lo = 0i, hi = integer.lshift(u.lo, integer.create(n - 64)) }
	end
	return { lo = 0i, hi = 0i }
end

-- subU subtracts two unsigned 128-bit pairs, wrapping (serialize.go Uint128.Sub).
local function subU(x: U128, y: U128): U128
	local lo = integer.sub(x.lo, y.lo)
	local borrow = if integer.ult(x.lo, y.lo) then 1i else 0i
	local hi = integer.sub(integer.sub(x.hi, y.hi), borrow)
	return { lo = lo, hi = hi }
end

-- addU adds two unsigned 128-bit pairs, wrapping (serialize.go Uint128.Add).
local function addU(x: U128, y: U128): U128
	local lo = integer.add(x.lo, y.lo)
	local carry = if integer.ult(lo, x.lo) then 1i else 0i
	local hi = integer.add(integer.add(x.hi, y.hi), carry)
	return { lo = lo, hi = hi }
end

-- cmpU compares two unsigned 128-bit pairs, -1/0/+1 (serialize.go Uint128.Cmp).
local function cmpU(x: U128, y: U128): number
	if x.hi ~= y.hi then
		return if integer.ult(x.hi, y.hi) then -1 else 1
	end
	if x.lo ~= y.lo then
		return if integer.ult(x.lo, y.lo) then -1 else 1
	end
	return 0
end

-- ---------------------------------------------------------------------------
-- The capacity check (serialize.go fixedPointCapacity): the whole-unit bounds
-- a Q format with integerBits bits can represent, clamped to the int64 domain.
-- Signedness follows the bounds: signed when min < 0, with the sign bit
-- counting toward integerBits.
local function capacity(integerBits: number, signed: boolean): { integer }
	if signed then
		if integerBits >= 65 then
			return { MIN_INT64, MAX_INT64 }
		end
		local minRep = integer.sub(0i, integer.lshift(1i, integer.create(integerBits - 1)))
		if integerBits >= 64 then
			return { minRep, MAX_INT64 }
		end
		return { minRep, integer.sub(integer.lshift(1i, integer.create(integerBits - 1)), 1i) }
	end
	if integerBits >= 63 then
		return { 0i, MAX_INT64 }
	end
	return { 0i, integer.sub(integer.lshift(1i, integer.create(integerBits)), 1i) }
end

-- misuse is the rule a fixed point declaration breaks, or nil (STANDARD.md
-- fixed; serialize.go fixedPointParams64 and fixedPointParams128;
-- IMPLEMENTING.md U16): integerBits and fractionBits integers, integerBits at
-- least 1, fractionBits at least 0, the storage width (at most 64, or exactly
-- 128 when wide), min and max integers with min <= max, and the bounds inside
-- the Q format's capacity. The arm raises it on write and measure and refuses
-- ValueOutOfRange on read (U7), before any bit moves.
local function misuse(integerBits: any, fractionBits: any, min: any, max: any, wide: boolean): string?
	if type(integerBits) ~= "number" or type(fractionBits) ~= "number" or integerBits // 1 ~= integerBits or fractionBits // 1 ~= fractionBits then
		return RAISE_PARAMS
	end
	if typeof(min) ~= "integer" or typeof(max) ~= "integer" then
		return RAISE_PARAMS
	end
	local storage = integerBits + fractionBits
	if integerBits < 1 or fractionBits < 0 or integer.lt(max, min) then
		return RAISE_PARAMS
	end
	if (wide and storage ~= 128) or (not wide and storage > 64) then
		return RAISE_PARAMS
	end
	local c = capacity(integerBits, integer.lt(min, 0i))
	if integer.lt(min, c[1]) or integer.lt(c[2], max) then
		return RAISE_BOUNDS
	end
	return nil
end

-- params64 derives a validated Q format of storage 64 bits or fewer
-- (STANDARD.md fixed; serialize.go fixedPointParams64): rawMin, rawRange (raw
-- bounds, shifted left by fractionBits, in the unsigned domain) and the bit
-- count of the raw range. The arm has applied misuse first.
local function params64(integerBits: number, fractionBits: number, min: integer, max: integer): (integer, integer, number)
	local rawMin = integer.lshift(min, integer.create(fractionBits))
	local rawMax = integer.lshift(max, integer.create(fractionBits))
	local rawRange = integer.sub(rawMax, rawMin)
	return rawMin, rawRange, bitsRequired64(rawMin, rawMax)
end

-- params128 derives the validated wide Q format, storage exactly 128 bits
-- (STANDARD.md fixed; serialize.go fixedPointParams128). A degenerate range
-- costs zero bits on every storage width; otherwise the bit count is the
-- whole-unit range's bit length plus fractionBits. The arm has applied misuse
-- first.
local function params128(integerBits: number, fractionBits: number, min: integer, max: integer): (U128, U128, number)
	local rawMin = lshU(from64(min), fractionBits)
	local rawMax = lshU(from64(max), fractionBits)
	local rawRange = subU(rawMax, rawMin)
	local numBits
	if min == max then
		numBits = 0
	else
		numBits = bitsRequired64(min, max) + fractionBits
	end
	return rawMin, rawRange, numBits
end

-- ---------------------------------------------------------------------------
-- The dispatched arms (DESIGN.md (h)): writeFixed64 readFixed64 measureFixed64
-- writeFixed128 readFixed128 measureFixed128. Each does the whole operation in
-- serialize.go's order: raise on API misuse, refuse on a latched stream, then
-- the refusals and the bits; a read assigns its holder last.

-- serializeFixed64 writes a raw fixed point value in h.value, an integer, which
-- must be within [min,max] whole units, as an offset from the raw (scaled)
-- minimum in the bits required for the raw range (STANDARD.md fixed; serialize.go
-- SerializeFixed64). The wire is byte identical to SerializeInt64 over the raw
-- bounds.
local function writeFixed64(self: any, h: { value: integer }, integerBits: number, fractionBits: number, min: integer, max: integer): boolean
	local bad = misuse(integerBits, fractionBits, min, max, false)
	if bad ~= nil then
		return self:_misuse(bad)
	end
	local rawMin, rawRange, numBits = params64(integerBits, fractionBits, min, max)
	if self:_latched() then
		return false
	end
	local offset = integer.sub(h.value, rawMin)
	if integer.ugt(offset, rawRange) then
		return self:_misuse(RAISE_RANGE)
	end
	if self:_available() < numBits then
		return self:_misuse(RAISE_ROOM)
	end
	if numBits > 0 then
		self:_writeBits64(offset, numBits)
	end
	return true
end

local function readFixed64(self: any, h: { value: integer }, integerBits: number, fractionBits: number, min: integer, max: integer): boolean
	local bad = misuse(integerBits, fractionBits, min, max, false)
	if bad ~= nil then
		return self:_misuse(bad)
	end
	local rawMin, rawRange, numBits = params64(integerBits, fractionBits, min, max)
	if self:_latched() then
		return false
	end
	if self:_available() < numBits then
		return self:_fail(PAST_END)
	end
	local offset = 0i
	if numBits > 0 then
		offset = self:_readBits64(numBits)
	end
	if integer.ugt(offset, rawRange) then
		return self:_fail(VALUE_OUT_OF_RANGE)
	end
	h.value = integer.add(rawMin, offset)
	return true
end

local function measureFixed64(self: any, h: { value: integer }, integerBits: number, fractionBits: number, min: integer, max: integer): boolean
	local bad = misuse(integerBits, fractionBits, min, max, false)
	if bad ~= nil then
		return self:_misuse(bad)
	end
	local rawMin, rawRange, numBits = params64(integerBits, fractionBits, min, max)
	if self:_latched() then
		return false
	end
	local offset = integer.sub(h.value, rawMin)
	if integer.ugt(offset, rawRange) then
		return self:_misuse(RAISE_RANGE)
	end
	return self:_measure(numBits)
end

-- serializeFixed128 writes a raw wide fixed point value in h.value, an Int128
-- pair, which must be within [min,max] whole units (STANDARD.md fixed; serialize.go
-- SerializeFixed128). integerBits plus fractionBits must equal 128.
local function writeFixed128(self: any, h: { value: U128 }, integerBits: number, fractionBits: number, min: integer, max: integer): boolean
	local bad = misuse(integerBits, fractionBits, min, max, true)
	if bad ~= nil then
		return self:_misuse(bad)
	end
	local rawMin, rawRange, numBits = params128(integerBits, fractionBits, min, max)
	if self:_latched() then
		return false
	end
	local offset = subU({ lo = h.value.lo, hi = h.value.hi }, rawMin)
	if cmpU(offset, rawRange) > 0 then
		return self:_misuse(RAISE_RANGE)
	end
	if self:_available() < numBits then
		return self:_misuse(RAISE_ROOM)
	end
	if numBits > 0 then
		if numBits <= 64 then
			self:_writeBits64(offset.lo, numBits)
		else
			self:_writeBits64(offset.lo, 64)
			self:_writeBits64(offset.hi, numBits - 64)
		end
	end
	return true
end

local function readFixed128(self: any, h: { value: U128 }, integerBits: number, fractionBits: number, min: integer, max: integer): boolean
	local bad = misuse(integerBits, fractionBits, min, max, true)
	if bad ~= nil then
		return self:_misuse(bad)
	end
	local rawMin, rawRange, numBits = params128(integerBits, fractionBits, min, max)
	if self:_latched() then
		return false
	end
	if self:_available() < numBits then
		return self:_fail(PAST_END)
	end
	local lo = 0i
	local hi = 0i
	if numBits > 0 then
		if numBits <= 64 then
			lo = self:_readBits64(numBits)
		else
			lo = self:_readBits64(64)
			hi = self:_readBits64(numBits - 64)
		end
	end
	local offset = { lo = lo, hi = hi }
	if cmpU(offset, rawRange) > 0 then
		return self:_fail(VALUE_OUT_OF_RANGE)
	end
	local v = addU(rawMin, offset)
	h.value = { lo = v.lo, hi = v.hi }
	return true
end

local function measureFixed128(self: any, h: { value: U128 }, integerBits: number, fractionBits: number, min: integer, max: integer): boolean
	local bad = misuse(integerBits, fractionBits, min, max, true)
	if bad ~= nil then
		return self:_misuse(bad)
	end
	local rawMin, rawRange, numBits = params128(integerBits, fractionBits, min, max)
	if self:_latched() then
		return false
	end
	local offset = subU({ lo = h.value.lo, hi = h.value.hi }, rawMin)
	if cmpU(offset, rawRange) > 0 then
		return self:_misuse(RAISE_RANGE)
	end
	return self:_measure(numBits)
end

return table.freeze({
	writeFixed64 = writeFixed64,
	readFixed64 = readFixed64,
	measureFixed64 = measureFixed64,
	writeFixed128 = writeFixed128,
	readFixed128 = readFixed128,
	measureFixed128 = measureFixed128,
})
