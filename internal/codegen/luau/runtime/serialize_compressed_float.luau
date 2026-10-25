--!strict
-- serialize_compressed_float.luau — the compressed_float family (DESIGN.md
-- (a), (e), (g) and (h); STANDARD.md, Floating Point, "compressed_float"): a
-- float quantized to a resolution, wire compatible with serialize.go v1.15.1.
--
-- The wire constants come from CompressedFloatParams (serialize.go stream.go):
-- delta = max - min, values = delta / res clamped to [1, 4294967040],
-- max_integer_value = ceil(values) and bits = bits_required(0, max_integer_value),
-- all computed in float32. Every step of the quantization arithmetic is float32
-- with its own rounding (fround, DESIGN.md (e)): the product rounds before 0.5
-- is added and that sum rounds before the floor — two roundings, never one, and
-- no step widened to double before the floor (STANDARD.md, compressed_float).
--
-- A non-finite value (NaN, +Inf, -Inf) raises on write before the latch check
-- and before any arithmetic (STANDARD.md, compressed_float, Non-finite inputs;
-- DESIGN.md (e)) — the one deliberate deviation from serialize.go's write arm,
-- which clamps NaN to 0. The read and measure arms never see the value.
--
-- Copyright © 2016 - 2026, Más Bandwidth LLC. BSD 3-Clause (see LICENSE).

local Bits = require("./serialize_bits")
local Float = require("./serialize_float")
local Stream = require("./serialize_stream")

local fround = Float.fround
local bitsRequired = Bits.bitsRequired

-- Raise messages for API misuse: serialize.go's panic strings (serialize.go
-- v1.15.1 serialize.go; DESIGN.md (a) and (g)), plus STANDARD.md's non-finite
-- write raise.
local RAISE_FLOAT_PARAMS = "serialize: compressed float requires min < max and resolution > 0"
local RAISE_PRECOMPUTED = "serialize: precomputed compressed float constants must be exactly what CompressedFloatParams derives"
local RAISE_NONFINITE = "serialize: compressed float value must be finite"
local RAISE_VALUE = "serialize: serializeCompressedFloat value must be a number"
local RAISE_VALUE_PRECOMPUTED = "serialize: serializeCompressedFloatPrecomputed value must be a number"

local VALUES_CLAMP_HIGH = 4294967040.0

local M = {}

local function isFinite(x: number): boolean
	return x == x and x ~= math.huge and x ~= -math.huge
end

-- compressedFloatParams derives the wire constants from a (min,max,resolution)
-- declaration (serialize.go stream.go CompressedFloatParams; STANDARD.md
-- compressed_float). Each parameter is narrowed to float32 on entry, as Go's
-- float32 parameters are (DESIGN.md (e)). A declaration with min >= max or
-- resolution <= 0 is API misuse and raises.
function M.compressedFloatParams(min: number, max: number, resolution: number): (number, number, number)
	min = fround(min)
	max = fround(max)
	resolution = fround(resolution)
	if not (min < max) or not (resolution > 0) then
		error(RAISE_FLOAT_PARAMS, 2)
	end
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

-- validateCompressedFloatConstants raises unless the precomputed constants are
-- exactly what CompressedFloatParams derives (serialize.go stream.go
-- validateCompressedFloatConstants; DESIGN.md (g)).
local function validateCompressedFloatConstants(maxIntegerValue: number, bits: number, delta: number)
	if maxIntegerValue == 0 or bits ~= bitsRequired(0, maxIntegerValue) or not (delta > 0) or delta - delta ~= 0 then
		error(RAISE_PRECOMPUTED, 2)
	end
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
	local n = fround(fround(h.value - min) / delta)
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
		return s:_fail(Stream.SerializeError.Overflow)
	end
	s:_writeBits(integerVal, bits)
	return true
end

-- readCommon is the read-side home (serialize.go read_stream.go
-- compressedFloat32Precomputed): refuse an integer above max_integer_value, then
-- divide by max_integer_value, multiply by delta and add min, every step float32
-- with its own rounding. The destination is left unwritten on a refusal and the
-- stream is terminal (DESIGN.md (g)).
local function readCommon(s: any, h: any, maxIntegerValue: number, bits: number, delta: number, min: number): boolean
	if s:_latched() then
		return false
	end
	if s:_available() < bits then
		return s:_fail(Stream.SerializeError.Overflow)
	end
	local integerVal = s:_readBits(bits)
	if integerVal > maxIntegerValue then
		return s:_fail(Stream.SerializeError.ValueOutOfRange)
	end
	local n = fround(fround(integerVal) / fround(maxIntegerValue))
	h.value = fround(fround(n * delta) + min)
	return true
end

-- The dispatched write arms. A non-finite value raises before the latch check and
-- before any arithmetic (STANDARD.md compressed_float; DESIGN.md (e)).
function M.writeCompressedFloat(s: any, h: any, min: number, max: number, resolution: number): boolean
	local value = h.value
	if type(value) ~= "number" then
		error(RAISE_VALUE, 2)
	end
	if not isFinite(value) then
		error(RAISE_NONFINITE, 2)
	end
	local maxIntegerValue, bits, delta = M.compressedFloatParams(min, max, resolution)
	return writeCommon(s, h, maxIntegerValue, bits, delta, fround(min))
end

function M.writeCompressedFloatPrecomputed(s: any, h: any, maxIntegerValue: number, bits: number, delta: number, min: number): boolean
	local value = h.value
	if type(value) ~= "number" then
		error(RAISE_VALUE_PRECOMPUTED, 2)
	end
	if not isFinite(value) then
		error(RAISE_NONFINITE, 2)
	end
	validateCompressedFloatConstants(maxIntegerValue, bits, delta)
	return writeCommon(s, h, maxIntegerValue, bits, delta, fround(min))
end

function M.readCompressedFloat(s: any, h: any, min: number, max: number, resolution: number): boolean
	local maxIntegerValue, bits, delta = M.compressedFloatParams(min, max, resolution)
	return readCommon(s, h, maxIntegerValue, bits, delta, fround(min))
end

function M.readCompressedFloatPrecomputed(s: any, h: any, maxIntegerValue: number, bits: number, delta: number, min: number): boolean
	validateCompressedFloatConstants(maxIntegerValue, bits, delta)
	return readCommon(s, h, maxIntegerValue, bits, delta, fround(min))
end

function M.measureCompressedFloat(s: any, h: any, min: number, max: number, resolution: number): boolean
	local _, bits, _ = M.compressedFloatParams(min, max, resolution)
	return s:_measure(bits)
end

function M.measureCompressedFloatPrecomputed(s: any, h: any, maxIntegerValue: number, bits: number, delta: number, min: number): boolean
	validateCompressedFloatConstants(maxIntegerValue, bits, delta)
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
