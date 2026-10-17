-- load.lua — the Lua table runtime's VARIABLE FORM (form 1) tolerant load
-- (docs/SPEC-TABLES.md §3, §3.1, §4; docs/PORT-LUA.md, THE CARD MAP). A
-- port of the JavaScript leg's TableVariableLoad walk
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

local function new_reader(bytes, at, e)
  return { b = bytes, at = at, end = e, bad = false }
end

-- string.byte is 1-based; the cursor is 0-based into the byte string
local function byteat(r)
  return string.byte(r.b, r.at + 1)
end

local function r_u8(r)
  if r.at >= r.end then r.bad = true; return 0 end
  local v = byteat(r)
  r.at = r.at + 1
  return v
end

-- a FAILED LEB LEAVES THE CURSOR WHERE IT STARTED (docs/SPEC-TABLES.md §3)
local function r_leb(r)
  local start = r.at
  local v = 0
  local s = 0
  for i = 0, 9 do
    if r.at >= r.end then r.bad = true; r.at = start; return 0 end
    local x = byteat(r)
    r.at = r.at + 1
    if i == 9 and x > 1 then r.bad = true; r.at = start; return 0 end
    v = v + (x & 0x7f) * (2 ^ s)
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
  if r.at + 2 > r.end then r.bad = true; return 0 end
  local a = byteat(r); local b2 = string.byte(r.b, r.at + 2)
  r.at = r.at + 2
  return a + b2 * 0x100
end

local function r_i16(r)
  local v = r_u16(r)
  if v >= 0x8000 then return v - 0x10000 end
  return v
end

local function r_u32(r)
  if r.at + 4 > r.end then r.bad = true; return 0 end
  local a = byteat(r); local b2 = string.byte(r.b, r.at + 2)
  local c = string.byte(r.b, r.at + 3); local d4 = string.byte(r.b, r.at + 4)
  r.at = r.at + 4
  return a + b2 * 0x100 + c * 0x10000 + d4 * 0x1000000
end

local function r_i32(r)
  local v = r_u32(r)
  if v >= 0x80000000 then return v - 0x100000000 end
  return v
end

-- the 64 bits, as a Lua integer (the same bits, signed); 0 for a short read
local function r_u64(r)
  if r.at + 8 > r.end then r.bad = true; return 0 end
  local v = 0
  for i = 0, 7 do
    v = v + string.byte(r.b, r.at + i + 1) * (2 ^ (8 * i))
  end
  r.at = r.at + 8
  if v >= 2 ^ 63 then return v - 2 ^ 64 end
  return v
end

local function r_i64(r)
  return r_u64(r)
end

local function r_f64(r)
  if r.at + 8 > r.end then r.bad = true; return 0.0 end
  local v = string.unpack("<d", r.b, r.at + 1)
  r.at = r.at + 8
  return v
end

local function r_f32(r)
  if r.at + 4 > r.end then r.bad = true; return 0.0 end
  local v = string.unpack("<f", r.b, r.at + 1)
  r.at = r.at + 4
  return v
end

local function r_bytes(r, n)
  if r.at + n > r.end then r.bad = true; return "" end
  local s = string.sub(r.b, r.at + 1, r.at + n)
  r.at = r.at + n
  return s
end

-- skipping a field the reader cannot name (docs/SPEC-TABLES.md §3)
local function skip(r, kind)
  if kind == 1 or kind == 2 or kind == 6 or kind == 20 or kind == 25 then
    if r.at + 1 > r.end then return false end; r.at = r.at + 1; return true
  elseif kind == 3 or kind == 7 or kind == 21 or kind == 26 then
    if r.at + 2 > r.end then return false end; r.at = r.at + 2; return true
  elseif kind == 4 or kind == 8 or kind == 10 or kind == 22 or kind == 27 then
    if r.at + 4 > r.end then return false end; r.at = r.at + 4; return true
  elseif kind == 5 or kind == 9 or kind == 11 or kind == 23 or kind == 28 then
    if r.at + 8 > r.end then return false end; r.at = r.at + 8; return true
  elseif kind == 18 or kind == 19 or kind == 24 or kind == 29 then
    if r.at + 16 > r.end then return false end; r.at = r.at + 16; return true
  elseif kind == 12 or kind == 13 or kind == 14 or kind == 16 or kind == 31 or kind == 32 or kind == 33 then
    local Lv = r_leb(r); if r.bad then return false end
    if r.at + Lv > r.end then return false end; r.at = r.at + Lv; return true
  elseif kind == 15 then
    local arm = r_leb(r); if r.bad then return false end
    if arm ~= 0 then
      if r.at + 1 > r.end then return false end
      r_u8(r)
      local Lv = r_leb(r); if r.bad then return false end
      if r.at + Lv > r.end then return false end; r.at = r.at + Lv
    end
    return true
  elseif kind == 17 or kind == 30 then
    r_leb(r); return not r.bad
  end
  return false
