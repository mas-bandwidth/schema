--!strict
-- Generated moo storage <-> the facade's owning values. Descriptors carry all
-- field names, bounds and enum variants; no parallel wire schema lives here.
local T = require("./CardTable")
local S = require("./schema_tables")
local L = require("./table_load")
local W = require("./table_save")
local J = require("./json")
local R = require("./table_retain")
local serialize = require("./serialize")
local M = {}

local function decode(desc: any, src: any, seen: any): any
  if src == nil then return nil end
  if seen[src] ~= nil then return seen[src] end
  local dst: any = {}
  seen[src] = dst
  for _, f in ipairs(desc.fields) do
    local v = src[f.slot]
    if f.s == "sblob" and v ~= nil then
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
    elseif f.cls == "I64" and type(v) == "number" then
      v = serialize.Int64.fromNumber(v)
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
  local value = T.NewCard()
  if not J.fromJson(T.CardDescriptor, value, text, report) then return nil end
  return fromStorage(value)
end
function M.Save(root: any): string?
  local ok, bytes = pcall(function() return W.save(T.CardDescriptor, toStorage(root)) end)
  return if ok and bytes ~= nil then buffer.tostring(bytes) else nil
end
function M.ToJSON(root: any): string?
  local ok, text = pcall(function() return J.toJson(T.CardDescriptor, toStorage(root)) end)
  return if ok then text else nil
end
function M.LoadRetain(wire: string, report: any, capacity: number, ids: number): (any, any)
  if type(wire) ~= "string" then return nil, nil end
  -- moo's retained occurrences are keyed by the decoded storage objects.
  -- Upgrade is a read-only policy check followed by a retaining write: keep
  -- those objects alive through both halves, rather than re-encoding a copy.
  local retained = R.newStore(capacity, ids)
  local value = R.LoadRetain("Card", buffer.fromstring(wire), report, retained)
  local normalized = fromStorage(value)
  return normalized, {store=retained, value=value, normalized=normalized}
end
function M.SaveRetain(root: any, retained: any, report: any): string?
  -- Private Upgrade seam only: no caller edits occur between load and save.
  assert(root == retained.normalized, "retaining write must pair with its load")
  local ok, bytes = pcall(function() return R.SaveRetain("Card", retained.value, retained.store, report) end)
  return if ok and bytes ~= nil then buffer.tostring(bytes) else nil
end
return M
