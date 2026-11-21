-- The Lua 5.4 driver of the packet-end conformance vectors
-- (testdata/wire/packet-end/README.md): read each packet with the generated
-- one-packet read and hold its verdict.
--
-- usage: lua main.lua <generated dir> <vectors dir> <vector file>...
-- The trailing arguments are the directory's own listing of *.bin (the make
-- recipe passes it): a vector on disk that this list does not hold, a listed
-- vector that is not on disk, and an empty listing each fail the run, so a
-- new or renamed vector cannot go unread. The last stdout line is
-- "packet end Lua: ran N, failed M" (tools/mustrun reads it).
local usage = "usage: lua main.lua <generated dir> <vectors dir> <vector file>..."
local dir = assert(arg[1], usage)
local vectors = assert(arg[2], usage)
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

local ran = 0
local known, listed = {}, 0
for _, case in ipairs(cases) do known[case.name .. ".bin"] = false end
for i = 3, #arg do
  listed = listed + 1
  if known[arg[i]] == nil then
    fail(arg[i], "a vector in the directory that this driver does not read")
  else
    known[arg[i]] = true
  end
end
if listed == 0 then
  fail("listing", "the vector directory listing is empty")
end
for _, case in ipairs(cases) do
  if not known[case.name .. ".bin"] then
    fail(case.name, "the vector is not in the directory listing")
  end
end

for _, case in ipairs(cases) do
  ran = ran + 1
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

print(string.format("packet end Lua: ran %d, failed %d", ran, failures))
if failures > 0 or ran == 0 then
  os.exit(1)
end
