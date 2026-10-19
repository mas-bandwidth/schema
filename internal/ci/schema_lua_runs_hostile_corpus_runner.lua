-- The Lua generated-reader leg of docs/HOSTILE-CORPUS.md. This runner uses
-- only committed files and the generated module's own readers.
local manifestPath, corpusDir, unitsPath, outDir = arg[1], arg[2], arg[3], arg[4]
if not manifestPath or not corpusDir or not unitsPath or not outDir then
  io.stderr:write("usage: runner.lua manifest corpus units.tsv answers-dir\n")
  os.exit(2)
end

local function read(path)
  local f, err = io.open(path, "rb")
  if not f then error(err or ("cannot open " .. path)) end
  local data = f:read("*a")
  f:close()
  return data
end

local function fields(line)
  local out = {}
  for field in (line .. "\t"):gmatch("(.-)\t") do out[#out + 1] = field end
  return out
end

local units = {}
for line in io.lines(unitsPath) do
  local f = fields(line)
  if #f >= 4 then
    units[f[1]] = { dir = f[2], module = f[3], unavailable = f[4] }
  end
end

local function caseResult(c)
  local unit = units[c[3]]
  if not unit then return "refuse", "reader_unavailable", nil end
  if unit.unavailable ~= "" then return "refuse", "reader_unavailable", nil end
  package.path = unit.dir .. "/?.lua;" .. package.path
  local T = require(unit.module)
  local J, L, M = require("json"), require("load"), require("message")
  local payload = read(corpusDir .. "/" .. c[5])
  local report = { malformed = false, refused = 0, unknown = 0, kindMismatch = 0,
    clamped = 0, widened = 0, duplicate = 0, reason = "" }
  local value, reason

  if c[2] == "text" then
    value = J.FromJson(T, c[4], payload, report)
    if report.malformed then return "refuse", "damaged", nil end
  elseif c[2] == "wire" then
    value = L.Load(T, c[4], payload, report)
    if report.reason and report.reason ~= "" then
      return "refuse", report.reason, nil
    end
    if report.refused > 0 then
      local form = #payload > 0 and payload:byte(1) or 0
      reason = form == 2 and "message_form_as_file" or "newer_form"
      return "refuse", reason, nil
    end
    if report.malformed or value == nil then return "refuse", "damaged", nil end
  elseif c[2] == "announce" or c[2] == "announce_bytes" or c[2] == "announce_entries" or c[2] == "announce_second" then
    local vocab, first = {}, { malformed = false, refused = 0, reason = "" }
    if not M.announceRead(T, vocab, payload, first) then
      return "refuse", first.reason ~= "" and first.reason or "damaged", nil
    end
    if c[2] == "announce_second" then
      local second = { malformed = false, refused = 0, reason = "" }
      if M.announceRead(T, vocab, payload, second) then return "accept", "-", nil end
      return "refuse", second.reason ~= "" and second.reason or "damaged", nil
    end
    -- Lua's generated announcement reader has no bounded-receiver options;
    -- report its actual result. The two corpus bound cases expose that gap.
    return "accept", "-", nil
  elseif c[2] == "message" or c[2] == "message_novocab" then
    local vocab = {}
    if c[2] == "message" then
      local announced = { malformed = false, refused = 0, reason = "" }
      if not M.announceRead(T, vocab, T.Announcement, announced) then
        return "refuse", announced.reason ~= "" and announced.reason or "damaged", nil
      end
    end
    value = M.loadMessages(T, c[4], vocab, payload, report)
    if report.reason and report.reason ~= "" then
      return "refuse", report.reason, nil
    end
    if report.refused > 0 then return "refuse", "damaged", nil end
    if report.malformed or value == nil or value[1] == nil then return "refuse", "damaged", nil end
    value = value[1]
  else
    return "refuse", "unknown_carrier", nil
  end

  local canonical = J.ToJson(T, c[4], value)
  if canonical == nil then return "refuse", "damaged", nil end
  return "accept", "-", canonical
end

local planted = os.getenv("HOSTILE_LUA_PLANT") ~= nil
for raw in io.lines(manifestPath) do
  if raw ~= "" and raw:sub(1, 1) ~= "#" then
    local c = fields(raw)
    local verdict, reason, canonical = "refuse", "reader_error", nil
    local ok, v, r, text = pcall(caseResult, c)
    if ok then verdict, reason, canonical = v, r, text end
    -- Negative control: perturb one scratch answer after the real reader has
    -- run, and require the Go test's manifest comparison to catch the change.
    if planted and c[1] == "wire-form-ff" then verdict, reason, canonical = "refuse", "damaged", nil end
    if canonical ~= nil then
      local f = assert(io.open(outDir .. "/" .. c[1], "wb"))
      f:write(canonical)
      f:close()
    end
    io.write("CASE\t", c[1], "\t", verdict, "\t", reason, "\n")
  end
end
