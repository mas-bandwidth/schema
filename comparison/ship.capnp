@0xb3f9a1c2d4e58607;
# Cap'n Proto equivalent of the corpus ShipCreate. Structs are inline groups so
# there is no per-vector object overhead - the efficient way to write this.

enum ShipType { none @0; fighter @1; corvette @2; bomber @3; destroyer @4; carrier @5; }
enum Team { none @0; red @1; blue @2; }

struct Vec3i { x @0 :Int32; y @1 :Int32; z @2 :Int32; }
struct Quat16 { x @0 :Int16; y @1 :Int16; z @2 :Int16; w @3 :Int16; }

struct ShipCreate {
  shipType @0 :ShipType;
  position @1 :Vec3i;
  rotation @2 :Quat16;
  linearVelocity @3 :Vec3i;
  flags @4 :UInt32;
  team @5 :Team;
  health @6 :UInt16;
  thrust @7 :UInt8;
}
