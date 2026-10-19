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
// read report, the table-wire-load card) and wire (load then save, the
// table-wire-load and table-wire-save cards). A case whose unit the target
// refuses, or whose construct its wire does not carry, is answered
// `<case>.absent`. One process per surface, one file per case, and nothing on
// stderr, which the driver script treats as a crash.

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

local writeBytes = function(path, b) {
  local f = file(path, "wb");
  for (local i = 0; i < b.len(); i++) f.writen(b[i] & 0xff, 'b');
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

local shq = function(s) {
  local out = "'";
  for (local i = 0; i < s.len(); i++) {
    if ((s[i] & 0xff) == 39) out += "'\\''";
    else out += format("%c", s[i] & 0xff);
  }
  return out + "'";
};

local baseName = function(path) {
  local cut = -1;
  local at;
  while ((at = path.find("/", cut + 1)) != null) cut = at;
  return path.slice(cut + 1);
};

// THE TABLE MODULE'S NAME. The target names it <Package>Tables, and the
// manifest key is not the package (unit scalars is package scalardemo), so the
// unit directory's *Tables.nut files name it. A unit that also contains a
// source file named Tables.schema emits a per-file Tables.nut beside the
// package module, so the file whose header says it is the TABLE surface is the
// one; zero or more than one is no module.
local tableModuleName = function(dir) {
  local tmp = getenv("TMPDIR");
  if (tmp == null || tmp.len() == 0) tmp = getenv("TMP");
  if (tmp == null || tmp.len() == 0) tmp = dir;
  local h = 2166136261;
  for (local i = 0; i < dir.len(); i++) h = ((h ^ (dir[i] & 0xff)) * 16777619) & 0xffffffff;
  local listing = tmp + "/schema-tables-" + format("%08x", h) + ".txt";
  local name = null;
  try {
    system("ls -1 -- " + shq(dir) + "/*Tables.nut > " + shq(listing) + " 2>/dev/null");
    local text = readText(listing);
    local lines = [];
    local start = 0;
    for (local i = 0; i < text.len(); i++) {
      if (text[i] == 10) {
        if (i > start) lines.append(text.slice(start, i));
        start = i + 1;
      }
    }
    if (start < text.len()) lines.append(text.slice(start));
    local found = 0;
    foreach (path in lines) {
      local leaf = baseName(path);
      if (leaf.len() <= 4 || leaf.slice(leaf.len() - 4) != ".nut") continue;
      local src = "";
      try {
        local g = file(path, "rb");
        local gn = g.len() < 512 ? g.len() : 512;
        local gb = gn > 0 ? g.readblob(gn) : blob(0);
        g.close();
        for (local k = 0; k < gb.len(); k++) src += format("%c", gb[k] & 0xff);
      } catch (e) { continue; }
      if (src.find("the TABLE surface") == null) continue;
      found++;
      name = leaf.slice(0, leaf.len() - 4);
    }
    if (found != 1) name = null;
  } catch (e) { name = null; }
  try { remove(listing); } catch (e) {}
  return name;
};

local newReport = function() {
  return { unknown = 0, kindMismatch = 0, widened = 0, clamped = 0, duplicate = 0, malformed = false, refused = false };
};

local openUnit = function(unit) {
  local dir = corpus + "/" + unit;
  local module = tableModuleName(dir);
  if (module == null) throw "main.nut: " + unit + " has no single *Tables.nut with a TABLE SURFACE";
  local schema = dofile(dir + "/schema_module.nut", true);
  return [schema, schema.require(module)];
};

local loadReport = function(unit, root, bytes) {
  local opened = openUnit(unit);
  local schema = opened[0];
  local T = opened[1];
  local L = schema.require("schema_table_load");
  local report = newReport();
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
    local cname = f[1]; local unit = f[2]; local root = f[3]; local wirepath = f[4];
    if (!exists(corpus + "/" + unit + "/schema_table_load.nut") ||
        !exists(corpus + "/" + unit + "/schema_table_save.nut")) {
      writeAbsent(cname);
      continue;
    }
    local out = null;
    try {
      local opened = openUnit(unit);
      local schema = opened[0];
      local T = opened[1];
      local L = schema.require("schema_table_load");
      local S = schema.require("schema_table_save");
      local report = newReport();
      local v = L.load(T, root, readBytes(wirepath), report);
      if (v != null && !report.refused) out = S.save(T, root, v);
    } catch (e) { out = null; }
    if (out == null) { writeAbsent(cname); continue; }
    writeBytes(outdir + "/" + cname, out);
  }
  return;
}
