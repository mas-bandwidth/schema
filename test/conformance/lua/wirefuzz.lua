-- wirefuzz.lua — THE LUA LEG's WIRE-FUZZ DRIVER (test/conformance/README.md,
-- "The wire-fuzz driver"; docs/SPEC-TABLES.md §4.2, the read is the verifier).
-- ONE lua process for the whole run, on a pipe, that only reads: the harness
-- generates every mutant and holds the oracle; this leg loads each mutant
-- through the generated tolerant Load, saves whatever it decoded, and answers
-- what happened. It decides nothing about what a mutant means.
--
--   lua test/conformance/lua/wirefuzz.lua [<generated dir>]
--
-- The host shape is docs/PORT-LUA.md's THE HARNESS, the one main.lua uses: the
-- working directory is the repository root, the generated tree is a PATH (the
-- argument, else LUA_CONFORMANCE_GENERATED, else build/conformance-lua), one
-- directory per unit key holding <Package>Tables.lua beside the table runtime.
-- A unit the target refused is a `<key>.refused` file there, not a directory,
-- so its roots answer 0 in the roster.
--
-- The stream is little-endian throughout (README, the table):
--   in   u32 roster count, then per root: u16 n + unit key, u16 n + root name,
--        u8 FORM, u8 RETAIN
--   out  one byte per roster entry: 1 when this leg has a codec for it
--   in   per mutant: u32 roster index, u32 length, the bytes, until EOF
--   out  per mutant: u8 loaded; i32 unknown, kind_mismatch, widened, clamped,
--        duplicate; u8 malformed; u8 refused; i64 measure; i32 retained,
--        retain_lost; i64 saved, then that many bytes

local GENERATED = arg[1] or os.getenv("LUA_CONFORMANCE_GENERATED") or "build/conformance-lua"

local stdin, stdout = io.stdin, io.stdout
stdout:setvbuf("full")

-- exactly n bytes from the pipe, or nil at EOF
local function read_exact(n)
  if n == 0 then return "" end
  local s = stdin:read(n)
  if s == nil or #s ~= n then return nil end
  return s
end

local function read_u16()
  local s = read_exact(2)
  return s and string.unpack("<I2", s)
end

local function read_u32()
  local s = read_exact(4)
  return s and string.unpack("<I4", s)
end

local function read_name()
  local n = read_u16()
  if n == nil then return nil end
  return read_exact(n)
end

