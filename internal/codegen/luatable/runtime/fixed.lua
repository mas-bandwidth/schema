-- fixed.lua — the Lua table runtime's FIXED FORM (docs/SPEC-TABLES.md §3.4,
-- form byte 3), the reader and writer for one record. Emitted beside the unit's
-- <Package>Tables.lua by internal/codegen/luatable. The layout is built from
-- the descriptors the shape walks (docs/FIXED-FORM-ALGORITHM.md), so a record
-- is the eight-byte hash of the writer's vocabulary block and then the values
-- in declared order, every field at its declared storage width, nothing padded
-- (docs/FIXED-FORM-ALGORITHM.md §1, §3).
--
-- A buffer READ is a Lua string and a cursor; a WRITE is a table of pieces
-- joined once by table.concat. Fixed-width little-endian values are
-- string.unpack/string.pack with a `<` format (docs/PORT-LUA.md).
local D = require("descriptor")
local F = {}

local formByte = 3
local maxRecordBytes = 65536
local maxRecords = 4096

-- A FLOAT IN THE FIXED FORM IS FINITE (docs/SPEC-TABLES.md §3.3, §3.4): the
-- record carries the bit pattern of a finite value, -0 and the subnormals
-- exactly. A NaN or an infinity is refused on write and refused on read from
-- the record's raw bits, before they become a number, as the named refusal
-- non_finite_float.
local F32_EXP = 0x7f800000
local F64_EXP = 0x7ff0000000000000

local function pack_float(f32, v)
  if f32 then
    local p = string.pack("<f", v)
    if string.unpack("<I4", p) & F32_EXP == F32_EXP then error("non_finite_float", 0) end
    return p
  end
  if v - v ~= 0 then error("non_finite_float", 0) end
  return string.pack("<d", v)
end

local function unpack_float(f32, buf, pos)
  if f32 then
    if string.unpack("<I4", buf, pos) & F32_EXP == F32_EXP then error("non_finite_float", 0) end
    return string.unpack("<f", buf, pos)
  end
  if string.unpack("<i8", buf, pos) & F64_EXP == F64_EXP then error("non_finite_float", 0) end
  return string.unpack("<d", buf, pos)
end

local function zeros(n)
  if n <= 0 then return "" end
  return string.rep("\0", n)
end

local function pack_uint(v, w)
  if w == 1 then return string.pack("<I1", v) end
  if w == 2 then return string.pack("<I2", v) end
  if w == 4 then return string.pack("<I4", v) end
  return string.pack("<I8", v)
end

local function pack_int(v, w, signed)
  if w == 1 then return signed and string.pack("<i1", v) or string.pack("<I1", v) end
  if w == 2 then return signed and string.pack("<i2", v) or string.pack("<I2", v) end
  if w == 4 then return signed and string.pack("<i4", v) or string.pack("<I4", v) end
  return signed and string.pack("<i8", v) or string.pack("<I8", v)
end

local function unpack_int(buf, pos, w, signed)
  if w == 1 then
    if signed then return string.unpack("<i1", buf, pos) end
    return string.unpack("<I1", buf, pos)
  end
  if w == 2 then
    if signed then return string.unpack("<i2", buf, pos) end
    return string.unpack("<I2", buf, pos)
  end
  if w == 4 then
    if signed then return string.unpack("<i4", buf, pos) end
    return string.unpack("<I4", buf, pos)
  end
  if signed then return string.unpack("<i8", buf, pos) end
  return string.unpack("<I8", buf, pos)
end

-- A WIDE VALUE IS SIXTEEN BYTES, the low 64-bit half first (docs/
-- SPEC-TABLES.md §3 kinds 18/19 and 24/29; §3.4: the fixed form names no kind
-- and stores the declared storage image, so an int128 is sixteen bytes and a
-- fixed(16, 16) its raw scaled integer).
local function pack_wide(v)
  v = v or {}
  return string.pack("<I8", v.lo or 0) .. string.pack("<I8", v.hi or 0)
end

