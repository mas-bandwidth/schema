-- THE MESSAGE FORM'S RUNTIME (docs/SPEC-TABLES.md §3.3), form byte 2: the
-- announcement reader and the batch verbs. Receive references use the sender's
-- announced vocabulary; send references use the unit's own closure, emitted by
-- messageform.go as _M.MessageVocabulary in projection order. The wire is the bitpacked batch:
-- form byte 2, an eight-bit count (M-1), then M bodies as one bit stream, then
-- zero pad to the byte boundary.
--
-- A body is a sequence of fields, each a REFERENCE at refBits followed by a
-- payload at its declared width; reference 0 is the body terminator. Elision
-- and the declared defaults are the variable form's (§3). Damage is terminal
-- for the batch. The form is fixed tables only (§3.3): a fixed root's closure
-- holds no pointer, blob, map, unbounded array or guarded branch, so a body is
-- its fields alone and no node table rides.
local D = require("descriptor")
local M = {}

local NODE_ID = 0xffffffffffffffff

-- bits_required(0, n): the width of a count or reference whose largest value
-- is n (§3.3, "a reference is bits_required(0, E) bits").
local function bits_max(n)
  if n <= 0 then return 0 end
  local b = 0
  while n > 0 do
    b = b + 1
    n = n >> 1
  end
  return b
end

local function bits_range(lo, hi)
  if hi <= lo then return 0 end
  return bits_max(hi - lo)
end

-- UNSIGNED 64-BIT COMPARISON AND BIT WIDTH, for the halves of a 128-bit value.
-- Lua's integer comparison is signed; a uint64's high bit is not a sign
-- (docs/PORT-LUA.md, 64-BIT AND 128-BIT INTEGERS).
local function u_lt(a, b)
  if (a < 0) ~= (b < 0) then return b < 0 end
  return a < b
end

local function u_bits(n)
  local b = 0
  while n ~= 0 do
    b = b + 1
    n = n >> 1
  end
  return b
end

-- A WIDE VALUE'S RANGED PACKING (docs/SPEC-TABLES.md §3.3, kinds 18/19 and
-- 24/29): the wire carries value - base at bits_required(min, max), with the
-- base and the difference both 128-bit two's complement pairs.
local function u128_sub(a, b)
  local borrow = u_lt(a.lo, b.lo) and 1 or 0
  return { lo = a.lo - b.lo, hi = a.hi - b.hi - borrow }
end

local function u128_add(a, b)
  local lo = a.lo + b.lo
  local carry = (u_lt(lo, a.lo) or u_lt(lo, b.lo)) and 1 or 0
  return { lo = lo, hi = a.hi + b.hi + carry }
end

local function wide_bits(lo, hi)
  local d = u128_sub(hi, lo)
  if d.hi ~= 0 then return 64 + u_bits(d.hi) end
  return u_bits(d.lo)
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
  local n = -self.off & 7
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

function Writer:align()
  self:put(0, -self.bits & 7)
end

function Writer:raw(s)
  self:align()
  if self.measure then
    self.bits = self.bits + #s * 8
    return
  end
  for i = 1, #s do
    local at = (self.bits >> 3) + 1
    self.bytes[at] = string.byte(s, i)
    self.bits = self.bits + 8
  end
end

function Writer:result()
  local n = (self.bits + 7) >> 3
  local parts = {}
  for i = 1, n do
    parts[i] = string.char(self.bytes[i] or 0)
  end
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
      elseif f.shape == "wide" then
        if f.lo ~= nil and f.hi ~= nil then
          ok = e.packing == 1 and (e.bits or 0) == wide_bits(f.lo, f.hi)
        else
          ok = e.packing == nil
        end
      elseif f.shape == "array" then
        ok = (e.max or 0) == (f.bound or 0) and (e.min or 0) == (f.min or 0) and (e.elem or 0) == (f.elem_kind or 0)
      elseif f.shape == "flags" then
        ok = (e.bits or 0) == (f.bits or 0)
      end
      if ok then return i end
    end
  end
  return slot(T, f.id)
end

local function field_by_id(_T, desc, id)
  for _, f in ipairs(desc.fields) do
    if f.id == id then return f end
  end
  return nil
end

-- a retired variant reads the declared default, an element's None
-- (docs/SPEC-TABLES.md §2.15, READ)
local function variant_name(_T, f, id)
  for _, v in ipairs(f.variants) do
    if v[2] == id then
      if v.deprecated then return f.default end
      return v[1]
    end
  end
  return nil
