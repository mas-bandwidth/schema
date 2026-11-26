// The Squirrel leg's per-PR happy path (make/happy-path-squirrel.mk): one
// ShipCreate written and read back through the generated module.
// usage: sq main.nut <serialize runtime dir> <generated dir>
// sq exits 0 on a runtime error, so the recipe requires the final ok line:
// any failure throws a FAILED message and never reaches it.
local serialize = dofile(vargv[0] + "/serialize.nut", true);
local h = dofile(vargv[1] + "/schema_module.nut", true).require("Happy");

local v = h.ShipCreate();
v.ShipType = h.ShipType.Corvette;
v.Position.X = 1.5;
v.Position.Y = -2.25;
v.Position.Z = 3.0;
v.Health = 777;
v.Alive = true;

local w = serialize.WriteStream(blob(h.ShipCreateMaxBytes));
if (!h.WriteShipCreate(w, v)) throw "happy squirrel: FAILED: WriteShipCreate refused";
w.flush();
local bytes = w.data();

local got = h.ShipCreate();
if (!h.ReadShipCreate(serialize.ReadStream(bytes), got)) throw "happy squirrel: FAILED: ReadShipCreate refused";

local eq = function(name, a, b) {
  if (a != b) throw "happy squirrel: FAILED: " + name + ": got " + a + ", want " + b;
};
eq("ship_type", got.ShipType, h.ShipType.Corvette);
eq("position.x", got.Position.X, 1.5);
eq("position.y", got.Position.Y, -2.25);
eq("position.z", got.Position.Z, 3.0);
eq("health", got.Health, 777);
eq("alive", got.Alive, true);

print("happy squirrel: ok (" + bytes.len() + " bytes)\n");
