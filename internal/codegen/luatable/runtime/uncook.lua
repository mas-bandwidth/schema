-- uncook.lua — the Lua table runtime's read of an OPENED cook
-- (docs/SPEC-TABLES.md §7.2; docs/PORT-LUA.md). Emitted beside the unit's
-- <Package>Tables.lua by internal/codegen/luatable, which embeds runtime/*.lua
-- by glob.
--
-- Cook.Open proved this file is this build's cook, so a read takes the region
-- on its own terms: the data part is Lock's region written verbatim, the root
-- at zero, each field at its descriptor's offset, and a reference an int64
-- self-relative delta the directory names. The value it builds is the one the
-- variable-form load builds (§4): the same storage, so a cook and a wire of one
-- instance read to equal values.
--
-- It decodes no wire framing and follows no LEB128: every scalar is at its
-- storage width, an enum slot is the ORDINAL (not the wire id), a map's entries
-- are by-value records in the holder's node extent, and a list's elements lie
-- at the slot's delta (§7.2).
local U = {}

local cook = require("cook")

local function u8(ctx, at)
  return string.byte(ctx.data, at + 1) or 0
end

local function u16(ctx, at)
  return string.unpack("<I2", ctx.data, at + 1)
end

local function u32(ctx, at)
  return string.unpack("<I4", ctx.data, at + 1)
end

local function i32(ctx, at)
  return string.unpack("<i4", ctx.data, at + 1)
end

local function i64(ctx, at)
  return string.unpack("<i8", ctx.data, at + 1)
end

local function align_up(n, a)
  if a <= 1 then return n end
  return (n + a - 1) & ~(a - 1)
end

-- enum_width is the ORDINAL slot's storage width (docs/SPEC-TABLES.md §4.2,
-- §7.2): the smallest byte width the variant count fits, which is how the
-- cook sizes the slot. It mirrors the Go oracle's StorageBitsFor(Max).
local function enum_width(f)
  local n = #(f.variants or {})
  if n <= 0xff then return 1 end
  if n <= 0xffff then return 2 end
  if n <= 0xffffffff then return 4 end
  return 8
end

-- one INTEGER at its storage width, sign-extended where the declaration is
-- signed (docs/SPEC-TABLES.md §7.2): the region is the storage, not the wire.
local function int_at(ctx, at, width, signed)
  local v
  if width == 1 then
    v = u8(ctx, at)
  elseif width == 2 then
    v = u16(ctx, at)
  elseif width == 4 then
    v = u32(ctx, at)
  else
    -- 64 bits kept as the Lua integer with the same bits (docs/PORT-LUA.md,
    -- 64-BIT AND 128-BIT INTEGERS)
    v = i64(ctx, at)
  end
  if signed and width < 8 then
    local top = 1 << (width * 8 - 1)
    if v >= top then v = v - (1 << (width * 8)) end
  end
  return v
end

local function fresh(T, desc)
  local ctor = T["New" .. desc.name]
  if ctor == nil then return {} end
  return ctor()
end

local read_record, read_union, read_array, read_map, read_field, read_element

-- present_offset is the address of an optional's presence bool: the last
-- storage piece, after the value's own pieces (docs/SPEC-TABLES.md §7.2,
-- ir.FieldPieces). It is the address the cook's region puts it at, computed
-- from the descriptor the way the cook writer lays the pieces out (§19.3), so
-- a read and a write of one record agree.
local function present_offset(T, f)
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
    if f.list then
      cur = at + 16
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
  elseif s == "bool" then
    cur = at + 1
  elseif s == "float" then
    cur = at + (f.f32 and 4 or 8)
  elseif s == "flags" then
    cur = at + 8
  elseif s == "enum" then
    cur = at + enum_width(f)
  elseif s == "int" then
    cur = at + f.width
  else
    cur = at
  end
  return cur
end

-- A REFERENCE (§7.2): zero is null; any other delta resolves to slot + delta,
-- which the directory names. A table target is the node's ONE instance, so two
-- slots naming one node land one Lua reference; a byte buffer target is its
-- node's box, the load runtime's own shape (§3.1, §6.3).
local function resolve(ctx, at, shape)
  local delta = i64(ctx, at)
  if delta == 0 then return nil end
  local i = ctx.off[at + delta]
  if i == nil then return nil end
  if shape == "string_blob" or shape == "bytes_blob" then
    local box = ctx.boxes[i]
    if box == nil then
      local e = ctx.dir[i]
      local n = u32(ctx, e.offset)
      box = { value = string.sub(ctx.data, e.offset + 8 + 1, e.offset + 8 + n) }
      ctx.boxes[i] = box
    end
    return box
  end
  return ctx.nodes[i]
end

-- one VALUE of a field's declared kind at its own offset (docs/SPEC-TABLES.md
-- §7.2). The second answer is false for an ABSENT ?T, whose storage keeps the
-- constructor's declared default (§2.3).
read_field = function(ctx, f, base)
  local at = base + f.at
  if f.optional and u8(ctx, base + present_offset(ctx.T, f)) == 0 then return nil, false end
  local s = f.shape
  local v
  if s == "bool" then
    v = u8(ctx, at) ~= 0
  elseif s == "int" then
    v = int_at(ctx, at, f.width, f.signed)
  elseif s == "flags" then
    -- a flags slot is a uint64 in every target (§7.2)
    v = int_at(ctx, at, 8, false)
  elseif s == "float" then
    -- every read refuses a NaN or an infinity on its raw bits
    -- (docs/SPEC-TABLES.md §3.3), the uncook included: the scan Read runs first refuses
    -- one, and this test holds the rule for a handle a caller checked itself
    if f.f32 then
      if string.unpack("<I4", ctx.data, at + 1) & 0x7f800000 == 0x7f800000 then
        ctx.nonFinite = true
        return nil, false
      end
      v = string.unpack("<f", ctx.data, at + 1)
    else
      if string.unpack("<i8", ctx.data, at + 1) & 0x7ff0000000000000 == 0x7ff0000000000000 then
        ctx.nonFinite = true
        return nil, false
      end
      v = string.unpack("<d", ctx.data, at + 1)
    end
  elseif s == "string" or s == "bytes" then
    local n = i32(ctx, at + f.companion)
    if n < 0 then n = 0 end
    v = string.sub(ctx.data, at + 1, at + n)
  elseif s == "wstring" then
    -- the used length is in CODE UNITS and each unit is two bytes in the
    -- cook's order (§7.2)
    local n = i32(ctx, at + f.companion)
    local units = {}
    for i = 0, n - 1 do
      units[#units + 1] = u16(ctx, at + i * 2)
    end
    v = units
  elseif s == "enum" then
    -- an ENUM slot holds the ORDINAL, not the wire's variant-name hash
    -- (§7.2); zero is None, and the output stores nil (false in a sequence)
    local ord = int_at(ctx, at, enum_width(f), false)
    if ord == 0 then
      v = nil
    else
      local variant = f.variants[ord]
      v = variant and variant[1] or nil
      -- a retired variant reads the declared default (docs/SPEC-TABLES.md §2.15)
      if variant and variant.deprecated then v = f.default end
    end
  elseif s == "pointer" or s == "string_blob" or s == "bytes_blob" then
    v = resolve(ctx, at, s)
  elseif s == "table" then
    local st = ctx.T.Tables[f.target]
    local inst = fresh(ctx.T, st)
    read_record(ctx, inst, st, at)
    v = inst
  elseif s == "union" then
    v = read_union(ctx, f.target, at)
  elseif s == "map" then
    v = read_map(ctx, f, at)
  elseif s == "array" then
    v = read_array(ctx, f, at)
  end
  return v, true
end

-- zero_elem is a sequence slot's zero (§2.9, the load runtime's own): a
-- sequence cannot hold nil, so a null pointer or None enum is false.
local function zero_elem(T, e)
  local s = e.shape
  if s == "bool" then return false end
  if s == "int" or s == "flags" then return 0 end
  if s == "float" then return 0.0 end
  if s == "string" or s == "bytes" then return "" end
  if s == "wstring" then return {} end
  if s == "enum" then return false end
  if s == "pointer" or s == "string_blob" or s == "bytes_blob" then return false end
  if s == "table" then
    local d = T.Tables[e.target]
    if d then return fresh(T, d) end
    return {}
  end
  if s == "union" then return { tag = 0 } end
  return false
end

read_element = function(ctx, e, at)
  local v = read_field(ctx, e, at)
  if
    v == nil and (e.shape == "enum" or e.shape == "pointer" or e.shape == "string_blob" or e.shape == "bytes_blob")
  then
    return false
  end
  return v
end

-- AN ARRAY's slots (§7.2, §2.9): fixed holds its bound, a counted array holds
-- its live count (the companion), a keyed array holds E.Max positional slots,
-- and an unbounded array's elements lie at the slot's self-relative delta.
read_array = function(ctx, f, at)
  local out = {}
  if f.list then
    local delta = i64(ctx, at)
    local count = i32(ctx, at + 8)
    if delta ~= 0 then
      local start = at + delta
      for i = 0, count - 1 do
        out[#out + 1] = read_element(ctx, f.elem, start + i * f.elem_step)
      end
    end
    return out
  end
  if f.fixed then
    for i = 0, f.bound - 1 do
      out[#out + 1] = read_element(ctx, f.elem, at + i * f.elem_step)
    end
    return out
  end
  if f.keyed ~= nil then
    for i = 1, f.bound do
      out[i] = zero_elem(ctx.T, f.elem)
    end
    for i = 0, f.bound - 1 do
      out[i + 1] = read_element(ctx, f.elem, at + i * f.elem_step)
    end
    return out
  end
  local count = i32(ctx, at + f.companion)
  if count < 0 then count = 0 end
  for i = 0, count - 1 do
    out[#out + 1] = read_element(ctx, f.elem, at + i * f.elem_step)
  end
  return out
end

-- A MAP's entries are by-value records inside the holder's node extent, in
-- ascending key order (§2.8, §7.2). The key and the value are the entry's own
-- fields; a null pointer or blob value is false, an entry that is still an
-- entry (§3.1).
read_map = function(ctx, f, at)
  local out = {}
  local delta = i64(ctx, at)
  if delta == 0 then return out end
  local entry = ctx.T.Tables[f.entry]
  local count = i32(ctx, at + 8)
  local start = at + delta
  local kf, vf = f.key_field, f.value_field
  for i = 0, count - 1 do
    local inst = fresh(ctx.T, entry)
    read_record(ctx, inst, entry, start + i * entry.size)
    local key = inst[kf.name]
    local value = inst[vf.name]
    if value == nil and (vf.shape == "pointer" or vf.shape == "string_blob" or vf.shape == "bytes_blob") then
      value = false
    end
    out[key] = value
  end
  return out
end

-- A UNION is a TAG beside its arms (§2.6, §7.2): only the set arm's bytes are
-- live, and an arm is read as its field line is. A tag naming no arm reads
-- None; a pointer arm that is null leaves its key absent, as the load does.
read_union = function(ctx, target, at)
  local un = ctx.T.Unions[target]
  if un == nil then return { tag = 0 } end
  local tag = int_at(ctx, at, un.tag, false)
  if tag == 0 or tag > #un.arms then return { tag = 0 } end
  local arm = un.arms[tag]
  local out = { tag = arm.tag }
  local f = arm.field
  if f ~= nil then
    local v, present = read_field(ctx, f, at + un.arm_offset)
    if present and v ~= nil then out[arm.slot] = v end
  end
  return out
end

-- ONE RECORD (§7.2): every field at its offset. A field under a FALSE guard is
-- elided by the writer and is not an edge (§3, §3.1), so it keeps the
-- constructor's declared default; the unguarded fields are read first, because
-- a guard's terms name bool fields of the same record.
read_record = function(ctx, v, st, base)
  if st == nil then return end
  for _, f in ipairs(st.fields) do
    if f.guard == nil then
      local value, present = read_field(ctx, f, base)
      if present then v[f.name] = value end
      if f.optional then v[f.name .. "_present"] = present end
    end
  end
  for _, f in ipairs(st.fields) do
    if f.guard ~= nil then
      local live = true
      for _, t in ipairs(f.guard) do
        if v[t.field] ~= t.want then
          live = false
          break
        end
      end
      if live then
        local value, present = read_field(ctx, f, base)
        if present then v[f.name] = value end
        if f.optional then v[f.name .. "_present"] = present end
      end
    end
  end
end

-- U.Read reads an OPENED cook's region into the root value. h is the handle
-- Cook.Open answered; the directory names every node, so each table node is
-- built once and a reference to it reads the same Lua value wherever it is
-- named (docs/SPEC-TABLES.md §7.2, §3.1). Open reads only the header (§7.1),
-- so a read of every node first runs cook-check's two passes (§7.4) over a
-- handle no Check has proved: the walk below follows the directory and every
-- reference, and it is O(n) already, so the scan costs it nothing in order.
-- A cook the scan refuses reads nil and the reason.
function U.Read(T, h)
  if h.dir == nil then
    local checked, why = cook.Check(T, h.name, h.bytes)
    if checked == nil then return nil, why end
    h.dir = checked.dir
  end
  local data = string.sub(h.bytes, h.data_offset + 1, h.data_offset + h.data_length)
  local by_id = {}
  for _, d in pairs(T.Tables) do
    by_id[d.type_id] = d
  end
  local ctx = { T = T, h = h, data = data, dir = h.dir, off = {}, nodes = {}, boxes = {} }
  for i, e in ipairs(h.dir) do
    ctx.off[e.offset] = i
  end
  for i, e in ipairs(h.dir) do
    local st = by_id[e.type_id]
    if st ~= nil then ctx.nodes[i] = fresh(T, st) end
  end
  for i, e in ipairs(h.dir) do
    local st = by_id[e.type_id]
    if st ~= nil then read_record(ctx, ctx.nodes[i], st, e.offset) end
  end
  if ctx.nonFinite then return nil, "non_finite_float" end
  return ctx.nodes[1]
end

return U
