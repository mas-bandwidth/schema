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
local D = require("descriptor")

local L = {}

local NODE_TABLE_ID = -1 -- 0xffffffffffffffff
local BUILD_VERSION_ID = -2 -- 0xfffffffffffffffe
local MESSAGE_VOCABULARY_ID = -3 -- 0xfffffffffffffffd
local STRING_BLOB_ID = 0x704be0d8faaffc58
local BYTES_BLOB_ID = 0x2f2ec0474f1c4fe4

-- CRC-32C, the Castagnoli polynomial reflected (0x82F63B78) with the initial
-- value and final XOR §2.14 names. ONE TABLE, built once when the file is
-- required (a constant: docs/PORTING.md M13 bans the read-a-static, build-on-nil,
-- store-back cache), is the check save.lua seals and load.lua and refusal.lua
-- verify (docs/SPEC-TABLES.md §2.14, §3.4).
local crc_table = {}
for i = 0, 255 do
  local c = i
  for _ = 1, 8 do
    if c & 1 == 1 then
      c = (c >> 1) ~ 0x82f63b78
    else
      c = c >> 1
    end
  end
  crc_table[i] = c
end

function L.crc32c(bytes, n)
  local crc = 0xffffffff
  for i = 1, n or #bytes do
    crc = crc_table[(crc ~ bytes:byte(i)) & 0xff] ~ (crc >> 8)
  end
  return crc ~ 0xffffffff
end

-- THE READER (docs/PORT-LUA.md, BYTE BUFFERS): a Lua string of immutable
-- bytes and a 0-based cursor with a limit for a nested body.
local function new_reader(b, at, lim)
  return { b = b, at = at, lim = lim, bad = false }
end

-- a reader the caller already owns, pointed at another range: no table is
-- built (docs/PORTING.md M1)
local function point(r, b, at, lim)
  r.b, r.at, r.lim, r.bad = b, at, lim, false
  return r
end

-- A NESTED BODY IS BOUNDED BY NARROWING THE READER'S LIMIT, never by slicing a
-- new view (docs/PORTING.md M1: a sub-view is an allocation and a limit is an
-- integer). narrow(r, n) bounds r to the n bytes at its cursor and answers what
-- restore(r, at, n, lim, bad) puts back: the limit, the sticky bad flag a
-- nested read may have set (it belongs to that body alone), and the cursor
-- past the body's L, wherever the body stopped.
local function narrow(r, n)
  local lim, bad = r.lim, r.bad
  r.lim = r.at + n
  r.bad = false
  return lim, bad
end

local function restore(r, at, n, lim, bad)
  r.lim = lim
  r.bad = bad
  r.at = at + n
end

local function byteat(r)
  return string.byte(r.b, r.at + 1)
end

local function r_u8(r)
  if r.at >= r.lim then
    r.bad = true
    return 0
  end
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
    if r.at >= r.lim then
      r.bad = true
      r.at = start
      return 0
    end
    local x = byteat(r)
    r.at = r.at + 1
    if i == 9 and x > 1 then
      r.bad = true
      r.at = start
      return 0
    end
    v = v | ((x & 0x7f) << s)
    if (x & 0x80) == 0 then
      if i > 0 and x == 0 then
        r.bad = true
        r.at = start
        return 0
      end
      return v
    end
    s = s + 7
  end
  r.bad = true
  r.at = start
  return 0
end

-- A LEB128 VALUE IS AN UNSIGNED 64-BIT NUMBER (docs/SPEC-TABLES.md §3;
-- decode.go reads it as a uint64): one at or past 2^63 is a negative Lua
-- integer, so a reference, a length or a count is bounded UNSIGNED, never by a
-- signed `>`. above(v, n) is v > n unsigned; fits(r, n) is "n more bytes lie
-- inside this body".
local function above(v, n)
  return math.ult(n, v)
end

local function fits(r, n)
  return r.at <= r.lim and not math.ult(r.lim - r.at, n)
end

local function r_i8(r)
  local v = r_u8(r)
  if v >= 0x80 then return v - 0x100 end
  return v
end

local function r_u16(r)
  if r.at + 2 > r.lim then
    r.bad = true
    return 0
  end
  local v = string.unpack("<I2", r.b, r.at + 1)
  r.at = r.at + 2
  return v
end

local function r_i16(r)
  if r.at + 2 > r.lim then
    r.bad = true
    return 0
  end
  local v = string.unpack("<i2", r.b, r.at + 1)
  r.at = r.at + 2
  return v
end

local function r_u32(r)
  if r.at + 4 > r.lim then
    r.bad = true
    return 0
  end
  local v = string.unpack("<I4", r.b, r.at + 1)
  r.at = r.at + 4
  return v
end

local function r_i32(r)
  if r.at + 4 > r.lim then
    r.bad = true
    return 0
  end
  local v = string.unpack("<i4", r.b, r.at + 1)
  r.at = r.at + 4
  return v
end

-- 64 bits kept as the Lua integer with the same bits (docs/PORT-LUA.md,
-- 64-BIT AND 128-BIT INTEGERS): an unsigned read is the same signed bits.
local function r_u64(r)
  if r.at + 8 > r.lim then
    r.bad = true
    return 0
  end
  local v = string.unpack("<i8", r.b, r.at + 1)
  r.at = r.at + 8
  return v
end

local function r_i64(r)
  return r_u64(r)
end

local function r_f64(r)
  if r.at + 8 > r.lim then
    r.bad = true
    return 0.0
  end
  local v = string.unpack("<d", r.b, r.at + 1)
  r.at = r.at + 8
  return v
end

local function r_f32(r)
  if r.at + 4 > r.lim then
    r.bad = true
    return 0.0
  end
  local v = string.unpack("<f", r.b, r.at + 1)
  r.at = r.at + 4
  return v
end

local function r_bytes(r, n)
  if r.at + n > r.lim then
    r.bad = true
    return ""
  end
  local s = string.sub(r.b, r.at + 1, r.at + n)
  r.at = r.at + n
  return s
end

-- THE FIXED WIDTHS of §3's kind table; 0 is a length- or reference-shaped
-- kind (or one a skipper reads by its L).
local function width_of(kind)
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
  if kind == 12 or kind == 13 or kind == 14 or kind == 16 or kind == 31 or kind == 32 then
    local n = r_leb(r)
    if r.bad or n < 0 or r.at + n > r.lim then return false end
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
      if r.bad or n < 0 or r.at + n > r.lim then return false end
      r.at = r.at + n
    end
    return true
  end
  return false
end

