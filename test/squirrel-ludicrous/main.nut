// test/squirrel-ludicrous/main.nut — the Squirrel cross-language wire test for
// the fixed-point and 128-bit unit (examples128/), the twin of
// test/luau-ludicrous/main.luau, test/js-ludicrous/main.mjs and
// test/ludicrous_main.cpp block for block: the generated module writes the
// SAME pinned instances and byte-compares against the C++-pinned
// testdata/wire goldens, reads them back, zeroes an untaken 128-bit field,
// installs the specified 128-bit defaults, and rejects (never clamps) hostile
// offsets and truncation. Run from the repository root. Prints OK, or the
// failures and an error.
//
// usage: sq test/squirrel-ludicrous/main.nut <generated examples128 dir> <testdata/wire dir>

if (vargv.len() != 2) throw "usage: sq test/squirrel-ludicrous/main.nut <generated dir> <wire dir>";
local generated = vargv[0];
local wireDir = vargv[1];

local serialize = dofile("internal/codegen/squirrel/serialize/serialize.nut", true);
local lud = dofile(generated + "/schema_module.nut", true).require("Ludicrous");

local failed = 0;
local check = function(ok, what) {
  if (!ok) {
    print("FAILED: " + what + "\n");
    failed++;
  }
};

local HEX = "0123456789abcdef";
local hexOf = function(b) {
  local s = "";
  for (local i = 0; i < b.len(); i++) {
    s += format("%c%c", HEX[(b[i] >> 4) & 15], HEX[b[i] & 15]);
  }
  return s;
};

local readGolden = function(name) {
  local f = file(wireDir + "/" + name + ".bin", "rb");
  local b = f.len() == 0 ? blob(0) : f.readblob(f.len());
  f.close();
  return b;
};

local copyOf = function(b, n) {
  local out = blob(n);
  for (local i = 0; i < n; i++) {
    out[i] = b[i];
  }
  return out;
};

local i128 = function(lo, hi) {
  return [lo, hi];
};

local eq128 = function(a, b) {
  return a[0] == b[0] && a[1] == b[1];
};

local samplesEqual = function(a, b) {
  return a.len() == b.len() && a[0] == b[0] && a[1] == b[1];
};

local fixedProbeEqual = function(a, b) {
  return a.Angle == b.Angle && a.Position == b.Position && eq128(a.Reach, b.Reach) &&
    a.Ticks == b.Ticks && samplesEqual(a.Samples, b.Samples);
};

local wideProbeEqual = function(a, b) {
  return eq128(a.EntityId, b.EntityId) && eq128(a.Energy, b.Energy) && eq128(a.Flux, b.Flux) &&
    eq128(a.Bias, b.Bias) && eq128(a.Seed, b.Seed);
};

// goldenWire byte-compares written wire against the C++-pinned golden.
local goldenWire = function(name, data) {
  local golden = readGolden(name);
  check(hexOf(data) == hexOf(golden),
    "wire golden " + name + ": Squirrel bytes " + hexOf(data) + " must equal the C++-pinned bytes " + hexOf(golden));
};

// setBits forces bits [pos, pos + n) of a copy of a wire image to 1: the
// hostile reader's tool, smuggling an offset into a range's bit headroom,
// which a read must REJECT, never clamp (STANDARD.md).
local setBits = function(data, pos, n) {
  local out = copyOf(data, data.len());
  for (local i = pos; i < pos + n; i++) {
    out[i / 8] = (out[i / 8] | (1 << (i % 8))) & 0xff;
  }
  return out;
};

local write = function(fn, value, what) {
  local stream = serialize.WriteStream(blob(256));
  check(fn(stream, value), "write " + what);
  stream.flush();
  return stream.data();
};

local read = function(fn, data, out) {
  return fn(serialize.ReadStream(data), out);
};

