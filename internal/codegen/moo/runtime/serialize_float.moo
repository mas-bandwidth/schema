--!strict
-- serialize_float.moo -- IEEE floats in a language without them (DESIGN.md,
-- "The float boundary" and "Float32 arithmetic").
--
-- moo has no float and no double: a number is Q48.16 fixed point. The wire
-- still carries IEEE binary32 and binary64, so this file holds the two things
-- that bridge them:
--
-- THE BOUNDARY: the one exact rule between an IEEE value and a moo number.
--   wire -> number: the exact IEEE value x becomes round(x * 2^16) / 2^16,
--     rounding to nearest, ties to even. -0 becomes 0. NaN, +inf, -inf and any
--     x whose rounded raw value is outside [-(2^63 - 1), 2^63 - 1] are REFUSED
--     (there is no moo number for them).
--   number -> wire: the IEEE binary32 / binary64 nearest the number, ties to
--     even. Every moo number is a finite normal float, so this never fails;
--     0 is +0.
--   The rule is moo's own (moo docs/DESIGN.md "Boundary": moo_numfromdouble,
--   moo_numtofloatbits, moo_numtodoublebits), reached through buffer.readf32 /
--   readf64 / writef32 / writef64, so a value read from the wire and the same
--   value passed through moo's C API give the same bits. test/boundary holds it
--   bit for bit against an oracle computed by the C compiler's own float
--   conversions (oracle/boundary_oracle.c).
--
-- FLOAT32 ARITHMETIC: compressed_float is defined in float32 arithmetic
--   (STANDARD.md compressed_float). It is emulated here exactly, on float32 bit
--   patterns held as numbers in [0, 2^32): add, sub, mul and div rounded to
--   nearest even, with subnormals, infinities and a NaN, the conversion from an
--   unsigned integer, floor and ceil, and the ordered comparisons. A decimal
--   reader (Float32.parse) rounds a decimal literal once, as C's strtof does,
--   so a declaration like res = 0.01 means the float32 every other port means.
--   test/softfloat holds every operation against the C compiler's float32
--   arithmetic (oracle/softfloat_oracle.c).
--
-- Copyright © 2016 - 2026, Más Bandwidth LLC. BSD 3-Clause (see LICENSE).

local TWO31 = 2147483648
local TWO23 = 8388608
local TWO24 = 16777216

local POW2: { number } = {}
for i = 0, 46 do
	POW2[i] = 2 ^ i
end

local F = {}

-- ---------------------------------------------------------------------------
-- The boundary, through moo's own buffer conversions.

local scratch = buffer.create(8)

-- toFloatBits(n): the binary32 bits nearest the number n, ties to even.
local function toFloatBits(n: number): number
	buffer.writef32(scratch, 0, n)
	return buffer.readu32(scratch, 0)
end

-- toDoubleBits(n): the binary64 bits nearest n, ties to even, as (lo, hi).
local function toDoubleBits(n: number): (number, number)
	buffer.writef64(scratch, 0, n)
	return buffer.readu32(scratch, 0), buffer.readu32(scratch, 4)
end

-- fromFloatBits(bits): the number for binary32 bits, or nil when the boundary
-- refuses them (NaN, infinite, outside the number range).
local function fromFloatBits(bits: number): number?
	buffer.writeu32(scratch, 0, bits)
	local ok, v = pcall(buffer.readf32, scratch, 0)
	if ok then
		return v
	end
	return nil
end

-- fromDoubleBits(lo, hi): the number for binary64 bits, or nil when refused.
local function fromDoubleBits(lo: number, hi: number): number?
	buffer.writeu32(scratch, 0, lo)
	buffer.writeu32(scratch, 4, hi)
	local ok, v = pcall(buffer.readf64, scratch, 0)
	if ok then
		return v
	end
	return nil
end

F.toFloatBits = toFloatBits
F.toDoubleBits = toDoubleBits
F.fromFloatBits = fromFloatBits
F.fromDoubleBits = fromDoubleBits

-- ---------------------------------------------------------------------------
-- Float32 arithmetic on bit patterns.

local QNAN = 0x7FC00000
local PINF = 0x7F800000
local NINF = 0xFF800000

local function isNaN(b: number): boolean
	return b % TWO31 > PINF
end

local function isInf(b: number): boolean
	return b % TWO31 == PINF
end

local function isFinite(b: number): boolean
	return b % TWO31 < PINF
end

