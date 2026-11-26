--!strict
-- The per-PR happy path, the moo leg (make/happy-path.mk): one ShipCreate
-- written with the generated write, read back into a fresh value with the
-- generated read, every field compared. A float64 is its binary64 pattern, a
-- serialize.Uint64 (low half, high half). Any failure prints
-- "FAILED: <what>" and raises, so moo exits non-zero.
local serialize = require("./serialize")
local h = require("./Happy")

local U64 = serialize.Uint64
local X, Y, Z = U64.new(0, 0x3ff80000), U64.new(0, 0xc0020000), U64.new(0, 0x40080000) -- 1.5, -2.25, 3.0
local failed = 0

local function ck(ok: boolean, what: string)
	if not ok then
		print("FAILED: " .. what)
		failed += 1
	end
end

local v = h.NewShipCreate()
v.ShipType = h.ShipType.Corvette
v.Position.X, v.Position.Y, v.Position.Z = X, Y, Z
v.Health = 777
v.Alive = true

local w = serialize.WriteStream.new(buffer.create(h.ShipCreateMaxBytes))
ck(h.WriteShipCreate(w, v), "write")
w:flush()
local bytes = w:data()
local back = h.NewShipCreate()
ck(h.ReadShipCreate(serialize.ReadStream.new(bytes), back), "read")
ck(back.ShipType == h.ShipType.Corvette, "ship_type")
ck(U64.cmp(back.Position.X, X) == 0, "position.x")
ck(U64.cmp(back.Position.Y, Y) == 0, "position.y")
ck(U64.cmp(back.Position.Z, Z) == 0, "position.z")
ck(back.Health == 777, "health")
ck(back.Alive == true, "alive")
if failed > 0 then
	error(string.format("happy moo: %d check(s) failed", failed), 0)
end
print(string.format("happy moo: ok (%d bytes)", buffer.len(bytes)))
