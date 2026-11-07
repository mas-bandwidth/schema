--!nonstrict
-- Native facade unit checks. Only the unit manifest supplies expected fixtures;
-- conformance drivers receive the harness's input-only manifest.
local cards = require("./cards")
local inputs = require("./inputs")
local passed, failed = 0, 0

local function test(name, body)
  local ok, err = pcall(body)
  if ok then
    passed = passed + 1
  else
    failed = failed + 1
    error("FAIL " .. name .. ": " .. tostring(err), 0)
  end
end

local function read(path) return assert(inputs.files[path], "missing fixture " .. path) end

local CASES = "tables/cards/conformance/cases/"
local EXPECTED = "tables/cards/conformance/expected/"

local function eq(got, want, what)
  if got ~= want then error(string.format("%s: got %q, want %q", what, tostring(got), tostring(want)), 2) end
end

local function refused(value, err, class, what)
  if value ~= nil then error(what .. ": accepted, want refusal " .. class, 2) end
  eq(err.class, class, what .. " class")
end

-- a refused record names its reason in the message, never as free text alone
local function json_card(fields) return '{"id":"x"' .. fields .. "}" end

test("sha256 matches the FIPS 180-4 vectors", function()
  eq(cards.sha256hex(""), "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855", "empty")
  eq(cards.sha256hex("abc"), "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad", "abc")
  eq(cards.sha256hex("abcdbcdecdefdefgefghfghighijhijkijkljklmklmnlmnomnopnopq"),
    "248d6a61d20638b8e5c026930c3e6039a33ce45964ff2167f6ecedd419db06c1", "two blocks")
end)

test("TestCardRoundTripsThePopulatedFixture", function()
  local wire = read(CASES .. "card-populated.wire")
  local json = read(CASES .. "card-populated.json")
  local record, report = cards.Load(wire)
  assert(record ~= nil, tostring(report))
  eq(report.malformed, false, "malformed")
  local text = cards.ToJSON(record)
  eq(text, json, "ToJSON reproduces card-populated.json")
  local again = cards.FromJSON(json)
  assert(again ~= nil, "FromJSON refused the fixture")
  eq(again:Wire(), wire, "FromJSON reproduces card-populated.wire")
  -- a Lua string is immutable: the record's wire is its own by construction
  eq(record:Wire(), wire, "Wire")
end)

test("TestCardBoundsRefuseBeforePublishing", function()
  local body = function(n) return json_card(',"body":"' .. string.rep("y", n) .. '"') end
  assert(cards.FromJSON(body(cards.MaxBodyBytes)), "exactly 64 KiB body was refused")
  local record, err = cards.FromJSON(body(cards.MaxBodyBytes + 1))
  refused(record, err, "body-bound 65536", "64 KiB + 1 body")
  assert(err.message:find("65537") and err.message:find("65536"), err.message)

  local collections = { { "paths", '{"s":"p"}' }, { "_new", '{"s":"n"}' }, { "shared", '{"s":"s"}' },
    { "needs", '{"s":"d"}' }, { "consumers", '{"kind":"k"}' } }
  for _, c in ipairs(collections) do
    local field, element = c[1], c[2]
    local function list(n)
      return json_card(',"' .. field .. '":[' .. string.rep(element .. ",", n - 1) .. element .. "]")
    end
    assert(cards.FromJSON(list(cards.MaxListEntries)), "exactly 512 entries in " .. field .. " was refused")
    local r, e = cards.FromJSON(list(cards.MaxListEntries + 1))
    refused(r, e, "list-bound " .. field .. " 512", "513 entries in " .. field)
    assert(e.message:find("513"), e.message)
  end
end)

test("TestParseRenderMatchesTheCorpus", function()
  local record = cards.Parse(read(CASES .. "normalized-valid.md"))
  assert(record ~= nil, "normalized-valid did not parse")
  eq(record:Wire(), read(EXPECTED .. "normalized-valid.wire"), "parsed wire")

  local none, err = cards.Parse(read(CASES .. "rejected-legacy.md"))
  refused(none, err, read(EXPECTED .. "rejected-legacy.refused"):gsub("\n$", ""), "rejected-legacy")

  for _, name in ipairs({ "card-populated", "initial-populated", "consumers-populated", "empty-consumers" }) do
    local r = assert(cards.Load(read(CASES .. name .. ".wire")))
    eq(cards.Render(r), read(EXPECTED .. name .. ".txt"), "render " .. name)
  end

  -- a brief that is not UTF-8, or carries a NUL, or has no RESULT line
  local bad, e1 = cards.Parse("RESULT: x sha=aaaaaaaaaaaa tier: pro\n\xff")
  refused(bad, e1, "malformed", "invalid UTF-8")
  bad, e1 = cards.Parse("RESULT: x sha=aaaaaaaaaaaa tier: pro\n\0")
  refused(bad, e1, "malformed", "NUL")
  bad, e1 = cards.Parse("KIND: x\n")
  refused(bad, e1, "malformed", "no RESULT line")
  -- a stranded key after prose names the key
  bad, e1 = cards.Parse("RESULT: x sha=aaaaaaaaaaaa tier: pro\nprose\nKIND: late\n")
  refused(bad, e1, "stranded-key KIND", "stranded key")
end)

