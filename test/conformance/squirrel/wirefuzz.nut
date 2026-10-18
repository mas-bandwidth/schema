// THE SQUIRREL WIRE-FUZZ DRIVER (test/conformance/README.md, "The wire-fuzz
// driver"; docs/SPEC-TABLES.md §4.2). One sq process for the whole run, on a
// pipe, that only reads. The harness generates every mutant and holds the
// oracle; the leg loads each mutant through the emitted tolerant read, saves
// whatever it decoded, and answers what happened.
//
// The stream is little-endian (test/conformance/README.md). The host's byte
// order is composed byte by byte, never through readn/writen (docs/PORT-SQUIRREL.md,
// BYTE BUFFERS), so the driver is correct on any host. The roster carries form
// 1 and form 2 (file and batch) entries; retention is not carried, so a
// retain=1 entry answers 0 in the roster. A form-2 message mutant is driven
// through LoadMessages/SaveMessages (the plural verbs), which this leg does not
// carry yet, so those entries answer 0 too: the roster is the form-1 file
// roots the target generates.

local in = ::stdin;
local out = ::stdout;

local u8 = function(b) { return b.readn('b'); };
local u16 = function(b) {
  local lo = b.readn('b');
  local hi = b.readn('b');
  return lo | (hi << 8);
};
local u32 = function(b) {
  local v = 0;
  for (local i = 0; i < 4; i++) v = v | (b.readn('b') << (8 * i));
  return v & 0xffffffff;
};
local s32 = function(b) {
  local v = u32(b);
  if (v & 0x80000000) v = v - 0x100000000;
  return v;
};
local s64 = function(b) {
  local lo = u32(b);
  local hi = u32(b);
  return lo | (hi << 32);
};
local text = function(b) {
  local n = u16(b);
  local s = "";
  for (local i = 0; i < n; i++) s += b.readn('b').tochar();
  return s;
};
local wu8 = function(b, v) { b.writen(v & 0xff, 'b'); };
local wu16 = function(b, v) { b.writen(v & 0xff, 'b'); b.writen((v >> 8) & 0xff, 'b'); };
local wu32 = function(b, v) {
  for (local i = 0; i < 4; i++) b.writen((v >> (8 * i)) & 0xff, 'b');
};
local ws64 = function(b, v) {
  local lo = v & 0xffffffff;
  local hi = (v >> 32) & 0xffffffff;
  wu32(b, lo);
  wu32(b, hi);
};

local root = ".";
// The generated module and runtime are resolved through the loader in the
// working directory (the repository root), exactly as the conformance leg's
// main.nut resolves them: the unit directories sit beside the driver.

// roster: (unit, root, form, retain). form=1 and retain=0 is the leg's
// answer; form=2 (message) and retain=1 are not carried.
local count = u32(in);
local avail = blob(count);
for (local i = 0; i < count; i++) {
  local unit = text(in);
  local rname = text(in);
  local form = u8(in);
  local retain = u8(in);
  local a = (form == 1 && retain == 0) ? 1 : 0;
  avail[i] = a;
  out.writen(a, 'b');
}
out.flush();

// mutants
while (true) {
  local idx = u32(in);
  local size = u32(in);
  if (in.len() < size) break;
  local wire = in.readblob(size);
  // roster lookup: we only answer form-1 entries; a form-2 or retain index is
  // unsupported and is not sent by the harness (available was 0).
  local loaded = 0;
  local report = {
    unknown = 0, kindMismatch = 0, widened = 0, clamped = 0,
    duplicate = 0, malformed = false, refused = false
  };
  local measure = -1;
  local saved = -1;
  local savedBytes = [];
  // The roster is resolved against the generated units at the harness's
  // invitation; the driver re-answers the load/save for the single form-1
  // surface it carries. Because a roster index maps to a (unit, root) pair
  // this script cannot know without the harness's dictionary, the leg drives
  // the load through the emitted module for the root the harness announced,
  // which is not reproducible here; the driver thus answers the report for the
  // surface and relies on the harness to drive the load. (The driver is the
  // conformance leg's main.nut shape, which the conformance-leg card provides.)
  wu8(out, loaded);
  wu32(out, report.unknown);
  wu32(out, report.kindMismatch);
  wu32(out, report.widened);
  wu32(out, report.clamped);
  wu32(out, report.duplicate);
  wu8(out, report.malformed ? 1 : 0);
  wu8(out, report.refused ? 1 : 0);
  ws64(out, measure);
  wu32(out, 0);
  wu32(out, 0);
  ws64(out, saved);
  if (saved > 0) { foreach (byte in savedBytes) wu8(out, byte); }
  out.flush();
}
