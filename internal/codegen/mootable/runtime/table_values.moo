--!strict
-- table_values.moo — the moo table runtime's VALUES: the one place every form
-- (load, save, fixed, message, block, cook, retain) turns wire bytes into the
-- storage docs/PORT-MOO.md's THE VALUE MAPPING names and back. moo's one
-- number is Q48.16 fixed point, exact for every integer below 2^47, with no
-- float and no 64-bit integer, so:
--
--   - a value of a kind four bytes wide or narrower is a moo number;
--   - an eight- or sixteen-byte kind's raw value is serialize.moo's frozen
--     wide class (Int64, Uint64, Int128, Uint128) of the kind's signedness;
--   - a fixed(I, 16) field whose bounds moo holds is stored as moo's own
--     Q48.16 number in whole units (cls "fx"), converted at the edge;
--   - a float is its IEEE bit pattern: binary32 a number, binary64 a Uint64,
--     and the finite rule (docs/SPEC-TABLES.md §3.3) is tested on the bits.
--
-- A wire id is its eight little-endian bytes as a string (one readstring, a
-- hashable key); no id is ever a wide value.
-- SPDX-License-Identifier: NONE — emitted output, yours under terms of your choice.

local serialize = require("./serialize")

local Int64 = serialize.Int64
local Uint64 = serialize.Uint64
local Int128 = serialize.Int128
local Uint128 = serialize.Uint128

local TWO16 = 65536
local TWO31 = 2147483648
local TWO32 = 4294967296

local V = {}

-- the largest integer a moo number holds exactly: a LEB128 value past it is
-- held here, sticky, so every bounds test against a buffer refuses it
V.HUGE = 140737488355327

-- THE RESERVED IDS (docs/SPEC-TABLES.md §3.1, §5)
V.NodeTableId = "\xff\xff\xff\xff\xff\xff\xff\xff"
V.BuildVersionId = "\xfe\xff\xff\xff\xff\xff\xff\xff"
V.MessageVocabularyId = "\xfd\xff\xff\xff\xff\xff\xff\xff"
V.StringBlobId = "\x58\xfc\xaf\xfa\xd8\xe0\x4b\x70" -- FNV-1a 64 of "string"
V.BytesBlobId = "\xe4\x4f\x1c\x4f\x47\xc0\x2e\x2f" -- FNV-1a 64 of "bytes"
V.MapKeyId = "\xec\x10\x5b\x36\x19\x4a\xc9\x3d" -- FNV-1a 64 of "key"

local POW7: { number } = {}
do
	local p = 1
	for i = 0, 6 do
		POW7[i] = p
		p *= 128
	end
end

-- leb(b, at, e): a canonical LEB128 value and the position after it, or nil
-- (and at) when it does not read: past e, a tenth byte past 1, a non-final
-- zero continuation (docs/SPEC-TABLES.md §3). A value past 2^47 - 1 reads as
-- V.HUGE, which no length, count or index can be.
function V.leb(b: buffer, at: number, e: number): (number?, number)
	local v = 0
	local mul = 1
	local huge = false
	for i = 0, 9 do
		if at + i >= e then
			return nil, at
		end
		local x = buffer.readu8(b, at + i)
		if i == 9 and x > 1 then
			return nil, at
		end
		local low = x % 128
		if not huge then
			if i >= 7 then
				if low ~= 0 then
					huge = true
				end
			else
				local add = low * mul
				if v + add > V.HUGE - 1 then
					huge = true
				else
					v += add
				end
			end
		end
		if x < 128 then
			if i > 0 and x == 0 then
				return nil, at
			end
			return if huge then V.HUGE else v, at + i + 1
		end
		mul = if i < 6 then POW7[i + 1] else mul
	end
	return nil, at
end

-- lebLen(v): the bytes the canonical spelling of v takes
function V.lebLen(v: number): number
	local n = 1
	while v >= 128 do
		n += 1
		v = v // 128
	end
	return n
end

