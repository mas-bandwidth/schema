-- The Lua packet finite-float gate (SPEC §4.3, serialize format 2.0). Loads the
-- generated Finite.schema module and its vendored serialize.lua runtime from
-- the directory given as the first argument (the negative controls pass their
-- sabotaged copy), then checks every way a bare float reaches the Lua packet
-- wire: a float32 and a float64 each alone, a float64 on a byte and one behind
-- a bool, both widths as list elements, and a fixed array of float64.
--
-- The rule, through the runtime: a NaN (any sign, any payload) or an infinity
-- is refused on write (serialize.lua traps write misuse, IMPLEMENTING.md U9,
-- naming non_finite_float) and on read on its raw bits (the reader returns
-- false and the stream latches non_finite_float). Negative zero, the
-- subnormals and the largest finite values ride bit exact. The quantized
-- float32 rides its step index, not the pattern; its write refuses a
-- non-finite value too.
--
-- Each failing vector prints "FAILED: <vector>: <detail>". The negative
-- controls compare the set of failing vector names against the .expected list
-- beside this file. Prints OK and exits 0, or exits 1.
local dir = assert(arg[1], "usage: lua main.lua <generated dir>")
package.path = dir .. "/?.lua;" .. package.path

local serialize = require("serialize")
local F = require("Finite")

local failures = 0
local function fail(name, detail)
  io.stderr:write("FAILED: " .. name .. ": " .. detail .. "\n")
  failures = failures + 1
end

local function hex(s)
  return (s:gsub(".", function(c) return string.format("%02x", c:byte()) end))
end

local function f32(bits) return (string.unpack("<f", string.pack("<I4", bits))) end
local function f64(bits) return (string.unpack("<d", string.pack("<i8", bits))) end
local function bits32(v) return (string.unpack("<I4", string.pack("<f", v))) end
local function bits64(v) return (string.unpack("<i8", string.pack("<d", v))) end

-- The non-finite patterns: quiet and signalling NaN of either sign, a NaN with
-- a payload, and both infinities. Each has an all-ones exponent field.
local nonFinite32 = {
  { "quiet-nan", 0x7fc00000 }, { "signalling-nan", 0x7f800001 },
  { "negative-nan", 0xffc00000 }, { "payload-nan", 0x7fc0dead },
  { "positive-inf", 0x7f800000 }, { "negative-inf", 0xff800000 },
}
local nonFinite64 = {
  { "quiet-nan", 0x7ff8000000000000 }, { "signalling-nan", 0x7ff0000000000001 },
  { "negative-nan", 0xfff8000000000000 }, { "payload-nan", 0x7ff800000000dead },
  { "positive-inf", 0x7ff0000000000000 }, { "negative-inf", 0xfff0000000000000 },
}
-- The finite edge patterns that must ride bit exact: no writer normalizes one.
local finite32 = {
  { "negative-zero", 0x80000000 }, { "min-subnormal", 0x00000001 },
  { "max-subnormal", 0x007fffff }, { "negative-subnormal", 0x80000001 },
  { "max-finite", 0x7f7fffff }, { "negative-max-finite", 0xff7fffff },
}
local finite64 = {
  { "negative-zero", 0x8000000000000000 }, { "min-subnormal", 0x0000000000000001 },
  { "max-subnormal", 0x000fffffffffffff }, { "negative-subnormal", 0x8000000000000001 },
  { "max-finite", 0x7fefffffffffffff }, { "negative-max-finite", 0xffefffffffffffff },
}

-- A Finite whose every float is a plain finite value; the vectors overwrite
-- one position.
local function finite()
  local v = F.NewFinite()
  v.Aligned, v.Single, v.Flag, v.Unaligned = 1.5, 2.5, true, 3.5
  v.Singles, v.SinglesCount = { 4.5 }, 1
  v.Doubles, v.DoublesCount = { 5.5 }, 1
  v.Quantized = 5.0
  return v
end

