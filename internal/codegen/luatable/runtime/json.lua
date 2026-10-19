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
  return string.char(0xf0 | (code >> 18), 0x80 | ((code >> 12) & 0x3f),
    0x80 | ((code >> 6) & 0x3f), 0x80 | (code & 0x3f))
end

-- the length of one WELL-FORMED UTF-8 sequence at s[i], or -1 (§16.3)
local function utf8_seq(s, i)
  local c = s:byte(i)
  if c == nil then return -1 end
  if c < 0x80 then return 1 end
  local want, code
  if c >= 0xc2 and c <= 0xdf then want, code = 2, c & 0x1f
  elseif c >= 0xe0 and c <= 0xef then want, code = 3, c & 0x0f
  elseif c >= 0xf0 and c <= 0xf4 then want, code = 4, c & 0x07
  else return -1 end
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
  if c < 0xe0 then code, at = c & 0x1f, 2
  elseif c < 0xf0 then code, at = c & 0x0f, 3
  else code, at = c & 0x07, 4 end
  for i = 2, at do code = (code << 6) | (s:byte(i) & 0x3f) end
  return code
end

----------------------------------------------------------------------------
-- the reader (§16.2)
----------------------------------------------------------------------------

local function new_in(text) return { t = text, p = 1, bad = false } end

local function space(inp)
  while inp.p <= #inp.t do
    local c = inp.t:byte(inp.p)
    if c == 32 or c == 9 or c == 10 or c == 13 then
      inp.p = inp.p + 1
    elseif c == 47 then
      local n = inp.t:byte(inp.p + 1)
      if n == 47 then
        inp.p = inp.p + 2
        while inp.p <= #inp.t and inp.t:byte(inp.p) ~= 10 do inp.p = inp.p + 1 end
      elseif n == 42 then
        local e = inp.t:find("*/", inp.p + 2, true)
        if e == nil then inp.bad = true; inp.p = #inp.t + 1; return end
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
  if inp.p > #inp.t then inp.bad = true; return nil, false, false end
  local c = inp.t:byte(inp.p)
  if c == 34 then inp.p = inp.p + 1; return nil, true, true end
  if c == 92 then
    inp.p = inp.p + 1
    if inp.p > #inp.t then inp.bad = true; return nil, false, false end
    local e = inp.t:sub(inp.p, inp.p)
    inp.p = inp.p + 1
    local unit
    if e == '"' then unit = '"'
    elseif e == '\\' then unit = '\\'
    elseif e == '/' then unit = '/'
    elseif e == 'b' then unit = '\b'
    elseif e == 'f' then unit = '\f'
    elseif e == 'n' then unit = '\n'
    elseif e == 'r' then unit = '\r'
    elseif e == 't' then unit = '\t'
    elseif e == 'u' then
      local high = hex4(inp)
      if high == nil then inp.bad = true; return nil, false, false end
      local code = high
      if high >= 0xd800 and high <= 0xdbff and inp.p + 1 <= #inp.t
        and inp.t:sub(inp.p, inp.p + 1) == "\\u" then
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
  if c < 0x20 then inp.bad = true; return nil, false, false end
  -- a raw UTF-8 sequence, consumed whole; anything that is not a code point
  -- reads as U+FFFD, one per sequence (§16.3)
  local want = 1
  if c >= 0xc2 and c <= 0xdf then want = 2
  elseif c >= 0xe0 and c <= 0xef then want = 3
  elseif c >= 0xf0 and c <= 0xf4 then want = 4 end
  if c >= 0x80 and c <= 0xbf then want = 1 end
  local start = inp.p
  inp.p = inp.p + 1
  while inp.p - start < want and inp.p <= #inp.t do
    local b = inp.t:byte(inp.p)
    if b < 0x80 or b > 0xbf then break end
    inp.p = inp.p + 1
  end
  local unit = inp.t:sub(start, inp.p - 1)
  if utf8_seq(unit, 1) ~= #unit then unit = utf8_encode(0xfffd) end
  return unit, false, true
end

