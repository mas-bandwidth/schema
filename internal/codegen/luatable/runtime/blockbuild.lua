-- blockbuild.lua — the Lua table runtime's BLOCK FORM builder
-- (docs/SPEC-TABLES.md §19, §19.1). Emitted beside the unit's
-- <Package>Tables.lua by internal/codegen/luatable, the same glob that emits
-- every other runtime file.
--
-- The block form is a THIRD projection of a fixed table (§2.7): a projection
-- record at offset 0 of a 64-byte aligned extent, then each out-of-line
-- array's rows in declaration order. This file writes what Go's generated
-- builder writes (§19.1): the three-word prologue, and one sixteen-byte
-- (offset_of, count, stride) triple per out-of-line array. It writes no row
-- and no padding: the tail is UNSPECIFIED and rows past `count` belong to the
-- caller. Begin is O(out-of-line arrays) and touches no row.
--
-- The layout arithmetic is the C ABI walk ir/blocklayout.go defines: every
-- field at its own alignment, the projection's prologue 24 bytes, each
-- out-of-line array starting at max(64, alignof(element)), and the used extent
-- rounded up to 64. The descriptors carry the record sizes and alignments, so
-- this walk needs no second model.
local B = {}

-- the magic is 0x4B4C42414D484353, "SCHMABLK" in a little-endian store
-- (docs/SPEC-TABLES.md §19.1)
local MAGIC = 0x4b4c42414d484353
local PROLOGUE = 24
local BLOCK_ALIGN = 64
local LITTLE = 1

local function align_up(n, a)
  if a <= 1 then return n end
  local r = n % a
  if r ~= 0 then n = n + (a - r) end
  return n
end

local function pack_u32(v)
  return string.pack("<I4", v)
end
local function pack_u64(v)
  return string.pack("<I8", v)
end

-- storage_bits_for is ir.StorageBitsFor: the smallest unsigned width holding
-- max (§5.1), and an enum's own storage width.
local function storage_bits_for(max)
  if max <= 0xff then return 8 end
  if max <= 0xffff then return 16 end
  if max <= 0xffffffff then return 32 end
  return 64
end