-- the form byte, the trailer and the id table, and the root body's own
-- framing (docs/SPEC-TABLES.md §3): nil when the wire is refused or damaged.
-- The id table and its seen-set are the loader state's own (st.ids, st.seen,
-- emptied here before they are filled), and so is the answer.
local function open(bytes, blen, report, st)
  if blen < 1 then
    report.malformed = true
    report.wireDamaged = true
    return nil
  end
  if bytes:byte(1) ~= 1 then
    report.refused = 1
    return nil
  end
  if blen < 9 then
    report.malformed = true
    report.wireDamaged = true
    return nil
  end
  local count = string.unpack("<i8", bytes, blen - 7)
  if count < 0 or count > (blen - 9) // 8 or count > D.ID_TABLE_BOUND then
    report.malformed = true
    report.wireDamaged = true
    return nil
  end
  local endpos = blen - 8 - count * 8
  local table_ids, seen = st.ids, st.seen
  for i = 1, #table_ids do
    seen[table_ids[i]] = nil
    table_ids[i] = nil
  end
  for i = 0, count - 1 do
    local id = string.unpack("<i8", bytes, endpos + i * 8 + 1)
    if seen[id] then
      report.malformed = true
      report.wireDamaged = true
      return nil
    end
    seen[id] = true
    table_ids[i + 1] = id
  end
  -- roots end at their own zero reference; bytes after it are damage (§3)
  local w = point(st.scan, bytes, 1, endpos)
  while true do
    local ref = r_leb(w)
    if w.bad then break end
    if ref == 0 then
      if w.at ~= w.lim then
        report.malformed = true
        report.wireDamaged = true
        return nil
      end
      break
    end
    if above(ref, #table_ids) then break end
    if w.at >= w.lim then break end
    local kind = r_u8(w)
    if not skip(w, kind) then break end
  end
  local o = st.o
  o.table, o.lim = table_ids, endpos
  return o
end

-- THE NODE RECORDS (§3.1): the numbering is positional and whole or nothing.
-- The scan is authoritative: the count must match the records it frames. A node
-- table rides only in a VARIABLE root's body; in a FIXED root the reserved id is
-- an ordinary field this table cannot name, skipped and counted unknown (§3.1,
-- §4), never a numbering to parse — the Go oracle's body reader treats it the
-- same (§3). Each record is its type id and the range of its body in the wire
-- itself (at, lim), never a copy of it. A wire with no node table answers NO_NODES,
-- the one empty list, which nothing writes to.
local NO_NODES = {}

local function node_scan(bytes, o, report, variable, st)
  local w = point(st.scan, bytes, 1, o.lim)
  local at, lim = nil, nil
  local present = false
  local bad = false
  while not bad do
    local ref = r_leb(w)
    if w.bad then break end
    if ref == 0 then break end
    if ref < 0 or ref > #o.table then break end
    if w.at >= w.lim then break end
    local id = o.table[ref]
    local kind = r_u8(w)
    if id == NODE_TABLE_ID and variable then
      present = true
      if kind ~= 12 then
        bad = true
        break
      end
      local n = r_leb(w)
      if w.bad or n < 0 or w.at + n > w.lim then
        bad = true
        break
      end
      at, lim = w.at, w.at + n
      w.at = w.at + n
    elseif not skip(w, kind) then
      break
    end
  end
  if present and bad then
    report.malformed = true
    report.wireDamaged = true
    return nil
  end
  if not present then return NO_NODES end
  local rec = point(st.scan, bytes, at, lim)
  local count = r_leb(rec)
  if rec.bad then
    report.malformed = true
    report.wireDamaged = true
    return nil
  end
  local raw = {}
  while rec.at < rec.lim do
    local tref = r_leb(rec)
    if rec.bad or tref == 0 or above(tref, #o.table) then
      report.malformed = true
      report.wireDamaged = true
      return nil
    end
    local n = r_leb(rec)
    if rec.bad or n < 0 or rec.at + n > rec.lim then
      report.malformed = true
      report.wireDamaged = true
      return nil
    end
    raw[#raw + 1] = { tid = o.table[tref], at = rec.at, lim = rec.at + n }
    rec.at = rec.at + n
  end
  if count ~= #raw then
    report.malformed = true
    report.wireDamaged = true
    return nil
  end
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
  while cut > 0 and (s:byte(cut + 1) & 0xc0) == 0x80 do
    cut = cut - 1
  end
  return s:sub(1, cut)
end

-- THE WIDENING RUNGS (§4): a narrower kind on its own signedness ladder, and
-- f32 into f64.
local function widens(kind, declared)
  if declared == 3 or declared == 4 or declared == 5 then return kind >= 2 and kind < declared end
  if declared == 7 or declared == 8 or declared == 9 then return kind >= 6 and kind < declared end
  -- the 128-bit rungs (ir.TableKindWidens): int128 takes kinds 2..5, uint128
  -- takes kinds 6..9, and scalar_at extends the narrower value into the pair
  if declared == 18 then return kind >= 2 and kind <= 5 end
  if declared == 19 then return kind >= 6 and kind <= 9 end
  if declared == 11 then return kind == 10 end
  return false
end

-- a bound the value is clamped to is the VALUE it becomes: a wide bound is the
-- descriptor's own { lo, hi } table, which a value must never alias (a load that
-- reads into the caller's value puts it back in place, and a descriptor is a
-- constant, docs/PORTING.md M13), so it is copied
local function bound_value(b)
  if type(b) == "table" then return { lo = b.lo, hi = b.hi } end
  return b
end

local function less_than(d, a, b)
  if a == b then return false end
  if type(a) == "table" or type(b) == "table" then
    -- a 128-bit value is two halves (docs/PORT-LUA.md, 64-BIT AND 128-BIT
    -- INTEGERS): signed compares the high half signed, then the low half by
    -- its unsigned bits; unsigned compares both by math.ult.
    if d.signed then
      if a.hi ~= b.hi then return a.hi < b.hi end
      return math.ult(a.lo, b.lo)
    end
    if a.hi ~= b.hi then return math.ult(a.hi, b.hi) end
    return math.ult(a.lo, b.lo)
  end
  if d.signed then return a < b end
  return math.ult(a, b)
end

-- A NaN OR AN INFINITY IS REFUSED ON ITS RAW BITS (docs/SPEC-TABLES.md §3.3):
-- the test runs on the wire's bits before they become a Lua number and before the
-- field is written. The read names the float <Owner>.<field> (the first name
-- wins) and the load, the tolerant one included, refuses as non_finite_float
-- and lands nothing. ctx.owner and ctx.field are the body and the field the
-- walk is in; an element is named by its array field.
local F32_EXP = 0x7f800000
local F64_EXP = 0x7ff0000000000000

local function non_finite(report, ctx, d)
  if report.nonFiniteFloat ~= nil then return end
  local owner, field = "?", d.name
  if ctx ~= nil then
    owner = ctx.owner or owner
    if ctx.field ~= nil then field = ctx.field end
  end
  report.nonFiniteFloat = owner .. "." .. tostring(field)
end

-- a scalar decoded at the WIRE kind (the declared kind or one it widens),
-- sign-extended where the kind is signed, then the declared range and the
-- bits(N) clamp (§4, decode.go scalarAt).
local function scalar_at(r, d, kind, report, ctx)
  local w = width_of(kind)
  if w == 0 or r.at + w > r.lim then
    report.malformed = true
    return nil
  end
  local x
  if kind == 1 then
    -- A BOOL BYTE PAST 1 IS MALFORMED, and non-zero is still true
    -- (docs/SPEC-TABLES.md §3; decode.go scalarAt).
    local b = r_u8(r)
    if b > 1 then report.malformed = true end
    x = b ~= 0
  elseif kind == 10 then
    local b = string.unpack("<I4", r.b, r.at + 1)
    if b & F32_EXP == F32_EXP then
      r.at = r.at + 4
      non_finite(report, ctx, d)
      return nil
    end
    x = r_f32(r)
  elseif kind == 11 then
    local b = string.unpack("<i8", r.b, r.at + 1)
    if b & F64_EXP == F64_EXP then
      r.at = r.at + 8
      non_finite(report, ctx, d)
      return nil
    end
    x = r_f64(r)
  elseif kind == 2 or kind == 20 then
    x = r_i8(r)
  elseif kind == 3 or kind == 21 then
    x = r_i16(r)
  elseif kind == 4 or kind == 22 then
    x = r_i32(r)
  elseif kind == 5 or kind == 23 or kind == 36 then
    x = r_i64(r)
  elseif kind == 6 or kind == 25 then
    x = r_u8(r)
  elseif kind == 7 or kind == 26 then
    x = r_u16(r)
  elseif kind == 8 or kind == 27 then
    x = r_u32(r)
  elseif kind == 9 or kind == 28 then
    x = r_u64(r)
  elseif kind == 18 or kind == 19 or kind == 24 or kind == 29 then
    -- int128/uint128 and fixed128/ufixed128: sixteen bytes, low 64-bit half
    -- first, two's complement for signed kinds 18 and 24 (docs/SPEC-TABLES.md
    -- §3 kinds 18/19 and 20-29); storage is the two halves (docs/PORT-LUA.md,
    -- 64-BIT AND 128-BIT INTEGERS, FIXED-POINT).
    x = { lo = r_u64(r), hi = r_u64(r) }
  else
    report.malformed = true
    return nil
  end
  if r.bad then
    report.malformed = true
    return nil
  end
  if d.shape == "wide" and type(x) ~= "table" then
    -- a narrower signed kind widens by SIGN-EXTENDING into the high half; an
    -- unsigned one zero-extends (docs/SPEC-TABLES.md §4; gotable wide.go).
    if d.signed and x < 0 then
      x = { lo = x, hi = -1 }
    else
      x = { lo = x, hi = 0 }
    end
  end
  if d.lo ~= nil and d.hi ~= nil then
    if less_than(d, x, d.lo) then
      x = bound_value(d.lo)
      report.clamped = report.clamped + 1
    elseif less_than(d, d.hi, x) then
      x = bound_value(d.hi)
      report.clamped = report.clamped + 1
    end
  end
  -- THE bits(N) CLAMP is a bits(N) field's alone (decode.go scalarAt: the
  -- declared kind is TBits): a flags field's descriptor carries its wire bits
  -- for the JSON writer, and an undeclared flag bit on the wire lands as it
  -- rode, uncounted (§4).
  if d.shape == "int" and d.bits ~= nil and d.bits > 0 and d.bits < w * 8 then
    local bmax = (1 << d.bits) - 1
    if kind ~= 1 and less_than({ signed = false }, bmax, x) then
      x = bmax
      report.clamped = report.clamped + 1
    end
  end
  return x
end

-- an enum reference (§3): 0 is None, a reference past the id table is damage,
-- and a variant this enum cannot name reads None and counts unknown. A
-- variant retired in place (docs/SPEC-TABLES.md §2.15, READ) reads the
-- field's declared default — an element's is None — and is not unknown.
local function enum_read(atable, r, d, report)
  local ref = r_leb(r)
  if r.bad then
    report.malformed = true
    return nil, false
  end
  if ref == 0 then return nil, true end
  if above(ref, #atable) then
    report.malformed = true
    return nil, false
  end
  local id = atable[ref]
  for _, v in ipairs(d.variants) do
    if v[2] == id then
      if v.deprecated then return d.default, true end
      return v[1], true
    end
  end
  report.unknown = report.unknown + 1
  return nil, true
end

local read_body
local read_field
local read_array_body
local read_element
local read_union

local function fresh(T, desc)
  return D.fresh(T, desc)
end

-- the table a nested value is read into: the one the holder already has
-- (put back to the declared defaults in place) when the field is read for the
-- first time, a fresh one otherwise (docs/PORTING.md M1)
local function renew(T, desc, existing)
  if type(existing) == "table" then return D.reset(T, desc, existing) end
  return D.fresh(T, desc)
end

-- THE PENDING POINTER SLOTS (§3.1): one entry per slot a load queues for
-- resolution, kept in the loader state and reused, so queueing a slot builds
-- nothing once the state has run a load of its size. `null` is what the slot
-- holds when its index resolves to nothing: nil for a field or an arm (absent),
-- false for an array element, a keyed slot or a map value (a sequence and a
-- key cannot hold nil). Resolution runs in queue order and every entry writes
-- its slot, so a slot the wire names twice takes the LAST occurrence's answer,
-- null included (§3: a repeated id is last-wins; decodenodes.go resolveCell
-- clears the cell before it resolves).
local function queue(ctx, holder, key, index, d, null)
  local n = ctx.np + 1
  ctx.np = n
  local pending = ctx.pending
  local p = pending[n]
  if p == nil then
    p = {}
    pending[n] = p
  end
  p.holder, p.key, p.index, p.d, p.null = holder, key, index, d, null
end

-- a repeat of a FIXED array that builds a fresh array from the one an
-- earlier occurrence left keeps that occurrence's unreplaced slots (decode.go
-- arrayBody leaves fv.Elems standing). A pointer slot there is still queued
-- against the earlier array, so its entries are pointed at the new one, where
-- they resolve before the repeat's own entries for the same slots.
local function adopt(ctx, from, to)
  local pending = ctx.pending
  for i = 1, ctx.np do
    local p = pending[i]
    if p.holder == from then p.holder = to end
  end
