// THE STRICT READ AND ITS NAMED REFUSALS (docs/SPEC-TABLES.md §6.5, §11,
// §2.14). ReadStrict answers the root or refuses with ONE name: a counter the
// tolerant load moved (clamped, unknown, kind_mismatch, duplicate, malformed),
// a form byte the root's own form does not read, or a checksum that does not
// match. It is a wrapper over schema_table_load's tolerant walk; the counters
// are the load's report (docs/SPEC-TABLES.md §4).
local _M = vargv[0];
local _load = vargv[1]("schema_table_load");

local FORM_VARIABLE = 1;
local FORM_MESSAGE = 2;
local FORM_FIXED = 3;

// the report's six counters, in place, as load.nut's own (docs/SPEC-TABLES.md §4)
local reset = function(report) {
  if (!("unknown" in report)) report.unknown <- 0;
  if (!("kindMismatch" in report)) report.kindMismatch <- 0;
  if (!("widened" in report)) report.widened <- 0;
  if (!("clamped" in report)) report.clamped <- 0;
  if (!("duplicate" in report)) report.duplicate <- 0;
  if (!("malformed" in report)) report.malformed <- false;
  if (!("refused" in report)) report.refused <- false;
};

// CRC-32C, the Castagnoli polynomial, reflected 0x82F63B78, initial and final
// 0xFFFFFFFF (docs/SPEC-TABLES.md §2.14). No table: a strict read is not hot.
local crc32c = function(b, n) {
  local crc = 0xFFFFFFFF;
  for (local i = 0; i < n; i++) {
    crc = (crc ^ (b[i] & 0xff)) & 0xFFFFFFFF;
    for (local k = 0; k < 8; k++) {
      if (crc & 1) crc = ((crc >> 1) ^ 0x82F63B78) & 0xFFFFFFFF;
      else crc = (crc >> 1) & 0xFFFFFFFF;
    }
  }
  return (crc ^ 0xFFFFFFFF) & 0xFFFFFFFF;
};

local u32le = function(b, at) {
  return (b[at] & 0xff) | ((b[at + 1] & 0xff) << 8) |
         ((b[at + 2] & 0xff) << 16) | ((b[at + 3] & 0xff) << 24);
};

// §2.14's order: the form byte first, then the trailer before any field is
// decoded. A mismatch is the whole-file damage named integrity_mismatch.
local integrityOpen = function(report, b) {
  if (b.len() < 5 || u32le(b, b.len() - 4) != crc32c(b, b.len() - 4)) {
    report.malformed = true;
    return null;
  }
  local out = blob(b.len() - 4);
  for (local i = 0; i < out.len(); i++) out[i] = b[i];
  return out;
};

// ReadStrict(T, root, bytes, report): the root instance or null, with
// report.reason the name of the first refusal. A form byte the root does not
// read moves no counter (§3, §6.5); a tolerant counter does (§4, §11).
_M.ReadStrict <- function(T, root, bytes, report) {
  if (report == null) report = {};
  reset(report);
  report.reason <- null;
  local d = T.Descriptors[root];
  local fixed = d != null && d.fixed;
  if (bytes.len() < 1) {
    report.malformed = true;
    report.reason = "malformed";
    return null;
  }
  local form = bytes[0] & 0xff;
  if (fixed) {
    // a fixed root reads form 3 only; form 1 is OLDER (§3, §3.4), form 2 is a
    // batch where a file was expected, and any other byte is a newer form.
    if (form == FORM_VARIABLE) { report.reason = "previous_form"; return null; }
    if (form == FORM_MESSAGE) { report.reason = "message_form_as_file"; return null; }
    if (form != FORM_FIXED) { report.reason = "newer_form"; return null; }
  }
  // a variable root reads form 1 only (§3)
  if (!fixed && form != FORM_VARIABLE) {
    if (form == FORM_MESSAGE) report.reason = "message_form_as_file";
    else report.reason = "newer_form";
    return null;
  }
  local integrity = null;
  if (d != null && ("integrity" in d)) integrity = d.integrity;
  else if ("Integrity" in T) integrity = T.Integrity;
  if (integrity != null && integrity != "") {
    local body = integrityOpen(report, bytes);
    if (body == null) { report.reason = "integrity_mismatch"; return null; }
    bytes = body;
  }
  local v = _load.load(T, root, bytes, report);
  if (report.malformed) report.reason = "malformed";
  else if (report.duplicate > 0) report.reason = "duplicate";
  else if (report.clamped > 0) report.reason = "clamped";
  else if (report.unknown > 0) report.reason = "unknown";
  else if (report.kindMismatch > 0) report.reason = "kind_mismatch";
  else report.reason = null;
  if (report.reason != null) return null;
  return v;
};

// the names this file owns, for a caller that wants the spelling rather than
// the reason string (docs/SPEC-TABLES.md §11)
_M.ReadStrictNames <- {
  previous_form = true, message_form_as_file = true, newer_form = true,
  integrity_mismatch = true, clamped = true, unknown = true,
  kind_mismatch = true, duplicate = true, malformed = true
};
