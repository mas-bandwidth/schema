-- wirefuzz.lua — the LUA WIRE-FUZZ DRIVER (test/conformance/README.md, "The
-- wire-fuzz driver"; docs/SPEC-TABLES.md §4.2). ONE process on a pipe: the
-- harness owns the mutants and the oracle, and this leg binds a roster entry to
-- its generated table module and answers, per mutant, the load verdict, the six
-- counters, malformed and refused, the region measure, and the re-saved bytes.
-- It decides nothing about what a mutant means.
--
-- Host shape (docs/PORT-LUA.md, THE HARNESS): the driver is one lua process
-- reading a little-endian stream on stdin, as main.lua's driver is for the
-- conformance surfaces. The generated units live one directory per unit key
-- under the single argument (build/conformance-lua), which goes on package.path.

local rootdir = arg[1]
if rootdir == nil then
  io.stderr:write("wirefuzz.lua: need the generated units directory\n")
  os.exit(2)
end
package.path = rootdir .. "/?.lua;" .. package.path

local function u16()
  local a, b = io.stdin:read(1, 1)
  if not a then return nil end
  return a:byte() + b:byte() * 256
end
local function u32()
  local a, b, c, d = io.stdin:read(1, 1, 1, 1)
  if not a then return nil end
  return a:byte() + b:byte() * 256 + c:byte() * 65536 + d:byte() * 16777216
end
local function name()
  local n = u16()
  if not n then return nil end
  local s = io.stdin:read(n)
  if not s or #s ~= n then return nil end
  return s
end

local function put_u8(v) io.write(string.char(v & 0xff)) end
local function put_i32(v)
  v = v & 0xffffffff
  for i = 0, 3 do io.write(string.char((v >> (8 * i)) & 0xff)) end
end
local function put_i64(v)
  v = v & 0xffffffffffffffff
  for i = 0, 7 do io.write(string.char((v >> (8 * i)) & 0xff)) end
end

-- one unit key to its generated table module: the directory build/
-- conformance-lua/<key> holds <Package>Tables.lua and the runtime files
local modules = {}
local function load_module(key)
  if modules[key] ~= nil then return modules[key] end
  local dir = rootdir .. "/" .. key
  local md = nil
  local ok = pcall(function()
    for entry in io.popen("ls " .. dir):lines() do
      if entry:match("^%w+Tables%.lua$") then
        md = entry:gsub("%.lua$", "")
        break
      end
    end
  end)
  if not ok or md == nil then
    modules[key] = false
    return false
  end
  local ok2, tbl = pcall(require, md)
  if not ok2 then
    modules[key] = false
    return false
  end
  modules[key] = tbl
  return tbl
end

local count = u32()
if not count then os.exit(1) end
local roster = {} -- index -> { unit, root }
local available = {}
for i = 1, count do
  local unit = name()
  local root = name()
  local form = io.stdin:read(1)
  local retain = io.stdin:read(1)
  local ok = false
  if unit and root and form and form:byte() == 1 and retain:byte() == 0 then
    local tbl = load_module(unit)
    if tbl and tbl.Tables and tbl.Tables[root] then ok = true end
  end
  roster[i] = { unit = unit, root = root }
  available[i] = ok
  io.write(ok and "\1" or "\0")
end
io.flush()

while true do
  local index = u32()
  if not index then break end
  local size = u32()
  if not size then break end
  local wire = io.stdin:read(size)
  if not wire or #wire ~= size then break end

  local loaded = 0
  local unknown, km, widened, clamped, duplicate = 0, 0, 0, 0, 0
  local malformed, refused = false, false
  local measure = -1
  local saved = -1
  local savedBytes = nil

  local ent = roster[index + 1]
  if available[index + 1] and ent then
    local tbl = load_module(ent.unit)
    local loadr = require("load")
    local saver = require("save")
    local report = { malformed = false, refused = 0, unknown = 0, kindMismatch = 0,
                     clamped = 0, widened = 0, duplicate = 0 }
    local ok, v = pcall(loadr.Load, tbl, ent.root, wire, report)
    if ok and v ~= nil then
      loaded = 1
      unknown, km = report.unknown, report.kindMismatch
      widened, clamped, duplicate = report.widened, report.clamped, report.duplicate
      malformed = report.malformed
      refused = report.refused > 0
      local ok2, b = pcall(saver.Save, tbl, ent.root, v)
      if ok2 and b ~= nil then
        saved = #b
        savedBytes = b
      end
    end
  end

  put_u8(loaded)
  put_i32(unknown)
  put_i32(km)
  put_i32(widened)
  put_i32(clamped)
  put_i32(duplicate)
  put_u8(malformed and 1 or 0)
  put_u8(refused and 1 or 0)
  put_i64(measure)
  put_i32(0)
  put_i32(0)
  put_i64(saved)
  if saved > 0 then io.write(savedBytes) end
  io.flush()
end
