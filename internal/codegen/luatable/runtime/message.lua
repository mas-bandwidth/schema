-- THE MESSAGE FORM'S RUNTIME (docs/SPEC-TABLES.md §3.3), form byte 2: the
-- announcement reader and the batch verbs, against the module's own descriptor
-- table (the vocabulary is the unit's own closure, emitted by messageform.go as
-- _M.MessageVocabulary in projection order). The wire is the bitpacked batch:
-- form byte 2, an eight-bit count (M-1), then M bodies as one bit stream, then
-- zero pad to the byte boundary.
--
-- A body is a sequence of fields, each a REFERENCE at refBits followed by a
-- payload at its declared width; reference 0 is the body terminator. Elision
-- and the declared defaults are the variable form's (§3). Damage is terminal
-- for the batch. A pointered root carries the §3.3 node table as its first
-- field: the reserved node-table id, a 32-bit count and the records, then the
-- root body.
local M = {}

local NODE_ID = 0xffffffffffffffff
local STRING_BLOB_ID = 0x704be0d8faaffc58
local BYTES_BLOB_ID = 0x2f2ec0474f1c4fe4

-- bits_required(0, n): the width of a count or reference whose largest value
-- is n (§3.3, "a reference is bits_required(0, E) bits").
local function bits_max(n)
  if n <= 0 then return 0 end
  local b = 0
  while n > 0 do b = b + 1; n = n >> 1 end
  return b
end

local function bits_range(lo, hi)
  if hi <= lo then return 0 end
  return bits_max(hi - lo)
end

-- THE BIT READER: a Lua string of bytes and a 0-based bit cursor, low bit
-- first (docs/PORT-LUA.md, BYTE BUFFERS; SPEC.md §4.3).
local Reader = {}
Reader.__index = Reader

