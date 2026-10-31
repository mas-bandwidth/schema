-- json.lua — the Lua table runtime's TEXT FORM (docs/SPEC-TABLES.md §16,
-- §16.1–§16.7; docs/PORT-LUA.md, THE CARD MAP). A port of the JavaScript leg's
-- variable text walk (internal/codegen/jstable/variableruntime.go) and of the
-- Go reference engine (internal/tabletext/write.go, read.go): one JSON text in
-- and out of one table, driven by the descriptor (§8.1).
--
-- Every value is written in DECLARATION order, defaults included, pretty-printed
-- with a two-space indent and one trailing newline (§16.1). The reader accepts
-- comments and trailing commas and never writes them; unknown keys are skipped
-- and counted, a repeated key is last-wins and counted, and a value of the wrong
-- JSON type is a kind mismatch, never coerced (§16.2). A node named by more than
-- one pointer slot is DEFINED once as `{ "&node": N, ... }` and REFERENCED by
-- `{ "&node": N }` after that (§16.7).
local D = require("descriptor")
local J = {}

local MAX_DEPTH = 128
local MAX_KEY = 255
local MAX_NUMBER = 511
local BASE64 = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/"

----------------------------------------------------------------------------
-- UTF-8
----------------------------------------------------------------------------

local function utf8_encode(code)
  if code < 0x80 then
    return string.char(code)
  elseif code < 0x800 then
    return string.char(0xc0 | (code >> 6), 0x80 | (code & 0x3f))
  elseif code < 0x10000 then
    return string.char(0xe0 | (code >> 12), 0x80 | ((code >> 6) & 0x3f), 0x80 | (code & 0x3f))
  end
  return string.char(
    0xf0 | (code >> 18),
    0x80 | ((code >> 12) & 0x3f),
    0x80 | ((code >> 6) & 0x3f),
    0x80 | (code & 0x3f)
  )
end

-- the length of one WELL-FORMED UTF-8 sequence at s[i], or -1 (§16.3)
local function utf8_seq(s, i)
  local c = s:byte(i)
  if c == nil then return -1 end
  if c < 0x80 then return 1 end
  local want, code
  if c >= 0xc2 and c <= 0xdf then
    want, code = 2, c & 0x1f
  elseif c >= 0xe0 and c <= 0xef then
    want, code = 3, c & 0x0f
  elseif c >= 0xf0 and c <= 0xf4 then
    want, code = 4, c & 0x07
  else
    return -1
  end
  if i + want - 1 > #s then return -1 end
  for k = 1, want - 1 do
    local b = s:byte(i + k)
    if b < 0x80 or b > 0xbf then return -1 end
    code = (code << 6) | (b & 0x3f)
  end
  if want == 3 and code < 0x800 then return -1 end
  if want == 4 and code < 0x10000 then return -1 end
  if code >= 0xd800 and code <= 0xdfff then return -1 end
  if code > 0x10ffff then return -1 end
  return want
end

local function utf8_decode(s)
  local c = s:byte(1)
  if c < 0x80 then return c end
  local code, at
  if c < 0xe0 then
    code, at = c & 0x1f, 2
  elseif c < 0xf0 then
    code, at = c & 0x0f, 3
  else
    code, at = c & 0x07, 4
  end
  for i = 2, at do
    code = (code << 6) | (s:byte(i) & 0x3f)
  end
  return code
end

----------------------------------------------------------------------------
-- the reader (§16.2)
----------------------------------------------------------------------------

local function new_in(text)
  return { t = text, p = 1, bad = false }
end

local function space(inp)
  while inp.p <= #inp.t do
    local c = inp.t:byte(inp.p)
    if c == 32 or c == 9 or c == 10 or c == 13 then
      inp.p = inp.p + 1
    elseif c == 47 then
      local n = inp.t:byte(inp.p + 1)
      if n == 47 then
        inp.p = inp.p + 2
        while inp.p <= #inp.t and inp.t:byte(inp.p) ~= 10 do
          inp.p = inp.p + 1
        end
      elseif n == 42 then
        local e = inp.t:find("*/", inp.p + 2, true)
        if e == nil then
          inp.bad = true
          inp.p = #inp.t + 1
          return
        end
        inp.p = e + 2
      else
        inp.bad = true
        return
      end
    else
      return
    end
  end
end

local function peek(inp)
  space(inp)
  if inp.p <= #inp.t then return inp.t:sub(inp.p, inp.p) end
  return nil
end

