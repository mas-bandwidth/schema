// cards_test.nut — the Squirrel card facade's unit tests (tables/cards/cards_test.go,
// parse_test.go, edit_test.go; the Lua twin is tables/cards/lua/cards_test.lua).
// Each test reads committed files and in-memory values only, with no clock,
// no socket and no process: the driver and conformance targets that start
// processes are the functional tier. Run from the repository root with the
// generated Card unit built (make cards-squirrel-test).
//
// sq EXITS 0 ON A RUNTIME ERROR (docs/PORT-SQUIRREL.md, THE HARNESS), so a
// failure is reported on stderr, and the make target fails a run that writes
// anything there whatever its exit status.

local GENERATED = ::getenv("CARDS_SQUIRREL_GENERATED");
if (GENERATED == null || GENERATED == "") GENERATED = "build/tables-generated-squirrel/cards";

local T = ::dofile(GENERATED + "/schema_module.nut", true).require("CardsTables");
local cards = ::loadfile("tables/cards/squirrel/cards.nut", true).call(::getroottable(), T);

local passed = 0, failed = 0;

local test = function(name, body) {
  try {
    body();
    passed++;
  } catch (e) {
    failed++;
    ::error("FAIL " + name + ": " + e + "\n");
  }
};

local read = function(path) {
  local f = ::file(path, "rb");
  local data = f.len() > 0 ? f.readblob(f.len()) : ::blob(0);
  f.close();
  return data;
};

local CASES = "tables/cards/conformance/cases/";
local EXPECTED = "tables/cards/conformance/expected/";

local show = function(v) {
  if (v instanceof ::blob) return "<blob " + v.len() + ">";
  return "" + v;
};

local eq = function(got, want, what) {
  local same = (got instanceof ::blob && want instanceof ::blob) ? cards.sameBytes(got, want) : got == want;
  if (!same) throw what + ": got " + show(got) + ", want " + show(want);
};

local ok = function(cond, what) { if (!cond) throw what; };

local accepted = function(v, what) {
  if (cards.refused(v)) throw what + ": refused (" + v.cls + "): " + v.message;
  return v;
};

local refused = function(v, cls, what) {
  if (!cards.refused(v)) throw what + ": accepted, want refusal " + cls;
  eq(v.cls, cls, what + " class");
};

local text = function(path) { return cards.toStr(read(path)); };

local chomp = function(s) { return (s.len() > 0 && s[s.len() - 1] == '\n') ? s.slice(0, s.len() - 1) : s; };

local rep = function(s, n) {
  local out = "";
  for (local i = 0; i < n; i++) out += s;
  return out;
};

local jsonCard = function(fields) { return "{\"id\":\"x\"" + fields + "}"; };

test("sha256 matches the FIPS 180-4 vectors", function() {
  eq(cards.sha256hex(""), "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855", "empty");
  eq(cards.sha256hex("abc"), "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad", "abc");
  eq(cards.sha256hex("abcdbcdecdefdefgefghfghighijhijkijkljklmklmnlmnomnopnopq"),
    "248d6a61d20638b8e5c026930c3e6039a33ce45964ff2167f6ecedd419db06c1", "two blocks");
});

test("TestCardRoundTripsThePopulatedFixture", function() {
  local wire = read(CASES + "card-populated.wire");
  local json = text(CASES + "card-populated.json");
  local record = accepted(cards.Load(wire), "Load");
  eq(record.report.Unknown, 0, "unknown");
  eq(cards.ToJSON(record), json, "ToJSON reproduces card-populated.json");
  local again = accepted(cards.FromJSON(json), "FromJSON");
  eq(again.Wire(), wire, "FromJSON reproduces card-populated.wire");
  // the record owns its wire: changing the caller's blob, or the copy Wire()
  // hands out, leaves it alone
  wire[0] = wire[0] ^ 0xff;
  local out = record.Wire();
  out[1] = out[1] ^ 0xff;
  eq(record.Wire(), read(CASES + "card-populated.wire"), "Wire after the caller's blobs changed");
});

