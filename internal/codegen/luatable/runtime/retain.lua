-- retain.lua — RETAIN-UNKNOWN for the Lua table runtime (docs/SPEC-TABLES.md
-- §6.6; docs/PORT-LUA.md). A retaining load keeps every field this build could
-- not name, with the body it came from, in RESOLVED form: each reference
-- widened to the sixty-four-bit id it names, each framing length recomputed.
-- A retaining save writes those records back in the body they came from, after
-- the body's declared fields and in the order they were retained, interning
-- their references again in first-use order; the id list, the node table and
-- every byte of §3 are ordinary. A fixed-class root refuses the three names.
--
-- The record layout is READER-PRIVATE, as the Go and JavaScript ports' are: a
-- record carries its body's path (the node index the load gave it and the step
-- pairs inside that node), the field id and kind it could not name, and its
-- resolved payload. Retained references are re-interned by their ids, so a
-- reordered id table still names the same field and a save into a different
-- trailer order is correct.
--
-- THE SAVE is save.lua's walk with the path threaded through it, so a record
-- is emitted exactly where the body it belongs to ends. Everything else — the
-- form byte, elision at the declared default, the node numbering, the node
-- table, the terminator and the trailer — is unchanged.
local S = {}

local load = require("load")
local D = require("descriptor")

local NODE_TABLE_ID = -1 -- 0xffffffffffffffff
local MAX64 = 0xffffffffffffffff

-- A RECORD'S BODY PATH (§6.6): the node index the load gave the body (1 is
-- the root, k+1 is node record k) and the step pairs inside it. A step is the
-- field ordinal and the element index the Go reference's tableRetainPath
-- carries, so the two ports address the same body.
local function path_root()
  return { node = 1, steps = {} }
end

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

local function record_u32(b, at)
  return string.unpack("<I4", b, at + 1)
end

local function record_u64(b, at)
  return string.unpack("<i8", b, at + 1)
end

-- THE WRITER (docs/PORT-LUA.md, BYTE BUFFERS): a table of pieces joined once
-- by table.concat.
local function W()
  return { parts = {}, n = 0 }
end

local function w_raw(w, s)
  w.parts[#w.parts + 1] = s
  w.n = w.n + #s
end

local function w_u8(w, v)
  w_raw(w, string.char(v & 0xff))
end

-- CANONICAL LEB128 (§3), unsigned, of a Lua integer's low 64 bits.
local function w_leb(w, v)
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
-- §3, §3.3), save.lua's w_float: -0 and the subnormals exactly, and a NaN or
-- an infinity (or a double past the float32 range in a float32 field) is
-- refused by the writer (false), never written.
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

