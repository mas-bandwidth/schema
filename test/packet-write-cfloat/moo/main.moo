--!strict
local serialize = require("./serialize")
local c = require("./CFloat")
local inputs = require("./inputs") :: any
local failed = 0
local cases = 0
for line in string.gmatch(buffer.tostring(inputs.cases), "[^\n]+") do
 local name, text, verdict, _, backText = string.match(line, "^(%S+)%s+(%S+)%s+(%S+)%s*(%S*)%s*(%S*)")
 if name and string.sub(name, 1, 1) ~= "#" then
  cases += 1
  local n = if text == "NaN" or text == "+Inf" or text == "-Inf" or text == "3.4028235e+38" then 0 else tonumber(text) :: number
  local b = buffer.create(4)
  if text == "NaN" then buffer.writeu32(b, 0, 0x7fc00000) elseif text == "+Inf" then buffer.writeu32(b, 0, 0x7f800000) elseif text == "-Inf" then buffer.writeu32(b, 0, 0xff800000) elseif text == "3.4028235e+38" then buffer.writeu32(b, 0, 0x7f7fffff) else buffer.writef32(b, 0, n) end
  local value = c.NewCFloat()
  value.X = buffer.readu32(b, 0)
  local stream = serialize.WriteStream.new(buffer.create(c.CFloatMaxBytes))
  local ran, ok = pcall(c.WriteCFloat, stream, value)
  if not ran or (verdict == "refused" and ok) or (verdict == "written" and not ok) then
   print("FAILED: " .. name .. ": write verdict")
   failed += 1
  elseif verdict == "written" and ok then
   stream:flush()
   local read = c.NewCFloat()
   local success = c.ReadCFloat(serialize.ReadStream.new(stream:data()), read)
   buffer.writeu32(b, 0, read.X)
   local expected = if backText ~= "" then tonumber(backText) :: number else n
   local expectedBits = buffer.create(4)
   buffer.writef32(expectedBits, 0, expected)
   if not success or buffer.readu32(b, 0) ~= buffer.readu32(expectedBits, 0) then
    print("FAILED: " .. name .. ": read back")
    failed += 1
   end
  end
 end
end
-- The count tools/mustrun reads. An empty case list is a failure (it used to
-- print "all cases passed" and exit 0).
print(string.format("packet cfloat moo: ran %d, failed %d", cases, failed))
if failed > 0 or cases == 0 then error(string.format("packet cfloat moo: %d failures over %d cases", failed, cases), 0) end
