-- cards.lua — the Lua card facade (tables/cards/cards.go, parse.go, render.go,
-- edit.go, upgrade.go), the owning layer a customer requires.
--
-- The generated table module (CardsTables) and the runtime modules beside it
-- (load, save, json, retain; docs/PORT-LUA.md) answer the wire and text forms;
-- this facade is the thin owning layer: a Record that keeps its own copy of
-- the wire, checked writes that refuse before publishing, and the two customer
-- ceilings the schema deliberately leaves unbounded (docs/SPEC-TABLES.md §2.5,
-- §2.9). It is the Go leg's facade in Lua's own spelling and answers the same
-- nine surfaces byte for byte on the shared corpus (tla/CardHops.tla:
-- ImplementationsAgree, HopOrderIrrelevant; tables/cards/conformance).
--
-- THE ERROR IDIOM. A refusal is the Lua pair `nil, err`, never a raised error:
-- err is a CardError whose `class` is one of the closed refusal classes
-- tables/cards/conformance/FORMAT.md fixes (body-bound, list-bound,
-- stranded-key, retain-lost, unknown-present, malformed), never free text, and
-- whose tostring is the message. A read that completed answers its report as
-- the second value, and a refusal after a read carries it as err.report.
--
-- THE REQUIRE PATH. The caller puts the generated directory (CardsTables.lua
-- and the runtime modules) and this file's directory on package.path.
--
-- A `*string` field (body, done, source, an extra's value) is a BOX, `{ value = <string> }`,
-- and nil when null (docs/PORT-LUA.md, THE TYPE MAPPING): the facade reads and
-- writes it through `unbox` and `box` and never hands the box out.
--
-- A string is a Lua byte string, so a "well-formed" string is valid UTF-8 with
-- no NUL (utf8.len is strict: a surrogate or an overlong sequence is refused),
-- and every bound is a byte count.

local T = require("CardsTables")
local load_mod = require("load")
local save_mod = require("save")
local json_mod = require("json")
local retain_mod = require("retain")

local M = {}

local ROOT = "Card"

-- MaxBodyBytes is the customer ceiling on one card's opaque body. The schema
-- declares body *string with no bound (docs/SPEC-TABLES.md §2.5); this facade
-- refuses a larger body before any bytes are published.
M.MaxBodyBytes = 64 << 10

-- MaxListEntries is the customer ceiling on each of a card's collection fields
-- (paths/_new/shared/needs/extra/consumers), which the schema leaves unbounded
-- (docs/SPEC-TABLES.md §2.9).
M.MaxListEntries = 512

-- MaxRetainBytes and MaxRetainIDs are the customer capacities of the retained
-- byte store and the retained-id store; neither grows on demand
-- (docs/SPEC-TABLES.md §6.6).
M.MaxRetainBytes = 1 << 20
M.MaxRetainIDs = 4096

-- ---- errors -----------------------------------------------------------------

local CardError = {}
CardError.__index = CardError
CardError.__tostring = function(e) return e.message end
M.CardError = CardError

local function refuse(message, class, report)
  return setmetatable({ message = message, class = class, report = report }, CardError)
end

-- ---- the read report ----------------------------------------------------------

-- newReport is the caller's zeroed counters, the Lua spelling of a zero Go
-- TableReport: the runtime counts into the table it is handed.
local function newReport()
  return {
    retained = 0, retainLost = 0, widened = 0, unknown = 0, kindMismatch = 0,
    clamped = 0, duplicate = 0, malformed = false, refused = 0, blobShared = 0,
    missingRequired = 0, wireDamaged = false,
  }
end
M.NewReport = newReport

-- ---- Record -------------------------------------------------------------------

-- Record owns the wire and the decoded Card. A Lua string is immutable, so no
-- caller's buffer can alias the record's wire (docs/SPEC-TABLES.md §6.2).
local Record = {}
Record.__index = Record
M.Record = Record

local function newRecord(wire, root, report)
  return setmetatable({ wire = wire, root = root, report = report }, Record)
end

-- Wire returns the record's wire bytes.
function Record:Wire() return self.wire end

-- ---- UTF-8 and whitespace helpers ----------------------------------------------

local function wellFormed(s)
  return utf8.len(s) ~= nil and not s:find("\0", 1, true)
end

-- Go's unicode.IsSpace set, as UTF-8 byte sequences, for strings.TrimSpace.
local SPACE_SEQS = {
  "\xC2\x85", "\xC2\xA0", "\xE1\x9A\x80", "\xE2\x80\xA8", "\xE2\x80\xA9",
  "\xE2\x80\xAF", "\xE2\x81\x9F", "\xE3\x80\x80",
}
for b = 0x80, 0x8A do SPACE_SEQS[#SPACE_SEQS + 1] = "\xE2\x80" .. string.char(b) end

local function spaceAt(s, i)
  local b = s:byte(i)
  if b == nil then return 0 end
  if b == 0x20 or (b >= 0x09 and b <= 0x0D) then return 1 end
  if b < 0x80 then return 0 end
  for _, seq in ipairs(SPACE_SEQS) do
    if s:sub(i, i + #seq - 1) == seq then return #seq end
  end
  return 0
end

local function spaceBefore(s, j)
  for _, n in ipairs({ 1, 2, 3 }) do
    local i = j - n + 1
    if i >= 1 and spaceAt(s, i) == n then return n end
  end
  return 0
end

-- trim is strings.TrimSpace.
local function trim(s)
  local i, j = 1, #s
  while i <= j do
    local n = spaceAt(s, i)
    if n == 0 then break end
    i = i + n
  end
  while j >= i do
    local n = spaceBefore(s, j)
    if n == 0 then break end
    j = j - n
  end
  return s:sub(i, j)
end

local function unbox(b)
  if b == nil then return nil end
  return b.value
end

local function box(s) return { value = s } end

local function hasCRLF(s) return s:find("[\r\n]") ~= nil end

local function set(...)
  local out = {}
  for _, k in ipairs({ ... }) do out[k] = true end
  return out
end

-- ---- the checked-write helper (tables/cards/cards.go checkCard) -----------------

local function listBound(name, n)
  return refuse(
    string.format("cards: %s has %d entries, past the %d-entry ceiling (docs/SPEC-TABLES.md §2.9)",
      name, n, M.MaxListEntries),
    "list-bound " .. name .. " 512")
end

local function checkStrings(list, name)
  if #list > M.MaxListEntries then return listBound(name, #list) end
  for _, e in ipairs(list) do
    if not wellFormed(e.s) then
      return refuse("cards: " .. name .. " has an element that is not well-formed UTF-8 without NUL", "malformed")
    end
  end
  return nil
end

local headerKeyPattern = "^[A-Za-z][A-Za-z0-9%-]*$"

local reservedHeaderKeys = set("RESULT", "tier", "REPO", "BASE", "KIND", "PRIORITY", "PATHS", "NEW",
  "SHARED", "DEPENDS-ON", "TEST", "DONE-WHEN", "DEADLINE", "SOURCE")

-- checkExtras enforces the extras rules before any write publishes: the
-- 512-entry ceiling, the admitted header key grammar, no CR or LF in a key or
-- value, well-formed keys and values, and no key duplicating a mapped header
-- name (HEADER MAPPING, docs/SPEC-TABLES.md §2.9).
local function checkExtras(list)
  if #list > M.MaxListEntries then return listBound("extra", #list) end
  for _, e in ipairs(list) do
    if not wellFormed(e.key) then
      return refuse("cards: extra has a key that is not well-formed UTF-8 without NUL", "malformed")
    end
    if not e.key:find(headerKeyPattern) then
      return refuse(string.format("cards: extra key %q is not an admitted header key", e.key), "malformed")
    end
    if hasCRLF(e.key) then return refuse("cards: extra has a key with a CR or LF", "malformed") end
    if reservedHeaderKeys[e.key] then
      return refuse(string.format("cards: extra key %q duplicates a mapped header key", e.key), "malformed")
    end
    local value = unbox(e.value)
    if value ~= nil then
      if not wellFormed(value) then
        return refuse("cards: extra has a value that is not well-formed UTF-8 without NUL", "malformed")
      end
      if hasCRLF(value) then return refuse("cards: extra has a value with a CR or LF", "malformed") end
    end
  end
  return nil
end

local CONSUMER_STRINGS = { "kind", "who", "route", "model", "outcome", "line" }

local function checkConsumers(list)
  if #list > M.MaxListEntries then return listBound("consumers", #list) end
  for _, e in ipairs(list) do
    for _, name in ipairs(CONSUMER_STRINGS) do
      if not wellFormed(e[name]) then
        return refuse("cards: consumers has a " .. name .. " that is not well-formed UTF-8 without NUL", "malformed")
      end
    end
  end
  return nil
end

-- checkCard enforces the customer bounds and the well-formedness of the opaque
-- body and every collection element before bytes are published
-- (docs/SPEC-TABLES.md §2.5, §2.9).
local function checkCard(root)
  if root == nil then return refuse("cards: nil card", "malformed") end
  local body = unbox(root.body)
  if body ~= nil then
    if #body > M.MaxBodyBytes then
      return refuse(string.format("cards: body is %d bytes, past the %d-byte ceiling", #body, M.MaxBodyBytes),
        "body-bound 65536")
    end
    if not wellFormed(body) then
      return refuse("cards: body is not well-formed UTF-8 without NUL", "malformed")
    end
  end
  return checkStrings(root.paths, "paths")
    or checkStrings(root._new, "_new")
    or checkStrings(root.shared, "shared")
    or checkStrings(root.needs, "needs")
    or checkExtras(root.extra)
    or checkConsumers(root.consumers)
end

-- save is the generated Save behind the facade's one refusal.
local function save(root)
  local wire = save_mod.Save(T, ROOT, root)
  if wire == nil then return nil, refuse("cards: save did not fill the measured wire", "malformed") end
  return wire
end

-- ---- the owning facade (tables/cards/cards.go) -----------------------------------

-- readWire is the one tolerant read every facade read shares: the measure
-- first (a wire that is not a card), then the load into a zeroed report.
local function readWire(wire)
  local report = newReport()
  if load_mod.LoadMeasure(T, ROOT, wire) == nil then
    return nil, refuse("cards: wire is not a card", "malformed", report), report
  end
  local root = load_mod.LoadTolerant(T, ROOT, wire, report)
  if root == nil then return nil, refuse("cards: wire is malformed", "malformed", report), report end
  return root, nil, report
end

-- Load reads wire into a Record (docs/SPEC-TABLES.md §6.2). Refusals and
-- malformed wire are errors; tolerant events ride in the returned report. The
-- ORIGINAL wire is kept, never rewritten, so an old input is preserved byte
-- for byte.
function M.Load(wire)
  local root, err, report = readWire(wire)
  if err ~= nil then return nil, err end
  local cerr = checkCard(root)
  if cerr ~= nil then
    cerr.report = report
    return nil, cerr
  end
  return newRecord(wire, root, report), report
end

-- FromJSON builds a Record from JSON text (docs/SPEC-TABLES.md §16). Unknown,
-- mismatched, clamped and malformed inputs are refused, as are the customer
-- bounds and any body or element that is not well-formed UTF-8 without NUL.
-- Bytes are published only on complete success.
function M.FromJSON(text)
  local report = newReport()
  local root = json_mod.FromJsonTolerant(T, ROOT, text, report)
  if root == nil or report.malformed then return nil, refuse("cards: JSON is malformed", "malformed") end
  if report.unknown > 0 or report.kindMismatch > 0 or report.clamped > 0 then
    return nil, refuse(
      string.format("cards: JSON has loss events (unknown=%d kindMismatch=%d clamped=%d): refused, never truncated",
        report.unknown, report.kindMismatch, report.clamped),
      "malformed")
  end
  local cerr = checkCard(root)
  if cerr ~= nil then return nil, cerr end
  local wire, serr = save(root)
  if wire == nil then return nil, serr end
  return newRecord(wire, root, report), report
end

-- ToJSON writes the record's known-field JSON text and answers the record's
-- load report. It is the known-field view, not a lossless archival rewrite
-- (docs/SPEC-TABLES.md §16).
function M.ToJSON(record)
  if record == nil or record.root == nil then return nil, refuse("cards: nil record", "malformed") end
  local text = json_mod.ToJson(T, ROOT, record.root)
  if text == nil then return nil, refuse("cards: the card has no JSON spelling", "malformed", record.report) end
  return text, record.report
end

-- ---- parse (tables/cards/parse.go) -------------------------------------------------

local strandedKeys = set("KIND", "PATHS", "TEST", "LEGS", "SOURCE", "START", "STOP", "DEPENDS-ON",
  "PRIORITY", "NEW", "SHARED", "REPO", "BASE", "tier", "RESULT")

local TIERS = set("flash", "pro", "heavy", "frontier")
local PRIORITIES = set("normal", "high", "critical", "blocker")
local COLUMNS = set("waiting", "ready", "working", "review", "merging", "landed", "plan", "check")

-- splitLines answers every physical line with its content and its terminator
-- end offset (the 0-based offset just past the LF). A trailing LF does not
-- invent a final empty line.
local function splitLines(text)
  local refs = {}
  local i, n = 1, 1
  while i <= #text do
    local j = text:find("\n", i, true)
    local lineEnd, termEnd = #text, #text
    if j ~= nil then lineEnd, termEnd = j - 1, j end
    local content = text:sub(i, lineEnd)
    if content:sub(-1) == "\r" then content = content:sub(1, -2) end
    refs[#refs + 1] = { content = content, termEnd = termEnd, num = n }
    n = n + 1
    i = termEnd + 1
  end
  return refs
end

-- matchHeader recognizes a header key line: one letter then letters, digits
-- and hyphens, a colon, optional ASCII whitespace and the rest as the value.
-- Prose, blank and any line with leading whitespace is not one.
local function matchHeader(content)
  local key, value = content:match("^([A-Za-z][A-Za-z0-9%-]*):[ \t\n\f\r]*(.*)$")
  if key == nil then return nil end
  return key, value
end

local function isBlank(content) return content:find("[^ \t\r]") == nil end

local function checkStrBound(name, value, bound)
  if #value > bound then
    return refuse(string.format("cards: %s is %d bytes, past the %d-byte bound", name, #value, bound), "malformed")
  end
  return nil
end

local function isHex(s) return s ~= "" and s:find("[^0-9a-fA-F]") == nil end

local function lastIndex(s, needle)
  local at = nil
  local from = 1
  while true do
    local i = s:find(needle, from, true)
    if i == nil then return at end
    at = i
    from = i + 1
  end
end

-- applyResult parses the RESULT value "id sha=<sha> tier: <tier>"
-- (docs/SPEC-TABLES.md §2, §4).
local function applyResult(p, value)
  local i = lastIndex(value, " tier:")
  if i == nil then return refuse('cards: RESULT line has no " tier:"', "malformed") end
  local tierStr = trim(value:sub(i + #" tier:"))
  local rest = value:sub(1, i - 1)
  local j = lastIndex(rest, " sha=")
  if j == nil then return refuse('cards: RESULT line has no " sha="', "malformed") end
  local idStr = trim(rest:sub(1, j - 1))
  local shaStr = rest:sub(j + #" sha=")
  local err = checkStrBound("id", idStr, 64)
  if err ~= nil then return err end
  if not isHex(shaStr) or (#shaStr ~= 12 and #shaStr ~= 40) then
    return refuse(string.format("cards: RESULT sha %q is not 12 or 40 hex digits", shaStr), "malformed")
  end
  if not TIERS[tierStr] then
    return refuse(string.format("cards: RESULT tier: unknown tier %q", tierStr), "malformed")
  end
  p.id, p.sha, p.tier = idStr, shaStr, tierStr
  return nil
end

local function assignStr(p, field, value, bound, name, num)
  local err = checkStrBound(name, value, bound)
  if err ~= nil then return refuse(string.format("cards: %s on line %d: %s", name, num, err.message), "malformed") end
  p[field] = value
  return nil
end

-- splitListChecked splits a comma-separated list, trims elements, drops empty
-- ones, and enforces the element bound and the entry ceiling
-- (docs/SPEC-TABLES.md §2.9). A whole value of "-" or empty is an empty list;
-- PATHS alone also takes the customer sentinel "none".
local function splitListChecked(p, field, value, bound, noneSentinel, name, num)
  local v = trim(value)
  if v == "" or v == "-" or (noneSentinel and v == "none") then
    p[field] = {}
    return nil
  end
  local out = {}
  for part in (v .. ","):gmatch("([^,]*),") do
    part = trim(part)
    if part ~= "" then
      local err = checkStrBound(name .. " element", part, bound)
      if err ~= nil then
        return refuse(string.format("cards: %s on line %d: %s", name, num, err.message), "malformed")
      end
      out[#out + 1] = part
    end
  end
  if #out > M.MaxListEntries then
    return refuse(string.format("cards: %s on line %d has %d entries, past the %d-entry ceiling", name, num, #out,
      M.MaxListEntries), "malformed")
  end
  p[field] = out
  return nil
end

local function assignMapped(p, key, value, num)
  if key == "REPO" then return assignStr(p, "repo", value, 96, "REPO", num)
  elseif key == "BASE" then return assignStr(p, "base", value, 96, "BASE", num)
  elseif key == "KIND" then return assignStr(p, "kind", value, 24, "KIND", num)
  elseif key == "PRIORITY" then
    local pr = trim(value)
    if not PRIORITIES[pr] then
      return refuse(string.format("cards: PRIORITY on line %d: unknown priority %q", num, pr), "malformed")
    end
    p.priority = pr
  elseif key == "PATHS" then return splitListChecked(p, "paths", value, 192, true, "PATHS", num)
  elseif key == "NEW" then return splitListChecked(p, "news", value, 192, false, "NEW", num)
  elseif key == "SHARED" then return splitListChecked(p, "shared", value, 192, false, "SHARED", num)
  elseif key == "DEPENDS-ON" then return splitListChecked(p, "needs", value, 64, false, "DEPENDS-ON", num)
  elseif key == "TEST" then return assignStr(p, "test", value, 192, "TEST", num)
  elseif key == "DONE-WHEN" then p.doneWhen = value
  elseif key == "DEADLINE" then
    local digits = trim(value):match("^finish within ([0-9]+) minutes%.$")
    local n = digits and math.tointeger(tonumber(digits))
    if digits == nil then
      return refuse(string.format('cards: DEADLINE on line %d: not of the form "finish within N minutes."', num),
        "malformed")
    end
    if n == nil or #digits > 18 or n > 100000 then
      return refuse(string.format("cards: DEADLINE on line %d: minutes out of range", num), "malformed")
    end
    p.deadline = n
  elseif key == "SOURCE" then p.source = value
  end
  return nil
end

-- applyHeader dispatches one header line to its field, an agreeing tier, or an
-- extra, refusing duplicates and CR/LF inside any single-line value.
local function applyHeader(p, key, value, num, seen)
  if hasCRLF(value) then
    return refuse(string.format("cards: %s on line %d has a CR or LF in its value", key, num), "malformed")
  end
  if key == "tier" then
    if seen.tier ~= nil then
      return refuse(string.format("cards: duplicate header key %q on lines %d and %d", key, seen.tier, num),
        "malformed")
    end
    seen.tier = num
    local t = trim(value)
    if not TIERS[t] then
      return refuse(string.format("cards: tier on line %d: unknown tier %q", num, t), "malformed")
    end
    if t ~= p.tier then
      return refuse(string.format("cards: tier on line %d disagrees with RESULT", num), "malformed")
    end
    return nil
  end
  if key == "RESULT" then
    return refuse(string.format("cards: duplicate header key %q on lines %d and %d", key, seen.RESULT, num),
      "malformed")
  end
  if reservedHeaderKeys[key] then
    if seen[key] ~= nil then
      return refuse(string.format("cards: duplicate header key %q on lines %d and %d", key, seen[key], num),
        "malformed")
    end
    seen[key] = num
    return assignMapped(p, key, value, num)
  end
  local err = checkStrBound("extra key", key, 32)
  if err ~= nil then return refuse(string.format("cards: line %d: %s", num, err.message), "malformed") end
  p.extras[#p.extras + 1] = { key = key, value = value }
  return nil
end

local function rows(list)
  local out = {}
  for i, s in ipairs(list) do out[i] = { s = s } end
  return out
end

-- build publishes the parsed card through the generated save and the checked
-- write (docs/SPEC-TABLES.md §6.2). Bytes are published only on complete
-- success.
local function build(p, body)
  local card = T.NewCard()
  card.id, card.sha, card.tier = p.id, p.sha, p.tier
  card.repo, card._base, card.kind = p.repo, p.base, p.kind
  card.priority, card.stream = p.priority, p.stream
  card.paths, card._new, card.shared, card.needs = rows(p.paths), rows(p.news), rows(p.shared), rows(p.needs)
  card.test = p.test
  if p.doneWhen ~= "" then card.done = box(p.doneWhen) end
  card.deadline_minutes = p.deadline
  if p.source ~= "" then card.source = box(p.source) end
  for i, kv in ipairs(p.extras) do card.extra[i] = { key = kv.key, value = box(kv.value) } end
  if #body > 0 then card.body = box(body) end
  local cerr = checkCard(card)
  if cerr ~= nil then return nil, cerr end
  local wire, serr = save(card)
  if wire == nil then return nil, serr end
  return newRecord(wire, card, newReport()), newReport()
end

-- Parse reads admitted brief text and publishes a typed Record only on complete
-- checked success (docs/SPEC-TABLES.md §2, §6.2). The body is the exact suffix
-- immediately after the last header line's terminator; header layout may
-- canonicalize, body bytes never do.
function M.Parse(text)
  if utf8.len(text) == nil then return nil, refuse("cards: brief is not valid UTF-8", "malformed") end
  if text:find("\0", 1, true) then return nil, refuse("cards: brief contains a NUL byte", "malformed") end
  local lines = splitLines(text)
  if #lines == 0 then return nil, refuse("cards: brief has no RESULT line", "malformed") end
  local key, value = matchHeader(lines[1].content)
  if key ~= "RESULT" then return nil, refuse("cards: line 1 is not a RESULT line", "malformed") end
  local p = {
    id = "", sha = "", tier = nil, repo = "", base = "", kind = "", priority = nil, stream = "",
    paths = {}, news = {}, shared = {}, needs = {}, test = "", doneWhen = "", source = "",
    deadline = 0, extras = {},
  }
  local err = applyResult(p, value)
  if err ~= nil then return nil, err end
  local lastHeaderEnd = lines[1].termEnd
  local seen = { RESULT = 1 }
  local prose = false
  for k = 2, #lines do
    local ln = lines[k]
    if not isBlank(ln.content) then
      local hk, hv = matchHeader(ln.content)
      if hk == nil then
        prose = true
      elseif prose then
        if strandedKeys[hk] then
          return nil, refuse(string.format("cards: stranded header key %q on line %d", hk, ln.num),
            "stranded-key " .. hk)
        end
      else
        local aerr = applyHeader(p, hk, hv, ln.num, seen)
        if aerr ~= nil then return nil, aerr end
        lastHeaderEnd = ln.termEnd
      end
    end
  end
  return build(p, text:sub(lastHeaderEnd + 1))
end

-- ---- render (tables/cards/render.go) --------------------------------------------------

local function joined(list)
  local out = {}
  for i, e in ipairs(list) do out[i] = e.s end
  return table.concat(out, ", ")
end

local function renderCard(root)
  if not TIERS[root.tier or ""] then return nil end
  local out = { "RESULT: " .. root.id .. " sha=" .. root.sha .. " tier: " .. root.tier .. "\n" }
  local function line(key, value) out[#out + 1] = key .. ": " .. value .. "\n" end
  if root.repo ~= "" then line("REPO", root.repo) end
  if root._base ~= "" then line("BASE", root._base) end
  if root.kind ~= "" then line("KIND", root.kind) end
  if root.priority ~= nil then
    if not PRIORITIES[root.priority] then return nil end
    line("PRIORITY", root.priority)
  end
  if #root.paths > 0 then line("PATHS", joined(root.paths)) end
  if #root._new > 0 then line("NEW", joined(root._new)) end
  if #root.shared > 0 then line("SHARED", joined(root.shared)) end
  if #root.needs > 0 then line("DEPENDS-ON", joined(root.needs)) end
  if root.test ~= "" then line("TEST", root.test) end
  if root.done ~= nil then line("DONE-WHEN", unbox(root.done)) end
  if root.deadline_minutes ~= 0 then
    out[#out + 1] = "DEADLINE: finish within " .. root.deadline_minutes .. " minutes.\n"
  end
  if root.source ~= nil then line("SOURCE", unbox(root.source)) end
  for _, e in ipairs(root.extra) do line(e.key, unbox(e.value) or "") end
  if root.body ~= nil then out[#out + 1] = unbox(root.body) end
  return table.concat(out)
end

-- Render reads the record's wire into a fresh load and answers its
-- informational text plus the load report (docs/SPEC-TABLES.md §6.2, §16).
function M.Render(record)
  if record == nil or record.wire == nil then return nil, refuse("cards: nil record", "malformed") end
  local root, err, report = readWire(record.wire)
  if err ~= nil then return nil, err end
  local text = renderCard(root)
  if text == nil then return nil, refuse("cards: the card has no text spelling", "malformed", report) end
  return text, report
end

-- ---- upgrade and strict read (tables/cards/upgrade.go) --------------------------------

-- lossErr names the report events that make a rewrite unsafe (§6.6); Unknown,
-- Retained and Widened alone are not loss (§4).
local function lossErr(report)
  if report.retainLost > 0 then
    return refuse(string.format("cards: upgrade: %d retained field(s) lost, refusing to replace", report.retainLost),
      "retain-lost", report)
  end
  if report.kindMismatch > 0 then
    return refuse(string.format("cards: upgrade: %d kind mismatch(es), refusing to replace", report.kindMismatch),
      "malformed", report)
  end
  if report.clamped > 0 then
    return refuse(string.format("cards: upgrade: %d clamped value(s), refusing to replace", report.clamped),
      "malformed", report)
  end
  if report.malformed then return refuse("cards: upgrade: malformed wire, refusing to replace", "malformed", report) end
  return nil
end

-- upgrade is the retention seam Upgrade fixes the two customer capacities
-- through; the refusal probes drive smaller caller-owned storage through the
-- same path so a tiny capacity names retain-lost and answers nil while the
-- input is preserved.
local function upgrade(wire, retainBytes, retainIDs)
  local report = newReport()
  if load_mod.LoadMeasure(T, ROOT, wire) == nil then
    return nil, refuse("cards: upgrade: wire is not a card", "malformed", report)
  end
  local retain = { capacity = retainBytes, idCapacity = retainIDs, list = {}, used = 0 }
  local root = retain_mod.LoadRetain(T, ROOT, wire, retain, report)
  if root == nil then return nil, refuse("cards: upgrade: wire is malformed", "malformed", report) end
  local cerr = checkCard(root)
  if cerr ~= nil then
    cerr.report = report
    return nil, cerr
  end
  local size = retain_mod.MeasureRetain(T, ROOT, root, retain)
  if size == nil then
    return nil, refuse("cards: upgrade: measure refused the retained card", "malformed", report)
  end
  local saved = retain_mod.SaveRetain(T, ROOT, root, retain, report)
  if saved == nil or #saved ~= size then
    return nil, refuse("cards: upgrade: save did not fill the measured wire", "malformed", report)
  end
  local lerr = lossErr(report)
  if lerr ~= nil then return nil, lerr end
  return saved, report
end
M.upgrade = upgrade

-- Upgrade rewrites wire through the retaining path so every field the current
-- declaration cannot name is preserved at the end of its own body (§6.6). It
-- answers nil and a named error when any value would be lost (retain-lost,
-- kind mismatch, clamped, malformed) or the wire is refused; the input is never
-- touched (tla/CardVersioning.tla: NoFieldLost, BodyUnchanged,
-- RefusalKeepsOriginal, ShippedUpgradeSucceeds).
function M.Upgrade(wire) return upgrade(wire, M.MaxRetainBytes, M.MaxRetainIDs) end

-- ReadStrict reads wire and refuses any data the current declaration cannot
-- support: unknown, kind mismatch, clamped, malformed and every load failure
-- (docs/SPEC-TABLES.md §4).
function M.ReadStrict(wire)
  local record, report = M.Load(wire)
  if record == nil then return nil, report end
  if report.unknown > 0 then
    return nil, refuse(string.format("cards: strict: %d unknown field(s), unsupported data", report.unknown),
      "unknown-present", report)
  end
  if report.kindMismatch > 0 then
    return nil, refuse(string.format("cards: strict: %d kind mismatch(es), unsupported data", report.kindMismatch),
      "malformed", report)
  end
  if report.clamped > 0 then
    return nil, refuse(string.format("cards: strict: %d clamped value(s), unsupported data", report.clamped),
      "malformed", report)
  end
  if report.malformed then
    return nil, refuse("cards: strict: malformed wire, unsupported data", "malformed", report)
  end
  return record, report
end

-- ---- the task hash (tables/cards/edit.go) -------------------------------------------

-- SHA-256 (FIPS 180-4), in Lua 5.4 integers: Lua has no digest in its
-- standard library and the hash is part of the contract.
local K = {
  0x428a2f98, 0x71374491, 0xb5c0fbcf, 0xe9b5dba5, 0x3956c25b, 0x59f111f1, 0x923f82a4, 0xab1c5ed5,
  0xd807aa98, 0x12835b01, 0x243185be, 0x550c7dc3, 0x72be5d74, 0x80deb1fe, 0x9bdc06a7, 0xc19bf174,
  0xe49b69c1, 0xefbe4786, 0x0fc19dc6, 0x240ca1cc, 0x2de92c6f, 0x4a7484aa, 0x5cb0a9dc, 0x76f988da,
  0x983e5152, 0xa831c66d, 0xb00327c8, 0xbf597fc7, 0xc6e00bf3, 0xd5a79147, 0x06ca6351, 0x14292967,
  0x27b70a85, 0x2e1b2138, 0x4d2c6dfc, 0x53380d13, 0x650a7354, 0x766a0abb, 0x81c2c92e, 0x92722c85,
  0xa2bfe8a1, 0xa81a664b, 0xc24b8b70, 0xc76c51a3, 0xd192e819, 0xd6990624, 0xf40e3585, 0x106aa070,
  0x19a4c116, 0x1e376c08, 0x2748774c, 0x34b0bcb5, 0x391c0cb3, 0x4ed8aa4a, 0x5b9cca4f, 0x682e6ff3,
  0x748f82ee, 0x78a5636f, 0x84c87814, 0x8cc70208, 0x90befffa, 0xa4506ceb, 0xbef9a3f7, 0xc67178f2,
}

local MASK = 0xffffffff

local function rotr(x, n) return ((x >> n) | (x << (32 - n))) & MASK end

local function sha256hex(message)
  local h0, h1, h2, h3, h4, h5, h6, h7 =
    0x6a09e667, 0xbb67ae85, 0x3c6ef372, 0xa54ff53a, 0x510e527f, 0x9b05688c, 0x1f83d9ab, 0x5be0cd19
  local bits = #message * 8
  local padded = message .. "\x80" .. string.rep("\0", (55 - #message) % 64) .. string.pack(">I8", bits)
  local w = {}
  for chunk = 1, #padded, 64 do
    for i = 0, 15 do w[i] = string.unpack(">I4", padded, chunk + i * 4) end
    for i = 16, 63 do
      local s0 = rotr(w[i - 15], 7) ~ rotr(w[i - 15], 18) ~ (w[i - 15] >> 3)
      local s1 = rotr(w[i - 2], 17) ~ rotr(w[i - 2], 19) ~ (w[i - 2] >> 10)
      w[i] = (w[i - 16] + s0 + w[i - 7] + s1) & MASK
    end
    local a, b, c, d, e, f, g, h = h0, h1, h2, h3, h4, h5, h6, h7
    for i = 0, 63 do
      local S1 = rotr(e, 6) ~ rotr(e, 11) ~ rotr(e, 25)
      local ch = (e & f) ~ ((~e & MASK) & g)
      local t1 = (h + S1 + ch + K[i + 1] + w[i]) & MASK
      local S0 = rotr(a, 2) ~ rotr(a, 13) ~ rotr(a, 22)
      local maj = (a & b) ~ (a & c) ~ (b & c)
      local t2 = (S0 + maj) & MASK
      h, g, f, e, d, c, b, a = g, f, e, (d + t1) & MASK, c, b, a, (t1 + t2) & MASK
    end
    h0, h1, h2, h3 = (h0 + a) & MASK, (h1 + b) & MASK, (h2 + c) & MASK, (h3 + d) & MASK
    h4, h5, h6, h7 = (h4 + e) & MASK, (h5 + f) & MASK, (h6 + g) & MASK, (h7 + h) & MASK
  end
  return string.format("%08x%08x%08x%08x%08x%08x%08x%08x", h0, h1, h2, h3, h4, h5, h6, h7)
end
M.sha256hex = sha256hex

-- refuseLossy names the read events that make a record unsafe to hash or
-- rewrite (docs/SPEC-TABLES.md §4, §6.6).
local function refuseLossy(report)
  local lost = report.unknown > 0 or report.kindMismatch > 0 or report.clamped > 0 or report.retainLost > 0
  if lost or report.malformed then
    return refuse(
      string.format("cards: record has loss events (unknown=%d kindMismatch=%d clamped=%d retainLost=%d "
        .. "malformed=%s): refused, never hashed or rewritten",
        report.unknown, report.kindMismatch, report.clamped, report.retainLost, tostring(report.malformed)),
      "unknown-present", report)
  end
  return nil
end

local function frame(text) return string.pack("<I8", #text) .. text end

local function framePaths(list)
  local out = { string.pack("<I8", #list) }
  for i, e in ipairs(list) do out[i + 1] = frame(e.s) end
  return table.concat(out)
end

-- TaskHash answers the hex digest of a record's TASK: body, paths, new, shared,
-- test and done in that order, each framed as an unsigned 64-bit little-endian
-- byte length then its bytes, each list as a count then its length-prefixed
-- elements; needs, tier, priority and machine fields are excluded
-- (docs/SPEC-TABLES.md §5; tla/CardHops.tla: TaskHashStable). A nil, malformed
-- or lossy record is refused with an error, never hashed.
function M.TaskHash(record)
  if record == nil or record.root == nil then return nil, refuse("cards: nil record", "malformed") end
  local lossy = refuseLossy(record.report)
  if lossy ~= nil then return nil, lossy end
  local root = record.root
  return sha256hex(frame(unbox(root.body) or "") .. framePaths(root.paths) .. framePaths(root._new)
    .. framePaths(root.shared) .. frame(root.test or "") .. frame(unbox(root.done) or ""))
end

-- ---- the field edits (tables/cards/edit.go) ---------------------------------------------

local function validateStrings(name, values, bound)
  for _, s in ipairs(values) do
    if #s > bound then
      return refuse(string.format("cards: %s element is %d bytes, past its %d-byte bound", name, #s, bound),
        "malformed")
    end
    if not wellFormed(s) then
      return refuse("cards: " .. name .. " has an element that is not well-formed UTF-8 without NUL", "malformed")
    end
  end
  return nil
end

-- edit is the shared atomic rewrite: check the current record is safe, reload it
-- into a fresh independent decode, apply mutate to the root, validate the whole
-- rebuilt card, pack it, and only on complete success swap the record's owned
-- wire (docs/SPEC-TABLES.md §6.2). Any refusal leaves the original wire
-- untouched (tla/CardHops.tla: EditDropsUnknown, BodyUnchanged).
local function edit(record, mutate)
  if record == nil or record.root == nil then return nil, refuse("cards: nil record", "malformed") end
  local lossy = refuseLossy(record.report)
  if lossy ~= nil then return nil, lossy end
  local report = newReport()
  local root = load_mod.LoadTolerant(T, ROOT, record.wire, report)
  if root == nil then return nil, refuse("cards: reload into the fresh decode failed", "malformed") end
  lossy = refuseLossy(report)
  if lossy ~= nil then return nil, lossy end
  mutate(root)
  local cerr = checkCard(root)
  if cerr ~= nil then return nil, cerr end
  local wire, serr = save(root)
  if wire == nil then return nil, serr end
  record.wire, record.root, record.report = wire, root, report
  return true
end

local function setList(field, bound)
  return function(record, values)
    local err = validateStrings(field, values, bound)
    if err ~= nil then return nil, err end
    if #values > M.MaxListEntries then return nil, listBound(field, #values) end
    return edit(record, function(root) root[field] = rows(values) end)
  end
end

M.SetNeeds = setList("needs", 64)
M.SetPaths = setList("paths", 192)

local function setVariant(field, allowed)
  return function(record, value)
    if not allowed[value] then
      return nil, refuse(string.format("cards: %s %s is not a declared variant (docs/SPEC-TABLES.md §8)", field, value),
        "malformed")
    end
    return edit(record, function(root) root[field] = value end)
  end
end

M.SetTier = setVariant("tier", TIERS)
M.SetPriority = setVariant("priority", PRIORITIES)
M.SetColumn = setVariant("column", COLUMNS)

function M.SetHolder(record, holder)
  local err = validateStrings("holder", { holder }, 32)
  if err ~= nil then return nil, err end
  return edit(record, function(root) root.holder = holder end)
end

return M
