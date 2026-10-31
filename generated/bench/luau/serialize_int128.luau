--!strict
-- serialize_int128.luau — the ranged 128-bit integer (STANDARD.md, int128
-- (ranged); DESIGN.md (h)) and the two-lane Int128/Uint128 helpers that fixed
-- and the type wire reuse.
--
-- Int128 and Uint128 are the same pair { lo, hi } of 64-bit integers, the
-- two's-complement halves in the wire's order (DESIGN.md (c)). Every
-- operation is a function on the class table, never an operator. Arithmetic
-- wraps modulo 2^128; the ranged operations convert the bounds to the
-- unsigned domain before subtracting, so a range wider than 2^127 is exact
-- (STANDARD.md, int128 (ranged)).
--
-- Copyright © 2016 - 2026, Más Bandwidth LLC. BSD 3-Clause (see LICENSE).

local RAISE_MINMAX = "serialize: min must not be greater than max"
local RAISE_INT128 = "serialize: serializeInt128 parameters must be Int128 pairs"

local PAST_END = "past_end"
local OUT_OF_RANGE = "value_out_of_range"

-- The write-misuse traps (IMPLEMENTING.md U9, U6): raised, never latched.
local RAISE_RANGE = "serialize: serializeInt128 value must be in [min,max]"
local RAISE_ROOM = "serialize: the write does not fit the room left in the buffer"

local MASK32 = 0xffffffffi
local ZERO = 0i
local ONE = 1i

local Int128 = {}
Int128.__index = Int128

local Uint128 = {}
Uint128.__index = Uint128

local function new(lo: integer, hi: integer): any
	return { lo = lo, hi = hi }
end

local function isPair(x: any): boolean
	return type(x) == "table" and typeof(x.lo) == "integer" and typeof(x.hi) == "integer"
end

local function add(a: any, b: any): any
	local lo = integer.add(a.lo, b.lo)
	local carry = if integer.ult(lo, a.lo) then ONE else ZERO
	local hi = integer.add(integer.add(a.hi, b.hi), carry)
	return new(lo, hi)
end

local function sub(a: any, b: any): any
	local lo = integer.sub(a.lo, b.lo)
	local borrow = if integer.ult(a.lo, b.lo) then ONE else ZERO
	local hi = integer.sub(integer.sub(a.hi, b.hi), borrow)
	return new(lo, hi)
end

local function cmpUnsigned(a: any, b: any): number
	if a.hi ~= b.hi then
		return if integer.ult(a.hi, b.hi) then -1 else 1
	end
	if a.lo ~= b.lo then
		return if integer.ult(a.lo, b.lo) then -1 else 1
	end
	return 0
end

local function cmpSigned(a: any, b: any): number
	if a.hi ~= b.hi then
		return if integer.lt(a.hi, b.hi) then -1 else 1
	end
	if a.lo ~= b.lo then
		return if integer.ult(a.lo, b.lo) then -1 else 1
	end
	return 0
end

local function len(a: any): number
	if a.hi ~= ZERO then
		return 64 + (64 - integer.tonumber(integer.countlz(a.hi)))
	end
	return 64 - integer.tonumber(integer.countlz(a.lo))
end

local function lsh(a: any, n: number): any
	if n <= 0 then
		return new(a.lo, a.hi)
	end
	if n >= 128 then
		return new(ZERO, ZERO)
	end
	if n < 64 then
		local ni = integer.create(n)
		local lo = integer.lshift(a.lo, ni)
		local hi = integer.bor(integer.lshift(a.hi, ni), integer.rshift(a.lo, integer.create(64 - n)))
		return new(lo, hi)
	end
	return new(ZERO, integer.lshift(a.lo, integer.create(n - 64)))
end

local function rsh(a: any, n: number): any
	if n <= 0 then
		return new(a.lo, a.hi)
	end
	if n >= 128 then
		return new(ZERO, ZERO)
	end
	if n < 64 then
		local ni = integer.create(n)
		local lo = integer.bor(integer.rshift(a.lo, ni), integer.lshift(a.hi, integer.create(64 - n)))
		return new(lo, integer.rshift(a.hi, ni))
	end
	return new(integer.rshift(a.hi, integer.create(n - 64)), ZERO)
end

local function band(a: any, b: any): any
	return new(integer.band(a.lo, b.lo), integer.band(a.hi, b.hi))
