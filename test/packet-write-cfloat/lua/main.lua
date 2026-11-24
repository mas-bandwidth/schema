-- The Lua packet Write's compressed-float contract (SPEC §5.1 row 10).
-- Read the shared case list and require a false return, never an exception,
-- for every refused value. Lawful values are written and read back, and a
-- case that pins its wire must write exactly those bytes.
local generated = assert(arg[1], "usage: lua main.lua <generated dir> <fixture dir>")
local fixtures = assert(arg[2], "usage: lua main.lua <generated dir> <fixture dir>")
package.path = generated .. "/?.lua;" .. package.path

local serialize = require("serialize")
local CFloat = require("PacketCFloat")
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

local function hex(bytes)
  return (bytes:gsub(".", function(c) return string.format("%02x", c:byte()) end))
end

-- Each kind of case: the generated writer, reader, constructor and field.
local kinds = {
  CFloat = { write = CFloat.WriteCFloat, read = CFloat.ReadCFloat, new = CFloat.NewCFloat, field = "X" },
  RatioEndpoint = { write = CFloat.WriteRatioEndpoint, read = CFloat.ReadRatioEndpoint, new = CFloat.NewRatioEndpoint, field = "Ratio" },
}

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
    local expected = value
    local kind = kinds.CFloat
    local wantWire = nil
    local malformed = #fields % 2 ~= 1
    for i = 4, #fields - 1, 2 do
      local key, operand = fields[i], fields[i + 1]
      if key == "reads-back" then expected = number(operand)
      elseif key == "type" and kinds[operand] ~= nil then kind = kinds[operand]
      elseif key == "wire" then wantWire = operand:lower()
      else malformed = true end
    end
    local stream = serialize.WriteStream.new(32)
    local input = kind.new()
    input[kind.field] = value
    local ok, result = pcall(kind.write, stream, input)
    if malformed then
      fail(name, "malformed case modifiers")
    elseif verdict == "refused" then
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
        if wantWire ~= nil and hex(wire) ~= wantWire then
          fail(name, "wrote " .. hex(wire) .. ", want " .. wantWire)
        end
        local readStream = serialize.ReadStream.new(wire)
        local decoded = kind.new()
        local readOK, readResult = pcall(kind.read, readStream, decoded)
        if not readOK then
          fail(name, "reader raised: " .. tostring(readResult))
        elseif readResult ~= true then
          fail(name, "reader refused: " .. tostring(readStream:error()))
        else
          if expected == 0 then expected = 0 end
          if decoded[kind.field] ~= expected then
            fail(name, string.format("read back %.9g, want quantized value %.9g", decoded[kind.field], expected))
          end
        end
      end
    else
      fail(name, "unknown verdict " .. tostring(verdict))
    end
  end
end
file:close()

-- The last stdout line is the count tools/mustrun reads. An empty cases.txt
-- is a failure: before this line it printed "OK ... 0 cases" and exited 0.
print(string.format("packet-write-cfloat Lua: ran %d, failed %d", count, failures))
if failures ~= 0 or count == 0 then
  io.stderr:write(string.format("packet-write-cfloat Lua: %d of %d cases FAILED\n", failures, count))
  os.exit(1)
end
