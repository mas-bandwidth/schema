--!strict
-- serialize_wide.moo -- the 64-bit and 128-bit integers of serialize.moo
-- (DESIGN.md, "The number model").
--
-- A moo number is Q48.16 fixed point: it holds every integer up to 2^47 - 1
-- exactly and no wider one, and moo has no integer type. So a 64-bit or
-- 128-bit wire value is a WIDE value: a frozen table of 32-bit limbs, least
-- significant first, at [1]..[n] (n = 2 or 4), each limb a number in
-- [0, 2^32). The four classes are the same limbs read two ways:
--
--   Uint64, Int64     two limbs, read unsigned / two's complement signed
--   Uint128, Int128   four limbs, read unsigned / two's complement signed
--
-- Arithmetic wraps modulo 2^64 or 2^128 exactly like serialize.go's uint64,
-- int64, Uint128 and Int128 (int128.go): shifts of the width or more give zero
-- (all sign bits for a signed right shift), division by zero returns zero
-- quotient and remainder, and signed division truncates toward zero with the
-- remainder taking the dividend's sign. Every limb operation stays below 2^47,
-- so no moo number ever saturates here.
--
-- Copyright © 2016 - 2026, Más Bandwidth LLC. BSD 3-Clause (see LICENSE).

local TWO32 = 4294967296
local MASK32 = 4294967295
local TWO16 = 65536
local MAX_EXACT = 140737488355327 -- 2^47 - 1, the largest integer a moo number holds exactly

local W = {}

-- ---------------------------------------------------------------------------
-- Limb arithmetic on plain arrays of n limbs. Every function returns a fresh
-- array and never touches its arguments.

local function zero(n: number): { number }
	local r = table.create(n, 0)
	return r
end

local function isLimb(x: any): boolean
	return type(x) == "number" and x >= 0 and x <= MASK32 and x == math.floor(x)
end

local function add(a: { number }, b: { number }, n: number): { number }
	local r = table.create(n, 0)
	local carry = 0
	for i = 1, n do
		local s = a[i] + b[i] + carry
		if s >= TWO32 then
			r[i] = s - TWO32
			carry = 1
		else
			r[i] = s
			carry = 0
		end
	end
	return r
end

local function sub(a: { number }, b: { number }, n: number): { number }
	local r = table.create(n, 0)
	local borrow = 0
	for i = 1, n do
		local d = a[i] - b[i] - borrow
		if d < 0 then
			r[i] = d + TWO32
			borrow = 1
		else
			r[i] = d
			borrow = 0
		end
	end
	return r
end

local function neg(a: { number }, n: number): { number }
	return sub(zero(n), a, n)
end

local function isZero(a: { number }, n: number): boolean
	for i = 1, n do
		if a[i] ~= 0 then
			return false
		end
	end
	return true
end

local function isNegative(a: { number }, n: number): boolean
	return a[n] >= 2147483648
end

-- cmpU compares a and b as unsigned: -1, 0 or 1.
local function cmpU(a: { number }, b: { number }, n: number): number
	for i = n, 1, -1 do
		if a[i] < b[i] then
			return -1
		elseif a[i] > b[i] then
			return 1
		end
	end
	return 0
end

-- cmpS compares a and b as two's complement signed: -1, 0 or 1.
local function cmpS(a: { number }, b: { number }, n: number): number
	local na, nb = isNegative(a, n), isNegative(b, n)
	if na ~= nb then
		return if na then -1 else 1
	end
	return cmpU(a, b, n)
end

-- bitLen is the unsigned bit length: floor(log2(a)) + 1, and 0 for zero.
local function bitLen(a: { number }, n: number): number
	for i = n, 1, -1 do
		if a[i] ~= 0 then
			return (i - 1) * 32 + (32 - bit32.countlz(a[i]))
		end
	end
	return 0
end

-- shl is a logical left shift; counts of 32n or more give zero.
local function shl(a: { number }, k: number, n: number): { number }
	local r = table.create(n, 0)
	if k >= 32 * n then
		return r
	end
	local q = k // 32
	local s = k % 32
	for i = n, q + 1, -1 do
		local v = a[i - q]
		if s == 0 then
			r[i] = v
		else
			local lowPart = if i - q - 1 >= 1 then bit32.rshift(a[i - q - 1], 32 - s) else 0
			r[i] = bit32.bor(bit32.lshift(v, s), lowPart)
		end
	end
	return r
end