end

local function bor(a: any, b: any): any
	return new(integer.bor(a.lo, b.lo), integer.bor(a.hi, b.hi))
end

local function bxor(a: any, b: any): any
	return new(integer.bxor(a.lo, b.lo), integer.bxor(a.hi, b.hi))
end

local function bnot(a: any): any
	return new(integer.bnot(a.lo), integer.bnot(a.hi))
end

local function neg(a: any): any
	return sub(new(ZERO, ZERO), a)
end

-- mul is the low 128 bits of the product, by 32-bit cross terms so no 64x64
-- intermediate overflows the integer type.
local function mul(a: any, b: any): any
	local a0 = integer.band(a.lo, MASK32)
	local a1 = integer.rshift(a.lo, 32i)
	local b0 = integer.band(b.lo, MASK32)
	local b1 = integer.rshift(b.lo, 32i)
	local p0 = integer.mul(a0, b0)
	local p1 = integer.mul(a0, b1)
	local p2 = integer.mul(a1, b0)
	local p3 = integer.mul(a1, b1)
	local s = integer.add(p1, p2)
	local scarry = if integer.ult(s, p1) then ONE else ZERO
	local lo = integer.add(p0, integer.lshift(s, 32i))
	local c0 = if integer.ult(lo, p0) then ONE else ZERO
	local hi = p3
	hi = integer.add(hi, integer.rshift(s, 32i))
	hi = integer.add(hi, integer.lshift(scarry, 32i))
	hi = integer.add(hi, c0)
	hi = integer.add(hi, integer.mul(a.lo, b.hi))
	hi = integer.add(hi, integer.mul(a.hi, b.lo))
	return new(lo, hi)
end

local function divMod(a: any, b: any): (any, any)
	if a.hi == ZERO and a.lo == ZERO then
		return new(ZERO, ZERO), new(ZERO, ZERO)
	end
	if b.hi == ZERO and b.lo == ZERO then
		return new(ZERO, ZERO), new(ZERO, ZERO)
	end
	local quotient = new(ZERO, ZERO)
	local remainder = new(ZERO, ZERO)
	local n = len(a)
	for i = n - 1, 0, -1 do
		remainder = lsh(remainder, 1)
		local bit = integer.band(rsh(a, i).lo, ONE)
		remainder = new(integer.bor(remainder.lo, bit), remainder.hi)
		if cmpUnsigned(remainder, b) >= 0 then
			remainder = sub(remainder, b)
			quotient = bor(quotient, lsh(new(ONE, ZERO), i))
		end
	end
	return quotient, remainder
end

-- The shared constructors and operations. Int128 compares signed, Uint128
-- compares unsigned; every other operation is on the bit pattern.

local function from64(x: integer): any
	local hi = if integer.lt(x, ZERO) then 0xffffffffffffffffi else ZERO
	return new(x, hi)
end

local function attach(cls: any, compare: (any, any) -> number)
	cls.new = new
	cls.from64 = from64
	cls.int64 = function(x: any): integer
		return x.lo
	end
	cls.uint64 = function(x: any): integer
		return x.lo
	end
	cls.int128 = function(x: any): any
		return new(x.lo, x.hi)
	end
	cls.uint128 = function(x: any): any
		return new(x.lo, x.hi)
	end
	cls.isZero = function(x: any): boolean
		return x.lo == ZERO and x.hi == ZERO
	end
	cls.isNegative = function(x: any): boolean
		return integer.lt(x.hi, ZERO)
	end
	cls.len = len
	cls.cmp = compare
	cls.add = add
	cls.sub = sub
	cls.mul = mul
	cls.divMod = divMod
	cls.div = function(a: any, b: any): any
		local q = divMod(a, b)
		return q
	end
	cls.mod = function(a: any, b: any): any
		local _, r = divMod(a, b)
		return r
	end
	cls.neg = neg
	cls.bnot = bnot
	cls.band = band
	cls.bor = bor
	cls.bxor = bxor
	cls.lsh = lsh
	cls.rsh = rsh
end

attach(Int128, cmpSigned)
attach(Uint128, cmpUnsigned)

-- bitsRequired128 is serialize.go BitsRequired128: the span in the unsigned
-- domain, so ranges wider than 2^127 are exact (STANDARD.md, int128).
local function bitsRequired128(min: any, max: any): number
	if min.lo == max.lo and min.hi == max.hi then
		return 0
	end
	return len(sub(max, min))