local function literal(inp, word)
  if inp.t:sub(inp.p, inp.p + #word - 1) == word then
    inp.p = inp.p + #word
    return true
  end
  inp.bad = true
  return false
end

local function hex4(inp)
  if inp.p + 3 > #inp.t then return nil end
  local s = inp.t:sub(inp.p, inp.p + 3)
  if not s:match("^%x%x%x%x$") then return nil end
  inp.p = inp.p + 4
  return tonumber(s, 16)
end

-- ONE STRING BODY CHARACTER as UTF-8: an escape's code point or one UTF-8
-- sequence read whole, so a clamp lands only between code points (§16.3).
local function scan_unit(inp)
  if inp.p > #inp.t then
    inp.bad = true
    return nil, false, false
  end
  local c = inp.t:byte(inp.p)
  if c == 34 then
    inp.p = inp.p + 1
    return nil, true, true
  end
  if c == 92 then
    inp.p = inp.p + 1
    if inp.p > #inp.t then
      inp.bad = true
      return nil, false, false
    end
    local e = inp.t:sub(inp.p, inp.p)
    inp.p = inp.p + 1
    local unit
    if e == '"' then
      unit = '"'
    elseif e == "\\" then
      unit = "\\"
    elseif e == "/" then
      unit = "/"
    elseif e == "b" then
      unit = "\b"
    elseif e == "f" then
      unit = "\f"
    elseif e == "n" then
      unit = "\n"
    elseif e == "r" then
      unit = "\r"
    elseif e == "t" then
      unit = "\t"
    elseif e == "u" then
      local high = hex4(inp)
      if high == nil then
        inp.bad = true
        return nil, false, false
      end
      local code = high
      if high >= 0xd800 and high <= 0xdbff and inp.p + 1 <= #inp.t and inp.t:sub(inp.p, inp.p + 1) == "\\u" then
        local mark = inp.p
        inp.p = inp.p + 2
        local low = hex4(inp)
        if low ~= nil and low >= 0xdc00 and low <= 0xdfff then
          code = 0x10000 + ((high - 0xd800) << 10) + (low - 0xdc00)
        else
          inp.p = mark
        end
      end
      if code >= 0xd800 and code <= 0xdfff then code = 0xfffd end
      unit = utf8_encode(code)
    else
      inp.bad = true
      return nil, false, false
    end
    return unit, false, true
  end
  if c < 0x20 then
    inp.bad = true
    return nil, false, false
  end
  -- a raw UTF-8 sequence, consumed whole; a byte sequence that is not
  -- well-formed UTF-8 is REFUSED BY NAME (string_not_utf8), never replaced
  -- with U+FFFD, and before any clamp (§16.3)
  local want = 1
  if c >= 0xc2 and c <= 0xdf then
    want = 2
  elseif c >= 0xe0 and c <= 0xef then
    want = 3
  elseif c >= 0xf0 and c <= 0xf4 then
    want = 4
  end
  if c >= 0x80 and c <= 0xbf then want = 1 end
  local start = inp.p
  inp.p = inp.p + 1
  while inp.p - start < want and inp.p <= #inp.t do
    local b = inp.t:byte(inp.p)
    if b < 0x80 or b > 0xbf then break end
    inp.p = inp.p + 1
  end
  local unit = inp.t:sub(start, inp.p - 1)
  if utf8_seq(unit, 1) ~= #unit then
    -- §16.3: ill-formed UTF-8 is refused by name at every string site, before
    -- any bound clamp; the read answers string_not_utf8 (<root> at the top)
    inp.utf8 = true
    inp.bad = true
    return nil, false, false
  end
  return unit, false, true
end

-- ONE JSON string into bytes, clamped at cap bytes (cap < 0 unbounded). A
-- fourth truth is returned when the body carried a U+0000 unit — the \u0000
-- escape into text storage §16.3 refuses as text_holds_zero — so the caller
-- that places the value can name the field; callers that only scan (an object
-- key that names no field, a skipped value) ignore it, because a zero that
-- never lands in text storage is not the event.
local function scan_string(inp, cap)
  if peek(inp) ~= '"' then
    inp.bad = true
    return nil, false, false
  end
  inp.p = inp.p + 1
  local out, n, clamped, nul = {}, 0, false, false
  while true do
    local unit, done, ok = scan_unit(inp)
    if not ok then return nil, false, false, false end
    if done then break end
    if unit == "\0" then nul = true end
    if clamped or (cap >= 0 and n + #unit > cap) then
      clamped = true
    else
      out[#out + 1] = unit
      n = n + #unit
    end
  end
  return table.concat(out), clamped, true, nul
end

-- ONE JSON string into UTF-16 code units, clamped at cap units (§16.2). The
-- fourth truth is §16.3's zero: a U+0000 unit bound for wide text storage.
local function scan_wstring(inp, cap)
  if peek(inp) ~= '"' then
    inp.bad = true
    return nil, false, false
  end
  inp.p = inp.p + 1
  local out, n, clamped, nul = {}, 0, false, false
  while true do
    local unit, done, ok = scan_unit(inp)
    if not ok then return nil, false, false, false end
    if done then break end
    local code = utf8_decode(unit)
    local units
    if code < 0x10000 then
      units = { code }
    else
      local rest = code - 0x10000
      units = { 0xd800 + (rest >> 10), 0xdc00 + (rest & 0x3ff) }
    end
    if code == 0 then nul = true end
    if clamped or (cap >= 0 and n + #units > cap) then
      clamped = true
    else
      for i = 1, #units do
        out[#out + 1] = units[i]
      end
      n = n + #units
    end
  end
  return out, clamped, true, nul
end

-- ONE RFC 8259 number; the token kept for conversion
local function scan_number(inp)
  space(inp)
  local start = inp.p
  local function digit()
    local b = inp.t:byte(inp.p)
    return b ~= nil and b >= 48 and b <= 57
  end
  if inp.t:byte(inp.p) == 45 then inp.p = inp.p + 1 end
  if inp.p > #inp.t then return nil end
  local c = inp.t:byte(inp.p)
  if c == 48 then
    inp.p = inp.p + 1
  elseif c ~= nil and c >= 49 and c <= 57 then
    while digit() do
      inp.p = inp.p + 1
    end
  else
    return nil
  end
  if inp.t:byte(inp.p) == 46 then
    inp.p = inp.p + 1
    if not digit() then return nil end
    while digit() do
      inp.p = inp.p + 1
    end
  end
  local e = inp.t:byte(inp.p)
  if e == 101 or e == 69 then
    inp.p = inp.p + 1
    local s = inp.t:byte(inp.p)
    if s == 45 or s == 43 then inp.p = inp.p + 1 end
    if not digit() then return nil end
    while digit() do
      inp.p = inp.p + 1
    end
  end
  local n = inp.p - start
  if n <= 0 or n > MAX_NUMBER then return nil end
  return inp.t:sub(start, inp.p - 1)
end

local function scan_label(inp)
  space(inp)
  local c = inp.t:byte(inp.p)
  if c == nil or c < 49 or c > 57 then
    inp.bad = true
    return nil
  end
  local start = inp.p
  while true do
    local b = inp.t:byte(inp.p)
    if b == nil or b < 48 or b > 57 then break end
    inp.p = inp.p + 1
  end
  local digits = inp.t:sub(start, inp.p - 1)
  if #digits > 20 or (#digits == 20 and digits > "18446744073709551615") then
    inp.bad = true
    return nil
  end
  local v = 0
  for i = 1, #digits do
    v = v * 10 + (digits:byte(i) - 48)
  end
  return v
end

local skip_value

-- a skipped OBJECT may still be a pointer's: `&node` opening it takes its
-- label with no node, so a later reference reads null (§16.7)
local function skip_container(inp, close, depth, graph)
  if depth > MAX_DEPTH then
    inp.bad = true
    return false
  end
  inp.p = inp.p + 1
  local first = true
  while true do
    local c = peek(inp)
    if c == close then
      inp.p = inp.p + 1
      return true
    end
    if c == nil then
      inp.bad = true
      return false
    end
    if close == "}" then
      local key, _, ok = scan_string(inp, MAX_KEY)
      if not ok then return false end
      if peek(inp) ~= ":" then
        inp.bad = true
        return false
      end
      inp.p = inp.p + 1
      if key ~= "" and key:sub(1, 1) == "&" and graph ~= nil then
        if not first or key ~= "&node" then
          inp.bad = true
          return false
        end
        local label = scan_label(inp)
        if label == nil then return false end
        if graph[label] == nil then graph[label] = { node = nil, st = nil, open = false } end
        first = false
        c = peek(inp)
        if c == "," then
          inp.p = inp.p + 1
          goto continue
        elseif c == close then
          inp.p = inp.p + 1
          return true
        end
        inp.bad = true
        return false
      end
    end
    first = false
    if not skip_value(inp, depth + 1, graph) then return false end
    c = peek(inp)
    if c == "," then
      inp.p = inp.p + 1
    elseif c == close then
      inp.p = inp.p + 1
      return true
    else
      inp.bad = true
      return false
    end
    ::continue::
  end
end

skip_value = function(inp, depth, graph)
  local c = peek(inp)
  if c == "{" then return skip_container(inp, "}", depth, graph) end
  if c == "[" then return skip_container(inp, "]", depth, graph) end
  if c == '"' then
    local _, _, ok = scan_string(inp, -1)
    return ok
  end
  if c == "t" then return literal(inp, "true") end
  if c == "f" then return literal(inp, "false") end
  if c == "n" then return literal(inp, "null") end
  if c == nil then
    inp.bad = true
    return false
  end
  if scan_number(inp) == nil then
    inp.bad = true
    return false
  end
  return true
end

----------------------------------------------------------------------------
-- storage helpers
----------------------------------------------------------------------------

local function fresh(T, desc)
  if desc == nil then return {} end
  local ctor = T["New" .. desc.name]
  if ctor == nil then return {} end
  return ctor()
end

local function zero_elem(T, e)
  local s = e.shape
  if s == "bool" then return false end
  if s == "int" or s == "flags" then return 0 end
  if s == "wide" then return { lo = 0, hi = 0 } end
  if s == "float" then return 0.0 end
  if s == "string" or s == "bytes" then return "" end
  if s == "wstring" then return {} end
  if s == "enum" then return false end
  if s == "pointer" or s == "string_blob" or s == "bytes_blob" then return false end
  if s == "table" then return fresh(T, T.Tables[e.target]) end
  if s == "union" then return { tag = 0 } end
  if s == "array" or s == "map" then return {} end
  return nil
end

local function shape_of(d)
  local s = d.shape
  -- a timestamp's text is an RFC 3339 string, not a number (docs/SPEC-TABLES.md
  -- §2.11, §16.2); its storage is still the int64 count.
  if d.timestamp then return "s" end
  if s == "bool" then return "b" end
  if s == "string" or s == "wstring" or s == "bytes" then return "s" end
  if s == "string_blob" or s == "bytes_blob" then return "s" end
  if s == "enum" then return "s" end
  if s == "flags" then return "a" end
  if s == "table" or s == "union" or s == "pointer" or s == "map" then return "o" end
  if s == "array" then
    if d.keyed ~= nil then return "o" end
    return "a"
  end
  return "n"
end

-- expect_shape is the JSON shape a value must have to read into descriptor d
-- (docs/SPEC-TABLES.md §16.2): a table field's shape is an object — except the
-- array-root table's (docs/SPEC-TABLES.md §16.10), whose text IS an array.
local function expect_shape(T, d)
  if d.shape == "table" then
    local td = T.Tables[d.target]
    if td ~= nil and td.array_root then return "a" end
  end
  return shape_of(d)
end

-- the JSON shape of the value at the cursor, without consuming it (§16.2)
local function value_shape(inp)
  local c = peek(inp)
  if c == "{" then return "o" end
  if c == "[" then return "a" end
  if c == '"' then return "s" end
  if c == "t" or c == "f" then return "b" end
  if c == "n" then return "z" end
  if c == nil then return nil end
  return "n"
end

local function find_key(desc, key)
  for _, fd in ipairs(desc.fields) do
    if fd.key == key then return fd end
  end
  return nil
end

-- text_list_value returns the VALUE field of a text-list element record
-- (docs/SPEC-TABLES.md §2.13), or nil. A text list's element is a generated
-- element record (<Table><Field>Text: a map_entry with one field `value`), and
-- the list's text IS the array of those values — written and read flat, with
-- no record around each element, as Go does (internal/tabletext writeTextList,
-- readTextList).
local function text_list_value(T, fd)
  if fd == nil or fd.shape ~= "table" then return nil end
  local td = T.Tables[fd.target]
  if td == nil or not td.map_entry then return nil end
  for _, f in ipairs(td.fields) do
    if f.name == "value" then return f end
  end
  return nil
end

----------------------------------------------------------------------------
-- integers (§16.2): JSON has one number type, so any token whose VALUE is
-- integral is that integer; a fractional value is the wrong shape, and a
-- magnitude past 64 bits saturates and counts clamped.
----------------------------------------------------------------------------

local MAX64 = 0xffffffffffffffff
local MAX64_TEXT = "18446744073709551615"

local function mag_of_digits(digits)
  local d = digits:gsub("^0+", "")
  if d == "" then return 0, false end
  if #d > 20 or (#d == 20 and d > MAX64_TEXT) then return MAX64, true end
  local v = 0
  for i = 1, #d do
    v = v * 10 + (d:byte(i) - 48)
  end
  return v, false
end

local function interpret_integer(token, integral)
  local out = { neg = false, mag = 0, frac = false, sat = false, finite = true }
  if integral then
    local t = token
    if t:sub(1, 1) == "-" then
      out.neg = true
      t = t:sub(2)
    end
    out.mag, out.sat = mag_of_digits(t)
    if out.mag == 0 then out.neg = false end
    return out
  end
  local x = tonumber(token)
  if x == nil or x ~= x or x == math.huge or x == -math.huge then
    out.finite = false
    return out
  end
  out.neg = x < 0 or (x == 0 and 1 / x < 0)
  local a = math.abs(x)
  if a >= 18446744073709551616.0 then
    out.mag, out.sat = MAX64, true
    return out
  end
  if math.floor(a) ~= a then
    out.frac = true
    return out
  end
  local text = string.format("%.0f", a)
  out.mag = mag_of_digits(text)
  if out.mag == 0 then out.neg = false end
  return out
end

local function int_less(n, bound)
  if n.neg then
    if bound >= 0 then return true end
    local bm = 0 - bound
    return math.ult(n.mag, bm)
  end
  if bound < 0 then return false end
  return math.ult(n.mag, bound)
end

local function int_greater(n, bound)
  if bound < 0 then
    if not n.neg then return true end
    return math.ult(0 - bound, n.mag)
  end
  if n.neg then return false end
  return math.ult(bound, n.mag)
end

local function set_from_bound(n, bound)
  if bound < 0 then
    n.neg, n.mag = true, 0 - bound
  else
    n.neg, n.mag = false, bound
  end
end

local function place_integer(fd, n, report)
  if not n.finite or n.frac then
    report.kindMismatch = report.kindMismatch + 1
    return nil
  end
  if n.sat then report.clamped = report.clamped + 1 end
  if fd.lo ~= nil and fd.hi ~= nil then
    if int_less(n, fd.lo) then
      set_from_bound(n, fd.lo)
      report.clamped = report.clamped + 1
    elseif int_greater(n, fd.hi) then
      set_from_bound(n, fd.hi)
      report.clamped = report.clamped + 1
    end
  end
  if fd.bits ~= nil and fd.bits > 0 and fd.bits < 64 then
    local bmax = (1 << fd.bits) - 1
    if int_greater(n, bmax) then
      set_from_bound(n, bmax)
      report.clamped = report.clamped + 1
    end
  end
  local width = fd.width or 8
  local moved = false
  if fd.signed then
    local high = 0x7fffffffffffffff
    if width < 8 then high = (1 << (width * 8 - 1)) - 1 end
    if n.neg then
      local low = high + 1
      if math.ult(low, n.mag) then
        n.mag = low
        moved = true
      end
      local v = 0 - n.mag
      if moved then report.clamped = report.clamped + 1 end
      return v
    end
    if math.ult(high, n.mag) then
      n.mag = high
      moved = true
    end
    if moved then report.clamped = report.clamped + 1 end
    return n.mag
  end
  if n.neg then
    report.clamped = report.clamped + 1
    return 0
  end
  local high = MAX64
  if width < 8 then high = (1 << (width * 8)) - 1 end
  if math.ult(high, n.mag) then
    n.mag = high
    moved = true
  end
  if moved then report.clamped = report.clamped + 1 end
  return n.mag
end

-- one map key: the token is the whole string and must be a JSON number
local function map_int_key(fd, key)
  local probe = new_in(key)
  local token = scan_number(probe)
  if token == nil or probe.p ~= #key + 1 then return false, "malformed" end
  local n = interpret_integer(token, true)
  if n.frac or n.sat then return false, "mismatch" end
  local width = fd.width or 8
  if fd.signed then
    local high = 0x7fffffffffffffff
    if width < 8 then high = (1 << (width * 8 - 1)) - 1 end
    if n.neg then
      if math.ult(high + 1, n.mag) then return false, "mismatch" end
      return 0 - n.mag, nil
    end
    if math.ult(high, n.mag) then return false, "mismatch" end
    return n.mag, nil
  end
  if n.neg then return false, "mismatch" end
  local high = MAX64
  if width < 8 then high = (1 << (width * 8)) - 1 end
  if math.ult(high, n.mag) then return false, "mismatch" end
  return n.mag, nil
end

----------------------------------------------------------------------------
-- the WIDE kinds (§16.2): int128/uint128 and fixed/ufixed have exact text
-- spellings — a 128-bit integer is a decimal integer and a fixed point value
-- is whole units with at least one fractional digit. Nothing here passes
-- through a double: the lanes are 64-bit and the decimal work is integer
-- digits (docs/PORT-LUA.md, 64-BIT AND 128-BIT INTEGERS, FIXED-POINT).
----------------------------------------------------------------------------

local WIDE_BAND = 40
local WIDE_SIGNED_MIN = "170141183460469231731687303715884105728"
local WIDE_SIGNED_MAX = "170141183460469231731687303715884105727"
local WIDE_UNSIGNED_MAX = "340282366920938463463374607431768211455"

local function dec_norm(s)
  local t = s:gsub("^0+", "")
  if t == "" then return "0" end
  return t
end

local function dec_less(a, b)
  if #a ~= #b then return #a < #b end
  return a < b
end

local function dec_mul(s, m)
  local out, carry = {}, 0
  for i = #s, 1, -1 do
    local p = (s:byte(i) - 48) * m + carry
    out[#out + 1] = string.char(48 + p % 10)
    carry = p // 10
  end
  while carry > 0 do
    out[#out + 1] = string.char(48 + carry % 10)
    carry = carry // 10
  end
  local r = {}
  for i = #out, 1, -1 do
    r[#r + 1] = out[i]
  end
  return table.concat(r)
end

local function dec_div(s, d)
  local q, rem = {}, 0
  for i = 1, #s do
    local cur = rem * 10 + (s:byte(i) - 48)
    q[#q + 1] = string.char(48 + cur // d)
    rem = cur % d
  end
  return dec_norm(table.concat(q)), rem
end

-- ONE JSON number token as its sign, its digits with the leading and trailing
-- zeros stripped, and the decimal point's position among those digits — the
-- normalization ParseWide makes before any arithmetic (§16.2).
local function wide_token_parts(token)
  local i = 1
  local neg = false
  local c = token:sub(1, 1)
  if c == "-" or c == "+" then
    neg = c == "-"
    i = 2
  end
  local intStart = i
  while i <= #token do
    local b = token:byte(i)
    if b < 48 or b > 57 then break end
    i = i + 1
  end
  local intDigits = token:sub(intStart, i - 1)
  local fracDigits = ""
  if token:sub(i, i) == "." then
    i = i + 1
    local start = i
    while i <= #token do
      local b = token:byte(i)
      if b < 48 or b > 57 then break end
      i = i + 1
    end
    fracDigits = token:sub(start, i - 1)
  end
  local exp = 0
  local e = token:sub(i, i)
  if e == "e" or e == "E" then
    i = i + 1
    local eneg = false
    local s = token:sub(i, i)
    if s == "-" or s == "+" then
      eneg = s == "-"
      i = i + 1
    end
    while i <= #token do
      local b = token:byte(i)
      if b < 48 or b > 57 then break end
      if exp < 100000 then exp = exp * 10 + (b - 48) end
      i = i + 1
    end
    if eneg then exp = -exp end
  end
  local digits = intDigits .. fracDigits
  local point = #intDigits + exp
  local lead = 0
  while lead < #digits and digits:byte(lead + 1) == 48 do
    lead = lead + 1
  end
  digits = digits:sub(lead + 1)
  point = point - lead
  digits = digits:gsub("0+$", "")
  return neg, digits, point
end

local function wide_mul_add(lo, hi, m, a)
  local l0, l1 = lo & 0xffffffff, (lo >> 32) & 0xffffffff
  local l2, l3 = hi & 0xffffffff, (hi >> 32) & 0xffffffff
  local p = l0 * m + a
  l0 = p & 0xffffffff
  local c = p >> 32
  p = l1 * m + c
  l1 = p & 0xffffffff
  c = p >> 32
  p = l2 * m + c
  l2 = p & 0xffffffff
  c = p >> 32
  p = l3 * m + c
  l3 = p & 0xffffffff
  return l0 | (l1 << 32), l2 | (l3 << 32)
end

local function wide_mul_small(lo, hi, m)
  return wide_mul_add(lo, hi, m, 0)
end

local function wide_neg(lo, hi)
  lo = ~lo + 1
  hi = ~hi
  if lo == 0 then hi = hi + 1 end
  return lo, hi
end

local function ushr64(x, n)
  if n == 0 then return x end
  if n >= 64 then return 0 end
  return (x >> n) & ((1 << (64 - n)) - 1)
end

local function wide_shr(lo, hi, n)
  if n == 0 then return lo, hi end
  if n >= 128 then return 0, 0 end
  if n >= 64 then return ushr64(hi, n - 64), 0 end
  return ushr64(lo, n) | (hi << (64 - n)), ushr64(hi, n)
end

local function wide_low(lo, hi, n)
  if n <= 0 then return 0, 0 end
  if n >= 64 then
    if n == 64 then return lo, 0 end
    return lo, hi & ((1 << (n - 64)) - 1)
  end
  return lo & ((1 << n) - 1), 0
end

local function dec_to_lanes(s)
  local lo, hi = 0, 0
  for i = 1, #s do
    lo, hi = wide_mul_add(lo, hi, 10, s:byte(i) - 48)
  end
  return lo, hi
end

local function wide_div10(lo, hi)
  local l = { lo & 0xffffffff, (lo >> 32) & 0xffffffff, hi & 0xffffffff, (hi >> 32) & 0xffffffff }
  local rem = 0
  for i = 4, 1, -1 do
    local cur = rem * 4294967296 + l[i]
    l[i] = cur // 10
    rem = cur % 10
  end
  return l[1] | (l[2] << 32), l[3] | (l[4] << 32), rem
end

local function wide_decimal(lo, hi)
  if lo == 0 and hi == 0 then return "0" end
  local digits = {}
  repeat
    local rem
    lo, hi, rem = wide_div10(lo, hi)
    digits[#digits + 1] = string.char(48 + rem)
  until lo == 0 and hi == 0
  local out = {}
  for i = #digits, 1, -1 do
    out[#out + 1] = digits[i]
  end
  return table.concat(out)
end

-- the declared F of a fixed/ufixed kind (§16.2); a 128-bit integer has none
local function wide_frac(d)
  if d.kind == nil or d.kind < 20 then return 0 end
  local f = d.type_name and d.type_name:match("^u?fixed%(%d+%s*,%s*(%d+)%)")
  return tonumber(f) or 0
end

local function wide_less_signed(al, ah, bl, bh)
  local an, bn = ah < 0, bh < 0
  if an ~= bn then return an end
  if ah ~= bh then return ah < bh end
  return math.ult(al, bl)
end

local function wide_less_unsigned(al, ah, bl, bh)
  if ah ~= bh then return math.ult(ah, bh) end
  return math.ult(al, bl)
end

-- the raw value of one token, scaled by 2^F (§16.2: a fraction that is not
-- exactly representable is a kind mismatch, never rounded; a magnitude past
-- 128 bits saturates to the kind's domain and counts as a clamp)
local function wide_parse(token, kind, frac)
  local neg, digits, point = wide_token_parts(token)
  local signed = kind == 18 or (kind >= 20 and kind <= 24)
  if digits == "" then return { lo = 0, hi = 0 }, true, false end
  local saturated = false
  local mag
  if point > WIDE_BAND then
    saturated = true
  elseif point < -WIDE_BAND then
    return nil, false, false
  else
    local scale = point - #digits
    mag = digits
    if scale >= 0 then
      if scale > 0 then mag = mag .. string.rep("0", scale) end
      for _ = 1, frac do
        mag = dec_mul(mag, 2)
      end
    else
      local k = -scale
      for _ = 1, k do
        local rem
        mag, rem = dec_div(mag, 5)
        if rem ~= 0 then return nil, false, false end
      end
      local e = frac - k
      if e >= 0 then
        for _ = 1, e do
          mag = dec_mul(mag, 2)
        end
      else
        for _ = 1, -e do
          local rem
          mag, rem = dec_div(mag, 2)
          if rem ~= 0 then return nil, false, false end
        end
      end
    end
    mag = dec_norm(mag)
    if mag == "0" then return { lo = 0, hi = 0 }, true, false end
    local limit
    if signed then
      limit = neg and WIDE_SIGNED_MIN or WIDE_SIGNED_MAX
    else
      limit = WIDE_UNSIGNED_MAX
    end
    if dec_less(limit, mag) then saturated = true end
  end
  if saturated then
    if signed then
      if neg then return { lo = 0, hi = 0x8000000000000000 }, true, true end
      return { lo = 0xffffffffffffffff, hi = 0x7fffffffffffffff }, true, true
    end
    if neg then return { lo = 0, hi = 0 }, true, true end
    return { lo = 0xffffffffffffffff, hi = 0xffffffffffffffff }, true, true
  end
  if not signed and neg then return { lo = 0, hi = 0 }, true, true end
  local lo, hi = dec_to_lanes(mag)
  if neg then
    lo, hi = wide_neg(lo, hi)
  end
  return { lo = lo, hi = hi }, true, false
end

-- a declared bound in lanes: a 128-bit descriptor carries { lo, hi }, a
-- narrower fixed kind its raw Lua integer (§16.2)
local function bound_lanes(d, b)
  if type(b) == "table" then return b.lo or 0, b.hi or 0 end
  if d.signed then return b, (b < 0 and -1 or 0) end
  return b, 0
end

local function wide_clamp(value, d)
  if d.lo == nil or d.hi == nil then return value, false end
  local less = d.signed and wide_less_signed or wide_less_unsigned
  local llo, lhi = bound_lanes(d, d.lo)
  local hlo, hhi = bound_lanes(d, d.hi)
  if less(value.lo, value.hi, llo, lhi) then return { lo = llo, hi = lhi }, true end
  if less(hlo, hhi, value.lo, value.hi) then return { lo = hlo, hi = hhi }, true end
  return value, false
end

-- §16.2's write spelling: a decimal integer for a 128-bit kind, the shortest
-- exact decimal in whole units for a fixed point value, at least one digit
-- after the point
local function wide_text(d, val)
  local kind = d.kind or 0
  local signed = kind == 18 or (kind >= 20 and kind <= 24)
  local lo, hi
  if type(val) == "table" then
    lo, hi = val.lo or 0, val.hi or 0
  elseif val == nil then
    lo, hi = 0, 0
  elseif signed then
    lo, hi = val, (val < 0 and -1 or 0)
  else
    lo, hi = val, 0
  end
  local neg = false
  if signed and hi < 0 then
    neg = true
    lo, hi = wide_neg(lo, hi)
  end
  local sign = neg and "-" or ""
  if kind == 18 or kind == 19 then return sign .. wide_decimal(lo, hi) end
  local frac = wide_frac(d)
  local wlo, whi = wide_shr(lo, hi, frac)
  local out = sign .. wide_decimal(wlo, whi) .. "."
  local flo, fhi = wide_low(lo, hi, frac)
  if flo == 0 and fhi == 0 then return out .. "0" end
  while flo ~= 0 or fhi ~= 0 do
    local mlo, mhi = wide_mul_small(flo, fhi, 10)
    local dlo = wide_shr(mlo, mhi, frac)
    out = out .. string.char(48 + dlo)
    flo, fhi = wide_low(mlo, mhi, frac)
  end
  return out
end

-- one wide token into its storage (docs/SPEC-TABLES.md §16.2): a 128-bit
-- shape holds { lo, hi }, a narrower fixed kind holds its raw Lua integer
local function read_wide(inp, d, holder, key, report)
  local token = scan_number(inp)
  if token == nil then
    inp.bad = true
    return false
  end
  local value, exact, saturated = wide_parse(token, d.kind or 0, wide_frac(d))
  if not exact then
    report.kindMismatch = report.kindMismatch + 1
    return true
  end
  if saturated then report.clamped = report.clamped + 1 end
  local clamped
  value, clamped = wide_clamp(value, d)
  if clamped then report.clamped = report.clamped + 1 end
  if d.shape == "wide" then
    holder[key] = value
  else
    holder[key] = value.lo
  end
  return true
end

----------------------------------------------------------------------------
-- the read walk
----------------------------------------------------------------------------

local read_value
local parse_timestamp

-- A REQUIRED FIELD MUST BE NAMED IN THE OBJECT (docs/SPEC-TABLES.md §2.3,
-- §16.2): closing one object counts missing_required once, naming in
-- declaration order every field marked required that the object did not name,
-- exactly as the binary read's close does (load.lua, read_body). A required
-- field inside an `if` guard is demanded only while its guard holds (§2.3,
-- §3): with the branch off it is absent and no refusal is owed.
local function require_seen(desc, v, seen, report)
  local missing = nil
  for _, f in ipairs(desc.fields) do
    if f.required and not seen[f.name] then
      local hold = true
      if f.guard ~= nil then
        for _, t in ipairs(f.guard) do
          if (v[t.field] == true) ~= t.want then
            hold = false
            break
          end
        end
      end
      if hold then
        if missing == nil then missing = "" end
        if #missing > 0 then missing = missing .. ", " end
        missing = missing .. f.name
      end
    end
  end
  if missing ~= nil then
    report.missingRequired = (report.missingRequired or 0) + 1
    report.reason = "missing_required: " .. missing
    return false
  end
  return true
end

local function read_keys(T, desc, inp, v, depth, first_key, graph, report)
  local seen = {}
  while true do
    local key
    if first_key ~= nil then
      key, first_key = first_key, nil
    else
      local c = peek(inp)
      if c == "}" then
        inp.p = inp.p + 1
        return require_seen(desc, v, seen, report)
      end
      if c == nil then
        inp.bad = true
        return false
      end
      local start = inp.p
      local _, _, ok = scan_string(inp, MAX_KEY)
      if not ok then return false end
      key = inp.t:sub(start + 1, inp.p - 2)
      if peek(inp) ~= ":" then
        inp.bad = true
        return false
      end
      inp.p = inp.p + 1
    end
    if key ~= "" and key:sub(1, 1) == "&" then
      inp.bad = true
      return false
    end
    local fd = find_key(desc, key)
    if fd == nil then
      report.unknown = report.unknown + 1
      if not skip_value(inp, depth + 1, graph) then return false end
    else
      if seen[fd.name] then report.duplicate = report.duplicate + 1 end
      seen[fd.name] = true
      local shape = value_shape(inp)
      local takes_null = fd.optional or fd.shape == "pointer" or fd.shape == "string_blob" or fd.shape == "bytes_blob"
      if shape == "z" and takes_null then
        literal(inp, "null")
        if fd.optional then
          local def = fresh(T, desc)
          v[fd.name] = def[fd.name]
          v[fd.name .. "_present"] = false
        else
          v[fd.name] = nil
        end
      else
        local want = expect_shape(T, fd)
        if shape ~= want then
          report.kindMismatch = report.kindMismatch + 1
          if not skip_value(inp, depth + 1, graph) then return false end
        else
          if not read_value(T, fd, inp, v, fd.name, depth, graph, report, false, desc.name, fd.name) then
            return false
          end
        end
        if fd.optional then v[fd.name .. "_present"] = true end
      end
    end
    local c = peek(inp)
    if c == "," then
      inp.p = inp.p + 1
    elseif c == "}" then
      inp.p = inp.p + 1
      return require_seen(desc, v, seen, report)
    else
      inp.bad = true
      return false
    end
  end
end

local function read_table(T, desc, inp, v, depth, graph, report)
  if depth > MAX_DEPTH then
    inp.bad = true
    return false
  end
  if peek(inp) ~= "{" then
    inp.bad = true
    return false
  end
  inp.p = inp.p + 1
  return read_keys(T, desc, inp, v, depth, nil, graph, report)
end

local function read_pointer(T, d, inp, holder, key, depth, graph, report)
  if depth + 1 > MAX_DEPTH then
    inp.bad = true
    return false
  end
  if peek(inp) ~= "{" then
    inp.bad = true
    return false
  end
  inp.p = inp.p + 1
  local c = peek(inp)
  if c == "}" then
    inp.p = inp.p + 1
    holder[key] = fresh(T, T.Tables[d.target])
    return require_seen(T.Tables[d.target], holder[key], {}, report)
  end
  if c == nil then
    inp.bad = true
    return false
  end
  local start = inp.p
  local _, _, ok = scan_string(inp, MAX_KEY)
  if not ok then return false end
  local k = inp.t:sub(start + 1, inp.p - 2)
  if peek(inp) ~= ":" then
    inp.bad = true
    return false
  end
  inp.p = inp.p + 1
  if k ~= "&node" then
    local node = fresh(T, T.Tables[d.target])
    holder[key] = node
    return read_keys(T, T.Tables[d.target], inp, node, depth + 1, k, graph, report)
  end
  local label = scan_label(inp)
  if label == nil then return false end
  local entry = graph[label]
  c = peek(inp)
  if c == "," then
    inp.p = inp.p + 1
    c = peek(inp)
  end
  local bare = c == "}"
  if bare ~= (entry ~= nil) then
    inp.bad = true
    return false
  end
  if bare then
    inp.p = inp.p + 1
    if entry.open then
      inp.bad = true
      return false
    end
    holder[key] = nil
    if entry.st == nil then return true end
    if entry.st ~= d.target then
      report.kindMismatch = report.kindMismatch + 1
      return true
    end
    holder[key] = entry.node
    return true
  end
  local node = fresh(T, T.Tables[d.target])
  holder[key] = node
  graph[label] = { node = nil, st = nil, open = true }
  if not read_keys(T, T.Tables[d.target], inp, node, depth + 1, nil, graph, report) then return false end
  graph[label] = { node = node, st = d.target, open = false }
  return true
end

local function read_base64(inp, bound, report)
  if peek(inp) ~= '"' then
    inp.bad = true
    return nil
  end
  inp.p = inp.p + 1
  local out = {}
  local acc, held = 0, 0
  local clamped, bad = false, false
  while true do
    if inp.p > #inp.t then
      inp.bad = true
      return nil
    end
    local ch = inp.t:sub(inp.p, inp.p)
    inp.p = inp.p + 1
    if ch == '"' then break end
    if ch ~= "=" and not bad then
      local at = BASE64:find(ch, 1, true)
      if at == nil then
        bad = true
      else
        acc = ((acc << 6) | (at - 1)) & 0xffffff
        held = held + 6
        if held >= 8 then
          held = held - 8
          if bound < 0 or #out < bound then
            out[#out + 1] = string.char((acc >> held) & 0xff)
          else
            clamped = true
          end
        end
      end
    end
  end
  if bad then
    report.kindMismatch = report.kindMismatch + 1
    return false
  end
  if clamped then report.clamped = report.clamped + 1 end
  return table.concat(out)
end

-- A TEXT LIST (docs/SPEC-TABLES.md §2.13, §16.2) is stored as an array of its
-- generated element record, whose text is the PLAIN array of strings: the
-- record's one string field is each element, and an absent ?string element is
-- `null`. The element record is the only generated entry reached through an
-- array — it is tagged map_entry like a map's entry and holds exactly one
-- field (`value`), while a map entry holds two (`key`, `value`).
local function text_list_elem(T, fd)
  if fd.shape ~= "array" or fd.elem == nil or fd.elem.shape ~= "table" then return nil, nil end
  local d = T.Tables[fd.elem.target]
  if d == nil or not d.map_entry or #d.fields ~= 1 then return nil, nil end
  local vf = d.fields[1]
  if vf == nil or vf.shape ~= "string" then return nil, nil end
  return d, vf
end

-- A text list's TOTAL clamp names its field once, the first time (§2.13,
-- §6.5, §16.2), in the report's clamped-field slot, never in Reason, as the
-- generated Go records it in TableReport.ClampedField; the strict read refuses
-- it as "clamped: <Table>.<field>". The element and count clamps are §2.13's
-- ordinary counters and name no field here, as in the Go JSON read.
local function text_list_clamped(report, table_name, field_name)
  report.clamped = report.clamped + 1
  if not report.ClampedField or report.ClampedField == "" then report.ClampedField = table_name .. "." .. field_name end
end

-- ONE text list's JSON array (§16.2): strings in INDEX order, `null` for an
-- absent ?string element. A plain null, any other JSON type, an element past a
-- [..N] count and an element past `| total = N` are the rows' own verdicts.
local function read_text_list(T, fd, _d, vf, inp, holder, key, depth, graph, report, table_name, field_name)
  local out = {}
  local bound = fd.list and 0x7fffffff or (fd.bound or 0)
  local cap = vf.bound or -1
  local total = fd.total or 0
  local placed, spent, full = 0, 0, false
  while true do
    local c = peek(inp)
    if c == "]" then
      inp.p = inp.p + 1
      break
    end
    if c == nil then
      inp.bad = true
      return false
    end
    if full then
      if not skip_value(inp, depth + 1, graph) then return false end
    elseif placed >= bound then
      report.clamped = report.clamped + 1
      if not skip_value(inp, depth + 1, graph) then return false end
    else
      local vs = value_shape(inp)
      if vs == "z" and vf.optional then
        literal(inp, "null")
        placed = placed + 1
        out[placed] = zero_elem(T, fd.elem)
      elseif vs ~= "s" then
        report.kindMismatch = report.kindMismatch + 1
        if not skip_value(inp, depth + 1, graph) then return false end
        placed = placed + 1
        out[placed] = zero_elem(T, fd.elem)
      else
        local txt, clamped, ok, nul = scan_string(inp, cap)
        if not ok then return false end
        -- §2.13.1: an unbounded element or key whose text decodes to U+0000
        -- refuses the read too; scan_string flags a zero at every cap here.
        if nul then
          report.Reason = "text_holds_zero: " .. table_name .. "." .. field_name
          inp.bad = true
          return false
        end
        if clamped then report.clamped = report.clamped + 1 end
        local rec = zero_elem(T, fd.elem)
        rec[vf.name] = txt
        if vf.optional then rec[vf.present] = true end
        if total > 0 then
          spent = spent + #txt
          if spent > total then
            text_list_clamped(report, table_name, field_name)
            full = true
          else
            placed = placed + 1
            out[placed] = rec
          end
        else
          placed = placed + 1
          out[placed] = rec
        end
      end
    end
    c = peek(inp)
    if c == "," then
      inp.p = inp.p + 1
    elseif c == "]" then
      inp.p = inp.p + 1
      break
    else
      inp.bad = true
      return false
    end
  end
  holder[key] = out
  return true
end

local function read_array(T, fd, inp, holder, key, depth, graph, report, table_name, field_name)
  if peek(inp) ~= "[" then
    inp.bad = true
    return false
  end
  inp.p = inp.p + 1
  local out = {}
  if fd.fixed then
    for i = 1, fd.bound do
      out[i] = zero_elem(T, fd.elem)
    end
  end
  local td, tvf = text_list_elem(T, fd)
  if td ~= nil then
    return read_text_list(T, fd, td, tvf, inp, holder, key, depth, graph, report, table_name, field_name)
  end
  local bound = fd.list and 0x7fffffff or fd.bound
  local placed = 0
  local vf = text_list_value(T, fd.elem)
  local eshape = vf and shape_of(vf) or expect_shape(T, fd.elem)
  while true do
    local c = peek(inp)
    local vs = value_shape(inp)
    if c == "]" then
      inp.p = inp.p + 1
      break
    end
    if c == nil then
      inp.bad = true
      return false
    end
    if placed >= bound then
      report.clamped = report.clamped + 1
      if not skip_value(inp, depth + 1, graph) then return false end
    elseif fd.elem.shape == "pointer" and vs == "z" then
      literal(inp, "null")
      placed = placed + 1
      out[placed] = false
    elseif vf and vf.optional and vs == "z" then
      -- an absent ?string(M) element is null (§2.13)
      literal(inp, "null")
      placed = placed + 1
      out[placed] = zero_elem(T, fd.elem)
    elseif vs ~= eshape then
      report.kindMismatch = report.kindMismatch + 1
      if not skip_value(inp, depth + 1, graph) then return false end
      placed = placed + 1
      out[placed] = zero_elem(T, fd.elem)
    else
      placed = placed + 1
      if out[placed] == nil then out[placed] = zero_elem(T, fd.elem) end
      if vf then
        -- a text list element is the record's value, read alone into it
        if not read_value(T, vf, inp, out[placed], "value", depth + 1, graph, report, true, table_name, field_name) then
          return false
        end
      elseif not read_value(T, fd.elem, inp, out, placed, depth + 1, graph, report, true, table_name, field_name) then
        return false
      end
    end
    c = peek(inp)
    if c == "," then
      inp.p = inp.p + 1
    elseif c == "]" then
      inp.p = inp.p + 1
      break
    else
      inp.bad = true
      return false
    end
  end
  holder[key] = out
  return true
end

local function read_map(T, fd, inp, holder, key, depth, graph, report, table_name, field_name)
  if peek(inp) ~= "{" then
    inp.bad = true
    return false
  end
  inp.p = inp.p + 1
  local out, seen = {}, {}
  local kf = fd.key_field
  local string_key = kf.shape == "string"
  while true do
    local c = peek(inp)
    if c == "}" then
      inp.p = inp.p + 1
      break
    end
    if c == nil then
      inp.bad = true
      return false
    end
    -- §2.13.1: an UNBOUNDED map key has no length bound, so it is scanned
    -- with none; a bounded key keeps the walker's own scratch cap and its
    -- declared bound below.
    local keycap = MAX_KEY
    if string_key and kf.bound == nil then keycap = -1 end
    local kraw, clamped, ok, nul = scan_string(inp, keycap)
    if not ok then return false end
    if peek(inp) ~= ":" then
      inp.bad = true
      return false
    end
    inp.p = inp.p + 1
    if nul then
      -- §16.3: a map key is text storage, so a zero in it is the event even
      -- when the key would be dropped for its length; one malformed, never
      -- also a clamp or a kind mismatch.
      report.Reason = "text_holds_zero: " .. table_name .. "." .. field_name
      inp.bad = true
      return false
    end
    local kval, skip = nil, false
    if clamped then
      if string_key then
        report.clamped = report.clamped + 1
      else
        report.kindMismatch = report.kindMismatch + 1
      end
      skip = true
    elseif string_key then
      if kf.bound ~= nil and kf.bound > 0 and #kraw > kf.bound then
        report.clamped = report.clamped + 1
        skip = true
      else
        kval = kraw
      end
    else
      local v, why = map_int_key(kf, kraw)
      if why == "malformed" then
        report.malformed = true
        inp.bad = true
        return false
      elseif why == "mismatch" then
        report.kindMismatch = report.kindMismatch + 1
        skip = true
      else
        kval = v
      end
    end
    -- §16.2 map row and the `*T`/`*bytes`/`*string` rows: a pointer or blob
    -- VALUE may be `null`, a present entry holding a null reference (false,
    -- the null element, since a Lua table cannot hold nil), as the Go
    -- reference map reader keeps it.
    local vf = fd.value_field
    local null_ref = false
    if
      not skip
      and value_shape(inp) == "z"
      and (vf.shape == "pointer" or vf.shape == "string_blob" or vf.shape == "bytes_blob")
    then
      null_ref = true
    elseif not skip and value_shape(inp) ~= expect_shape(T, vf) then
      report.kindMismatch = report.kindMismatch + 1
      skip = true
    end
    if null_ref then
      literal(inp, "null")
      if seen[kval] then report.duplicate = report.duplicate + 1 end
      seen[kval] = true
      out[kval] = false
    elseif skip then
      if not skip_value(inp, depth + 1, graph) then return false end
    else
      if seen[kval] then report.duplicate = report.duplicate + 1 end
      seen[kval] = true
      local tmp = {}
      if not read_value(T, fd.value_field, inp, tmp, "v", depth + 1, graph, report, true, table_name, field_name) then
        return false
      end
      out[kval] = tmp.v
    end
    c = peek(inp)
    if c == "," then
      inp.p = inp.p + 1
    elseif c == "}" then
      inp.p = inp.p + 1
      break
    else
      inp.bad = true
      return false
    end
  end
  holder[key] = out
  return true
end

local function read_keyed(T, fd, inp, holder, key, depth, graph, report, table_name, field_name)
  if peek(inp) ~= "{" then
    inp.bad = true
    return false
  end
  inp.p = inp.p + 1
  local out = {}
  for i = 1, fd.bound do
    out[i] = zero_elem(T, fd.elem)
  end
  local seen = {}
  local eshape = expect_shape(T, fd.elem)
  while true do
    local c = peek(inp)
    if c == "}" then
      inp.p = inp.p + 1
      break
    end
    if c == nil then
      inp.bad = true
      return false
    end
    local start = inp.p
    local _, _, ok = scan_string(inp, MAX_KEY)
    if not ok then return false end
    local name = inp.t:sub(start + 1, inp.p - 2)
    if peek(inp) ~= ":" then
      inp.bad = true
      return false
    end
    inp.p = inp.p + 1
    local slot = nil
    for i, v in ipairs(fd.keyed) do
      if v[1] == name then
        slot = i
        break
      end
    end
    if slot == nil then
      report.unknown = report.unknown + 1
      if not skip_value(inp, depth + 1, graph) then return false end
    else
      if seen[slot] then report.duplicate = report.duplicate + 1 end
      seen[slot] = true
      if value_shape(inp) ~= eshape then
        report.kindMismatch = report.kindMismatch + 1
        if not skip_value(inp, depth + 1, graph) then return false end
      else
        if not read_value(T, fd.elem, inp, out, slot, depth + 1, graph, report, true, table_name, field_name) then
          return false
        end
      end
    end
    c = peek(inp)
    if c == "," then
      inp.p = inp.p + 1
    elseif c == "}" then
      inp.p = inp.p + 1
      break
    else
      inp.bad = true
      return false
    end
  end
  holder[key] = out
  return true
end

local function read_union(T, u, inp, depth, graph, report, table_name, field_name)
  if peek(inp) ~= "{" then
    inp.bad = true
    return nil
  end
  inp.p = inp.p + 1
  if peek(inp) == "}" then
    inp.p = inp.p + 1
    return { tag = 0 }
  end
  local start = inp.p
  local _, _, ok = scan_string(inp, MAX_KEY)
  if not ok then return nil end
  local name = inp.t:sub(start + 1, inp.p - 2)
  if peek(inp) ~= ":" then
    inp.bad = true
    return nil
  end
  inp.p = inp.p + 1
  local tag, arm = 0, nil
  for i, a in ipairs(u.arms) do
    if a.name == name then
      tag, arm = i, a
      break
    end
  end
  local out = { tag = 0 }
  if tag == 0 then
    report.unknown = report.unknown + 1
    if not skip_value(inp, depth + 1, graph) then return nil end
  elseif arm.field == nil then
    if value_shape(inp) ~= "z" then
      report.kindMismatch = report.kindMismatch + 1
      if not skip_value(inp, depth + 1, graph) then return nil end
    else
      literal(inp, "null")
      out.tag = tag
    end
  elseif
    (arm.field.shape == "pointer" or arm.field.shape == "string_blob" or arm.field.shape == "bytes_blob")
    and value_shape(inp) == "z"
  then
    -- §16.2 union row: the arm takes its own row, and a pointer or blob arm
    -- reads `null` as the arm selected with a null reference
    literal(inp, "null")
    out.tag = tag
  elseif value_shape(inp) ~= expect_shape(T, arm.field) then
    report.kindMismatch = report.kindMismatch + 1
    if not skip_value(inp, depth + 1, graph) then return nil end
  else
    out.tag = tag
    if not read_value(T, arm.field, inp, out, arm.slot, depth + 1, graph, report, false, table_name, field_name) then
      return nil
    end
  end
  local c = peek(inp)
  if c == "," then
    inp.p = inp.p + 1
    c = peek(inp)
  end
  if c == "}" then
    inp.p = inp.p + 1
    return out
  end
  inp.bad = true
  return nil
end

read_value = function(T, d, inp, holder, key, depth, graph, report, elem, table_name, field_name)
  local s = d.shape
  if s == "pointer" then
    return read_pointer(T, d, inp, holder, key, depth, graph, report)
  elseif s == "string" then
    local out, clamped, ok, nul = scan_string(inp, d.bound)
    if not ok then return false end
    if nul then
      -- §16.3: a JSON text that lands U+0000 in bounded text storage is
      -- refused as text_holds_zero, naming the innermost field.
      report.Reason = "text_holds_zero: " .. table_name .. "." .. field_name
      inp.bad = true
      return false
    end
    if clamped then report.clamped = report.clamped + 1 end
    holder[key] = out
    return true
  elseif s == "wstring" then
    local out, clamped, ok, nul = scan_wstring(inp, d.bound)
    if not ok then return false end
    if nul then
      -- §16.3: a U+0000 unit bound for wide text storage refuses the read
      -- with the same name a narrow field takes.
      report.Reason = "text_holds_zero: " .. table_name .. "." .. field_name
      inp.bad = true
      return false
    end
    if clamped then report.clamped = report.clamped + 1 end
    holder[key] = out
    return true
  elseif s == "bytes" then
    local out = read_base64(inp, d.bound, report)
    if out == nil then return false end
    holder[key] = out == false and "" or out
    return true
  elseif s == "string_blob" then
    -- A BLOB IS A NODE (docs/SPEC-TABLES.md §16.2 `*string`, §3.1): the read
    -- boxes it as { value = <bytes> }, as load.lua does, so it keeps an
    -- identity the save numbers by (docs/PORT-LUA.md, storage table).
    local out, _, ok, nul = scan_string(inp, -1)
    if not ok then return false end
    if nul then
      -- §16.3: `*string` is text storage too, and the innermost field is the
      -- blob's own.
      report.Reason = "text_holds_zero: " .. table_name .. "." .. field_name
      inp.bad = true
      return false
    end
    holder[key] = { value = out }
    return true
  elseif s == "bytes_blob" then
    -- §16.2 `*bytes`: a body that is not base64 is kind_mismatch, the
    -- reference left null; a decoded body is boxed as the `*string` row is.
    local before = report.kindMismatch
    local out = read_base64(inp, -1, report)
    if out == nil then return false end
    if report.kindMismatch ~= before then
      holder[key] = elem and false or nil
    else
      holder[key] = { value = out == false and "" or out }
    end
    return true
  elseif s == "array" then
    if d.keyed ~= nil then return read_keyed(T, d, inp, holder, key, depth, graph, report, table_name, field_name) end
    return read_array(T, d, inp, holder, key, depth, graph, report, table_name, field_name)
  elseif s == "map" then
    return read_map(T, d, inp, holder, key, depth, graph, report, table_name, field_name)
  elseif s == "table" then
    local td = T.Tables[d.target]
    if td == nil then
      inp.bad = true
      return false
    end
    local node = fresh(T, td)
    holder[key] = node
    if td.array_root then
      -- THE ARRAY ROOT NESTED (docs/SPEC-TABLES.md §16.10): the by-value
      -- field's text is the array too, and an object there is the ordinary
      -- §16.2 kind mismatch, skipped and counted.
      if td.fields == nil or #td.fields ~= 1 then
        inp.bad = true
        return false
      end
      local fd = td.fields[1]
      if value_shape(inp) ~= "a" then
        report.kindMismatch = report.kindMismatch + 1
        if not skip_value(inp, depth + 1, graph) then return false end
        return true
      end
      return read_value(T, fd, inp, node, fd.name, depth + 1, graph, report, false, td.name, fd.name)
    end
    return read_table(T, td, inp, node, depth + 1, graph, report)
  elseif s == "union" then
    local u = T.Unions[d.target]
    if u == nil then
      inp.bad = true
      return false
    end
    local out = read_union(T, u, inp, depth, graph, report, table_name, field_name)
    if out == nil then return false end
    holder[key] = out
    return true
  elseif s == "enum" then
    local start = inp.p
    local _, _, ok = scan_string(inp, MAX_KEY)
    if not ok then return false end
    local name = inp.t:sub(start + 1, inp.p - 2)
    if name == "None" then
      holder[key] = elem and false or nil
      return true
    end
    for _, v in ipairs(d.variants) do
      if v[1] == name then
        if v.deprecated then
          -- a retired variant keeps its name and reads the declared default,
          -- an element's None, never unknown (docs/SPEC-TABLES.md §2.15)
          if elem then
            holder[key] = false
          else
            holder[key] = d.default
          end
          return true
        end
        holder[key] = name
        return true
      end
    end
    holder[key] = elem and false or nil
    report.unknown = report.unknown + 1
    return true
  elseif s == "flags" then
    if peek(inp) ~= "[" then
      inp.bad = true
      return false
    end
    inp.p = inp.p + 1
    local bits = 0
    while true do
      local c = peek(inp)
      if c == "]" then
        inp.p = inp.p + 1
        break
      end
      if c == nil then
        inp.bad = true
        return false
      end
      if c ~= '"' then
        report.kindMismatch = report.kindMismatch + 1
        if not skip_value(inp, depth + 1, graph) then return false end
      else
        local st = inp.p
        local _, _, ok = scan_string(inp, MAX_KEY)
        if not ok then return false end
        local nm = inp.t:sub(st + 1, inp.p - 2)
        local found = false
        local names = d.variants or {}
        for i, v in ipairs(names) do
          if v == nm then
            bits = bits | (1 << (i - 1))
            found = true
            break
          end
        end
        if not found then report.unknown = report.unknown + 1 end
      end
      c = peek(inp)
      if c == "," then
        inp.p = inp.p + 1
      elseif c == "]" then
        inp.p = inp.p + 1
        break
      else
        inp.bad = true
        return false
      end
    end
    holder[key] = bits
    return true
  elseif s == "bool" then
    local c = peek(inp)
    if c == "t" then
      if not literal(inp, "true") then return false end
      holder[key] = true
      return true
    end
    if not literal(inp, "false") then return false end
    holder[key] = false
    return true
  elseif s == "float" then
    local token = scan_number(inp)
    if token == nil then
      inp.bad = true
      return false
    end
    local x = tonumber(token)
    -- A `-0` TOKEN IS THE FLOAT -0.0 (docs/SPEC-TABLES.md §3.3, "-0 IS A
    -- VALUE, AND IT RIDES"; §16.3): Lua reads an integer token "-0" as +0, so
    -- the sign the token spells is kept here.
    if x == 0 and token:sub(1, 1) == "-" then x = -0.0 end
    if x == nil or x ~= x or x == math.huge or x == -math.huge then
      report.kindMismatch = report.kindMismatch + 1
      return true
    end
    if d.f32 then
      local narrow = string.unpack("<f", string.pack("<f", x))
      if narrow == math.huge or narrow == -math.huge then
        -- the EXACT float32 overflow midpoint, compared on the token's own
        -- digits: parsing through a double first rounds the upper band onto
        -- the midpoint, where narrowing would answer infinity (§16.2)
        local mid = "340282356779733661637539395458142568448"
        local neg = token:sub(1, 1) == "-"
        local digits = neg and token:sub(2) or token
        local below = false
        if digits:match("^%d+$") then
          local d0 = digits:gsub("^0+", "")
          if d0 == "" then
            below = true
          elseif #d0 < #mid then
            below = true
          elseif #d0 == #mid and d0 < mid then
            below = true
          end
        end
        if below then
          x = neg and -3.4028234663852886e38 or 3.4028234663852886e38
        else
          report.kindMismatch = report.kindMismatch + 1
          return true
        end
      else
        x = narrow
      end
    end
    holder[key] = x
    return true
  elseif s == "wide" then
    -- §16.2's wide rows: a 128-bit integer takes any integral token, a fixed
    -- takes any token whose value is exactly representable in Q I.F; a finer
    -- fraction is the wrong shape, never rounded.
    return read_wide(inp, d, holder, key, report)
  elseif s == "int" then
    if d.timestamp then
      -- §2.11, §16.2: a timestamp is read from its RFC 3339 text, never the
      -- wide/fixed integer spelling kind 36 shares a width with.
      local text, _, ok = scan_string(inp, -1)
      if not ok then return false end
      local ns = parse_timestamp(text)
      if ns == nil then
        -- a string outside RFC 3339 §5.6's exact date-time production, or one
        -- naming an instant outside Time.UnixNano's domain, is refused by
        -- name.
        report.Reason = "timestamp_not_rfc3339: " .. field_name
        report.malformed = true
        inp.bad = true
        return false
      end
      holder[key] = ns
      return true
    end
    if d.kind ~= nil and d.kind >= 18 then
      -- a fixed/ufixed narrower than 128 bits rides in a Lua integer but its
      -- TEXT is the wide whole-unit spelling (§16.2)
      return read_wide(inp, d, holder, key, report)
    end
    local token = scan_number(inp)
    if token == nil then
      inp.bad = true
      return false
    end
    local integral = token:find("[%.eE]") == nil
    local n = interpret_integer(token, integral)
    local v = place_integer(d, n, report)
    if v ~= nil then holder[key] = v end
    return true
  end
  inp.bad = true
  return false
end

parse_timestamp = function(text)
  -- docs/SPEC-TABLES.md §2.11 and §16.2: the text form is EXACTLY RFC 3339
  -- §5.6's date-time production, no more and no less. Month 01-12, a day valid
  -- for that month and year, hour 00-23, minute 00-59 and second 00-59 (a leap
  -- second is refused); a fraction of one or more digits after a dot truncates
  -- to nanoseconds; and Z, z or a signed HH:MM offset whose hours are 00-23 and
  -- minutes 00-59, with nothing before or after. An instant outside
  -- Time.UnixNano's domain names no count and is refused too.
  local n = #text
  if n < 20 then return nil end
  if text:sub(5, 5) ~= "-" or text:sub(8, 8) ~= "-" or text:sub(14, 14) ~= ":" or text:sub(17, 17) ~= ":" then
    return nil
  end
  local sep = text:sub(11, 11)
  if sep ~= "T" and sep ~= "t" then return nil end

  local function digit(i)
    local b = string.byte(text, i)
    if b == nil or b < 48 or b > 57 then return nil end
    return b - 48
  end
  local function two(i)
    local hi, lo = digit(i), digit(i + 1)
    if hi == nil or lo == nil then return nil end
    return hi * 10 + lo
  end

  local year = 0
  for i = 1, 4 do
    local d = digit(i)
    if d == nil then return nil end
    year = year * 10 + d
  end
  local month = two(6)
  local day = two(9)
  local hour = two(12)
  local minute = two(15)
  local second = two(18)
  if month == nil or day == nil or hour == nil or minute == nil or second == nil then return nil end
  if month < 1 or month > 12 then return nil end
  local month_days
  if month == 2 then
    month_days = (year % 4 == 0 and (year % 100 ~= 0 or year % 400 == 0)) and 29 or 28
  elseif month == 4 or month == 6 or month == 9 or month == 11 then
    month_days = 30
  else
    month_days = 31
  end
  if day < 1 or day > month_days then return nil end
  if hour > 23 or minute > 59 or second > 59 then return nil end

  local index = 20
  local nanos = 0
  if text:sub(index, index) == "." then
    index = index + 1
    local digits = 0
    while index <= n do
      local d = digit(index)
      if d == nil then break end
      if digits < 9 then nanos = nanos * 10 + d end
      digits = digits + 1
      index = index + 1
    end
    if digits == 0 then return nil end
    while digits < 9 do
      nanos = nanos * 10
      digits = digits + 1
    end
  end

  local offset = 0
  if index > n then return nil end
  local c = text:sub(index, index)
  if c == "Z" or c == "z" then
    index = index + 1
    if index ~= n + 1 then return nil end
  elseif c == "+" or c == "-" then
    local sign = c == "-" and -1 or 1
    index = index + 1
    local oh = two(index)
    index = index + 2
    if oh == nil or text:sub(index, index) ~= ":" then return nil end
    index = index + 1
    local om = two(index)
    index = index + 2
    if om == nil then return nil end
    if oh > 23 or om > 59 then return nil end
    if index ~= n + 1 then return nil end
    offset = sign * (oh * 3600 + om * 60)
  else
    return nil
  end

  -- days_from_civil (Hinnant), exact for the four-digit years RFC 3339 allows
  local y, m = year, month
  if m <= 2 then y = y - 1 end
  local era = y // 400
  local yoe = y - era * 400
  local moy = m + 9
  if m > 2 then moy = m - 3 end
  local doy = (153 * moy + 2) // 5 + day - 1
  local doe = yoe * 365 + yoe // 4 - yoe // 100 + doy
  local days = era * 146097 + doe - 719468
  local seconds = days * 86400 + hour * 3600 + minute * 60 + second - offset

  -- Time.UnixNano's domain: the second before 1677-09-21T00:12:43 is
  -- -9223372037 and its fraction must reach 145224192 nanoseconds; the second
  -- of 2262-04-11T23:47:16 is 9223372036 and its fraction may not pass
  -- 854775807.
  local min_second, max_second = -9223372037, 9223372036
  local min_nanos, max_nanos = 145224192, 854775807
  if seconds < min_second or seconds > max_second then return nil end
  if seconds == min_second and nanos < min_nanos then return nil end
  if seconds == max_second and nanos > max_nanos then return nil end
  return seconds * 1000000000 + nanos
end

-- ONE TEXT: the root's object and nothing after it (§16.1). This is the
-- TOLERANT read: it keeps the recovered value and counts every event it met.
function J.FromJsonTolerant(T, name, text, report)
  local desc = T.Tables[name]
  if desc == nil then
    report.malformed = true
    return nil
  end
  local inp = new_in(text)
  local graph = nil
  if desc.variable then graph = {} end
  local v = fresh(T, desc)
  local ok
  if desc.array_root then
    -- THE ARRAY ROOT (docs/SPEC-TABLES.md §16.10): a top-level array fills
    -- the one field by its row; any other top-level value is refused by name.
    if desc.fields == nil or #desc.fields ~= 1 then
      inp.bad = true
    elseif value_shape(inp) ~= "a" then
      report.Reason = "json_root_not_array: " .. name
      inp.bad = true
    else
      local fd = desc.fields[1]
      ok = read_value(T, fd, inp, v, fd.name, 0, graph, report, false, desc.name, fd.name)
    end
  else
    ok = read_table(T, desc, inp, v, 0, graph, report)
  end
  if ok then
    space(inp)
    if inp.p ~= #inp.t + 1 then inp.bad = true end
  end
  if inp.bad or not ok then report.malformed = true end
  if inp.utf8 and (report.Reason == nil or report.Reason == "") then
    -- §16.3: the READ refuses ill-formed UTF-8 by name; <root> names the top
    report.Reason = "string_not_utf8: <root>"
  end
  return v
end

-- THE PLAIN NAME IS THE STRICT READ (§6.5, §16.2): it answers nil where the
-- tolerant report is not clean under the strict set, writing that reason into
-- the supplied report. Unknown and duplicate alone are evolution, not loss,
-- and read clean. The precedence is Go's TableReport.Err
-- (internal/codegen/gotable/refusal.go): missing_required once carried, then
-- malformed, blob_shared, clamped and kind_mismatch.
function J.FromJson(T, name, text, report)
  local v = J.FromJsonTolerant(T, name, text, report)
  local reason = nil
  if report.MissingRequired ~= nil and report.MissingRequired > 0 then
    reason = report.Reason or "missing_required"
  elseif report.malformed then
    reason = "malformed"
  elseif report.blobShared ~= nil and report.blobShared > 0 then
    reason = "blob_shared"
  elseif report.clamped ~= nil and report.clamped > 0 then
    -- TableClampedField (§2.13): a clamp that named its field refuses naming it
    if report.ClampedField ~= nil and report.ClampedField ~= "" then
      reason = "clamped: " .. report.ClampedField
    else
      reason = "clamped"
    end
  elseif report.kindMismatch ~= nil and report.kindMismatch > 0 then
    reason = "kind_mismatch"
  end
  if reason ~= nil then
    report.Reason = reason
    return nil
  end
  return v
end

----------------------------------------------------------------------------
-- the write walk (§16.1, §16.3, §16.7)
----------------------------------------------------------------------------

local function W()
  return { parts = {} }
end
local function raw(w, s)
  w.parts[#w.parts + 1] = s
end
local function put(w, c)
  w.parts[#w.parts + 1] = c
end
local function line(w, depth)
  w.parts[#w.parts + 1] = "\n" .. string.rep("  ", depth)
end

local function write_string(w, s)
  local hex = "0123456789abcdef"
  put(w, '"')
  local i = 1
  while i <= #s do
    local c = s:byte(i)
    if c == 34 then
      raw(w, '\\"')
      i = i + 1
    elseif c == 92 then
      raw(w, "\\\\")
      i = i + 1
    elseif c == 8 then
      raw(w, "\\b")
      i = i + 1
    elseif c == 12 then
      raw(w, "\\f")
      i = i + 1
    elseif c == 10 then
      raw(w, "\\n")
      i = i + 1
    elseif c == 13 then
      raw(w, "\\r")
      i = i + 1
    elseif c == 9 then
      raw(w, "\\t")
      i = i + 1
    elseif c < 0x20 then
      raw(w, "\\u00" .. hex:sub((c >> 4) + 1, (c >> 4) + 1) .. hex:sub((c & 0xf) + 1, (c & 0xf) + 1))
      i = i + 1
    elseif c < 0x80 then
      put(w, string.char(c))
      i = i + 1
    else
      local width = utf8_seq(s, i)
      if width < 0 then
        raw(w, utf8_encode(0xfffd))
        i = i + 1
      else
        raw(w, s:sub(i, i + width - 1))
        i = i + width
      end
    end
  end
  put(w, '"')
end

local function write_wstring(w, units)
  local hex = "0123456789abcdef"
  put(w, '"')
  local i = 1
  while i <= #units do
    local code = units[i]
    if code >= 0xd800 and code <= 0xdbff and i + 1 <= #units then
      local low = units[i + 1]
      if low >= 0xdc00 and low <= 0xdfff then
        code = 0x10000 + ((code - 0xd800) << 10) + (low - 0xdc00)
        i = i + 1
      end
    end
    if code >= 0xd800 and code <= 0xdfff then code = 0xfffd end
    if code == 34 then
      raw(w, '\\"')
    elseif code == 92 then
      raw(w, "\\\\")
    elseif code == 8 then
      raw(w, "\\b")
    elseif code == 12 then
      raw(w, "\\f")
    elseif code == 10 then
      raw(w, "\\n")
    elseif code == 13 then
      raw(w, "\\r")
    elseif code == 9 then
      raw(w, "\\t")
    elseif code < 0x20 then
      raw(w, "\\u00" .. hex:sub((code >> 4) + 1, (code >> 4) + 1) .. hex:sub((code & 0xf) + 1, (code & 0xf) + 1))
    else
      raw(w, utf8_encode(code))
    end
    i = i + 1
  end
  put(w, '"')
end

local function write_base64(w, data)
  put(w, '"')
  local i = 1
  while #data - i + 1 >= 3 do
    local a, b, c = data:byte(i, i + 2)
    local t = (a << 16) | (b << 8) | c
    raw(
      w,
      BASE64:sub(((t >> 18) & 63) + 1, ((t >> 18) & 63) + 1)
        .. BASE64:sub(((t >> 12) & 63) + 1, ((t >> 12) & 63) + 1)
        .. BASE64:sub(((t >> 6) & 63) + 1, ((t >> 6) & 63) + 1)
        .. BASE64:sub((t & 63) + 1, (t & 63) + 1)
    )
    i = i + 3
  end
  local left = #data - i + 1
  if left > 0 then
    local a = data:byte(i)
    local b = left == 2 and data:byte(i + 1) or 0
    local t = (a << 16) | (b << 8)
    raw(
      w,
      BASE64:sub(((t >> 18) & 63) + 1, ((t >> 18) & 63) + 1)
        .. BASE64:sub(((t >> 12) & 63) + 1, ((t >> 12) & 63) + 1)
        .. (left == 2 and BASE64:sub(((t >> 6) & 63) + 1, ((t >> 6) & 63) + 1) or "=")
        .. "="
    )
  end
  put(w, '"')
end

-- the unsigned decimal of a 64-bit pattern, with integer arithmetic never a
-- float and never a signed `%d` (docs/PORT-LUA.md, 64-BIT AND 128-BIT INTEGERS)
local function u64_text(v)
  if v >= 0 then return string.format("%d", v) end
  local ok, s = pcall(string.format, "%u", v)
  if ok then return s end
  -- fall back to digit-by-digit unsigned division by ten
  local digits = {}
  local q = v
  repeat
    local r = 0
    local nq = 0
    for bit = 63, 0, -1 do
      r = r << 1
      local b = (q >> bit) & 1
      r = r | b
      if math.ult(r, 10) then
        nq = nq << 1
      else
        r = r - 10
        nq = (nq << 1) | 1
      end
    end
    local d = 0
    while r > 0 do
      r = r - 1
      d = d + 1
    end
    digits[#digits + 1] = string.char(48 + d)
    q = nq
  until q == 0
  local out = {}
  for i = #digits, 1, -1 do
    out[#out + 1] = digits[i]
  end
  return table.concat(out)
end

local function civil_from_days(z)
  z = z + 719468
  local era = (z >= 0 and z or z - 146096) // 146097
  local doe = z - era * 146097
  local yoe = (doe - doe // 1460 + doe // 36524 - doe // 146096) // 365
  local y = yoe + era * 400
  local doy = doe - (365 * yoe + yoe // 4 - yoe // 100)
  local mp = (5 * doy + 2) // 153
  local d = doy - (153 * mp + 2) // 5 + 1
  local m = mp < 10 and mp + 3 or mp - 9
  if m <= 2 then y = y + 1 end
  return y, m, d
end

local function write_timestamp(w, ns)
  local day = 86400000000000
  local days = ns // day
  local rem = ns - days * day
  local sec = rem // 1000000000
  local frac = rem % 1000000000
  local y, m, d = civil_from_days(days)
  raw(w, string.format('"%04d-%02d-%02dT%02d:%02d:%02d', y, m, d, sec // 3600, (sec % 3600) // 60, sec % 60))
  if frac ~= 0 then
    local f = string.format("%09d", frac):gsub("0+$", "")
    raw(w, "." .. f)
  end
  raw(w, 'Z"')
end

local function format_g(v, prec)
  return string.format("%." .. prec .. "g", v)
end

local function float_text(v, single)
  if v ~= v or v == math.huge or v == -math.huge then return nil end
  -- -0 IS A VALUE AND WRITES AS -0, exactly as Go's writer does (§3.3,
  -- §16.3); -0 == +0 under ==, so the sign is read from the reciprocal.
  if v == 0 and 1 / v < 0 then return "-0" end
  local low, high = single and 6 or 15, single and 9 or 17
  for digits = low, high do
    local text = format_g(v, digits)
    if digits >= high then return text end
    local back = tonumber(text)
    if back ~= nil then
      if single then
        local narrow = string.unpack("<f", string.pack("<f", back))
        if narrow == v then return text end
      elseif back == v then
        return text
      end
    end
  end
  return nil
end

-- a map's entries in ascending key order, bytewise for strings, the key's own
-- signedness for integers (§2.8)
local bytes_less = D.bytes_less

local function map_entries(cache, fd, x)
  if x == nil then return {} end
  local got = cache[x]
  if got ~= nil then return got end
  local kf = fd.key_field
  local keys = {}
  for key in pairs(x) do
    keys[#keys + 1] = key
  end
  table.sort(keys, function(a, b)
    if kf.shape == "string" then return bytes_less(a, b) end
    if kf.signed then return a < b end
    return math.ult(a, b)
  end)
  got = {}
  for i, key in ipairs(keys) do
    got[i] = key
  end
  cache[x] = got
  return got
end

local count_of = D.count_of
local guard_holds = D.guard_holds

-- THE NUMBERING PASS (§16.7): how many slots name each node, and whether the
-- graph has a cycle, which the text refuses as the wire does (§3.1). It is the
-- one pointer walk of descriptor.lua (D.number_nodes, docs/PORTING.md M15): the
-- reference count is the walk's own, so the text's labels agree with the
-- wire's numbering by construction.
local function count_nodes(T, desc, root)
  local nodes = D.number_nodes(T, desc, root, false)
  if nodes == nil then return nil end
  return { count = nodes.refs, labels = {}, next = 0 }
end

local write_value, write_table, write_pointer, write_field, write_fields

write_fields = function(T, desc, v, depth, g, w, any)
  -- EVERY NAMED FIELD IS WRITTEN, defaults included (docs/SPEC-TABLES.md
  -- §16.1), so a `| required` field is never omitted at its default (§2.3,
  -- §16.2): the only skips are a false `if` guard and an absent optional,
  -- and neither can hold for a required field (§2.3).
  for _, fd in ipairs(desc.fields) do
    if guard_holds(fd, v) and not (fd.optional and not v[fd.name .. "_present"]) then
      if not any then
        put(w, "{")
      else
        put(w, ",")
      end
      any = true
      line(w, depth + 1)
      write_string(w, fd.key)
      raw(w, ": ")
      if not write_field(T, fd, v, depth + 1, g, w) then return false, any end
    end
  end
  return true, any
end

write_table = function(T, desc, v, depth, g, w)
  if depth > MAX_DEPTH then return false end
  if desc.array_root then
    -- THE ARRAY ROOT (docs/SPEC-TABLES.md §16.10): the table's text IS its
    -- one field's array — written and read as that field's own §16.2 row,
    -- with no object around it and no key.
    if desc.fields == nil or #desc.fields ~= 1 then return false end
    local fd = desc.fields[1]
    return write_field(T, fd, v, depth, g, w)
  end
  local ok, any = write_fields(T, desc, v, depth, g, w, false)
  if not ok then return false end
  if not any then
    raw(w, "{}")
    return true
  end
  line(w, depth)
  put(w, "}")
  return true
end

write_pointer = function(T, target, node, depth, g, w)
  if node == nil or node == false then
    raw(w, "null")
    return true
  end
  local count = g.count[node] or 0
  local desc = T.Tables[target]
  if count <= 1 then return write_table(T, desc, node, depth, g, w) end
  if depth > MAX_DEPTH then return false end
  local label = g.labels[node]
  if label ~= nil then
    put(w, "{")
    line(w, depth + 1)
    raw(w, '"&node": ' .. string.format("%d", label))
    line(w, depth)
    put(w, "}")
    return true
  end
  g.next = g.next + 1
  label = g.next
  g.labels[node] = label
  put(w, "{")
  line(w, depth + 1)
  raw(w, '"&node": ' .. string.format("%d", label))
  local before = #w.parts
  local ok = write_fields(T, desc, node, depth, g, w, true)
  if not ok then return false end
  if #w.parts == before then return false end
  line(w, depth)
  put(w, "}")
  return true
end

local function write_union(T, u, val, depth, g, w)
  local tag = val and val.tag or 0
  if tag == 0 then
    raw(w, "{}")
    return true
  end
  local arm = u and u.arms[tag]
  if arm == nil then return false end
  put(w, "{")
  line(w, depth + 1)
  write_string(w, arm.name)
  raw(w, ": ")
  if arm.field == nil then
    raw(w, "null")
  else
    if not write_value(T, arm.field, val[arm.slot], depth + 1, g, w, false) then return false end
  end
  line(w, depth)
  put(w, "}")
  return true
end

-- ONE text list written as its plain JSON array (§16.2): the element record's
-- string, or `null` for an absent ?string element. The write-side bounds are
-- §2.13's: the element past M, a zero byte or ill-formed UTF-8, and a sum past
-- `| total = N` refuse the value (nil from ToJson), as the generated Go does.
local function write_text_list(T, fd, _d, vf, val, depth, _g, w)
  local n = count_of(fd, val)
  local total = fd.total or 0
  local spent = 0
  local texts = {}
  for i = 1, n do
    local rec = val[i]
    if rec == nil then rec = zero_elem(T, fd.elem) end
    if vf.optional and rec[vf.present] ~= true then
      texts[i] = false
    else
      local s = rec[vf.name]
      if s == nil then s = "" end
      if vf.bound ~= nil and vf.bound > 0 and #s > vf.bound then return false end
      if s:find("\0", 1, true) ~= nil then return false end
      local at = 1
      while at <= #s do
        local width = utf8_seq(s, at)
        if width < 0 then return false end
        at = at + width
      end
      spent = spent + #s
      if total > 0 and spent > total then return false end
      texts[i] = s
    end
  end
  if n == 0 then
    raw(w, "[]")
    return true
  end
  put(w, "[")
  for i = 1, n do
    if i > 1 then put(w, ",") end
    line(w, depth + 1)
    if texts[i] == false then
      raw(w, "null")
    else
      write_string(w, texts[i])
    end
  end
  line(w, depth)
  put(w, "]")
  return true
end

local function write_array(T, fd, val, depth, g, w)
  local td, tvf = text_list_elem(T, fd)
  if td ~= nil then return write_text_list(T, fd, td, tvf, val, depth, g, w) end
  local n = count_of(fd, val)
  if n == 0 then
    raw(w, "[]")
    return true
  end
  local vf = text_list_value(T, fd.elem)
  put(w, "[")
  for i = 1, n do
    if i > 1 then put(w, ",") end
    line(w, depth + 1)
    local x = val[i]
    if x == nil then x = zero_elem(T, fd.elem) end
    if vf then
      -- a text list element IS its value (docs/SPEC-TABLES.md §2.13), so
      -- the record's field is written alone
      if not write_value(T, vf, x.value, depth + 1, g, w, true) then return false end
    elseif not write_value(T, fd.elem, x, depth + 1, g, w, true) then
      return false
    end
  end
  line(w, depth)
  put(w, "]")
  return true
end

local function write_keyed(T, fd, val, depth, g, w)
  local n = fd.bound or 0
  if n == 0 then
    raw(w, "{}")
    return true
  end
  put(w, "{")
  for slot = 1, n do
    if slot > 1 then put(w, ",") end
    line(w, depth + 1)
    local keyed = fd.keyed and fd.keyed[slot]
    if keyed == nil then return false end
    write_string(w, keyed[1])
    raw(w, ": ")
    local x = val[slot]
    if x == nil then x = zero_elem(T, fd.elem) end
    if not write_value(T, fd.elem, x, depth + 1, g, w, true) then return false end
  end
  line(w, depth)
  put(w, "}")
  return true
end

local function write_map(T, fd, val, depth, g, w)
  local keys = map_entries({}, fd, val)
  if #keys == 0 then
    raw(w, "{}")
    return true
  end
  local kf = fd.key_field
  put(w, "{")
  for i, key in ipairs(keys) do
    if i > 1 then put(w, ",") end
    line(w, depth + 1)
    if kf.shape == "string" then
      write_string(w, key)
    else
      write_string(w, kf.signed and string.format("%d", key) or u64_text(key))
    end
    raw(w, ": ")
    if not write_value(T, fd.value_field, val[key], depth + 1, g, w, true) then return false end
  end
  line(w, depth)
  put(w, "}")
  return true
end

write_value = function(T, d, val, depth, g, w, _elem)
  local s = d.shape
  if s == "pointer" then
    return write_pointer(T, d.target, val, depth, g, w)
  elseif s == "string" then
    write_string(w, val or "")
    return true
  elseif s == "wstring" then
    write_wstring(w, val or {})
    return true
  elseif s == "bytes" then
    write_base64(w, val or "")
    return true
  elseif s == "string_blob" or s == "bytes_blob" then
    -- §16.2 `*bytes`/`*string`, docs/SPEC-TABLES.md §16.7 (the byte buffer
    -- paragraph): the blob is written in place and takes no label, so a blob
    -- named from more than one slot has no spelling and the writer refuses
    -- the graph. A loaded blob is its node box (load.lua); a plain string
    -- built in code is accepted as save.lua accepts it, and as there equal
    -- plain strings in two slots are ONE node, so the writer refuses them.
    if val == nil or val == false then
      raw(w, "null")
      return true
    end
    if (g.count[val] or 0) > 1 then return false end
    local body = type(val) == "table" and val.value or val
    if s == "string_blob" then
      write_string(w, body)
    else
      write_base64(w, body)
    end
    return true
  elseif s == "table" then
    local desc = T.Tables[d.target]
    if desc == nil then return false end
    return write_table(T, desc, val or fresh(T, desc), depth, g, w)
  elseif s == "union" then
    return write_union(T, T.Unions[d.target], val, depth, g, w)
  elseif s == "array" then
    if d.keyed ~= nil then return write_keyed(T, d, val or {}, depth, g, w) end
    return write_array(T, d, val or {}, depth, g, w)
  elseif s == "map" then
    return write_map(T, d, val or {}, depth, g, w)
  elseif s == "enum" then
    if val == nil or val == false then
      write_string(w, "None")
      return true
    end
    for _, v in ipairs(d.variants) do
      if v[1] == val then
        write_string(w, val)
        return true
      end
    end
    return false
  elseif s == "flags" then
    local bits = val or 0
    if bits == 0 then
      raw(w, "[]")
      return true
    end
    local seen = 0
    put(w, "[")
    local first = true
    local names = d.variants or {}
    for i, name in ipairs(names) do
      if (bits & (1 << (i - 1))) ~= 0 then
        if not first then put(w, ",") end
        first = false
        line(w, depth + 1)
        write_string(w, name)
        seen = seen | (1 << (i - 1))
      end
    end
    if (bits & ~seen) ~= 0 then return false end
    line(w, depth)
    put(w, "]")
    return true
  elseif s == "bool" then
    raw(w, val and "true" or "false")
    return true
  elseif s == "float" then
    local text = float_text(val, d.f32)
    if text == nil then return false end
    raw(w, text)
    return true
  elseif s == "wide" then
    raw(w, wide_text(d, val))
    return true
  elseif s == "int" then
    if d.timestamp then
      write_timestamp(w, val)
      return true
    end
    if d.kind ~= nil and d.kind >= 18 then
      raw(w, wide_text(d, val))
      return true
    end
    if d.signed then
      raw(w, string.format("%d", val))
    else
      raw(w, u64_text(val))
    end
    return true
  end
  return false
end

write_field = function(T, fd, v, depth, g, w)
  if fd.shape == "pointer" then return write_pointer(T, fd.target, v[fd.name], depth, g, w) end
  return write_value(T, fd, v[fd.name], depth, g, w, false)
end

-- ONE INSTANCE as ONE text (§16.1): a trailing newline belongs to the form.
function J.ToJson(T, name, v)
  local desc = T.Tables[name]
  if desc == nil or v == nil then return nil end
  -- docs/SPEC-TABLES.md §2.15: a retired variant is refused, by name
  local refused = D.refuse_deprecated(T, name, v)
  if refused ~= nil then return nil, refused end
  local g = count_nodes(T, desc, v)
  if g == nil then return nil end
  local w = W()
  -- a float the text has no spelling for (a NaN or an infinity, §3.3, §16.3)
  -- refuses the write, named
  if not write_table(T, desc, v, 0, g, w) then return nil, D.refuse_non_finite(T, name, v) end
  w.parts[#w.parts + 1] = "\n"
  return table.concat(w.parts)
end

return J
