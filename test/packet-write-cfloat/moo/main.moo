--!strict
local serialize = require("./serialize")
local c = require("./PacketCFloat")
local inputs = require("./inputs") :: any
local failed = 0
local cases = 0

local function hex(b: buffer): string
 local parts = {}
 for i = 0, buffer.len(b) - 1 do
  table.insert(parts, string.format("%02x", buffer.readu8(b, i)))
 end
 return table.concat(parts)
end

for line in string.gmatch(buffer.tostring(inputs.cases), "[^\n]+") do
 local fields = {}
 for word in string.gmatch(line, "%S+") do table.insert(fields, word) end
 local name, text, verdict = fields[1], fields[2], fields[3]
 if name and string.sub(name, 1, 1) ~= "#" then
  cases += 1
  local failedBefore = failed
  -- [reads-back <v>] [type RatioEndpoint] [wire <hex>] [bits <n>], in pairs
  local backText = ""
  local ratio = false
  local wantWire = ""
  local wantBits = -1
  local malformed = #fields < 3 or #fields % 2 ~= 1
  local i = 4
  while i < #fields do
   if fields[i] == "reads-back" then backText = fields[i + 1]
   elseif fields[i] == "type" and fields[i + 1] == "RatioEndpoint" then ratio = true
   elseif fields[i] == "wire" then wantWire = string.lower(fields[i + 1])
   elseif fields[i] == "bits" and string.match(fields[i + 1], "^%d+$") then wantBits = tonumber(fields[i + 1]) :: number
   else malformed = true end
   i += 2
  end
  local n = if text == "NaN" or text == "+Inf" or text == "-Inf" or text == "3.4028235e+38" then 0 else tonumber(text) :: number
  local b = buffer.create(4)
  if text == "NaN" then buffer.writeu32(b, 0, 0x7fc00000) elseif text == "+Inf" then buffer.writeu32(b, 0, 0x7f800000) elseif text == "-Inf" then buffer.writeu32(b, 0, 0xff800000) elseif text == "3.4028235e+38" then buffer.writeu32(b, 0, 0x7f7fffff) else buffer.writef32(b, 0, n) end
  local value = c.NewCFloat()
  value.X = buffer.readu32(b, 0)
  local ratioValue = c.NewRatioEndpoint()
  ratioValue.Ratio = buffer.readu32(b, 0)
  local stream = serialize.WriteStream.new(buffer.create(if ratio then c.RatioEndpointMaxBytes else c.CFloatMaxBytes))
  local ran, ok
  if ratio then
   ran, ok = pcall(c.WriteRatioEndpoint, stream, ratioValue)
  else
   ran, ok = pcall(c.WriteCFloat, stream, value)
  end
  if malformed then
   print("FAILED: " .. tostring(name) .. ": malformed case")
   failed += 1
  elseif not ran or (verdict == "refused" and ok) or (verdict == "written" and not ok) then
   print("FAILED: " .. name .. ": write verdict")
   failed += 1
  elseif verdict == "written" and ok then
   -- the bits the write emitted, before the flush pads the last byte
   local bits = stream:bitsProcessed()
   stream:flush()
   local wire = stream:data()
   local success
   if ratio then
    local read = c.NewRatioEndpoint()
    success = c.ReadRatioEndpoint(serialize.ReadStream.new(wire), read)
    buffer.writeu32(b, 0, read.Ratio)
   else
    local read = c.NewCFloat()
    success = c.ReadCFloat(serialize.ReadStream.new(wire), read)
    buffer.writeu32(b, 0, read.X)
   end
   local expected = if backText ~= "" then tonumber(backText) :: number else n
   local expectedBits = buffer.create(4)
   buffer.writef32(expectedBits, 0, expected)
   if wantBits >= 0 and bits ~= wantBits then
    print("FAILED: " .. name .. ": wrote " .. tostring(bits) .. " bits before the flush, want " .. tostring(wantBits))
    failed += 1
   elseif wantWire ~= "" and hex(wire) ~= wantWire then
    print("FAILED: " .. name .. ": wrote " .. hex(wire) .. ", want " .. wantWire)
    failed += 1
   elseif not success or buffer.readu32(b, 0) ~= buffer.readu32(expectedBits, 0) then
    print("FAILED: " .. name .. ": read back")
    failed += 1
   end
  end
  -- the case's answer tools/casegate reads (the FAILED line says why)
  print("case " .. name .. (if failed == failedBefore then " ok" else " FAILED: see the FAILED line above"))
 end
end
-- A summary; tools/casegate holds every case of the file to an answer. An
-- empty case list is a failure.
print(string.format("packet cfloat moo: ran %d, failed %d", cases, failed))
if failed > 0 or cases == 0 then error(string.format("packet cfloat moo: %d failures over %d cases", failed, cases), 0) end
