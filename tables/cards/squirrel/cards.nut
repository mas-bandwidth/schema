// cards.nut — the Squirrel card facade (tables/cards/cards.go, parse.go,
// render.go, edit.go, upgrade.go), the owning layer a customer loads. It is a
// port of tables/cards/lua/cards.lua into Squirrel's own spelling.
//
// The generated table module (CardsTables) and the runtime beside it
// (docs/PORT-SQUIRREL.md, THE RUNTIME LAYOUT) answer the wire and text forms;
// this facade is the thin owning layer: a Record that keeps its own copy of
// the wire, checked writes that refuse before publishing, and the two customer
// ceilings the schema deliberately leaves unbounded (docs/SPEC-TABLES.md §2.5,
// §2.9). It answers the nine surfaces of tables/cards/conformance byte for
// byte with the Go leg (tla/CardHops.tla: ImplementationsAgree,
// HopOrderIrrelevant).
//
// THE LOADING. The caller loads the generated unit through its own loader and
// hands the module to this script, which answers the facade table:
//
//   local T = dofile(gen + "/schema_module.nut", true).require("CardsTables");
//   local cards = loadfile("tables/cards/squirrel/cards.nut", true).call(getroottable(), T);
//
// THE READS ARE STRICT (docs/plans/strict-only.md). Every read goes through
// the unit's public strict entries — CardLoad, CardFromJson, CardLoadRetain,
// CardSave, CardSaveRetain — and answers the whole value or refuses; nothing
// here reads a tolerant walk, repairs a value or counts a repair. The report a
// Record carries is the four counters an accepted read may move (Retained,
// RetainLost, Widened, Unknown), read off the runtime's report by name.
//
// THE ERROR IDIOM. A refusal is an instance of cards.CardError, answered in
// place of the value and never thrown: its `cls` is one of the closed refusal
// classes tables/cards/conformance/FORMAT.md fixes (body-bound 65536,
// list-bound <field> 512, stranded-key <KEY>, retain-lost, unknown-present,
// malformed), never free text, and its `message` (and tostring) is the reason
// for a person. A refusal after a read carries that read's report.
//
// THE VALUES. A wire is a blob, and a Record owns its own copy: the blob a
// caller handed in, or the one Wire() hands out, can change without touching
// the record. A text is a Squirrel string (bytes; every bound is a byte
// count). A `*string` member (Body, Done, Source, a KeyValue's Value) is a
// string or null (docs/PORT-SQUIRREL.md, THE TYPE MAPPING), and an enum
// member is its variant's name or null.

local T = vargv[0];

local M = {};

local ROOT = "Card";

// MaxBodyBytes is the customer ceiling on one card's opaque body. The schema
// declares body *string with no bound (docs/SPEC-TABLES.md §2.5); this facade
// refuses a larger body before any bytes are published.
M.MaxBodyBytes <- 65536;

// MaxListEntries is the customer ceiling on each of a card's collection fields
// (paths/_new/shared/needs/extra/consumers), which the schema leaves unbounded
// (docs/SPEC-TABLES.md §2.9).
M.MaxListEntries <- 512;

// MaxRetainBytes and MaxRetainIDs are the customer capacities of the retained
// byte store and the retained-id store; neither grows on demand
// (docs/SPEC-TABLES.md §6.6).
M.MaxRetainBytes <- 1 << 20;
M.MaxRetainIDs <- 4096;

// ---- errors -----------------------------------------------------------------

local CardError = class {
  message = null;
  cls = null;
  report = null;
  constructor(message, cls, report = null) {
    this.message = message;
    this.cls = cls;
    this.report = report;
  }
  function _tostring() { return this.message; }
};
M.CardError <- CardError;

local refuse = function(message, cls, report = null) { return CardError(message, cls, report); };

M.refused <- function(v) { return v instanceof CardError; };

// ---- the read report ----------------------------------------------------------

// Report is the four counters an accepted strict read may move. A strict read
// refuses a kind mismatch, an out-of-range value, a repeat and damage, so the
// rest of Go's TableReport is zero on every Report this facade answers
// (tables/cards/conformance/FORMAT.md, "The read report").
local Report = class {
  Retained = 0;
  RetainLost = 0;
  Widened = 0;
  Unknown = 0;
};
M.Report <- Report;

local counter = function(r, name) {
  if (r == null || !(name in r) || typeof r[name] != "integer") return 0;
  return r[name];
};

local reportOf = function(r) {
  local out = Report();
  out.Retained = counter(r, "retained");
  out.RetainLost = counter(r, "retainLost");
  out.Widened = counter(r, "widened");
  out.Unknown = counter(r, "unknown");
  return out;
};

// fresh copies a runtime report a strict read filled, with every counter and
// flag at rest: the retaining read counts into the table it is handed, so it
// is handed one with every slot the runtime names and none of a prior read's
// values (no slot is spelled here, so a renamed counter still resets).
local fresh = function(r) {
  local out = {};
  foreach (k, v in r) {
    if (typeof v == "integer") out[k] <- 0;
    else if (typeof v == "bool") out[k] <- false;
    else out[k] <- null;
  }
  return out;
};

// ---- bytes, strings and UTF-8 ----------------------------------------------------

local copyBlob = function(b) {
  local out = blob(b.len());
  for (local i = 0; i < b.len(); i++) out[i] = b[i] & 0xff;
  return out;
};

local toBlob = function(s) {
  if (typeof s != "string") return copyBlob(s);
  local out = blob(s.len());
  for (local i = 0; i < s.len(); i++) out[i] = s[i] & 0xff;
  return out;
};
M.toBlob <- toBlob;

