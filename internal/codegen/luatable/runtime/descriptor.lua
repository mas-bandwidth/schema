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
    if math.type(v) ~= "float" or not (elem or v == d.default or (v ~= v and d.default ~= d.default)) then
      return fail(where, "float at its default")
    end
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

return D
