-- The Lua 5.4 driver of the packet value-defaults fixture
-- (test/packet-defaults/README.md).
local dir = assert(arg[1], "usage: lua main.lua <generated dir> <goldens dir>")
local goldens = assert(arg[2], "usage: lua main.lua <generated dir> <goldens dir>")
package.path = dir .. "/?.lua;" .. dir .. "/plain/?.lua;" .. package.path

local serialize = require("serialize")
local d = require("Defaults")
local p = require("Plain")

local function check(ok, what)
  if not ok then
    error("FAILED: " .. what, 2)
  end
end

local function deepEqual(a, b)
  if type(a) ~= type(b) then return false end
  if type(a) ~= "table" then return a == b end
  for k, x in pairs(a) do
    if not deepEqual(x, b[k]) then return false end
  end
  for k in pairs(b) do
    if a[k] == nil then return false end
  end
  return true
end

local function readFile(path)
  local f = assert(io.open(path, "rb"))
  local s = f:read("a")
  f:close()
  return s
end

local function write(mod, name, value)
  local w = serialize.WriteStream.new(256)
  check(mod["Write" .. name](w, value), "Write" .. name)
  local bits = w:bitsProcessed()
  w:flush()
  return w:data(), bits
end

local function checkRead(name, bytes, bits, initial, want)
  local r = serialize.ReadStream.new(bytes)
  check(d["Read" .. name](r, initial), "Read" .. name)
  check(r:bitsProcessed() == bits, name .. " consumed bits")
  check(deepEqual(initial, want), name .. " decoded value")
end

local function pin(typeName, pinName, value, initial, want)
  local bytes = readFile(goldens .. "/" .. pinName .. ".bin")
  local bits = tonumber((readFile(goldens .. "/" .. pinName .. ".bits"):gsub("%s+", "")))
  local gotBytes, gotBits = write(d, typeName, value)
  check(gotBytes == bytes and gotBits == bits, "C++ pin " .. pinName)
  checkRead(typeName, bytes, bits, initial(), want())
end

local function zero()
  local v = d.NewSample()
  v.Name, v.Token, v.Caps = "", "", 0
  v.EmptyName, v.EmptyToken, v.EmptyCaps = "", "", 0
  return v
end

local function sample()
  local v = zero()
  v.Name = string.char(195, 169, 240, 144, 128, 128)
  v.Token = string.char(92, 110, 92, 116)
  v.Caps = 5
  return v
end

local function short()
  local v = zero()
  v.Name = "A"
  v.Token = string.char(0, 255)
  v.Caps = 2
  return v
end

local function dirty()
  local v = zero()
  v.Name = string.rep("d", 6)
  v.Token = string.rep("\xa1", 4)
  v.Caps = 7
  v.EmptyName = string.rep("o", 3)
  v.EmptyToken = string.rep("\xb1", 2)
  v.EmptyCaps = 7
  return v
end

check(deepEqual(d.NewSample(), sample()), "packet-default constructor bytes")
local zSample = dirty()
d.ZeroSample(zSample)
check(deepEqual(zSample, zero()), "ZeroSample resets to zero form")

local empty = d.NewEmptyOnly()
check(empty.Name == "" and empty.Token == "" and empty.Caps == 0, "explicit empty defaults")

local prefix = d.NewPrefix()
check(prefix.Name == string.char(195, 169) and prefix.Token == string.char(92, 110), "short string and bytes defaults")

local wide = d.NewWideMask()
check(wide.High == (1 << 63) and wide.All == -1, "bit-63 and full 64-bit masks")
local wideBytes = string.rep("\0", 7) .. "\x80" .. string.rep("\xff", 8)
local gotWide, wideBits = write(d, "WideMask", wide)
check(gotWide == wideBytes and wideBits == 128, "WideMask wire")
local wideInit = d.NewWideMask()
wideInit.High, wideInit.All = 0, 0
checkRead("WideMask", wideBytes, 128, wideInit, wide)

