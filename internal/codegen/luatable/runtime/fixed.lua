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

local formByte = 3
local maxRecordBytes = 65536
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
  if strfind(s, "\0", 1, true) or not D.utf8_ok(s) then error("text_ill_formed", 0) end
end

local function zeros(n)
  if n <= 0 then return "" end
  return string.rep("\0", n)
end

-- EVERY DEAD BYTE IN A FIXED RECORD IS ZERO (docs/rulings.json
-- fixed-dead-bytes-are-zero; docs/FIXED-FORM-ALGORITHM.md §3.1, §4.5, §5.3;
-- docs/SPEC-TABLES.md §3.4, §13.10): an absent optional's payload, a counted
-- array's slots past its count, a text's or bytes' units past its used length,
-- and an inactive union arm. A non-zero byte is wire_damaged, and the whole
-- read refuses. The range is [from, to), in Lua's one-based cursor.
local function check_dead(buf, from, to)
  for i = from, to - 1 do
    if buf:byte(i) ~= 0 then error("wire_damaged", 0) end
  end
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
    -- a compressed float's -0 is stored as +0 (§3.3, ruling
    -- fixed-compressed-negzero)
    return pack_float(d.f32, D.cf(d, v or 0.0))
  elseif s == "string" or s == "bytes" then
    v = v or ""
    local n = #v
    local cap = size - 4
    if n > cap then error("length_over_bound") end
    if s == "string" then check_utf8(v) end
    return string.pack("<i4", n) .. string.sub(v, 1, n) .. zeros(cap - n)
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
  for _, f in ipairs(desc.fields) do
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

-- THE BOUNDS PASS REFUSES, IT NEVER CLAMPS (docs/FIXED-FORM-ALGORITHM.md
-- §4.6, docs/plans/strict-only.md): a ranged scalar, a bits(N), a compressed
-- float, an enum ordinal past the writer's top variant and a union tag past
-- the writer's arm count each refuse the whole read as out_of_range.
local function refuse_out_of_range()
  error("out_of_range", 0)
end

-- A BOOL OR A PRESENT BYTE IS 0 OR 1 (docs/FIXED-FORM-ALGORITHM.md §4.5): no
-- writer emits another byte, so any other refuses wire_damaged and is never
-- normalized.
local function bool_byte(buf, pos)
  local b = buf:byte(pos)
  if b > 1 then error("wire_damaged", 0) end
  return b == 1
end