-- unpack(b): sign (0 or 1), the integer significand m and exponent e with the
-- value |x| = m * 2^e, for a finite b. Subnormals have no implicit bit.
local function unpack(b: number): (number, number, number)
	local sign = if b >= TWO31 then 1 else 0
	local mag = b % TWO31
	local biased = mag // TWO23
	local frac = mag % TWO23
	if biased == 0 then
		return sign, frac, -149
	end
	return sign, frac + TWO23, biased - 150
end

local function bitLength(x: number): number
	-- x < 2^47
	local hi = x // 4294967296
	if hi > 0 then
		return 32 + (32 - bit32.countlz(hi))
	end
	return 32 - bit32.countlz(x)
end

-- roundPack(sign, sig, exp): the binary32 nearest sig * 2^exp, ties to even,
-- with sig an integer in [0, 2^47). A caller that dropped nonzero bits below
-- sig's last bit folds them into that last bit (a sticky bit), which is only
-- sound when that bit lies below the rounding position; every caller keeps at
-- least two bits of headroom.
local function roundPack(sign: number, sig: number, exp: number): number
	local signBits = sign * TWO31
	if sig == 0 then
		return signBits
	end
	local L = bitLength(sig)
	local E = L - 1 + exp -- the unbiased exponent of the leading bit
	local shift: number
	if E < -126 then
		shift = -149 - exp -- subnormal: the last bit is 2^-149
	else
		shift = L - 24
	end
	local q: number
	if shift > 0 then
		if shift >= L then
			-- every bit drops: 1 only when sig is above half the last place
			-- (shift == L; a tie goes to even, 0)
			q = if shift == L and sig > POW2[L - 1] then 1 else 0
		else
			q = sig // POW2[shift]
			local r = sig - q * POW2[shift]
			local half = POW2[shift - 1]
			if r > half or (r == half and q % 2 == 1) then
				q += 1
			end
		end
	else
		q = sig * POW2[-shift]
	end
	local lsbExp = exp + shift
	if q >= TWO24 then
		q = q // 2 -- the rounding carried out of the significand; q was 2^24
		lsbExp += 1
	end
	if q >= TWO23 then
		local biased = lsbExp + 150
		if biased >= 255 then
			return signBits + PINF
		end
		return signBits + biased * TWO23 + (q - TWO23)
	end
	return signBits + q -- subnormal or zero (lsbExp is -149)
end

