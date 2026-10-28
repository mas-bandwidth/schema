--!strict
-- serialize_int_relative.luau — int_relative (DESIGN.md (a), (c), (g) and
-- (h); STANDARD.md int_relative), wire compatible with serialize (format
-- version 2.0) and serialize.go v2.0.0 write_stream.go, read_stream.go and
-- measure_stream.go: the same values produce the same bytes and the same
-- refusals.
--
-- int_relative encodes an increasing sequence compactly: a ladder of one-bit
-- "does it fit in this tier?" flags, then the difference in the first tier it
-- fits (or current as 32 raw bits in the absolute tier). Both values live in
-- the non-negative int32 domain, 0 to 2147483647; previous outside it is API
-- misuse (serialize.go panicIntRelativeDomain), and every reconstruction is
-- checked against the domain and against previous, so no tier can wrap.
--
-- Copyright © 2016 - 2026, Más Bandwidth LLC. BSD 3-Clause (see LICENSE).

-- The non-negative int32 domain (STANDARD.md int_relative; serialize.go
-- stream.go intRelativeMin/intRelativeMax).
local INT_RELATIVE_MIN = 0
local INT_RELATIVE_MAX = 2147483647

-- serialize.go's panicIntRelativeDomain (serialize.go serialize.go), raised
-- when the caller's own previous lies outside the domain (DESIGN.md (g)).
local RAISE_DOMAIN = "serialize: int relative previous must be in [0,2147483647]"

-- A parameter serialize.go's int32 type could not hold is the Luau form of a
-- Go compile error (DESIGN.md (a)): raised, naming the rule.
local RAISE_PREVIOUS_TYPE = "serialize: serializeIntRelative previous must be integer"
local RAISE_CURRENT_TYPE = "serialize: serializeIntRelative current must be integer"
local RAISE_HOLDER_TYPE = "serialize: serializeIntRelative holder must be table"

-- The latched kinds (DESIGN.md (a)); the stream values are the same strings.
local PAST_END = "past_end"
local VALUE_OUT_OF_RANGE = "value_out_of_range"

-- The write-misuse traps (IMPLEMENTING.md U9, U6): raised, never latched.
local RAISE_RANGE = "serialize: serializeIntRelative current must be above previous and in the domain"
local RAISE_ROOM = "serialize: the write does not fit the room left in the buffer"
local NON_CANONICAL = "non_canonical"

-- The last bounded tier's top difference (STANDARD.md int_relative): an
-- absolute tier carrying a difference no greater than this is a second
-- encoding of a value a bounded tier carries.
local BOUNDED_MAX = 69914

-- intRelativeBuckets (serialize.go stream.go): each bucket's payload is
-- serialize_int(d, min, max), whose wire width is BitsRequired(min, max).
-- STANDARD.md int_relative names them bounded-3 .. bounded-17 by that width.
local BUCKETS = {
	{ min = 2, max = 6, bits = 3 },
	{ min = 7, max = 23, bits = 5 },
	{ min = 24, max = 280, bits = 9 },
	{ min = 281, max = 4377, bits = 13 },
	{ min = 4378, max = 69914, bits = 17 },
}

-- an int32 holds only integers, so a non-integer number is not a value the
-- parameter TYPE could hold (DESIGN.md (a)).
local function isInteger(value: any): boolean
	return type(value) == "number" and value == value and value % 1 == 0
end