-- ONE JSON string into bytes, clamped at cap bytes (cap < 0 unbounded)
local function scan_string(inp, cap)
  if peek(inp) ~= '"' then inp.bad = true; return nil, false, false end
  inp.p = inp.p + 1
  local out, n, clamped = {}, 0, false
  while true do
    local unit, done, ok = scan_unit(inp)
    if not ok then return nil, false, false end
    if done then break end
    if clamped or (cap >= 0 and n + #unit > cap) then
      clamped = true
    else
      out[#out + 1] = unit
      n = n + #unit
    end
  end
  return table.concat(out), clamped, true
end

-- ONE JSON string into UTF-16 code units, clamped at cap units (§16.2)
local function scan_wstring(inp, cap)
  if peek(inp) ~= '"' then inp.bad = true; return nil, false, false end
  inp.p = inp.p + 1
  local out, n, clamped = {}, 0, false
  while true do
    local unit, done, ok = scan_unit(inp)
    if not ok then return nil, false, false end
    if done then break end
    local code = utf8_decode(unit)
    local units
    if code < 0x10000 then
      units = { code }
    else
      local rest = code - 0x10000
      units = { 0xd800 + (rest >> 10), 0xdc00 + (rest & 0x3ff) }
    end
    if clamped or (cap >= 0 and n + #units > cap) then
      clamped = true
    else
      for i = 1, #units do out[#out + 1] = units[i] end
      n = n + #units
    end
  end
  return out, clamped, true
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
    while digit() do inp.p = inp.p + 1 end
  else
    return nil
  end
  if inp.t:byte(inp.p) == 46 then
    inp.p = inp.p + 1
    if not digit() then return nil end
    while digit() do inp.p = inp.p + 1 end
  end
  local e = inp.t:byte(inp.p)
  if e == 101 or e == 69 then
    inp.p = inp.p + 1
    local s = inp.t:byte(inp.p)
    if s == 45 or s == 43 then inp.p = inp.p + 1 end
    if not digit() then return nil end
    while digit() do inp.p = inp.p + 1 end
  end
  local n = inp.p - start
  if n <= 0 or n > MAX_NUMBER then return nil end
  return inp.t:sub(start, inp.p - 1)
end

local function scan_label(inp)
  space(inp)
  local c = inp.t:byte(inp.p)
  if c == nil or c < 49 or c > 57 then inp.bad = true; return nil end
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
  for i = 1, #digits do v = v * 10 + (digits:byte(i) - 48) end
  return v
end

local skip_value

-- a skipped OBJECT may still be a pointer's: `&node` opening it takes its
-- label with no node, so a later reference reads null (§16.7)
local function skip_container(inp, close, depth, graph)
  if depth > MAX_DEPTH then inp.bad = true; return false end
  inp.p = inp.p + 1
  local first = true
  while true do
    local c = peek(inp)
    if c == close then inp.p = inp.p + 1; return true end
    if c == nil then inp.bad = true; return false end
    if close == '}' then
      local key, _, ok = scan_string(inp, MAX_KEY)
      if not ok then return false end
      if peek(inp) ~= ':' then inp.bad = true; return false end
      inp.p = inp.p + 1
      if key ~= "" and key:sub(1, 1) == "&" and graph ~= nil then
        if not first or key ~= "&node" then inp.bad = true; return false end
        local label = scan_label(inp)
        if label == nil then return false end
        if graph[label] == nil then graph[label] = { node = nil, st = nil, open = false } end
        first = false
        c = peek(inp)
        if c == ',' then
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
    if c == ',' then
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
  if c == '{' then return skip_container(inp, '}', depth, graph) end
  if c == '[' then return skip_container(inp, ']', depth, graph) end
  if c == '"' then local _, _, ok = scan_string(inp, -1); return ok end
  if c == 't' then return literal(inp, "true") end
  if c == 'f' then return literal(inp, "false") end
  if c == 'n' then return literal(inp, "null") end
  if c == nil then inp.bad = true; return false end
  if scan_number(inp) == nil then inp.bad = true; return false end
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
  if s == "bool" then return 'b' end
  if s == "string" or s == "wstring" or s == "bytes" then return 's' end
  if s == "string_blob" or s == "bytes_blob" then return 's' end
  if s == "enum" then return 's' end
  if s == "flags" then return 'a' end
  if s == "table" or s == "union" or s == "pointer" or s == "map" then return 'o' end
  if s == "array" then
    if d.keyed ~= nil then return 'o' end
    return 'a'
  end
  return 'n'
end

-- the JSON shape of the value at the cursor, without consuming it (§16.2)
local function value_shape(inp)
  local c = peek(inp)
  if c == '{' then return 'o' end
  if c == '[' then return 'a' end
  if c == '"' then return 's' end
  if c == 't' or c == 'f' then return 'b' end
  if c == 'n' then return 'z' end
  if c == nil then return nil end
  return 'n'
end

local function find_key(desc, key)
  for _, fd in ipairs(desc.fields) do
    if fd.key == key then return fd end
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
  for i = 1, #d do v = v * 10 + (d:byte(i) - 48) end
  return v, false
end

local function interpret_integer(token, integral)
  local out = { neg = false, mag = 0, frac = false, sat = false, finite = true }
  if integral then
    local t = token
    if t:sub(1, 1) == "-" then out.neg = true; t = t:sub(2) end
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
  if math.floor(a) ~= a then out.frac = true; return out end
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
  if bound < 0 then n.neg, n.mag = true, 0 - bound
  else n.neg, n.mag = false, bound end
end

local function place_integer(fd, n, report)
  if not n.finite or n.frac then
    report.kindMismatch = report.kindMismatch + 1
    return nil
  end
  if n.sat then report.clamped = report.clamped + 1 end
  if fd.lo ~= nil and fd.hi ~= nil then
    if int_less(n, fd.lo) then set_from_bound(n, fd.lo); report.clamped = report.clamped + 1
    elseif int_greater(n, fd.hi) then set_from_bound(n, fd.hi); report.clamped = report.clamped + 1 end
  end
  if fd.bits ~= nil and fd.bits > 0 and fd.bits < 64 then
    local bmax = (1 << fd.bits) - 1
    if int_greater(n, bmax) then set_from_bound(n, bmax); report.clamped = report.clamped + 1 end
  end
  local width = fd.width or 8
  local moved = false
  if fd.signed then
    local high = 0x7fffffffffffffff
    if width < 8 then high = (1 << (width * 8 - 1)) - 1 end
    if n.neg then
      local low = high + 1
      if math.ult(low, n.mag) then n.mag = low; moved = true end
      local v = 0 - n.mag
      if moved then report.clamped = report.clamped + 1 end
      return v
    end
    if math.ult(high, n.mag) then n.mag = high; moved = true end
    if moved then report.clamped = report.clamped + 1 end
    return n.mag
  end
  if n.neg then report.clamped = report.clamped + 1; return 0 end
  local high = MAX64
  if width < 8 then high = (1 << (width * 8)) - 1 end
  if math.ult(high, n.mag) then n.mag = high; moved = true end
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
-- the read walk
----------------------------------------------------------------------------

local read_value
local parse_timestamp

local function read_keys(T, desc, inp, v, depth, first_key, graph, report)
  local seen = {}
  while true do
    local key
    if first_key ~= nil then
      key, first_key = first_key, nil
    else
      local c = peek(inp)
      if c == '}' then inp.p = inp.p + 1; return true end
      if c == nil then inp.bad = true; return false end
      local start = inp.p
      local _, _, ok = scan_string(inp, MAX_KEY)
      if not ok then return false end
      key = inp.t:sub(start + 1, inp.p - 2)
      if peek(inp) ~= ':' then inp.bad = true; return false end
      inp.p = inp.p + 1
    end
    if key ~= "" and key:sub(1, 1) == "&" then inp.bad = true; return false end
    local fd = find_key(desc, key)
    if fd == nil then
      report.unknown = report.unknown + 1
      if not skip_value(inp, depth + 1, graph) then return false end
    else
      if seen[fd.name] then report.duplicate = report.duplicate + 1 end
      seen[fd.name] = true
      local shape = value_shape(inp)
      local takes_null = fd.optional or fd.shape == "pointer"
        or fd.shape == "string_blob" or fd.shape == "bytes_blob"
      if shape == 'z' and takes_null then
        literal(inp, "null")
        if fd.optional then
          local def = fresh(T, desc)
          v[fd.name] = def[fd.name]
          v[fd.name .. "_present"] = false
        else
          v[fd.name] = nil
        end
      else
        local want = shape_of(fd)
        if shape ~= want then
          report.kindMismatch = report.kindMismatch + 1
          if not skip_value(inp, depth + 1, graph) then return false end
        else
          if not read_value(T, fd, inp, v, fd.name, depth, graph, report, false) then return false end
        end
        if fd.optional then v[fd.name .. "_present"] = true end
      end
    end
    local c = peek(inp)
    if c == ',' then
      inp.p = inp.p + 1
    elseif c == '}' then
      inp.p = inp.p + 1
      return true
    else
      inp.bad = true
      return false
    end
  end
end

local function read_table(T, desc, inp, v, depth, graph, report)
  if depth > MAX_DEPTH then inp.bad = true; return false end
  if peek(inp) ~= '{' then inp.bad = true; return false end
  inp.p = inp.p + 1
  return read_keys(T, desc, inp, v, depth, nil, graph, report)
end

local function read_pointer(T, d, inp, holder, key, depth, graph, report)
  if depth + 1 > MAX_DEPTH then inp.bad = true; return false end
  if peek(inp) ~= '{' then inp.bad = true; return false end
  inp.p = inp.p + 1
  local c = peek(inp)
  if c == '}' then
    inp.p = inp.p + 1
    holder[key] = fresh(T, T.Tables[d.target])
    return true
  end
  if c == nil then inp.bad = true; return false end
  local start = inp.p
  local _, _, ok = scan_string(inp, MAX_KEY)
  if not ok then return false end
  local k = inp.t:sub(start + 1, inp.p - 2)
  if peek(inp) ~= ':' then inp.bad = true; return false end
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
  if c == ',' then inp.p = inp.p + 1; c = peek(inp) end
  local bare = c == '}'
  if bare ~= (entry ~= nil) then inp.bad = true; return false end
  if bare then
    inp.p = inp.p + 1
    if entry.open then inp.bad = true; return false end
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
  if not read_keys(T, T.Tables[d.target], inp, node, depth + 1, nil, graph, report) then
    return false
  end
  graph[label] = { node = node, st = d.target, open = false }
  return true
end

local function read_base64(inp, bound, report)
  if peek(inp) ~= '"' then inp.bad = true; return nil end
  inp.p = inp.p + 1
  local out = {}
  local acc, held = 0, 0
  local clamped, bad = false, false
  while true do
    if inp.p > #inp.t then inp.bad = true; return nil end
    local ch = inp.t:sub(inp.p, inp.p)
    inp.p = inp.p + 1
    if ch == '"' then break end
    if ch ~= '=' and not bad then
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

local function read_array(T, fd, inp, holder, key, depth, graph, report)
  if peek(inp) ~= '[' then inp.bad = true; return false end
  inp.p = inp.p + 1
  local out = {}
  if fd.fixed then
    for i = 1, fd.bound do out[i] = zero_elem(T, fd.elem) end
  end
  local bound = fd.list and 0x7fffffff or fd.bound
  local placed = 0
  local eshape = shape_of(fd.elem)
  while true do
    local c = peek(inp)
    local vs = value_shape(inp)
    if c == ']' then inp.p = inp.p + 1; break end
    if c == nil then inp.bad = true; return false end
    if placed >= bound then
      report.clamped = report.clamped + 1
      if not skip_value(inp, depth + 1, graph) then return false end
    elseif fd.elem.shape == "pointer" and vs == 'z' then
      literal(inp, "null")
      placed = placed + 1
      out[placed] = false
    elseif vs ~= eshape then
      report.kindMismatch = report.kindMismatch + 1
      if not skip_value(inp, depth + 1, graph) then return false end
      placed = placed + 1
      out[placed] = zero_elem(T, fd.elem)
    else
      placed = placed + 1
      if out[placed] == nil then out[placed] = zero_elem(T, fd.elem) end
      if not read_value(T, fd.elem, inp, out, placed, depth + 1, graph, report, true) then
        return false
      end
    end
    c = peek(inp)
    if c == ',' then
      inp.p = inp.p + 1
    elseif c == ']' then
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

local function read_map(T, fd, inp, holder, key, depth, graph, report)
  if peek(inp) ~= '{' then inp.bad = true; return false end
  inp.p = inp.p + 1
  local out, seen = {}, {}
  local kf = fd.key_field
  local string_key = kf.shape == "string"
  while true do
    local c = peek(inp)
    if c == '}' then inp.p = inp.p + 1; break end
    if c == nil then inp.bad = true; return false end
    local start = inp.p
    local _, clamped, ok = scan_string(inp, MAX_KEY)
    if not ok then return false end
    local kraw = inp.t:sub(start + 1, inp.p - 2)
    if peek(inp) ~= ':' then inp.bad = true; return false end
    inp.p = inp.p + 1
    local kval, skip = nil, false
    if clamped then
      if string_key then report.clamped = report.clamped + 1
      else report.kindMismatch = report.kindMismatch + 1 end
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
    if not skip and value_shape(inp) == 'z' and (vf.shape == "pointer"
        or vf.shape == "string_blob" or vf.shape == "bytes_blob") then
      null_ref = true
    elseif not skip and value_shape(inp) ~= shape_of(vf) then
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
      if not read_value(T, fd.value_field, inp, tmp, "v", depth + 1, graph, report, true) then
        return false
      end
      out[kval] = tmp.v
    end
    c = peek(inp)
    if c == ',' then
      inp.p = inp.p + 1
    elseif c == '}' then
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

local function read_keyed(T, fd, inp, holder, key, depth, graph, report)
  if peek(inp) ~= '{' then inp.bad = true; return false end
  inp.p = inp.p + 1
  local out = {}
  for i = 1, fd.bound do out[i] = zero_elem(T, fd.elem) end
  local seen = {}
  local eshape = shape_of(fd.elem)
  while true do
    local c = peek(inp)
    if c == '}' then inp.p = inp.p + 1; break end
    if c == nil then inp.bad = true; return false end
    local start = inp.p
    local _, _, ok = scan_string(inp, MAX_KEY)
    if not ok then return false end
    local name = inp.t:sub(start + 1, inp.p - 2)
    if peek(inp) ~= ':' then inp.bad = true; return false end
    inp.p = inp.p + 1
    local slot = nil
    for i, v in ipairs(fd.keyed) do if v[1] == name then slot = i; break end end
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
        if not read_value(T, fd.elem, inp, out, slot, depth + 1, graph, report, true) then
          return false
        end
      end
    end
    c = peek(inp)
    if c == ',' then
      inp.p = inp.p + 1
    elseif c == '}' then
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

local function read_union(T, u, inp, depth, graph, report)
  if peek(inp) ~= '{' then inp.bad = true; return nil end
  inp.p = inp.p + 1
  if peek(inp) == '}' then inp.p = inp.p + 1; return { tag = 0 } end
  local start = inp.p
  local _, _, ok = scan_string(inp, MAX_KEY)
  if not ok then return nil end
  local name = inp.t:sub(start + 1, inp.p - 2)
  if peek(inp) ~= ':' then inp.bad = true; return nil end
  inp.p = inp.p + 1
  local tag, arm = 0, nil
  for i, a in ipairs(u.arms) do if a.name == name then tag, arm = i, a; break end end
  local out = { tag = 0 }
  if tag == 0 then
    report.unknown = report.unknown + 1
    if not skip_value(inp, depth + 1, graph) then return nil end
  elseif arm.field == nil then
    if value_shape(inp) ~= 'z' then
      report.kindMismatch = report.kindMismatch + 1
      if not skip_value(inp, depth + 1, graph) then return nil end
    else
      literal(inp, "null")
      out.tag = tag
    end
  elseif (arm.field.shape == "pointer" or arm.field.shape == "string_blob"
      or arm.field.shape == "bytes_blob") and value_shape(inp) == 'z' then
    -- §16.2 union row: the arm takes its own row, and a pointer or blob arm
    -- reads `null` as the arm selected with a null reference
    literal(inp, "null")
    out.tag = tag
  elseif value_shape(inp) ~= shape_of(arm.field) then
    report.kindMismatch = report.kindMismatch + 1
    if not skip_value(inp, depth + 1, graph) then return nil end
  else
    out.tag = tag
    if not read_value(T, arm.field, inp, out, arm.name, depth + 1, graph, report, false) then
      return nil
    end
  end
  local c = peek(inp)
  if c == ',' then inp.p = inp.p + 1; c = peek(inp) end
  if c == '}' then
    inp.p = inp.p + 1
    return out
  end
  inp.bad = true
  return nil
end

read_value = function(T, d, inp, holder, key, depth, graph, report, elem)
  local s = d.shape
  if s == "pointer" then
    return read_pointer(T, d, inp, holder, key, depth, graph, report)
  elseif s == "string" then
    local out, clamped, ok = scan_string(inp, d.bound)
    if not ok then return false end
    if clamped then report.clamped = report.clamped + 1 end
    holder[key] = out
    return true
  elseif s == "wstring" then
    local out, clamped, ok = scan_wstring(inp, d.bound)
    if not ok then return false end
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
    local out, _, ok = scan_string(inp, -1)
    if not ok then return false end
    holder[key] = { value = out }
    return true
  elseif s == "bytes_blob" then
    -- §16.2 `*bytes`: a body that is not base64 is kind_mismatch, the
    -- reference left null; a decoded body is boxed as the `*string` row is.
    local before = report.kindMismatch
    local out = read_base64(inp, -1, report)
    if out == nil then return false end
    if report.kindMismatch ~= before then holder[key] = elem and false or nil
    else holder[key] = { value = out == false and "" or out } end
    return true
  elseif s == "array" then
    if d.keyed ~= nil then return read_keyed(T, d, inp, holder, key, depth, graph, report) end
    return read_array(T, d, inp, holder, key, depth, graph, report)
  elseif s == "map" then
    return read_map(T, d, inp, holder, key, depth, graph, report)
  elseif s == "table" then
    local node = fresh(T, T.Tables[d.target])
    holder[key] = node
    return read_table(T, T.Tables[d.target], inp, node, depth + 1, graph, report)
  elseif s == "union" then
    local u = T.Unions[d.target]
    if u == nil then inp.bad = true; return false end
    local out = read_union(T, u, inp, depth, graph, report)
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
      if v[1] == name then holder[key] = name; return true end
    end
    holder[key] = elem and false or nil
    report.unknown = report.unknown + 1
    return true
  elseif s == "flags" then
    if peek(inp) ~= '[' then inp.bad = true; return false end
    inp.p = inp.p + 1
    local bits = 0
    while true do
      local c = peek(inp)
      if c == ']' then inp.p = inp.p + 1; break end
      if c == nil then inp.bad = true; return false end
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
          if v == nm then bits = bits | (1 << (i - 1)); found = true; break end
        end
        if not found then report.unknown = report.unknown + 1 end
      end
      c = peek(inp)
      if c == ',' then inp.p = inp.p + 1
      elseif c == ']' then inp.p = inp.p + 1; break
      else inp.bad = true; return false end
    end
    holder[key] = bits
    return true
  elseif s == "bool" then
    local c = peek(inp)
    if c == 't' then
      if not literal(inp, "true") then return false end
      holder[key] = true
      return true
    end
    if not literal(inp, "false") then return false end
    holder[key] = false
    return true
  elseif s == "float" then
    local token = scan_number(inp)
    if token == nil then inp.bad = true; return false end
    local x = tonumber(token)
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
          if d0 == "" then below = true
          elseif #d0 < #mid then below = true
          elseif #d0 == #mid and d0 < mid then below = true end
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
  elseif s == "int" then
    if d.timestamp then
      local text, _, ok = scan_string(inp, -1)
      if not ok then return false end
      local ns = parse_timestamp(text)
      if ns == nil then
        report.malformed = true
        inp.bad = true
        return false
      end
      holder[key] = ns
      return true
    end
    local token = scan_number(inp)
    if token == nil then inp.bad = true; return false end
    local integral = not (token:find("[%.eE]") ~= nil)
    local n = interpret_integer(token, integral)
    local v = place_integer(d, n, report)
    if v ~= nil then holder[key] = v end
    return true
  end
  inp.bad = true
  return false
end

parse_timestamp = function(text)
  local y, m, d, hh, mm, ss, rest = text:match(
    "^(%d%d%d%d)%-(%d%d)%-(%d%d)[Tt](%d%d):(%d%d):(%d%d)(.*)$")
  if y == nil then return nil end
  y, m, d, hh, mm, ss = tonumber(y), tonumber(m), tonumber(d),
    tonumber(hh), tonumber(mm), tonumber(ss)
  local frac, tail = 0, rest
  if rest:sub(1, 1) == "." then
    local digits = rest:match("^%.(%d+)")
    if digits == nil then return nil end
    tail = rest:sub(#digits + 2)
    digits = (digits .. "000000000"):sub(1, 9)
    frac = tonumber(digits)
  end
  local offset = 0
  if tail ~= "Z" and tail ~= "z" then
    local sign, oh, om = tail:match("^([+-])(%d%d):(%d%d)$")
    if sign == nil then return nil end
    offset = (tonumber(oh) * 60 + tonumber(om)) * 60
    if sign == "-" then offset = -offset end
  end
  -- days_from_civil (Hinnant), the inverse of civil_from_days below
  local yy = y - (m <= 2 and 1 or 0)
  local era = (yy >= 0 and yy or yy - 399) // 400
  local yoe = yy - era * 400
  local doy = (153 * (m + (m > 2 and -3 or 9)) + 2) // 5 + d - 1
  local doe = yoe * 365 + yoe // 4 - yoe // 100 + doy
  local days = era * 146097 + doe - 719468
  local secs = days * 86400 + hh * 3600 + mm * 60 + ss - offset
  return secs * 1000000000 + frac
end

-- ONE TEXT: the root's object and nothing after it (§16.1)
function J.FromJson(T, name, text, report)
  local desc = T.Tables[name]
  if desc == nil then report.malformed = true; return nil end
  local inp = new_in(text)
  local graph = nil
  if desc.variable then graph = {} end
  local v = fresh(T, desc)
  local ok = read_table(T, desc, inp, v, 0, graph, report)
  if ok then
    space(inp)
    if inp.p ~= #inp.t + 1 then inp.bad = true end
  end
  if inp.bad or not ok then report.malformed = true end
  return v
end

----------------------------------------------------------------------------
-- the write walk (§16.1, §16.3, §16.7)
----------------------------------------------------------------------------

local function W() return { parts = {} } end
local function raw(w, s) w.parts[#w.parts + 1] = s end
local function put(w, c) w.parts[#w.parts + 1] = c end
local function line(w, depth)
  w.parts[#w.parts + 1] = "\n" .. string.rep("  ", depth)
end

local function write_string(w, s)
  local hex = "0123456789abcdef"
  put(w, '"')
  local i = 1
  while i <= #s do
    local c = s:byte(i)
    if c == 34 then raw(w, '\\"'); i = i + 1
    elseif c == 92 then raw(w, '\\\\'); i = i + 1
    elseif c == 8 then raw(w, '\\b'); i = i + 1
    elseif c == 12 then raw(w, '\\f'); i = i + 1
    elseif c == 10 then raw(w, '\\n'); i = i + 1
    elseif c == 13 then raw(w, '\\r'); i = i + 1
    elseif c == 9 then raw(w, '\\t'); i = i + 1
    elseif c < 0x20 then
      raw(w, "\\u00" .. hex:sub((c >> 4) + 1, (c >> 4) + 1) .. hex:sub((c & 0xf) + 1, (c & 0xf) + 1))
      i = i + 1
    elseif c < 0x80 then
      put(w, string.char(c))
      i = i + 1
    else
      local width = utf8_seq(s, i)
      if width < 0 then raw(w, utf8_encode(0xfffd)); i = i + 1
      else raw(w, s:sub(i, i + width - 1)); i = i + width end
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
    if code == 34 then raw(w, '\\"')
    elseif code == 92 then raw(w, '\\\\')
    elseif code == 8 then raw(w, '\\b')
    elseif code == 12 then raw(w, '\\f')
    elseif code == 10 then raw(w, '\\n')
    elseif code == 13 then raw(w, '\\r')
    elseif code == 9 then raw(w, '\\t')
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
    raw(w, BASE64:sub(((t >> 18) & 63) + 1, ((t >> 18) & 63) + 1)
      .. BASE64:sub(((t >> 12) & 63) + 1, ((t >> 12) & 63) + 1)
      .. BASE64:sub(((t >> 6) & 63) + 1, ((t >> 6) & 63) + 1)
      .. BASE64:sub((t & 63) + 1, (t & 63) + 1))
    i = i + 3
  end
  local left = #data - i + 1
  if left > 0 then
    local a = data:byte(i)
    local b = left == 2 and data:byte(i + 1) or 0
    local t = (a << 16) | (b << 8)
    raw(w, BASE64:sub(((t >> 18) & 63) + 1, ((t >> 18) & 63) + 1)
      .. BASE64:sub(((t >> 12) & 63) + 1, ((t >> 12) & 63) + 1)
      .. (left == 2 and BASE64:sub(((t >> 6) & 63) + 1, ((t >> 6) & 63) + 1) or "=")
      .. "=")
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
    while r > 0 do r = r - 1; d = d + 1 end
    digits[#digits + 1] = string.char(48 + d)
    q = nq
  until q == 0
  local out = {}
  for i = #digits, 1, -1 do out[#out + 1] = digits[i] end
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
  raw(w, string.format('"%04d-%02d-%02dT%02d:%02d:%02d', y, m, d,
    sec // 3600, (sec % 3600) // 60, sec % 60))
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
local function bytes_less(a, b)
  local n = math.min(#a, #b)
  local i = 1
  while i <= n do
    local j = math.min(i + 255, n)
    local ca, cb = a:sub(i, j), b:sub(i, j)
    if ca ~= cb then
      for k = 1, j - i + 1 do
        local x, y = ca:byte(k), cb:byte(k)
        if x ~= y then return x < y end
      end
    end
    i = j + 1
  end
  return #a < #b
end

local function map_entries(cache, fd, x)
  if x == nil then return {} end
  local got = cache[x]
  if got ~= nil then return got end
  local kf = fd.key_field
  local keys = {}
  for key in pairs(x) do keys[#keys + 1] = key end
  table.sort(keys, function(a, b)
    if kf.shape == "string" then return bytes_less(a, b) end
    if kf.signed then return a < b end
    return math.ult(a, b)
  end)
  got = {}
  for i, key in ipairs(keys) do got[i] = key end
  cache[x] = got
  return got
end

local function count_of(fd, v)
  local s = fd.shape
  if s == "bytes" then return #v end
  if fd.list then return #v end
  if fd.fixed then return fd.bound or 0 end
  return math.min(#v, fd.bound or 0)
end

-- A GUARDED FIELD RIDES ONLY WHEN ITS GUARD HOLDS (docs/SPEC-TABLES.md §3: a
-- field under a false guard is elided whatever its storage holds; §3.1: it is
-- not an edge, so its target takes no node). fd.guard is the conjunction of
-- the field's `if` branch conditions, each a bool field of the same body and
-- the value it must hold (luatable.go emitDescriptor; tabletext.Guards); a
-- field with no guard always rides. A bool not set reads false.
local function guard_holds(fd, v)
  local g = fd.guard
  if g == nil then return true end
  for _, t in ipairs(g) do
    if (v[t.field] == true) ~= t.want then return false end
  end
  return true
end

-- THE NUMBERING PASS (§16.7): how many slots name each node, and whether the
-- graph has a cycle, which the text refuses as the wire does (§3.1).
local edge_value, edge_union, edges
edge_value = function(T, fd, val, holder, name, out, cache)
  local s = fd.shape
  if s == "string_blob" or s == "bytes_blob" then
    -- a blob is a node too (§3.1): counted by its box, never descended
    if val ~= nil and val ~= false then out[#out + 1] = { blob = true, val = val } end
  elseif s == "pointer" then
    if val ~= nil and val ~= false then out[#out + 1] = { target = fd.target, val = val } end
  elseif s == "table" then
    local d = T.Tables[fd.target]
    if d then edges(T, d, val, out, cache) end
  elseif s == "array" then
    local n = count_of(fd, val)
    for i = 1, n do edge_value(T, fd.elem, val[i], val, i, out, cache) end
  elseif s == "union" then
    edge_union(T, T.Unions[fd.target], val, out, cache)
  elseif s == "map" then
    local d = T.Tables[fd.entry]
    if d then
      for _, key in ipairs(map_entries(cache, fd, val)) do
        local tmp = { [fd.key_field.name] = key, [fd.value_field.name] = val[key] }
        edges(T, d, tmp, out, cache)
        -- the value field's own pointer is reached through the entry above
      end
    end
  end
end

edge_union = function(T, u, val, out, cache)
  local tag = val and val.tag or 0
  if tag == 0 or u == nil then return end
  local arm = u.arms[tag]
  if arm == nil or arm.field == nil then return end
  edge_value(T, arm.field, val[arm.name], val, arm.name, out, cache)
end

edges = function(T, desc, v, out, cache)
  for _, fd in ipairs(desc.fields) do
    if guard_holds(fd, v) and not (fd.optional and not v[fd.name .. "_present"]) then
      edge_value(T, fd, v[fd.name], v, fd.name, out, cache)
    end
  end
  return out
end

local function count_nodes(T, desc, root)
  local g = { count = {}, labels = {}, next = 0 }
  local open = { [root] = true }
  local cache = {}
  local stack = { { v = root, edges = edges(T, desc, root, {}, cache), i = 0 } }
  while #stack > 0 do
    local top = stack[#stack]
    if top.i >= #top.edges then
      open[top.v] = nil
      stack[#stack] = nil
    else
      local e = top.edges[top.i + 1]
      top.i = top.i + 1
      if not e.blob and open[e.val] then return nil end
      local n = (g.count[e.val] or 0) + 1
      g.count[e.val] = n
      if n == 1 and not e.blob then
        local d = T.Tables[e.target]
        if d == nil then return nil end
        open[e.val] = true
        stack[#stack + 1] = { v = e.val, edges = edges(T, d, e.val, {}, cache), i = 0 }
      end
    end
  end
  return g
end

local write_value, write_table, write_pointer, write_field, write_fields

write_fields = function(T, desc, v, depth, g, w, any)
  for _, fd in ipairs(desc.fields) do
    if guard_holds(fd, v) and not (fd.optional and not v[fd.name .. "_present"]) then
      if not any then put(w, '{') else put(w, ',') end
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
  local ok, any = write_fields(T, desc, v, depth, g, w, false)
  if not ok then return false end
  if not any then raw(w, "{}"); return true end
  line(w, depth)
  put(w, '}')
  return true
end

write_pointer = function(T, target, node, depth, g, w)
  if node == nil or node == false then raw(w, "null"); return true end
  local count = g.count[node] or 0
  local desc = T.Tables[target]
  if count <= 1 then return write_table(T, desc, node, depth, g, w) end
  if depth > MAX_DEPTH then return false end
  local label = g.labels[node]
  if label ~= nil then
    put(w, '{')
    line(w, depth + 1)
    raw(w, '"&node": ' .. string.format("%d", label))
    line(w, depth)
    put(w, '}')
    return true
  end
  g.next = g.next + 1
  label = g.next
  g.labels[node] = label
  put(w, '{')
  line(w, depth + 1)
  raw(w, '"&node": ' .. string.format("%d", label))
  local before = #w.parts
  local ok = write_fields(T, desc, node, depth, g, w, true)
  if not ok then return false end
  if #w.parts == before then return false end
  line(w, depth)
  put(w, '}')
  return true
end

local function write_union(T, u, val, depth, g, w)
  local tag = val and val.tag or 0
  if tag == 0 then raw(w, "{}"); return true end
  local arm = u and u.arms[tag]
  if arm == nil then return false end
  put(w, '{')
  line(w, depth + 1)
  write_string(w, arm.name)
  raw(w, ": ")
  if arm.field == nil then
    raw(w, "null")
  else
    if not write_value(T, arm.field, val[arm.name], depth + 1, g, w, false) then return false end
  end
  line(w, depth)
  put(w, '}')
  return true
end

local function write_array(T, fd, val, depth, g, w)
  local n = count_of(fd, val)
  if n == 0 then raw(w, "[]"); return true end
  put(w, '[')
  for i = 1, n do
    if i > 1 then put(w, ',') end
    line(w, depth + 1)
    local x = val[i]
    if x == nil then x = zero_elem(T, fd.elem) end
    if not write_value(T, fd.elem, x, depth + 1, g, w, true) then return false end
  end
  line(w, depth)
  put(w, ']')
  return true
end

local function write_keyed(T, fd, val, depth, g, w)
  local n = fd.bound or 0
  if n == 0 then raw(w, "{}"); return true end
  put(w, '{')
  for slot = 1, n do
    if slot > 1 then put(w, ',') end
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
  put(w, '}')
  return true
end

local function write_map(T, fd, val, depth, g, w)
  local keys = map_entries({}, fd, val)
  if #keys == 0 then raw(w, "{}"); return true end
  local kf = fd.key_field
  put(w, '{')
  for i, key in ipairs(keys) do
    if i > 1 then put(w, ',') end
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
  put(w, '}')
  return true
end

write_value = function(T, d, val, depth, g, w, elem)
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
    if val == nil or val == false then raw(w, "null"); return true end
    if (g.count[val] or 0) > 1 then return false end
    local body = type(val) == "table" and val.value or val
    if s == "string_blob" then write_string(w, body) else write_base64(w, body) end
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
    if val == nil or val == false then write_string(w, "None"); return true end
    for _, v in ipairs(d.variants) do
      if v[1] == val then write_string(w, val); return true end
    end
    return false
  elseif s == "flags" then
    local bits = val or 0
    if bits == 0 then raw(w, "[]"); return true end
    local seen = 0
    put(w, '[')
    local first = true
    local names = d.variants or {}
    for i, name in ipairs(names) do
      if (bits & (1 << (i - 1))) ~= 0 then
        if not first then put(w, ',') end
        first = false
        line(w, depth + 1)
        write_string(w, name)
        seen = seen | (1 << (i - 1))
      end
    end
    if (bits & ~seen) ~= 0 then return false end
    line(w, depth)
    put(w, ']')
    return true
  elseif s == "bool" then
    raw(w, val and "true" or "false")
    return true
  elseif s == "float" then
    local text = float_text(val, d.f32)
    if text == nil then return false end
    raw(w, text)
    return true
  elseif s == "int" then
    if d.timestamp then write_timestamp(w, val); return true end
    if d.signed then raw(w, string.format("%d", val))
    else raw(w, u64_text(val)) end
    return true
  end
  return false
end

write_field = function(T, fd, v, depth, g, w)
  if fd.shape == "pointer" then
    return write_pointer(T, fd.target, v[fd.name], depth, g, w)
  end
  return write_value(T, fd, v[fd.name], depth, g, w, false)
end

-- ONE INSTANCE as ONE text (§16.1): a trailing newline belongs to the form.
function J.ToJson(T, name, v)
  local desc = T.Tables[name]
  if desc == nil or v == nil then return nil end
  local g = count_nodes(T, desc, v)
  if g == nil then return nil end
  local w = W()
  if not write_table(T, desc, v, 0, g, w) then return nil end
  w.parts[#w.parts + 1] = "\n"
  return table.concat(w.parts)
end

return J
