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
    damaged = false, refused = 0, unknown = 0, kindChanged = 0, outOfRange = 0,
    widened = 0, repeatedKey = 0, retained = 0, retainLost = 0,
  }
end

-- One strict read (docs/SPEC-TABLES.md §6.5). A fixed root's file is form 3
-- through ReadFixed; every other form byte is unknown_form and is not handed
-- to the variable walk. A variable root is Load, which already refuses every
-- form byte but 1. Returns the value, or nil and the refusal name.
local function isFixed(u, name)
  return u.T.Fixed ~= nil and u.T.Fixed[name] ~= nil
end

-- A fixed table (table and not variable) is form 3 only. ReadFixed exists
-- only when the emitter listed the root; a fixed root without it refuses.
local function fixedClass(u, name)
  local desc = u.T.Tables[name]
  if desc ~= nil and desc.table and not desc.variable then return true end
  return isFixed(u, name)
end

local function strictRead(u, name, bytes)
  if fixedClass(u, name) then
    if #bytes >= 1 and bytes:byte(1) == 3 and isFixed(u, name) then
      local ok, v = pcall(u.T.ReadFixed, name, bytes)
      if not ok then return nil, tostring(v) end
      return v
    end
    return nil, "unknown_form"
  end
  local r = newReport()
  local v = u.load.Load(u.T, name, bytes, r)
  if v == nil then return nil, r.reason or "refused" end
  return v
end

-- A framing vector is a form-1 file read as the VARIABLE form even at a fixed
-- root (docs/LAYERS/1-framing.md). The walk parses a node table only when the
-- descriptor says variable; the bit is restored before the next row.
local function loadVariable(u, name, bytes)
  local desc = u.T.Tables[name]
  local saved = desc and desc.variable
  if desc ~= nil then desc.variable = true end
  local r = newReport()
  local ok, v = pcall(u.load.Load, u.T, name, bytes, r)
  if desc ~= nil then desc.variable = saved end
  if not ok then return nil, tostring(v) end
  if v == nil then return nil, r.reason or "refused" end
  return v
end

-- The root's own writer: form 3 for a fixed root, the variable save otherwise.
local function canonicalWrite(u, name, v)
  if isFixed(u, name) then
    local ok, saved = pcall(u.T.WriteFixed, name, v)
    if not ok then return nil, tostring(saved) end
    return saved
  end
  return u.save.Save(u.T, name, v)
end

-- A read's answer (test/conformance/README.md): `refused` alone, or `read`,
-- the §16 text of the whole value when asked, and beside it <case>.wire.
-- A refusal writes nothing else. One case that cannot write back leaves no
-- wire, so the harness fails that case and the surface continues.
local function answerRead(outdir, name, u, root, v, withValue)
  if v == nil then
    writeBytes(outdir .. "/" .. name, "refused\n")
    return
  end
  local saved, why = canonicalWrite(u, root, v)
  if saved == nil then
    io.stderr:write("driver: " .. name .. ": write-back refused: " .. tostring(why) .. "\n")
    writeBytes(outdir .. "/" .. name, "read\n")
    return
  end
  writeBytes(outdir .. "/" .. name .. ".wire", saved)
  if not withValue then
    writeBytes(outdir .. "/" .. name, "read\n")
    return
  end
  local json, jwhy = u.json.ToJson(u.T, root, v)
  if json == nil then
    io.stderr:write("driver: " .. name .. ": ToJson refused: " .. tostring(jwhy) .. "\n")
    writeBytes(outdir .. "/" .. name, "read\n")
    return
  end
  writeBytes(outdir .. "/" .. name, "read\n" .. json)
end

-- report: one strict read, then the verdict and the value.
local function surfaceReport(all, outdir)
  for _, f in ipairs(kind(all, "report")) do
    local u = unit(f[3])
    if not u then
      absent(outdir, f[2], NOT_GENERATED)
    else
      local v = strictRead(u, f[4], mustRead(f[5]))
      answerRead(outdir, f[2], u, f[4], v, true)
    end
  end
end

-- THE FRAMING VECTORS (docs/LAYERS/1-framing.md): the same answer, read as
-- the variable form. A derived line is `framing <case> <unit> <root> <wire>`.
local function surfaceFraming(all, outdir)
  for _, f in ipairs(kind(all, "framing")) do
    local u = unit(f[3])
    if not u then
      absent(outdir, f[2], NOT_GENERATED)
    else
      local v = loadVariable(u, f[4], mustRead(f[5]))
      answerRead(outdir, f[2], u, f[4], v, f[6] ~= "no-value")
    end
  end
