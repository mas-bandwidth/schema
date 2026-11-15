// test/packet-write-bareint/squirrel/main.nut — the SQUIRREL leg of the packet
// bare-integer width fixture (test/packet-write-bareint/cases.txt; SPEC §5.1
// row 11). Squirrel refuses in every build: a refused value makes Write return
// false, never a raise. A written value reads back exactly.
//
// usage: sq main.nut [cases] [bareint-dir] [carriers-dir]
//
// The number model (docs/PORT-SQUIRREL.md, 64-BIT AND 128-BIT INTEGERS) gives
// each case value its storage: a value in int64 is the integer; a uint64 above
// 2^63 - 1 is the integer with the same 64 bits; a value no integer holds is
// the float the storage can hold instead, refused as not an integer. A
// negative uint64 is the same 64 bits as a uint64 the width carries, so the
// leg cannot hold it apart: the driver checks it writes those 64 bits.

local argv = vargv;
local casesPath = argv.len() >= 1 ? argv[0] : "test/packet-write-bareint/cases.txt";
local bareDir = argv.len() >= 2 ? argv[1] : "build/packet-write-bareint/squirrel/bareint";
local carrierDir = argv.len() >= 3 ? argv[2] : "build/packet-write-bareint/squirrel/carriers";

local serialize = dofile("internal/codegen/squirrel/serialize/serialize.nut", true);
local b = dofile(bareDir + "/schema_module.nut", true).require("BareInt");
local c = dofile(carrierDir + "/schema_module.nut", true).require("Carriers");

local failed = 0;
local checked = 0;
local check = function(ok, what) {
  checked++;
  if (!ok) {
    print("FAILED: " + what + "\n");
    failed++;
  }
};

local readText = function(path) {
  local f = file(path, "rb");
  local data = f.len() == 0 ? blob(0) : f.readblob(f.len());
  f.close();
  local s = "";
  for (local i = 0; i < data.len(); i++) s += format("%c", data[i] & 0xff);
  return s;
};

// storage parses a decimal case value into what this leg's storage holds for
// it: digit by digit into two 32-bit limbs, because tointeger() saturates.
local MIN64 = 0x8000000000000000;
local storage = function(text, unsigned64) {
  local neg = text[0] == '-';
  local hi = 0, lo = 0, over = false;
  for (local i = neg ? 1 : 0; i < text.len(); i++) {
    local d = text[i] - '0';
    if (d < 0 || d > 9) throw "bad case value " + text;
    lo = lo * 10 + d;
    hi = hi * 10 + (lo >> 32);
    lo = lo & 0xffffffff;
    if (hi > 0xffffffff) over = true;
    hi = hi & 0xffffffff;
  }
  if (over) return text.tofloat();
  if (neg) {
    if (hi == 0x80000000 && lo == 0) return MIN64;
    if (hi >= 0x80000000) return text.tofloat();
    return -((hi << 32) | lo);
  }
  if (hi >= 0x80000000 && !unsigned64) return text.tofloat();
  return (hi << 32) | lo;
};

local write = function(mod, name, value) {
  local stream = serialize.WriteStream(blob(64));
  local ok = mod["Write" + name](stream, value);
  if (!ok) return null;
  local bits = stream.bitsProcessed();
  stream.flush();
  return { bytes = stream.data(), bits = bits };
};

local same = function(x, y) {
  if (x.len() != y.len()) return false;
  for (local i = 0; i < x.len(); i++) if (x[i] != y[i]) return false;
  return true;
};

// outcome writes one value and asks for a verdict: "refused" is a false
// return, "written" a true one; a raise is neither.
local outcome = function(mod, type, value, want, what) {
  local w;
  try {
    w = write(mod, type, value);
  } catch (e) {
    check(false, what + ": the write raised (" + e + ")");
    return;
  }
  check(want == "refused" ? w == null : w != null, what);
};

