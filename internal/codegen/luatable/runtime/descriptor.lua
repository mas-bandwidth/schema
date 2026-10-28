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
        return deprecated_value(T, arm.field, val[arm.name], u.name .. "." .. arm.name, seen)
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

-- THE FINITE RULE ON WRITE (docs/SPEC-TABLES.md §3.3; Glenn, 2026-10-27,
-- "tables go finite-only too, same rule as packets"): every writer refuses a
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
        return non_finite_value(T, arm.field, val[arm.name], u.name .. "." .. arm.name, seen)
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

return D
