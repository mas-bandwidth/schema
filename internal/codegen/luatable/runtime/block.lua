-- block.lua — the Lua table runtime's BLOCK FORM, the READ half
-- (docs/SPEC-TABLES.md §19.2, §19.3). It is emitted beside the unit's
-- <Package>Tables.lua by internal/codegen/luatable, which embeds runtime/*.lua
-- by glob.
--
-- Nothing declares the form: every fixed table has one, and a consumer that
-- reads a block calls BlockOpen. The bytes are memory another build wrote, so
-- Open checks the prologue and every array's (offset_of, count, stride) once,
-- points at them, and copies nothing. The projection and row layouts are
-- derived here from the SAME descriptors the other table runtimes walk (§8.1,
-- §19.2): a record is the C ABI's natural layout, each field at its own
-- alignment, the record's size rounded up to the greatest of them (§19.3).
--
-- The block form's base is 64-byte aligned (§19.1). Lua strings have no
-- address, so the base's alignment is the caller's optional byte offset into
-- whatever buffer the string is a view of; a non-zero, non-multiple-of-64
-- offset is `unaligned_base`, exactly as a C# view's byteOffset or a Go
-- pointer is checked.
local B = {}

local MAGIC = 0x4b4c42414d484353
local FOREIGN_MAGIC = 0x5343484d41424c4b
local PROLOGUE = 24
local BLOCK_ALIGN = 64

-- §19.1: the prologue's byte_order word is THE PRODUCER'S OWN order, 1 little
-- and 2 big, and BlockOpen refuses a word that disagrees with this build's.
-- Lua's "=" format prefix is the host's order, so every read below is native
-- and a big-endian host decodes a big-endian block without a single case.
local NATIVE_ORDER = string.pack("I2", 1):byte(1) == 1 and 1 or 2
B.ByteOrder = NATIVE_ORDER

local function align_up(n, a)
  return (n + a - 1) // a * a
end

local function max_align(a, b)
  if a > b then return a end
  return b
end

-- elem_of is one VALUE of a field's declared type: the scalar itself, or an
-- array's element. It is the §19.3 model's leaf, and its spelling is the
-- descriptor's own shape.
local function elem_of(T, d)
  local s = d.shape
  if s == "bool" then
    return { size = 1, align = 1 }
  elseif s == "float" then
    if d.f32 then return { size = 4, align = 4 } end
    return { size = 8, align = 8 }
  elseif s == "int" then
    if d.timestamp then return { size = 8, align = 8 } end
    -- a bits(N) slot is a uint32 up to 32 bits and a uint64 past them, whatever
    -- its wire width (ir.elementPiece); the descriptor's width is the wire's
    if d.bits ~= nil then
      local w = d.bits <= 32 and 4 or 8
      return { size = w, align = w }
    end
    local w = d.width
    return { size = w, align = w < 8 and w or 8 }
  elseif s == "wide" then
    -- a 128-bit integer, and a fixed of 128 bits, is sixteen bytes AT SIXTEEN:
    -- the C ABI's natural alignment for a 128-bit integer (ir.elementPiece,
    -- docs/SPEC-TABLES.md §7.2, §19.3). Found by the layout agreement gate.
    return { size = d.width, align = d.width }
  elseif s == "enum" then
    -- A block row's enum slot holds the ordinal at the enum's own derived
    -- storage width; the descriptor's kind is the table-wire kind and has no
    -- fixed width at all (§19.2). The fixed form carries the same width when
    -- it is emitted; otherwise the smallest unsigned fitting the variant
    -- count is what §2.4 derives.
    local w = T.FixedEnums and T.FixedEnums[d.enum]
    if w == nil then
      local n = #(d.variants or {})
      if n <= 0xff then
        w = 1
      elseif n <= 0xffff then
        w = 2
      elseif n <= 0xffffffff then
        w = 4
      else
        w = 8
      end
    end
    return { size = w, align = w }
  elseif s == "flags" then
    return { size = 8, align = 8 }
  elseif s == "table" then
    local row = T.Tables[d.target]
    if row == nil then error("block: no descriptor for record " .. tostring(d.target), 0) end
    return { size = row.size, align = row.align }
  elseif s == "pointer" or s == "string_blob" or s == "bytes_blob" then
    return { size = 8, align = 8 }
  end
  return { size = 1, align = 1 }
end

-- out_of_line is §19's DEPTH ONE, BOUNDED ONLY rule: only the block-form
-- TABLE'S OWN bounded arrays of records move out of line. A fixed [N]T, an
-- enum-keyed [E]T, and every array inside a row stay exactly where they are.
local function out_of_line(f)
  return f.shape == "array" and not f.list and not f.fixed and f.elem ~= nil and f.elem.shape == "table"
end

-- pieces spells one field's storage as the contiguous members a C record
-- declares, in order: a string is a buffer AND its used length, a counted
-- array its elements AND its count, an optional a presence byte. The
-- projection replaces an out-of-line array with its sixteen-byte triple.
local function pieces(T, f, projection)
  if projection and out_of_line(f) then return { { size = 16, align = 8 } } end
  local p = {}
  local s = f.shape
  if s == "map" then
    p[#p + 1] = { size = 16, align = 8 }
  elseif s == "pointer" or s == "string_blob" or s == "bytes_blob" then
    p[#p + 1] = { size = 8, align = 8 }
  elseif s == "string" then
    p[#p + 1] = { size = f.bound + 1, align = 1 }
    p[#p + 1] = { size = 4, align = 4 }
  elseif s == "wstring" then
    p[#p + 1] = { size = 2 * (f.bound + 1), align = 2 }
    p[#p + 1] = { size = 4, align = 4 }
  elseif s == "bytes" then
    p[#p + 1] = { size = f.bound, align = 1 }
    p[#p + 1] = { size = 4, align = 4 }
  elseif s == "array" then
    if f.list then
      p[#p + 1] = { size = 16, align = 8 }
    else
      local e = elem_of(T, f.elem)
      local slots = f.keyed and #f.keyed or f.bound
      p[#p + 1] = { size = e.size * slots, align = e.align }
      if not f.fixed then p[#p + 1] = { size = 4, align = 4 } end
    end
  else
    local e = elem_of(T, f)
    p[#p + 1] = { size = e.size, align = e.align }
  end
  if f.optional then p[#p + 1] = { size = 1, align = 1 } end
  return p
end

-- Layout derives one record's C ABI layout from the descriptors: a projection
-- opens with the generated 24-byte prologue (§19.1), every row is the record
-- its descriptor names. It is the §19.3 model, not the wire's packed one.
function B.Layout(T, name, projection)
  local d = T.Tables[name]
  if d == nil then error("block: no descriptor for table " .. tostring(name), 0) end
  local offset = projection and PROLOGUE or 0
  local record_align = projection and 8 or 1
  local fields = {}
  for _, f in ipairs(d.fields) do
    local ps = pieces(T, f, projection)
    local start = nil
    local field_align = 1
    local at = offset
    local offsets = {}
    for i, p in ipairs(ps) do
      if p.align > record_align then record_align = p.align end
      if p.align > field_align then field_align = p.align end
      at = align_up(at, p.align)
      if start == nil then start = at end
      offsets[i] = { offset = at, size = p.size }
      at = at + p.size
    end
    offset = at
    if start == nil then
      -- an empty record is one byte in both languages; a field that spells
      -- no piece still has its own position
      start = at
    end
    local fl = {
      f = f,
      offset = start,
      size = at - start,
      align = field_align,
      pieces = offsets,
      outOfLine = projection and out_of_line(f),
      isArray = false,
      counted = false,
      optional = f.optional == true,
      bound = 0,
      elemSize = 0,
      countOffset = nil,
      presentOffset = nil,
      element = nil,
    }
    if fl.optional then fl.presentOffset = offsets[#offsets].offset end
    if fl.outOfLine then
      fl.isArray = true
      fl.counted = true
      fl.bound = f.bound
      fl.elemSize = 0
      fl.countOffset = offsets[1].offset + 8
    elseif f.shape == "string" or f.shape == "wstring" or f.shape == "bytes" then
      fl.counted = true
      fl.isArray = f.shape == "bytes"
      fl.bound = f.bound
      fl.elemSize = f.shape == "wstring" and 2 or 1
      fl.countOffset = offsets[2].offset
      fl.element = nil
    elseif f.shape == "array" and not f.list then
      fl.isArray = true
      fl.bound = f.keyed and #f.keyed or f.bound
      local e = elem_of(T, f.elem)
      fl.elemSize = e.size
      if f.elem.shape == "table" then fl.element = B.Layout(T, f.elem.target, false) end
      if not f.fixed then
        fl.counted = true
        fl.countOffset = offsets[2].offset
      end
    elseif f.shape == "table" then
      fl.elemSize = elem_of(T, f).size
      fl.element = B.Layout(T, f.target, false)
    else
      fl.elemSize = (offsets[1] and offsets[1].size) or 0
    end
    fields[#fields + 1] = fl
  end
  local size = align_up(offset, record_align)
  if size == 0 then size = 1 end
  return { name = name, size = size, align = record_align, fields = fields }
end

-- BlockOpen checks once and points, and this is the WHOLE check, in §19.2's
-- order: the prologue's magic read bytewise, the byte order it establishes,
-- the build version against this build's own, then each array's pitch, its
-- count against the declared maximum, its offset and its extent inside the
-- block, THEN the used extent against the bytes the caller passed, and LAST
-- the base's alignment — the only clause that reads nothing out of the block.
-- On a failure it returns nil and the refusal's name (§7's vocabulary).
function B.BlockOpen(T, name, bytes, build_version, base_offset)
  if type(bytes) ~= "string" then return nil, "truncated" end
  if build_version == nil then build_version = T.BuildVersion end
  local layout = B.Layout(T, name, true)

  -- the alignment clause is last, but a nil base is what a non-string is:
  -- there is no block to read and no offset to measure
  if #bytes < layout.size then return nil, "truncated" end
  local magic = string.unpack("=I8", bytes, 1)
  if magic ~= MAGIC then
    if magic == FOREIGN_MAGIC then return nil, "foreign_order" end
    return nil, "not_a_cook"
  end
  if string.unpack("=I8", bytes, 17) ~= NATIVE_ORDER then return nil, "not_a_cook" end
  if build_version == nil or string.unpack("=I8", bytes, 9) ~= build_version then return nil, "wrong_build_version" end

  local used = layout.size
  local arrays = {}
  for _, fl in ipairs(layout.fields) do
    if fl.outOfLine then
      local triple = fl.offset
      local offset_of = string.unpack("=I8", bytes, triple + 1)
      local count = string.unpack("=I4", bytes, triple + 9)
      local stride = string.unpack("=I4", bytes, triple + 13)
      local row = B.Layout(T, fl.f.elem.target, false)
      if stride ~= row.size then return nil, "bad_layout" end
      if count > fl.f.bound then return nil, "bad_layout" end
      local elem_align = 1
      while elem_align < 16 and stride % (elem_align * 2) == 0 do
        elem_align = elem_align * 2
      end
      local start_align = max_align(BLOCK_ALIGN, elem_align)
      if offset_of < layout.size or offset_of % start_align ~= 0 then return nil, "bad_layout" end
      if offset_of > #bytes then return nil, "bad_layout" end
      local rows = count * stride -- both bounded above: this cannot carry
      if rows > #bytes - offset_of then return nil, "bad_layout" end
      local ending = offset_of + rows
      if ending > used then used = ending end
      arrays[#arrays + 1] = {
        field = fl.f,
        offsetOf = offset_of,
        count = count,
        stride = stride,
        layout = row,
      }
    end
  end
  local padding = (BLOCK_ALIGN - (used % BLOCK_ALIGN)) % BLOCK_ALIGN
  if padding > #bytes - used then return nil, "truncated" end
  used = used + padding
  base_offset = base_offset or 0
  if base_offset % BLOCK_ALIGN ~= 0 then return nil, "unaligned_base" end
  return {
    name = name,
    bytes = bytes,
    buildVersion = build_version,
    layout = layout,
    arrays = arrays,
    used = used,
    baseOffset = base_offset,
  }
end

B.Open = B.BlockOpen

-- ---- the reflection (docs/SPEC-TABLES.md §8, §19.2) ----

local function cook_text(bytes, at, used)
  local out = { '"' }
  for i = 0, used - 1 do
    local c = bytes:byte(at + i + 1)
    if c >= 0x20 and c < 0x7f and c ~= 0x22 and c ~= 0x5c then
      out[#out + 1] = string.char(c)
    else
      out[#out + 1] = string.format("\\x%02x", c)
    end
  end
  out[#out + 1] = '" len=' .. used
  return table.concat(out)
end

-- dump_scalar spells one value as the canonical ROW DUMP does: a FLOAT is the
-- IEEE-754 bit pattern of a finite value (a block row is a byte-identical
-- projection; FORMAT.md), an enum its ordinal at elem_size, and every integer
-- its signed or unsigned width. A float whose bits are a NaN or an infinity
-- is never spelled (docs/SPEC-TABLES.md §3.3, ruling 2 of 2026-10-27): nil,
-- and the caller refuses it by name.
local function dump_scalar(bytes, at, kind, width)
  local p = at + 1
  if kind == 1 then
    return bytes:byte(p) ~= 0 and "true" or "false"
  elseif kind == 10 then
    local b = string.unpack("=I4", bytes, p)
    if b & 0x7f800000 == 0x7f800000 then return nil end
    return string.format("0x%08x", b)
  elseif kind == 11 then
    local b = string.unpack("=i8", bytes, p)
    if b & 0x7ff0000000000000 == 0x7ff0000000000000 then return nil end
    return string.format("0x%016x", b)
  elseif kind >= 2 and kind <= 5 then
    local v
    if width == 1 then
      v = string.unpack("=i1", bytes, p)
    elseif width == 2 then
      v = string.unpack("=i2", bytes, p)
    elseif width == 4 then
      v = string.unpack("=i4", bytes, p)
    else
      v = string.unpack("=i8", bytes, p)
    end
    return string.format("%d", v)
  end
  local v
  if width == 1 then
    v = string.unpack("=I1", bytes, p)
  elseif width == 2 then
    v = string.unpack("=I2", bytes, p)
  elseif width == 4 then
    v = string.unpack("=I4", bytes, p)
  else
    v = string.unpack("=I8", bytes, p)
  end
  return string.format("%d", v)
end

local function join(prefix, name)
  if prefix == "" then return name end
  return prefix .. "." .. name
end

local function dump_record(out, bytes, base, layout, prefix)
  for _, fl in ipairs(layout.fields) do
    if not fl.outOfLine then
      local name = join(prefix, fl.f.name)
      if fl.counted then
        -- a string, a bytes, or a counted array's used length companion
        local used = string.unpack("=i4", bytes, base + fl.countOffset + 1)
        if used < 0 or used > fl.bound then
          error(
            "block: "
              .. layout.name
              .. "."
              .. fl.f.name
              .. " carries a used length of "
              .. tostring(used)
              .. ", outside [ 0, "
              .. fl.bound
              .. " ]",
            0
          )
        end
        out[#out + 1] = "  " .. name .. " = " .. cook_text(bytes, base + fl.offset, used)
      else
        local slots = fl.isArray and fl.bound or 1
        for s = 0, slots - 1 do
          local at = base + fl.offset + s * fl.elemSize
          local slot = fl.isArray and (name .. "[" .. s .. "]") or name
          if fl.element ~= nil then
            dump_record(out, bytes, at, fl.element, slot)
          else
            -- an array slot is spelled by its ELEMENT's kind: a float
            -- array's slot is a float's bit pattern, never an integer
            local kind = fl.isArray and (fl.f.elem_kind or fl.f.kind) or fl.f.kind
            local text = dump_scalar(bytes, at, kind, fl.elemSize)
            if text == nil then error("non_finite_float: " .. layout.name .. "." .. fl.f.name, 0) end
            out[#out + 1] = "  " .. slot .. " = " .. text
          end
        end
      end
      if fl.optional then
        local present = bytes:byte(base + fl.presentOffset + 1) ~= 0
        out[#out + 1] = "  " .. name .. "#present = " .. tostring(present)
      end
    end
  end
end

-- B.Dump is the canonical row dump of an opened block (FORMAT.md): the
-- projection's own fields, then every out-of-line array in declaration order,
-- row by row, every value read through the descriptors and nothing else.
function B.Dump(handle)
  local out = { "projection " .. handle.name .. " @0" }
  dump_record(out, handle.bytes, 0, handle.layout, "")
  for _, a in ipairs(handle.arrays) do
    out[#out + 1] =
      string.format("array %s %s @%d count=%d stride=%d", a.field.name, a.layout.name, a.offsetOf, a.count, a.stride)
    for r = 0, a.count - 1 do
      local at = a.offsetOf + r * a.stride
      out[#out + 1] = string.format("row %d @%d", r, at)
      dump_record(out, handle.bytes, at, a.layout, "")
    end
  end
  return table.concat(out, "\n") .. "\n"
end

return B