// toStr answers bytes [from, to) of a blob or string as a string, built by
// halves so a long body costs n log n, not one copy per byte.
local toStr;
toStr = function(b, from, to) {
  if (to - from <= 64) {
    local s = "";
    for (local i = from; i < to; i++) s += (b[i] & 0xff).tochar();
    return s;
  }
  local mid = from + ((to - from) >> 1);
  return toStr(b, from, mid) + toStr(b, mid, to);
};
M.toStr <- function(b) { return typeof b == "string" ? b : toStr(b, 0, b.len()); };

local sameBytes = function(a, b) {
  if (a.len() != b.len()) return false;
  for (local i = 0; i < a.len(); i++) if ((a[i] & 0xff) != (b[i] & 0xff)) return false;
  return true;
};
M.sameBytes <- sameBytes;

// utf8Valid is strict UTF-8 over a string or blob: no overlong form, no
// surrogate, nothing past U+10FFFF, no truncated sequence.
local utf8Valid = function(s) {
  local n = s.len();
  local i = 0;
  while (i < n) {
    local c = s[i] & 0xff;
    if (c < 0x80) { i++; continue; }
    local need = 0, min = 0, cp = 0;
    if (c >= 0xc2 && c <= 0xdf) { need = 1; cp = c & 0x1f; min = 0x80; }
    else if (c >= 0xe0 && c <= 0xef) { need = 2; cp = c & 0x0f; min = 0x800; }
    else if (c >= 0xf0 && c <= 0xf4) { need = 3; cp = c & 0x07; min = 0x10000; }
    else return false;
    if (i + need >= n) return false;
    for (local k = 1; k <= need; k++) {
      local x = s[i + k] & 0xff;
      if ((x & 0xc0) != 0x80) return false;
      cp = (cp << 6) | (x & 0x3f);
    }
    if (cp < min || cp > 0x10ffff || (cp >= 0xd800 && cp <= 0xdfff)) return false;
    i += need + 1;
  }
  return true;
};

local hasNUL = function(s) {
  for (local i = 0; i < s.len(); i++) if ((s[i] & 0xff) == 0) return true;
  return false;
};

local wellFormed = function(s) { return utf8Valid(s) && !hasNUL(s); };

local hasCRLF = function(s) {
  for (local i = 0; i < s.len(); i++) {
    local c = s[i] & 0xff;
    if (c == 13 || c == 10) return true;
  }
  return false;
};

// Go's unicode.IsSpace set, as UTF-8 byte sequences, for strings.TrimSpace.
local SPACE_SEQS = [
  [0xc2, 0x85], [0xc2, 0xa0], [0xe1, 0x9a, 0x80], [0xe2, 0x80, 0xa8], [0xe2, 0x80, 0xa9],
  [0xe2, 0x80, 0xaf], [0xe2, 0x81, 0x9f], [0xe3, 0x80, 0x80],
];
for (local b = 0x80; b <= 0x8a; b++) SPACE_SEQS.append([0xe2, 0x80, b]);

local seqAt = function(s, i, seq) {
  if (i < 0 || i + seq.len() > s.len()) return false;
  foreach (k, x in seq) if ((s[i + k] & 0xff) != x) return false;
  return true;
};

// spaceAt answers the byte length of the space starting at i, or 0
local spaceAt = function(s, i) {
  if (i >= s.len()) return 0;
  local b = s[i] & 0xff;
  if (b == 0x20 || (b >= 0x09 && b <= 0x0d)) return 1;
  if (b < 0x80) return 0;
  foreach (seq in SPACE_SEQS) if (seqAt(s, i, seq)) return seq.len();
  return 0;
};

// spaceBefore answers the byte length of the space ending just before j, or 0
local spaceBefore = function(s, j) {
  foreach (n in [1, 2, 3]) {
    local i = j - n;
    if (i >= 0 && spaceAt(s, i) == n) return n;
  }
  return 0;
};

// trim is strings.TrimSpace.
local trim = function(s) {
  local i = 0, j = s.len();
  while (i < j) {
    local n = spaceAt(s, i);
    if (n == 0) break;
    i += n;
  }
  while (j > i) {
    local n = spaceBefore(s, j);
    if (n == 0) break;
    j -= n;
  }
  return s.slice(i, j);
};

local set = function(names) {
  local out = {};
  foreach (k in names) out[k] <- true;
  return out;
};

