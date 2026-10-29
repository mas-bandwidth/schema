-- driver.lua — the Lua card conformance driver
-- (tables/cards/conformance/FORMAT.md, test/conformance/README.md "The shape").
--
-- It answers the driver contract — `<driver> <manifest> list` and
-- `<driver> <manifest> <surface> <outdir>` — over the nine surfaces this leg
-- implements (load, save, render, parse, bounds, upgrade, edit, taskhash, view)
-- through the facade in cards.lua.
--
-- The working directory is the repository root, which is the contract: every
-- path in the manifest is repo-relative, so this driver never resolves one.
-- The generated Card unit is a PATH, so the negative controls and the Makefile
-- can point the same driver at another tree: CARDS_LUA_GENERATED names it, the
-- default is what the Makefile writes (build/tables-generated-lua/cards).

local GENERATED = os.getenv("CARDS_LUA_GENERATED") or "build/tables-generated-lua/cards"

-- THE SEARCH PATH IS REPLACED, NEVER PREPENDED (docs/PORT-LUA.md, THE RUNTIME
-- LAYOUT): the generated tree and this facade's directory answer a require,
-- and nothing else does.
package.path = "tables/cards/lua/?.lua;" .. GENERATED .. "/?.lua"
package.cpath = ""

local cards = require("cards")
local tables = require("CardsTables")

local SURFACES = { "load", "save", "render", "parse", "bounds", "upgrade", "edit", "taskhash", "view" }

-- the test-only environment switches the negative controls set: each damages
-- ONE surface's answer so the check must go red on that surface alone
local sabotageRender = os.getenv("CARDS_LUA_SABOTAGE_RENDER") == "1"
local sabotageUpgrade = os.getenv("CARDS_LUA_SABOTAGE_UPGRADE") == "1"
local sabotageHash = os.getenv("CARDS_LUA_SABOTAGE_HASH") == "1"

local function fail(message)
  io.stderr:write("cards-driver: " .. tostring(message) .. "\n")
  os.exit(1)
end

local function readFile(path)
  local f = assert(io.open(path, "rb"))
  local data = f:read("a")
  f:close()
  return data
end

local function writeFile(path, data)
  local f = assert(io.open(path, "wb"))
  f:write(data)
  f:close()
end

local function readManifest(path)
  local lines = {}
  for raw in io.lines(path) do
    local text = raw:gsub("^%s+", ""):gsub("%s+$", "")
    if #text > 0 and text:sub(1, 1) ~= "#" then
      local parts = {}
      for word in text:gmatch("%S+") do parts[#parts + 1] = word end
      lines[#lines + 1] = parts
    end
  end
  return lines
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
local SHAPE_WORD = { array = "list", string_blob = "blob" }

local function view()
  local names = {}
  for name in pairs(tables.Tables) do names[#names + 1] = name end
  table.sort(names)
  local out = {}
  for _, name in ipairs(names) do
    for _, f in ipairs(tables.Tables[name].fields) do
      local shape = SHAPE_WORD[f.shape] or "value"
      local bound = "-"
      if shape == "value" and f.bound ~= nil and f.bound > 0 then bound = tostring(f.bound) end
      out[#out + 1] = string.format("%s %s %s %s %s\n", name, f.name, f.type_name, shape, bound)
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
    if record == nil then return refuse(outdir, name, err.class) end
    writeFile(outdir .. "/" .. name .. ".wire", record:Wire())
  elseif surface == "render" then
    local record, err = cards.Load(readFile(input))
    if record == nil then fail(err) end
    local text, rerr = cards.Render(record)
    if text == nil then fail(rerr) end
    if sabotageRender then text = "\0" .. text:sub(2) end
    writeFile(outdir .. "/" .. name .. ".txt", text)
  elseif surface == "parse" then
    local record, err = cards.Parse(readFile(input))
    if record == nil then return refuse(outdir, name, err.class) end
    writeFile(outdir .. "/" .. name .. ".wire", record:Wire())
  elseif surface == "upgrade" then
    local out, report = cards.Upgrade(readFile(input))
    if out == nil then return refuse(outdir, name, report.class) end
    if sabotageUpgrade then out = string.char(out:byte(1) ~ 0xff) .. out:sub(2) end
    writeFile(outdir .. "/" .. name .. ".wire", out)
    writeFile(outdir .. "/" .. name .. ".report", reportText(report))
  elseif surface == "edit" then
    local record, err = cards.Load(readFile(input))
    if record == nil then fail(err) end
    local rest = {}
    for k = 4, #c do rest[#rest + 1] = c[k] end
    local ok, eerr = applyEdit(record, rest)
    if not ok then return refuse(outdir, name, eerr.class) end
    writeFile(outdir .. "/" .. name .. ".wire", record:Wire())
  elseif surface == "taskhash" then
    local record, err = cards.Load(readFile(input))
    if record == nil then fail(err) end
    local hex, herr = cards.TaskHash(record)
    if hex == nil then fail(herr) end
    if sabotageHash then hex = string.rep("0", 64) end
    writeFile(outdir .. "/" .. name .. ".hex", hex)
  elseif surface == "view" then
    writeFile(outdir .. "/view.txt", view())
  end
end

local function main()
  local manifest, verb = arg[1], arg[2]
  if manifest == nil or verb == nil then
    io.stderr:write("usage: driver <manifest> list\n       driver <manifest> <surface> <outdir>\n")
    os.exit(2)
  end
  if verb == "list" then
    for _, s in ipairs(SURFACES) do io.stdout:write(s .. "\n") end
    os.exit(0)
  end
  local outdir = arg[3]
  if outdir == nil then
    io.stderr:write("usage: driver <manifest> <surface> <outdir>\n")
    os.exit(2)
  end
  local known = false
  for _, s in ipairs(SURFACES) do known = known or s == verb end
  if not known then os.exit(2) end
  for _, c in ipairs(readManifest(manifest)) do
    if c[1] == verb then one(verb, c, outdir) end
  end
end

main()