test("TestCardBoundsRefuseBeforePublishing", function() {
  local body = function(n) { return jsonCard(",\"body\":\"" + rep("y", n) + "\""); };
  accepted(cards.FromJSON(body(cards.MaxBodyBytes)), "exactly 64 KiB body");
  local err = cards.FromJSON(body(cards.MaxBodyBytes + 1));
  refused(err, "body-bound 65536", "64 KiB + 1 body");
  ok(err.message.find("65537") != null && err.message.find("65536") != null, err.message);

  local collections = [["paths", "{\"s\":\"p\"}"], ["_new", "{\"s\":\"n\"}"], ["shared", "{\"s\":\"s\"}"],
    ["needs", "{\"s\":\"d\"}"], ["consumers", "{\"kind\":\"k\"}"]];
  foreach (c in collections) {
    local field = c[0], element = c[1];
    local list = function(n) { return jsonCard(",\"" + field + "\":[" + rep(element + ",", n - 1) + element + "]"); };
    accepted(cards.FromJSON(list(cards.MaxListEntries)), "exactly 512 entries in " + field);
    local e = cards.FromJSON(list(cards.MaxListEntries + 1));
    refused(e, "list-bound " + field + " 512", "513 entries in " + field);
    ok(e.message.find("513") != null, e.message);
  }
});

test("TestFromJSONRefusesWhatItCannotCarryWhole", function() {
  // an unknown key, a kind mismatch, an out-of-range value and trailing
  // rubbish are each refused, never dropped or clamped
  refused(cards.FromJSON(jsonCard(",\"nope\":1")), "malformed", "an unknown key");
  refused(cards.FromJSON(jsonCard(",\"epoch\":\"seven\"")), "malformed", "a kind mismatch");
  refused(cards.FromJSON(jsonCard(",\"deadline_minutes\":100001")), "malformed", "past the declared range");
  refused(cards.FromJSON(jsonCard("") + "x"), "malformed", "trailing rubbish");
});

test("TestParseRenderMatchesTheCorpus", function() {
  local record = accepted(cards.Parse(read(CASES + "normalized-valid.md")), "normalized-valid");
  eq(record.Wire(), read(EXPECTED + "normalized-valid.wire"), "parsed wire");

  refused(cards.Parse(read(CASES + "rejected-legacy.md")), chomp(text(EXPECTED + "rejected-legacy.refused")),
    "rejected-legacy");

  foreach (name in ["card-populated", "initial-populated", "consumers-populated", "empty-consumers"]) {
    local r = accepted(cards.Load(read(CASES + name + ".wire")), name);
    eq(cards.Render(r), text(EXPECTED + name + ".txt"), "render " + name);
  }

  // a brief that is not UTF-8, or carries a NUL, or has no RESULT line
  refused(cards.Parse("RESULT: x sha=aaaaaaaaaaaa tier: pro\n\xff"), "malformed", "invalid UTF-8");
  local nul = cards.toBlob("RESULT: x sha=aaaaaaaaaaaa tier: pro\n?");
  nul[nul.len() - 1] = 0;
  refused(cards.Parse(nul), "malformed", "NUL");
  refused(cards.Parse("KIND: x\n"), "malformed", "no RESULT line");
  // a surrogate and an overlong sequence are not UTF-8 either
  refused(cards.Parse("RESULT: x sha=aaaaaaaaaaaa tier: pro\n\xed\xa0\x80"), "malformed", "a surrogate");
  refused(cards.Parse("RESULT: x sha=aaaaaaaaaaaa tier: pro\n\xc0\xaf"), "malformed", "an overlong slash");
  // a stranded key after prose names the key
  refused(cards.Parse("RESULT: x sha=aaaaaaaaaaaa tier: pro\nprose\nKIND: late\n"), "stranded-key KIND",
    "stranded key");
  // the body is the exact suffix after the last header line
  local p = accepted(cards.Parse("RESULT: x sha=aaaaaaaaaaaa tier: pro\nKIND: k\n\n  body\n"), "body");
  eq(p.root.Body, "\n  body\n", "the body bytes");
  eq(p.root.Kind, "k", "KIND");
  refused(cards.Parse("RESULT: x sha=aaaaaaaaaaaa tier: pro\nDEADLINE: finish within 100001 minutes.\n"),
    "malformed", "a deadline past its range");
});

