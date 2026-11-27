-- The Lua 5.4 leg of test/packet-void (SPEC §4.8): answers every case of
-- testdata/wire/packet-void/cases.txt, one "case <name> ok|FAILED: <why>"
-- line each, for tools/casegate. The pins are the Go oracle's bytes, never
-- this writer's.
--
--   lua main.lua <generated directory> <testdata/wire/packet-void>
local usage = "usage: lua main.lua <generated dir> <pins dir>"
local dir = assert(arg[1], usage)
local pins = assert(arg[2], usage)
package.path = dir .. "/?.lua;" .. package.path

local serialize = require("serialize")
local V = require("Void")

local TYPES = { Mixed = true, Late = true, Tags = true, Single = true, Empty = true, Frame = true, Batch = true }

local function readFile(path)
  local f = assert(io.open(path, "rb"))
  local s = f:read("a")
  f:close()
  return s
end

local function hex(s)
  return (s:gsub(".", function(c) return string.format("%02x", c:byte()) end))
end

-- same compares the value's wire-meaningful fields: the selected arm and the
-- counted elements; an unselected arm and storage past a count are
-- unspecified (SPEC §4.8).
local function sameArm(a, b)
  if a.EntriesCount ~= b.EntriesCount or a.Marker ~= b.Marker then return false end
  for i = 1, a.EntriesCount do
    if a.Entries[i].Retries ~= b.Entries[i].Retries then return false end
  end
  return true
end

local function sameMixed(a, b)
  if a.Type ~= b.Type then return false end
  return a.Type ~= V.MixedType.Payload or sameArm(a.Payload, b.Payload)
end

local function same(typeName, a, b)
  if typeName == "Mixed" then return sameMixed(a, b) end
  if typeName == "Late" then
    if a.Type ~= b.Type then return false end
    return a.Type ~= V.LateType.Payload or sameArm(a.Payload, b.Payload)
  end
  if typeName == "Frame" then
    return a.Lead == b.Lead and a.Tail == b.Tail and sameMixed(a.Choice, b.Choice)
  end
  if typeName == "Batch" then
    if a.ValuesCount ~= b.ValuesCount then return false end
    for i = 1, a.ValuesCount do
      if not sameMixed(a.Values[i], b.Values[i]) then return false end
    end
    return true
  end
  return a.Type == b.Type
end

local function mixed(tag)
  local v = V.NewMixed()
  v.Type = tag
  return v
end

-- The value of each accept case, by name, and its type; nil for a name this
-- driver does not know, which is FAILED and never written with another
-- case's value.
local function build(name)
  if name == "mixed-none" then return mixed(V.MixedType.None), "Mixed" end
  if name == "mixed-ping" then return mixed(V.MixedType.Ping), "Mixed" end
  if name == "mixed-payload" then return mixed(V.MixedType.Payload), "Mixed" end
  if name == "late-payload" or name == "late-pong" then
    local v = V.NewLate()
    v.Type = name == "late-payload" and V.LateType.Payload or V.LateType.Pong
    return v, "Late"
  end
  if name == "tags-none" or name == "tags-ping" or name == "tags-pong" then
    local v = V.NewTags()
    v.Type = ({ ["tags-none"] = V.TagsType.None, ["tags-ping"] = V.TagsType.Ping, ["tags-pong"] = V.TagsType.Pong })[name]
    return v, "Tags"
  end
  if name == "single-none" or name == "single-ack" then
    local v = V.NewSingle()
    v.Type = name == "single-ack" and V.SingleType.Ack or V.SingleType.None
    return v, "Single"
  end
  if name == "empty-none" then return V.NewEmpty(), "Empty" end
  if name == "frame-ping" then
    local v = V.NewFrame()
    v.Lead = 5
    v.Choice = mixed(V.MixedType.Ping)
    v.Tail = 6
    return v, "Frame"
  end
  if name == "batch-empty" or name == "batch-ping" or name == "batch-ping-payload" then
    local v = V.NewBatch()
    if name == "batch-ping" or name == "batch-ping-payload" then
      v.Values[1] = mixed(V.MixedType.Ping)
      v.ValuesCount = 1
    end
    if name == "batch-ping-payload" then
      v.Values[2] = mixed(V.MixedType.Payload)
      v.ValuesCount = 2
    end
    return v, "Batch"
  end
  return nil
end

-- dirty fills the storage a read reuses, so a read that does not construct
-- the payload arm with its declared defaults shows as a value difference.
local function dirtyArm(arm)
  arm.EntriesCount = 2
  arm.Entries[1].Retries = 9
  arm.Entries[2].Retries = 9
  arm.Marker = 0
end

local function fresh(typeName)
  local v = V["New" .. typeName]()
  if typeName == "Mixed" or typeName == "Late" then dirtyArm(v.Payload) end
  if typeName == "Batch" then
    dirtyArm(v.Values[1].Payload)
    dirtyArm(v.Values[2].Payload)
  end
  return v
end

local function write(typeName, value)
  local w = serialize.WriteStream.new(256)
  if not V["Write" .. typeName](w, value) then return nil end
  local bits = w:bitsProcessed()
  w:flush()
  return w:data(), bits
end