// makeState is test/ludicrous_main.cpp's make_state: the values stay mirrored.
local makeState = function() {
  local inp = lud.LudicrousState();
  inp.Mode = lud.DriveMode.Ludicrous;
  inp.Probe.Angle = 2981888;          // +45.5 * 2^16
  inp.Probe.Position = -809119744;    // -12346.1875 * 2^16
  inp.Probe.Reach = i128(65535999999, 0); // raw_max - 1
  inp.Probe.Ticks = 777777;
  inp.Probe.Samples[0] = -524288;     // raw_min
  inp.Probe.Samples[1] = 524288;      // raw_max
  inp.Wide.EntityId = i128(0xfedcba9876543210, 0x0123456789abcdef);
  inp.Wide.Energy = i128(4999999999, 0);
  inp.Wide.Flux = i128(7, 0x0000000800000000); // 2^99 + 7
  // Wide.Bias and Wide.Seed stay at their SPECIFIED DEFAULTS (-250 and 2^65)
  inp.KeysCount = 2;
  inp.Keys[0] = i128(1, 0);
  inp.Keys[1] = i128(0, 0x8000000000000000); // 2^127
  inp.HasTarget = true;
  inp.TargetId = i128(42, 0);
  return inp;
};

// worst-case bounds, hand-derived (SPEC §6.1 item 4): the numbers
// test/ludicrous_main.cpp static_asserts
check(lud.FixedProbeMaxBits == 156, "FixedProbe worst case");
check(lud.WideProbeMaxBits == 403, "WideProbe worst case");
check(lud.LudicrousStateMaxBits == 1205, "LudicrousState worst case");
check(lud.ProtocolId == 0xd2ded2cc8a482aa1, "the unit's protocol id is the C++ reference's");

// construction installs the specified defaults (SPEC §4.2), one of which no
// 64-bit literal can spell
{
  local zero = lud.LudicrousState();
  check(zero.Mode == lud.DriveMode.None, "a fresh state starts at DriveMode None");
  check(eq128(zero.Probe.Reach, i128(0, 0)), "reach starts zero");
  check(eq128(zero.Wide.EntityId, i128(0, 0)), "entity_id starts zero");
  check(eq128(zero.Wide.Bias, i128(-250, -1)), "bias defaults -250");
  check(eq128(zero.Wide.Seed, i128(0, 2)), "seed defaults 2^65");
  check(zero.KeysCount == 0, "keys start empty");
  check(eq128(zero.TargetId, i128(0, 0)), "target_id starts zero");
}

// the taken-branch wire: generated bytes == the C++-pinned golden
local takenWire = null;
{
  local inp = makeState();
  takenWire = write(lud.WriteLudicrousState, inp, "LudicrousState");
  goldenWire("ludicrous_state", takenWire);
  local out = lud.LudicrousState();
  check(read(lud.ReadLudicrousState, takenWire, out), "read LudicrousState");
  check(out.Mode == lud.DriveMode.Ludicrous, "mode round-trips");
  check(fixedProbeEqual(out.Probe, inp.Probe), "the fixed probe round-trips");
  check(wideProbeEqual(out.Wide, inp.Wide), "the wide probe round-trips, the defaults riding the wire");
  check(out.KeysCount == 2 && eq128(out.Keys[0], inp.Keys[0]) && eq128(out.Keys[1], inp.Keys[1]), "keys round-trip");
  check(out.HasTarget && eq128(out.TargetId, i128(42, 0)), "the taken branch round-trips");
}

// the untaken branch: the 128-bit field under it reads back ZERO into a
// dirty object (SPEC §5)
{
  local inp = makeState();
  inp.HasTarget = false;
  local data = write(lud.WriteLudicrousState, inp, "LudicrousState untargeted");
  goldenWire("ludicrous_state_untargeted", data);
  local out = lud.LudicrousState();
  out.TargetId = i128(0xdead, 0);
  check(read(lud.ReadLudicrousState, data, out), "read LudicrousState untargeted");
  check(!out.HasTarget, "has_target reads false");
  check(eq128(out.TargetId, i128(0, 0)), "the untaken 128-bit field reads as zero (SPEC §5)");
}

