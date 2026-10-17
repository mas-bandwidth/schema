-- save.lua — the Lua table runtime's VARIABLE FORM (form 1) SAVE and its
-- MEASURE (docs/SPEC-TABLES.md §3, §3.1; docs/PORT-LUA.md, THE CARD MAP). A
-- port of the JavaScript leg's TableVariableSave walk
-- (internal/codegen/jstable/variableruntime.go) and of the Go reference engine
-- (internal/tablewire encode.go, nodes.go, nodetable.go). Emitted beside the
-- unit's <Package>Tables.lua by internal/codegen/luatable, which embeds
-- runtime/*.lua by glob.
--
-- THE SAVE is DRIVEN BY THE DESCRIPTOR (§8.1): the form byte, the root's
-- fields, elision at the declared default, the node table for pointers in
-- first-visit order, the terminator, then the id table and its count. A data
-- cycle or a value with no wire identity refuses the save (nil).
local S = {}

local NODE_TABLE_ID = -1         -- 0xffffffffffffffff
local STRING_BLOB_ID = 0x704be0d8faaffc58
local BYTES_BLOB_ID = 0x2f2ec0474f1c4fe4
local MAX64 = 0xffffffffffffffff

-- THE WRITER (docs/PORT-LUA.md, BYTE BUFFERS): a table of pieces joined once
-- by table.concat.
local function W()
  return { parts = {}, n = 0 }
end

local function w_raw(w, s)
  w.parts[#w.parts + 1] = s
  w.n = w.n + #s
end

local function w_u8(w, v)
  w_raw(w, string.char(v & 0xff))
end

-- CANONICAL LEB128 (§3), unsigned, of a Lua integer's low 64 bits.
local function w_leb(w, v)
  v = v & MAX64
  local out = {}
  repeat
    local b = v & 0x7f
    v = v >> 7
    if v ~= 0 then out[#out + 1] = string.char(b | 0x80) else out[#out + 1] = string.char(b) end
  until v == 0
  w_raw(w, table.concat(out))
end

local function w_u16(w, v) w_raw(w, string.pack("<I2", v)) end
local function w_u32(w, v) w_raw(w, string.pack("<I4", v)) end
local function w_u64(w, v) w_raw(w, string.pack("<i8", v)) end
local function w_f64(w, v) w_raw(w, string.pack("<d", v)) end
local function w_f32(w, v) w_raw(w, string.pack("<f", v)) end

local function w_int(w, width, v)
  if width == 1 then w_u8(w, v)
  elseif width == 2 then w_u16(w, v)
  elseif width == 4 then w_u32(w, v)
  else w_u64(w, v) end
end

-- A FLOAT32 RIDES AS ITS IEEE-754 BIT PATTERN (§3): a NaN carries its payload
-- in the top of the double's mantissa. Lua has no NaN payload plumbing, so a
-- float32 field is written with the standard pack, which preserves the value.
local function narrow_f32(v)
  return string.pack("<f", v)
end

-- THE NODE NUMBERING (§3.1, internal/tablewire/nodes.go): first-visit order of
-- a depth-first pre-order walk from the root over POINTER EDGES, descending
-- through every by-value edge to reach them. Node index 1 is the root and
-- record k is index k + 1. A reference to a node whose descent is still open
-- is a DATA CYCLE, and the save refuses it (nil).
local number_nodes
local edges

local function count_of(fd, v)
  local s = fd.shape
  if s == "bytes" then return #v end
  if fd.list then return #v end
  return math.min(#v, fd.bound or 0)
end

-- the pointer and blob edges of one node's by-value closure, in the walk's
-- order: fields in declaration order, array elements in index order.
edges = function(T, desc, v, out)
  for _, fd in ipairs(desc.fields) do
    local val = v[fd.name]
    local s = fd.shape
    if s == "string_blob" or s == "bytes_blob" then
      if val ~= nil then out[#out + 1] = { blob = true, fd = fd, holder = v, val = val } end
    elseif s == "pointer" then
      if val ~= nil then out[#out + 1] = { blob = false, target = fd.target, val = val } end
    elseif s == "table" then
      local d = T.Tables[fd.target]
      if d then edges(T, d, val, out) end
    elseif s == "array" then
      local n = count_of(fd, val)
      local e = fd.elem
      local es = e.shape
      if es == "pointer" then
        for i = 1, n do if val[i] ~= nil then out[#out + 1] = { blob = false, target = e.target, val = val[i] } end end
      elseif es == "table" then
        local d = T.Tables[e.target]
        for i = 1, n do if d then edges(T, d, val[i], out) end end
      end
    end
  end
  return out
end

number_nodes = function(T, desc, root)
  local nodes = { list = {}, index = {}, slots = {} }
  nodes.index[root] = 1
  local open = { [root] = true }
  local stack = { { v = root, edges = edges(T, desc, root, {}), i = 0 } }
  while #stack > 0 do
    local top = stack[#stack]
    if top.i >= #top.edges then open[top.v] = nil; stack[#stack] = nil
    else
      local e = top.edges[top.i + 1]
      top.i = top.i + 1
      if e.blob then
        if e.fd.shape == "string_blob" and type(e.val) == "string" then
          local m = nodes.slots[e.holder]
          if m == nil then m = {}; nodes.slots[e.holder] = m end
          if m[e.fd.name] == nil then
            nodes.list[#nodes.list + 1] = { typeId = STRING_BLOB_ID, body = e.val }
            m[e.fd.name] = #nodes.list + 1
          end
        else
          if nodes.index[e.val] == nil then
            local tid = e.fd.shape == "string_blob" and STRING_BLOB_ID or BYTES_BLOB_ID
            nodes.list[#nodes.list + 1] = { typeId = tid, body = e.val }
            nodes.index[e.val] = #nodes.list + 1
          end
        end
      else
        if open[e.val] then return nil end
        if nodes.index[e.val] ~= nil then
        else
          local d = T.Tables[e.target]
          if d == nil then return nil end
          nodes.list[#nodes.list + 1] = { typeId = d.type_id, codec = d, v = e.val }
          nodes.index[e.val] = #nodes.list + 1
          open[e.val] = true
          stack[#stack + 1] = { v = e.val, edges = edges(T, d, e.val, {}), i = 0 }
        end
      end
    end
  end
  return nodes
end

local write_body
local write_field

-- ELISION AT THE DECLARED DEFAULT (§3): a pointer and a blob at nil, an array
-- when empty, a map when empty, a nested table when its body is its terminator
-- alone, and any other field equal to its declared default.
local function at_default(fd, v)
  local x = v[fd.name]
  local s = fd.shape
  if s == "pointer" or s == "string_blob" or s == "bytes_blob" then return x == nil end
  if s == "array" then return count_of(fd, x) == 0 end
  if s == "map" then
    local n = 0
    for _ in pairs(x or {}) do n = n + 1 end
    return n == 0
  end
  return x == fd.default
end

local function id_ref(ids, id)
  for i, v in ipairs(ids.list) do if v == id then return i end end
  ids.list[#ids.list + 1] = id
  return #ids.list
end

-- THE SAVE WALK (§3, §3.1). write_field writes into the writer `w`. It
-- returns false on a refusal; elision at the declared default writes nothing.
write_field = function(T, ids, nodes, fd, v, w)
  local x = v[fd.name]
  local s = fd.shape
  if s == "bool" then
    if not at_default(fd, v) then
      w_leb(w, id_ref(ids, fd.id)); w_u8(w, 1); w_u8(w, x and 1 or 0)
    end
  elseif s == "int" or s == "flags" then
    if not at_default(fd, v) then
      w_leb(w, id_ref(ids, fd.id)); w_u8(w, fd.kind); w_int(w, fd.width, x or 0)
    end
  elseif s == "float" then
    if not at_default(fd, v) then
      w_leb(w, id_ref(ids, fd.id)); w_u8(w, fd.kind)
      if fd.f32 then w_raw(w, narrow_f32(x)) else w_f64(w, x) end
    end
  elseif s == "string" then
    if not at_default(fd, v) then
      w_leb(w, id_ref(ids, fd.id)); w_u8(w, 12); w_leb(w, #x); w_raw(w, x)
    end
  elseif s == "bytes" then
    if not at_default(fd, v) then
      w_leb(w, id_ref(ids, fd.id)); w_u8(w, 14)
      local body = W()
      w_u8(body, 6); w_leb(body, #x); w_raw(body, x)
      w_leb(w, body.n); w_raw(w, table.concat(body.parts))
    end
  elseif s == "enum" then
    if not at_default(fd, v) then
      local vref = 0
      if x ~= nil then
        for _, var in ipairs(fd.variants) do if var[1] == x then vref = id_ref(ids, var[2]); break end end
      end
      if vref == 0 and x ~= nil then return false end
      w_leb(w, id_ref(ids, fd.id)); w_u8(w, 30); w_leb(w, vref)
    end
  elseif s == "pointer" then
    if x ~= nil then w_leb(w, id_ref(ids, fd.id)); w_u8(w, 17); w_leb(w, nodes.index[x]) end
  elseif s == "string_blob" or s == "bytes_blob" then
    if x ~= nil then
      local idx
      if s == "string_blob" and type(x) == "string" then idx = nodes.slots[v][fd.name] else idx = nodes.index[x] end
      w_leb(w, id_ref(ids, fd.id)); w_u8(w, 17); w_leb(w, idx)
    end
  elseif s == "table" then
    local d = T.Tables[fd.target]
    local mark = #ids.list
    local ref = id_ref(ids, fd.id)
    local body = W()
    local ok = write_body(T, ids, nodes, d, x, body)
    if not ok then return false end
    if body.n <= 1 then ids.list[mark + 1] = nil; return true end
    w_leb(w, ref); w_u8(w, 13); w_leb(w, body.n); w_raw(w, table.concat(body.parts))
  elseif s == "array" then
    local n = count_of(fd, x)
    if n == 0 then return true end
    w_leb(w, id_ref(ids, fd.id)); w_u8(w, 14)
    local body = W()
    w_u8(body, fd.elem_kind); w_leb(body, n)
    for i = 1, n do
      if not write_element(T, ids, nodes, fd.elem, x[i], body) then return false end
    end
    w_leb(w, body.n); w_raw(w, table.concat(body.parts))
  elseif s == "map" then
    local n = 0
    for _ in pairs(x or {}) do n = n + 1 end
    if n == 0 then return true end
    w_leb(w, id_ref(ids, fd.id)); w_u8(w, 14)
    local body = W()
    w_u8(body, fd.elem_kind or 13); w_leb(body, n)
    local entry = T.Tables[fd.entry]
    local keyname = fd.key_field.name
    local valname = fd.value_field.name
    for key, value in pairs(x) do
      local eb = W()
      local kv = { [keyname] = key, [valname] = value }
      local ok = write_body(T, ids, nodes, entry, kv, eb)
      if not ok then return false end
      w_leb(body, eb.n); w_raw(body, table.concat(eb.parts))
    end
    w_leb(w, body.n); w_raw(w, table.concat(body.parts))
  end
  return true
end

local write_element

write_element = function(T, ids, nodes, e, val, body)
  local s = e.shape
  if s == "bool" then w_u8(body, val and 1 or 0)
  elseif s == "int" or s == "flags" then w_int(body, e.width, val or 0)
  elseif s == "float" then if e.f32 then w_raw(body, narrow_f32(val)) else w_f64(body, val) end
  elseif s == "enum" then
    local vref = 0
    if val ~= nil then for _, var in ipairs(e.variants) do if var[1] == val then vref = id_ref(ids, var[2]); break end end end
    if vref == 0 and val ~= nil then return false end
    w_leb(body, vref)
  elseif s == "pointer" then w_leb(body, val == nil and 0 or (nodes.index[val] or 0))
  elseif s == "table" then
    local d = T.Tables[e.target]
    local eb = W()
    if not write_body(T, ids, nodes, d, val, eb) then return false end
    w_leb(body, eb.n); w_raw(body, table.concat(eb.parts))
  elseif s == "union" then
    local u = T.Unions[e.target]
    if u == nil then return false end
    if not write_union(T, ids, nodes, u, val, body) then return false end
  elseif s == "string" then w_leb(body, #val); w_raw(body, val)
  elseif s == "wstring" then
    local parts = {}
    for i = 1, #val do parts[#parts + 1] = string.pack("<I2", val[i]) end
    w_leb(body, #table.concat(parts)); w_raw(body, table.concat(parts))
  elseif s == "bytes" then w_leb(body, #val); w_raw(body, val)
  end
  return true
end

write_body = function(T, ids, nodes, desc, v, w)
  for _, fd in ipairs(desc.fields) do
    if not write_field(T, ids, nodes, fd, v, w) then return false end
  end
  w_u8(w, 0)
  return true
end

local write_union
local write_union_at

write_union = function(T, ids, nodes, u, val, w)
  local tag = val.tag or 0
  if tag == 0 then w_leb(w, 0); return true end
  local arm = u.arms[tag]
  if arm == nil then return false end
  local f = arm.field
  if f == nil then
    w_leb(w, id_ref(ids, arm.id)); w_u8(w, 32); w_leb(w, 0)
    return true
  end
  w_leb(w, id_ref(ids, arm.id)); w_u8(w, f.kind)
  local body = W()
  if not write_union_at(T, ids, nodes, f, val[arm.name], body) then return false end
  w_leb(w, body.n); w_raw(w, table.concat(body.parts))
  return true
end

write_union_at = function(T, ids, nodes, f, val, body)
  local s = f.shape
  if s == "bool" then w_u8(body, val and 1 or 0)
  elseif s == "int" or s == "flags" then w_int(body, f.width, val or 0)
  elseif s == "float" then if f.f32 then w_raw(body, narrow_f32(val)) else w_f64(body, val) end
  elseif s == "string" then w_leb(body, #val); w_raw(body, val)
  elseif s == "enum" then
    local vref = 0
    if val ~= nil then for _, var in ipairs(f.variants) do if var[1] == val then vref = id_ref(ids, var[2]); break end end end
    if vref == 0 and val ~= nil then return false end
    w_leb(body, vref)
  elseif s == "pointer" then w_leb(body, val == nil and 0 or (nodes.index[val] or 0))
  elseif s == "table" then
    local d = T.Tables[f.target]
    if not write_body(T, ids, nodes, d, val) then return false end
  elseif s == "union" then
    local inner = T.Unions[f.target]
    if not write_union(T, ids, nodes, inner, val, body) then return false end
  elseif s == "array" then
    local n = count_of(f, val)
    w_u8(body, f.elem_kind); w_leb(body, n)
    for i = 1, n do if not write_element(T, ids, nodes, f.elem, val[i], body) then return false end end
  end
  return true
end

-- THE SAVE (docs/SPEC-TABLES.md §3, §3.1): the form byte, the root's fields,
-- the node table LAST in the root body under the reserved id at kind 12, the
-- terminator, then the id table and its count. A data cycle or a value with no
-- wire identity refuses the save (nil).
function S.Save(T, name, v)
  local desc = T.Tables[name]
  if desc == nil then return nil end
  local nodes = number_nodes(T, desc, v)
  if nodes == nil then return nil end
  local idx = W()
  local ids = { list = {} }
  w_u8(idx, 1)
  for _, fd in ipairs(desc.fields) do
    if not write_field(T, ids, nodes, fd, v, idx) then return nil end
  end
  if #nodes.list > 0 then
    w_leb(idx, id_ref(ids, NODE_TABLE_ID))
    w_u8(idx, 12)
    local sub = W()
    w_leb(sub, #nodes.list)
    for _, n in ipairs(nodes.list) do
      w_leb(sub, id_ref(ids, n.typeId))
      if n.codec == nil then
        w_leb(sub, #n.body); w_raw(sub, n.body)
      else
        local body = W()
        if not write_body(T, ids, nodes, n.codec, n.v, body) then return nil end
        w_leb(sub, body.n); w_raw(sub, table.concat(body.parts))
      end
    end
    w_leb(idx, sub.n); w_raw(idx, table.concat(sub.parts))
  end
  w_u8(idx, 0)
  for _, id in ipairs(ids.list) do w_u64(idx, id) end
  w_u64(idx, #ids.list)
  return table.concat(idx.parts)
end

-- THE MEASURE: the length of what Save would write — the saved length.
function S.Measure(T, name, v)
  local saved = S.Save(T, name, v)
  if saved == nil then return nil end
  return #saved
end

return S
