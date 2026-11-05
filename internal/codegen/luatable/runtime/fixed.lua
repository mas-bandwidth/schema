-- fixed.lua — the Lua table runtime's FIXED FORM (docs/SPEC-TABLES.md §3.4,
-- form byte 3), the reader and writer for one record or a batch. Emitted beside
-- the unit's <Package>Tables.lua by internal/codegen/luatable. The layout is
-- built from the descriptors the shape walks (docs/FIXED-FORM-ALGORITHM.md), so
-- a record is the eight-byte hash of the writer's vocabulary block and then the
-- values in declared order, every field at its declared storage width, nothing
-- padded (docs/FIXED-FORM-ALGORITHM.md §1, §3).
--
-- A buffer READ is a Lua string and a cursor; a WRITE is a table of pieces
-- joined once by table.concat. Fixed-width little-endian values are
-- string.unpack/string.pack with a `<` format (docs/PORT-LUA.md).
local D = require("descriptor")
local L = require("load")
local F = {}

local strfind = string.find
local utf8len = utf8.len

local formByte = 3
local maxRecordBytes = 65536
local maxRecords = 4096
local maxPlanEntries = 4096

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

local function check_utf8(s)
  if strfind(s, "\0", 1, true) or utf8len(s) == nil then error("text_ill_formed", 0) end
end

