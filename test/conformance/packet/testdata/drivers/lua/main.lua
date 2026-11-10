-- The packet carrier's Lua driver (test/conformance/packet/carrier.go states
-- the contract): write|read <codec> <in> <out>. The generated module and the
-- serialize runtime it vendors sit beside it; one codec per seed root.
local serialize = require("serialize")
local Seed = require("Seed")

local function fail(msg)
  io.stderr:write("driver: " .. msg .. "\n")
  os.exit(1)
end

local function writeSeedT(f)
  local v = Seed.NewT()
  local at = 1
  if #f < 1 then return nil end
  local tag = f[at]
  at = at + 1
  if tag == 1 then
    if at > #f then return nil end
    v.U.Type = Seed.UType.A
    v.U.A = Seed.NewSmall()
    v.U.A.V = f[at]
    at = at + 1
  elseif tag == 0 then
    v.U.Type = Seed.UType.None
  else
    return nil
  end
  for i = 1, 4 do
    if at > #f then return nil end
    v.Data[i] = f[at]
    at = at + 1
  end
  if at ~= #f + 1 then return nil end
  local stream = serialize.WriteStream.new(64)
  if not Seed.WriteT(stream, v) then return nil end
  stream:flush()
  return stream:data()
end

local function readSeedT(p)
  local v = Seed.NewT()
  if not Seed.ReadT(serialize.ReadStream.new(p), v) then return nil end
  local out = { v.U.Type }
  if v.U.Type == Seed.UType.A then out[#out + 1] = v.U.A.V end
  for i = 1, 4 do out[#out + 1] = v.Data[i] end
  return out
end

local codecs = { ["union-before-bytes"] = { writeSeedT, readSeedT } }

local verb, codec, inPath, outPath = arg[1], arg[2], arg[3], arg[4]
if outPath == nil then fail("usage: driver write|read <codec> <in> <out>") end
if codecs[codec] == nil then fail("no codec " .. codec) end
local writer, reader = codecs[codec][1], codecs[codec][2]
local input = io.open(inPath, "rb")
if input == nil then fail("cannot open " .. inPath) end
local out = {}
for line in input:lines() do
  if verb == "write" then
    local fields = {}
    for t in line:gmatch("%S+") do
      local n = math.tointeger(tonumber(t))
      if n == nil then fail("not an integer: " .. t) end
      fields[#fields + 1] = n
    end
    local p = writer(fields)
    if p == nil then
      out[#out + 1] = "refused"
    else
      out[#out + 1] = (p:gsub(".", function(c) return string.format("%02x", c:byte()) end))
    end
  elseif verb == "read" then
    if #line % 2 ~= 0 or line:find("[^0-9a-f]") then fail("bad hex line") end
    local p = line:gsub("..", function(h) return string.char(tonumber(h, 16)) end)
    local fields = reader(p)
    if fields == nil then
      out[#out + 1] = "refused"
    else
      local parts = { "ok" }
      for _, n in ipairs(fields) do parts[#parts + 1] = tostring(n) end
      out[#out + 1] = table.concat(parts, " ")
    end
  else
    fail("no verb " .. tostring(verb))
  end
end
input:close()
local file = io.open(outPath, "wb")
if file == nil then fail("cannot open " .. outPath) end
if #out > 0 then file:write(table.concat(out, "\n"), "\n") end
file:close()