end

local function variant_id(_T, f, name)
  for _, v in ipairs(f.variants) do
    if v[1] == name then return v[2] end
  end
  return nil
end

-- A scalar's declared width and range (docs/SPEC-TABLES.md §3.3, the payload
-- table): a ranged value rides value - min at bits_required(min, max).
local function int_bits(d)
  if d.lo ~= nil and d.hi ~= nil then return bits_range(d.lo, d.hi), d.lo end
  if d.bits ~= nil then return d.bits, 0 end
  return d.width * 8, 0
end

local function elem_zero(d)
  local s = d.shape
  if s == "bool" or s == "int" or s == "flags" then return 0 end
  if s == "wide" then return { lo = 0, hi = 0 } end
  if s == "float" then return 0.0 end
  if s == "string" or s == "bytes" then return "" end
  if s == "wstring" then return {} end
  if s == "enum" then return false end
  if s == "union" then return { tag = 0 } end
  return nil
end

-- A PLAIN FLOAT'S SPECIAL VALUES TAKE ONE RULE (docs/SPEC-TABLES.md §3.3,
-- "-0 IS A VALUE, AND IT RIDES"): the elision compares BIT PATTERNS, never
-- ==, because -0 == +0 under == and a -0 must ride the message form too.
local function same_float(f32, a, b)
  if type(a) ~= "number" or type(b) ~= "number" then return a == b end
  if f32 then return string.pack("<f", a) == string.pack("<f", b) end
  return string.pack("<d", a) == string.pack("<d", b)
end

local function same_scalar(d, v)
  if v == nil then return false end
  local def = d.default
  if def == nil then def = elem_zero(d) end
  if d.shape == "wide" then return v.lo == def.lo and v.hi == def.hi end
  if d.shape == "float" then return same_float(d.f32, v, def) end
  return v == def
end

local rides, rides_table

local function elem_rides(T, d, v)
  if v == nil or v == false then return false end
  local s = d.shape
  if s == "table" then
    return rides_table(T, d.target, v)
  elseif s == "array" then
    if d.fixed then
      if d.elem.shape == "table" then return true end
      for i = 1, d.bound do
        if elem_rides(T, d.elem, v[i]) then return true end
      end
      return false
    end
    if d.keyed then
      for i = 1, #v do
        if elem_rides(T, d.elem, v[i]) then return true end
      end
      return false
    end
    return #v > 0
  elseif s == "union" then
    return v.tag ~= nil and v.tag ~= 0
  elseif s == "enum" then
    return v ~= nil
  elseif s == "bool" or s == "int" or s == "flags" then
    return v ~= 0 and v ~= false
  elseif s == "float" then
    -- AN ELEMENT'S ZERO IS +0, AND THE COMPARE IS BY BITS (§3.3): a -0 element
    -- rides (and a NaN or an infinity reaches the writer, which refuses it).
    return not same_float(d.f32, v, 0.0)
  elseif s == "string" or s == "bytes" then
    return #v > 0
  elseif s == "wstring" then
    return #v > 0
  end
  return true
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
  if s == "array" then return elem_rides(T, f, v) end
  if s == "union" then return (v == nil and false) or (v.tag or 0) ~= 0 end
  if s == "enum" then
    local def = f.default
    return (v or false) ~= (def or false)
  end
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
local read_value, load_body, variant_index

local function read_scalar(R, d)
  local s = d.shape
  if s == "bool" then
    local v, ok = R:get(1)
    return v ~= 0, ok
  elseif s == "int" or s == "flags" then
    local width, base = int_bits(d)
    if s == "flags" then
      width, base = d.bits, 0
    end
    local v, ok = R:get(width)
    if not ok then return 0, false end
    if base ~= 0 then v = v + base end
    if base == 0 and d.signed and width < 64 then
      local sign = 1 << (width - 1)
      if v & sign ~= 0 then v = v - (1 << width) end
    end
    return v, true
  elseif s == "wide" then
    local bits, base = 128, nil
    if d.lo ~= nil and d.hi ~= nil then
      bits, base = wide_bits(d.lo, d.hi), d.lo
    end
    local lo, ok = R:get(bits < 64 and bits or 64)
    if not ok then return 0, false end
    local hi = 0
    if bits > 64 then
      hi, ok = R:get(bits - 64)
      if not ok then return 0, false end
    end
    if base ~= nil then
      local sum = u128_add({ lo = lo, hi = hi }, base)
      lo, hi = sum.lo, sum.hi
    end
    return { lo = lo, hi = hi }, true
  elseif s == "float" then
    -- A NaN OR AN INFINITY IS REFUSED ON ITS RAW BITS (docs/SPEC-TABLES.md
    -- §3.3), before they become a number: the third answer says so, and the
    -- batch refuses as non_finite_float and lands nothing.
    local width = d.f32 and 32 or 64
    local v, ok = R:get(width)
    if not ok then return 0.0, false end
    if width == 32 then
      v = v & 0xffffffff
      if v & D.F32_EXP == D.F32_EXP then return 0.0, false, true end
      return string.unpack("<f", string.pack("<I4", v)), true
    end
    if v & D.F64_EXP == D.F64_EXP then return 0.0, false, true end
    return string.unpack("<d", string.pack("<I8", v)), true
  end
  return 0, false