-- rawFinite lays out Finite's wire from raw patterns, bypassing every float
-- entry point, so a reader can be handed a pattern no conforming writer emits.
local QUANTIZED_BITS = 10 -- ceil(10 / 0.01) = 1000 steps
local function rawFinite(r)
  local s = serialize.WriteStream.new(64)
  assert(s:serializeBits64({ value = r.aligned }, 64))
  assert(s:serializeBits({ value = r.single }, 32))
  assert(s:serializeBool({ value = true }))
  assert(s:serializeBits64({ value = r.unaligned }, 64))
  assert(s:serializeBits({ value = 1 }, 3))
  assert(s:serializeBits({ value = r.singles }, 32))
  assert(s:serializeBits({ value = 1 }, 3))
  assert(s:serializeBits64({ value = r.doubles }, 64))
  assert(s:serializeBits({ value = r.quantized }, QUANTIZED_BITS))
  s:flush()
  return s:data()
end

local function rawPair(a, b)
  local s = serialize.WriteStream.new(32)
  assert(s:serializeBits64({ value = a }, 64))
  assert(s:serializeBits64({ value = b }, 64))
  s:flush()
  return s:data()
end

-- The raw layout of finite() above.
local function rawBase()
  return {
    aligned = bits64(1.5), single = bits32(2.5), unaligned = bits64(3.5),
    singles = bits32(4.5), doubles = bits64(5.5), quantized = 500,
  }
end