// hostile reads REJECT, never clamp (STANDARD.md, SPEC §5)
{
  // fixed: angle's 25 offset bits start at bit 2; all ones is above the raw range
  check(!read(lud.ReadLudicrousState, setBits(takenWire, 2, 25), lud.LudicrousState()), "a smuggled fixed offset is REJECTED");
  // int128: energy's 34 offset bits start at bit 286; all ones is above 10^10
  check(!read(lud.ReadLudicrousState, setBits(takenWire, 286, 34), lud.LudicrousState()), "a smuggled int128 offset is REJECTED");
  // truncation: running out of input mid-read is a read failure (SPEC §5)
  check(!read(lud.ReadLudicrousState, copyOf(takenWire, 4), lud.LudicrousState()), "a truncated stream is a read failure");
}

// DegenerateProbe: min == max costs ZERO bits (SPEC §4.6); the whole wire
// is the tail byte
{
  check(lud.DegenerateProbeMaxBits == 8, "three degenerate fields cost zero bits");
  local inp = lud.DegenerateProbe();
  inp.LockedFixed = -196608; // -3 * 2^16, the ONE legal raw
  inp.LockedInt = 7;
  inp.LockedWide = i128(-12345678901234, -1);
  inp.Tail = 0xa5;
  local data = write(lud.WriteDegenerateProbe, inp, "DegenerateProbe");
  goldenWire("degenerate_probe", data);
  local out = lud.DegenerateProbe();
  check(read(lud.ReadDegenerateProbe, data, out), "read DegenerateProbe");
  check(out.LockedFixed == inp.LockedFixed && out.LockedInt == inp.LockedInt &&
    eq128(out.LockedWide, inp.LockedWide) && out.Tail == inp.Tail,
    "DegenerateProbe round-trips: every value materialized from its range");
}

// UnsignedProbe: ufixed(I, F) (SPEC §4.3); span's raw value fills uint64's
// HIGH HALF, carried as the integer's unsigned bit pattern
{
  check(lud.UnsignedProbeMaxBits == 196, "UnsignedProbe worst case");
  local inp = lud.UnsignedProbe();
  inp.Angle = 2981888;
  inp.Span = 0xffffffffffff0000; // raw_max, the uint64 high half
  inp.Reach = i128(131071999999, 0); // raw_max - 1
  inp.Ticks = 777777;
  inp.Samples[0] = 0;
  inp.Samples[1] = 1048576;
  inp.Locked = 196608; // 3 * 2^16, the ONE legal raw
  inp.Tail = 0xa5;
  local uWire = write(lud.WriteUnsignedProbe, inp, "UnsignedProbe");
  goldenWire("unsigned_probe", uWire);
  local out = lud.UnsignedProbe();
  check(read(lud.ReadUnsignedProbe, uWire, out), "read UnsignedProbe");
  check(out.Angle == inp.Angle && out.Span == inp.Span && eq128(out.Reach, inp.Reach) &&
    out.Ticks == inp.Ticks && samplesEqual(out.Samples, inp.Samples) && out.Locked == inp.Locked &&
    out.Tail == inp.Tail, "UnsignedProbe round-trips, the uint64 high half bit-exact");
  // the write-side degenerate refusal: any raw but 3 * 2^16 is refused
  inp.Locked = 196609;
  check(!lud.WriteUnsignedProbe(serialize.WriteStream(blob(256)), inp), "a wrong degenerate ufixed raw is REFUSED on write");
  // hostile: span's 64 offset bits from bit 25, all ones, above the raw
  // range in the UNSIGNED domain
  check(!read(lud.ReadUnsignedProbe, setBits(uWire, 25, 64), lud.UnsignedProbe()), "a smuggled ufixed high-half offset is REJECTED");
}

if (failed > 0) throw "squirrel ludicrous: " + failed + " check(s) failed";
print("OK\n");
