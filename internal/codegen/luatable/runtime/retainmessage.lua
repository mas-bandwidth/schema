-- retainmessage.lua — RETAIN-UNKNOWN on the MESSAGE form (docs/SPEC-TABLES.md
-- §3.3, §6.6; the Go reference is internal/codegen/gotable/retainmessage.go,
-- the walker's tableRetainMessageIn). A retaining load keeps every field a batch
-- body carries that this build cannot name, in RESOLVED form: each reference
-- widened to the sixty-four-bit id it names, each framing length recomputed.
-- The records are the SAME reader-private layout the variable form's capture
-- writes (retain.lua), keyed by the body's path, so the variable-form
-- SaveRetain writes them back unchanged (§6.6: retention crosses from the
-- message form to the variable form, never the other way).
--
-- The wire is the announced form-2 batch (message.lua): form byte 2, an
-- eight-bit count (M-1), then M bodies as one bit stream. Each body is decoded
-- against the unit's own projection-order vocabulary; fields the descriptor
-- names are read into the value, and fields it cannot name are captured.
local S = {}

local NODE_ID = 0xffffffffffffffff
local STRING_BLOB_ID = 0x704be0d8faaffc58
local BYTES_BLOB_ID = 0x2f2ec0474f1c4fe4
local MAX64 = 0xffffffffffffffff
local MAXUINT32 = 0xffffffff
local F32_EXP = 0x7f800000
local F64_EXP = 0x7ff0000000000000
local DEPTH_MAX = 64

-- ---- a bit reader over a byte string, low bit first (docs/PORT-LUA.md) ----
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
function Reader:skip(n)
  if n < 0 or self.off + n > self.nbits then return false end
  self.off = self.off + n
  return true
end
function Reader:align()
  local v, ok = self:get(-self.off & 7)
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

-- ---- a byte writer for the resolved record (pieces joined once) ------------
local function W()
  return { parts = {}, n = 0 }
end
local function w_raw(w, s)
  w.parts[#w.parts + 1] = s
  w.n = w.n + #s
end
local function w_leb(w, v)
  v = v & MAX64
  local out = {}
  repeat
    local b = v & 0x7f
    v = v >> 7
    out[#out + 1] = string.char(v ~= 0 and (b | 0x80) or b)
  until v == 0
  w_raw(w, table.concat(out))
end
local function w_u8(w, v)
  w_raw(w, string.char(v & 0xff))
end
local function w_u32(w, v)
  w_raw(w, string.pack("<I4", v))
end
local function w_u64(w, v)
  w_raw(w, string.pack("<i8", v))
end

-- ---- shape helpers (docs/SPEC-TABLES.md §3.3) ------------------------------
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
local function kind_width(kind)
  if kind == 1 or kind == 2 or kind == 6 or kind == 20 or kind == 25 then
    return 1
  elseif kind == 3 or kind == 7 or kind == 21 or kind == 26 then
    return 2
  elseif kind == 4 or kind == 8 or kind == 10 or kind == 22 or kind == 27 then
    return 4
  elseif kind == 5 or kind == 9 or kind == 11 or kind == 23 or kind == 28 or kind == 36 then
    return 8
  elseif kind == 18 or kind == 19 or kind == 24 or kind == 29 then
    return 16
  end
  return 0
end
local function file_minimum(kind)
  local n = kind_width(kind)
  if n ~= 0 then return n end
  if kind == 13 then return 16 end
  if kind == 14 or kind == 16 then return 10 end
  if kind == 15 or kind == 30 then return 8 end
  if kind == 12 or kind == 31 or kind == 32 or kind == 33 then return 1 end
  if kind == 17 then return -1 end
  return -1
end
local function leb_bytes(v)
  local n = 1
  while v >= 0x80 do
    v = v >> 7
    n = n + 1
  end
  return n
end

local function vocab_entry(vocab, ref)
  if ref == 0 or ref > #vocab then return nil end
  return vocab[ref]
end
local function field_by_id(desc, id)
  if desc == nil then return nil end
  for _, f in ipairs(desc.fields) do
    if f.id == id then return f end
  end
  return nil
end
local function table_by_type(T, id)
  for name, d in pairs(T.Tables) do
    if d.type_id == id then return name, d end
  end
  return nil
end
local function variant_index(variants, id)
  for i, v in ipairs(variants) do
    if v[2] == id then return i end
  end
  return nil
end

local function message_count(r, s)
  local w = bits_range(s.min or 0, s.max or 0)
  local raw, ok = r:get(w)
  if not ok then return 0, false end
  return raw + (s.min or 0), true
end

-- tableMessageSkip: advance past a field by its announced shape alone (the Go
-- reference's tableMessageSkip, gotable/message.go). Returns true on success.
local message_skip
local function message_skip_body(r, vocab, indexBits, depth)
  if depth > 128 then return false end
  while true do
    local ref, ok = r:get(vocab.refBits)
    if not ok or ref == 0 then return ok end
    local e = vocab_entry(vocab, ref)
    if e == nil or not message_skip(r, vocab, indexBits, e, depth) then return false end
  end
end
message_skip = function(r, vocab, indexBits, e, depth)
  if depth > 128 then return false end
  local s = e
  local k = s.kind
  if k == 0 or k == 32 then return true end
  if k == 30 then
    local ref, ok = r:get(vocab.refBits)
    return ok and ref ~= 0
  end
  if k == 13 then return message_skip_body(r, vocab, indexBits, depth + 1) end
  if k == 17 then return indexBits > 0 and r:skip(indexBits) end
  if k == 15 then
    local ref, ok = r:get(vocab.refBits)
    if not ok or ref == 0 then return ok end
    local arm = vocab_entry(vocab, ref)
    return arm ~= nil
      and arm.id ~= 0xfffffffffffffffd
      and arm.kind ~= 0
      and message_skip(r, vocab, indexBits, arm, depth + 1)
  end
  if k == 12 or k == 33 then
    local n, ok = message_count(r, s)
    if not ok then return false end
    local width = 16
    if k == 12 then
      width = 8
      if not r:align() then return false end
    end
    return r:skip(n * width)
  end
  if k == 31 then
    if not r:align() then return false end
    local n, ok = r:get(32)
    return ok and r:skip(n * 8)
  end
  if k == 14 or k == 16 then
    local n, ok = message_count(r, s)
    if not ok then return false end
    if k == 14 and s.elem == 6 and not r:align() then return false end
    -- §3.3: a raw element rides at its kind's storage width when the
    -- announcement carries no packed width.
    local width = s.elem_bits
    if width == nil and kind_width(s.elem) ~= 0 then width = kind_width(s.elem) * 8 end
    if width ~= nil or s.elem == 0 or s.elem == 32 then
      width = width or 0
      if width < 0 then width = 0 end
      if k == 16 then width = width + vocab.refBits end
      return r:skip(n * width)
    end
    for _ = 1, n do
      if k == 16 and not r:skip(vocab.refBits) then return false end
      local before = r.off
      if
        not message_skip(
          r,
          vocab,
          indexBits,
          { id = 0, kind = s.elem, bits = s.elem_bits, packing = s.elem_packing, max = s.elem_max, min = s.elem_min },
          depth + 1
        ) or r.off == before
      then
        return false
      end
    end
    return true
  end
  if s.bits ~= nil and s.bits >= 0 then return r:skip(s.bits) end
  -- §3: a RAW scalar's width is its kind's storage width; the announcement
  -- omits Bits for it. §6.6's skip must consume exactly the wire's bits.
  local width = kind_width(k)
  if width ~= 0 then return r:skip(width * 8) end
  return false
end

-- ---- the resolved-payload walker (the Go tableRetainMessageIn) --------------
local In = {}
In.__index = In
local function new_in(bits, vocab, w, limit)
  return setmetatable({ bits = bits, vocab = vocab, w = w, limit = limit }, In)
end
function In:room(n)
  return n >= 0 and n <= self.limit - self.w.n
end
function In:ref(zero)
  local ref, ok = self.bits:get(self.vocab.refBits)
  if not ok then return nil, false end
  if ref == 0 then
    if not zero or not self:room(8) then return nil, false end
    w_u64(self.w, 0)
    return 0, true
  end
  local e = vocab_entry(self.vocab, ref)
  if e == nil or e.id == 0 or e.id >= 0xfffffffffffffffd or not self:room(8) then return nil, false end
  w_u64(self.w, e.id)
  return e, true
end
function In:scalar(e)
  local width = kind_width(e.kind)
  if width == 0 or not self:room(width) then return false end
  -- §3: RAW signed scalars ride two's complement at the kind's width; the
  -- resolved record's signed bytes need the sign extension §6.6 preserves.
  local bits = e.bits
  if bits == nil or bits < 0 then bits = width * 8 end
  if e.packing == 2 then
    local probe = new_reader(self.bits.data, self.bits.off)
    local index, ok = probe:get(bits)
    if not ok or index > (e.qcount or 0) then return false end
  end
  local v, ok = self.bits:get(math.min(bits, 64))
  if not ok then return false end
  local hi = 0
  if bits > 64 then
    hi, ok = self.bits:get(bits - 64)
    if not ok then return false end
  end
  if e.packing == 1 then
    local base = e.base or 0
    local sum = v + base
    local carry = 0
    if sum < v then carry = 1 end
    v = sum
    hi = hi + carry
  elseif e.packing == 2 then
    local idx = math.min(v, e.qcount or 0)
    local ratio = idx / (e.qcount or 1)
    local scaled = ratio * (e.qdelta or 0)
    v = string.unpack("<I4", string.pack("<f", scaled + (e.qmin or 0)))
  end
  if
    (e.packing == nil or e.packing == 0)
    and bits > 0
    and bits < 64
    and (e.kind >= 2 and e.kind <= 5 or e.kind >= 20 and e.kind <= 24)
  then
    local sign = 1 << (bits - 1)
    if v & sign ~= 0 then v = v - (1 << bits) end
  end
  local signed = (e.kind >= 2 and e.kind <= 5) or (e.kind >= 20 and e.kind <= 24)
  if width == 1 then
    if signed then
      w_raw(self.w, string.pack("<i1", v))
    else
      w_u8(self.w, v & 0xff)
    end
  elseif width == 2 then
    if signed then
      w_raw(self.w, string.pack("<i2", v))
    else
      w_raw(self.w, string.pack("<I2", v & 0xffff))
    end
  elseif width == 4 then
    if signed then
      w_raw(self.w, string.pack("<i4", v))
    else
      w_raw(self.w, string.pack("<I4", v & MAXUINT32))
    end
  elseif width == 8 then
    w_u64(self.w, v)
  elseif width == 16 then
    w_raw(self.w, string.pack("<i8", v))
    w_raw(self.w, string.pack("<i8", hi))
  end
  return true
end
function In:opaque(e, framed)
  local n, ok
  if e.kind == 12 or e.kind == 33 then
    n, ok = message_count(self.bits, e)
    if not ok then return false end
    if e.kind == 33 then
      n = n * 2
    elseif not self.bits:align() then
      return false
    end
  else
    if not self.bits:align() then return false end
    n, ok = self.bits:get(32)
    if not ok then return false end
  end
  local extra = 0
  if framed then extra = leb_bytes(n) end
  if n > 0x7fffffff or not self:room(n + extra) then return false end
  if framed then w_leb(self.w, n) end
  for _ = 1, n do
    local b, ok2 = self.bits:get(8)
    if not ok2 then return false end
    w_u8(self.w, b)
  end
  return true
end
function In:framed(e, depth)
  if depth > DEPTH_MAX or not self:room(8) then return false end
  local body = W()
  local inner = new_in(self.bits, self.vocab, body, self.limit - self.w.n - 4)
  if not inner:content(e, depth) then return false end
  w_u32(self.w, body.n)
  w_raw(self.w, table.concat(body.parts))
  return true
end
function In:content(e, depth)
  if depth > DEPTH_MAX then return false end
  local k = e.kind
  if k == 17 then return false end
  if k == 13 then
    while true do
      local entry, ok = self:ref(true)
      if not ok then return false end
      if entry == 0 then return true end
      if not self:room(1) then return false end
      w_u8(self.w, entry.kind)
      if not self:payload(entry, depth) then return false end
    end
  elseif k == 14 or k == 16 then
    return self:elements(e, depth)
  elseif k == 15 or k == 30 then
    return self:payload(e, depth)
  elseif k == 32 then
    return true
  elseif k == 12 or k == 31 or k == 33 then
    return self:opaque(e, false)
  end
  return self:scalar(e)
end
function In:elements(e, depth)
  if e.elem == 17 then return false end
  local n, ok = message_count(self.bits, e)
  if not ok then return false end
  if e.kind == 14 and e.elem == 6 and not self.bits:align() then return false end
  if not self:room(1 + leb_bytes(n)) then return false end
  w_u8(self.w, e.elem)
  w_leb(self.w, n)
  local floor = file_minimum(e.elem)
  if e.kind == 16 then floor = 16 end
  if floor < 0 or n > math.floor((self.limit - self.w.n) / floor) then return false end
  local element = {
    id = 0,
    kind = e.elem,
    bits = e.elem_bits,
    packing = e.elem_packing,
    max = e.elem_max,
    min = e.elem_min,
    qcount = e.elem_qcount,
    qmin = e.elem_qmin,
    qdelta = e.elem_qdelta,
  }
  -- §3.3: a raw element's payload width is its kind's storage width.
  if element.bits == nil and kind_width(e.elem) ~= 0 then element.bits = kind_width(e.elem) * 8 end
  for _ = 1, n do
    if e.kind == 16 then
      if not self:ref(false) then return false end
      if not self:framed(element, depth + 1) then return false end
    else
      if not self:payload(element, depth) then return false end
    end
  end
  return true
end
function In:payload(e, depth)
  local k = e.kind
  if k == 0 or k == 17 then return false end
  if k == 32 then
    if not self:room(1) then return false end
    w_leb(self.w, 0)
    return true
  end
  if k == 30 then
    local _, ok = self:ref(true)
    return ok
  end
  if k == 15 then
    local arm, ok = self:ref(true)
    if not ok then return false end
    if arm == 0 then return true end
    if arm.kind == 0 or not self:room(1) then return false end
    w_u8(self.w, arm.kind)
    return self:framed(arm, depth + 1)
  end
  if k == 13 or k == 14 or k == 16 then return self:framed(e, depth + 1) end
  if k == 12 or k == 31 or k == 33 then return self:opaque(e, true) end
  return self:scalar(e)
end

-- ---- the record store (the variable form's, retain.lua) --------------------
local function path_step(p, ordinal, index)
  local q = { node = p.node, steps = {} }
  for k, v in ipairs(p.steps) do
    q.steps[k] = v
  end
  q.steps[#q.steps + 1] = { o = ordinal, i = index }
  return q
end
local function path_key(p)
  local out = { tostring(p.node), "|" }
  for _, s in ipairs(p.steps) do
    out[#out + 1] = tostring(s.o) .. "," .. tostring(s.i) .. ";"
  end
  return table.concat(out)
end

-- capture one unknown field at a path into the store (the Go capture). It
-- first skips the field on the real reader, then re-walks it into resolved
-- form; on success the record joins the store.
local function capture(_T, vocab, reader, indexBits, e, path, store, report, owner)
  local start = reader.off
  if not message_skip(reader, vocab, indexBits, e, 0) then return false end
  -- A RETAINED UNKNOWN FLOAT IS TESTED ON ITS RAW BITS LIKE A KNOWN ONE
  -- (docs/SPEC-TABLES.md §3.3, §6.6, ruling 3 of 2026-10-27): a plain kind 10
  -- or 11 entry holding a NaN or an infinity refuses the batch as
  -- non_finite_float, named <Owner>.0x<id>.
  if (e.kind == 10 or e.kind == 11) and (e.packing or 0) == 0 then
    local probe = new_reader(reader.data, start)
    local width = e.kind == 10 and 32 or 64
    local b = probe:get(width)
    local mask = e.kind == 10 and F32_EXP or F64_EXP
    if b ~= nil and b & mask == mask then
      if report.nonFiniteFloat == nil then report.nonFiniteFloat = owner .. string.format(".0x%016x", e.id) end
      return true
    end
  end
  if store == nil then return true end
  if not store.list then store.list = {} end
  local probe = new_in(new_reader(reader.data, start), vocab, W(), math.huge)
  if not probe:payload(e, 0) or probe.bits.off ~= reader.off then
    report.retainLost = report.retainLost + 1
    return true
  end
  local header = 26 + #path.steps * 8
  if header + probe.w.n > 0xffffffff then
    report.retainLost = report.retainLost + 1
    return true
  end
  store.list[#store.list + 1] =
    { key = path_key(path), id = e.id, kind = e.kind, payload = table.concat(probe.w.parts), placed = false }
  report.retained = report.retained + 1
  return true
end

-- ---- decoding: read known fields, capture unknown ones ---------------------
local decode_value, decode_body

local function decode_scalar(R, d)
  local s = d.shape
  if s == "bool" then
    local v, ok = R:get(1)
    return v ~= 0, ok
  elseif s == "int" or s == "flags" then
    local width, base = d.bits or (d.width or 0) * 8, 0
    if d.lo ~= nil and d.hi ~= nil then
      width = bits_range(d.lo, d.hi)
      base = d.lo
    end
    if s == "flags" then
      width = d.bits or 8
      base = 0
    end
    local v, ok = R:get(width)
    if not ok then return 0, false end
    if base ~= 0 then v = v + base end
    if base == 0 and d.signed and width < 64 then
      local sign = 1 << (width - 1)
      if v & sign ~= 0 then v = v - (1 << width) end
    end
    return v, true
  elseif s == "float" then
    -- A NaN OR AN INFINITY IS REFUSED ON ITS RAW BITS (docs/SPEC-TABLES.md
    -- §3.3): the third answer says so, and the batch refuses as
    -- non_finite_float and lands nothing.
    local w = d.f32 and 32 or 64
    local v, ok = R:get(w)
    if not ok then return 0.0, false end
    if w == 32 then
      v = v & MAXUINT32
      if v & F32_EXP == F32_EXP then return 0.0, false, true end
      return string.unpack("<f", string.pack("<I4", v)), true
    end
    if v & F64_EXP == F64_EXP then return 0.0, false, true end
    return string.unpack("<d", string.pack("<I8", v)), true
  end
  return 0, false
end

-- A BODY PATH STEP BELONGS TO THE HOLDER (§6.6, retain.lua's capture and
-- save): a declared field's table adds its own (ordinal, 0) step, but an
-- array's element and a union's arm are already addressed by the step the
-- array or the union added, (ordinal, index) or (ordinal, tag), so `inner` is
-- true for them and `ord` carries the holding field's ordinal to an arm.
decode_value = function(T, vocab, reader, indexBits, f, holder, key, path, store, report, ord, inner)
  local s = f.shape
  if s == "pointer" or s == "string_blob" or s == "bytes_blob" then
    local index, ok = reader:get(indexBits)
    if not ok then return false end
    if index ~= 0 then
      local node = (reader.nodes or {})[index]
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
    local n, ok = reader:get(w)
    if not ok or not reader:align() then return false end
    local raw = reader:raw(n)
    if raw == nil then return false end
    if n > f.bound then raw = raw:sub(1, f.bound) end
    holder[key] = raw
    return true
  elseif s == "wstring" then
    local w = bits_range(0, f.bound)
    local n, ok = reader:get(w)
    if not ok then return false end
    local out = {}
    for i = 1, n do
      local u, ok2 = reader:get(16)
      if not ok2 then return false end
      out[i] = u
    end
    holder[key] = out
    return true
  elseif s == "enum" then
    local ref, ok = reader:get(vocab.refBits)
    if not ok then return false end
    if ref == 0 then
      holder[key] = nil
      return true
    end
    local e = vocab_entry(vocab, ref)
    if e == nil then return false end
    for _, vr in ipairs(f.variants) do
      if vr[2] == e.id then
        holder[key] = vr[1]
        return true
      end
    end
    report.retainLost = report.retainLost + 1
    return true
  elseif s == "union" then
    local ref, ok = reader:get(vocab.refBits)
    if not ok then return false end
    if ref == 0 then
      holder[key] = { tag = 0 }
      return true
    end
    local e = vocab_entry(vocab, ref)
    if e == nil then return false end
    local un = T.Unions[f.target]
    if un == nil then return false end
    for i, arm in ipairs(un.arms) do
      if arm.id == e.id then
        local uv = { tag = i }
        local o = ord or (f.ordinal - 1)
        local p = path_step(path, o, arm.tag)
        if arm.field ~= nil then
          local owner, field = reader.owner, reader.field
          reader.owner, reader.field = un.name, arm.name
          local aok = decode_value(T, vocab, reader, indexBits, arm.field, uv, arm.slot, p, store, report, o, true)
          reader.owner, reader.field = owner, field
          if not aok then return false end
        end
        holder[key] = uv
        return true
      end
    end
    report.retainLost = report.retainLost + 1
    return true
  elseif s == "table" then
    local nv
    if holder[key] == nil then
      nv = T["New" .. f.target]()
    else
      nv = holder[key]
    end
    local p = path
    if not inner then p = path_step(path, f.ordinal - 1, 0) end
    if not decode_body(T, vocab, reader, indexBits, T.Tables[f.target], nv, p, store, report) then return false end
    holder[key] = nv
    return true
  elseif s == "array" then
    local d = f
    local n, ok
    if d.fixed then
      ok = true
      n = d.bound
    elseif d.list then
      n, ok = reader:get(32)
    else
      n, ok = reader:get(bits_range(d.min or 0, d.bound))
      if ok then n = n + (d.min or 0) end
    end
    if not ok then return false end
    if d.elem_kind == 6 and d.elem.shape ~= "bytes" then
      if not reader:align() then return false end
      local raw = reader:raw(n)
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
        local kref, kok = reader:get(vocab.refBits)
        if not kok then return false end
        local ke = vocab_entry(vocab, kref)
        if ke == nil then return false end
        local kidx = variant_index(d.keyed, ke.id)
        if kidx == nil then report.retainLost = report.retainLost + 1 end
        local p = path_step(path, f.ordinal - 1, (kidx or 1) - 1)
        local ev = {}
        if d.elem.shape == "table" or d.elem.shape == "union" then
          if not decode_value(T, vocab, reader, indexBits, d.elem, ev, "v", p, store, report, f.ordinal - 1, true) then
            return false
          end
        else
          if
            not decode_value(T, vocab, reader, indexBits, d.elem, ev, "v", path, store, report, f.ordinal - 1, true)
          then
            return false
          end
        end
        if kidx ~= nil then out[kidx] = ev.v end
      end
    else
      for i = 1, n do
        local p = path_step(path, f.ordinal - 1, i - 1)
        local ev = {}
        if not decode_value(T, vocab, reader, indexBits, d.elem, ev, "v", p, store, report, f.ordinal - 1, true) then
          return false
        end
        out[i] = ev.v
      end
    end
    holder[key] = out
    return true
  elseif s == "map" then
    local n, ok = reader:get(bits_range(0, f.bound))
    if not ok then return false end
    local entry = T.Tables[f.entry]
    local out = {}
    for j = 1, n do
      local p = path_step(path, f.ordinal - 1, j - 1)
      if entry == nil then return false end
      local kv = T["New" .. entry.name]()
      if not decode_body(T, vocab, reader, indexBits, entry, kv, p, store, report) then return false end
      local kf = f.key_field.name
      out[kv[kf]] = kv
    end
    holder[key] = out
    return true
  else
    local v, ok, nonfinite = decode_scalar(reader, f)
    if nonfinite and report.nonFiniteFloat == nil then
      report.nonFiniteFloat = (reader.owner or "?") .. "." .. tostring(reader.field or f.name)
    end
    if ok then holder[key] = v end
    return ok
  end
end

local decode_fields

-- decode_body names a float <Table>.<field> for the non_finite_float refusal
-- (docs/SPEC-TABLES.md §3.3), and gives the holder's name back on return.
decode_body = function(T, vocab, reader, indexBits, desc, value, path, store, report)
  if desc == nil then return false end
  local owner, field = reader.owner, reader.field
  reader.owner, reader.field = desc.name, nil
  local ok = decode_fields(T, vocab, reader, indexBits, desc, value, path, store, report)
  reader.owner, reader.field = owner, field
  return ok
end

decode_fields = function(T, vocab, reader, indexBits, desc, value, path, store, report)
  while true do
    local ref, ok = reader:get(vocab.refBits)
    if not ok then return false end
    if ref == 0 then return true end
    local e = vocab_entry(vocab, ref)
    if e == nil then return false end
    if e.id == NODE_ID then
      if not message_skip(reader, vocab, indexBits, e, 0) then return false end
    else
      local fd = field_by_id(desc, e.id)
      if fd == nil then
        if not capture(T, vocab, reader, indexBits, e, path, store, report, desc.name) then return false end
        if report.nonFiniteFloat ~= nil then return false end
      else
        reader.field = fd.name
        if not decode_value(T, vocab, reader, indexBits, fd, value, fd.name, path, store, report) then return false end
      end
    end
  end
end

-- ---- LoadRetainMessages: read the batch, building values and retaining ------
local function load_one_retain(T, root, vocab, reader, store, report)
  local indexBits = 1
  local save = reader.off
  local ref, ok = reader:get(vocab.refBits)
  if not ok then return nil end
  local e = ref ~= 0 and vocab_entry(vocab, ref) or nil
  local nodes = { [1] = T["New" .. root]() }
  reader.nodes = nodes
  if e ~= nil and e.id == NODE_ID then
    local count, cok = reader:get(32)
    if not cok then return nil end
    indexBits = bits_max(count + 1)
    if indexBits < 1 then indexBits = 1 end
    for k = 1, count do
      local tref, tok = reader:get(vocab.refBits)
      if not tok then return nil end
      local te = vocab_entry(vocab, tref)
      if te == nil then return nil end
      local node = {}
      local p = { node = k + 1, steps = {} }
      if te.id == STRING_BLOB_ID or te.id == BYTES_BLOB_ID then
        local n, nok = reader:get(32)
        if not nok then return nil end
        local raw = reader:raw(n)
        if raw == nil then return nil end
        node.blob = raw
      else
        local tname, td = table_by_type(T, te.id)
        if tname == nil then return nil end
        local def = T["New" .. tname]()
        for kk, vv in pairs(def) do
          node[kk] = vv
        end
        if not decode_body(T, vocab, reader, indexBits, td, node, p, store, report) then return nil end
      end
      nodes[k + 1] = node
    end
  else
    reader.off = save
  end
  local desc = T.Tables[root]
  if desc == nil then return nil end
  local p = { node = 1, steps = {} }
  if not decode_body(T, vocab, reader, indexBits, desc, nodes[1], p, store, report) then return nil end
  return nodes[1]
end

local function announce_read(T, vocab, data, report)
  return (require("message")).announceRead(T, vocab, data, report)
end

function S.AnnounceRead(T, vocab, data, report)
  return announce_read(T, vocab, data, report)
end

function S.LoadRetainMessages(T, root, vocab, data, stores, report)
  report = report or {}
  report.malformed = false
  report.refused = 0
  report.reason = ""
  report.nonFiniteFloat = nil
  report.retained = report.retained or 0
  report.retainLost = report.retainLost or 0
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
  local reader = new_reader(data, 16)
  local values = {}
  for i = 1, count do
    local store = stores and stores[i] or nil
    local v = load_one_retain(T, root, vocab, reader, store, report)
    if report.nonFiniteFloat ~= nil then
      -- A NON-FINITE FLOAT REFUSES THE BATCH (docs/SPEC-TABLES.md §3.3): no
      -- record lands and nothing captured stays.
      report.reason = "non_finite_float: " .. report.nonFiniteFloat
      if stores ~= nil then
        for j = 1, i do
          if stores[j] ~= nil then stores[j].list = {} end
        end
      end
      return {}
    end
    if v == nil then
      report.malformed = true
      return values
    end
    values[i] = v
  end
  if not reader:align() or reader.off ~= reader.nbits then report.malformed = true end
  return values
end

return S
