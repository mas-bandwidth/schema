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
--
-- THE SEARCH PATH IS REPLACED, NEVER PREPENDED (docs/PORT-LUA.md, THE RUNTIME
-- LAYOUT). A unit is one directory, and loading it sets package.path to that
-- directory ALONE and package.cpath to nothing, so no module outside the
-- generated tree and no native module can answer a require. Every unit's
-- modules are required afresh into a clean package.loaded, because every unit
-- ships its own copy of the runtime under the same basenames.
--
-- THE POINTER COLUMN, IN A LANGUAGE WITH NO POINTERS. A forgery line carries
-- the buffer the caller holds — an aligned base, or that many bytes past one.
-- A Lua string has no address a caller can observe or place, so the block's
-- and the cook's Open take the LEAD beside the bytes, as the Elixir leg's do
-- (docs/SPEC-TABLES.md, the reading tier): stating the fact makes the check a
-- real one. No buffer at all is nil.

local GENERATED = os.getenv("LUA_CONFORMANCE_GENERATED") or "build/conformance-lua"

-- every surface test/conformance/README.md names, in its table's order
local SURFACE_ORDER = {
  "wire", "message", "report", "framing", "retain", "retain-save", "json-read",
  "json-write", "json-hostile", "cook", "cook-write", "cook-foreign", "block",
  "block-foreign", "block-dump", "forgery", "cook-forgery", "cook-reason",
  "block-reason",
}
local SURFACES = {}
for _, s in ipairs(SURFACE_ORDER) do SURFACES[s] = true end

local function fail(message)
  io.stderr:write("driver: " .. message .. "\n")
  os.exit(1)
end

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

