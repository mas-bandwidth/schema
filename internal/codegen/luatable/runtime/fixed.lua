-- fixed.lua — the Lua table runtime's FIXED FORM (docs/SPEC-TABLES.md §3.4,
-- form byte 3), the reader and writer for one record. Emitted beside the unit's
-- <Package>Tables.lua by internal/codegen/luatable. The layout is built from
-- the descriptors the shape walks (docs/FIXED-FORM-ALGORITHM.md), so a record
-- is the eight-byte hash of the writer's vocabulary block and then the values
-- in declared order, every field at its declared storage width, nothing padded
-- (docs/FIXED-FORM-ALGORITHM.md §1, §3).
--
-- A buffer READ is a Lua string and a cursor; a WRITE is a table of pieces
-- joined once by table.concat. Fixed-width little-endian values are
-- string.unpack/string.pack with a `<` format (docs/PORT-LUA.md).
local F = {}

local formByte = 3
local maxRecordBytes = 65536
local maxRecords = 4096

local function zeros(n)
  if n <= 0 then return "" end
  return string.rep("\0", n)
end

local function pack_uint(v, w)
  if w == 1 then return string.pack("<I1", v) end
  if w == 2 then return string.pack("<I2", v) end
  if w == 4 then return string.pack("<I4", v) end
  return string.pack("<I8", v)
end

local function pack_int(v, w, signed)
  if w == 1 then return signed and string.pack("<i1", v) or string.pack("<I1", v) end
  if w == 2 then return signed and string.pack("<i2", v) or string.pack("<I2", v) end
  if w == 4 then return signed and string.pack("<i4", v) or string.pack("<I4", v) end
  return signed and string.pack("<i8", v) or string.pack("<I8", v)
end

local function unpack_int(buf, pos, w, signed)
  if w == 1 then
    if signed then return string.unpack("<i1", buf, pos) end
    return string.unpack("<I1", buf, pos)
  end
  if w == 2 then
    if signed then return string.unpack("<i2", buf, pos) end
    return string.unpack("<I2", buf, pos)
  end
  if w == 4 then
    if signed then return string.unpack("<i4", buf, pos) end
    return string.unpack("<I4", buf, pos)
  end
  if signed then return string.unpack("<i8", buf, pos) end
  return string.unpack("<I8", buf, pos)
end

local encode_table, encode_value, decode_table, decode_value

