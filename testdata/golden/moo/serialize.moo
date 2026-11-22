--!strict
-- serialize.moo -- the entry: returns the API table (DESIGN.md, "The API").
--
-- serialize.moo is a bitpacking serializer for moo, wire compatible with
-- serialize (C++, STANDARD.md, format version 2.0), serialize.go and every
-- other port: the same values produce the same bytes. Load it as
-- require("./serialize") from a file in the same directory; every module it
-- loads is required as "./serialize_<name>" from this directory alone
-- (DESIGN.md, "Loading").
--
-- Copyright © 2016 - 2026, Más Bandwidth LLC. BSD 3-Clause (see LICENSE).

local Stream = require("./serialize_stream")
local Bitpacker = require("./serialize_bitpacker")
local Wide = require("./serialize_wide")
local Float = require("./serialize_float")

export type Holder<T> = Stream.Holder<T>

local S = {}

-- The wire format version this library implements (STANDARD.md, Format version).
S.FORMAT_VERSION = "2.0"

-- the refusal reasons, STANDARD.md's closed list: stream:error() returns one
S.Refusal = Stream.Refusal
S.WriteStream = Stream.WriteStream
S.ReadStream = Stream.ReadStream
S.MeasureStream = Stream.MeasureStream
S.Continue = Stream.Continue
S.Until = Stream.Until

S.BitWriter = Bitpacker.BitWriter
S.BitReader = Bitpacker.BitReader

-- the wide integers (DESIGN.md, "The number model")
S.Uint64 = Wide.Uint64
S.Int64 = Wide.Int64
S.Uint128 = Wide.Uint128
S.Int128 = Wide.Int128

-- a float32 carried exactly (DESIGN.md, "The float boundary")
S.Float32 = Float.Float32

-- bitsRequired(min, max): the bits a range of uint32 values takes
-- (serialize.go BitsRequired): 0 when min == max, else the bit length of
-- max - min modulo 2^32.
S.bitsRequired = Stream.bitsRequired

-- bitsRequired64(min, max) / bitsRequired128(min, max): the same over Uint64
-- and Uint128 (or numbers), subtracting in the unsigned domain.
function S.bitsRequired64(min: any, max: any): number
	local a, b = Wide.Uint64._limbsOf(min, "bitsRequired64 min"), Wide.Uint64._limbsOf(max, "bitsRequired64 max")
	return Wide.bitLen(Wide.sub(b, a, 2), 2)
end

function S.bitsRequired128(min: any, max: any): number
	local a, b = Wide.Uint128._limbsOf(min, "bitsRequired128 min"), Wide.Uint128._limbsOf(max, "bitsRequired128 max")
	return Wide.bitLen(Wide.sub(b, a, 4), 4)
end

-- signedToUnsigned / unsignedToSigned: the zig-zag pair over int32 / uint32
-- (serialize.go SignedToUnsigned, UnsignedToSigned).
function S.signedToUnsigned(n: number): number
	if type(n) ~= "number" or n ~= math.floor(n) or n < -2147483648 or n > 2147483647 then
		error("serialize: signedToUnsigned n must be an int32", 2)
	end
	return if n >= 0 then n * 2 else -n * 2 - 1
end

function S.unsignedToSigned(n: number): number
	if type(n) ~= "number" or n ~= math.floor(n) or n < 0 or n > 4294967295 then
		error("serialize: unsignedToSigned n must be a uint32", 2)
	end
	return if n % 2 == 0 then n // 2 else -(n // 2) - 1
end

-- compressedFloatParams(min, max, res): the precomputed compressed float
-- constants (maxIntegerValue, bits, delta as a Float32); min, max and res are
-- numbers (by the boundary rule) or Float32 values.
S.compressedFloatParams = Stream.compressedFloatParams

-- refusalName(reason): a latched reason's name as STANDARD.md spells it, or
-- "none" for nil (IMPLEMENTING.md U14); stream:refusal() is the same for a
-- stream. A value outside the closed list (an object's own error value) is
-- the caller's refusal, named "rejected".
S.refusalName = Stream.refusalName

-- copyString(source, destSize): the reference's serialize_copy_string
-- (IMPLEMENTING.md U15, v2.1.1), defined on the source's bytes: the bytes
-- before the first zero byte; if more than destSize - 1, the first destSize - 1
-- of them, then bytes dropped from the end while the first byte left out is a
-- continuation byte (10xxxxxx). For well-formed UTF-8 that is the longest
-- prefix that fits a string field of buffer size destSize and does not end
-- inside a character. destSize below 1 is write misuse, and traps (U9).
function S.copyString(source: string, destSize: number): string
	if type(source) ~= "string" then
		error("serialize: copyString source must be a string", 2)
	end
	if type(destSize) ~= "number" or destSize ~= math.floor(destSize) or destSize < 1 then
		error("serialize: copyString destSize must be an integer of at least 1", 2)
	end
	local nul = string.find(source, "\0", 1, true)
	local n = if nul ~= nil then nul - 1 else #source
	if n > destSize - 1 then
		n = destSize - 1
		-- step back over continuation bytes so no character is cut in two
		while n > 0 and n < #source do
			local c = string.byte(source, n + 1) :: number
			if c < 0x80 or c >= 0xC0 then
				break
			end
			n -= 1
		end
	end
	return string.sub(source, 1, n)
end

return table.freeze(S)