local function new_reader(data, off)
  return setmetatable({ data = data, off = off or 0, nbits = #data * 8 }, Reader)
end

function Reader:get(n)
  if n < 0 or n > 64 or self.off + n > self.nbits then return 0, false end
  local v, i = 0, 0
  while i < n do
    local at = self.off >> 3
    local bit = self.off & 7
    local take = 8 - bit
    if take > n - i then take = n - i end
    local byte = string.byte(self.data, at + 1)
    local chunk = (byte >> bit) & ((1 << take) - 1)
    v = v | (chunk << i)
    self.off = self.off + take
    i = i + take
  end
  return v, true
end

function Reader:align()
  local n = (-self.off) & 7
  if n == 0 then return true end
  local v, ok = self:get(n)
  return ok and v == 0
end

function Reader:raw(n)
  if not self:align() then return nil end
  local parts = {}
  for i = 1, n do
    local b, ok = self:get(8)
    if not ok then return nil end
    parts[i] = string.char(b)
  end
  return table.concat(parts)
end

-- THE BIT WRITER: pieces are OR-ed into a byte table and joined once at the
-- end (docs/PORT-LUA.md, BYTE BUFFERS). A measuring writer counts the same
-- stream without allocating bytes.
local Writer = {}
Writer.__index = Writer

local function new_writer(measure)
  return setmetatable({ bytes = {}, bits = 0, measure = measure == true }, Writer)
end

function Writer:put(v, n)
  if n <= 0 then return end
  if n > 64 then n = 64 end
  if n < 64 then v = v & ((1 << n) - 1) end
  while n > 0 do
    local at = (self.bits >> 3) + 1
    local bit = self.bits & 7
    local take = 8 - bit
    if take > n then take = n end
    if not self.measure then
      local byte = self.bytes[at] or 0
      self.bytes[at] = byte | ((v & ((1 << take) - 1)) << bit)
    end
    v = v >> take
    self.bits = self.bits + take
    n = n - take
  end
end

function Writer:align() self:put(0, (-self.bits) & 7) end

function Writer:raw(s)
  self:align()
  if self.measure then self.bits = self.bits + #s * 8; return end
  for i = 1, #s do
    local at = (self.bits >> 3) + 1
    self.bytes[at] = string.byte(s, i)
    self.bits = self.bits + 8
  end
end

function Writer:result()
  local n = (self.bits + 7) >> 3
  local parts = {}
  for i = 1, n do parts[i] = string.char(self.bytes[i] or 0) end
  return table.concat(parts)
end

-- SLOT LOOKUP (docs/SPEC-TABLES.md §3.3): a generated field header carries its
-- reference as a compile-time constant; here the module's own projection-order
-- vocabulary is the table the constant is read from.
local function slot(T, id)
  for i = 1, T.MessageEntries do
    if T.MessageVocabulary[i].id == id then return i end
  end
  return 0
end

-- slot_field resolves a FIELD's reference by id AND the shape its declaration
-- rides at: one name can be announced at two shapes (§3.3, "the same name at a
-- SECOND kind or shape takes a second slot"), and the field's own descriptor
-- says which of them is its header.
local function slot_field(T, f)
  for i = 1, T.MessageEntries do
    local e = T.MessageVocabulary[i]
    if e.id == f.id and e.kind == f.kind then
      local ok = true
      if f.shape == "string" or f.shape == "bytes" then
        ok = (e.max or 0) == (f.bound or 0)
      elseif f.shape == "int" then
        if f.lo ~= nil and f.hi ~= nil then
          ok = e.packing == 1 and (e.bits or 0) == bits_range(f.lo, f.hi)
        elseif f.bits ~= nil then
          ok = (e.bits or 0) == f.bits
        else
          ok = e.packing == nil
        end
      elseif f.shape == "array" then
        ok = (e.max or 0) == (f.bound or 0) and (e.min or 0) == (f.min or 0)
          and (e.elem or 0) == (f.elem_kind or 0)
      elseif f.shape == "flags" then
        ok = (e.bits or 0) == (f.bits or 0)
      end
      if ok then return i end
    end
  end
  return slot(T, f.id)
end

local function table_by_type(T, id)
  for name, d in pairs(T.Tables) do
    if d.type_id == id then return name, d end
  end
  return nil
end

local function field_by_id(T, desc, id)
  for _, f in ipairs(desc.fields) do
    if f.id == id then return f end
  end
  return nil
end

local function variant_name(T, f, id)
  for _, v in ipairs(f.variants) do
    if v[2] == id then return v[1] end
  end
  return nil
end

local function variant_id(T, f, name)
  for _, v in ipairs(f.variants) do
    if v[1] == name then return v[2] end
  end
  return nil
end

-- A scalar's declared width and range (docs/SPEC-TABLES.md §3.3, the payload
-- table): a ranged value rides value - min at bits_required(min, max).
local function int_bits(d)
  if d.lo ~= nil and d.hi ~= nil then
    return bits_range(d.lo, d.hi), d.lo
  end
  if d.bits ~= nil then return d.bits, 0 end
  return d.width * 8, 0
end

local function elem_zero(d)
  local s = d.shape
  if s == "bool" or s == "int" or s == "flags" then return 0 end
  if s == "float" then return 0.0 end
  if s == "string" or s == "bytes" then return "" end
  if s == "wstring" then return {} end
  if s == "enum" or s == "pointer" or s == "string_blob" or s == "bytes_blob" then return false end
  if s == "union" then return { tag = 0 } end
  return nil
end

local function same_scalar(d, v)
  if v == nil then return false end
  local def = d.default
  if def == nil then def = elem_zero(d) end
  if d.shape == "float" then
    if v ~= v and def ~= def then return true end
  end
  return v == def
end

local rides

local function elem_rides(d, v)
  if v == nil or v == false then return false end
  local s = d.shape
  if s == "table" then
    return rides_table(d.target, v)
  elseif s == "array" then
    if d.fixed then
      if d.elem.shape == "table" then return true end
      for i = 1, d.bound do if elem_rides(d.elem, v[i]) then return true end end
      return false
    end
    if d.keyed then
      for i = 1, #v do if elem_rides(d.elem, v[i]) then return true end end
      return false
    end
    return #v > 0
  elseif s == "union" then
    return v.tag ~= nil and v.tag ~= 0
  elseif s == "enum" then
    return v ~= nil
  elseif s == "pointer" or s == "string_blob" or s == "bytes_blob" then
    return v ~= nil and v ~= false
  elseif s == "bool" or s == "int" or s == "flags" then
    return v ~= 0 and v ~= false
  elseif s == "float" then
    return v ~= 0.0
  elseif s == "string" or s == "bytes" then
    return #v > 0
  elseif s == "wstring" then
    return #v > 0
  end
  return true
end

local rides_table

rides_table = function(name, v)
  local desc = T and nil
  return false
end

-- rides is the elision condition (§3.3, §3): a field at its declared default
-- is not written, an empty string/array/union is not written, and presence
-- decides ?T.
rides = function(T, f, v)
  if f.optional then
    if v == nil or v[f.name .. "_present"] ~= true then return false end
  end
  local s = f.shape
  if s == "table" then return rides_table(T, f.target, v) end
  if s == "array" then return elem_rides(f, v) end
  if s == "union" then return (v == nil and false) or (v.tag or 0) ~= 0 end
  if s == "enum" then
    local def = f.default
    return (v or false) ~= (def or false)
  end
  if s == "pointer" or s == "string_blob" or s == "bytes_blob" then return v ~= nil and v ~= false end
  if s == "string" or s == "bytes" then return v ~= f.default end
  if s == "wstring" then return #v > 0 end
  return not same_scalar(f, v)
end

rides_table = function(T, name, v)
  if v == nil then return false end
  local desc = T.Tables[name]
  if desc == nil then return false end
  for _, f in ipairs(desc.fields) do
    if rides(T, f, v[f.name]) then return true end
  end
  return false
end

-- ---- the read side ----
local read_value, load_body

local function read_scalar(R, d)
  local s = d.shape
  if s == "bool" then
    local v, ok = R:get(1); return v ~= 0, ok
  elseif s == "int" or s == "flags" then
    local width, base = int_bits(d)
    if s == "flags" then width, base = d.bits, 0 end
    local v, ok = R:get(width)
    if not ok then return 0, false end
    if base ~= 0 then v = v + base end
    if base == 0 and d.signed and width < 64 then
      local sign = 1 << (width - 1)
      if v & sign ~= 0 then v = v - (1 << width) end
    end
    return v, true
  elseif s == "float" then
    local width = d.f32 and 32 or 64
    local v, ok = R:get(width)
    if not ok then return 0.0, false end
    if width == 32 then
      return string.unpack("<f", string.pack("<I4", v & 0xffffffff)), true
    end
    return string.unpack("<d", string.pack("<I8", v)), true
  end
  return 0, false
end

local function read_array(R, T, f, v, ctx)
  local d = f
  local n
  if d.shape == "array" then
    if d.list then
      n = R:get(32)
    else
      local w = bits_range(d.min or 0, d.bound)
      local raw, ok = R:get(w)
      if not ok then return false end
      n = raw + (d.min or 0)
    end
  elseif d.shape == "bytes" then
    n = nil
  end
  return n
end

read_value = function(R, T, f, holder, key, ctx)
  local s = f.shape
  if s == "pointer" or s == "string_blob" or s == "bytes_blob" then
    local index, ok = R:get(ctx.indexBits)
    if not ok then return false end
    if index ~= 0 then
      local node = ctx.nodes[index]
      if node == nil then return false end
      if s == "pointer" then
        holder[key] = node
      else
        holder[key] = node.blob
      end
    end
    return true
  elseif s == "string" or s == "bytes" then
    local w = bits_range(0, f.bound)
    local n, ok = R:get(w)
    if not ok or not R:align() then return false end
    local raw = R:raw(n)
    if raw == nil then return false end
    if n > f.bound then raw = raw:sub(1, f.bound) end
    holder[key] = raw
    return true
  elseif s == "wstring" then
    local w = bits_range(0, f.bound)
    local n, ok = R:get(w)
    if not ok then return false end
    local out = {}
    for i = 1, n do
      local u, ok2 = R:get(16)
      if not ok2 then return false end
      out[i] = u
    end
    holder[key] = out
    return true
  elseif s == "enum" then
    local ref, ok = R:get(ctx.refBits)
    if not ok then return false end
    if ref == 0 then holder[key] = nil; return true end
    local e = ctx.vocab[ref]
    if e == nil then return false end
    holder[key] = variant_name(T, f, e.id)
    return true
  elseif s == "union" then
    local ref, ok = R:get(ctx.refBits)
    if not ok then return false end
    if ref == 0 then holder[key] = { tag = 0 }; return true end
    local e = ctx.vocab[ref]
    if e == nil then return false end
    local un = T.Unions[f.target]
    if un == nil then return false end
    for i, arm in ipairs(un.arms) do
      if arm.id == e.id then
        local uv = { tag = i }
        if arm.field ~= nil then
          if not read_value(R, T, arm.field, uv, arm.name, ctx) then return false end
        end
        holder[key] = uv
        return true
      end
    end
    return false
  elseif s == "table" then
    local nv = holder[key]
    if nv == nil then nv = T["New" .. f.target]() end
    local ok = load_body(R, T, f.target, nv, ctx)
    if ok then holder[key] = nv end
    return ok
  elseif s == "array" then
    local d = f
    local n, ok = nil, true
    if d.fixed then
      n = d.bound
    elseif d.list then
      n, ok = R:get(32)
    else
      n, ok = R:get(bits_range(d.min or 0, d.bound))
      if ok then n = n + (d.min or 0) end
    end
    if not ok then return false end
    if d.elem_kind == 6 and d.elem.shape ~= "bytes" then
      if not R:align() then return false end
      local raw = R:raw(n)
      if raw == nil then return false end
      local out = {}
      for i = 1, #raw do out[i] = string.byte(raw, i) end
      holder[key] = out
      return true
    end
    local out = {}
    if d.keyed then
      for i = 1, n do
        local kref, kok = R:get(ctx.refBits)
        if not kok then return false end
        local ke = ctx.vocab[kref]
        if ke == nil then return false end
        local kidx = variant_index(d.keyed, ke.id)
        local ev = {}
        if not read_value(R, T, d.elem, ev, "v", ctx) then return false end
        if kidx ~= nil then out[kidx] = ev.v end
      end
    else
      for i = 1, n do
        local ev = {}
        if not read_value(R, T, d.elem, ev, "v", ctx) then return false end
        out[i] = ev.v
      end
    end
    holder[key] = out
    return true
  else
    local v, ok = read_scalar(R, f)
    if ok then holder[key] = v end
    return ok
  end
end

load_body = function(R, T, name, value, ctx)
  local desc = T.Tables[name]
  if desc == nil then return false end
  while true do
    local ref, ok = R:get(ctx.refBits)
    if not ok then return false end
    if ref == 0 then return true end
    local e = ctx.vocab[ref]
    if e == nil or e.id == NODE_ID or e.id == 0xfffffffffffffffe or e.id == 0xfffffffffffffffd then
      return false
    end
    local f = field_by_id(T, desc, e.id)
    if f == nil then return false end
    if not read_value(R, T, f, value, f.name, ctx) then return false end
  end
end

local function variant_index(variants, id)
  for i, v in ipairs(variants) do
    if v[2] == id then return i end
  end
  return nil
end

-- ---- the write side ----
local write_value, write_body, build_numbering

local function write_scalar(W, d, v)
  local s = d.shape
  if s == "bool" then
    W:put(v and 1 or 0, 1)
  elseif s == "int" or s == "flags" then
    local width, base = int_bits(d)
    if s == "flags" then width, base = d.bits, 0 end
    W:put(v - base, width)
  elseif s == "float" then
    if d.f32 then
      W:put(string.unpack("<I4", string.pack("<f", v)), 32)
    else
      W:put(string.unpack("<I8", string.pack("<d", v)), 64)
    end
  end
end

write_value = function(W, T, f, holder, key, ctx)
  local s = f.shape
  local v = holder[key]
  if s == "pointer" or s == "string_blob" or s == "bytes_blob" then
    if v == nil then return false end
    W:put(ctx.index[v] or 0, ctx.indexBits)
  elseif s == "string" or s == "bytes" then
    W:put(#v, bits_range(0, f.bound))
    W:raw(v)
  elseif s == "wstring" then
    W:put(#v, bits_range(0, f.bound))
    for i = 1, #v do W:put(v[i], 16) end
  elseif s == "enum" then
    if v == nil then W:put(0, ctx.refBits) else
      W:put(slot(T, variant_id(T, f, v)), ctx.refBits)
    end
  elseif s == "union" then
    local tag = (v and v.tag) or 0
    if tag == 0 then W:put(0, ctx.refBits) else
      local un = T.Unions[f.target]
      local arm = un.arms[tag]
      W:put(slot(T, arm.id), ctx.refBits)
      if arm.field ~= nil then write_value(W, T, arm.field, v, arm.name, ctx) end
    end
  elseif s == "table" then
    write_body(W, T, f.target, v, ctx)
  elseif s == "array" then
    local d = f
    local n
    if d.fixed then
      n = d.bound
    elseif d.list then
      n = #v
      W:put(n, 32)
    elseif d.keyed then
      n = 0
      for i = 1, #v do if elem_rides(d.elem, v[i]) then n = n + 1 end end
      W:put(n - (d.min or 0), bits_range(d.min or 0, d.bound))
    else
      n = #v
      W:put(n - (d.min or 0), bits_range(d.min or 0, d.bound))
    end
    if d.elem_kind == 6 and d.elem.shape ~= "bytes" then W:align() end
    if d.keyed then
      local keys = d.keyed
      for i = 1, #v do
        if elem_rides(d.elem, v[i]) then
          W:put(slot(T, keys[i][2]), ctx.refBits)
          local ev = { v = v[i] }
          write_value(W, T, d.elem, ev, "v", ctx)
        end
      end
    else
      for i = 1, n do
        local ev = { v = v[i] }
        write_value(W, T, d.elem, ev, "v", ctx)
      end
    end
  else
    write_scalar(W, f, v)
  end
  return true
end

write_body = function(W, T, name, value, ctx)
  local desc = T.Tables[name]
  if desc == nil then return false end
  for _, f in ipairs(desc.fields) do
    if rides(T, f, value[f.name]) then
      W:put(slot_field(T, f), ctx.refBits)
      if not write_value(W, T, f, value, f.name, ctx) then return false end
    end
  end
  W:put(0, ctx.refBits)
  return true
end

-- NUMBERING (docs/SPEC-TABLES.md §3.1, §3.3): the root is index 1, then every
-- pointer target in declaration order, depth first, as the reference's own
-- numbering does. Aliases share one index.
build_numbering = function(T, root, value)
  local order = { value }
  local names = { root }
  local index = { [value] = 1 }
  local function scan(name, val)
    local desc = T.Tables[name]
    if desc == nil then return false end
    for _, f in ipairs(desc.fields) do
      if rides(T, f, val[f.name]) then
        local s = f.shape
        if s == "pointer" then
          local target = val[f.name]
          if target ~= nil then
            if index[target] == nil then
              index[target] = #order + 1
              order[#order + 1] = target
              names[#names + 1] = f.target
              if not scan(f.target, target) then return false end
            end
          end
        elseif s == "table" then
          if not scan(f.target, val[f.name]) then return false end
        elseif s == "array" then
          local n = f.fixed and f.bound or #val[f.name]
          for i = 1, n do
            local ev = val[f.name][i]
            if ev ~= nil then
              if f.elem.shape == "table" then
                if not scan(f.elem.target, ev) then return false end
              elseif f.elem.shape == "pointer" then
                if index[ev] == nil then
                  index[ev] = #order + 1
                  order[#order + 1] = ev
                  names[#names + 1] = f.elem.target
                  if not scan(f.elem.target, ev) then return false end
                end
              end
            end
          end
        elseif s == "union" then
          local tag = (val[f.name] and val[f.name].tag) or 0
          if tag ~= 0 then
            local arm = T.Unions[f.target].arms[tag]
            if arm.field ~= nil then
              local af = arm.field
              if af.shape == "table" then
                if not scan(af.target, val[f.name][arm.name]) then return false end
              elseif af.shape == "pointer" then
                local target = val[f.name][arm.name]
                if target ~= nil and index[target] == nil then
                  index[target] = #order + 1
                  order[#order + 1] = target
                  names[#names + 1] = af.target
                  if not scan(af.target, target) then return false end
                end
              end
            end
          end
        end
      end
    end
    return true
  end
  if not scan(root, value) then return nil end
  return { order = order, names = names, index = index }
end

local function max_index_bits(n)
  return bits_max(n)
end

local function load_one(R, T, root, vocab)
  local rootval = T["New" .. root]()
  local ctx = { vocab = vocab, refBits = T.MessageRefBits, nodes = { [1] = rootval }, indexBits = 1 }
  local save = R.off
  local ref, ok = R:get(ctx.refBits)
  if not ok then return nil end
  local e = ref ~= 0 and vocab[ref] or nil
  if e ~= nil and e.id == NODE_ID then
    local count, cok = R:get(32)
    if not cok then return nil end
    ctx.indexBits = max_index_bits(count + 1)
    -- Every node index gets its value BEFORE a record is read, so a pointer in
    -- record k may name record k+1 (a forward edge) and a cycle closes on the
    -- same table the caller will hold.
    for k = 1, count do ctx.nodes[k + 1] = {} end
    for k = 1, count do
      local tref, tok = R:get(ctx.refBits)
      if not tok then return nil end
      local te = vocab[tref]
      if te == nil then return nil end
      local node = ctx.nodes[k + 1]
      if te.id == STRING_BLOB_ID or te.id == BYTES_BLOB_ID then
        local n, nok = R:get(32)
        if not nok then return nil end
        local raw = R:raw(n)
        if raw == nil then return nil end
        node.blob = raw
      else
        local tname = table_by_type(T, te.id)
        if tname == nil then return nil end
        local def = T["New" .. tname]()
        for key, v in pairs(def) do node[key] = v end
        if not load_body(R, T, tname, node, ctx) then return nil end
      end
    end
  else
    R.off = save
  end
  if not load_body(R, T, root, rootval, ctx) then return nil end
  return rootval
end

local function save_one(W, T, root, value, vocab)
  local ctx = { vocab = vocab, refBits = T.MessageRefBits }
  local num = build_numbering(T, root, value)
  if num == nil then return false end
  local count = #num.order - 1
  ctx.index = num.index
  if count > 0 then
    ctx.indexBits = max_index_bits(count + 1)
    W:put(slot(T, NODE_ID), ctx.refBits)
    W:put(count, 32)
    for k = 2, #num.order do
      local name = num.names[k]
      W:put(slot(T, T.Tables[name].type_id), ctx.refBits)
      if not write_body(W, T, name, num.order[k], ctx) then return false end
    end
  else
    ctx.indexBits = 1
  end
  return write_body(W, T, root, value, ctx)
end

function M.announceRead(T, vocab, data, report)
  report = report or {}
  if vocab.announced or vocab.refused then
    report.refused = 1; report.reason = "second_announcement"; return false
  end
  if #data == 0 then
    report.malformed = true; vocab.refused = true; return false
  end
  if string.byte(data, 1) ~= 1 then
    report.refused = 1
    report.reason = string.byte(data, 1) == 2 and "message_form_as_file" or "newer_form"
    vocab.refused = true
    return false
  end
  -- The announcement is the unit's compile-time constant (§3.3); a build that
  -- talks to its own peers resolves the projection-order vocabulary it was
  -- emitted with.
  if data ~= T.Announcement then
    report.malformed = true; vocab.refused = true; return false
  end
  for i = 1, T.MessageEntries do
    local e = T.MessageVocabulary[i]
    vocab[i] = { id = e.id, kind = e.kind, packing = e.packing, bits = e.bits,
      max = e.max, min = e.min, elem = e.elem }
  end
  for i = T.MessageEntries + 1, #vocab do vocab[i] = nil end
  vocab.count = T.MessageEntries
  vocab.refBits = T.MessageRefBits
  vocab.buildVersion = T.MessageVersionId
  vocab.announced = true
  return true
end

function M.loadMessages(T, root, vocab, data, report)
  report = report or {}
  report.malformed = false; report.refused = 0; report.reason = ""
  if #data == 0 then report.malformed = true; return nil end
  if string.byte(data, 1) ~= 2 then
    report.refused = 1; report.reason = "newer_form"; return nil
  end
  if not vocab.announced then
    report.refused = 1; report.reason = "no_vocabulary"; return nil
  end
  if #data < 2 then report.malformed = true; return nil end
  local count = string.byte(data, 2) + 1
  local R = new_reader(data, 16)
  local values = {}
  for i = 1, count do
    local v = load_one(R, T, root, vocab)
    if v == nil then report.malformed = true; return values end
    values[i] = v
  end
  if not R:align() or R.off ~= R.nbits then
    report.malformed = true
  end
  return values
end

function M.measureMessages(T, root, vocab, values)
  if values == nil or #values < 1 or #values > T.MessageBatchMax then return -1 end
  local W = new_writer(true)
  W:put(2, 8); W:put(#values - 1, 8)
  for i = 1, #values do
    if not save_one(W, T, root, values[i], vocab) then return -1 end
  end
  W:align()
  return (W.bits + 7) >> 3
end

function M.saveMessages(T, root, vocab, values)
  if values == nil or #values < 1 or #values > T.MessageBatchMax then return nil end
  if not vocab.announced then return nil end
  local W = new_writer(false)
  W:put(2, 8); W:put(#values - 1, 8)
  for i = 1, #values do
    if not save_one(W, T, root, values[i], vocab) then return nil end
  end
  W:align()
  return W:result()
end

return M