-- alignAdd(ma, ea, mb, eb): |a| >= |b| as integers A and B over one exponent,
-- A + B and A - B exact in a moo number (both below 2^47, the largest moo
-- integer being 2^47 - 1: a sum past it saturates, the audit of 2026-10-27).
local function alignAdd(ma: number, ea: number, mb: number, eb: number): (number, number, number)
	local diff = ea - eb
	if diff <= 22 then
		-- exact: ma * 2^diff < 2^46
		return ma * POW2[diff], mb, eb
	end
	-- a is normal (b's exponent is at least -149, 23 binades below), so
	-- 2^23 <= ma < 2^24: a in units of 2^(ea - 22) is ma * 2^22, in [2^45,
	-- 2^46). b shifted into units of 2^(ea - 21) (drop >= 2), then doubled
	-- with every dropped bit folded into the new last bit (a sticky bit): B <
	-- 2^23 + 2, so A + B < 2^47 and never saturates. An odd B is strictly
	-- between two values, so no tie is invented, and its last bit sits 22
	-- bits below a's last bit; the sum or difference has at least 45 bits, so
	-- roundPack drops at least 21 of them and the sticky bit lies below its
	-- half-way bit.
	local drop = diff - 21
	local kept: number, lost: boolean
	if drop >= 25 then
		kept, lost = 0, mb ~= 0
	else
		kept = mb // POW2[drop]
		lost = kept * POW2[drop] ~= mb
	end
	return ma * POW2[22], kept * 2 + (if lost then 1 else 0), ea - 22
end

-- fadd(a, b): a + b in binary32, rounded to nearest even.
local function fadd(a: number, b: number): number
	if isNaN(a) or isNaN(b) then
		return QNAN
	end
	if isInf(a) or isInf(b) then
		if isInf(a) and isInf(b) and a ~= b then
			return QNAN
		end
		return if isInf(a) then a else b
	end
	local sa, ma, ea = unpack(a)
	local sb, mb, eb = unpack(b)
	if ma == 0 and mb == 0 then
		-- +0 unless both are -0 (round to nearest)
		return if sa == 1 and sb == 1 then TWO31 else 0
	end
	if ea < eb or (ea == eb and ma < mb) then
		sa, ma, ea, sb, mb, eb = sb, mb, eb, sa, ma, ea
	end
	-- |a| >= |b| now
	local A, B, exp = alignAdd(ma, ea, mb, eb)
	if sa == sb then
		return roundPack(sa, A + B, exp)
	end
	local d = A - B
	if d == 0 then
		return 0
	end
	return roundPack(sa, d, exp)
end

local function fsub(a: number, b: number): number
	local nb = if isNaN(b) then b else (if b >= TWO31 then b - TWO31 else b + TWO31)
	return fadd(a, nb)
end

-- fmul(a, b): a * b in binary32, rounded to nearest even.
local function fmul(a: number, b: number): number
	if isNaN(a) or isNaN(b) then
		return QNAN
	end
	local sa, sb = (if a >= TWO31 then 1 else 0), (if b >= TWO31 then 1 else 0)
	local sign = if sa ~= sb then 1 else 0
	if isInf(a) or isInf(b) then
		if a % TWO31 == 0 or b % TWO31 == 0 then
			return QNAN -- inf * 0
		end
		return sign * TWO31 + PINF
	end
	local _, ma, ea = unpack(a)
	local _, mb, eb = unpack(b)
	if ma == 0 or mb == 0 then
		return sign * TWO31
	end
	-- normalize subnormal significands to [2^23, 2^24), so the product P has
	-- 47 or 48 bits and rounding always drops 23 or more of them
	while ma < TWO23 do
		ma *= 2
		ea -= 1
	end
	while mb < TWO23 do
		mb *= 2
		eb -= 1
	end
	-- P = X * 2^12 + Y with X < 2^37 and Y < 2^24; floor(P / 4) < 2^46 is
	-- kept, doubled, and the two dropped bits fold into a sticky last bit
	local ah, al = ma // 4096, ma % 4096
	local bh, bl = mb // 4096, mb % 4096
	local X = ah * bh * 4096 + ah * bl + al * bh
	local Y = al * bl
	local quarter = X * 1024 + Y // 4
	local sig = quarter * 2 + (if Y % 4 ~= 0 then 1 else 0)
	return roundPack(sign, sig, ea + eb + 1)
end

-- fdiv(a, b): a / b in binary32, rounded to nearest even.
local function fdiv(a: number, b: number): number
	if isNaN(a) or isNaN(b) then
		return QNAN
	end
	local sa, sb = (if a >= TWO31 then 1 else 0), (if b >= TWO31 then 1 else 0)
	local sign = if sa ~= sb then 1 else 0
	local aInf, bInf = isInf(a), isInf(b)
	local aZero, bZero = a % TWO31 == 0, b % TWO31 == 0
	if aInf and bInf or aZero and bZero then
		return QNAN
	end
	if aInf or bZero then
		return sign * TWO31 + PINF
	end
	if bInf or aZero then
		return sign * TWO31
	end
	local _, ma, ea = unpack(a)
	local _, mb, eb = unpack(b)
	-- normalize both significands to [2^23, 2^24)
	while ma < TWO23 do
		ma *= 2
		ea -= 1
	end
	while mb < TWO23 do
		mb *= 2
		eb -= 1
	end
	-- 27 quotient bits of ma / mb by restoring division, then a sticky bit
	local q = 0
	local r = ma
	for _ = 1, 27 do
		q *= 2
		if r >= mb then
			r -= mb
			q += 1
		end
		r *= 2
	end
	-- q = floor(ma / mb * 2^26); value = q * 2^(ea - eb - 26) plus the rest
	local sig = q * 2 + (if r ~= 0 then 1 else 0)
	return roundPack(sign, sig, ea - eb - 27)
end

-- fromUint(n): the binary32 nearest the non-negative integer n (< 2^47).
local function fromUint(n: number): number
	return roundPack(0, n, 0)
end

-- floorToInt / ceilToInt: the integer floor or ceiling of a finite binary32
-- whose magnitude is below 2^47.
local function floorToInt(b: number): number
	local s, m, e = unpack(b)
	local v: number
	if e >= 0 then
		v = m * POW2[e]
		return if s == 1 then -v else v
	end
	local k = -e
	local whole = if k > 46 then 0 else m // POW2[k]
	local exact = (if k > 46 then m == 0 else whole * POW2[k] == m)
	if s == 1 then
		return if exact then -whole else -whole - 1
	end
	return whole
end

local function ceilToInt(b: number): number
	local nb = if b >= TWO31 then b - TWO31 else b + TWO31
	return -floorToInt(nb)
end

-- key(b): an ordering key for a non-NaN pattern (+0 and -0 equal).
local function key(b: number): number
	if b >= TWO31 then
		return -(b - TWO31)
	end
	return b
end

local function lt(a: number, b: number): boolean
	return not isNaN(a) and not isNaN(b) and key(a) < key(b)
end

local function le(a: number, b: number): boolean
	return not isNaN(a) and not isNaN(b) and key(a) <= key(b)
end

F.isNaN = isNaN
F.isInf = isInf
F.isFinite = isFinite
F.fadd = fadd
F._unpack = unpack -- the white-box tests (test/softfloat) only
F._alignAdd = alignAdd
F.fsub = fsub
F.fmul = fmul
F.fdiv = fdiv
F.fromUint = fromUint
F.floorToInt = floorToInt
F.ceilToInt = ceilToInt
F.lt = lt
F.le = le
F.QNAN = QNAN
F.PINF = PINF
F.NINF = NINF

-- ---------------------------------------------------------------------------
-- The decimal reader: a decimal literal rounded once to binary32, as strtof.
-- Big integers here are arrays of 16-bit digits, least significant first.

local function bigFromDigits(digits: string): { number }
	local big = { 0 }
	for i = 1, #digits do
		local d = (string.byte(digits, i) :: number) - 48
		local carry = d
		for j = 1, #big do
			local v = big[j] * 10 + carry
			big[j] = v % 65536
			carry = v // 65536
		end
		if carry > 0 then
			table.insert(big, carry)
		end
	end
	return big
end

local function bigMulSmall(a: { number }, m: number): { number }
	local r = {}
	local carry = 0
	for j = 1, #a do
		local v = a[j] * m + carry
		r[j] = v % 65536
		carry = v // 65536
	end
	while carry > 0 do
		table.insert(r, carry % 65536)
		carry = carry // 65536
	end
	return r
end

local function bigTrim(a: { number }): { number }
	while #a > 1 and a[#a] == 0 do
		table.remove(a)
	end
	return a
end

local function bigBitLen(a: { number }): number
	a = bigTrim(a)
	local top = a[#a]
	if top == 0 then
		return 0
	end
	return (#a - 1) * 16 + (32 - bit32.countlz(top))
end

local function bigShl(a: { number }, k: number): { number }
	local r = {}
	local q = k // 16
	local s = k % 16
	for i = 1, q do
		r[i] = 0
	end
	local carry = 0
	for j = 1, #a do
		local v = a[j] * POW2[s] + carry
		r[q + j] = v % 65536
		carry = v // 65536
	end
	if carry > 0 then
		r[q + #a + 1] = carry
	end
	return bigTrim(r)
end

local function bigCmp(a: { number }, b: { number }): number
	a, b = bigTrim(a), bigTrim(b)
	if #a ~= #b then
		return if #a < #b then -1 else 1
	end
	for i = #a, 1, -1 do
		if a[i] ~= b[i] then
			return if a[i] < b[i] then -1 else 1
		end
	end
	return 0
end

local function bigSub(a: { number }, b: { number }): { number }
	local r = {}
	local borrow = 0
	for i = 1, #a do
		local d = a[i] - (b[i] or 0) - borrow
		if d < 0 then
			d += 65536
			borrow = 1
		else
			borrow = 0
		end
		r[i] = d
	end
	return bigTrim(r)
end

-- bigDivSticky(n, d): floor(n / d) as a number (the caller keeps it below
-- 2^47) and whether the remainder is nonzero.
local function bigDivSticky(n: { number }, d: { number }): (number, boolean)
	local q = 0
	local shift = bigBitLen(n) - bigBitLen(d)
	if shift < 0 then
		return 0, bigBitLen(n) > 0
	end
	local r = n
	for i = shift, 0, -1 do
		local ds = bigShl(d, i)
		q *= 2
		if bigCmp(r, ds) >= 0 then
			r = bigSub(r, ds)
			q += 1
		end
	end
	return q, bigBitLen(r) > 0
end

-- parseFloat32(text): the binary32 bits of a decimal literal
-- [+-]digits[.digits][(e|E)[+-]digits], rounded to nearest even, or nil.
local function parseFloat32(text: string): number?
	local mantissaText = text
	local exp10 = 0
	local epos = string.find(text, "[eE]")
	if epos ~= nil then
		mantissaText = string.sub(text, 1, epos - 1)
		local e = string.sub(text, epos + 1)
		if string.match(e, "^[+-]?%d+$") == nil or #e > 6 then
			return nil
		end
		exp10 = tonumber(e) :: number
	end
	local sign, intMatch, fracMatch = string.match(mantissaText, "^([+-]?)(%d*)%.?(%d*)$")
	if sign == nil or intMatch == nil or fracMatch == nil then
		return nil
	end
	local intPart: string, fracPart: string = intMatch, fracMatch
	if #intPart == 0 and #fracPart == 0 then
		return nil
	end
	local signBit = if sign == "-" then 1 else 0
	local digits = intPart .. fracPart
	exp10 -= #fracPart
	digits = string.gsub(digits, "^0+", "")
	if #digits == 0 then
		return signBit * TWO31
	end
	if exp10 + #digits > 60 then
		return signBit * TWO31 + PINF -- far past the largest float32 (about 3.4e38)
	end
	if exp10 + #digits < -60 then
		return signBit * TWO31 -- far below half the least subnormal (about 7e-46)
	end
	local num = bigFromDigits(digits)
	local den = { 1 }
	if exp10 >= 0 then
		for _ = 1, exp10 do
			num = bigMulSmall(num, 10)
		end
	else
		for _ = 1, -exp10 do
			den = bigMulSmall(den, 10)
		end
	end
	-- scale so the quotient has 40 bits: q = floor(num * 2^k / den), value = q * 2^-k
	local k = 40 - (bigBitLen(num) - bigBitLen(den))
	local q, sticky
	if k >= 0 then
		q, sticky = bigDivSticky(bigShl(num, k), den)
	else
		q, sticky = bigDivSticky(num, bigShl(den, -k))
	end
	-- q has 40 or 41 bits; fold the sticky into an extra last bit
	return roundPack(signBit, q * 2 + (if sticky then 1 else 0), -k - 1)
end

F.parseFloat32 = parseFloat32

-- ---------------------------------------------------------------------------
-- Float32: a binary32 value carried exactly, for the declarations and values
-- moo numbers cannot spell (0.01 as C means 0.01f, a NaN, an infinity).

local Float32: any = {}
local meta: any = { __index = Float32 }

local function wrap(bits: number): any
	return table.freeze(setmetatable({ bits = bits }, meta))
end

function Float32.fromBits(bits: number): any
	if type(bits) ~= "number" or bits < 0 or bits > 4294967295 or bits ~= math.floor(bits) then
		error("serialize: Float32.fromBits requires an integer in [0, 2^32)", 2)
	end
	return wrap(bits)
end

-- fromNumber(n): the float32 nearest n by the boundary rule.
function Float32.fromNumber(n: number): any
	if type(n) ~= "number" then
		error("serialize: Float32.fromNumber requires a number", 2)
	end
	return wrap(toFloatBits(n))
end

-- parse(text): a decimal literal rounded once to float32, as C's strtof.
function Float32.parse(text: string): any
	local bits = if type(text) == "string" then parseFloat32(text) else nil
	if bits == nil then
		error("serialize: Float32.parse cannot read " .. string.format("%q", tostring(text)), 2)
	end
	return wrap(bits)
end

function Float32.is(x: any): boolean
	return getmetatable(x) == meta
end

-- toNumber(f): the moo number for f by the boundary rule, or nil when the
-- boundary refuses it (NaN, infinite, out of range).
function Float32.toNumber(f: any): number?
	return fromFloatBits(f.bits)
end

meta.__eq = function(a: any, b: any): boolean
	return getmetatable(a) == meta and getmetatable(b) == meta and a.bits == b.bits
end
meta.__tostring = function(f: any): string
	return string.format("Float32(0x%08X)", f.bits)
end

F.Float32 = table.freeze(Float32)

-- toFloat32Bits(x): the binary32 bits of a parameter given as a Float32 or a
-- number (the number by the boundary rule), or nil for anything else.
function F.toFloat32Bits(x: any): number?
	if type(x) == "number" then
		return toFloatBits(x)
	end
	if getmetatable(x) == meta then
		return x.bits
	end
	return nil
end

-- float32Bits(x, what): toFloat32Bits, raising naming what for anything else.
function F.float32Bits(x: any, what: string): number
	local bits = F.toFloat32Bits(x)
	if bits == nil then
		error("serialize: " .. what .. " must be a number or a Float32", 3)
	end
	return bits
end

return table.freeze(F)
