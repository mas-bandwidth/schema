-- text.lua — the Lua table runtime's BASE64 TEXT carrier (docs/SPEC-TABLES.md
-- §16.8; docs/PORT-LUA.md, THE CARD MAP). Emitted beside the unit's
-- <Package>Tables.lua by internal/codegen/luatable, which embeds runtime/*.lua
-- by glob.
--
-- The text IS the binary wire: SaveText encodes the whole file Save writes,
-- form byte first, with the standard alphabet, padded (docs/SPEC-TABLES.md
-- §16.8). It is opaque — nothing is editable, and a hand edit is damage. The
-- Lua interpreter ships no base64 codec, so one is written here on immutable
-- byte strings and a table of pieces joined once by table.concat
-- (docs/PORT-LUA.md, BYTE BUFFERS).
--
-- LoadText decodes the body and hands it to the STRICT read
-- (runtime/refusal.lua, §6.5): a body that decodes but is not canonical (a pad
-- bit set, a CR or LF present) is base64_not_canonical, any other body that
-- does not decode is base64_damaged,
-- and a decoded wire the binary reader refuses keeps the binary reader's name
-- (§16.8). The integrity trailer rides inside the wire with no rule of its own
-- (§2.14), so the CRC is the strict read's, never this carrier's.
local load = require("load")
local save = require("save")
local refusal = require("refusal")

local Text = {}

local alphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/"
local enc = {}
local dec = {}
for i = 1, 64 do
  local c = alphabet:sub(i, i)
  enc[i - 1] = c
  dec[c] = i - 1
end

-- THE PAD (§16.8: standard alphabet, padded): the text length is always a
-- multiple of four, and a one- or two-byte tail carries two or one '='.
local function pad(out, n)
  if n > 0 then out[#out + 1] = string.rep("=", n) end
end

-- ENCODE (§16.8): three bytes to four characters, the standard alphabet.
local function encode(s)
  local out = {}
  local n = #s
  local i = 1
  while i + 2 <= n do
    local a, b, c = s:byte(i, i + 2)
    local v = (a << 16) | (b << 8) | c
    out[#out + 1] = enc[(v >> 18) & 63]
    out[#out + 1] = enc[(v >> 12) & 63]
    out[#out + 1] = enc[(v >> 6) & 63]
    out[#out + 1] = enc[v & 63]
    i = i + 3
  end
  local rem = n - i + 1
  if rem == 1 then
    local v = s:byte(i) << 16
    out[#out + 1] = enc[(v >> 18) & 63]
    out[#out + 1] = enc[(v >> 12) & 63]
    pad(out, 2)
  elseif rem == 2 then
    local a, b = s:byte(i, i + 1)
    local v = (a << 16) | (b << 8)
    out[#out + 1] = enc[(v >> 18) & 63]
    out[#out + 1] = enc[(v >> 12) & 63]
    out[#out + 1] = enc[(v >> 6) & 63]
    pad(out, 1)
  end
  return table.concat(out)
end

-- DECODE BODY (§16.8): nil for a body that is not padded standard base64 — a
-- length that is not a multiple of four, a byte outside the alphabet, or a
-- pad anywhere but the last one or two characters. Else the bytes, and true
-- when the last quantum's pad bits are zero (the canonical encoding).
local function decode_body(s)
  local n = #s
  if n % 4 ~= 0 then return nil end
  local padn = 0
  if n >= 1 and s:sub(n, n) == "=" then
    padn = 1
    if n >= 2 and s:sub(n - 1, n - 1) == "=" then padn = 2 end
  end
  local out = {}
  local canonical = true
  for i = 1, n, 4 do
    local a = dec[s:sub(i, i)]
    local b = dec[s:sub(i + 1, i + 1)]
    if a == nil or b == nil then return nil end
    if padn > 0 and i == n - 3 then
      local count = 4 - padn
      local third, fourth
      if count >= 3 then
        third = dec[s:sub(i + 2, i + 2)]
        if third == nil then return nil end
      end
      if count >= 4 then
        fourth = dec[s:sub(i + 3, i + 3)]
        if fourth == nil then return nil end
      end
      if count == 3 then
        if third & 3 ~= 0 then canonical = false end
        local v = (a << 18) | (b << 12) | (third << 6)
        out[#out + 1] = string.char((v >> 16) & 255, (v >> 8) & 255)
      else
        if b & 15 ~= 0 then canonical = false end
        local v = (a << 18) | (b << 12)
        out[#out + 1] = string.char((v >> 16) & 255)
      end
    else
      local c = dec[s:sub(i + 2, i + 2)]
      local d = dec[s:sub(i + 3, i + 3)]
      if c == nil or d == nil then return nil end
      local v = (a << 18) | (b << 12) | (c << 6) | d
      out[#out + 1] = string.char((v >> 16) & 255, (v >> 8) & 255, v & 255)
    end
  end
  return table.concat(out), canonical
end

-- DECODE (docs/SPEC-TABLES.md §16.8, the mechanical rule): the bytes of a
-- CANONICAL body, else nil and the refusal's name. A text is
-- base64_not_canonical when dropping its CR and LF bytes leaves padded
-- standard base64 that decodes but the text is not exactly the canonical
-- encoding of those bytes (a pad bit set, or a CR or LF present); every other
-- failure is base64_damaged.
local function decode(s)
  local wire, canonical = decode_body(s)
  if wire ~= nil then
    if canonical then return wire end
    return nil, "base64_not_canonical"
  end
  local stripped = (s:gsub("[\r\n]", ""))
  if stripped ~= s and decode_body(stripped) ~= nil then return nil, "base64_not_canonical" end
  return nil, "base64_damaged"
end

-- SaveText(T, name, value) is the padded standard base64 of the whole binary
-- wire Save writes (docs/SPEC-TABLES.md §16.8): LoadText(SaveText(v)) equals
-- Save(v) byte for byte.
function Text.SaveText(T, name, v)
  local wire, why = save.Save(T, name, v)
  if wire == nil then error(why or "invalid_value", 0) end
  return encode(wire)
end

-- LoadText(T, name, text [, report [, integrity]]) decodes one body and reads
-- it as the STRICT read (docs/SPEC-TABLES.md §16.8, §6.5): a body that decodes
-- but is not canonical refuses base64_not_canonical, any other body that does
-- not decode refuses base64_damaged, and a decoded wire refuses with the
-- binary reader's own name (§3's form byte, §2.14's trailer, or the report's
-- reason). `integrity` is the root's declared check, "crc32c" when the unit
-- declares one. A supplied report receives this read's counts on success.
function Text.LoadText(T, name, text, report, integrity)
  if report ~= nil then
    report.malformed = false
    report.refused = 0
    report.unknown = 0
    report.kindMismatch = 0
    report.clamped = 0
    report.widened = 0
    report.duplicate = 0
  end
  if type(text) ~= "string" then error("base64_damaged", 0) end
  local wire, why = decode(text)
  if wire == nil then error(why, 0) end
  local root = refusal.ReadStrict(T, name, wire, integrity)
  if report ~= nil and integrity == nil then load.LoadTolerant(T, name, wire, report) end
  return root
end

return Text