-- THE UNIT, its own copy of the runtime (docs/PORT-LUA.md, "How a module
-- requires its runtime"): every unit directory carries its own load.lua and
-- save.lua, and a control may sabotage one unit's copy, so a unit's modules
-- are required with ONLY its directory on package.path and then dropped from
-- package.loaded, never shared with the next unit by a cached require.
local units = {}

local function table_module(key)
  local p = io.popen("ls " .. GENERATED .. "/" .. key .. "/*Tables.lua 2>/dev/null")
  if p == nil then return nil end
  local path = p:read("*l")
  p:close()
  return path
end

local function load_unit(key)
  local u = units[key]
  if u ~= nil then return u end
  units[key] = false
  if not key:match("^[%w_%-]+$") then return false end
  local path = table_module(key)
  if path == nil then return false end
  local dir, module = path:match("^(.*)/([^/]+)%.lua$")
  local before = {}
  for name in pairs(package.loaded) do before[name] = true end
  local saved_path = package.path
  package.path = dir .. "/?.lua"
  local ok, T, L, S = pcall(function()
    return require(module), require("load"), require("save")
  end)
  package.path = saved_path
  for name in pairs(package.loaded) do
    if not before[name] then package.loaded[name] = nil end
  end
  if not ok then
    io.stderr:write("wirefuzz.lua: unit " .. key .. ": " .. tostring(T) .. "\n")
    return false
  end
  u = { T = T, load = L, save = S }
  units[key] = u
  return u
end

-- THE ROSTER. A FORM-1 entry (a file, docs/SPEC-TABLES.md §3) whose unit this
-- target generated and whose table it carries answers 1. Every other entry
-- answers 0: a form-2 BATCH (this leg drives no LoadMessages here), and the
-- RETAIN arm (§6.6), which the Lua leg does not carry.
local count = read_u32()
if count == nil then os.exit(0) end
local roster = {}
local known = {}
for i = 1, count do
  local unit = read_name()
  local root = read_name()
  local head = read_exact(2)
  if unit == nil or root == nil or head == nil then
    io.stderr:write("wirefuzz.lua: the roster ended early\n")
    os.exit(2)
  end
  local form, retain = head:byte(1), head:byte(2)
  local entry = nil
  if form == 1 and retain == 0 then
    local u = load_unit(unit)
    if u and u.T.Tables and u.T.Tables[root] ~= nil then
      entry = { unit = u, root = root, desc = u.T.Tables[root] }
    end
  end
  roster[i] = entry
  known[i] = entry and "\1" or "\0"
end
stdout:write(table.concat(known))
stdout:flush()

-- ONE MUTANT. A VARIABLE root is sized by the runtime's LoadMeasure before it
-- is loaded, as a region caller sizes it (§6.5), and a refusal there means no
-- load; a FIXED root always loads and answers measure -1. A runtime that
-- carries no LoadMeasure answers -1 for every root, and the harness names that
-- disagreement itself; this driver never computes a measure of its own. Until
-- it does, a variable no-root reply reads to the harness as a LoadMeasure
-- refusal, whose branch compares no counters: the strict no-root check is the
-- in-package differential's (luatable TestLuaWireFuzzAgreesWithTheGoEngine).
-- A NaN OR AN INFINITY IS REFUSED BY EVERY READ (docs/SPEC-TABLES.md §3.3):
-- the tolerant load lands nothing and names the float in the report, and the
-- reply says so with the refusal verdict, as every leg's does.
local function answer(entry, wire)
  local report = { malformed = false, refused = 0, unknown = 0, kindMismatch = 0,
                   clamped = 0, widened = 0, duplicate = 0 }
  local loaded, measure, saved = false, -1, nil
  local T, L, S = entry.unit.T, entry.unit.load, entry.unit.save
  local measured = true
  if entry.desc.variable and L.LoadMeasure ~= nil then
    measure = L.LoadMeasure(T, entry.root, wire) or -1
    measured = measure >= 0
  end
  if measured then
    local v = L.LoadTolerant(T, entry.root, wire, report)
    if v ~= nil then
      loaded = true
      saved = S.Save(T, entry.root, v)
    end
  end
  return loaded, report, measure, saved
end

while true do
  local head = read_exact(8)
  if head == nil then break end
  local index, size = string.unpack("<I4I4", head)
  local wire = read_exact(size)
  if wire == nil then break end
  local entry = roster[index + 1]
  local loaded, report, measure, saved = false, nil, -1, nil
  if entry ~= nil then
    -- A RAISE IS A CRASH, attributed to this mutant: the reply is not written,
    -- the reason goes to stderr and the process exits, exactly as a native leg
    -- that faults (README: "a crash is attributed to the mutant that caused
    -- it"). A raise is never folded into a quiet "no root".
    local ok, a, b, c, d = pcall(answer, entry, wire)
    if not ok then
      stdout:flush()
      io.stderr:write("wirefuzz.lua: " .. entry.root .. ": the Lua load or save raised: " .. tostring(a) .. "\n")
      os.exit(3)
    end
    loaded, report, measure, saved = a, b, c, d
  end
  report = report or { malformed = false, refused = 0, unknown = 0, kindMismatch = 0,
                       clamped = 0, widened = 0, duplicate = 0 } -- an unknown entry
  stdout:write(string.pack("<Bi4i4i4i4i4BBi8i4i4i8",
    loaded and 1 or 0,
    report.unknown, report.kindMismatch, report.widened, report.clamped, report.duplicate,
    report.malformed and 1 or 0, ((report.refused or 0) > 0 or report.nonFiniteFloat ~= nil) and 1 or 0,
    measure, 0, 0, saved and #saved or -1))
  if saved ~= nil and #saved > 0 then stdout:write(saved) end
  stdout:flush()
end
