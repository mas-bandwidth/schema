--!strict
-- serialize_compressed_float.luau — the compressed_float family (DESIGN.md
-- (a), (e), (g) and (h); STANDARD.md, Floating Point, "compressed_float"): a
-- float quantized to a resolution, wire compatible with serialize.go v2.0.0.
--
-- The wire constants come from CompressedFloatParams (serialize.go stream.go):
-- delta = max - min, values = delta / res clamped to [1, 4294967040],
-- max_integer_value = ceil(values) and bits = bits_required(0, max_integer_value),
-- all computed in float32. Every step of the quantization arithmetic is float32
-- with its own rounding (fround, DESIGN.md (e)): the product rounds before 0.5
-- is added and that sum rounds before the floor — two roundings, never one, and
-- no step widened to double before the floor (STANDARD.md, compressed_float).
--
-- A non-finite value (NaN, +Inf, -Inf) traps on write and measure before any
-- arithmetic, never clamped (STANDARD.md, compressed_float, Non-finite inputs;
-- IMPLEMENTING.md U9, U6; DESIGN.md (e)) — the one deliberate deviation from
-- serialize.go's write arm, which clamps NaN to 0. The read arm never sees the
-- value.
--
-- The decode must be finite (STANDARD.md, compressed_float, "The decode must
-- be finite"): a reader refuses a decode that is not finite with
-- NonFiniteFloat, its destination unwritten, whatever the declaration. The
-- writer and the measure trap on a declaration whose delta or delta / res is
-- not finite in float32, or whose top integer decodes past FLT_MAX (min +
-- delta, rounded once in float32), as the reference asserts (IMPLEMENTING.md,
-- "The check model"); only write and measure check these, never the reader.
--
-- Copyright © 2016 - 2026, Más Bandwidth LLC. BSD 3-Clause (see LICENSE).

local Bits = require("./serialize_bits")
local Float = require("./serialize_float")
local Stream = require("./serialize_stream")

local fround = Float.fround
local bitsRequired = Bits.bitsRequired

-- Raise messages for API misuse: serialize.go's panic strings (serialize.go
-- v2.0.0 serialize.go; DESIGN.md (a) and (g)), plus STANDARD.md's non-finite
-- write raise.
local RAISE_FLOAT_PARAMS = "serialize: compressed float requires min < max and resolution > 0"
local RAISE_PRECOMPUTED = "serialize: precomputed compressed float constants must be exactly what CompressedFloatParams derives"
local RAISE_NONFINITE = "serialize: compressed float value must be finite"
local RAISE_VALUE = "serialize: serializeCompressedFloat value must be a number"
local RAISE_VALUE_PRECOMPUTED = "serialize: serializeCompressedFloatPrecomputed value must be a number"
local RAISE_DECODE = "serialize: compressed float declaration's top integer must decode to a finite float32 (min + delta)"
local RAISE_DECLARATION = "serialize: compressed float declaration's delta and delta / resolution must be finite in float32"
local RAISE_ROOM = "serialize: the write does not fit the room left in the buffer"

local VALUES_CLAMP_HIGH = 4294967040.0

local M = {}

local function isFinite(x: number): boolean
	return x == x and x ~= math.huge and x ~= -math.huge
end

-- params derives the wire constants from a (min,max,resolution) declaration
-- (serialize.go stream.go CompressedFloatParams; STANDARD.md compressed_float),
-- unchecked: the caller has refused min >= max and resolution <= 0. Each
-- parameter is narrowed to float32 on entry, as Go's float32 parameters are
-- (DESIGN.md (e)). A non-finite delta or delta / res is clamped here, which
-- is what the read path keeps (the corpus header of compressed_float.txt:
-- "the read path is untouched"); the write and measure arms trap on it first.
local function params(min: number, max: number, resolution: number): (number, number, number)
	min = fround(min)
	max = fround(max)
	resolution = fround(resolution)
	local delta = fround(max - min)
	local values = fround(delta / resolution)
	if not (values >= 1.0) then
		values = 1.0
	elseif values > VALUES_CLAMP_HIGH then
		values = VALUES_CLAMP_HIGH
	end
	local maxIntegerValue = math.ceil(values)
	local bits = bitsRequired(0, maxIntegerValue)
	return maxIntegerValue, bits, delta
end

