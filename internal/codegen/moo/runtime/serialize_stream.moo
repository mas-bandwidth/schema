--!strict
-- serialize_stream.moo -- the three streams, WriteStream, ReadStream and
-- MeasureStream (DESIGN.md, "The API" and "Writer and reader"), wire compatible
-- with serialize (STANDARD.md, format version 2.0) and serialize.go.
--
-- Every serialize method exists on every stream, and one function implements
-- each method for all three, branching on the stream's kind exactly where
-- serialize.go's three implementations differ. The order inside each is
-- serialize.go's: raise on API misuse (before anything else, even on a stream
-- with a latched error), refuse on a latched stream, the refusals, the bounds
-- check of the whole width, then the bits; a read assigns its holder last.
-- On a READ, a bad caller declaration (a bit count, a range, a Q format, a
-- buffer size, a compressed float declaration) is refused as
-- value_out_of_range through the one latch, never raised: a reader never
-- traps (IMPLEMENTING.md U7, U16).
--
-- The writer is STRICT: serialize.go's bytes for the same values. A reader
-- refuses everything STANDARD.md and serialize.go refuse, each refusal naming
-- its reason from STANDARD.md's closed list (Refusal), and one thing more that
-- only a moo reader can meet: a finite IEEE value with no moo number (the
-- float boundary, DESIGN.md), refused as value_out_of_range. Hostile bytes
-- never raise; a raise is API misuse, decided by the caller's own arguments.
--
-- Copyright © 2016 - 2026, Más Bandwidth LLC. BSD 3-Clause (see LICENSE).

local Bitpacker = require("./serialize_bitpacker")
local Wide = require("./serialize_wide")
local Float = require("./serialize_float")

local MASK = Bitpacker.MASK
local TWO32 = 4294967296

local Uint64, Int64, Uint128, Int128 = Wide.Uint64, Wide.Int64, Wide.Uint128, Wide.Int128

export type Holder<T> = { value: T }

-- The refusal reasons (STANDARD.md, Refusal reasons): a closed list, spelled
-- exactly as every implementation spells them. A stream latches the FIRST
-- refusal's reason; later refusals on a failed stream are its echoes. A write
-- latches past_end when the buffer is full, value_out_of_range for a value
-- outside the range it is serialized with, and non_finite_float for a float
-- or double bit pattern that is NaN or an infinity, as serialize.go's writer.
-- rejected (standard 2.1) is the one reason the wire never chooses: the
-- caller's own refusal of a value whose bytes were well formed (reject, or a
-- nested object whose serialize returns false; IMPLEMENTING.md U2).
local Refusal = table.freeze({
	PastEnd = "past_end",
	ValueOutOfRange = "value_out_of_range",
	NonzeroPadding = "nonzero_padding",
	TrailingData = "trailing_data",
	NonFiniteFloat = "non_finite_float",
	InvalidUtf8 = "invalid_utf8",
	EmbeddedNul = "embedded_nul",
	NonCanonical = "non_canonical",
	Rejected = "rejected",
})
local PAST_END = Refusal.PastEnd
local RANGE = Refusal.ValueOutOfRange
local NONZERO_PADDING = Refusal.NonzeroPadding
local TRAILING_DATA = Refusal.TrailingData
local NON_FINITE = Refusal.NonFiniteFloat
local INVALID_UTF8 = Refusal.InvalidUtf8
local EMBEDDED_NUL = Refusal.EmbeddedNul
local NON_CANONICAL = Refusal.NonCanonical
local REJECTED = Refusal.Rejected

-- serialize.go's panic texts (serialize.go, the panic constants).
local RAISE_BITS_RANGE = "serialize: bits must be in [1,32]"
local RAISE_BITS_RANGE64 = "serialize: bits must be in [1,64]"
local RAISE_MIN_MAX = "serialize: min must not be greater than max"
local RAISE_INT_RELATIVE_DOMAIN = "serialize: int relative previous must be in [0,2147483647]"
local RAISE_FIXED_PARAMS = "serialize: fixed point requires integer bits >= 1, fraction bits >= 0, a Q format that fits the storage width, and min <= max"
local RAISE_FIXED_BOUNDS = "serialize: fixed point bounds in whole units do not fit the Q format"
local RAISE_BUFFER_SIZE = "serialize: string buffer size must be in [1,1<<31)"
local RAISE_FLOAT_PARAMS = "serialize: compressed float requires min < max and resolution > 0"
local RAISE_PRECOMPUTED = "serialize: precomputed compressed float constants must be exactly what CompressedFloatParams derives"
local RAISE_NOT_FINITE = "serialize: compressed float value must be finite"
local RAISE_DECLARATION_FINITE = "serialize: compressed float declaration's delta and delta / res must be finite in float32"
local RAISE_COMPRESSED_DECODE = "serialize: compressed float declaration decodes its top integer to a value that is not finite"
local RAISE_STRING_TEXT = "serialize: serializeString value must be well-formed UTF-8 with no zero byte"
local RAISE_BUFFER_BYTES = "serialize: bit writer buffer size must be a multiple of 8 bytes"
local RAISE_NOT_AN_OBJECT = "serialize: serializeObject requires a table with a serialize method"
local RAISE_REJECT_NOT_READ = "serialize: reject is read only; a write or measure stream has nothing to refuse"

-- ---------------------------------------------------------------------------
-- Argument checks: a value serialize.go's parameter TYPE could not hold is the
-- moo form of a Go compile error, and raises naming the method and argument.

local function isInteger(x: any, lo: number, hi: number): boolean
	return type(x) == "number" and x >= lo and x <= hi and x == math.floor(x)
end

local function need(cond: boolean, method: string, name: string, kind: string)
	if not cond then
		error("serialize: " .. method .. " " .. name .. " must be " .. kind, 3)
	end
end

local function int32Arg(x: any, method: string, name: string): number
	need(isInteger(x, -2147483648, 2147483647), method, name, "an int32")
	return x
end

local function uintArg(x: any, bits: number, method: string, name: string): number
	need(isInteger(x, 0, MASK[bits]), method, name, "a uint" .. bits)
	return x
end

-- wideArg: a wide value of class C, or an integer-valued number the class
-- holds (a negative number for an unsigned class does not), as limbs.
local function wideArg(C: any, x: any, method: string, name: string): { number }
	if type(x) == "number" then
		need(x == math.floor(x) and (C.signed or x >= 0), method, name, C.name .. " or an integer-valued number")
		return Wide.fromNumber(x, C.limbs)
	end
	need(C.is(x), method, name, C.name .. " or an integer-valued number")
	return x :: any
end

