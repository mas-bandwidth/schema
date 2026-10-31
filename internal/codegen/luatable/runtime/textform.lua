-- textform.lua — the Lua table runtime's HAND-EDITABLE TEXT FORM read
-- (docs/SPEC-TABLES.md §16.9.1, §16.9.2, §16.9.4). Emitted beside the unit's
-- <Package>Tables.lua by internal/codegen/luatable, which embeds runtime/*.lua
-- by glob.
--
-- The surface is the restricted s-expression grammar of §16.9.1, resolved
-- STRICTLY against the table descriptor, then landed through the JSON walk of
-- the same value (§16.9.2): every place JSON would clamp, skip or last-wins is
-- refused here by its §16.9.4 name before that walk runs. The document is not
-- retained; this card is the read only.
local J = require("json")

local TF = {}

local MAX_DEPTH = 128

local function fail(name, pos)
  error({ name = name, offset = (pos or 1) - 1 }, 0)
end

----------------------------------------------------------------------------
-- UTF-8 (§16.9.1: invalid UTF-8 is text_encoding)
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

-- the length of one WELL-FORMED UTF-8 sequence at s[i], or -1
local function utf8_seq(s, i)
  local c = s:byte(i)
  if c == nil then return -1 end
  if c < 0x80 then return 1 end
  local n, lo, hi
  if c >= 0xc2 and c <= 0xdf then
    n, lo, hi = 2, 0x80, 0xbf
  elseif c == 0xe0 then
    n, lo, hi = 3, 0xa0, 0xbf
  elseif c >= 0xe1 and c <= 0xec then
    n, lo, hi = 3, 0x80, 0xbf
  elseif c == 0xed then
    n, lo, hi = 3, 0x80, 0x9f
  elseif c >= 0xee and c <= 0xef then
    n, lo, hi = 3, 0x80, 0xbf
  elseif c == 0xf0 then
    n, lo, hi = 4, 0x90, 0xbf
  elseif c >= 0xf1 and c <= 0xf3 then
    n, lo, hi = 4, 0x80, 0xbf
  elseif c == 0xf4 then
    n, lo, hi = 4, 0x80, 0x8f
  else
    return -1
  end
  if i + n - 1 > #s then return -1 end
  local b = s:byte(i + 1)
  if b < lo or b > hi then return -1 end
  for k = 2, n - 1 do
    b = s:byte(i + k)
    if b < 0x80 or b > 0xbf then return -1 end
  end
  return n
end

local function utf8_decode(s)
  local c = s:byte(1)
  if c < 0x80 then return c end
  local code, n
  if c < 0xe0 then
    code, n = c & 0x1f, 2
  elseif c < 0xf0 then
    code, n = c & 0x0f, 3
  else
    code, n = c & 0x07, 4
  end
  for i = 2, n do
    code = (code << 6) | (s:byte(i) & 0x3f)
  end
  return code
end

----------------------------------------------------------------------------
-- the parser (§16.9.1): gap, comments, atoms, strings and lists
----------------------------------------------------------------------------

local function limits_of(l)
  local d = { bytes = 65536, tokens = 8192, nodes = 4096, allocation = 65536, depth = MAX_DEPTH }
  if l == nil then return d end
  if l.bytes and l.bytes > 0 then d.bytes = l.bytes end
  if l.tokens and l.tokens > 0 then d.tokens = l.tokens end
  if l.nodes and l.nodes > 0 then d.nodes = l.nodes end
  if l.allocation and l.allocation > 0 then d.allocation = l.allocation end
  if l.depth and l.depth > 0 and l.depth <= MAX_DEPTH then d.depth = l.depth end
  return d
end

local function skip_gap(st)
  local t = st.t
  while st.p <= #t do
    local c = t:byte(st.p)
    if c == 32 or c == 9 or c == 10 then
      st.p = st.p + 1
    elseif c == 13 then
      if t:byte(st.p + 1) ~= 10 then fail("text_syntax", st.p) end
      st.p = st.p + 2
    elseif c == 59 then
      st.p = st.p + 1
      while st.p <= #t do
        local b = t:byte(st.p)
        if b == 10 then break end
        if b == 13 then break end
        st.p = st.p + 1
      end
    else
      return
    end
  end
end

local function hex4(st)
  local s = st.t:sub(st.p, st.p + 3)
  if #s ~= 4 or not s:match("^%x%x%x%x$") then fail("text_encoding", st.p) end
  st.p = st.p + 4
  return tonumber(s, 16)
end

-- ONE string body unit; returns the decoded bytes (§16.9.1: JSON escapes,
-- raw newlines, invalid UTF-8 and unpaired surrogates are text_encoding)
local function scan_string(st)
  local t = st.t
  local start = st.p
  st.p = st.p + 1
  local out = {}
  while true do
    if st.p > #t then fail("text_unterminated", start) end
    local c = t:byte(st.p)
    if c == 34 then
      st.p = st.p + 1
      break
    elseif c == 92 then
      st.p = st.p + 1
      if st.p > #t then fail("text_unterminated", start) end
      local e = t:sub(st.p, st.p)
      st.p = st.p + 1
      if e == '"' then
        out[#out + 1] = '"'
      elseif e == "\\" then
        out[#out + 1] = "\\"
      elseif e == "/" then
        out[#out + 1] = "/"
      elseif e == "b" then
        out[#out + 1] = "\b"
      elseif e == "f" then
        out[#out + 1] = "\f"
      elseif e == "n" then
        out[#out + 1] = "\n"
      elseif e == "r" then
        out[#out + 1] = "\r"
      elseif e == "t" then
        out[#out + 1] = "\t"
      elseif e == "u" then
        local high = hex4(st)
        local code = high
        if high >= 0xd800 and high <= 0xdbff then
          if t:sub(st.p, st.p + 1) ~= "\\u" then fail("text_encoding", st.p) end
          st.p = st.p + 2
          local low = hex4(st)
          if low < 0xdc00 or low > 0xdfff then fail("text_encoding", st.p) end
          code = 0x10000 + ((high - 0xd800) << 10) + (low - 0xdc00)
        elseif high >= 0xdc00 and high <= 0xdfff then
          fail("text_encoding", st.p)
        end
        out[#out + 1] = utf8_encode(code)
      else
        fail("text_encoding", st.p)
      end
    elseif c < 0x20 then
      fail("text_encoding", st.p)
    elseif c < 0x80 then
      out[#out + 1] = string.char(c)
      st.p = st.p + 1
    else
      local n = utf8_seq(t, st.p)
      if n < 0 then fail("text_encoding", st.p) end
      out[#out + 1] = t:sub(st.p, st.p + n - 1)
      st.p = st.p + n
    end
  end
  local s = table.concat(out)
  st.allocation = st.allocation + #s
  if st.allocation > st.limits.allocation then fail("text_limit", start) end
  return s
end

local keyword_pattern = "^:[A-Za-z_][A-Za-z0-9_/%.%-]*$"

-- RFC 8259's number grammar, written without alternation (Lua patterns have
-- none): no leading zero, a fraction only with digits, an optional exponent
local function is_number(tok)
  local t = tok
  if t:sub(1, 1) == "-" then t = t:sub(2) end
  local intpart, rest = t:match("^(%d+)(.*)$")
  if intpart == nil then return false end
  if #intpart > 1 and intpart:sub(1, 1) == "0" then return false end
  local frac
  if rest:sub(1, 1) == "." then
    frac, rest = rest:match("^%.(%d+)(.*)$")
    if frac == nil then return false end
  end
  if rest == "" then return true end
  if rest:sub(1, 1) == "e" or rest:sub(1, 1) == "E" then
    local exp = rest:match("^[eE]([%+%-]?%d+)$")
    return exp ~= nil
  end
  return false
end

local function scan_atom(st)
  local t = st.t
  local start = st.p
  while st.p <= #t do
    local c = t:byte(st.p)
    if c == 40 or c == 41 or c == 34 or c == 59 or c == 32 or c == 9 or c == 10 or c == 13 then break end
    st.p = st.p + 1
  end
  local tok = t:sub(start, st.p - 1)
  if tok == "" then fail("text_syntax", start) end
  if tok == "true" then return { k = "bool", v = true, pos = start } end
  if tok == "false" then return { k = "bool", v = false, pos = start } end
  if tok == "null" then return { k = "null", pos = start } end
  if tok:match(keyword_pattern) then return { k = "kw", v = tok:sub(2), pos = start } end
  if is_number(tok) then return { k = "num", v = tok, pos = start } end
  fail("text_syntax", start)
end

local parse_value

local function scan_list(st, depth)
  if depth > st.limits.depth then fail("text_limit", st.p) end
  st.nodes = st.nodes + 1
  if st.nodes > st.limits.nodes then fail("text_limit", st.p) end
  st.tokens = st.tokens + 1
  if st.tokens > st.limits.tokens then fail("text_limit", st.p) end
  local open = st.p
  st.p = st.p + 1
  local items = {}
  while true do
    skip_gap(st)
    if st.p > #st.t then fail("text_unterminated", open) end
    if st.t:byte(st.p) == 41 then
      st.tokens = st.tokens + 1
      if st.tokens > st.limits.tokens then fail("text_limit", st.p) end
      st.p = st.p + 1
      return { k = "list", items = items, pos = open }
    end
    items[#items + 1] = parse_value(st, depth)
  end
end

parse_value = function(st, depth)
  skip_gap(st)
  if st.p > #st.t then fail("text_unterminated", st.p) end
  local c = st.t:byte(st.p)
  if c == 40 then return scan_list(st, depth + 1) end
  if c == 34 then
    st.tokens = st.tokens + 1
    if st.tokens > st.limits.tokens then fail("text_limit", st.p) end
    local start = st.p
    return { k = "str", v = scan_string(st), pos = start }
  end
  st.tokens = st.tokens + 1
  if st.tokens > st.limits.tokens then fail("text_limit", st.p) end
  return scan_atom(st)
end

----------------------------------------------------------------------------
-- JSON spelling of an accepted value (§16.9.2 lands it through the §16 walk)
----------------------------------------------------------------------------

local function quote(s)
  local out = { '"' }
  for i = 1, #s do
    local c = s:byte(i)
    if c == 34 then
      out[#out + 1] = '\\"'
    elseif c == 92 then
      out[#out + 1] = "\\\\"
    elseif c == 8 then
      out[#out + 1] = "\\b"
    elseif c == 12 then
      out[#out + 1] = "\\f"
    elseif c == 10 then
      out[#out + 1] = "\\n"
    elseif c == 13 then
      out[#out + 1] = "\\r"
    elseif c == 9 then
      out[#out + 1] = "\\t"
    elseif c < 0x20 then
      out[#out + 1] = string.format("\\u%04x", c)
    else
      out[#out + 1] = s:sub(i, i)
    end
  end
  out[#out + 1] = '"'
  return table.concat(out)
end

local BASE64 = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/"
local b64val = {}
for i = 1, #BASE64 do
  b64val[BASE64:sub(i, i)] = i - 1
end

-- padded standard base64 (§16.9.2 bytes row): the decoded length, or nil for
-- a bad alphabet, missing pad or nonzero unused pad bits
local function base64_len(s)
  local n = #s
  if n % 4 ~= 0 then return nil end
  if n == 0 then return 0 end
  local pad = 0
  if s:sub(n, n) == "=" then
    pad = 1
    if s:sub(n - 1, n - 1) == "=" then pad = 2 end
  end
  local out = 0
  for i = 1, n, 4 do
    local a, b = b64val[s:sub(i, i)], b64val[s:sub(i + 1, i + 1)]
    if a == nil or b == nil then return nil end
    local c, d = s:sub(i + 2, i + 2), s:sub(i + 3, i + 3)
    local last = i + 3 == n
    if c == "=" or d == "=" then
      if not last then return nil end
      if c == "=" then
        if d ~= "=" or pad ~= 2 then return nil end
        if b % 16 ~= 0 then return nil end
        out = out + 1
      else
        if d ~= "=" or pad ~= 1 then return nil end
        local cv = b64val[c]
        if cv == nil or cv % 4 ~= 0 then return nil end
        out = out + 2
      end
    else
      local cv, dv = b64val[c], b64val[d]
      if cv == nil or dv == nil then return nil end
      out = out + 3
    end
  end
  return out
end

-- ONE RFC 3339 instant in §16.2's UTC nanosecond domain, or nil
local function timestamp_ns(text)
  local y, m, d, hh, mm, ss, rest = text:match("^(%d%d%d%d)%-(%d%d)%-(%d%d)[Tt](%d%d):(%d%d):(%d%d)(.*)$")
  if y == nil then return nil end
  y, m, d, hh, mm, ss = tonumber(y), tonumber(m), tonumber(d), tonumber(hh), tonumber(mm), tonumber(ss)
  if y < 1 or y > 9999 or m < 1 or m > 12 or d < 1 or d > 31 or hh > 23 or mm > 59 or ss > 60 then return nil end
  local frac, tail = 0, rest
  if rest:sub(1, 1) == "." then
    local digits = rest:match("^%.(%d+)")
    if digits == nil then return nil end
    tail = rest:sub(#digits + 2)
    frac = tonumber((digits .. "000000000"):sub(1, 9))
  end
  local offset = 0
  if tail ~= "Z" and tail ~= "z" then
    local sign, oh, om = tail:match("^([+-])(%d%d):(%d%d)$")
    if sign == nil then return nil end
    oh, om = tonumber(oh), tonumber(om)
    if oh > 23 or om > 59 then return nil end
    offset = (oh * 60 + om) * 60
    if sign == "-" then offset = -offset end
  end
  local yy = y - (m <= 2 and 1 or 0)
  local era = (yy >= 0 and yy or yy - 399) // 400
  local yoe = yy - era * 400
  local doy = (153 * (m + (m > 2 and -3 or 9)) + 2) // 5 + d - 1
  local doe = yoe * 365 + yoe // 4 - yoe // 100 + doy
  local days = era * 146097 + doe - 719468
  local secs = days * 86400 + hh * 3600 + mm * 60 + ss - offset
  return secs * 1000000000 + frac
end

-- the exact integral value of an RFC 8259 number token as sign + decimal
-- digits, or nil when the token has a nonzero fraction (§16.9.2 integers)
local function integral_digits(token)
  local neg = false
  local t = token
  if t:sub(1, 1) == "-" then
    neg = true
    t = t:sub(2)
  end
  local exp = 0
  local e = t:find("[eE]")
  if e then
    exp = tonumber(t:sub(e + 1))
    t = t:sub(1, e - 1)
  end
  local dot = t:find(".", 1, true)
  local intpart, fracpart
  if dot then
    intpart, fracpart = t:sub(1, dot - 1), t:sub(dot + 1)
  else
    intpart, fracpart = t, ""
  end
  local digits = intpart .. fracpart
  local shift = exp - #fracpart
  if shift < 0 then
    local drop = -shift
    if drop >= #digits then
      if not digits:match("^0*$") then return nil end
      digits = "0"
    else
      local tail = digits:sub(#digits - drop + 1)
      if not tail:match("^0*$") then return nil end
      digits = digits:sub(1, #digits - drop)
    end
  elseif shift > 0 then
    digits = digits .. string.rep("0", shift)
  end
  digits = digits:gsub("^0+", "")
  if digits == "" then
    neg = false
    digits = "0"
  end
  return neg, digits
end

local function digits_less(a, b)
  a = a:gsub("^0+", "")
  b = b:gsub("^0+", "")
  if a == "" then a = "0" end
  if b == "" then b = "0" end
  if #a ~= #b then return #a < #b end
  return a < b
end

local function int_bounds(d)
  if d.lo ~= nil and d.hi ~= nil then return tostring(d.lo), tostring(d.hi) end
  local bits = d.bits
  if bits == nil or bits <= 0 then bits = (d.width or 8) * 8 end
  if d.signed then
    if bits >= 64 then return "-9223372036854775808", "9223372036854775807" end
    local hi = (1 << (bits - 1)) - 1
    return tostring(-hi - 1), tostring(hi)
  end
  if bits >= 64 then return "0", "18446744073709551615" end
  return "0", tostring((1 << bits) - 1)
end

-- §16.9.2 integer rows: exact integral value, kind and declared range checked
local function integer_text(d, node)
  local neg, digits = integral_digits(node.v)
  if digits == nil then fail("text_kind_mismatch", node.pos) end
  local lo, hi = int_bounds(d)
  local below
  if neg then
    below = lo:sub(1, 1) ~= "-" or digits_less(lo:sub(2), digits)
  else
    if lo:sub(1, 1) == "-" then
      below = false
    else
      below = digits_less(digits, lo)
    end
  end
  local above
  if neg then
    if hi:sub(1, 1) ~= "-" then
      above = false
    else
      above = digits_less(hi:sub(2), digits)
    end
  else
    if hi:sub(1, 1) == "-" then
      above = true
    else
      above = digits_less(hi, digits)
    end
  end
  if below or above then fail("text_bound", node.pos) end
  if neg then return "-" .. digits end
  return digits
end

local function utf16_units(s)
  local n, i = 0, 1
  while i <= #s do
    local len = utf8_seq(s, i)
    local c = utf8_decode(s:sub(i, i + len - 1))
    n = n + (c >= 0x10000 and 2 or 1)
    i = i + len
  end
  return n
end

local function key_name(node)
  if node.k == "kw" or node.k == "str" then return node.v end
  fail("text_kind_mismatch", node.pos)
end

local function find_field(desc, key)
  for _, fd in ipairs(desc.fields) do
    if fd.key == key then return fd end
  end
  return nil
end

local function find_field_name(desc, name)
  for _, fd in ipairs(desc.fields) do
    if fd.name == name then return fd end
  end
  return nil
end

local resolve_value

local function resolve_record(T, desc, node, allow_label, labels)
  if node.k ~= "list" then fail("text_kind_mismatch", node.pos) end
  local items = node.items
  if #items % 2 ~= 0 then fail("text_kind_mismatch", node.pos) end
  local present, raw, parts = {}, {}, {}
  local label = nil
  local start = 1
  if #items >= 2 then
    local first = items[1]
    local name = first.k == "str" and first.v or nil
    if name == "&node" then
      if not allow_label then fail("text_reference", first.pos) end
      local lv = items[2]
      if lv.k ~= "num" or not lv.v:match("^%d+$") then fail("text_reference", lv.pos) end
      label = tonumber(lv.v)
      start = 3
      if start > #items then
        if not labels[label] then fail("text_reference", lv.pos) end
        return '{"&node":' .. tostring(label) .. "}"
      end
      if labels[label] then fail("text_reference", lv.pos) end
    end
  end
  local i = start
  while i <= #items do
    local keynode, valnode = items[i], items[i + 1]
    local key = key_name(keynode)
    if key:sub(1, 1) == "&" then fail("text_reference", keynode.pos) end
    local fd = find_field(desc, key)
    if fd == nil then fail("text_unknown_key", keynode.pos) end
    if present[fd.name] then fail("text_duplicate_key", keynode.pos) end
    present[fd.name] = true
    raw[fd.name] = valnode
    parts[#parts + 1] = quote(key) .. ":" .. resolve_value(T, fd, valnode, labels)
    i = i + 2
  end
  local function guard_holds(fd)
    if fd.guard == nil then return true end
    for _, term in ipairs(fd.guard) do
      local n = raw[term.field]
      local val
      if n ~= nil then
        if n.k ~= "bool" then fail("text_kind_mismatch", n.pos) end
        val = n.v
      else
        local g = find_field_name(desc, term.field)
        val = g and g.default
        if type(val) ~= "boolean" then val = false end
      end
      if val ~= term.want then return false end
    end
    return true
  end
  for _, fd in ipairs(desc.fields) do
    if present[fd.name] and not guard_holds(fd) then fail("text_inactive_field", raw[fd.name].pos) end
    if not present[fd.name] and fd.required then fail("text_missing_required", node.pos) end
  end
  if label ~= nil then labels[label] = true end
  if label ~= nil then
    return '{"&node":' .. tostring(label) .. (#parts > 0 and "," .. table.concat(parts, ",") or "") .. "}"
  end
  return "{" .. table.concat(parts, ",") .. "}"
end

local function variants_has(d, name)
  for _, v in ipairs(d.variants or {}) do
    if type(v) == "table" then
      if v[1] == name then return true end
    elseif v == name then
      return true
    end
  end
  return false
end

local function resolve_array(T, d, node, labels)
  if node.k ~= "list" then fail("text_kind_mismatch", node.pos) end
  local items = node.items
  if d.keyed ~= nil then
    if #items % 2 ~= 0 then fail("text_kind_mismatch", node.pos) end
    local parts, seen = {}, {}
    local i = 1
    while i <= #items do
      local name = key_name(items[i])
      local slot
      for k, v in ipairs(d.keyed) do
        if v[1] == name then
          slot = k
          break
        end
      end
      if slot == nil then fail("text_unknown_key", items[i].pos) end
      if seen[slot] then fail("text_duplicate_key", items[i].pos) end
      seen[slot] = true
      parts[#parts + 1] = quote(name) .. ":" .. resolve_value(T, d.elem, items[i + 1], labels)
      i = i + 2
    end
    return "{" .. table.concat(parts, ",") .. "}"
  end
  if d.fixed and #items ~= d.bound then fail("text_bound", node.pos) end
  if not d.fixed and d.bound and d.bound > 0 and #items > d.bound then fail("text_bound", node.pos) end
  local parts = {}
  for idx = 1, #items do
    parts[idx] = resolve_value(T, d.elem, items[idx], labels)
  end
  return "[" .. table.concat(parts, ",") .. "]"
end

local function resolve_map(T, d, node, labels)
  if node.k ~= "list" then fail("text_kind_mismatch", node.pos) end
  local parts, seen = {}, {}
  for _, entry in ipairs(node.items) do
    if entry.k ~= "list" or #entry.items ~= 2 then fail("text_kind_mismatch", entry.pos) end
    local kd, vd = d.key_field, d.value_field
    local knode = entry.items[1]
    local key
    if kd.shape == "string" then
      if knode.k ~= "str" then fail("text_kind_mismatch", knode.pos) end
      key = knode.v
      if kd.bound ~= nil and #key > kd.bound then fail("text_bound", knode.pos) end
    elseif kd.shape == "int" then
      if knode.k ~= "num" then fail("text_kind_mismatch", knode.pos) end
      key = integer_text(kd, knode)
    else
      key = key_name(knode)
    end
    if seen[key] then fail("text_duplicate_key", knode.pos) end
    seen[key] = true
    parts[#parts + 1] = quote(key) .. ":" .. resolve_value(T, vd, entry.items[2], labels)
  end
  return "{" .. table.concat(parts, ",") .. "}"
end

local function resolve_union(T, d, node, labels)
  if node.k ~= "list" then fail("text_kind_mismatch", node.pos) end
  if #node.items == 0 then return "{}" end
  if #node.items ~= 2 then fail("text_kind_mismatch", node.pos) end
  local u = T.Unions[d.target]
  if u == nil then fail("text_kind_mismatch", node.pos) end
  local keynode = node.items[1]
  local name = key_name(keynode)
  local arm
  for _, a in ipairs(u.arms) do
    if a.name == name then
      arm = a
      break
    end
  end
  if arm == nil then fail("text_unknown_key", keynode.pos) end
  local valnode = node.items[2]
  if arm.field == nil then
    if valnode.k ~= "null" then fail("text_kind_mismatch", valnode.pos) end
    return "{" .. quote(name) .. ":null}"
  end
  return "{" .. quote(name) .. ":" .. resolve_value(T, arm.field, valnode, labels) .. "}"
end

resolve_value = function(T, d, node, labels)
  local s = d.shape
  if node.k == "null" then
    if d.optional or s == "pointer" or s == "string_blob" or s == "bytes_blob" then return "null" end
    fail("text_kind_mismatch", node.pos)
  end
  if s == "int" then
    if d.timestamp then
      if node.k ~= "str" then fail("text_kind_mismatch", node.pos) end
      if timestamp_ns(node.v) == nil then fail("text_timestamp", node.pos) end
      return quote(node.v)
    end
    if node.k ~= "num" then fail("text_kind_mismatch", node.pos) end
    return integer_text(d, node)
  elseif s == "float" then
    if node.k ~= "num" then fail("text_kind_mismatch", node.pos) end
    local x = tonumber(node.v)
    if x == nil or x ~= x or x == math.huge or x == -math.huge then fail("text_kind_mismatch", node.pos) end
    if d.f32 then
      local narrow = string.unpack("<f", string.pack("<f", x))
      if narrow == math.huge or narrow == -math.huge then fail("text_bound", node.pos) end
    end
    return node.v
  elseif s == "bool" then
    if node.k ~= "bool" then fail("text_kind_mismatch", node.pos) end
    return node.v and "true" or "false"
  elseif s == "string" then
    if node.k ~= "str" then fail("text_kind_mismatch", node.pos) end
    if d.bound ~= nil and #node.v > d.bound then fail("text_bound", node.pos) end
    return quote(node.v)
  elseif s == "wstring" then
    if node.k ~= "str" then fail("text_kind_mismatch", node.pos) end
    if d.bound ~= nil and utf16_units(node.v) > d.bound then fail("text_bound", node.pos) end
    return quote(node.v)
  elseif s == "bytes" then
    if node.k ~= "str" then fail("text_kind_mismatch", node.pos) end
    local n = base64_len(node.v)
    if n == nil then fail("text_encoding", node.pos) end
    if d.bound ~= nil and n > d.bound then fail("text_bound", node.pos) end
    return quote(node.v)
  elseif s == "string_blob" then
    if node.k ~= "str" then fail("text_kind_mismatch", node.pos) end
    return quote(node.v)
  elseif s == "bytes_blob" then
    if node.k ~= "str" then fail("text_kind_mismatch", node.pos) end
    if base64_len(node.v) == nil then fail("text_encoding", node.pos) end
    return quote(node.v)
  elseif s == "enum" then
    if node.k ~= "kw" and node.k ~= "str" then fail("text_kind_mismatch", node.pos) end
    local name = node.v
    if name ~= "None" and not variants_has(d, name) then fail("text_unknown_variant", node.pos) end
    return quote(name)
  elseif s == "flags" then
    if node.k ~= "list" then fail("text_kind_mismatch", node.pos) end
    local seen, parts = {}, {}
    for _, item in ipairs(node.items) do
      if item.k ~= "kw" and item.k ~= "str" then fail("text_kind_mismatch", item.pos) end
      local name = item.v
      if not variants_has(d, name) then fail("text_unknown_variant", item.pos) end
      if seen[name] then fail("text_duplicate_key", item.pos) end
      seen[name] = true
      parts[#parts + 1] = quote(name)
    end
    return "[" .. table.concat(parts, ",") .. "]"
  elseif s == "table" then
    return resolve_record(T, T.Tables[d.target], node, false, labels)
  elseif s == "pointer" then
    return resolve_record(T, T.Tables[d.target], node, true, labels)
  elseif s == "array" then
    return resolve_array(T, d, node, labels)
  elseif s == "map" then
    return resolve_map(T, d, node, labels)
  elseif s == "union" then
    return resolve_union(T, d, node, labels)
  end
  fail("text_kind_mismatch", node.pos)
end

-- Load(T, name, text [, limits]) reads one hand-editable document strictly
-- (§16.9.1–§16.9.4) and returns its table value, or raises a refusal table
-- whose `name` is the stable §16.9.4 name and whose `offset` is zero-based.
function TF.Load(T, name, text, limits)
  if type(text) ~= "string" then fail("text_kind_mismatch", 1) end
  local desc = T.Tables[name]
  if desc == nil then fail("text_unknown_key", 1) end
  local lim = limits_of(limits)
  if #text > lim.bytes then fail("text_limit", 1) end
  local st = { t = text, p = 1, tokens = 0, nodes = 0, allocation = 0, limits = lim }
  local root = parse_value(st, 0)
  skip_gap(st)
  if st.p <= #text then fail("text_syntax", st.p) end
  if root.k ~= "list" then fail("text_kind_mismatch", root.pos) end
  local js = resolve_record(T, desc, root, false, {})
  local report =
    { malformed = false, refused = 0, unknown = 0, kindMismatch = 0, clamped = 0, widened = 0, duplicate = 0 }
  local v = J.FromJson(T, name, js, report)
  if v == nil or report.malformed then fail("text_syntax", 1) end
  if report.duplicate > 0 then fail("text_duplicate_key", 1) end
  if report.unknown > 0 then fail("text_unknown_key", 1) end
  if report.kindMismatch > 0 then fail("text_kind_mismatch", 1) end
  if report.clamped > 0 then fail("text_bound", 1) end
  if report.widened > 0 then fail("text_bound", 1) end
  return v
end

TF.LoadTextForm = TF.Load

return TF