end

read_value = function(R, T, f, holder, key, ctx)
  local s = f.shape
  if s == "string" or s == "bytes" then
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
    if ref == 0 then
      holder[key] = nil
      return true
    end
    local e = ctx.vocab[ref]
    if e == nil then return false end
    holder[key] = variant_name(T, f, e.id)
    return true
  elseif s == "union" then
    local ref, ok = R:get(ctx.refBits)
    if not ok then return false end
    if ref == 0 then
      holder[key] = { tag = 0 }
      return true
    end
    local e = ctx.vocab[ref]
    if e == nil then return false end
    local un = T.Unions[f.target]
    if un == nil then return false end
    for i, arm in ipairs(un.arms) do
      if arm.id == e.id then
        local uv = { tag = i }
        if arm.field ~= nil then
          local owner, field = ctx.owner, ctx.field
          ctx.owner, ctx.field = un.name, arm.name
          local aok = read_value(R, T, arm.field, uv, arm.slot, ctx)
          ctx.owner, ctx.field = owner, field
          if not aok then return false end
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
    local n, ok
    if d.fixed then
      n, ok = d.bound, true
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
      for i = 1, #raw do
        out[i] = string.byte(raw, i)
      end
      holder[key] = out
      return true
    end
    local out = {}
    if d.keyed then
      for _ = 1, n do
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
    local v, ok, nonfinite = read_scalar(R, f)
    if nonfinite and ctx.nonFinite == nil then
      ctx.nonFinite = (ctx.owner or "?") .. "." .. tostring(ctx.field or f.name)
    end
    if ok then holder[key] = v end
    return ok
  end
end

local load_fields

-- load_body names a float <Table>.<field> for the non_finite_float refusal
-- (docs/SPEC-TABLES.md §3.3), and gives the holder's name back on return.
load_body = function(R, T, name, value, ctx)
  local owner, field = ctx.owner, ctx.field
  ctx.owner, ctx.field = name, nil
  local ok = load_fields(R, T, name, value, ctx)
  ctx.owner, ctx.field = owner, field
  return ok
end

load_fields = function(R, T, name, value, ctx)
  local desc = T.Tables[name]
  if desc == nil then return false end
  while true do
    local ref, ok = R:get(ctx.refBits)
    if not ok then return false end
    if ref == 0 then return true end
    local e = ctx.vocab[ref]
    if e == nil or e.id == NODE_ID or e.id == 0xfffffffffffffffe or e.id == 0xfffffffffffffffd then return false end
    local f = field_by_id(T, desc, e.id)
    if f == nil then return false end
    ctx.field = f.name
    if not read_value(R, T, f, value, f.name, ctx) then return false end
  end
end

variant_index = function(variants, id)
  for i, v in ipairs(variants) do
    if v[2] == id then return i end
  end
  return nil
end

-- ---- the write side ----
local write_value, write_body

