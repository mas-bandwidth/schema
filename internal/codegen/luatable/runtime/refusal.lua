-- refusal.lua — the Lua table runtime's STRICT READ (docs/SPEC-TABLES.md §6.5)
-- and its REFUSALS BY NAME (§11, §2.14). Emitted beside the unit's
-- <Package>Tables.lua by internal/codegen/luatable, which embeds runtime/*.lua
-- by glob.
--
-- ReadStrict sits over the tolerant load (runtime/load.lua, the variable form)
-- and over the fixed form's reader (runtime/fixed.lua, form 3). It refuses, by
-- the §6.5/§11 name, a wire whose report moved any of malformed, clamped,
-- kind_mismatch, duplicate or unknown, a form byte that belongs to another
-- registry entry, and a declaring root's file whose CRC-32C trailer does not
-- match (§2.14). Every error names the counter and the first field the load
-- touched, so a caller reads the same vocabulary a Go caller matches with
-- errors.Is.
local load = require("load")

local R = {}

-- THE REPORT'S FIVE (§4). malformed is the report's boolean; the other four
-- are counters, and each is refused with its §6.5 spelling.
local counters = {
  { key = "clamped", name = "clamped" },
  { key = "kindMismatch", name = "kind_mismatch" },
  { key = "duplicate", name = "duplicate" },
  { key = "unknown", name = "unknown" },
}

local function refuse(name, field)
  error(name .. ": " .. (field or "wire"), 0)
end

-- CRC-32C, the Castagnoli polynomial reflected (0x82F63B78) with the
-- initial value and final XOR the §2.14 vocabulary names. The table is built
-- once, on the first declaring root read.
local crc_table = nil

local function crc32c(bytes)
  if crc_table == nil then
    crc_table = {}
    for i = 0, 255 do
      local c = i
      for _ = 1, 8 do
        if c & 1 == 1 then c = (c >> 1) ~ 0x82f63b78 else c = c >> 1 end
      end
      crc_table[i] = c
    end
  end
  local crc = 0xffffffff
  for i = 1, #bytes do
    crc = crc_table[(crc ~ bytes:byte(i)) & 0xff] ~ (crc >> 8)
  end
  return crc ~ 0xffffffff
end

-- THE DESCRIPTOR/REPORT PROXIES record the FIRST field an increment names.
-- A counter is written after the field it belongs to has been touched, so the
-- proxy remembers the last descriptor field read and the report proxy stamps
-- it onto the first nonzero write of every counter. Nothing in the tolerant
-- load changes: the walk still reads the same descriptors, through a table
-- that answers with their own values.
local function instrument(T, report, first)
  local current = nil
  local function field(f)
    return setmetatable({}, {
      __index = function(_, k)
        current = f.name
        return f[k]
      end,
    })
  end
  local descs = {}
  for name, d in pairs(T.Tables) do
    local fields = {}
    for i, f in ipairs(d.fields) do fields[i] = field(f) end
    descs[name] = setmetatable({}, {
      __index = function(_, k)
        if k == "fields" then return fields end
        if k == "name" then current = d.name end
        return d[k]
      end,
    })
  end
  local proxy = setmetatable({}, {
    __index = function(_, k)
      if k == "Tables" then return descs end
      return T[k]
    end,
  })
  local seen = {}
  for _, c in ipairs(counters) do seen[c.key] = true end
  seen.malformed = true
  local wrapped = setmetatable({}, {
    __index = report,
    __newindex = function(t, k, v)
      local before = report[k]
      if seen[k] and v ~= before and first[k] == nil then first[k] = current end
      report[k] = v
    end,
  })
  return proxy, wrapped
end

-- ReadStrict(T, name, bytes [, integrity]) reads one root strictly
-- (docs/SPEC-TABLES.md §6.5). It returns the root or raises an error whose
-- message begins with the refusal's name: previous_form, unknown_form,
-- message_form_as_file, integrity_mismatch, malformed, clamped, kind_mismatch,
-- duplicate or unknown. `integrity` is the root's declared check (§2.14),
-- "crc32c" when the unit declares one.
function R.ReadStrict(T, name, bytes, integrity)
  if type(bytes) ~= "string" then refuse("malformed", name) end
  local form = #bytes >= 1 and bytes:byte(1) or nil
  local fixed = T.Fixed and T.Fixed[name]

  -- THE FORM BYTE IS READ FIRST (§2.14's order, §6.5). Form 1 is the file
  -- form EVERY table carries, fixed class or not; form 3 is the fixed form
  -- and only a root with a compiled layout reads it; a form-3 file at a root
  -- with none is previous_form, and a byte outside the registry is named.
  if form == nil then refuse("malformed", name) end
  if form == 3 then
    if fixed == nil then refuse("previous_form", name) end
    return T.ReadFixed(name, bytes)
  end
  if form ~= 1 then
    if form == 2 then refuse("message_form_as_file", name) end
    refuse("unknown_form", "form " .. string.format("%d", form))
  end

  if integrity ~= nil then
    if integrity ~= "crc32c" then refuse("malformed", name) end
    if #bytes < 5 then refuse("integrity_mismatch", name) end
    local want = string.unpack("<I4", bytes, #bytes - 3)
    local got = crc32c(bytes:sub(1, #bytes - 4))
    if want ~= got then refuse("integrity_mismatch", name) end
    bytes = bytes:sub(1, #bytes - 4)
  end

  local report = {
    malformed = false, refused = 0, unknown = 0, kindMismatch = 0,
    clamped = 0, widened = 0, duplicate = 0,
  }
  local first = {}
  local proxied, wrapped = instrument(T, report, first)
  local root = load.Load(proxied, name, bytes, wrapped)

  if report.refused ~= 0 then refuse("unknown_form", name) end
  if report.malformed then refuse("malformed", first.malformed or name) end
  for _, c in ipairs(counters) do
    if report[c.key] > 0 then refuse(c.name, first[c.key] or name) end
  end
  if root == nil then refuse("malformed", name) end
  return root
end

return R