-- declarationMisuse applies the panicIntRelativeDomain rule and the holder
-- shape (the Luau form of Go's non-nil *int32): previous is the caller's own
-- state, never packet data, so a previous outside the domain is caller misuse
-- on every stream, checked before the latch (serialize.go read/write/measure
-- SerializeIntRelative; DESIGN.md (g)). It returns the rule broken, or nil; the
-- arm raises it on write and measure and refuses ValueOutOfRange on read
-- (IMPLEMENTING.md U7, U16).
local function declarationMisuse(previous: any, holder: any): string?
	if not isInteger(previous) then
		return RAISE_PREVIOUS_TYPE
	end
	if previous < INT_RELATIVE_MIN or previous > INT_RELATIVE_MAX then
		return RAISE_DOMAIN
	end
	if type(holder) ~= "table" then
		return RAISE_HOLDER_TYPE
	end
	return nil
end

-- writeWidth is the number of bits the writer emits: the flag ladder plus
-- either the matched bucket's payload or the absolute tier's 32 bits
-- (STANDARD.md int_relative; serialize.go write_stream.go SerializeIntRelative
-- and measure_stream.go SerializeIntRelative).
local function writeWidth(difference: number): number
	if difference == 1 then
		return 1
	end
	local bits = 1
	for _, bucket in BUCKETS do
		bits += 1
		if difference <= bucket.max then
			return bits + bucket.bits
		end
	end
	return bits + 32
end

-- canRead refuses past the end before any bit moves (DESIGN.md (g): no read
-- goes past the data's end; STANDARD.md int_relative rule 3).
local function canRead(s: any, bits: number): boolean
	if s:_available() < bits then
		return s:_fail(PAST_END)
	end
	return true
end

-- result checks a reconstructed value in a width that cannot wrap: it must
-- lie in the domain and be strictly greater than previous, and the holder is
-- assigned only once both hold (STANDARD.md int_relative; serialize.go
-- read_stream.go intRelativeResult; DESIGN.md (g)).
local function result(s: any, previous: number, reconstructed: number, holder: any): boolean
	if reconstructed > INT_RELATIVE_MAX or reconstructed <= previous then
		return s:_fail(VALUE_OUT_OF_RANGE)
	end
	holder.value = reconstructed
	return true
end

local M = {}

-- writeIntRelative writes current relative to previous, taking the first tier
-- the difference fits (STANDARD.md int_relative: the canonical encoding).
-- previous outside the domain, previous not less than current, current
-- outside the domain and a write that does not fit the room left all trap
-- before any bit moves, and latch nothing (IMPLEMENTING.md U9; DESIGN.md (g)).
function M.writeIntRelative(s: any, previous: number, holder: any): boolean
	local bad = declarationMisuse(previous, holder)
	if bad ~= nil then
		return s:_misuse(bad)
	end
	if s:_latched() then
		return false
	end
	local current = holder.value
	if not isInteger(current) then
		return s:_misuse(RAISE_CURRENT_TYPE)
	end
	if previous >= current then
		return s:_misuse(RAISE_RANGE)
	end
	if current > INT_RELATIVE_MAX then
		return s:_misuse(RAISE_RANGE)
	end
	local difference = current - previous
	local width = writeWidth(difference)
	if s:_available() < width then
		return s:_misuse(RAISE_ROOM)
	end
	if difference == 1 then
		s:_writeBits(1, 1)
		return true
	end
	s:_writeBits(0, 1)
	for _, bucket in BUCKETS do
		local inBucket = difference <= bucket.max
		s:_writeBits(if inBucket then 1 else 0, 1)
		if inBucket then
			s:_writeBits(difference - bucket.min, bucket.bits)
			return true
		end
	end
	s:_writeBits(current, 32)
	return true
end

-- readIntRelative reads current relative to previous. Every tier's
-- reconstruction is checked in a width that cannot wrap; the absolute tier's
-- 32 bits are unsigned, so a top-bit-set group is refused rather than
-- reappearing as a negative value (STANDARD.md int_relative; serialize.go
-- read_stream.go SerializeIntRelative). A refusal latches and leaves the
-- holder unwritten.
function M.readIntRelative(s: any, previous: number, holder: any): boolean
	local bad = declarationMisuse(previous, holder)
	if bad ~= nil then
		return s:_misuse(bad)
	end
	if s:_latched() then
		return false
	end
	if not canRead(s, 1) then
		return false
	end
	if s:_readBits(1) == 1 then
		return result(s, previous, previous + 1, holder)
	end
	for _, bucket in BUCKETS do
		if not canRead(s, 1) then
			return false
		end
		if s:_readBits(1) == 1 then
			if not canRead(s, bucket.bits) then
				return false
			end
			local offset = s:_readBits(bucket.bits)
			-- the bounded payload is a ranged integer: a decoded offset past
			-- the bucket's span is refused (STANDARD.md int_relative rule 2)
			if offset > bucket.max - bucket.min then
				return s:_fail(VALUE_OUT_OF_RANGE)
			end
			return result(s, previous, previous + bucket.min + offset, holder)
		end
	end
	if not canRead(s, 32) then
		return false
	end
	-- the absolute tier: the domain and ordering rule first, then the
	-- canonical rule, a difference a bounded tier carries refused as
	-- NonCanonical (STANDARD.md int_relative, "The absolute tier is canonical
	-- too"). The group is unsigned and a number, so the difference cannot wrap.
	local absolute = s:_readBits(32)
	if absolute > INT_RELATIVE_MAX or absolute <= previous then
		return s:_fail(VALUE_OUT_OF_RANGE)
	end
	if absolute - previous <= BOUNDED_MAX then
		return s:_fail(NON_CANONICAL)
	end
	holder.value = absolute
	return true
end

-- measureIntRelative counts exactly the bits writeIntRelative would emit and
-- refuses the same caller errors (serialize.go measure_stream.go
-- SerializeIntRelative; DESIGN.md (h)).
function M.measureIntRelative(s: any, previous: number, holder: any): boolean
	local bad = declarationMisuse(previous, holder)
	if bad ~= nil then
		return s:_misuse(bad)
	end
	if s:_latched() then
		return false
	end
	local current = holder.value
	if not isInteger(current) then
		return s:_misuse(RAISE_CURRENT_TYPE)
	end
	if previous >= current then
		return s:_misuse(RAISE_RANGE)
	end
	if current > INT_RELATIVE_MAX then
		return s:_misuse(RAISE_RANGE)
	end
	return s:_measure(writeWidth(current - previous))
end

return table.freeze(M)
