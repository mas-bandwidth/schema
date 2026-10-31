--!strict
-- The Luau packet finite-float gate (SPEC §4.3, serialize format 2.0). Copied
-- into the directory the generator wrote Finite.schema's module and its
-- vendored serialize.luau runtime to (the negative controls copy it beside
-- their sabotaged runtime), then run there. It checks every way a bare float
-- reaches the Luau packet wire: a float32 and a float64 each alone, a float64
-- on a byte and one behind a bool, both widths as list elements, and a fixed
-- array of float64.
--
-- The rule, through the runtime: a NaN (any sign, any payload) or an infinity
-- is refused on write (serialize.luau traps write misuse, IMPLEMENTING.md U9:
-- "must be finite") and on read on its raw bits (the reader returns false and
-- the stream latches non_finite_float). Negative zero, the subnormals and the
-- largest finite values ride bit exact. The quantized float32 rides its step
-- index, not the pattern; its write refuses a non-finite value too.
--
-- Each failing vector prints "FAILED: <vector>: <detail>". The negative
-- controls compare the set of failing vector names against the .expected list
-- in test/packet-finite/luau. Prints OK, or ends with an error.
local serialize = require("./serialize")
local F = require("./Finite")

local failures = 0
local function fail(name: string, detail: string)
	print("FAILED: " .. name .. ": " .. detail)
	failures += 1
end

local function hex(b: buffer): string
	local parts = table.create(buffer.len(b), "")
	for i = 0, buffer.len(b) - 1 do
		parts[i + 1] = string.format("%02x", buffer.readu8(b, i))
	end
	return table.concat(parts)
end

-- A float64 pattern is its two 32-bit halves, { lo, hi }, in the wire's order.
type Bits64 = { number }

local function f32(bits: number): number
	local b = buffer.create(4)
	buffer.writeu32(b, 0, bits)
	return buffer.readf32(b, 0)
end
local function f64(p: Bits64): number
	local b = buffer.create(8)
	buffer.writeu32(b, 0, p[1])
	buffer.writeu32(b, 4, p[2])
	return buffer.readf64(b, 0)
end
local function bits32(v: number): number
	local b = buffer.create(4)
	buffer.writef32(b, 0, v)
	return buffer.readu32(b, 0)
end
local function bits64(v: number): Bits64
	local b = buffer.create(8)
	buffer.writef64(b, 0, v)
	return { buffer.readu32(b, 0), buffer.readu32(b, 4) }
end
local function same64(a: Bits64, b: Bits64): boolean
	return a[1] == b[1] and a[2] == b[2]
end

-- The non-finite patterns: quiet and signalling NaN of either sign, a NaN with
-- a payload, and both infinities. Each has an all-ones exponent field.
local nonFinite32: { { any } } = {
	{ "quiet-nan", 0x7fc00000 }, { "signalling-nan", 0x7f800001 },
	{ "negative-nan", 0xffc00000 }, { "payload-nan", 0x7fc0dead },
	{ "positive-inf", 0x7f800000 }, { "negative-inf", 0xff800000 },
}
local nonFinite64: { { any } } = {
	{ "quiet-nan", { 0, 0x7ff80000 } }, { "signalling-nan", { 1, 0x7ff00000 } },
	{ "negative-nan", { 0, 0xfff80000 } }, { "payload-nan", { 0xdead, 0x7ff80000 } },
	{ "positive-inf", { 0, 0x7ff00000 } }, { "negative-inf", { 0, 0xfff00000 } },
}
-- The finite edge patterns that must ride bit exact: no writer normalizes one.
local finite32: { { any } } = {
	{ "negative-zero", 0x80000000 }, { "min-subnormal", 0x00000001 },
	{ "max-subnormal", 0x007fffff }, { "negative-subnormal", 0x80000001 },
	{ "max-finite", 0x7f7fffff }, { "negative-max-finite", 0xff7fffff },
}
local finite64: { { any } } = {
	{ "negative-zero", { 0, 0x80000000 } }, { "min-subnormal", { 1, 0 } },
	{ "max-subnormal", { 0xffffffff, 0x000fffff } }, { "negative-subnormal", { 1, 0x80000000 } },
	{ "max-finite", { 0xffffffff, 0x7fefffff } }, { "negative-max-finite", { 0xffffffff, 0xffefffff } },
}