local function encode_array(T, d, v, size, elemSize)
  v = v or {}
  local parts = {}
  if d.list then
    error("fixed: an unbounded array (docs/SPEC-TABLES.md §2.9) is not fixed")
  end
  if d.fixed then
    for i = 1, d.bound do
      local ev = v[i]
      if ev == nil then
        parts[#parts + 1] = zeros(elemSize)
      else
        parts[#parts + 1] = encode_value(T, d.elem, ev, elemSize)
      end
    end
    return table.concat(parts)
  end
  if d.keyed then
    local n = d.bound or #d.keyed
    for i = 1, n do
      local ev = v[i]
      if ev == nil then
        parts[#parts + 1] = zeros(elemSize)
      else
        parts[#parts + 1] = encode_value(T, d.elem, ev, elemSize)
      end
    end
    return table.concat(parts)
  end
  local n = #v
  if n > d.bound then
    error("count_over_bound")
  end
  parts[#parts + 1] = string.pack("<i4", n)
  for i = 1, n do
    parts[#parts + 1] = encode_value(T, d.elem, v[i], elemSize)
  end
  local used = 4 + n * elemSize
  if used > size then error("count_over_bound") end
  parts[#parts + 1] = zeros(size - used)
  return table.concat(parts)
end

local function encode_union(T, d, v, size)
  v = v or { tag = 0 }
  local ud = T.FixedUnions[d.target]
  if ud == nil then error("fixed: no union descriptor for " .. tostring(d.target)) end
  local tag = v.tag or 0
  if tag > #ud.arms then tag = 0 end
  local parts = { pack_uint(tag, ud.tag) }
  local arm = ud.arms[tag]
  if arm ~= nil and tag ~= 0 then
    local fd = T.Unions[d.target].arms[tag].field
    local payload = encode_value(T, fd, v[arm.name], arm.size)
    parts[#parts + 1] = payload
  else
    parts[#parts + 1] = zeros(size - ud.tag)
  end
  return table.concat(parts)
end

encode_value = function(T, d, v, size, elemSize)
  local s = d.shape
  if s == "int" then
    if d.timestamp then return pack_int(v or 0, 8, true) end
    return pack_int(v or 0, d.width, d.signed)
  elseif s == "bool" then
    return string.char((v and 1 or 0))
  elseif s == "float" then
    if d.f32 then return string.pack("<f", v or 0.0) end
    return string.pack("<d", v or 0.0)
  elseif s == "string" or s == "bytes" then
    local n = #v
    local cap = size - 4
    if n > cap then error("length_over_bound") end
    return string.pack("<i4", n) .. string.sub(v, 1, n) .. zeros(cap - n)
  elseif s == "wstring" then
    local n = #v
    local cap = math.floor((size - 4) / 2)
    if n > cap then error("length_over_bound") end
    local parts = { string.pack("<i4", n) }
    for i = 1, n do parts[#parts + 1] = string.pack("<I2", v[i] or 0) end
    parts[#parts + 1] = zeros((size - 4) - 2 * n)
    return table.concat(parts)
  elseif s == "enum" then
    local w = T.FixedEnums[d.enum] or 4
    local ord = 0
    if v ~= nil then
      for i, var in ipairs(d.variants) do
        if var[1] == v then ord = i break end
      end
    end
    return pack_uint(ord, w)
  elseif s == "flags" then
    return pack_uint(v or 0, 8)
  elseif s == "table" then
    return encode_table(T, d.target, v)
  elseif s == "union" then
    return encode_union(T, d, v, size)
  elseif s == "array" then
    return encode_array(T, d, v, size, elemSize)
  elseif s == "pointer" or s == "string_blob" or s == "bytes_blob" then
    return zeros(size)
  end
  error("fixed: unknown shape " .. tostring(s))
end

encode_table = function(T, name, v)
  local desc = T.Tables[name]
  if desc == nil then error("fixed: no descriptor for table " .. tostring(name)) end
  v = v or {}
  local sizes = T.FixedFields[name]
  local parts = {}
  for i, f in ipairs(desc.fields) do
    local fs = sizes[i].size
    if f.optional then
      local present = v[f.name .. "_present"] == true
      if present and v[f.name] ~= nil then
        parts[#parts + 1] = "\1" .. encode_value(T, f, v[f.name], fs - 1, sizes[i].elem)
      else
        parts[#parts + 1] = "\0" .. zeros(fs - 1)
      end
    else
      parts[#parts + 1] = encode_value(T, f, v[f.name], fs, sizes[i].elem)
    end
  end
  return table.concat(parts)
end

decode_value = function(T, d, buf, pos, size, elemSize)
  local s = d.shape
  if s == "int" then
    local w = 8
    local signed = d.signed
    if d.timestamp then w = 8; signed = true else w = d.width end
    return unpack_int(buf, pos, w, signed)
  elseif s == "bool" then
    return buf:byte(pos) ~= 0, pos + size
  elseif s == "float" then
    if d.f32 then return string.unpack("<f", buf, pos) end
    return string.unpack("<d", buf, pos)
  elseif s == "string" or s == "bytes" then
    local n = string.unpack("<i4", buf, pos)
    local cap = size - 4
    if n < 0 or n > cap then error("length_over_bound") end
    return string.sub(buf, pos + 4, pos + 4 + n - 1), pos + size
  elseif s == "wstring" then
    local n = string.unpack("<i4", buf, pos)
    local cap = math.floor((size - 4) / 2)
    if n < 0 or n > cap then error("length_over_bound") end
    local t = {}
    for i = 1, n do t[i] = string.unpack("<I2", buf, pos + 4 + (i - 1) * 2) end
    return t, pos + size
  elseif s == "enum" then
    local w = T.FixedEnums[d.enum] or 4
    local raw = unpack_int(buf, pos, w, false)
    local v = nil
    if raw ~= 0 and d.variants[raw] ~= nil then v = d.variants[raw][1] end
    return v, pos + w
  elseif s == "flags" then
    return unpack_int(buf, pos, 8, false), pos + 8
  elseif s == "table" then
    local child = T.Tables[d.target].fixed_size
    local v = decode_table(T, d.target, buf, pos, child)
    return v, pos + size
  elseif s == "union" then
    local ud = T.FixedUnions[d.target]
    local tag = unpack_int(buf, pos, ud.tag, false)
    if tag > #ud.arms then tag = 0 end
    local v = { tag = tag }
    if tag ~= 0 then
      local arm = ud.arms[tag]
      local fd = T.Unions[d.target].arms[tag].field
      v[arm.name] = decode_value(T, fd, buf, pos + ud.tag, arm.size)
    end
    return v, pos + size
  elseif s == "array" then
    local t = {}
    if d.fixed or d.keyed then
      local n = d.bound or #d.keyed
      for i = 1, n do
        t[i] = decode_value(T, d.elem, buf, pos + (i - 1) * elemSize, elemSize)
      end
      return t, pos + size
    end
    local n = string.unpack("<i4", buf, pos)
    if n < 0 or n > d.bound then error("count_over_bound") end
    for i = 1, n do
      t[i] = decode_value(T, d.elem, buf, pos + 4 + (i - 1) * elemSize, elemSize)
    end
    return t, pos + size
  elseif s == "pointer" or s == "string_blob" or s == "bytes_blob" then
    return nil, pos + size
  end
  error("fixed: unknown shape " .. tostring(s))
end

decode_table = function(T, name, buf, pos, writerSize)
  local desc = T.Tables[name]
  if desc == nil then error("fixed: no descriptor for table " .. tostring(name)) end
  local v = T["New" .. name]()
  local sizes = T.FixedFields[name]
  local cursor = pos
  for i, f in ipairs(desc.fields) do
    local fs = sizes[i].size
    if cursor - pos + fs <= writerSize then
      if f.optional then
        local present = buf:byte(cursor) ~= 0
        v[f.name .. "_present"] = present
        if present then
          v[f.name] = decode_value(T, f, buf, cursor + 1, fs - 1, sizes[i].elem)
        end
        cursor = cursor + fs
      else
        v[f.name], cursor = decode_value(T, f, buf, cursor, fs, sizes[i].elem)
      end
    else
      break
    end
  end
  return v
end

-- F.read parses the file framing (docs/FIXED-FORM-ALGORITHM.md §2): the form
-- byte, the reserved zeroes, the hash, the layout and one record.
function F.read(T, name, buf)
  if #buf < 1 then error("malformed") end
  local form = buf:byte(1)
  if form ~= formByte then
    if form == 1 then error("previous_form") end
    error("malformed")
  end
  if #buf < 20 then error("malformed") end
  for i = 2, 8 do
    if buf:byte(i) ~= 0 then error("malformed") end
  end
  local hash = string.unpack("<I8", buf, 9)
  local layoutLen = string.unpack("<I4", buf, 17)
  if layoutLen < 4 or 20 + layoutLen > #buf then error("layout_malformed") end
  local layout = string.sub(buf, 21, 20 + layoutLen)
  local count = string.unpack("<I4", layout, 1)
  if count == 0 or #layout ~= 4 + 17 * count then error("layout_malformed") end
  local rootKind = layout:byte(5 + 8)
  local rootSize = string.unpack("<I4", layout, 5 + 9)
  if rootKind ~= 13 then error("layout_malformed") end
  if rootSize > maxRecordBytes then error("layout_record_too_large") end
  local recSize = 8 + rootSize
  local rest = #buf - 20 - layoutLen
  if recSize <= 8 or rest <= 0 or rest % recSize ~= 0 then error("malformed") end
  if rest / recSize > maxRecords then error("plan_too_large") end
  local at = 21 + layoutLen
  if string.unpack("<I8", buf, at) ~= hash then error("no_layout") end
  return decode_table(T, name, buf, at + 8, rootSize), hash, layout
end

-- F.write writes the header and ONE record (docs/FIXED-FORM-ALGORITHM.md §2,
-- §3.1): the template is the hash and a zeroed body, and each field lands at
-- its constant offset, little-endian, at its declared width.
function F.write(T, name, value)
  local f = T.Fixed[name]
  if f == nil then error("fixed: no fixed descriptor for table " .. tostring(name)) end
  local body = encode_table(T, name, value)
  if #body ~= f.size then error("layout_malformed") end
  local header = string.char(formByte) .. zeros(7) .. pack_uint(f.hash, 8) .. pack_uint(#f.layout, 4) .. f.layout
  return header .. pack_uint(f.hash, 8) .. body
end

return F