-- The refuse-write cases: the type's default value with the one change the
-- case names. The writer's refusal is a false return; a raised error is not
-- the writer's refusal and is FAILED.
local function buildRefusal(name)
  local v, typeName
  if name == "mixed-write-tag-negative" or name == "mixed-write-tag-text" or name == "mixed-write-tag-3"
    or name == "mixed-write-payload-type" or name == "mixed-write-marker-8" then
    v, typeName = V.NewMixed(), "Mixed"
    if name == "mixed-write-tag-negative" then v.Type = -1 end
    if name == "mixed-write-tag-text" then v.Type = "ping" end
    if name == "mixed-write-tag-3" then v.Type = 3 end
    if name == "mixed-write-payload-type" then v.Type, v.Payload = V.MixedType.Payload, 1 end
    if name == "mixed-write-marker-8" then v.Type, v.Payload.Marker = V.MixedType.Payload, 8 end
  elseif name == "tags-write-tag-3" then
    v, typeName = V.NewTags(), "Tags"
    v.Type = 3
  elseif name == "empty-write-tag-1" then
    v, typeName = V.NewEmpty(), "Empty"
    v.Type = 1
  end
  return v, typeName
end

local function refuseWrite(name, typeName)
  local value, built = buildRefusal(name)
  if value == nil then return "unknown refuse-write case" end
  if built ~= typeName then return "case type " .. typeName .. ", the driver builds " .. built end
  local w = serialize.WriteStream.new(256)
  local ok, res = pcall(V["Write" .. typeName], w, value)
  if not ok then
    return "not the writer's refusal: " .. tostring(res)
  end
  if res ~= false then return "write accepted" end
  return nil
end

-- selectPayload reads mixed-ping's pin, then selects the payload arm the Lua
-- API's way: the union keeps its Payload object, so selecting is setting the
-- tag, and the arm must still carry its declared defaults.
local function selectPayload()
  local v = V.NewMixed()
  if not V.ReadMixed(serialize.ReadStream.new(readFile(pins .. "/mixed-ping.bin")), v) then
    return nil, "read mixed-ping refused"
  end
  if v.Type ~= V.MixedType.Ping then return nil, "read mixed-ping: tag " .. tostring(v.Type) end
  v.Type = V.MixedType.Payload
  local got, gotBits = write("Mixed", v)
  if got == nil then return nil, "write refused" end
  return got, gotBits
end

local function answer(name, typeName, verdict, bits)
  if not TYPES[typeName] then return "unknown type " .. typeName end
  if verdict == "refuse-write" then return refuseWrite(name, typeName) end
  if verdict == "select" then
    if name ~= "mixed-ping-select-payload" then return "unknown select case" end
    local got, gotBits = selectPayload()
    if got == nil then return gotBits end
    local pin = readFile(pins .. "/" .. name .. ".bin")
    if got ~= pin then return "write " .. hex(got) .. ", pin " .. hex(pin) end
    if gotBits ~= tonumber(bits) then return "write has " .. gotBits .. " bits, want " .. bits end
    return nil
  end
  local pin = readFile(pins .. "/" .. name .. ".bin")
  if verdict == "refuse" then
    if V["Read" .. typeName](serialize.ReadStream.new(pin), fresh(typeName)) then
      return "read accepted"
    end
    return nil
  end
  if verdict ~= "accept" then return "verdict " .. verdict end

  local value, built = build(name)
  if value == nil then return "unknown accept case" end
  if built ~= typeName then return "case type " .. typeName .. ", the driver builds " .. built end
  local want = tonumber(bits)

  local got, gotBits = write(typeName, value)
  if got == nil then return "write refused" end
  if got ~= pin then return "write " .. hex(got) .. ", pin " .. hex(pin) end
  if gotBits ~= want then return "write has " .. gotBits .. " bits, want " .. bits end

  local back = fresh(typeName)
  if not V["Read" .. typeName](serialize.ReadStream.new(pin), back) then
    return "read refused"
  end
  if not same(typeName, back, value) then return "read value differs" end
  local again = write(typeName, back)
  if again ~= pin then return "write what was read differs" end

  -- The body read stops before the end check, so its cursor is the object's
  -- bit count.
  local body = serialize.ReadStream.new(pin)
  if not V["Read" .. typeName .. "Body"](body, fresh(typeName)) then
    return "body read refused"
  end
  if body:bitsProcessed() ~= want then
    return "read has " .. body:bitsProcessed() .. " bits, want " .. bits
  end
  return nil
end

for line in (readFile(pins .. "/cases.txt") .. "\n"):gmatch("([^\n]*)\n") do
  local fields = {}
  for f in line:gmatch("%S+") do fields[#fields + 1] = f end
  if #fields > 0 and fields[1]:sub(1, 1) ~= "#" then
    if #fields < 4 then
      print("case " .. fields[1] .. " FAILED: a case line needs <name> <type> <verdict> <bits>")
    else
      local ok, why = pcall(answer, fields[1], fields[2], fields[3], fields[4])
      if not ok then why = "raised: " .. tostring(why) end
      print(why and ("case " .. fields[1] .. " FAILED: " .. why) or ("case " .. fields[1] .. " ok"))
    end
  end
end