-- shr is a right shift, logical when fill is 0 and arithmetic when fill is
-- MASK32 (the sign limb); counts of 32n or more give the fill.
local function shr(a: { number }, k: number, n: number, fill: number): { number }
	local r = table.create(n, fill)
	if k >= 32 * n then
		return r
	end
	local q = k // 32
	local s = k % 32
	local function limb(i: number): number
		if i <= n then
			return a[i]
		end
		return fill
	end
	for i = 1, n - q do
		local v = limb(i + q)
		if s == 0 then
			r[i] = v
		else
			r[i] = bit32.bor(bit32.rshift(v, s), bit32.lshift(limb(i + q + 1), 32 - s))
		end
	end
	return r
end

local function bitwise(a: { number }, b: { number }, n: number, op: (number, number) -> number): { number }
	local r = table.create(n, 0)
	for i = 1, n do
		r[i] = op(a[i], b[i])
	end
	return r
end

local function bnot(a: { number }, n: number): { number }
	local r = table.create(n, 0)
	for i = 1, n do
		r[i] = MASK32 - a[i]
	end
	return r
end

-- mul is the product modulo 2^(32n), schoolbook over 16-bit halves so every
-- partial product is below 2^32 and every column sum (at most 8 of them)
-- below 2^35.
local function mul(a: { number }, b: { number }, n: number): { number }
	local h = 2 * n
	local ah = table.create(h, 0)
	local bh = table.create(h, 0)
	for i = 1, n do
		ah[2 * i - 1] = a[i] % TWO16
		ah[2 * i] = a[i] // TWO16
		bh[2 * i - 1] = b[i] % TWO16
		bh[2 * i] = b[i] // TWO16
	end
	local col = table.create(h, 0)
	for i = 1, h do
		local ai = ah[i]
		if ai ~= 0 then
			for j = 1, h - i + 1 do
				col[i + j - 1] += ai * bh[j]
			end
		end
	end
	local carry = 0
	for i = 1, h do
		local v = col[i] + carry
		col[i] = v % TWO16
		carry = v // TWO16
	end
	local r = table.create(n, 0)
	for i = 1, n do
		r[i] = col[2 * i - 1] + col[2 * i] * TWO16
	end
	return r
end