local function write_scalar(W, d, v)
  local s = d.shape
  if s == "bool" then
    W:put(v and 1 or 0, 1)
  elseif s == "int" or s == "flags" then
    local width, base = int_bits(d)
    if s == "flags" then
      width, base = d.bits, 0
    end
    W:put(v - base, width)
  elseif s == "wide" then
    local lo, hi = (v and v.lo) or 0, (v and v.hi) or 0
    local bits = 128
    if d.lo ~= nil and d.hi ~= nil then
      bits = wide_bits(d.lo, d.hi)
      local d2 = u128_sub({ lo = lo, hi = hi }, d.lo)
      lo, hi = d2.lo, d2.hi
    end
    W:put(lo, bits < 64 and bits or 64)
    if bits > 64 then W:put(hi, bits - 64) end
  elseif s == "float" then
    -- A NaN OR AN INFINITY IS REFUSED, never written (docs/SPEC-TABLES.md
    -- §3.3): measure and save both answer the refusal. A float32's PACKED bits
    -- are tested, so a double past the float32 range is refused too.
    if d.f32 then
      local b = string.unpack("<I4", string.pack("<f", v))
      if b & D.F32_EXP == D.F32_EXP then return false end
      W:put(b, 32)
    else
      if v - v ~= 0 then return false end
      W:put(string.unpack("<I8", string.pack("<d", v)), 64)
    end
  end
  return true
end