local function kind(all, what)
  local out = {}
  for _, f in ipairs(all) do
    if f[1] == what then out[#out + 1] = f end
  end
  return out
end

local function readBytes(path)
  local f = io.open(path, "rb")
  if f == nil then return nil end
  local data = f:read("a")
  f:close()
  return data
end

local function mustRead(path)
  local data = readBytes(path)
  if data == nil then fail("cannot read " .. path) end
  return data
end

local function writeBytes(path, data)
  local f = assert(io.open(path, "wb"))
  f:write(data)
  f:close()
end

-- A LEG THAT CANNOT ANSWER ONE CASE SAYS SO BESIDE WHERE THE ANSWER WOULD GO
-- (test/conformance/README.md, "Exit codes"): `<case>.absent`, with the reason.
local function absent(outdir, name, reason)
  writeBytes(outdir .. "/" .. name .. ".absent", reason)
end

local NOT_GENERATED = "unit not generated for lua: docs/SPEC-TABLES.md §15"

---------------------------------------------------------------------------
-- the units
---------------------------------------------------------------------------

-- the runtime modules a surface may reach, required when the unit loads so no
-- later require runs against another unit's search path
local RUNTIME = {
  "load", "save", "json", "retain", "message", "cook",
  "cookwrite", "block",
}

-- the unit's table module is <Package>Tables.lua beside its runtime; the
-- schema's own type modules may end in "Tables" too, so the one that carries
-- the table descriptors is the one whose source declares them
local function tableModule(dir)
  local p = io.popen("ls " .. dir .. "/*Tables.lua 2>/dev/null")
  local found
  for name in p:lines() do
    local src = readBytes(name)
    if src ~= nil and src:find("\n_M.Tables = {}\n", 1, true) then
      found = name
      break
    end
  end
  p:close()
  return found
end

local baseline = {}
for name in pairs(package.loaded) do baseline[name] = true end

local units = {}

local function unit(key)
  local u = units[key]
  if u ~= nil then
    return u or nil
  end
  local dir = GENERATED .. "/" .. key
  local modpath = tableModule(dir)
  if modpath == nil then
    units[key] = false
    return nil
  end
  for name in pairs(package.loaded) do
    if not baseline[name] then package.loaded[name] = nil end
  end
  package.path = dir .. "/?.lua"
  package.cpath = ""
  u = { T = require(modpath:match("([^/]+)%.lua$")) }
  for _, m in ipairs(RUNTIME) do
    u[m] = require(m)
  end
  units[key] = u
  return u
end

local function newReport()
  return {
    malformed = false, refused = 0, unknown = 0, kindMismatch = 0, clamped = 0,
    widened = 0, duplicate = 0, retained = 0, retainLost = 0,
  }
end

local function reportLine(r)
  -- a non-finite float is refused by every read (docs/SPEC-TABLES.md §3.3)
  local verdict = (r.refused > 0 or r.nonFiniteFloat ~= nil) and "refused" or "read"
  return string.format("%d,%d,%d,%d,%d,%s,%s\n",
    r.unknown, r.kindMismatch, r.widened, r.clamped,
    r.duplicate, tostring(r.malformed), verdict)
end

---------------------------------------------------------------------------
-- the variable form and the text form (docs/SPEC-TABLES.md §3, §4, §16)
---------------------------------------------------------------------------

local function surfaceReport(all, outdir)
  for _, f in ipairs(kind(all, "report")) do
    local u = unit(f[3])
    if not u then
      absent(outdir, f[2], NOT_GENERATED)
    else
      local r = newReport()
      u.load.LoadTolerant(u.T, f[4], mustRead(f[5]), r)
      writeBytes(outdir .. "/" .. f[2], reportLine(r))
    end
  end
end

-- THE FRAMING VECTORS (docs/LAYERS/1-framing.md): the read report as the
-- report surface prints it, then the §16 text of the value the read left,
-- unless the row says `no-value` or the read refused or met damage. DAMAGE
-- DISCARDS THE WHOLE LOAD (docs/SPEC-TABLES.md §4): a damaged read owes the
-- report line saying malformed and nothing after it.
local function surfaceFraming(all, outdir)
  for _, f in ipairs(kind(all, "framing")) do
    local u = unit(f[3])
    if not u then
      absent(outdir, f[2], NOT_GENERATED)
    else
      local r = newReport()
      local v = u.load.LoadTolerant(u.T, f[4], mustRead(f[5]), r)
      local text = reportLine(r)
      local refused = r.refused > 0 or r.nonFiniteFloat ~= nil
      if v == nil and not refused then
        r.malformed = true
        text = reportLine(r)
      end
      if f[6] ~= "no-value" and not refused and not r.malformed then
        local json, why = u.json.ToJson(u.T, f[4], v)
        if json == nil then fail(f[2] .. ": ToJson refused the value: " .. tostring(why)) end
        text = text .. json
      end
      writeBytes(outdir .. "/" .. f[2], text)
    end
  end
end

-- The WIRE surface loads a form-1 wire and re-saves it (docs/SPEC-TABLES.md
-- §3). A writer refusal is a NAMED absence (the manifest's refused-by=
-- marker): Save answers nil and the refusal's name, e.g. blob_shared for one
-- blob two slots name (§2.5).
local function surfaceWire(all, outdir)
  for _, f in ipairs(kind(all, "instance")) do
    local u = unit(f[3])
    if not u then
      absent(outdir, f[2], NOT_GENERATED)
    else
      local v = u.load.LoadTolerant(u.T, f[4], mustRead(f[5]), newReport())
      if v == nil then fail(f[2] .. ": load refused " .. f[5]) end
      local saved, why = u.save.Save(u.T, f[4], v)
      if saved == nil then
        if why == nil then fail(f[2] .. ": Save refused without a name") end
        writeBytes(outdir .. "/" .. f[2] .. ".absent", why)
      else
        writeBytes(outdir .. "/" .. f[2], saved)
      end
    end
  end
end

local function noText(f)
  return f[6] == "no-text"
end

-- json-read: FromJson of the instance's committed text, then Save, the bytes
-- (§16.2). The read is the STRICT one, as every leg's text read is.
local function surfaceJsonRead(all, outdir)
  for _, f in ipairs(kind(all, "instance")) do
    if not noText(f) then
      local u = unit(f[3])
      if not u then
        absent(outdir, f[2], NOT_GENERATED)
      else
        local text = mustRead("testdata/conformance/tables/json/" .. f[2] .. ".json")
        local r = newReport()
        local v = u.json.FromJson(u.T, f[4], text, r)
        if v == nil then fail(f[2] .. ": FromJson refused the text: " .. tostring(r.Reason)) end
        local saved, why = u.save.Save(u.T, f[4], v)
        if saved == nil then fail(f[2] .. ": Save refused the value: " .. tostring(why)) end
        writeBytes(outdir .. "/" .. f[2], saved)
      end
    end
  end
end

-- json-write: the instance's wire loaded, then ToJson, the text (§16.1).
local function surfaceJsonWrite(all, outdir)
  for _, f in ipairs(kind(all, "instance")) do
    if not noText(f) then
      local u = unit(f[3])
      if not u then
        absent(outdir, f[2] .. ".json", NOT_GENERATED)
      else
        local v = u.load.LoadTolerant(u.T, f[4], mustRead(f[5]), newReport())
        if v == nil then fail(f[2] .. ": load refused " .. f[5]) end
        local text, why = u.json.ToJson(u.T, f[4], v)
        if text == nil then fail(f[2] .. ": ToJson refused the value: " .. tostring(why)) end
        writeBytes(outdir .. "/" .. f[2] .. ".json", text)
      end
    end
  end
end

-- THE JSON-HOSTILE surface (test/conformance/README.md): FromJson of each
-- hostile tree, writing the report line, or `refused` when the read refuses
-- (docs/SPEC-TABLES.md §16.2, §16.3, §4).
local function surfaceJsonHostile(all, outdir)
  for _, f in ipairs(kind(all, "json-hostile")) do
    local u = unit(f[3])
    if not u then
      absent(outdir, f[2], NOT_GENERATED)
    else
      local text = mustRead(f[5] .. "/" .. f[4] .. ".json")
      local r = newReport()
      u.json.FromJsonTolerant(u.T, f[4], text, r)
      if r.malformed then
        writeBytes(outdir .. "/" .. f[2], "refused\n")
      else
        writeBytes(outdir .. "/" .. f[2], reportLine(r))
      end
    end
  end
end

---------------------------------------------------------------------------
-- the message form (docs/SPEC-TABLES.md §3.3)
---------------------------------------------------------------------------

local function connectionNamed(all, key)
  for _, c in ipairs(kind(all, "connection")) do
    if c[2] == key then return c end
  end
  fail("no connection " .. key)
end

-- one direction's VOCABULARY, read from the connection's announcement by the
-- receiving unit (§3.3)
local function vocabulary(u, conn, what)
  local vocab = {}
  for i = 1, (u.T.MessageEntries or 0) do vocab[i] = {} end
  if not u.T.AnnounceRead(vocab, mustRead(conn[5]), newReport()) then
    fail(what .. ": the announcement was refused: " .. conn[5])
  end
  return vocab
end

-- `message` reads TWO files: the connection's announcement into one
-- direction's vocabulary, then the batch against it; LoadMessages,
-- SaveMessages, the bytes (test/conformance/README.md).
local function surfaceMessage(all, outdir)
  for _, f in ipairs(kind(all, "message")) do
    -- message <name> <connection> <root> <file-form wire> <message-form wire>
    local conn = connectionNamed(all, f[3])
    local u = unit(conn[3])
    if f[6] == "-" then
      -- a named refusal row: the root is a variable table (§3.3)
      absent(outdir, f[2], "variable table: the message form is fixed tables only (SPEC-TABLES §3.3)")
    elseif not u then
      absent(outdir, f[2], NOT_GENERATED)
    elseif u.T.MessageRoots == nil or not u.T.MessageRoots[f[4]] then
      absent(outdir, f[2], "variable table: the message form is fixed tables only (SPEC-TABLES §3.3)")
    else
      local vocab = vocabulary(u, conn, f[2])
      local r = newReport()
      local values = u.T.LoadMessages(f[4], vocab, mustRead(f[6]), r)
      if values == nil or #values < 1 then fail(f[2] .. ": message load refused: " .. tostring(r.reason)) end
      local saved = u.T.SaveMessages(f[4], vocab, values)
      if saved == nil then fail(f[2] .. ": message save refused") end
      writeBytes(outdir .. "/" .. f[2], saved)
    end
  end
end

---------------------------------------------------------------------------
-- RETAIN-UNKNOWN (docs/SPEC-TABLES.md §6.6)
---------------------------------------------------------------------------

-- the roomy capacities a `full` row names: a megabyte of records, and the id
-- count that megabyte can hold (C / 8, §6.6's count bound)
local ROOMY = 1 << 20

local function retainStore(capacity, ids)
  return { list = {}, used = 0, capacity = capacity, idCapacity = ids }
end

-- THE TWO RETAIN SURFACES ARE ONE ROUND TRIP AND TWO SHAPES
-- (test/conformance/README.md): `retain` prints the two load counters beside
-- the `unknown` retention never moves and the save's own `retain_lost`;
-- `retain-save` writes the bytes the pair saved. A row's capacity is a RULE:
-- `short` is ONE BYTE SHORT OF THE LAST RECORD, so the driver loads once at a
-- roomy capacity, reads what its own store used, and loads again one byte
-- under that.
local function retainRow(f)
  local key, root, wirePath = f[3], f[4], f[5]
  local short, idsField = f[6] == "short", f[7]
  local u = unit(key)
  if not u then return nil, NOT_GENERATED end
  local ids = ROOMY // 8
  if idsField ~= "full" then ids = math.tointeger(tonumber(idsField)) end
  local wire = mustRead(wirePath)
  local report = newReport()
  local roots, stores = {}, {}
  stores[1] = retainStore(ROOMY, ids)
  if short then
    if u.retain.LoadRetain(u.T, root, wire, stores[1], newReport()) == nil then
      fail(f[2] .. ": capacity probe refused")
    end
    stores[1] = retainStore(math.max(0, stores[1].used - 1), ids)
  end
  roots[1] = u.retain.LoadRetain(u.T, root, wire, stores[1], report)
  if roots[1] == nil or report.malformed then fail(f[2] .. ": retained load refused") end
  local answer = { retained = report.retained, lost = report.retainLost, unknown = report.unknown }
  local save = newReport()
  local parts = {}
  for i, v in ipairs(roots) do
    local size = u.retain.MeasureRetain(u.T, root, v, stores[i])
    if size == nil then fail(f[2] .. ": retaining measure refused") end
    local out = u.retain.SaveRetain(u.T, root, v, stores[i], save)
    if out == nil then fail(f[2] .. ": retaining save refused") end
    if #out ~= size then fail(f[2] .. ": the save wrote " .. #out .. " bytes, the measure said " .. size) end
    -- THE SAVE IS REPEATABLE: a second walk with a fresh report writes the
    -- same bytes and answers the same save-side loss (§6.6)
    local again = newReport()
    local second = u.retain.SaveRetain(u.T, root, v, stores[i], again)
    if second ~= out then fail(f[2] .. ": the save is not repeatable") end
    parts[#parts + 1] = out
  end
  answer.saveLost = save.retainLost
  answer.saved = table.concat(parts)
  return answer
end

local function surfaceRetain(all, outdir, saves)
  for _, f in ipairs(all) do
    if f[1] == "retain" then
      local answer, why = retainRow(f)
      if answer == nil then
        absent(outdir, f[2], why)
      elseif saves then
        writeBytes(outdir .. "/" .. f[2], answer.saved)
      else
        writeBytes(outdir .. "/" .. f[2], string.format("%d,%d,%d %d\n",
          answer.retained, answer.lost, answer.unknown, answer.saveLost))
      end
    end
  end
end

---------------------------------------------------------------------------
-- the accelerators: placing a buffer the way a forgery line asks for
---------------------------------------------------------------------------

-- EXACTLY THE CLAIM: what fits of the file, and zeroes past it. The claim may
-- run PAST the file, or short of it, which is what a truncation is.
local function place(source, extent)
  if extent < 0 or extent == #source then return source end
  if extent < #source then return source:sub(1, extent) end
  return source .. string.rep("\0", extent - #source)
end

-- THE OTHER BYTE ORDER (docs/SPEC-TABLES.md §19.1, §7.1): the same file with
-- the MAGIC WORD's eight bytes reversed, which is what it looks like to a
-- reader of the other order. Every leg on every host must refuse.
local function foreign(source)
  if #source < 8 then return source end
  return source:sub(1, 8):reverse() .. source:sub(9)
end

local function verdict(handle)
  return handle ~= nil and "open\n" or "refuse\n"
end

-- a forgery line: forgery <case> <kind> <subject> <file> <extent> <lead>, the
-- lead `null` for no buffer at all
local function forged(f)
  if f[7] == "null" then return nil, 0 end
  return place(mustRead(f[5]), math.tointeger(tonumber(f[6]))), math.tointeger(tonumber(f[7]))
end

---------------------------------------------------------------------------
-- the BLOCK form (docs/SPEC-TABLES.md §19)
---------------------------------------------------------------------------

-- a block line's name says which projection it is (the C++ and JavaScript
-- legs bind the same two by the same prefixes)
local BLOCK_ROOTS = { block_render = "RenderFrame", block_padded = "PaddedFrame" }

local function blockNamed(all, name)
  for _, b in ipairs(kind(all, "block")) do
    if b[2] == name then
      for prefix, root in pairs(BLOCK_ROOTS) do
        if name:sub(1, #prefix) == prefix then
          local u = unit(b[3])
          if not u then fail(name .. ": " .. NOT_GENERATED) end
          return u, root, b
        end
      end
    end
  end
  fail("no block named " .. name)
end

local function blockOpen(all, name, bytes, lead)
  local u, root = blockNamed(all, name)
  return u.block.BlockOpen(u.T, root, bytes, nil, lead)
end

local function surfaceBlock(all, outdir, transform)
  for _, b in ipairs(kind(all, "block")) do
    writeBytes(outdir .. "/" .. b[2], verdict(blockOpen(all, b[2], transform(mustRead(b[4])), 0)))
  end
end

-- the canonical ROW DUMP (testdata/conformance/tables/FORMAT.md), written by
-- the runtime's own descriptor walk (runtime/block.lua, B.Dump)
local function surfaceBlockDump(all, outdir)
  for _, b in ipairs(kind(all, "block")) do
    local u = blockNamed(all, b[2])
    local h, why = blockOpen(all, b[2], mustRead(b[4]), 0)
    if h == nil then fail(b[2] .. " does not open: " .. tostring(why)) end
    writeBytes(outdir .. "/" .. b[2], u.block.Dump(h))
  end
end

---------------------------------------------------------------------------
-- the COOKED form (docs/SPEC-TABLES.md §7)
---------------------------------------------------------------------------

local function cookNamed(all, name)
  for _, c in ipairs(kind(all, "cook")) do
    if c[2] == name then
      local u = unit(c[3])
      if not u then fail(name .. ": " .. NOT_GENERATED) end
      return u, c[4], c
    end
  end
  fail("no cook named " .. name)
end

local function cookOpen(all, name, bytes, lead)
  local u, root = cookNamed(all, name)
  return u.cook.Open(u.T, root, bytes, lead)
end

-- ---- the COOK's node dump (docs/SPEC-TABLES.md §7.5)
--
-- Every node this side reaches, through its OWN derefs, and generic over the
-- descriptors, which is the whole point of them: a pointer slot is eight
-- bytes holding the SIGNED SELF-RELATIVE delta of §6.3, so a deref is one add,
-- and a delta of zero is null. A by-value nesting is not a node; it is storage
-- inside one, and the walk descends through it to reach the pointer slots.

local function dumpText(data, at, used)
  if used < 0 then used = 0 end
  local out = { '"' }
  for i = 0, used - 1 do
    local c = data:byte(at + i + 1)
    if c >= 0x20 and c < 0x7f and c ~= 0x22 and c ~= 0x5c then
      out[#out + 1] = string.char(c)
    else
      out[#out + 1] = string.format("\\x%02x", c)
    end
  end
  out[#out + 1] = '" len=' .. used
  return table.concat(out)
end

local function join(prefix, name)
  if prefix == "" then return name end
  return prefix .. "." .. name
end

-- present_offset is the address of an optional's presence bool: the last
-- storage piece, after the value's own pieces (docs/SPEC-TABLES.md §7.2),
-- computed exactly as runtime/uncook.lua and runtime/cookwrite.lua compute it
local function alignUp(n, a)
  if a <= 1 then return n end
  return (n + a - 1) & ~(a - 1)
end

local function scalarWidth(f)
  local s = f.shape
  if s == "bool" then return 1 end
  if s == "int" then return f.width end
  if s == "flags" then return 8 end
  return nil
end

local function presentOffset(T, f)
  local at, s = f.at, f.shape
  if s == "string" then return alignUp(at + (f.bound or 0) + 1, 4) + 4 end
  if s == "bytes" then return alignUp(at + (f.bound or 0), 4) + 4 end
  if s == "array" then
    if f.list then return at + 16 end
    local cur = at + (f.elem_size or 0) * (f.bound or 0)
    if not f.fixed and f.keyed == nil then cur = alignUp(cur, 4) + 4 end
    return cur
  end
  if s == "pointer" then return at + 8 end
  if s == "table" then return at + T.Tables[f.target].size end
  local w = scalarWidth(f)
  if w ~= nil then return at + w end
  fail("the dump met an optional " .. tostring(s) .. ", which this walk does not place")
end

local function cookScalar(data, at, f)
  local s = f.shape
  if s == "bool" then
    return data:byte(at + 1) ~= 0 and "true" or "false"
  elseif s == "int" or s == "flags" then
    local width = scalarWidth(f)
    local signed = s == "int" and f.signed
    if width == 8 then
      local v = string.unpack("<i8", data, at + 1)
      if signed then return string.format("%d", v) end
      return string.format("%u", v) -- the same 64 bits, read unsigned
    end
    local fmt = (signed and "<i" or "<I") .. width
    return string.format("%d", (string.unpack(fmt, data, at + 1)))
  end
  -- Nothing in the pointered corpus is a float, and a canonical
  -- cross-language spelling of one is a decision this gate should not make in
  -- passing (test/conformance/js/main.mjs says the same). The day one
  -- arrives, the gate says so rather than drifting.
  fail("the dump met a " .. tostring(s) .. ", whose canonical cross-language spelling this gate does not fix")
end

local function cookDump(h, rootName)
  local T = h.T
  local data = h.bytes:sub(h.data_offset + 1, h.data_offset + h.data_length)
  local length = h.data_length
  local out, reached = {}, {}

  local function emit(path, value) out[#out + 1] = "  " .. path .. " = " .. value end

  local node

  local function storageWalk(at, desc, path, depth)
    for _, f in ipairs(desc.fields) do
      local name = join(path, f.name)
      local s = f.shape
      -- every COUNT COMPANION, against its declared bound, and a NEGATIVE one
      -- refuses too (§7.4's pass two)
      local used = -1
      local counted = (s == "string" or s == "bytes") or (s == "array" and not f.list and not f.fixed and f.keyed == nil)
      if counted then
        used = string.unpack("<i4", data, at + f.at + f.companion + 1)
        if used < 0 or used > f.bound then
          fail(desc.name .. "." .. f.name .. " carries a count companion of " .. used ..
            ", outside [ 0, " .. f.bound .. " ]")
        end
      end
      if s == "pointer" then
        local slot = at + f.at
        local delta = string.unpack("<i8", data, slot + 1)
        if delta == 0 then
          emit(name, "null") -- NULL IN A REGION IS A DELTA OF ZERO (§6.3)
        else
          local target = slot + delta
          if target < 0 or target >= length then
            fail(desc.name .. "." .. f.name .. " resolves outside the region — a delta of " ..
              delta .. " from a slot at " .. slot)
          end
          local tdesc = T.Tables[f.target]
          if tdesc == nil then
            fail(desc.name .. "." .. f.name .. " is a pointer whose descriptor names no record")
          end
          emit(name, "-> @" .. target)
          node(target, tdesc, depth + 1)
        end
      elseif s == "string" or s == "bytes" then
        emit(name, dumpText(data, at + f.at, used))
      elseif s == "table" then
        storageWalk(at + f.at, T.Tables[f.target], name, depth)
      elseif s == "array" and not f.list then
        -- a COUNTED array writes all N slots (§7.2), and a slot past the live
        -- count holds the value-initialized element: walking all of them is
        -- what the check does too
        for i = 0, f.bound - 1 do
          local element = name .. "[" .. i .. "]"
          local eat = at + f.at + i * f.elem_step
          if f.elem.shape == "table" then
            storageWalk(eat, T.Tables[f.elem.target], element, depth)
          else
            emit(element, cookScalar(data, eat, f.elem))
          end
        end
      else
        emit(name, cookScalar(data, at + f.at, f))
      end
      if counted and s == "array" then
        emit(name .. "#count", tostring(used))
      end
      if f.optional then
        emit(name .. "#present", data:byte(at + presentOffset(T, f) + 1) ~= 0 and "true" or "false")
      end
    end
  end

  node = function(offset, desc, depth)
    if depth > 4096 then
      fail("the walk nested past any depth a region can hold — a cycle the deref did not close")
    end
    for _, r in ipairs(reached) do
      if r.offset == offset then
        if r.desc ~= desc then
          fail("two references name the node at offset " .. offset .. " as two different tables: " ..
            r.desc.name .. " and " .. desc.name)
        end
        -- one node, one visit: sharing and a back-reference are the same fact (§6.3)
        return
      end
    end
    if offset > length or desc.size > length - offset then
      fail("the node at offset " .. offset .. " (" .. desc.name .. ", sizeof " .. desc.size ..
        ") does not fit inside the region's " .. length .. " bytes")
    end
    local index = #reached
    reached[#reached + 1] = { offset = offset, desc = desc }
    out[#out + 1] = "node " .. index .. " " .. desc.name .. " @" .. offset
    storageWalk(offset, desc, "", depth)
  end

  node(0, T.Tables[rootName], 0)
  return table.concat(out, "\n") .. "\n"
end

local function surfaceCook(all, outdir)
  for _, c in ipairs(kind(all, "cook")) do
    local _, root = cookNamed(all, c[2])
    local h, why = cookOpen(all, c[2], mustRead(c[5]), 0)
    if h == nil then fail(c[2] .. " does not open: " .. tostring(why)) end
    writeBytes(outdir .. "/" .. c[2], cookDump(h, root))
  end
end

local function surfaceCookForeign(all, outdir)
  for _, c in ipairs(kind(all, "cook")) do
    writeBytes(outdir .. "/" .. c[2], verdict(cookOpen(all, c[2], foreign(mustRead(c[5])), 0)))
  end
end

-- the two forgery batteries (FORMAT.md, `forgery`): the verdict, or the
-- reason the generated reader names beside its nil — the TableRefuseReason
-- value name of the FIRST failing clause, or `ok` (§6.5, §7, §19.2). This
-- driver places bytes and prints what it is told, and judges nothing.
local function surfaceForgery(all, outdir, forgeryKind, reasons)
  for _, f in ipairs(kind(all, "forgery")) do
    if f[3] == forgeryKind then
      local bytes, lead = forged(f)
      local h, why
      if forgeryKind == "cook" then
        h, why = cookOpen(all, f[4], bytes, lead)
      else
        h, why = blockOpen(all, f[4], bytes, lead)
      end
      if reasons then
        writeBytes(outdir .. "/" .. f[2], (h ~= nil and "ok" or tostring(why)) .. "\n")
      else
        writeBytes(outdir .. "/" .. f[2], verdict(h))
      end
    end
  end
end

-- ---- the COOK WRITE surface (docs/SPEC-TABLES.md §7.6, §19.1)
--
-- Measure, refuse a short buffer without writing, then cook both byte orders:
-- `<instance>` little-endian and `<instance>-be` big (test/conformance/
-- README.md). The tool is the reference, so a cook the writer refuses where
-- the tool wrote one is a red, never an absence.
local ORDERS = { { 1, "" }, { 2, "-be" } }

local function surfaceCookWrite(all, outdir)
  local instances = {}
  for _, i in ipairs(kind(all, "instance")) do instances[i[2]] = i end
  for _, f in ipairs(kind(all, "cook-write")) do
    local inst = instances[f[2]]
    if inst == nil then fail("cook-write " .. f[2] .. " names no instance") end
    local u = unit(inst[3])
    if not u then
      for _, o in ipairs(ORDERS) do absent(outdir, f[2] .. o[2], NOT_GENERATED) end
    else
      local v = u.load.LoadTolerant(u.T, inst[4], mustRead(inst[5]), newReport())
      if v == nil then fail(f[2] .. ": cook input did not load") end
      local size, why = u.cookwrite.CookMeasure(u.T, inst[4], v)
      if size == nil or size < 1 then fail(f[2] .. ": cook measure refused: " .. tostring(why)) end
      for _, o in ipairs(ORDERS) do
        local name = f[2] .. o[2]
        if u.cookwrite.Cook(u.T, inst[4], v, size - 1, o[1]) ~= nil then
          fail(name .. ": a capacity one short of the measure wrote a cook")
        end
        local cooked, cw = u.cookwrite.Cook(u.T, inst[4], v, size, o[1])
        if cooked == nil then fail(name .. ": cook write refused: " .. tostring(cw)) end
        if #cooked ~= size then fail(name .. ": the cook is " .. #cooked .. " bytes, the measure said " .. size) end
        writeBytes(outdir .. "/" .. name, cooked)
      end
    end
  end
end

---------------------------------------------------------------------------
-- main
---------------------------------------------------------------------------

local function usage()
  io.stderr:write("usage: driver <manifest> list\n       driver <manifest> <surface> <outdir>\n")
  os.exit(2)
end

if #arg < 2 then usage() end
local surface = arg[2]

if surface == "list" then
  for _, name in ipairs(SURFACE_ORDER) do io.write(name .. "\n") end
  os.exit(0)
end

if #arg < 3 then usage() end
if not SURFACES[surface] then os.exit(2) end

local all = lines(arg[1])
local outdir = arg[3]
local identity = function(s) return s end

if surface == "wire" then surfaceWire(all, outdir)
elseif surface == "report" then surfaceReport(all, outdir)
elseif surface == "framing" then surfaceFraming(all, outdir)
elseif surface == "json-read" then surfaceJsonRead(all, outdir)
elseif surface == "json-write" then surfaceJsonWrite(all, outdir)
elseif surface == "json-hostile" then surfaceJsonHostile(all, outdir)
elseif surface == "message" then surfaceMessage(all, outdir)
elseif surface == "retain" then surfaceRetain(all, outdir, false)
elseif surface == "retain-save" then surfaceRetain(all, outdir, true)
elseif surface == "cook" then surfaceCook(all, outdir)
elseif surface == "cook-write" then surfaceCookWrite(all, outdir)
elseif surface == "cook-foreign" then surfaceCookForeign(all, outdir)
elseif surface == "block" then surfaceBlock(all, outdir, identity)
elseif surface == "block-foreign" then surfaceBlock(all, outdir, foreign)
elseif surface == "block-dump" then surfaceBlockDump(all, outdir)
elseif surface == "forgery" then surfaceForgery(all, outdir, "block", false)
elseif surface == "cook-forgery" then surfaceForgery(all, outdir, "cook", false)
elseif surface == "cook-reason" then surfaceForgery(all, outdir, "cook", true)
elseif surface == "block-reason" then surfaceForgery(all, outdir, "block", true)
end
os.exit(0)
