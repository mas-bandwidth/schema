-- The Lua packet Write's compressed-float contract (SPEC §5.1 row 10).
-- Read the shared case list and require a false return, never an exception,
-- for every refused value. Lawful values are written and read back.
local generated = assert(arg[1], "usage: lua main.lua <generated dir> <fixture dir>")
local fixtures = assert(arg[2], "usage: lua main.lua <generated dir> <fixture dir>")
package.path = generated .. "/?.lua;" .. package.path

local serialize = require("serialize")
local CFloat = require("CFloat")
local failures = 0
local count = 0

local function fail(name, detail)
  io.stderr:write("FAILED: " .. name .. ": " .. detail .. "\n")
  failures = failures + 1
end

local function number(text)
  if text == "NaN" then return 0 / 0 end
  if text == "+Inf" then return math.huge end
  if text == "-Inf" then return -math.huge end
  return assert(tonumber(text), "invalid case number: " .. text)
end

local file = assert(io.open(fixtures .. "/cases.txt", "r"))
for line in file:lines() do
  local fields = {}
  if not line:match("^%s*#") then
    for word in line:gmatch("%S+") do fields[#fields + 1] = word end
  end
  if #fields > 0 then
    count = count + 1
    local name, valueText, verdict = fields[1], fields[2], fields[3]
    local value = number(valueText)
    local stream = serialize.WriteStream.new(32)
    local ok, result = pcall(CFloat.WriteCFloat, stream, { X = value })
    if verdict == "refused" then
      if not ok then
        fail(name, "writer raised instead of returning false: " .. tostring(result))
      elseif result ~= false then
        fail(name, "writer returned " .. tostring(result) .. ", want false")
      end
    elseif verdict == "written" then
      if not ok then
        fail(name, "writer raised for a lawful value: " .. tostring(result))
      elseif result ~= true then
        fail(name, "writer returned " .. tostring(result) .. ", want true")
      else
        stream:flush()
        local wire = stream:data()
        local readStream = serialize.ReadStream.new(wire)
        local decoded = CFloat.NewCFloat()
        local readOK, readResult = pcall(CFloat.ReadCFloat, readStream, decoded)
        if not readOK then
          fail(name, "reader raised: " .. tostring(readResult))
        elseif readResult ~= true then
          fail(name, "reader refused: " .. tostring(readStream:error()))
        else
          local expected = value
          if fields[4] == "reads-back" then expected = number(fields[5]) end
          if expected == 0 then expected = 0 end
          if decoded.X ~= expected then
            fail(name, string.format("read back %.9g, want quantized value %.9g", decoded.X, expected))
          end
        end
      end
    else
      fail(name, "unknown verdict " .. tostring(verdict))
    end
  end
end
file:close()

if failures ~= 0 then
  io.stderr:write(string.format("packet-write-cfloat Lua: %d of %d cases FAILED\n", failures, count))
  os.exit(1)
end
print(string.format("OK: packet-write-cfloat Lua, %d cases", count))
