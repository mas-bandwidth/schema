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
local TWO32 = 4294967296

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

-- bnTo32(a, n): the low n 32-bit limbs of a big integer
local function bnTo32(a: Big, n: number): { number }
	local out: { number } = table.create(n, 0)
	local acc, held, j = 0, 0, 1
	for i = 1, #a do
		acc += a[i] * POW2[held]
		held += 24
		while held >= 32 do
			if j > n then
				return out
			end
			local q = acc // TWO32
			out[j] = acc - q * TWO32
			acc = q
			held -= 32
			j += 1
		end
	end
	if held > 0 and j <= n then
		out[j] = acc
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

J._expansion = expansion
J._bnDecimal = bnDecimal
J._bnFrom32 = bnFrom32
J._bnTo32 = bnTo32
J._Int64 = Int64
J._Uint64 = Uint64
J._Int128 = Int128
J._Uint128 = Uint128

return J