test("TestUpgradeMatchesTheCorpus", function()
  for _, name in ipairs({ "initial-populated", "consumers-populated", "empty-consumers", "future-note" }) do
    local out, report = cards.Upgrade(read(CASES .. name .. ".wire"))
    assert(out ~= nil, name .. ": " .. tostring(report))
    eq(out, read(EXPECTED .. name .. ".wire"), "upgrade " .. name)
  end
  local out, err = cards.Upgrade(read(CASES .. "truncated.wire"))
  refused(out, err, "malformed", "truncated")

  -- a retained field the caller's storage cannot hold is named and refused,
  -- and the input is never mutated (tla/CardVersioning.tla: RefusalKeepsOriginal)
  local future = read(CASES .. "future-note.wire")
  local lost, e2 = cards.upgrade(future, 0, 0)
  refused(lost, e2, "retain-lost", "zero retention capacity")
  eq(e2.report.retainLost > 0, true, "the report counts the loss")
  eq(future, read(CASES .. "future-note.wire"), "input preserved")
end)

-- the independent byte vector: each text is its length as eight little-endian
-- bytes then its bytes, each list its count then its framed elements
test("TestTaskHashFramesLogicalFields", function()
  local a = assert(cards.FromJSON(json_card(',"body":"a","paths":[{"s":"bc"}]')))
  local b = assert(cards.FromJSON(json_card(',"body":"ab","paths":[{"s":"c"}]')))
  eq(cards.TaskHash(a), "49d2070600dfd1bc43252553ebe0c9ac99c1374c16186f8d5ed876e7f8c089ae", "body a / paths [bc]")
  eq(cards.TaskHash(b), "4d76000071d4b7c8ce523ac264a5fbc8cd06b405ae03626ee9ce244492326f39", "body ab / paths [c]")

  for _, name in ipairs({ "card-populated", "initial-populated", "empty-consumers" }) do
    local record = assert(cards.Load(read(CASES .. name .. ".wire")))
    eq(cards.TaskHash(record), read(EXPECTED .. name .. ".hex"), "TaskHash " .. name)
  end

  local lossy = assert(cards.Load(read(CASES .. "future-note.wire")))
  local hex, err = cards.TaskHash(lossy)
  refused(hex, err, "unknown-present", "a lossy record")
end)

test("TestFieldEditLeavesTheTaskHashAlone", function()
  local wire = read(CASES .. "card-populated.wire")
  local record = assert(cards.Load(wire))
  local before = cards.TaskHash(record)
  local body0 = record.root.body.value

  assert(cards.SetNeeds(record, { "a", "b" }))
  assert(cards.SetTier(record, "heavy"))
  assert(cards.SetPriority(record, "blocker"))
  assert(cards.SetColumn(record, "landed"))
  assert(cards.SetHolder(record, "someone"))
  eq(record:Wire() ~= wire, true, "the edits changed the wire")
  eq(cards.TaskHash(record), before, "needs, tier, priority, column and holder edits keep the hash")
  eq(record.root.body.value, body0, "the body stays byte-equal")

  assert(cards.SetPaths(record, { "x/y" }))
  eq(cards.TaskHash(record) ~= before, true, "a paths edit changes the hash")

  -- every refusal leaves the wire byte-equal
  local kept = record:Wire()
  local ok, err = cards.SetTier(record, "ultra")
  refused(ok, err, "malformed", "an undeclared tier")
  ok, err = cards.SetHolder(record, string.rep("h", 33))
  refused(ok, err, "malformed", "a holder past its bound")
  local many = {}
  for i = 1, cards.MaxListEntries + 1 do many[i] = "p" end
  ok, err = cards.SetPaths(record, many)
  refused(ok, err, "list-bound paths 512", "513 paths")
  eq(record:Wire(), kept, "the wire after the refusals")

  -- a record carrying an unknown field is never rewritten
  local future = assert(cards.Load(read(CASES .. "future-note.wire")))
  ok, err = cards.SetNeeds(future, { "a" })
  refused(ok, err, "unknown-present", "an edit over an unknown field")
end)

test("TestReadStrictRefusesWhatTheDeclarationCannotName", function()
  local record = cards.ReadStrict(read(CASES .. "card-populated.wire"))
  assert(record ~= nil, "a clean card was refused")
  local none, err = cards.ReadStrict(read(CASES .. "future-note.wire"))
  refused(none, err, "unknown-present", "an unknown field")
end)


test("Unicode scalar validation rejects overlong, surrogate and out-of-range UTF-8", function()
  for _, bytes in ipairs({"\192\128", "\237\160\128", "\244\144\128\128", "\240\128\128\128"}) do
    local value, err = cards.Parse("RESULT: x sha=aaaaaaaaaaaa tier: pro\n" .. bytes)
    refused(value, err, "malformed", "invalid Unicode scalar")
  end
  assert(cards.Parse("RESULT: x sha=aaaaaaaaaaaa tier: pro\n\240\159\140\159"))
end)

test("Consumer int64 values survive the codec and an edit exactly", function()
  local text = '{"id":"x","consumers":[{"started":9223372036854775807,"ended":-9223372036854775808,"cost_micro_usd":9007199254740993}]}'
  local record = assert(cards.FromJSON(text))
  local before = assert(cards.ToJSON(record))
  for _, exact in ipairs({"9223372036854775807", "-9223372036854775808", "9007199254740993"}) do
    assert(before:find(exact, 1, true), exact)
  end
  assert(cards.SetHolder(record, "reader"))
  local after = assert(cards.ToJSON(record))
  for _, exact in ipairs({"9223372036854775807", "-9223372036854775808", "9007199254740993"}) do
    assert(after:find(exact, 1, true), exact)
  end
end)

test("Codec conversion preserves shared-blob refusal", function()
  local codec = require("./cards_codec")
  local root = codec.NewCard()
  local shared = {value="same"}
  root.body, root.source = shared, shared
  eq(codec.Save(root), nil, "shared blob must not be cloned into two permitted blobs")
end)

return string.format("cards (luau): %d passed, %d failed\n", passed, failed)