// verdict writes one value and asks for the case's verdict: "refused" is a
// false return; "written" is a true return whose bytes read back to value.
local verdict = function(mod, type, value, want, what) {
  local v = mod[type]();
  v.Value = value;
  local w;
  try {
    w = write(mod, type, v);
  } catch (e) {
    check(false, what + ": the write raised (" + e + ")");
    return null;
  }
  if (want == "refused") {
    check(w == null, what + ": refused");
    return null;
  }
  check(w != null, what + ": written");
  if (w == null) return null;
  local back = mod[type]();
  back.Value = 0;
  local ok = mod["Read" + type](serialize.ReadStream(w.bytes), back);
  check(ok && typeof back.Value == "integer" && back.Value == value, what + ": reads back " + value);
  return w;
};

local lines = split(readText(casesPath), "\n");
local cases = 0;
local wire = {};
local aliased = [];
foreach (line in lines) {
  line = strip(line);
  if (line.len() == 0 || line[0] == '#') continue;
  local fields = [];
  foreach (f in split(line, " \t")) if (f.len() > 0) fields.append(f);
  if (fields.len() != 4) throw "bad case line: " + line;
  local name = fields[0], type = fields[1], text = fields[2], want = fields[3];
  local value = storage(text, type == "U64");
  cases++;
  if (type == "U64" && text[0] == '-') {
    aliased.append({ name = name, type = type, value = value });
    continue;
  }
  local w = verdict(b, type, value, want, name);
  if (w != null) wire[type + ":" + value] <- w;
}
check(cases > 0, "the shared list has cases");

// A negative uint64 is stored as the same 64 bits as an unsigned value: it
// writes exactly those bits, the bits of a uint64 the width carries.
foreach (a in aliased) {
  local w = verdict(b, a.type, a.value, "written", a.name + " (the same 64 bits as a uint64 in the width)");
  local key = a.type + ":" + a.value;
  if (w != null && key in wire) check(same(w.bytes, wire[key].bytes), a.name + ": the same bytes as the in-width twin");
}

// A value that is not an integer is refused at every width.
foreach (type in ["I8", "I16", "I32", "I64", "U8", "U16", "U32", "U64"]) {
  foreach (bad in [1.5, "1", null, true]) verdict(b, type, bad, "refused", type + " holding a " + typeof bad);
}

// Every path the writer emits the check on: fixed and counted array elements
// and a union arm's payload.
local elements = function(pair, list) {
  local v = c.Elements();
  v.Pair = pair;
  v.List = list;
  v.ListCount = list.len();
  return v;
};
outcome(c, "Elements", elements([-128, 127], [0, 65535]), "written", "array elements at the width's bounds are written");
outcome(c, "Elements", elements([128, 0], [0]), "refused", "a fixed array element above the width is refused");
outcome(c, "Elements", elements([0, -129], [0]), "refused", "a fixed array element below the width is refused");
outcome(c, "Elements", elements([0, 0], [0, 65536]), "refused", "a counted array element above the width is refused");
outcome(c, "Elements", elements([0, 0], [-1]), "refused", "a negative counted array element is refused");
local arm = function(type, fill) {
  local v = c.Arm();
  v.Type = type;
  fill(v);
  return v;
};
outcome(c, "Arm", arm(c.ArmType.Small, function(v) { v.Small.Value = -128; }), "written", "an arm payload at the width's bound is written");
outcome(c, "Arm", arm(c.ArmType.Small, function(v) { v.Small.Value = 128; }), "refused", "an arm payload above the width is refused");
outcome(c, "Arm", arm(c.ArmType.Elements, function(v) { v.Elements = elements([0, 200], [0]); }), "refused", "an arm payload's array element above the width is refused");

if (failed > 0) throw "packet write bare-int squirrel: " + failed + " check(s) failed";
print("packet write bare-int Squirrel: " + cases + " shared cases and " + checked + " checks OK\n");
