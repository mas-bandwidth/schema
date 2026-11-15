// test/packet-end/squirrel/main.nut — the SQUIRREL leg of the packet end
// conformance corpus (testdata/wire/packet-end/README.md; docs/SPEC.md §5,
// READ_PACKET in docs/PACKET-WIRE-ALGORITHM.md §3).
//
// The shared exact packet reads as 3. One whole byte left after the object is
// refused, and so are non-zero unused bits in the final byte.
//
// usage: sq main.nut [golden-dir] [generated-dir]

local argv = vargv;
local goldenDir = argv.len() >= 1 ? argv[0] : "testdata/wire/packet-end";
local generatedDir = argv.len() >= 2 ? argv[1] : "build/packet-end/squirrel";

local serialize = dofile("internal/codegen/squirrel/serialize/serialize.nut", true);
local End = dofile(generatedDir + "/schema_module.nut", true).require("End");

local failed = 0;
local check = function(ok, what) {
  if (!ok) {
    print("FAILED: " + what + "\n");
    failed++;
  }
};

local readBytes = function(path) {
  local f = file(path, "rb");
  local b = f.len() == 0 ? blob(0) : f.readblob(f.len());
  f.close();
  return b;
};

local readPacket = function(name) {
  local data = readBytes(goldenDir + "/" + name);
  local rs = serialize.ReadStream(data);
  local value = End.End();
  local ok = End.ReadEnd(rs, value);
  return { ok = ok, value = value.Value, bits = rs.bitsProcessed() };
};

local exact = readPacket("exact.bin");
check(exact.ok, "exact packet refused");
check(exact.value == 3, "exact packet read " + exact.value + ", want 3");
check(exact.bits == 8, "exact packet consumed " + exact.bits + " bits, want all 8");

local trailing = readPacket("trailing.bin");
check(!trailing.ok, "trailing packet accepted with a whole byte left after the object");

local padding = readPacket("padding.bin");
check(!padding.ok, "padding packet accepted with non-zero unused bits in the final byte");

if (failed > 0) throw "packet end squirrel: " + failed + " check(s) failed";
print("packet end Squirrel: exact packet reads 3; trailing byte and padding refused\n");
