-- descriptor.lua — the Lua table runtime's descriptor walk (docs/PORT-LUA.md,
-- THE RUNTIME LAYOUT; docs/SPEC-TABLES.md §8.1). Emitted beside the unit's
-- <Package>Tables.lua by internal/codegen/luatable, which embeds
-- runtime/*.lua by glob; each later card adds its own file beside this one.
--
-- A table module is DATA: one descriptor per table (name, type_id, fields)
-- and per table-side union (arms). This file is the one walk over it that
-- every later file shares: find a field by name or by wire id, and check a
-- value against its descriptor's shapes and declared defaults.
local D = {}

-- D.field returns the descriptor of the field named name, or nil.
function D.field(desc, name)
  for _, f in ipairs(desc.fields) do
    if f.name == name then return f end
  end
  return nil
end

-- D.by_id returns the descriptor of the field whose wire id (§5) is id, or nil.
function D.by_id(desc, id)
  for _, f in ipairs(desc.fields) do
    if f.id == id then return f end
  end
  return nil
end

local check_value

local function fail(where, what)
  return where .. ": " .. what
end

-- a field's value against its descriptor; elem is true for an array element,
-- which carries no default (§2.9)
local function check_field(T, where, d, v)
  local s = d.shape
  if s == "array" then
    if type(v) ~= "table" then return fail(where, "an array is a sequence") end
    if d.fixed and #v ~= d.bound then return fail(where, "a fixed array holds its bound") end
    for i = 1, #v do
      local err = check_value(T, where .. "[" .. i .. "]", d.elem, v[i], true)
      if err then return err end
    end
    return nil
  elseif s == "map" then
    if type(v) ~= "table" or next(v) ~= nil then return fail(where, "a map is born empty") end
    return nil
  end
  return check_value(T, where, d, v, false)
end

check_value = function(T, where, d, v, elem)
  local s = d.shape
  if s == "pointer" or s == "string_blob" or s == "bytes_blob" then
    -- a sequence cannot hold nil, so false is the null element
    if not (v == nil or (elem and v == false)) then return fail(where, "a pointer is born null") end
  elseif s == "bool" then
    if type(v) ~= "boolean" or not (elem or v == d.default) then return fail(where, "bool at its default") end
  elseif s == "int" or s == "flags" then
    if math.type(v) ~= "integer" or not (elem or v == d.default) then return fail(where, "integer at its default") end
  elseif s == "float" then
    if math.type(v) ~= "float" or not (elem or v == d.default) then return fail(where, "float at its default") end
  elseif s == "string" or s == "bytes" then
    if type(v) ~= "string" or not (elem or v == d.default) then return fail(where, "byte string at its default") end
  elseif s == "wstring" then
    if type(v) ~= "table" or #v ~= 0 then return fail(where, "wide text is born empty") end
  elseif s == "enum" then
    -- a table enum is its variant NAME (§5); None is nil, and false in a sequence
    if elem then
      if v ~= false then return fail(where, "an enum element is born None") end
    elseif v ~= d.default then
      return fail(where, "enum at its default variant name")
    end
  elseif s == "table" then
    if type(v) ~= "table" then return fail(where, "a nested table") end
    return D.check_defaults(T, d.target, v, where)
  elseif s == "union" then
    if type(v) ~= "table" or v.tag ~= 0 then return fail(where, "a union is born empty") end
    if T.Unions[d.target] == nil then return fail(where, "union " .. d.target .. " has no descriptor") end
  else
    return fail(where, "unknown shape " .. tostring(s))
  end
  return nil
end

-- D.check_defaults checks a value of table `name` against the module T's
-- descriptor: every field in the shape its descriptor names and at its declared
-- default, an optional's <field>_present false (§2.3), and no key the
-- descriptor does not name. It returns nil, or the first finding.
function D.check_defaults(T, name, v, where)
  where = where or name
  local desc = T.Tables[name]
  if desc == nil then return fail(where, "no descriptor for table " .. tostring(name)) end
  local seen = {}
  for _, f in ipairs(desc.fields) do
    local err = check_field(T, where .. "." .. f.name, f, v[f.name])
    if err then return err end
    seen[f.name] = true
    if f.optional then
      if v[f.name .. "_present"] ~= false then return fail(where .. "." .. f.name, "an optional is born absent") end
      seen[f.name .. "_present"] = true
    end
  end
  for k in pairs(v) do
    if not seen[k] then return fail(where, "a key " .. tostring(k) .. " no descriptor names") end
  end
  return nil
end

-- D.region_align returns the unit's region alignment (docs/SPEC-TABLES.md §7.1,
-- §8.5; ir.TableRegionAlign) carried on the module or on a table descriptor.
function D.region_align(T, desc)
  if T ~= nil and T.RegionAlign ~= nil then return T.RegionAlign end
  if T ~= nil and T.Registry ~= nil and T.Registry.region_align ~= nil then return T.Registry.region_align end
  if desc ~= nil and desc.region_align ~= nil then return desc.region_align end
  return 8
end

-- D.record_size returns a record descriptor's fixed byte size (ir.RecordLayout).
function D.record_size(desc)
  return desc.size
end

-- D.list_layout returns the element size and alignment of an array/list field
-- (ir.ListElementLayout) from its descriptor.
function D.list_layout(T, f)
  if f.elem_size ~= nil and f.elem_align ~= nil then return f.elem_size, f.elem_align end
  local e = f.elem
  if e == nil then return 0, 1 end
  if e.shape == "table" and T ~= nil and T.Tables ~= nil then
    local sub = T.Tables[e.target]
    if sub ~= nil then return sub.size, sub.align end
  end
  return 0, 1
end

-- D.pointer_reachable walks the declaration graph from root (a table descriptor
-- or name in T.Tables) and returns (nodes, string_blob, bytes_blob) matching
-- ir.PointerReachable and ir.PointerReachableBlobs:
--   nodes: map from type_id -> table descriptor for every pointer-reachable table
--   string_blob: true if a *string [blob] field is reachable from root
--   bytes_blob: true if a *bytes [blob] or *[N]u8 [blob] field is reachable
function D.pointer_reachable(T, root)
  local start = type(root) == "table" and root or (T.Tables and T.Tables[root])
  local nodes = {}
  local string_blob = false
  local bytes_blob = false
  if start == nil then return nodes, false, false end
  local visited = {}
  local visit_struct, visit_field, visit_elem
  visit_elem = function(e, _parent_f)
    if e == nil then return end
    local s = e.shape
    if s == "pointer" then
      local target = T.Tables[e.target]
      if target ~= nil then
        nodes[target.type_id] = target
        visit_struct(target)
      end
    elseif s == "string_blob" then
      string_blob = true
    elseif s == "bytes_blob" then
      bytes_blob = true
    elseif s == "table" then
      local target = T.Tables[e.target]
      if target ~= nil then visit_struct(target) end
    elseif s == "union" then
      local un = T.Unions and T.Unions[e.target]
      if un ~= nil then
        for _, arm in ipairs(un.arms) do
          if arm.field ~= nil then visit_field(arm.field) end
        end
      end
    end
  end
  visit_field = function(f)
    if f == nil then return end
    if f.shape == "map" then
      local entry = T.Tables[f.entry]
      if entry ~= nil then visit_struct(entry) end
      return
    elseif f.shape == "array" then
      visit_elem(f.elem, f)
      return
    end
    visit_elem(f, f)
  end
  visit_struct = function(st)
    if st == nil or visited[st.name] then return end
    visited[st.name] = true
    for _, f in ipairs(st.fields) do
      visit_field(f)
    end
  end
  visit_struct(start)
  return nodes, string_blob, bytes_blob
end

-- THE WRITE RULE OF A RETIRED VARIANT (docs/SPEC-TABLES.md §2.15, WRITE): a
-- strict write refuses a value holding a deprecated variant with
-- invalid_value naming <Table>.<field>. D.deprecated_variant answers the
-- first such "<Table>.<field>" in v — a scalar enum field, an array or map
-- element, a map key, and every table, pointer target and union arm v
-- reaches — or nil. A unit with no retired variant answers nil at once.
local deprecated_in

local function variant_deprecated(d, name)
  if type(name) ~= "string" then return false end
  for _, var in ipairs(d.variants or {}) do
    if var[1] == name then return var.deprecated == true end
  end
  return false
end

local function deprecated_value(T, d, val, where, seen)
  if val == nil or val == false or d == nil then return nil end
  local s = d.shape
  if s == "enum" then
    if variant_deprecated(d, val) then return where end
  elseif s == "array" then
    if type(val) ~= "table" then return nil end
    for _, x in pairs(val) do
      local hit = deprecated_value(T, d.elem, x, where, seen)
      if hit then return hit end
    end
  elseif s == "map" then
    if type(val) ~= "table" then return nil end
    for k, x in pairs(val) do
      local hit = deprecated_value(T, d.key_field, k, where, seen) or deprecated_value(T, d.value_field, x, where, seen)
      if hit then return hit end
    end
  elseif s == "table" or s == "pointer" then
    local desc = T.Tables[d.target]
    if desc ~= nil and type(val) == "table" then return deprecated_in(T, desc, val, seen) end
  elseif s == "union" then
    local u = T.Unions[d.target]
    if u ~= nil and type(val) == "table" then
      local arm = u.arms[val.tag or 0]
      if arm ~= nil and arm.field ~= nil then
        return deprecated_value(T, arm.field, val[arm.slot], u.name .. "." .. arm.name, seen)
      end
    end
  end
  return nil
end

deprecated_in = function(T, desc, v, seen)
  if seen[v] then return nil end
  seen[v] = true
  for _, fd in ipairs(desc.fields) do
    local hit = deprecated_value(T, fd, v[fd.name], desc.name .. "." .. fd.name, seen)
    if hit then return hit end
  end
  return nil
end

function D.deprecated_variant(T, name, v)
  if not T.DeprecatedVariants then return nil end
  local desc = T.Tables[name]
  if desc == nil or type(v) ~= "table" then return nil end
  return deprecated_in(T, desc, v, {})
end

-- D.refuse_deprecated is the §2.15 refusal's text, nil when v holds no
-- retired variant: "invalid_value: <Table>.<field>".
function D.refuse_deprecated(T, name, v)
  local hit = D.deprecated_variant(T, name, v)
  if hit == nil then return nil end
  return "invalid_value: " .. hit .. " holds a deprecated enum variant"
end

-- THE FINITE RULE ON WRITE (docs/SPEC-TABLES.md §3.3):
-- every writer refuses a
-- float holding a NaN or an infinity, and the encoders test each float as they
-- write it (x - x is NaN for both and zero for every finite x). This walk is
-- the cold path that NAMES the refusal once an encoder has refused: the first
-- "<Owner>.<field>" in v that holds one — a scalar, an array or map element,
-- a union arm, and every table, pointer target and node v reaches — or nil.
D.F32_EXP = 0x7f800000
D.F64_EXP = 0x7ff0000000000000

-- D.finite answers whether a writer carries v in a float field: a float32
-- field tests the PACKED bits, so a double past the float32 range (which
-- would narrow to an infinity) is not finite there.
function D.finite(v, f32)
  if type(v) ~= "number" then return true end
  if f32 then return string.unpack("<I4", string.pack("<f", v)) & D.F32_EXP ~= D.F32_EXP end
  return v - v == 0
end

local non_finite_in

local function non_finite_value(T, d, val, where, seen)
  if val == nil or val == false or d == nil then return nil end
  local s = d.shape
  if s == "float" then
    if not D.finite(val, d.f32) then return where end
  elseif s == "array" then
    if type(val) ~= "table" then return nil end
    for _, x in pairs(val) do
      local hit = non_finite_value(T, d.elem, x, where, seen)
      if hit then return hit end
    end
  elseif s == "map" then
    if type(val) ~= "table" then return nil end
    for _, x in pairs(val) do
      local hit = non_finite_value(T, d.value_field, x, where, seen)
      if hit then return hit end
    end
  elseif s == "table" or s == "pointer" then
    local desc = T.Tables[d.target]
    if desc ~= nil and type(val) == "table" then return non_finite_in(T, desc, val, seen) end
  elseif s == "union" then
    local u = T.Unions[d.target]
    if u ~= nil and type(val) == "table" then
      local arm = u.arms[val.tag or 0]
      if arm ~= nil and arm.field ~= nil then
        return non_finite_value(T, arm.field, val[arm.slot], u.name .. "." .. arm.name, seen)
      end
    end
  end
  return nil
end

non_finite_in = function(T, desc, v, seen)
  if seen[v] then return nil end
  seen[v] = true
  for _, fd in ipairs(desc.fields) do
    local hit = non_finite_value(T, fd, v[fd.name], desc.name .. "." .. fd.name, seen)
    if hit then return hit end
  end
  return nil
end

-- D.refuse_non_finite is a writer's refusal text once its encoder refused,
-- "non_finite_float: <Owner>.<field>", or nil when v holds no NaN or infinity
-- (the encoder refused for another reason).
function D.refuse_non_finite(T, name, v)
  local desc = T.Tables[name]
  if desc == nil or type(v) ~= "table" then return nil end
  local hit = non_finite_in(T, desc, v, {})
  if hit == nil then return nil end
  return "non_finite_float: " .. hit
end

-- THE ONE POINTER WALK (docs/PORTING.md M15; docs/SPEC-TABLES.md §3.1, §6.3).
-- The numbering of the save, of the retaining save, of the cook's layout and
-- the pack that follows it, and of the JSON text form's reference count is ONE
-- depth-first walk over a member's fields in DECLARATION ORDER that descends
-- every by-value edge in place (a nested table, an element of a bounded or
-- keyed array of them, a map entry, a union's set arm) to reach the pointer and
-- blob fields inside it. Every consumer calls D.number_nodes and differs only
-- in what it does with the answer, so a node's first visit is the same in all
-- of them by construction. The shape to refuse is a second copy: it is
-- self-consistent, the wire surface stays green, and the two numberings part
-- the first time a by-value edge declared before a pointer reaches a shared
-- node first (retain.lua's copy had already lost the optional's presence gate).

-- A GUARDED FIELD RIDES ONLY WHEN ITS GUARD HOLDS (§3: a field under a false
-- guard is elided whatever its storage holds; §3.1: it is not an edge, so its
-- target takes no node). fd.guard is the conjunction of the field's `if` branch
-- conditions, each a bool field of the same body and the value it must hold
-- (luatable.go emitDescriptor; tabletext.Guards); a field with no guard always
-- rides. A bool not set reads false.
function D.guard_holds(fd, v)
  local g = fd.guard
  if g == nil then return true end
  for _, t in ipairs(g) do
    if (v[t.field] == true) ~= t.want then return false end
  end
  return true
end

-- BYTEWISE KEY ORDER (§2.8): Lua's string < is strcoll, which is bytewise only
-- under the C locale, so a string key is ordered by its unsigned bytes, a
-- shorter prefix first, whatever locale the host runs.
function D.bytes_less(a, b)
  local n = math.min(#a, #b)
  local i = 1
  while i <= n do
    local j = math.min(i + 255, n)
    local ca, cb = a:sub(i, j), b:sub(i, j)
    if ca ~= cb then
      for k = 1, j - i + 1 do
        local x, y = ca:byte(k), cb:byte(k)
        if x ~= y then return x < y end
      end
    end
    i = j + 1
  end
  return #a < #b
end

-- A MAP'S ENTRIES IN ASCENDING KEY ORDER (§2.8, §3.1; internal/tablewire
-- nodes.go MapEntryOrder): one entry table per key, built once per map and
-- shared by the numbering and the write, so both walks meet the same slots.
function D.map_entries(cache, fd, x)
  if x == nil then return {} end
  local got = cache[x]
  if got ~= nil then return got end
  local kf = fd.key_field
  local keys = {}
  for key in pairs(x) do
    keys[#keys + 1] = key
  end
  table.sort(keys, function(a, b)
    if kf.shape == "string" then return D.bytes_less(a, b) end
    if kf.signed or kf.width < 8 then return a < b end
    return math.ult(a, b)
  end)
  local keyname = kf.name
  local valname = fd.value_field.name
  got = {}
  for i, key in ipairs(keys) do
    got[i] = { [keyname] = key, [valname] = x[key] }
  end
  cache[x] = got
  return got
end

-- the live element count of an array field's value
function D.count_of(fd, v)
  local s = fd.shape
  if s == "bytes" then return #v end
  if fd.list then return #v end
  if fd.fixed then return fd.bound or 0 end
  return math.min(#v, fd.bound or 0)
end

local STRING_BLOB_ID = 0x704be0d8faaffc58
local BYTES_BLOB_ID = 0x2f2ec0474f1c4fe4

local edge_value, edge_union, edges

-- one value's pointer and blob edges, in the walk's order. holder/name name
-- the slot the value occupies (a table's field or a union arm's payload);
-- cache holds each map's entries (D.map_entries).
edge_value = function(T, fd, val, holder, name, out, cache)
  local s = fd.shape
  if s == "string_blob" or s == "bytes_blob" then
    if val ~= nil and val ~= false then
      out[#out + 1] = { blob = true, shape = s, name = name, holder = holder, val = val }
    end
  elseif s == "pointer" then
    if val ~= nil and val ~= false then out[#out + 1] = { blob = false, target = fd.target, val = val } end
  elseif s == "table" then
    local d = T.Tables[fd.target]
    if d then edges(T, d, val, out, cache) end
  elseif s == "array" then
    local n = D.count_of(fd, val)
    local e = fd.elem
    for i = 1, n do
      edge_value(T, e, val[i], val, i, out, cache)
    end
  elseif s == "union" then
    edge_union(T, T.Unions[fd.target], val, out, cache)
  elseif s == "map" then
    -- A MAP IS A BY-VALUE EDGE at its field's position (§2.8, §3.1): its
    -- entries in ascending key order, each descended before the next
    local d = T.Tables[fd.entry]
    for _, kv in ipairs(D.map_entries(cache, fd, val)) do
      edges(T, d, kv, out, cache)
    end
  end
end

-- a union arm's payload is a field line: its pointers, blobs, tables, arrays
-- and nested unions are edges at the arm's position (§2.6, §3.1).
edge_union = function(T, u, val, out, cache)
  local tag = type(val) == "table" and val.tag or 0
  if tag == 0 or u == nil then return end
  local arm = u.arms[tag]
  if arm == nil or arm.field == nil then return end
  edge_value(T, arm.field, val[arm.slot], val, arm.slot, out, cache)
end

-- the pointer and blob edges of one node's by-value closure, in the walk's
-- order: fields in declaration order, array elements in index order. An absent
-- by-value node (nil) holds only its declared defaults and no pointer or blob
-- edge (§3, §4.2). AN ABSENT ?T OWNS NO NODE (§2.3): the walk gates on the
-- presence companion, so a value made absent after it was present writes no
-- stale node for the edges its untouched storage still holds.
edges = function(T, desc, v, out, cache)
  if v == nil then return out end
  for _, fd in ipairs(desc.fields) do
    if D.guard_holds(fd, v) and not (fd.optional and not v[fd.name .. "_present"]) then
      edge_value(T, fd, v[fd.name], v, fd.name, out, cache)
    end
  end
  return out
end

-- D.number_nodes numbers the nodes (§3.1, internal/tablewire/nodes.go):
-- first-visit order of a depth-first pre-order walk from root over POINTER
-- EDGES, every by-value edge descended in place. Node index 1 is the root and
-- record k is index k + 1. It answers
--   { list = { {typeId, codec, v} | {typeId, body, string, val}, ... },
--     index = { [value or blob box] = index },
--     refs = { [value or blob box] = how many slots name it },
--     entries = the maps' entry tables }
-- or nil for a DATA CYCLE (a reference to a node whose descent is still open),
-- or nil, "blob_shared" when refuse_shared is set and a blob is named twice
-- (§2.5, §16.7: a blob is owned by one slot; the save refuses it by name).
-- A BLOB NODE IS NUMBERED BY IDENTITY (§3.1, §6.3): a loaded blob is the box
-- load gave its node, so two distinct nodes with equal bytes stay two and one
-- box named from two slots is one node.
function D.number_nodes(T, desc, root, refuse_shared)
  local nodes = { list = {}, index = {}, refs = {}, entries = {} }
  local list, index, refs = nodes.list, nodes.index, nodes.refs
  index[root] = 1
  local open = { [root] = true }
  local stack = { { v = root, edges = edges(T, desc, root, {}, nodes.entries), i = 0 } }
  while #stack > 0 do
    local top = stack[#stack]
    if top.i >= #top.edges then
      open[top.v] = nil
      stack[#stack] = nil
    else
      local e = top.edges[top.i + 1]
      top.i = top.i + 1
      local val = e.val
      if e.blob then
        refs[val] = (refs[val] or 0) + 1
        if index[val] ~= nil then
          if refuse_shared then return nil, "blob_shared" end
        else
          local isstring = e.shape == "string_blob"
          local tid = isstring and STRING_BLOB_ID or BYTES_BLOB_ID
          local body = type(val) == "table" and val.value or val
          list[#list + 1] = { typeId = tid, body = body, string = isstring, val = val }
          index[val] = #list + 1
        end
      else
        if open[val] then return nil end
        refs[val] = (refs[val] or 0) + 1
        if index[val] == nil then
          local d = T.Tables[e.target]
          if d == nil then return nil end
          list[#list + 1] = { typeId = d.type_id, codec = d, v = val }
          index[val] = #list + 1
          open[val] = true
          stack[#stack + 1] = { v = val, edges = edges(T, d, val, {}, nodes.entries), i = 0 }
        end
      end
    end
  end
  return nodes
end

-- THE DEFAULTS IN PLACE (docs/PORTING.md M1; docs/SPEC-TABLES.md §3, §4). A
-- load fills caller-owned storage, so the caller's value is put back to the
-- declared defaults IN PLACE and read into: D.reset(T, desc, v) leaves v
-- deep-equal to the module's New<Name>() and allocates nothing where v already
-- has the shape (a nested table, a fixed array, a union value, a wide integer
-- and a wide string are reused; a list, a map and a counted array are emptied
-- to their born length). TestLuaResetEqualsTheConstructor holds it to the
-- constructor over every table.
local reset_value, reset_union, reset_array, reset_clear

-- empties a table of every key
reset_clear = function(t)
  for k in pairs(t) do
    t[k] = nil
  end
end

-- one array element's zero (§2.9): a record's own defaults, a union's tag 0,
-- None as false, and a scalar's zero; x is the slot's present value
local function reset_elem(T, e, x)
  local s = e.shape
  if s == "table" then
    local d = T.Tables[e.target]
    if type(x) == "table" then
      D.reset(T, d, x)
      return x
    end
    return D.fresh(T, d)
  elseif s == "union" then
    return reset_union(x)
  elseif s == "bool" or s == "enum" or s == "pointer" or s == "string_blob" or s == "bytes_blob" then
    return false
  elseif s == "float" then
    return 0.0
  elseif s == "string" or s == "bytes" then
    return ""
  elseif s == "wstring" then
    if type(x) == "table" then
      reset_clear(x)
      return x
    end
    return {}
  elseif s == "wide" then
    if type(x) == "table" then
      x.lo, x.hi = 0, 0
      return x
    end
    return { lo = 0, hi = 0 }
  end
  return 0
end

reset_union = function(x)
  if type(x) == "table" then
    reset_clear(x)
    x.tag = 0
    return x
  end
  return { tag = 0 }
end

reset_array = function(T, fd, x)
  local n
  if fd.list then
    n = 0
  elseif fd.fixed or fd.keyed ~= nil then
    n = fd.bound
  else
    n = fd.min or 0
  end
  if type(x) ~= "table" then x = {} end
  local e = fd.elem
  for i = 1, n do
    x[i] = reset_elem(T, e, x[i])
  end
  for i = #x, n + 1, -1 do
    x[i] = nil
  end
  return x
end

reset_value = function(T, fd, v)
  local name, s = fd.name, fd.shape
  if s == "array" then
    v[name] = reset_array(T, fd, v[name])
  elseif s == "map" or s == "wstring" then
    local x = v[name]
    if type(x) == "table" then
      reset_clear(x)
    else
      v[name] = {}
    end
  elseif s == "table" then
    local d = T.Tables[fd.target]
    local x = v[name]
    if type(x) == "table" then
      D.reset(T, d, x)
    else
      v[name] = D.fresh(T, d)
    end
  elseif s == "union" then
    v[name] = reset_union(v[name])
  elseif s == "wide" then
    local x, def = v[name], fd.default
    if type(x) == "table" then
      x.lo, x.hi = def.lo, def.hi
    else
      v[name] = { lo = def.lo, hi = def.hi }
    end
  elseif s == "pointer" or s == "string_blob" or s == "bytes_blob" then
    v[name] = nil
  elseif s == "enum" then
    v[name] = fd.default
  else
    v[name] = fd.default
  end
  if fd.optional then v[fd.pkey] = false end
end

-- one array element's zero, put in place into the slot x holds (a union's
-- table is emptied and tagged 0, a record reset), for a load that reads a fixed
-- array into the array it already holds
D.reset_elem = reset_elem

-- D.fresh builds one table's defaults, the descriptor's own constructor: the
-- module's New<Name>() where it has one (it always does), else the walk's.
function D.fresh(T, desc)
  local ctor = desc.ctor
  if ctor ~= nil then return ctor() end
  return D.reset(T, desc, {})
end

-- D.reset puts v back to desc's declared defaults in place and answers it.
function D.reset(T, desc, v)
  for _, fd in ipairs(desc.fields) do
    reset_value(T, fd, v)
  end
  return v
end

-- THE KEYED ACCESSOR (docs/PORTING.md M5; docs/SPEC-TABLES.md §2.4, §3.2). An
-- enum-keyed array has one slot per NAMED variant: key 1 lives at index 1 and
-- E.Max at the last, and None (key 0) has no slot. D.keyed(T, table, field)
-- resolves the field once and answers the accessor:
--   get(array, key), set(array, key, value)   one unsigned compare covers both
--     ends, so None, a negative key and any key past E.Max are refused in the
--     one place the key is spelled; a Lua error always runs, and a bare
--     `array[key]` is nil for all three and refuses nothing;
--   each(array)    for key, element in acc.each(array): the key beside the
--     element, so the caller never does the shift;
--   key(name)      the key of a variant NAME (the table wire's own identity,
--     §5), refused by name when the enum has no such variant;
--   max            E.Max.
function D.keyed(T, tname, fname)
  local desc = T.Tables[tname]
  if desc == nil then error("keyed: no table " .. tostring(tname), 2) end
  local f = D.field(desc, fname)
  if f == nil or f.keyed == nil then
    error("keyed: " .. tname .. "." .. tostring(fname) .. " is not an enum-keyed array", 2)
  end
  local n = #f.keyed
  local where = tname .. "." .. fname
  local function refuse(key)
    error(
      string.format("keyed_refused: key %s names no variant of %s (1 through %d; 0 is None)", tostring(key), where, n),
      3
    )
  end
  local by_name = {}
  for i, v in ipairs(f.keyed) do
    by_name[v[1]] = i
  end
  local function step(array, i)
    i = i + 1
    if i > n then return nil end
    return i, array[i]
  end
  return {
    max = n,
    get = function(array, key)
      if math.type(key) ~= "integer" or not math.ult(key - 1, n) then refuse(key) end
      return array[key]
    end,
    set = function(array, key, value)
      if math.type(key) ~= "integer" or not math.ult(key - 1, n) then refuse(key) end
      array[key] = value
    end,
    each = function(array)
      return step, array, 0
    end,
    key = function(name)
      local k = by_name[name]
      if k == nil then error(string.format("keyed_refused: %s names no variant of %s", tostring(name), where), 2) end
      return k
    end,
  }
end

-- THE DESCRIPTORS ARE CONSTANTS (docs/PORTING.md M13). The generated module
-- ends with D.seal(_M): first D.prepare builds, once, every fact the runtime
-- derives from the descriptors (a table's pointer reachability, the type-id
-- index, the fixed form's layout trees), then every data table the module
-- holds is frozen against NEW KEYS by a metatable whose __newindex refuses, so
-- a read-a-static, build-on-nil, store-back cache written into a descriptor
-- fails where it is written. Lua has no way to refuse the overwrite of a key
-- that is already there without moving the table behind a proxy (a function
-- call on every descriptor read), so the second half of the rule is a gate:
-- D.fingerprint(T) is the whole tree as one string and the test that runs
-- every surface requires it unchanged (TestLuaDescriptorsAreConstants).
local SEALED = {
  __newindex = function(_, k)
    error("a descriptor is a constant: refusing to write key " .. tostring(k) .. " into it (docs/PORTING.md M13)", 2)
  end,
  __metatable = false,
}

local function seal_tree(t, seen)
  if seen[t] then return end
  seen[t] = true
  for _, v in pairs(t) do
    if type(v) == "table" then seal_tree(v, seen) end
  end
  setmetatable(t, SEALED)
end

function D.prepare(T)
  local ids = {}
  for _, d in pairs(T.Tables) do
    ids[d.type_id] = d
    local nodes, string_blob, bytes_blob = D.pointer_reachable(T, d)
    d.reach = { nodes = nodes, string_blob = string_blob, bytes_blob = bytes_blob }
    -- the field of each wire id, the `| required` fields (false when there are
    -- none, so a load tracks arrivals only where a field is demanded) and each
    -- optional's presence key, so the load never scans a field list, builds
    -- a name or allocates a set per body
    local byid, required = {}, {}
    for _, f in ipairs(d.fields) do
      byid[f.id] = f
      if f.required then required[#required + 1] = f end
      if f.optional then f.pkey = f.name .. "_present" end
    end
    d.byid = byid
    d.required = #required > 0 and required or false
    d.ctor = T["New" .. d.name]
  end
  for _, u in pairs(T.Unions) do
    local byid = {}
    for _, a in ipairs(u.arms) do
      byid[a.id] = a
    end
    u.byid = byid
  end
  T.TypeIds = ids
  if T.Fixed ~= nil then require("fixed").prepare(T) end
end

function D.seal(T)
  D.prepare(T)
  local seen = {}
  for _, v in pairs(T) do
    if type(v) == "table" then seal_tree(v, seen) end
  end
end

-- D.fingerprint answers the module's data as one string: every key and scalar
-- of every table the module holds, in sorted key order, a table reached twice
-- named by its first visit. Two calls on an unmodified module answer the same
-- string.
function D.fingerprint(T)
  local out, seen, n = {}, {}, 0
  local function walk(t, path)
    n = n + 1
    seen[t] = n
    local keys = {}
    for k in pairs(t) do
      keys[#keys + 1] = k
    end
    table.sort(keys, function(a, b)
      local ta, tb = type(a), type(b)
      if ta ~= tb then return ta < tb end
      if ta == "number" or ta == "string" then return a < b end
      return tostring(a) < tostring(b)
    end)
    for _, k in ipairs(keys) do
      local v = t[k]
      local at = path .. "." .. tostring(k)
      if type(v) == "table" then
        if seen[v] then
          out[#out + 1] = at .. "=@" .. seen[v]
        else
          out[#out + 1] = at .. "=#"
          walk(v, at)
        end
      elseif math.type(v) == "float" then
        out[#out + 1] = at .. "=float:" .. string.format("%a", v)
      elseif type(v) ~= "function" then
        out[#out + 1] = at .. "=" .. type(v) .. ":" .. tostring(v)
      end
    end
  end
  walk(T, "")
  return table.concat(out, "\n")
end

return D
