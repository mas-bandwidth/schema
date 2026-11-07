--!strict
-- Generated Luau storage <-> the facade's owning values. Descriptors carry all
-- field names, bounds and enum variants; no parallel wire schema lives here.
local T = require("./CardTable")
local S = require("./schema_tables")
local L = require("./schema_load")
local W = require("./schema_save")
local J = require("./schema_json")
local R = require("./schema_retain")
local M = {}

local function decode(desc: any, src: any, seen: any): any
  if src == nil then return nil end
  if seen[src] ~= nil then return seen[src] end
  local dst: any = {}
  seen[src] = dst
  for _, f in ipairs(desc.fields) do
    local v = src[f.slot]
    if f.s == "str" then
      v = buffer.readstring(v, 0, src[f.slot .. "Length"])
    elseif f.s == "sblob" and v ~= nil then
      local box = seen[v]
      if box == nil then box = { value = buffer.tostring(v) }; seen[v] = box end
      v = box
    elseif f.s == "enum" then
      v = if v == 0 then nil else f.vs[v][1]
    elseif f.s == "arr" then
      local list = {}
      for i, item in ipairs(v) do list[i] = decode(S.lookup(f.e.t), item, seen) end
      v = list
    end
    dst[f.name] = v
  end
  return dst
end

local function encode(desc: any, src: any, seen: any): any
  if seen[src] ~= nil then return seen[src] end
  local dst = desc.new()
  seen[src] = dst
  for _, f in ipairs(desc.fields) do
    local v = src[f.name]
    if f.s == "str" then
      assert(type(v) == "string" and #v <= f.bound, "invalid bounded string")
      buffer.writestring(dst[f.slot], 0, v)
      dst[f.slot .. "Length"] = #v
      v = dst[f.slot]
    elseif f.s == "sblob" and v ~= nil then
      local bytes = seen[v]
      if bytes == nil then bytes = buffer.fromstring(v.value); seen[v] = bytes end
      v = bytes
    elseif f.s == "enum" then
      local ordinal = 0
      if v ~= nil then
        for i, variant in ipairs(f.vs) do if (variant :: any)[1] == v then ordinal = i; break end end
        assert(ordinal ~= 0, "invalid enum")
      end
      v = ordinal
    elseif f.s == "arr" then
      local list = {}
      for i, item in ipairs(v) do list[i] = encode(S.lookup(f.e.t), item, seen) end
      v = list
    elseif f.dom == "integer" and type(v) == "number" then
      v = integer.create(v)
    end
    dst[f.slot] = v
  end
  return dst
end

local function fromStorage(src: any): any
  return decode(T.CardDescriptor, src, {})
end
local function toStorage(src: any): any
  return encode(T.CardDescriptor, src, {})
end
function M.NewCard(): any return fromStorage(T.NewCard()) end
function M.Load(wire: string, report: any): any
  if type(wire) ~= "string" then return nil end
  return fromStorage(L.loadTolerant("Card", buffer.fromstring(wire), report))
end
function M.FromJSON(text: string, report: any): any
  return fromStorage(J.FromJsonTolerant("Card", buffer.fromstring(text), report))
end
function M.Save(root: any): string?
  local ok, bytes = pcall(function() return W.save(T.CardDescriptor, toStorage(root)) end)
  return if ok and bytes ~= nil then buffer.tostring(bytes) else nil
end
function M.ToJSON(root: any): string?
  local ok, bytes = pcall(function() return J.ToJson("Card", toStorage(root), {}) end)
  return if ok and bytes ~= nil then buffer.tostring(bytes) else nil
end
function M.LoadRetain(wire: string, report: any, capacity: number, ids: number): (any, any)
  if type(wire) ~= "string" then return nil, nil end
  local value, retained = R.LoadRetainTolerant("Card", buffer.fromstring(wire), report, capacity, ids)
  return fromStorage(value), retained
end
function M.SaveRetain(root: any, retained: any, report: any): string?
  local ok, bytes = pcall(function() return R.SaveRetain("Card", toStorage(root), retained, report) end)
  return if ok and bytes ~= nil then buffer.tostring(bytes) else nil
end
return M
