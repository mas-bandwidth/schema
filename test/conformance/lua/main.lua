-- THE LUA CONFORMANCE DRIVER (test/conformance/README.md; docs/PORT-LUA.md,
-- THE HARNESS). The driver script execs this file once per surface:
--
--   driver <manifest> list
--   driver <manifest> <surface> <outdir>
--
-- The working directory is the repository root, which is the contract: every
-- path in the manifest is repo-relative, so this driver never resolves one.
-- The manifest is DERIVED (the harness removed every expected answer), so a
-- driver cannot pass by reading the answer.
--
-- The generated tree is a PATH, not a static require, for the same reason the
-- JavaScript leg's is (test/conformance/js/main.mjs): the negative control
-- points this same driver at a sabotaged copy of the generated modules and
-- requires the matrix to go red. LUA_CONFORMANCE_GENERATED names it; the
-- default is what the Makefile writes (build/conformance-lua).

local GENERATED = os.getenv("LUA_CONFORMANCE_GENERATED") or "build/conformance-lua"
local SURFACES = { report = true, wire = true, ["json-hostile"] = true }

local function lines(manifest)
  local f = assert(io.open(manifest, "r"), "open " .. manifest)
  local out = {}
  for raw in f:lines() do
    local text = raw:gsub("^%s+", ""):gsub("%s+$", "")
    if #text > 0 and text:sub(1, 1) ~= "#" then
      local parts = {}
      for word in text:gmatch("%S+") do parts[#parts + 1] = word end
      out[#out + 1] = parts
    end
  end
  f:close()
  return out
end

local function unitModule(unit)
  -- the unit's table module is <Package>Tables.lua beside its runtime; find it
  -- by glob so the package name never has to be guessed here
  local p = io.popen("ls " .. GENERATED .. "/" .. unit .. "/*Tables.lua 2>/dev/null | head -1")
  local name = p:read("*l")
  p:close()
  return name
end

local function loadUnit(unit)
  local modpath = unitModule(unit)
  if not modpath then
    return nil
  end
  local dir = modpath:match("^(.*)/[^/]+$")
  package.path = dir .. "/?.lua;" .. package.path
  local module = modpath:match("([^/]+)%.lua$")
  return require(module), require("load"), require("save")
end

local function readBytes(path)
  local f = assert(io.open(path, "rb"))
  local data = f:read("*a")
  f:close()
  return data
end

local function writeBytes(path, data)
  local f = assert(io.open(path, "wb"))
  f:write(data)
  f:close()
end

local function reportLine(r)
  local verdict = r.refused > 0 and "refused" or "read"
  return string.format("%d,%d,%d,%d,%d,%s,%s\n",
    r.unknown, r.kindMismatch, r.widened, r.clamped,
    r.duplicate, tostring(r.malformed), verdict)
end

local function surfaceReport(manifest, outdir)
  for _, f in ipairs(lines(manifest)) do
    if f[1] == "report" and #f >= 5 then
      local T, load = loadUnit(f[3])
      if not T then
        writeBytes(outdir .. "/" .. f[2] .. ".absent",
          "unit not generated for lua: docs/SPEC-TABLES.md §15\n")
      else
        local r = { malformed = false, refused = 0, unknown = 0, kindMismatch = 0, clamped = 0, widened = 0, duplicate = 0 }
        load.Load(T, f[4], readBytes(f[5]), r)
        writeBytes(outdir .. "/" .. f[2], reportLine(r))
      end
    end
  end
end

-- The WIRE surface loads a form-1 wire and re-saves it (docs/SPEC-TABLES.md
-- §3). A case whose unit the lua target does not generate is answered
-- `<case>.absent` — the matrix counts it as the row of work left, never as a
-- pass (test/conformance/README.md).
local function surfaceWire(manifest, outdir)
  for _, f in ipairs(lines(manifest)) do
    if f[1] == "instance" then
      local T, load, save = loadUnit(f[3])
      if not T then
        writeBytes(outdir .. "/" .. f[2] .. ".absent",
          "unit not generated for lua: docs/SPEC-TABLES.md §15\n")
      else
        local r = { malformed = false, refused = 0, unknown = 0, kindMismatch = 0, clamped = 0, widened = 0, duplicate = 0 }
        local v = load.Load(T, f[4], readBytes(f[5]), r)
        if v == nil then
          error("load refused " .. f[5])
        end
        local saved = save.Save(T, f[4], v)
        writeBytes(outdir .. "/" .. f[2], saved)
      end
    end
  end
end

-- THE JSON-HOSTILE surface (test/conformance/README.md): FromJson of each
-- hostile tree, writing the report line, or `refused` when the read refuses
-- (docs/SPEC-TABLES.md §16.2, §16.3, §4).
local function surfaceJsonHostile(manifest, outdir)
  for _, f in ipairs(lines(manifest)) do
    if f[1] == "json-hostile" and #f >= 5 then
      local T = loadUnit(f[3])
      if not T then
        writeBytes(outdir .. "/" .. f[2] .. ".absent",
          "unit not generated for lua: docs/SPEC-TABLES.md §15\n")
      else
        local J = require("json")
        local text = readBytes(f[5] .. "/" .. f[4] .. ".json")
        local r = { malformed = false, refused = 0, unknown = 0, kindMismatch = 0, clamped = 0, widened = 0, duplicate = 0 }
        J.FromJsonTolerant(T, f[4], text, r)
        if r.malformed then
          writeBytes(outdir .. "/" .. f[2], "refused\n")
        else
          writeBytes(outdir .. "/" .. f[2], reportLine(r))
        end
      end
    end
  end
end

local function usage()
  io.stderr:write("usage: driver <manifest> list\n       driver <manifest> <surface> <outdir>\n")
  os.exit(2)
end

if #arg < 2 then usage() end
local surface = arg[2]

if surface == "list" then
  for name in pairs(SURFACES) do io.write(name .. "\n") end
  os.exit(0)
end

if #arg < 3 then usage() end
if not SURFACES[surface] then os.exit(2) end

if surface == "report" then surfaceReport(arg[1], arg[3]) end
if surface == "wire" then surfaceWire(arg[1], arg[3]) end
if surface == "json-hostile" then surfaceJsonHostile(arg[1], arg[3]) end
os.exit(0)
