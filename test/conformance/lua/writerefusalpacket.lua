-- The Lua probe of the write-refusal-packet surface (docs/SPEC.md section 5.1,
-- test/conformance/write-refusal-packet/rows.txt). Every row builds a fresh
-- lawful PacketWriteRefusal, applies ONE op, writes it into a 256-byte stream
-- and prints "<name> refused" or "<name> wrote". The Lua leg refuses by
-- returning false from the generated Write; that is the only refusal this probe
-- reports. A write that raises is printed as "<name> raised" so it answers no
-- row and the check names it, never as a refusal.
local serialize = require("serialize")
local G = require("PacketWriteRefusal")
local inputs = require("inputs")

local function unhex(s)
  return (s:gsub("%x%x", function(h) return string.char(tonumber(h, 16)) end))
end

-- int128 is a { lo, hi } pair of two's-complement 64-bit lanes, and a ufixed(32, 32)
-- raw value a uint64 bit pattern in a Lua integer: the decimals rows.txt names
-- past int64 are spelled here, and any other must fit an integer.
local wide128 = {
  ["100000000000000000001"] = { lo = 0x6BC75E2D63100001, hi = 5 },
  ["100000000000000000000"] = { lo = 0x6BC75E2D63100000, hi = 5 },
}
local raw64 = {
  ["17179869184000000001"] = 0xEE6B280000000001,
  ["17179869184000000000"] = 0xEE6B280000000000,
}
local function integer(s)
  return raw64[s] or assert(math.tointeger(tonumber(s)), s)
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
  wide = function(v, s)
    local pair = wide128[s]
    if pair == nil then
      local n = integer(s)
      pair = { lo = n, hi = n < 0 and -1 or 0 }
    end
    v.Wide = pair
  end,
  fx = function(v, n) v.Fx = n end,
  ufx = function(v, s) v.Ufx = integer(s) end,
}

for line in inputs.rows:gmatch("[^\n]+") do
  local f = {}
  for w in line:gmatch("%S+") do f[#f + 1] = w end
  if #f == 4 and f[1]:sub(1, 1) ~= "#" then
    local name, op, arg = f[1], f[3], f[4]
    local set = assert(ops[op], "unknown op " .. op)
    local v = G.NewPacketWriteRefusal()
    if op ~= "text_bytes" and op ~= "wide" and op ~= "ufx" then arg = integer(arg) end
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
