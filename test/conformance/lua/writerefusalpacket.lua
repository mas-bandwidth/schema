-- The Lua probe of the write-refusal-packet surface (docs/SPEC.md section 5.1,
-- test/conformance/write-refusal-packet/rows.txt). Every row builds a fresh
-- lawful PacketWriteRefusal, applies ONE op, writes it into a 256-byte stream
-- and prints "<name> refused" or "<name> wrote". The Lua leg refuses by
-- returning false from the generated Write; that is the only refusal this probe
-- reports. A write that raises is printed as "<name> raised" so it answers no
-- row and the check names it, never as a refusal.
local serialize = require("serialize")
local G = require("RefusalPacket")
local inputs = require("inputs")

local function unhex(s)
  return (s:gsub("%x%x", function(h) return string.char(tonumber(h, 16)) end))
end

-- dec128 is a decimal argument as the int128 storage, { lo, hi }: the two's
-- complement halves, each a wrapping 64-bit integer. acc = acc * 10 + d runs
-- on 32-bit limbs of lo so the carry into hi is exact.
local function dec128(s)
  local neg, digits = s:match("^(%-?)(%d+)$")
  assert(digits, "rows: " .. s .. " is not a decimal integer")
  local lo, hi = 0, 0
  for d in digits:gmatch("%d") do
    local p0 = (lo & 0xffffffff) * 10 + tonumber(d)
    local p1 = (lo >> 32) * 10 + (p0 >> 32)
    lo = (p0 & 0xffffffff) | ((p1 & 0xffffffff) << 32)
    hi = hi * 10 + (p1 >> 32)
  end
  if neg == "-" then
    lo, hi = ~lo + 1, ~hi
    if lo == 0 then hi = hi + 1 end
  end
  return { lo = lo, hi = hi }
end

local ops = {
  scalar = function(v, n) v.Scalar = n end,
  narrow = function(v, n) v.Narrow = n end,
  grade = function(v, n) v.Grade = n end,
  shot_tag = function(v, n) v.Shot.Type = n end,
  window_count = function(v, n) v.WindowCount = n end,
  text_length = function(v, n) v.Text = string.rep("a", n) end,
  text_bytes = function(v, hex) v.Text = unhex(hex) end,
  text_fill = function(v, n) v.Text = string.rep("a", n) end,
  fixed_constant = function(v, n) v.FixedConstant = n end,
  reserved_zero = function(v, n) v.ReservedZero = n end,
  perks = function(v, n) v.Perks = n end,
  trio_len = function(v, n) local t = {}; for i = 1, n do t[i] = 0 end; v.Trio = t end,
  trio_elem = function(v, n) v.Trio[1] = n end,
  ints_elem = function(v, n) v.Ints[1] = n; v.IntsCount = 1 end,
  pick_small = function(v, n) v.Pick.Type = G.PickType.Small; v.Pick.Small.V = n end,
  huge = function(v, dec) v.Huge = dec128(dec) end,
  fx = function(v, n) v.Fx = n end, -- fixed(16, 16) storage is the raw integer
}

for line in inputs.rows:gmatch("[^\n]+") do
  local f = {}
  for w in line:gmatch("%S+") do f[#f + 1] = w end
  if #f == 4 and f[1]:sub(1, 1) ~= "#" then
    local name, op, arg = f[1], f[3], f[4]
    local set = assert(ops[op], "unknown op " .. op)
    local v = G.NewPacketWriteRefusal()
    if op ~= "text_bytes" and op ~= "huge" then arg = assert(math.tointeger(tonumber(arg)), arg) end
    set(v, arg)
    local stream = serialize.WriteStream.new(256)
    local ran, ok = pcall(G.WritePacketWriteRefusal, stream, v)
    if not ran then
      print(name .. " raised")
      io.stderr:write(name .. ": the writer raised: " .. tostring(ok) .. "\n")
    elseif ok == false then
      print(name .. " refused")
    elseif ok == true then
      print(name .. " wrote")
    else
      print(name .. " odd")
      io.stderr:write(name .. ": the writer returned " .. tostring(ok) .. "\n")
    end
  end
end
