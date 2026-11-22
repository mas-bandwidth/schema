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
  ratio = function(v, x) v.Ratio = x end,
}

for line in inputs.rows:gmatch("[^\n]+") do
  local f = {}
  for w in line:gmatch("%S+") do f[#f + 1] = w end
  if #f == 4 and f[1]:sub(1, 1) ~= "#" then
    local name, op, arg = f[1], f[3], f[4]
    local set = assert(ops[op], "unknown op " .. op)
    local v = G.NewPacketWriteRefusal()
    if op == "ratio" then
      if arg == "nan" then arg = 0 / 0
      elseif arg == "inf" then arg = math.huge
      elseif arg == "-inf" then arg = -math.huge
      else arg = assert(tonumber(arg), arg) end
    elseif op ~= "text_bytes" then arg = assert(math.tointeger(tonumber(arg)), arg) end
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