end

-- The WIRE surface loads a wire and re-saves it (docs/SPEC-TABLES.md §3).
-- A blob named from two slots is a named absence (FORMAT.md, wire-refused)
-- and no bytes. Any other refusal is that one case, not an exit.
local function wireRefusal(why)
  return type(why) == "string" and why:find("blob_shared", 1, true) ~= nil
end

local function surfaceWire(all, outdir)
  for _, f in ipairs(kind(all, "instance")) do
    local u = unit(f[3])
    if not u then
      absent(outdir, f[2], NOT_GENERATED)
    else
      local v, why = strictRead(u, f[4], mustRead(f[5]))
      if v == nil then
        if wireRefusal(why) then
          absent(outdir, f[2], "blob_shared")
        else
          io.stderr:write("driver: " .. f[2] .. ": load refused " .. f[5] .. " (" .. tostring(why) .. ")\n")
        end
      else
        local saved, wwhy = canonicalWrite(u, f[4], v)
        if saved == nil then
          if wireRefusal(wwhy) then
            absent(outdir, f[2], "blob_shared")
          else
            io.stderr:write("driver: " .. f[2] .. ": Save refused: " .. tostring(wwhy) .. "\n")
          end
        else
          writeBytes(outdir .. "/" .. f[2], saved)
        end
      end
    end
  end
end

local function noText(f)
  return f[6] == "no-text"
end

-- json-read: the text read of the instance's committed text, then the root's
-- own writer, the bytes (§16.2).
local function surfaceJsonRead(all, outdir)
  for _, f in ipairs(kind(all, "instance")) do
    if not noText(f) then
      local u = unit(f[3])
      if not u then
        absent(outdir, f[2], NOT_GENERATED)
      else
        local text = mustRead("testdata/conformance/tables/json/" .. f[2] .. ".json")
        local v = u.json.FromJson(u.T, f[4], text, newReport())
        if v == nil then
          io.stderr:write("driver: " .. f[2] .. ": FromJson refused the text\n")
        else
          local saved, why = canonicalWrite(u, f[4], v)
          if saved == nil then
            io.stderr:write("driver: " .. f[2] .. ": Save refused the value: " .. tostring(why) .. "\n")
          else
            writeBytes(outdir .. "/" .. f[2], saved)
          end
        end
      end
    end
  end
end

-- damageText puts into a fixed root's top-level text field what no read
-- delivers (testdata/conformance/tables/FORMAT.md, `json-write-refusal`): a narrow
-- field is a Lua string, which holds the bytes 61 FF 62 as they are; a wide
-- field is its array of UTF-16 units, here 0061 D800 0062.
local function damageText(v, damage)
  if damage == "none" then return end
  local form, name = damage:match("^(%a+):(.+)$")
  if form == "narrow" then
    v[name] = "a\xffb"
  elseif form == "wide" then
    v[name] = { 0x61, 0xd800, 0x62 }
  else
    fail("damage " .. damage .. " is not one this driver knows")
  end
end

-- json-write: the instance's wire loaded strictly, then ToJson, the text (§16.1).
local function surfaceJsonWrite(all, outdir)
  for _, f in ipairs(kind(all, "instance")) do
    if not noText(f) then
      local u = unit(f[3])
      if not u then
        absent(outdir, f[2] .. ".json", NOT_GENERATED)
      else
        local v, why = strictRead(u, f[4], mustRead(f[5]))
        if v == nil then
          io.stderr:write("driver: " .. f[2] .. ": load refused " .. f[5] .. " (" .. tostring(why) .. ")\n")
        else
          local text, jwhy = u.json.ToJson(u.T, f[4], v)
          if text == nil then
            io.stderr:write("driver: " .. f[2] .. ": ToJson refused the value: " .. tostring(jwhy) .. "\n")
          else
            writeBytes(outdir .. "/" .. f[2] .. ".json", text)
          end
        end
      end
    end
  end
  -- THE VALUES THE WRITER REFUSES (docs/SPEC-TABLES.md §16.14, W0): the valid
  -- wire, then the line's damage put into storage in code, then ToJson, which
  -- must refuse. The load is the strict read; there is no tolerant twin.
  for _, f in ipairs(kind(all, "json-write-refusal")) do
    local u = unit(f[3])
    if not u then
      absent(outdir, f[2] .. ".json", NOT_GENERATED)
    else
      local v, why = strictRead(u, f[4], mustRead(f[5]))
      if v == nil then
        io.stderr:write("driver: " .. f[2] .. ": load refused " .. f[5] .. " (" .. tostring(why) .. ")\n")
      else
        damageText(v, f[6])
        local text = u.json.ToJson(u.T, f[4], v)
        writeBytes(outdir .. "/" .. f[2] .. ".json", text or "refused\n")
      end
    end
  end
