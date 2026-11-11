-- The round-trip gate's Lua probe (test/roundtrip):
--   LUA_PATH=<generated dir>/?.lua lua probe.lua <unit module> <inputs dir>
-- Every call is the generated API: the unit module and the runtime modules the
-- generator wrote beside it (load, save, json, retain).
local T = require(arg[1])
local load = require("load")
local save = require("save")
local json = require("json")
local retain = require("retain")
local inputs = arg[2]

local function read(path)
  local f = io.open(path, "rb")
  if not f then return nil end
  local s = f:read("a")
  f:close()
  return s
end

local function keep(root, name, ext, bytes)
  local f = assert(io.open(inputs .. "/out/" .. root .. "/" .. name .. ext, "wb"))
  f:write(bytes)
  f:close()
end

local function say(root, name, verb, verdict)
  io.write("RT ", root, " ", name, " ", verb, " ", (tostring(verdict):gsub("%s+", " ")), "\n")
end

local function why(r, e)
  if type(r) == "table" and r.reason ~= nil then return tostring(r.reason) end
  return tostring(e)
end

-- call answers f's verdict, and an error as a refusal by name.
local function call(f)
  local ok, v = pcall(f)
  if ok then return v end
  return "refused:error " .. tostring(v)
end

for line in io.lines(inputs .. "/cases.txt") do
  local root, name, class = line:match("^(%S+) (%S+) (%S+)$")
  if T.Tables[root] == nil then
    say(root, name, "probe", "refused:the unit generated no such root")
    goto continue
  end
  do
    local text = read(inputs .. "/samples/" .. root .. "/" .. name .. ".json")
    local v, want
    local fromjson = call(function()
      local r = {}
      v = json.FromJson(T, root, text, r)
      if v == nil then return "refused:" .. why(r) end
      return "ok"
    end)
    say(root, name, "fromjson", fromjson)
    if v == nil then goto continue end
    want = json.ToJson(T, root, v)
    local function loadVerdict(bytes)
      local r = {}
      local got, e = load.Load(T, root, bytes, r)
      if got == nil then return "refused:" .. why(r, e) end
      return json.ToJson(T, root, got) == want and "ok" or "diff"
    end
    local eng = read(inputs .. "/engine/" .. root .. "/" .. name .. ".bin")
    local wire
    say(root, name, "save", call(function()
      local w, e = save.Save(T, root, v)
      if w == nil then return "refused:" .. tostring(e) end
      wire = w
      keep(root, name, ".save", w)
      return "ok"
    end))
    if wire ~= nil then say(root, name, "load", call(function() return loadVerdict(wire) end)) end
    if eng ~= nil then say(root, name, "loadeng", call(function() return loadVerdict(eng) end)) end
    if class == "fixed" then
      say(root, name, "fixedsave", call(function()
        local fw, e = T.FixedSave(root, { v })
        if fw == nil then return "refused:" .. tostring(e) end
        keep(root, name, ".fixed", fw)
        return "ok"
      end))
      if eng ~= nil then
        say(root, name, "fixedloadeng", call(function()
          local vals, e = T.FixedLoad(root, eng, 4)
          if type(vals) ~= "table" or vals[1] == nil then return "refused:" .. tostring(e or vals) end
          return json.ToJson(T, root, vals[1]) == want and "ok" or "diff"
        end))
      end
    end
    if class == "variable" then
      if wire ~= nil then
        say(root, name, "retain", call(function()
          local store, r = {}, {}
          local got, e = retain.LoadRetain(T, root, wire, store, r)
          if got == nil then return "refused:" .. why(r, e) end
          local sr = {}
          local back, se = retain.SaveRetain(T, root, got, store, sr)
          if back == nil then return "refused:" .. tostring(se) end
          return (json.ToJson(T, root, got) == want and back == wire) and "ok" or "diff"
        end))
      end
      say(root, name, "json", call(function()
        local r = {}
        local j = json.FromJson(T, root, want, r)
        if j == nil then return "refused:" .. why(r) end
        return json.ToJson(T, root, j) == want and "ok" or "diff"
      end))
    end
  end
  ::continue::
end