local isAlpha = function(c) { return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z'); };
local isDigit = function(c) { return c >= '0' && c <= '9'; };

// isHeaderKey is the admitted header key grammar: a letter, then letters,
// digits and hyphens.
local isHeaderKey = function(s) {
  if (s.len() == 0 || !isAlpha(s[0])) return false;
  for (local i = 1; i < s.len(); i++) {
    local c = s[i];
    if (!(isAlpha(c) || isDigit(c) || c == '-')) return false;
  }
  return true;
};

// ---- Record -------------------------------------------------------------------

// Record owns the wire and the decoded Card. The wire is the record's own
// blob, never the caller's (docs/SPEC-TABLES.md §6.2).
local Record = class {
  wire = null;
  root = null;
  report = null;
  constructor(wire, root, report) {
    this.wire = wire;
    this.root = root;
    this.report = report;
  }
  // Wire answers a copy of the record's wire bytes.
  function Wire() { return copyBlob(this.wire); }
};
M.Record <- Record;

// ---- the checked-write helper (tables/cards/cards.go checkCard) -----------------

local listBound = function(name, n) {
  return refuse(format("cards: %s has %d entries, past the %d-entry ceiling (docs/SPEC-TABLES.md §2.9)",
    name, n, M.MaxListEntries), "list-bound " + name + " 512");
};

local checkStrings = function(list, name) {
  if (list.len() > M.MaxListEntries) return listBound(name, list.len());
  foreach (e in list) {
    if (!wellFormed(e.S))
      return refuse("cards: " + name + " has an element that is not well-formed UTF-8 without NUL", "malformed");
  }
  return null;
};

local RESERVED_HEADER_KEYS = set(["RESULT", "tier", "REPO", "BASE", "KIND", "PRIORITY", "PATHS", "NEW",
  "SHARED", "DEPENDS-ON", "TEST", "DONE-WHEN", "DEADLINE", "SOURCE"]);

// checkExtras enforces the extras rules before any write publishes: the
// 512-entry ceiling, the admitted header key grammar, no CR or LF in a key or
// value, well-formed keys and values, and no key duplicating a mapped header
// name (HEADER MAPPING, docs/SPEC-TABLES.md §2.9).
local checkExtras = function(list) {
  if (list.len() > M.MaxListEntries) return listBound("extra", list.len());
  foreach (e in list) {
    if (!wellFormed(e.Key)) return refuse("cards: extra has a key that is not well-formed UTF-8 without NUL", "malformed");
    if (!isHeaderKey(e.Key)) return refuse("cards: extra key \"" + e.Key + "\" is not an admitted header key", "malformed");
    if (hasCRLF(e.Key)) return refuse("cards: extra has a key with a CR or LF", "malformed");
    if (e.Key in RESERVED_HEADER_KEYS)
      return refuse("cards: extra key \"" + e.Key + "\" duplicates a mapped header key", "malformed");
    if (e.Value != null) {
      if (!wellFormed(e.Value))
        return refuse("cards: extra has a value that is not well-formed UTF-8 without NUL", "malformed");
      if (hasCRLF(e.Value)) return refuse("cards: extra has a value with a CR or LF", "malformed");
    }
  }
  return null;
};

local CONSUMER_STRINGS = ["Kind", "Who", "Route", "Model", "Outcome", "Line"];

local checkConsumers = function(list) {
  if (list.len() > M.MaxListEntries) return listBound("consumers", list.len());
  foreach (e in list) {
    foreach (name in CONSUMER_STRINGS) {
      if (!wellFormed(e[name]))
        return refuse("cards: consumers has a " + name.tolower() + " that is not well-formed UTF-8 without NUL", "malformed");
    }
  }
  return null;
};

// checkCard enforces the customer bounds and the well-formedness of the opaque
// body and every collection element before bytes are published
// (docs/SPEC-TABLES.md §2.5, §2.9).
local checkCard = function(root) {
  if (root == null) return refuse("cards: null card", "malformed");
  if (root.Body != null) {
    if (root.Body.len() > M.MaxBodyBytes)
      return refuse(format("cards: body is %d bytes, past the %d-byte ceiling", root.Body.len(), M.MaxBodyBytes),
        "body-bound 65536");
    if (!wellFormed(root.Body)) return refuse("cards: body is not well-formed UTF-8 without NUL", "malformed");
  }
  local err = checkStrings(root.Paths, "paths");
  if (err == null) err = checkStrings(root.New, "_new");
  if (err == null) err = checkStrings(root.Shared, "shared");
  if (err == null) err = checkStrings(root.Needs, "needs");
  if (err == null) err = checkExtras(root.Extra);
  if (err == null) err = checkConsumers(root.Consumers);
  return err;
};

// save is the generated strict save behind the facade's one refusal.
local save = function(root) {
  local wire = T.CardSave(root);
  if (wire == null) return refuse("cards: the save refused the card", "malformed");
  return wire;
};

// ---- the owning facade (tables/cards/cards.go) -----------------------------------

// readWire is the one strict read every facade read shares: the whole card or
// a refusal (docs/SPEC-TABLES.md §6.5), and the runtime report it filled.
local readWire = function(wire) {
  local r = {};
  if (wire == null) return [refuse("cards: wire is not a card", "malformed"), r];
  local root = T.CardLoad(wire, r);
  if (root == null) return [refuse("cards: wire is not a card", "malformed", reportOf(r)), r];
  return [root, r];
};

// Load reads wire into a Record (docs/SPEC-TABLES.md §6.2). Every refusal of
// the strict read and every customer bound is a CardError; the counters an
// accepted read moves ride in record.report. A copy of the ORIGINAL wire is
// kept, never rewritten, so an old input is preserved byte for byte.
M.Load <- function(wire) {
  local got = readWire(wire);
  if (got[0] instanceof CardError) return got[0];
  local report = reportOf(got[1]);
  local cerr = checkCard(got[0]);
  if (cerr != null) {
    cerr.report = report;
    return cerr;
  }
  return Record(copyBlob(wire), got[0], report);
};

// FromJSON builds a Record from JSON text, a string or a blob
// (docs/SPEC-TABLES.md §16). The strict read refuses what it cannot carry
// whole; an unknown key is refused here too, as are the customer bounds and
// any body or element that is not well-formed UTF-8 without NUL. Bytes are
// published only on complete success.
M.FromJSON <- function(text) {
  local r = {};
  local root = T.CardFromJson(text, r);
  if (root == null) return refuse("cards: JSON is not a card", "malformed");
  if (counter(r, "unknown") > 0)
    return refuse(format("cards: JSON has %d unknown key(s): refused, never dropped", counter(r, "unknown")), "malformed");
  local cerr = checkCard(root);
  if (cerr != null) return cerr;
  local wire = save(root);
  if (wire instanceof CardError) return wire;
  return Record(wire, root, Report());
};

// ToJSON answers the record's known-field JSON text. It is the known-field
// view, not a lossless archival rewrite (docs/SPEC-TABLES.md §16).
M.ToJSON <- function(record) {
  if (record == null || record.root == null) return refuse("cards: null record", "malformed");
  local text = T.CardToJson(record.root);
  if (text == null) return refuse("cards: the card has no JSON spelling", "malformed", record.report);
  return text;
};

// ---- parse (tables/cards/parse.go) -------------------------------------------------

local STRANDED_KEYS = set(["KIND", "PATHS", "TEST", "LEGS", "SOURCE", "START", "STOP", "DEPENDS-ON",
  "PRIORITY", "NEW", "SHARED", "REPO", "BASE", "tier", "RESULT"]);

local TIERS = set(["flash", "pro", "heavy", "frontier"]);
local PRIORITIES = set(["normal", "high", "critical", "blocker"]);
local COLUMNS = set(["waiting", "ready", "working", "review", "merging", "landed", "plan", "check"]);

// splitLines answers every physical line with its content and its terminator
// end offset (the offset just past the LF). A trailing LF does not invent a
// final empty line.
local splitLines = function(text) {
  local refs = [];
  local i = 0, num = 1;
  while (i < text.len()) {
    local j = text.find("\n", i);
    local lineEnd = text.len(), termEnd = text.len();
    if (j != null) { lineEnd = j; termEnd = j + 1; }
    local content = text.slice(i, lineEnd);
    if (content.len() > 0 && content[content.len() - 1] == '\r') content = content.slice(0, content.len() - 1);
    refs.append({ content = content, termEnd = termEnd, num = num });
    num++;
    i = termEnd;
  }
  return refs;
};

// matchHeader recognizes a header key line: one letter then letters, digits
// and hyphens, a colon, optional ASCII whitespace and the rest as the value.
// Prose, blank and any line with leading whitespace is not one.
local matchHeader = function(content) {
  if (content.len() == 0 || !isAlpha(content[0])) return null;
  local i = 1;
  while (i < content.len() && (isAlpha(content[i]) || isDigit(content[i]) || content[i] == '-')) i++;
  if (i >= content.len() || content[i] != ':') return null;
  local key = content.slice(0, i);
  i++;
  while (i < content.len()) {
    local c = content[i];
    if (c != ' ' && c != '\t' && c != '\n' && c != '\f' && c != '\r') break;
    i++;
  }
  return [key, content.slice(i)];
};

local isBlank = function(content) {
  foreach (c in content) if (c != ' ' && c != '\t' && c != '\r') return false;
  return true;
};

local checkStrBound = function(name, value, bound) {
  if (value.len() > bound)
    return refuse(format("cards: %s is %d bytes, past the %d-byte bound", name, value.len(), bound), "malformed");
  return null;
};

local isHex = function(s) {
  if (s.len() == 0) return false;
  foreach (c in s) {
    if (!(isDigit(c) || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F'))) return false;
  }
  return true;
};

local lastIndex = function(s, needle) {
  local at = null, from = 0;
  while (true) {
    local i = s.find(needle, from);
    if (i == null) return at;
    at = i;
    from = i + 1;
  }
};

// applyResult parses the RESULT value "id sha=<sha> tier: <tier>"
// (docs/SPEC-TABLES.md §2, §4).
local applyResult = function(p, value) {
  local i = lastIndex(value, " tier:");
  if (i == null) return refuse("cards: RESULT line has no \" tier:\"", "malformed");
  local tierStr = trim(value.slice(i + 6));
  local rest = value.slice(0, i);
  local j = lastIndex(rest, " sha=");
  if (j == null) return refuse("cards: RESULT line has no \" sha=\"", "malformed");
  local idStr = trim(rest.slice(0, j));
  local shaStr = rest.slice(j + 5);
  local err = checkStrBound("id", idStr, 64);
  if (err != null) return err;
  if (!isHex(shaStr) || (shaStr.len() != 12 && shaStr.len() != 40))
    return refuse("cards: RESULT sha \"" + shaStr + "\" is not 12 or 40 hex digits", "malformed");
  if (!(tierStr in TIERS)) return refuse("cards: RESULT tier: unknown tier \"" + tierStr + "\"", "malformed");
  p.id = idStr;
  p.sha = shaStr;
  p.tier = tierStr;
  return null;
};

local assignStr = function(p, field, value, bound, name, num) {
  local err = checkStrBound(name, value, bound);
  if (err != null) return refuse(format("cards: %s on line %d: %s", name, num, err.message), "malformed");
  p[field] = value;
  return null;
};

// splitListChecked splits a comma-separated list, trims elements, drops empty
// ones, and enforces the element bound and the entry ceiling
// (docs/SPEC-TABLES.md §2.9). A whole value of "-" or empty is an empty list;
// PATHS alone also takes the customer sentinel "none".
local splitListChecked = function(p, field, value, bound, noneSentinel, name, num) {
  local v = trim(value);
  if (v == "" || v == "-" || (noneSentinel && v == "none")) {
    p[field] = [];
    return null;
  }
  local out = [];
  local from = 0;
  while (true) {
    local k = v.find(",", from);
    local part = trim(v.slice(from, k == null ? v.len() : k));
    if (part != "") {
      local err = checkStrBound(name + " element", part, bound);
      if (err != null) return refuse(format("cards: %s on line %d: %s", name, num, err.message), "malformed");
      out.append(part);
    }
    if (k == null) break;
    from = k + 1;
  }
  if (out.len() > M.MaxListEntries)
    return refuse(format("cards: %s on line %d has %d entries, past the %d-entry ceiling", name, num, out.len(),
      M.MaxListEntries), "malformed");
  p[field] = out;
  return null;
};

// parseDeadline answers the minutes of "finish within N minutes.", or a
// refusal naming the line.
local parseDeadline = function(value, num) {
  local v = trim(value);
  local pre = "finish within ", post = " minutes.";
  local shapeErr = refuse(format("cards: DEADLINE on line %d: not of the form \"finish within N minutes.\"", num),
    "malformed");
  if (v.len() < pre.len() + post.len() + 1) return shapeErr;
  if (v.slice(0, pre.len()) != pre || v.slice(v.len() - post.len()) != post) return shapeErr;
  local digits = v.slice(pre.len(), v.len() - post.len());
  foreach (c in digits) if (!isDigit(c)) return shapeErr;
  if (digits.len() > 18) return refuse(format("cards: DEADLINE on line %d: minutes out of range", num), "malformed");
  local n = digits.tointeger();
  if (n > 100000) return refuse(format("cards: DEADLINE on line %d: minutes out of range", num), "malformed");
  return n;
};

local assignMapped = function(p, key, value, num) {
  switch (key) {
    case "REPO": return assignStr(p, "repo", value, 96, "REPO", num);
    case "BASE": return assignStr(p, "repoBase", value, 96, "BASE", num);
    case "KIND": return assignStr(p, "kind", value, 24, "KIND", num);
    case "PRIORITY": {
      local pr = trim(value);
      if (!(pr in PRIORITIES))
        return refuse(format("cards: PRIORITY on line %d: unknown priority \"%s\"", num, pr), "malformed");
      p.priority = pr;
      return null;
    }
    case "PATHS": return splitListChecked(p, "paths", value, 192, true, "PATHS", num);
    case "NEW": return splitListChecked(p, "news", value, 192, false, "NEW", num);
    case "SHARED": return splitListChecked(p, "shared", value, 192, false, "SHARED", num);
    case "DEPENDS-ON": return splitListChecked(p, "needs", value, 64, false, "DEPENDS-ON", num);
    case "TEST": return assignStr(p, "test", value, 192, "TEST", num);
    case "DONE-WHEN": p.doneWhen = value; return null;
    case "DEADLINE": {
      local n = parseDeadline(value, num);
      if (n instanceof CardError) return n;
      p.deadline = n;
      return null;
    }
    case "SOURCE": p.source = value; return null;
  }
  return null;
};

// applyHeader dispatches one header line to its field, an agreeing tier, or an
// extra, refusing duplicates and CR/LF inside any single-line value.
local applyHeader = function(p, key, value, num, seen) {
  if (hasCRLF(value)) return refuse(format("cards: %s on line %d has a CR or LF in its value", key, num), "malformed");
  if (key == "tier") {
    if ("tier" in seen)
      return refuse(format("cards: duplicate header key \"%s\" on lines %d and %d", key, seen.tier, num), "malformed");
    seen.tier <- num;
    local t = trim(value);
    if (!(t in TIERS)) return refuse(format("cards: tier on line %d: unknown tier \"%s\"", num, t), "malformed");
    if (t != p.tier) return refuse(format("cards: tier on line %d disagrees with RESULT", num), "malformed");
    return null;
  }
  if (key == "RESULT")
    return refuse(format("cards: duplicate header key \"%s\" on lines %d and %d", key, seen.RESULT, num), "malformed");
  if (key in RESERVED_HEADER_KEYS) {
    if (key in seen)
      return refuse(format("cards: duplicate header key \"%s\" on lines %d and %d", key, seen[key], num), "malformed");
    seen[key] <- num;
    return assignMapped(p, key, value, num);
  }
  local err = checkStrBound("extra key", key, 32);
  if (err != null) return refuse(format("cards: line %d: %s", num, err.message), "malformed");
  p.extras.append([key, value]);
  return null;
};

local pathRows = function(list) {
  local out = [];
  foreach (s in list) {
    local e = T.PathText();
    e.S = s;
    out.append(e);
  }
  return out;
};

local idRows = function(list) {
  local out = [];
  foreach (s in list) {
    local e = T.CardID();
    e.S = s;
    out.append(e);
  }
  return out;
};

// build publishes the parsed card through the generated save and the checked
// write (docs/SPEC-TABLES.md §6.2). Bytes are published only on complete
// success.
local build = function(p, body) {
  local card = T.Card();
  card.Id = p.id;
  card.Sha = p.sha;
  card.Tier = p.tier;
  card.Repo = p.repo;
  card.Base = p.repoBase;
  card.Kind = p.kind;
  card.Priority = p.priority;
  card.Paths = pathRows(p.paths);
  card.New = pathRows(p.news);
  card.Shared = pathRows(p.shared);
  card.Needs = idRows(p.needs);
  card.Test = p.test;
  if (p.doneWhen != "") card.Done = p.doneWhen;
  card.DeadlineMinutes = p.deadline;
  if (p.source != "") card.Source = p.source;
  foreach (kv in p.extras) {
    local e = T.KeyValue();
    e.Key = kv[0];
    e.Value = kv[1];
    card.Extra.append(e);
  }
  if (body.len() > 0) card.Body = body;
  local cerr = checkCard(card);
  if (cerr != null) return cerr;
  local wire = save(card);
  if (wire instanceof CardError) return wire;
  return Record(wire, card, Report());
};

// Parse reads admitted brief text (a string or a blob) and publishes a typed
// Record only on complete checked success (docs/SPEC-TABLES.md §2, §6.2). The
// body is the exact suffix immediately after the last header line's
// terminator; header layout may canonicalize, body bytes never do.
M.Parse <- function(text) {
  if (typeof text != "string") text = toStr(text, 0, text.len());
  if (!utf8Valid(text)) return refuse("cards: brief is not valid UTF-8", "malformed");
  if (hasNUL(text)) return refuse("cards: brief contains a NUL byte", "malformed");
  local lines = splitLines(text);
  if (lines.len() == 0) return refuse("cards: brief has no RESULT line", "malformed");
  local first = matchHeader(lines[0].content);
  if (first == null || first[0] != "RESULT") return refuse("cards: line 1 is not a RESULT line", "malformed");
  local p = {
    id = "", sha = "", tier = null, repo = "", repoBase = "", kind = "", priority = null,
    paths = [], news = [], shared = [], needs = [], test = "", doneWhen = "", source = "",
    deadline = 0, extras = [],
  };
  local err = applyResult(p, first[1]);
  if (err != null) return err;
  local lastHeaderEnd = lines[0].termEnd;
  local seen = { RESULT = 1 };
  local prose = false;
  for (local k = 1; k < lines.len(); k++) {
    local ln = lines[k];
    if (isBlank(ln.content)) continue;
    local h = matchHeader(ln.content);
    if (h == null) {
      prose = true;
    } else if (prose) {
      if (h[0] in STRANDED_KEYS)
        return refuse(format("cards: stranded header key \"%s\" on line %d", h[0], ln.num), "stranded-key " + h[0]);
    } else {
      local aerr = applyHeader(p, h[0], h[1], ln.num, seen);
      if (aerr != null) return aerr;
      lastHeaderEnd = ln.termEnd;
    }
  }
  return build(p, text.slice(lastHeaderEnd));
};

// ---- render (tables/cards/render.go) --------------------------------------------------

local joined = function(list) {
  local out = "";
  foreach (i, e in list) out += (i > 0 ? ", " : "") + e.S;
  return out;
};

local renderCard = function(root) {
  if (root.Tier == null || !(root.Tier in TIERS)) return null;
  local out = "RESULT: " + root.Id + " sha=" + root.Sha + " tier: " + root.Tier + "\n";
  local line = function(key, value) { return key + ": " + value + "\n"; };
  if (root.Repo != "") out += line("REPO", root.Repo);
  if (root.Base != "") out += line("BASE", root.Base);
  if (root.Kind != "") out += line("KIND", root.Kind);
  if (root.Priority != null) {
    if (!(root.Priority in PRIORITIES)) return null;
    out += line("PRIORITY", root.Priority);
  }
  if (root.Paths.len() > 0) out += line("PATHS", joined(root.Paths));
  if (root.New.len() > 0) out += line("NEW", joined(root.New));
  if (root.Shared.len() > 0) out += line("SHARED", joined(root.Shared));
  if (root.Needs.len() > 0) out += line("DEPENDS-ON", joined(root.Needs));
  if (root.Test != "") out += line("TEST", root.Test);
  if (root.Done != null) out += line("DONE-WHEN", root.Done);
  if (root.DeadlineMinutes != 0) out += "DEADLINE: finish within " + root.DeadlineMinutes + " minutes.\n";
  if (root.Source != null) out += line("SOURCE", root.Source);
  foreach (e in root.Extra) out += line(e.Key, e.Value == null ? "" : e.Value);
  if (root.Body != null) out += root.Body;
  return out;
};

// Render reads the record's wire into a fresh strict load and answers its
// informational text (docs/SPEC-TABLES.md §6.2, §16).
M.Render <- function(record) {
  if (record == null || record.wire == null) return refuse("cards: null record", "malformed");
  local got = readWire(record.wire);
  if (got[0] instanceof CardError) return got[0];
  local text = renderCard(got[0]);
  if (text == null) return refuse("cards: the card has no text spelling", "malformed", reportOf(got[1]));
  return text;
};

// ---- upgrade and strict read (tables/cards/upgrade.go) --------------------------------

// Upgraded is an accepted upgrade: the rewritten wire and its read report.
local Upgraded = class {
  wire = null;
  report = null;
  constructor(wire, report) {
    this.wire = wire;
    this.report = report;
  }
};
M.Upgraded <- Upgraded;

// upgrade is the retention seam Upgrade fixes the two customer capacities
// through; the refusal probes drive smaller caller-owned storage through the
// same path so a tiny capacity names retain-lost while the input is
// preserved. The strict read decides the card first (a damaged, mismatched or
// out-of-range wire is refused whole); the retaining read then captures what
// the declaration cannot name, and the save places it back (§6.6).
local upgrade = function(wire, retainBytes, retainIDs) {
  local got = readWire(wire);
  if (got[0] instanceof CardError) {
    got[0].message = "cards: upgrade: wire is not a card";
    return got[0];
  }
  local r = fresh(got[1]);
  local store = { capacity = retainBytes, idCapacity = retainIDs };
  local root = T.CardLoadRetain(wire, store, r);
  if (root == null) return refuse("cards: upgrade: the retaining read refused the card", "malformed", reportOf(r));
  local cerr = checkCard(root);
  if (cerr != null) {
    cerr.report = reportOf(r);
    return cerr;
  }
  local size = T.CardMeasureRetain(root, store);
  if (size < 0) return refuse("cards: upgrade: measure refused the retained card", "malformed", reportOf(r));
  local out = blob(size);
  local n = T.CardSaveRetain(root, store, out, r);
  if (n != size) return refuse("cards: upgrade: save did not fill the measured wire", "malformed", reportOf(r));
  local report = reportOf(r);
  if (report.RetainLost > 0)
    return refuse(format("cards: upgrade: %d retained field(s) lost, refusing to replace", report.RetainLost),
      "retain-lost", report);
  return Upgraded(out, report);
};
M.upgrade <- upgrade;

// Upgrade rewrites wire through the retaining path so every field the current
// declaration cannot name is preserved at the end of its own body (§6.6). It
// answers an Upgraded, or a CardError when any value would be lost
// (retain-lost) or the wire is refused (malformed); the input is never touched
// (tla/CardVersioning.tla: NoFieldLost, BodyUnchanged, RefusalKeepsOriginal,
// ShippedUpgradeSucceeds).
M.Upgrade <- function(wire) { return upgrade(wire, M.MaxRetainBytes, M.MaxRetainIDs); };

// ReadStrict reads wire and refuses any data the current declaration cannot
// support: an unknown field (unknown-present) and every refusal of the strict
// load (docs/SPEC-TABLES.md §4).
M.ReadStrict <- function(wire) {
  local record = M.Load(wire);
  if (record instanceof CardError) return record;
  if (record.report.Unknown > 0)
    return refuse(format("cards: strict: %d unknown field(s), unsupported data", record.report.Unknown),
      "unknown-present", record.report);
  return record;
};

// ---- the task hash (tables/cards/edit.go) -------------------------------------------

// SHA-256 (FIPS 180-4) in Squirrel's 64-bit integers, every word masked to
// 32 bits: the standard library has no digest and the hash is the contract.
local K = [
  0x428a2f98, 0x71374491, 0xb5c0fbcf, 0xe9b5dba5, 0x3956c25b, 0x59f111f1, 0x923f82a4, 0xab1c5ed5,
  0xd807aa98, 0x12835b01, 0x243185be, 0x550c7dc3, 0x72be5d74, 0x80deb1fe, 0x9bdc06a7, 0xc19bf174,
  0xe49b69c1, 0xefbe4786, 0x0fc19dc6, 0x240ca1cc, 0x2de92c6f, 0x4a7484aa, 0x5cb0a9dc, 0x76f988da,
  0x983e5152, 0xa831c66d, 0xb00327c8, 0xbf597fc7, 0xc6e00bf3, 0xd5a79147, 0x06ca6351, 0x14292967,
  0x27b70a85, 0x2e1b2138, 0x4d2c6dfc, 0x53380d13, 0x650a7354, 0x766a0abb, 0x81c2c92e, 0x92722c85,
  0xa2bfe8a1, 0xa81a664b, 0xc24b8b70, 0xc76c51a3, 0xd192e819, 0xd6990624, 0xf40e3585, 0x106aa070,
  0x19a4c116, 0x1e376c08, 0x2748774c, 0x34b0bcb5, 0x391c0cb3, 0x4ed8aa4a, 0x5b9cca4f, 0x682e6ff3,
  0x748f82ee, 0x78a5636f, 0x84c87814, 0x8cc70208, 0x90befffa, 0xa4506ceb, 0xbef9a3f7, 0xc67178f2,
];

local MASK = 0xffffffff;

local rotr = function(x, n) { return ((x >>> n) | (x << (32 - n))) & MASK; };

local HEX = "0123456789abcdef";

local hex32 = function(x) {
  local s = "";
  for (local sh = 28; sh >= 0; sh -= 4) s += HEX[(x >>> sh) & 0xf].tochar();
  return s;
};

// sha256hex answers the lowercase hex digest of a blob or string.
local sha256hex = function(message) {
  local h = [0x6a09e667, 0xbb67ae85, 0x3c6ef372, 0xa54ff53a, 0x510e527f, 0x9b05688c, 0x1f83d9ab, 0x5be0cd19];
  local n = message.len();
  local total = ((n + 9 + 63) / 64) * 64;
  local padded = blob(total);
  for (local i = 0; i < n; i++) padded[i] = message[i] & 0xff;
  padded[n] = 0x80;
  local bits = n * 8;
  for (local i = 0; i < 8; i++) padded[total - 1 - i] = (bits >>> (8 * i)) & 0xff;
  local w = array(64, 0);
  for (local chunk = 0; chunk < total; chunk += 64) {
    for (local i = 0; i < 16; i++) {
      local at = chunk + i * 4;
      w[i] = (padded[at] << 24) | (padded[at + 1] << 16) | (padded[at + 2] << 8) | padded[at + 3];
    }
    for (local i = 16; i < 64; i++) {
      local s0 = rotr(w[i - 15], 7) ^ rotr(w[i - 15], 18) ^ (w[i - 15] >>> 3);
      local s1 = rotr(w[i - 2], 17) ^ rotr(w[i - 2], 19) ^ (w[i - 2] >>> 10);
      w[i] = (w[i - 16] + s0 + w[i - 7] + s1) & MASK;
    }
    local a = h[0], b = h[1], c = h[2], d = h[3], e = h[4], f = h[5], g = h[6], hh = h[7];
    for (local i = 0; i < 64; i++) {
      local S1 = rotr(e, 6) ^ rotr(e, 11) ^ rotr(e, 25);
      local ch = (e & f) ^ ((~e & MASK) & g);
      local t1 = (hh + S1 + ch + K[i] + w[i]) & MASK;
      local S0 = rotr(a, 2) ^ rotr(a, 13) ^ rotr(a, 22);
      local maj = (a & b) ^ (a & c) ^ (b & c);
      local t2 = (S0 + maj) & MASK;
      hh = g; g = f; f = e; e = (d + t1) & MASK; d = c; c = b; b = a; a = (t1 + t2) & MASK;
    }
    h[0] = (h[0] + a) & MASK; h[1] = (h[1] + b) & MASK; h[2] = (h[2] + c) & MASK; h[3] = (h[3] + d) & MASK;
    h[4] = (h[4] + e) & MASK; h[5] = (h[5] + f) & MASK; h[6] = (h[6] + g) & MASK; h[7] = (h[7] + hh) & MASK;
  }
  local out = "";
  foreach (x in h) out += hex32(x);
  return out;
};
M.sha256hex <- sha256hex;

// refuseLossy names the read events that make a record unsafe to hash or
// rewrite: an unknown field, or a retained field lost (docs/SPEC-TABLES.md
// §4, §6.6). Damage, a kind mismatch and an out-of-range value never reach
// here: the strict read refused them whole.
local refuseLossy = function(report) {
  if (report.Unknown > 0 || report.RetainLost > 0)
    return refuse(format("cards: record has loss events (unknown=%d retainLost=%d): refused, never hashed or rewritten",
      report.Unknown, report.RetainLost), "unknown-present", report);
  return null;
};

local putU64 = function(out, n) {
  for (local i = 0; i < 8; i++) out.writen((n >>> (8 * i)) & 0xff, 'b');
};

local frame = function(out, text) {
  putU64(out, text.len());
  for (local i = 0; i < text.len(); i++) out.writen(text[i] & 0xff, 'b');
};

local framePaths = function(out, list) {
  putU64(out, list.len());
  foreach (e in list) frame(out, e.S);
};

// TaskHash answers the hex digest of a record's TASK: body, paths, new, shared,
// test and done in that order, each framed as an unsigned 64-bit little-endian
// byte length then its bytes, each list as a count then its length-prefixed
// elements; needs, tier, priority and machine fields are excluded
// (docs/SPEC-TABLES.md §5; tla/CardHops.tla: TaskHashStable). A null or lossy
// record is refused, never hashed.
M.TaskHash <- function(record) {
  if (record == null || record.root == null) return refuse("cards: null record", "malformed");
  local lossy = refuseLossy(record.report);
  if (lossy != null) return lossy;
  local root = record.root;
  local m = blob();
  frame(m, root.Body == null ? "" : root.Body);
  framePaths(m, root.Paths);
  framePaths(m, root.New);
  framePaths(m, root.Shared);
  frame(m, root.Test == null ? "" : root.Test);
  frame(m, root.Done == null ? "" : root.Done);
  return sha256hex(m);
};

// ---- the field edits (tables/cards/edit.go) ---------------------------------------------

local validateStrings = function(name, values, bound) {
  foreach (s in values) {
    if (s.len() > bound)
      return refuse(format("cards: %s element is %d bytes, past its %d-byte bound", name, s.len(), bound), "malformed");
    if (!wellFormed(s))
      return refuse("cards: " + name + " has an element that is not well-formed UTF-8 without NUL", "malformed");
  }
  return null;
};

// edit is the shared atomic rewrite: check the current record is safe, reload
// it strictly into a fresh independent decode, apply mutate to the root,
// validate the whole rebuilt card, save it, and only on complete success swap
// the record's owned wire (docs/SPEC-TABLES.md §6.2). Any refusal leaves the
// original wire untouched (tla/CardHops.tla: EditDropsUnknown, BodyUnchanged).
local edit = function(record, mutate) {
  if (record == null || record.root == null) return refuse("cards: null record", "malformed");
  local lossy = refuseLossy(record.report);
  if (lossy != null) return lossy;
  local got = readWire(record.wire);
  if (got[0] instanceof CardError) return refuse("cards: reload into the fresh decode was refused", "malformed");
  local report = reportOf(got[1]);
  lossy = refuseLossy(report);
  if (lossy != null) return lossy;
  local root = got[0];
  mutate(root);
  local cerr = checkCard(root);
  if (cerr != null) return cerr;
  local wire = save(root);
  if (wire instanceof CardError) return wire;
  record.wire = wire;
  record.root = root;
  record.report = report;
  return true;
};

local setList = function(field, member, bound, rowsOf) {
  return function(record, values) {
    local err = validateStrings(field, values, bound);
    if (err != null) return err;
    if (values.len() > M.MaxListEntries) return listBound(field, values.len());
    return edit(record, function(root) { root[member] = rowsOf(values); });
  };
};

M.SetNeeds <- setList("needs", "Needs", 64, idRows);
M.SetPaths <- setList("paths", "Paths", 192, pathRows);

local setVariant = function(field, member, allowed) {
  return function(record, value) {
    if (typeof value != "string" || !(value in allowed))
      return refuse(format("cards: %s %s is not a declared variant (docs/SPEC-TABLES.md §8)", field, "" + value),
        "malformed");
    return edit(record, function(root) { root[member] = value; });
  };
};

M.SetTier <- setVariant("tier", "Tier", TIERS);
M.SetPriority <- setVariant("priority", "Priority", PRIORITIES);
M.SetColumn <- setVariant("column", "Column", COLUMNS);

M.SetHolder <- function(record, holder) {
  local err = validateStrings("holder", [holder], 32);
  if (err != null) return err;
  return edit(record, function(root) { root.Holder = holder; });
};

return M;
