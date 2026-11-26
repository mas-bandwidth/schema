--!strict
-- The Luau leg's per-PR happy path (make/happy-path.mk): one ShipCreate
-- written and read back through the generated module. The recipe copies this
-- file beside the generated modules (Luau requires are file-relative); a
-- failure raises, which exits the interpreter non-zero.
local serialize = require("./serialize")
local h = require("./Happy")

local v = h.NewShipCreate()
v.ShipType = h.ShipType.Corvette
v.Position.X, v.Position.Y, v.Position.Z = 1.5, -2.25, 3.0
v.Health = 777
v.Alive = true

local w = serialize.WriteStream.new(buffer.create(h.ShipCreateMaxBytes))
if not h.WriteShipCreate(w, v) then
	error("happy luau: FAILED: WriteShipCreate refused", 0)
end
w:flush()
local bytes: buffer = w:data()

local got = h.NewShipCreate()
if not h.ReadShipCreate(serialize.ReadStream.new(bytes), got) then
	error("happy luau: FAILED: ReadShipCreate refused", 0)
end

local function eq(name: string, a: any, b: any)
	if a ~= b then
		error(`happy luau: FAILED: {name}: got {tostring(a)}, want {tostring(b)}`, 0)
	end
end
eq("ship_type", got.ShipType, h.ShipType.Corvette)
eq("position.x", got.Position.X, 1.5)
eq("position.y", got.Position.Y, -2.25)
eq("position.z", got.Position.Z, 3.0)
eq("health", got.Health, 777)
eq("alive", got.Alive, true)

print(`happy luau: ok ({buffer.len(bytes)} bytes)`)