-- element_size is one value of a descriptor's declared type: a scalar's
-- storage width, or a nested record's computed size and alignment
-- (docs/SPEC-TABLES.md §19.3). An array element uses the descriptor's own
-- elem_size/elem_align where it has them (ir.ListElementLayout).
local function element_size(T, d)
  local s = d.shape
  if s == "bool" then return 1, 1 end
  if s == "int" or s == "flags" then
    local w = d.width or 8
    return w, w
  end
  if s == "float" then
    if d.f32 then return 4, 4 end
    return 8, 8
  end
  if s == "enum" then
    -- an enum's storage is the smallest unsigned width holding its top value;
    -- the descriptor lists the variants, and a widened enum's bound is not a
    -- block-form fact this runtime can see, so the count of variants is the
    -- storage width's own witness for today's corpus
    local w = math.floor(storage_bits_for(#d.variants) / 8)
    return w, w
  end
  if s == "table" then
    local sub = T.Tables[d.target]
    if sub ~= nil then return sub.size, sub.align end
    return 0, 1
  end
  return 1, 1
end

-- out_of_line is ir.BlockOutOfLine (§2.7, "DEPTH ONE, BOUNDED ONLY"): the
-- table's own bounded `[..N]T` of a struct/type moves its storage out of the
-- projection and into the triple. A fixed `[N]T`, an enum-keyed `[E]T` and an
-- unbounded `[]T` do not.
local function out_of_line(d)
  return d.shape == "array"
    and not d.fixed
    and not d.list
    and d.keyed == nil
    and d.elem ~= nil
    and d.elem.shape == "table"
end

-- pieces is ir.fieldPieces (§19.3): a field's contiguous storage members in
-- order, each at its own alignment. A string is a buffer and an int32 length;
-- a counted array is its elements and an int32 count; an optional adds a
-- one-byte presence companion. `projection` selects the block spelling, where
-- an out-of-line array is its sixteen-byte triple.
local function pieces(T, d, projection)
  if projection and out_of_line(d) then return { { size = 16, align = 8 } } end
  local out = {}
  local s = d.shape
  if s == "map" then
    out[#out + 1] = { size = 16, align = 8 }
  elseif s == "pointer" or s == "string_blob" or s == "bytes_blob" then
    out[#out + 1] = { size = 8, align = 8 }
  elseif s == "string" then
    out[#out + 1] = { size = d.bound + 1, align = 1 }
    out[#out + 1] = { size = 4, align = 4 }
  elseif s == "wstring" then
    out[#out + 1] = { size = (d.bound + 1) * 2, align = 2 }
    out[#out + 1] = { size = 4, align = 4 }
  elseif s == "bytes" then
    out[#out + 1] = { size = d.bound, align = 1 }
    out[#out + 1] = { size = 4, align = 4 }
  elseif s == "array" then
    local es, ea
    if d.elem_size ~= nil and d.elem_align ~= nil then
      es, ea = d.elem_size, d.elem_align
    elseif d.elem ~= nil then
      es, ea = element_size(T, d.elem)
    else
      es, ea = 0, 1
    end
    if d.list then
      -- an unbounded array is the map's sixteen-byte slot (§2.9)
      out[#out + 1] = { size = 16, align = 8 }
    else
      out[#out + 1] = { size = es * d.bound, align = ea }
      if not d.fixed and d.keyed == nil then out[#out + 1] = { size = 4, align = 4 } end
    end
  else
    local es, ea = element_size(T, d)
    out[#out + 1] = { size = es, align = ea }
  end
  if d.optional then out[#out + 1] = { size = 1, align = 1 } end
  return out
end

-- projection lays out a table's PROJECTION record (§19.1): the prologue and
-- then the table's own fields at their natural offsets, with every
-- out-of-line array's storage replaced in place by its triple. It returns the
-- record size, the greatest alignment, each field's offset, and the arrays in
-- declaration order.
local function projection(T, desc)
  local offset = PROLOGUE
  local max_align = 8
  local fields = {}
  local arrays = {}
  for _, d in ipairs(desc.fields) do
    local start = -1
    local field_align = 1
    for _, p in ipairs(pieces(T, d, true)) do
      if p.align > max_align then max_align = p.align end
      if p.align > field_align then field_align = p.align end
      offset = align_up(offset, p.align)
      if start < 0 then start = offset end
      offset = offset + p.size
    end
    if start >= 0 then
      fields[d.name] = { offset = start, size = offset - start, align = field_align }
      if out_of_line(d) then
        arrays[#arrays + 1] =
          { name = d.name, triple = start, max = d.bound, stride = d.elem_size, align = d.elem_align }
      end
    end
  end
  local size = align_up(offset, max_align)
  if size == 0 then size = 1 end
  return size, max_align, fields, arrays
end

-- strided alignment is ir.elemAlignOf: a standard-layout record's alignment
-- is the greatest power of two dividing its stride, bounded by 16 (§19.1).
local function stride_align(stride)
  local a = 1
  while a < 16 and stride % (a * 2) == 0 do
    a = a * 2
  end
  return a
end

-- B.BlockMaxBytes is <Table>BlockMaxBytes (§19.1): the projection plus every
-- out-of-line array at its declared maximum, one extent, allocated once.
function B.BlockMaxBytes(T, root)
  local desc = T.Tables[root]
  if desc == nil then return nil, "blockbuild: no table descriptor for " .. tostring(root) end
  local size, _, _, arrays = projection(T, desc)
  local offset = size
  for _, a in ipairs(arrays) do
    local align = math.max(BLOCK_ALIGN, stride_align(a.stride))
    offset = align_up(offset, align) + a.max * a.stride
  end
  return align_up(offset, BLOCK_ALIGN)
end

-- B.Begin writes the prologue and the triples, and nothing else (§19.1). It
-- refuses a count past its declared maximum, naming the array, its count and
-- its maximum; it never clamps. Storage is the string it returns -- the block
-- itself -- of the used extent, and rows past each count are unspecified.
--
-- The module carries the unit's build version in T.BuildVersion (the constant
-- §20 defines); a producer without one has not stamped the prologue and is
-- refused.
function B.Begin(T, root, counts)
  local desc = T.Tables[root]
  if desc == nil then return nil, "blockbuild: no table descriptor for " .. tostring(root) end
  if T.BuildVersion == nil then
    return nil,
      "blockbuild: T.BuildVersion is nil; the block prologue carries the unit's build version"
        .. " (docs/SPEC-TABLES.md §19.1)"
  end
  counts = counts or {}
  local size, _, _, arrays = projection(T, desc)

  for _, a in ipairs(arrays) do
    local count = counts[a.name]
    if count == nil then count = 0 end
    if math.type(count) ~= "integer" or count < 0 or count > a.max then
      return nil, { array = a.name, count = count, maximum = a.max }
    end
  end

  local writes = {
    { offset = 0, bytes = pack_u64(MAGIC) },
    { offset = 8, bytes = pack_u64(T.BuildVersion) },
    { offset = 16, bytes = pack_u64(LITTLE) },
  }
  local offset = size
  local rows = {}
  for _, a in ipairs(arrays) do
    local count = counts[a.name] or 0
    local align = math.max(BLOCK_ALIGN, stride_align(a.stride))
    offset = align_up(offset, align)
    rows[#rows + 1] = { name = a.name, offset = offset, count = count, stride = a.stride }
    writes[#writes + 1] = { offset = a.triple, bytes = pack_u64(offset) .. pack_u32(count) .. pack_u32(a.stride) }
    offset = offset + count * a.stride
  end
  local used = align_up(offset, BLOCK_ALIGN)

  local parts = {}
  local at = 0
  for _, w in ipairs(writes) do
    if w.offset > at then
      parts[#parts + 1] = string.rep("\0", w.offset - at)
      at = w.offset
    end
    parts[#parts + 1] = w.bytes
    at = at + #w.bytes
  end
  if used > at then parts[#parts + 1] = string.rep("\0", used - at) end

  return {
    root = root,
    bytes = table.concat(parts),
    projection = size,
    arrays = rows,
    build_version = T.BuildVersion,
  }
end

-- B.BlockBytes is <Table>BlockBytes (block) (§19.1): the used extent, the
-- greatest offset_of + count * stride rounded up to 64.
function B.BlockBytes(block)
  if block == nil or block.bytes == nil then return 0 end
  return #block.bytes
end

return B