-- paramsMisuse is the rule a (min,max,resolution) declaration breaks, or nil:
-- numbers, with min < max and resolution > 0 once narrowed to float32
-- (serialize.go CompressedFloatParams' panic; IMPLEMENTING.md U16).
local function paramsMisuse(min: any, max: any, resolution: any): string?
	if type(min) ~= "number" or type(max) ~= "number" or type(resolution) ~= "number" then
		return RAISE_FLOAT_PARAMS
	end
	if not (fround(min) < fround(max)) or not (fround(resolution) > 0) then
		return RAISE_FLOAT_PARAMS
	end
	return nil
end

-- declarationMisuse is the write-side check of a (min,max,resolution)
-- declaration (IMPLEMENTING.md, "The check model"): paramsMisuse, then a
-- delta and a delta / res that are finite in float32, then a top decode
-- min + delta that is finite in float32. It returns the rule broken, or nil.
-- Write and measure trap on it (U9, U6); the read path keeps paramsMisuse
-- alone (U16), and refuses a non-finite decode at runtime.
local function declarationMisuse(min: any, max: any, resolution: any): string?
	local bad = paramsMisuse(min, max, resolution)
	if bad ~= nil then
		return bad
	end
	local delta = fround(fround(max) - fround(min))
	if not isFinite(delta) or not isFinite(fround(delta / fround(resolution))) then
		return RAISE_DECLARATION
	end
	if not isFinite(fround(fround(min) + delta)) then
		return RAISE_DECODE
	end
	return nil
end

-- compressedFloatParams is the reference's compressed_float_params: the wire
-- constants (max_integer_value, bits, delta) of a declaration. Like the
-- reference's, it traps on min >= max, resolution <= 0, and a delta or a
-- delta / res that is not finite in float32 (IMPLEMENTING.md, "The check
-- model"; U9).
function M.compressedFloatParams(min: number, max: number, resolution: number): (number, number, number)
	local bad = paramsMisuse(min, max, resolution)
	if bad == nil then
		local delta = fround(fround(max) - fround(min))
		if not isFinite(delta) or not isFinite(fround(delta / fround(resolution))) then
			bad = RAISE_DECLARATION
		end
	end
	if bad ~= nil then
		error(bad, 2)
	end
	return params(min, max, resolution)
end

-- constantsValid is true when the precomputed constants are what can be
-- checked without the declaration they came from (serialize.go stream.go
-- validateCompressedFloatConstants; IMPLEMENTING.md U16): max_integer_value an
-- integer in [1, 4294967040], the top of CompressedFloatParams' clamp (so
-- bitsRequired never sees a value past 32 bits and cannot wrap at 2^32),
-- bits exactly bitsRequired(0, max_integer_value), delta positive and finite,
-- and min a number. The arm raises on write and measure and refuses
-- ValueOutOfRange on read (U7).
local function constantsValid(maxIntegerValue: any, bits: any, delta: any, min: any): boolean
	if type(maxIntegerValue) ~= "number" or type(bits) ~= "number" or type(delta) ~= "number" or type(min) ~= "number" then
		return false
	end
	if not (maxIntegerValue >= 1 and maxIntegerValue <= VALUES_CLAMP_HIGH) or maxIntegerValue // 1 ~= maxIntegerValue then
		return false
	end
	return bits == bitsRequired(0, maxIntegerValue) and delta > 0 and delta - delta == 0
end

-- writeCommon is the one audited home of the write-side quantization arithmetic
-- (serialize.go write_stream.go compressedFloat32Precomputed; STANDARD.md
-- compressed_float): clamp (value - min)/delta to [0,1], multiply by
-- max_integer_value, add 0.5, floor, then clamp the integer to
-- max_integer_value. The !>= / !<= clamp form forces NaN into range too, mirror
-- of Go's. The whole width is bounds checked before any bit moves (DESIGN.md (g)).
local function writeCommon(s: any, h: any, maxIntegerValue: number, bits: number, delta: number, min: number): boolean
	if s:_latched() then
		return false
	end
	-- the value is a float32, narrowed as Go's float32(v) narrows it, before
	-- any arithmetic (DESIGN.md (e))
	local n = fround(fround(fround(h.value) - min) / delta)
	if not (n >= 0) then
		n = 0
	elseif not (n <= 1) then
		n = 1
	end
	local integerVal = math.floor(fround(fround(n * fround(maxIntegerValue)) + 0.5))
	if integerVal > maxIntegerValue then
		integerVal = maxIntegerValue
	end
	if s:_available() < bits then
		return s:_misuse(RAISE_ROOM)
	end
	s:_writeBits(integerVal, bits)
	return true
end

-- readCommon is the read-side home (serialize.go read_stream.go
-- compressedFloat32Precomputed): refuse an integer above max_integer_value, then
-- divide by max_integer_value, multiply by delta and add min, every step float32
-- with its own rounding, and refuse a decode that is not finite with
-- NonFiniteFloat. The destination is left unwritten on a refusal and the
-- stream is terminal (DESIGN.md (g)).
local function readCommon(s: any, h: any, maxIntegerValue: number, bits: number, delta: number, min: number): boolean
	if s:_latched() then
		return false
	end
	if s:_available() < bits then
		return s:_fail(Stream.SerializeError.PastEnd)
	end
	local integerVal = s:_readBits(bits)
	if integerVal > maxIntegerValue then
		return s:_fail(Stream.SerializeError.ValueOutOfRange)
	end
	local n = fround(fround(integerVal) / fround(maxIntegerValue))
	local decoded = fround(fround(n * delta) + min)
	if not isFinite(decoded) then
		return s:_fail(Stream.SerializeError.NonFiniteFloat)
	end
	h.value = decoded
	return true
end


-- valueMisuse is the write-side check of the value (IMPLEMENTING.md, "The
-- check model"; U9): a number, finite as a float32 (NaN, an infinity, or a
-- finite double past FLT_MAX, which narrows to one, all trap; never clamped
-- in this always-checked port). It returns the rule broken, or nil.
local function valueMisuse(h: any, raiseValue: string): string?
	local value = h.value
	if type(value) ~= "number" then
		return raiseValue
	end
	if not isFinite(fround(value)) then
		return RAISE_NONFINITE
	end
	return nil
end

-- precomputedMisuse is the check of precomputed constants (U16), and on write
-- and measure the top decode min + delta finite in float32 ("The check
-- model": write path only; a reader refuses the non-finite decode at runtime).
local function precomputedMisuse(maxIntegerValue: any, bits: any, delta: any, min: any, writing: boolean): string?
	if not constantsValid(maxIntegerValue, bits, delta, min) or not constantsValid(maxIntegerValue, bits, fround(delta), min) then
		return RAISE_PRECOMPUTED
	end
	if writing and not isFinite(fround(fround(min) + fround(delta))) then
		return RAISE_DECODE
	end
	return nil
end

-- The dispatched arms. The declaration is checked first, on every stream
-- (IMPLEMENTING.md U16): on write and measure it traps (U9, U6), on read it
-- refuses ValueOutOfRange (s:_misuse, U7), before any bit moves. On write and
-- measure the value is then checked, and traps when it is not finite as a
-- float32, before any arithmetic (STANDARD.md compressed_float; DESIGN.md
-- (e)). A measure makes every check a write makes, except room (U6). The
-- precomputed arms narrow delta to float32, as Go's float32 parameter is,
-- before it is validated.
function M.writeCompressedFloat(s: any, h: any, min: number, max: number, resolution: number): boolean
	local bad = declarationMisuse(min, max, resolution) or valueMisuse(h, RAISE_VALUE)
	if bad ~= nil then
		return s:_misuse(bad)
	end
	local maxIntegerValue, bits, delta = params(min, max, resolution)
	return writeCommon(s, h, maxIntegerValue, bits, delta, fround(min))
end

function M.writeCompressedFloatPrecomputed(s: any, h: any, maxIntegerValue: number, bits: number, delta: number, min: number): boolean
	local bad = precomputedMisuse(maxIntegerValue, bits, delta, min, true) or valueMisuse(h, RAISE_VALUE_PRECOMPUTED)
	if bad ~= nil then
		return s:_misuse(bad)
	end
	return writeCommon(s, h, maxIntegerValue, bits, fround(delta), fround(min))
end

function M.readCompressedFloat(s: any, h: any, min: number, max: number, resolution: number): boolean
	local bad = paramsMisuse(min, max, resolution)
	if bad ~= nil then
		return s:_misuse(bad)
	end
	local maxIntegerValue, bits, delta = params(min, max, resolution)
	return readCommon(s, h, maxIntegerValue, bits, delta, fround(min))
end

function M.readCompressedFloatPrecomputed(s: any, h: any, maxIntegerValue: number, bits: number, delta: number, min: number): boolean
	local bad = precomputedMisuse(maxIntegerValue, bits, delta, min, false)
	if bad ~= nil then
		return s:_misuse(bad)
	end
	return readCommon(s, h, maxIntegerValue, bits, fround(delta), fround(min))
end

function M.measureCompressedFloat(s: any, h: any, min: number, max: number, resolution: number): boolean
	local bad = declarationMisuse(min, max, resolution) or valueMisuse(h, RAISE_VALUE)
	if bad ~= nil then
		return s:_misuse(bad)
	end
	local _, bits, _ = params(min, max, resolution)
	return s:_measure(bits)
end

function M.measureCompressedFloatPrecomputed(s: any, h: any, maxIntegerValue: number, bits: number, delta: number, min: number): boolean
	local bad = precomputedMisuse(maxIntegerValue, bits, delta, min, true) or valueMisuse(h, RAISE_VALUE_PRECOMPUTED)
	if bad ~= nil then
		return s:_misuse(bad)
	end
	return s:_measure(bits)
end

return table.freeze({
	compressedFloatParams = M.compressedFloatParams,
	writeCompressedFloat = M.writeCompressedFloat,
	readCompressedFloat = M.readCompressedFloat,
	measureCompressedFloat = M.measureCompressedFloat,
	writeCompressedFloatPrecomputed = M.writeCompressedFloatPrecomputed,
	readCompressedFloatPrecomputed = M.readCompressedFloatPrecomputed,
	measureCompressedFloatPrecomputed = M.measureCompressedFloatPrecomputed,
})