end

-- takes slot j out of the queue; its entry is kept at the end for reuse
local function unqueue(ctx, j)
  local pending, n = ctx.pending, ctx.np
  local p = pending[j]
  for k = j, n - 1 do
    pending[k] = pending[k + 1]
  end
  pending[n] = p
  ctx.np = n - 1
end

-- a map entry's key at its declared default: the descriptor's own, except a
-- wide one, which is a table the constructor builds fresh
local function key_default(T, entry, kf)
  if kf.shape == "wide" then return D.fresh(T, entry)[kf.name] end
  return kf.default
end

-- a body that is read again from its defaults (damage inside its own L reads
-- the defaults) must not take the pointer slots the damaged read queued: they
-- are pointed at the state's sink, a table nothing reads, and still resolve
-- and count as they always did
local function discard(ctx, from)
  local pending, sink = ctx.pending, ctx.sink
  for i = from + 1, ctx.np do
    pending[i].holder = sink
  end
end

local function zero_elem(T, e)
  local s = e.shape
  if s == "bool" then return false end
  if s == "int" or s == "flags" then return 0 end
  if s == "wide" then return { lo = 0, hi = 0 } end
  if s == "float" then return 0.0 end
  if s == "string" or s == "bytes" then return "" end
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

-- an array's zero (§2.9): a fixed array holds its bound of zero elements,
-- every other array and a byte string are empty
local function zero_array(T, f)
  if f.shape == "bytes" then return "" end
  local out = {}
  if f.fixed then
    for i = 1, f.bound do
      out[i] = zero_elem(T, f.elem)
    end
  end
  return out
end

-- one element of an array body (docs/SPEC-TABLES.md §2.9, §3)
read_element = function(T, ctx, e, ek, sub, out, index, reuse)
  local s = e.shape
  if s == "pointer" or s == "string_blob" or s == "bytes_blob" then
    local i = r_leb(sub)
    if sub.bad then
      ctx.report.malformed = true
      return false, false
    end
    queue(ctx, out, index, i, e, false)
    return false, true
  elseif s == "enum" then
    local x, ok = enum_read(ctx.table, sub, e, ctx.report)
    if not ok then return false, false end
    -- None is false in a sequence (a sequence cannot hold nil, §2.9);
    -- `(x == nil) and false or x` is x itself, so it is spelled out
    if x == nil then return false, true end
    return x, true
  elseif s == "table" then
    -- A PLAIN TEXT LIST (docs/SPEC-TABLES.md §2.13, §3) rides as an array of
    -- element kind 12: each element is its own L and bytes, stored as the
    -- element record's one string field. Every other table element (a ? string
    -- element record's included) is a body under kind 13 below.
    if ek == 12 then
      local d = T.Tables[e.target]
      local vf = d and d.fields[1]
      if vf == nil or vf.shape ~= "string" then
        ctx.report.malformed = true
        return false, false
      end
      local n = r_leb(sub)
      if sub.bad or not fits(sub, n) then
        ctx.report.malformed = true
        return false, false
      end
      local raw = r_bytes(sub, n)
      local inst = fresh(T, d)
      if not utf8_ok(raw) then
        ctx.report.malformed = true
        return inst, true
      end
      if vf.bound ~= nil and vf.bound > 0 and #raw > vf.bound then
        ctx.report.clamped = ctx.report.clamped + 1
        raw = utf8_clamp(raw, vf.bound)
      end
      inst[vf.name] = raw
      return inst, true
    end
    local n = r_leb(sub)
    if sub.bad or not fits(sub, n) then
      ctx.report.malformed = true
      return false, false
    end
    local d = T.Tables[e.target]
    -- a fixed array's slot is read into the record it holds, once, and a
    -- counted or unbounded element is built
    local inst = reuse and renew(T, d, out[index]) or fresh(T, d)
    local np0 = ctx.np
    local at0 = sub.at
    local lim, bad = narrow(sub, n)
    read_body(T, ctx, d, sub, inst, true)
    local clean = sub.at == sub.lim
    restore(sub, at0, n, lim, bad)
    if not clean then
      ctx.report.malformed = true
      discard(ctx, np0)
      inst = renew(T, d, inst)
    end
    return inst, true
  elseif s == "union" then
    local u = T.Unions[e.target]
    if u == nil then
      ctx.report.malformed = true
      return false, false
    end
    local x = read_union(T, ctx, u, sub)
    if x == nil then return false, false end
    return x, true
  elseif s == "string" then
    local n = r_leb(sub)
    if sub.bad or not fits(sub, n) then
      ctx.report.malformed = true
      return false
    end
    local raw = r_bytes(sub, n)
    if not utf8_ok(raw) then
      ctx.report.malformed = true
      return e.default or "", true
    end
    if e.bound ~= nil and e.bound > 0 and #raw > e.bound then
      ctx.report.clamped = ctx.report.clamped + 1
      return utf8_clamp(raw, e.bound), true
    end
    return raw, true
  end
  local x = scalar_at(sub, e, ek, ctx.report, ctx)
  if x == nil then return false, false end
  return x, true
end