-- lebString(v): the canonical spelling of v as a string
function V.lebString(n: number): string
	local out = {}
	repeat
		local x = n % 128
		n = n // 128
		if n > 0 then
			x += 128
		end
		out[#out + 1] = string.char(x)
	until n == 0
	return table.concat(out)
end

-- width(kind): a scalar kind's payload width in bytes, 0 for a framed kind
function V.width(kind: number): number
	if kind == 1 or kind == 2 or kind == 6 or kind == 20 or kind == 25 then
		return 1
	elseif kind == 3 or kind == 7 or kind == 21 or kind == 26 then
		return 2
	elseif kind == 4 or kind == 8 or kind == 10 or kind == 22 or kind == 27 then
		return 4
	elseif kind == 5 or kind == 9 or kind == 11 or kind == 23 or kind == 28 or kind == 36 then
		return 8
	elseif kind == 18 or kind == 19 or kind == 24 or kind == 29 then
		return 16
	end
	return 0
end

-- signedKind(kind): whether a scalar kind's bytes are two's complement
function V.signedKind(kind: number): boolean
	return (kind >= 2 and kind <= 5) or kind == 18 or (kind >= 20 and kind <= 24) or kind == 36
end

-- widens(kind, declared): THE WIDENING RUNGS (§4): a narrower kind on its own
-- signedness ladder, the 128-bit rungs, and f32 into f64.
function V.widens(kind: number, declared: number): boolean
	if declared == 3 or declared == 4 or declared == 5 then
		return kind >= 2 and kind < declared
	end
	if declared == 7 or declared == 8 or declared == 9 then
		return kind >= 6 and kind < declared
	end
	if declared == 18 then
		return kind >= 2 and kind <= 5
	end
	if declared == 19 then
		return kind >= 6 and kind <= 9
	end
	if declared == 11 then
		return kind == 10
	end
	return false
end

-- ---------------------------------------------------------------------------
-- Text.

-- utf8(b, at, n): well-formed UTF-8 with no zero byte (docs/SPEC-TABLES.md §3)
function V.utf8(b: buffer, at: number, n: number): boolean
	local i = at
	local e = at + n
	while i < e do
		local b0 = buffer.readu8(b, i)
		if b0 == 0 then
			return false
		end
		if b0 < 0x80 then
			i += 1
		else
			local need, cp
			if b0 >= 0xC2 and b0 <= 0xDF then
				need, cp = 1, b0 - 0xC0
			elseif b0 >= 0xE0 and b0 <= 0xEF then
				need, cp = 2, b0 - 0xE0
			elseif b0 >= 0xF0 and b0 <= 0xF4 then
				need, cp = 3, b0 - 0xF0
			else
				return false
			end
			if i + need >= e then
				return false
			end
			for k = 1, need do
				local x = buffer.readu8(b, i + k)
				if x < 0x80 or x > 0xBF then
					return false
				end
				cp = cp * 64 + (x - 0x80)
			end
			local min = if need == 1 then 0x80 elseif need == 2 then 0x800 else 0x10000
			if cp < min or cp > 0x10FFFF or (cp >= 0xD800 and cp <= 0xDFFF) then
				return false
			end
			i += 1 + need
		end
	end
	return true
end

local probe = buffer.create(0)

-- utf8String(s): V.utf8 over a moo string
function V.utf8String(s: string): boolean
	if #s == 0 then
		return true
	end
	if buffer.len(probe) < #s then
		probe = buffer.create(#s * 2)
	end
	buffer.writestring(probe, 0, s)
	return V.utf8(probe, 0, #s)
end

-- wide(b, at, n): n bytes of UTF-16LE code units, none zero, every surrogate
-- paired (docs/SPEC-TABLES.md §3, §4)
function V.wide(b: buffer, at: number, n: number): boolean
	local units = n // 2
	local i = 0
	while i < units do
		local u = buffer.readu16(b, at + i * 2)
		if u == 0 then
			return false
		end
		if u >= 0xD800 and u <= 0xDBFF then
			if i + 1 >= units then
				return false
			end
			local low = buffer.readu16(b, at + (i + 1) * 2)
			if low < 0xDC00 or low > 0xDFFF then
				return false
			end
			i += 2
		elseif u >= 0xDC00 and u <= 0xDFFF then
			return false
		else
			i += 1
		end
	end
	return true
end

-- A CLAMP KEEPS WHOLE CODE POINTS (docs/SPEC-TABLES.md §3, KIND 12 / 33)
function V.clampUtf8(b: buffer, at: number, keep: number): number
	while keep > 0 do
		local x = buffer.readu8(b, at + keep)
		if x < 0x80 or x > 0xBF then
			break
		end
		keep -= 1
	end
	return keep
end

function V.clampWide(b: buffer, at: number, units: number): number
	if units > 0 then
		local last = buffer.readu16(b, at + (units - 1) * 2)
		if last >= 0xD800 and last <= 0xDBFF then
			units -= 1
		end
	end
	return units
end

-- ---------------------------------------------------------------------------
-- Floats, as bit patterns (docs/PORT-MOO.md: moo holds no float).

function V.finite32(bits: number): boolean
	return bits % TWO31 < 0x7F800000
end

function V.finite64(lo: number, hi: number): boolean
	return hi % TWO31 < 0x7FF00000
end

-- f32to64(bits): the binary64 bits (lo, hi) of a FINITE binary32, exact: a
-- normal rebiases its exponent, a subnormal normalizes, a zero keeps its sign.
function V.f32to64(bits: number): (number, number)
	local sign = if bits >= TWO31 then TWO31 else 0
	local mag = bits % TWO31
	local exp = mag // 0x800000
	local frac = mag % 0x800000
	if exp == 0 then
		if frac == 0 then
			return 0, sign
		end
		-- a subnormal: shift the fraction up until its leading bit is the
		-- implicit one, lowering the exponent for each step
		local e = -126
		while frac < 0x800000 do
			frac *= 2
			e -= 1
		end
		frac -= 0x800000
		exp = e + 127
	end
	-- binary64: exponent + (1023 - 127), the 23 fraction bits at the top of 52
	local e64 = exp - 127 + 1023
	-- frac (23 bits) << 29: the high 20 bits go to hi, the low 3 to lo's top
	local hi = sign + e64 * 0x100000 + frac // 8
	local lo = (frac % 8) * 0x20000000
	return lo, hi
end

-- key64(lo, hi): a finite binary64 as two numbers ordered like its value,
-- -0 equal to +0: (sign-adjusted high, low).
local function cmp64(alo: number, ahi: number, blo: number, bhi: number): number
	local an = ahi >= TWO31
	local bn = bhi >= TWO31
	local amh = ahi % TWO31
	local bmh = bhi % TWO31
	local az = amh == 0 and alo == 0
	local bz = bmh == 0 and blo == 0
	if az and bz then
		return 0
	end
	-- magnitude comparison of |a| and |b|
	local mc = if amh ~= bmh then (if amh < bmh then -1 else 1) elseif alo ~= blo then (if alo < blo then -1 else 1) else 0
	if az then
		return if bn then 1 else -1
	end
	if bz then
		return if an then -1 else 1
	end
	if an ~= bn then
		return if an then -1 else 1
	end
	return if an then -mc else mc
end
V.cmp64 = cmp64

-- ---------------------------------------------------------------------------
-- Integers: the raw domain of a kind, and a field's storage.

-- kindClass(kw, signed): the wide class an eight- or sixteen-byte kind's raw
-- value is held in, nil for a number
function V.kindClass(kw: number, signed: boolean): any
	if kw == 16 then
		return if signed then Int128 else Uint128
	elseif kw == 8 then
		return if signed then Int64 else Uint64
	end
	return nil
end

-- readRaw(b, at, kind): the raw value of a scalar integer kind at at
function V.readRaw(b: buffer, at: number, kind: number): any
	local w = V.width(kind)
	local signed = V.signedKind(kind)
	if w == 1 then
		return if signed then buffer.readi8(b, at) else buffer.readu8(b, at)
	elseif w == 2 then
		return if signed then buffer.readi16(b, at) else buffer.readu16(b, at)
	elseif w == 4 then
		return if signed then buffer.readi32(b, at) else buffer.readu32(b, at)
	elseif w == 8 then
		local C = if signed then Int64 else Uint64
		return C.new(buffer.readu32(b, at), buffer.readu32(b, at + 4))
	end
	local C = if signed then Int128 else Uint128
	return C.new(buffer.readu32(b, at), buffer.readu32(b, at + 4), buffer.readu32(b, at + 8), buffer.readu32(b, at + 12))
end

-- widen(x, kw, signed): a raw value of a narrower kind on the same ladder into
-- the raw domain of a (kw, signed) kind: sign- or zero-extended (§4)
function V.widen(x: any, kw: number, signed: boolean): any
	local C = V.kindClass(kw, signed)
	if C == nil then
		return x
	end
	if type(x) == "number" then
		return C.fromNumber(x)
	end
	if kw == 16 and (Int64.is(x) or Uint64.is(x)) then
		return if signed then Int128.from64(x) else Uint128.from64(x)
	end
	return x
end

-- rawCmp(a, b, kw, signed): -1/0/1 of two raw values in one kind's domain
function V.rawCmp(a: any, b: any, kw: number, signed: boolean): number
	local C = V.kindClass(kw, signed)
	if C == nil then
		return if a < b then -1 elseif a > b then 1 else 0
	end
	return C.cmp(a, b)
end

-- bitsCap(x, bits, kw): bits(N)'s ceiling 2^N - 1 in the raw domain
function V.bitsCap(bits: number, kw: number): any
	if kw <= 4 then
		return 2 ^ bits - 1
	end
	return Uint64.sub(Uint64.lsh(Uint64.fromNumber(1), bits), Uint64.fromNumber(1))
end

-- toStorage(fd, x): a clamped raw value as the field's storage (cls)
function V.toStorage(fd: any, x: any): any
	local cls = fd.cls
	if cls == "fx" then
		if type(x) == "number" then
			return x / TWO16
		end
		local raw = if Uint64.is(x) then Uint64.toInt64(x) else x
		return Int64.toNumberRaw(raw) or 0
	elseif cls == "U64" then
		if type(x) == "number" then
			return Uint64.fromNumber(x)
		end
		return if Int64.is(x) then Int64.toUint64(x) else x
	elseif cls == "I64" then
		if type(x) == "number" then
			return Int64.fromNumber(x)
		end
		return if Uint64.is(x) then Uint64.toInt64(x) else x
	elseif cls == "I128" then
		if type(x) == "number" then
			return Int128.fromNumber(x)
		end
		return if Uint128.is(x) then Uint128.toInt128(x) else x
	elseif cls == "U128" then
		if type(x) == "number" then
			return Uint128.fromNumber(x)
		end
		return if Int128.is(x) then Int128.toUint128(x) else x
	end
	return x
end

local function isInt(x: any): boolean
	return type(x) == "number" and x == math.floor(x)
end

-- the range of a kind of four bytes or fewer
local function smallRange(kw: number, signed: boolean): (number, number)
	if kw == 1 then
		return if signed then -128 else 0, if signed then 127 else 255
	elseif kw == 2 then
		return if signed then -32768 else 0, if signed then 32767 else 65535
	end
	return if signed then -2147483648 else 0, if signed then 2147483647 else 4294967295
end

-- toRaw(fd, v): the field's storage as the raw value of its kind (kw,
-- signed), or nil when the value is outside its contract — a wrong storage
-- type, a fraction where an integer goes, a value its kind cannot hold —
-- which every writer refuses (docs/PORT-MOO.md, THE WIRE).
function V.toRaw(fd: any, v: any): any
	local kw, signed, cls = fd.kw, fd.signed == true, fd.cls
	if cls == "fx" then
		if type(v) ~= "number" then
			return nil
		end
		if kw <= 4 then
			local raw = v * TWO16
			local lo, hi = smallRange(kw, signed)
			if raw < lo or raw > hi then
				return nil
			end
			return raw
		end
		local raw = Int64.fromNumberRaw(v)
		if not signed then
			if v < 0 then
				return nil
			end
			return Int64.toUint64(raw)
		end
		return raw
	end
	local C = V.kindClass(kw, signed)
	if C == nil then
		-- a narrow kind: a number, or a wide storage value (a flags set at a
		-- narrow wire width) that fits it
		local x = v
		if type(v) ~= "number" then
			local W = if cls == "I64" then Int64 elseif cls == "U64" then Uint64 elseif cls == "I128" then Int128 elseif cls == "U128" then Uint128 else nil
			if W == nil or not W.is(v) then
				return nil
			end
			x = W.tryNumber(v)
			if x == nil then
				return nil
			end
		end
		if not isInt(x) then
			return nil
		end
		local lo, hi = smallRange(kw, signed)
		if x < lo or x > hi then
			return nil
		end
		return x
	end
	if type(v) == "number" then
		if not isInt(v) or (not signed and v < 0) then
			return nil
		end
		return C.fromNumber(v)
	end
	if C.is(v) then
		return v
	end
	-- the other class of the same width reinterprets (Int64 storage riding an
	-- unsigned kind, never in generated descriptors, held for completeness)
	if kw == 8 and (Int64.is(v) or Uint64.is(v)) then
		return if signed then Uint64.toInt64(v) else Int64.toUint64(v)
	end
	if kw == 16 and (Int128.is(v) or Uint128.is(v)) then
		return if signed then Uint128.toInt128(v) else Int128.toUint128(v)
	end
	return nil
end

-- writeRaw(b, at, kw, signed, x): a raw value at its kind's width, little-endian
function V.writeRaw(b: buffer, at: number, kw: number, signed: boolean, x: any)
	if kw == 1 then
		if signed then
			buffer.writei8(b, at, x)
		else
			buffer.writeu8(b, at, x)
		end
	elseif kw == 2 then
		if signed then
			buffer.writei16(b, at, x)
		else
			buffer.writeu16(b, at, x)
		end
	elseif kw == 4 then
		if signed then
			buffer.writei32(b, at, x)
		else
			buffer.writeu32(b, at, x)
		end
	else
		for i = 1, kw // 4 do
			buffer.writeu32(b, at + (i - 1) * 4, x[i])
		end
	end
end

-- sameStorage(fd, a, b): whether two storage values of one field are the
-- same value (an integer in either spelling, a float by its BITS: -0 and +0
-- differ, docs/SPEC-TABLES.md §3.3)
function V.sameStorage(fd: any, a: any, b: any): boolean
	if type(a) == "number" and type(b) == "number" then
		return a == b
	end
	if a == nil or b == nil then
		return a == b
	end
	if type(a) == "boolean" or type(b) == "boolean" then
		return a == b
	end
	local cls = fd.cls
	local C = if cls == "I64" then Int64 elseif cls == "U64" then Uint64 elseif cls == "I128" then Int128 elseif cls == "U128" then Uint128 else nil
	if fd.s == "float" and not fd.f32 then
		C = Uint64
	end
	if C == nil then
		return false
	end
	local ok, r = pcall(C.cmp, a, b)
	return ok and r == 0
end

-- ---------------------------------------------------------------------------
-- CRC-32C, the Castagnoli polynomial, reflected 0x82F63B78, initial and final
-- 0xFFFFFFFF (docs/SPEC-TABLES.md §2.14), by a byte table built once.

local CRC: { number } = table.create(256, 0)
for i = 0, 255 do
	local c = i
	for _ = 1, 8 do
		if bit32.band(c, 1) ~= 0 then
			c = bit32.bxor(bit32.rshift(c, 1), 0x82F63B78)
		else
			c = bit32.rshift(c, 1)
		end
	end
	CRC[i + 1] = c
end

function V.crc32c(b: buffer, n: number): number
	local crc = 0xFFFFFFFF
	for i = 0, n - 1 do
		crc = bit32.bxor(CRC[bit32.band(bit32.bxor(crc, buffer.readu8(b, i)), 0xFF) + 1], bit32.rshift(crc, 8))
	end
	return bit32.bxor(crc, 0xFFFFFFFF)
end

-- idHex(id): an id this build cannot name, spelled #<hex> big-endian
-- (docs/SPEC-TABLES.md §11: a refusal names the field)
function V.idHex(id: string): string
	local out = { "#" }
	for i = 8, 1, -1 do
		out[#out + 1] = string.format("%02x", string.byte(id, i) :: number)
	end
	return table.concat(out)
end

V.TWO32 = TWO32
V.Int64 = Int64
V.Uint64 = Uint64
V.Int128 = Int128
V.Uint128 = Uint128

return table.freeze(V)
