--!strict
-- Packet floats are finite (SPEC §4.3, serialize format 2.0), the moo leg.
-- A float is its IEEE bit pattern in moo (docs/PORT-MOO.md, THE VALUE
-- MAPPING): a float32 a number holding the binary32 pattern, a float64 a
-- serialize.Uint64 holding the binary64 pattern.
--
-- THE WRITE HALF: at every float slot (the bare float32 and float64 fields,
-- a fixed float32 array element, a counted float64 array element, the
-- compressed float32) every NaN and infinity pattern makes the generated
-- write return false, unlatched and untrapped (SPEC §6.3's early-out; the
-- stream's refusal stays "none").
-- THE READ HALF: crafted bytes carrying an all-ones exponent at each bare
-- and array slot are refused as non_finite_float; the largest finite pattern
-- beside them reads.
-- -0 AND THE SUBNORMALS ride bit exact both ways.
--
-- Every failed vector prints one line, "FAILED: <vector>: <detail>"; the
-- negative controls compare the failing vector names with write.expected and
-- read.expected beside this file.
local serialize = require("./serialize")
local f = require("./Finite")

local U64 = serialize.Uint64
local failed = 0

local function fail(vector: string, detail: string)
	print("FAILED: " .. vector .. ": " .. detail)
	failed += 1
end

-- The wire layout of Floats, LSB-first bit offsets: narrow at 0, wide at 32,
-- narrows[i] at 96 + 32(i - 1), the count of wides in 3 bits at 192,
-- wides[i] at 195 + 64(i - 1), then the step index in 11 bits: 462 bits.
local NARROW_AT, WIDE_AT = 0, 32
local function narrowsAt(i: number): number
	return 96 + 32 * (i - 1)
end
local function widesAt(i: number): number
	return 195 + 64 * (i - 1)
end
local FULL_BITS = 462

local function getBits(b: buffer, offset: number, bits: number): number
	local value = 0
	for i = 0, bits - 1 do
		local at = offset + i
		if bit32.band(buffer.readu8(b, bit32.rshift(at, 3)), bit32.lshift(1, bit32.band(at, 7))) ~= 0 then
			value = bit32.bor(value, bit32.lshift(1, i))
		end
	end
	return value
end

local function putBits(b: buffer, offset: number, bits: number, value: number)
	for i = 0, bits - 1 do
		local at = offset + i
		local index = bit32.rshift(at, 3)
		local mask = bit32.lshift(1, bit32.band(at, 7))
		local byte = buffer.readu8(b, index)
		if bit32.extract(value, i) == 1 then
			byte = bit32.bor(byte, mask)
		else
			byte = bit32.band(byte, bit32.bnot(mask))
		end
		buffer.writeu8(b, index, byte)
	end
end

-- a float64 pattern as its two 32-bit halves on the wire, low half first
local function get64(b: buffer, offset: number): any
	return U64.new(getBits(b, offset, 32), getBits(b, offset + 32, 32))
end

local function put64(b: buffer, offset: number, pattern: any)
	putBits(b, offset, 32, pattern[1])
	putBits(b, offset + 32, 32, pattern[2])
end

local function hex64(pattern: any): string
	return string.format("0x%08x%08x", pattern[2], pattern[1])
end

local function baseline(): any
	local v = f.NewFloats()
	v.Narrow = 0x3fc00000 -- 1.5
	v.Wide = U64.new(0x00000000, 0xc0020000) -- -2.25
	v.Narrows[1] = 0x3f000000 -- 0.5
	v.Narrows[2] = 0xbf400000 -- -0.75
	v.Narrows[3] = 0x40400000 -- 3
	v.WidesCount = 4
	v.Wides[1] = U64.new(0, 0x3ff00000) -- 1
	v.Wides[2] = U64.new(0, 0x40000000) -- 2
	v.Wides[3] = U64.new(0, 0xc0100000) -- -4
	v.Wides[4] = U64.new(0, 0x40200000) -- 8
	v.Quantized = 0x3fa00000 -- 1.25
	return v
end

local function write(value: any): (boolean, buffer, any)
	local stream = serialize.WriteStream.new(buffer.create(f.FloatsMaxBytes))
	local ok = f.WriteFloats(stream, value)
	if ok then
		stream:flush()
		return true, stream:data(), stream
	end
	return false, buffer.create(0), stream
end

local function read(b: buffer, into: any): (boolean, string)
	local stream = serialize.ReadStream.new(b)
	local ok = f.ReadFloats(stream, into)
	return ok, stream:refusal()
end

-- THE WRITE HALF.
local nonFinite32 = { 0x7f800000, 0xff800000, 0x7fc00000, 0xffc00000, 0x7f800001, 0x7fffffff, 0xff812345 }
local nonFinite64 = {
	U64.new(0x00000000, 0x7ff00000), -- +inf
	U64.new(0x00000000, 0xfff00000), -- -inf
	U64.new(0x00000000, 0x7ff80000), -- quiet NaN
	U64.new(0x00000000, 0xfff80000), -- -quiet NaN
	U64.new(0x00000001, 0x7ff00000), -- signalling NaN, lowest payload
	U64.new(0xffffffff, 0x7fffffff), -- NaN, every payload bit
	U64.new(0xdeadbeef, 0xfff00000), -- -NaN, a payload
}

local function checkWriteRefused(vector: string, value: any)
	local stream = serialize.WriteStream.new(buffer.create(f.FloatsMaxBytes))
	local ran, ok = pcall(f.WriteFloats, stream, value)
	if not ran then
		fail(vector, "the write trapped (" .. tostring(ok) .. ")")
	elseif ok then
		fail(vector, "the write accepted a non-finite pattern")
	elseif stream:refusal() ~= "none" then
		fail(vector, "the write latched " .. stream:refusal() .. ", want an unlatched false")
	end
end

local writeSlots32: { { any } } = {
	{ "narrow", function(v: any, x: number) v.Narrow = x end },
	{ "narrows[2]", function(v: any, x: number) v.Narrows[2] = x end },
	{ "quantized", function(v: any, x: number) v.Quantized = x end },
}
local writeSlots64: { { any } } = {
	{ "wide", function(v: any, x: any) v.Wide = x end },
	{ "wides[3]", function(v: any, x: any) v.Wides[3] = x end },
}
for _, slot in writeSlots32 do
	for _, bits in nonFinite32 do
		local value = baseline()
		slot[2](value, bits)
		checkWriteRefused(string.format("write/%s/0x%08x", slot[1], bits), value)
	end
end
for _, slot in writeSlots64 do
	for _, pattern in nonFinite64 do
		local value = baseline()
		slot[2](value, pattern)
		checkWriteRefused(string.format("write/%s/%s", slot[1], hex64(pattern)), value)
	end
end

-- THE READ HALF.
local wrote, clean = write(baseline())
if not wrote then
	fail("baseline", "the baseline write refused")
end
if buffer.len(clean) * 8 < FULL_BITS then
	fail("baseline", string.format("the wire is %d bytes, want %d bits", buffer.len(clean), FULL_BITS))
end
local cleanOk = read(clean, f.NewFloats())
if not cleanOk then
	fail("baseline", "the baseline wire does not read")
end

local function crafted(): buffer
	local b = buffer.create(buffer.len(clean))
	buffer.copy(b, 0, clean)
	return b
end

local slots32 = { { "narrow", NARROW_AT } }
for i = 1, 3 do
	table.insert(slots32, { "narrows[" .. i .. "]", narrowsAt(i) })
end
local slots64 = { { "wide", WIDE_AT } }
for i = 1, 4 do
	table.insert(slots64, { "wides[" .. i .. "]", widesAt(i) })
end
for _, slot in slots32 do
	local name, at = slot[1] :: string, slot[2] :: number
	for _, bits in nonFinite32 do
		local b = crafted()
		putBits(b, at, 32, bits)
		local vector = string.format("read/%s/0x%08x", name, bits)
		local ok, refusal = read(b, f.NewFloats())
		if ok then
			fail(vector, "the read accepted a non-finite pattern")
		elseif refusal ~= "non_finite_float" then
			fail(vector, "the read refused with " .. refusal .. ", want non_finite_float")
		end
	end
	local b = crafted()
	putBits(b, at, 32, 0x7f7fffff)
	local into = f.NewFloats()
	local ok = read(b, into)
	local back = if name == "narrow" then into.Narrow else into.Narrows[tonumber(string.match(name, "%d")) :: number]
	if not ok or back ~= 0x7f7fffff then
		fail("finite/" .. name, "the largest finite float32 does not read exactly")
	end
end
for _, slot in slots64 do
	local name, at = slot[1] :: string, slot[2] :: number
	for _, pattern in nonFinite64 do
		local b = crafted()
		put64(b, at, pattern)
		local vector = string.format("read/%s/%s", name, hex64(pattern))
		local ok, refusal = read(b, f.NewFloats())
		if ok then
			fail(vector, "the read accepted a non-finite pattern")
		elseif refusal ~= "non_finite_float" then
			fail(vector, "the read refused with " .. refusal .. ", want non_finite_float")
		end
	end
	local largest = U64.new(0xffffffff, 0x7fefffff)
	local b = crafted()
	put64(b, at, largest)
	local into = f.NewFloats()
	local ok = read(b, into)
	local back = if name == "wide" then into.Wide else into.Wides[tonumber(string.match(name, "%d")) :: number]
	if not ok or not U64.is(back) or U64.cmp(back, largest) ~= 0 then
		fail("finite/" .. name, "the largest finite float64 does not read exactly")
	end
end

-- -0 AND THE SUBNORMALS: bit exact on the wire and back.
local exact = {
	{ "-0", 0x80000000, U64.new(0x00000000, 0x80000000) },
	{ "smallest-subnormal", 0x00000001, U64.new(0x00000001, 0x00000000) },
	{ "-largest-subnormal", 0x807fffff, U64.new(0xffffffff, 0x800fffff) },
}
for _, case in exact do
	local name, b32, b64 = case[1] :: string, case[2] :: number, case[3]
	local value = baseline()
	value.Narrow = b32
	value.Narrows[3] = b32
	value.Wide = b64
	value.Wides[2] = b64
	local ok, bytes = write(value)
	if not ok then
		fail("exact/" .. name, "the write refused a finite pattern")
		continue
	end
	if getBits(bytes, NARROW_AT, 32) ~= b32 or getBits(bytes, narrowsAt(3), 32) ~= b32 then
		fail("exact/" .. name, "a float32 pattern did not ride bit exact")
	end
	if U64.cmp(get64(bytes, WIDE_AT), b64) ~= 0 or U64.cmp(get64(bytes, widesAt(2)), b64) ~= 0 then
		fail("exact/" .. name, "a float64 pattern did not ride bit exact")
	end
	local back = f.NewFloats()
	if not read(bytes, back) then
		fail("exact/" .. name, "the read refused a finite pattern")
		continue
	end
	if back.Narrow ~= b32 or back.Narrows[3] ~= b32 or U64.cmp(back.Wide, b64) ~= 0 or U64.cmp(back.Wides[2], b64) ~= 0 then
		fail("exact/" .. name, "the read did not carry the pattern back")
	end
end

if failed > 0 then
	error(string.format("packet finite floats moo: %d vector(s) failed", failed), 0)
end
print("packet finite floats moo: write and read refusals at every float slot, -0 and the subnormals OK")
