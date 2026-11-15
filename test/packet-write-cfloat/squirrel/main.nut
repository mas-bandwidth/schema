// test/packet-write-cfloat/squirrel/main.nut — Squirrel packet Write range
// contract (SPEC §5.1 row 10, PACKET-WIRE-ALGORITHM §2.5).
//
// usage: sq main.nut <cases.txt> <generated-dir>

local argv = vargv;
if (argv.len() < 2) throw "usage: main.nut <cases.txt> <generated-dir>";
local casesPath = argv[0];
local generatedDir = argv[1];

local serialize = dofile("internal/codegen/squirrel/serialize/serialize.nut", true);
local CFloat = dofile(generatedDir + "/schema_module.nut", true).require("CFloat");

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
local check = function(ok, name, detail) {
  if (!ok) {
    print("FAILED: packet-write-cfloat " + name + ": " + detail + "\n");
    failed++;
  }
};

local input = readText(casesPath);
local start = 0;
for (local i = 0; i <= input.len(); i++) {
  if (i < input.len() && input[i] != 10) continue;
  local raw = input.slice(start, i);
  start = i + 1;
  local fields = splitFields(raw);
  if (fields.len() < 3 || fields[0][0] == 35) continue;
  local name = fields[0];
  local value = parseFloat(fields[1]);
  local expected = fields[2];
  local readBack = value;
  if (fields.len() >= 5 && fields[3] == "reads-back") readBack = parseFloat(fields[4]);

  local record = CFloat.CFloat();
  record.X = value;
  local stream = serialize.WriteStream(blob(16));
  local wrote = false;
  local raised = null;
  try {
    wrote = CFloat.WriteCFloat(stream, record);
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

  local bits = stream.bitsProcessed();
  stream.flush();
  local read = serialize.ReadStream(stream.data());
  local got = CFloat.CFloat();
  local readOK = false;
  try {
    readOK = CFloat.ReadCFloat(read, got);
  } catch (e) {
    check(false, name, "readback raised: " + e);
    continue;
  }
  check(readOK && read.bitsProcessed() == bits && got.X == readBack,
    name, "lawful value did not read back as its quantized value");
}

if (failed != 0) return 1;
print("packet-write-cfloat-squirrel: shared cases passed\n");
return 0;