decode_value = function(T, d, buf, pos, size, elemSize)
  local s = d.shape
  if s == "int" then
    local w = d.timestamp and 8 or size
    local signed = d.timestamp or d.signed
    local v = unpack_int(buf, pos, w, signed)
    if d.lo ~= nil and d.hi ~= nil and type(v) == "number" then
      -- unsigned storage compares by math.ult, as the variable walk's
      -- less_than does: an eight-byte raw value's high bit is not a sign
      if d.signed then
        if v < d.lo or v > d.hi then refuse_out_of_range() end
      elseif math.ult(v, d.lo) or math.ult(d.hi, v) then
        refuse_out_of_range()
      end
    end
    -- a bits(N) field past its mask refuses (§4.6), compared unsigned as the
    -- range above is: an eight-byte slot's high bit is not a sign. A declared
    -- range on the same field already held it
    if
      d.lo == nil
      and d.bits ~= nil
      and d.bits > 0
      and d.bits < w * 8
      and type(v) == "number"
      and math.ult((1 << d.bits) - 1, v)
    then
      refuse_out_of_range()
    end
    return v, pos + w
  elseif s == "wide" then
    local v = unpack_wide(buf, pos)
    -- a 128-bit range is held on its raw halves and refuses (§4.6), the
    -- halves compared as the variable walk's less_than compares them
    if d.lo ~= nil and d.hi ~= nil then
      local function lt128(a, b)
        if d.signed then
          if a.hi ~= b.hi then return a.hi < b.hi end
          return math.ult(a.lo, b.lo)
        end
        if a.hi ~= b.hi then return math.ult(a.hi, b.hi) end
        return math.ult(a.lo, b.lo)
      end
      if lt128(v, d.lo) or lt128(d.hi, v) then refuse_out_of_range() end
    end
    return v, pos + size
  elseif s == "bool" then
    return bool_byte(buf, pos), pos + size
  elseif s == "float" then
    local v, nxt = unpack_float(d.f32, buf, pos)
    -- a ranged float past an end refuses (§4.6); a -0 lands as +0,
    -- uncounted (§3.3, the special values)
    if d.fmin ~= nil and d.fmax ~= nil then
      if v < d.fmin or v > d.fmax then
        refuse_out_of_range()
      elseif v == 0 then
        v = 0.0
      end
    end
    return v, nxt
  elseif s == "string" or s == "bytes" then
    local n = string.unpack("<i4", buf, pos)
    local cap = size - 4
    if n < 0 or n > cap then error("length_over_bound") end
    check_dead(buf, pos + 4 + n, pos + size)
    local sub = string.sub(buf, pos + 4, pos + 4 + n - 1)
    if s == "string" then check_utf8(sub) end
    return sub, pos + size
  elseif s == "enum" then
    local w = T.FixedEnums[d.enum] or 4
    local raw = unpack_int(buf, pos, w, false)
    -- an ordinal past the top variant refuses (§4.6)
    if raw > #d.variants then refuse_out_of_range() end
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
    -- a mask bit at or above W refuses (docs/SPEC-TABLES.md §4, A TABLE LOAD
    -- HOLDS A MASK TO ITS WIDTH; fixed.go element)
    local m = unpack_int(buf, pos, 8, false)
    if d.bits ~= nil and d.bits < 64 and m >> d.bits ~= 0 then refuse_out_of_range() end
    return m, pos + 8
  elseif s == "table" then
    local child = T.Tables[d.target].fixed_size
    local v = decode_table(T, d.target, buf, pos, child)
    return v, pos + size
  elseif s == "union" then
    local ud = T.FixedUnions[d.target]
    local tag = unpack_int(buf, pos, ud.tag, false)
    if tag > #ud.arms then refuse_out_of_range() end
    local live = ud.tag
    if tag ~= 0 then
      local arm = ud.arms[tag]
      local fd = T.Unions[d.target].arms[tag].field
      if fd ~= nil then live = live + arm.size end
    end
    check_dead(buf, pos + live, pos + size)
    local v = { tag = tag }
    if tag ~= 0 then
      local arm = ud.arms[tag]
      local fd = T.Unions[d.target].arms[tag].field
      if fd ~= nil then v[arm.slot] = decode_value(T, fd, buf, pos + ud.tag, arm.size, arm.elem) end
    end
    return v, pos + size
  elseif s == "array" then
    local t = {}
    if d.elem.shape == "enum" then
      if d.fixed or d.keyed then
        local n = d.bound or #d.keyed
        for i = 1, n do
          local ev = decode_value(T, d.elem, buf, pos + (i - 1) * elemSize, elemSize)
          if ev == nil then
            t[i] = false
          else
            t[i] = ev
          end
        end
        return t, pos + size
      end
      local n = string.unpack("<i4", buf, pos)
      if n < 0 or n < (d.min or 0) or n > d.bound then error("count_over_bound") end
      check_dead(buf, pos + 4 + n * elemSize, pos + size)
      for i = 1, n do
        local ev = decode_value(T, d.elem, buf, pos + 4 + (i - 1) * elemSize, elemSize)
        if ev == nil then
          t[i] = false
        else
          t[i] = ev
        end
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
    if n < 0 or n < (d.min or 0) or n > d.bound then error("count_over_bound") end
    check_dead(buf, pos + 4 + n * elemSize, pos + size)
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
  for _, f in ipairs(desc.fields) do
    local fs = f.fixed_size
    if cursor - pos + fs <= writerSize then
      if f.optional then
        local present = bool_byte(buf, cursor)
        v[f.name .. "_present"] = present
        if present then
          v[f.name] = decode_value(T, f, buf, cursor + 1, fs - 1, f.fixed_elem)
        else
          check_dead(buf, cursor + 1, cursor + fs)
        end
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
  if count > maxPlanEntries then return { refusal = "plan_too_large", kind = 13, size = 0, children = {} } end
  local i = 0
  local function parse()
    i = i + 1
    if i > count then return nil end
    local at = 4 + (i - 1) * 17
    local kind = layout:byte(at + 9)
    if not ((kind >= 1 and kind <= 16) or (kind >= 18 and kind <= 30) or kind == 32 or kind == 35 or kind == 36) then
      return nil
    end
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
  local bad_tag_width = false
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
    elseif node.kind == 15 then
      -- THE WRITER'S TAG WIDTH IS ITS OWN LAYOUT'S: their_tag := te.size -
      -- widest arm (docs/FIXED-FORM-ALGORITHM.md §5.2 EMIT kind 15), never a
      -- width this reader derives from the arm count. A width that is not an
      -- ordinal width, 1, 2, 4 or 8 (§1's union row), is no tag a lock
      -- records (§5.9 #36): the entry refuses layout_malformed, and nothing
      -- clamps it.
      local widest = 0
      for _, c in ipairs(node.children) do
        if c.size > widest then widest = c.size end
        if not check_bounds(c) then return false end
      end
      local w = node.size - widest
      node.tag = w
      if w ~= 1 and w ~= 2 and w ~= 4 and w ~= 8 then bad_tag_width = true end
    else
      for _, c in ipairs(node.children) do
        if not check_bounds(c) then return false end
      end
    end
    return true
  end
  local bounded = check_bounds(root)
  if bad_tag_width then return nil end
  if not bounded then
    return { refusal = "layout_record_too_large", kind = root.kind, size = root.size, children = {} }
  end
  return root
end

local decode_old_value

-- THE LADDER (docs/FIXED-FORM-ALGORITHM.md §5.1; Go's tableFixedWidens): a
-- writer kind widens into a reader kind only up its own rung, signed 2..5,
-- unsigned 6..9, the fixed(I,F) rungs 20..24 and 25..29, and f32 into f64.
-- Never across signedness, never into or out of the 128-bit kinds.
local function ladder(from, to)
  if from >= 6 and from <= 9 and to >= 6 and to <= 9 then return to > from end
  if from >= 2 and from <= 5 and to >= 2 and to <= 5 then return to > from end
  if from >= 20 and from <= 24 and to >= 20 and to <= 24 then return to > from end
  if from >= 25 and from <= 29 and to >= 25 and to <= 29 then return to > from end
  return from == 10 and to == 11
end

-- A LAWFUL OLDER WRITER NAMES ONLY FIELDS, ARMS, VARIANTS AND KEYS THIS READER
-- RETAINS (§5.1 refuses a removal; deprecation keeps the slot and its name):
-- every child of the writer's node is named among the reader's (Go's
-- tableFixedChildrenNamed).
local function children_named(w, r)
  local named = {}
  for _, c in ipairs(r.children) do
    named[c.id] = true
  end
  for _, c in ipairs(w.children) do
    if not named[c.id] then return false end
  end
  return true
end

local lawful_pair

-- UNLAWFUL (docs/FIXED-FORM-ALGORITHM.md §5.2 MATCH and EMIT, §5.4, §5.9 #30):
-- the writer's layout tree walked beside the READER's own, as Go's
-- tableFixedCompile walks them. A writer field the reader names nowhere, a
-- removed union arm, a removed enum variant or keyed key, a kind moved off
-- every ladder, a shrunk array ([N] or [..N], bytes(N) among them) or a
-- narrowed string(N) makes the handed-in lineage entry UNLAWFUL: no plan is
-- laid, nothing is counted and read past, and a file under its hash refuses
-- by the name returned here before any record lands. The writer's ?T into
-- this reader's bare T is `kind_mismatch` (ruling R2); every other unlawful
-- pair is `layout_malformed`. nil is a lawful pair. desc is this reader's
-- table descriptor (nil where the walk has none); it names only the array
-- head, which the tree does not carry.
local function lawful_table(T, desc, w, r)
  if not children_named(w, r) then return "layout_malformed" end
  local wById = {}
  for _, c in ipairs(w.children) do
    wById[c.id] = c
  end
  local dById = {}
  if desc ~= nil then
    for _, f in ipairs(desc.fields) do
      if f.id ~= nil then dById[f.id] = f end
    end
  end
  for _, rc in ipairs(r.children) do
    local wc = wById[rc.id]
    if wc ~= nil then
      local why = lawful_pair(T, dById[rc.id], wc, rc)
      if why ~= nil then return why end
    end
  end
  return nil
end

lawful_pair = function(T, d, w, r)
  if w.kind ~= r.kind and r.kind ~= 35 then
    if ladder(w.kind, r.kind) then return nil end
    if w.kind == 35 then return "kind_mismatch" end
    return "layout_malformed"
  end
  -- T INTO ?T: the reader wrapped a value the writer carried bare, and the
  -- payload is held to the writer's own entry (§5.2 EMIT, bill §12.8)
  if r.kind == 35 and w.kind ~= 35 then return lawful_pair(T, d, w, r.children[1]) end
  local k = r.kind
  if k == 35 then
    return lawful_pair(T, d, w.children[1], r.children[1])
  elseif k == 13 then
    local desc = d ~= nil and d.shape == "table" and T.Tables[d.target] or nil
    return lawful_table(T, desc, w, r)
  elseif k == 14 then
    -- [N], [..N] and bytes(N): the writer's element count past the reader's
    -- is a shrunk bound (§5.1 "bound narrowed"), refused even when the live
    -- count would fit
    local we, re = w.children[1], r.children[1]
    if we == nil or re == nil then return "layout_malformed" end
    local head = 0
    if d ~= nil and (d.shape == "bytes" or (d.shape == "array" and not d.fixed and not d.keyed)) then head = 4 end
    if w.size < head then return "layout_record_too_large" end
    local theirN = we.size > 0 and (w.size - head) // we.size or 0
    local myN = re.size > 0 and (r.size - head) // re.size or 0
    if theirN > myN then return "layout_malformed" end
    return lawful_pair(T, d ~= nil and d.elem or nil, we, re)
  elseif k == 16 then
    -- [Enum]T: every writer key named by the reader's enum, and no more
    -- writer slots than the reader holds
    local wk, rk, we, re = w.children[1], r.children[1], w.children[2], r.children[2]
    if wk == nil or rk == nil or we == nil or re == nil then return "layout_malformed" end
    if not children_named(wk, rk) then return "layout_malformed" end
    local theirN = we.size > 0 and w.size // we.size or 0
    local myN = re.size > 0 and r.size // re.size or 0
    if theirN > myN then return "layout_malformed" end
    return lawful_pair(T, d ~= nil and d.elem or nil, we, re)
  elseif k == 15 then
    -- a removed arm is unlawful even when the file never takes it
    if not children_named(w, r) then return "layout_malformed" end
    local un = d ~= nil and d.shape == "union" and T.Unions[d.target] or nil
    for i, ra in ipairs(r.children) do
      for _, wa in ipairs(w.children) do
        if wa.id == ra.id then
          local arm = un ~= nil and un.arms[i] or nil
          local why = lawful_pair(T, arm ~= nil and arm.field or nil, wa, ra)
          if why ~= nil then return why end
          break
        end
      end
    end
    return nil
  elseif k == 30 then
    -- a removed variant is unlawful on the grown-width widen and the
    -- same-width remap alike
    if not children_named(w, r) then return "layout_malformed" end
    return nil
  elseif k == 12 then
    -- a narrowed string(N) is unlawful even when the used length would fit
    if w.size > r.size then return "layout_malformed" end
    return nil
  end
  -- a same-kind leaf grown within eight bytes is a ZERO widen; any other
  -- width moved is a narrowed span
  if w.size ~= r.size and not (w.size < r.size and r.size <= 8) then return "layout_malformed" end
  return nil
end

local function compat_old(d, node)
  if d == nil or node == nil then return false end
  local s = d.shape
  if s == "int" then
    if d.timestamp then return node.kind == 36 and node.size == 8 end
    if d.bits then
      -- A bits(N) rides at its declared storage width under the unsigned
      -- integer kind its bit count picks (ir.TableFixedLeafKind,
      -- ir.TableFixedStorageBytes): kinds 6, 7 and 8 at four bytes and kind
      -- 9 at eight. An older writer may have declared a narrower bits(N), so
      -- every kind the walk assigns at those widths is a lawful source.
      if node.size == 4 then return node.kind >= 6 and node.kind <= 8 end
      return node.size == 8 and node.kind == 9
    end
    if d.kind and d.kind >= 20 and d.kind <= 23 then
      return node.kind >= 20 and node.kind <= d.kind and node.size <= d.width
    end
    if d.kind and d.kind >= 25 and d.kind <= 28 then
      return node.kind >= 25 and node.kind <= d.kind and node.size <= d.width
    end
    if d.kind and d.kind >= 20 and d.kind <= 29 then return node.kind == d.kind and node.size == d.width end
    return node.kind >= 2 and node.kind <= 9 and node.size <= d.width
  elseif s == "wide" then
    if d.kind == 18 then
      return (node.kind == 18 and node.size == 16) or (node.kind >= 2 and node.kind <= 5 and node.size <= 8)
    elseif d.kind == 19 then
      return (node.kind == 19 and node.size == 16) or (node.kind >= 6 and node.kind <= 9 and node.size <= 8)
    elseif d.kind == 24 then
      return (node.kind == 24 and node.size == 16) or (node.kind >= 20 and node.kind <= 23 and node.size <= 8)
    elseif d.kind == 29 then
      return (node.kind == 29 and node.size == 16) or (node.kind >= 25 and node.kind <= 28 and node.size <= 8)
    elseif d.signed then
      return ((node.kind == 18 or node.kind == 24) and node.size == 16)
        or (((node.kind >= 2 and node.kind <= 5) or (node.kind >= 20 and node.kind <= 23)) and node.size <= 8)
    else
      return ((node.kind == 19 or node.kind == 29) and node.size == 16)
        or (((node.kind >= 6 and node.kind <= 9) or (node.kind >= 25 and node.kind <= 28)) and node.size <= 8)
    end
  elseif s == "bool" then
    return node.kind == 1 and node.size == 1
  elseif s == "float" then
    local w = d.f32 and 4 or 8
    return (node.kind == 10 or node.kind == 11) and node.size <= w
  elseif s == "string" then
    return node.kind == 12 and node.size >= 4
  elseif s == "bytes" then
    return (node.kind == 14 or node.kind == 12) and node.size >= 4
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
    if child.id ~= nil and byId[child.id] == nil then byId[child.id] = { node = child, pos = cursor } end
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
          local present = bool_byte(buf, cpos)
          v[f.name .. "_present"] = present
          if present and child.children[1] ~= nil and compat_old(f, child.children[1]) then
            v[f.name] = decode_old_value(T, f, child.children[1], buf, cpos + 1, rep)
          elseif not present then
            check_dead(buf, cpos + 1, cpos + child.size)
          end
        elseif compat_old(f, child) then
          -- THE PRESENT COMPANION THE READER GAINED IS TRUE for an old T (§5.9
          -- #40): the writer sent the value, so the ?T this reader declares is
          -- present.
          v[f.name .. "_present"] = true
          v[f.name] = decode_old_value(T, f, child, buf, cpos, rep)
        end
      elseif child.kind == 35 then
        -- A WRITER'S ?T READ INTO A READER'S BARE T refuses by name: the
        -- reader cannot hold an absent value (rulings 2026-11-22, R2)
        error("kind_mismatch", 0)
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
    local signed = (node.kind >= 2 and node.kind <= 5) or (node.kind >= 20 and node.kind <= 23) or d.timestamp
    return unpack_int(buf, pos, node.size, signed)
  elseif s == "wide" then
    if node.size == 16 then return { lo = unpack_int(buf, pos, 8, false), hi = unpack_int(buf, pos + 8, 8, false) } end
    if rep then rep.widened = rep.widened + 1 end
    local signed = (node.kind >= 2 and node.kind <= 5) or (node.kind >= 20 and node.kind <= 23)
    local x = unpack_int(buf, pos, node.size, signed)
    if signed and x < 0 then return { lo = x, hi = -1 } end
    return { lo = x, hi = 0 }
  elseif s == "bool" then
    return bool_byte(buf, pos)
  elseif s == "float" then
    -- an older writer's float32 widens into a float64 field by the ordinary
    -- conversion, exact for every finite float32 (§3.3)
    local w = d.f32 and 4 or 8
    if rep and node.size < w then rep.widened = rep.widened + 1 end
    return D.cf(d, (unpack_float(node.size == 4 or node.kind == 10, buf, pos)))
  elseif s == "string" or s == "bytes" then
    local n = string.unpack("<i4", buf, pos)
    local cap = node.size - 4
    if n < 0 or n > cap then error("length_over_bound") end
    check_dead(buf, pos + 4 + n, pos + node.size)
    local sub = string.sub(buf, pos + 4, pos + 4 + n - 1)
    if s == "string" then check_utf8(sub) end
    return sub
  elseif s == "enum" then
    local w = T.FixedEnums[d.enum] or 4
    if rep and node.size < w then rep.widened = rep.widened + 1 end
    local raw = unpack_int(buf, pos, node.size, false)
    -- THE WRITER'S VARIANT COUNT GOVERNS (§4.6): an ordinal past it refuses
    if raw > #node.children then refuse_out_of_range() end
    local var = raw ~= 0 and d.variants[raw] or nil
    if var ~= nil then
      -- a retired variant reads the declared default (docs/SPEC-TABLES.md §2.15)
      if var.deprecated then return d.default end
      return var[1]
    end
    return nil
  elseif s == "flags" then
    -- an older layout holds a mask to W too (docs/SPEC-TABLES.md §4;
    -- fixedlineage.go element)
    local m = unpack_int(buf, pos, node.size, false)
    if d.bits ~= nil and d.bits < 64 and m >> d.bits ~= 0 then refuse_out_of_range() end
    return m
  elseif s == "table" then
    return decode_old_table(T, d.target, node, buf, pos, rep)
  elseif s == "union" then
    local nArms = #node.children
    -- THE WRITER'S RAW TAG AT THE WRITER'S TAG WIDTH (§5.2 EMIT kind 15, the
    -- ruling of 2026-11-25): te.size - widest arm, laid by layout_tree. A
    -- tag grown 1 -> 8 is read at the width the writer stored it, so the
    -- arm's payload is never read into the tag, and a writer whose tag is
    -- wider than its arm count needs is never read at the narrower width
    -- (test TestLuaFixedR14GrownTagReadsTheWritersTagWidth).
    local w = node.tag
    local tag = string.unpack("<I" .. w, buf, pos)
    -- THE WRITER'S ARM COUNT GOVERNS (§4.6): a tag past it refuses; an
    -- 8-byte tag with its top bit set unpacks negative, and is past it too
    if tag < 0 or tag > nArms then refuse_out_of_range() end
    local live = w
    if tag ~= 0 then
      local armNode = node.children[tag]
      if armNode ~= nil then live = live + armNode.size end
    end
    check_dead(buf, pos + live, pos + node.size)
    if tag > #T.Unions[d.target].arms then tag = 0 end
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
      -- Slots absent from the writer's fixed or enum-keyed array get
      -- the element default (docs/FIXED-FORM-ALGORITHM.md §5.9).
      for i = n + 1, d.bound do
        t[i] = L.zero_elem(T, d.elem)
      end
      return t
    end
    local n = string.unpack("<i4", buf, pos)
    local cap = es > 0 and math.floor((node.size - 4) / es) or 0
    if n < 0 or n > cap then error("count_over_bound") end
    if n < (d.min or 0) then error("count_over_bound") end
    check_dead(buf, pos + 4 + n * es, pos + node.size)
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

-- THE STRICT SET (docs/SPEC-TABLES.md §6.5, §3.4; docs/FIXED-FORM-ALGORITHM.md
-- §4.6): the fixed form carries its own rule, and a RANGED integer past the
-- writer's declared bound (or this reader's when the entry carries no writer
-- range) is the `out_of_range` refusal. A bits(N) past its mask, a ranged
-- 128-bit value and a compressed float past this reader's declared bound
-- refuse by the same name (the entry carries writer ranges for integers
-- only). The plain name walks what it decoded once.
local function strict_value(T, d, v, ranges, wr)
  if v == nil then return end
  local s = d.shape
  if s == "int" then
    local lo, hi = d.lo, d.hi
    if wr ~= nil then
      lo, hi = wr.lo, wr.hi
    end
    if lo ~= nil and hi ~= nil then
      if d.signed then
        if v < lo or v > hi then error("out_of_range", 0) end
      else
        if math.ult(v, lo) or math.ult(hi, v) then error("out_of_range", 0) end
      end
    elseif d.bits ~= nil and d.bits > 0 and d.bits < 64 then
      -- a bits(N) past its mask refuses by the same name (§4.6), compared
      -- unsigned: a bits(N) above 32 bits can land as a negative integer
      if math.ult((1 << d.bits) - 1, v) then error("out_of_range", 0) end
    end
  elseif s == "wide" then
    -- a 128-bit range is held on its raw halves, compared as the identity
    -- walk compares them
    local function lt128(a, b)
      if a.hi ~= b.hi then
        if d.signed then return a.hi < b.hi end
        return math.ult(a.hi, b.hi)
      end
      return math.ult(a.lo, b.lo)
    end
    if wr ~= nil then
      local wlo = type(wr.lo) == "table" and wr.lo or { lo = wr.lo, hi = (d.signed and wr.lo < 0) and -1 or 0 }
      local whi = type(wr.hi) == "table" and wr.hi or { lo = wr.hi, hi = (d.signed and wr.hi < 0) and -1 or 0 }
      if lt128(v, wlo) or lt128(whi, v) then error("out_of_range", 0) end
    end
    if d.lo ~= nil and d.hi ~= nil then
      if lt128(v, d.lo) or lt128(d.hi, v) then error("out_of_range", 0) end
    end
  elseif s == "float" then
    -- a compressed float is a ranged scalar here, held at float32 (§4.6)
    if D.float_off_range(d, v) then error("out_of_range", 0) end
  elseif s == "table" then
    local desc = T.Tables[d.target]
    if desc == nil then return end
    for _, f in ipairs(desc.fields) do
      strict_value(T, f, v[f.name], nil, ranges and ranges[f.id] or nil)
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
      strict_value(T, d.elem, v[i], nil, wr)
    end
  end
end

-- F.write writes the header and ONE record (docs/FIXED-FORM-ALGORITHM.md §2,
-- §3.1): the template is the hash and a zeroed body, and each field lands at
-- its constant offset, little-endian, at its declared width.
function F.write(T, name, value)
  local f = T.Fixed[name]
  if f == nil then error("fixed: no fixed descriptor for table " .. tostring(name)) end
  -- a retired variant (docs/SPEC-TABLES.md §2.15) and a value off the write
  -- contract (docs/SPEC.md §5.1) are refused, by name, before the first byte
  local refused = D.refuse_write(T, name, value)
  if refused ~= nil then return nil, refused end
  local body = encode_table(T, name, value)
  if #body ~= f.size then error("layout_malformed") end
  local out = f.header .. f.hashPrefix .. body
  if f.integrity then out = out .. string.pack("<I4", L.crc32c(out, #out)) end
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
  -- A FIXED-FORM FILE HOLDS AT LEAST ONE RECORD (docs/FIXED-FORM-ALGORITHM.md
  -- §5.3 step 9, docs/SPEC-TABLES.md §22.6): an empty batch is refused, by
  -- the leg's refusal value, before a byte is written.
  if #values == 0 then return nil, "invalid_value" end
  local parts = { f.header }
  local hPrefix = f.hashPrefix
  for i = 1, #values do
    local v = values[i]
    local refused = D.refuse_write(T, name, v)
    if refused ~= nil then return nil, refused end
    local body = encode_table(T, name, v)
    if #body ~= f.size then error("layout_malformed", 0) end
    parts[#parts + 1] = hPrefix .. body
  end
  local out = table.concat(parts)
  local desc = T.Tables[name]
  if desc ~= nil and desc.integrity ~= nil then out = out .. string.pack("<I4", L.crc32c(out, #out)) end
  return out
end

-- F.load parses a batch fixed-form file under §5.3's named refusal order and
-- returns the array of decoded records, the header hash, and the layout bytes.
local function load_impl(T, name, buf, strict, cap)
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
    -- AN UNLAWFUL ENTRY LAYS NO PLAN (§5.4, §5.9 #30): it refuses by name
    -- before any record is read, and nothing is counted
    if entry.unlawful then error(entry.unlawful, 0) end
    layout = entry.layout
  end
  local rootSize = entry ~= nil and entry.tree.size or string.unpack("<I4", layout, 5 + 9)
  if rootSize <= 0 or rootSize > maxRecordBytes or (entry ~= nil and entry.record ~= 8 + rootSize) then
    error("layout_record_too_large", 0)
  end
  local recSize = entry ~= nil and entry.record or (8 + rootSize)
  local rest = #buf - 20 - layoutLen
  -- A FIXED-FORM FILE HOLDS AT LEAST ONE RECORD (§5.3 step 9, SPEC-TABLES
  -- §22.6): a header and a layout with no record behind them is refused,
  -- never read as a batch of none.
  if recSize <= 8 or rest <= 0 or rest % recSize ~= 0 then error("truncated", 0) end
  local nRec = rest // recSize
  -- THE CAPACITY IS THE CALLER'S ARGUMENT: an omitted (or nil) capacity is
  -- the file's own record count, as Go's destination slice is, so a file is
  -- never refused for a limit the spec does not state (§5.3 step 9); only a
  -- caller-set capacity below the count refuses batch_too_large.
  cap = cap or nRec
  -- a read into ONE value holds no value to read from a file of none (§5.3
  -- step 9), as the Go engine refuses it at a capacity of one
  if nRec == 0 and cap == 1 then error("truncated", 0) end
  if nRec > cap then error("batch_too_large", 0) end
  local rep = (entry ~= nil and entry.hash ~= fixed.hash) and { widened = 0 } or nil
  local out = {}
  for k = 1, nRec do
    local at = 21 + layoutLen + (k - 1) * recSize
    if string.unpack("<I8", buf, at) ~= hash then error("no_layout", 0) end
    local v
    if rep ~= nil then
      v = decode_old_table(T, name, entry.tree, buf, at + 8, rep)
      -- the identity walk refuses its bounds as it decodes; an older
      -- writer's plan is held to them after (§4.6)
      if strict then strict_value(T, { shape = "table", target = name }, v, entry.ranges) end
    else
      v = decode_table(T, name, buf, at + 8, rootSize)
    end
    out[k] = v
  end
  -- the fixed form has no unknown census (§5.4, §5.9 #30): a lawful entry
  -- names nothing this reader lacks, so the unknown count is always 0
  return out, hash, layout, rep and rep.widened or 0, 0
end

-- F.load is a whole file's read: the records, the layout hash and bytes,
-- and the widened and unknown counts (unknown is always 0: an unlawful
-- lineage entry refuses rather than being counted).
function F.load(T, name, buf, strict, cap, report, into)
  if report ~= nil then
    report.damaged = false
    report.refused = 0
    report.unknown = 0
    report.kindChanged = 0
    report.outOfRange = 0
    report.widened = 0
    report.repeatedKey = 0
    report.sharedBlob = 0
    report.missingRequired = 0
    report.reason = nil
    report.Reason = nil
  end
  local ok, out, h, l, w, u = pcall(load_impl, T, name, buf, strict, cap)
  if not ok then
    if report ~= nil or into ~= nil then
      if report ~= nil then
        report.damaged = true
        report.refused = 1
        report.reason = tostring(out)
        report.Reason = report.reason
      end
      if into ~= nil and T["New" .. name] ~= nil then
        local new = T["New" .. name]
        for i = 1, #into do
          D.copy_into(into[i], new())
        end
      end
      return nil
    end
    error(out, 0)
  end
  if into ~= nil and T["New" .. name] ~= nil then
    local new = T["New" .. name]
    local n = #out
    for i = 1, n do
      if into[i] == nil then into[i] = new() end
      D.copy_into(into[i], out[i])
    end
    for i = n + 1, #into do
      into[i] = nil
    end
    return into, h, l, w, u
  end
  return out, h, l, w, u
end

-- F.read is one record's read, and it is the load at a capacity of one
-- (docs/FIXED-FORM-ALGORITHM.md §4, ONE path): every file gets the load's
-- verdict, and the value, its layout hash and bytes come back.
function F.read(T, name, buf, strict)
  local ok, out, h, l = pcall(load_impl, T, name, buf, strict, 1)
  if not ok then error(out, 0) end
  return out[1], h, l
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
    fixed.header = string.char(formByte)
      .. zeros(7)
      .. pack_uint(fixed.hash, 8)
      .. pack_uint(#fixed.layout, 4)
      .. fixed.layout
    fixed.hashPrefix = pack_uint(fixed.hash, 8)
    -- EACH OLDER ENTRY'S LAWFULNESS IS A PROPERTY OF THE (WRITER LAYOUT,
    -- READER) PAIR, decided here once per known entry: entry.unlawful is the
    -- name a file under its hash refuses, or false (lawful_table)
    local own = layout_tree(fixed.layout)
    for _, entry in ipairs(fixed.known or {}) do
      entry.tree = layout_tree(entry.layout) or false
      entry.unlawful = false
      if entry.hash ~= fixed.hash and entry.tree and not entry.tree.refusal then
        if not own or own.refusal then
          entry.unlawful = own and own.refusal or "layout_malformed"
        else
          entry.unlawful = lawful_table(T, desc, entry.tree, own) or false
        end
      end
    end
  end
end

return F