-- write runs a generated writer into a fresh stream: true and the bytes, or
-- false and the refusal (a false return, or the runtime's trap message).
local function write(writer, value)
  local s = serialize.WriteStream.new(64)
  local okCall, result = pcall(writer, s, value)
  if not okCall then return false, tostring(result) end
  if result ~= true then return false, "returned " .. tostring(result) end
  s:flush()
  return true, s:data()
end

local function read(reader, value, wire)
  local s = serialize.ReadStream.new(wire)
  local okCall, result = pcall(reader, s, value)
  if not okCall then return nil, "raised " .. tostring(result) end
  return result, s:error()
end

local function checkWriteRefused(name, writer, value, wantReason)
  local ok, detail = write(writer, value)
  if ok then
    fail(name, "write accepted a non-finite float (" .. hex(detail) .. ")")
  elseif wantReason and not string.find(detail, wantReason, 1, true) then
    fail(name, "write refused with " .. detail .. ", want " .. wantReason)
  end
end

local function checkReadRefused(name, reader, value, wire)
  local ok, reason = read(reader, value, wire)
  if ok == nil then
    fail(name, "the reader " .. reason .. "; a reader refuses, never raises")
  elseif ok then
    fail(name, "read accepted a non-finite pattern")
  elseif reason ~= "non_finite_float" then
    fail(name, "read refused with " .. tostring(reason) .. ", want non_finite_float")
  end
end

local function checkRoundTrip(name, writer, reader, value, newValue, want, same)
  local ok, wire = write(writer, value)
  if not ok then
    fail(name, "write refused a finite value: " .. wire)
    return
  end
  if wire ~= want then
    fail(name, "wire " .. hex(wire) .. ", want " .. hex(want) .. ": the pattern did not ride bit exact")
    return
  end
  local got = newValue()
  local rok, reason = read(reader, got, wire)
  if not rok then
    fail(name, "read refused a finite pattern: " .. tostring(reason))
    return
  end
  if not same(got) then
    fail(name, "read back a value that is not the pattern written")
  end
end

-- The positions a float32 and a float64 take in Finite, as a setter on a value
-- and a setter on the raw layout.
local positions32 = {
  { "single", function(v, x) v.Single = x end, function(r, b) r.single = b end },
  { "singles", function(v, x) v.Singles[1] = x end, function(r, b) r.singles = b end },
}
local positions64 = {
  { "aligned", function(v, x) v.Aligned = x end, function(r, b) r.aligned = b end },
  { "unaligned", function(v, x) v.Unaligned = x end, function(r, b) r.unaligned = b end },
  { "doubles", function(v, x) v.Doubles[1] = x end, function(r, b) r.doubles = b end },
}

local vectors = 0

for _, c in ipairs(nonFinite32) do
  local name, pattern = c[1], c[2]
  for _, p in ipairs(positions32) do
    local v = finite()
    p[2](v, f32(pattern))
    checkWriteRefused(p[1] .. "/write/" .. name, F.WriteFinite, v, "non_finite_float")
    local r = rawBase()
    p[3](r, pattern)
    checkReadRefused(p[1] .. "/read/" .. name, F.ReadFinite, F.NewFinite(), rawFinite(r))
    vectors = vectors + 2
  end
  local q = finite()
  q.Quantized = f32(pattern)
  checkWriteRefused("quantized/write/" .. name, F.WriteFinite, q, nil)
  vectors = vectors + 1
end

for _, c in ipairs(nonFinite64) do
  local name, pattern = c[1], c[2]
  for _, p in ipairs(positions64) do
    local v = finite()
    p[2](v, f64(pattern))
    checkWriteRefused(p[1] .. "/write/" .. name, F.WriteFinite, v, "non_finite_float")
    local r = rawBase()
    p[3](r, pattern)
    checkReadRefused(p[1] .. "/read/" .. name, F.ReadFinite, F.NewFinite(), rawFinite(r))
    vectors = vectors + 2
  end
  local pair = F.NewPair()
  pair.Values = { 1.0, f64(pattern) }
  checkWriteRefused("pair/write/" .. name, F.WritePair, pair, "non_finite_float")
  checkReadRefused("pair/read/" .. name, F.ReadPair, F.NewPair(), rawPair(bits64(1.0), pattern))
  vectors = vectors + 2
  local q = finite()
  q.Quantized = f64(pattern)
  checkWriteRefused("quantized/write/double-" .. name, F.WriteFinite, q, nil)
  vectors = vectors + 1
end

-- A finite double above FLT_MAX narrows to infinity in a float32 field: the
-- writer refuses it (serialize.lua's float32 rounding is Go's conversion).
do
  local v = finite()
  v.Single = 1e39
  checkWriteRefused("single/write/narrows-to-inf", F.WriteFinite, v, "non_finite_float")
  vectors = vectors + 1
end

for _, c in ipairs(finite32) do
  local name, pattern = c[1], c[2]
  for _, p in ipairs(positions32) do
    local v = finite()
    p[2](v, f32(pattern))
    local r = rawBase()
    p[3](r, pattern)
    checkRoundTrip(p[1] .. "/roundtrip/" .. name, F.WriteFinite, F.ReadFinite, v, F.NewFinite, rawFinite(r),
      function(got)
        local x = p[1] == "single" and got.Single or got.Singles[1]
        return bits32(x) == pattern
      end)
    vectors = vectors + 1
  end
end

for _, c in ipairs(finite64) do
  local name, pattern = c[1], c[2]
  for _, p in ipairs(positions64) do
    local v = finite()
    p[2](v, f64(pattern))
    local r = rawBase()
    p[3](r, pattern)
    checkRoundTrip(p[1] .. "/roundtrip/" .. name, F.WriteFinite, F.ReadFinite, v, F.NewFinite, rawFinite(r),
      function(got)
        local x = (p[1] == "aligned" and got.Aligned) or (p[1] == "unaligned" and got.Unaligned) or got.Doubles[1]
        return bits64(x) == pattern
      end)
    vectors = vectors + 1
  end
  local pair = F.NewPair()
  pair.Values = { f64(pattern), 1.0 }
  checkRoundTrip("pair/roundtrip/" .. name, F.WritePair, F.ReadPair, pair, F.NewPair, rawPair(pattern, bits64(1.0)),
    function(got) return bits64(got.Values[1]) == pattern end)
  vectors = vectors + 1
end

if failures > 0 then
  io.stderr:write(string.format("packet finite Lua: %d of %d vectors FAILED\n", failures, vectors))
  os.exit(1)
end
print(string.format("OK: packet finite Lua, %d vectors", vectors))
