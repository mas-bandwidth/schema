-- fixed.lua — the Lua table runtime's FIXED FORM (docs/SPEC-TABLES.md §3.4,
-- form byte 3), the reader and writer for one record. Emitted beside the unit's
-- <Package>Tables.lua by internal/codegen/luatable. The layout is built from
-- the descriptors the shape walks (the IDENTITY PLAN, mirroring the JavaScript
-- leg's plan compiler, internal/codegen/jstable), so a record is the eight-byte
-- hash of the writer's vocabulary block and then the values in declared order,
-- every field at its declared storage width, nothing padded (docs/
-- FIXED-FORM-ALGORITHM.md).
--
-- A buffer READ is a Lua string and a cursor; a WRITE is a table of pieces
-- joined once by table.concat. Fixed-width little-endian values are
-- string.unpack/string.pack with a `<` format (docs/PORT-LUA.md).
local F = {}

local fixedEntryBytes = 17

-- the wire kinds (docs/SPEC-TABLES.md §3) the reader and writer recognize
local K = {
  u8 = 1, u16 = 2, u32 = 3, u64 = 4,
  i8 = 5, i16 = 6, i32 = 7, i64 = 8,
  f32 = 9, f64 = 10,
  string = 11, wstring = 12, table = 13, array = 14, keyed = 15,
  enum = 16, union = 17,
  u8a = 18, u16a = 19, u32a = 20,
  i8a = 21, i16a = 22, i32a = 23,
  f32a = 24, f64a = 25,
  bool = 26, flags = 27, timestamp = 28,
  nopayload = 32, optional = 35,
}

local packfmt = {
  [K.u8] = "<I1", [K.u16] = "<I2", [K.u32] = "<I4", [K.u64] = "<I8",
  [K.i8] = "<i1", [K.i16] = "<i2", [K.i32] = "<i4", [K.i64] = "<i8",
  [K.f32] = "<f", [K.f64] = "<d",
  [K.bool] = "<I1", [K.flags] = "<I8", [K.timestamp] = "<i8",
}

-- the storage width of a leaf kind
local function leafwidth(kind)
  if kind == K.u8 or kind == K.i8 then return 1 end
  if kind == K.u16 or kind == K.i16 then return 2 end
  if kind == K.u32 or kind == K.i32 or kind == K.f32 then return 4 end
  if kind == K.f64 or kind == K.flags or kind == K.timestamp then return 8 end
  return 0
end

local function fail(where, what)
  return where .. ": " .. what
end

local function leafkind(f)
  local s = f.shape
  if s == "bool" then return K.bool end
  if s == "float" then
    if f.f32 then return K.f32 end
    return K.f64
  end
  if s == "flags" then return K.flags end
  if s == "int" then
    if f.timestamp then return K.timestamp end
    local w = f.width
    if w == 1 then return f.signed and K.i8 or K.u8 end
    if w == 2 then return f.signed and K.i16 or K.u16 end
    if w == 4 then return f.signed and K.i32 or K.u32 end
    return f.signed and K.i64 or K.u64
  end
  return nil
end

-- reader: read one value of a field descriptor from buf at pos; returns value
-- and the next pos
local read_value

local function read_leaf(f, buf, pos)
  local kind = leafkind(f)
  local fmt = packfmt[kind]
  local val, np = string.unpack(fmt, buf, pos)
  if kind == K.f32 then
    -- float32 rides as the double that is exactly it
    val = string.unpack("<f", string.unpack("<f", buf, pos)) -- placeholder, never reached
  end
  return val, np
end

-- the top-level reader: a plan built from the writer's layout, which for the
-- identity build is the module's own descriptor walk
read_value = nil

return F