-- A Finite whose every float is a plain finite value; the vectors overwrite
-- one position.
local function finite(): any
	local v: any = F.NewFinite()
	v.Aligned, v.Single, v.Flag, v.Unaligned = 1.5, 2.5, true, 3.5
	v.Singles[1], v.SinglesCount = 4.5, 1
	v.Doubles[1], v.DoublesCount = 5.5, 1
	v.Quantized = 5.0
	return v
end

local function put64(s: any, p: Bits64)
	assert(s:serializeBits({ value = p[1] }, 32))
	assert(s:serializeBits({ value = p[2] }, 32))
end

-- rawFinite lays out Finite's wire from raw patterns, bypassing every float
-- entry point, so a reader can be handed a pattern no conforming writer emits.
local QUANTIZED_BITS = 10 -- ceil(10 / 0.01) = 1000 steps
local function rawFinite(r: any): buffer
	local s = serialize.WriteStream.new(buffer.create(64))
	put64(s, r.aligned)
	assert(s:serializeBits({ value = r.single }, 32))
	assert(s:serializeBool({ value = true }))
	put64(s, r.unaligned)
	assert(s:serializeBits({ value = 1 }, 3))
	assert(s:serializeBits({ value = r.singles }, 32))
	assert(s:serializeBits({ value = 1 }, 3))
	put64(s, r.doubles)
	assert(s:serializeBits({ value = r.quantized }, QUANTIZED_BITS))
	s:flush()
	return s:data()
end

local function rawPair(a: Bits64, b: Bits64): buffer
	local s = serialize.WriteStream.new(buffer.create(32))
	put64(s, a)
	put64(s, b)
	s:flush()
	return s:data()
end

-- The raw layout of finite() above.
local function rawBase(): any
	return {
		aligned = bits64(1.5), single = bits32(2.5), unaligned = bits64(3.5),
		singles = bits32(4.5), doubles = bits64(5.5), quantized = 500,
	}
end