local function check_wstring(t)
  local n = #t
  local i = 1
  while i <= n do
    local u = t[i] or 0
    if u == 0 then error("text_ill_formed", 0) end
    if u >= 0xD800 and u <= 0xDBFF then
      if i + 1 > n or (t[i + 1] or 0) < 0xDC00 or (t[i + 1] or 0) > 0xDFFF then
        error("text_ill_formed", 0)
      end
      i = i + 2
    elseif u >= 0xDC00 and u <= 0xDFFF then
      error("text_ill_formed", 0)
    else
      i = i + 1
    end
  end
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
  if tag < 0 or tag > #ud.arms then tag = 0 end
  local parts = { pack_uint(tag, ud.tag) }
  local arm = ud.arms[tag]
  if arm ~= nil and tag ~= 0 then
    local fd = T.Unions[d.target].arms[tag].field
    if fd ~= nil then
      local payload = encode_value(T, fd, v[arm.slot], arm.size, arm.elem)
      parts[#parts + 1] = payload
      parts[#parts + 1] = zeros(size - ud.tag - #payload)
    else
      parts[#parts + 1] = zeros(size - ud.tag)
    end
  else
    parts[#parts + 1] = zeros(size - ud.tag)
  end
  return table.concat(parts)
end

encode_value = function(T, d, v, size, elemSize)
  local s = d.shape
  if s == "int" then
    if d.timestamp then return pack_int(v or 0, 8, true) end
    -- THE FIXED STORAGE WIDTH IS THE FIELD'S OWN SIZE (docs/
    -- FIXED-FORM-ALGORITHM.md §3): bits(N) rides at 4 or 8 bytes even where
    -- the variable form's descriptor width is narrower.
    return pack_int(v or 0, size, d.signed)
  elseif s == "wide" then
    return pack_wide(v)
  elseif s == "bool" then
    return string.char((v and 1 or 0))
  elseif s == "float" then
    return pack_float(d.f32, v or 0.0)
  elseif s == "string" or s == "bytes" then
    v = v or ""
    local n = #v
    local cap = size - 4
    if n > cap then error("length_over_bound") end
    if s == "string" then check_utf8(v) end
    return string.pack("<i4", n) .. string.sub(v, 1, n) .. zeros(cap - n)
  elseif s == "wstring" then
    v = v or {}
    local n = #v
    local cap = math.floor((size - 4) / 2)
    if n > cap then error("length_over_bound") end
    check_wstring(v)
    local parts = { string.pack("<i4", n) }
    for i = 1, n do
      parts[#parts + 1] = string.pack("<I2", v[i] or 0)
    end
    parts[#parts + 1] = zeros((size - 4) - 2 * n)
    return table.concat(parts)
  elseif s == "enum" then
    local w = T.FixedEnums[d.enum] or 4
    local ord = 0
    if v ~= nil and v ~= false then
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
  local parts = {}
  for i, f in ipairs(desc.fields) do
    local fs = f.fixed_size
    if f.optional then
      local present = v[f.name .. "_present"] == true
      if present and v[f.name] ~= nil then
        parts[#parts + 1] = "\1" .. encode_value(T, f, v[f.name], fs - 1, f.fixed_elem)
      else
        parts[#parts + 1] = "\0" .. zeros(fs - 1)
      end
    else
      parts[#parts + 1] = encode_value(T, f, v[f.name], fs, f.fixed_elem)
    end
  end
  return table.concat(parts)
end

decode_value = function(T, d, buf, pos, size, elemSize)
  local s = d.shape
  if s == "int" then
    local w = d.timestamp and 8 or size
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
    local sub = string.sub(buf, pos + 4, pos + 4 + n - 1)
    if s == "string" then check_utf8(sub) end
    return sub, pos + size
  elseif s == "wstring" then
    local n = string.unpack("<i4", buf, pos)
    local cap = math.floor((size - 4) / 2)
    if n < 0 or n > cap then error("length_over_bound") end
    local t = {}
    for i = 1, n do
      t[i] = string.unpack("<I2", buf, pos + 4 + (i - 1) * 2)
    end
    check_wstring(t)
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
      if fd ~= nil then
        v[arm.slot] = decode_value(T, fd, buf, pos + ud.tag, arm.size, arm.elem)
      end
    end
    return v, pos + size
  elseif s == "array" then
    local t = {}
    if d.elem.shape == "enum" then
      if d.fixed or d.keyed then
        local n = d.bound or #d.keyed
        for i = 1, n do
          local ev = decode_value(T, d.elem, buf, pos + (i - 1) * elemSize, elemSize)
          t[i] = ev == nil and false or ev
        end
        return t, pos + size
      end
      local n = string.unpack("<i4", buf, pos)
      if n < 0 or n > d.bound then error("count_over_bound") end
      for i = 1, n do
        local ev = decode_value(T, d.elem, buf, pos + 4 + (i - 1) * elemSize, elemSize)
        t[i] = ev == nil and false or ev
      end
      return t, pos + size
    end
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
  local cursor = pos
  for i, f in ipairs(desc.fields) do
    local fs = f.fixed_size
    if cursor - pos + fs <= writerSize then
      if f.optional then
        local present = buf:byte(cursor) ~= 0
        v[f.name .. "_present"] = present
        if present then v[f.name] = decode_value(T, f, buf, cursor + 1, fs - 1, f.fixed_elem) end
        cursor = cursor + fs
      else
        v[f.name], cursor = decode_value(T, f, buf, cursor, fs, f.fixed_elem)
      end
    else
      break
    end
  end
  return v
end

-- A LOCKED LAYOUT AS A TREE (docs/FIXED-FORM-ALGORITHM.md §5.2): the layout
-- bytes are pre-order entries of {id, kind, size, children}, and an older
-- writer's record is read against ITS OWN tree, never against this build's
-- descriptors.
local function layout_tree(layout)
  if #layout < 4 then return nil end
  local count = string.unpack("<I4", layout, 1)
  if count == 0 or #layout ~= 4 + 17 * count then return nil end
  if count > maxPlanEntries then
    return { refusal = "plan_too_large", kind = 13, size = 0, children = {} }
  end
  local i = 0
  local function parse()
    i = i + 1
    if i > count then return nil end
    local at = 4 + (i - 1) * 17
    local kind = layout:byte(at + 9)
    if not ((kind >= 1 and kind <= 16) or (kind >= 18 and kind <= 30) or kind == 32 or kind == 33 or kind == 35 or kind == 36) then return nil end
    local size = string.unpack("<I4", layout, at + 10)
    local kids = string.unpack("<I4", layout, at + 14)
    local node = {
      id = string.unpack("<I8", layout, at + 1),
      kind = kind,
      size = size,
      children = {},
    }
    for k = 1, kids do
      local ch = parse()
      if ch == nil then return nil end
      node.children[k] = ch
    end
    return node
  end
  local root = parse()
  if i ~= count or root == nil or root.kind ~= 13 then return nil end
  local function check_bounds(node)
    if node.size > maxRecordBytes then return false end
    if node.kind == 13 then
      if node.size == 0 then return false end
      local sum = 0
      for _, c in ipairs(node.children) do
        sum = sum + c.size
        if not check_bounds(c) then return false end
      end
      if sum ~= node.size then return false end
    elseif node.kind == 35 then
      if #node.children ~= 1 or 1 + node.children[1].size ~= node.size then return false end
      if not check_bounds(node.children[1]) then return false end
    else
      for _, c in ipairs(node.children) do
        if not check_bounds(c) then return false end
      end
    end
    return true
  end
  if not check_bounds(root) then
    return { refusal = "layout_record_too_large", kind = root.kind, size = root.size, children = {} }
  end
  return root
end

local decode_old_value

local function compat_old(d, node)
  if d == nil or node == nil then return false end
  local s = d.shape
  if s == "int" then
    if d.timestamp then return node.kind == 36 and node.size == 8 end
    if d.bits then return node.kind == 32 and (node.size == 4 or node.size == 8) end
    if d.kind and d.kind >= 20 and d.kind <= 29 then return node.kind == d.kind and node.size == d.width end
    return node.kind >= 2 and node.kind <= 9 and node.size <= d.width
  elseif s == "wide" then
    return (node.kind == 18 or node.kind == 19 or (node.kind >= 20 and node.kind <= 29)) and node.size == 16
  elseif s == "bool" then
    return node.kind == 1 and node.size == 1
  elseif s == "float" then
    local w = d.f32 and 4 or 8
    return (node.kind == 10 or node.kind == 11) and node.size <= w
  elseif s == "string" then
    return node.kind == 12 and node.size >= 4
  elseif s == "bytes" then
    return (node.kind == 14 or node.kind == 12) and node.size >= 4
  elseif s == "wstring" then
    return node.kind == 33 and node.size >= 4
  elseif s == "enum" then
    return node.kind == 30 and node.size >= 1
  elseif s == "flags" then
    return node.kind >= 6 and node.kind <= 9
  elseif s == "table" then
    return node.kind == 13
  elseif s == "union" then
    return node.kind == 15
  elseif s == "array" then
    return node.kind == 14 or node.kind == 16
  end
  return false
end

-- decode_old_table walks THIS build's descriptors against the WRITER's tree:
-- a field the older layout does not carry keeps its declared default, and a
-- field both carry is read at the older size (docs/FIXED-FORM-ALGORITHM.md
-- §5.2's PLAN: the writer's sizes, never the reader's).
local function decode_old_table(T, name, node, buf, pos, rep)
  local desc = T.Tables[name]
  if desc == nil then error("fixed: no descriptor for table " .. tostring(name)) end
  local v = T["New" .. name]()
  local byId = {}
  local cursor = pos
  for _, child in ipairs(node.children) do
    if child.id ~= nil and byId[child.id] == nil then
      byId[child.id] = { node = child, pos = cursor }
    end
    cursor = cursor + child.size
  end
  for i, f in ipairs(desc.fields) do
    local hit = f.id and byId[f.id] or nil
    if hit == nil and node.children[i] ~= nil and node.children[i].id == 0 then
      -- fallback when synthetic node id is 0
      hit = nil
    end
    if hit ~= nil then
      local child, cpos = hit.node, hit.pos
      if f.optional then
        if child.kind == 35 then
          local present = buf:byte(cpos) ~= 0
          v[f.name .. "_present"] = present
          if present and child.children[1] ~= nil and compat_old(f, child.children[1]) then
            v[f.name] = decode_old_value(T, f, child.children[1], buf, cpos + 1, rep)
          end
        elseif compat_old(f, child) then
          -- THE PRESENT COMPANION THE READER GAINED IS TRUE for an old T (§5.9
          -- #40): the writer sent the value, so the ?T this reader declares is
          -- present.
          v[f.name .. "_present"] = true
          v[f.name] = decode_old_value(T, f, child, buf, cpos, rep)
        end
      elseif compat_old(f, child) then
        v[f.name] = decode_old_value(T, f, child, buf, cpos, rep)
      end
    end
  end
  return v
end

decode_old_value = function(T, d, node, buf, pos, rep)
  local s = d.shape
  if s == "int" then
    local w = d.timestamp and 8 or (d.bits and (d.bits <= 32 and 4 or 8) or d.width)
    if rep and node.size < w then rep.widened = rep.widened + 1 end
    local signed = (node.kind >= 2 and node.kind <= 5) or d.timestamp
    return unpack_int(buf, pos, node.size, signed)
  elseif s == "wide" then
    return { lo = unpack_int(buf, pos, 8, false), hi = unpack_int(buf, pos + 8, 8, false) }
  elseif s == "bool" then
    return buf:byte(pos) ~= 0
  elseif s == "float" then
    -- an older writer's float32 widens into a float64 field by the ordinary
    -- conversion, exact for every finite float32 (§3.3)
    local w = d.f32 and 4 or 8
    if rep and node.size < w then rep.widened = rep.widened + 1 end
    return (unpack_float(node.size == 4 or node.kind == 10, buf, pos))
  elseif s == "string" or s == "bytes" then
    local n = string.unpack("<i4", buf, pos)
    local cap = node.size - 4
    if n < 0 or n > cap then error("length_over_bound") end
    local sub = string.sub(buf, pos + 4, pos + 4 + n - 1)
    if s == "string" then check_utf8(sub) end
    return sub
  elseif s == "wstring" then
    local n = string.unpack("<i4", buf, pos)
    local cap = math.floor((node.size - 4) / 2)
    if n < 0 or n > cap then error("length_over_bound") end
    local t = {}
    for i = 1, n do
      t[i] = string.unpack("<I2", buf, pos + 4 + (i - 1) * 2)
    end
    check_wstring(t)
    return t
  elseif s == "enum" then
    local w = T.FixedEnums[d.enum] or 4
    if rep and node.size < w then rep.widened = rep.widened + 1 end
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
    return decode_old_table(T, d.target, node, buf, pos, rep)
  elseif s == "union" then
    local nArms = #node.children
    local w = (nArms <= 255 and 1) or (nArms <= 65535 and 2) or 4
    local tag = unpack_int(buf, pos, w, false)
    if tag > nArms or tag > #T.Unions[d.target].arms then tag = 0 end
    local v = { tag = tag }
    if tag ~= 0 then
      local armNode = node.children[tag]
      local arm = T.Unions[d.target].arms[tag]
      if armNode ~= nil and arm ~= nil and arm.field ~= nil and compat_old(arm.field, armNode) then
        v[arm.slot] = decode_old_value(T, arm.field, armNode, buf, pos + w, rep)
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
        local ev = decode_old_value(T, d.elem, elem, buf, pos + (i - 1) * es, rep)
        if ev == nil and d.elem.shape == "enum" then ev = false end
        t[i] = ev
      end
      return t
    end
    local n = string.unpack("<i4", buf, pos)
    local cap = es > 0 and math.floor((node.size - 4) / es) or 0
    if n < 0 or n > cap then error("count_over_bound") end
    for i = 1, n do
      local ev = decode_old_value(T, d.elem, elem, buf, pos + 4 + (i - 1) * es, rep)
      if ev == nil and d.elem.shape == "enum" then ev = false end
      t[i] = ev
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
  local desc = T.Tables[name]
  if desc ~= nil and desc.integrity ~= nil then
    if #buf < 5 or string.unpack("<I4", buf, #buf - 3) ~= L.crc32c(buf, #buf - 4) then
      error("integrity_mismatch")
    end
    buf = buf:sub(1, #buf - 4)
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
    if entry.tree.refusal then error(entry.tree.refusal) end
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
  local out = f.header .. f.hashPrefix .. body
  if f.integrity then
    out = out .. string.pack("<I4", L.crc32c(out, #out))
  end
  return out
end

-- F.measure answers the exact byte length of a batch of `count` records for
-- table `name` (docs/SPEC-TABLES.md §3.4).
function F.measure(T, name, count)
  local f = T.Fixed[name]
  if f == nil or count < 0 then return -1 end
  local n = 20 + #f.layout + count * (8 + f.size)
  local desc = T.Tables[name]
  if desc ~= nil and desc.integrity ~= nil then n = n + 4 end
  return n
end

-- F.save writes the header and a batch of `values` (docs/FIXED-FORM-ALGORITHM.md
-- §2, §3.1), appending the CRC-32C trailer when the root declares integrity.
function F.save(T, name, values)
  local f = T.Fixed[name]
  if f == nil then error("fixed: no fixed descriptor for table " .. tostring(name)) end
  values = values or {}
  local parts = { f.header }
  local hPrefix = f.hashPrefix
  for i = 1, #values do
    local v = values[i]
    local refused = D.refuse_deprecated(T, name, v)
    if refused ~= nil then error(refused, 0) end
    local body = encode_table(T, name, v)
    if #body ~= f.size then error("layout_malformed", 0) end
    parts[#parts + 1] = hPrefix .. body
  end
  local out = table.concat(parts)
  local desc = T.Tables[name]
  if desc ~= nil and desc.integrity ~= nil then
    out = out .. string.pack("<I4", L.crc32c(out, #out))
  end
  return out
end

-- F.load parses a batch fixed-form file under §5.3's named refusal order and
-- returns the array of decoded records, the header hash, and the layout bytes.
function F.load(T, name, buf, strict, cap)
  if #buf < 1 then error("truncated", 0) end
  local form = buf:byte(1)
  if form ~= formByte then
    if form == 1 then error("previous_form", 0) end
    if form == 2 then error("message_form_as_file", 0) end
    error("newer_form", 0)
  end
  local desc = T.Tables[name]
  if desc ~= nil and desc.integrity ~= nil then
    if #buf < 5 or string.unpack("<I4", buf, #buf - 3) ~= L.crc32c(buf, #buf - 4) then
      error("integrity_mismatch", 0)
    end
    buf = buf:sub(1, #buf - 4)
  end
  if #buf < 20 then error("truncated", 0) end
  for i = 2, 8 do
    if buf:byte(i) ~= 0 then error("reserved_not_zero", 0) end
  end
  local hash = string.unpack("<I8", buf, 9)
  local layoutLen = string.unpack("<I4", buf, 17)
  if layoutLen < 4 or 20 + layoutLen > #buf then error("layout_malformed", 0) end
  local layout = string.sub(buf, 21, 20 + layoutLen)
  local fixed = T.Fixed[name]
  if fixed == nil then error("fixed: no fixed descriptor for table " .. tostring(name), 0) end
  local known, entry, pick = fixed.known, nil, nil
  if known ~= nil then
    for i, k in ipairs(known) do
      if k.hash == hash then
        pick, entry = i, k
        break
      end
    end
    if entry == nil then error("layout_unknown", 0) end
    if fixed.floor and pick <= fixed.floor then error("layout_unsupported", 0) end
    if #layout ~= #entry.layout or layout ~= entry.layout then error("layout_malformed", 0) end
    if not entry.tree then error("layout_malformed", 0) end
    if entry.tree.refusal then error(entry.tree.refusal, 0) end
    layout = entry.layout
  end
  local rootSize = entry ~= nil and entry.tree.size or string.unpack("<I4", layout, 5 + 9)
  if rootSize <= 0 or rootSize > maxRecordBytes or (entry ~= nil and entry.record ~= 8 + rootSize) then error("layout_record_too_large", 0) end
  local recSize = entry ~= nil and entry.record or (8 + rootSize)
  local rest = #buf - 20 - layoutLen
  if recSize <= 8 or rest % recSize ~= 0 then error("truncated", 0) end
  local nRec = rest // recSize
  -- THE CAPACITY IS THE CALLER'S ARGUMENT, AND ITS DEFAULT IS 4,096: an
  -- omitted (or nil) capacity is the default, never an unbounded read, while
  -- a caller that sets 4,097 or more may read that many records.
  cap = cap or maxRecords
  if nRec > cap then error("batch_too_large", 0) end
  local rep = (entry ~= nil and entry.hash ~= fixed.hash) and { widened = 0 } or nil
  local out = {}
  for k = 1, nRec do
    local at = 21 + layoutLen + (k - 1) * recSize
    if string.unpack("<I8", buf, at) ~= hash then error("no_layout", 0) end
    local v
    if rep ~= nil then
      v = decode_old_table(T, name, entry.tree, buf, at + 8, rep)
    else
      v = decode_table(T, name, buf, at + 8, rootSize)
    end
    if strict then strict_value(T, { shape = "table", target = name }, v) end
    out[k] = v
  end
  return out, hash, layout, rep and rep.widened or 0
end

-- F.prepare builds, once, every derived fact the fixed form reads from a
-- known layout (docs/PORTING.md M13: a descriptor is a constant, never built on
-- first use). D.seal calls it before the module's descriptors are frozen: each
-- known entry of every fixed table gets its layout tree, false for a layout the
-- parser refuses.
function F.prepare(T)
  -- EACH FIXED FIELD'S SIZE AND ELEMENT WIDTH ARE THE DESCRIPTOR'S
  -- CONSTANTS (docs/PORTING.md M13): the walk never indexes FixedFields per
  -- field. This covers every fixed table AND every fixed type a union arm can
  -- name, which is why it walks FixedFields rather than T.Fixed.
  for name, sizes in pairs(T.FixedFields) do
    local desc = T.Tables[name]
    if desc ~= nil then
      for i, f in ipairs(desc.fields) do
        local s = sizes[i]
        if s ~= nil then
          f.fixed_size = s.size
          f.fixed_elem = s.elem
        end
      end
    end
  end
  for _, fixed in pairs(T.Fixed) do
    local desc = T.Tables[fixed.table]
    fixed.integrity = desc ~= nil and desc.integrity ~= nil
    -- THE WRITER'S HEADER AND HASH PREFIX ARE THE DESCRIPTOR'S CONSTANTS
    -- (docs/PORTING.md M13), built once here rather than on every save.
    fixed.header = string.char(formByte) .. zeros(7) .. pack_uint(fixed.hash, 8) .. pack_uint(#fixed.layout, 4) .. fixed.layout
    fixed.hashPrefix = pack_uint(fixed.hash, 8)
    for _, entry in ipairs(fixed.known or {}) do
      entry.tree = layout_tree(entry.layout) or false
    end
  end
end

return F
