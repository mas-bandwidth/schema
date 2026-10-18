// test/conformance/squirrel/main.nut — the SQUIRREL conformance driver's one
// script (test/conformance/README.md, test/conformance/squirrel/driver; docs/
// PORT-SQUIRREL.md, THE HARNESS).
//
// usage: sq main.nut <manifest> list
//        sq main.nut <manifest> <surface> <outdir>
//
// The working directory is the repository root, so every path the manifest
// names resolves from here. The generated corpus is build/conformance-squirrel
// by default (make/squirrel.mk); $SCHEMA_SQUIRREL_CONFORMANCE overrides it, and
// the negative control points the same driver at a sabotaged copy. The driver
// answers the surfaces whose runtime is on the base: report (the variable wire's
// read report, the table-wire-load card) and wire (whose save is the
// table-wire-save card's; until it lands every case is answered absent). One
// process per surface, one file per case, and nothing on stderr, which the
// driver script treats as a crash.

local argv = vargv;
if (argv.len() < 2) { return; }
local manifest = argv[0];
local mode = argv[1];
local outdir = argv.len() >= 3 ? argv[2] : "";
local corpus = "build/conformance-squirrel";
local override = getenv("SCHEMA_SQUIRREL_CONFORMANCE");
if (override != null && override.len() > 0) corpus = override;

local exists = function(path) {
  try { local f = file(path, "rb"); f.close(); return true; }
  catch (e) { return false; }
};

local readBytes = function(path) {
  local f = file(path, "rb");
  local b = f.readblob(f.len());
  f.close();
  return b;
};

local readText = function(path) {
  local b = readBytes(path);
  local s = "";
  for (local i = 0; i < b.len(); i++) s += format("%c", b[i] & 0xff);
  return s;
};

local writeText = function(path, s) {
  local f = file(path, "wb");
  for (local i = 0; i < s.len(); i++) f.writen(s[i] & 0xff, 'b');
  f.close();
};

local writeAbsent = function(name) {
  local f = file(outdir + "/" + name + ".absent", "wb");
  f.close();
};

local splitFields = function(line) {
  local out = [];
  local cur = "";
  local n = line.len();
  for (local i = 0; i <= n; i++) {
    local c = i < n ? line[i] : 32;
    if (c == 32 || c == 9 || c == 13 || i == n) {
      if (cur.len() > 0) { out.append(cur); cur = ""; }
    } else {
      cur += format("%c", c & 0xff);
    }
  }
  return out;
};

local manifestLines = function(s) {
  local out = [];
  local start = 0;
  local n = s.len();
  for (local i = 0; i < n; i++) {
    if (s[i] == 10) { out.append(s.slice(start, i)); start = i + 1; }
  }
  if (start < n) out.append(s.slice(start));
  return out;
};

local cap = function(s) {
  if (s.len() == 0) return s;
  return format("%c", (s[0] & 0xdf)) + s.slice(1);
};

local loadReport = function(unit, root, bytes) {
  local schema = dofile(corpus + "/" + unit + "/schema_module.nut", true);
  local T = schema.require(cap(unit) + "Tables");
  local L = schema.require("schema_table_load");
  local report = { unknown = 0, kindMismatch = 0, widened = 0, clamped = 0, duplicate = 0, malformed = false, refused = false };
  L.load(T, root, bytes, report);
  local verdict = report.refused ? "refused" : "read";
  local m = report.malformed ? "true" : "false";
  return report.unknown.tostring() + "," + report.kindMismatch.tostring() + "," +
         report.widened.tostring() + "," + report.clamped.tostring() + "," +
         report.duplicate.tostring() + "," + m + "," + verdict + "\n";
};

if (mode == "list") {
  print("report\n");
  print("wire\n");
  return;
}

local lines = manifestLines(readText(manifest));

if (mode == "report") {
  foreach (ln in lines) {
    local f = splitFields(ln);
    if (f.len() < 5 || f[0] != "report") continue;
    local cname = f[1]; local unit = f[2]; local root = f[3]; local wirepath = f[4];
    if (!exists(corpus + "/" + unit + "/schema_table_load.nut")) {
      writeAbsent(cname);
      continue;
    }
    local ok = false;
    local line = "";
    try {
      line = loadReport(unit, root, readBytes(wirepath));
      ok = true;
    } catch (e) { ok = false; }
    if (!ok) { writeAbsent(cname); continue; }
    writeText(outdir + "/" + cname, line);
  }
  return;
}

if (mode == "wire") {
  foreach (ln in lines) {
    local f = splitFields(ln);
    if (f.len() < 5 || f[0] != "instance") continue;
    writeAbsent(f[1]);
  }
  return;
}
