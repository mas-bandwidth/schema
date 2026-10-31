// test/conformance/squirrel/wirefuzz.nut — the SQUIRREL WIRE-FUZZ DRIVER
// (test/conformance/README.md, "The wire-fuzz driver"; docs/SPEC-TABLES.md
// §4.2; docs/PORT-SQUIRREL.md, THE HARNESS and THE CARD MAP).
//
// usage: sq test/conformance/squirrel/wirefuzz.nut [builder]
//
// ONE sq process for the whole run, on a pipe, that only reads. The harness
// generates every mutant and holds the oracle; this leg loads each mutant
// through the emitted tolerant read (schema_table_load), saves whatever it
// decoded (schema_table_save), and answers what happened. It decides nothing
// about what a mutant means.
//
// The host shape is the conformance leg's main.nut: the working directory is
// the repository root, the generated corpus is build/conformance-squirrel
// (make/squirrel.mk) unless $SCHEMA_SQUIRREL_CONFORMANCE names another, and a
// unit is opened through its own loader, schema_module.nut, then the table
// module the target emitted, the unit directory's single *Tables.nut. The
// module is named by the unit's PACKAGE, not by its manifest key.
//
// THE STREAM IS LITTLE-ENDIAN (test/conformance/README.md), and every
// multi-byte value is composed byte by byte, never through readn/writen of a
// wider type, which are the HOST's byte order (docs/PORT-SQUIRREL.md, BYTE
// BUFFERS).
//
// THE ROSTER this leg answers 1 for is the form-1 FILE roots of the units the
// target generates, with retention off. A form-2 (message) entry is driven
// through LoadMessages/SaveMessages, which this leg does not carry, and the
// Squirrel port has no retention (docs/SPEC-TABLES.md §6.6): both answer 0, as
// does a root of a unit the target refuses (build/conformance-squirrel/
// REFUSED.txt), so the harness never sends them a mutant.
//
// TWO ARMS, ONE DRIVER (make/squirrel-fuzz.mk). The Squirrel load builds
// class instances and has no caller region to size (docs/PORT-SQUIRREL.md,
// THE TYPE MAPPING), so it has no LoadMeasure, and `measure` is -1 on every
// reply. The harness's plain arm holds a VARIABLE root's reply to its region
// measure, so in that arm (no argument) this leg answers the FIXED-class
// roots, whose Load always fills a value (loaded = 1) and is never measured.
// The VARIABLE-class roots run under the harness's builder arm (`builder`,
// harness --builder), which compares the builder's completion — `loaded` is
// whether the read finished — the report and the saved bytes, and never a
// region measure.
//
// A THROW IS A FAILED PROBE. The emitted load and save report; they never
// throw (docs/PORT-SQUIRREL.md, BYTE BUFFERS: "a thrown error is a runtime
// error, and a runtime error is what the harness counts as a failed probe").
// Nothing here catches one: an uncaught error writes to stderr and ends the
// stream, and the harness names the mutant that caused it.

local corpus = "build/conformance-squirrel";
local override = getenv("SCHEMA_SQUIRREL_CONFORMANCE");
if (override != null && override.len() > 0) corpus = override;

local input = ::stdin;
local out = ::stdout;

local FORM_VARIABLE = 1;
local builder = vargv.len() >= 1 && vargv[0] == "builder";

// read exactly n bytes, or null at the end of the stream. A pipe has no
// length, and the stream's readblob caps a read at len(), so the bytes are
// COPIED eight at a time through readn('l') and writen('l'): both are the
// host's order, so the copy is byte for byte on any host and no value is
// interpreted. The tail is copied one unsigned byte ('b') at a time.
local take = function(n) {
  local b = blob(n);
  try {
    local words = n >> 3;
    for (local i = 0; i < words; i++) b.writen(input.readn('l'), 'l');
    for (local i = words << 3; i < n; i++) b.writen(input.readn('b'), 'b');
  } catch (e) { return null; }
  b.seek(0);
  return b;
};

local u16At = function(b, at) { return (b[at] & 0xff) | ((b[at + 1] & 0xff) << 8); };
local u32At = function(b, at) {
  return (b[at] & 0xff) | ((b[at + 1] & 0xff) << 8) |
         ((b[at + 2] & 0xff) << 16) | ((b[at + 3] & 0xff) << 24);
};

local nameOf = function() {
  local h = take(2);
  if (h == null) throw "wirefuzz.nut: the roster ended inside a name";
  local n = u16At(h, 0);
  local b = take(n);
  if (b == null) throw "wirefuzz.nut: the roster ended inside a name";
  local s = "";
  for (local i = 0; i < n; i++) s += format("%c", b[i] & 0xff);
  return s;
};

local putU8 = function(b, v) { b.writen(v & 0xff, 'b'); };
local putLE = function(b, v, n) {
  for (local i = 0; i < n; i++) b.writen((v >> (8 * i)) & 0xff, 'b');
};

