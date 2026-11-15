-- The Lua driver of the packet bare-integer write cases
-- (test/packet-write-bareint/cases.txt, SPEC.md §5.1 row 11): every case is
-- written through the generated Write<Type>; a refused case must return false
-- (never raise), and a written case must read back exactly.
--
-- The storage is the leg's own (docs/PORT-LUA.md, 64-BIT AND 128-BIT
-- INTEGERS): every field holds a Lua integer, the uint64 bit pattern for U64.
-- A value the integer cannot carry (above int64's max for I64; negative or
-- above 2^64-1 for U64) can only be held as a float, and that is the value
-- the case writes.
local generated = assert(arg[1], "usage: lua main.lua <generated dir> <fixture dir>")
local fixtures = assert(arg[2], "usage: lua main.lua <generated dir> <fixture dir>")
package.path = generated .. "/?.lua;" .. package.path

local serialize = require("serialize")
local B = require("BareInt")
local failures = 0
local count = 0

local function fail(what, detail)
  io.stderr:write("FAILED: packet-write-bareint " .. what .. ": " .. detail .. "\n")
  failures = failures + 1
end

-- cmpDec compares two decimal strings of non-negative integers.
local function cmpDec(a, b)
  if #a ~= #b then
    return #a < #b and -1 or 1
  end
  if a == b then
    return 0
  end
  return a < b and -1 or 1
end

-- valueOf is the value a caller holds for text in a field of type t.
local function valueOf(t, text)
  local neg = text:sub(1, 1) == "-"
  local digits = neg and text:sub(2) or text
  if t == "U64" then
    if neg or cmpDec(digits, "18446744073709551615") > 0 then
      return assert(tonumber(text), text) + 0.0
    end
    -- the bit pattern: integer arithmetic wraps past int64's max
    local v = 0
    for i = 1, #digits do
      v = v * 10 + (digits:byte(i) - 48)
    end
    return v
  end
  -- tonumber keeps a decimal int64 can carry an integer and lexes any other
  -- as a float
  return assert(tonumber(text), text)
end

-- write returns the verdict and, when written, the value read back; a raise
-- is reported as its own failure, never as a refusal.
local function write(t, value, what)
  local v = B["New" .. t]()
  v.Value = value
  local stream = serialize.WriteStream.new(32)
  local ran, ok = pcall(B["Write" .. t], stream, v)
  if not ran then
    fail(what, "the writer raised: " .. tostring(ok))
    return nil
  end
  if ok ~= true then
    return false
  end
  stream:flush()
  local back = B["New" .. t]()
  local readStream = serialize.ReadStream.new(stream:data())
  local readRan, readOK = pcall(B["Read" .. t], readStream, back)
  if not readRan then
    fail(what, "the reader raised: " .. tostring(readOK))
    return nil
  end
  if readOK ~= true then
    fail(what, "the reader refused the written bytes")
    return nil
  end
  return true, back.Value
end

local file = assert(io.open(fixtures .. "/cases.txt", "r"))
for line in file:lines() do
  local fields = {}
  if not line:match("^%s*#") then
    for word in line:gmatch("%S+") do fields[#fields + 1] = word end
  end
  if #fields > 0 then
    count = count + 1
    local name, t, text, verdict = fields[1], fields[2], fields[3], fields[4]
    local value = valueOf(t, text)
    local ok, back = write(t, value, name)
    if verdict == "refused" then
      if ok == true then
        fail(name .. " refused", "the writer wrote " .. text .. " and it read back as " .. tostring(back))
      end
    elseif verdict == "written" then
      if ok == false then
        fail(name .. " written", "the writer refused a value its width carries")
      elseif ok == true and not (math.type(back) == "integer" and back == value) then
        fail(name .. " reads back exactly", "read back " .. tostring(back) .. ", want " .. text)
      end
    else
      fail(name, "unknown verdict " .. tostring(verdict))
    end
  end
end
file:close()
if count == 0 then
  fail("cases", "the case list is empty")
end

-- A value that is not an integer is refused at every width: a fraction, a
-- whole float, a NaN, an infinity and a non-number.
local kinds = { "I8", "I16", "I32", "I64", "U8", "U16", "U32", "U64" }
local notIntegers = {
  { "fraction", 1.5 }, { "whole-float", 1.0 }, { "nan", 0 / 0 },
  { "infinity", math.huge }, { "text", "1" },
}
for _, t in ipairs(kinds) do
  for _, c in ipairs(notIntegers) do
    local what = t .. " " .. c[1]
    if write(t, c[2], what) == true then
      fail(what .. " refused", "the writer wrote a value that is not an integer")
    end
  end
end

if failures ~= 0 then
  io.stderr:write(string.format("packet-write-bareint Lua: %d failures over %d cases\n", failures, count))
  os.exit(1)
end
print(string.format("OK: packet-write-bareint Lua, %d cases; each out-of-width and non-integer value refused, each boundary written and read back", count))
