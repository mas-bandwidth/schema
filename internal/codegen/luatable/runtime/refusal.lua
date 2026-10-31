-- refusal.lua — the Lua table runtime's STRICT READ (docs/SPEC-TABLES.md §6.5)
-- and its REFUSALS BY NAME (§11, §2.14). Emitted beside the unit's
-- <Package>Tables.lua by internal/codegen/luatable, which embeds runtime/*.lua
-- by glob.
--
-- ReadStrict sits over the tolerant load (runtime/load.lua, the variable form)
-- and over the fixed form's reader (runtime/fixed.lua, form 3). It refuses,
-- with ONE typed reason in TableReport.Err()'s order, a read that is of an
-- unknown form, integrity-mismatched, wire-damaged, missing a required field,
-- malformed, blob-shared, clamped or kind-mismatched, or that holds a float
-- whose bits are a NaN or an infinity (non_finite_float, which every read
-- refuses and which outranks every later verdict; §6.5, §11, §2.3, §2.5,
-- §3.3). Unknown, widened and duplicate
-- ALONE read clean: they are evolution, exact decoding and data identity, not
-- loss.
local load = require("load")

local R = {}

-- THE REPORT'S COUNTERS, in TableReport.Err()'s order (internal/codegen/
-- gotable/refusal.go). malformed is the report's boolean; the others are
-- counters, and each is refused with its §6.5 spelling.
local counters = {
  { key = "blobShared", name = "blob_shared" },
  { key = "clamped", name = "clamped" },
  { key = "kindMismatch", name = "kind_mismatch" },
}

local function refuse(name, _field)
  error(name, 0)
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
    for i, f in ipairs(d.fields) do
      fields[i] = field(f)
    end
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
  for _, c in ipairs(counters) do
    seen[c.key] = true
  end
  seen.malformed = true
  local wrapped = setmetatable({}, {
    __index = report,
    __newindex = function(_t, k, v)
      local before = report[k]
      if seen[k] and v ~= before and first[k] == nil then first[k] = current end
      report[k] = v
    end,
  })
  return proxy, wrapped
end

-- ReadStrict(T, name, bytes [, integrity]) reads one root strictly
-- (docs/SPEC-TABLES.md §6.5). It returns the root or raises an error whose
-- message begins with the refusal's name: unknown_form, integrity_mismatch,
-- wire_damaged, missing_required, malformed, blob_shared, clamped,
-- kind_mismatch or non_finite_float.
-- The form-byte reasons previous_form, newer_form and message_form_as_file are
-- the REPORT's reasons; TableReport.Err() folds them all into unknown_form.
-- `integrity` is the root's declared check (§2.14), "crc32c" when the unit
-- declares one.
function R.ReadStrict(T, name, bytes, integrity)
  if type(bytes) ~= "string" then refuse("wire_damaged", name) end
  local form = #bytes >= 1 and bytes:byte(1) or nil
  local fixed = T.Fixed and T.Fixed[name]

  -- THE FORM BYTE IS READ FIRST (§2.14's order, §6.5). Form 1 is the file
  -- form EVERY table carries, fixed class or not; form 3 is the fixed form
  -- and only a root with a compiled layout reads it. Every other byte is the
  -- one strict reason unknown_form, as TableReport.Err() answers for
  -- previous_form, newer_form and message_form_as_file alike.
  if form ~= nil and form == 3 and fixed ~= nil then return T.ReadFixed(name, bytes) end
  if form ~= nil and form ~= 1 then refuse("unknown_form", "form " .. string.format("%d", form)) end

  -- THE REQUIRED CHECK IS THE DESCRIPTOR'S (§2.14): a declaring root's check
  -- rides in T.Tables[name].integrity, so a caller need not pass it. The
  -- argument still works for a caller that names the check itself. A short
  -- form-1 file under a declaring root is integrity_mismatch, before any
  -- framing rule; an empty file is wire damage, which the load below reports.
  local declared = T.Tables[name] and T.Tables[name].integrity or nil
  local check = declared or integrity
  if check ~= nil and #bytes >= 1 and bytes:byte(1) == 1 then
    if check ~= "crc32c" then refuse("malformed", name) end
    if #bytes < 5 then refuse("integrity_mismatch", name) end
    local want = string.unpack("<I4", bytes, #bytes - 3)
    if want ~= load.crc32c(bytes:sub(1, #bytes - 4)) then refuse("integrity_mismatch", name) end
    -- A declaring root hands the file to the load, which strips and checks the
    -- trailer itself; only an argument with no declaration is stripped here.
    if declared == nil then bytes = bytes:sub(1, #bytes - 4) end
  end

  local report = {
    malformed = false,
    refused = 0,
    unknown = 0,
    kindMismatch = 0,
    clamped = 0,
    widened = 0,
    duplicate = 0,
    blobShared = 0,
    missingRequired = 0,
    wireDamaged = false,
    reason = "",
  }
  local first = {}
  local proxied, wrapped = instrument(T, report, first)
  local root = load.LoadTolerant(proxied, name, bytes, wrapped)

  -- TableReport.Err()'s order (§6.5): unknown form, integrity mismatch, a
  -- non-finite float (§3.3: it outranks every verdict the walk reached after
  -- it), wire damage, missing_required, malformed, blob_shared, clamped,
  -- kind_mismatch.
  if report.refused ~= 0 then refuse("unknown_form", name) end
  if report.reason == "integrity_mismatch" then refuse("integrity_mismatch", name) end
  if report.nonFiniteFloat ~= nil then refuse("non_finite_float", report.nonFiniteFloat) end
  if report.wireDamaged then refuse("wire_damaged", name) end
  if report.missingRequired > 0 then refuse("missing_required", first.missingRequired or name) end
  if report.malformed then refuse("malformed", first.malformed or name) end
  for _, c in ipairs(counters) do
    if report[c.key] > 0 then refuse(c.name, first[c.key] or name) end
  end
  if root == nil then refuse("wire_damaged", name) end
  return root
end

return R