local exists = function(path) {
  try { local f = file(path, "rb"); f.close(); return true; }
  catch (e) { return false; }
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

local textOf = function(b) {
  local s = "";
  for (local i = 0; i < b.len(); i++) s += format("%c", b[i] & 0xff);
  return s;
};

// THE TABLE MODULE'S NAME. The target names it <Package>Tables, and the
// manifest key is not the package (unit scalars is package scalardemo), so the
// unit directory's *Tables.nut files name it. A unit that also contains a
// source file named Tables.schema emits a per-file Tables.nut beside the
// package module, so the file whose header says it is the TABLE surface is the
// one; zero or more than one is no module, and the unit is not opened
// and its roots answer 0.
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
    local f = file(listing, "rb");
    local n = f.len();
    local b = n > 0 ? f.readblob(n) : blob(0);
    f.close();
    local text = textOf(b);
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
        src = textOf(gb);
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

// one unit key to its opened module, its load and its save; false when the
// target did not generate the unit
local units = {};
local openUnit = function(key) {
  if (key in units) return units[key];
  local dir = corpus + "/" + key;
  local opened = false;
  local module = tableModuleName(dir);
  if (module != null && exists(dir + "/schema_module.nut") && exists(dir + "/schema_table_load.nut") &&
      exists(dir + "/schema_table_save.nut")) {
    local schema = dofile(dir + "/schema_module.nut", true);
    opened = {
      T = schema.require(module),
      L = schema.require("schema_table_load"),
      S = schema.require("schema_table_save")
    };
  }
  units[key] <- opened;
  return opened;
};

// THE ROSTER (test/conformance/README.md): u32 count, then per root the unit
// key, the root's name, the FORM and RETAIN; one byte per entry in reply.
local head = take(4);
if (head == null) return;
local count = u32At(head, 0);
local roster = array(count, null);
local reply = blob(count);
for (local i = 0; i < count; i++) {
  local unit = nameOf();
  local root = nameOf();
  local fr = take(2);
  if (fr == null) throw "wirefuzz.nut: the roster ended inside an entry";
  local form = fr[0] & 0xff;
  local retain = fr[1] & 0xff;
  local codec = null;
  if (form == FORM_VARIABLE && retain == 0) {
    local u = openUnit(unit);
    if (u != false && (root in u.T.Descriptors) && u.T.Descriptors[root].variable == builder) {
      codec = { u = u, root = root, d = u.T.Descriptors[root] };
    }
  }
  roster[i] = codec;
  putU8(reply, codec == null ? 0 : 1);
}
out.writeblob(reply);
out.flush();

// THE MUTANTS, one reply each, flushed before the next is read
for (;;) {
  local mh = take(8);
  if (mh == null) break;
  local index = u32At(mh, 0);
  local size = u32At(mh, 4);
  local wire = take(size);
  if (wire == null) throw "wirefuzz.nut: the stream ended inside a mutant";
  if (index >= count || roster[index] == null) {
    throw "wirefuzz.nut: a mutant for roster entry " + index + ", which this leg answered 0";
  }
  local c = roster[index];
  local report = {
    unknown = 0, kindMismatch = 0, widened = 0, clamped = 0,
    duplicate = 0, malformed = false, refused = false, stopped = false
  };
  local v = c.u.L.loadTolerant(c.u.T, c.root, wire, report);
  // THE BUILDER ARM: `loaded` is whether the read finished. A refused form
  // and a framing too damaged to open answer null, a root body that stopped
  // before its terminator says so in `stopped`, and a stopped builder saves
  // nothing.
  // THE PLAIN ARM (a fixed-class root): the root always loads. Where the read
  // opened nothing, the value is the one it read into, at its declared
  // defaults, and that is what is saved (test/conformance/harness/
  // wirefuzz.go, the oracle on a refusal; docs/SPEC-TABLES.md §4.2).
  local loaded = v != null && !report.stopped;
  if (!builder) {
    if (v == null) v = c.d.make();
    loaded = true;
  }
  local saved = null;
  if (loaded) saved = c.u.S.save(c.u.T, c.root, v);
  local r = blob(0);
  putU8(r, loaded ? 1 : 0);
  putLE(r, report.unknown, 4);
  putLE(r, report.kindMismatch, 4);
  putLE(r, report.widened, 4);
  putLE(r, report.clamped, 4);
  putLE(r, report.duplicate, 4);
  putU8(r, report.malformed ? 1 : 0);
  putU8(r, report.refused ? 1 : 0);
  putLE(r, -1, 8);   // measure: no region (the header above)
  putLE(r, 0, 4);    // retained: no retention
  putLE(r, 0, 4);    // retain_lost
  putLE(r, saved == null ? -1 : saved.len(), 8);
  out.writeblob(r);
  if (saved != null && saved.len() > 0) out.writeblob(saved);
  out.flush();
}