write_value = function(W, T, f, holder, key, ctx)
  local s = f.shape
  local v = holder[key]
  if s == "string" or s == "bytes" then
    W:put(#v, bits_range(0, f.bound))
    W:raw(v)
  elseif s == "wstring" then
    W:put(#v, bits_range(0, f.bound))
    for i = 1, #v do
      W:put(v[i], 16)
    end
  elseif s == "enum" then
    if v == nil then
      W:put(0, ctx.refBits)
    else
      W:put(slot(T, variant_id(T, f, v)), ctx.refBits)
    end
  elseif s == "union" then
    local tag = (v and v.tag) or 0
    if tag == 0 then
      W:put(0, ctx.refBits)
    else
      local un = T.Unions[f.target]
      local arm = un.arms[tag]
      W:put(slot(T, arm.id), ctx.refBits)
      if arm.field ~= nil and not write_value(W, T, arm.field, v, arm.slot, ctx) then return false end
    end
  elseif s == "table" then
    if not write_body(W, T, f.target, v, ctx) then return false end
  elseif s == "array" then
    local d = f
    local n
    if d.fixed then
      n = d.bound
    elseif d.keyed then
      n = 0
      for i = 1, #v do
        if elem_rides(T, d.elem, v[i]) then n = n + 1 end
      end
      W:put(n - (d.min or 0), bits_range(d.min or 0, d.bound))
    else
      n = #v
      W:put(n - (d.min or 0), bits_range(d.min or 0, d.bound))
    end
    if d.elem_kind == 6 and d.elem.shape ~= "bytes" then W:align() end
    if d.keyed then
      local keys = d.keyed
      for i = 1, #v do
        if elem_rides(T, d.elem, v[i]) then
          W:put(slot(T, keys[i][2]), ctx.refBits)
          local ev = { v = v[i] }
          if not write_value(W, T, d.elem, ev, "v", ctx) then return false end
        end
      end
    else
      for i = 1, n do
        local ev = { v = v[i] }
        if not write_value(W, T, d.elem, ev, "v", ctx) then return false end
      end
    end
  elseif not write_scalar(W, f, v) then
    return false
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

local function load_one(R, T, root, vocab)
  local rootval = T["New" .. root]()
  local ctx = { vocab = vocab, refBits = vocab.refBits }
  if not load_body(R, T, root, rootval, ctx) then return nil, ctx.nonFinite end
  return rootval
end

local function save_one(W, T, root, value, vocab)
  local ctx = { vocab = vocab, refBits = T.MessageRefBits }
  return write_body(W, T, root, value, ctx)
end

-- THE ANNOUNCEMENT IS AN ORDINARY FORM-1 FILE (docs/SPEC-TABLES.md §3.3):
-- a body carrying the build version and the vocabulary, then the trailer. The
-- reader parses it as Go's AnnounceRead does rather than comparing it with the
-- unit's own constant: the build version KEYS the vocabulary and gates nothing,
-- so an engine announcement at another build reads like any other.
local ANNOUNCE_BUILD_VERSION_ID = 0xfffffffffffffffe
local ANNOUNCE_VOCABULARY_ID = 0xfffffffffffffffd

-- read_leb is the file's one number shape (§3): unsigned LEB128, canonical.
-- at is 1-based; nil is malformed.
local function read_leb(s, at)
  local v, shift, i = 0, 0, at
  while true do
    if i > #s then return nil end
    local b = string.byte(s, i)
    if i - at == 9 and b > 1 then return nil end
    v = v | ((b & 0x7f) << shift)
    i = i + 1
    if (b & 0x80) == 0 then
      if i - at > 1 and b == 0 then return nil end
      return v, i
    end
    shift = shift + 7
  end
end

-- known_kind is §3's closed set plus the 0 an entry takes when its framing is
-- not an entry's to give (§3.3).
local function known_kind(kind)
  return kind == 0 or (kind >= 1 and kind <= 17) or (kind >= 18 and kind <= 29) or (kind >= 30 and kind <= 33)
end

-- skip_shape steps over one announced shape, which is what makes the entry
-- count readable before an entry is resolved (docs/SPEC-TABLES.md §3.3).
local function skip_shape(kind, s, at)
  local function leb(from)
    local _, nextAt = read_leb(s, from)
    return nextAt
  end
  if
    kind == 0
    or kind == 1
    or kind == 11
    or kind == 13
    or kind == 15
    or kind == 17
    or kind == 30
    or kind == 31
    or kind == 32
  then
    return at
  elseif (kind >= 2 and kind <= 9) or kind == 18 or kind == 19 then
    if at > #s then return nil end
    local packing = string.byte(s, at)
    at = at + 1
    if packing == 0 then return at end
    if packing ~= 1 then return nil end
    at = leb(at)
    if at == nil then return nil end
    if kind == 18 or kind == 19 then
      if at + 15 > #s then return nil end
      return at + 16
    end
    return leb(at)
  elseif kind == 10 then
    if at > #s then return nil end
    local packing = string.byte(s, at)
    at = at + 1
    if packing == 0 then return at end
    if packing ~= 2 or at + 11 > #s then return nil end
    return at + 12
  elseif kind >= 20 and kind <= 29 then
    if at > #s then return nil end
    local packing = string.byte(s, at)
    at = at + 1
    if packing == 0 then return at end
    if packing ~= 1 then return nil end
    at = leb(at)
    if at == nil then return nil end
    if at + 15 > #s then return nil end
    return at + 16
  elseif kind == 12 or kind == 33 then
    return leb(at)
  elseif kind == 14 then
    at = leb(at)
    if at == nil then return nil end
    at = leb(at)
    if at == nil then return nil end
    if at > #s then return nil end
    local elem = string.byte(s, at)
    at = at + 1
    if elem == 12 or elem == 33 then return nil end
    return skip_shape(elem, s, at)
  elseif kind == 16 then
    at = leb(at)
    if at == nil then return nil end
    if at > #s then return nil end
    local elem = string.byte(s, at)
    at = at + 1
    if elem == 12 or elem == 33 then return nil end
    return skip_shape(elem, s, at)
  end
  return nil
end

-- vocabulary_entries retains the sender's slots in announcement order.
-- tooLarge is the RECEIVER's capacity bound, refused at the entry that passes
-- it (docs/SPEC-TABLES.md §3.3, "the entry bound is that capacity").
local function vocabulary_entries(s, maxEntries)
  local at, count = 1, 0
  local entries, seen = {}, {}
  while at <= #s do
    if at + 8 > #s then return nil, false end
    local id = string.unpack("<I8", s, at)
    local kind = string.byte(s, at + 8)
    at = at + 9
    if id == ANNOUNCE_BUILD_VERSION_ID or id == ANNOUNCE_VOCABULARY_ID then return nil, false end
    if not known_kind(kind) then return nil, false end
    local start = at - 9
    at = skip_shape(kind, s, at)
    if at == nil then return nil, false end
    local triple = s:sub(start, at - 1)
    if seen[triple] then return nil, false end
    seen[triple] = true
    count = count + 1
    if count > maxEntries then return nil, true end
    entries[count] = { id = id, kind = kind }
  end
  return entries, false
end

-- announceRead reads one announcement into the receiver's vocabulary
-- (docs/SPEC-TABLES.md §3.3). maxBytes is the vocabulary field's byte bound,
-- checked from its own length before an entry is touched; maxEntries is the
-- receiver's entry capacity. Zero or nil takes the conforming defaults.
function M.announceRead(T, vocab, data, report, maxBytes, maxEntries)
  report = report or {}
  if vocab.announced or vocab.refused then
    report.refused = 1
    report.reason = "second_announcement"
    return false
  end
  if #data == 0 then
    report.malformed = true
    vocab.refused = true
    return false
  end
  if string.byte(data, 1) ~= 1 then
    report.refused = 1
    report.reason = string.byte(data, 1) == 2 and "message_form_as_file" or "newer_form"
    vocab.refused = true
    return false
  end
  if maxBytes == nil or maxBytes == 0 then maxBytes = 64 * 1024 end
  if maxEntries == nil or maxEntries == 0 then maxEntries = 4096 end
  local function damage()
    report.malformed = true
    vocab.refused = true
    return false
  end
  local function too_large(version)
    if version ~= nil then vocab.buildVersion = version end
    report.refused = 1
    report.reason = "vocabulary_too_large"
    vocab.refused = true
    return false
  end
  if #data < 9 then return damage() end
  local count = string.unpack("<I8", data, #data - 7)
  if count < 0 or count > (#data // 8) then return damage() end
  local span = count * 8 + 8
  if span + 1 > #data then return damage() end
  local first = #data - span + 1
  local ids, seen = {}, {}
  for i = 1, count do
    local id = string.unpack("<I8", data, first + (i - 1) * 8)
    if seen[id] then return damage() end
    seen[id] = true
    ids[i] = id
  end
  local body = data:sub(2, first - 1)
  local at = 1
  local version, vocabulary = nil, nil
  local seenVersion, seenVocabulary = 0, 0
  while true do
    local ref, nextAt = read_leb(body, at)
    if ref == nil then return damage() end
    at = nextAt
    if ref == 0 then break end
    if ref > count or at > #body then return damage() end
    local id = ids[ref]
    local kind = string.byte(body, at)
    at = at + 1
    if id == ANNOUNCE_BUILD_VERSION_ID then
      if kind ~= 9 or at + 7 > #body then return damage() end
      version = string.unpack("<I8", body, at)
      at = at + 8
      seenVersion = seenVersion + 1
    elseif id == ANNOUNCE_VOCABULARY_ID then
      if kind ~= 14 then return damage() end
      local n
      n, at = read_leb(body, at)
      if n == nil or at + n - 1 > #body then return damage() end
      local field = body:sub(at, at + n - 1)
      at = at + n
      if #field < 1 or string.byte(field, 1) ~= 6 then return damage() end
      local length, nextAt2 = read_leb(field, 2)
      if length == nil or nextAt2 + length - 1 ~= #field then return damage() end
      vocabulary = field:sub(nextAt2, nextAt2 + length - 1)
      if length > maxBytes then return too_large(version) end
      seenVocabulary = seenVocabulary + 1
    else
      -- THE ANNOUNCEMENT READS TOLERANTLY (docs/SPEC-TABLES.md §3.3): an
      -- unknown field this reader cannot skip is the one body it cannot read.
      return damage()
    end
  end
  if at <= #body then return damage() end
  if seenVersion ~= 1 or seenVocabulary ~= 1 then return damage() end
  local entries, tooLarge = vocabulary_entries(vocabulary, maxEntries)
  if entries == nil then
    if tooLarge then return too_large(version) end
    return damage()
  end
  for i, e in ipairs(entries) do vocab[i] = e end
  for i = #entries + 1, #vocab do vocab[i] = nil end
  vocab.count = #entries
  vocab.refBits = bits_max(#entries)
  vocab.buildVersion = version
  vocab.announced = true
  return true
end

-- THE STRICT SET (docs/SPEC-TABLES.md §6.5): a value a body states past its
-- declared bound is `clamped`; the tolerant read keeps BASE's answer, the
-- plain name counts it and refuses. The walk is the descriptor's own.
local function strict_count(T, d, v)
  if v == nil then return 0 end
  local s = d.shape
  if s == "int" then
    if d.lo ~= nil and d.hi ~= nil and (v < d.lo or v > d.hi) then return 1 end
    return 0
  elseif s == "table" then
    local desc = T.Tables[d.target]
    if desc == nil then return 0 end
    local n = 0
    for _, f in ipairs(desc.fields) do
      n = n + strict_count(T, f, v[f.name])
    end
    return n
  elseif s == "union" then
    local tag = v.tag or 0
    local un = T.Unions[d.target]
    if tag ~= 0 and un ~= nil then
      local arm = un.arms[tag]
      if arm ~= nil and arm.field ~= nil then return strict_count(T, arm.field, v[arm.slot]) end
    end
    return 0
  elseif s == "array" then
    local n = 0
    for i = 1, #v do
      n = n + strict_count(T, d.elem, v[i])
    end
    return n
  end
  return 0
end

-- THE MESSAGE FORM IS FIXED TABLES ONLY (docs/SPEC-TABLES.md §3.3): a root
-- outside the unit's MessageRoots (a variable table, or no table at all) is
-- refused by name before any byte is read or written.
local function message_root(T, root)
  if T.MessageRoots == nil or not T.MessageRoots[root] then
    error("message: " .. tostring(root) .. " is not a fixed table: the message form is fixed tables only (docs/SPEC-TABLES.md §3.3)", 0)
  end
end

-- loadMessages is the read behind both names (docs/SPEC-TABLES.md §6.5):
-- `strict` is the plain name's read. Damage discards every body, under both
-- names (§4). Evolution alone retains the existing strict/tolerant choice.
function M.loadMessages(T, root, vocab, data, report, strict)
  message_root(T, root)
  report = report or {}
  -- THE PLAIN NAME'S REPORT IS THIS READ'S ALONE (docs/SPEC-TABLES.md §6.5):
  -- every counter is cleared before the walk, so a reused report cannot refuse
  -- a clean batch for a previous read's clamped or malformed count.
  if strict then
    report.unknown = 0
    report.kindMismatch = 0
    report.clamped = 0
    report.widened = 0
    report.duplicate = 0
    report.blobShared = 0
  end
  report.malformed = false
  report.refused = 0
  report.reason = ""
  report.nonFiniteFloat = nil
  if #data == 0 then
    report.malformed = true
    return nil
  end
  if string.byte(data, 1) ~= 2 then
    report.refused = 1
    report.reason = "newer_form"
    return nil
  end
  if not vocab.announced then
    report.refused = 1
    report.reason = "no_vocabulary"
    return nil
  end
  if #data < 2 then
    report.malformed = true
    return nil
  end
  local count = string.byte(data, 2) + 1
  local R = new_reader(data, 16)
  local values = {}
  for i = 1, count do
    local v, nonfinite = load_one(R, T, root, vocab)
    if nonfinite ~= nil then
      -- A NON-FINITE FLOAT REFUSES THE BATCH, the tolerant read too
      -- (docs/SPEC-TABLES.md §3.3): no record lands, and Reason names it.
      report.nonFiniteFloat = nonfinite
      report.reason = "non_finite_float: " .. nonfinite
      return {}
    end
    if v == nil then
      report.malformed = true
      return {}
    end
    values[i] = v
  end
  if not R:align() or R.off ~= R.nbits then report.malformed = true end
  if report.malformed then return {} end
  if strict then
    local clamped = 0
    for i = 1, #values do
      clamped = clamped + strict_count(T, { shape = "table", target = root }, values[i])
    end
    if clamped > 0 then report.clamped = (report.clamped or 0) + clamped end
    local refused = report.malformed
      or (report.clamped or 0) > 0
      or (report.kindMismatch or 0) > 0
      or (report.blobShared or 0) > 0
    if refused then values = {} end
  end
  return values
end

-- docs/SPEC-TABLES.md §2.15: the first value of a batch holding a retired
-- variant, refused by name
local function refuse_batch(T, root, values)
  for i = 1, #values do
    local refused = D.refuse_deprecated(T, root, values[i])
    if refused ~= nil then return refused end
  end
  return nil
end

function M.measureMessages(T, root, vocab, values)
  message_root(T, root)
  if values == nil or #values < 1 or #values > T.MessageBatchMax then return -1 end
  local refused = refuse_batch(T, root, values)
  if refused ~= nil then return -1, refused end
  local W = new_writer(true)
  W:put(2, 8)
  W:put(#values - 1, 8)
  for i = 1, #values do
    local ok, why = save_one(W, T, root, values[i], vocab)
    if not ok then return -1, why or D.refuse_non_finite(T, root, values[i]) end
  end
  W:align()
  return (W.bits + 7) >> 3
end

function M.saveMessages(T, root, vocab, values)
  message_root(T, root)
  if values == nil or #values < 1 or #values > T.MessageBatchMax then return nil end
  if not vocab.announced then return nil end
  local refused = refuse_batch(T, root, values)
  if refused ~= nil then return nil, refused end
  local W = new_writer(false)
  W:put(2, 8)
  W:put(#values - 1, 8)
  for i = 1, #values do
    local ok, why = save_one(W, T, root, values[i], vocab)
    if not ok then return nil, why or D.refuse_non_finite(T, root, values[i]) end
  end
  W:align()
  return W:result()
end

return M