-- misuse: a bad caller declaration (IMPLEMENTING.md U7, U16). On a read it is
-- refused as value_out_of_range through the one latch, so a stream that already
-- failed keeps its first reason, and returns false; on a write or a measure it
-- raises message (moo's one write-misuse mechanism, U9), level counted from
-- misuse's caller as error counts it.
local function misuse(s: any, message: string, level: number): boolean
	if s._kind == "read" then
		return s:_fail(RANGE)
	end
	error(message, level + 1)
end

-- bitsArg: true when bits is an integer in [1, max]; else misuse.
local function bitsArg(s: any, bits: any, max: number, message: string): boolean
	if not isInteger(bits, 1, max) then
		return misuse(s, message, 3)
	end
	return true
end

-- needDecl: need, for a declaration: misuse on a read refuses, never raises.
local function needDecl(s: any, cond: boolean, method: string, name: string, kind: string): boolean
	if not cond then
		return misuse(s, "serialize: " .. method .. " " .. name .. " must be " .. kind, 3)
	end
	return true
end

-- wideDecl: wideArg, for a declaration: the limbs, or nil after misuse.
local function wideDecl(s: any, C: any, x: any, method: string, name: string): { number }?
	local ok = if type(x) == "number" then x == math.floor(x) and (C.signed or x >= 0) else C.is(x)
	if not ok then
		misuse(s, "serialize: " .. method .. " " .. name .. " must be " .. C.name .. " or an integer-valued number", 3)
		return nil
	end
	if type(x) == "number" then
		return Wide.fromNumber(x, C.limbs)
	end
	return x :: any
end

-- ---------------------------------------------------------------------------
-- What every stream shares.

local Shared: any = {}

function Shared.isWriting(self: any): boolean
	return self._kind ~= "read"
end

function Shared.isReading(self: any): boolean
	return self._kind == "read"
end

-- error returns the first refusal latched on the stream, a Refusal reason
-- (or, after serializeObject, the object's own error value), or nil.
function Shared.error(self: any): any
	return self._error
end

function Shared.ok(self: any): boolean
	return self._error == nil
end

function Shared.bitsProcessed(self: any): number
	return self._bits
end

-- bytesProcessed: the bits processed rounded up to a byte; after a write, the
-- packet size (STANDARD.md, Flush).
function Shared.bytesProcessed(self: any): number
	return (self._bits + 7) // 8
end

function Shared.setContext(self: any, context: any)
	self._context = context
end

function Shared.context(self: any): any
	return self._context
end

-- the latch (STANDARD.md, Reader Obligations, failure is terminal). Only a
-- read latches. On a write or a measure every path here is write misuse (a
-- value out of range, a non-finite float, a string past its buffer, a write
-- past the room left), which traps and never latches (IMPLEMENTING.md U9, U6):
-- a write or measure stream has no refusal state for misuse to set.
local TRAP: { [string]: string } = {
	past_end = "serialize: the write does not fit the room left in the buffer",
	value_out_of_range = "serialize: the value is out of range for its declaration",
	non_finite_float = "serialize: a float or double value must be finite",
}

function Shared._fail(self: any, kind: any): boolean
	if self._kind ~= "read" then
		error(TRAP[kind] or ("serialize: write misuse (" .. tostring(kind) .. ")"), 3)
	end
	if self._error == nil then
		self._error = kind
	end
	return false
end

-- _room: true when n more bits fit (a measure stream always has room).
function Shared._room(self: any, n: number): boolean
	return self._bits + n <= self._numBits
end

function Shared.alignBits(self: any): number
	if self._kind == "measure" then
		return 7
	end
	return (8 - self._bits % 8) % 8
end

-- The primitives below run only after every check has passed.

-- _put writes the low n bits of value (masked by the caller), n in [0, 32].
function Shared._put(self: any, value: number, n: number)
	if n == 0 then
		return
	end
	if self._kind == "write" then
		self._packer:_pack(value, n)
	end
	self._bits += n
end

-- _putWide writes the low n bits of limbs, n in [0, 32 * #limbs], least
-- significant limb first (STANDARD.md bits: the low 32 bits first; uint128:
-- the low half first). The bits are the same however the value is split.
function Shared._putWide(self: any, limbs: { number }, n: number)
	if self._kind == "measure" then
		self._bits += n -- a measure never looks at the value
		return
	end
	local i = 1
	while n > 0 do
		local take = if n > 32 then 32 else n
		self:_put(bit32.band(limbs[i], MASK[take]), take)
		n -= take
		i += 1
	end
end

-- _get reads n bits in [0, 32] as a number.
function Shared._get(self: any, n: number): number
	if n == 0 then
		return 0
	end
	local v = self._reader:_take(n)
	self._bits += n
	return v
end

-- _getWide reads n bits into count limbs, least significant first.
function Shared._getWide(self: any, n: number, count: number): { number }
	local limbs = table.create(count, 0)
	local i = 1
	while n > 0 do
		local take = if n > 32 then 32 else n
		limbs[i] = self:_get(take)
		n -= take
		i += 1
	end
	return limbs
end

-- ---------------------------------------------------------------------------
-- bits, bool and the fixed-width unsigned integers (STANDARD.md bits, bool).

-- fixedWidth runs a no-parameter method of width n: write the masked value,
-- read it, or measure n.
local function fixedWidth(s: any, n: number, writeValue: () -> number): (boolean, number?)
	if s._error ~= nil then
		return false, nil
	end
	if not s:_room(n) then
		return s:_fail(PAST_END), nil
	end
	if s._kind == "read" then
		return true, s:_get(n)
	end
	s:_put(writeValue(), n)
	return true, nil
end

function Shared.serializeBits(self: any, h: any, bits: number): boolean
	if not bitsArg(self, bits, 32, RAISE_BITS_RANGE) then
		return false
	end
	if self._kind ~= "read" then
		-- the value must fit its width: masking is not checking (U8); a
		-- measure checks what a write checks (U6)
		uintArg(h.value, bits, "serializeBits", "value")
	end
	local ok, v = fixedWidth(self, bits, function()
		return bit32.band(h.value, MASK[bits])
	end)
	if ok and v ~= nil then
		h.value = v
	end
	return ok
end

function Shared.serializeBits64(self: any, h: any, bits: number): boolean
	if not bitsArg(self, bits, 64, RAISE_BITS_RANGE64) then
		return false
	end
	local limbs: { number }
	if self._kind ~= "read" then
		limbs = wideArg(Uint64, h.value, "serializeBits64", "value")
		-- the value must fit its width: masking is not checking (U8)
		need(Wide.bitLen(limbs, 2) <= bits, "serializeBits64", "value", "a uint" .. bits)
	end
	if self._error ~= nil then
		return false
	end
	if not self:_room(bits) then
		return self:_fail(PAST_END)
	end
	if self._kind == "read" then
		h.value = Uint64._wrap(self:_getWide(bits, 2))
	else
		self:_putWide(limbs, bits)
	end
	return true
end

function Shared.serializeBool(self: any, h: any): boolean
	if self._kind ~= "read" then
		need(type(h.value) == "boolean", "serializeBool", "value", "a boolean")
	end
	local ok, v = fixedWidth(self, 1, function()
		return if h.value then 1 else 0
	end)
	if ok and v ~= nil then
		h.value = v ~= 0
	end
	return ok
end

local function uintMethod(bits: number, name: string)
	return function(self: any, h: any): boolean
		if self._kind ~= "read" then
			uintArg(h.value, bits, name, "value")
		end
		local ok, v = fixedWidth(self, bits, function()
			return h.value
		end)
		if ok and v ~= nil then
			h.value = v
		end
		return ok
	end
end

Shared.serializeUint8 = uintMethod(8, "serializeUint8")
Shared.serializeUint16 = uintMethod(16, "serializeUint16")
Shared.serializeUint32 = uintMethod(32, "serializeUint32")

-- wideFixed: a fixed-width wide method (uint64, uint128, the double bits).
local function wideFixed(C: any, bits: number, name: string)
	return function(self: any, h: any): boolean
		local limbs: { number }
		if self._kind ~= "read" then
			limbs = wideArg(C, h.value, name, "value")
		end
		if self._error ~= nil then
			return false
		end
		if not self:_room(bits) then
			return self:_fail(PAST_END)
		end
		if self._kind == "read" then
			h.value = C._wrap(self:_getWide(bits, C.limbs))
		else
			self:_putWide(limbs, bits)
		end
		return true
	end
end

Shared.serializeUint64 = wideFixed(Uint64, 64, "serializeUint64")
Shared.serializeUint128 = wideFixed(Uint128, 128, "serializeUint128")

-- ---------------------------------------------------------------------------
-- Ranged integers (STANDARD.md int, int64, int128): the offset from min in
-- the bit length of max - min, computed in the unsigned domain.

function Shared.serializeInt(self: any, h: any, min: number, max: number): boolean
	-- int bounds are int32, judged on the caller's own values (U5); min <= max
	if not needDecl(self, isInteger(min, -2147483648, 2147483647), "serializeInt", "min", "an int32") then
		return false
	end
	if not needDecl(self, isInteger(max, -2147483648, 2147483647), "serializeInt", "max", "an int32") then
		return false
	end
	if min > max then
		return misuse(self, RAISE_MIN_MAX, 2)
	end
	local diff = max - min
	local bits = if diff == 0 then 0 else 32 - bit32.countlz(diff)
	if self._kind ~= "read" then
		local v = int32Arg(h.value, "serializeInt", "value")
		if self._error ~= nil then
			return false
		end
		if v < min or v > max then
			return self:_fail(RANGE)
		end
		if not self:_room(bits) then
			return self:_fail(PAST_END)
		end
		self:_put(v - min, bits)
		return true
	end
	if self._error ~= nil then
		return false
	end
	if not self:_room(bits) then
		return self:_fail(PAST_END)
	end
	local offset = self:_get(bits)
	if offset > diff then
		return self:_fail(RANGE)
	end
	h.value = offset + min
	return true
end

-- ranged: int64 and int128, the same rule over n limbs.
local function ranged(C: any, name: string)
	local n = C.limbs
	return function(self: any, h: any, min: any, max: any): boolean
		local lo = wideDecl(self, C, min, name, "min")
		if lo == nil then
			return false
		end
		local hi = wideDecl(self, C, max, name, "max")
		if hi == nil then
			return false
		end
		if Wide.cmpS(lo, hi, n) > 0 then
			return misuse(self, RAISE_MIN_MAX, 2)
		end
		local range = Wide.sub(hi, lo, n)
		local bits = Wide.bitLen(range, n)
		if self._kind ~= "read" then
			local v = wideArg(C, h.value, name, "value")
			if self._error ~= nil then
				return false
			end
			if Wide.cmpS(v, lo, n) < 0 or Wide.cmpS(v, hi, n) > 0 then
				return self:_fail(RANGE)
			end
			if not self:_room(bits) then
				return self:_fail(PAST_END)
			end
			self:_putWide(Wide.sub(v, lo, n), bits)
			return true
		end
		if self._error ~= nil then
			return false
		end
		if not self:_room(bits) then
			return self:_fail(PAST_END)
		end
		local offset = self:_getWide(bits, n)
		if Wide.cmpU(offset, range, n) > 0 then
			return self:_fail(RANGE)
		end
		h.value = C._wrap(Wide.add(offset, lo, n))
		return true
	end
end

Shared.serializeInt64 = ranged(Int64, "serializeInt64")
Shared.serializeInt128 = ranged(Int128, "serializeInt128")

-- ---------------------------------------------------------------------------
-- Fixed point (STANDARD.md fixed): a raw Q value as an offset from the raw
-- minimum, over bounds in whole units (serialize.go fixedPointParams64/128).

-- fixedParams returns the raw minimum and raw range as limbs of the storage
-- width (2 or 4) and the bit count, or nil after misuse (a read refuses).
local function fixedParams(s: any, width: number, integerBits: any, fractionBits: any, min: any, max: any, name: string): ({ number }?, { number }?, number)
	if not needDecl(s, isInteger(integerBits, -2147483648, 2147483647), name, "integerBits", "an integer") then
		return nil, nil, 0
	end
	if not needDecl(s, isInteger(fractionBits, -2147483648, 2147483647), name, "fractionBits", "an integer") then
		return nil, nil, 0
	end
	local lo = wideDecl(s, Int64, min, name, "min")
	if lo == nil then
		return nil, nil, 0
	end
	local hi = wideDecl(s, Int64, max, name, "max")
	if hi == nil then
		return nil, nil, 0
	end
	local total = integerBits + fractionBits
	local fits = if width == 2 then total <= 64 else total == 128
	if integerBits < 1 or fractionBits < 0 or not fits or Wide.cmpS(lo, hi, 2) > 0 then
		misuse(s, RAISE_FIXED_PARAMS, 3)
		return nil, nil, 0
	end
	-- fixedPointCapacity: the whole-unit bounds the Q format holds, signed when
	-- min is negative (the sign bit counts toward integerBits)
	local signed = Wide.isNegative(lo, 2)
	local capLo, capHi
	if signed then
		if integerBits >= 65 then
			capLo, capHi = { 0, 2147483648 }, { 4294967295, 2147483647 }
		else
			capLo = Wide.neg(Wide.shl({ 1, 0 }, integerBits - 1, 2), 2)
			capHi = if integerBits >= 64 then { 4294967295, 2147483647 } else Wide.sub(Wide.shl({ 1, 0 }, integerBits - 1, 2), { 1, 0 }, 2)
		end
	else
		capLo = { 0, 0 }
		capHi = if integerBits >= 63 then { 4294967295, 2147483647 } else Wide.sub(Wide.shl({ 1, 0 }, integerBits, 2), { 1, 0 }, 2)
	end
	if Wide.cmpS(lo, capLo, 2) < 0 or Wide.cmpS(hi, capHi, 2) > 0 then
		misuse(s, RAISE_FIXED_BOUNDS, 3)
		return nil, nil, 0
	end
	local function extend(x: { number }): { number }
		if width == 2 then
			return x
		end
		local fill = if Wide.isNegative(x, 2) then 4294967295 else 0
		return { x[1], x[2], fill, fill }
	end
	local rawMin = Wide.shl(extend(lo), fractionBits, width)
	local rawMax = Wide.shl(extend(hi), fractionBits, width)
	local rawRange = Wide.sub(rawMax, rawMin, width)
	local bits: number
	if width == 2 then
		bits = Wide.bitLen(rawRange, 2)
	elseif Wide.cmpU(lo, hi, 2) == 0 then
		bits = 0 -- a degenerate range costs zero bits on every width (serialize#54)
	else
		bits = Wide.bitLen(Wide.sub(hi, lo, 2), 2) + fractionBits
	end
	return rawMin, rawRange, bits
end

local function fixedMethod(C: any, width: number, name: string)
	return function(self: any, h: any, integerBits: number, fractionBits: number, min: any, max: any): boolean
		local rawMinOrNil, rawRangeOrNil, bits = fixedParams(self, width, integerBits, fractionBits, min, max, name)
		if rawMinOrNil == nil or rawRangeOrNil == nil then
			return false
		end
		local rawMin: { number }, rawRange: { number } = rawMinOrNil, rawRangeOrNil
		if self._kind ~= "read" then
			local v = wideArg(C, h.value, name, "value")
			if self._error ~= nil then
				return false
			end
			local offset = Wide.sub(v, rawMin, width)
			if Wide.cmpU(offset, rawRange, width) > 0 then
				return self:_fail(RANGE)
			end
			if not self:_room(bits) then
				return self:_fail(PAST_END)
			end
			self:_putWide(offset, bits)
			return true
		end
		if self._error ~= nil then
			return false
		end
		if not self:_room(bits) then
			return self:_fail(PAST_END)
		end
		local offset = self:_getWide(bits, width)
		if Wide.cmpU(offset, rawRange, width) > 0 then
			return self:_fail(RANGE)
		end
		h.value = C._wrap(Wide.add(rawMin, offset, width))
		return true
	end
end

Shared.serializeFixed64 = fixedMethod(Int64, 2, "serializeFixed64")
Shared.serializeFixed128 = fixedMethod(Int128, 4, "serializeFixed128")

-- serializeFixedNumber(h, min, max): a moo number as fixed point Q48.16, its
-- own representation, over [min, max] whole units (max at most 2^47 - 1):
-- the wire of serializeFixed64(raw, 48, 16, min, max), exact both ways. A read whose raw
-- value is -2^63 (the one Q48.16 pattern with no moo number) is refused.
local MAX_NUMBER_UNITS = { 4294967295, 32767 } -- 2^47 - 1 whole units, the largest moo integer

function Shared.serializeFixedNumber(self: any, h: any, min: any, max: any): boolean
	-- the bounds must be moo numbers: an unsigned Q48.16 holds whole units to
	-- 2^48 - 1, whose raw values past 2^63 have no moo number (and would read
	-- back as negative numbers)
	local maxLimbs = wideDecl(self, Int64, max, "serializeFixedNumber", "max")
	if maxLimbs == nil then
		return false
	end
	if Wide.cmpS(maxLimbs, MAX_NUMBER_UNITS, 2) > 0 then
		return misuse(self, "serialize: serializeFixedNumber max must be at most 2^47 - 1 whole units, the largest moo integer", 2)
	end
	local raw: any = nil
	if self._kind ~= "read" then
		need(type(h.value) == "number", "serializeFixedNumber", "value", "a number")
		raw = Int64.fromNumberRaw(h.value)
	end
	local holder = { value = raw }
	if not Shared.serializeFixed64(self, holder, 48, 16, min, max) then
		return false
	end
	if self._kind == "read" then
		local n = Int64.toNumberRaw(holder.value)
		if n == nil then
			return self:_fail(RANGE)
		end
		h.value = n
	end
	return true
end

-- ---------------------------------------------------------------------------
-- int_relative (STANDARD.md int_relative; serialize.go intRelativeBuckets).

local BUCKETS = { { 2, 6 }, { 7, 23 }, { 24, 280 }, { 281, 4377 }, { 4378, 69914 } }

local function bitsRequired32(min: number, max: number): number
	local diff = max - min
	if diff == 0 then
		return 0
	end
	return 32 - bit32.countlz(diff)
end
local INT_RELATIVE_MAX = 2147483647
local INT_RELATIVE_BOUNDED_MAX = 69914 -- the last bounded tier's top difference

function Shared.serializeIntRelative(self: any, previous: number, h: any): boolean
	-- previous's domain is judged before anything narrows it (U16)
	if not isInteger(previous, 0, 2147483647) then
		return misuse(self, RAISE_INT_RELATIVE_DOMAIN, 2)
	end
	if self._kind ~= "read" then
		local current = int32Arg(h.value, "serializeIntRelative", "value")
		if self._error ~= nil then
			return false
		end
		if previous >= current then
			return self:_fail(RANGE)
		end
		local difference = current - previous
		-- room for the whole tier before its first bit (U10): the unary
		-- flags, then the tier's field
		local total = 6 + 32
		if difference == 1 then
			total = 1
		else
			for k, bucket in BUCKETS do
				if difference <= bucket[2] then
					total = 1 + k + bitsRequired32(bucket[1], bucket[2])
					break
				end
			end
		end
		if not self:_room(total) then
			return self:_fail(PAST_END)
		end
		if not self:serializeBool({ value = difference == 1 }) then
			return false
		end
		if difference == 1 then
			return true
		end
		for _, bucket in BUCKETS do
			local inBucket = difference <= bucket[2]
			if not self:serializeBool({ value = inBucket }) then
				return false
			end
			if inBucket then
				return self:serializeInt({ value = difference }, bucket[1], bucket[2])
			end
		end
		return self:serializeBits({ value = current }, 32)
	end
	if self._error ~= nil then
		return false
	end
	-- every tier's reconstruction is checked against the domain and previous
	-- (value_out_of_range); then the absolute tier is canonical too: a current
	-- no more than 69914 above previous has a bounded encoding, and the
	-- absolute one is a second encoding of it (non_canonical). Numbers hold
	-- every value here exactly (all below 2^33), so nothing wraps.
	local function result(reconstructed: number, absolute: boolean): boolean
		if reconstructed > INT_RELATIVE_MAX or reconstructed <= previous then
			return self:_fail(RANGE)
		end
		if absolute and reconstructed - previous <= INT_RELATIVE_BOUNDED_MAX then
			return self:_fail(NON_CANONICAL)
		end
		h.value = reconstructed
		return true
	end
	local flag = { value = false }
	if not self:serializeBool(flag) then
		return false
	end
	if flag.value then
		return result(previous + 1, false)
	end
	for _, bucket in BUCKETS do
		if not self:serializeBool(flag) then
			return false
		end
		if flag.value then
			local difference = { value = 0 }
			if not self:serializeInt(difference, bucket[1], bucket[2]) then
				return false
			end
			return result(previous + difference.value, false)
		end
	end
	local v = { value = 0 }
	if not self:serializeBits(v, 32) then
		return false
	end
	return result(v.value, true)
end

-- ---------------------------------------------------------------------------
-- float and double (STANDARD.md float, double; Finite values only, every one
-- bit exact).
--
-- The format carries FINITE values only: a pattern whose exponent field is all
-- ones (NaN with any payload, either infinity) is refused on read as
-- non_finite_float, tested on the raw bits before the holder is written, and
-- latched as non_finite_float on write (and measure), as serialize.go's
-- writer does. Every finite pattern travels bit for bit.
--
-- serializeFloatBits / serializeDoubleBits carry the finite IEEE bit pattern
-- itself (a number in [0, 2^32), a Uint64), -0, the subnormals and values far
-- past 2^47 included, which no moo number can hold. serializeFloat /
-- serializeDouble carry a moo number across the float boundary: a write sends
-- the nearest IEEE value, ties to even (always finite); a read refuses a
-- non-finite pattern as non_finite_float, a finite value outside the number
-- range as value_out_of_range, and otherwise rounds to the nearest number,
-- ties to even.

local F32_EXPONENT = 0x7F800000
local F64_EXPONENT_HI = 0x7FF00000 -- the exponent field, in the high limb

local function floatBitsAreFinite(bits: number): boolean
	return bit32.band(bits, F32_EXPONENT) ~= F32_EXPONENT
end

local function doubleBitsAreFinite(hi: number): boolean
	return bit32.band(hi, F64_EXPONENT_HI) ~= F64_EXPONENT_HI
end

function Shared.serializeFloatBits(self: any, h: any): boolean
	if self._kind ~= "read" then
		uintArg(h.value, 32, "serializeFloatBits", "value")
	end
	if self._error ~= nil then
		return false
	end
	if self._kind ~= "read" and not floatBitsAreFinite(h.value) then
		return self:_fail(NON_FINITE)
	end
	if not self:_room(32) then
		return self:_fail(PAST_END)
	end
	if self._kind ~= "read" then
		self:_put(h.value, 32)
		return true
	end
	local bits = self:_get(32)
	if not floatBitsAreFinite(bits) then
		return self:_fail(NON_FINITE)
	end
	h.value = bits
	return true
end

function Shared.serializeDoubleBits(self: any, h: any): boolean
	local limbs: { number }
	if self._kind ~= "read" then
		limbs = wideArg(Uint64, h.value, "serializeDoubleBits", "value")
	end
	if self._error ~= nil then
		return false
	end
	if self._kind ~= "read" and not doubleBitsAreFinite(limbs[2]) then
		return self:_fail(NON_FINITE)
	end
	if not self:_room(64) then
		return self:_fail(PAST_END)
	end
	if self._kind ~= "read" then
		self:_putWide(limbs, 64)
		return true
	end
	local got = self:_getWide(64, 2)
	if not doubleBitsAreFinite(got[2]) then
		return self:_fail(NON_FINITE)
	end
	h.value = Uint64._wrap(got)
	return true
end

function Shared.serializeFloat(self: any, h: any): boolean
	if self._kind ~= "read" then
		need(type(h.value) == "number", "serializeFloat", "value", "a number")
		return Shared.serializeFloatBits(self, { value = Float.toFloatBits(h.value) })
	end
	local bits = { value = 0 }
	if not Shared.serializeFloatBits(self, bits) then
		return false
	end
	local n = Float.fromFloatBits(bits.value)
	if n == nil then
		return self:_fail(RANGE)
	end
	h.value = n
	return true
end

function Shared.serializeDouble(self: any, h: any): boolean
	if self._kind ~= "read" then
		need(type(h.value) == "number", "serializeDouble", "value", "a number")
		local lo, hi = Float.toDoubleBits(h.value)
		return Shared.serializeDoubleBits(self, { value = Uint64._wrap({ lo, hi }) })
	end
	local bits: any = { value = nil }
	if not Shared.serializeDoubleBits(self, bits) then
		return false
	end
	local n = Float.fromDoubleBits(bits.value[1], bits.value[2])
	if n == nil then
		return self:_fail(RANGE)
	end
	h.value = n
	return true
end

-- ---------------------------------------------------------------------------
-- compressed_float (STANDARD.md compressed_float; serialize.go
-- CompressedFloatParams and compressedFloat32Precomputed), in float32
-- arithmetic emulated bit for bit (serialize_float.moo).

local F_HALF = 0x3F000000
local F_ONE = 0x3F800000
local F_MAX_VALUES = 0x4F7FFFFF -- 4294967040, the largest float32 below 2^32

-- compressedFloatParams(min, max, res): the declaration's wire constants as
-- (maxIntegerValue, bits, delta float32 bits), or nil for a declaration that
-- is not min < max and res > 0 (the caller decides: raise, or refuse on a
-- read). min, max and res are float32 bit patterns.
local function compressedFloatParams(min: number, max: number, res: number): (number?, number, number, boolean)
	if not Float.lt(min, max) or not Float.lt(0, res) then
		return nil, 0, 0, false
	end
	local delta = Float.fsub(max, min)
	local values = Float.fdiv(delta, res)
	-- the fourth result: delta and delta / res are finite in float32. A
	-- declaration where either is not is write misuse (the check model; U9),
	-- never clamped there; a read clamps it, as every reader does
	local finite = Float.isFinite(delta) and Float.isFinite(values)
	if not Float.le(F_ONE, values) then
		values = F_ONE
	elseif Float.lt(F_MAX_VALUES, values) then
		values = F_MAX_VALUES
	end
	local maxIntegerValue = Float.ceilToInt(values)
	return maxIntegerValue, bitsRequired32(0, maxIntegerValue), delta, finite
end

-- precomputedOk: what the precomputed constants can be checked for without
-- the declaration they came from (U16): bits is bitsRequired(0,
-- maxIntegerValue), maxIntegerValue lies in [1, 4294967040] (the largest a
-- declaration derives), and delta is positive and finite.
local MAX_INTEGER_VALUE = 4294967040

local function precomputedOk(maxIntegerValue: number, bits: number, delta: number): boolean
	return maxIntegerValue >= 1 and maxIntegerValue <= MAX_INTEGER_VALUE and bits == bitsRequired32(0, maxIntegerValue) and Float.lt(0, delta) and Float.isFinite(delta)
end

-- compressed runs the shared arm. valueBits is the value to write as float32
-- bits (write only); the read returns the decoded float32 bits.
local function compressed(s: any, valueBits: number?, maxIntegerValue: number, bits: number, delta: number, min: number): (boolean, number?)
	-- a declaration whose top integer decodes to a value that is not finite
	-- (min + delta, rounded once) is non-conforming as a whole (STANDARD.md
	-- compressed_float, The decode must be finite): write misuse, trapped on
	-- write and measure (U9, U6); a reader refuses the non-finite decode itself
	if s._kind ~= "read" and not Float.isFinite(Float.fadd(min, delta)) then
		error(RAISE_COMPRESSED_DECODE, 3)
	end
	if s._kind == "measure" then
		s._bits += bits
		return true, nil
	end
	if s._kind == "write" then
		local v = valueBits :: number
		local normalized = Float.fdiv(Float.fsub(v, min), delta)
		if not Float.le(0, normalized) then
			normalized = 0
		elseif not Float.le(normalized, F_ONE) then
			normalized = F_ONE
		end
		local product = Float.fmul(normalized, Float.fromUint(maxIntegerValue))
		local integerValue = Float.floorToInt(Float.fadd(product, F_HALF))
		if integerValue > maxIntegerValue then
			integerValue = maxIntegerValue
		end
		if s._error ~= nil then
			return false, nil
		end
		if not s:_room(bits) then
			return s:_fail(PAST_END), nil
		end
		s:_put(integerValue, bits)
		return true, nil
	end
	if s._error ~= nil then
		return false, nil
	end
	if not s:_room(bits) then
		return s:_fail(PAST_END), nil
	end
	local integerValue = s:_get(bits)
	if integerValue > maxIntegerValue then
		return s:_fail(RANGE), nil
	end
	local normalized = Float.fdiv(Float.fromUint(integerValue), Float.fromUint(maxIntegerValue))
	local decoded = Float.fadd(Float.fmul(normalized, delta), min)
	-- every operand finite and the integer in range, and still the final add can
	-- round past FLT_MAX: no declaration hands NaN or an infinity across the wire
	if not Float.isFinite(decoded) then
		return s:_fail(NON_FINITE), nil
	end
	return true, decoded
end

-- compressedValue: the value to write as float32 bits. The Bits form must be
-- finite (IMPLEMENTING.md, The check model: a compressed_float value that is
-- not finite is misuse); a number always is.
local function compressedValue(s: any, h: any, asBits: boolean, name: string): number?
	if s._kind == "read" then
		return nil
	end
	if asBits then
		uintArg(h.value, 32, name, "value")
		if not Float.isFinite(h.value) then
			error(RAISE_NOT_FINITE, 3)
		end
		return h.value
	end
	need(type(h.value) == "number", name, "value", "a number")
	return Float.toFloatBits(h.value)
end

local function compressedMethod(asBits: boolean, precomputed: boolean, name: string)
	return function(self: any, h: any, a: any, b: any, c: any, d: any): boolean
		local maxIntegerValue: number, bits: number, delta: number, min: number
		-- every declaration is checked before any bits move; on a read a bad
		-- one is refused (U7, U16)
		if precomputed then
			-- (maxIntegerValue, bits, delta, min)
			if not needDecl(self, isInteger(a, 0, 4294967295), name, "maxIntegerValue", "a uint32") then
				return false
			end
			if not needDecl(self, isInteger(b, -2147483648, 2147483647), name, "bits", "an integer") then
				return false
			end
			local db, mb = Float.toFloat32Bits(c), Float.toFloat32Bits(d)
			if not needDecl(self, db ~= nil, name, "delta", "a number or a Float32") or not needDecl(self, mb ~= nil, name, "min", "a number or a Float32") then
				return false
			end
			maxIntegerValue, bits, delta, min = a, b, db :: number, mb :: number
			if not precomputedOk(maxIntegerValue, bits, delta) then
				return misuse(self, RAISE_PRECOMPUTED, 2)
			end
		else
			-- (min, max, res)
			local mnb, mxb, rb = Float.toFloat32Bits(a), Float.toFloat32Bits(b), Float.toFloat32Bits(c)
			if not needDecl(self, mnb ~= nil, name, "min", "a number or a Float32") or not needDecl(self, mxb ~= nil, name, "max", "a number or a Float32") or not needDecl(self, rb ~= nil, name, "res", "a number or a Float32") then
				return false
			end
			min = mnb :: number
			local m: number?, finite: boolean
			m, bits, delta, finite = compressedFloatParams(min, mxb :: number, rb :: number)
			if m == nil then
				return misuse(self, RAISE_FLOAT_PARAMS, 2)
			end
			if not finite and self._kind ~= "read" then
				error(RAISE_DECLARATION_FINITE, 2)
			end
			maxIntegerValue = m
		end
		local valueBits = compressedValue(self, h, asBits, name)
		local ok, decoded = compressed(self, valueBits, maxIntegerValue, bits, delta, min)
		if not ok or decoded == nil then
			return ok
		end
		if asBits then
			h.value = decoded
			return true
		end
		local n = Float.fromFloatBits(decoded)
		if n == nil then
			return self:_fail(RANGE)
		end
		h.value = n
		return true
	end
end

Shared.serializeCompressedFloat = compressedMethod(false, false, "serializeCompressedFloat")
Shared.serializeCompressedFloatBits = compressedMethod(true, false, "serializeCompressedFloatBits")
Shared.serializeCompressedFloatPrecomputed = compressedMethod(false, true, "serializeCompressedFloatPrecomputed")
Shared.serializeCompressedFloatPrecomputedBits = compressedMethod(true, true, "serializeCompressedFloatPrecomputedBits")

-- ---------------------------------------------------------------------------
-- align, bytes, string, end (STANDARD.md align, bytes, string, end).

function Shared.serializeAlign(self: any): boolean
	if self._kind == "measure" then
		if self._error ~= nil then
			return false
		end
		self._bits += 7
		return true
	end
	if self._error ~= nil then
		return false
	end
	local n = (8 - self._bits % 8) % 8
	if n == 0 then
		return true
	end
	if not self:_room(n) then
		return self:_fail(PAST_END)
	end
	if self._kind == "write" then
		self:_put(0, n)
		return true
	end
	if self:_get(n) ~= 0 then
		return self:_fail(NONZERO_PADDING)
	end
	return true
end

-- serializeBytes(data[, offset, count]): aligns, then copies count bytes of
-- the buffer (all of it by default); a read fills them. Both sides know the
-- length: it is not sent.
function Shared.serializeBytes(self: any, data: buffer, offset: number?, count: number?): boolean
	-- the destination's own capacity bounds the declaration; on a read a range
	-- outside it, a negative count included, is refused (U7, U16)
	if not needDecl(self, type(data) == "buffer", "serializeBytes", "data", "a buffer") then
		return false
	end
	local off = offset or 0
	local cnt = count or (buffer.len(data) - (if type(off) == "number" then off else 0))
	if not needDecl(self, isInteger(off, 0, buffer.len(data)) and isInteger(cnt, 0, buffer.len(data) - off), "serializeBytes", "offset and count", "a range inside the buffer") then
		return false
	end
	-- a write checks room for the align and the payload together before it
	-- writes either (U10)
	if self._kind == "write" then
		if self._error ~= nil then
			return false
		end
		if not self:_room((8 - self._bits % 8) % 8 + cnt * 8) then
			return self:_fail(PAST_END)
		end
	end
	if not self:serializeAlign() then
		return false
	end
	if not self:_room(cnt * 8) then
		return self:_fail(PAST_END)
	end
	if self._kind == "write" then
		self._packer:_copy(data, off, cnt)
	elseif self._kind == "read" then
		self._reader:_copyOut(data, off, cnt)
	end
	self._bits += cnt * 8
	return true
end

local function bufferSizeArg(s: any, bufferSize: any): boolean
	if not isInteger(bufferSize, 1, 2147483647) then
		return misuse(s, RAISE_BUFFER_SIZE, 3)
	end
	return true
end

-- validUtf8 is Go's utf8.Valid: no overlong forms, no surrogates, nothing
-- past U+10FFFF.
local function validUtf8(s: string): boolean
	local i, n = 1, #s
	while i <= n do
		local c = string.byte(s, i) :: number
		if c < 0x80 then
			i += 1
		else
			local need2: number, lo: number, hi: number
			if c >= 0xC2 and c <= 0xDF then
				need2, lo, hi = 1, 0x80, 0xBF
			elseif c == 0xE0 then
				need2, lo, hi = 2, 0xA0, 0xBF
			elseif (c >= 0xE1 and c <= 0xEC) or c == 0xEE or c == 0xEF then
				need2, lo, hi = 2, 0x80, 0xBF
			elseif c == 0xED then
				need2, lo, hi = 2, 0x80, 0x9F
			elseif c == 0xF0 then
				need2, lo, hi = 3, 0x90, 0xBF
			elseif c >= 0xF1 and c <= 0xF3 then
				need2, lo, hi = 3, 0x80, 0xBF
			elseif c == 0xF4 then
				need2, lo, hi = 3, 0x80, 0x8F
			else
				return false
			end
			if i + need2 > n then
				return false
			end
			local c1 = string.byte(s, i + 1) :: number
			if c1 < lo or c1 > hi then
				return false
			end
			for k = 2, need2 do
				local ck = string.byte(s, i + k) :: number
				if ck < 0x80 or ck > 0xBF then
					return false
				end
			end
			i += need2 + 1
		end
	end
	return true
end

-- serializeString(h, bufferSize): the length as serializeInt(length, 0,
-- bufferSize - 1), then the bytes, aligned. A writer is handed well-formed
-- UTF-8 with no zero byte (misuse raises otherwise); a reader checks the
-- claimed length against the bytes that remain before it reads them, then
-- refuses a zero byte (embedded_nul) BEFORE it checks the encoding
-- (invalid_utf8), so a payload breaking both is embedded_nul.
function Shared.serializeString(self: any, h: any, bufferSize: number): boolean
	if not bufferSizeArg(self, bufferSize) then
		return false
	end
	if self._kind ~= "read" then
		need(type(h.value) == "string", "serializeString", "value", "a string")
		if string.find(h.value, "\0", 1, true) ~= nil or not validUtf8(h.value) then
			error(RAISE_STRING_TEXT, 2)
		end
		if self._error ~= nil then
			return false
		end
		local s: string = h.value
		if #s >= bufferSize then
			return self:_fail(RANGE)
		end
		-- room for the length, the align and the payload together before the
		-- first bit of any of them (U10)
		local lengthBits = bitsRequired32(0, bufferSize - 1)
		local pad = (8 - (self._bits + lengthBits) % 8) % 8
		if self._kind == "measure" then
			pad = 7
		end
		if not self:_room(lengthBits + pad + #s * 8) then
			return self:_fail(PAST_END)
		end
		if not self:serializeInt({ value = #s }, 0, bufferSize - 1) then
			return false
		end
		if not self:serializeAlign() then
			return false
		end
		if not self:_room(#s * 8) then
			return self:_fail(PAST_END)
		end
		if self._kind == "write" and #s > 0 then
			self._packer:_copy(buffer.fromstring(s), 0, #s)
		end
		self._bits += #s * 8
		return true
	end
	if self._error ~= nil then
		return false
	end
	local length = { value = 0 }
	if not self:serializeInt(length, 0, bufferSize - 1) then
		return false
	end
	if not self:serializeAlign() then
		return false
	end
	local n = length.value
	if not self:_room(n * 8) then
		return self:_fail(PAST_END)
	end
	local s = buffer.readstring(self._reader._data, self._bits // 8, n)
	self._reader._bitsRead += n * 8
	self._bits += n * 8
	if string.find(s, "\0", 1, true) ~= nil then
		return self:_fail(EMBEDDED_NUL)
	end
	if not validUtf8(s) then
		return self:_fail(INVALID_UTF8)
	end
	h.value = s
	return true
end

-- serializeEnd(): the end of the object (STANDARD.md end). A packet reader
-- calls it after the last field of the top-level object. A read refuses a
-- whole byte or more left over as trailing_data, checked FIRST, then reads the
-- final byte's 0 to 7 unused bits and refuses a set one as nonzero_padding; an
-- accepted end leaves bitsProcessed() at 8 times the byte count. On a failed
-- read stream it is refused with the first refusal's reason. On a write or a
-- measure it is a no-op returning true, whatever the stream's state (U12): it
-- emits nothing and costs nothing.
function Shared.serializeEnd(self: any): boolean
	if self._kind ~= "read" then
		return true
	end
	if self._error ~= nil then
		return false
	end
	local remaining = self._numBits - self._bits
	if remaining >= 8 then
		return self:_fail(TRAILING_DATA)
	end
	if remaining > 0 and self:_get(remaining) ~= 0 then
		return self:_fail(NONZERO_PADDING)
	end
	return true
end

-- ---------------------------------------------------------------------------
-- object (STANDARD.md object): object:serialize(stream) returns true, or false
-- and optionally the caller's own error value, which the stream latches;
-- rejected when none is given (STANDARD.md, Refusal reasons: a nested object
-- that fails with no refusal of its own refuses as rejected; IMPLEMENTING.md
-- U2). It goes through the one latch, so a reason a primitive inside the
-- object latched first is kept.

function Shared.serializeObject(self: any, object: any): boolean
	local method: any = if type(object) == "table" then object.serialize else nil
	if type(method) ~= "function" then
		return misuse(self, RAISE_NOT_AN_OBJECT, 2)
	end
	if self._error ~= nil then
		return false
	end
	local ok, err = (method :: any)(object, self)
	if not ok and self._kind ~= "read" then
		return false -- a write or measure records nothing (U9, U6)
	end
	if not ok then
		return self:_fail(if err ~= nil then err else REJECTED)
	end
	return self._error == nil
end

-- reject: the caller's own refusal of a value whose bytes were well formed
-- (STANDARD.md, Refusal reasons, rejected; IMPLEMENTING.md U2). On a read it
-- latches rejected through the one latch, so it is terminal and a reason
-- latched first is kept, and it returns false, so a serialize method can
-- `return stream:reject()`. It is read only: on a write or a measure there is
-- nothing to refuse, and calling it there is API misuse, which raises as every
-- other misuse does (IMPLEMENTING.md U9: moo is always checked).
function Shared.reject(self: any): boolean
	if self._kind ~= "read" then
		error(RAISE_REJECT_NOT_READ, 2)
	end
	return self:_fail(REJECTED)
end

-- refusalName(reason): the name of a latched reason, as STANDARD.md spells it,
-- or "none" for no refusal (IMPLEMENTING.md U14). An object's own error value
-- (serializeObject latches it) is outside the closed list: it is the caller
-- refusing, so it is named rejected, and a refused stream never names none.
local CLOSED: { [any]: boolean } = {}
for _, reason in Refusal :: any do
	CLOSED[reason] = true
end

local function refusalName(reason: any): string
	if reason == nil then
		return "none"
	end
	if CLOSED[reason] then
		return reason
	end
	return REJECTED
end

-- refusal(): the stream's refusal name, or "none" (U14).
function Shared.refusal(self: any): string
	return refusalName(self._error)
end

-- fail(reason): the public refusal (U14). On a read it latches reason through
-- the one latch, so the first reason wins; no reason, or one outside the
-- closed list, latches rejected (never none: a refused stream that reported
-- none would be relabelled past_end by the next read). On a write or a measure
-- it records nothing and returns false (U6, U9).
function Shared.fail(self: any, reason: any?): boolean
	if self._kind ~= "read" then
		return false
	end
	return self:_fail(if reason ~= nil and CLOSED[reason] then reason else REJECTED)
end

-- ---------------------------------------------------------------------------
-- The three classes.

local function class(kind: string): any
	local c: any = {}
	c.__index = c
	for k, v in Shared do
		c[k] = v
	end
	c._kindName = kind
	return c
end

local WriteStream = class("write")

-- new(buffer): the buffer's size must be a multiple of 8 bytes.
function WriteStream.new(buf: buffer): any
	local self = setmetatable({ _kind = "write", _context = nil }, WriteStream)
	self:reset(buf)
	return self
end

function WriteStream.reset(self: any, buf: buffer)
	if type(buf) ~= "buffer" or buffer.len(buf) % 8 ~= 0 then
		error(RAISE_BUFFER_BYTES, 2)
	end
	self._data = buf
	self._numBits = buffer.len(buf) * 8
	self._bits = 0
	self._packer = Bitpacker.BitWriter.new(buf)
	self._error = nil
end

function WriteStream.bitsAvailable(self: any): number
	return self._numBits - self._bits
end

-- flush stores the partial word (STANDARD.md, Flush). Call it before data.
function WriteStream.flush(self: any)
	self._packer:flushBits()
end

-- data returns the written bytes, bytesProcessed() of them, as a fresh buffer.
function WriteStream.data(self: any): buffer
	local n = self:bytesProcessed()
	local out = buffer.create(n)
	if n > 0 then
		buffer.copy(out, 0, self._data, 0, n)
	end
	return out
end

local ReadStream = class("read")

function ReadStream.new(data: buffer): any
	local self = setmetatable({ _kind = "read", _context = nil }, ReadStream)
	self:reset(data)
	return self
end

-- reset points the stream at data and clears all read state, including a
-- latched error (STANDARD.md: a failure persists until re-initialization).
-- Data that is not a buffer is caller misuse on a read: the stream starts
-- refused as value_out_of_range over no bytes, never a raise (U7).
function ReadStream.reset(self: any, data: buffer)
	if type(data) ~= "buffer" then
		self._numBits = 0
		self._bits = 0
		self._reader = Bitpacker.BitReader.new(buffer.create(0))
		self._error = RANGE
		return
	end
	self._numBits = buffer.len(data) * 8
	self._bits = 0
	self._reader = Bitpacker.BitReader.new(data)
	self._error = nil
end

local MeasureStream = class("measure")

function MeasureStream.new(): any
	local self = setmetatable({ _kind = "measure", _context = nil }, MeasureStream)
	self:reset()
	return self
end

function MeasureStream.reset(self: any)
	self._numBits = math.huge
	self._bits = 0
	self._error = nil
end

-- Continue / Until (serialize.go): sentinel loops that stop at the first
-- error, so they always end on hostile data.
local function Continue(stream: any, more: Holder<boolean>): boolean
	if not stream:serializeBool(more) then
		return false
	end
	return more.value
end

local function Until(stream: any, done: Holder<boolean>): boolean
	if not stream:serializeBool(done) then
		return false
	end
	return not done.value
end

-- the public compressedFloatParams: Float32 or number parameters, the
-- constants as (maxIntegerValue, bits, delta as a Float32)
local function publicCompressedFloatParams(min: any, max: any, res: any): (number, number, any)
	local m, b, d, finite = compressedFloatParams(Float.float32Bits(min, "compressedFloatParams min"), Float.float32Bits(max, "compressedFloatParams max"), Float.float32Bits(res, "compressedFloatParams res"))
	if m == nil then
		error(RAISE_FLOAT_PARAMS, 2)
	end
	if not finite then
		error(RAISE_DECLARATION_FINITE, 2)
	end
	return m, b, Float.Float32.fromBits(d)
end

local function bitsRequired(min: number, max: number): number
	need(isInteger(min, 0, 4294967295), "bitsRequired", "min", "a uint32")
	need(isInteger(max, 0, 4294967295), "bitsRequired", "max", "a uint32")
	local diff = (max - min) % TWO32
	if diff == 0 then
		return 0
	end
	return 32 - bit32.countlz(diff)
end

return table.freeze({
	Refusal = Refusal,
	WriteStream = table.freeze(WriteStream),
	ReadStream = table.freeze(ReadStream),
	MeasureStream = table.freeze(MeasureStream),
	Continue = Continue,
	Until = Until,
	compressedFloatParams = publicCompressedFloatParams,
	bitsRequired = bitsRequired,
	refusalName = refusalName,
	_compressedFloatParamsBits = compressedFloatParams,
	_validUtf8 = validUtf8,
})