-- divModU is unsigned shift-subtract long division; a zero divisor gives zero
-- quotient and zero remainder (serialize.go int128.go DivMod).
local function divModU(a: { number }, b: { number }, n: number): ({ number }, { number })
	if isZero(b, n) then
		return zero(n), zero(n)
	end
	local q = zero(n)
	local r = zero(n)
	for i = bitLen(a, n) - 1, 0, -1 do
		r = shl(r, 1, n)
		local bit = bit32.extract(a[i // 32 + 1], i % 32, 1)
		r[1] = bit32.bor(r[1], bit)
		if cmpU(r, b, n) >= 0 then
			r = sub(r, b, n)
			local li = i // 32 + 1
			q[li] = bit32.bor(q[li], bit32.lshift(1, i % 32))
		end
	end
	return q, r
end

-- divModS is signed division with C++ semantics: truncation toward zero, the
-- remainder taking the dividend's sign; MIN / -1 wraps to MIN, remainder 0.
local function divModS(a: { number }, b: { number }, n: number): ({ number }, { number })
	local an, bn = isNegative(a, n), isNegative(b, n)
	local am = if an then neg(a, n) else a
	local bm = if bn then neg(b, n) else b
	local q, r = divModU(am, bm, n)
	if an ~= bn then
		q = neg(q, n)
	end
	if an then
		r = neg(r, n)
	end
	return q, r
end

-- divSmall divides a by a small positive integer d (d <= 2^15), returning the
-- quotient and the remainder as a number.
local function divSmall(a: { number }, d: number, n: number): ({ number }, number)
	local q = table.create(n, 0)
	local rem = 0
	for i = n, 1, -1 do
		local hi = rem * TWO16 + a[i] // TWO16
		local qh = hi // d
		rem = hi % d
		local lo = rem * TWO16 + a[i] % TWO16
		local ql = lo // d
		rem = lo % d
		q[i] = qh * TWO16 + ql
	end
	return q, rem
end

-- mulSmallAdd returns a * m + c modulo 2^(32n), m and c small (<= 2^15).
local function mulSmallAdd(a: { number }, m: number, c: number, n: number): { number }
	local r = table.create(n, 0)
	local carry = c
	for i = 1, n do
		local lo = (a[i] % TWO16) * m + carry
		local loDigit = lo % TWO16
		local hi = (a[i] // TWO16) * m + lo // TWO16
		r[i] = (hi % TWO16) * TWO16 + loDigit
		carry = hi // TWO16
	end
	return r
end

-- fromNumber: an integer-valued moo number (every one is within 2^47) as n limbs, two's complement.
local function fromNumber(x: number, n: number): { number }
	local m = if x < 0 then -x else x
	local r = table.create(n, 0)
	r[1] = m % TWO32
	r[2] = m // TWO32
	if x < 0 then
		return neg(r, n)
	end
	return r
end

-- toNumber: the limbs as a moo number, or nil when the value is not an integer
-- of at most 2^47 - 1 in magnitude (signed reads two's complement).
local function toNumber(a: { number }, n: number, signed: boolean): number?
	local negative = signed and isNegative(a, n)
	local m = if negative then neg(a, n) else a
	for i = 3, n do
		if m[i] ~= 0 then
			return nil
		end
	end
	if m[2] >= 32768 then
		return nil
	end
	local v = m[2] * TWO32 + m[1]
	return if negative then -v else v
end

local function toDecimal(a: { number }, n: number, signed: boolean): string
	local negative = signed and isNegative(a, n)
	local m = if negative then neg(a, n) else a
	if isZero(m, n) then
		return "0"
	end
	local digits = {}
	while not isZero(m, n) do
		local r
		m, r = divSmall(m, 10000, n)
		table.insert(digits, 1, r)
	end
	local parts = { tostring(digits[1]) }
	for i = 2, #digits do
		parts[i] = string.format("%04d", digits[i])
	end
	return (if negative then "-" else "") .. table.concat(parts)
end

-- parse reads optional '-', then decimal digits or 0x and hex digits, modulo
-- 2^(32n). Returns nil on any other text.
local function parse(text: string, n: number): { number }?
	local negative = false
	local body = text
	if string.sub(body, 1, 1) == "-" then
		negative = true
		body = string.sub(body, 2)
	end
	local base = 10
	if string.sub(body, 1, 2) == "0x" or string.sub(body, 1, 2) == "0X" then
		base = 16
		body = string.sub(body, 3)
	end
	if #body == 0 then
		return nil
	end
	local acc = zero(n)
	for i = 1, #body do
		local c = string.byte(body, i) :: number
		local d: number
		if c >= 48 and c <= 57 then
			d = c - 48
		elseif base == 16 and c >= 97 and c <= 102 then
			d = c - 87
		elseif base == 16 and c >= 65 and c <= 70 then
			d = c - 55
		else
			return nil
		end
		acc = mulSmallAdd(acc, base, d, n)
	end
	if negative then
		acc = neg(acc, n)
	end
	return acc
end

W.add = add
W.sub = sub
W.neg = neg
W.isZero = isZero
W.isNegative = isNegative
W.cmpU = cmpU
W.cmpS = cmpS
W.bitLen = bitLen
W.shl = shl
W.shr = shr
W.mul = mul
W.divModU = divModU
W.divModS = divModS
W.fromNumber = fromNumber
W.toNumber = toNumber
W.isLimb = isLimb
W.TWO32 = TWO32
W.MASK32 = MASK32
W.MAX_EXACT = MAX_EXACT

-- ---------------------------------------------------------------------------
-- The four classes. A value is a frozen table of n limbs at [1]..[n] with its
-- class as metatable. Methods take wide values of the same class, or an
-- integer-valued moo number, which converts.

local classes: { [any]: boolean } = {}

local function makeClass(name: string, n: number, signed: boolean): any
	local C: any = {}
	local meta: any = { __index = C }

	local function wrap(limbs: { number }): any
		return table.freeze(setmetatable(limbs, meta))
	end

	local function limbsOf(x: any, what: string): { number }
		if type(x) == "number" then
			if x ~= math.floor(x) then
				error("serialize: " .. what .. " must be " .. name .. " or an integer-valued number", 3)
			end
			return fromNumber(x, n)
		end
		if getmetatable(x) ~= meta then
			error("serialize: " .. what .. " must be " .. name .. " or an integer-valued number", 3)
		end
		return x
	end

	C.name = name
	C.limbs = n
	C.signed = signed
	C._wrap = wrap
	C._limbsOf = limbsOf

	-- new(l1, ..., ln): the limbs, least significant first, each in [0, 2^32).
	function C.new(...: number): any
		local args = { ... }
		if #args ~= n then
			error("serialize: " .. name .. ".new takes " .. n .. " limbs, least significant first", 2)
		end
		local limbs = table.create(n, 0)
		for i = 1, n do
			if not isLimb(args[i]) then
				error("serialize: " .. name .. ".new limb " .. i .. " must be an integer in [0, 2^32)", 2)
			end
			limbs[i] = args[i]
		end
		return wrap(limbs)
	end

	-- fromNumber(x): an integer-valued moo number, two's complement for negatives.
	function C.fromNumber(x: number): any
		if type(x) ~= "number" or x ~= math.floor(x) then
			error("serialize: " .. name .. ".fromNumber requires an integer-valued number", 2)
		end
		return wrap(fromNumber(x, n))
	end

	-- parse(text): decimal or 0x hexadecimal, optional '-', modulo the width.
	function C.parse(text: string): any
		local limbs = parse(text, n)
		if limbs == nil then
			error("serialize: " .. name .. ".parse cannot read " .. string.format("%q", text), 2)
		end
		return wrap(limbs)
	end

	function C.is(x: any): boolean
		return getmetatable(x) == meta
	end

	function C.toNumber(x: any): number
		local v = toNumber(limbsOf(x, name .. ".toNumber"), n, signed)
		if v == nil then
			error("serialize: " .. name .. " " .. toDecimal(x, n, signed) .. " is not an integer moo can hold exactly (|x| <= 2^47 - 1)", 2)
		end
		return v
	end

	-- tryNumber(x) is toNumber without the raise: nil when it does not fit.
	function C.tryNumber(x: any): number?
		return toNumber(limbsOf(x, name .. ".tryNumber"), n, signed)
	end

	function C.toString(x: any): string
		return toDecimal(limbsOf(x, name .. ".toString"), n, signed)
	end

	function C.isZero(x: any): boolean
		return isZero(limbsOf(x, name .. ".isZero"), n)
	end

	function C.isNegative(x: any): boolean
		return signed and isNegative(limbsOf(x, name .. ".isNegative"), n)
	end

	function C.cmp(x: any, y: any): number
		local a, b = limbsOf(x, name .. ".cmp"), limbsOf(y, name .. ".cmp")
		return if signed then cmpS(a, b, n) else cmpU(a, b, n)
	end

	function C.add(x: any, y: any): any
		return wrap(add(limbsOf(x, name .. ".add"), limbsOf(y, name .. ".add"), n))
	end

	function C.sub(x: any, y: any): any
		return wrap(sub(limbsOf(x, name .. ".sub"), limbsOf(y, name .. ".sub"), n))
	end

	function C.mul(x: any, y: any): any
		return wrap(mul(limbsOf(x, name .. ".mul"), limbsOf(y, name .. ".mul"), n))
	end

	function C.divMod(x: any, y: any): (any, any)
		local a, b = limbsOf(x, name .. ".divMod"), limbsOf(y, name .. ".divMod")
		local q, r
		if signed then
			q, r = divModS(a, b, n)
		else
			q, r = divModU(a, b, n)
		end
		return wrap(q), wrap(r)
	end

	function C.div(x: any, y: any): any
		local q, _ = C.divMod(x, y)
		return q
	end

	function C.mod(x: any, y: any): any
		local _, r = C.divMod(x, y)
		return r
	end

	function C.neg(x: any): any
		return wrap(neg(limbsOf(x, name .. ".neg"), n))
	end

	function C.bnot(x: any): any
		return wrap(bnot(limbsOf(x, name .. ".bnot"), n))
	end

	function C.band(x: any, y: any): any
		return wrap(bitwise(limbsOf(x, name .. ".band"), limbsOf(y, name .. ".band"), n, bit32.band))
	end

	function C.bor(x: any, y: any): any
		return wrap(bitwise(limbsOf(x, name .. ".bor"), limbsOf(y, name .. ".bor"), n, bit32.bor))
	end

	function C.bxor(x: any, y: any): any
		return wrap(bitwise(limbsOf(x, name .. ".bxor"), limbsOf(y, name .. ".bxor"), n, bit32.bxor))
	end

	local function shiftCount(k: any, what: string): number
		if type(k) ~= "number" or k < 0 or k ~= math.floor(k) then
			error("serialize: " .. name .. "." .. what .. " count must be a non-negative integer", 3)
		end
		return k
	end

	function C.lsh(x: any, k: number): any
		return wrap(shl(limbsOf(x, name .. ".lsh"), shiftCount(k, "lsh"), n))
	end

	-- rsh is arithmetic for the signed classes and logical for the unsigned.
	function C.rsh(x: any, k: number): any
		local a = limbsOf(x, name .. ".rsh")
		local fill = if signed and isNegative(a, n) then MASK32 else 0
		return wrap(shr(a, shiftCount(k, "rsh"), n, fill))
	end

	-- len is the bit length of the bit pattern read unsigned (Go's Uint128.Len).
	function C.len(x: any): number
		return bitLen(limbsOf(x, name .. ".len"), n)
	end

	meta.__eq = function(x: any, y: any): boolean
		if getmetatable(x) ~= getmetatable(y) then
			return false
		end
		return cmpU(x, y, n) == 0
	end
	meta.__lt = function(x: any, y: any): boolean
		return C.cmp(x, y) < 0
	end
	meta.__le = function(x: any, y: any): boolean
		return C.cmp(x, y) <= 0
	end
	meta.__add = C.add
	meta.__sub = C.sub
	meta.__mul = C.mul
	meta.__unm = C.neg
	meta.__tostring = C.toString

	classes[C] = true
	return C
end

W.Uint64 = makeClass("Uint64", 2, false)
W.Int64 = makeClass("Int64", 2, true)
W.Uint128 = makeClass("Uint128", 4, false)
W.Int128 = makeClass("Int128", 4, true)

-- Reinterpretations between the classes of one width, and widening /
-- narrowing between the widths (serialize.go's Int128From64, Uint128From64,
-- Int128.Int64, Uint128.Uint64, Int128.Uint128, Uint128.Int128).
local function reinterpret(to: any, x: any, from: any): any
	local a: { number } = from._limbsOf(x, to.name)
	local limbs = table.create(#a, 0)
	for i = 1, #a do
		limbs[i] = a[i]
	end
	return to._wrap(limbs)
end

function W.Int64.toUint64(x: any): any
	return reinterpret(W.Uint64, x, W.Int64)
end
function W.Uint64.toInt64(x: any): any
	return reinterpret(W.Int64, x, W.Uint64)
end
function W.Int128.toUint128(x: any): any
	return reinterpret(W.Uint128, x, W.Int128)
end
function W.Uint128.toInt128(x: any): any
	return reinterpret(W.Int128, x, W.Uint128)
end

-- Int128.from64(x): sign-extends an Int64 (or number); Uint128.from64(x):
-- zero-extends a Uint64 (or number).
function W.Int128.from64(x: any): any
	local a = W.Int64._limbsOf(x, "Int128.from64 value")
	local fill = if isNegative(a, 2) then MASK32 else 0
	return W.Int128._wrap({ a[1], a[2], fill, fill })
end
function W.Uint128.from64(x: any): any
	local a = W.Uint64._limbsOf(x, "Uint128.from64 value")
	return W.Uint128._wrap({ a[1], a[2], 0, 0 })
end
-- toInt64 / toUint64 truncate to the low 64 bits, like a native narrowing.
function W.Int128.toInt64(x: any): any
	local a = W.Int128._limbsOf(x, "Int128.toInt64 value")
	return W.Int64._wrap({ a[1], a[2] })
end
function W.Uint128.toUint64(x: any): any
	local a = W.Uint128._limbsOf(x, "Uint128.toUint64 value")
	return W.Uint64._wrap({ a[1], a[2] })
end

-- fromNumberRaw / toNumberRaw: a moo number and its own Q48.16 raw bits as an
-- Int64 (value * 2^16, exact). toNumberRaw refuses the one raw value moo has
-- no number for, -2^63 (moo's range is [-(2^63 - 1), 2^63 - 1] raw), with nil.
function W.Int64.fromNumberRaw(x: number): any
	if type(x) ~= "number" then
		error("serialize: Int64.fromNumberRaw requires a number", 2)
	end
	-- work on the magnitude: -x is always a moo number, and floor(|x|) <= 2^47 - 1
	local m = if x < 0 then -x else x
	local whole = math.floor(m)
	local frac = (m - whole) * TWO16 -- exact: m - whole is k / 2^16, k in [0, 2^16)
	local limbs = shl(fromNumber(whole, 2), 16, 2)
	limbs[1] += frac
	if x < 0 then
		limbs = neg(limbs, 2)
	end
	return W.Int64._wrap(limbs)
end

function W.Int64.toNumberRaw(x: any): number?
	local a = W.Int64._limbsOf(x, "Int64.toNumberRaw value")
	if a[2] == 2147483648 and a[1] == 0 then
		return nil
	end
	local frac = a[1] % TWO16
	local whole = shr(a, 16, 2, if isNegative(a, 2) then MASK32 else 0)
	local w = toNumber(whole, 2, true)
	if w == nil then
		-- whole is -2^47, which moo cannot hold; the value itself, -2^47 plus
		-- a positive fraction, it can
		return -MAX_EXACT + (frac / TWO16 - 1)
	end
	return w + frac / TWO16
end

return table.freeze(W)