-- THE KEY SCAN (docs/SPEC-TABLES.md §2.8; decode.go mapKey): before an
-- entry's body decodes, its field headers are scanned for the key's id and the
-- LAST occurrence is the key; a body with no key has the key's default. The
-- key kind is the reader's declaration: a key under another kind is a kind
-- mismatch, and a key the scan cannot read, or a terminator that does not end
-- the entry, is damage. Nothing the scan reads is counted.
local function map_key(ctx, kf, b, at, lim, key)
  local scan = point(ctx.scan, b, at, lim)
  local junk = ctx.junk
  junk.unknown, junk.kindMismatch, junk.widened, junk.clamped, junk.duplicate = 0, 0, 0, 0, 0
  junk.malformed, junk.nonFiniteFloat = false, nil
  local kindbad, widened = false, false
  while true do
    local ref = r_leb(scan)
    if scan.bad then return key, false, kindbad, widened end
    if ref == 0 then return key, scan.at == scan.lim, kindbad, widened end
    if above(ref, #ctx.table) or scan.at >= scan.lim then return key, false, kindbad, widened end
    local kind = r_u8(scan)
    if ctx.table[ref] == kf.id then
      if kind ~= kf.kind and widens(kind, kf.kind) then
        widened = true
        local x = scalar_at(scan, kf, kind, junk)
        if x == nil then return key, false, kindbad, widened end
        key = x
      else
        kindbad = kind ~= kf.kind
        if kindbad then
          if not skip(scan, kind) then return key, false, kindbad, widened end
        elseif kf.shape == "string" then
          local n = r_leb(scan)
          if scan.bad or not fits(scan, n) then return key, false, kindbad, widened end
          local raw = r_bytes(scan, n)
          if not utf8_ok(raw) then return key, false, kindbad, widened end
          key = raw
        else
          local x = scalar_at(scan, kf, kind, junk)
          if x == nil then return key, false, kindbad, widened end
          key = x
        end
      end
    elseif not skip(scan, kind) then
      return key, false, kindbad, widened
    end
  end
end

-- KEY ORDER (docs/SPEC-TABLES.md §2.8; tabletext.MapKeyOrder): text by its
-- bytes, a signed integer signed, every other key unsigned.
local function key_order(kf, a, b)
  if a == b then return 0 end
  if kf.shape == "string" then
    -- by unsigned bytes, a shorter prefix first: Lua's string < is strcoll,
    -- bytewise only under the C locale
    local n = math.min(#a, #b)
    for i = 1, n do
      local x, y = a:byte(i), b:byte(i)
      if x ~= y then
        if x < y then return -1 end
        return 1
      end
    end
    if #a < #b then return -1 end
    return 1
  end
  if kf.signed then
    if a < b then return -1 end
    return 1
  end
  if math.ult(a, b) then return -1 end
  return 1
end

-- AN ARRAY BODY (§2.9, §3, decode.go arrayBody): a body shorter than its own
-- header is inert; a damaged count is malformed and keeps the field; an
-- element kind that is not this array's (and does not widen) is a kind
-- mismatch and keeps the field; a bounded array's excess is clamped.
--
-- A BODY'S HEADER IS READ AGAINST THE PARENT (decode.go subTo): `hdr` is
-- positioned at the body and bounded by the reader that carries it, because a
-- canonical count is up to ten bytes and L may be shorter; a count spelled
-- past `endpos` leaves no element bytes. It returns the value, or nil to keep
-- the field's own, and false second when the element kind is not this
-- array's: the payload is not this array's, so an optional stays absent and an
-- arm holding it is not selected (§2.3, §3). `prior` is the field's value
-- before this occurrence: a FIXED array keeps the elements a damaged repeat
-- could not replace (decode.go arrayBody leaves fv.Elems standing), while a
-- counted or unbounded one is replaced whole.
local array_elements

read_array_body = function(T, ctx, fd, r, endpos, prior, reuse)
  local report = ctx.report
  -- the header is read through the reader that carries the body, bounded by its
  -- parent, and the elements through the same reader with its limit narrowed to
  -- the body's own end (docs/PORTING.md M1: a limit, never a new view)
  local lim, bad = r.lim, r.bad
  r.bad = false
  local ek = r_u8(r)
  local count = r_leb(r)
  if r.bad then
    r.lim, r.bad = lim, bad
    report.malformed = true
    return nil
  end
  r.lim = math.max(r.at, endpos)
  local out, selected = array_elements(T, ctx, fd, r, ek, count, prior, reuse)
  r.lim, r.bad = lim, bad
  return out, selected
end

-- the elements of an array body, read from the reader sub, bounded to the body
array_elements = function(T, ctx, fd, sub, ek, count, prior, reuse)
  local report = ctx.report

  if fd.shape == "bytes" then
    local expected = 6
    if ek ~= expected then
      if not widens(ek, expected) then
        report.kindMismatch = report.kindMismatch + 1
        return nil, false
      end
      report.widened = report.widened + 1
    end
    local keep = count
    if fd.bound ~= nil and above(count, fd.bound) then
      keep = fd.bound
      report.clamped = report.clamped + 1
    end
    -- `keep` bytes, or the bytes the body holds (a body that is short is
    -- malformed); a count at or past 2^63 is a negative integer and keeps none
    local take = keep
    if take < 0 then take = 0 end
    local avail = sub.lim - sub.at
    if take > avail then
      take = avail
      report.malformed = true
    end
    local out = string.sub(sub.b, sub.at + 1, sub.at + take)
    sub.at = sub.at + take
    return out
  end

  if fd.shape == "map" then
    -- A MAP (docs/SPEC-TABLES.md §2.8; decode.go mapField): an element kind
    -- that is not 13 is the array kind mismatch of §4 and the field keeps
    -- what it holds; then every entry's KEY is scanned before its body is
    -- read, ascending against the last entry that landed.
    if ek ~= 13 then
      report.kindMismatch = report.kindMismatch + 1
      return nil, true
    end
    -- PAST THE INT32 STORAGE CAP is the engine's CountRefusal (§2.8, §2.9),
    -- not damage: the field keeps nothing, the reason is named, and meeting
    -- the cap adds no malformed event or other counter (decode.go
    -- collectionCap). A count AT the cap decodes normally.
    if above(count, 0x7fffffff) then
      report.reason = "count_refusal"
      return {}
    end
    local entry = T.Tables[fd.entry]
    local kf = fd.key_field
    local keyname = kf.name
    local valname = fd.value_field.name
    local out = {}
    local last = nil
    local landed = false
    local widened = false
    local i = 0
    while math.ult(i, count) do
      i = i + 1
      local n = r_leb(sub)
      if sub.bad or not fits(sub, n) then
        report.malformed = true
        break
      end
      local at = sub.at
      sub.at = sub.at + n
      local key, keyok, kindbad, ewidened = map_key(ctx, kf, sub.b, at, at + n, key_default(T, entry, kf))
      if ewidened and not widened then
        -- the MAP counts one widened, at the first key the declaration widens
        widened = true
        report.widened = report.widened + 1
      end
      if kindbad then
        -- A MAP WITH HALF ITS KEYS IS NOT A MAP: it resets to empty and one
        -- kind mismatch counts; events inside earlier entries stand
        report.kindMismatch = report.kindMismatch + 1
        out = {}
        break
      end
      if not keyok then
        report.malformed = true
        break
      end
      if kf.shape == "string" and kf.bound ~= nil and #key > kf.bound then
        -- KEYS NEVER CLAMP: the entry is dropped whole and counted. An
        -- unbounded key carries no bound, so it is never dropped for its
        -- length (docs/SPEC-TABLES.md §2.13.1).
        report.clamped = report.clamped + 1
      else
        local order = -1
        if landed then order = key_order(kf, last, key) end
        if order > 0 then
          -- DESCENDING: the map keeps the ascending prefix it has
          report.malformed = true
          break
        end
        local inst = fresh(T, entry)
        sub.at = at
        local elim, ebad = narrow(sub, n)
        read_body(T, ctx, entry, sub, inst, true, keyname)
        restore(sub, at, n, elim, ebad)
        local dkey = inst[keyname]
        if order == 0 then
          -- EQUAL: a DUPLICATE, and last wins WHOLE
          report.duplicate = report.duplicate + 1
          for j = ctx.np, 1, -1 do
            local pp = ctx.pending[j]
            if pp.holder == out and pp.key == last then unqueue(ctx, j) end
          end
          out[last] = nil
        else
          last = dkey
          landed = true
        end
        -- A VALUE THAT IS A POINTER OR BLOB is queued for RESOLUTION (§3.1)
        -- against the entry's instance; point the queued slot at the map
        -- itself under the entry's key, so resolve lands the value there.
        -- An entry whose value is null is still an entry (§2.8, §3.1): a key
        -- cannot hold nil, so it holds false, as a null list element does.
        local vf = fd.value_field
        if vf.shape == "pointer" or vf.shape == "string_blob" or vf.shape == "bytes_blob" then
          out[dkey] = false
          for j = ctx.np, 1, -1 do
            local pp = ctx.pending[j]
            if pp.holder == inst then
              pp.holder = out
              pp.key = dkey
              pp.null = false
            end
          end
        else
          out[dkey] = inst[valname]
        end
      end
    end
    return out
  end

  local expected = fd.elem_kind
  if ek ~= expected then
    if not widens(ek, expected) then
      report.kindMismatch = report.kindMismatch + 1
      return nil, false
    end
    report.widened = report.widened + 1
  end

  if fd.keyed ~= nil then
    -- A keyed slot a repeat does not name keeps what the earlier occurrence
    -- placed (decode.go keyed writes one slot at a time and clears none).
    local out
    if prior ~= nil and reuse then
      -- the array the field already holds, read into in place
      out = prior
    else
      out = {}
      for i = 1, fd.bound do
        if prior ~= nil then
          out[i] = prior[i]
        else
          out[i] = zero_elem(T, fd.elem)
        end
      end
    end
    -- AN ENUM-KEYED BODY places its triples BY KEY (docs/SPEC-TABLES.md §3.2;
    -- decode.go keyed): its count is never clamped, since a slot this reader
    -- cannot name is skipped and counted unknown, and a slot lands once per
    -- triple whatever the count says.
    local i = 0
    while math.ult(i, count) do
      i = i + 1
      local keyref = r_leb(sub)
      if sub.bad or keyref == 0 or above(keyref, #ctx.table) then
        report.malformed = true
        break
      end
      local n = r_leb(sub)
      if sub.bad or not fits(sub, n) then
        report.malformed = true
        break
      end
      local slot = nil
      local keyid = ctx.table[keyref]
      for k, v in ipairs(fd.keyed) do
        if v[2] == keyid then slot = k end
      end
      if slot == nil then
        report.unknown = report.unknown + 1
        sub.at = sub.at + n
      else
        local at0 = sub.at
        local blim, bbad = narrow(sub, n)
        local body = sub
        local e = fd.elem
        local es = e.shape
        if es == "pointer" or es == "string_blob" or es == "bytes_blob" then
          local idx = r_leb(body)
          if body.bad or body.at ~= body.lim then
            report.malformed = true
          else
            queue(ctx, out, slot, idx, e, false)
          end
        elseif es == "enum" then
          -- a slot's value is read inside its own L, which also skips its
          -- slack (§3.2): only a reference that cannot be read is damage
          local x, ok = enum_read(ctx.table, body, e, report)
          if ok then
            if x == nil then
              out[slot] = false
            else
              out[slot] = x
            end
          end
        elseif es == "table" then
          local d = T.Tables[e.target]
          local inst = reuse and renew(T, d, out[slot]) or fresh(T, d)
          local np0 = ctx.np
          read_body(T, ctx, d, body, inst, true)
          if body.at ~= body.lim then
            report.malformed = true
            discard(ctx, np0)
            inst = renew(T, d, inst)
          end
          out[slot] = inst
        elseif es == "union" then
          local u = T.Unions[e.target]
          local x = nil
          if u == nil then
            report.malformed = true
          else
            x = read_union(T, ctx, u, body)
          end
          if x == nil or body.at ~= body.lim then
            report.malformed = true
          else
            out[slot] = x
          end
        elseif es == "string" then
          local raw = string.sub(body.b, body.at + 1, body.lim)
          body.at = body.lim
          if not utf8_ok(raw) then
            report.malformed = true
          else
            if e.bound ~= nil and e.bound > 0 and #raw > e.bound then
              report.clamped = report.clamped + 1
              raw = utf8_clamp(raw, e.bound)
            end
            out[slot] = raw
          end
        else
          local x = scalar_at(body, e, ek, report, ctx)
          if x ~= nil then out[slot] = x end
        end
        restore(sub, at0, n, blim, bbad)
      end
    end
    return out
  end

  local out
  if fd.fixed and prior ~= nil and reuse then
    -- the array the field already holds, read into in place
    out = prior
  else
    out = {}
    if fd.fixed then
      if prior ~= nil then
        for i = 1, fd.bound do
          out[i] = prior[i]
        end
        adopt(ctx, prior, out)
      else
        for i = 1, fd.bound do
          out[i] = zero_elem(T, fd.elem)
        end
      end
    end
  end
  local keep = count
  if fd.list then
    -- PAST THE INT32 STORAGE CAP is the engine's CountRefusal (§2.9),
    -- not damage: the field keeps nothing, the reason is named, and meeting
    -- the cap adds no malformed event or other counter (decode.go
    -- collectionCap). A count AT the cap decodes normally.
    if above(count, 0x7fffffff) then
      report.reason = "count_refusal"
      return {}
    end
  elseif above(count, fd.bound) then
    keep = fd.bound
    report.clamped = report.clamped + 1
  end
  local total = fd.total
  local spent = 0
  for i = 1, keep do
    if sub.at >= sub.lim then
      report.malformed = true
      break
    end
    -- AN ELEMENT OF A FIXED ARRAY OF UNIONS IS RESET THE MOMENT ITS ARM IS
    -- READ (decode.go element, docs/SPEC-TABLES.md §2.6, §4): a damaged
    -- repeat leaves None, never the arm an earlier occurrence placed. A
    -- counted or unbounded element that fails is simply not counted.
    if fd.fixed and fd.elem.shape == "union" then
      if reuse then
        out[i] = D.reset_elem(T, fd.elem, out[i])
      else
        out[i] = zero_elem(T, fd.elem)
      end
    end
    local el, ok = read_element(T, ctx, fd.elem, ek, sub, out, i, reuse)
    if not ok then break end
    -- THE TOTAL (docs/SPEC-TABLES.md §2.13): keep the longest PREFIX whose
    -- element bytes sum within `total`; the first element that would carry
    -- the sum past it is dropped with every element after it, counted ONCE.
    if total ~= nil and total > 0 then
      local d = T.Tables[fd.elem.target]
      local vf = d and d.fields[1]
      local add = 0
      if
        vf ~= nil
        and vf.shape == "string"
        and (vf.optional == nil or vf.optional == false or (el ~= nil and el[vf.pkey] == true))
      then
        add = #((el and el[vf.name]) or "")
      end
      if spent + add > total then
        report.clamped = report.clamped + 1
        break
      end
      spent = spent + add
    end
    out[i] = el
  end
  return out
end

-- ONE BODY (§3, §4): fields by id in any order, a field this table cannot
-- name skipped by its kind and counted unknown, a kind that widens decoded at
-- its own width and counted widened, any other kind a kind mismatch skipped
-- whole. Damage stops the body.
local read_body_fields

-- read_body keeps the walk's float naming (ctx.owner, ctx.field) across a
-- nested body: the body's own fields are named by it, and the holder's name
-- comes back when it returns.
read_body = function(T, ctx, desc, r, v, nested, mapkey)
  local owner, field = ctx.owner, ctx.field
  ctx.owner, ctx.field = desc.name, nil
  local ok = read_body_fields(T, ctx, desc, r, v, nested, mapkey)
  ctx.owner, ctx.field = owner, field
  return ok
end

read_body_fields = function(T, ctx, desc, r, v, nested, mapkey)
  local report = ctx.report
  -- `| required` fields (docs/SPEC-TABLES.md §2.3): each gets a seen flag,
  -- set when its id rides, and a record that ends without one is refused with
  -- missing_required naming the field. A required field inside an `if` guard
  -- is demanded only while its guard holds — with the branch off it is absent
  -- and no refusal is owed (§2.3, §3).
  local req = desc.required
  local arrived = nil
  if req then arrived = {} end
  -- which of the first 62 fields this body has already read: a nested value is
  -- read INTO the table the value already holds only the first time its field
  -- rides, so a repeat of the field starts from the field's defaults again
  local seen = 0
  local missingBefore = report.missingRequired or 0
  while true do
    local ref = r_leb(r)
    if r.bad then
      report.malformed = true
      return false
    end
    if ref == 0 then
      local missing = nil
      if req then
        for _, f in ipairs(req) do
          if not arrived[f.id] then
            local hold = true
            if f.guard ~= nil then
              for _, t in ipairs(f.guard) do
                if (v[t.field] == true) ~= t.want then
                  hold = false
                  break
                end
              end
            end
            if hold then
              if missing == nil then missing = "" end
              if #missing > 0 then missing = missing .. ", " end
              missing = missing .. f.name
            end
          end
        end
      end
      if missing ~= nil then
        report.missingRequired = (report.missingRequired or 0) + 1
        report.reason = "missing_required: " .. missing
        return false
      end
      -- a body below this one that was refused for a missing required field
      -- refuses this one too: the report is shared down the walk (§2.3)
      return (report.missingRequired or 0) == missingBefore
    end
    if above(ref, #ctx.table) then
      report.malformed = true
      return false
    end
    if r.at >= r.lim then
      report.malformed = true
      return false
    end
    local kind = r_u8(r)
    local id = ctx.table[ref]
    -- the announcement's reserved ids ride in the announcement and nowhere
    -- else (§3.3): in any body they are malformed
    if id == BUILD_VERSION_ID or id == MESSAGE_VOCABULARY_ID then
      report.malformed = true
      return false
    end
    if id == NODE_TABLE_ID then
      -- the node table rides in a VARIABLE root body and nowhere else (§3.1);
      -- a reader holding no numbering skips it by its kind and counts unknown
      if nested then
        report.malformed = true
        return false
      end
      if desc.variable then
        if not skip(r, kind) then
          report.malformed = true
          return false
        end
      else
        report.unknown = report.unknown + 1
        if not skip(r, kind) then
          report.malformed = true
          return false
        end
      end
    else
      local fd = desc.byid[id]
      if fd ~= nil then ctx.field = fd.name end
      if fd == nil then
        report.unknown = report.unknown + 1
        if not skip(r, kind) then
          report.malformed = true
          return false
        end
      elseif kind ~= fd.kind then
        if widens(kind, fd.kind) then
          local x = scalar_at(r, fd, kind, report, ctx)
          if x == nil then return false end
          v[fd.name] = x
          if arrived then arrived[fd.id] = true end
          if fd.optional then v[fd.pkey] = true end
          -- a map entry's KEY widens once per MAP (§2.8), counted by the map
          if fd.name ~= mapkey then report.widened = report.widened + 1 end
        else
          report.kindMismatch = report.kindMismatch + 1
          if not skip(r, kind) then
            report.malformed = true
            return false
          end
        end
      else
        local bit, ord = 0, fd.ordinal
        if ord < 63 then bit = 1 << ord end
        local first = bit ~= 0 and (seen & bit) == 0
        seen = seen | bit
        if not read_field(T, ctx, fd, r, v, first) then return false end
        if arrived then arrived[fd.id] = true end
      end
    end
  end
end

-- A PAYLOAD'S OUTCOME. read_payload answers (value, status):
--   OK         the value read; a pointer or blob answers no value, its slot is
--              queued for resolution (§3.1)
--   DAMAGED    the content is wrong inside its own framing (ill-formed text, a
--              body that does not end at its terminator, a scalar whose L is
--              not its width) and the counter is set; the value is what a FIELD
--              then holds in its place (its declared default), which an ARM
--              does not keep: the union reads None
--   STOP       the framing itself broke, the counter is set, nothing is kept
--   INERT      an array body shorter than its own header (§3): nothing read
--   KEEP       an array body whose header could not be read: the holder keeps
--              what it has
--   MISMATCH   an array body of another element kind: the payload is not this
--              array's, so an optional stays absent and an arm is not selected
local OK, DAMAGED, STOP, INERT, KEEP, MISMATCH = 1, 2, 3, 4, 5, 6

-- A FIELD'S PAYLOAD, read ONCE for a field and for a union arm (docs/PORTING.md
-- M18; docs/SPEC-TABLES.md §2.6, §3). An arm payload is exactly the bytes a
-- field puts after its own framing prefix, so the arm is read by the SAME code
-- one level down: `n` is the arm's L standing in for the field's own length
-- where the kind has one, and the fixed width the L frames where it does not;
-- a field passes nil and reads its own L from r. (holder, key) is the slot the
-- value lands in, which a pointer queues for resolution; `kind` is the wire kind
-- on the header, which differs from fd.kind only when the kind widens. The shape
-- to refuse is a second reader for arms beside this one: it drifts from the
-- field's the first time a kind changes, and the drift is silent on both wires.
local function read_payload(T, ctx, fd, r, n, holder, key, kind, reuse)
  local report = ctx.report
  local s = fd.shape
  if s == "bool" or s == "int" or s == "flags" or s == "float" or s == "wide" then
    if n ~= nil and kind == fd.kind then
      -- an arm's L must BE the kind's width
      local w = width_of(kind)
      if w == 0 or n ~= w then
        report.malformed = true
        return nil, DAMAGED
      end
    end
    local x = scalar_at(r, fd, kind, report, ctx)
    if x == nil then return nil, STOP end
    return x, OK
  elseif s == "string" then
    if n == nil then
      n = r_leb(r)
      if r.bad or not fits(r, n) then
        report.malformed = true
        return nil, STOP
      end
    end
    local raw = r_bytes(r, n)
    if not utf8_ok(raw) then
      report.malformed = true
      return fd.default or "", DAMAGED
    end
    if fd.bound ~= nil and fd.bound > 0 and #raw > fd.bound then
      report.clamped = report.clamped + 1
      raw = utf8_clamp(raw, fd.bound)
    end
    return raw, OK
  elseif s == "enum" then
    local x, ok = enum_read(ctx.table, r, fd, report)
    if not ok then return nil, STOP end
    if n ~= nil and r.at ~= r.lim then
      report.malformed = true
      return nil, DAMAGED
    end
    return x, OK
  elseif s == "pointer" or s == "string_blob" or s == "bytes_blob" then
    local i = r_leb(r)
    if r.bad then
      report.malformed = true
      return nil, STOP
    end
    if n ~= nil and r.at ~= r.lim then
      report.malformed = true
      return nil, DAMAGED
    end
    queue(ctx, holder, key, i, fd)
    return nil, OK
  elseif s == "table" then
    if n == nil then
      n = r_leb(r)
      if r.bad or not fits(r, n) then
        report.malformed = true
        return nil, STOP
      end
    end
    local d = T.Tables[fd.target]
    -- the first read of a field is into the record the holder already has,
    -- put back to its defaults; a repeat of the field is read into a new one
    local inst
    if reuse then
      inst = renew(T, d, holder[key])
    else
      inst = fresh(T, d)
    end
    local np0 = ctx.np
    local at0 = r.at
    local lim, bad = narrow(r, n)
    read_body(T, ctx, d, r, inst, true)
    local clean = r.at == r.lim
    restore(r, at0, n, lim, bad)
    if not clean then
      report.malformed = true
      discard(ctx, np0)
      return renew(T, d, inst), DAMAGED
    end
    return inst, OK
  elseif s == "union" then
    local u = T.Unions[fd.target]
    if u == nil then
      report.malformed = true
      return nil, STOP
    end
    -- a union field is its arm header in place; an arm that is another union
    -- is that union's own arm header inside this arm's L (decode.go arm): its
    -- damage reads None, and slack after it is skipped by the outer L, uncounted
    local x = read_union(T, ctx, u, r)
    if x == nil then return nil, STOP end
    return x, OK
  elseif s == "array" or s == "map" or s == "bytes" then
    if n == nil then
      n = r_leb(r)
      if r.bad or not fits(r, n) then
        report.malformed = true
        return nil, STOP
      end
    end
    local endpos = r.at + n
    if n < 2 then
      r.at = endpos
      return nil, INERT
    end
    -- a fixed or keyed array keeps the elements a damaged repeat could not
    -- replace (decode.go arrayBody leaves fv.Elems standing)
    local prior = nil
    if fd.fixed or fd.keyed ~= nil then prior = holder[key] end
    local out, sel = read_array_body(T, ctx, fd, r, endpos, prior, reuse)
    r.at = endpos
    if sel == false then return nil, MISMATCH end
    if out == nil then return nil, KEEP end
    return out, OK
  end
  report.malformed = true
  return nil, STOP
end

-- ONE FIELD (§3, §4), at its declared kind: its payload is read by
-- read_payload, the code a union arm is read by too (M18), and the field line
-- is what it does with the outcome: a value is stored, damaged content reads the
-- field's default, framing damage stops the body, and an optional is present
-- unless its payload was another array's.
read_field = function(T, ctx, fd, r, v, first)
  local x, st = read_payload(T, ctx, fd, r, nil, v, fd.name, fd.kind, first)
  if st == STOP then return false end
  if st == OK or st == DAMAGED then v[fd.name] = x end
  if fd.optional and st ~= MISMATCH then v[fd.pkey] = true end
  return true
end

-- AN ARM HEADER is a field header (§3): the arm id reference, its kind, L,
-- then L bytes of arm payload. A retyped arm is an ordinary kind mismatch,
-- and damage inside L reads the union None and the parent reads on.
local read_arm
local read_arm_payload

-- read_union names a float arm <Union>.<arm> (docs/SPEC-TABLES.md §3.3), and
-- gives the holder's name back when it returns.
read_union = function(T, ctx, u, r)
  local owner, field = ctx.owner, ctx.field
  local x = read_arm(T, ctx, u, r)
  ctx.owner, ctx.field = owner, field
  return x
end

read_arm = function(T, ctx, u, r)
  local report = ctx.report
  local armref = r_leb(r)
  if r.bad then
    report.malformed = true
    return nil
  end
  if armref == 0 then return { tag = 0 } end
  if above(armref, #ctx.table) then
    report.malformed = true
    return nil
  end
  local armid = ctx.table[armref]
  if r.at >= r.lim then
    report.malformed = true
    return nil
  end
  local armkind = r_u8(r)
  local n = r_leb(r)
  if r.bad or not fits(r, n) then
    report.malformed = true
    return nil
  end
  local arm = u.byid[armid]
  if arm == nil then
    report.unknown = report.unknown + 1
    r.at = r.at + n
    return { tag = 0 }
  end
  -- the payload is read through the same reader, its limit narrowed to the arm's
  -- L (docs/PORTING.md M1)
  local at0 = r.at
  local lim, bad = narrow(r, n)
  local x = read_arm_payload(T, ctx, u, arm, armkind, r, n)
  restore(r, at0, n, lim, bad)
  return x
end

-- one arm's payload, read from the reader sub bounded to the arm's L
read_arm_payload = function(T, ctx, u, arm, armkind, sub, n)
  local report = ctx.report
  local f = arm.field
  ctx.owner, ctx.field = u.name, arm.name
  if f == nil then
    if armkind ~= 32 then
      report.kindMismatch = report.kindMismatch + 1
      return { tag = 0 }
    end
    if n ~= 0 then
      report.malformed = true
      return { tag = 0 }
    end
    return { tag = arm.tag }
  end
  if armkind ~= f.kind then
    if widens(armkind, f.kind) then
      -- the same payload reader, at the kind the header carried
      local out = { tag = arm.tag }
      local x, st = read_payload(T, ctx, f, sub, n, out, arm.slot, armkind)
      if st ~= OK then return { tag = 0 } end
      if sub.at ~= sub.lim then
        report.malformed = true
        return { tag = 0 }
      end
      report.widened = report.widened + 1
      out[arm.slot] = x
      return out
    end
    report.kindMismatch = report.kindMismatch + 1
    return { tag = 0 }
  end
  -- THE ARM'S PAYLOAD IS A FIELD LINE (docs/PORTING.md M18): the field's own
  -- reader, one level down, with the arm's L standing in for the field's
  -- length. What the field would store the arm holds under its name; what the
  -- field would default or stop on, the union reads None, and the parent reads
  -- on past L.
  local out = { tag = arm.tag }
  local x, st = read_payload(T, ctx, f, sub, n, out, arm.slot, armkind)
  if st == OK then
    out[arm.slot] = x
  elseif st == INERT then
    -- shorter than its own header, it is inert and the arm holds its zero
    out[arm.slot] = zero_array(T, f)
  else
    return { tag = 0 }
  end
  return out
end

-- RESOLUTION (§3.1, decodenodes.go resolveCell): 0 is null, 1 is the root
-- (checked against the reader's own root type), k + 1 is record k. A blob
-- record resolves to its NODE BOX, so two slots naming one record land one
-- Lua reference and two distinct records of equal bytes stay two (§3.1, §6.3).
local function resolve(ctx, root, rootdesc)
  local report = ctx.report
  local good = ctx.good
  local seen_blob = nil
  local pending = ctx.pending
  for i = 1, ctx.np do
    local p = pending[i]
    local d = p.d
    local value = nil
    if good and p.index ~= 0 then
      if p.index == 1 then
        if d.shape == "pointer" and d.target == rootdesc.name then
          value = root
        else
          report.kindMismatch = report.kindMismatch + 1
        end
      elseif above(p.index - 1, #ctx.nodes) then
        report.malformed = true
      else
        local node = ctx.nodes[p.index - 1]
        if node ~= nil and (node.kind == "unknown" or node.kind == "null") then
          -- §3.1: unknown and refused blob nodes keep their index, but a
          -- pointer to either resolves to null without a kind mismatch.
          value = nil
        elseif node ~= nil then
          if d.shape == "string_blob" then
            if node.kind == "s" then
              value = node.box
              if seen_blob == nil then seen_blob = {} end
              if seen_blob[p.index] then
                report.blobShared = (report.blobShared or 0) + 1
              else
                seen_blob[p.index] = true
              end
            else
              report.kindMismatch = report.kindMismatch + 1
            end
          elseif d.shape == "bytes_blob" then
            if node.kind == "b" then
              value = node.box
              if seen_blob == nil then seen_blob = {} end
              if seen_blob[p.index] then
                report.blobShared = (report.blobShared or 0) + 1
              else
                seen_blob[p.index] = true
              end
            else
              report.kindMismatch = report.kindMismatch + 1
            end
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
    -- §3.1: 0 is null and null is elided, so a null slot resolves to the
    -- slot's null (queue): nil for a pointer field, which is absent, and false
    -- for a LIST element (§3.1's "A null slot is index 0 ... and it rides in
    -- its place"), so the trailing null and the count stay. A slot that does
    -- not resolve is WRITTEN null rather than left alone, because an earlier
    -- occurrence of the same slot may already have resolved into it.
    if value == nil then value = p.null end
    p.holder[p.key] = value
    -- the queue keeps its entries for the next load, and no value it named
    p.holder, p.d = nil, nil
  end
  ctx.np = 0
end

-- THE LOADER STATE (docs/PORTING.md M1: the read path allocates nothing, and the
-- storage it works in is the caller's). L.NewState() builds everything a load
-- keeps between its first and its last byte: the id table and its seen-set, the
-- two readers (one that walks the bodies, one that scans), the queue of pointer
-- slots, the context. A caller that loads many records keeps ONE state and
-- hands it to each load; a load with no state builds one, and answers exactly
-- what it answered before. A state serves one load at a time.
function L.NewState()
  local scan = new_reader("", 0, 0)
  local junk = { unknown = 0, kindMismatch = 0, widened = 0, clamped = 0, duplicate = 0, malformed = false }
  local sink = {}
  return {
    ids = {},
    seen = {},
    o = { table = nil, lim = 0 },
    scan = scan,
    rd = new_reader("", 0, 0),
    junk = junk,
    sink = sink,
    ctx = {
      table = nil,
      report = nil,
      pending = {},
      np = 0,
      nodes = NO_NODES,
      good = false,
      owner = nil,
      field = nil,
      scan = scan,
      junk = junk,
      sink = sink,
    },
  }
end

-- the queue of a load that stopped before it resolved, and the sink the damaged
-- bodies' slots were pointed at, are given back empty
local function release(ctx)
  local pending = ctx.pending
  for i = 1, ctx.np do
    local p = pending[i]
    p.holder, p.d = nil, nil
  end
  ctx.np = 0
  local sink = ctx.sink
  if next(sink) ~= nil then
    for k in pairs(sink) do
      sink[k] = nil
    end
  end
  ctx.report, ctx.nodes = nil, NO_NODES
end

-- LoadTolerant loads a form-1 wire for table `name` of module T into a fresh
-- value and fills report. Damage discards the whole load (§4): every root
-- class returns nil when malformed, including a wire that cannot open.
-- Evolution remains tolerant under this explicit name (§6.5).
--
-- `state` (L.NewState()) and `into` are the CALLER-OWNED STORAGE form
-- (docs/PORTING.md M1): with `into`, the root is the caller's value, put back to
-- the declared defaults in place and read into, and what it already holds of a
-- nested table, a fixed array and a union is the storage that is read into; with
-- `state`, the readers, the id table and the queue are the caller's and nothing
-- of them is built. Neither changes a byte of what is read: both are nil in the
-- plain form. A load that answers nil leaves `into` at defaults or partly read.
--
-- This is THE TOLERANT READ (docs/SPEC-TABLES.md §6.5), under its explicit
-- name: it keeps a valid value and its evolution report. The plain L.Load below is
-- the strict read.
function L.LoadTolerant(T, name, bytes, report, state, into)
  local desc = T.Tables[name]
  state = state or L.NewState()
  report.nonFiniteFloat = nil
  -- THE INTEGRITY TRAILER (§2.14, §3.4): a declaring root's form-1 file ends
  -- with the little-endian CRC-32C of every byte before it. The form byte is
  -- read first, so a file of another form passes through to the open that
  -- names it; a form-1 file shorter than five bytes or one whose trailer does
  -- not match is the whole-file damage integrity_mismatch, and nothing is
  -- decoded. The trailer is then removed and every rule below speaks of the
  -- file without it (it is outside the region measure): the wire's length is
  -- four less, and no copy of it is made.
  local blen = #bytes
  if desc ~= nil and desc.integrity ~= nil and blen >= 1 and bytes:byte(1) == 1 then
    if blen < 5 then
      report.malformed = true
      report.reason = "integrity_mismatch"
      return nil
    end
    local n = blen - 4
    if string.unpack("<I4", bytes, n + 1) ~= L.crc32c(bytes, n) then
      report.malformed = true
      report.reason = "integrity_mismatch"
      return nil
    end
    blen = n
  end
  local o = open(bytes, blen, report, state)
  if o == nil then
    if report.malformed then return nil end
    -- An unsupported form keeps its existing class-specific refusal; it is
    -- not a decoded damaged value (§3, §4.2).
    if desc == nil then return nil end
    if report.refused ~= 0 and desc.variable then return nil end
    if into ~= nil then return D.reset(T, desc, into) end
    return fresh(T, desc)
  end
  if desc == nil then
    report.malformed = true
    return nil
  end
  local raw = node_scan(bytes, o, report, desc.variable, state)
  local ctx = state.ctx
  ctx.table, ctx.report, ctx.np, ctx.good, ctx.owner, ctx.field = o.table, report, 0, raw ~= nil, nil, nil
  ctx.nodes = NO_NODES
  if raw ~= nil then
    if #raw > 0 then ctx.nodes = {} end
    for _, rec in ipairs(raw) do
      -- A BLOB NODE KEEPS ITS IDENTITY (§3.1: every reachable node is written
      -- once and two references to one node are one node; §6.3: sharing and a
      -- back-reference are the same fact). A Lua string is a value, so two
      -- equal-content blobs would be one key; the node is boxed instead, and
      -- every slot naming it resolves to the SAME box, as a *T slot resolves
      -- to the same instance table.
      if rec.tid == STRING_BLOB_ID then
        local body = string.sub(bytes, rec.at + 1, rec.lim)
        if utf8_ok(body) then
          ctx.nodes[#ctx.nodes + 1] = { kind = "s", blob = body, box = { value = body } }
        else
          report.malformed = true
          report.wireDamaged = true
          -- §3.1: the record keeps its index even when its content is refused,
          -- so one unnameable node never shifts the rest; every pointer naming
          -- it reads null.
          ctx.nodes[#ctx.nodes + 1] = { kind = "null" }
        end
      elseif rec.tid == BYTES_BLOB_ID then
        local body = string.sub(bytes, rec.at + 1, rec.lim)
        ctx.nodes[#ctx.nodes + 1] = { kind = "b", blob = body, box = { value = body } }
      else
        local subdesc = T.TypeIds[rec.tid]
        if subdesc == nil then
          report.unknown = report.unknown + 1
          -- §3.1: numbering is positional in the table, so a node this reader
          -- cannot name KEEPS ITS INDEX; every pointer naming it reads null.
          ctx.nodes[#ctx.nodes + 1] = { kind = "unknown" }
        else
          local inst = fresh(T, subdesc)
          ctx.nodes[#ctx.nodes + 1] = { kind = "t", codec = subdesc, value = inst, at = rec.at, lim = rec.lim }
        end
      end
    end
    local rd = state.rd
    for _, node in ipairs(ctx.nodes) do
      if node.kind == "t" then read_body(T, ctx, node.codec, point(rd, bytes, node.at, node.lim), node.value, true) end
    end
  end
  local root
  if into ~= nil then
    root = D.reset(T, desc, into)
  else
    root = fresh(T, desc)
  end
  local missingBefore = report.missingRequired or 0
  read_body(T, ctx, desc, point(state.rd, bytes, 1, o.lim), root, false)
  -- A NON-FINITE FLOAT REFUSES THE READ, the tolerant one too, and outranks
  -- any damage the walk met after it (docs/SPEC-TABLES.md §3.3): nothing
  -- lands, and Reason names the float.
  if report.nonFiniteFloat ~= nil then
    report.reason = "non_finite_float: " .. report.nonFiniteFloat
    release(ctx)
    return nil
  end
  if report.missingRequired and report.missingRequired > missingBefore then
    release(ctx)
    return nil
  end
  resolve(ctx, root, desc)
  release(ctx)
  if report.malformed then return nil end
  return root
end

-- strict_refusal answers the refusal's §11/§6.5 name when the strict read
-- refuses this wire, and nil when it accepts it. It reads the FORM BYTE FIRST
-- in refusal.lua's order (§2.14, §6.5), then the report the tolerant walk just
-- filled: a refused form, integrity_mismatch, a count_refusal (the engine's
-- CountRefusal, §2.9), wire_damaged, missing_required, malformed, then the
-- counters blob_shared, clamped and kind_mismatch; a non-finite float
-- (§3.3) outranks every one the walk reaches. The set is the one refusal.lua's ReadStrict
-- owns; unknown, widened and duplicate ALONE are evolution, exact decoding and
-- data identity, not loss (§6.5), so they read clean. This entry reads the
-- variable form, so every form byte but 1 is the one reason unknown_form:
-- TableReport.Err() folds previous_form, newer_form and message_form_as_file
-- alike (§11).
local function strict_refusal(bytes, report, root)
  if type(bytes) ~= "string" then return "wire_damaged" end
  local form = #bytes >= 1 and bytes:byte(1) or nil
  if form ~= nil and form ~= 1 then return "unknown_form" end
  if report.refused ~= 0 then return "unknown_form" end
  if report.reason == "integrity_mismatch" then return "integrity_mismatch" end
  -- A NON-FINITE FLOAT outranks every verdict the walk could reach after it
  -- (§3.3): no read keeps one.
  if report.nonFiniteFloat ~= nil then return "non_finite_float" end
  -- THE WALK'S CountRefusal (§2.9): the builder-path storage refusal is the
  -- load's own result, so it refuses by name before any damage class does.
  if report.reason == "count_refusal" then return "count_refusal" end
  if report.wireDamaged then return "wire_damaged" end
  if (report.missingRequired or 0) > 0 then return "missing_required" end
  if report.malformed then return report.reason or "malformed" end
  if (report.blobShared or 0) > 0 then return "blob_shared" end
  if report.clamped > 0 then return "clamped" end
  if report.kindMismatch > 0 then return "kind_mismatch" end
  if root == nil then return "wire_damaged" end
  return nil
end

-- Load is THE STRICT READ (docs/SPEC-TABLES.md §6.5: THE PLAIN NAME IS THE
-- STRICT READ): it runs the tolerant walk into the caller's report, overwriting
-- it with THIS read's report (every counter counted as the tolerant read counts
-- it), and answers the root only where the strict set accepts the wire.
-- Otherwise it answers nil, raising nothing, with report.reason naming the
-- refusal. A nil report refuses the same way (§6.5).
function L.Load(T, name, bytes, report, state, into)
  local r = report
  if r == nil then r = {} end
  r.malformed = false
  r.refused = 0
  r.unknown = 0
  r.kindMismatch = 0
  r.clamped = 0
  r.widened = 0
  r.duplicate = 0
  r.blobShared = 0
  r.missingRequired = 0
  r.wireDamaged = false
  r.reason = nil
  local root = L.LoadTolerant(T, name, bytes, r, state, into)
  local reason = strict_refusal(bytes, r, root)
  if reason ~= nil then
    r.reason = reason
    return nil
  end
  return root
end

-- THE EXTENT WALK (docs/SPEC-TABLES.md §6.5, §8.5; internal/tablewire/extent.go):
-- LoadMeasure(T, name, bytes) ports tablewire.FileRegionMeasure(u, root, data):
-- the same walk of the framing, the same refusals, the same rounding; returns
-- the byte count or nil, and changes nothing in Load.

local function round_up(n, a)
  return (n + a - 1) & -a
end

local function round_region(n, align)
  return (n + align - 1) & -align
end

-- trailer(data) in internal/tablewire/decode.go: locates the ID table from the
-- end of the wire without running bodyExtent's early-terminator check.
local function measure_trailer(bytes)
  if type(bytes) ~= "string" or #bytes < 9 or bytes:byte(1) ~= 1 then return nil end
  local count = string.unpack("<i8", bytes, #bytes - 7)
  if count < 0 or count > (#bytes - 9) // 8 or count > D.ID_TABLE_BOUND then return nil end
  local endpos = #bytes - 8 - count * 8
  local table_ids = {}
  local seen = {}
  for i = 0, count - 1 do
    local id = string.unpack("<i8", bytes, endpos + i * 8 + 1)
    if seen[id] then return nil end
    seen[id] = true
    table_ids[i + 1] = id
  end
  return { table = table_ids, lim = endpos }
end

local function extent_body(r)
  local n = r_leb(r)
  if r.bad or n < 0 or n > r.lim - r.at then
    r.at = r.lim
    return nil, nil
  end
  local at = r.at
  r.at = at + n
  return at, at + n
end

local function arm_is_pointer(f)
  if f == nil then return false end
  local s = f.shape
  if s == "pointer" or s == "string_blob" or s == "bytes_blob" then return true end
  if s == "array" and f.elem ~= nil then
    local es = f.elem.shape
    if es == "pointer" or es == "string_blob" or es == "bytes_blob" then return true end
  end
  return false
end

local measure_record, measure_field, measure_union

measure_record = function(T, ids, scan, desc, b, at, lim)
  local r = new_reader(b, at, lim)
  while true do
    local ref = r_leb(r)
    if r.bad or ref <= 0 then return true end
    if ref > #ids or r.at >= r.lim then return true end
    local id = ids[ref]
    local kind = r_u8(r)
    local f = desc ~= nil and D.by_id(desc, id) or nil
    if f == nil then
      if not skip(r, kind) then return true end
    elseif kind == 15 and f.shape == "union" then
      local un = T.Unions[f.target]
      if un ~= nil then
        if not measure_union(T, ids, scan, un, r) then return false end
      else
        if not skip(r, kind) then return true end
      end
    elseif kind ~= 13 and kind ~= 14 and kind ~= 16 then
      if not skip(r, kind) then return true end
    else
      local pat, plim = extent_body(r)
      if pat == nil then return true end
      if kind == f.kind then
        if not measure_field(T, ids, scan, f, r.b, pat, plim) then return false end
      end
    end
  end
end

measure_field = function(T, ids, scan, f, b, at, lim)
  if f.shape == "pointer" or f.shape == "string_blob" or f.shape == "bytes_blob" then return true end
  if f.shape == "map" or f.shape == "array" then
    local r = new_reader(b, at, lim)
    if r.lim - r.at < 2 then return true end
    local kind = r_u8(r)
    local expected = (f.shape == "map") and 13 or f.elem_kind
    if kind ~= expected and not widens(kind, expected) then return true end
    local count = r_leb(r)
    if r.bad then return true end
    if f.shape == "map" or f.list then
      local size, align
      if f.shape == "map" then
        local entry = T.Tables[f.entry]
        size, align = entry.size, entry.align
      else
        size, align = D.list_layout(T, f)
      end
      if count < 0 or count > 0x7fffffff then return false end
      local floor = width_of(kind)
      if kind == 17 then floor = 1 end
      if kind == 13 then floor = 2 end
      if floor < 1 then floor = 1 end
      if count > (r.lim - r.at) // floor then return false end
      scan.at = round_up(scan.at, align) + count * size
    end
    if kind ~= 13 and kind ~= 15 then return true end
    local keyed = (f.shape == "array" and f.keyed ~= nil)
    local i = 0
    -- `count` is an UNSIGNED 64-bit number (docs/SPEC-TABLES.md §3): a huge
    -- count is a Lua negative integer, so the loop bound is unsigned and the
    -- payload end stops the walk, exactly as the engine's uint64 loop does.
    while math.ult(i, count) and r.at < r.lim do
      i = i + 1
      if keyed then
        r_leb(r)
        if r.bad then return true end
      end
      if kind == 15 then
        local un = T.Unions[f.elem.target]
        if un ~= nil then
          if not measure_union(T, ids, scan, un, r) then return false end
        end
      else
        local pat, plim = extent_body(r)
        if pat == nil then return true end
        local st = (f.shape == "map") and T.Tables[f.entry] or T.Tables[f.elem.target]
        if st ~= nil and not measure_record(T, ids, scan, st, r.b, pat, plim) then return false end
      end
    end
    return true
  end
  if f.shape == "table" then return measure_record(T, ids, scan, T.Tables[f.target], b, at, lim) end
  return true
end

measure_union = function(T, ids, scan, un, r)
  local ref = r_leb(r)
  if r.bad then
    r.at = r.lim
    return true
  end
  if ref == 0 then return true end
  if ref < 0 or ref > #ids or r.at >= r.lim then
    r.at = r.lim
    return true
  end
  local id = ids[ref]
  r_u8(r)
  local pat, plim = extent_body(r)
  if pat == nil then return true end
  if un == nil then return true end
  for _, arm in ipairs(un.arms) do
    if arm.id == id and arm.field ~= nil and not arm_is_pointer(arm.field) then
      if arm.field.shape == "union" then
        local arm_r = new_reader(r.b, pat, plim)
        return measure_union(T, ids, scan, T.Unions[arm.field.target], arm_r)
      end
      return measure_field(T, ids, scan, arm.field, r.b, pat, plim)
    end
  end
  return true
end

function L.LoadMeasure(T, name, bytes)
  local desc = T.Tables[name]
  if desc == nil then return nil end
  -- THE TRAILER IS OUTSIDE THE REGION MEASURE (§2.14, §3.4): a declaring
  -- root's file is checked and stripped first, exactly as the load does, and a
  -- mismatch answers nil.
  if desc.integrity ~= nil and #bytes >= 1 and bytes:byte(1) == 1 then
    if #bytes < 5 then return nil end
    local n = #bytes - 4
    if string.unpack("<I4", bytes, n + 1) ~= L.crc32c(bytes:sub(1, n)) then return nil end
    bytes = bytes:sub(1, n)
  end
  local o = measure_trailer(bytes)
  if o == nil then return nil end
  local align = D.region_align(T, desc)
  local function size_of(st, b, at, lim)
    local scan = { at = 0 }
    if not measure_record(T, o.table, scan, st, b, at, lim) then return 0, false end
    return round_region(round_region(D.record_size(st), align) + scan.at, align), true
  end
  local total, ok = size_of(desc, bytes, 1, o.lim)
  if not ok then return nil end
  local raw = node_scan(bytes, o, {}, desc.variable, L.NewState())
  if raw == nil then return nil end
  local reach = desc.reach
  local nodes, string_blob, bytes_blob = reach.nodes, reach.string_blob, reach.bytes_blob
  for _, rec in ipairs(raw) do
    local st = nodes[rec.tid]
    if st ~= nil then
      local n, rok = size_of(st, bytes, rec.at, rec.lim)
      if not rok then return nil end
      total = total + n
    elseif (rec.tid == BYTES_BLOB_ID and bytes_blob) or (rec.tid == STRING_BLOB_ID and string_blob) then
      local blen = rec.lim - rec.at
      if blen > 0xffffffff then return nil end
      local n = 8 + blen
      if rec.tid == STRING_BLOB_ID then n = n + 1 end
      total = total + round_region(n, align)
    end
  end
  return total + (#raw + 1) * 16
end

return L