-- write runs a generated writer into a fresh stream: true and the bytes' hex,
-- or false and the refusal (a false return, or the runtime's trap message).
local function write(writer: any, value: any): (boolean, string, buffer?)
	local s = serialize.WriteStream.new(buffer.create(64))
	local okCall, result = pcall(writer, s, value)
	if not okCall then
		return false, tostring(result), nil
	end
	if result ~= true then
		return false, "returned " .. tostring(result), nil
	end
	s:flush()
	local data = s:data()
	return true, hex(data), data
end

local function read(reader: any, value: any, wire: buffer): (boolean?, string)
	local s = serialize.ReadStream.new(wire)
	local okCall, result = pcall(reader, s, value)
	if not okCall then
		return nil, "raised " .. tostring(result)
	end
	return result == true, tostring(s:error())
end

local function checkWriteRefused(name: string, writer: any, value: any)
	local ok, detail = write(writer, value)
	if ok then
		fail(name, "write accepted a non-finite float (" .. detail .. ")")
	elseif not string.find(detail, "must be finite", 1, true) then
		fail(name, "write refused with " .. detail .. ", want the runtime's finite trap")
	end
end

local function checkReadRefused(name: string, reader: any, value: any, wire: buffer)
	local ok, reason = read(reader, value, wire)
	if ok == nil then
		fail(name, "the reader " .. reason .. "; a reader refuses, never raises")
	elseif ok then
		fail(name, "read accepted a non-finite pattern")
	elseif reason ~= "non_finite_float" then
		fail(name, "read refused with " .. reason .. ", want non_finite_float")
	end
end

local function checkRoundTrip(name: string, writer: any, reader: any, value: any, newValue: () -> any, want: buffer, same: (any) -> boolean)
	local ok, detail, wire = write(writer, value)
	if not ok or wire == nil then
		fail(name, "write refused a finite value: " .. detail)
		return
	end
	if detail ~= hex(want) then
		fail(name, "wire " .. detail .. ", want " .. hex(want) .. ": the pattern did not ride bit exact")
		return
	end
	local got = newValue()
	local rok, reason = read(reader, got, wire)
	if not rok then
		fail(name, "read refused a finite pattern: " .. reason)
		return
	end
	if not same(got) then
		fail(name, "read back a value that is not the pattern written")
	end
end

-- The positions a float32 and a float64 take in Finite, as a setter on a value
-- and a setter on the raw layout.
local positions32: { { any } } = {
	{ "single", function(v: any, x: number) v.Single = x end, function(r: any, b: number) r.single = b end },
	{ "singles", function(v: any, x: number) v.Singles[1] = x end, function(r: any, b: number) r.singles = b end },
}
local positions64: { { any } } = {
	{ "aligned", function(v: any, x: number) v.Aligned = x end, function(r: any, b: Bits64) r.aligned = b end },
	{ "unaligned", function(v: any, x: number) v.Unaligned = x end, function(r: any, b: Bits64) r.unaligned = b end },
	{ "doubles", function(v: any, x: number) v.Doubles[1] = x end, function(r: any, b: Bits64) r.doubles = b end },
}

local vectors = 0

for _, c in nonFinite32 do
	local name, pattern = c[1], c[2]
	for _, p in positions32 do
		local v = finite()
		p[2](v, f32(pattern))
		checkWriteRefused(p[1] .. "/write/" .. name, F.WriteFinite, v)
		local r = rawBase()
		p[3](r, pattern)
		checkReadRefused(p[1] .. "/read/" .. name, F.ReadFinite, F.NewFinite(), rawFinite(r))
		vectors += 2
	end
	local q = finite()
	q.Quantized = f32(pattern)
	checkWriteRefused("quantized/write/" .. name, F.WriteFinite, q)
	vectors += 1
end

for _, c in nonFinite64 do
	local name, pattern = c[1], c[2]
	for _, p in positions64 do
		local v = finite()
		p[2](v, f64(pattern))
		checkWriteRefused(p[1] .. "/write/" .. name, F.WriteFinite, v)
		local r = rawBase()
		p[3](r, pattern)
		checkReadRefused(p[1] .. "/read/" .. name, F.ReadFinite, F.NewFinite(), rawFinite(r))
		vectors += 2
	end
	local pair: any = F.NewPair()
	pair.Values[1], pair.Values[2] = 1.0, f64(pattern)
	checkWriteRefused("pair/write/" .. name, F.WritePair, pair)
	checkReadRefused("pair/read/" .. name, F.ReadPair, F.NewPair(), rawPair(bits64(1.0), pattern))
	vectors += 2
	local q = finite()
	q.Quantized = f64(pattern)
	checkWriteRefused("quantized/write/double-" .. name, F.WriteFinite, q)
	vectors += 1
end

-- A finite double above FLT_MAX narrows to infinity in a float32 field: the
-- writer refuses it (serialize.luau's float32 rounding is Go's conversion).
do
	local v = finite()
	v.Single = 1e39
	checkWriteRefused("single/write/narrows-to-inf", F.WriteFinite, v)
	vectors += 1
end

for _, c in finite32 do
	local name, pattern = c[1], c[2]
	for _, p in positions32 do
		local v = finite()
		p[2](v, f32(pattern))
		local r = rawBase()
		p[3](r, pattern)
		local which = p[1]
		checkRoundTrip(which .. "/roundtrip/" .. name, F.WriteFinite, F.ReadFinite, v, F.NewFinite, rawFinite(r),
			function(got: any): boolean
				local x = if which == "single" then got.Single else got.Singles[1]
				return bits32(x) == pattern
			end)
		vectors += 1
	end
end

for _, c in finite64 do
	local name, pattern = c[1], c[2]
	for _, p in positions64 do
		local v = finite()
		p[2](v, f64(pattern))
		local r = rawBase()
		p[3](r, pattern)
		local which = p[1]
		checkRoundTrip(which .. "/roundtrip/" .. name, F.WriteFinite, F.ReadFinite, v, F.NewFinite, rawFinite(r),
			function(got: any): boolean
				local x = if which == "aligned" then got.Aligned elseif which == "unaligned" then got.Unaligned else got.Doubles[1]
				return same64(bits64(x), pattern)
			end)
		vectors += 1
	end
	local pair: any = F.NewPair()
	pair.Values[1], pair.Values[2] = f64(pattern), 1.0
	checkRoundTrip("pair/roundtrip/" .. name, F.WritePair, F.ReadPair, pair, F.NewPair, rawPair(pattern, bits64(1.0)),
		function(got: any): boolean
			return same64(bits64(got.Values[1]), pattern)
		end)
	vectors += 1
end

if failures > 0 then
	error(string.format("packet finite Luau: %d of %d vectors FAILED", failures, vectors), 0)
end
print(string.format("OK: packet finite Luau, %d vectors", vectors))