test("TestUpgradeMatchesTheCorpus", function() {
  foreach (name in ["initial-populated", "consumers-populated", "empty-consumers", "future-note"]) {
    local out = accepted(cards.Upgrade(read(CASES + name + ".wire")), name);
    eq(out.wire, read(EXPECTED + name + ".wire"), "upgrade " + name);
  }
  local future = accepted(cards.Upgrade(read(CASES + "future-note.wire")), "future-note");
  eq(future.report.Retained, 1, "future-note retained");
  eq(future.report.Unknown, 1, "future-note unknown");
  refused(cards.Upgrade(read(CASES + "truncated.wire")), "malformed", "truncated");

  // a retained field the caller's storage cannot hold is named and refused,
  // and the input is never mutated (tla/CardVersioning.tla: RefusalKeepsOriginal)
  local wire = read(CASES + "future-note.wire");
  local lost = cards.upgrade(wire, 0, 0);
  refused(lost, "retain-lost", "zero retention capacity");
  ok(lost.report.RetainLost > 0, "the report counts the loss");
  eq(wire, read(CASES + "future-note.wire"), "input preserved");
});

// the independent byte vector: each text is its length as eight little-endian
// bytes then its bytes, each list its count then its framed elements
test("TestTaskHashFramesLogicalFields", function() {
  local a = accepted(cards.FromJSON(jsonCard(",\"body\":\"a\",\"paths\":[{\"s\":\"bc\"}]")), "a");
  local b = accepted(cards.FromJSON(jsonCard(",\"body\":\"ab\",\"paths\":[{\"s\":\"c\"}]")), "b");
  eq(cards.TaskHash(a), "49d2070600dfd1bc43252553ebe0c9ac99c1374c16186f8d5ed876e7f8c089ae", "body a / paths [bc]");
  eq(cards.TaskHash(b), "4d76000071d4b7c8ce523ac264a5fbc8cd06b405ae03626ee9ce244492326f39", "body ab / paths [c]");

  foreach (name in ["card-populated", "initial-populated", "empty-consumers"]) {
    local record = accepted(cards.Load(read(CASES + name + ".wire")), name);
    eq(cards.TaskHash(record), text(EXPECTED + name + ".hex"), "TaskHash " + name);
  }

  local lossy = accepted(cards.Load(read(CASES + "future-note.wire")), "future-note");
  refused(cards.TaskHash(lossy), "unknown-present", "a lossy record");
});

test("TestFieldEditLeavesTheTaskHashAlone", function() {
  local wire = read(CASES + "card-populated.wire");
  local record = accepted(cards.Load(wire), "Load");
  local before = cards.TaskHash(record);
  local body0 = record.root.Body;

  accepted(cards.SetNeeds(record, ["a", "b"]), "SetNeeds");
  accepted(cards.SetTier(record, "heavy"), "SetTier");
  accepted(cards.SetPriority(record, "blocker"), "SetPriority");
  accepted(cards.SetColumn(record, "landed"), "SetColumn");
  accepted(cards.SetHolder(record, "someone"), "SetHolder");
  ok(!cards.sameBytes(record.Wire(), wire), "the edits changed the wire");
  eq(cards.TaskHash(record), before, "needs, tier, priority, column and holder edits keep the hash");
  eq(record.root.Body, body0, "the body stays byte-equal");

  accepted(cards.SetPaths(record, ["x/y"]), "SetPaths");
  ok(cards.TaskHash(record) != before, "a paths edit changes the hash");

  // every refusal leaves the wire byte-equal
  local kept = record.Wire();
  refused(cards.SetTier(record, "ultra"), "malformed", "an undeclared tier");
  refused(cards.SetHolder(record, rep("h", 33)), "malformed", "a holder past its bound");
  local many = [];
  for (local i = 0; i <= cards.MaxListEntries; i++) many.append("p");
  refused(cards.SetPaths(record, many), "list-bound paths 512", "513 paths");
  eq(record.Wire(), kept, "the wire after the refusals");

  // a record carrying an unknown field is never rewritten
  local future = accepted(cards.Load(read(CASES + "future-note.wire")), "future-note");
  refused(cards.SetNeeds(future, ["a"]), "unknown-present", "an edit over an unknown field");
});

test("TestReadStrictRefusesWhatTheDeclarationCannotName", function() {
  accepted(cards.ReadStrict(read(CASES + "card-populated.wire")), "a clean card");
  refused(cards.ReadStrict(read(CASES + "future-note.wire")), "unknown-present", "an unknown field");
  refused(cards.Load(read(CASES + "truncated.wire")), "malformed", "a truncated wire");
});

::print(::format("cards (squirrel): %d passed, %d failed\n", passed, failed));
if (failed > 0) ::error("cards (squirrel): " + failed + " test(s) failed\n");
