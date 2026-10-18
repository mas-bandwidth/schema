-- load.lua — the Lua table runtime's VARIABLE FORM (form 1) tolerant load
-- (docs/SPEC-TABLES.md §3, §3.1, §4; docs/PORT-LUA.md, THE CARD MAP). A port
-- of the JavaScript leg's TableVariableLoad walk
-- (internal/codegen/jstable/variableruntime.go) and of the Go reference
-- engine (internal/tablewire decode.go, decodenodes.go). Emitted beside the
-- unit's <Package>Tables.lua by internal/codegen/luatable, which embeds
-- runtime/*.lua by glob.
--
-- The load is DRIVEN BY THE DESCRIPTOR (docs/SPEC-TABLES.md §8.1): one entry
-- per field naming its wire id (§5), its wire kind (§3), its shape and the
-- facts the shape needs. Every rule lives once, here.
--
-- THE REPORT (docs/SPEC-TABLES.md §4): the six counters and the verdict. The
-- caller owns it; the codec never makes one. A load returns the root value
-- and fills the report.
local L = {}

local NODE_TABLE_ID = -1         -- 0xffffffffffffffff
local BUILD_VERSION_ID = -2      -- 0xfffffffffffffffe
local MESSAGE_VOCABULARY_ID = -3 -- 0xfffffffffffffffd
local STRING_BLOB_ID = 0x704be0d8faaffc58
local BYTES_BLOB_ID = 0x2f2ec0474f1c4fe4

-- THE READER (docs/PORT-LUA.md, BYTE BUFFERS): a Lua string of immutable
-- bytes and a 0-based cursor with a limit for a nested body.
local function new_reader(b, at, lim)
  return { b = b, at = at, lim = lim, bad = false }
end

local function byteat(r)
  return string.byte(r.b, r.at + 1)
end

local function r_u8(r)
  if r.at >= r.lim then r.bad = true; return 0 end
  local v = byteat(r)
  r.at = r.at + 1
  return v
end

-- CANONICAL LEB128 (§3): a failed read leaves the cursor where it started.
local function r_leb(r)
  local start = r.at
  local v = 0
  local s = 0
  for i = 0, 9 do
    if r.at >= r.lim then r.bad = true; r.at = start; return 0 end
    local x = byteat(r)
    r.at = r.at + 1
    if i == 9 and x > 1 then r.bad = true; r.at = start; return 0 end
    v = v | ((x & 0x7f) << s)
    if (x & 0x80) == 0 then
      if i > 0 and x == 0 then r.bad = true; r.at = start; return 0 end
      return v
    end
    s = s + 7
  end
  r.bad = true
  r.at = start
  return 0
end

local function r_i8(r)
  local v = r_u8(r)
  if v >= 0x80 then return v - 0x100 end
  return v
end

local function r_u16(r)
  if r.at + 2 > r.lim then r.bad = true; return 0 end
  local v = string.unpack("<I2", r.b, r.at + 1)
  r.at = r.at + 2
  return v
end

local function r_i16(r)
  if r.at + 2 > r.lim then r.bad = true; return 0 end
  local v = string.unpack("<i2", r.b, r.at + 1)
  r.at = r.at + 2
  return v
end

local function r_u32(r)
  if r.at + 4 > r.lim then r.bad = true; return 0 end
  local v = string.unpack("<I4", r.b, r.at + 1)
  r.at = r.at + 4
  return v
end

local function r_i32(r)
  if r.at + 4 > r.lim then r.bad = true; return 0 end
  local v = string.unpack("<i4", r.b, r.at + 1)
  r.at = r.at + 4
  return v
end

-- 64 bits kept as the Lua integer with the same bits (docs/PORT-LUA.md,
-- 64-BIT AND 128-BIT INTEGERS): an unsigned read is the same signed bits.
local function r_u64(r)
  if r.at + 8 > r.lim then r.bad = true; return 0 end
  local v = string.unpack("<i8", r.b, r.at + 1)
  r.at = r.at + 8
  return v
end

local function r_i64(r)
  return r_u64(r)
end

local function r_f64(r)
  if r.at + 8 > r.lim then r.bad = true; return 0.0 end
  local v = string.unpack("<d", r.b, r.at + 1)
  r.at = r.at + 8
  return v
end

local function r_f32(r)
  if r.at + 4 > r.lim then r.bad = true; return 0.0 end
  local v = string.unpack("<f", r.b, r.at + 1)
  r.at = r.at + 4
  return v
end

local function r_bytes(r, n)
  if r.at + n > r.lim then r.bad = true; return "" end
  local s = string.sub(r.b, r.at + 1, r.at + n)
  r.at = r.at + n
  return s
end

-- THE FIXED WIDTHS of §3's kind table; 0 is a length- or reference-shaped
-- kind (or one a skipper reads by its L).
local function width_of(kind)
  if kind == 1 or kind == 2 or kind == 6 or kind == 20 or kind == 25 then return 1
  elseif kind == 3 or kind == 7 or kind == 21 or kind == 26 then return 2
  elseif kind == 4 or kind == 8 or kind == 10 or kind == 22 or kind == 27 then return 4
  elseif kind == 5 or kind == 9 or kind == 11 or kind == 23 or kind == 28 or kind == 36 then return 8
  elseif kind == 18 or kind == 19 or kind == 24 or kind == 29 then return 16
  end
  return 0
end

-- skipping a field the reader cannot name (§3): fixed widths first, then
-- references, then lengths, and a unknown kind is damage (not skippable).
local function skip(r, kind)
  local w = width_of(kind)
  if w > 0 then
    if r.at + w > r.lim then return false end
    r.at = r.at + w
    return true
  end
  if kind == 17 or kind == 30 then
    r_leb(r)
    return not r.bad
  end
  if kind == 12 or kind == 13 or kind == 14 or kind == 16 or kind == 31 or kind == 32 or kind == 33 then
    local n = r_leb(r)
    if r.bad or r.at + n > r.lim then return false end
    r.at = r.at + n
    return true
  end
  if kind == 15 then
    local arm = r_leb(r)
    if r.bad then return false end
    if arm ~= 0 then
      if r.at + 1 > r.lim then return false end
      r_u8(r)
      local n = r_leb(r)
      if r.bad or r.at + n > r.lim then return false end
      r.at = r.at + n
    end
    return true
  end
  return false
end

-- the form byte, the trailer and the id table, and the root body's own
-- framing (docs/SPEC-TABLES.md §3): nil when the wire is refused or damaged.
local function open(bytes, report)
  if #bytes < 1 then report.malformed = true; return nil end
  if bytes:byte(1) ~= 1 then report.refused = 1; return nil end
  if #bytes < 9 then report.malformed = true; return nil end
  local count = string.unpack("<i8", bytes, #bytes - 7)
  if count < 0 or count > (#bytes - 9) // 8 then report.malformed = true; return nil end
  local endpos = #bytes - 8 - count * 8
  local table_ids = {}
  local seen = {}
  for i = 0, count - 1 do
    local id = string.unpack("<i8", bytes, endpos + i * 8 + 1)
    if seen[id] then report.malformed = true; return nil end
    seen[id] = true
    table_ids[i + 1] = id
  end
  -- roots end at their own zero reference; bytes after it are damage (§3)
  local w = new_reader(bytes, 1, endpos)
  while true do
    local ref = r_leb(w)
    if w.bad then break end
    if ref == 0 then
      if w.at ~= w.lim then report.malformed = true; return nil end
      break
    end
    if ref > #table_ids then break end
    if w.at >= w.lim then break end
    local kind = r_u8(w)
    if not skip(w, kind) then break end
  end
  return { table = table_ids, lim = endpos }
end

-- THE NODE RECORDS (§3.1): the numbering is positional and whole or nothing.
-- The scan is authoritative: the count must match the records it frames.
local function node_scan(bytes, o, report)
  local w = new_reader(bytes, 1, o.lim)
  local payload = nil
  local present = false
  local bad = false
  while not bad do
    local ref = r_leb(w)
    if w.bad then break end
    if ref == 0 then break end
    if ref > #o.table then break end
    if w.at >= w.lim then break end
    local id = o.table[ref]
    local kind = r_u8(w)
    if id == NODE_TABLE_ID then
      present = true
      if kind ~= 12 then bad = true; break end
      local n = r_leb(w)
      if w.bad or w.at + n > w.lim then bad = true; break end
      payload = string.sub(w.b, w.at + 1, w.at + n)
      w.at = w.at + n
    elseif not skip(w, kind) then
      break
    end
  end
  if present and bad then report.malformed = true; return nil end
  if not present then return {} end
  local rec = new_reader(payload, 0, #payload)
  local count = r_leb(rec)
  if rec.bad then report.malformed = true; return nil end
  local raw = {}
  while rec.at < rec.lim do
    local tref = r_leb(rec)
    if rec.bad or tref == 0 or tref > #o.table then report.malformed = true; return nil end
    local n = r_leb(rec)
    if rec.bad or rec.at + n > rec.lim then report.malformed = true; return nil end
    raw[#raw + 1] = { tid = o.table[tref], body = string.sub(payload, rec.at + 1, rec.at + n) }
    rec.at = rec.at + n
  end
  if count ~= #raw then report.malformed = true; return nil end
  return raw
end

-- UTF-8 well-formedness and no zero byte (docs/SPEC-TABLES.md §3): ill-formed
-- text is framing-class damage, the field reads its declared default.
local function utf8_ok(s)
  local i, n = 1, #s
  while i <= n do
    local c = s:byte(i)
    if c == 0 then return false end
    if c < 0x80 then
      i = i + 1
    elseif c >= 0xc2 and c <= 0xdf then
      if i + 1 > n then return false end
      local b1 = s:byte(i + 1)
      if b1 < 0x80 or b1 > 0xbf then return false end
      i = i + 2
    elseif c >= 0xe0 and c <= 0xef then
      if i + 2 > n then return false end
      local b1, b2 = s:byte(i + 1), s:byte(i + 2)
      if b1 == nil or b2 == nil then return false end
      if b1 < 0x80 or b1 > 0xbf or b2 < 0x80 or b2 > 0xbf then return false end
      if c == 0xe0 and b1 < 0xa0 then return false end
      if c == 0xed and b1 > 0x9f then return false end
      i = i + 3
    elseif c >= 0xf0 and c <= 0xf4 then
      if i + 3 > n then return false end
      local b1, b2, b3 = s:byte(i + 1), s:byte(i + 2), s:byte(i + 3)
      if b1 == nil or b2 == nil or b3 == nil then return false end
      if b1 < 0x80 or b1 > 0xbf or b2 < 0x80 or b2 > 0xbf or b3 < 0x80 or b3 > 0xbf then return false end
      if c == 0xf0 and b1 < 0x90 then return false end
      if c == 0xf4 and b1 > 0x8f then return false end
      i = i + 4
    else
      return false
    end
  end
  return true
end

-- A CLAMP CUTS AT A CODE POINT BOUNDARY (§3)
local function utf8_clamp(s, bound)
  local cut = bound
  while cut > 0 and (s:byte(cut + 1) & 0xc0) == 0x80 do cut = cut - 1 end
  return s:sub(1, cut)
end

-- wide text is L/2 UTF-16 code units, two bytes little-endian, surrogates
-- paired and no zero unit (§3): an odd L or ill-formed content is damage.
local function wstring_ok(s)
  if #s % 2 ~= 0 then return false, {} end
  local units = {}
  local i = 1
  while i <= #s do
    local u = s:byte(i) + s:byte(i + 1) * 0x100
    if u == 0 then return false, {} end
    if u >= 0xd800 and u <= 0xdbff then
      if i + 3 > #s then return false, {} end
      local lo = s:byte(i + 2) + s:byte(i + 3) * 0x100
      if lo < 0xdc00 or lo > 0xdfff then return false, {} end
      units[#units + 1] = u
      units[#units + 1] = lo
      i = i + 4
    elseif u >= 0xdc00 and u <= 0xdfff then
      return false, {}
    else
      units[#units + 1] = u
      i = i + 2
    end
  end
  return true, units
end

-- THE WIDENING RUNGS (§4): a narrower kind on its own signedness ladder, and
-- f32 into f64.
local function widens(kind, declared)
  if declared == 3 or declared == 4 or declared == 5 then return kind >= 2 and kind < declared end
  if declared == 7 or declared == 8 or declared == 9 then return kind >= 6 and kind < declared end
  if declared == 11 then return kind == 10 end
  return false
end

local function less_than(d, a, b)
  if a == b then return false end
  if d.signed then return a < b end
  return math.ult(a, b)
end

-- a scalar decoded at the WIRE kind (the declared kind or one it widens),
-- sign-extended where the kind is signed, then the declared range and the
-- bits(N) clamp (§4, decode.go scalarAt).
local function scalar_at(r, d, kind, report)
  local w = width_of(kind)
  if w == 0 or r.at + w > r.lim then report.malformed = true; return nil end
  local x
  if kind == 1 then
    x = r_u8(r) ~= 0
  elseif kind == 10 then
    x = r_f32(r)
  elseif kind == 11 then
    x = r_f64(r)
  elseif kind == 2 then
    x = r_i8(r)
  elseif kind == 3 then
    x = r_i16(r)
  elseif kind == 4 then
    x = r_i32(r)
  elseif kind == 5 or kind == 36 then
    x = r_i64(r)
  elseif kind == 6 then
    x = r_u8(r)
  elseif kind == 7 then
    x = r_u16(r)
  elseif kind == 8 then
    x = r_u32(r)
  elseif kind == 9 then
    x = r_u64(r)
  else
    report.malformed = true; return nil
  end
  if r.bad then report.malformed = true; return nil end
  if d.lo ~= nil and d.hi ~= nil then
    if less_than(d, x, d.lo) then x = d.lo; report.clamped = report.clamped + 1
    elseif less_than(d, d.hi, x) then x = d.hi; report.clamped = report.clamped + 1 end
  end
  if d.bits ~= nil and d.bits > 0 and d.bits < w * 8 then
    local bmax = (1 << d.bits) - 1
    if kind == 1 then
    elseif less_than({ signed = false }, bmax, x) then x = bmax; report.clamped = report.clamped + 1 end
  end
  return x
end

-- an enum reference (§3): 0 is None, a reference past the id table is damage,
-- and a variant this enum cannot name reads None and counts unknown.
local function enum_read(atable, r, d, report)
  local ref = r_leb(r)
  if r.bad then report.malformed = true; return nil, false end
  if ref == 0 then return nil, true end
  if ref > #atable then report.malformed = true; return nil, false end
  local id = atable[ref]
  for _, v in ipairs(d.variants) do
    if v[2] == id then return v[1], true end
  end
  report.unknown = report.unknown + 1
  return nil, true
end

local read_body
local read_field
local read_array_body
local read_element
local read_union
local read_union_at

local function fresh(T, desc)
  local ctor = T["New" .. desc.name]
  if ctor == nil then return {} end
  return ctor()
end

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

-- one element of an array body (docs/SPEC-TABLES.md §2.9, §3)
read_element = function(T, ctx, e, ek, sub, out, index)
  local s = e.shape
  if s == "pointer" or s == "string_blob" or s == "bytes_blob" then
    local i = r_leb(sub)
    if sub.bad then ctx.report.malformed = true; return false, false end
    ctx.pending[#ctx.pending + 1] = { holder = out, key = index, index = i, d = e }
    return false, true
  elseif s == "enum" then
    local x, ok = enum_read(ctx.table, sub, e, ctx.report)
    if not ok then return false, false end
    return (x == nil) and false or x, true
  elseif s == "table" then
    local n = r_leb(sub)
    if sub.bad or sub.at + n > sub.lim then ctx.report.malformed = true; return false, false end
    local d = T.Tables[e.target]
    local inst = fresh(T, d)
    local body = new_reader(sub.b, sub.at, sub.at + n)
    read_body(T, ctx, d, body, inst, true)
    if body.at ~= body.lim then ctx.report.malformed = true; inst = fresh(T, d) end
    sub.at = sub.at + n
    return inst, true
  elseif s == "union" then
    local u = T.Unions[e.target]
    if u == nil then ctx.report.malformed = true; return false, false end
    local x = read_union(T, ctx, u, sub)
    if x == nil then return false, false end
    return x, true
  elseif s == "string" then
    local n = r_leb(sub)
    if sub.bad or sub.at + n > sub.lim then ctx.report.malformed = true; return false end
    local raw = r_bytes(sub, n)
    if not utf8_ok(raw) then ctx.report.malformed = true; return e.default or "", true end
    if e.bound ~= nil and e.bound > 0 and #raw > e.bound then
      ctx.report.clamped = ctx.report.clamped + 1
      return utf8_clamp(raw, e.bound), true
    end
    return raw, true
  elseif s == "wstring" then
    local n = r_leb(sub)
    if sub.bad or sub.at + n > sub.lim then ctx.report.malformed = true; return false end
    local raw = r_bytes(sub, n)
    local ok, units = wstring_ok(raw)
    if not ok then ctx.report.malformed = true; return {}, true end
    if e.bound ~= nil and e.bound > 0 and #units > e.bound then
      ctx.report.clamped = ctx.report.clamped + 1
      local keep = {}
      for i = 1, e.bound do keep[i] = units[i] end
      if e.bound > 0 and keep[e.bound] >= 0xd800 and keep[e.bound] <= 0xdbff then keep[e.bound] = nil end
      return keep, true
    end
    return units, true
  end
  local x = scalar_at(sub, e, ek, ctx.report)
  if x == nil then return false, false end
  return x, true
end

-- AN ARRAY BODY (§2.9, §3, decode.go arrayBody): a body shorter than its own
-- header is inert; a damaged count is malformed and keeps the field; an
-- element kind that is not this array's (and does not widen) is a kind
-- mismatch and keeps the field; a bounded array's excess is clamped.
read_array_body = function(T, ctx, fd, sub)
  local report = ctx.report
  local ek = r_u8(sub)
  local count = r_leb(sub)
  if sub.bad then report.malformed = true; return nil end

  if fd.shape == "bytes" then
    local expected = 6
    if ek ~= expected then
      if not widens(ek, expected) then report.kindMismatch = report.kindMismatch + 1; return "" end
      report.widened = report.widened + 1
    end
    local keep = count
    if fd.bound ~= nil and count > fd.bound then keep = fd.bound; report.clamped = report.clamped + 1 end
    local out = {}
    for i = 1, keep do
      if sub.at >= sub.lim then report.malformed = true; break end
      out[#out + 1] = string.char(r_u8(sub))
    end
    return table.concat(out)
  end

  if fd.shape == "map" then
    local expected = fd.elem_kind or 13
    if ek ~= expected then
      if not widens(ek, expected) then report.kindMismatch = report.kindMismatch + 1; return {} end
      report.widened = report.widened + 1
    end
    local entry = T.Tables[fd.entry]
    local keyname = fd.key_field.name
    local valname = fd.value_field.name
    local out = {}
    local seen = {}
    for i = 1, count do
      if sub.at >= sub.lim then report.malformed = true; break end
      local n = r_leb(sub)
      if sub.bad or sub.at + n > sub.lim then report.malformed = true; break end
      local body = new_reader(sub.b, sub.at, sub.at + n)
      local inst = fresh(T, entry)
      read_body(T, ctx, entry, body, inst, true)
      if body.at ~= body.lim then report.malformed = true end
      sub.at = sub.at + n
      local key = inst[keyname]
      if seen[key] then report.duplicate = report.duplicate + 1 else seen[key] = true end
      -- A VALUE THAT IS A POINTER OR BLOB is queued for RESOLUTION (§3.1)
      -- against the entry's instance, so copying it here would take the nil
      -- placeholder before resolve fills it. Point the queued slot at the map
      -- itself under the entry's key, so resolve lands the resolved value
      -- there (§3.1: a map value is a pointer field of the generated entry).
      local vf = fd.value_field
      if vf.shape == "pointer" or vf.shape == "string_blob" or vf.shape == "bytes_blob" then
        for j = #ctx.pending, 1, -1 do
          local pp = ctx.pending[j]
          if pp.holder == inst then
            pp.holder = out
            pp.key = key
          end
        end
      else
        out[key] = inst[valname]
      end
    end
    return out
  end

  local expected = fd.elem_kind
  if ek ~= expected then
    if not widens(ek, expected) then report.kindMismatch = report.kindMismatch + 1; return {} end
    report.widened = report.widened + 1
  end

  if fd.keyed ~= nil then
    local out = {}
    for i = 1, fd.bound do out[i] = zero_elem(T, fd.elem) end
    local keep = count
    if count > fd.bound then keep = fd.bound; report.clamped = report.clamped + 1 end
    for _ = 1, keep do
      if sub.at >= sub.lim then report.malformed = true; break end
      local keyref = r_leb(sub)
      if sub.bad or keyref == 0 or keyref > #ctx.table then report.malformed = true; break end
      local n = r_leb(sub)
      if sub.bad or sub.at + n > sub.lim then report.malformed = true; break end
      local slot = nil
      local keyid = ctx.table[keyref]
      for k, v in ipairs(fd.keyed) do if v[2] == keyid then slot = k end end
      if slot == nil then
        report.unknown = report.unknown + 1
      else
        local body = new_reader(sub.b, sub.at, sub.at + n)
        local e = fd.elem
        local es = e.shape
        if es == "pointer" or es == "string_blob" or es == "bytes_blob" then
          local idx = r_leb(body)
          if body.bad or body.at ~= body.lim then report.malformed = true
          else
            ctx.pending[#ctx.pending + 1] = { holder = out, key = slot, index = idx, d = e }
          end
        elseif es == "enum" then
          local x, ok = enum_read(ctx.table, body, e, report)
          if not ok or body.at ~= body.lim then report.malformed = true
          else out[slot] = (x == nil) and false or x end
        elseif es == "table" then
          local d = T.Tables[e.target]
          local inst = fresh(T, d)
          read_body(T, ctx, d, body, inst, true)
          if body.at ~= body.lim then report.malformed = true; inst = fresh(T, d) end
          out[slot] = inst
        elseif es == "union" then
          local u = T.Unions[e.target]
          local x = nil
          if u == nil then report.malformed = true else x = read_union(T, ctx, u, body) end
          if x == nil or body.at ~= body.lim then report.malformed = true else out[slot] = x end
        elseif es == "string" then
          local raw = string.sub(body.b, body.at + 1, body.lim)
          body.at = body.lim
          if not utf8_ok(raw) then report.malformed = true
          else
            if e.bound ~= nil and e.bound > 0 and #raw > e.bound then
              report.clamped = report.clamped + 1
              raw = utf8_clamp(raw, e.bound)
            end
            out[slot] = raw
          end
        elseif es == "wstring" then
          local raw = string.sub(body.b, body.at + 1, body.lim)
          body.at = body.lim
          local ok, units = wstring_ok(raw)
          if not ok then report.malformed = true else out[slot] = units end
        else
          local x = scalar_at(body, e, ek, report)
          if x == nil or body.at ~= body.lim then report.malformed = true
          else out[slot] = x end
        end
      end
      sub.at = sub.at + n
    end
    return out
  end

  local out = {}
  if fd.fixed then
    for i = 1, fd.bound do out[i] = zero_elem(T, fd.elem) end
  end
  local keep = count
  if fd.list then
    if count > 0x7fffffff then report.malformed = true; return {} end
  elseif count > fd.bound then
    keep = fd.bound
    report.clamped = report.clamped + 1
  end
  for i = 1, keep do
    if sub.at >= sub.lim then report.malformed = true; break end
    local el, ok = read_element(T, ctx, fd.elem, ek, sub, out, i)
    if not ok then break end
    out[i] = el
  end
  return out
end

-- ONE BODY (§3, §4): fields by id in any order, a field this table cannot
-- name skipped by its kind and counted unknown, a kind that widens decoded at
-- its own width and counted widened, any other kind a kind mismatch skipped
-- whole. Damage stops the body.
read_body = function(T, ctx, desc, r, v, nested)
  local report = ctx.report
  while true do
    local ref = r_leb(r)
    if r.bad then report.malformed = true; return false end
    if ref == 0 then return true end
    if ref > #ctx.table then report.malformed = true; return false end
    if r.at >= r.lim then report.malformed = true; return false end
    local kind = r_u8(r)
    local id = ctx.table[ref]
    -- the announcement's reserved ids ride in the announcement and nowhere
    -- else (§3.3): in any body they are malformed
    if id == BUILD_VERSION_ID or id == MESSAGE_VOCABULARY_ID then report.malformed = true; return false end
    if id == NODE_TABLE_ID then
      -- the node table rides in a VARIABLE root body and nowhere else (§3.1);
      -- a reader holding no numbering skips it by its kind and counts unknown
      if nested then report.malformed = true; return false end
      if desc.variable then
        if not skip(r, kind) then report.malformed = true; return false end
      else
        report.unknown = report.unknown + 1
        if not skip(r, kind) then report.malformed = true; return false end
      end
    else
      local fd = nil
      for _, f in ipairs(desc.fields) do if f.id == id then fd = f; break end end
      if fd == nil then
        report.unknown = report.unknown + 1
        if not skip(r, kind) then report.malformed = true; return false end
      elseif kind ~= fd.kind then
        if widens(kind, fd.kind) then
          local x = scalar_at(r, fd, kind, report)
          if x == nil then return false end
          v[fd.name] = x
          if fd.optional then v[fd.name .. "_present"] = true end
          report.widened = report.widened + 1
        else
          report.kindMismatch = report.kindMismatch + 1
          if not skip(r, kind) then report.malformed = true; return false end
        end
      else
        if not read_field(T, ctx, fd, r, v) then return false end
      end
    end
  end
end

-- ONE FIELD (§3, §4), at its declared kind: scalars and strings decode into
-- storage, a table nests by value, an array is AN ARRAY BODY, a union is an
-- arm header, and a pointer stores its index for RESOLUTION (§3.1).
read_field = function(T, ctx, fd, r, v)
  local report = ctx.report
  local s = fd.shape
  if s == "bool" or s == "int" or s == "flags" or s == "float" then
    local x = scalar_at(r, fd, fd.kind, report)
    if x == nil then return false end
    v[fd.name] = x
    if fd.optional then v[fd.name .. "_present"] = true end
    return true
  elseif s == "string" then
    local n = r_leb(r)
    if r.bad or r.at + n > r.lim then report.malformed = true; return false end
    local raw = r_bytes(r, n)
    if not utf8_ok(raw) then
      report.malformed = true
      v[fd.name] = fd.default or ""
      if fd.optional then v[fd.name .. "_present"] = true end
      return true
    end
    if fd.bound ~= nil and fd.bound > 0 and #raw > fd.bound then
      report.clamped = report.clamped + 1
      raw = utf8_clamp(raw, fd.bound)
    end
    v[fd.name] = raw
    if fd.optional then v[fd.name .. "_present"] = true end
    return true
  elseif s == "wstring" then
    local n = r_leb(r)
    if r.bad or r.at + n > r.lim then report.malformed = true; return false end
    local raw = r_bytes(r, n)
    local ok, units = wstring_ok(raw)
    if not ok then
      report.malformed = true
      v[fd.name] = {}
      if fd.optional then v[fd.name .. "_present"] = true end
      return true
    end
    if fd.bound ~= nil and fd.bound > 0 and #units > fd.bound then
      report.clamped = report.clamped + 1
      local keep = {}
      for i = 1, fd.bound do keep[i] = units[i] end
      if fd.bound > 0 and keep[fd.bound] >= 0xd800 and keep[fd.bound] <= 0xdbff then keep[fd.bound] = nil end
      units = keep
    end
    v[fd.name] = units
    if fd.optional then v[fd.name .. "_present"] = true end
    return true
  elseif s == "enum" then
    local x, ok = enum_read(ctx.table, r, fd, report)
    if not ok then return false end
    v[fd.name] = x
    if fd.optional then v[fd.name .. "_present"] = true end
    return true
  elseif s == "pointer" or s == "string_blob" or s == "bytes_blob" then
    local i = r_leb(r)
    if r.bad then report.malformed = true; return false end
    ctx.pending[#ctx.pending + 1] = { holder = v, key = fd.name, index = i, d = fd }
    if fd.optional then v[fd.name .. "_present"] = true end
    return true
  elseif s == "table" then
    local n = r_leb(r)
    if r.bad or r.at + n > r.lim then report.malformed = true; return false end
    local d = T.Tables[fd.target]
    local inst = fresh(T, d)
    local body = new_reader(r.b, r.at, r.at + n)
    read_body(T, ctx, d, body, inst, true)
    if body.at ~= body.lim then report.malformed = true; inst = fresh(T, d) end
    r.at = r.at + n
    v[fd.name] = inst
    if fd.optional then v[fd.name .. "_present"] = true end
    return true
  elseif s == "union" then
    local u = T.Unions[fd.target]
    if u == nil then report.malformed = true; return false end
    local x = read_union(T, ctx, u, r)
    if x == nil then return false end
    v[fd.name] = x
    if fd.optional then v[fd.name .. "_present"] = true end
    return true
  elseif s == "array" or s == "map" or s == "bytes" then
    local n = r_leb(r)
    if r.bad or r.at + n > r.lim then report.malformed = true; return false end
    local endpos = r.at + n
    if n >= 2 then
      local sub = new_reader(r.b, r.at, endpos)
      local out = read_array_body(T, ctx, fd, sub)
      if out ~= nil then v[fd.name] = out end
    end
    r.at = endpos
    if fd.optional then v[fd.name .. "_present"] = true end
    return true
  end
  return false
end

-- AN ARM HEADER is a field header (§3): the arm id reference, its kind, L,
-- then L bytes of arm payload. A retyped arm is an ordinary kind mismatch,
-- and damage inside L reads the union None and the parent reads on.
read_union = function(T, ctx, u, r)
  local report = ctx.report
  local armref = r_leb(r)
  if r.bad then report.malformed = true; return nil end
  if armref == 0 then return { tag = 0 } end
  if armref > #ctx.table then report.malformed = true; return nil end
  local armid = ctx.table[armref]
  if r.at >= r.lim then report.malformed = true; return nil end
  local armkind = r_u8(r)
  local n = r_leb(r)
  if r.bad or r.at + n > r.lim then report.malformed = true; return nil end
  local sub = new_reader(r.b, r.at, r.at + n)
  r.at = r.at + n
  local arm = nil
  for _, a in ipairs(u.arms) do if a.id == armid then arm = a; break end end
  if arm == nil then
    report.unknown = report.unknown + 1
    return { tag = 0 }
  end
  local f = arm.field
  local none = { tag = 0 }
  if f == nil then
    if armkind ~= 32 then report.kindMismatch = report.kindMismatch + 1; return none end
    if n ~= 0 then report.malformed = true; return none end
    return { tag = arm.tag }
  end
  if armkind ~= f.kind then
    if widens(armkind, f.kind) then
      local x = scalar_at(sub, f, armkind, report)
      if x == nil then return none end
      if sub.at ~= sub.lim then report.malformed = true; return none end
      report.widened = report.widened + 1
      local out = { tag = arm.tag }
      out[arm.name] = x
      return out
    end
    report.kindMismatch = report.kindMismatch + 1
    return none
  end
  local s = f.shape
  local out = { tag = arm.tag }
  if s == "bool" or s == "int" or s == "flags" or s == "float" then
    local w = width_of(armkind)
    if w == 0 or n ~= w then report.malformed = true; return none end
    local x = scalar_at(sub, f, armkind, report)
    if x == nil then return none end
    out[arm.name] = x
    return out
  elseif s == "string" then
    local raw = string.sub(sub.b, sub.at + 1, sub.lim)
    sub.at = sub.lim
    if not utf8_ok(raw) then report.malformed = true; return none end
    if f.bound ~= nil and f.bound > 0 and #raw > f.bound then
      report.clamped = report.clamped + 1
      raw = utf8_clamp(raw, f.bound)
    end
    out[arm.name] = raw
    return out
  elseif s == "wstring" then
    local raw = string.sub(sub.b, sub.at + 1, sub.lim)
    sub.at = sub.lim
    local ok, units = wstring_ok(raw)
    if not ok then report.malformed = true; return none end
    if f.bound ~= nil and f.bound > 0 and #units > f.bound then
      report.clamped = report.clamped + 1
      local keep = {}
      for i = 1, f.bound do keep[i] = units[i] end
      if f.bound > 0 and keep[f.bound] >= 0xd800 and keep[f.bound] <= 0xdbff then keep[f.bound] = nil end
      units = keep
    end
    out[arm.name] = units
    return out
  elseif s == "enum" then
    local x, ok = enum_read(ctx.table, sub, f, report)
    if not ok then report.malformed = true; return none end
    if sub.at ~= sub.lim then report.malformed = true; return none end
    out[arm.name] = x
    return out
  elseif s == "pointer" or s == "string_blob" or s == "bytes_blob" then
    local i = r_leb(sub)
    if sub.bad or sub.at ~= sub.lim then report.malformed = true; return none end
    ctx.pending[#ctx.pending + 1] = { holder = out, key = arm.name, index = i, d = f }
    return out
  elseif s == "union" then
    local inner = T.Unions[f.target]
    if inner == nil then report.malformed = true; return none end
    local x = read_union(T, ctx, inner, sub)
    if x == nil or sub.at ~= sub.lim then report.malformed = true; return none end
    out[arm.name] = x
    return out
  elseif s == "table" then
    local d = T.Tables[f.target]
    local inst = fresh(T, d)
    read_body(T, ctx, d, sub, inst, true)
    if sub.at ~= sub.lim then report.malformed = true; return none end
    out[arm.name] = inst
    return out
  elseif s == "array" or s == "map" or s == "bytes" then
    local x = read_array_body(T, ctx, f, sub)
    if x == nil then return none end
    out[arm.name] = x
    return out
  end
  return none
end

-- RESOLUTION (§3.1, decodenodes.go resolveCell): 0 is null, 1 is the root
-- (checked against the reader's own root type), k + 1 is record k.
local function resolve(ctx, root, rootdesc)
  local report = ctx.report
  local good = ctx.good
  for _, p in ipairs(ctx.pending) do
    local d = p.d
    local value = nil
    if good and p.index ~= 0 then
      if p.index == 1 then
        if d.shape == "pointer" and d.target == rootdesc.name then value = root
        else report.kindMismatch = report.kindMismatch + 1 end
      elseif p.index - 1 > #ctx.nodes then
        report.malformed = true
      else
        local node = ctx.nodes[p.index - 1]
        if node ~= nil then
          if d.shape == "string_blob" then
            if node.kind == "s" then value = node.blob else report.kindMismatch = report.kindMismatch + 1 end
          elseif d.shape == "bytes_blob" then
            if node.kind == "b" then value = node.blob else report.kindMismatch = report.kindMismatch + 1 end
          elseif node.kind == "t" and node.codec.name == d.target then
            value = node.value
          else
            report.kindMismatch = report.kindMismatch + 1
          end
        elseif d.shape == "pointer" then
          value = false
        end
      end
    end
    -- §3.1: 0 is null and null is elided, so a null slot resolves to nil —
    -- a pointer field of a table is absent. A LIST element keeps its false
    -- placeholder (§3.1's "A null slot is index 0 ... and it rides in its
    -- place"): read_element stored it, so an assignment of nil here must not
    -- drop the trailing null and the count.
    if value ~= nil then p.holder[p.key] = value end
  end
end

-- Load loads a form-1 wire for table `name` of module T into a fresh value,
-- fills report, and returns the value (or nil when refused).
function L.Load(T, name, bytes, report)
  local o = open(bytes, report)
  if o == nil then return nil end
  local desc = T.Tables[name]
  if desc == nil then report.malformed = true; return nil end
  local raw = node_scan(bytes, o, report)
  local ctx = { table = o.table, report = report, pending = {}, nodes = {}, good = raw ~= nil }
  if raw ~= nil then
    for _, rec in ipairs(raw) do
      if rec.tid == STRING_BLOB_ID then
        if utf8_ok(rec.body) then
          ctx.nodes[#ctx.nodes + 1] = { kind = "s", blob = rec.body }
        else
          report.malformed = true
          ctx.nodes[#ctx.nodes + 1] = nil
        end
      elseif rec.tid == BYTES_BLOB_ID then
        ctx.nodes[#ctx.nodes + 1] = { kind = "b", blob = rec.body }
      else
        local subdesc = nil
        for _, d in pairs(T.Tables) do if d.type_id == rec.tid then subdesc = d end end
        if subdesc == nil then
          report.unknown = report.unknown + 1
          ctx.nodes[#ctx.nodes + 1] = nil
        else
          local inst = fresh(T, subdesc)
          ctx.nodes[#ctx.nodes + 1] = { kind = "t", codec = subdesc, value = inst, body = rec.body }
        end
      end
    end
    for _, node in ipairs(ctx.nodes) do
      if node ~= nil and node.kind == "t" then
        read_body(T, ctx, node.codec, new_reader(node.body, 0, #node.body), node.value, true)
      end
    end
  end
  local root = fresh(T, desc)
  read_body(T, ctx, desc, new_reader(bytes, 1, o.lim), root, false)
  resolve(ctx, root, desc)
  return root
end

return L
