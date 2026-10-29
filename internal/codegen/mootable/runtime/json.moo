--!strict
-- json.moo — the moo table runtime's TEXT FORM (docs/SPEC-TABLES.md §16;
-- docs/PORT-MOO.md, THE LAYERS, layer 4). One JSON text in and out of one
-- table, by one walk over the descriptors (§8.1), held byte for byte and
-- counter for counter to the compiler's own engine, internal/tabletext.
--
-- moo has no float, no double and no 64-bit integer: its one number is Q48.16
-- fixed point, exact for every integer below 2^47. So this file carries its own
-- arithmetic for the three conversions the form needs, and every one of them is
-- EXACT INTEGER ARITHMETIC, with no float anywhere on its path:
--
--   * a binary32 / binary64 BIT PATTERN to its text: C's `%.*g` at the fewest
--     digits (6..9, 15..17) whose text reads back to the same value at the
--     field's own width — internal/tabletext/write.go's writeFloat, step for
--     step, the read back included (a float32 is read back through a double
--     and narrowed, as Go's `float32(ParseFloat(text, 64))` does). The digits
--     come from the value's exact expansion, rounded half to even.
--   * a decimal token to the NEAREST binary32 / binary64 pattern, ties to even,
--     in one rounding at the field's own width (`strconv.ParseFloat(token,
--     32|64)`), an overflow being an infinity the walk refuses to store.
--   * the wide kinds (int128, uint128 and fixed point) both ways, exactly
--     (internal/tabletext/wide.go's ParseWide and FormatWide).
--
-- The big integers are arrays of 24-bit limbs, least significant first; every
-- limb operation stays below 2^47, so no moo number ever saturates in here.
-- SPDX-License-Identifier: NONE — emitted output, yours under terms of your choice.

local serialize = require("./serialize")

local Int64: any = serialize.Int64
local Uint64: any = serialize.Uint64
local Int128: any = serialize.Int128
local Uint128: any = serialize.Uint128

local J = {}

local POW2: { number } = {}
for i = 0, 46 do
	POW2[i] = 2 ^ i
end

local TWO31 = 2147483648

----------------------------------------------------------------------------
-- BIG INTEGERS: 24-bit limbs, least significant first, no high zero limb, so
-- zero is the empty array.
----------------------------------------------------------------------------

type Big = { number }

local BASE = 16777216 -- 2^24
local HALFBASE = 8388608 -- 2^23

local function bnTrim(a: Big): Big
	while #a > 0 and a[#a] == 0 do
		table.remove(a)
	end
	return a
end

-- bnSmall(x): an integer in [0, 2^47) as a big integer
local function bnSmall(x: number): Big
	local r: Big = {}
	while x > 0 do
		local q = x // BASE
		table.insert(r, x - q * BASE)
		x = q
	end
	return r
end

-- bnMulSmall(a, m): a * m in place, m in [1, 2^22]
local function bnMulSmall(a: Big, m: number): Big
	local carry = 0
	for i = 1, #a do
		local v = a[i] * m + carry
		carry = v // BASE
		a[i] = v - carry * BASE
	end
	while carry > 0 do
		local q = carry // BASE
		table.insert(a, carry - q * BASE)
		carry = q
	end
	return a
end

-- bnAddSmall(a, s): a + s in place, s in [0, 2^24)
local function bnAddSmall(a: Big, s: number): Big
	local i = 1
	while s > 0 do
		local v = (if i <= #a then a[i] else 0) + s
		if v >= BASE then
			a[i] = v - BASE
			s = 1
		else
			a[i] = v
			s = 0
		end
		i += 1
	end
	return a
end

local POW5: { number } = {}
for i = 0, 9 do
	POW5[i] = 5 ^ i
end

-- bnMulPow5(a, n): a * 5^n in place
local function bnMulPow5(a: Big, n: number): Big
	while n >= 9 do
		bnMulSmall(a, 1953125)
		n -= 9
	end
	if n > 0 then
		bnMulSmall(a, POW5[n])
	end
	return a
end

-- bnShl(a, k): a * 2^k, a new array
local function bnShl(a: Big, k: number): Big
	local n = #a
	if n == 0 then
		return {}
	end
	local q = k // 24
	local s = k - q * 24
	local r: Big = table.create(n + q + 1, 0)
	if s == 0 then
		for i = 1, n do
			r[q + i] = a[i]
		end
	else
		local mul = POW2[s]
		local carry = 0
		for i = 1, n do
			local v = a[i] * mul + carry
			carry = v // BASE
			r[q + i] = v - carry * BASE
		end
		r[q + n + 1] = carry
	end
	return bnTrim(r)
end

-- bnShr1(a): a // 2 in place
local function bnShr1(a: Big)
	local n = #a
	for i = 1, n - 1 do
		a[i] = a[i] // 2 + (a[i + 1] % 2) * HALFBASE
	end
	if n > 0 then
		a[n] = a[n] // 2
		bnTrim(a)
	end
end

local function bnCmp(a: Big, b: Big): number
	local n, m = #a, #b
	if n ~= m then
		return if n < m then -1 else 1
	end
	for i = n, 1, -1 do
		local x, y = a[i], b[i]
		if x ~= y then
			return if x < y then -1 else 1
		end
	end
	return 0
end

-- bnSub(a, b): a - b in place, a >= b
local function bnSub(a: Big, b: Big)
	local borrow = 0
	local m = #b
	for i = 1, #a do
		if i > m and borrow == 0 then
			break
		end
		local v = a[i] - (if i <= m then b[i] else 0) - borrow
		if v < 0 then
			v += BASE
			borrow = 1
		else
			borrow = 0
		end
		a[i] = v
	end
	bnTrim(a)
end

local function bnBitLen(a: Big): number
	local n = #a
	if n == 0 then
		return 0
	end
	return (n - 1) * 24 + 32 - bit32.countlz(a[n])
end

-- bnDivSmall(a, d): a // d in place, returning a % d; d in [1, 2^22]
local function bnDivSmall(a: Big, d: number): number
	local r = 0
	for i = #a, 1, -1 do
		local v = r * BASE + a[i]
		local q = v // d
		a[i] = q
		r = v - q * d
	end
	bnTrim(a)
	return r
end

-- bnFromDigits(s): a run of decimal digits as a big integer
local function bnFromDigits(s: string): Big
	local n = #s
	local first = n % 6
	if first == 0 then
		first = 6
	end
	local a = bnSmall(tonumber(string.sub(s, 1, first)) :: number)
	local i = first + 1
	while i <= n do
		bnMulSmall(a, 1000000)
		bnAddSmall(a, tonumber(string.sub(s, i, i + 5)) :: number)
		i += 6
	end
	return a
end

-- bnDecimal(a): the decimal spelling of a big integer
local function bnDecimal(a: Big): string
	if #a == 0 then
		return "0"
	end
	local c = table.clone(a)
	local chunks: { string } = {}
	while #c > 0 do
		local r = bnDivSmall(c, 1000000)
		if #c > 0 then
			table.insert(chunks, string.format("%06d", r))
		else
			table.insert(chunks, string.format("%d", r))
		end
	end
	local out = table.create(#chunks)
	for i = #chunks, 1, -1 do
		table.insert(out, chunks[i])
	end
	return table.concat(out)
end

-- bnFrom32(limbs, n): a frozen wide value's n 32-bit limbs as a big integer
local function bnFrom32(limbs: { number }, n: number): Big
	local r: Big = {}
	local acc, held = 0, 0
	for i = 1, n do
		local v = limbs[i]
		-- each 32-bit limb spills into the 24-bit limbs in two parts
		local lo = v % 65536
		local hi = v // 65536
		acc += lo * POW2[held]
		held += 16
		while held >= 24 do
			local q = acc // BASE
			table.insert(r, acc - q * BASE)
			acc = q
			held -= 24
		end
		acc += hi * POW2[held]
		held += 16
		while held >= 24 do
			local q = acc // BASE
			table.insert(r, acc - q * BASE)
			acc = q
			held -= 24
		end
	end
	if held > 0 then
		table.insert(r, acc)
	end
	return bnTrim(r)
end

-- bnTo32(a, n): the low n 32-bit limbs of a big integer, through its bytes
local function bnTo32(a: Big, n: number): { number }
	local bytes: { number } = table.create(4 * n, 0)
	for i = 1, #a do
		local v = a[i]
		local at = (i - 1) * 3
		for k = 1, 3 do
			if at + k <= 4 * n then
				bytes[at + k] = v % 256
			end
			v = v // 256
		end
	end
	local out: { number } = table.create(n, 0)
	for j = 1, n do
		local b = (j - 1) * 4
		out[j] = bytes[b + 1] + bytes[b + 2] * 256 + bytes[b + 3] * 65536 + bytes[b + 4] * 16777216
	end
	return out
end

-- bnDivBits(R, D, nbits): floor(R / D) for a quotient below 2^nbits, as
-- (high, low) with the quotient high * 2^24 + low; R is left holding the
-- remainder and D is left as it came in. nbits at most 60.
local function bnDivBits(R: Big, D: Big, nbits: number): (number, number)
	local T = bnShl(D, nbits - 1)
	local qh, ql = 0, 0
	for i = nbits - 1, 0, -1 do
		qh *= 2
		ql *= 2
		if ql >= BASE then
			ql -= BASE
			qh += 1
		end
		if bnCmp(R, T) >= 0 then
			bnSub(R, T)
			ql += 1
		end
		if i > 0 then
			bnShr1(T)
		end
	end
	return qh, ql
end

----------------------------------------------------------------------------
-- BINARY FROM A RATIO: the binary float with a P-bit significand nearest
-- A / B * 2^t (A, B > 0), ties to even, in ONE rounding. It returns the
-- significand Q = qh * 2^24 + ql (Q < 2^P) and the exponent e of the value
-- Q * 2^e, e >= EMIN (the subnormal floor). P is 24 or 53.
----------------------------------------------------------------------------

local function roundBinary(A: Big, B: Big, t: number, P: number, EMIN: number): (number, number, number)
	local e = t + bnBitLen(A) - bnBitLen(B) - P
	if e < EMIN then
		e = EMIN
	end
	local s = t - e
	local R: Big, D: Big
	if s >= 0 then
		R = bnShl(A, s)
		D = table.clone(B)
	else
		R = table.clone(A)
		D = bnShl(B, -s)
	end
	-- A * 2^s / B < 2^(P + 1) by the choice of e
	local qh, ql = bnDivBits(R, D, P + 1)
	local top = POW2[P - 24] -- Q >= 2^P iff qh >= top
	local up
	if qh >= top then
		local odd = ql % 2
		ql = ql // 2 + (qh % 2) * HALFBASE
		qh = qh // 2
		e += 1
		up = odd == 1 and (#R > 0 or ql % 2 == 1)
	else
		local c = bnCmp(bnShl(R, 1), D)
		up = c > 0 or (c == 0 and ql % 2 == 1)
	end
	if up then
		ql += 1
		if ql == BASE then
			ql = 0
			qh += 1
		end
		if qh == top and ql == 0 then
			-- the significand carried to 2^P: it is 2^(P - 1) one binade up
			if P > 24 then
				qh = POW2[P - 25]
			else
				qh, ql = 0, HALFBASE
			end
			e += 1
		end
	end
	return qh, ql, e
end

-- the two formats: significand bits, the subnormal floor, the decimal
-- exponents past which a value is certainly infinite or certainly zero
local F32 = { P = 24, EMIN = -149, inf = 39, zero = -46 }
local F64 = { P = 53, EMIN = -1074, inf = 309, zero = -324 }
type Format = typeof(F32)

-- float32Bits(qh, ql, e): the binary32 pattern (sign clear) of Q * 2^e, or
-- the infinity's when it overflowed
local function float32Bits(qh: number, ql: number, e: number): number
	local q = qh * BASE + ql
	local biased = 0
	if q >= HALFBASE then
		biased = e + 150
		q -= HALFBASE
	end
	if biased >= 255 then
		return 0x7F800000
	end
	return biased * HALFBASE + q
end

-- float64Bits(qh, ql, e): the binary64 pattern (sign clear) as (hi, lo)
local function float64Bits(qh: number, ql: number, e: number): (number, number)
	local biased = 0
	if qh >= 268435456 then -- Q >= 2^52
		biased = e + 1075
		qh -= 268435456
	end
	if biased >= 2047 then
		return 0x7FF00000, 0
	end
	return biased * 1048576 + qh // 256, (qh % 256) * BASE + ql
end

-- decimalBinary(digits, q, fmt): the value digits * 10^q (digits a nonempty
-- run with no leading zero) rounded once to the format: 0 and nothing for a
-- zero, 2 for an infinity, or 1 and (qh, ql, e).
local function decimalBinary(digits: string, q: number, fmt: Format): (number, number, number, number)
	local nd = #digits
	if nd + q - 1 >= fmt.inf then
		return 2, 0, 0, 0
	end
	if nd + q <= fmt.zero then
		return 0, 0, 0, 0
	end
	local A = bnFromDigits(digits)
	local B: Big = { 1 }
	if q >= 0 then
		bnMulPow5(A, q)
	else
		bnMulPow5(B, -q)
	end
	local qh, ql, e = roundBinary(A, B, q, fmt.P, fmt.EMIN)
	return 1, qh, ql, e
end

----------------------------------------------------------------------------
-- NUMBER TOKENS (RFC 8259): [-] int [frac] [exp], already validated by the
-- reader's scan. A token is taken apart into its sign, its significant digits
-- and the power of ten they are scaled by.
----------------------------------------------------------------------------

-- tokenParts(token, cap): negative, the digits (leading and trailing zeros
-- stripped, "" for a zero) and the exponent q with value digits * 10^q.
-- The exponent's digits accumulate only while it is below cap, as each Go
-- reader's does (ParseFloat: 10000; the exact readers: 100000).
local function tokenParts(token: string, cap: number): (boolean, string, number)
	local n = #token
	local i = 1
	local negative = false
	local c = string.byte(token, 1)
	if c == 45 or c == 43 then
		negative = c == 45
		i = 2
	end
	local intStart = i
	while i <= n do
		local d = string.byte(token, i) :: number
		if d < 48 or d > 57 then
			break
		end
		i += 1
	end
	local intDigits = string.sub(token, intStart, i - 1)
	local fracDigits = ""
	if i <= n and string.byte(token, i) == 46 then
		i += 1
		local fracStart = i
		while i <= n do
			local d = string.byte(token, i) :: number
			if d < 48 or d > 57 then
				break
			end
			i += 1
		end
		fracDigits = string.sub(token, fracStart, i - 1)
	end
	local exp = 0
	if i <= n then
		local e = string.byte(token, i)
		if e == 101 or e == 69 then
			i += 1
			local expNegative = false
			local s = string.byte(token, i)
			if s == 45 or s == 43 then
				expNegative = s == 45
				i += 1
			end
			while i <= n do
				local d = string.byte(token, i) :: number
				if d < 48 or d > 57 then
					break
				end
				if exp < cap then
					exp = exp * 10 + d - 48
				end
				i += 1
			end
			if expNegative then
				exp = -exp
			end
		end
	end
	local digits = intDigits .. fracDigits
	local q = exp - #fracDigits
	local lead = string.find(digits, "[1-9]")
	if lead == nil then
		return negative, "", 0
	end
	local last = #digits
	while string.byte(digits, last) == 48 do
		last -= 1
	end
	q += #digits - last
	return negative, string.sub(digits, lead :: number, last), q
end

-- parseFloat32(token): the binary32 nearest the token's value, ties to even,
-- one rounding (strconv.ParseFloat(token, 32)); an overflow is the infinity's
-- pattern, signed, exactly as the Go reader lands it before refusing it.
function J.parseFloat32(token: string): number
	local negative, digits, q = tokenParts(token, 10000)
	local sign = if negative then TWO31 else 0
	if digits == "" then
		return sign
	end
	local kind, qh, ql, e = decimalBinary(digits, q, F32)
	if kind == 0 then
		return sign
	end
	if kind == 2 then
		return sign + 0x7F800000
	end
	return sign + float32Bits(qh, ql, e)
end

-- parseFloat64(token): the binary64 nearest the token's value as (hi, lo)
function J.parseFloat64(token: string): (number, number)
	local negative, digits, q = tokenParts(token, 10000)
	local sign = if negative then TWO31 else 0
	if digits == "" then
		return sign, 0
	end
	local kind, qh, ql, e = decimalBinary(digits, q, F64)
	if kind == 0 then
		return sign, 0
	end
	if kind == 2 then
		return sign + 0x7FF00000, 0
	end
	local hi, lo = float64Bits(qh, ql, e)
	return sign + hi, lo
end

----------------------------------------------------------------------------
-- THE FLOAT SPELLING (docs/SPEC-TABLES.md §16.1): C's `%.*g` at the fewest
-- digits that read back as the same value at the field's own width.
----------------------------------------------------------------------------

-- incrementDigits(d): the decimal string d plus one, and whether it carried
-- into a new leading digit (then the result is 1 followed by zeros, same length)
local function incrementDigits(d: string): (string, boolean)
	local n = #d
	local at = n
	while at >= 1 and string.byte(d, at) == 57 do
		at -= 1
	end
	if at == 0 then
		return "1" .. string.rep("0", n - 1), true
	end
	return string.sub(d, 1, at - 1) .. string.char((string.byte(d, at) :: number) + 1) .. string.rep("0", n - at), false
end

-- spellG(negative, d, E): `%.*g` of the value 0.d * 10^(E + 1), d holding
-- exactly the precision's digits: the exponent form when E < -4 or E >= the
-- precision, the plain form otherwise, trailing zeros and a trailing point
-- removed, and an exponent of at least two digits (write.go's formatG).
local function spellG(negative: boolean, d: string, E: number): string
	local p = #d
	local sign = if negative then "-" else ""
	local last = p
	while last > 1 and string.byte(d, last) == 48 do
		last -= 1
	end
	if E < -4 or E >= p then
		local mant = string.sub(d, 1, 1)
		if last > 1 then
			mant ..= "." .. string.sub(d, 2, last)
		end
		local mag = if E < 0 then -E else E
		return sign .. mant .. "e" .. (if E < 0 then "-" else "+") .. (if mag < 10 then "0" else "") .. string.format("%d", mag)
	end
	if E >= 0 then
		local whole = string.sub(d, 1, E + 1)
		if last > E + 1 then
			return sign .. whole .. "." .. string.sub(d, E + 2, last)
		end
		return sign .. whole
	end
	return sign .. "0." .. string.rep("0", -E - 1) .. string.sub(d, 1, last)
end

-- expansion(M, e, high): the value M * 2^e (M > 0) as its first high + 1
-- significant decimal digits, truncated, the decimal exponent E of the first
-- one, and whether any digit past them is nonzero.
local function expansion(M: Big, e: number, high: number): (string, number, boolean)
	-- floor(log10(v)) from the binary exponent; 78913 / 2^18 is log10(2)
	-- to within the slack the correction below absorbs
	local E = ((e + bnBitLen(M) - 1) * 78913) // 262144
	while true do
		local k = high - E
		local A = table.clone(M)
		local B: Big = { 1 }
		if k >= 0 then
			bnMulPow5(A, k)
		else
			bnMulPow5(B, -k)
		end
		local t = e + k
		if t >= 0 then
			A = bnShl(A, t)
		else
			B = bnShl(B, -t)
		end
		-- W = floor(v * 10^k) < 10^(high + 2) < 2^60
		local nbits = bnBitLen(A) - bnBitLen(B) + 1
		if nbits < 1 then
			nbits = 1
		end
		local qh, ql = bnDivBits(A, B, nbits)
		local sticky = #A > 0
		local W = bnShl(bnSmall(qh), 24)
		bnAddSmall(W, ql)
		local digits = bnDecimal(W)
		local n = #digits
		if n == high + 2 then
			sticky = sticky or string.byte(digits, high + 2) ~= 48
			return string.sub(digits, 1, high + 1), E + 1, sticky
		end
		if n == high + 1 then
			return digits, E, sticky
		end
		-- the estimate was off: move it by the digits it missed and redo
		E += if n > high + 2 then n - high - 1 else -1
	end
end

-- roundAt(W, sticky, p): W (high + 1 digits) rounded half to even at p digits,
-- and whether the rounding carried into a new decade
local function roundAt(W: string, sticky: boolean, p: number): (string, boolean)
	local d = string.sub(W, 1, p)
	local nextDigit = (string.byte(W, p + 1) :: number) - 48
	local rest = sticky or string.find(W, "[1-9]", p + 2) ~= nil
	local lastOdd = ((string.byte(d, p) :: number) - 48) % 2 == 1
	if nextDigit > 5 or (nextDigit == 5 and (rest or lastOdd)) then
		return incrementDigits(d)
	end
	return d, false
end

-- the significand of a positive binary64 (hi, lo) as a big integer, and e
local function float64Parts(hi: number, lo: number): (Big, number)
	local biased = hi // 1048576
	local frac = hi % 1048576
	local l1 = lo % BASE
	local l2 = lo // BASE + (frac % 65536) * 256
	local l3 = frac // 65536
	if biased > 0 then
		l3 += 16 -- the implicit 2^52
	end
	return bnTrim({ l1, l2, l3 }), if biased == 0 then -1074 else biased - 1075
end

-- readsBack(d, E, single, bits, hi, lo): whether the text of the digits d at
-- decimal exponent E reads back as the value: parsed to the nearest double
-- and, for a float32, narrowed to the nearest float32, exactly as write.go's
-- `float32(strconv.ParseFloat(text, 64))` does (two roundings, on purpose).
local function readsBack(d: string, E: number, single: boolean, bits: number, hi: number, lo: number): boolean
	local last = #d
	while string.byte(d, last) == 48 do
		last -= 1
	end
	local q = E - #d + 1 + (#d - last)
	local kind, qh, ql, e = decimalBinary(string.sub(d, 1, last), q, F64)
	if kind ~= 1 then
		return false
	end
	local bh, bl = float64Bits(qh, ql, e)
	if bh >= 0x7FF00000 then
		return false -- past the largest double: ParseFloat's range error
	end
	if not single then
		return bh == hi and bl == lo
	end
	local M, me = float64Parts(bh, bl)
	local nh, nl, ne = roundBinary(M, { 1 }, me, 24, -149)
	return float32Bits(nh, nl, ne) == bits
end

local function shortest(negative: boolean, M: Big, e: number, single: boolean, bits: number, hi: number, lo: number): string
	if #M == 0 then
		return if negative then "-0" else "0"
	end
	local low, high = 15, 17
	if single then
		low, high = 6, 9
	end
	local W, E, sticky = expansion(M, e, high)
	for p = low, high do
		local d, carried = roundAt(W, sticky, p)
		local Ep = if carried then E + 1 else E
		if p >= high or readsBack(d, Ep, single, bits, hi, lo) then
			return spellG(negative, d, Ep)
		end
	end
	error("unreachable")
end

-- formatFloat32(bits): the text of a finite binary32 pattern, or nil for a
-- NaN or an infinity, which have no JSON spelling (§16.3)
function J.formatFloat32(bits: number): string?
	local mag = bits % TWO31
	if mag >= 0x7F800000 then
		return nil
	end
	local biased = mag // HALFBASE
	local frac = mag % HALFBASE
	local M = bnSmall(if biased > 0 then frac + HALFBASE else frac)
	local e = if biased == 0 then -149 else biased - 150
	return shortest(bits >= TWO31, M, e, true, mag, 0, 0)
end

-- formatFloat64(hi, lo): the text of a finite binary64 pattern, or nil
function J.formatFloat64(hi: number, lo: number): string?
	local mag = hi % TWO31
	if mag >= 0x7FF00000 then
		return nil
	end
	local M, e = float64Parts(mag, lo)
	return shortest(hi >= TWO31, M, e, false, 0, mag, lo)
end


----------------------------------------------------------------------------
-- INTEGERS (docs/SPEC-TABLES.md §16.2 Numbers): JSON has one number type, so
-- an integer field takes any token whose VALUE is integral. A token is a sign
-- and a magnitude (a big integer); the field's reading goes through the
-- nearest double where the spelling has a fraction or an exponent, exactly as
-- internal/tabletext's interpretInteger does, and a map key reads the digits
-- exactly (interpretIntegerExact).
----------------------------------------------------------------------------

local MAX64: Big = { 16777215, 16777215, 65535 } -- 2^64 - 1

-- bnShr(a, k): a // 2^k, a new array
local function bnShr(a: Big, k: number): Big
	local q = k // 24
	local s = k - q * 24
	local n = #a
	local r: Big = table.create(math.max(n - q, 0), 0)
	if s == 0 then
		for i = q + 1, n do
			r[i - q] = a[i]
		end
		return bnTrim(r)
	end
	local div = POW2[s]
	local up = POW2[24 - s]
	for i = q + 1, n do
		local lowPart = a[i] // div
		local high = if i < n then (a[i + 1] % div) * up else 0
		r[i - q] = lowPart + high
	end
	return bnTrim(r)
end

-- bnToNumber(a): a big integer below 2^47 as a number
local function bnToNumber(a: Big): number
	local v = 0
	for i = #a, 1, -1 do
		v = v * BASE + a[i]
	end
	return v
end

-- dblRound(a): the integer a rounded to the nearest double, ties to even,
-- as an integer again: the value Go's float64(magnitude) compares with
local function dblRound(a: Big): Big
	if bnBitLen(a) <= 53 then
		return a
	end
	local qh, ql, e = roundBinary(a, { 1 }, 0, 53, -1074)
	local q = bnShl(bnSmall(qh), 24)
	bnAddSmall(q, ql)
	return bnShl(q, e)
end

-- the signed order of two (negative, magnitude) pairs; a zero is never negative
local function cmpSigned(an: boolean, am: Big, bn: boolean, bm: Big): number
	if an ~= bn then
		return if an then -1 else 1
	end
	local c = bnCmp(am, bm)
	return if an then -c else c
end

-- interpretInteger(token, integral): whether the value is finite, fractional
-- or saturated past 64 bits, its sign and its magnitude (tabletext's
-- interpretInteger, branch for branch)
local function interpretInteger(token: string, integral: boolean): (boolean, boolean, boolean, boolean, Big)
	if integral then
		local negative = string.byte(token, 1) == 45
		local digits = if negative then string.sub(token, 2) else token
		if #digits > 20 then
			return true, false, true, negative, table.clone(MAX64)
		end
		local mag = bnFromDigits(digits)
		if bnCmp(mag, MAX64) > 0 then
			return true, false, true, negative, table.clone(MAX64)
		end
		return true, false, false, negative and #mag > 0, mag
	end
	local hi, lo = J.parseFloat64(token)
	local mag = hi % TWO31
	if mag >= 0x7FF00000 then
		return false, false, false, false, {}
	end
	local negative = hi >= TWO31
	local M, e = float64Parts(mag, lo)
	if #M == 0 then
		return true, false, false, false, {}
	end
	if bnBitLen(M) + e > 64 then
		return true, false, true, negative, table.clone(MAX64)
	end
	if e >= 0 then
		return true, false, false, negative, bnShl(M, e)
	end
	local whole = bnShr(M, -e)
	if bnCmp(bnShl(whole, -e), M) ~= 0 then
		return true, true, false, negative, {}
	end
	return true, false, false, negative and #whole > 0, whole
end

-- interpretIntegerExact(token): a map key's reading, the token's own digits
-- where they stand, no double on the path (tabletext's interpretIntegerExact)
local function interpretIntegerExact(token: string): (boolean, boolean, boolean, Big)
	local negative, digits, q = tokenParts(token, 100000)
	if digits == "" then
		return false, false, false, {}
	end
	if q < 0 then
		return true, false, negative, {} -- a significant digit below the point
	end
	if #digits + q > 20 then
		return false, true, negative, table.clone(MAX64)
	end
	local mag = bnFromDigits(digits)
	for _ = 1, q do
		bnMulSmall(mag, 10)
	end
	if bnCmp(mag, MAX64) > 0 then
		return false, true, negative, table.clone(MAX64)
	end
	return false, false, negative, mag
end

-- inDomain(negative, mag, signed, bytes): the value the target's storage of
-- `bytes` holds, and whether the domain moved it (tabletext's integerInDomain)
local function inDomain(negative: boolean, mag: Big, signed: boolean, bytes: number): (boolean, Big, boolean)
	if signed then
		local high = bnSmall(0)
		local floor = bnShl({ 1 }, bytes * 8 - 1)
		high = table.clone(floor)
		bnSub(high, { 1 })
		if negative then
			if bnCmp(mag, floor) > 0 then
				return true, floor, true
			end
			return true, mag, false
		end
		if bnCmp(mag, high) > 0 then
			return false, high, true
		end
		return false, mag, false
	end
	if negative then
		return false, {}, true
	end
	local high = bnShl({ 1 }, bytes * 8)
	bnSub(high, { 1 })
	if bnCmp(mag, high) > 0 then
		return false, high, true
	end
	return false, mag, false
end

----------------------------------------------------------------------------
-- WIDE VALUES: a serialize.moo class value (frozen 32-bit limbs) to and from a
-- (negative, magnitude) pair.
----------------------------------------------------------------------------

local CLASS: { [string]: any } = { I64 = Int64, U64 = Uint64, I128 = Int128, U128 = Uint128 }
local LIMBS: { [string]: number } = { I64 = 2, U64 = 2, I128 = 4, U128 = 4 }

-- signedOf(v, signed, n): a number or an n-limb wide value as (negative, mag)
local function signedOf(v: any, signed: boolean, n: number): (boolean, Big)
	if type(v) == "number" then
		if v < 0 then
			return true, bnSmall(-v)
		end
		return false, bnSmall(v)
	end
	local raw = bnFrom32(v, n)
	if signed and v[n] >= TWO31 then
		local mag = bnShl({ 1 }, 32 * n)
		bnSub(mag, raw)
		return true, mag
	end
	return false, raw
end

-- wideOf(cls, negative, mag): the class value of a signed magnitude, two's
-- complement for a negative
local function wideOf(cls: string, negative: boolean, mag: Big): any
	local n = LIMBS[cls]
	local raw = mag
	if negative and #mag > 0 then
		raw = bnShl({ 1 }, 32 * n)
		bnSub(raw, mag)
	end
	local limbs = bnTo32(raw, n)
	return CLASS[cls].new(table.unpack(limbs, 1, n))
end

-- ParseWide (tabletext/wide.go): the raw integer value * 2^frac of a token,
-- whether it is exact in Q I.F, and whether it saturated the 128-bit domain
local DECIMAL_BAND = 40

local function parseWide(token: string, signed: boolean, frac: number): (boolean, boolean, Big, boolean)
	local negative, digits, q = tokenParts(token, 100000)
	if digits == "" then
		return true, false, {}, false
	end
	local point = #digits + q
	local hi = bnShl({ 1 }, if signed then 127 else 128)
	bnSub(hi, { 1 })
	local lo = if signed then bnShl({ 1 }, 127) else {}
	if point > DECIMAL_BAND then
		if negative then
			return true, #lo > 0, lo, true
		end
		return true, false, hi, true
	end
	if point < -DECIMAL_BAND then
		return false, false, {}, false
	end
	local raw = bnShl(bnFromDigits(digits), frac)
	if q >= 0 then
		bnMulPow5(raw, q)
		raw = bnShl(raw, q)
	else
		local k = -q
		while k > 0 do
			local step = if k >= 6 then 6 else k
			local d = POW5[step] * POW2[step]
			if bnDivSmall(raw, d) ~= 0 then
				return false, false, {}, false
			end
			k -= step
		end
	end
	if negative and #raw > 0 then
		if bnCmp(raw, lo) > 0 then
			return true, #lo > 0, lo, true
		end
		return true, true, raw, false
	end
	if negative then
		return true, false, raw, false
	end
	if bnCmp(raw, hi) > 0 then
		return true, false, hi, true
	end
	return true, false, raw, false
end

-- FormatWide (tabletext/wide.go): a 128-bit integer as its digits, a fixed
-- value as the shortest exact decimal in whole units with at least one
-- fractional digit
local function formatWide(negative: boolean, mag: Big, frac: number?): string
	local sign = if negative and #mag > 0 then "-" else ""
	if frac == nil then
		return sign .. bnDecimal(mag)
	end
	local whole = bnShr(mag, frac)
	local fraction = table.clone(mag)
	if #whole > 0 then
		bnSub(fraction, bnShl(whole, frac))
	end
	if #fraction == 0 then
		return sign .. bnDecimal(whole) .. ".0"
	end
	-- fraction / 2^frac = fraction * 5^frac / 10^frac: frac digits exactly
	bnMulPow5(fraction, frac)
	local digits = bnDecimal(fraction)
	digits = string.rep("0", frac - #digits) .. digits
	local last = #digits
	while string.byte(digits, last) == 48 do
		last -= 1
	end
	return sign .. bnDecimal(whole) .. "." .. string.sub(digits, 1, last)
end

----------------------------------------------------------------------------
-- TIMESTAMPS (docs/SPEC-TABLES.md §2.11, §16.2): strict RFC 3339 §5.6, the
-- UTC nanosecond count an Int64. internal/rfc3339's parser, line for line.
----------------------------------------------------------------------------

local function leap(year: number): boolean
	return year % 4 == 0 and (year % 100 ~= 0 or year % 400 == 0)
end

local MONTH_DAYS = { 31, 28, 31, 30, 31, 30, 31, 31, 30, 31, 30, 31 }

local function monthDays(year: number, month: number): number
	if month == 2 and leap(year) then
		return 29
	end
	return MONTH_DAYS[month]
end

-- days from civil (Howard Hinnant), the count since 1970-01-01
local function daysFrom(year: number, month: number, day: number): number
	local y = if month <= 2 then year - 1 else year
	local era = y // 400
	local yoe = y - era * 400
	local moy = if month > 2 then month - 3 else month + 9
	local doy = (153 * moy + 2) // 5 + day - 1
	local doe = yoe * 365 + yoe // 4 - yoe // 100 + doy
	return era * 146097 + doe - 719468
end

local function digitAt(s: string, i: number): number
	local c = string.byte(s, i)
	if c == nil or c < 48 or c > 57 then
		return -1
	end
	return c - 48
end

local function twoAt(s: string, i: number): number
	local a, b = digitAt(s, i), digitAt(s, i + 1)
	if a < 0 or b < 0 then
		return -1
	end
	return a * 10 + b
end

local BILLION = 1000000000

-- parseTimestamp(text): the Int64 nanosecond count, or nil for a spelling
-- outside RFC 3339 §5.6 or an instant outside the Int64 domain
local function parseTimestamp(s: string): any
	local n = #s
	if n < 20 then
		return nil
	end
	if string.byte(s, 5) ~= 45 or string.byte(s, 8) ~= 45 or string.byte(s, 14) ~= 58 or string.byte(s, 17) ~= 58 then
		return nil
	end
	local t = string.byte(s, 11)
	if t ~= 84 and t ~= 116 then
		return nil
	end
	local year = 0
	for i = 1, 4 do
		local d = digitAt(s, i)
		if d < 0 then
			return nil
		end
		year = year * 10 + d
	end
	local month, day, hour, minute, second = twoAt(s, 6), twoAt(s, 9), twoAt(s, 12), twoAt(s, 15), twoAt(s, 18)
	if month < 0 or day < 0 or hour < 0 or minute < 0 or second < 0 then
		return nil
	end
	if month < 1 or month > 12 or day < 1 or day > monthDays(year, month) then
		return nil
	end
	if hour > 23 or minute > 59 or second > 59 then
		return nil
	end
	local i = 20
	local nanos = 0
	if i <= n and string.byte(s, i) == 46 then
		i += 1
		local count = 0
		while i <= n do
			local d = digitAt(s, i)
			if d < 0 then
				break
			end
			if count < 9 then
				nanos = nanos * 10 + d
			end
			count += 1
			i += 1
		end
		if count == 0 then
			return nil
		end
		while count < 9 do
			nanos *= 10
			count += 1
		end
	end
	if i > n then
		return nil
	end
	local offset = 0
	local c = string.byte(s, i)
	if c == 90 or c == 122 then
		if i ~= n then
			return nil
		end
	elseif c == 43 or c == 45 then
		local oh = twoAt(s, i + 1)
		if oh < 0 or string.byte(s, i + 3) ~= 58 then
			return nil
		end
		local om = twoAt(s, i + 4)
		if om < 0 or oh > 23 or om > 59 or i + 5 ~= n then
			return nil
		end
		offset = (oh * 3600 + om * 60) * (if c == 45 then -1 else 1)
	else
		return nil
	end
	local seconds = daysFrom(year, month, day) * 86400 + hour * 3600 + minute * 60 + second - offset
	if seconds < -9223372037 or seconds > 9223372036 then
		return nil
	end
	if seconds == -9223372037 and nanos < 145224192 then
		return nil
	end
	if seconds == 9223372036 and nanos > 854775807 then
		return nil
	end
	return Int64.add(Int64.mul(Int64.fromNumber(seconds), BILLION), nanos)
end

-- formatTimestamp(v): RFC 3339 with nanoseconds and a Z offset, as Go's
-- time.RFC3339Nano spells it: trailing fraction zeros dropped, no fraction
-- on a whole second
local function formatTimestamp(v: any): string
	local q, r = Int64.divMod(v, BILLION)
	local seconds = Int64.toNumber(q)
	local nanos = Int64.toNumber(r)
	if nanos < 0 then
		nanos += BILLION
		seconds -= 1
	end
	local days = seconds // 86400
	local rest = seconds - days * 86400
	-- civil from days (Howard Hinnant)
	local z = days + 719468
	local era = z // 146097
	local doe = z - era * 146097
	local yoe = (doe - doe // 1460 + doe // 36524 - doe // 146096) // 365
	local y = yoe + era * 400
	local doy = doe - (365 * yoe + yoe // 4 - yoe // 100)
	local mp = (5 * doy + 2) // 153
	local d = doy - (153 * mp + 2) // 5 + 1
	local m = if mp < 10 then mp + 3 else mp - 9
	if m <= 2 then
		y += 1
	end
	local text = string.format("%04d-%02d-%02dT%02d:%02d:%02d", y, m, d, rest // 3600, (rest // 60) % 60, rest % 60)
	if nanos > 0 then
		local f = string.format("%09d", nanos)
		local last = 9
		while string.byte(f, last) == 48 do
			last -= 1
		end
		text ..= "." .. string.sub(f, 1, last)
	end
	return text .. "Z"
end

----------------------------------------------------------------------------
-- UTF-8 and the string escapes (docs/SPEC-TABLES.md §16.3)
----------------------------------------------------------------------------

local function utf8Encode(code: number): string
	if code < 0x80 then
		return string.char(code)
	elseif code < 0x800 then
		return string.char(0xC0 + code // 64, 0x80 + code % 64)
	elseif code < 0x10000 then
		return string.char(0xE0 + code // 4096, 0x80 + (code // 64) % 64, 0x80 + code % 64)
	end
	return string.char(0xF0 + code // 262144, 0x80 + (code // 4096) % 64, 0x80 + (code // 64) % 64, 0x80 + code % 64)
end

-- utf8Sequence(s, i): the length of ONE well-formed UTF-8 sequence at s[i], or
-- the negated length of the maximal subpart when the bytes there are ill-formed
-- (Unicode Table 3-7; tabletext's utf8Sequence)
local function utf8Sequence(s: string, i: number): number
	local lead = string.byte(s, i) :: number
	local want
	local lo, hi = 0x80, 0xBF
	if lead < 0x80 then
		return 1
	elseif lead >= 0xC2 and lead <= 0xDF then
		want = 2
	elseif lead >= 0xE0 and lead <= 0xEF then
		want = 3
		if lead == 0xE0 then
			lo = 0xA0
		elseif lead == 0xED then
			hi = 0x9F
		end
	elseif lead >= 0xF0 and lead <= 0xF4 then
		want = 4
		if lead == 0xF0 then
			lo = 0x90
		elseif lead == 0xF4 then
			hi = 0x8F
		end
	else
		return -1
	end
	for k = 1, want - 1 do
		local c = string.byte(s, i + k)
		if c == nil or c < lo or c > hi then
			return -k
		end
		lo, hi = 0x80, 0xBF
	end
	return want
end

local HEX = "0123456789abcdef"

local SHORT_ESCAPE: { [number]: string } = {
	[34] = "\\\"",
	[92] = "\\\\",
	[8] = "\\b",
	[12] = "\\f",
	[10] = "\\n",
	[13] = "\\r",
	[9] = "\\t",
}

local function controlEscape(c: number): string
	local hiNibble = c // 16
	return "\\u00" .. string.sub(HEX, hiNibble + 1, hiNibble + 1) .. string.sub(HEX, c % 16 + 1, c % 16 + 1)
end

type Out = { parts: { string }, err: string? }

local function put(w: Out, s: string)
	table.insert(w.parts, s)
end

-- writeString: one JSON string, the seven short escapes, every other control
-- byte as \u00xx, and an ill-formed sequence as U+FFFD per maximal subpart
local function writeString(w: Out, s: string)
	put(w, "\"")
	local n = #s
	local i = 1
	while i <= n do
		-- a run of plain printable ASCII goes out whole
		local at = string.find(s, "[%z\1-\31\"\\\128-\255]", i)
		if at == nil then
			put(w, string.sub(s, i))
			break
		end
		if at > i then
			put(w, string.sub(s, i, at - 1))
		end
		local c = string.byte(s, at) :: number
		local short = SHORT_ESCAPE[c]
		if short ~= nil then
			put(w, short)
			i = at + 1
		elseif c < 0x20 then
			put(w, controlEscape(c))
			i = at + 1
		else
			local width = utf8Sequence(s, at)
			if width < 0 then
				put(w, "\u{FFFD}")
				i = at - width
			else
				put(w, string.sub(s, at, at + width - 1))
				i = at + width
			end
		end
	end
	put(w, "\"")
end

-- writeWString: a wide field's UTF-16 code units back to UTF-8; a pair is one
-- code point, an unpaired surrogate one U+FFFD, a zero unit \u0000
local function writeWString(w: Out, b: buffer, units: number)
	put(w, "\"")
	local i = 0
	while i < units do
		local code = buffer.readu16(b, i * 2)
		if code >= 0xD800 and code <= 0xDBFF and i + 1 < units then
			local low = buffer.readu16(b, i * 2 + 2)
			if low >= 0xDC00 and low <= 0xDFFF then
				code = 0x10000 + (code - 0xD800) * 1024 + (low - 0xDC00)
				i += 1
			end
		end
		if code >= 0xD800 and code <= 0xDFFF then
			code = 0xFFFD
		end
		local short = SHORT_ESCAPE[code]
		if short ~= nil then
			put(w, short)
		elseif code < 0x20 then
			put(w, controlEscape(code))
		else
			put(w, utf8Encode(code))
		end
		i += 1
	end
	put(w, "\"")
end

local BASE64 = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/"
local B64VALUE: { number } = table.create(256, -1)
for i = 1, 64 do
	B64VALUE[(string.byte(BASE64, i) :: number) + 1] = i - 1
end

local function b64(v: number): string
	return string.sub(BASE64, v + 1, v + 1)
end

-- writeBase64: standard alphabet, padded
local function writeBase64(w: Out, b: buffer?, n: number)
	local parts = table.create(n // 3 * 4 + 6)
	table.insert(parts, "\"")
	local i = 0
	while n - i >= 3 do
		local v = buffer.readu8(b :: buffer, i) * 65536 + buffer.readu8(b :: buffer, i + 1) * 256 + buffer.readu8(b :: buffer, i + 2)
		table.insert(parts, b64(v // 262144) .. b64((v // 4096) % 64) .. b64((v // 64) % 64) .. b64(v % 64))
		i += 3
	end
	if i < n then
		local left = n - i
		local v = buffer.readu8(b :: buffer, i) * 65536
		if left == 2 then
			v += buffer.readu8(b :: buffer, i + 1) * 256
		end
		local s = b64(v // 262144) .. b64((v // 4096) % 64)
		if left == 2 then
			s ..= b64((v // 64) % 64) .. "="
		else
			s ..= "=="
		end
		table.insert(parts, s)
	end
	table.insert(parts, "\"")
	put(w, table.concat(parts))
end

-- decodeBase64(s, bound): §16.2's grammar, then the bytes (at most bound,
-- -1 for none) and whether any were dropped; nil when it is not base64
local function decodeBase64(s: string, bound: number): (buffer?, number, boolean)
	local n = #s
	local symbols, pad = 0, 0
	for i = 1, n do
		local c = string.byte(s, i) :: number
		if c == 61 then
			pad += 1
		elseif pad > 0 or B64VALUE[c + 1] < 0 then
			return nil, 0, false
		else
			symbols += 1
		end
	end
	if pad > 0 then
		if pad > 2 or (symbols + pad) % 4 ~= 0 or symbols % 4 < 2 then
			return nil, 0, false
		end
	elseif symbols % 4 == 1 then
		return nil, 0, false
	end
	local total = symbols * 6 // 8
	local keep = if bound >= 0 and total > bound then bound else total
	local out = buffer.create(keep)
	local acc, held, o = 0, 0, 0
	for i = 1, symbols do
		acc = (acc % 256) * 64 + B64VALUE[(string.byte(s, i) :: number) + 1]
		held += 6
		if held >= 8 then
			held -= 8
			if o < keep then
				buffer.writeu8(out, o, (acc // POW2[held]) % 256)
			end
			o += 1
		end
	end
	return out, keep, keep < total
end

----------------------------------------------------------------------------
-- THE READER (docs/SPEC-TABLES.md §16.1, §16.2): one text, one cursor.
----------------------------------------------------------------------------

local MAX_DEPTH = 128
local MAX_KEY = 255

export type Report = {
	unknown: number,
	kindMismatch: number,
	widened: number,
	clamped: number,
	duplicate: number,
	malformed: boolean,
	reason: string?,
	blobShared: number,
}

type Reader = {
	t: string,
	n: number,
	pos: number,
	bad: boolean,
	zero: boolean,
	report: Report,
	graph: { [any]: { open: boolean, node: any, t: string? } }?,
}

local function space(r: Reader)
	local t, n = r.t, r.n
	while r.pos <= n do
		local c = string.byte(t, r.pos)
		if c == 32 or c == 9 or c == 10 or c == 13 then
			r.pos += 1
		elseif c == 47 then
			local nx = string.byte(t, r.pos + 1)
			if nx == 47 then
				local e = string.find(t, "\n", r.pos + 2, true)
				r.pos = if e == nil then n + 1 else e
			elseif nx == 42 then
				local _, e = string.find(t, "*/", r.pos + 2, true)
				if e == nil then
					r.bad = true
					r.pos = n + 1
					return
				end
				r.pos = (e :: number) + 1
			else
				r.bad = true
				return
			end
		else
			return
		end
	end
end

-- peek: the byte at the cursor after whitespace and comments, 0 at the end
local function peek(r: Reader): number
	space(r)
	if r.pos <= r.n then
		return string.byte(r.t, r.pos) :: number
	end
	return 0
end

local SHAPE_OF: { [number]: string } = { [123] = "o", [91] = "a", [34] = "s", [116] = "b", [102] = "b", [110] = "z" }

local function valueShape(r: Reader): string
	local c = peek(r)
	if c == 0 then
		return ""
	end
	return SHAPE_OF[c] or "n"
end

local function literal(r: Reader, word: string): boolean
	if string.sub(r.t, r.pos, r.pos + #word - 1) ~= word then
		r.bad = true
		return false
	end
	r.pos += #word
	return true
end

local function hex4(r: Reader): number
	if r.pos + 3 > r.n then
		return -1
	end
	local v = 0
	for i = 0, 3 do
		local c = string.byte(r.t, r.pos + i) :: number
		local d
		if c >= 48 and c <= 57 then
			d = c - 48
		elseif c >= 97 and c <= 102 then
			d = c - 87
		elseif c >= 65 and c <= 70 then
			d = c - 55
		else
			return -1
		end
		v = v * 16 + d
	end
	r.pos += 4
	return v
end

local ESCAPE: { [number]: string } = {
	[34] = "\"",
	[92] = "\\",
	[47] = "/",
	[98] = "\b",
	[102] = "\f",
	[110] = "\n",
	[114] = "\r",
	[116] = "\t",
}

-- scanUnit: ONE string body character as UTF-8 — an escape's code point or a
-- UTF-8 sequence read whole — and whether it was the closing quote; nil and
-- false where the text is not JSON (tabletext's scanUnit)
local function scanUnit(r: Reader): (string?, boolean)
	if r.pos > r.n then
		r.bad = true
		return nil, false
	end
	local t = r.t
	local c = string.byte(t, r.pos) :: number
	if c == 34 then
		r.pos += 1
		return nil, true
	end
	if c == 92 then
		r.pos += 1
		if r.pos > r.n then
			r.bad = true
			return nil, false
		end
		local e = string.byte(t, r.pos) :: number
		r.pos += 1
		local short = ESCAPE[e]
		if short ~= nil then
			return short, false
		end
		if e ~= 117 then
			r.bad = true
			return nil, false
		end
		local high = hex4(r)
		if high < 0 then
			r.bad = true
			return nil, false
		end
		local code = high
		if high >= 0xD800 and high <= 0xDBFF and r.pos + 1 <= r.n and string.byte(t, r.pos) == 92 and string.byte(t, r.pos + 1) == 117 then
			local mark = r.pos
			r.pos += 2
			local low = hex4(r)
			if low >= 0xDC00 and low <= 0xDFFF then
				code = 0x10000 + (high - 0xD800) * 1024 + (low - 0xDC00)
			else
				r.pos = mark -- a lone lead surrogate never found its partner
			end
		end
		if code >= 0xD800 and code <= 0xDFFF then
			code = 0xFFFD
		end
		return utf8Encode(code), false
	end
	if c < 0x20 then
		r.bad = true -- a raw control character is not a JSON string body
		return nil, false
	end
	local want = 1
	if c >= 0xC0 and c < 0xE0 then
		want = 2
	elseif c >= 0xE0 and c < 0xF0 then
		want = 3
	elseif c >= 0xF0 and c < 0xF8 then
		want = 4
	end
	local start = r.pos
	r.pos += 1
	while r.pos - start < want and r.pos <= r.n do
		local k = string.byte(t, r.pos) :: number
		if k < 0x80 or k > 0xBF then
			break
		end
		r.pos += 1
	end
	local unit = string.sub(t, start, r.pos - 1)
	if c >= 0x80 and utf8Sequence(unit, 1) ~= #unit then
		-- A SEQUENCE THAT IS NOT A CODE POINT IS REFUSED BY NAME (§16.3)
		if r.report.reason == nil then
			r.report.reason = "string_not_utf8: <root>"
		end
		r.bad = true
		return nil, false
	end
	return unit, false
end

-- scanText(r, capacity, store): one JSON string, ONE CODE POINT AT A TIME, so
-- a string longer than capacity (bytes; < 0 unbounded) is clamped at a code
-- point boundary; store says the text lands in text storage, where a zero
-- refuses the read (§16.3). Returns the text, whether it clamped, and ok.
local function scanText(r: Reader, capacity: number, store: boolean): (string, boolean, boolean)
	if peek(r) ~= 34 then
		r.bad = true
		return "", false, false
	end
	r.pos += 1
	local t = r.t
	local parts: { string } = {}
	local length = 0
	local clamped = false
	while true do
		-- a run of plain ASCII is one code point a byte: placed whole
		local at = string.find(t, "[%z\1-\31\"\\\128-\255]", r.pos)
		if at == nil then
			r.bad = true
			r.pos = r.n + 1
			return "", false, false
		end
		if at > r.pos then
			local run = at - r.pos
			if not clamped then
				if capacity >= 0 and length + run > capacity then
					local fit = capacity - length
					table.insert(parts, string.sub(t, r.pos, r.pos + fit - 1))
					length += fit
					clamped = true
				else
					table.insert(parts, string.sub(t, r.pos, at - 1))
					length += run
				end
			end
			r.pos = at
		end
		local unit, done = scanUnit(r)
		if done then
			break
		end
		if unit == nil then
			return "", false, false
		end
		if store and unit == "\0" then
			r.zero = true -- §16.3: a zero bound for text storage
			r.bad = true
			return "", false, false
		end
		if clamped or (capacity >= 0 and length + #unit > capacity) then
			clamped = true
		else
			table.insert(parts, unit)
			length += #unit
		end
	end
	return table.concat(parts), clamped, true
end

local function scanString(r: Reader, capacity: number): (string, boolean, boolean)
	return scanText(r, capacity, false)
end

local function scanStored(r: Reader, capacity: number): (string, boolean, boolean)
	return scanText(r, capacity, true)
end

-- scanWString(r, capacity): one JSON string as UTF-16 code units, appended
-- one code point at a time so a clamp never splits a pair
local function scanWString(r: Reader, capacity: number): ({ number }, boolean, boolean)
	if peek(r) ~= 34 then
		r.bad = true
		return {}, false, false
	end
	r.pos += 1
	local out: { number } = {}
	local clamped = false
	while true do
		local unit, done = scanUnit(r)
		if done then
			break
		end
		if unit == nil then
			return {}, false, false
		end
		local c = string.byte(unit, 1) :: number
		local code
		if #unit == 1 then
			code = c
		elseif #unit == 2 then
			code = (c % 32) * 64 + (string.byte(unit, 2) :: number) % 64
		elseif #unit == 3 then
			code = (c % 16) * 4096 + ((string.byte(unit, 2) :: number) % 64) * 64 + (string.byte(unit, 3) :: number) % 64
		else
			code = (c % 8) * 262144 + ((string.byte(unit, 2) :: number) % 64) * 4096 + ((string.byte(unit, 3) :: number) % 64) * 64 + (string.byte(unit, 4) :: number) % 64
		end
		if code == 0 then
			r.zero = true
			r.bad = true
			return {}, false, false
		end
		local need = if code < 0x10000 then 1 else 2
		if clamped or (capacity >= 0 and #out + need > capacity) then
			clamped = true
		elseif need == 1 then
			table.insert(out, code)
		else
			local rest = code - 0x10000
			table.insert(out, 0xD800 + rest // 1024)
			table.insert(out, 0xDC00 + rest % 1024)
		end
	end
	return out, clamped, true
end

-- walkNumber: the cursor over ONE RFC 8259 number, and whether it was
-- integral in FORM — no fraction and no exponent
local function walkNumber(r: Reader): (boolean, boolean)
	space(r)
	local t, n = r.t, r.n
	local integral = true
	if r.pos <= n and string.byte(t, r.pos) == 45 then
		r.pos += 1
	end
	if r.pos > n then
		return false, false
	end
	local c = string.byte(t, r.pos) :: number
	if c == 48 then
		r.pos += 1
	elseif c >= 49 and c <= 57 then
		local _, e = string.find(t, "^%d+", r.pos)
		r.pos = (e :: number) + 1
	else
		return false, false
	end
	if r.pos <= n and string.byte(t, r.pos) == 46 then
		r.pos += 1
		local _, e = string.find(t, "^%d+", r.pos)
		if e == nil then
			return false, false
		end
		r.pos = (e :: number) + 1
		integral = false
	end
	if r.pos <= n then
		local x = string.byte(t, r.pos)
		if x == 101 or x == 69 then
			r.pos += 1
			local s = string.byte(t, r.pos)
			if s == 45 or s == 43 then
				r.pos += 1
			end
			local _, e = string.find(t, "^%d+", r.pos)
			if e == nil then
				return false, false
			end
			r.pos = (e :: number) + 1
			integral = false
		end
	end
	return integral, true
end

local function scanNumber(r: Reader): (string, boolean, boolean)
	space(r)
	local start = r.pos
	local integral, ok = walkNumber(r)
	if not ok or r.pos <= start then
		return "", false, false
	end
	return string.sub(r.t, start, r.pos - 1), integral, true
end

-- scanLabel: `&node`'s value, a positive integer with no sign, fraction,
-- exponent or leading zero (§16.7)
local function scanLabel(r: Reader): any
	space(r)
	local _, e = string.find(r.t, "^[1-9]%d*", r.pos)
	if e == nil then
		r.bad = true
		return nil
	end
	local digits = string.sub(r.t, r.pos, e :: number)
	r.pos = (e :: number) + 1
	if #digits > 20 or (#digits == 20 and digits > "18446744073709551615") then
		r.bad = true
		return nil
	end
	-- a label is an identity, never arithmetic: past 2^47 it is its digits
	return if #digits <= 14 then tonumber(digits) :: number else (digits :: any)
end

local skipValue: (Reader, number) -> boolean

local function skippedAmpersand(r: Reader, key: string): boolean
	local graph = r.graph
	if graph == nil or key ~= "&node" then
		return false
	end
	local label = scanLabel(r)
	if label == nil then
		return false
	end
	if graph[label] == nil then
		graph[label] = { open = false, node = nil, t = nil }
	end
	return true
end

local function skipContainer(r: Reader, close: number, depth: number): boolean
	if depth > MAX_DEPTH then
		r.bad = true
		return false
	end
	r.pos += 1
	local first = true
	while true do
		local c = peek(r)
		if c == close then
			r.pos += 1
			return true
		end
		if c == 0 then
			r.bad = true
			return false
		end
		local resumed = false
		if close == 125 then
			local key, _, ok = scanString(r, MAX_KEY)
			if not ok then
				return false
			end
			if peek(r) ~= 58 then
				r.bad = true
				return false
			end
			r.pos += 1
			if #key > 0 and string.byte(key, 1) == 38 and r.graph ~= nil then
				if not first or not skippedAmpersand(r, key) then
					r.bad = true
					return false
				end
				first = false
				c = peek(r)
				if c == 44 then
					r.pos += 1
					resumed = true
				elseif c == close then
					r.pos += 1
					return true
				else
					r.bad = true
					return false
				end
			end
		end
		if not resumed then
			first = false
			if not skipValue(r, depth + 1) then
				return false
			end
			c = peek(r)
			if c == 44 then
				r.pos += 1
			elseif c == close then
				r.pos += 1
				return true
			else
				r.bad = true
				return false
			end
		end
	end
end

skipValue = function(r: Reader, depth: number): boolean
	local c = peek(r)
	if c == 123 then
		return skipContainer(r, 125, depth)
	elseif c == 91 then
		return skipContainer(r, 93, depth)
	elseif c == 34 then
		local _, _, ok = scanString(r, 0)
		return ok
	elseif c == 116 then
		return literal(r, "true")
	elseif c == 102 then
		return literal(r, "false")
	elseif c == 110 then
		return literal(r, "null")
	elseif c == 0 then
		r.bad = true
		return false
	end
	local _, ok = walkNumber(r)
	if not ok then
		r.bad = true
	end
	return ok
end

-- jsonLayerAccepts(text): THE JSON LAYER over one whole text (docs/SPEC-TABLES.md
-- §16.12): a leading byte-order mark, one value, whitespace and comments, then
-- the end, under the reader's caps. JSONTestSuite holds it to the standard.
function J.jsonLayerAccepts(text: string): boolean
	local report: any = { reason = nil }
	local r: Reader = { t = text, n = #text, pos = 1, bad = false, zero = false, report = report, graph = nil }
	if string.sub(text, 1, 3) == "\239\187\191" then
		r.pos = 4
	end
	local ok = skipValue(r, 0)
	space(r)
	return ok and not r.bad and r.pos == r.n + 1
end

----------------------------------------------------------------------------
-- THE WALK over the descriptors (docs/SPEC-TABLES.md §8.1, §16.2): one
-- reader and one writer for every table, internal/tabletext's read.go and
-- write.go case for case. A descriptor carries every fact but two the walk
-- needs; the generated module hands those over through J.extra (below).
----------------------------------------------------------------------------

local St = require("./schema_tables")

-- EXTRA: the facts the text form needs beyond the descriptor, keyed by the
-- field (or element, or arm) descriptor they belong to: a flags set's variant
-- names (`flags`), and a text list's element (`tl`: opt, bound, rec).
local EXTRA: { [any]: any } = {}

function J.extra(fd: any, info: any)
	EXTRA[fd] = table.freeze(info)
end

local function flagsOf(fd: any): { string }?
	local x = EXTRA[fd]
	return if x ~= nil then x.flags else nil
end

local function textListOf(fd: any): any
	local x = EXTRA[fd]
	return if x ~= nil then x.tl else nil
end

local KEYS: { [any]: { [string]: number } } = {}

local function keyIndex(d: any): { [string]: number }
	local k = KEYS[d]
	if k == nil then
		k = {}
		for i, fd in d.fields do
			k[fd.json] = i
		end
		KEYS[d] = k
	end
	return k
end

local function isPointerShape(s: string?): boolean
	return s == "ptr" or s == "sblob" or s == "bblob"
end

-- takesNull: the two kinds where `null` is a value (§16.2): a ?T, which reads
-- it as ABSENT, and a pointer, which reads it as null
local function takesNull(fd: any): boolean
	if fd.present ~= nil or isPointerShape(fd.s) then
		return true
	end
	return fd.s == "arr" and isPointerShape(fd.e.s)
end

local function isWide(fd: any): boolean
	return fd.fbits ~= nil or fd.kw == 16
end

-- elementShape: the JSON shape an array's element takes (tabletext's
-- ElementShape over the element's descriptor)
local function elementShape(e: any): string
	local s = e.s
	if s == "union" then
		return "o"
	elseif s == "tab" or s == "ptr" then
		return if St.lookup(e.t).jsonArray then "a" else "o"
	elseif s == "enum" then
		return "s"
	elseif flagsOf(e) ~= nil then
		return "a"
	elseif e.timestamp then
		return "s"
	elseif s == "bool" then
		return "b"
	end
	return "n"
end

-- shape: the JSON shape a field's declaration takes (tabletext's Shape)
local function shape(fd: any): string
	local s = fd.s
	if s == "str" or s == "text" or s == "bytes" or s == "sblob" or s == "bblob" then
		return "s"
	elseif s == "map" then
		return "o"
	elseif s == "arr" then
		return if fd.array == "keyed" then "o" else "a"
	end
	return elementShape(fd)
end

-- fresh(d): a record at its declared defaults, built by the table's own
-- constructor, so a reset is exactly what construction holds
local function resetField(d: any, fd: any, rec: any)
	local fresh = d.new()
	local slot = fd.slot
	rec[slot] = fresh[slot]
	local length = slot .. "Length"
	if fresh[length] ~= nil then
		rec[length] = fresh[length]
	end
	local count = slot .. "Count"
	if fresh[count] ~= nil then
		rec[count] = fresh[count]
	end
	if fd.present ~= nil then
		rec[fd.present] = false
	end
end

local WIDE_ZERO: { [string]: any } = {
	I64 = Int64.fromNumber(0),
	U64 = Uint64.fromNumber(0),
	I128 = Int128.fromNumber(0),
	U128 = Uint128.fromNumber(0),
}

-- zeroOf(e, list): one value-initialized array slot (tabletext's elementZero)
local function zeroOf(e: any, list: boolean): any
	local s = e.s
	if isPointerShape(s) then
		return if list then false else nil
	elseif s == "tab" or s == "union" then
		return St.lookup(e.t).new()
	elseif s == "bool" then
		return false
	elseif s == "float" then
		return if e.f32 then 0 else WIDE_ZERO.U64
	elseif s == "str" then
		if e.wide then
			return buffer.create(2 * e.bound)
		end
		return ""
	elseif s == "text" then
		return if list then false else nil
	elseif s == "bytes" then
		return buffer.create(e.bound)
	elseif s == "int" then
		return WIDE_ZERO[e.cls] or 0
	end
	return 0
end

local readField: (Reader, any, any, any, number) -> boolean
local readTable: (Reader, any, any, number) -> boolean
local readTableKeys: (Reader, any, any, number, string?) -> boolean

local function nameZero(r: Reader, table: string, fd: any)
	if r.zero and r.report.reason == nil then
		r.report.reason = "text_holds_zero: " .. table .. "." .. fd.name
	end
end

local function readPointerCell(r: Reader, holder: any, key: any, t: string?, depth: number, null: any): boolean
	local graph = r.graph
	if graph == nil or t == nil then
		r.bad = true
		return false
	end
	if depth + 1 > MAX_DEPTH then
		r.bad = true
		return false
	end
	if peek(r) ~= 123 then
		r.bad = true
		return false
	end
	r.pos += 1
	local d = St.lookup(t)
	local c = peek(r)
	if c == 125 then
		r.pos += 1
		holder[key] = d.new()
		return true
	end
	if c == 0 then
		r.bad = true
		return false
	end
	local name, _, ok = scanString(r, MAX_KEY)
	if not ok then
		return false
	end
	if peek(r) ~= 58 then
		r.bad = true
		return false
	end
	r.pos += 1
	if name ~= "&node" then
		local node = d.new()
		holder[key] = node
		return readTableKeys(r, d, node, depth + 1, name)
	end
	local label = scanLabel(r)
	if label == nil then
		return false
	end
	local entry = graph[label]
	c = peek(r)
	if c == 44 then
		r.pos += 1
		c = peek(r)
	end
	local bare = c == 125
	if bare ~= (entry ~= nil) then
		r.bad = true
		return false
	end
	if bare then
		r.pos += 1
		local e = entry :: any
		if e.open then
			r.bad = true -- a reference inside its own definition: a cycle
			return false
		end
		holder[key] = null
		if e.t == nil then
			return true -- a definition the reader dropped names no node
		elseif e.t ~= t then
			r.report.kindMismatch += 1
		else
			holder[key] = e.node
		end
		return true
	end
	local node = d.new()
	holder[key] = node
	graph[label] = { open = true, node = nil, t = nil }
	if not readTableKeys(r, d, node, depth + 1, nil) then
		return false
	end
	graph[label] = { open = false, node = node, t = t }
	return true
end

-- the double a float field's value compares as: (hi, lo) of a binary64
local function widen32(bits: number): (number, number)
	local sign = if bits >= TWO31 then TWO31 else 0
	local mag = bits % TWO31
	local biased = mag // HALFBASE
	local frac = mag % HALFBASE
	if biased == 0 then
		if frac == 0 then
			return sign, 0
		end
		local b = 32 - bit32.countlz(frac)
		local m = frac - POW2[b - 1]
		local shift = 53 - b
		local e64 = b - 1 - 149 + 1023
		local hi, lo
		if shift >= 32 then
			hi, lo = m * POW2[shift - 32], 0
		else
			hi, lo = m // POW2[32 - shift], (m % POW2[32 - shift]) * POW2[shift]
		end
		return sign + e64 * 1048576 + hi, lo
	end
	return sign + (biased - 127 + 1023) * 1048576 + frac // 8, (frac % 8) * 536870912
end

local function narrow64(hi: number, lo: number): number
	local sign = if hi >= TWO31 then TWO31 else 0
	local M, e = float64Parts(hi % TWO31, lo)
	if #M == 0 then
		return sign
	end
	local qh, ql, ne = roundBinary(M, { 1 }, e, 24, -149)
	return sign + float32Bits(qh, ql, ne)
end

-- dcmp: the order of two doubles given as (hi, lo); -0 equals +0
local function dcmp(ah: number, al: number, bh: number, bl: number): number
	local am, bm = ah % TWO31, bh % TWO31
	local az, bz = am == 0 and al == 0, bm == 0 and bl == 0
	if az and bz then
		return 0
	end
	local an, bn = ah >= TWO31 and not az, bh >= TWO31 and not bz
	if an ~= bn then
		return if an then -1 else 1
	end
	local c = 0
	if am ~= bm then
		c = if am < bm then -1 else 1
	elseif al ~= bl then
		c = if al < bl then -1 else 1
	end
	return if an then -c else c
end

local function placeFloat(r: Reader, fd: any, holder: any, key: any, token: string): boolean
	local hi, lo
	if fd.f32 then
		local bits = J.parseFloat32(token)
		if bits % TWO31 >= 0x7F800000 then
			r.report.kindMismatch += 1
			return true
		end
		hi, lo = widen32(bits)
	else
		hi, lo = J.parseFloat64(token)
		if hi % TWO31 >= 0x7FF00000 then
			r.report.kindMismatch += 1
			return true
		end
	end
	if fd.flo ~= nil then
		local flo, fhi = fd.flo, fd.fhi
		if dcmp(hi, lo, flo[2], flo[1]) < 0 then
			hi, lo = flo[2], flo[1]
			r.report.clamped += 1
		elseif dcmp(hi, lo, fhi[2], fhi[1]) > 0 then
			hi, lo = fhi[2], fhi[1]
			r.report.clamped += 1
		elseif hi % TWO31 == 0 and lo == 0 then
			hi, lo = 0, 0 -- a compressed float's -0 lands as +0, uncounted
		end
	end
	if fd.f32 then
		local bits = narrow64(hi, lo)
		if bits % TWO31 >= 0x7F800000 then
			r.report.kindMismatch += 1
			return true
		end
		holder[key] = bits
	else
		holder[key] = Uint64.new(lo, hi)
	end
	return true
end

-- the declared (or bits(N)-implied) range of an integer field, as signed pairs
local function intBounds(fd: any): (boolean, boolean, Big, boolean, Big)
	local n = if fd.kw == 16 then 4 else 2
	if fd.lo ~= nil then
		local ln, lm = signedOf(fd.lo, fd.signed, n)
		local hn, hm = signedOf(fd.hi, fd.signed, n)
		return true, ln, lm, hn, hm
	end
	if fd.bits ~= nil and fd.bits < 64 and fd.timestamp == nil then
		local hi = bnShl({ 1 }, fd.bits)
		bnSub(hi, { 1 })
		return true, false, {}, false, hi
	end
	return false, false, {}, false, {}
end

local function storeInteger(fd: any, holder: any, key: any, negative: boolean, mag: Big)
	local cls = fd.cls
	if cls == "n" or cls == nil then
		local v = bnToNumber(mag)
		holder[key] = if negative then -v else v
	else
		holder[key] = wideOf(cls, negative, mag)
	end
end

local function placeInteger(r: Reader, fd: any, holder: any, key: any, token: string, integral: boolean): boolean
	local finite, fractional, saturated, negative, mag = interpretInteger(token, integral)
	if not finite or fractional then
		r.report.kindMismatch += 1
		return true
	end
	if saturated then
		r.report.clamped += 1
	end
	-- THE DECLARED RANGE FIRST, compared as the doubles the engine compares
	local bn, bm = negative, mag
	local has, ln, lm, hn, hm = intBounds(fd)
	if has then
		local scale = dblRound(mag)
		local dlo, dhi = dblRound(lm), dblRound(hm)
		if cmpSigned(negative, scale, ln, dlo) < 0 then
			bn, bm = ln and #dlo > 0, dlo
			r.report.clamped += 1
		elseif cmpSigned(negative, scale, hn, dhi) > 0 then
			bn, bm = hn and #dhi > 0, dhi
			r.report.clamped += 1
		end
		if bnCmp(bm, MAX64) > 0 then
			bm = table.clone(MAX64)
		end
	end
	-- THEN THE STORAGE WIDTH
	local bytes = fd.kw
	if fd.bits ~= nil then
		bytes = if fd.bits <= 32 then 4 else 8
	end
	local vn, vm, moved = inDomain(bn, bm, fd.signed, bytes)
	if moved then
		r.report.clamped += 1
	end
	storeInteger(fd, holder, key, vn, vm)
	return true
end

local function placeWide(r: Reader, fd: any, holder: any, key: any, token: string): boolean
	local exact, negative, mag, saturated = parseWide(token, fd.signed, fd.fbits or 0)
	if not exact then
		r.report.kindMismatch += 1
		return true
	end
	if saturated then
		r.report.clamped += 1
	end
	if fd.lo ~= nil then
		local n = if fd.kw == 16 then 4 else 2
		local ln, lm = signedOf(fd.lo, fd.signed, n)
		local hn, hm = signedOf(fd.hi, fd.signed, n)
		if cmpSigned(negative, mag, ln, lm) < 0 then
			negative, mag = ln, lm
			r.report.clamped += 1
		elseif cmpSigned(negative, mag, hn, hm) > 0 then
			negative, mag = hn, hm
			r.report.clamped += 1
		end
	end
	local cls = fd.cls
	if cls == "fx" then
		holder[key] = Int64.toNumberRaw(wideOf("I64", negative, mag))
	elseif cls == "n" then
		local v = bnToNumber(mag)
		holder[key] = if negative then -v else v
	else
		holder[key] = wideOf(cls, negative, mag)
	end
	return true
end

local function readUnion(r: Reader, t: string, holder: any, key: any, depth: number): boolean
	if peek(r) ~= 123 then
		r.bad = true
		return false
	end
	r.pos += 1
	local ud = St.lookup(t)
	local u = ud.new()
	holder[key] = u
	if peek(r) == 125 then
		r.pos += 1
		return true
	end
	local name, _, ok = scanString(r, MAX_KEY)
	if not ok then
		return false
	end
	if peek(r) ~= 58 then
		r.bad = true
		return false
	end
	r.pos += 1
	local arm = nil
	for _, a in ud.arms do
		if a.name == name then
			arm = a
			break
		end
	end
	if arm == nil then
		r.report.unknown += 1
		if not skipValue(r, depth + 1) then
			return false
		end
	elseif arm.s == "void" then
		if valueShape(r) ~= "z" then
			r.report.kindMismatch += 1
			if not skipValue(r, depth + 1) then
				return false
			end
		else
			if not literal(r, "null") then
				return false
			end
			u.Type = arm.tag
		end
	elseif arm.s == "tab" and arm.array == nil then
		-- a BODY arm: the declared type's object
		if valueShape(r) ~= "o" then
			r.report.kindMismatch += 1
			if not skipValue(r, depth + 1) then
				return false
			end
		else
			local bd = St.lookup(arm.t)
			local payload = bd.new()
			if not readTable(r, bd, payload, depth + 1) then
				return false
			end
			u[arm.slot] = payload
			u.Type = arm.tag
		end
	else
		-- THE ARM'S VALUE TAKES THE ARM'S OWN ROW: an arm is a field line
		local s = valueShape(r)
		if s == "z" and takesNull(arm) then
			if not literal(r, "null") then
				return false
			end
			u.Type = arm.tag
		elseif s ~= shape(arm) then
			r.report.kindMismatch += 1
			if not skipValue(r, depth + 1) then
				return false
			end
		else
			if not readField(r, ud, arm, u, depth + 1) then
				return false
			end
			u.Type = arm.tag
		end
	end
	local c = peek(r)
	if c == 44 then
		r.pos += 1
		c = peek(r)
	end
	if c == 125 then
		r.pos += 1
		return true
	end
	r.bad = true -- a second key: a one-of with two arms is not a value
	return false
end

local function readFlags(r: Reader, names: { string }, holder: any, key: any, depth: number): boolean
	if peek(r) ~= 91 then
		r.bad = true
		return false
	end
	r.pos += 1
	local low, high = 0, 0
	while true do
		local c = peek(r)
		if c == 93 then
			r.pos += 1
			break
		end
		if c == 0 then
			r.bad = true
			return false
		end
		if c ~= 34 then
			r.report.kindMismatch += 1
			if not skipValue(r, depth + 1) then
				return false
			end
		else
			local name, _, ok = scanString(r, MAX_KEY)
			if not ok then
				return false
			end
			local bit = table.find(names, name)
			if bit == nil then
				r.report.unknown += 1
			elseif bit <= 32 then
				low = bit32.bor(low, POW2[bit - 1])
			else
				high = bit32.bor(high, POW2[bit - 33])
			end
		end
		c = peek(r)
		if c == 44 then
			r.pos += 1
		elseif c == 93 then
			r.pos += 1
			break
		else
			r.bad = true
			return false
		end
	end
	holder[key] = Uint64.new(low, high)
	return true
end

-- readScalar: one value into holder[key]: a nested object, a union, a
-- vocabulary or a number. elem says the value is an array or keyed element
-- (a deprecated variant reads as the element's None, not the field default).
local function readScalar(r: Reader, fd: any, holder: any, key: any, depth: number, elem: boolean, name: string): boolean
	local s = fd.s
	if fd.timestamp then
		local text, _, ok = scanString(r, -1)
		if not ok then
			return false
		end
		local v = parseTimestamp(text)
		if v == nil then
			r.report.reason = "timestamp_not_rfc3339: " .. name
			r.bad = true
			return false
		end
		holder[key] = v
		return true
	end
	if s == "union" then
		return readUnion(r, fd.t, holder, key, depth)
	end
	if s == "ptr" then
		return readPointerCell(r, holder, key, fd.t, depth, if elem then false else nil)
	end
	if s == "tab" then
		local d = St.lookup(fd.t)
		local v = d.new()
		holder[key] = v
		return readTable(r, d, v, depth + 1)
	end
	if s == "enum" then
		local text, _, ok = scanString(r, MAX_KEY)
		if not ok then
			return false
		end
		local value = -1
		if text == "None" then
			value = 0
		else
			for i, v in fd.vs do
				if v[1] == text then
					value = i
					break
				end
			end
		end
		if value > 0 and fd.vs[value][3] then
			-- A DEPRECATED VARIANT READS AS THE DECLARED DEFAULT (§2.15)
			holder[key] = if elem then 0 else fd.def
			return true
		end
		if value < 0 then
			holder[key] = 0
			r.report.unknown += 1
			return true
		end
		holder[key] = value
		return true
	end
	local names = flagsOf(fd)
	if names ~= nil then
		return readFlags(r, names, holder, key, depth)
	end
	if s == "bool" then
		if peek(r) == 116 then
			if not literal(r, "true") then
				return false
			end
			holder[key] = true
			return true
		end
		if not literal(r, "false") then
			return false
		end
		holder[key] = false
		return true
	end
	local token, integral, ok = scanNumber(r)
	if not ok then
		r.bad = true
		return false
	end
	if s == "float" then
		return placeFloat(r, fd, holder, key, token)
	end
	if s == "int" then
		if isWide(fd) then
			return placeWide(r, fd, holder, key, token)
		end
		return placeInteger(r, fd, holder, key, token, integral)
	end
	r.bad = true
	return false
end

-- bytes into a record's buffer slot, re-established first (§16.2 last-wins)
local function readBase64Into(r: Reader, b: buffer, bound: number): (boolean, number)
	buffer.fill(b, 0, 0)
	local raw, _, ok = scanString(r, -1)
	if not ok then
		return false, 0
	end
	local out, n, clamped = decodeBase64(raw, bound)
	if out == nil then
		r.report.kindMismatch += 1
		return true, 0
	end
	if clamped then
		r.report.clamped += 1
	end
	buffer.copy(b, 0, out :: buffer, 0, n)
	return true, n
end

local function readWStringInto(r: Reader, b: buffer, bound: number): (boolean, number)
	local units, clamped, ok = scanWString(r, bound)
	if not ok then
		return false, 0
	end
	if clamped then
		r.report.clamped += 1
	end
	buffer.fill(b, 0, 0)
	for i, u in units do
		buffer.writeu16(b, (i - 1) * 2, u)
	end
	return true, #units
end

local function readArray(r: Reader, fd: any, rec: any, depth: number): boolean
	if peek(r) ~= 91 then
		r.bad = true
		return false
	end
	r.pos += 1
	local e = fd.e
	local slot = fd.slot
	local list = fd.array == "list"
	local bytesElem = e.buf == true
	local lengths: { number }? = nil
	local arr: any
	if bytesElem then
		arr = rec[slot]
		buffer.fill(arr, 0, 0)
	elseif list then
		arr = {}
		rec[slot] = arr
		if rec[slot .. "Length"] ~= nil then
			lengths = {}
			rec[slot .. "Length"] = lengths
		end
	else
		arr = rec[slot]
		lengths = rec[slot .. "Length"]
		for i = 1, fd.bound do
			arr[i] = zeroOf(e, false)
			if lengths ~= nil then
				lengths[i] = 0
			end
		end
	end
	local placed = 0
	local want = elementShape(e)
	local bound = if list then 2147483647 else fd.bound
	local tmp = { 0 }
	while true do
		local c = peek(r)
		if c == 93 then
			r.pos += 1
			break
		end
		if c == 0 then
			r.bad = true
			return false
		end
		if list and placed == #arr then
			table.insert(arr, zeroOf(e, true))
			if lengths ~= nil then
				table.insert(lengths, 0)
			end
		end
		local s = valueShape(r)
		if placed >= bound then
			r.report.clamped += 1
			if not skipValue(r, depth + 1) then
				return false
			end
		elseif isPointerShape(e.s) and s == "z" then
			if not literal(r, "null") then
				return false
			end
			arr[placed + 1] = if list then false else nil
			placed += 1
		elseif s ~= want then
			r.report.kindMismatch += 1
			if not skipValue(r, depth + 1) then
				return false
			end
			placed += 1
		elseif bytesElem then
			tmp[1] = 0
			if not readScalar(r, e, tmp, 1, depth + 1, true, fd.name) then
				return false
			end
			buffer.writeu8(arr, placed, tmp[1])
			placed += 1
		else
			if not readScalar(r, e, arr, placed + 1, depth + 1, true, fd.name) then
				return false
			end
			placed += 1
		end
		c = peek(r)
		if c == 44 then
			r.pos += 1
		elseif c == 93 then
			r.pos += 1
			break
		else
			r.bad = true
			return false
		end
	end
	if fd.array == "counted" then
		rec[slot .. "Count"] = placed
	end
	return true
end

local function readTextList(r: Reader, fd: any, tl: any, rec: any, depth: number): boolean
	if peek(r) ~= 91 then
		r.bad = true
		return false
	end
	r.pos += 1
	local slot = fd.slot
	local list = fd.array == "list"
	local ed = St.lookup(tl.rec)
	local arr: any
	if list then
		arr = {}
		rec[slot] = arr
	else
		arr = rec[slot]
		for i = 1, fd.bound do
			arr[i] = ed.new()
		end
	end
	local placed = 0
	local bound = if list then 2147483647 else fd.bound
	while true do
		local c = peek(r)
		if c == 93 then
			r.pos += 1
			break
		end
		if c == 0 then
			r.bad = true
			return false
		end
		if list and placed == #arr then
			table.insert(arr, (ed.new()))
		end
		local s = valueShape(r)
		if placed >= bound then
			r.report.clamped += 1
			if not skipValue(r, depth + 1) then
				return false
			end
		elseif tl.opt and s == "z" then
			if not literal(r, "null") then
				return false
			end
			placed += 1
		elseif s ~= "s" then
			r.report.kindMismatch += 1
			if not skipValue(r, depth + 1) then
				return false
			end
			placed += 1
		else
			local out, clamped, ok = scanStored(r, tl.bound)
			if not ok then
				return false
			end
			if clamped then
				r.report.clamped += 1
			end
			local el = arr[placed + 1]
			el.Value = out
			if tl.opt then
				el.ValuePresent = true
			end
			placed += 1
		end
		c = peek(r)
		if c == 44 then
			r.pos += 1
		elseif c == 93 then
			r.pos += 1
			break
		else
			r.bad = true
			return false
		end
	end
	if fd.array == "counted" then
		rec[slot .. "Count"] = placed
	end
	return true
end

local function readKeyed(r: Reader, fd: any, rec: any, depth: number): boolean
	if peek(r) ~= 123 then
		r.bad = true
		return false
	end
	r.pos += 1
	local e = fd.e
	local arr = rec[fd.slot]
	for i = 1, fd.bound do
		arr[i] = zeroOf(e, false)
	end
	local want = elementShape(e)
	local seen: { [number]: boolean } = {}
	while true do
		local c = peek(r)
		if c == 125 then
			r.pos += 1
			break
		end
		if c == 0 then
			r.bad = true
			return false
		end
		local name, _, ok = scanString(r, MAX_KEY)
		if not ok then
			return false
		end
		if peek(r) ~= 58 then
			r.bad = true
			return false
		end
		r.pos += 1
		local at = -1
		if name ~= "None" then
			for i, v in fd.keyvs do
				if v[1] == name then
					at = i
					break
				end
			end
		end
		if at > fd.bound then
			at = -1
		end
		if at > 0 then
			if seen[at] then
				r.report.duplicate += 1
			end
			seen[at] = true
		end
		if at < 0 then
			r.report.unknown += 1
			if not skipValue(r, depth + 1) then
				return false
			end
		elseif valueShape(r) ~= want then
			r.report.kindMismatch += 1
			if not skipValue(r, depth + 1) then
				return false
			end
		elseif not readScalar(r, e, arr, at, depth + 1, true, fd.name) then
			return false
		end
		c = peek(r)
		if c == 44 then
			r.pos += 1
		elseif c == 125 then
			r.pos += 1
			break
		else
			r.bad = true
			return false
		end
	end
	return true
end

-- the map key order: bytes for a string key, the number line for an integer
local function keyLess(a: any, b: any, cls: string?, signed: boolean): boolean
	if type(a) == "string" or type(a) == "number" then
		return a < b
	end
	local an, am = signedOf(a, signed, LIMBS[cls :: string])
	local bn, bm = signedOf(b, signed, LIMBS[cls :: string])
	return cmpSigned(an, am, bn, bm) < 0
end

local function keyId(v: any): any
	if type(v) == "string" or type(v) == "number" then
		return v
	end
	return tostring(v)
end

local function readMap(r: Reader, fd: any, rec: any, depth: number): boolean
	if peek(r) ~= 123 then
		r.bad = true
		return false
	end
	r.pos += 1
	local ed = St.lookup(fd.t)
	local kd, vd = ed.fields[1], ed.fields[2]
	local stringKey = kd.s == "str" or kd.s == "text"
	local entries: { any } = {}
	local at: { [any]: number } = {}
	while true do
		local c = peek(r)
		if c == 125 then
			r.pos += 1
			break
		end
		if c == 0 then
			r.bad = true
			return false
		end
		local name, over, ok = scanStored(r, MAX_KEY)
		if not ok then
			return false
		end
		if peek(r) ~= 58 then
			r.bad = true
			return false
		end
		r.pos += 1
		local entry = ed.new()
		local placed = "placed"
		if not over then
			if stringKey then
				entry[kd.slot] = name
			else
				-- THE KEY IS THE SPELLING AND NOTHING AROUND IT
				local probe: Reader = { t = name, n = #name, pos = 1, bad = false, zero = false, report = r.report, graph = nil }
				space(probe)
				local numberOk = false
				if probe.pos == 1 then
					local _, walked = walkNumber(probe)
					numberOk = walked and probe.pos == #name + 1
				end
				if not numberOk then
					placed = "malformed"
				else
					local fractional, saturated, negative, mag = interpretIntegerExact(name)
					if fractional or saturated then
						placed = "mismatch"
					else
						local vn, vm, moved = inDomain(negative, mag, kd.signed, kd.kw)
						if moved then
							placed = "mismatch"
						else
							storeInteger(kd, entry, kd.slot, vn, vm)
						end
					end
				end
			end
		end
		if placed == "malformed" then
			r.report.malformed = true
			r.bad = true
			return false
		end
		if over then
			if stringKey then
				r.report.clamped += 1
			else
				r.report.kindMismatch += 1
			end
			if not skipValue(r, depth) then
				return false
			end
		elseif placed == "mismatch" then
			r.report.kindMismatch += 1
			if not skipValue(r, depth) then
				return false
			end
		elseif kd.s == "str" and #name > kd.bound then
			r.report.clamped += 1
			if not skipValue(r, depth) then
				return false
			end
		else
			local value: boolean
			if valueShape(r) == "z" and takesNull(vd) then
				value = literal(r, "null")
				if value then
					resetField(ed, vd, entry)
				end
			else
				value = readField(r, ed, vd, entry, depth)
				if value and vd.present ~= nil then
					entry[vd.present] = true
				end
			end
			if not value then
				return false
			end
			local id = keyId(entry[kd.slot])
			local i = at[id]
			if i ~= nil then
				r.report.duplicate += 1
				entries[i] = entry
			else
				table.insert(entries, entry)
				at[id] = #entries
			end
		end
		c = peek(r)
		if c == 44 then
			r.pos += 1
		elseif c == 125 then
			r.pos += 1
			break
		else
			r.bad = true
			return false
		end
	end
	local ks, cls, signed = kd.slot, kd.cls, kd.signed == true
	table.sort(entries, function(a: any, b: any): boolean
		return keyLess(a[ks], b[ks], cls, signed)
	end)
	rec[fd.slot] = entries
	return true
end

readField = function(r: Reader, d: any, fd: any, rec: any, depth: number): boolean
	local s = fd.s
	local slot = fd.slot
	if s == "sblob" then
		local out, _, ok = scanStored(r, -1)
		if not ok then
			return false
		end
		-- a *string blob is a buffer, one node of identity (values.go)
		rec[slot] = if out == nil then nil else buffer.fromstring(out)
		return true
	elseif s == "bblob" then
		rec[slot] = nil
		local raw, _, ok = scanString(r, -1)
		if not ok then
			return false
		end
		local out = decodeBase64(raw, -1)
		if out == nil then
			r.report.kindMismatch += 1
			return true
		end
		rec[slot] = out
		return true
	elseif s == "ptr" then
		return readPointerCell(r, rec, slot, fd.t, depth, nil)
	elseif s == "str" then
		if fd.wide then
			local ok, n = readWStringInto(r, rec[slot], fd.bound)
			if not ok then
				return false
			end
			rec[slot .. "Length"] = n
			return true
		end
		local out, clamped, ok = scanStored(r, fd.bound)
		if not ok then
			return false
		end
		if clamped then
			r.report.clamped += 1
		end
		rec[slot] = out
		return true
	elseif s == "text" then
		local out, _, ok = scanStored(r, -1)
		if not ok then
			return false
		end
		rec[slot] = out
		return true
	elseif s == "bytes" then
		local ok, n = readBase64Into(r, rec[slot], fd.bound)
		if not ok then
			return false
		end
		rec[slot .. "Length"] = n
		return true
	elseif s == "map" then
		return readMap(r, fd, rec, depth)
	elseif s == "arr" then
		if fd.array == "keyed" then
			return readKeyed(r, fd, rec, depth)
		end
		local tl = textListOf(fd)
		if tl ~= nil then
			return readTextList(r, fd, tl, rec, depth)
		end
		return readArray(r, fd, rec, depth)
	end
	return readScalar(r, fd, rec, slot, depth, false, fd.name)
end

readTableKeys = function(r: Reader, d: any, rec: any, depth: number, firstKey: string?): boolean
	local keys = keyIndex(d)
	local seen: { [number]: boolean } = {}
	local first = firstKey
	while true do
		local key: string
		if first ~= nil then
			key = first
			first = nil
		else
			local c = peek(r)
			if c == 125 then
				r.pos += 1
				return true
			end
			if c == 0 then
				r.bad = true
				return false
			end
			local k, _, ok = scanString(r, MAX_KEY)
			if not ok then
				return false
			end
			if peek(r) ~= 58 then
				r.bad = true
				return false
			end
			r.pos += 1
			key = k
		end
		if #key > 0 and string.byte(key, 1) == 38 then
			r.bad = true -- the reserved `&` prefix out of place (§16.7)
			return false
		end
		local i = keys[key]
		if i == nil then
			r.report.unknown += 1
			if not skipValue(r, depth + 1) then
				return false
			end
		else
			if seen[i] then
				r.report.duplicate += 1
			end
			seen[i] = true
			local fd = d.fields[i]
			local s = valueShape(r)
			if s == "z" and takesNull(fd) then
				if not literal(r, "null") then
					return false
				end
				resetField(d, fd, rec)
			else
				if s ~= shape(fd) then
					r.report.kindMismatch += 1
					if not skipValue(r, depth + 1) then
						return false
					end
				elseif not readField(r, d, fd, rec, depth) then
					nameZero(r, d.name, fd)
					return false
				end
				-- PRESENCE of the KEY is presence (§16.2)
				if fd.present ~= nil then
					rec[fd.present] = true
				end
			end
		end
		local c = peek(r)
		if c == 44 then
			r.pos += 1
		elseif c == 125 then
			r.pos += 1
			return true
		else
			r.bad = true
			return false
		end
	end
end

readTable = function(r: Reader, d: any, rec: any, depth: number): boolean
	if depth > MAX_DEPTH then
		r.bad = true
		return false
	end
	if d.jsonArray then
		-- THE ARRAY ROOT (§16.10): the table's text is its one field's array
		if #d.fields ~= 1 then
			r.bad = true
			return false
		end
		if valueShape(r) ~= "a" then
			if r.report.reason == nil then
				r.report.reason = "json_root_not_array: " .. d.name
			end
			r.bad = true
			return false
		end
		if not readField(r, d, d.fields[1], rec, depth) then
			nameZero(r, d.name, d.fields[1])
			return false
		end
		return true
	end
	if peek(r) ~= 123 then
		r.bad = true
		return false
	end
	r.pos += 1
	return readTableKeys(r, d, rec, depth, nil)
end

local function resetReport(report: any)
	report.unknown = 0
	report.kindMismatch = 0
	report.widened = 0
	report.clamped = 0
	report.duplicate = 0
	report.malformed = false
	report.reason = nil
	report.blobShared = 0
end

-- fromJson(d, rec, text, report): FromJson (docs/SPEC-TABLES.md §16.1): ONE
-- instance from ONE text. A text that is not JSON (or refuses by name) makes
-- the read return false with report.malformed set, and the instance is reset
-- to its declared defaults (§6.5). The report receives THIS read's counts.
function J.fromJson(d: any, rec: any, text: any, report: any?): boolean
	local rep: any = report or {}
	resetReport(rep)
	local t: string = if type(text) == "buffer" then buffer.tostring(text) else text
	local r: Reader = { t = t, n = #t, pos = 1, bad = false, zero = false, report = rep, graph = if d.variable then {} else nil }
	-- a leading byte-order mark, and nowhere else (§16.12)
	if string.sub(t, 1, 3) == "\239\187\191" then
		r.pos = 4
	end
	local ok = readTable(r, d, rec, 0)
	if ok then
		space(r)
		if r.pos ~= r.n + 1 then
			r.bad = true -- data after the root object is not one text
		end
	end
	if r.bad or not ok then
		rep.malformed = true
		local fresh = d.new()
		for k, v in fresh do
			rec[k] = v
		end
		return false
	end
	return true
end

----------------------------------------------------------------------------
-- THE WRITER (docs/SPEC-TABLES.md §16.1, §16.3): every field in declaration
-- order, defaults included, two-space indent, one trailing newline; -1 (nil
-- here) for a value no text spells.
----------------------------------------------------------------------------

type W = {
	parts: { string },
	err: string?,
	count: { [any]: number },
	labels: { [any]: number },
	blobs: { [any]: number },
	next: number,
}

local INDENT: { string } = {}
local function line(w: W, depth: number)
	local s = INDENT[depth + 1]
	if s == nil then
		s = "\n" .. string.rep("  ", depth)
		INDENT[depth + 1] = s
	end
	table.insert(w.parts, s)
end

local function emit(w: W, s: string)
	table.insert(w.parts, s)
end

local function fail(w: W, why: string): boolean
	if w.err == nil then
		w.err = why
	end
	return false
end

local function guardHolds(fd: any, rec: any): boolean
	local guard = fd.guard
	if guard == nil then
		return true
	end
	for _, term in guard do
		if rec[term[1]] ~= term[2] then
			return false
		end
	end
	return true
end

local function written(fd: any, rec: any): boolean
	if not guardHolds(fd, rec) then
		return false
	end
	return fd.present == nil or rec[fd.present] == true
end

-- PASS ONE (§16.7): how many written slots name each node, cycles refused
local countFields: (W, any, any, { [any]: boolean }) -> boolean
local countField: (W, any, any, any, { [any]: boolean }) -> boolean

local function countNode(w: W, node: any, t: string, open: { [any]: boolean }): boolean
	if node == nil or node == false then
		return true
	end
	w.count[node] = (w.count[node] or 0) + 1
	if open[node] then
		return fail(w, "data cycle: a " .. t .. " reaches a node whose descent is still open (docs/SPEC-TABLES.md §3.1, §16.7)")
	end
	if w.count[node] > 1 then
		return true
	end
	open[node] = true
	if not countFields(w, St.lookup(t), node, open) then
		return false
	end
	open[node] = false
	return true
end

local function countValue(w: W, e: any, v: any, open: { [any]: boolean }): boolean
	local s = e.s
	if s == "ptr" then
		return countNode(w, v, e.t, open)
	elseif s == "bblob" then
		if v ~= nil and v ~= false then
			w.blobs[v] = (w.blobs[v] or 0) + 1
		end
		return true
	elseif s == "union" then
		if v.Type ~= 0 then
			local ud = St.lookup(e.t)
			local arm = ud.arms[v.Type]
			if arm ~= nil and arm.s ~= "void" then
				return countField(w, ud, arm, v, open)
			end
		end
		return true
	elseif s == "tab" then
		return countFields(w, St.lookup(e.t), v, open)
	end
	return true
end

countField = function(w: W, d: any, fd: any, rec: any, open: { [any]: boolean }): boolean
	local s = fd.s
	local v = rec[fd.slot]
	if s == "map" then
		local ed = St.lookup(fd.t)
		for _, entry in v do
			if not countFields(w, ed, entry, open) then
				return false
			end
		end
		return true
	elseif s == "arr" then
		local n = fd.bound
		if fd.array == "counted" then
			n = rec[fd.slot .. "Count"]
		elseif fd.array == "list" then
			n = #v
		end
		local e = fd.e
		if e.buf or textListOf(fd) ~= nil and e.s == "str" then
			return true
		end
		for i = 1, n do
			if not countValue(w, e, v[i], open) then
				return false
			end
		end
		return true
	end
	return countValue(w, fd, v, open)
end

countFields = function(w: W, d: any, rec: any, open: { [any]: boolean }): boolean
	for _, fd in d.fields do
		if written(fd, rec) then
			if not countField(w, d, fd, rec, open) then
				return false
			end
		end
	end
	return true
end

local writeValue: (W, any, any, number) -> boolean
local writeField: (W, any, any, any, number) -> boolean
local writeFields: (W, any, any, number, boolean) -> (boolean, boolean)

local function writePointer(w: W, node: any, t: string, depth: number): boolean
	if node == nil or node == false then
		emit(w, "null")
		return true
	end
	local d = St.lookup(t)
	if (w.count[node] or 0) <= 1 then
		return writeValue(w, d, node, depth)
	end
	if depth > MAX_DEPTH then
		return fail(w, "table " .. t .. ": nested past the depth cap of 128 (docs/SPEC-TABLES.md §16.7)")
	end
	local label = w.labels[node]
	if label ~= nil then
		emit(w, "{")
		line(w, depth + 1)
		emit(w, "\"&node\": " .. string.format("%d", label))
		line(w, depth)
		emit(w, "}")
		return true
	end
	w.next += 1
	label = w.next
	w.labels[node] = label
	emit(w, "{")
	line(w, depth + 1)
	emit(w, "\"&node\": " .. string.format("%d", label))
	local before = #w.parts
	local ok = writeFields(w, d, node, depth, true)
	if not ok then
		return false
	end
	if #w.parts == before then
		return fail(w, "table " .. t .. ": a shared node with no field to write has no definition the text form can spell (docs/SPEC-TABLES.md §16.7)")
	end
	line(w, depth)
	emit(w, "}")
	return true
end

local function writeInteger(fd: any, v: any): string
	if isWide(fd) then
		local raw: any = v
		local signed = fd.signed
		local n = if fd.kw == 16 then 4 else 2
		if fd.cls == "fx" then
			raw = Int64.fromNumberRaw(v)
			signed = true
			n = 2
		end
		local negative, mag = signedOf(raw, signed, n)
		return formatWide(negative, mag, fd.fbits)
	end
	if type(v) == "number" then
		return string.format("%d", v)
	end
	return tostring(v)
end

-- writeScalar: one value: a union, a nested object, a vocabulary, a number
local function writeScalar(w: W, fd: any, v: any, depth: number): boolean
	local s = fd.s
	if s == "union" then
		local ud = St.lookup(fd.t)
		local tag = v.Type
		if tag == 0 then
			emit(w, "{}")
			return true
		end
		local arm = ud.arms[tag]
		if arm == nil then
			return fail(w, "union " .. ud.name .. ": tag " .. tostring(tag) .. " names no arm (docs/SPEC-TABLES.md §5)")
		end
		emit(w, "{")
		line(w, depth + 1)
		writeString(w :: any, arm.name)
		emit(w, ": ")
		if arm.s == "void" then
			emit(w, "null")
		elseif not writeField(w, ud, arm, v, depth + 1) then
			return false
		end
		line(w, depth)
		emit(w, "}")
		return true
	elseif s == "ptr" then
		return writePointer(w, v, fd.t, depth)
	elseif s == "tab" then
		return writeValue(w, St.lookup(fd.t), v, depth)
	elseif s == "enum" then
		if v == 0 then
			writeString(w :: any, "None")
			return true
		end
		local variant = fd.vs[v]
		if variant == nil then
			return fail(w, "enum " .. tostring(fd.t) .. ": value " .. tostring(v) .. " names no variant (docs/SPEC-TABLES.md §5)")
		end
		if variant[3] then
			return fail(w, "enum " .. tostring(fd.t) .. ": value " .. tostring(v) .. " names the deprecated variant " .. variant[1] .. " — a strict write refuses it (docs/SPEC-TABLES.md §2.15)")
		end
		writeString(w :: any, variant[1])
		return true
	end
	local names = flagsOf(fd)
	if names ~= nil then
		local low, high = v[1], v[2]
		if low == 0 and high == 0 then
			emit(w, "[]")
			return true
		end
		emit(w, "[")
		local first = true
		for bit = 0, 63 do
			local set = if bit < 32 then bit32.btest(low, POW2[bit]) else bit32.btest(high, POW2[bit - 32])
			if set then
				if bit >= #names then
					return fail(w, "flags: bit " .. tostring(bit) .. " names no variant (docs/SPEC-TABLES.md §16.2)")
				end
				if not first then
					emit(w, ",")
				end
				first = false
				line(w, depth + 1)
				writeString(w :: any, names[bit + 1])
			end
		end
		line(w, depth)
		emit(w, "]")
		return true
	end
	if s == "bool" then
		emit(w, if v then "true" else "false")
		return true
	elseif s == "float" then
		local text: string? = if fd.f32 then J.formatFloat32(v) else J.formatFloat64(v[2], v[1])
		if text == nil then
			return fail(w, "a float that is not finite has no JSON spelling (docs/SPEC-TABLES.md §16.3)")
		end
		emit(w, text :: string)
		return true
	elseif fd.timestamp then
		writeString(w :: any, formatTimestamp(v))
		return true
	end
	emit(w, writeInteger(fd, v))
	return true
end

local function writeArray(w: W, fd: any, rec: any, depth: number): boolean
	local v = rec[fd.slot]
	local e = fd.e
	local n = fd.bound
	if fd.array == "counted" then
		n = rec[fd.slot .. "Count"]
	elseif fd.array == "list" then
		n = #v
	end
	if n == 0 then
		emit(w, "[]")
		return true
	end
	local tl = textListOf(fd)
	emit(w, "[")
	for i = 1, n do
		if i > 1 then
			emit(w, ",")
		end
		line(w, depth + 1)
		if tl ~= nil then
			local el = v[i]
			if tl.opt and not el.ValuePresent then
				emit(w, "null")
			else
				writeString(w :: any, el.Value)
			end
		elseif e.buf then
			emit(w, string.format("%d", buffer.readu8(v, i - 1)))
		elseif not writeScalar(w, e, v[i], depth + 1) then
			return false
		end
	end
	line(w, depth)
	emit(w, "]")
	return true
end

local function writeKeyed(w: W, fd: any, rec: any, depth: number): boolean
	local v = rec[fd.slot]
	local n = fd.bound
	if n <= 0 then
		emit(w, "{}")
		return true
	end
	emit(w, "{")
	for i = 1, n do
		if i > 1 then
			emit(w, ",")
		end
		line(w, depth + 1)
		local variant = fd.keyvs[i]
		if variant == nil then
			return fail(w, "enum-keyed array " .. fd.name .. ": slot " .. tostring(i - 1) .. " belongs to no variant (docs/SPEC-TABLES.md §3.2)")
		end
		writeString(w :: any, variant[1])
		emit(w, ": ")
		if not writeScalar(w, fd.e, v[i], depth + 1) then
			return false
		end
	end
	line(w, depth)
	emit(w, "}")
	return true
end

local function writeMap(w: W, fd: any, rec: any, depth: number): boolean
	local entries = rec[fd.slot]
	if #entries == 0 then
		emit(w, "{}")
		return true
	end
	local ed = St.lookup(fd.t)
	local kd, vd = ed.fields[1], ed.fields[2]
	local ks, cls, signed = kd.slot, kd.cls, kd.signed == true
	local order = table.clone(entries)
	table.sort(order, function(a: any, b: any): boolean
		return keyLess(a[ks], b[ks], cls, signed)
	end)
	emit(w, "{")
	for i, entry in order do
		if i > 1 then
			emit(w, ",")
		end
		line(w, depth + 1)
		local k = entry[ks]
		writeString(w :: any, if type(k) == "string" then k else writeInteger(kd, k))
		emit(w, ": ")
		if vd.present ~= nil and not entry[vd.present] then
			emit(w, "null")
		elseif not writeField(w, ed, vd, entry, depth + 1) then
			return false
		end
	end
	line(w, depth)
	emit(w, "}")
	return true
end

writeField = function(w: W, d: any, fd: any, rec: any, depth: number): boolean
	local s = fd.s
	local v = rec[fd.slot]
	if s == "sblob" then
		if v == nil then
			emit(w, "null")
		else
			writeString(w :: any, buffer.tostring(v))
		end
		return true
	elseif s == "bblob" then
		if v == nil then
			emit(w, "null")
			return true
		end
		if (w.blobs[v] or 0) > 1 then
			return fail(w, "field " .. fd.name .. " names a byte buffer another slot names: a shared blob has no spelling (docs/SPEC-TABLES.md §16.7)")
		end
		writeBase64(w :: any, v, buffer.len(v))
		return true
	elseif s == "str" then
		if fd.wide then
			writeWString(w :: any, v, rec[fd.slot .. "Length"])
		else
			writeString(w :: any, v)
		end
		return true
	elseif s == "text" then
		writeString(w :: any, v or "")
		return true
	elseif s == "bytes" then
		writeBase64(w :: any, v, rec[fd.slot .. "Length"])
		return true
	elseif s == "map" then
		return writeMap(w, fd, rec, depth)
	elseif s == "arr" then
		if fd.array == "keyed" then
			return writeKeyed(w, fd, rec, depth)
		end
		return writeArray(w, fd, rec, depth)
	end
	return writeScalar(w, fd, v, depth)
end

writeFields = function(w: W, d: any, rec: any, depth: number, any: boolean): (boolean, boolean)
	for _, fd in d.fields do
		if written(fd, rec) then
			emit(w, if any then "," else "{")
			any = true
			line(w, depth + 1)
			writeString(w :: any, fd.json)
			emit(w, ": ")
			if not writeField(w, d, fd, rec, depth + 1) then
				return false, any
			end
		end
	end
	return true, any
end

writeValue = function(w: W, d: any, rec: any, depth: number): boolean
	if depth > MAX_DEPTH then
		return fail(w, "table " .. d.name .. ": nested past the depth cap of 128 (docs/SPEC-TABLES.md §16.7)")
	end
	if d.jsonArray then
		return writeField(w, d, d.fields[1], rec, depth)
	end
	local ok, any = writeFields(w, d, rec, depth, false)
	if not ok then
		return false
	end
	if not any then
		emit(w, "{}")
		return true
	end
	line(w, depth)
	emit(w, "}")
	return true
end

-- toJson(d, rec): ToJson's text (docs/SPEC-TABLES.md §16.1), ending in exactly
-- one newline, or nil and the reason no text spells the value (§16.3)
function J.toJson(d: any, rec: any): (string?, string?)
	local w: W = { parts = {}, err = nil, count = {}, labels = {}, blobs = {}, next = 0 }
	local open: { [any]: boolean } = { [rec] = true }
	if not countFields(w, d, rec, open) then
		return nil, w.err
	end
	if not writeValue(w, d, rec, 0) then
		return nil, w.err
	end
	emit(w, "\n")
	return table.concat(w.parts), nil
end

-- toJsonMeasure(d, rec): the exact bytes ToJson writes, -1 when it refuses
function J.toJsonMeasure(d: any, rec: any): number
	local text = J.toJson(d, rec)
	return if text == nil then -1 else #text
end

-- toJsonInto(d, rec, out): ToJson into a caller's buffer: the size, or -1 for
-- a value no text spells or a buffer too small for the text
function J.toJsonInto(d: any, rec: any, out: buffer): number
	local text = J.toJson(d, rec)
	if text == nil or #text > buffer.len(out) then
		return -1
	end
	buffer.writestring(out, 0, text)
	return #text
end

return table.freeze(J)