-- A PLAIN FLOAT COMPARES BY ITS BIT PATTERN (§3.3, "-0 IS A VALUE, AND IT
-- RIDES"), at its own width: -0 under a +0 default rides.
local function same_float(f32, a, b)
  if type(a) ~= "number" or type(b) ~= "number" then return a == b end
  if f32 then return string.pack("<f", a) == string.pack("<f", b) end
  return string.pack("<d", a) == string.pack("<d", b)
end

-- THE NODE NUMBERING (§3.1, internal/tablewire/nodes.go) is the one pointer
-- walk of descriptor.lua (D.number_nodes, docs/PORTING.md M15), the save's own:
-- the retaining save numbers the same graph the same way. A blob named twice
-- is one node here, as it was before the walk was shared.
local guard_holds, count_of, map_entries = D.guard_holds, D.count_of, D.map_entries

local function number_nodes(T, desc, root)
  return D.number_nodes(T, desc, root, false)
end

local write_body
local write_field
local write_element
local write_union

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
  if s == "float" then return same_float(fd.f32, x, fd.default) end
  return x == fd.default
end

local function id_ref(ids, id)
  for i, v in ipairs(ids.list) do
    if v == id then return i end
  end
  ids.list[#ids.list + 1] = id
  return #ids.list
end

-- THE RETAINED IDS' STORAGE IS THE CALLER'S, DECLARED BY CAPACITY (§6.6, THE
-- RETAINED IDS). An id a retained record names takes its trailer slot like
-- any other, but an id this build cannot name (ids.known, the unit's
-- T.WireIds) also takes one entry of the caller's id list, retain.idCapacity
-- of them, and a repeat takes none. Past the capacity the answer is nil: the
-- record that needed the id is dropped and counted, and the save goes on.
-- A store that declares no capacity is unbounded.
local function id_ref_record(ids, id)
  for i, v in ipairs(ids.list) do
    if v == id then return i end
  end
  if ids.known ~= nil and not ids.known[id] then
    if ids.idCapacity ~= nil and ids.retained >= ids.idCapacity then return nil end
    ids.retained = ids.retained + 1
    ids.flag[#ids.list + 1] = true
  end
  ids.list[#ids.list + 1] = id
  return #ids.list
end

-- AN ENUM-KEYED ELEMENT (§2.4, §3.2): its payload is bare — the key's L
-- frames it — and a zero element does not ride, so the pair is absent.
local function id_rollback(ids, keep)
  for i = #ids.list, keep + 1, -1 do
    ids.list[i] = nil
    -- a rolled-back retained id gives its entry of the caller's list back
    if ids.flag ~= nil and ids.flag[i] then
      ids.flag[i] = nil
      ids.retained = ids.retained - 1
    end
  end
end

local function write_keyed_one(T, ids, nodes, e, val, w, path, ord, retain, report)
  local s = e.shape
  if s == "bool" then
    if not val then return true, false end
    w_u8(w, 1)
  elseif s == "int" or s == "flags" then
    if (val or 0) == 0 then return true, false end
    w_int(w, e.width, e.signed, val or 0)
  elseif s == "float" then
    -- A KEYED SLOT ELIDES BY THE SAME BIT COMPARE (§3.3): a -0 slot rides.
    if val == nil then val = 0.0 end
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
    if not write_body(T, ids, nodes, d, val, w, path, retain, report) then return false end
    return true, w.n > 1
  elseif s == "union" then
    local u = T.Unions[e.target]
    if u == nil then return false end
    if type(val) ~= "table" or (val.tag or 0) == 0 then return true, false end
    if not write_union(T, ids, nodes, u, val, w, path, ord, retain, report) then return false end
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
write_field = function(T, ids, nodes, fd, v, w, path, retain, report)
  if not guard_holds(fd, v) then return true end
  if fd.optional and not v[fd.name .. "_present"] then return true end
  local x = v[fd.name]
  local s = fd.shape
  if s == "bool" then
    if fd.optional or not at_default(fd, v) then
      w_leb(w, id_ref(ids, fd.id))
      w_u8(w, 1)
      w_u8(w, x and 1 or 0)
    end
  elseif s == "int" or s == "flags" then
    if fd.optional or not at_default(fd, v) then
      w_leb(w, id_ref(ids, fd.id))
      w_u8(w, fd.kind)
      w_int(w, fd.width, fd.signed, x or 0)
    end
  elseif s == "float" then
    if fd.optional or not at_default(fd, v) then
      w_leb(w, id_ref(ids, fd.id))
      w_u8(w, fd.kind)
      if not w_float(w, fd.f32, x) then return false end
    end
  elseif s == "string" then
    if fd.optional or not at_default(fd, v) then
      w_leb(w, id_ref(ids, fd.id))
      w_u8(w, 12)
      w_leb(w, #x)
      w_raw(w, x)
    end
  elseif s == "wstring" then
    -- WIDE TEXT (§2.13, gotable write.go TWString): its UTF-16 code units,
    -- little-endian, L their byte count; it rides when it holds a unit
    if fd.optional or not at_default(fd, v) then
      w_leb(w, id_ref(ids, fd.id))
      w_u8(w, fd.kind)
      w_leb(w, #x * 2)
      for i = 1, #x do
        w_raw(w, string.pack("<I2", x[i]))
      end
    end
  elseif s == "bytes" then
    if fd.optional or not at_default(fd, v) then
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
    if fd.optional or not at_default(fd, v) then
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
    if x ~= nil and x ~= false then
      w_leb(w, id_ref(ids, fd.id))
      w_u8(w, 17)
      w_leb(w, nodes.index[x] or 0)
    end
  elseif s == "union" then
    local tag = x and x.tag or 0
    if tag ~= 0 then
      local u = T.Unions[fd.target]
      if u == nil then return false end
      w_leb(w, id_ref(ids, fd.id))
      w_u8(w, 15)
      if not write_union(T, ids, nodes, u, x, w, path, fd.ordinal - 1, retain, report) then return false end
    end
  elseif s == "table" then
    local d = T.Tables[fd.target]
    local mark = #ids.list
    local ref = id_ref(ids, fd.id)
    local body = W()
    local ok = write_body(T, ids, nodes, d, x, body, path_step(path, fd.ordinal - 1, 0), retain, report)
    if not ok then return false end
    if not fd.optional and body.n <= 1 then
      ids.list[mark + 1] = nil
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
        local ok, rides = write_keyed_one(
          T,
          ids,
          nodes,
          fd.elem,
          x[i],
          eb,
          path_step(path, fd.ordinal - 1, i - 1),
          fd.ordinal - 1,
          retain,
          report
        )
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
      if count == 0 and not fd.optional then
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
      if not fd.optional and at_default(fd, v) then return true end
      local n = count_of(fd, x)
      w_leb(w, id_ref(ids, fd.id))
      w_u8(w, 14)
      local body = W()
      w_u8(body, fd.elem_kind)
      w_leb(body, n)
      for i = 1, n do
        if
          not write_element(
            T,
            ids,
            nodes,
            fd.elem,
            x[i],
            body,
            path_step(path, fd.ordinal - 1, i - 1),
            fd.ordinal - 1,
            retain,
            report
          )
        then
          return false
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
    if n == 0 and not fd.optional then return true end
    w_leb(w, id_ref(ids, fd.id))
    w_u8(w, 14)
    local body = W()
    w_u8(body, fd.elem_kind or 13)
    w_leb(body, n)
    local entry = T.Tables[fd.entry]
    local mi = 0
    for _, kv in ipairs(map_entries(nodes.entries, fd, x)) do
      local eb = W()
      mi = mi + 1
      local ok = write_body(T, ids, nodes, entry, kv, eb, path_step(path, fd.ordinal - 1, mi - 1), retain, report)
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
write_element = function(T, ids, nodes, e, val, body, path, ord, retain, report)
  local s = e.shape
  if s == "bool" then
    w_u8(body, val and 1 or 0)
  elseif s == "int" or s == "flags" then
    w_int(body, e.width, e.signed, val or 0)
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
    if not write_body(T, ids, nodes, d, val, eb, path, retain, report) then return false end
    w_leb(body, eb.n)
    w_raw(body, table.concat(eb.parts))
  elseif s == "union" then
    local u = T.Unions[e.target]
    if u == nil then return false end
    if not write_union(T, ids, nodes, u, val, body, path, ord, retain, report) then return false end
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

-- ---- THE RESOLVING WALK (docs/SPEC-TABLES.md §6.6) -------------------------
--
-- Capture reads a skipped field by its kind and writes its RESOLVED form: an
-- 8-byte id for every reference, an array's element kind and count, a body's
-- recomputed framing. Save reads that form and writes the wire again,
-- re-interning every id in first-use order. Kind 17 (a node index) and kinds
-- this walk cannot take are the excluded classes and are a retain_lost.

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
  return 0
end

local function tr_reader(b, at, lim)
  return { b = b, at = at, lim = lim, bad = false }
end

local function tr_u8(r)
  if r.at >= r.lim then
    r.bad = true
    return 0
  end
  local v = string.byte(r.b, r.at + 1)
  r.at = r.at + 1
  return v
end

local function tr_leb(r)
  local start = r.at
  local v, s = 0, 0
  for i = 0, 9 do
    if r.at >= r.lim then
      r.bad = true
      r.at = start
      return 0
    end
    local x = string.byte(r.b, r.at + 1)
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

local function tr_has(r, n)
  return r.at <= r.lim and not math.ult(r.lim - r.at, n)
end

local function cw_raw(w, s)
  w.parts[#w.parts + 1] = s
  w.n = w.n + #s
end
local function cw_u8(w, v)
  cw_raw(w, string.char(v & 0xff))
end
local function cw_raw_bytes(w, b, at, n)
  cw_raw(w, string.sub(b, at + 1, at + n))
end
local function cw_leb(w, v)
  v = v & MAX64
  local out = {}
  repeat
    local b = v & 0x7f
    v = v >> 7
    out[#out + 1] = string.char(v ~= 0 and (b | 0x80) or b)
  until v == 0
  cw_raw(w, table.concat(out))
end
local function cw_u32(w, v)
  cw_raw(w, string.pack("<I4", v))
end
local function cw_u64(w, v)
  cw_raw(w, string.pack("<i8", v))
end

local function tr_ref(r, atable, zero, w)
  local v = tr_leb(r)
  if r.bad then return nil end
  if v == 0 then
    if not zero then return nil end
    cw_u64(w, 0)
    return 0
  end
  if math.ult(#atable, v) then return nil end
  local id = atable[v]
  if id == 0 or id == -1 or id == -2 or id == -3 then return nil end
  cw_u64(w, id)
  return id
end

local tr_content, tr_payload

local function tr_framed(r, atable, kind, n, depth, w)
  local body = W()
  if not tr_content(r, atable, kind, n, depth, body) then return false end
  cw_u32(w, body.n)
  cw_raw(w, table.concat(body.parts))
  return true
end

tr_content = function(r, atable, kind, n, depth, w)
  if depth > 64 or not tr_has(r, n) then return false end
  local endpos = r.at + n
  if kind == 13 then
    while true do
      local id = tr_ref(r, atable, true, w)
      if id == nil then return false end
      if id == 0 then break end
      if r.at >= r.lim then return false end
      local k = tr_u8(r)
      cw_u8(w, k)
      if not tr_payload(r, atable, k, depth, w) or r.at > endpos then return false end
    end
  elseif kind == 14 or kind == 16 then
    if r.at >= r.lim then return false end
    local k = tr_u8(r)
    cw_u8(w, k)
    local count = tr_leb(r)
    if r.bad then return false end
    cw_leb(w, count)
    local minimum = file_minimum(k)
    if kind == 16 then minimum = 16 end
    if minimum <= 0 then return false end
    if math.ult(count, 0) then return false end
    for _ = 1, count do
      if kind == 16 then
        if tr_ref(r, atable, false, w) == nil then return false end
        local length = tr_leb(r)
        if r.bad or math.ult(endpos - r.at, length) then return false end
        if not tr_framed(r, atable, k, length, depth + 1, w) then return false end
      else
        if not tr_payload(r, atable, k, depth, w) then return false end
      end
      if r.at > endpos then return false end
    end
  elseif kind == 15 or kind == 30 then
    if not tr_payload(r, atable, kind, depth, w) then return false end
  elseif kind == 17 then
    return false
  else
    if not tr_has(r, n) then return false end
    cw_raw_bytes(w, r.b, r.at, n)
    r.at = r.at + n
  end
  return r.at == endpos
end

tr_payload = function(r, atable, kind, depth, w)
  local width = kind_width(kind)
  if width ~= 0 then
    if not tr_has(r, width) then return false end
    cw_raw_bytes(w, r.b, r.at, width)
    r.at = r.at + width
    return true
  end
  if kind == 12 or kind == 31 or kind == 32 or kind == 33 then
    local n = tr_leb(r)
    if r.bad or not tr_has(r, n) then return false end
    cw_leb(w, n)
    cw_raw_bytes(w, r.b, r.at, n)
    r.at = r.at + n
    return true
  elseif kind == 13 or kind == 14 or kind == 16 then
    local n = tr_leb(r)
    if r.bad or not tr_has(r, n) then return false end
    return tr_framed(r, atable, kind, n, depth + 1, w)
  elseif kind == 15 then
    local id = tr_ref(r, atable, true, w)
    if id == nil then return false end
    if id == 0 then return true end
    if r.at >= r.lim then return false end
    local k = tr_u8(r)
    cw_u8(w, k)
    local n = tr_leb(r)
    if r.bad or not tr_has(r, n) then return false end
    return tr_framed(r, atable, k, n, depth + 1, w)
  elseif kind == 30 then
    return tr_ref(r, atable, true, w) ~= nil
  end
  return false
end

-- THE SAVE HALF of the resolving walk: a record's resolved payload is written
-- as wire again, every reference interned into `ids` by its id.
local function out_leb(w, v)
  w_leb(w, v)
end
local function out_ref(o)
  if not tr_has(o.r, 8) then return nil end
  local id = record_u64(o.r.b, o.r.at)
  o.r.at = o.r.at + 8
  if id == 0 then
    out_leb(o.w, 0)
    return 0
  end
  local ref = id_ref_record(o.ids, id)
  if ref == nil then return nil end
  out_leb(o.w, ref)
  return id
end
local function out_raw(o, n)
  if not tr_has(o.r, n) then return false end
  w_raw(o.w, string.sub(o.r.b, o.r.at + 1, o.r.at + n))
  o.r.at = o.r.at + n
  return true
end
local out_content, out_payload
-- THE LENGTH INSIDE A RECORD FRAMES THE RESOLVED CONTENT, NOT THE WIRE: save
-- writes the content into its own buffer and then the CANONICAL LEB128 length
-- that frames what it wrote (§6.6, ONE POST-ORDER PASS). The recorded length is
-- only what delimits the record's content while it is read.
local function out_framed(o, kind, depth)
  if not tr_has(o.r, 4) then return false end
  local n = record_u32(o.r.b, o.r.at)
  o.r.at = o.r.at + 4
  local sub = W()
  local inner = { r = o.r, w = sub, ids = o.ids }
  if not out_content(inner, kind, n, depth) then return false end
  w_leb(o.w, sub.n)
  w_raw(o.w, table.concat(sub.parts))
  return true
end
out_content = function(o, kind, n, depth)
  if depth > 64 or not tr_has(o.r, n) then return false end
  local endpos = o.r.at + n
  if kind == 13 then
    while true do
      local id = out_ref(o)
      if id == nil then return false end
      if id == 0 then break end
      if not tr_has(o.r, 1) then return false end
      local k = tr_u8(o.r)
      w_u8(o.w, k)
      if not out_payload(o, k, depth) or o.r.at > endpos then return false end
    end
  elseif kind == 14 or kind == 16 then
    if not tr_has(o.r, 1) then return false end
    local k = tr_u8(o.r)
    w_u8(o.w, k)
    local count = tr_leb(o.r)
    if o.r.bad then return false end
    out_leb(o.w, count)
    for _ = 1, count do
      if kind == 16 then
        if out_ref(o) == nil or not out_framed(o, k, depth + 1) then return false end
      elseif not out_payload(o, k, depth) then
        return false
      end
      if o.r.at > endpos then return false end
    end
  elseif kind == 15 or kind == 30 then
    if not out_payload(o, kind, depth) then return false end
  else
    if not out_raw(o, n) then return false end
  end
  return o.r.at == endpos
end
out_payload = function(o, kind, depth)
  local width = kind_width(kind)
  if width ~= 0 then return out_raw(o, width) end
  if kind == 12 or kind == 31 or kind == 32 or kind == 33 then
    local n = tr_leb(o.r)
    if o.r.bad or not tr_has(o.r, n) then return false end
    out_leb(o.w, n)
    return out_raw(o, n)
  elseif kind == 13 or kind == 14 or kind == 16 then
    return out_framed(o, kind, depth + 1)
  elseif kind == 15 then
    local id = out_ref(o)
    if id == nil then return false end
    if id == 0 then return true end
    if not tr_has(o.r, 1) then return false end
    local k = tr_u8(o.r)
    w_u8(o.w, k)
    return out_framed(o, k, depth + 1)
  elseif kind == 30 then
    return out_ref(o) ~= nil
  end
  return false
end

-- A BODY'S RETAINED TAIL (§6.6): the records that came from this body, in the
-- order they were retained, after its declared fields and before its
-- terminator. A record whose reference cannot be interned or whose resolved
-- payload no longer walks is dropped and counted once by the save.
local function emit_tail(ids, w, path, retain, report)
  if retain == nil then return true end
  local key = path_key(path)
  for _, rec in ipairs(retain.list or {}) do
    if rec.key == key then
      local mark = #ids.list
      local probe = W()
      local ref = id_ref_record(ids, rec.id)
      local o = { r = tr_reader(rec.payload, 0, #rec.payload), w = probe, ids = ids }
      -- THE WALK REACHED IT, whatever came of the emit (Go's tail marks placed
      -- before it looks at the emit): a record reached but unwritable, or one
      -- whose id the caller's full id list has no entry for, counts one
      -- retain_lost here and is not counted again by count_lost.
      rec.placed = true
      if ref ~= nil then
        w_leb(probe, ref)
        w_u8(probe, rec.kind)
      end
      if ref ~= nil and out_payload(o, rec.kind, 0) and o.r.at == o.r.lim then
        w_raw(w, table.concat(probe.parts))
      else
        id_rollback(ids, mark)
        if report ~= nil then report.retainLost = report.retainLost + 1 end
      end
    end
  end
  return true
end

local function count_lost(retain, report)
  if retain == nil or report == nil then return end
  for _, rec in ipairs(retain.list or {}) do
    if not rec.placed then report.retainLost = report.retainLost + 1 end
  end
end

-- A nil by-value node saves as its default: every field at its declared
-- default is elided in form 1, leaving the terminator byte (§3, §4.2).
write_body = function(T, ids, nodes, desc, v, w, path, retain, report)
  if v ~= nil then
    for _, fd in ipairs(desc.fields) do
      if not write_field(T, ids, nodes, fd, v, w, path, retain, report) then return false end
    end
  end
  emit_tail(ids, w, path, retain, report)
  w_u8(w, 0)
  return true
end

local write_union_at

write_union = function(T, ids, nodes, u, val, w, path, ord, retain, report)
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
  if not write_union_at(T, ids, nodes, f, val[arm.slot], body, path_step(path, ord, tag), ord, retain, report) then
    return false
  end
  w_leb(w, body.n)
  w_raw(w, table.concat(body.parts))
  return true
end

-- AN ARM'S PAYLOAD (§2.6): its blob arm names the same node the numbering
-- reached at the arm's position (edge_union, §3.1, §6.3).
write_union_at = function(T, ids, nodes, f, val, body, path, ord, retain, report)
  local s = f.shape
  if s == "bool" then
    w_u8(body, val and 1 or 0)
  elseif s == "int" or s == "flags" then
    w_int(body, f.width, f.signed, val or 0)
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
    if not write_body(T, ids, nodes, d, val, body, path, retain, report) then return false end
  elseif s == "union" then
    local inner = T.Unions[f.target]
    if not write_union(T, ids, nodes, inner, val, body, path, ord, retain, report) then return false end
  elseif s == "array" then
    local n = count_of(f, val)
    w_u8(body, f.elem_kind)
    w_leb(body, n)
    for i = 1, n do
      if not write_element(T, ids, nodes, f.elem, val[i], body, path_step(path, ord, i - 1), ord, retain, report) then
        return false
      end
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

-- ---- THE LOAD HALF: THE CAPTURE WALK (docs/SPEC-TABLES.md §6.6) ----------
--
-- The plain reader drops a field it cannot name; the capture walk follows the
-- same framing by the descriptor and copies that field's payload out with its
-- body's path. Nothing the plain reader counted changes: this walk adds the
-- retained and retain_lost counters only.

local function wire_open(bytes)
  if #bytes < 1 or bytes:byte(1) ~= 1 then return nil end
  if #bytes < 9 then return nil end
  local count = string.unpack("<i8", bytes, #bytes - 7)
  if count < 0 or count > (#bytes - 9) // 8 or count > D.ID_TABLE_BOUND then return nil end
  local endpos = #bytes - 8 - count * 8
  local ids = {}
  local seen = {}
  for i = 0, count - 1 do
    local id = string.unpack("<i8", bytes, endpos + i * 8 + 1)
    if seen[id] then return nil end
    seen[id] = true
    ids[i + 1] = id
  end
  return { table = ids, lim = endpos }
end

local function wire_skip(r, kind)
  local w = kind_width(kind)
  if w > 0 then
    if not tr_has(r, w) then return false end
    r.at = r.at + w
    return true
  end
  if kind == 17 or kind == 30 then
    tr_leb(r)
    return not r.bad
  end
  if kind == 12 or kind == 13 or kind == 14 or kind == 16 or kind == 31 or kind == 32 or kind == 33 then
    local n = tr_leb(r)
    if r.bad or not tr_has(r, n) then return false end
    r.at = r.at + n
    return true
  end
  if kind == 15 then
    local arm = tr_leb(r)
    if r.bad then return false end
    if arm ~= 0 then
      tr_u8(r)
      if r.bad then return false end
      local n = tr_leb(r)
      if r.bad or not tr_has(r, n) then return false end
      r.at = r.at + n
    end
    return true
  end
  return false
end

local function wire_nodes(bytes, o)
  local r = tr_reader(bytes, 1, o.lim)
  local payload = nil
  while true do
    local ref = tr_leb(r)
    if r.bad or ref == 0 then break end
    if math.ult(#o.table, ref) or r.at >= r.lim then break end
    local id = o.table[ref]
    local kind = tr_u8(r)
    if id == NODE_TABLE_ID then
      if kind ~= 12 then return {} end
      local n = tr_leb(r)
      if r.bad or not tr_has(r, n) then return {} end
      payload = string.sub(bytes, r.at + 1, r.at + n)
      r.at = r.at + n
    elseif not wire_skip(r, kind) then
      break
    end
  end
  if payload == nil then return {} end
  local rec = tr_reader(payload, 0, #payload)
  -- the payload opens with the node count (save.lua writes it, load.lua reads
  -- it first); the records follow
  tr_leb(rec)
  if rec.bad then return {} end
  local raw = {}
  while rec.at < rec.lim do
    local tref = tr_leb(rec)
    if rec.bad or tref == 0 or math.ult(#o.table, tref) then return raw end
    local n = tr_leb(rec)
    if rec.bad or not tr_has(rec, n) then return raw end
    raw[#raw + 1] = { tid = o.table[tref], body = string.sub(payload, rec.at + 1, rec.at + n) }
    rec.at = rec.at + n
  end
  return raw
end

local function desc_for(T, tid)
  for _, d in pairs(T.Tables) do
    if d.type_id == tid then return d end
  end
  return nil
end

local function field_of(desc, id)
  for _, f in ipairs(desc.fields) do
    if f.id == id then return f end
  end
  return nil
end

local capture_body, capture_value

capture_value = function(T, atable, b, at, lim, f, path, ord, report, retain)
  local r = tr_reader(b, at, lim)
  local s = f.shape
  if s == "enum" then
    -- AN UNKNOWN ENUM VARIANT REFERENCE IS AN EXCLUDED CLASS (§6.6): the FIELD
    -- is this reader's, and it writes its own value under that id, so a
    -- retained copy would be a second occurrence. One retain_lost.
    local ref = tr_leb(r)
    if r.bad or ref == 0 or math.ult(#atable, ref) then return end
    local id = atable[ref]
    for _, var in ipairs(f.variants or {}) do
      if var[2] == id then return end
    end
    report.retainLost = report.retainLost + 1
  elseif s == "table" then
    local d = T.Tables[f.target]
    if d ~= nil then capture_body(T, atable, b, at, lim, d, path, report, retain) end
  elseif s == "union" then
    local armref = tr_leb(r)
    if r.bad or armref == 0 or math.ult(#atable, armref) then return end
    local armid = atable[armref]
    local kind = tr_u8(r)
    local n = tr_leb(r)
    if r.bad or not tr_has(r, n) then return end
    local u = T.Unions[f.target]
    if u == nil then return end
    local arm = nil
    for _, a in ipairs(u.arms) do
      if a.id == armid then
        arm = a
        break
      end
    end
    if arm == nil or arm.field == nil then
      -- AN UNKNOWN UNION ARM ID IS AN EXCLUDED CLASS (§6.6), one level in
      report.retainLost = report.retainLost + 1
      return
    end
    if kind ~= arm.field.kind then return end
    local p = path_step(path, ord, arm.tag)
    capture_value(T, atable, b, r.at, r.at + n, arm.field, p, ord, report, retain)
  elseif s == "array" or s == "map" or s == "bytes" then
    local n = tr_leb(r)
    if r.bad or not tr_has(r, n) then return end
    local body = tr_reader(b, r.at, r.at + n)
    if s == "bytes" then return end
    local ek = tr_u8(body)
    local count = tr_leb(body)
    if body.bad or ek ~= (f.elem_kind or 13) then return end
    if s == "map" then
      local entry = T.Tables[f.entry]
      if entry == nil then return end
      for j = 1, count do
        local elen = tr_leb(body)
        if body.bad or not tr_has(body, elen) then return end
        capture_body(T, atable, b, body.at, body.at + elen, entry, path_step(path, ord, j - 1), report, retain)
        body.at = body.at + elen
      end
      return
    end
    local e = f.elem
    if e == nil then return end
    if e.shape ~= "table" and e.shape ~= "union" then return end
    if f.keyed ~= nil then
      for _ = 1, count do
        local keyref = tr_leb(body)
        local elen = tr_leb(body)
        if body.bad or math.ult(#atable, keyref) or not tr_has(body, elen) then return end
        local slot = nil
        local keyid = atable[keyref]
        for k, v in ipairs(f.keyed) do
          if v[2] == keyid then slot = k end
        end
        if slot ~= nil then
          capture_value(T, atable, b, body.at, body.at + elen, e, path_step(path, ord, slot - 1), ord, report, retain)
        else
          -- AN UNKNOWN KEYED-ARRAY SLOT IS AN EXCLUDED CLASS (§6.6): a slot is
          -- not a field and the reader rewrites the array body whole.
          report.retainLost = report.retainLost + 1
        end
        body.at = body.at + elen
      end
    else
      for i = 1, count do
        local elen = tr_leb(body)
        if body.bad or not tr_has(body, elen) then return end
        capture_value(T, atable, b, body.at, body.at + elen, e, path_step(path, ord, i - 1), ord, report, retain)
        body.at = body.at + elen
      end
    end
  end
end

capture_body = function(T, atable, b, at, lim, desc, path, report, retain)
  if desc == nil then return end
  local r = tr_reader(b, at, lim)
  while true do
    local ref = tr_leb(r)
    if r.bad or ref == 0 then return end
    if math.ult(#atable, ref) or r.at >= r.lim then return end
    local kind = tr_u8(r)
    local id = atable[ref]
    if id == NODE_TABLE_ID then
      if not wire_skip(r, kind) then return end
    else
      local fd = field_of(desc, id)
      if fd == nil then
        local start = r.at
        local endr = tr_reader(b, start, r.lim)
        if not wire_skip(endr, kind) then return end
        local finish = endr.at
        -- A RETAINED UNKNOWN FLOAT IS TESTED LIKE A KNOWN ONE
        -- (docs/SPEC-TABLES.md §3.3, §6.6, ruling 3 of 2026-10-27): kind 10's
        -- or 11's raw bits, and a NaN or an infinity refuses the read as
        -- non_finite_float, named <Owner>.0x<id>.
        if
          kind == 10 and string.unpack("<I4", b, start + 1) & D.F32_EXP == D.F32_EXP
          or kind == 11 and string.unpack("<i8", b, start + 1) & D.F64_EXP == D.F64_EXP
        then
          if report.nonFiniteFloat == nil then report.nonFiniteFloat = desc.name .. string.format(".0x%016x", id) end
          return
        end
        local w = W()
        local rr = tr_reader(b, start, finish)
        local ok = kind ~= 17 and tr_payload(rr, atable, kind, 0, w) and rr.at == finish
        if ok then
          local payload = table.concat(w.parts)
          local cost = #payload + 16
          if retain ~= nil and retain.capacity ~= nil and (retain.used or 0) + cost > retain.capacity then
            report.retainLost = report.retainLost + 1
          else
            if retain ~= nil then
              retain.list = retain.list or {}
              retain.used = (retain.used or 0) + cost
              retain.list[#retain.list + 1] =
                { key = path_key(path), id = id, kind = kind, payload = payload, placed = false }
            end
            report.retained = report.retained + 1
          end
        else
          report.retainLost = report.retainLost + 1
        end
        r.at = finish
      elseif kind ~= fd.kind then
        if not wire_skip(r, kind) then return end
      else
        local start = r.at
        local endr = tr_reader(b, start, r.lim)
        if not wire_skip(endr, kind) then return end
        local finish = endr.at
        local shape = fd.shape
        local ord = (fd.ordinal or 0) - 1
        if shape == "table" then
          local er = tr_reader(b, start, finish)
          local n = tr_leb(er)
          if not er.bad and tr_has(er, n) then
            local d = T.Tables[fd.target]
            if d ~= nil then
              capture_body(T, atable, b, er.at, er.at + n, d, path_step(path, ord, 0), report, retain)
            end
          end
        elseif shape == "enum" or shape == "union" or shape == "array" or shape == "map" or shape == "bytes" then
          capture_value(T, atable, b, start, finish, fd, path, ord, report, retain)
        end
        r.at = finish
      end
    end
  end
end

local function capture(T, name, bytes, retain, report)
  local o = wire_open(bytes)
  if o == nil then return end
  local desc = T.Tables[name]
  capture_body(T, o.table, bytes, 1, o.lim, desc, path_root(), report, retain)
  local raw = wire_nodes(bytes, o)
  for k, rec in ipairs(raw) do
    local d = desc_for(T, rec.tid)
    if d ~= nil then
      capture_body(T, o.table, rec.body, 0, #rec.body, d, { node = k + 1, steps = {} }, report, retain)
    elseif rec.tid ~= T.StringBlobTypeId and rec.tid ~= T.BytesBlobTypeId then
      -- the unit's own blob nodes (a *string or *bytes field's payload) are
      -- this reader's to read, not an unknown record: only a node of a type
      -- this build cannot name is retain_lost
      report.retainLost = report.retainLost + 1
    end
  end
end

-- THE SAVE (docs/SPEC-TABLES.md §3, §3.1, §6.6): save.lua's walk with the
-- body path, so a retained record is written after its own body's fields and
-- before the body's terminator; the root's tail precedes the node table.
local function save_retain(T, name, v, retain, report)
  local desc = T.Tables[name]
  if desc == nil then return nil end
  local nodes = number_nodes(T, desc, v)
  if nodes == nil then return nil end
  local idx = W()
  local ids = { list = {}, known = T.WireIds, flag = {}, retained = 0 }
  if retain ~= nil then ids.idCapacity = retain.idCapacity end
  w_u8(idx, 1)
  local root = path_root()
  for _, fd in ipairs(desc.fields) do
    if not write_field(T, ids, nodes, fd, v, idx, root, retain, report) then return nil end
  end
  emit_tail(ids, idx, root, retain, report)
  if #nodes.list > 0 then
    w_leb(idx, id_ref(ids, NODE_TABLE_ID))
    w_u8(idx, 12)
    local sub = W()
    w_leb(sub, #nodes.list)
    for k, n in ipairs(nodes.list) do
      w_leb(sub, id_ref(ids, n.typeId))
      if n.codec == nil then
        w_leb(sub, #n.body)
        w_raw(sub, n.body)
      else
        local body = W()
        if not write_body(T, ids, nodes, n.codec, n.v, body, { node = k + 1, steps = {} }, retain, report) then
          return nil
        end
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
  return table.concat(idx.parts)
end

local function fixed_refusal(name)
  -- THE SAME SENTENCE THE GO CONST CARRIES (§6.6, §11): level 0 so the message
  -- is the sentence itself rather than a source position.
  error(name .. ": retention requires a variable root and its region directory", 0)
end

-- LoadRetain is Load's path into a REGION (§6.5, §6.6): the plain load runs
-- unchanged and the capture walk adds what it could not name. A FIXED-class
-- root is refused by name, as the Go reference's const is.
function S.LoadRetain(T, name, bytes, retain, report)
  local desc = T.Tables[name]
  if desc == nil then return nil end
  if not desc.variable then fixed_refusal(name) end
  if retain ~= nil then
    retain.list = {}
    retain.used = 0
  end
  local rep = report
    or {
      malformed = false,
      refused = 0,
      unknown = 0,
      kindMismatch = 0,
      clamped = 0,
      widened = 0,
      duplicate = 0,
      retained = 0,
      retainLost = 0,
    }
  rep.retained = rep.retained or 0
  rep.retainLost = rep.retainLost or 0
  local v = load.Load(T, name, bytes, rep)
  if rep.malformed then return nil end
  local retainedBefore, lostBefore = rep.retained, rep.retainLost
  capture(T, name, bytes, retain, rep)
  -- a retained unknown float that is not finite refuses the whole read, and
  -- nothing it captured stays (§3.3, ruling 3)
  if rep.nonFiniteFloat ~= nil then
    rep.reason = "non_finite_float"
    if retain ~= nil then
      retain.list = {}
      retain.used = 0
    end
    rep.retained, rep.retainLost = retainedBefore, lostBefore
    return nil
  end
  if rep.malformed then return nil end
  return v
end

-- MeasureRetain is the length SaveRetain would write (§6.6), measured under
-- the same walk, including each retained frame once.
function S.MeasureRetain(T, name, v, retain)
  local desc = T.Tables[name]
  if desc == nil then return nil end
  if not desc.variable then fixed_refusal(name) end
  if retain ~= nil then
    for _, rec in ipairs(retain.list or {}) do
      rec.placed = false
    end
  end
  -- docs/SPEC-TABLES.md §2.15: a retired variant is refused, by name
  local refused = D.refuse_deprecated(T, name, v)
  if refused ~= nil then return nil, refused end
  local saved = save_retain(T, name, v, retain, nil)
  if saved == nil then return nil, D.refuse_non_finite(T, name, v) end
  return #saved
end

-- SaveRetain requires a report (§6.6): that is the only place a caller learns
-- a retained record was dropped. The plain Save is unchanged.
function S.SaveRetain(T, name, v, retain, report)
  local desc = T.Tables[name]
  if desc == nil then return nil end
  if not desc.variable then fixed_refusal(name) end
  if report == nil then return nil end
  if retain ~= nil then
    for _, rec in ipairs(retain.list or {}) do
      rec.placed = false
    end
  end
  local refused = D.refuse_deprecated(T, name, v)
  if refused ~= nil then return nil, refused end
  local saved = save_retain(T, name, v, retain, report)
  if saved == nil then return nil, D.refuse_non_finite(T, name, v) end
  count_lost(retain, report)
  return saved
end

return S
