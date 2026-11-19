-- messagefuzz.lua — Form-2 differential message-fuzz pipe driver for Lua.
local GENERATED = arg[1] or os.getenv("LUA_CONFORMANCE_GENERATED") or "build/conformance-lua"

local stdin, stdout = io.stdin, io.stdout
stdout:setvbuf("full")

local function read_exact(n)
  if n == 0 then return "" end
  local s = stdin:read(n)
  if s == nil or #s ~= n then return nil end
  return s
end

local function read_u16()
  local s = read_exact(2)
  return s and string.unpack("<I2", s)
end

local function read_u32()
  local s = read_exact(4)
  return s and string.unpack("<I4", s)
end

local function read_name()
  local n = read_u16()
  if n == nil then return nil end
  return read_exact(n)
end

local function new_report()
  return {
    unknown = 0,
    kindChanged = 0,
    widened = 0,
    outOfRange = 0,
    duplicate = 0,
    damaged = false,
    refused = 0,
    nonFiniteFloat = nil,
    reason = nil,
  }
end

local units = {}

local function table_module(key)
  local p = io.popen("ls " .. GENERATED .. "/" .. key .. "/*Tables.lua 2>/dev/null")
  if p == nil then return nil end
  local path = p:read("*l")
  p:close()
  return path
end

local function make_vocab(n)
  local v = {}
  local cap = math.max(n or 0, 4096)
  for i = 1, cap do v[i] = {} end
  return v
end

local function load_unit(key)
  local u = units[key]
  if u ~= nil then return u end
  units[key] = false
  if not key:match("^[%w_%-]+$") then return false end
  local path = table_module(key)
  if path == nil then return false end
  local dir, module = path:match("^(.*)/([^/]+)%.lua$")
  local before = {}
  for name in pairs(package.loaded) do before[name] = true end
  local saved_path = package.path
  package.path = dir .. "/?.lua"
  local ok, T = pcall(function()
    return require(module)
  end)
  package.path = saved_path
  for name in pairs(package.loaded) do
    if not before[name] then package.loaded[name] = nil end
  end
  if not ok or type(T) ~= "table" or T.AnnounceRead == nil or T.LoadMessages == nil or T.SaveMessages == nil or T.Announcement == nil then
    return false
  end
  local own_vocab = make_vocab(T.MessageEntries)
  local ar = new_report()
  if not T.AnnounceRead(own_vocab, T.Announcement, ar) then
    return false
  end
  u = { T = T, own_vocab = own_vocab }
  units[key] = u
  return u
end

local count = read_u32()
if count == nil then os.exit(0) end
local roster = {}
local known = {}
for i = 1, count do
  local unit = read_name()
  local root = read_name()
  local head = read_exact(2)
  if unit == nil or root == nil or head == nil then os.exit(2) end
  local form, retain = head:byte(1), head:byte(2)
  local entry = nil
  if form == 5 and retain == 0 then
    local u = load_unit(unit)
    if u and u.T.MessageRoots and u.T.MessageRoots[root] then
      entry = { unit = u, root = root }
    end
  end
  roster[i] = entry
  known[i] = entry and "\1" or "\0"
end
stdout:write(table.concat(known))
stdout:flush()

while true do
  local index = read_u32()
  if index == nil then break end
  local size = read_u32()
  if size == nil then os.exit(2) end
  local frame = read_exact(size)
  if frame == nil then os.exit(2) end
  local e = roster[index + 1]
  if e == nil then os.exit(2) end
  local T = e.unit.T
  local rep = new_report()
  local loaded = 0
  local malformed = 0
  local refused = 0
  local saved = nil

  if #frame < 4 then
    malformed = 1
    refused = 1
  else
    local alen = string.unpack("<I4", frame, 1)
    if 4 + alen > #frame then
      malformed = 1
      refused = 1
    else
      local vocab = e.unit.own_vocab
      local ann_ok = true
      if alen > 0 then
        vocab = make_vocab(4096)
        local ann = frame:sub(5, 4 + alen)
        if not T.AnnounceRead(vocab, ann, rep) or rep.damaged or (rep.refused and rep.refused ~= 0) or (rep.reason ~= nil and rep.reason ~= "") then
          ann_ok = false
        end
      end
      if not ann_ok then
        malformed = rep.damaged and 1 or 0
        refused = 1
      else
        local batch = frame:sub(5 + alen)
        local values = T.LoadMessages(e.root, vocab, batch, rep)
        local ok = values ~= nil and #values >= 1 and not rep.damaged and (rep.refused == nil or rep.refused == 0) and rep.nonFiniteFloat == nil and (rep.reason == nil or rep.reason == "")
        if not ok then
          malformed = rep.damaged and 1 or 0
          refused = 1
        else
          saved = T.SaveMessages(e.root, e.unit.own_vocab, values)
          if saved == nil then
            refused = 1
          else
            loaded = 1
          end
        end
      end
    end
  end

  local slen = saved and #saved or -1
  local hdr = string.pack("<Bi4i4i4i4i4BBi8i4i4i8",
    loaded, rep.unknown or 0, 0, rep.widened or 0, 0, 0,
    malformed, refused, -1, 0, 0, slen)
  stdout:write(hdr)
  if saved ~= nil then stdout:write(saved) end
  stdout:flush()
end
