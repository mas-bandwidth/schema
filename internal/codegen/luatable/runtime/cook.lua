-- cook.lua — the Lua table runtime's COOKED FORM (docs/SPEC-TABLES.md §7,
-- §7.1, §7.2, §7.4; docs/PORT-LUA.md). Emitted beside the unit's
-- <Package>Tables.lua by internal/codegen/luatable, which embeds runtime/*.lua
-- by glob.
--
-- A cook is a region image of a load, opened in place: the header's magic,
-- order, build version, reserved words, alignment and part lengths (§7.1) and
-- the base's alignment. §7.4's two passes over the attribution and the region
-- are cook-check's, C.Check below.
-- Every refusal answers its TableRefuseReason name: not_a_cook, foreign_order,
-- wrong_build_version, reserved_not_zero, bad_alignment, truncated,
-- unaligned_base, bad_layout.
--
-- Open answers a handle or nil plus the reason name, from the header and the
-- base alone (§7.1). Check runs Open and then §7.4's two passes over the
-- attribution and the region, the scan internal/tablecook's Check runs: it
-- decodes no payload and follows no reference while checking.
local C = {}

local MAGIC = 0x4B4F4F434D484353
local HEADER_BYTES = 64
local MAX_ALIGN = 64
local BLOB_HEADER = 8
local BLOB_ALIGN = 8
local NOT_MATERIALIZED = -1 -- 0xffffffffffffffff as a Lua integer

local function u64(b, at)
  return string.unpack("<i8", b, at + 1)
end

local function u32(b, at)
  return string.unpack("<I4", b, at + 1)
end

local function i32(b, at)
  return string.unpack("<i4", b, at + 1)
end

local function refuse(why)
  return nil, why
end

-- align_up is §7.1's derivation of the data part's offset FROM the recorded
-- alignment; a fact a reader computes is a fact two writers cannot disagree
-- about.
local function align_up(n, a)
  return (n + a - 1) & ~(a - 1)
end

-- THE HEADER (§7.1), in the order C++'s TableCookOpen checks it: magic
-- bytewise first, then the byte-order word, build version, reserved words, the
-- alignment (before it is used), both part lengths, the root's fit, and the
-- base. Lua strings have no caller-supplied base, so the last clause is the
-- null buffer alone.
local function open_header(T, name, bytes)
  if type(bytes) ~= "string" or bytes == nil then return refuse("unaligned_base") end
  local n = #bytes
  if n < HEADER_BYTES then return refuse("truncated") end
  local magic = string.unpack("<I8", bytes, 1)
  if magic ~= MAGIC then
    if string.unpack(">I8", bytes, 1) == MAGIC then return refuse("foreign_order") end
    return refuse("not_a_cook")
  end
  if u64(bytes, 16) ~= 1 then return refuse("not_a_cook") end
  local build = u64(bytes, 8)
  if build ~= T.BuildVersion then return refuse("wrong_build_version") end
  if u64(bytes, 48) ~= 0 or u64(bytes, 56) ~= 0 then return refuse("reserved_not_zero") end
  local data_length = u64(bytes, 24)
  local attrib_length = u64(bytes, 32)
  local alignment = u64(bytes, 40)
  local root = T.Tables[name]
  if root == nil then return refuse("bad_layout") end
  if alignment < 8 or alignment > MAX_ALIGN or (alignment & (alignment - 1)) ~= 0 or alignment % root.align ~= 0 then
    return refuse("bad_alignment")
  end
  local data_offset = align_up(HEADER_BYTES, alignment)
  if n < data_offset then return refuse("truncated") end
  if data_length < 0 or attrib_length < 0 or data_length > n - data_offset then return refuse("truncated") end
  if attrib_length ~= n - data_offset - data_length then return refuse("truncated") end
  if data_length < root.size then return refuse("truncated") end
  return {
    T = T,
    name = name,
    bytes = bytes,
    data_offset = data_offset,
    data_length = data_length,
    attrib_offset = data_offset + data_length,
    attrib_length = attrib_length,
    alignment = alignment,
  }
end

-- THE DIRECTORY, decoded in file order (§6.3). An entry is offset then type
-- id, both u64 and both in the producer's order.
local function directory(h)
  local n = h.attrib_length
  if n == 0 then return nil, "bad_layout" end
  if n % 16 ~= 0 then return nil, "bad_layout" end
  local out = {}
  for i = 0, n / 16 - 1 do
    local at = h.attrib_offset + i * 16
    out[i + 1] = { offset = u64(h.bytes, at), type_id = u64(h.bytes, at + 8) }
  end
  return out
end

-- PASS ONE (§7.4): the directory, linearly and with no state. A sentinel
-- entry, a type id no table and neither reserved blob id names, a first entry
-- that is not the root at offset zero, offsets that do not ascend, an offset
-- unaligned for its own type, and a node whose size does not fit before
-- data_length all refuse.
local function check_directory(h, dir)
  local by_id = {}
  for _, d in pairs(h.T.Tables) do
    by_id[d.type_id] = d
  end
  local prev_end = 0
  for i, e in ipairs(dir) do
    if e.offset == NOT_MATERIALIZED then return "bad_layout" end
    local st = by_id[e.type_id]
    local size, align
    if st ~= nil then
      size, align = st.size, st.align
    elseif e.type_id == h.T.BytesBlobTypeId or e.type_id == h.T.StringBlobTypeId then
      size, align = BLOB_HEADER, BLOB_ALIGN
    else
      return "bad_layout"
    end
    if i == 1 and (e.offset ~= 0 or st == nil) then return "bad_layout" end
    if e.offset < prev_end then return "bad_layout" end
    if e.offset % align ~= 0 then return "bad_layout" end
    if e.offset + size > h.data_length then return "bad_layout" end
    prev_end = e.offset + size
  end
  return nil
end

-- PASS TWO's state (§7.4). There is none per node: an entry's type id says
-- which walk to run. base and extent are the holder's own bounds for the
-- element-array and map-slot clauses; arrays remembers what is already placed
-- in that node so no two overlap. All reset per node.
local function new_scan(h, dir)
  return {
    h = h,
    data = string.sub(h.bytes, h.data_offset + 1, h.data_offset + h.data_length),
    dir = dir,
    by_offset = nil,
  }
end

local function index_offsets(s)
  if s.by_offset == nil then
    s.by_offset = {}
    for i, e in ipairs(s.dir) do
      s.by_offset[e.offset] = i
    end
  end
  return s.by_offset
end

local function array_extent(at, delta, count, size, base, extent)
  if base < 0 or extent < base or at < base or at > extent or count < 0 or size <= 0 then return nil end
  if delta < base - at or delta > extent - at then return nil end
  local start = at + delta
  if count > (extent - start) // size then return nil end
  return start, start + count * size
end

local function placed_array(s, at, size, align)
  local delta = u64(s.data, at)
  local count = i32(s.data, at + 8)
  if count < 0 then return nil end
  if delta == 0 then
    if count ~= 0 then return nil end
    return 0, 0
  end
  if count == 0 then return nil end
  local start, finish = array_extent(at, delta, count, size, s.base, s.extent)
  if start == nil then return nil end
  if start % align ~= 0 then return nil end
  for _, other in ipairs(s.arrays) do
    if start < other.finish and other.start < finish then return nil end
  end
  s.arrays[#s.arrays + 1] = { start = start, finish = finish }
  return start, count
end

local function companion(s, at, bound)
  local n = i32(s.data, at)
  if n < 0 or n > bound then return false end
  return true
end

local record, field, element, ref, key_order

-- A REFERENCE (§7.4 clause 1): zero is null; any other delta resolves to
-- slot + delta, which the directory must NAME, and the entry's type id must be
-- the one the declaration requires. Being named is what makes it aligned and
-- inside the region, pass one having proved both.
ref = function(s, at, f)
  local delta = u64(s.data, at)
  if delta == 0 then return true end
  local target = at + delta
  local by_offset = index_offsets(s)
  local slot = by_offset[target]
  if slot == nil then return false end
  local want
  if f.shape == "string_blob" then
    want = s.h.T.StringBlobTypeId
  elseif f.shape == "bytes_blob" then
    want = s.h.T.BytesBlobTypeId
  else
    want = f.target_id
  end
  return s.dir[slot].type_id == want
end

-- A UNION TAG (§7.4 clause 3): the one field VALUE the scan reads, because it
-- chooses which arm's storage is live; a tag past the last arm refuses. A set
-- arm's field line is checked where a field's is, and a body arm's record is
-- walked.
local function union(s, at, f)
  local un = s.h.T.Unions[f.target]
  if un == nil then return false end
  local tag
  if un.tag == 1 then
    tag = string.unpack("<I1", s.data, at + 1)
  elseif un.tag == 2 then
    tag = string.unpack("<I2", s.data, at + 1)
  elseif un.tag == 4 then
    tag = string.unpack("<I4", s.data, at + 1)
  else
    tag = string.unpack("<I8", s.data, at + 1)
  end
  if tag == 0 then return true end
  if tag > #un.arms then return false end
  local arm = un.arms[tag]
  if arm.field == nil then return true end
  local armAt = at + un.arm_offset
  if arm.field.shape == "table" then return record(s, armAt, s.h.T.Tables[arm.field.target]) end
  return field(s, armAt, arm.field)
end

-- one ELEMENT of an array (§7.4): a pointer's slot, a record, or a union's
-- storage; a float is tested for finiteness and any other scalar steers
-- nothing.
element = function(s, at, elem)
  if elem == nil then return true end
  if elem.shape == "float" then
    -- A FLOAT IS FINITE (docs/SPEC-TABLES.md §3.3): the check is the cook's
    -- strict validator (§7), and it refuses a NaN or an infinity on its raw
    -- bits as non_finite_float, as every read does.
    local bad
    if elem.f32 then
      bad = string.unpack("<I4", s.data, at + 1) & 0x7f800000 == 0x7f800000
    else
      bad = string.unpack("<i8", s.data, at + 1) & 0x7ff0000000000000 == 0x7ff0000000000000
    end
    if bad then
      s.nonFinite = true
      return false
    end
    return true
  end
  if elem.shape == "pointer" or elem.shape == "string_blob" or elem.shape == "bytes_blob" then
    return ref(s, at, elem)
  elseif elem.shape == "string" or elem.shape == "bytes" or elem.shape == "wstring" then
    return companion(s, at + elem.companion, elem.bound)
  elseif elem.shape == "table" then
    return record(s, at, s.h.T.Tables[elem.target])
  elseif elem.shape == "union" then
    return union(s, at, elem)
  end
  return true
end

local function slots(s, at, f, n)
  local step = f.elem_step
  for i = 0, n - 1 do
    if not element(s, at + i * step, f.elem) then return false end
  end
  return true
end

-- THE MAP-SLOT and ELEMENT-ARRAY clauses (§7.4 clauses 4 and 5): the slot's
-- sixteen bytes hold an int64 self-relative delta and an int32 count; the
-- array must be contained, aligned, fit and overlap no other array in this
-- node. A map's keys are then read strictly ascending with no repeat, because
-- a cook's Find is a binary search over them.
local function list(s, at, f)
  local start, count = placed_array(s, at, f.elem_size, f.elem_align)
  if start == nil or count == 0 then return start ~= nil end
  return slots(s, start, f, count)
end

local function map_slot(s, at, f)
  local entry = s.h.T.Tables[f.entry]
  if entry == nil then return false end
  local start, count = placed_array(s, at, entry.size, entry.align)
  if start == nil or count == 0 then return start ~= nil end
  for i = 0, count - 1 do
    if not record(s, start + i * entry.size, entry) then return false end
  end
  local key
  for _, fl in ipairs(entry.fields) do
    if fl.name == f.key_field.name then key = fl end
  end
  if key == nil then return false end
  for i = 1, count - 1 do
    local order = key_order(s, key, entry, start + (i - 1) * entry.size, start + i * entry.size)
    if order == nil or order >= 0 then return false end
  end
  return true
end

-- key_order compares two stored map keys: a bounded string by its used bytes,
-- an integer at its own signedness.
local function used_bytes(s, key, _entry, at)
  at = at + key.at
  local n = i32(s.data, at + key.companion)
  if n < 0 then n = 0 end
  local cap = key.bound + 1
  if n > cap then n = cap end
  return string.sub(s.data, at + 1, at + n)
end

local function cmp_u64(x, y)
  if x == y then return 0 end
  if math.ult(x, y) then return -1 end
  return 1
end

key_order = function(s, key, entry, a, b)
  if key.shape == "string" then
    local x = used_bytes(s, key, entry, a)
    local y = used_bytes(s, key, entry, b)
    local n = math.min(#x, #y)
    for i = 1, n do
      if x:byte(i) ~= y:byte(i) then
        if x:byte(i) < y:byte(i) then return -1 end
        return 1
      end
    end
    if #x < #y then return -1 end
    if #x > #y then return 1 end
    return 0
  end
  local at = a + key.at
  local bt = b + key.at
  local width = key.width
  local x, y
  if width == 1 then
    x, y = string.unpack("<I1", s.data, at + 1), string.unpack("<I1", s.data, bt + 1)
  elseif width == 2 then
    x, y = string.unpack("<I2", s.data, at + 1), string.unpack("<I2", s.data, bt + 1)
  elseif width == 4 then
    x, y = string.unpack("<I4", s.data, at + 1), string.unpack("<I4", s.data, bt + 1)
  else
    x, y = u64(s.data, at), u64(s.data, bt)
  end
  if key.signed then
    if x < y then return -1 end
    if x > y then return 1 end
    return 0
  end
  return cmp_u64(x, y)
end

-- one FIELD (§7.4): map slot, pointer slot, count companion, bounded or
-- keyed array, element array, or a scalar that steers nothing.
field = function(s, at, f)
  if f == nil then return true end
  local shape = f.shape
  if shape == "map" then
    return map_slot(s, at, f)
  elseif shape == "pointer" or shape == "string_blob" or shape == "bytes_blob" then
    return ref(s, at, f)
  end
  if shape == "array" then
    if f.list then return list(s, at, f) end
    local n = f.bound
    if f.fixed or f.keyed ~= nil then
      if not slots(s, at, f, n) then return false end
    else
      if not slots(s, at, f, n) then return false end
      if not companion(s, at + f.companion, f.bound) then return false end
    end
    return true
  end
  if shape == "string" or shape == "bytes" or shape == "wstring" then return companion(s, at + f.companion, f.bound) end
  return element(s, at, f)
end

-- one RECORD's declaration: every field, including those nested by value, at
-- its own offset (§7.4's "the companions of fixed-size tables and plain types
-- nested by value").
record = function(s, base, st)
  if st == nil then return false end
  for _, f in ipairs(st.fields) do
    if not field(s, base + f.at, f) then return false end
  end
  return true
end

-- one NODE. A BYTE BUFFER's node has one count companion — its length — which
-- with the header (and a string's terminator) must fit its own extent (§7.4
-- clause 2).
local function node(s, base, extent, type_id, st)
  if st == nil then
    local length = u32(s.data, base)
    local need = BLOB_HEADER + length
    if type_id == s.h.T.StringBlobTypeId then need = need + 1 end
    return need <= extent
  end
  s.base, s.extent, s.arrays = base, base + extent, {}
  return record(s, base, st)
end

-- PASS TWO (§7.4): every node, in directory order, and no reference is
-- followed, so no forged aliasing or cycle costs anything.
local function scan(h, dir)
  local s = new_scan(h, dir)
  local by_id = {}
  for _, d in pairs(h.T.Tables) do
    by_id[d.type_id] = d
  end
  for i, e in ipairs(dir) do
    local extent = h.data_length - e.offset
    if i < #dir then extent = dir[i + 1].offset - e.offset end
    if not node(s, e.offset, extent, e.type_id, by_id[e.type_id]) then
      if s.nonFinite then return "non_finite_float" end
      return "bad_layout"
    end
  end
  return nil
end

-- C.Open is the runtime's MATCH AND POINT (§7, §7.1): the header's clauses in
-- their order, then the alignment of the BASE, and it answers a handle or nil
-- and the TableRefuseReason name. It reads nothing past the header, so it is
-- O(1) whatever the cook holds: the attribution part and the region are
-- cook-check's to bound (§7.4: "`schema cook-check`'s refusals are the
-- tool's, not the runtime's"), and C.Check below runs them.
--
-- A Lua string has no address a caller can observe or place, so the BASE's
-- placement is an argument, as the Elixir leg's `lead` is (§7, the reading
-- tier): base_offset is how many bytes past an aligned base the caller's
-- buffer begins, nil or 0 for an aligned one. No buffer at all is the
-- reference's null base, unaligned_base, which open_header answers first.
function C.Open(T, name, bytes, base_offset)
  local h, why = open_header(T, name, bytes)
  if h == nil then return nil, why end
  -- THE ALIGNMENT OF THE BASE: the header pads the data part to the region's
  -- alignment, and the alignment divides 64, so the derived data offset
  -- carries the property from the file's base to the region's. A base that is
  -- not aligned is the CALLER's placement, not the file's.
  if (base_offset or 0) % h.alignment ~= 0 then return nil, "unaligned_base" end
  h.root = T.Tables[name]
  return h
end

-- C.Check is `schema cook-check`'s scan (§7.4) on the Lua leg: Open's clauses,
-- then pass one over the directory and pass two over the region, in their
-- order, answering a handle or nil and the TableRefuseReason name. It is the
-- offline check a caller runs once over a cook it did not write; Open never
-- runs it.
function C.Check(T, name, bytes, base_offset)
  local h, why = C.Open(T, name, bytes, base_offset)
  if h == nil then return nil, why end
  -- the tool's header check (internal/tablecook ReadHeader, §7.1): the data
  -- part is a whole number of the region's alignment, which is what puts the
  -- attribution part on an aligned boundary. Open does not read it (it points
  -- and does no arithmetic with it); the offline check owes it. The fuzz
  -- oracle's first run found Check opening a cook whose alignment word had
  -- been forged to a larger power of two.
  if h.data_length % h.alignment ~= 0 then return nil, "bad_layout" end
  local dir, dw = directory(h)
  if dir == nil then return nil, dw end
  local cw = check_directory(h, dir)
  if cw ~= nil then return nil, cw end
  local sw = scan(h, dir)
  if sw ~= nil then return nil, sw end
  h.dir = dir
  return h
end

return C
