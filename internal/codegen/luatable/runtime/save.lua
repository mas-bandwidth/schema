-- save.lua — the Lua table runtime's VARIABLE FORM (form 1) SAVE and its
-- MEASURE (docs/SPEC-TABLES.md §3, §3.1; docs/PORT-LUA.md, THE CARD MAP). A
-- port of the JavaScript leg's TableVariableSave walk
-- (internal/codegen/jstable/variableruntime.go) and of the Go reference engine
-- (internal/tablewire encode.go, nodes.go, nodetable.go). Emitted beside the
-- unit's <Package>Tables.lua by internal/codegen/luatable, which embeds
-- runtime/*.lua by glob.
--
-- THE SAVE is DRIVEN BY THE DESCRIPTOR (§8.1): the form byte, the root's
-- fields, elision at the declared default, the node table for pointers in
-- first-visit order, the terminator, then the id table and its count. A data
-- cycle or a value with no wire identity refuses the save (nil).
local S = {}

-- THE CHECK is load.lua's one CRC-32C table (docs/SPEC-TABLES.md §2.14): the
-- save seals it and the load and the strict read verify it.
local load = require("load")
local D = require("descriptor")
local crc32c = load.crc32c

local NODE_TABLE_ID = -1 -- 0xffffffffffffffff
local MAX64 = 0xffffffffffffffff

-- THE WRITER (docs/PORT-LUA.md, BYTE BUFFERS): a table of pieces joined once
-- by table.concat.
local function W()
  return { parts = {}, n = 0 }
end

local function w_raw(w, s)
  w.parts[#w.parts + 1] = s
  w.n = w.n + #s
end

-- one string per byte value, built once, so a byte or a one-byte LEB128 is a
-- table read and not a call
local CH = {}
for i = 0, 255 do
  CH[i] = string.char(i)
end

local function w_u8(w, v)
  w_raw(w, CH[v & 0xff])
end

-- CANONICAL LEB128 (§3), unsigned, of a Lua integer's low 64 bits. The common
-- value, a reference or a length under 128, is one byte.
local function w_leb(w, v)
  if v >= 0 and v < 0x80 then
    w_raw(w, CH[v])
    return
  end
  v = v & MAX64
  local out = {}
  repeat
    local b = v & 0x7f
    v = v >> 7
    if v ~= 0 then
      out[#out + 1] = string.char(b | 0x80)
    else
      out[#out + 1] = string.char(b)
    end
  until v == 0
  w_raw(w, table.concat(out))
end

local function w_u64(w, v)
  w_raw(w, string.pack("<i8", v))
end
local function w_f64(w, v)
  w_raw(w, string.pack("<d", v))
end

local function w_int(w, width, signed, v)
  if width == 1 then
    w_u8(w, v)
  elseif width == 2 then
    w_raw(w, signed and string.pack("<i2", v) or string.pack("<I2", v))
  elseif width == 4 then
    w_raw(w, signed and string.pack("<i4", v) or string.pack("<I4", v))
  else
    w_u64(w, v)
  end
end

-- A FLOAT RIDES AS THE BIT PATTERN OF A FINITE VALUE (docs/SPEC-TABLES.md
-- §3, §3.3): -0 and the subnormals exactly, and a NaN or an infinity is
-- refused by the writer (false), never written. A float32 is narrowed by the
-- standard pack, exact for every value a float32 holds, and its PACKED bits
-- are what is tested, so a double past the float32 range (which would narrow
-- to an infinity) is refused too.
local function w_float(w, f32, v)
  if f32 then
    local p = string.pack("<f", v)
    if string.unpack("<I4", p) & D.F32_EXP == D.F32_EXP then return false end
    w_raw(w, p)
    return true
  end
  if v - v ~= 0 then return false end
  w_f64(w, v)
  return true
end

-- WELL-FORMED UTF-8 with no zero byte is a text list element's write bound
-- (docs/SPEC-TABLES.md §2.13, §3), as a string(N) field's is; Go's Save
-- refuses a value that breaks it.
local function text_ok(s)
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

-- THE NODE NUMBERING (§3.1, internal/tablewire/nodes.go) is the one pointer
-- walk of descriptor.lua (D.number_nodes, docs/PORTING.md M15), shared with
-- the retaining save, the cook's layout and the JSON text form. A shared blob
-- refuses the save by name (blob_shared, §2.5, §16.7).
local guard_holds, count_of, map_entries = D.guard_holds, D.count_of, D.map_entries

local function number_nodes(T, desc, root)
  return D.number_nodes(T, desc, root, true)
end

local write_body
local write_field
local write_element
local write_union

-- A PLAIN FLOAT'S SPECIAL VALUES TAKE ONE RULE (docs/SPEC-TABLES.md §3.3,
-- "-0 IS A VALUE, AND IT RIDES"): the default elision compares BIT PATTERNS,
-- never ==, because -0 == +0 under == and a -0 must ride. The width is the
-- field's own, so a float32 compares as four bytes and a float64 as eight.
local function same_float(f32, a, b)
  if type(a) ~= "number" or type(b) ~= "number" then return a == b end
  if f32 then return string.pack("<f", a) == string.pack("<f", b) end
  return string.pack("<d", a) == string.pack("<d", b)
end

-- ELISION AT THE DECLARED DEFAULT (§3): a pointer and a blob at nil, an array
-- when empty, a map when empty, a nested table when its body is its terminator
-- alone, and any other field equal to its declared default.
local function at_default(fd, v)
  local x = v[fd.name]
  local s = fd.shape
  if s == "pointer" or s == "string_blob" or s == "bytes_blob" then return x == nil end
  if s == "array" then
    local n = count_of(fd, x)
    if n == 0 then return true end
    if fd.list or not fd.fixed then return false end
    local e = fd.elem
    local es = e.shape
    if es == "table" then return false end
    local def = 0
    if es == "bool" or es == "enum" or es == "pointer" or es == "string_blob" or es == "bytes_blob" then
      def = false
    elseif es == "string" or es == "bytes" then
      def = ""
    end
    for i = 1, n do
      local e1 = x[i]
      if es == "union" then
        -- a non-table element in a union array saves as the default union,
        -- tag 0 (§2.6, §4.2)
        if type(e1) == "table" and (e1.tag or 0) ~= 0 then return false end
      else
        if e1 == nil then e1 = def end
        if es == "float" then
          if not same_float(e.f32, e1, def) then return false end
        elseif e1 ~= def then
          return false
        end
      end
    end
    return true
  end
  if s == "map" then
    local n = 0
    for _ in pairs(x or {}) do
      n = n + 1
    end
    return n == 0
  end
  if s == "wstring" then return x == nil or #x == 0 end
  if s == "wide" then
    -- int128/uint128 at zero are their two zero halves (docs/PORT-LUA.md,
    -- 64-BIT AND 128-BIT INTEGERS): the declared default is a { lo, hi }.
    if type(x) ~= "table" then return false end
    return x.lo == fd.default.lo and x.hi == fd.default.hi
  end
  -- A PLAIN FLOAT COMPARES BY ITS BIT PATTERN (§3.3, "-0 IS A VALUE, AND IT
  -- RIDES"), at its own width: -0 under a +0 default rides.
  if s == "float" then return same_float(fd.f32, x, fd.default) end
  return x == fd.default
end

-- THE ID TABLE (§3): ids.list is the wire's table in first-use order and
-- ids.map its inverse, so a reference is one lookup however many ids the unit
-- has (a scan of the list was quadratic in the fields a body names).
local function id_ref(ids, id)
  local ref = ids.map[id]
  if ref ~= nil then return ref end
  local list = ids.list
  ref = #list + 1
  list[ref] = id
  ids.map[id] = ref
  return ref
end

-- AN ENUM-KEYED ELEMENT (§2.4, §3.2): its payload is bare — the key's L
-- frames it — and a zero element does not ride, so the pair is absent.
local function id_rollback(ids, keep)
  local list = ids.list
  for i = #list, keep + 1, -1 do
    ids.map[list[i]] = nil
    list[i] = nil
  end
end

local function write_keyed_one(T, ids, nodes, e, val, w)
  local s = e.shape
  if s == "bool" then
    if not val then return true, false end
    w_u8(w, 1)
  elseif s == "int" or s == "flags" then
    if (val or 0) == 0 then return true, false end
    w_int(w, e.width, e.signed, val or 0)
  elseif s == "float" then
    if val == nil then val = 0.0 end
    -- A KEYED SLOT ELIDES BY THE SAME BIT COMPARE (§3.3): a -0 slot rides.
    if same_float(e.f32, val, 0.0) then return true, false end
    if not w_float(w, e.f32, val) then return false end
  elseif s == "string" then
    if val == nil or #val == 0 then return true, false end
    w_raw(w, val)
  elseif s == "wstring" then
    if val == nil or #val == 0 then return true, false end
    for i = 1, #val do
      w_raw(w, string.pack("<I2", val[i]))
    end
  elseif s == "enum" then
    if val == nil or val == false then return true, false end
    local vref = 0
    for _, var in ipairs(e.variants) do
      if var[1] == val then
        vref = id_ref(ids, var[2])
        break
      end
    end
    if vref == 0 then return false end
    w_leb(w, vref)
  elseif s == "pointer" then
    if val == nil or val == false then return true, false end
    w_leb(w, nodes.index[val] or 0)
  elseif s == "table" then
    local d = T.Tables[e.target]
    if not write_body(T, ids, nodes, d, val, w) then return false end
    return true, w.n > 1
  elseif s == "union" then
    local u = T.Unions[e.target]
    if u == nil then return false end
    if type(val) ~= "table" or (val.tag or 0) == 0 then return true, false end
    if not write_union(T, ids, nodes, u, val, w) then return false end
  elseif s == "bytes" then
    if val == nil or #val == 0 then return true, false end
    w_u8(w, 6)
    w_leb(w, #val)
    w_raw(w, val)
  else
    return false
  end
  return true, true
end

-- THE SAVE WALK (§3, §3.1). write_field writes into the writer `w`. It
-- returns false on a refusal; elision at the declared default writes nothing.
write_field = function(T, ids, nodes, fd, v, w)
  if not guard_holds(fd, v) then return true end
  if fd.optional and not v[fd.name .. "_present"] then return true end
  local x = v[fd.name]
  local s = fd.shape
  if s == "bool" then
    if (fd.optional or fd.required) or not at_default(fd, v) then
      w_leb(w, id_ref(ids, fd.id))
      w_u8(w, 1)
      w_u8(w, x and 1 or 0)
    end
  elseif s == "int" or s == "flags" then
    if (fd.optional or fd.required) or not at_default(fd, v) then
      w_leb(w, id_ref(ids, fd.id))
      w_u8(w, fd.kind)
      w_int(w, fd.width, fd.signed, x or 0)
    end
  elseif s == "wide" then
    -- int128/uint128 and fixed128/ufixed128: sixteen bytes, the low 64-bit
    -- half then the high one, two's complement (docs/SPEC-TABLES.md §3 kinds
    -- 18/19 and 20-29; docs/PORT-LUA.md, 64-BIT AND 128-BIT INTEGERS,
    -- FIXED-POINT).
    if (fd.optional or fd.required) or not at_default(fd, v) then
      local q = x or { lo = 0, hi = 0 }
      w_leb(w, id_ref(ids, fd.id))
      w_u8(w, fd.kind)
      w_raw(w, string.pack("<i8", q.lo))
      w_raw(w, string.pack("<i8", q.hi))
    end
  elseif s == "float" then
    if (fd.optional or fd.required) or not at_default(fd, v) then
      w_leb(w, id_ref(ids, fd.id))
      w_u8(w, fd.kind)
      if not w_float(w, fd.f32, x) then return false end
    end
  elseif s == "string" then
    if (fd.optional or fd.required) or not at_default(fd, v) then
      w_leb(w, id_ref(ids, fd.id))
      w_u8(w, 12)
      w_leb(w, #x)
      w_raw(w, x)
    end
  elseif s == "wstring" then
    -- WIDE TEXT (§2.13, gotable write.go TWString): its UTF-16 code units,
    -- little-endian, L their byte count; it rides when it holds a unit
    if (fd.optional or fd.required) or not at_default(fd, v) then
      w_leb(w, id_ref(ids, fd.id))
      w_u8(w, fd.kind)
      w_leb(w, #x * 2)
      for i = 1, #x do
        w_raw(w, string.pack("<I2", x[i]))
      end
    end
  elseif s == "bytes" then
    if (fd.optional or fd.required) or not at_default(fd, v) then
      w_leb(w, id_ref(ids, fd.id))
      w_u8(w, 14)
      local body = W()
      w_u8(body, 6)
      w_leb(body, #x)
      w_raw(body, x)
      w_leb(w, body.n)
      w_raw(w, table.concat(body.parts))
    end
  elseif s == "enum" then
    if (fd.optional or fd.required) or not at_default(fd, v) then
      local ref = id_ref(ids, fd.id)
      local vref = 0
      if x ~= nil then
        for _, var in ipairs(fd.variants) do
          if var[1] == x then
            vref = id_ref(ids, var[2])
            break
          end
        end
      end
      if vref == 0 and x ~= nil then return false end
      w_leb(w, ref)
      w_u8(w, 30)
      w_leb(w, vref)
    end
  elseif s == "pointer" then
    -- null is elided (§3.1); false is null where nil cannot be held (a map
    -- entry's value, as a sequence's element)
    if x ~= nil and x ~= false then
      w_leb(w, id_ref(ids, fd.id))
      w_u8(w, 17)
      w_leb(w, nodes.index[x])
    end
  elseif s == "string_blob" or s == "bytes_blob" then
    -- A BLOB SLOT NAMES ITS NODE (§3.1, §6.3): the index the numbering gave the
    -- node's own box, never its content and never the slot that names it.
    -- A required blob rides even at nil, as the null node (§2.3, gotable
    -- write.go's pointer case).
    if (x ~= nil and x ~= false) or fd.required then
      w_leb(w, id_ref(ids, fd.id))
      w_u8(w, 17)
      w_leb(w, nodes.index[x] or 0)
    end
  elseif s == "union" then
    local tag = x and x.tag or 0
    if tag ~= 0 or fd.required then
      local u = T.Unions[fd.target]
      if u == nil then return false end
      w_leb(w, id_ref(ids, fd.id))
      w_u8(w, 15)
      if not write_union(T, ids, nodes, u, x, w) then return false end
    end
  elseif s == "table" then
    local d = T.Tables[fd.target]
    local mark = #ids.list
    local ref = id_ref(ids, fd.id)
    local body = W()
    local ok = write_body(T, ids, nodes, d, x, body)
    if not ok then return false end
    if not fd.required and not fd.optional and body.n <= 1 then
      id_rollback(ids, mark)
      return true
    end
    w_leb(w, ref)
    w_u8(w, 13)
    w_leb(w, body.n)
    w_raw(w, table.concat(body.parts))
  elseif s == "array" then
    if fd.keyed ~= nil then
      local mark = #ids.list
      local ref = id_ref(ids, fd.id)
      local outer = W()
      w_u8(outer, fd.elem_kind)
      local entries = W()
      local count = 0
      for i = 1, fd.bound or 0 do
        local emark = #ids.list
        local keyref = id_ref(ids, fd.keyed[i][2])
        local eb = W()
        local ok, rides = write_keyed_one(T, ids, nodes, fd.elem, x[i], eb)
        if not ok then return false end
        if rides then
          count = count + 1
          w_leb(entries, keyref)
          w_leb(entries, eb.n)
          w_raw(entries, table.concat(eb.parts))
        else
          id_rollback(ids, emark)
        end
      end
      if count == 0 and not fd.optional and not fd.required then
        id_rollback(ids, mark)
        return true
      end
      w_leb(outer, count)
      w_raw(outer, table.concat(entries.parts))
      w_leb(w, ref)
      w_u8(w, fd.kind)
      w_leb(w, outer.n)
      w_raw(w, table.concat(outer.parts))
    else
      if not fd.required and not fd.optional and at_default(fd, v) then return true end
      local n = count_of(fd, x)
      -- A TEXT LIST (docs/SPEC-TABLES.md §2.13): its storage is an array of
      -- the generated element record, its plain spelling rides each element
      -- as kind 12's own L and bytes, and `total` bounds the sum of the
      -- elements' byte lengths. A value past the element bound or the total
      -- refuses, returning -1 with nothing written.
      local textvf = nil
      if fd.elem.shape == "table" then
        local ep = T.Tables[fd.elem.target]
        local vf = ep and ep.fields[1]
        if vf ~= nil and vf.shape == "string" and (fd.elem_kind == 12 or fd.total ~= nil) then textvf = vf end
      end
      if textvf ~= nil then
        local spent = 0
        for i = 1, n do
          local el = x[i]
          local present = not textvf.optional or (el ~= nil and el[textvf.name .. "_present"] == true)
          if present then
            local text = (el ~= nil and el[textvf.name]) or ""
            if type(text) ~= "string" then return false end
            if textvf.bound ~= nil and textvf.bound > 0 and #text > textvf.bound then return false end
            if not text_ok(text) then return false end
            spent = spent + #text
          end
        end
        if fd.total ~= nil and fd.total > 0 and spent > fd.total then return false end
      end
      w_leb(w, id_ref(ids, fd.id))
      w_u8(w, 14)
      local body = W()
      w_u8(body, fd.elem_kind)
      w_leb(body, n)
      if fd.elem_kind == 12 and textvf ~= nil then
        for i = 1, n do
          local text = (x[i] ~= nil and x[i][textvf.name]) or ""
          w_leb(body, #text)
          w_raw(body, text)
        end
      else
        for i = 1, n do
          if not write_element(T, ids, nodes, fd.elem, x[i], body) then return false end
        end
      end
      w_leb(w, body.n)
      w_raw(w, table.concat(body.parts))
    end
  elseif s == "map" then
    local n = 0
    for _ in pairs(x or {}) do
      n = n + 1
    end
    if n == 0 and not fd.optional and not fd.required then return true end
    w_leb(w, id_ref(ids, fd.id))
    w_u8(w, 14)
    local body = W()
    w_u8(body, fd.elem_kind or 13)
    w_leb(body, n)
    local entry = T.Tables[fd.entry]
    for _, kv in ipairs(map_entries(nodes.entries, fd, x)) do
      local eb = W()
      local ok = write_body(T, ids, nodes, entry, kv, eb)
      if not ok then return false end
      w_leb(body, eb.n)
      w_raw(body, table.concat(eb.parts))
    end
    w_leb(w, body.n)
    w_raw(w, table.concat(body.parts))
  else
    -- a shape the save does not carry refuses (jstable TableVariableWriteField)
    return false
  end
  return true
end

-- ONE ARRAY ELEMENT (§2.4, §3).
write_element = function(T, ids, nodes, e, val, body)
  local s = e.shape
  if s == "bool" then
    w_u8(body, val and 1 or 0)
  elseif s == "int" or s == "flags" then
    w_int(body, e.width, e.signed, val or 0)
  elseif s == "wide" then
    local q = val or { lo = 0, hi = 0 }
    w_raw(body, string.pack("<i8", q.lo))
    w_raw(body, string.pack("<i8", q.hi))
  elseif s == "float" then
    if not w_float(body, e.f32, val or 0.0) then return false end
  elseif s == "enum" then
    local vref = 0
    if val ~= nil and val ~= false then
      for _, var in ipairs(e.variants) do
        if var[1] == val then
          vref = id_ref(ids, var[2])
          break
        end
      end
    end
    if vref == 0 and val ~= nil and val ~= false then return false end
    w_leb(body, vref)
  elseif s == "pointer" then
    w_leb(body, (val == nil or val == false) and 0 or (nodes.index[val] or 0))
  elseif s == "table" then
    local d = T.Tables[e.target]
    local eb = W()
    if not write_body(T, ids, nodes, d, val, eb) then return false end
    w_leb(body, eb.n)
    w_raw(body, table.concat(eb.parts))
  elseif s == "union" then
    local u = T.Unions[e.target]
    if u == nil then return false end
    if not write_union(T, ids, nodes, u, val, body) then return false end
  elseif s == "string" then
    val = val or ""
    w_leb(body, #val)
    w_raw(body, val)
  elseif s == "wstring" then
    val = val or {}
    local parts = {}
    for i = 1, #val do
      parts[#parts + 1] = string.pack("<I2", val[i])
    end
    w_leb(body, #table.concat(parts))
    w_raw(body, table.concat(parts))
  elseif s == "bytes" then
    val = val or ""
    w_leb(body, #val)
    w_raw(body, val)
  else
    -- a blob element (the checker refuses an array of byte buffers, §15) or
    -- any shape the save does not carry refuses
    return false
  end
  return true
end

-- A nil by-value node saves as its default: every field at its declared
-- default is elided in form 1, leaving the terminator byte (§3, §4.2).
write_body = function(T, ids, nodes, desc, v, w)
  if v ~= nil then
    local typed = ids.typed
    typed = typed and typed[desc.name]
    if typed ~= nil then
      if not typed(ids, nodes, v, w) then return false end
    else
      for _, fd in ipairs(desc.fields) do
        if not write_field(T, ids, nodes, fd, v, w) then return false end
      end
    end
  end
  w_u8(w, 0)
  return true
end

local write_union_at

write_union = function(T, ids, nodes, u, val, w)
  local tag = type(val) == "table" and val.tag or 0
  if tag == 0 then
    w_leb(w, 0)
    return true
  end
  local arm = u.arms[tag]
  if arm == nil then return false end
  local f = arm.field
  if f == nil then
    w_leb(w, id_ref(ids, arm.id))
    w_u8(w, 32)
    w_leb(w, 0)
    return true
  end
  w_leb(w, id_ref(ids, arm.id))
  w_u8(w, f.kind)
  local body = W()
  if not write_union_at(T, ids, nodes, f, val[arm.slot], body) then return false end
  w_leb(w, body.n)
  w_raw(w, table.concat(body.parts))
  return true
end

-- AN ARM'S PAYLOAD (§2.6): its blob arm names the same node the numbering
-- reached at the arm's position (edge_union, §3.1, §6.3).
write_union_at = function(T, ids, nodes, f, val, body)
  local s = f.shape
  if s == "bool" then
    w_u8(body, val and 1 or 0)
  elseif s == "int" or s == "flags" then
    w_int(body, f.width, f.signed, val or 0)
  elseif s == "wide" then
    local q = val or { lo = 0, hi = 0 }
    w_raw(body, string.pack("<i8", q.lo))
    w_raw(body, string.pack("<i8", q.hi))
  elseif s == "float" then
    if not w_float(body, f.f32, val) then return false end
  elseif s == "string" then
    w_raw(body, val)
  elseif s == "wstring" then
    for i = 1, #val do
      w_raw(body, string.pack("<I2", val[i]))
    end
  elseif s == "bytes" then
    w_u8(body, 6)
    w_leb(body, #val)
    w_raw(body, val)
  elseif s == "enum" then
    local vref = 0
    if val ~= nil and val ~= false then
      for _, var in ipairs(f.variants) do
        if var[1] == val then
          vref = id_ref(ids, var[2])
          break
        end
      end
    end
    if vref == 0 and val ~= nil and val ~= false then return false end
    w_leb(body, vref)
  elseif s == "pointer" then
    w_leb(body, (val == nil or val == false) and 0 or (nodes.index[val] or 0))
  elseif s == "table" then
    local d = T.Tables[f.target]
    if not write_body(T, ids, nodes, d, val, body) then return false end
  elseif s == "union" then
    local inner = T.Unions[f.target]
    if not write_union(T, ids, nodes, inner, val, body) then return false end
  elseif s == "array" then
    local n = count_of(f, val)
    w_u8(body, f.elem_kind)
    w_leb(body, n)
    for i = 1, n do
      if not write_element(T, ids, nodes, f.elem, val[i], body) then return false end
    end
  elseif s == "string_blob" or s == "bytes_blob" then
    -- AN ARM'S BLOB IS THE SAME NODE (§2.6): its index is the box's identity,
    -- as the slot's is (§3.1, §6.3).
    if val == nil or val == false then
      w_leb(body, 0)
    else
      w_leb(body, nodes.index[val] or 0)
    end
  else
    -- a map arm, or any shape the save does not carry, refuses
    return false
  end
  return true
end

-- THE SAVE (docs/SPEC-TABLES.md §3, §3.1): the form byte, the root's fields,
-- the node table LAST in the root body under the reserved id at kind 12, the
-- terminator, then the id table and its count. A data cycle or a value with no
-- wire identity refuses the save (nil). It is the walk S.Save drives by the
-- descriptors alone and S.SaveTyped drives through the unit's typed bodies
-- (docs/PORTING.md M22); the numbering, the node table, the id table and the
-- trailer are this one function either way.
local function save_with(T, name, v, typed)
  -- docs/SPEC-TABLES.md §2.15: a retired variant is refused, by name
  local refused = D.refuse_deprecated(T, name, v)
  if refused ~= nil then return nil, refused end
  local desc = T.Tables[name]
  if desc == nil then return nil end
  local nodes, why = number_nodes(T, desc, v)
  if nodes == nil then return nil, why end
  local idx = W()
  local ids = { list = {}, map = {}, typed = typed }
  w_u8(idx, 1)
  local root = typed and typed[name]
  if root ~= nil then
    if not root(ids, nodes, v, idx) then return nil, D.refuse_non_finite(T, name, v) end
  else
    for _, fd in ipairs(desc.fields) do
      if not write_field(T, ids, nodes, fd, v, idx) then return nil, D.refuse_non_finite(T, name, v) end
    end
  end
  if #nodes.list > 0 then
    w_leb(idx, id_ref(ids, NODE_TABLE_ID))
    w_u8(idx, 12)
    local sub = W()
    w_leb(sub, #nodes.list)
    for _, n in ipairs(nodes.list) do
      w_leb(sub, id_ref(ids, n.typeId))
      if n.codec == nil then
        w_leb(sub, #n.body)
        w_raw(sub, n.body)
      else
        local body = W()
        if not write_body(T, ids, nodes, n.codec, n.v, body) then return nil, D.refuse_non_finite(T, name, v) end
        w_leb(sub, body.n)
        w_raw(sub, table.concat(body.parts))
      end
    end
    w_leb(idx, sub.n)
    w_raw(idx, table.concat(sub.parts))
  end
  w_u8(idx, 0)
  for _, id in ipairs(ids.list) do
    w_u64(idx, id)
  end
  w_u64(idx, #ids.list)
  local out = table.concat(idx.parts)
  -- THE INTEGRITY TRAILER (docs/SPEC-TABLES.md §2.14, §3.4): a declaring
  -- root's file ends with the little-endian CRC-32C of every byte before it,
  -- after the id table and so the last four bytes. Measure is Save's length,
  -- so it carries the four bytes with it.
  if desc.integrity ~= nil then out = out .. string.pack("<I4", crc32c(out)) end
  return out
end

function S.Save(T, name, v)
  return save_with(T, name, v, nil)
end

-- THE TYPED SAVE (docs/PORTING.md M22). The unit's generated module carries,
-- per table, a typed body (T.TypedBodies[name], runtime/save.lua's S.Typed the
-- helpers it is written against): the scalar leaves, bool, integer, float and
-- string fields with no guard, presence or `required`, are written straight
-- from the value with their ids, kinds, widths and defaults as constants in the
-- body, and every other field goes through the descriptor walk's write_field.
-- S.Save above stays the descriptor-driven entry that reads the descriptors
-- on every field; S.SaveTyped writes the same bytes (TestLuaTypedSave...).
function S.SaveTyped(T, name, v)
  local typed = T.TypedBodies
  if typed == nil then error("SaveTyped: this module carries no typed save bodies", 2) end
  return save_with(T, name, v, typed)
end

-- what a generated typed body is written against: the writer's pieces, the id
-- reference, the default compare of a float, and the descriptor walk for the
-- fields it does not type
S.Typed = {
  leb = w_leb,
  raw = w_raw,
  float = w_float,
  same_float = same_float,
  id_ref = id_ref,
  field = write_field,
  -- a field header, the reference to its id then its kind byte: one call, and
  -- one string when the reference is under 128
  head = function(w, ids, id, kind)
    local ref = ids.map[id] or id_ref(ids, id)
    if ref < 0x80 then
      w_raw(w, CH[ref] .. CH[kind])
    else
      w_leb(w, ref)
      w_raw(w, CH[kind])
    end
  end,
}

-- THE MEASURE: the length of what Save would write — the saved length.
-- It mirrors jstable (variableform.go emitRootCodec: Measure is the length
-- of TableVariableSave), so it is equal to the saved length by construction.
function S.Measure(T, name, v)
  local saved, why = S.Save(T, name, v)
  if saved == nil then return nil, why end
  return #saved
end

return S
