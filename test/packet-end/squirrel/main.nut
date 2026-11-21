// test/packet-end/squirrel/main.nut — the SQUIRREL leg of the packet end
// conformance corpus (testdata/wire/packet-end/README.md; docs/SPEC.md §5,
// READ_PACKET in docs/PACKET-WIRE-ALGORITHM.md §3).
//
// The shared exact packet reads as 3. One whole byte left after the object is
// refused, and so are non-zero unused bits in the final byte.
//
// usage: sq main.nut [golden-dir] [generated-dir] --vectors <vector .bin file>...
//
// The arguments after "--vectors" are the vector directory's own listing of
// *.bin (the make recipe passes it): a vector on disk this driver does not
// read, a vector it reads that is not listed, and an empty listing each fail
// the run. The last stdout line is "<name>: ran N, failed M", N the checks
// that ran (tools/mustrun reads it; sq exits 0 on a runtime error, so a run
// that dies prints no count line and is red for that).
local argv = [];
local listed = null;
foreach (a in vargv) {
  if (listed != null) listed[a] <- false;
  else if (a == "--vectors") listed = {};
  else argv.append(a);
}
local goldenDir = argv.len() >= 1 ? argv[0] : "testdata/wire/packet-end";
local generatedDir = argv.len() >= 2 ? argv[1] : "build/packet-end/squirrel";

local serialize = dofile("internal/codegen/squirrel/serialize/serialize.nut", true);
local End = dofile(generatedDir + "/schema_module.nut", true).require("End");

local failed = 0;
local ran = 0;
local check = function(ok, what) {
  ran++;
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
  if (listed != null) {
    check(name in listed, "vector " + name + " is not in the directory listing");
    listed[name] <- true;
  }
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

if (listed == null || listed.len() == 0) check(false, "the vector directory listing is empty");
else foreach (name, wasRead in listed) check(wasRead, "vector " + name + " is in the directory and this driver does not read it");
print("packet end Squirrel: ran " + ran + ", failed " + failed + "\n");
if (failed > 0) throw "packet end squirrel: " + failed + " check(s) failed";
print("packet end Squirrel: exact packet reads 3; trailing byte and padding refused\n");
