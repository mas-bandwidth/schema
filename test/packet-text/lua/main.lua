-- The Lua 5.4 driver of the packet UTF-8 sweep (test/packet-text/README.md):
-- one hexadecimal wire per input line ("-" is the empty wire), one verdict
-- per output line, "REFUSE" or "OK <bits> <payload> <bits> <canonical>".
local dir = assert(arg[1], "usage: lua main.lua <generated dir>")
package.path = dir .. "/?.lua;" .. package.path

local serialize = require("serialize")
local Narrow = require("Narrow")

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

for line in io.lines() do
  local wire = unhex(line)
  local value = Narrow.NewNarrow()
  local r = serialize.ReadStream.new(wire)
  if not Narrow.ReadNarrow(r, value) then
    print("REFUSE")
  else
    local w = serialize.WriteStream.new(256)
    assert(Narrow.WriteNarrow(w, value), "the accepted value writes")
    local bits = w:bitsProcessed()
    w:flush()
    local data = w:data()
    assert(r:bitsProcessed() == bits, "the read consumed exactly the bits the write wrote")
    print(string.format("OK %d %s %d %s", bits, hex(value.Text), bits, hex(data)))
  end
end
