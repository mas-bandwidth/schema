-- The Lua leg's per-PR happy path (make/happy-path.mk): one ShipCreate
-- written and read back through the generated module.
-- usage: lua main.lua <generated dir>
local dir = assert(arg[1], "usage: lua main.lua <generated dir>")
package.path = dir .. "/?.lua;" .. package.path

local serialize = require("serialize")
local h = require("Happy")

local function fail(why)
  io.stderr:write("happy lua: FAILED: " .. why .. "\n")
  os.exit(1)
end

local v = h.NewShipCreate()
v.ShipType = h.ShipType.Corvette
v.Position.X, v.Position.Y, v.Position.Z = 1.5, -2.25, 3.0
v.Health = 777
v.Alive = true

local w = serialize.WriteStream.new(64)
if not h.WriteShipCreate(w, v) then fail("WriteShipCreate refused") end
w:flush()
local bytes = w:data()

local got = h.NewShipCreate()
if not h.ReadShipCreate(serialize.ReadStream.new(bytes), got) then fail("ReadShipCreate refused") end

local function eq(name, a, b)
  if a ~= b then fail(name .. ": got " .. tostring(a) .. ", want " .. tostring(b)) end
end
eq("ship_type", got.ShipType, h.ShipType.Corvette)
eq("position.x", got.Position.X, 1.5)
eq("position.y", got.Position.Y, -2.25)
eq("position.z", got.Position.Z, 3.0)
eq("health", got.Health, 777)
eq("alive", got.Alive, true)

print(("happy lua: ok (%d bytes)"):format(#bytes))
