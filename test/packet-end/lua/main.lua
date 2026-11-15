-- The Lua 5.4 driver of the packet-end conformance vectors
-- (testdata/wire/packet-end/README.md): read each packet with the generated
-- one-packet read and hold its verdict.
local dir = assert(arg[1], "usage: lua main.lua <generated dir> <vectors dir>")
local vectors = assert(arg[2], "usage: lua main.lua <generated dir> <vectors dir>")
package.path = dir .. "/?.lua;" .. package.path

local serialize = require("serialize")
local E = require("End")

local failures = 0
local function fail(name, detail)
  io.stderr:write("FAILED: " .. name .. ": " .. detail .. "\n")
  failures = failures + 1
end

local function readFile(path)
  local f = assert(io.open(path, "rb"))
  local s = f:read("a")
  f:close()
  return s
end

local cases = {
  { name = "exact", verdict = "accept", value = 3 },
  { name = "trailing", verdict = "refuse" },
  { name = "padding", verdict = "refuse" },
}

for _, case in ipairs(cases) do
  local bytes = readFile(vectors .. "/" .. case.name .. ".bin")
  local value = E.NewEnd()
  local stream = serialize.ReadStream.new(bytes)
  local ok = E.ReadEnd(stream, value)
  if case.verdict == "accept" then
    if not ok then
      fail(case.name, "refused the exact packet")
    elseif value.Value ~= case.value then
      fail(case.name, "read value " .. tostring(value.Value) .. ", want " .. case.value)
    else
      print("packet end: " .. case.name .. " reads " .. value.Value)
    end
  else
    if ok then
      fail(case.name, "accepted as " .. tostring(value.Value))
    else
      print("packet end: " .. case.name .. " refused")
    end
  end
end

if failures > 0 then
  os.exit(1)
end
