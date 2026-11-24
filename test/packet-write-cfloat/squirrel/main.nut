// test/packet-write-cfloat/squirrel/main.nut — Squirrel packet Write range
// contract (SPEC §5.1 row 10, PACKET-WIRE-ALGORITHM §2.5).
//
// usage: sq main.nut <cases.txt> <generated-dir>

local argv = vargv;
if (argv.len() < 2) throw "usage: main.nut <cases.txt> <generated-dir>";
local casesPath = argv[0];
local generatedDir = argv[1];

local serialize = dofile("internal/codegen/squirrel/serialize/serialize.nut", true);
local CFloat = dofile(generatedDir + "/schema_module.nut", true).require("PacketCFloat");

local readText = function(path) {
  local f = file(path, "rb");
  local b = f.len() == 0 ? blob(0) : f.readblob(f.len());
  f.close();
  local s = "";
  for (local i = 0; i < b.len(); i++) s += format("%c", b[i] & 0xff);
  return s;
};

local splitFields = function(line) {
  local out = [];
  local cur = "";
  for (local i = 0; i <= line.len(); i++) {
    local c = i < line.len() ? line[i] : 32;
    if (c == 32 || c == 9 || c == 13 || i == line.len()) {
      if (cur.len() > 0) { out.append(cur); cur = ""; }
    } else {
      cur += format("%c", c & 0xff);
    }
  }
  return out;
};

local parseFloat = function(text) {
  if (text == "NaN") return 0.0 / 0.0;
  if (text == "+Inf") return 1.0 / 0.0;
  if (text == "-Inf") return -1.0 / 0.0;
  return text.tofloat();
};

local failed = 0;
local ran = 0;
// One check per case: it prints the case's answer, the line tools/casegate
// reads.
local check = function(ok, name, detail) {
  ran++;
  if (!ok) {
    print("FAILED: packet-write-cfloat " + name + ": " + detail + "\n");
    failed++;
  }
  print("case " + name + (ok ? " ok" : " FAILED: " + detail) + "\n");
};

local input = readText(casesPath);
local start = 0;
for (local i = 0; i <= input.len(); i++) {
  if (i < input.len() && input[i] != 10) continue;
  local raw = input.slice(start, i);
  start = i + 1;
  local fields = splitFields(raw);
  if (fields.len() == 0 || fields[0][0] == 35) continue;
  if (fields.len() < 3) {
    check(false, fields[0], "malformed case line");
    continue;
  }
  local name = fields[0];
  local value = parseFloat(fields[1]);
  local expected = fields[2];
  // [reads-back <v>] [type RatioEndpoint|RatioPair] [wire <hex>] [bits <n>], in pairs
  local readBack = value;
  local ratio = false;
  local pair = false;
  local wantWire = null;
  local wantBits = null;
  local malformed = fields.len() % 2 != 1;
  for (local k = 3; k + 1 < fields.len(); k += 2) {
    if (fields[k] == "reads-back") readBack = parseFloat(fields[k + 1]);
    else if (fields[k] == "type" && fields[k + 1] == "RatioEndpoint") ratio = true;
    else if (fields[k] == "type" && fields[k + 1] == "RatioPair") pair = true;
    else if (fields[k] == "wire") wantWire = fields[k + 1].tolower();
    else if (fields[k] == "bits") {
      try { wantBits = fields[k + 1].tointeger(); } catch (e) { malformed = true; }
      if (wantBits != null && wantBits < 0) malformed = true;
    }
    else malformed = true;
  }
  if (malformed) {
    check(false, name, "malformed case modifiers");
    continue;
  }

  // a RatioPair writes the value into both fields
  local record = pair ? CFloat.RatioPair() : ratio ? CFloat.RatioEndpoint() : CFloat.CFloat();
  if (pair) {
    record.First = value;
    record.Second = value;
  } else if (ratio) record.Ratio = value; else record.X = value;
  local stream = serialize.WriteStream(blob(16));
  local wrote = false;
  local raised = null;
  try {
    if (pair) wrote = CFloat.WriteRatioPair(stream, record);
    else if (ratio) wrote = CFloat.WriteRatioEndpoint(stream, record);
    else wrote = CFloat.WriteCFloat(stream, record);
  } catch (e) {
    raised = e;
  }

  if (expected == "refused") {
    check(raised == null && wrote == false, name,
      raised != null ? "writer raised instead of refusing: " + raised : "writer accepted an off-range value");
    continue;
  }

  if (raised != null || wrote != true) {
    check(false, name, raised != null ? "lawful write raised: " + raised : "lawful write refused");
    continue;
  }

  // The public read ends the packet (SPEC §5): it consumes the final byte's
  // unused bits, so it stops at the written bits rounded up to a whole byte.
  // wantBits is what the write emitted, before the flush pads the last byte.
  if (wantBits != null && stream.bitsProcessed() != wantBits) {
    check(false, name, "wrote " + stream.bitsProcessed() + " bits before the flush, want " + wantBits);
    continue;
  }
  local bits = ((stream.bitsProcessed() + 7) / 8) * 8;
  stream.flush();
  local wire = stream.data();
  if (wantWire != null) {
    local gotWire = "";
    for (local k = 0; k < wire.len(); k++) gotWire += format("%02x", wire[k] & 0xff);
    if (gotWire != wantWire) {
      check(false, name, "wrote " + gotWire + ", want " + wantWire);
      continue;
    }
  }
  local read = serialize.ReadStream(wire);
  local got = pair ? CFloat.RatioPair() : ratio ? CFloat.RatioEndpoint() : CFloat.CFloat();
  local readOK = false;
  try {
    if (pair) readOK = CFloat.ReadRatioPair(read, got);
    else if (ratio) readOK = CFloat.ReadRatioEndpoint(read, got);
    else readOK = CFloat.ReadCFloat(read, got);
  } catch (e) {
    check(false, name, "readback raised: " + e);
    continue;
  }
  // a RatioPair reads back only when its two fields agree
  local gotValue = pair ? got.First : ratio ? got.Ratio : got.X;
  if (pair && got.Second != got.First) readOK = false;
  check(readOK && read.bitsProcessed() == bits && gotValue == readBack,
    name, "lawful value did not read back as its quantized value");
}

// A summary; tools/casegate holds every case of the file to an answer. An
// empty cases.txt is a failure.
print("packet-write-cfloat-squirrel: ran " + ran + ", failed " + failed + "\n");
if (failed != 0 || ran == 0) return 1;
print("packet-write-cfloat-squirrel: shared cases passed\n");
return 0;
