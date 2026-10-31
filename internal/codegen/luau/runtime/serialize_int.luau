--!strict
-- serialize_int.luau — the ranged integers int and int64 (DESIGN.md (h),
-- STANDARD.md, Integers: int (ranged) and int64 (ranged)).
--
-- The stream dispatches serializeInt and serializeInt64 here; each arm does
-- the whole operation in serialize.go v2.0.0's order: misuse first (a min
-- greater than max), then on write and measure a value outside [min,max] and
-- a write that does not fit the room left trap (IMPLEMENTING.md U9, U6: write
-- misuse never latches), and on read an offset past the span refuses
-- ValueOutOfRange; the offset moves in the operation's unsigned domain so a
-- span wider than 2^31 (or 2^63) is exact (STANDARD.md, int (ranged)).
--
-- A degenerate range min == max costs zero bits and takes its value from the
-- range (STANDARD.md, int (ranged)). The bitsRequired functions live here as
-- the card's FILE LAYOUT requires, in the same unsigned-domain arithmetic.
--
-- Copyright © 2016 - 2026, Más Bandwidth LLC. BSD 3-Clause (see LICENSE).

local RAISE_MINMAX = "serialize: min must not be greater than max"
local RAISE_INT32 = "serialize: serializeInt parameters must be int32 numbers"
local RAISE_INT64 = "serialize: serializeInt64 parameters must be integers"

local PAST_END = "past_end"
local OUT_OF_RANGE = "value_out_of_range"

-- The write-misuse traps (IMPLEMENTING.md U9): a value outside [min,max] and
-- an operation that does not fit the room left raise on write and measure
-- (the measure has no room to check, U6), never latch.
local RAISE_RANGE = "serialize: ranged integer value must be in [min,max]"
local RAISE_ROOM = "serialize: the write does not fit the room left in the buffer"

local INT32_MIN = -2147483648
local INT32_MAX = 2147483647
local TWO32 = 4294967296

local function isInt32(n: any): boolean
	return type(n) == "number" and n % 1 == 0 and n >= INT32_MIN and n <= INT32_MAX
end

local function isInteger(n: any): boolean
	return typeof(n) == "integer"
end

-- bitsRequired is serialize.go BitsRequired over the unsigned 32-bit domain
-- (STANDARD.md, int (ranged)): zero for a degenerate range, else the width of
-- max - min. span is an exact double for every int32 range (at most 2^32 - 1).
local function bitsRequired(min: number, max: number): number
	if min == max then
		return 0
	end
	local span = max - min
	return 64 - integer.tonumber(integer.countlz(integer.create(span)))
end

-- bitsRequired64 is serialize.go BitsRequired64: the subtraction is in the
-- unsigned 64-bit domain, so a range wider than 2^63 is exact (STANDARD.md,
-- int64 (ranged)).
local function bitsRequired64(min: integer, max: integer): number
	if min == max then
		return 0
	end
	return 64 - integer.tonumber(integer.countlz(integer.sub(max, min)))
end

-- writeInt: STANDARD.md, int (ranged). min <= max, the value in [min,max],
-- and offset value - min in uint32.
local function writeInt(s: any, holder: any, min: number, max: number): boolean
	if not isInt32(min) or not isInt32(max) or not isInt32(holder.value) then
		return s:_misuse(RAISE_INT32)
	end
	if min > max then
		return s:_misuse(RAISE_MINMAX)
	end
	if s:_latched() then
		return false
	end
	local value = holder.value
	if value < min or value > max then
		return s:_misuse(RAISE_RANGE)
	end
	local bits = bitsRequired(min, max)
	if s._kind == "measure" then
		return s:_measure(bits)
	end
	if s:_available() < bits then
		return s:_misuse(RAISE_ROOM)
	end
	if bits > 0 then
		s:_writeBits((value - min) % TWO32, bits)
	end
	return true
end

local function readInt(s: any, holder: any, min: number, max: number): boolean
	if not isInt32(min) or not isInt32(max) then
		return s:_misuse(RAISE_INT32)
	end
	if min > max then
		return s:_misuse(RAISE_MINMAX)
	end
	if s:_latched() then
		return false
	end
	local bits = bitsRequired(min, max)
	if s:_available() < bits then
		return s:_fail(PAST_END)
	end
	if bits == 0 then
		holder.value = min
		return true
	end
	local offset = s:_readBits(bits)
	-- compare in the unsigned domain before forming the value (STANDARD.md,
	-- int (ranged): reject, never clamp)
	if offset > max - min then
		return s:_fail(OUT_OF_RANGE)
	end
	holder.value = offset + min
	return true
end

local function measureInt(s: any, holder: any, min: number, max: number): boolean
	if not isInt32(min) or not isInt32(max) or not isInt32(holder.value) then
		return s:_misuse(RAISE_INT32)
	end
	if min > max then
		return s:_misuse(RAISE_MINMAX)
	end
	if s:_latched() then
		return false
	end
	if holder.value < min or holder.value > max then
		return s:_misuse(RAISE_RANGE)
	end
	return s:_measure(bitsRequired(min, max))
end

-- writeInt64: STANDARD.md, int64 (ranged). The offset is uint64(v) - uint64(min),
-- written as one group when bits <= 32 and otherwise low 32 bits then high.
local function writeInt64(s: any, holder: any, min: integer, max: integer): boolean
	if not isInteger(min) or not isInteger(max) or not isInteger(holder.value) then
		return s:_misuse(RAISE_INT64)
	end
	if integer.gt(min, max) then
		return s:_misuse(RAISE_MINMAX)
	end
	if s:_latched() then
		return false
	end
	local value = holder.value
	if integer.lt(value, min) or integer.gt(value, max) then
		return s:_misuse(RAISE_RANGE)
	end
	local bits = bitsRequired64(min, max)
	if s._kind == "measure" then
		return s:_measure(bits)
	end
	if s:_available() < bits then
		return s:_misuse(RAISE_ROOM)
	end
	if bits > 0 then
		s:_writeBits64(integer.sub(value, min), bits)
	end
	return true
end

local function readInt64(s: any, holder: any, min: integer, max: integer): boolean
	if not isInteger(min) or not isInteger(max) then
		return s:_misuse(RAISE_INT64)
	end
	if integer.gt(min, max) then
		return s:_misuse(RAISE_MINMAX)
	end
	if s:_latched() then
		return false
	end
	local bits = bitsRequired64(min, max)
	if s:_available() < bits then
		return s:_fail(PAST_END)
	end
	if bits == 0 then
		holder.value = min
		return true
	end
	local offset = s:_readBits64(bits)
	if not integer.ule(offset, integer.sub(max, min)) then
		return s:_fail(OUT_OF_RANGE)
	end
	holder.value = integer.add(offset, min)
	return true
end

local function measureInt64(s: any, holder: any, min: integer, max: integer): boolean
	if not isInteger(min) or not isInteger(max) or not isInteger(holder.value) then
		return s:_misuse(RAISE_INT64)
	end
	if integer.gt(min, max) then
		return s:_misuse(RAISE_MINMAX)
	end
	if s:_latched() then
		return false
	end
	if integer.lt(holder.value, min) or integer.gt(holder.value, max) then
		return s:_misuse(RAISE_RANGE)
	end
	return s:_measure(bitsRequired64(min, max))
end

return table.freeze({
	bitsRequired = bitsRequired,
	bitsRequired64 = bitsRequired64,
	writeInt = writeInt,
	readInt = readInt,
	measureInt = measureInt,
	writeInt64 = writeInt64,
	readInt64 = readInt64,
	measureInt64 = measureInt64,
})