local split = d.NewSplitMask()
split.Lead, split.Mask, split.Tail = 5, 1 << 32, 2
local gotSplit, splitBits = write(d, "SplitMask", split)
check(gotSplit == "\x05\x00\x00\x00\x28" and splitBits == 38, "SplitMask wire")
checkRead("SplitMask", "\x05\x00\x00\x00\x28", 38, d.NewSplitMask(), split)

local plain = p.NewSample()
for k, x in pairs(sample()) do plain[k] = x end
local plainBytes, plainBits = write(p, "Sample", plain)
local defBytes, defBits = write(d, "Sample", d.NewSample())
check(plainBytes == defBytes and plainBits == defBits, "defaultless twin")

pin("Sample", "sample-defaults", d.NewSample(), zero, sample)

local batch = d.NewBatch()
check(batch.CountedCount == 1 and deepEqual(batch.Head, sample()), "Batch born at defaults")
for _, s in ipairs(batch.Items) do check(deepEqual(s, sample()), "Batch fixed element at defaults") end
for _, s in ipairs(batch.Counted) do check(deepEqual(s, sample()), "Batch counted backing element at defaults") end
local function reusedBatch()
  local b = d.NewBatch()
  b.Counted[2] = dirty()
  b.Counted[3] = short()
  return b
end
pin("Batch", "batch-defaults", batch, reusedBatch, reusedBatch)

local z = d.NewZeroCount()
check(z.ItemsCount == 0 and deepEqual(z.Items, { sample(), sample() }), "ZeroCount born empty over defaulted elements")
pin("ZeroCount", "zero-count", z, function()
  local v = d.NewZeroCount()
  v.Items, v.ItemsCount = { dirty(), short() }, 2
  return v
end, function()
  local v = d.NewZeroCount()
  v.Items, v.ItemsCount = { dirty(), short() }, 0
  return v
end)

check(d.NewConditional().Enabled == true and deepEqual(d.NewConditional().Value, sample()), "Conditional born enabled at defaults")
pin("Conditional", "conditional-on", d.NewConditional(), function()
  local v = d.NewConditional()
  v.Enabled, v.Value = false, zero()
  return v
end, d.NewConditional)

local off = d.NewConditional()
off.Enabled = false
pin("Conditional", "conditional-off", off, function()
  local v = d.NewConditional()
  v.Value = dirty()
  return v
end, function()
  local v = d.NewConditional()
  v.Enabled, v.Value = false, zero()
  return v
end)

local function choice()
  local v = d.NewChoice()
  v.Type = d.ChoiceType.Sample
  return v
end
pin("Choice", "choice-sample", choice(), function()
  local v = d.NewChoice()
  v.Type = d.ChoiceType.Conditional
  return v
end, choice)
pin("Sample", "sample-short", short(), dirty, short)
pin("Sample", "sample-empty", zero(), dirty, zero)

for _, sent in ipairs({ short(), zero() }) do
  local value = choice()
  value.Sample = sent
  local wireBytes, wireBits = write(d, "Choice", value)
  local output = d.NewChoice()
  output.Type = d.ChoiceType.Conditional
  for _ = 1, 2 do
    output.Sample = dirty()
    local want = choice()
    want.Sample = sent
    checkRead("Choice", wireBytes, wireBits, output, want)
  end
end

local offChoice = d.NewChoice()
offChoice.Type = d.ChoiceType.Conditional
offChoice.Conditional.Enabled = false
local offBytes, offBits = write(d, "Choice", offChoice)
local output = choice()
for _ = 1, 2 do
  output.Conditional.Enabled = true
  output.Conditional.Value = dirty()
  local want = d.NewChoice()
  want.Type = d.ChoiceType.Conditional
  want.Conditional.Enabled = false
  want.Conditional.Value = zero()
  checkRead("Choice", offBytes, offBits, output, want)
end
print("packet defaults Lua: constructors, Zero, eight C++ goldens and reused storage OK")