end

-- THE JSON-HOSTILE surface (test/conformance/README.md): FromJson of
-- each hostile tree. `refused` alone, or `read`, the §16 text, and the
-- write-back wire (docs/SPEC-TABLES.md §16.2, §16.3, §4).
local function surfaceJsonHostile(all, outdir)
  for _, f in ipairs(kind(all, "json-hostile")) do
    local u = unit(f[3])
    if not u then
      absent(outdir, f[2], NOT_GENERATED)
    else
      local text = mustRead(f[5] .. "/" .. f[4] .. ".json")
      local v = u.json.FromJson(u.T, f[4], text, newReport())
      answerRead(outdir, f[2], u, f[4], v, true)
    end
  end
end

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
      return { refused = true }
    end
    stores[1] = retainStore(math.max(0, stores[1].used - 1), ids)
  end
  roots[1] = u.retain.LoadRetain(u.T, root, wire, stores[1], report)
  if roots[1] == nil or report.damaged then return { refused = true } end
  local answer = { retained = report.retained, lost = report.retainLost, unknown = report.unknown }
  local save = newReport()
  local parts = {}
  for i, v in ipairs(roots) do
    local size = u.retain.MeasureRetain(u.T, root, v, stores[i])
    if size == nil then return { refused = true } end
    local out = u.retain.SaveRetain(u.T, root, v, stores[i], save)
    if out == nil then return { refused = true } end
    if #out ~= size then return { refused = true } end
    -- THE SAVE IS REPEATABLE: a second walk with a fresh report writes the
    -- same bytes and answers the same save-side loss (§6.6)
    local again = newReport()
    local second = u.retain.SaveRetain(u.T, root, v, stores[i], again)
    if second ~= out then return { refused = true } end
    parts[#parts + 1] = out
  end
  answer.saveLost = save.retainLost
  answer.saved = table.concat(parts)
  local json, jwhy = u.json.ToJson(u.T, root, roots[1])
  if json == nil then return { refused = true } end
  answer.json = json
  return answer
end

local function surfaceRetain(all, outdir, saves)
  for _, f in ipairs(all) do
    if f[1] == "retain" then
      local answer, why = retainRow(f)
      if answer == nil then
        absent(outdir, f[2], why)
      elseif answer.refused then
        if not saves then writeBytes(outdir .. "/" .. f[2], "refused\n") end
      elseif saves then
        writeBytes(outdir .. "/" .. f[2], answer.saved)
      else
        writeBytes(outdir .. "/" .. f[2], "read\n" .. answer.json)
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
      -- THE RECIPE'S VALUE (FORMAT.md): the JSON input, or `-` for the
      -- no-text instance's wire; then the optional shared blob.
      local v, why
      if f[3] ~= nil and f[3] ~= "-" then
        v = u.json.FromJson(u.T, inst[4], mustRead(f[3]), newReport())
        if v == nil then why = "text read refused" end
        if v ~= nil and f[4] ~= nil and f[4] ~= "-" then
          v[f[5]] = v[f[4]]
        end
      else
        v, why = strictRead(u, inst[4], mustRead(inst[5]))
      end
      if v == nil then
        io.stderr:write("driver: " .. f[2] .. ": cook input did not load (" .. tostring(why) .. ")\n")
      else
        local size, cwhy = u.cookwrite.CookMeasure(u.T, inst[4], v)
        if size == nil or size < 1 then
          io.stderr:write("driver: " .. f[2] .. ": cook measure refused: " .. tostring(cwhy) .. "\n")
        else
          for _, o in ipairs(ORDERS) do
            local name = f[2] .. o[2]
            if u.cookwrite.Cook(u.T, inst[4], v, size - 1, o[1]) ~= nil then
              io.stderr:write("driver: " .. name .. ": a capacity one short of the measure wrote a cook\n")
            else
              local cooked, cw = u.cookwrite.Cook(u.T, inst[4], v, size, o[1])
              if cooked == nil or #cooked ~= size then
                io.stderr:write("driver: " .. name .. ": cook write refused: " .. tostring(cw) .. "\n")
              else
                writeBytes(outdir .. "/" .. name, cooked)
              end
            end
          end
        end
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