end

local function writeGroups(s: any, offset: any, bits: number)
	if bits <= 64 then
		s:_writeBits64(offset.lo, bits)
		return
	end
	s:_writeBits64(offset.lo, 64)
	s:_writeBits64(offset.hi, bits - 64)
end

local function readGroups(s: any, bits: number): any
	if bits <= 64 then
		return new(s:_readBits64(bits), ZERO)
	end
	local lo = s:_readBits64(64)
	local hi = s:_readBits64(bits - 64)
	return new(lo, hi)
end

-- writeInt128: STANDARD.md, int128 (ranged). The offset value - min is
-- computed in the unsigned domain, low half first, then the high remainder.
local function writeInt128(s: any, holder: any, min: any, max: any): boolean
	if not isPair(min) or not isPair(max) or not isPair(holder.value) then
		return s:_misuse(RAISE_INT128)
	end
	if cmpSigned(min, max) > 0 then
		return s:_misuse(RAISE_MINMAX)
	end
	if s:_latched() then
		return false
	end
	local value = holder.value
	if cmpSigned(value, min) < 0 or cmpSigned(value, max) > 0 then
		return s:_misuse(RAISE_RANGE)
	end
	local bits = bitsRequired128(min, max)
	if s._kind == "measure" then
		return s:_measure(bits)
	end
	if s:_available() < bits then
		return s:_misuse(RAISE_ROOM)
	end
	if bits > 0 then
		writeGroups(s, sub(value, min), bits)
	end
	return true
end

local function readInt128(s: any, holder: any, min: any, max: any): boolean
	if not isPair(min) or not isPair(max) then
		return s:_misuse(RAISE_INT128)
	end
	if cmpSigned(min, max) > 0 then
		return s:_misuse(RAISE_MINMAX)
	end
	if s:_latched() then
		return false
	end
	local bits = bitsRequired128(min, max)
	if s:_available() < bits then
		return s:_fail(PAST_END)
	end
	if bits == 0 then
		holder.value = new(min.lo, min.hi)
		return true
	end
	local offset = readGroups(s, bits)
	if cmpUnsigned(offset, sub(max, min)) > 0 then
		return s:_fail(OUT_OF_RANGE)
	end
	holder.value = add(offset, min)
	return true
end

local function measureInt128(s: any, holder: any, min: any, max: any): boolean
	if not isPair(min) or not isPair(max) or not isPair(holder.value) then
		return s:_misuse(RAISE_INT128)
	end
	if cmpSigned(min, max) > 0 then
		return s:_misuse(RAISE_MINMAX)
	end
	if s:_latched() then
		return false
	end
	if cmpSigned(holder.value, min) < 0 or cmpSigned(holder.value, max) > 0 then
		return s:_misuse(RAISE_RANGE)
	end
	return s:_measure(bitsRequired128(min, max))
end

-- The unsigned 128-bit arms (STANDARD.md, uint128; serialize.go
-- SerializeUint128): all 128 bits, low half first, no range.
local function writeUint128(s: any, holder: any): boolean
	if not isPair(holder.value) then
		return s:_misuse(RAISE_INT128)
	end
	if s:_latched() then
		return false
	end
	if s:_available() < 128 then
		return s:_misuse(RAISE_ROOM)
	end
	s:_writeBits64(holder.value.lo, 64)
	s:_writeBits64(holder.value.hi, 64)
	return true
end

-- readUint128 assigns its holder whatever it held before, an empty {}
-- included: a read's holder value is an output (audit 2026-10-27).
local function readUint128(s: any, holder: any): boolean
	if s:_latched() then
		return false
	end
	if s:_available() < 128 then
		return s:_fail(PAST_END)
	end
	local lo = s:_readBits64(64)
	local hi = s:_readBits64(64)
	holder.value = new(lo, hi)
	return true
end

return table.freeze({
	Int128 = table.freeze(Int128),
	Uint128 = table.freeze(Uint128),
	bitsRequired128 = bitsRequired128,
	writeInt128 = writeInt128,
	readInt128 = readInt128,
	measureInt128 = measureInt128,
	writeUint128 = writeUint128,
	readUint128 = readUint128,
})
