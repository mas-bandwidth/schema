// Packet floats are finite (SPEC §4.3, serialize format 2.0), the Squirrel
// leg. internal/codegen/squirrel/finite_test.go generates
// test/packet-finite/squirrel/Finite.schema beside this file and the vendored
// serialize runtime, and runs it under the pinned sq.
//
// The generated code carries no float codec of its own: every bare float
// reaches serialize.nut's serializeFloat / serializeDouble, which enforce the
// rule (serialize.nut v2.x, IMPLEMENTING.md U6, U9). This file holds the
// generated code to it at every float shape a packet carries:
//
// - WRITE: a NaN (any sign, any payload) and both infinities trap at the bare
//   float32 and float64 fields, an element of the fixed float32 array and of
//   the counted float64 array, and the compressed float32; and a double past
//   float32's range, which narrows to an infinity, traps at the float32 field.
//   A write refusal in serialize.nut is a trap (a throw), never a latch.
// - READ: crafted bytes holding an all-ones exponent at each of those slots
//   are refused on the raw bits with the latched reason non_finite_float, and
//   the largest finite pattern beside them reads.
// - -0 and the subnormals ride bit exact, on the wire and back.
//
// Prints OK; every failure is one FAILED line.
local schema = dofile("schema_module.nut", true);
local serialize = dofile("serialize.nut", true);
local Finite = schema.require("Finite");

local failed = 0;
local check = function(ok, what) {
  if (!ok) {
    print("FAILED: " + what + "\n");
    failed++;
  }
};

// a float32 pattern to the double holding it, and back: the blob's 'f' view
// converts exactly for every finite float32, which is all this file feeds it
local f32FromBits = function(bits) {
  local b = blob(4);
  b.writen(bits, 'i');
  b.seek(0);
  return b.readn('f');
};
local f32Bits = function(x) {
  local b = blob(4);
  b.writen(x, 'f');
  b.seek(0);
  return b.readn('i') & 0xffffffff;
};
local f64Bits = function(x) { return castf2i(x); };
local hex = function(bits) { return format("0x%x", bits); };

// The wire layout of Floats, LSB-first bit offsets (SPEC §4.3): narrow at 0,
// wide at 32 (low dword first), narrows[0..2] at 96/128/160, the count of
// wides in 3 bits at 192, wides[i] at 195 + 64 i, then the quantized step
// index in 11 bits. With four wides that is 462 bits.
const NARROW_AT = 0;
const WIDE_AT = 32;
const FULL_BITS = 462;
local narrowsAt = function(i) { return 96 + 32 * i; };
local widesAt = function(i) { return 195 + 64 * i; };

local getBits = function(bytes, offset, bits) {
  local value = 0;
  for (local i = 0; i < bits; i++) {
    local at = offset + i;
    value = value | (((bytes[at >> 3] >> (at & 7)) & 1) << i);
  }
  return value;
};

local putBits = function(bytes, offset, bits, value) {
  for (local i = 0; i < bits; i++) {
    local at = offset + i;
    local mask = 1 << (at & 7);
    if (((value >>> i) & 1) != 0) bytes[at >> 3] = (bytes[at >> 3] | mask) & 0xff;
    else bytes[at >> 3] = bytes[at >> 3] & (~mask & 0xff);
  }
};

local copy = function(b) {
  local out = blob(b.len());
  for (local i = 0; i < b.len(); i++) out[i] = b[i];
  return out;
};

local baseline = function() {
  local v = Finite.Floats();
  v.Narrow = 1.5;
  v.Wide = -2.25;
  v.Narrows[0] = 0.5;
  v.Narrows[1] = -0.75;
  v.Narrows[2] = 3.0;
  v.WidesCount = 4;
  v.Wides[0] = 1.0;
  v.Wides[1] = 2.0;
  v.Wides[2] = -4.0;
  v.Wides[3] = 8.0;
  v.Quantized = 1.25;
  return v;
};

local write = function(v) {
  local ws = serialize.WriteStream(blob(64));
  if (!Finite.WriteFloats(ws, v)) throw "write failed: " + ws.error();
  ws.flush();
  return ws.data();
};

// read answers the latched reason, "ok" for a read that returns true
local read = function(bytes, into) {
  local rs = serialize.ReadStream(bytes);
  if (Finite.ReadFloats(rs, into)) return "ok";
  return "" + rs.error();
};

// THE WRITE HALF: every non-finite value at every slot traps.
local nonFinite = [
  ["NaN", casti2f(0x7ff8000000000000)],
  ["-NaN", casti2f(0xfff8000000000000)],
  ["payload NaN", casti2f(0x7ff0000000000123)],
  ["signalling NaN", casti2f(0x7ff0000000000001)],
  ["-payload NaN", casti2f(0xfff8000000abcdef)],
  ["+inf", casti2f(0x7ff0000000000000)],
  ["-inf", casti2f(0xfff0000000000000)],
];

