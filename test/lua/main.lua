-- THE LUA LEG's interpreter harness (docs/CONTRIBUTING.md, "Adding a language";
-- docs/PORT-LUA.md, THE HARNESS). Loads a generated module by the name the
-- probe's inputs.lua carries (or argv[1]) and checks the shape every module
-- has: a table carrying the unit's protocol id as an integer.
local name = arg[1]
if not name then
  local ok, inputs = pcall(require, "inputs")
  name = ok and inputs.module or nil
end
assert(name, "usage: lua test/lua/main.lua <generated module name>")
local m = require(name)
assert(type(m) == "table", "generated module must return a table")
assert(math.type(m.ProtocolId) == "integer", "ProtocolId is a Lua integer")
print("ok")