local function unpack_wide(buf, pos)
  local lo = string.unpack("<I8", buf, pos)
  local hi = string.unpack("<I8", buf, pos + 8)
  return { lo = lo, hi = hi }, pos + 16
end

local encode_table, encode_value, decode_table, decode_value

local function encode_array(T, d, v, size, elemSize)
  v = v or {}
  local parts = {}
  if d.list then error("fixed: an unbounded array (docs/SPEC-TABLES.md §2.9) is not fixed") end
  if d.fixed then
    for i = 1, d.bound do
      local ev = v[i]
      if ev == nil then
        parts[#parts + 1] = zeros(elemSize)
      else
        parts[#parts + 1] = encode_value(T, d.elem, ev, elemSize)
      end
    end
    return table.concat(parts)
  end
  if d.keyed then
    local n = d.bound or #d.keyed
    for i = 1, n do
      local ev = v[i]
      if ev == nil then
        parts[#parts + 1] = zeros(elemSize)
      else
        parts[#parts + 1] = encode_value(T, d.elem, ev, elemSize)
      end
    end
    return table.concat(parts)
  end
  local n = #v
  if n > d.bound then error("count_over_bound") end
  parts[#parts + 1] = string.pack("<i4", n)
  for i = 1, n do
    parts[#parts + 1] = encode_value(T, d.elem, v[i], elemSize)
  end
  local used = 4 + n * elemSize
  if used > size then error("count_over_bound") end
  parts[#parts + 1] = zeros(size - used)
  return table.concat(parts)
end

local function encode_union(T, d, v, size)
  v = v or { tag = 0 }
  local ud = T.FixedUnions[d.target]
  if ud == nil then error("fixed: no union descriptor for " .. tostring(d.target)) end
  local tag = v.tag or 0
  if tag > #ud.arms then tag = 0 end
  local parts = { pack_uint(tag, ud.tag) }
  local arm = ud.arms[tag]
  if arm ~= nil and tag ~= 0 then
    local fd = T.Unions[d.target].arms[tag].field
    local payload = encode_value(T, fd, v[arm.slot], arm.size)
    parts[#parts + 1] = payload
  else
    parts[#parts + 1] = zeros(size - ud.tag)
  end
  return table.concat(parts)
end

encode_value = function(T, d, v, size, elemSize)
  local s = d.shape
  if s == "int" then
    if d.timestamp then return pack_int(v or 0, 8, true) end
    return pack_int(v or 0, d.width, d.signed)
  elseif s == "wide" then
    return pack_wide(v)
  elseif s == "bool" then
    return string.char((v and 1 or 0))
  elseif s == "float" then
    return pack_float(d.f32, v or 0.0)
  elseif s == "string" or s == "bytes" then
    local n = #v
    local cap = size - 4
    if n > cap then error("length_over_bound") end
    return string.pack("<i4", n) .. string.sub(v, 1, n) .. zeros(cap - n)
  elseif s == "wstring" then
    local n = #v
    local cap = math.floor((size - 4) / 2)
    if n > cap then error("length_over_bound") end
    local parts = { string.pack("<i4", n) }
    for i = 1, n do
      parts[#parts + 1] = string.pack("<I2", v[i] or 0)
    end
    parts[#parts + 1] = zeros((size - 4) - 2 * n)
    return table.concat(parts)
  elseif s == "enum" then
    local w = T.FixedEnums[d.enum] or 4
    local ord = 0
    if v ~= nil then
      for i, var in ipairs(d.variants) do
        if var[1] == v then
          ord = i
          break
        end
      end
    end
    return pack_uint(ord, w)
  elseif s == "flags" then
    return pack_uint(v or 0, 8)
  elseif s == "table" then
    return encode_table(T, d.target, v)
  elseif s == "union" then
    return encode_union(T, d, v, size)
  elseif s == "array" then
    return encode_array(T, d, v, size, elemSize)
  elseif s == "pointer" or s == "string_blob" or s == "bytes_blob" then
    return zeros(size)
  end
  error("fixed: unknown shape " .. tostring(s))
end

encode_table = function(T, name, v)
  local desc = T.Tables[name]
  if desc == nil then error("fixed: no descriptor for table " .. tostring(name)) end
  v = v or {}
  local sizes = T.FixedFields[name]
  local parts = {}
  for i, f in ipairs(desc.fields) do
    local fs = sizes[i].size
    if f.optional then
      local present = v[f.name .. "_present"] == true
      if present and v[f.name] ~= nil then
        parts[#parts + 1] = "\1" .. encode_value(T, f, v[f.name], fs - 1, sizes[i].elem)
      else
        parts[#parts + 1] = "\0" .. zeros(fs - 1)
      end
    else
      parts[#parts + 1] = encode_value(T, f, v[f.name], fs, sizes[i].elem)
    end
  end
  return table.concat(parts)
end

decode_value = function(T, d, buf, pos, size, elemSize)
  local s = d.shape
  if s == "int" then
    local w = d.timestamp and 8 or d.width
    local signed = d.timestamp or d.signed
    return unpack_int(buf, pos, w, signed)
  elseif s == "wide" then
    return unpack_wide(buf, pos)
  elseif s == "bool" then
    return buf:byte(pos) ~= 0, pos + size
  elseif s == "float" then
    return unpack_float(d.f32, buf, pos)
  elseif s == "string" or s == "bytes" then
    local n = string.unpack("<i4", buf, pos)
    local cap = size - 4
    if n < 0 or n > cap then error("length_over_bound") end
    return string.sub(buf, pos + 4, pos + 4 + n - 1), pos + size
  elseif s == "wstring" then
    local n = string.unpack("<i4", buf, pos)
    local cap = math.floor((size - 4) / 2)
    if n < 0 or n > cap then error("length_over_bound") end
    local t = {}
    for i = 1, n do
      t[i] = string.unpack("<I2", buf, pos + 4 + (i - 1) * 2)
    end
    return t, pos + size
  elseif s == "enum" then
    local w = T.FixedEnums[d.enum] or 4
    local raw = unpack_int(buf, pos, w, false)
    local v = nil
    -- a retired variant reads the declared default (docs/SPEC-TABLES.md §2.15)
    local var = raw ~= 0 and d.variants[raw] or nil
    if var ~= nil then
      if var.deprecated then
        v = d.default
      else
        v = var[1]
      end
    end
    return v, pos + w
  elseif s == "flags" then
    return unpack_int(buf, pos, 8, false), pos + 8
  elseif s == "table" then
    local child = T.Tables[d.target].fixed_size
    local v = decode_table(T, d.target, buf, pos, child)
    return v, pos + size
  elseif s == "union" then
    local ud = T.FixedUnions[d.target]
    local tag = unpack_int(buf, pos, ud.tag, false)
    if tag > #ud.arms then tag = 0 end
    local v = { tag = tag }
    if tag ~= 0 then
      local arm = ud.arms[tag]
      local fd = T.Unions[d.target].arms[tag].field
      v[arm.slot] = decode_value(T, fd, buf, pos + ud.tag, arm.size)
    end
    return v, pos + size
  elseif s == "array" then
    local t = {}
    if d.fixed or d.keyed then
      local n = d.bound or #d.keyed
      for i = 1, n do
        t[i] = decode_value(T, d.elem, buf, pos + (i - 1) * elemSize, elemSize)
      end
      return t, pos + size
    end
    local n = string.unpack("<i4", buf, pos)
    if n < 0 or n > d.bound then error("count_over_bound") end
    for i = 1, n do
      t[i] = decode_value(T, d.elem, buf, pos + 4 + (i - 1) * elemSize, elemSize)
    end
    return t, pos + size
  elseif s == "pointer" or s == "string_blob" or s == "bytes_blob" then
    return nil, pos + size
  end
  error("fixed: unknown shape " .. tostring(s))
end

decode_table = function(T, name, buf, pos, writerSize)
  local desc = T.Tables[name]
  if desc == nil then error("fixed: no descriptor for table " .. tostring(name)) end
  local v = T["New" .. name]()
  local sizes = T.FixedFields[name]
  local cursor = pos
  for i, f in ipairs(desc.fields) do
    local fs = sizes[i].size
    if cursor - pos + fs <= writerSize then
      if f.optional then
        local present = buf:byte(cursor) ~= 0
        v[f.name .. "_present"] = present
        if present then v[f.name] = decode_value(T, f, buf, cursor + 1, fs - 1, sizes[i].elem) end
        cursor = cursor + fs
      else
        v[f.name], cursor = decode_value(T, f, buf, cursor, fs, sizes[i].elem)
      end
    else
      break
    end
  end
  return v
end

-- A LOCKED LAYOUT AS A TREE (docs/FIXED-FORM-ALGORITHM.md §5.2): the layout
-- bytes are pre-order entries of {kind, size, children}, and an older writer's
-- record is read against ITS OWN tree, never against this build's descriptors.
local function layout_tree(layout)
  if #layout < 4 then return nil end
  local count = string.unpack("<I4", layout, 1)
  if count == 0 or #layout ~= 4 + 17 * count then return nil end
  local i = 0
  local function parse()
    i = i + 1
    if i > count then return nil end
    local at = 4 + (i - 1) * 17
    local node = {
      kind = layout:byte(at + 9),
      size = string.unpack("<I4", layout, at + 10),
      children = {},
    }
    local kids = string.unpack("<I4", layout, at + 14)
    for k = 1, kids do
      node.children[k] = parse()
    end
    return node
  end
  local root = parse()
  if i ~= count or root == nil then return nil end
  return root
end

local decode_old_value

-- decode_old_table walks THIS build's descriptors against the WRITER's tree:
-- a field the older layout does not carry keeps its declared default, and a
-- field both carry is read at the older size (docs/FIXED-FORM-ALGORITHM.md
-- §5.2's PLAN: the writer's sizes, never the reader's).
local function decode_old_table(T, name, node, buf, pos)
  local desc = T.Tables[name]
  if desc == nil then error("fixed: no descriptor for table " .. tostring(name)) end
  local v = T["New" .. name]()
  local cursor = pos
  for i, f in ipairs(desc.fields) do
    local child = node.children[i]
    if child == nil then break end
    if f.optional then
      if child.kind == 35 then
        local present = buf:byte(cursor) ~= 0
        v[f.name .. "_present"] = present
        if present and child.children[1] ~= nil then
          v[f.name] = decode_old_value(T, f, child.children[1], buf, cursor + 1)
        end
      else
        -- THE PRESENT COMPANION THE READER GAINED IS TRUE for an old T (§5.9
        -- #40): the writer sent the value, so the ?T this reader declares is
        -- present.
        v[f.name .. "_present"] = true
        v[f.name] = decode_old_value(T, f, child, buf, cursor)
      end
      cursor = cursor + child.size
    else
      v[f.name] = decode_old_value(T, f, child, buf, cursor)
      cursor = cursor + child.size
    end
  end
  return v
end

decode_old_value = function(T, d, node, buf, pos)
  local s = d.shape
  if s == "int" then
    local signed = d.signed
    if d.timestamp then signed = true end
    return unpack_int(buf, pos, node.size, signed)
  elseif s == "wide" then
    return { lo = unpack_int(buf, pos, 8, false), hi = unpack_int(buf, pos + 8, 8, false) }
  elseif s == "bool" then
    return buf:byte(pos) ~= 0
  elseif s == "float" then
    -- an older writer's float32 widens into a float64 field by the ordinary
    -- conversion, exact for every finite float32 (§3.3)
    return (unpack_float(node.size == 4 or node.kind == 10, buf, pos))
  elseif s == "string" or s == "bytes" then
    local n = string.unpack("<i4", buf, pos)
    local cap = node.size - 4
    if n < 0 or n > cap then error("length_over_bound") end
    return string.sub(buf, pos + 4, pos + 4 + n - 1)
  elseif s == "wstring" then
    local n = string.unpack("<i4", buf, pos)
    local cap = math.floor((node.size - 4) / 2)
    if n < 0 or n > cap then error("length_over_bound") end
    local t = {}
    for i = 1, n do
      t[i] = string.unpack("<I2", buf, pos + 4 + (i - 1) * 2)
    end
    return t
  elseif s == "enum" then
    local raw = unpack_int(buf, pos, node.size, false)
    local var = raw ~= 0 and d.variants[raw] or nil
    if var ~= nil then
      -- a retired variant reads the declared default (docs/SPEC-TABLES.md §2.15)
      if var.deprecated then return d.default end
      return var[1]
    end
    return nil
  elseif s == "flags" then
    return unpack_int(buf, pos, node.size, false)
  elseif s == "table" then
    return decode_old_table(T, d.target, node, buf, pos)
  elseif s == "union" then
    local tagNode = node.children[1]
    local w = tagNode and tagNode.size or 4
    local tag = unpack_int(buf, pos, w, false)
    if tag > #T.Unions[d.target].arms then tag = 0 end
    local v = { tag = tag }
    if tag ~= 0 then
      local armNode = node.children[1 + tag]
      local arm = T.Unions[d.target].arms[tag]
      if armNode ~= nil and arm.field ~= nil then
        v[arm.slot] = decode_old_value(T, arm.field, armNode, buf, pos + w)
      end
    end
    return v
  elseif s == "array" then
    local t = {}
    local elem
    if node.kind == 16 then
      elem = node.children[2]
    else
      elem = node.children[1]
    end
    local es = elem and elem.size or 0
    if node.kind == 16 or d.fixed then
      local n = es > 0 and math.floor(node.size / es) or 0
      for i = 1, n do
        t[i] = decode_old_value(T, d.elem, elem, buf, pos + (i - 1) * es)
      end
      return t
    end
    local n = string.unpack("<i4", buf, pos)
    local cap = es > 0 and math.floor((node.size - 4) / es) or 0
    if n < 0 or n > cap then error("count_over_bound") end
    for i = 1, n do
      t[i] = decode_old_value(T, d.elem, elem, buf, pos + 4 + (i - 1) * es)
    end
    return t
  elseif s == "pointer" or s == "string_blob" or s == "bytes_blob" then
    return nil
  end
  error("fixed: unknown shape " .. tostring(s))
end

-- THE STRICT SET (docs/SPEC-TABLES.md §6.5, §3.4): the fixed form carries its
-- own rule, and a RANGED integer clamps on load (§3.4 THE RECORD), so a value
-- the record states past its declared bound is the `clamped` refusal. The
-- tolerant read keeps BASE's answer; the plain name walks what it decoded once.
local function strict_value(T, d, v)
  if v == nil then return end
  local s = d.shape
  if s == "int" then
    if d.lo ~= nil and d.hi ~= nil and (v < d.lo or v > d.hi) then error("clamped", 0) end
  elseif s == "table" then
    local desc = T.Tables[d.target]
    if desc == nil then return end
    for _, f in ipairs(desc.fields) do
      strict_value(T, f, v[f.name])
    end
  elseif s == "union" then
    local tag = v.tag or 0
    local un = T.Unions[d.target]
    if tag ~= 0 and un ~= nil then
      local arm = un.arms[tag]
      if arm ~= nil and arm.field ~= nil then strict_value(T, arm.field, v[arm.slot]) end
    end
  elseif s == "array" then
    for i = 1, #v do
      strict_value(T, d.elem, v[i])
    end
  end
end

-- F.read parses the file framing (docs/FIXED-FORM-ALGORITHM.md §2): the form
-- byte, the reserved zeroes, the hash, the layout and one record. A fixed
-- table reads BACKWARD (§5.2): the header's hash selects a layout the BUILD
-- laid down, and an older writer's records are read under ITS layout bytes.
-- `strict` is the plain name's read (docs/SPEC-TABLES.md §6.5): the tolerant
-- twin passes false and keeps BASE's answers.
function F.read(T, name, buf, strict)
  if #buf < 1 then error("malformed") end
  local form = buf:byte(1)
  if form ~= formByte then
    if form == 1 then error("previous_form") end
    error("malformed")
  end
  if #buf < 20 then error("malformed") end
  for i = 2, 8 do
    if buf:byte(i) ~= 0 then error("malformed") end
  end
  local hash = string.unpack("<I8", buf, 9)
  local layoutLen = string.unpack("<I4", buf, 17)
  if layoutLen < 4 or 20 + layoutLen > #buf then error("layout_malformed") end
  local layout = string.sub(buf, 21, 20 + layoutLen)
  local fixed = T.Fixed[name]
  if fixed == nil then error("fixed: no fixed descriptor for table " .. tostring(name)) end
  local known, entry, pick = fixed.known, nil, nil
  if known ~= nil then
    for i, k in ipairs(known) do
      if k.hash == hash then
        pick, entry = i, k
        break
      end
    end
    if entry == nil then error("no_layout") end
    if fixed.floor and pick <= fixed.floor then error("layout_unsupported") end
    -- under a KNOWN hash the bytes are only COMPARED to the lock's layout,
    -- never walked: every §1.1 malformation is one name, layout_malformed
    -- (docs/FIXED-FORM-ALGORITHM.md §1.1, §2 step 5, §5.3).
    if #layout ~= #entry.layout or layout ~= entry.layout then error("layout_malformed") end
    -- the tree was built when the module was sealed (F.prepare): a lock's own
    -- layout that does not parse is false there
    if not entry.tree then error("layout_malformed") end
    layout = entry.layout
  end
  local count = string.unpack("<I4", layout, 1)
  if count == 0 or #layout ~= 4 + 17 * count then error("layout_malformed") end
  local rootKind, rootSize
  if entry ~= nil then
    rootKind, rootSize = entry.tree.kind, entry.tree.size
  else
    rootKind = layout:byte(5 + 8)
    rootSize = string.unpack("<I4", layout, 5 + 9)
  end
  if rootKind ~= 13 then error("layout_malformed") end
  if rootSize > maxRecordBytes then error("layout_record_too_large") end
  local recSize
  if entry ~= nil then
    recSize = entry.record
  else
    recSize = 8 + rootSize
  end
  local rest = #buf - 20 - layoutLen
  if recSize <= 8 or rest <= 0 or rest % recSize ~= 0 then error("malformed") end
  if rest / recSize > maxRecords then error("plan_too_large") end
  local at = 21 + layoutLen
  if string.unpack("<I8", buf, at) ~= hash then error("no_layout") end
  local value
  if entry ~= nil and entry.hash ~= fixed.hash then
    value = decode_old_table(T, name, entry.tree, buf, at + 8)
  else
    value = decode_table(T, name, buf, at + 8, rootSize)
  end
  if strict then strict_value(T, { shape = "table", target = name }, value) end
  return value, hash, layout
end

-- F.write writes the header and ONE record (docs/FIXED-FORM-ALGORITHM.md §2,
-- §3.1): the template is the hash and a zeroed body, and each field lands at
-- its constant offset, little-endian, at its declared width.
function F.write(T, name, value)
  local f = T.Fixed[name]
  if f == nil then error("fixed: no fixed descriptor for table " .. tostring(name)) end
  -- docs/SPEC-TABLES.md §2.15: a retired variant is refused, by name
  local refused = D.refuse_deprecated(T, name, value)
  if refused ~= nil then error(refused, 0) end
  local body = encode_table(T, name, value)
  if #body ~= f.size then error("layout_malformed") end
  local header = string.char(formByte) .. zeros(7) .. pack_uint(f.hash, 8) .. pack_uint(#f.layout, 4) .. f.layout
  return header .. pack_uint(f.hash, 8) .. body
end

-- F.prepare builds, once, every derived fact the fixed form reads from a
-- known layout (docs/PORTING.md M13: a descriptor is a constant, never built on
-- first use). D.seal calls it before the module's descriptors are frozen: each
-- known entry of every fixed table gets its layout tree, false for a layout the
-- parser refuses.
function F.prepare(T)
  for _, fixed in pairs(T.Fixed) do
    for _, entry in ipairs(fixed.known or {}) do
      entry.tree = layout_tree(entry.layout) or false
    end
  end
end

return F
