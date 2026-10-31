--!strict
-- serialize.luau — the entry: returns the API table (DESIGN.md (a)).
--
-- serialize.luau is a bitpacking serializer for Luau, wire compatible with
-- serialize (C++, STANDARD.md, format version 2.0) and serialize.go v2.0.0:
-- the same values produce the same bytes. Load it as require("./serialize")
-- from a file in the same directory; every module it loads is required as
-- "./serialize_<name>" from this directory alone (DESIGN.md (f)).
--
-- The streams live in serialize_stream.luau. Every function here that belongs
-- to a family forwards to that family's module, loaded lazily on the first
-- call (DESIGN.md (h)); while the file is absent the call raises
-- `serialize: <name> not implemented`.
--
-- Copyright © 2016 - 2026, Más Bandwidth LLC. BSD 3-Clause (see LICENSE).

local Stream = require("./serialize_stream")

export type Holder<T> = Stream.Holder<T>
export type Int128 = Stream.Int128

local family = Stream.family

-- lazyClass is a family's class (BitWriter, Int128 ...) reached through the
-- entry: each field is read from the family module on first use.
local function lazyClass(name: string, class: string): any
	return table.freeze(setmetatable({}, {
		__index = function(_, key)
			return family(name)[class][key]
		end,
	}))
end

local S = {}

-- The wire format version this library implements (STANDARD.md, Format version).
S.FORMAT_VERSION = "2.0"

S.SerializeError = Stream.SerializeError
S.WriteStream = Stream.WriteStream
S.ReadStream = Stream.ReadStream
S.MeasureStream = Stream.MeasureStream
S.Continue = Stream.Continue
S.Until = Stream.Until

-- refusalName(reason) is the refusal-name function (IMPLEMENTING.md U14): the
-- reason's name, "none" for nil.
S.refusalName = Stream.refusalName

-- copyString(source, destSize) is serialize_copy_string (IMPLEMENTING.md
-- U15): at most destSize - 1 bytes of source, stopping at a NUL, cut back to
-- a UTF-8 character boundary.
function S.copyString(source: string, destSize: number): string
	return family("bytes").copyString(source, destSize)
end

-- serialize_bitpacker.luau
S.BitWriter = lazyClass("bitpacker", "BitWriter")
S.BitReader = lazyClass("bitpacker", "BitReader")

-- serialize_int128.luau
S.Int128 = lazyClass("int128", "Int128")
S.Uint128 = lazyClass("int128", "Uint128")

-- serialize_bits.luau: bitsRequired (STANDARD.md int (ranged)) and zig-zag.
function S.bitsRequired(min: number, max: number): number
	return family("bits").bitsRequired(min, max)
end

function S.bitsRequired64(min: integer, max: integer): number
	return family("bits").bitsRequired64(min, max)
end

function S.signedToUnsigned(n: number): number
	return family("bits").signedToUnsigned(n)
end

function S.unsignedToSigned(n: number): number
	return family("bits").unsignedToSigned(n)
end

-- serialize_int128.luau: bitsRequired128 (STANDARD.md int128 (ranged)).
function S.bitsRequired128(min: Int128, max: Int128): number
	return family("int128").bitsRequired128(min, max)
end

-- serialize_compressed_float.luau: the precomputed compressed float constants
-- (STANDARD.md compressed_float): maxIntegerValue, bits, delta.
function S.compressedFloatParams(min: number, max: number, resolution: number): (number, number, number)
	return family("compressed_float").compressedFloatParams(min, max, resolution)
end

return table.freeze(S)