local writeSlots = [
  ["narrow (float32 field)", function(v, x) { v.Narrow = x; }],
  ["wide (float64 field)", function(v, x) { v.Wide = x; }],
  ["narrows[1] (float32 array element)", function(v, x) { v.Narrows[1] = x; }],
  ["wides[2] (float64 array element)", function(v, x) { v.Wides[2] = x; }],
  ["quantized (compressed float32)", function(v, x) { v.Quantized = x; }],
];

// traps answers whether writing v throws a not-finite trap
local traps = function(v) {
  try {
    write(v);
  } catch (e) {
    return typeof e == "string" && e.find("not finite") != null;
  }
  return false;
};

foreach (slot in writeSlots) {
  foreach (nf in nonFinite) {
    local v = baseline();
    slot[1](v, nf[1]);
    check(traps(v), "non_finite_float write refusal: " + nf[0] + " at " + slot[0]);
  }
}
{
  // a double past float32's range rounds to an infinity: not a float32 value
  local v = baseline();
  v.Narrow = 1e300;
  check(traps(v), "non_finite_float write refusal: 1e300 at narrow (float32)");
}

// THE READ HALF: crafted bytes carrying an all-ones exponent are refused.
local f32Patterns = [
  0x7f800000, // +inf
  0xff800000, // -inf
  0x7fc00000, // quiet NaN
  0xffc00000, // -quiet NaN
  0x7f800001, // signalling NaN, lowest payload
  0x7fffffff, // NaN, every payload bit
  0xff812345, // -NaN, a payload
];
local f64Patterns = [
  0x7ff0000000000000, // +inf
  0xfff0000000000000, // -inf
  0x7ff8000000000000, // quiet NaN
  0xfff8000000000000, // -quiet NaN
  0x7ff0000000000001, // signalling NaN, lowest payload
  0x7fffffffffffffff, // NaN, every payload bit
  0xfff00000deadbeef, // -NaN, a payload
];

local clean = write(baseline());
check(clean.len() * 8 >= FULL_BITS, "baseline wire is " + FULL_BITS + " bits");
check(read(clean, Finite.Floats()) == "ok", "baseline wire reads");

local f32Slots = [["narrow (float32 field)", NARROW_AT]];
for (local i = 0; i < 3; i++) f32Slots.append(["narrows[" + i + "] (float32 array element)", narrowsAt(i)]);
local f64Slots = [["wide (float64 field)", WIDE_AT]];
for (local i = 0; i < 4; i++) f64Slots.append(["wides[" + i + "] (float64 array element)", widesAt(i)]);

foreach (slot in f32Slots) {
  foreach (bits in f32Patterns) {
    local bytes = copy(clean);
    putBits(bytes, slot[1], 32, bits);
    local got = read(bytes, Finite.Floats());
    check(got == "non_finite_float", "non_finite_float read refusal: " + hex(bits) + " at " + slot[0] + " (got " + got + ")");
  }
  // the largest finite float32 beside them is a value, read exactly
  local bytes = copy(clean);
  putBits(bytes, slot[1], 32, 0x7f7fffff);
  check(read(bytes, Finite.Floats()) == "ok", "finite float32 0x7f7fffff at " + slot[0] + " reads");
}
foreach (slot in f64Slots) {
  foreach (bits in f64Patterns) {
    local bytes = copy(clean);
    putBits(bytes, slot[1], 64, bits);
    local got = read(bytes, Finite.Floats());
    check(got == "non_finite_float", "non_finite_float read refusal: " + hex(bits) + " at " + slot[0] + " (got " + got + ")");
  }
  local bytes = copy(clean);
  putBits(bytes, slot[1], 64, 0x7fefffffffffffff);
  check(read(bytes, Finite.Floats()) == "ok", "finite float64 0x7fefffffffffffff at " + slot[0] + " reads");
}

// -0 AND THE SUBNORMALS: bit exact on the wire and back.
foreach (c in [
  ["-0", 0x80000000, 0x8000000000000000],
  ["smallest subnormal", 0x00000001, 0x0000000000000001],
  ["-largest subnormal", 0x807fffff, 0x800fffffffffffff],
]) {
  local name = c[0], b32 = c[1], b64 = c[2];
  local v = baseline();
  v.Narrow = f32FromBits(b32);
  v.Narrows[2] = f32FromBits(b32);
  v.Wide = casti2f(b64);
  v.Wides[1] = casti2f(b64);
  local bytes = write(v);
  check(getBits(bytes, NARROW_AT, 32) == b32, name + " narrow wire bits");
  check(getBits(bytes, narrowsAt(2), 32) == b32, name + " narrows[2] wire bits");
  check(getBits(bytes, WIDE_AT, 64) == b64, name + " wide wire bits");
  check(getBits(bytes, widesAt(1), 64) == b64, name + " wides[1] wire bits");
  local back = Finite.Floats();
  check(read(bytes, back) == "ok", name + " reads");
  check(f32Bits(back.Narrow) == b32, name + " narrow read bits");
  check(f32Bits(back.Narrows[2]) == b32, name + " narrows[2] read bits");
  check(f64Bits(back.Wide) == b64, name + " wide read bits");
  check(f64Bits(back.Wides[1]) == b64, name + " wides[1] read bits");
}

if (failed == 0) print("OK\n");
