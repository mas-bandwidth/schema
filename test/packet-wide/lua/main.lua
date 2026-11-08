-- The Lua 5.4 driver of the packet wstring gate (test/packet-wide, SPEC §4.12).
local dir = assert(arg[1], "usage: lua main.lua <generated dir> [--contracts]")
local contracts = arg[2] == "--contracts"
package.path = dir .. "/?.lua;" .. dir .. "/shapes/?.lua;" .. package.path

local serialize = require("serialize")
local W = require("WideText")

local function unhex(s)
  if s == "-" or s == "" then return "" end
  return (s:gsub("..", function(cc)
    return string.char(tonumber(cc, 16))
  end))
end

local function hex(s)
  if #s == 0 then return "-" end
  return (s:gsub(".", function(c)
    return string.format("%02x", c:byte())
  end))
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

local function encode(mod, name, v)
  local w = serialize.WriteStream.new(1024)
  assert(mod["Write" .. name](w, v), "Write" .. name .. " failed")
  local bits = w:bitsProcessed()
  w:flush()
  return w:data(), bits
end

local function decode(mod, name, v, bytes)
  local r = serialize.ReadStream.new(bytes)
  local ok = mod["Read" .. name](r, v)
  return ok, r:bitsProcessed()
end

if contracts then
  local v = W.NewWideSeven()
  for _, bad in ipairs({
    "not_a_table",
    { 1, 2, 3, 4, 5, 6, 7, 8 },
    { 0 },
    { 0x10000 },
    { 1.5 },
  }) do
    v.Text = bad
    assert(W.WriteWideSeven(serialize.WriteStream.new(256), v) == false, "WriteWideSeven accepted invalid wstring")
  end
  v.Text = { 0xd800 }
  local bytes, _ = encode(W, "WideSeven", v)
  assert(decode(W, "WideSeven", v, bytes) == false, "lone surrogate must be refused on read")
  v.Text = { 0xffff }
  bytes, _ = encode(W, "WideSeven", v)
  v.Text = { 0x7f7f, 0x7f7f, 0x7f7f }
  assert(decode(W, "WideSeven", v, bytes) == true, "valid BMP unit must decode")
  assert(deepEqual(v.Text, { 0xffff }), "decoded wstring replaces previous sequence")

  local P = require("Shapes")
  local c = P.NewConditional()
  c.Enabled = true
  c.Text = { 0xd800, 0xdc00 }
  local cb, cbits = encode(P, "Conditional", c)
  assert(cbits == 68 and (cb:byte(1) & 15) == 5, "Conditional wire bits and header")
  local co = P.NewConditional()
  assert(decode(P, "Conditional", co, cb) and deepEqual(co, c), "Conditional round-trip")
  c.Enabled = false
  cb, _ = encode(P, "Conditional", c)
  assert(decode(P, "Conditional", co, cb) and deepEqual(co, P.NewConditional()), "Conditional disabled zeroes Text")

  local box = P.NewBox()
  assert(box.CountedCount == 1, "Box born count")
  box.Items[1].Value = { 0xffff }
  box.Counted[1].Value = { 65 }
  box.Choice.Type = P.ChoiceType.Text
  box.Choice.Text.Value = { 66 }
  local bb, _ = encode(P, "Box", box)
  local bo = P.NewBox()
  bo.Counted[2].Value = { 0x7f7f }
  for _ = 1, 2 do
    bo.Choice.Text.Value = { 0x7f7f, 0x7f7f }
    assert(decode(P, "Box", bo, bb), "Box decodes")
    assert(deepEqual(bo.Items, box.Items), "Box fixed array")
    assert(deepEqual(bo.Counted[1], box.Counted[1]), "Box counted element 1")
    assert(bo.CountedCount == 1, "Box CountedCount")
    assert(deepEqual(bo.Counted[2].Value, { 0x7f7f }), "Box element past count untouched")
    assert(deepEqual(bo.Choice.Text, box.Choice.Text), "Box selected union arm")
  end
  print("packet wide Lua contracts: OK")
  return
end

for line in io.lines() do
  local bound, raw = line:match("^(%d+) (.*)$")
  local name = bound == "7" and "WideSeven" or (bound == "4" and "WideFour" or nil)
  assert(name ~= nil and raw ~= nil, "a line is <bound> <wire>")
  local wire = unhex(raw)
  local value = W["New" .. name]()
  value.Text = { 0x7f7f, 0x7f7f }
  local ok, consumed = decode(W, name, value, wire)
  if not ok then
    print("REFUSE")
  else
    local bytes, bits = encode(W, name, value)
    assert(consumed == bits, "the read consumed exactly the bits the write wrote")
    local units = {}
    for i = 1, #value.Text do
      units[i] = string.format("%04x", value.Text[i])
    end
    local payload = #units == 0 and "-" or table.concat(units)
    print(string.format("OK %d %s %d %s", bits, payload, bits, hex(bytes)))
  end
end
