--!strict
-- serialize_fixed.luau — fixed64 and fixed128 (DESIGN.md (a), (b), (c), (g)
-- and (h); STANDARD.md "fixed (Q format, ranged)"): a fixed point value held
-- in an integer storage type of exactly integer_bits + fraction_bits bits,
-- the stored integer the real value scaled by 2^fraction_bits, min and max
-- whole-unit bounds. The encoding is an offset encoding over the RAW (scaled)
-- bounds, byte identical to serializeInt64 / serializeInt128 of the raw value
-- over the raw bounds (STANDARD.md fixed; mas-bandwidth/serialize (C++)
-- fixedPointParams64/128 and SerializeFixed64/128, v2.1.4).
--
-- The helpers this family needs — bitsRequired64 and the Uint128 pair
-- arithmetic — come from serialize_bits and serialize_int128, so the design's
-- dependency structure has one implementation of each; the bytes are the
-- reference's bytes.
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
-- The shared helpers (DESIGN.md (c)): bitsRequired64 from serialize_bits, and
-- the Uint128 pair arithmetic from serialize_int128, whose { lo, hi } of
-- integer bit patterns are the two's-complement halves with unsigned 128-bit
-- wrapping semantics (serialize.go int128.go).
local Bits = require("./serialize_bits")
local Int128 = require("./serialize_int128")
local Uint128: any = Int128.Uint128
local bitsRequired64 = Bits.bitsRequired64

type U128 = { lo: integer, hi: integer }

local MIN_INT64 = integer.lshift(1i, 63i)
local MAX_INT64 = 0x7fffffffffffffffi

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
	local rawMin = Uint128.lsh(Uint128.from64(min), fractionBits)
	local rawMax = Uint128.lsh(Uint128.from64(max), fractionBits)
	local rawRange = Uint128.sub(rawMax, rawMin)
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
	local offset = Uint128.sub({ lo = h.value.lo, hi = h.value.hi }, rawMin)
	if Uint128.cmp(offset, rawRange) > 0 then
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
	if Uint128.cmp(offset, rawRange) > 0 then
		return self:_fail(VALUE_OUT_OF_RANGE)
	end
	local v = Uint128.add(rawMin, offset)
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
	local offset = Uint128.sub({ lo = h.value.lo, hi = h.value.hi }, rawMin)
	if Uint128.cmp(offset, rawRange) > 0 then
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
