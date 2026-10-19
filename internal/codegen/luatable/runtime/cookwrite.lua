-- cookwrite.lua — the Lua table runtime's COOK WRITE side (docs/SPEC-TABLES.md
-- §7.6, §7.2; docs/PORT-LUA.md). Emitted beside the unit's <Package>Tables.lua
-- by internal/codegen/luatable, which embeds runtime/*.lua by glob.
--
-- Cook(T, name, value) writes the same cooked file internal/tablecook writes
-- from the same wire: the header of §7.1, the region of §7.2 (the depth-first
-- pre-order numbering of §3.1, every node at align_up(offset, alignof) for its
-- own type, every field written piece by piece at the layout the descriptor
-- names), and the attribution part of §7.6. Every byte no field covers is
-- zero. CookMeasure(T, name, value) answers that file's exact length.
--
-- Cook refuses (nil) on the same inputs the tool does: a data cycle, a map (the
-- tool's cook surface refuses a map-bearing unit by name, §2.8), a pointer the
-- numbering does not carry, and storage a counted array's live count does not
-- reach that would need an extent (§7.6).
local C = {}

local BLOB_HEADER = 8
local BLOB_ALIGN = 8
local HEADER_BYTES = 64
local MAGIC = 0x4B4F4F434D484353

local function align_up(n, a)
  if a <= 1 then return n end
  return (n + a - 1) & ~(a - 1)
end

local function max(a, b) if a > b then return a end return b end

-- THE WRITER (docs/PORT-LUA.md, BYTE BUFFERS): chunks placed by absolute
-- offset, joined once, with zeroes for every byte no field wrote (§7.2).
local function W()
  return { chunks = {} }
end

local function put(w, at, s)
  if w == nil or s == nil or #s == 0 then return end
  w.chunks[#w.chunks + 1] = { at = at, s = s }
end

-- little-endian, the tool's default order (`tablecook.Options{}`, §7.6)
local function put_uint(w, at, v, width)
  if width == 1 then put(w, at, string.char(v & 0xff))
  elseif width == 2 then put(w, at, string.pack("<I2", v & 0xffff))
  elseif width == 4 then put(w, at, string.pack("<I4", v & 0xffffffff))
  elseif width == 8 then put(w, at, string.pack("<i8", v))
  elseif width == 16 then
    put(w, at, string.pack("<i8", v))
    put(w, at + 8, string.pack("<i8", v < 0 and -1 or 0))
  end
end

local function put_f32(w, at, v) put(w, at, string.pack("<f", v)) end
local function put_f64(w, at, v) put(w, at, string.pack("<d", v)) end

-- THE NUMBERING (§3.1, internal/tablewire/nodes.go): the depth-first pre-order
-- over pointer edges, the root first at index one. It is the save's walk
-- (runtime/save.lua), which is the wire's own numbering.
local function guard_holds(fd, v)
  local g = fd.guard
  if g == nil then return true end
  for _, t in ipairs(g) do
    if (v[t.field] == true) ~= t.want then return false end
  end
  return true
end

local function bytes_less(a, b)
  local n = math.min(#a, #b)
  local i = 1
  while i <= n do
    local j = math.min(i + 255, n)
    local ca, cb = a:sub(i, j), b:sub(i, j)
    if ca ~= cb then
      for k = 1, j - i + 1 do
        local x, y = ca:byte(k), cb:byte(k)
        if x ~= y then return x < y end
      end
    end
    i = j + 1
  end
  return #a < #b
end

local function map_entries(cache, fd, x)
  if x == nil then return {} end
  local got = cache[x]
  if got ~= nil then return got end
  local kf = fd.key_field
  local keys = {}
  for key in pairs(x) do keys[#keys + 1] = key end
  table.sort(keys, function(a, b)
    if kf.shape == "string" then return bytes_less(a, b) end
    if kf.signed or kf.width < 8 then return a < b end
    return math.ult(a, b)
  end)
  local keyname = kf.name
  local valname = fd.value_field.name
  got = {}
  for i, key in ipairs(keys) do got[i] = { [keyname] = key, [valname] = x[key] } end
  cache[x] = got
  return got
end

local function count_of(fd, v)
  local s = fd.shape
  if s == "bytes" then return #v end
  if fd.list then return #v end
  if fd.fixed then return fd.bound or 0 end
  return math.min(#v, fd.bound or 0)
end

local edge_value, edge_union, edges

edge_value = function(T, fd, val, holder, name, out, cache)
  local s = fd.shape
  if s == "string_blob" or s == "bytes_blob" then
    if val ~= nil and val ~= false then
      out[#out + 1] = { blob = true, shape = s, name = name, holder = holder, val = val }
    end
  elseif s == "pointer" then
    if val ~= nil and val ~= false then out[#out + 1] = { blob = false, target = fd.target, val = val } end
  elseif s == "table" then
    local d = T.Tables[fd.target]
    if d then edges(T, d, val, out, cache) end
  elseif s == "array" then
    local n = count_of(fd, val)
    local e = fd.elem
    for i = 1, n do edge_value(T, e, val[i], val, i, out, cache) end
  elseif s == "union" then
    edge_union(T, T.Unions[fd.target], val, out, cache)
  elseif s == "map" then
    local d = T.Tables[fd.entry]
    for _, kv in ipairs(map_entries(cache, fd, val)) do edges(T, d, kv, out, cache) end
  end
end

edge_union = function(T, u, val, out, cache)
  local tag = type(val) == "table" and val.tag or 0
  if tag == 0 or u == nil then return end
  local arm = u.arms[tag]
  if arm == nil or arm.field == nil then return end
  edge_value(T, arm.field, val[arm.name], val, arm.name, out, cache)
end

edges = function(T, desc, v, out, cache)
  if v == nil then return out end
  for _, fd in ipairs(desc.fields) do
    if guard_holds(fd, v) then edge_value(T, fd, v[fd.name], v, fd.name, out, cache) end
  end
  return out
end

local function number_nodes(T, desc, root)
  local nodes = { list = {}, index = {}, entries = {} }
  nodes.index[root] = 1
  local open = { [root] = true }
  local stack = { { v = root, edges = edges(T, desc, root, {}, nodes.entries), i = 0 } }
  while #stack > 0 do
    local top = stack[#stack]
    if top.i >= #top.edges then open[top.v] = nil; stack[#stack] = nil
    else
      local e = top.edges[top.i + 1]
      top.i = top.i + 1
      if e.blob then
        if nodes.index[e.val] == nil then
          local tid = e.shape == "string_blob" and T.StringBlobTypeId or T.BytesBlobTypeId
          local body = type(e.val) == "table" and e.val.value or e.val
          nodes.list[#nodes.list + 1] = { typeId = tid, body = body, string = e.shape == "string_blob", val = e.val }
          nodes.index[e.val] = #nodes.list + 1
        end
      else
        if open[e.val] then return nil end
        if nodes.index[e.val] == nil then
          local d = T.Tables[e.target]
          if d == nil then return nil end
          nodes.list[#nodes.list + 1] = { typeId = d.type_id, desc = d, v = e.val }
          nodes.index[e.val] = #nodes.list + 1
          open[e.val] = true
          stack[#stack + 1] = { v = e.val, edges = edges(T, d, e.val, {}, nodes.entries), i = 0 }
        end
      end
    end
  end
  local entries = { { typeId = desc.type_id, desc = desc, v = root } }
  for _, n in ipairs(nodes.list) do entries[#entries + 1] = n end
  return entries, nodes.index
end

-- THE DEFAULT OF ONE ARRAY SLOT (§2.9, tabletext.elementZero): zero for a
-- scalar, the element type's own declared defaults for a record.
local function zero_elem(T, e)
  local s = e.shape
  if s == "bool" then return false end
  if s == "int" or s == "flags" then return 0 end
  if s == "float" then return 0.0 end
  if s == "string" or s == "bytes" then return "" end
  if s == "wstring" then return {} end
  if s == "enum" then return nil end
  if s == "pointer" or s == "string_blob" or s == "bytes_blob" then return false end
  if s == "table" then
    local d = T.Tables[e.target]
    if d and T["New" .. d.name] then return T["New" .. d.name]() end
    return {}
  end
  if s == "union" then return { tag = 0 } end
  return nil
end

-- enum_width is the ORDINAL slot's storage width (SPEC §4.2): the smallest
-- unsigned width fitting the variant count.
local function enum_width(f)
  local n = #(f.variants or {})
  if n <= 0xff then return 1 end
  if n <= 0xffff then return 2 end
  if n <= 0xffffffff then return 4 end
  return 8
end

-- the value a field writes when it was never set: its declared default, at the
-- same places the generated constructor puts one (§3, §4.2)
local function field_value(T, f, v)
  local x = v and v[f.name]
  if x ~= nil then return x end
  local s = f.shape
  if s == "table" then
    local d = T.Tables[f.target]
    if d and T["New" .. d.name] then return T["New" .. d.name]() end
    return {}
  elseif s == "union" then return { tag = 0 }
  elseif s == "wstring" then return {}
  elseif s == "array" or s == "map" then return x or {}
  elseif s == "pointer" or s == "string_blob" or s == "bytes_blob" then return nil
  elseif s == "string" or s == "bytes" then return f.default or ""
  elseif s == "enum" then return f.default
  elseif s == "bool" then return f.default == true
  elseif s == "int" or s == "flags" or s == "float" then return f.default or 0
  end
  return x
end

local write_record, write_field_value, write_array, write_value

-- write_record writes one record's own storage (§7.2). Unbounded arrays are the
-- extent walk's (they stay zero here, exactly as regionWriter leaves them).
write_record = function(T, desc, v, base, w, index)
  for _, f in ipairs(desc.fields) do
    local at = base + f.at
    local s = f.shape
    if s == "map" then return nil
    elseif s == "array" and f.list then
      -- the extent writer places it
    else
      local x = field_value(T, f, v)
      if f.optional then
        local present = v ~= nil and v[f.name .. "_present"] == true
        put_uint(w, base + present_offset(T, f), present and 1 or 0, 1)
      end
      if not write_field_value(T, f, x, at, w, index) then return nil end
    end
  end
  return true
end

-- write_field_value writes one VALUE at its record address: scalar, string,
-- pointer, array, nested record or union (§7.2).
write_field_value = function(T, f, x, at, w, index)
  local s = f.shape
  if s == "map" then return nil
  elseif s == "pointer" then
    return write_ref(T, f, x, at, w, index)
  elseif s == "string_blob" or s == "bytes_blob" then
    return write_ref(T, f, x, at, w, index)
  elseif s == "array" then
    return write_array(T, f, x, at, w, index)
  elseif s == "string" then
    local str = x or ""
    if f.bound and #str > f.bound then str = str:sub(1, f.bound) end
    put(w, at, str)
    put_uint(w, at + (f.companion or 0), #str, 4)
    return true
  elseif s == "bytes" then
    local str = x or ""
    if f.bound and #str > f.bound then str = str:sub(1, f.bound) end
    put(w, at, str)
    put_uint(w, at + (f.companion or 0), #str, 4)
    return true
  elseif s == "wstring" then
    local units = x or {}
    local n = math.min(#units, f.bound or #units)
    for i = 1, n do put(w, at + (i - 1) * 2, string.pack("<I2", units[i] & 0xffff)) end
    put_uint(w, at + (f.companion or 0), n, 4)
    return true
  elseif s == "bool" then
    put_uint(w, at, x and 1 or 0, 1)
    return true
  elseif s == "int" then
    put_uint(w, at, x or 0, f.width)
    return true
  elseif s == "flags" then
    put_uint(w, at, x or 0, 8)
    return true
  elseif s == "float" then
    local v = x
    if v == nil then v = f.default or 0.0 end
    if f.f32 then put_f32(w, at, v) else put_f64(w, at, v) end
    return true
  elseif s == "enum" then
    local v = 0
    if x ~= nil and x ~= false then
      for i, var in ipairs(f.variants) do if var[1] == x then v = i; break end end
    end
    put_uint(w, at, v, enum_width(f))
    return true
  elseif s == "table" then
    local d = T.Tables[f.target]
    if d == nil then return nil end
    return write_record(T, d, x, at, w, index)
  elseif s == "union" then
    local u = T.Unions[f.target]
    if u == nil then return nil end
    local tag = type(x) == "table" and (x.tag or 0) or 0
    put_uint(w, at, tag, u.tag)
    if tag == 0 then return true end
    local arm = u.arms[tag]
    if arm == nil then return nil end
    if arm.field ~= nil then
      local af = arm.field
      local armAt = at + u.arm_offset + (af.at or 0)
      local ax = type(x) == "table" and x[arm.name] or nil
      if af.shape == "table" then
        local d = T.Tables[af.target]
        if d == nil then return nil end
        if not write_record(T, d, ax, armAt, w, index) then return nil end
      else
        if not write_field_value(T, af, field_value(T, af, { [af.name] = ax }), armAt, w, index) then return nil end
      end
    end
    return true
  end
  return nil
end

-- write_ref writes one self-relative reference slot (§7.2, §6.3): zero is
-- null, anything else is target - slot, and a target the numbering does not
-- carry refuses (§7.6).
write_ref = function(T, f, x, at, w, index)
  if x == nil or x == false then
    put_uint(w, at, 0, 8)
    return true
  end
  local target = index[x]
  if target == nil then return nil end
  local entry = target.entry
  put_uint(w, at, HEADER_BYTES + entry.offset - at, 8)
  return true
end

-- write_array writes a fixed, counted or enum-keyed array's record storage
-- (§7.2): EVERY slot is written, a counted array's slots past the live count
-- taking the value-initialized element.
write_array = function(T, f, x, at, w, index)
  if f.list then return true end
  x = x or {}
  local n = f.bound or 0
  if not f.fixed and f.keyed == nil then
    local used = math.min(#x, n)
    put_uint(w, at + (f.companion or 0), used, 4)
    for i = 1, n do
      local e = x[i]
      if e == nil then e = zero_elem(T, f.elem) end
      if not write_field_value(T, f.elem, e, at + (i - 1) * f.elem_step, w, index) then return nil end
    end
  else
    for i = 1, n do
      local e = x[i]
      if e == nil then e = zero_elem(T, f.elem) end
      if not write_field_value(T, f.elem, e, at + (i - 1) * f.elem_step, w, index) then return nil end
    end
  end
  return true
end

-- present_offset is the address of an optional's presence bool: the last
-- storage piece, after the value's own pieces (§7.2, ir.FieldPieces).
function present_offset(T, f)
  local at = f.at
  local s = f.shape
  local cur
  if s == "string" then
    cur = align_up(at + (f.bound or 0) + 1, 4) + 4
  elseif s == "wstring" then
    cur = align_up(at + ((f.bound or 0) + 1) * 2, 4) + 4
  elseif s == "bytes" then
    cur = align_up(at + (f.bound or 0), 4) + 4
  elseif s == "array" then
    if f.list then cur = at + 16
    else
      cur = at + (f.elem_size or 0) * (f.bound or 0)
      if not f.fixed and f.keyed == nil then cur = align_up(cur, 4) + 4 end
    end
  elseif s == "map" then
    cur = at + 16
  elseif s == "pointer" or s == "string_blob" or s == "bytes_blob" then
    cur = at + 8
  elseif s == "table" then
    local d = T.Tables[f.target]
    cur = at + (d and d.size or 0)
  elseif s == "union" then
    local u = T.Unions[f.target]
    cur = at + (u and u.size or 0)
  elseif s == "bool" then cur = at + 1
  elseif s == "float" then cur = at + (f.f32 and 4 or 8)
  elseif s == "flags" then cur = at + 8
  elseif s == "enum" then cur = at + enum_width(f)
  elseif s == "int" then cur = at + f.width
  else cur = at end
  return cur
end

-- THE EXTENT PREDICATES (§2.9, tablecook.recordHasExtent): a record whose
-- by-value closure holds a list (or a map, which the cook refuses).
local record_has_extent, union_has_extent, field_has_extent
record_has_extent = function(T, desc)
  if desc == nil then return false end
  for _, f in ipairs(desc.fields) do
    if field_has_extent(T, f) then return true end
  end
  return false
end
union_has_extent = function(T, un)
  if un == nil then return false end
  for _, arm in ipairs(un.arms) do
    if arm.field ~= nil and field_has_extent(T, arm.field) then return true end
  end
  return false
end
field_has_extent = function(T, f)
  if f.shape == "map" or (f.shape == "array" and f.list) then return true end
  if f.shape == "pointer" or f.shape == "string_blob" or f.shape == "bytes_blob" then return false end
  if f.shape == "table" then return record_has_extent(T, T.Tables[f.target]) end
  if f.shape == "union" then return union_has_extent(T, T.Unions[f.target]) end
  return false
end

-- THE EXTENT WALK (tablecook/extent.go, §2.9, §7.2): after the record, each
-- unbounded array's whole body at its element alignment, then — element by
-- element in index order — the arrays its elements hold by value. It measures
-- with w nil and writes with w set; the two share this one walk.
local carve_field, carve_element, carve_extent

carve_extent = function(T, desc, v, recordBase, extentBase, cursor, w)
  for _, f in ipairs(desc.fields) do
    local x = field_value(T, f, v)
    if not carve_field(T, f, x, recordBase, extentBase, cursor, w) then return false end
  end
  return true
end

carve_field = function(T, f, x, recordBase, extentBase, cursor, w)
  local at = recordBase + (f.at or 0)
  local s = f.shape
  if s == "map" then return false end
  if s == "array" and f.list then
    x = x or {}
    local n = #x
    local size = f.elem_size or 0
    local align = f.elem_align or 1
    cursor.at = align_up(cursor.at, align)
    local start = cursor.at
    cursor.at = start + n * size
    local array = extentBase + start
    if n == 0 then put_uint(w, at, 0, 8) else put_uint(w, at, array - at, 8) end
    put_uint(w, at + 8, n, 4)
    if w ~= nil then
      for i = 1, n do
        local e = x[i]
        if e == nil then e = zero_elem(T, f.elem) end
        if not write_field_value(T, f.elem, e, array + (i - 1) * size, w, cursor.index) then return false end
      end
    end
    if field_has_extent(T, f.elem) then
      for i = 1, n do
        local e = x[i]
        if e == nil then e = zero_elem(T, f.elem) end
        if not carve_element(T, f.elem, e, array + (i - 1) * size, extentBase, cursor, w) then return false end
      end
    end
    return true
  end
  if s == "pointer" or s == "string_blob" or s == "bytes_blob" then return true end
  if s == "array" then
    x = x or {}
    local n
    if f.fixed or f.keyed ~= nil then n = f.bound or 0
    else n = math.min(#x, f.bound or 0) end
    for i = 1, n do
      local e = x[i]
      if e == nil then e = zero_elem(T, f.elem) end
      if not carve_element(T, f.elem, e, at + (i - 1) * (f.elem_step or 0), extentBase, cursor, w) then return false end
    end
    return true
  end
  if s == "table" then
    local d = T.Tables[f.target]
    if d == nil then return false end
    if not record_has_extent(T, d) then return true end
    return carve_extent(T, d, x, at, extentBase, cursor, w)
  end
  if s == "union" then
    local u = T.Unions[f.target]
    if u == nil then return false end
    if not union_has_extent(T, u) then return true end
    local tag = type(x) == "table" and (x.tag or 0) or 0
    if tag == 0 then return true end
    local arm = u.arms[tag]
    if arm == nil or arm.field == nil then return true end
    return carve_field(T, arm.field, type(x) == "table" and x[arm.name] or nil, at + u.arm_offset, extentBase, cursor, w)
  end
  return true
end

carve_element = function(T, e, x, at, extentBase, cursor, w)
  local s = e.shape
  if s == "table" then
    local d = T.Tables[e.target]
    if d == nil then return false end
    if not record_has_extent(T, d) then return true end
    return carve_extent(T, d, x, at, extentBase, cursor, w)
  elseif s == "union" then
    local u = T.Unions[e.target]
    if u == nil then return false end
    if not union_has_extent(T, u) then return true end
    local tag = type(x) == "table" and (x.tag or 0) or 0
    if tag == 0 then return true end
    local arm = u.arms[tag]
    if arm == nil or arm.field == nil then return true end
    return carve_field(T, arm.field, type(x) == "table" and x[arm.name] or nil, at + u.arm_offset, extentBase, cursor, w)
  end
  return true
end

-- THE LAYOUT (§7.2, tablecook.Layout): the root at zero, each node at
-- align_up(offset, its own alignment), a blob node at eight, a record with an
-- extent rounded to the region floor first, then the data length rounded to the
-- region's alignment. It fills each entry's offset and extent in place.
local function layout(T, name, v)
  local desc = T.Tables[name]
  if desc == nil then return nil end
  local entries = number_nodes(T, desc, v)
  if entries == nil then return nil end
  local index = {}
  for i, e in ipairs(entries) do
    local key = e.v
    if key == nil then key = e.val end
    if key ~= nil then index[key] = { entry = e } end
    e.index = i
  end
  local offset = 0
  local align = 8
  for _, e in ipairs(entries) do
    local size, nodeAlign = 0, 8
    if e.desc ~= nil then
      size = e.desc.size
      nodeAlign = e.desc.align
      align = max(align, nodeAlign)
      offset = align_up(offset, nodeAlign)
      e.offset = offset
      local record = size
      local extent = 0
      if record_has_extent(T, e.desc) then
        record = align_up(size, 8)
        local cursor = { at = 0, index = index }
        if not carve_extent(T, e.desc, e.v, 0, 0, cursor, nil) then return nil end
        extent = cursor.at
      end
      e.record = record
      e.extent = extent
      offset = offset + record + extent
    else
      size = BLOB_HEADER + #e.body + (e.string and 1 or 0)
      offset = align_up(offset, BLOB_ALIGN)
      e.offset = offset
      e.size = size
      e.record = size
      e.extent = 0
      offset = offset + size
    end
  end
  offset = align_up(offset, align)
  return {
    desc = desc,
    entries = entries,
    index = index,
    align = align,
    data = offset,
    total = HEADER_BYTES + offset + #entries * 16,
  }
end

-- CookMeasure answers the whole file's length (§7.6), -1 when the cook refuses.
function C.CookMeasure(T, name, v)
  local l = layout(T, name, v)
  if l == nil then return -1 end
  return l.total
end

-- Cook writes the cook (§7.6, §7.1, §7.2): header, data part, attribution part.
-- The capacity is the measure's answer; short input refuses and writes nothing.
function C.Cook(T, name, v, capacity)
  local l = layout(T, name, v)
  if l == nil then return nil end
  if capacity ~= nil and capacity < l.total then return nil end
  local w = W()
  for _, e in ipairs(l.entries) do
    if e.desc ~= nil then
      local base = HEADER_BYTES + e.offset
      if not write_record(T, e.desc, e.v, base, w, l.index) then return nil end
      if e.extent > 0 then
        local cursor = { at = 0, index = l.index }
        if not carve_extent(T, e.desc, e.v, base, base + e.record, cursor, w) then return nil end
        if cursor.at ~= e.extent then return nil end
      end
    else
      local base = HEADER_BYTES + e.offset
      put_uint(w, base, #e.body, 4)
      put(w, base + BLOB_HEADER, e.body)
    end
  end
  -- the header (§7.1)
  put_uint(w, 0, MAGIC, 8)
  put_uint(w, 8, T.BuildVersion, 8)
  put_uint(w, 16, 1, 8)
  put_uint(w, 24, l.data, 8)
  put_uint(w, 32, #l.entries * 16, 8)
  put_uint(w, 40, l.align, 8)
  put_uint(w, 48, 0, 8)
  put_uint(w, 56, 0, 8)
  -- the attribution part (§7.6): offset then type id, in index order
  local at = HEADER_BYTES + l.data
  for i, e in ipairs(l.entries) do
    put_uint(w, at + (i - 1) * 16, e.offset, 8)
    put_uint(w, at + (i - 1) * 16 + 8, e.typeId, 8)
  end
  return assemble(w, l.total)
end

-- assemble joins the pieces in offset order, filling every unclaimed byte with
-- zero (§7.2). Overlapping pieces are trimmed to the later write.
function assemble(w, total)
  table.sort(w.chunks, function(a, b) return a.at < b.at end)
  local parts = {}
  local pos = 0
  for _, c in ipairs(w.chunks) do
    if c.at > pos then
      parts[#parts + 1] = string.rep("\0", c.at - pos)
      pos = c.at
    end
    if c.at < pos then
      local skip = pos - c.at
      if skip < #c.s then
        parts[#parts + 1] = c.s:sub(skip + 1)
        pos = c.at + #c.s
      end
    else
      parts[#parts + 1] = c.s
      pos = c.at + #c.s
    end
  end
  if pos < total then parts[#parts + 1] = string.rep("\0", total - pos) end
  return table.concat(parts)
end

return C
