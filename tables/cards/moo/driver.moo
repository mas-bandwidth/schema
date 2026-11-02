--!nonstrict
-- File IO and argument handling belong to the Go CLI host; all card behavior
-- and descriptor rendering run here, through the native moo facade.
local cards = require("./cards")
local tables = require("./CardTable")
local inputs = require("./inputs")
local function fail(message) error(tostring(message), 0) end
local function readFile(path) return assert(inputs.files[path], "missing input " .. path) end
local function writeFile(path: string, data: string)
  local hex = {}
  for i = 1, #data do hex[i] = string.format("%02x", data:byte(i)) end
  print("FILE " .. path .. " " .. table.concat(hex))
end
-- reportText writes the read report, one Name=value line per counter of the
-- generated report in declaration order (FORMAT.md, "The read report").
local function reportText(report)
  return string.format(
    "Retained=%d\nRetainLost=%d\nWidened=%d\nUnknown=%d\nKindMismatch=%d\nClamped=%d\nDuplicate=%d\nMalformed=%s\n",
    report.retained, report.retainLost, report.widened, report.unknown, report.kindMismatch, report.clamped,
    report.duplicate, tostring(report.malformed))
end

local function refuse(outdir, name, class) writeFile(outdir .. "/" .. name .. ".refused", class .. "\n") end

local function splitList(v)
  if v == "" then return {} end
  local out = {}
  for item in (v .. ","):gmatch("([^,]*),") do out[#out + 1] = item end
  return out
end

-- applyEdit applies the one Set* edit a manifest line carries; a refusal is
-- the facade's (nil, err), a malformed line is this driver's own.
local function applyEdit(record, edit)
  if #edit ~= 1 then return nil, { class = "malformed", message = "an edit line carries exactly one edit" } end
  local key, value = edit[1]:match("^([^=]*)=(.*)$")
  if key == nil then return nil, { class = "malformed", message = "edit " .. edit[1] .. " has no =" } end
  if key == "needs" then return cards.SetNeeds(record, splitList(value))
  elseif key == "paths" then return cards.SetPaths(record, splitList(value))
  elseif key == "tier" then return cards.SetTier(record, value)
  elseif key == "priority" then return cards.SetPriority(record, value)
  elseif key == "column" then return cards.SetColumn(record, value)
  elseif key == "holder" then return cards.SetHolder(record, value)
  end
  return nil, { class = "malformed", message = "unknown edit " .. key }
end

-- view renders the generated view: every field of every table in the unit, in
-- table-name order then declaration order, one line each (docs/SPEC-TABLES.md
-- §8; FORMAT.md "The view"). Every word comes from the descriptors: the type
-- name, the shape and the bound are the table module's own rows.
local function view()
  local descriptors = {}
  for _, value in pairs(tables) do
    if type(value) == "table" and value.fields ~= nil then descriptors[#descriptors + 1] = value end
  end
  table.sort(descriptors, function(a, b) return a.name < b.name end)
  local out = {}
  for _, desc in ipairs(descriptors) do
    for _, f in ipairs(desc.fields) do
      local shape, bound, name = "value", "-", f.t
      if f.s == "arr" then shape, name = "list", f.e.t
      elseif f.s == "sblob" then shape, name = "blob", "string"
      elseif f.s == "str" then name, bound = "string", tostring(f.bound)
      elseif f.s == "int" then name = (if f.signed then "int" else "uint") .. tostring(f.fw * 8)
      end
      assert(name ~= nil, "unhandled descriptor shape " .. f.s)
      out[#out + 1] = string.format("%s %s %s %s %s\n", desc.name, f.name, name, shape, bound)
    end
  end
  return table.concat(out)
end
local function one(surface, c, outdir)
  local name, input = c[2], c[3]
  if surface == "load" then
    local record, report = cards.Load(readFile(input))
    if record == nil then fail(report) end
    local text, err = cards.ToJSON(record)
    if text == nil then fail(err) end
    writeFile(outdir .. "/" .. name .. ".json", text)
    writeFile(outdir .. "/" .. name .. ".report", reportText(report))
  elseif surface == "save" or surface == "bounds" then
    local record, err = cards.FromJSON(readFile(input))
    if record == nil then refuse(outdir, name, err.class); return end
    writeFile(outdir .. "/" .. name .. ".wire", record:Wire())
  elseif surface == "render" then
    local record, err = cards.Load(readFile(input))
    if record == nil then fail(err) end
    local text, rerr = cards.Render(record)
    if text == nil then fail(rerr) end
    writeFile(outdir .. "/" .. name .. ".txt", text)
  elseif surface == "parse" then
    local record, err = cards.Parse(readFile(input))
    if record == nil then refuse(outdir, name, err.class); return end
    writeFile(outdir .. "/" .. name .. ".wire", record:Wire())
  elseif surface == "upgrade" then
    local out, report = cards.Upgrade(readFile(input))
    if out == nil then refuse(outdir, name, report.class); return end
    writeFile(outdir .. "/" .. name .. ".wire", out)
    writeFile(outdir .. "/" .. name .. ".report", reportText(report))
  elseif surface == "edit" then
    local record, err = cards.Load(readFile(input))
    if record == nil then fail(err) end
    local rest = {}
    for k = 4, #c do rest[#rest + 1] = c[k] end
    local ok, eerr = applyEdit(record, rest)
    if not ok then refuse(outdir, name, eerr.class); return end
    writeFile(outdir .. "/" .. name .. ".wire", record:Wire())
  elseif surface == "taskhash" then
    local record, err = cards.Load(readFile(input))
    if record == nil then fail(err) end
    local hex, herr = cards.TaskHash(record)
    if hex == nil then fail(herr) end
    writeFile(outdir .. "/" .. name .. ".hex", hex)
  elseif surface == "view" then
    writeFile(outdir .. "/view.txt", view())
  end
end

if inputs.surface == "unit" then
  local result = require("./cards_test")
  writeFile("unit.txt", result)
else
  for _, row in ipairs(inputs.rows) do one(inputs.surface, row, "") end
end
