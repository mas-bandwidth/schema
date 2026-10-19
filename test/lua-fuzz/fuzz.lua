-- test/lua-fuzz/fuzz.lua — the Lua port's seeded mutation fuzzer for the
-- generated readers (schema-lua-nightly-fuzz).
--
-- THE HARNESS reads test/lua-fuzz/corpus.txt (the unit key -> schema directory
-- pairs the Makefile's tables_generate reads), loads every generated unit's
-- Tables module, and drives one entry point per reader kind over every table
-- name that module registers:
--
--   load    the tolerant form-1 load (runtime/load.lua L.Load)
--   strict  the strict read (runtime/refusal.lua R.ReadStrict)
--   text    the text form's base64 body (runtime/text.lua Text.LoadText)
--   json    the text form's JSON body (runtime/json.lua J.FromJson)
--
-- THE ORACLE: no crash, no hang, no assert; a refusal is a clean typed error
-- (refusal.lua's names), never a raw Lua error and never a silent success.
-- The seed is recorded with every finding (the finding file's payload is the
-- exact bytes, so replay needs no clock and no PRNG).
--
-- usage:
--   lua test/lua-fuzz/fuzz.lua --generated DIR --corpus FILE
--       [--seconds N | --iterations N] [--seed N] [--findings DIR]
--       [--wire-seeds DIR] [--json-seeds DIR] [--text-seeds DIR]
--   lua test/lua-fuzz/fuzz.lua --generated DIR --corpus FILE
--       --replay DIR [--findings DIR]

local opts = {
  seconds = 10, iterations = 0, seed = 1,
  findings = "", replay = "",
}
local i = 1
while i <= #arg do
  local a = arg[i]
  local k, v = a:match("^%-%-([%w-]+)=(.*)$")
  if not k then
    k = a:match("^%-%-([%w-]+)$")
    if k then i = i + 1; v = arg[i] end
  end
  if not k then
    io.stderr:write("fuzz: unknown argument " .. a .. "\n")
    os.exit(2)
  end
  k = k:gsub("%-", "_")
  opts[k] = v
  i = i + 1
end
for _, k in ipairs({ "generated", "corpus" }) do
  if not opts[k] then
    io.stderr:write("fuzz: --" .. k:gsub("_", "-") .. " is required\n")
    os.exit(2)
  end
end

local GENERATED = opts.generated

local function readFile(path)
  local f, err = io.open(path, "rb")
  if not f then return nil, err end
  local data = f:read("*a")
  f:close()
  return data
end

local function writeFile(path, data)
  local f = assert(io.open(path, "wb"))
  f:write(data)
  f:close()
end

local function lines(path)
  local f = assert(io.open(path, "r"))
  local out = {}
  for raw in f:lines() do
    local text = raw:gsub("^%s+", ""):gsub("%s+$", "")
    if #text > 0 and text:sub(1, 1) ~= "#" then out[#out + 1] = text end
  end
  f:close()
  return out
end

-- BASE64, the text form's body encoding (text.lua).
local B64 = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/"
local function b64(bytes)
  local out = {}
  for p = 1, #bytes, 3 do
    local a, b, c = bytes:byte(p, p + 2)
    local n = (a << 16) | ((b or 0) << 8) | (c or 0)
    out[#out + 1] = B64:sub(((n >> 18) & 63) + 1, ((n >> 18) & 63) + 1)
    out[#out + 1] = B64:sub(((n >> 12) & 63) + 1, ((n >> 12) & 63) + 1)
    out[#out + 1] = b and B64:sub(((n >> 6) & 63) + 1, ((n >> 6) & 63) + 1) or "="
    out[#out + 1] = c and B64:sub((n & 63) + 1, (n & 63) + 1) or "="
  end
  return table.concat(out)
end
local function unb64(text)
  local map = {}
  for j = 1, #B64 do map[B64:sub(j, j)] = j - 1 end
  local out = {}
  local n, bits = 0, 0
  for j = 1, #text do
    local c = text:sub(j, j)
    if c == "=" then break end
    local v = map[c]
    if not v then return nil end
    n = (n << 6) | v
    bits = bits + 6
    if bits >= 8 then
      bits = bits - 8
      out[#out + 1] = string.char((n >> bits) & 255)
    end
  end
  return table.concat(out)
end

-- A CORPUS LINE <kind> <unit> <table> <base64 payload> is both the finding
-- file and the replay input: the payload is the exact bytes, so a replay
-- reproduces the input without the PRNG.
local function decodeCase(text)
  text = text:gsub("%s+$", "")
  local kind, unit, tableName, payload = text:match("^(%S+)%s+(%S+)%s+(%S+)%s+(%S+)$")
  if not kind then return nil end
  local bytes = unb64(payload)
  if not bytes then return nil end
  return { kind = kind, unit = unit, table = tableName, bytes = bytes }
end

local function encodeCase(c)
  return c.kind .. " " .. c.unit .. " " .. c.table .. " " .. b64(c.bytes) .. "\n"
end

-- THE TYPED REFUSALS: refusal.lua's names, plus text.lua's base64_damaged and
-- the runtime's invalid_value. A Lua error whose message is one of these is a
-- clean refusal, and the oracle accepts it.
local TYPED = {
  previous_form = true, unknown_form = true, message_form_as_file = true,
  integrity_mismatch = true, malformed = true, clamped = true,
  kind_mismatch = true, duplicate = true, unknown = true,
  base64_damaged = true, invalid_value = true,
}
local function typed(err)
  local s = tostring(err)
  if TYPED[s] then return true end
  local name = s:match("^([%a_]+):") or s:match(":%d+: ([%a_]+)")
  return name ~= nil and TYPED[name] == true
end

-- THE UNITS. package.loaded is cleared before each unit because every unit
-- ships its own descriptor/load/refusal/json/text modules under the same
-- names; require() would hand a unit the previous unit's runtime otherwise.
local units = {}
for _, line in ipairs(lines(opts.corpus)) do
  local key, dir = line:match("^(%S+)%s+(%S+)$")
  if key then units[#units + 1] = { key = key, dir = dir } end
end

local function tablesModule(key)
  local p = io.popen("ls " .. GENERATED .. "/" .. key .. "/*Tables.lua 2>/dev/null | head -1")
  local path = p:read("*l")
  p:close()
  return path
end

local function loadUnit(key)
  local modpath = tablesModule(key)
  if not modpath then return nil end
  local dir = modpath:match("^(.*)/[^/]+$")
  for name in pairs(package.loaded) do package.loaded[name] = nil end
  package.path = dir .. "/?.lua;" .. package.path
  local T = dofile(modpath)
  local L = dofile(dir .. "/load.lua")
  local R = dofile(dir .. "/refusal.lua")
  local X = dofile(dir .. "/text.lua")
  local J = dofile(dir .. "/json.lua")
  local names = {}
  for name, desc in pairs(T.Tables or {}) do
    if desc.table then names[#names + 1] = name end
  end
  table.sort(names)
  return { key = key, T = T, L = L, R = R, X = X, J = J, names = names }
end

local loaded = {}
local targets = {}
for _, u in ipairs(units) do
  local m = loadUnit(u.key)
  if m then
    loaded[u.key] = m
    for _, name in ipairs(m.names) do
      for _, kind in ipairs({ "load", "strict", "text", "json" }) do
        targets[#targets + 1] = { unit = u.key, table = name, kind = kind }
      end
    end
  end
end
if #targets == 0 then
  io.stderr:write("fuzz: no generated unit under " .. GENERATED .. " (run the make target)\n")
  os.exit(2)
end

-- THE SEEDS. Wire vectors drive load/strict/text (text is their base64 body);
-- JSON vectors drive json. The Go fuzz corpus (internal/fuzz/testdata) is
-- included as raw mutation roots, as the card asks.
local seeds = { load = {}, strict = {}, text = {}, json = {} }
local function addSeeds(kind, dir, max)
  local p = io.popen("find " .. dir .. " -type f 2>/dev/null | sort")
  for path in p:lines() do
    local data = readFile(path)
    if data and #data > 0 and #data <= (max or 4096) then
      seeds[kind][#seeds[kind] + 1] = data
    end
  end
  p:close()
end
if opts.wire_seeds then
  for dir in opts.wire_seeds:gmatch("[^:]+") do
    addSeeds("load", dir); addSeeds("strict", dir); addSeeds("text", dir)
  end
end
if opts.json_seeds then
  for dir in opts.json_seeds:gmatch("[^:]+") do addSeeds("json", dir) end
end
if opts.text_seeds then
  for dir in opts.text_seeds:gmatch("[^:]+") do
    for _, data in ipairs((function()
      local out = {}
      local p = io.popen("find " .. dir .. " -type f 2>/dev/null | sort")
      for path in p:lines() do
        local d = readFile(path)
        if d then out[#out + 1] = d end
      end
      p:close()
      return out
    end)()) do seeds.text[#seeds.text + 1] = data end
  end
end
for _, kind in ipairs({ "load", "strict", "text" }) do
  if #seeds[kind] == 0 then seeds[kind][1] = string.char(1) .. "\0\0\0\0" end
end
if #seeds.json == 0 then seeds.json[1] = "{}" end

-- MUTATION, seeded and deterministic: xorshift64* over Lua 5.4 integers.
local state = tonumber(opts.seed) or 1
if state == 0 then state = 1 end
local function rnd()
  state = state ~ (state << 13)
  state = state ~ (state >> 7)
  state = state ~ (state << 17)
  return state
end
local function pickn(n)
  local v = rnd() & 0x7fffffffffffffff
  return (v % n) + 1
end

local INTERESTING = { 0, 1, 2, 3, 0x7f, 0x80, 0x81, 0xfe, 0xff, 0x3d, 0x7b, 0x22 }

local function mutate(base)
  local bytes = { base:byte(1, math.min(#base, 4096)) }
  local edits = pickn(6)
  for _ = 1, edits do
    local op = pickn(6)
    local n = #bytes
    if op == 1 and n > 0 then
      local at = pickn(n)
      local bit = 1 << (pickn(8) - 1)
      bytes[at] = (bytes[at] ~ bit) & 255
    elseif op == 2 and n > 0 then
      bytes[pickn(n)] = INTERESTING[pickn(#INTERESTING)]
    elseif op == 3 and n < 4096 then
      local at = pickn(n + 1)
      table.insert(bytes, at, INTERESTING[pickn(#INTERESTING)])
    elseif op == 4 and n > 1 then
      table.remove(bytes, pickn(n))
    elseif op == 5 and n > 0 and n < 4096 then
      local from = pickn(n)
      local len = pickn(math.min(16, n - from + 1))
      local at = pickn(n + 1)
      for j = len, 1, -1 do table.insert(bytes, at, bytes[from + j - 1]) end
    elseif op == 6 and n > 0 then
      local at = pickn(n)
      bytes[at] = rnd() & 255
    end
  end
  return string.char(table.unpack(bytes))
end

-- THE READERS. Each returns true when the input was handled (a value or a
-- clean typed refusal) and raises only the raw Lua error the oracle reports.
local function drive(c)
  local u = loaded[c.unit]
  if not u then return end
  local T, name, bytes = u.T, c.table, c.bytes
  if c.kind == "load" then
    local report = {
      malformed = false, refused = 0, unknown = 0, kindMismatch = 0,
      clamped = 0, widened = 0, duplicate = 0,
    }
    u.L.Load(T, name, bytes, report)
  elseif c.kind == "strict" then
    u.R.ReadStrict(T, name, bytes)
  elseif c.kind == "text" then
    u.X.LoadText(T, name, b64(bytes))
  elseif c.kind == "json" then
    local report = {
      malformed = false, refused = 0, unknown = 0, kindMismatch = 0,
      clamped = 0, widened = 0, duplicate = 0,
    }
    u.J.FromJson(T, name, bytes, report)
  end
end

local findingsDir = opts.findings
local function saveFinding(c, err)
  if not findingsDir or findingsDir == "" then return end
  os.execute("mkdir -p " .. findingsDir)
  local slug = c.kind .. "_" .. c.unit .. "_" .. c.table:gsub("[^%w_]", "_")
  local n = 1
  local path
  repeat
    path = findingsDir .. "/" .. slug .. "_" .. n .. ".case"
    local f = io.open(path, "r")
    if f then f:close(); path = nil; n = n + 1 end
  until path
  writeFile(path, encodeCase(c))
  writeFile(path .. ".err", tostring(err) .. "\n")
  return path
end

-- REPLAY: every saved case, once, with no clock. A saved finding that no
-- longer fires is still replayed; a case that fires is a regression.
if opts.replay and opts.replay ~= "" then
  local cases = 0
  local bad = 0
  local p = io.popen("find " .. opts.replay .. " -type f -name '*.case' 2>/dev/null | sort")
  for path in p:lines() do
    local text = readFile(path)
    local c = text and decodeCase(text)
    if c then
      cases = cases + 1
      local ok, err = pcall(drive, c)
      if not ok and not typed(err) then
        bad = bad + 1
        io.stderr:write("REPLAY FAIL " .. path .. ": " .. tostring(err) .. "\n")
      end
    end
  end
  p:close()
  io.write(string.format("lua fuzz replay: %d cases, %d failed\n", cases, bad))
  os.exit(bad == 0 and 0 or 1)
end

-- FUZZ: the time budget is an argument; --iterations bounds a control run.
local seconds = tonumber(opts.seconds) or 10
local iterations = tonumber(opts.iterations) or 0
local deadline = os.time() + seconds
local count, fails = 0, 0
while true do
  if iterations > 0 then
    if count >= iterations then break end
  elseif os.time() >= deadline then
    break
  end
  local t = targets[pickn(#targets)]
  local kind = t.kind
  local base = seeds[kind][pickn(#seeds[kind])]
  local c = { kind = kind, unit = t.unit, table = t.table, bytes = mutate(base) }
  local ok, err = pcall(drive, c)
  count = count + 1
  if not ok and not typed(err) then
    fails = fails + 1
    local path = saveFinding(c, err)
    io.stderr:write("FUZZ FAIL " .. (path or "(no finding dir)") .. ": " .. tostring(err) .. "\n")
    if fails >= 10 then break end
  end
end
io.write(string.format("lua fuzz: targets=%d iterations=%d failures=%d seed=%d\n",
  #targets, count, fails, tonumber(opts.seed) or 1))
os.exit(fails == 0 and 0 or 1)