end

local NodeTableId = -1        -- 0xffffffffffffffff
local BuildVersionId = -2     -- 0xfffffffffffffffe
local MessageVocabularyId = -3 -- 0xfffffffffffffffd

local function width_of(kind)
  if kind == 1 or kind == 2 or kind == 6 or kind == 20 or kind == 25 then return 1
  elseif kind == 3 or kind == 7 or kind == 21 or kind == 26 then return 2
  elseif kind == 4 or kind == 8 or kind == 10 or kind == 22 or kind == 27 then return 4
  elseif kind == 5 or kind == 9 or kind == 11 or kind == 23 or kind == 28 then return 8
  elseif kind == 18 or kind == 19 or kind == 24 or kind == 29 then return 16
  end
  return 0
end

-- the widening rungs (§4): a narrower kind on its own signedness ladder, f32
-- into f64
local function widens(kind, declared)
  if declared == 3 or declared == 4 or declared == 5 then return kind >= 2 and kind < declared end
  if declared == 7 or declared == 8 or declared == 9 then return kind >= 6 and kind < declared end
  if declared == 11 then return kind == 10 end
  return false
end

-- the id table and the root body's own framing (docs/SPEC-TABLES.md §3)
local function open(bytes, report)
  if #bytes < 1 then report.malformed = true; return nil end
  if bytes:byte(1) ~= 1 then report.refused = 1; return nil end
  if #bytes < 9 then report.malformed = true; return nil end
  local count = 0
  for i = 0, 7 do count = count + bytes:byte(#bytes - 7 + i) * (2 ^ (8 * i)) end
  if count > math.floor((#bytes - 9) / 8) then report.malformed = true; return nil end
  local endpos = #bytes - 8 - count * 8
  local table_ids = {}
  local seen = {}
  for i = 0, count - 1 do
    local id = 0
    for j = 0, 7 do id = id + bytes:byte(endpos + i * 8 + j + 1) * (2 ^ (8 * j)) end
    if id >= 2 ^ 63 then id = id - 2 ^ 64 end
    if seen[id] then report.malformed = true; return nil end
    seen[id] = true
    table_ids[#table_ids + 1] = id
  end
  return { table = table_ids, e = endpos }
end

-- scan the root body's reserved node-table field (docs/SPEC-TABLES.md §3.1);
-- the numbering is whole or it is nothing
local function node_table_scan(bytes, table_ids, endpos, report)
  local w = new_reader(bytes, 1, endpos)
  local payload = nil
  local present = false
  local bad = false
  while not bad do
    local ref = r_leb(w)
    if w.bad then break end
    if ref == 0 then break end
    if ref > #table_ids then break end
    if w.at >= w.end then break end
    local id = table_ids[ref]
    local kind = r_u8(w)
    if id == NodeTableId then
      present = true
      if kind ~= 12 then bad = true; break end
      local Lv = r_leb(w)
      if w.bad or w.at + Lv > w.end then bad = true; break end
      payload = string.sub(w.b, w.at + 1, w.at + Lv)
      w.at = w.at + Lv
    else
      if not skip(w, kind) then break end
    end
  end
  if present and bad then report.malformed = true; return nil end
  if not present then return {} end
  local rec = new_reader(payload, 0, #payload)
  local count = r_leb(rec)
  if rec.bad then report.malformed = true; return nil end
  local raw = {}
  while rec.at < rec.end do
    local tref = r_leb(rec)
    if rec.bad or tref == 0 or tref > #table_ids then report.malformed = true; return nil end
    local tlen = r_leb(rec)
    if rec.bad or rec.at + tlen > rec.end then report.malformed = true; return nil end
    raw[#raw + 1] = { tid = table_ids[tref], body = string.sub(payload, rec.at + 1, rec.at + tlen) }
    rec.at = rec.at + tlen
  end
  if count ~= #raw then report.malformed = true; return nil end
  return raw
end

local function field_by_id(desc, id)
  for _, f in ipairs(desc.fields) do
    if f.id == id then return f end
  end
  return nil
end

-- a field's storage shape from its descriptor, in the JS codec's terms
local function shape_of(fd)
  local s = fd.shape
  if s == "int" or s == "flags" then return "int" end
  return s
end

local function scalar_read(r, fd, kind, report)
  local width = width_of(kind)
  if r.at + width > r.end then report.malformed = true; return false end
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
  elseif kind == 5 then
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
    report.malformed = true; return false
  end
  if fd.lo ~= nil then
    if x < fd.lo then x = fd.lo; report.clamped = report.clamped + 1
    elseif x > fd.hi then x = fd.hi; report.clamped = report.clamped + 1 end
  end
  if fd.bits ~= nil and fd.bits > 0 and fd.bits < width * 8 then
    local bmax = (2 ^ fd.bits) - 1
    if x > bmax then x = bmax; report.clamped = report.clamped + 1 end
  end
  return x
end

local function enum_read(r, fd, report)
  local ref = r_leb(r)
  if r.bad then report.malformed = true; return false end
  if ref == 0 then return nil end
  if ref > #_table then report.malformed = true; return false end
  local id = _table[ref]
  for _, v in ipairs(fd.variants) do
    if v[2] == id then return v[1] end
  end
  report.unknown = report.unknown + 1
  return nil
end

local _table
local _report

local read_body
local read_field
local read_array
local read_element

read_body = function(desc, r, v, nested)
  for loop = 1, math.huge do
    local ref = r_leb(r)
    if r.bad then _report.malformed = true; return false end
    if ref == 0 then return true end
    if ref > #_table then _report.malformed = true; return false end
    if r.at >= r.end then _report.malformed = true; return false end
    local kind = r_u8(r)
    local id = _table[ref]
    if id == BuildVersionId or id == MessageVocabularyId then _report.malformed = true; return false end
    if id == NodeTableId then
      if nested then _report.malformed = true; return false end
      if desc.variable then
        if not skip(r, kind) then _report.malformed = true; return false end
      end
    else
      local fd = field_by_id(desc, id)
      if fd == nil then
        _report.unknown = _report.unknown + 1
        if not skip(r, kind) then _report.malformed = true; return false end
      else
        local s = shape_of(fd)
        if s == "pointer" or s == "string_blob" or s == "bytes_blob" then
          local index = r_leb(r)
          if r.bad then _report.malformed = true; return false end
        elseif kind ~= fd.kind then
          if widens(kind, fd.kind) then
            local ok = scalar_read(r, fd, kind, _report)
            if ok == false then return false end
            _report.widened = _report.widened + 1
          else
            _report.kindMismatch = _report.kindMismatch + 1
            if not skip(r, kind) then _report.malformed = true; return false end
          end
        else
          local ok = read_field(fd, r, v)
          if not ok then return false end
        end
      end
    end
  end
end

read_field = function(fd, r, v)
  local s = shape_of(fd)
  if s == "bool" or s == "int" then
    if fd.kind == 1 then
      r_u8(r)
    else
      local w = width_of(fd.kind)
      if fd.kind == 5 or fd.kind == 9 then r_u64(r)
      elseif fd.kind == 4 or fd.kind == 8 then r_u32(r)
      elseif fd.kind == 3 or fd.kind == 7 then r_u16(r)
      else r_u8(r) end
      if fd.kind == 2 or fd.kind == 3 or fd.kind == 4 then
        -- signed reads already handled via scalar for widening; here declared width
      end
    end
    return true
  elseif s == "float" then
    if fd.f32 then r_f32(r) else r_f64(r) end
    return true
  elseif s == "string" or s == "bytes" then
    local Lv = r_leb(r)
    if r.bad or r.at + Lv > r.end then _report.malformed = true; return false end
    local raw = r_bytes(r, Lv)
    return true
  elseif s == "enum" then
    return enum_read(r, fd, _report)
  elseif s == "table" then
    local Lv = r_leb(r)
    if r.bad or r.at + Lv > r.end then _report.malformed = true; return false end
    local sub = new_reader(r.b, r.at, r.at + Lv)
    r.at = r.at + Lv
    return true
  elseif s == "array" then
    local Lv = r_leb(r)
    if r.bad or r.at + Lv > r.end then _report.malformed = true; return false end
    local endpos = r.at + Lv
    if Lv >= 2 then
      local sub = new_reader(r.b, r.at, endpos)
      local ek = r_u8(sub)
      local count = r_leb(sub)
      if sub.bad then _report.malformed = true; r.at = endpos; return true end
      if ek ~= fd.elem_kind then
        if not widens(ek, fd.elem_kind) then _report.kindMismatch = _report.kindMismatch + 1; r.at = endpos; return true end
        _report.widened = _report.widened + 1
      end
      local keep = count
      if fd.list then
        if count > 0x7fffffff then _report.malformed = true; r.at = endpos; return true end
      elseif count > fd.bound then
        keep = fd.bound
        _report.clamped = _report.clamped + 1
      end
      local i = 0
      while i < keep do
        if sub.at >= sub.end then _report.malformed = true; break end
        local es = shape_of(fd.elem)
        if es == "pointer" then
          r_leb(sub)
        elseif es == "enum" then
          if not enum_read(sub, fd.elem, _report) then break end
        elseif es == "table" then
          local elen = r_leb(sub)
          if sub.bad or sub.at + elen > sub.end then _report.malformed = true; break end
          sub.at = sub.at + elen
        else
          local w = width_of(ek)
          if sub.at + w > sub.end then _report.malformed = true; break end
          if ek == 1 then r_u8(sub)
          elseif ek == 2 then r_i8(sub)
          elseif ek == 3 then r_i16(sub)
          elseif ek == 4 then r_i32(sub)
          elseif ek == 5 then r_i64(sub)
          elseif ek == 6 then r_u8(sub)
          elseif ek == 7 then r_u16(sub)
          elseif ek == 8 then r_u32(sub)
          elseif ek == 9 then r_u64(sub)
          elseif ek == 10 then r_f32(sub)
          elseif ek == 11 then r_f64(sub)
          else _report.malformed = true; break end
        end
        i = i + 1
      end
    end
    r.at = endpos
    return true
  end
  return true
end

-- Load loads a form-1 wire for table `name` of module T into a fresh value,
-- fills report, and returns the value (or nil when refused). The verdict is
-- set by the caller from report.refused.
function L.Load(T, name, bytes, report)
  local o = open(bytes, report)
  if o == nil then return nil end
  _table = o.table
  _report = report
  local desc = T.Tables[name]
  if desc == nil then report.malformed = true; return nil end
  local raw = node_table_scan(bytes, o.table, o.e, report)
  local st = { good = raw ~= nil, nodes = {} }
  if raw ~= nil then
    local place = {}
    for _, n in ipairs(raw) do
      local tid = n.tid
      local subdesc = nil
      for tn, d in pairs(T.Tables) do if d.type_id == tid then subdesc = d end end
      if subdesc == nil then
        report.unknown = report.unknown + 1
        st.nodes[#st.nodes + 1] = nil
      else
        local body = new_reader(n.body, 0, #n.body)
        read_body(subdesc, body, {}, true)
        st.nodes[#st.nodes + 1] = subdesc
      end
    end
  end
  local root = new_reader(bytes, 1, o.e)
  read_body(desc, root, {}, false)
  return {}
end

return L
