--!strict
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

-- one JSON text under the reader's cursor: p is the next byte, bad latches a
-- malformed text (§16.1)
type Input = { t: string, p: number, bad: boolean, zero: boolean?, notutf8: boolean? }

local MAX_DEPTH = 128
local MAX_KEY = 255
local MAX_NUMBER = 511
local BASE64 = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/"

-- A float32's shortest round-trip text is C's %.*g at the fewest digits from
-- 6 up to this cap that read back to the same float32; 9 always does
-- (docs/SPEC-TABLES.md §16.1; internal/tabletext/write.go's writeFloat).
local FLOAT32_DIGITS = 9

-- Luau has no bitwise operators; bit32 is enough for every value below it (a
-- UTF-8 sequence, a base64 triplet, a flags mask) (docs/PORT-LUAU.md).
local band, bor, bnot = bit32.band, bit32.bor, bit32.bnot
local function lshift(a: number, n: number): number return a * 2 ^ n end
local function rshift(a: number, n: number): number return math.floor(a / 2 ^ n) end
local function ult(a: number, b: number): boolean
  if a < 0 then
    if b >= 0 then return true end
    return a < b
  end
  if b < 0 then return false end
  return a < b
end

----------------------------------------------------------------------------
-- UTF-8
----------------------------------------------------------------------------

local function utf8_encode(code: number): string
  if code < 0x80 then
    return string.char(code)
  elseif code < 0x800 then
    return string.char(bor(0xc0, rshift(code, 6)), bor(0x80, band(code, 0x3f)))
  elseif code < 0x10000 then
    return string.char(bor(0xe0, rshift(code, 12)), bor(0x80, band(rshift(code, 6), 0x3f)), bor(0x80, band(code, 0x3f)))
  end
  return string.char(bor(0xf0, rshift(code, 18)), bor(0x80, band(rshift(code, 12), 0x3f)),
    bor(0x80, band(rshift(code, 6), 0x3f)), bor(0x80, band(code, 0x3f)))
end

-- the length of one WELL-FORMED UTF-8 sequence at s[i], or -1 (§16.3)
local function utf8_seq(s: string, i: number): number
  local c = s:byte(i)
  if c == nil then return -1 end
  if c < 0x80 then return 1 end
  local want, code
  if c >= 0xc2 and c <= 0xdf then want, code = 2, band(c, 0x1f)
  elseif c >= 0xe0 and c <= 0xef then want, code = 3, band(c, 0x0f)
  elseif c >= 0xf0 and c <= 0xf4 then want, code = 4, band(c, 0x07)
  else return -1 end
  if i + want - 1 > #s then return -1 end
  for k = 1, want - 1 do
    local b = s:byte(i + k)
    if b < 0x80 or b > 0xbf then return -1 end
    code = bor(lshift(code, 6), band(b, 0x3f))
  end
  if want == 3 and code < 0x800 then return -1 end
  if want == 4 and code < 0x10000 then return -1 end
  if code >= 0xd800 and code <= 0xdfff then return -1 end
  if code > 0x10ffff then return -1 end
  return want
end

local function utf8_decode(s: string): number
  local c = s:byte(1)
  if c < 0x80 then return c end
  local code, at
  if c < 0xe0 then code, at = band(c, 0x1f), 2
  elseif c < 0xf0 then code, at = band(c, 0x0f), 3
  else code, at = band(c, 0x07), 4 end
  for i = 2, at do code = bor(lshift(code, 6), band(s:byte(i), 0x3f)) end
  return code
end

----------------------------------------------------------------------------
-- the reader (§16.2)
----------------------------------------------------------------------------

local function new_in(text: string): Input return { t = text, p = 1, bad = false, zero = false } end

-- THE BYTE-ORDER MARK (docs/SPEC-TABLES.md §16.12): a text whose first three
-- bytes are EF BB BF reads as the text after them (RFC 8259 §8.1). Only the
-- UTF-8 mark, and only at the very start: anywhere else those bytes are not
-- JSON. No writer emits one.
local function byte_order_mark(inp: Input)
  if inp.p == 1 and inp.t:sub(1, 3) == "\xEF\xBB\xBF" then inp.p = 4 end
end

local function space(inp: Input)
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

local function peek(inp: Input): string?
  space(inp)
  if inp.p <= #inp.t then return inp.t:sub(inp.p, inp.p) end
  return nil
end

local function literal(inp: Input, word: string): boolean
  if inp.t:sub(inp.p, inp.p + #word - 1) == word then
    inp.p = inp.p + #word
    return true
  end
  inp.bad = true
  return false
end

local function hex4(inp: Input): number?
  if inp.p + 3 > #inp.t then return nil end
  local s = inp.t:sub(inp.p, inp.p + 3)
  if not s:match("^%x%x%x%x$") then return nil end
  inp.p = inp.p + 4
  return tonumber(s, 16)
end

-- ONE STRING BODY CHARACTER as UTF-8: an escape's code point or one UTF-8
-- sequence read whole, so a clamp lands only between code points (§16.3).
local function scan_unit(inp: Input): (string?, boolean, boolean)
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
          code = 0x10000 + (lshift((high - 0xd800), 10)) + (low - 0xdc00)
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
  -- a raw UTF-8 sequence, consumed whole; bytes that are not a well-formed
  -- sequence spell no code point, and the read REFUSES them by name at every
  -- string site, string_not_utf8, before any clamp (§16.3), never U+FFFD
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
  if utf8_seq(unit, 1) ~= #unit then
    inp.notutf8 = true
    inp.bad = true
    return nil, false, false
  end
  return unit, false, true
end

-- ONE JSON string into bytes, clamped at cap bytes (cap < 0 unbounded).
-- store says the string is bound for TEXT STORAGE, where a U+0000 refuses
-- the read (docs/SPEC-TABLES.md §16.3).
local function scan_string(inp: Input, cap: number, store: boolean?): (string?, boolean, boolean)
  if peek(inp) ~= '"' then inp.bad = true; return nil, false, false end
  inp.p = inp.p + 1
  local out, n, clamped = {}, 0, false
  while true do
    local unit, done, ok = scan_unit(inp)
    if not ok then return nil, false, false end
    if done then break end
    local u = unit :: string -- ok and not done: scan_unit returned a unit
    if store and #u == 1 and u:byte(1) == 0 then
      inp.zero = true
      inp.bad = true
      return nil, false, false
    end
    if clamped or (cap >= 0 and n + #u > cap) then
      clamped = true
    else
      out[#out + 1] = u
      n = n + #u
    end
  end
  return table.concat(out), clamped, true
end

-- ONE JSON string into UTF-16 code units, clamped at cap units (§16.2);
-- store says it is bound for text storage, so a U+0000 refuses (§16.3)
local function scan_wstring(inp: Input, cap: number, store: boolean?): ({ number }?, boolean, boolean)
  if peek(inp) ~= '"' then inp.bad = true; return nil, false, false end
  inp.p = inp.p + 1
  local out, n, clamped = {}, 0, false
  while true do
    local unit, done, ok = scan_unit(inp)
    if not ok then return nil, false, false end
    if done then break end
    local code = utf8_decode(unit :: string) -- ok and not done: a unit
    if store and code == 0 then
      inp.zero = true
      inp.bad = true
      return nil, false, false
    end
    local units
    if code < 0x10000 then
      units = { code }
    else
      local rest = code - 0x10000
      units = { 0xd800 + (rshift(rest, 10)), 0xdc00 + (band(rest, 0x3ff)) }
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
local function scan_number(inp: Input): string?
  space(inp)
  local start = inp.p
  local function digit(): boolean
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

local function scan_label(inp: Input): number?
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

local skip_value: (Input, number, any) -> boolean

-- a skipped OBJECT may still be a pointer's: `&node` opening it takes its
-- label with no node, so a later reference reads null (§16.7)
local function skip_container(inp: Input, close: string, depth: number, graph: any): boolean
  if depth > MAX_DEPTH then inp.bad = true; return false end
  inp.p = inp.p + 1
  local first = true
  while true do
    local c = peek(inp)
    if c == close then inp.p = inp.p + 1; return true end
    if c == nil then inp.bad = true; return false end
    local labelled = false
    if close == '}' then
      local scanned, _, ok = scan_string(inp, MAX_KEY)
      if not ok then return false end
      local key = scanned :: string -- an ok scan returns its string
      if peek(inp) ~= ':' then inp.bad = true; return false end
      inp.p = inp.p + 1
      if key ~= "" and key:sub(1, 1) == "&" and graph ~= nil then
        if not first or key ~= "&node" then inp.bad = true; return false end
        local label = scan_label(inp)
        if label == nil then return false end
        if graph[label] == nil then graph[label] = { node = nil, st = nil, open = false } end
        first = false
        labelled = true
        c = peek(inp)
        if c == close then inp.p = inp.p + 1; return true end
        if c ~= ',' then inp.bad = true; return false end
        inp.p = inp.p + 1
      end
    end
    if not labelled then
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
    end
  end
end

skip_value = function(inp: Input, depth: number, graph: any): boolean
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

-- walk_default is the WALK's declared default for a table (docs/SPEC-TABLES.md
-- §4, §16.2): the shape the reader starts from and the shape the writer reads.
-- It is not the generated PUBLIC constructor value: in the walk a string is its
-- text, not a buffer with a used length beside it, a counted array is its live
-- elements, not a bound-sized table with a Count, and a fixed array is padded to
-- its bound. Starting a text read from the public constructor would carry the
-- buffer's capacity and the counted array's allocation into the canonical text,
-- where the Go engine carries the declared default (0 length, count 0).
local walk_default: (any, any) -> any

-- the walk spells an enum as its VARIANT NAME (read_value stores the name);
-- a declared default comes through the descriptor as the variant's NUMBER.
local function walk_enum_name(fd: any, n: any): string
  local i = typeof(n) == "number" and n or 0
  if i == 0 then return "None" end
  local pair = fd.variants and fd.variants[i]
  if pair == nil then return "None" end
  return pair[1]
end

local function walk_elem(T: any, e: any): any
  local s = e.shape
  if s == "bool" then return false end
  if s == "int" then
    -- the zero of the element's own storage domain (docs/PORT-LUAU.md, THE
    -- TYPE MAPPING): an Int128 at 128 bits, an integer at 64
    if e.dom == "int128" then return { lo = 0i, hi = 0i } end
    if e.dom == "integer" then return 0i end
    return 0
  end
  if s == "flags" then return 0 end
  if s == "float" then return 0.0 end
  if s == "string" or s == "bytes" then return "" end
  if s == "wstring" then return {} end
  if s == "enum" then return false end
  if s == "pointer" or s == "string_blob" or s == "bytes_blob" then return false end
  if s == "table" then return walk_default(T, T.Tables[e.target]) end
  if s == "union" then return { tag = 0 } end
  if s == "array" then
    -- a fixed array is whole (its slots ARE the value); a counted array starts
    -- at its born count and a list at zero (docs/SPEC-TABLES.md §2.3, §2.9)
    local out = {}
    local n = 0
    if e.fixed or e.keyed ~= nil then n = e.bound or 0
    elseif e.raw ~= nil and e.raw.array == "counted" then n = e.raw.min or 0 end
    for i = 1, n do out[i] = walk_elem(T, e.elem) end
    return out
  end
  if s == "map" then return {} end
  return nil
end

local function zero_elem(T: any, e: any): any
  return walk_elem(T, e)
end

walk_default = function(T: any, desc: any): any
  if desc == nil then return {} end
  local out: { [string]: any } = {}
  for _, fd in desc.fields do
    local name = fd.name
    if not fd.optional then
      local s = fd.shape
      local def = fd.raw.def
      if s == "bool" then
        out[name] = def == true
      elseif s == "int" then
        if def ~= nil then out[name] = def
        elseif fd.dom == "int128" then out[name] = { lo = 0i, hi = 0i }
        elseif fd.dom == "integer" then out[name] = 0i
        else out[name] = 0 end
      elseif s == "float" then
        out[name] = def ~= nil and def or 0.0
      elseif s == "string" or s == "bytes" then
        out[name] = def ~= nil and def or ""
      elseif s == "wstring" then
        out[name] = {}
      elseif s == "enum" then
        out[name] = walk_enum_name(fd, def or 0)
      elseif s == "flags" then
        out[name] = def ~= nil and def or 0
      elseif s == "table" then
        out[name] = walk_default(T, T.Tables[fd.target])
      elseif s == "union" then
        out[name] = { tag = 0 }
      elseif s == "array" then
        out[name] = walk_elem(T, fd)
      elseif s == "map" then
        out[name] = {}
      end
    end
  end
  return out
end

-- §16.10: a table value uses its declared array-root shape at every depth.
local function shape_of(T: any, d: any): string
  local s = d.shape
  if s == "bool" then return 'b' end
  if s == "string" or s == "wstring" or s == "bytes" then return 's' end
  if s == "string_blob" or s == "bytes_blob" then return 's' end
  if s == "enum" then return 's' end
  -- a timestamp's text is a JSON STRING, never the count's number (§16.2)
  if s == "int" and d.timestamp then return 's' end
  if s == "flags" then return 'a' end
  if s == "table" then
    local target = T.Tables[d.target]
    if target ~= nil and target.jsonArray then return 'a' end
    return 'o'
  end
  if s == "union" or s == "pointer" or s == "map" then return 'o' end
  if s == "array" then
    if d.keyed ~= nil then return 'o' end
    return 'a'
  end
  return 'n'
end

-- the JSON shape of the value at the cursor, without consuming it (§16.2)
local function value_shape(inp: Input): string?
  local c = peek(inp)
  if c == '{' then return 'o' end
  if c == '[' then return 'a' end
  if c == '"' then return 's' end
  if c == 't' or c == 'f' then return 'b' end
  if c == 'n' then return 'z' end
  if c == nil then return nil end
  return 'n'
end

local function find_key(desc: any, key: string): any
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

local MAX64_TEXT = "18446744073709551615"
local TEN = integer.create(10)

local function dec_integer(digits: string, neg: boolean): integer
  local v = 0i
  for i = 1, #digits do
    v = integer.add(integer.mul(v, TEN), integer.create(digits:byte(i) - 48))
  end
  if neg then v = integer.sub(0i, v) end
  return v
end

local function dec_int128(digits: string, neg: boolean): any
  local lo, hi = 0i, 0i
  for i = 1, #digits do
    local d = integer.create(digits:byte(i) - 48)
    local l = integer.mul(lo, TEN)
    local h = integer.mul(hi, TEN)
    local l2 = integer.add(l, d)
    if integer.ult(l2, l) then h = integer.add(h, 1i) end
    lo, hi = l2, h
  end
  if neg then
    lo = integer.add(integer.bnot(lo), 1i)
    hi = integer.add(integer.bnot(hi), lo == 0i and 1i or 0i)
  end
  return { lo = lo, hi = hi }
end

local function int128_neg(v: any): boolean
  return integer.lt(v.hi, 0i)
end

local function int128_cmp(a: any, b: any): number
  if integer.lt(a.hi, b.hi) then return -1 end
  if integer.gt(a.hi, b.hi) then return 1 end
  if integer.ult(a.lo, b.lo) then return -1 end
  if integer.ult(b.lo, a.lo) then return 1 end
  return 0
end

local function int128_cmp_signed(a: any, b: any): number
  local an, bn = int128_neg(a), int128_neg(b)
  if an ~= bn then return an and -1 or 1 end
  return int128_cmp(a, b)
end

local function norm_digits(token: string, integral: boolean): (string, boolean, boolean, boolean)
  -- (digits, neg, finite, frac)
  local neg = token:sub(1, 1) == "-"
  local t = neg and token:sub(2) or token
  if integral then
    return t:gsub("^0+", ""), neg and t ~= "" and t:match("^0*$") == nil, true, false
  end
  local x = tonumber(token)
  if x == nil or x ~= x or x == math.huge or x == -math.huge then
    return "", false, false, false
  end
  if math.floor(x) ~= x then return "", false, true, true end
  local digits = string.format("%.0f", math.abs(x)):gsub("^0+", "")
  if digits == "" then digits = "0" end
  return digits, x < 0, true, false
end

local function mag_sat(digits: string): boolean
  return #digits > 20 or (#digits == 20 and digits > MAX64_TEXT)
end

-- one integer token into the field's own domain (docs/PORT-LUAU.md, THE TYPE
-- MAPPING): dom is "number", "integer" or "int128". A magnitude past the
-- domain saturates and counts clamped (§16.2).
local function place_integer(fd: any, token: string, integral: boolean, report: any): any
  local digits, neg, finite, frac = norm_digits(token, integral)
  if not finite or frac then report.kindMismatch = report.kindMismatch + 1; return nil end
  local sat = false
  if fd.dom == "int128" then
    local v = dec_int128(digits == "" and "0" or digits, neg)
    if fd.bits ~= nil and fd.bits > 0 and fd.bits < 128 then
      -- range is clamped below
    end
    if fd.lo ~= nil and fd.hi ~= nil then
      if int128_cmp_signed(v, fd.lo) < 0 then v = fd.lo; report.clamped = report.clamped + 1
      elseif int128_cmp_signed(v, fd.hi) > 0 then v = fd.hi; report.clamped = report.clamped + 1 end
    end
    return v
  end
  if fd.dom == "number" then
    local n = tonumber(digits == "" and "0" or digits) or 0
    if neg then n = -n end
    if mag_sat(digits) then sat = true end
    if sat then report.clamped = report.clamped + 1 end
    if fd.lo ~= nil and fd.hi ~= nil then
      local lo = typeof(fd.lo) == "integer" and integer.tonumber(fd.lo) or fd.lo
      local hi = typeof(fd.hi) == "integer" and integer.tonumber(fd.hi) or fd.hi
      if n < lo then n = lo; report.clamped = report.clamped + 1
      elseif n > hi then n = hi; report.clamped = report.clamped + 1 end
    end
    if fd.bits ~= nil and fd.bits > 0 and fd.bits < 64 then
      local bmax = 2 ^ fd.bits - 1
      if n > bmax then n = bmax; report.clamped = report.clamped + 1 end
    end
    local width = fd.width or 8
    if fd.signed then
      local hi = width < 8 and 2 ^ (width * 8 - 1) - 1 or 2 ^ 63 - 1
      local lo = width < 8 and -2 ^ (width * 8 - 1) or -2 ^ 63
      if n < lo then n = lo; report.clamped = report.clamped + 1
      elseif n > hi then n = hi; report.clamped = report.clamped + 1 end
    else
      if n < 0 then n = 0; report.clamped = report.clamped + 1 end
      local hi = width < 8 and 2 ^ (width * 8) - 1 or 2 ^ 64 - 1
      if n > hi then n = hi; report.clamped = report.clamped + 1 end
    end
    return n
  end
  if mag_sat(digits) then sat = true end
  local v = dec_integer(digits == "" and "0" or digits, false)
  if neg then
    if fd.signed == false then report.clamped = report.clamped + 1; v = 0i
    else v = integer.sub(0i, v) end
  end
  if sat then report.clamped = report.clamped + 1 end
  local function cmp(a: any, b: any): number
    if typeof(a) == "integer" and typeof(b) == "integer" then
      if integer.lt(a, b) then return -1 elseif integer.gt(a, b) then return 1 else return 0 end
    end
    local x: number = if typeof(a) == "integer" then integer.tonumber(a) else a
    local y: number = if typeof(b) == "integer" then integer.tonumber(b) else b
    if x < y then return -1 elseif x > y then return 1 else return 0 end
  end
  if fd.lo ~= nil and fd.hi ~= nil then
    if cmp(v, fd.lo) < 0 then v = fd.lo; report.clamped = report.clamped + 1
    elseif cmp(v, fd.hi) > 0 then v = fd.hi; report.clamped = report.clamped + 1 end
  end
  if fd.bits ~= nil and fd.bits > 0 and fd.bits < 64 then
    local bmax = integer.sub(integer.lshift(1i, integer.create(fd.bits)), 1i)
    if integer.gt(v, bmax) then v = bmax; report.clamped = report.clamped + 1 end
  end
  local width = fd.width or 8
  if fd.signed then
    local high = 0x7fffffffffffffffi
    local low = integer.bnot(high)
    if width < 8 then
      high = integer.sub(integer.lshift(1i, integer.create(width * 8 - 1)), 1i)
      low = integer.sub(0i, integer.lshift(1i, integer.create(width * 8 - 1)))
    end
    if integer.lt(v, low) then v = low; report.clamped = report.clamped + 1
    elseif integer.gt(v, high) then v = high; report.clamped = report.clamped + 1 end
  else
    if fd.signed == false and integer.lt(v, 0i) then v = 0i; report.clamped = report.clamped + 1 end
    local high = -1i
    if width < 8 then high = integer.sub(integer.lshift(1i, integer.create(width * 8)), 1i) end
    if integer.ult(high, v) then v = high; report.clamped = report.clamped + 1 end
  end
  if fd.dom == "number" then return integer.tonumber(v) end
  return v
end

----------------------------------------------------------------------------
-- the WIDE kinds, exactly (docs/SPEC-TABLES.md §16.2, the fixed(I, F) and
-- int128/uint128 rows and **Numbers**; internal/tabletext/wide.go): a fixed
-- token is the value in whole units scaled by 2^F, a 128-bit token is its
-- digits, and nothing on the path is a double. The arithmetic is a small
-- unsigned big integer in base-10^7 limbs (little-endian), exact in a double
-- for every product below 2^53 (docs/PORT-LUAU.md, THE TYPE MAPPING: the
-- storage is the raw scaled integer at width I+F).
----------------------------------------------------------------------------

local BN_BASE = 10000000

local function bn_norm(a: { number }): { number }
  while #a > 1 and a[#a] == 0 do a[#a] = nil end
  return a
end

-- a × m + add, for m and add at most 65536
local function bn_muladd(a: { number }, m: number, add: number): { number }
  local out = table.create(#a + 2) :: { number }
  local carry = add
  for i = 1, #a do
    local x = a[i] * m + carry
    carry = math.floor(x / BN_BASE)
    out[i] = x - carry * BN_BASE
  end
  while carry > 0 do
    local q = math.floor(carry / BN_BASE)
    out[#out + 1] = carry - q * BN_BASE
    carry = q
  end
  return bn_norm(out)
end

-- a ÷ d and the remainder, for d at most 65536
local function bn_divmod(a: { number }, d: number): ({ number }, number)
  local out = table.create(#a) :: { number }
  local rem = 0
  for i = #a, 1, -1 do
    local x = rem * BN_BASE + a[i]
    local q = math.floor(x / d)
    out[i] = q
    rem = x - q * d
  end
  return bn_norm(out), rem
end

local function bn_cmp(a: { number }, b: { number }): number
  if #a ~= #b then return #a < #b and -1 or 1 end
  for i = #a, 1, -1 do
    if a[i] ~= b[i] then return a[i] < b[i] and -1 or 1 end
  end
  return 0
end

local function bn_from_digits(s: string): { number }
  local a = {}
  local i = #s
  while i >= 1 do
    local j = math.max(1, i - 6)
    a[#a + 1] = tonumber(s:sub(j, i)) :: number
    i = j - 1
  end
  if #a == 0 then a[1] = 0 end
  return bn_norm(a)
end

local function bn_digits(a: { number }): string
  local parts = { tostring(a[#a]) }
  for i = #a - 1, 1, -1 do parts[#parts + 1] = string.format("%07d", a[i]) end
  return table.concat(parts)
end

-- an unsigned 128-bit {lo, hi} as a big integer, sixteen bits at a time
local function bn_from_u128(v: any): { number }
  local a = { 0 }
  for k = 7, 0, -1 do
    local word = k >= 4 and v.hi or v.lo
    local piece = integer.band(integer.rshift(word, integer.create((k % 4) * 16)), 0xffffi)
    a = bn_muladd(a, 65536, integer.tonumber(piece))
  end
  return a
end

-- a big integer below 2^128 as an unsigned {lo, hi}
local function bn_to_u128(a: { number }): any
  local lo, hi = 0i, 0i
  for k = 0, 7 do
    local piece
    a, piece = bn_divmod(a, 65536)
    local p = integer.lshift(integer.create(piece), integer.create((k % 4) * 16))
    if k < 4 then lo = integer.bor(lo, p) else hi = integer.bor(hi, p) end
  end
  return { lo = lo, hi = hi }
end

local function u128_negate(v: any): any
  local lo = integer.add(integer.bnot(v.lo), 1i)
  local hi = integer.add(integer.bnot(v.hi), lo == 0i and 1i or 0i)
  return { lo = lo, hi = hi }
end

local BN_2_127 = bn_from_digits("170141183460469231731687303715884105728")
local BN_U128_MAX = bn_from_digits("340282366920938463463374607431768211455")
local WIDE_BAND = 40 -- wide.go's wideDecimalBand

-- the 128-bit domain of a kind's signedness: the saturation bound a text value
-- clamps to before the declared range (§16.2 **Numbers**; wide.go wideLimits)
local function wide_limits(signed: boolean): (any, any)
  if signed then
    return { lo = 0i, hi = integer.bnot(0x7fffffffffffffffi) }, { lo = -1i, hi = 0x7fffffffffffffffi }
  end
  return { lo = 0i, hi = 0i }, { lo = -1i, hi = -1i }
end

-- one number token into a wide kind's RAW 128-bit integer, the value × 2^F
-- (§16.2 **Numbers**; wide.go ParseWide): exact is false when the value is not
-- representable in Q I.F (kind_mismatch), saturated when the magnitude clamped
-- to the storage domain (counted clamped).
local function wide_parse(fd: any, token: string): (any, boolean, boolean)
  local i = 1
  local negative = false
  local c = token:sub(1, 1)
  if c == "-" or c == "+" then negative = c == "-"; i = 2 end
  local int_digits = token:match("^%d*", i) :: string
  i = i + #int_digits
  local frac_digits = ""
  if token:sub(i, i) == "." then
    frac_digits = token:match("^%d*", i + 1) :: string
    i = i + 1 + #frac_digits
  end
  local exp = 0
  c = token:sub(i, i)
  if c == "e" or c == "E" then
    i = i + 1
    local exp_negative = false
    c = token:sub(i, i)
    if c == "-" or c == "+" then exp_negative = c == "-"; i = i + 1 end
    for k = i, #token do
      local b = token:byte(k)
      if b < 48 or b > 57 then break end
      if exp < 100000 then exp = exp * 10 + (b - 48) end
    end
    if exp_negative then exp = -exp end
  end
  local digits = int_digits .. frac_digits
  local point = #int_digits + exp
  local lead = #(digits:match("^0*") :: string)
  digits = digits:sub(lead + 1)
  point = point - lead
  digits = digits:gsub("0+$", "")
  local signed = fd.signed ~= false
  local lo, hi = wide_limits(signed)
  if digits == "" then return { lo = 0i, hi = 0i }, true, false end
  if point > WIDE_BAND then
    if negative then return lo, true, true end
    return hi, true, true
  end
  if point < -WIDE_BAND then return nil, false, false end
  local m = bn_from_digits(digits)
  local f = fd.fbits or 0
  for _ = 1, f // 16 do m = bn_muladd(m, 65536, 0) end
  if f % 16 > 0 then m = bn_muladd(m, 2 ^ (f % 16), 0) end
  local scale = point - #digits
  if scale >= 0 then
    for _ = 1, scale do m = bn_muladd(m, 10, 0) end
  else
    for _ = 1, -scale do
      local rem
      m, rem = bn_divmod(m, 10)
      if rem ~= 0 then return nil, false, false end
    end
  end
  if negative then
    if not signed then return lo, true, true end
    if bn_cmp(m, BN_2_127) > 0 then return lo, true, true end
    return u128_negate(bn_to_u128(m)), true, false
  end
  if (signed and bn_cmp(m, BN_2_127) >= 0) or (not signed and bn_cmp(m, BN_U128_MAX) > 0) then
    return hi, true, true
  end
  return bn_to_u128(m), true, false
end

-- a 128-bit value shifted left by sh bits (a fixed bound in whole units to the
-- raw scale, §4; ir.TableRawRange)
local function wide_shl(v: any, sh: number): any
  if sh == 0 then return v end
  if sh >= 128 then return { lo = 0i, hi = 0i } end
  if sh >= 64 then return { lo = 0i, hi = integer.lshift(v.lo, integer.create(sh - 64)) } end
  local hi = integer.bor(integer.lshift(v.hi, integer.create(sh)), integer.rshift(v.lo, integer.create(64 - sh)))
  return { lo = integer.lshift(v.lo, integer.create(sh)), hi = hi }
end

-- a value of the field's storage domain as a 128-bit {lo, hi}, sign-extended
-- for a signed kind (docs/PORT-LUAU.md, THE TYPE MAPPING)
local function wide_of(fd: any, x: any): any
  if typeof(x) == "table" then return x end
  local v = typeof(x) == "integer" and x or integer.create(x)
  local hi = (fd.signed ~= false and integer.lt(v, 0i)) and -1i or 0i
  return { lo = v, hi = hi }
end

local function wide_ucmp(a: any, b: any): number
  if a.hi ~= b.hi then return integer.ult(a.hi, b.hi) and -1 or 1 end
  if a.lo ~= b.lo then return integer.ult(a.lo, b.lo) and -1 or 1 end
  return 0
end

-- §16.2: a wide field's token placed exactly, saturated, then clamped to the
-- declared range on the raw scale (wide.go WideClamp; read.go placeWide)
local function place_wide(fd: any, token: string, report: any): any
  local raw, exact, saturated = wide_parse(fd, token)
  if not exact then report.kindMismatch = report.kindMismatch + 1; return nil end
  if saturated then report.clamped = report.clamped + 1 end
  if fd.lo ~= nil and fd.hi ~= nil then
    local f = fd.fbits or 0
    local lo, hi = wide_shl(wide_of(fd, fd.lo), f), wide_shl(wide_of(fd, fd.hi), f)
    local cmp = fd.signed ~= false and int128_cmp or wide_ucmp
    if cmp(raw, lo) < 0 then raw = lo; report.clamped = report.clamped + 1
    elseif cmp(raw, hi) > 0 then raw = hi; report.clamped = report.clamped + 1 end
  end
  if fd.dom == "int128" then return raw end
  if fd.dom == "integer" then return raw.lo end
  return integer.tonumber(raw.lo)
end

-- one map key: the token is the whole string and must be a JSON number
local function map_int_key(fd: any, key: string): (any, string?)
  local probe = new_in(key)
  local token = scan_number(probe)
  if token == nil or probe.p ~= #key + 1 then return false, "malformed" end
  local integral = not (token:find("[%.eE]") ~= nil)
  local _, _, finite, frac = norm_digits(token, integral)
  if not finite or frac then return false, "mismatch" end
  local v = place_integer(fd, token, integral, { kindMismatch = 0, clamped = 0 })
  if v == nil then return false, "mismatch" end
  return v, nil
end

----------------------------------------------------------------------------
-- the read walk
----------------------------------------------------------------------------

local read_value: (any, any, Input, any, any, number, any, any, boolean) -> boolean
local read_array: (any, any, Input, any, any, number, any, any) -> boolean
local parse_timestamp: (text: string) -> integer?

-- required_seen closes ONE object (docs/SPEC-TABLES.md §2.3, §6.5): every
-- field marked `| required` whose guard holds must have been named, whatever
-- its value, and an object that left one out counts missing_required once,
-- naming the missing fields in declaration order, exactly as the binary read
-- refuses a record that omitted it. A guard is a conjunction of (bool slot,
-- value) pairs over the object's own storage; a field under a false guard is
-- not demanded.
local function required_seen(desc: any, v: any, seen: { [string]: boolean }, report: any): boolean
  local missing: { string }? = nil
  for _, fd in desc.fields do
    local raw = fd.raw
    if raw.required == true and not seen[fd.name] then
      local holds = true
      if raw.guard ~= nil then
        for _, pair in raw.guard do
          if v[pair[1]] ~= pair[2] then holds = false; break end
        end
      end
      if holds then
        if missing == nil then missing = {} end
        local m = missing :: { string }
        m[#m + 1] = raw.name or fd.key
      end
    end
  end
  if missing == nil then return true end
  report.missingRequired = (report.missingRequired or 0) + 1
  if report.reason == nil then report.reason = "missing_required: " .. table.concat(missing :: { string }, ", ") end
  return false
end

local function read_keys(T: any, desc: any, inp: Input, v: any, depth: number, first_key: string?, graph: any, report: any): boolean
  local seen: { [string]: boolean } = {}
  while true do
    local key
    if first_key ~= nil then
      key, first_key = first_key, nil
    else
      local c = peek(inp)
      if c == '}' then inp.p = inp.p + 1; return required_seen(desc, v, seen, report) end
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
          local def = walk_default(T, desc)
          v[fd.name] = def[fd.name]
          v[fd.name .. "_present"] = false
        else
          v[fd.name] = nil
        end
      else
        local want = shape_of(T, fd)
        if shape ~= want then
          report.kindMismatch = report.kindMismatch + 1
          if not skip_value(inp, depth + 1, graph) then return false end
          -- §16.2 optional row: the KEY is presence whatever its value, and
          -- §4's declared default fills it, so a wrong-shaped present value
          -- is the default and PRESENT (Go writes the default object/array).
          if fd.optional then v[fd.name] = walk_elem(T, fd) end
        else
          if not read_value(T, fd, inp, v, fd.name, depth, graph, report, false) then
            -- §16.3: the innermost field whose text decoded to U+0000 names
            -- the refusal; a nested table names its own field first.
            if inp.zero and report.reason == nil then
              report.reason = "text_holds_zero: " .. desc.name .. "." .. (fd.raw.name or fd.key)
            elseif inp.notutf8 and report.reason == nil then
              report.reason = "string_not_utf8: " .. desc.name .. "." .. (fd.raw.name or fd.key)
            end
            return false
          end
        end
        if fd.optional then v[fd.name .. "_present"] = true end
      end
    end
    local c = peek(inp)
    if c == ',' then
      inp.p = inp.p + 1
    elseif c == '}' then
      inp.p = inp.p + 1
      return required_seen(desc, v, seen, report)
    else
      inp.bad = true
      return false
    end
  end
end

-- §16.10: an array-root table reads its sole field directly from the array.
local function read_table(T: any, desc: any, inp: Input, v: any, depth: number, graph: any, report: any): boolean
  if depth > MAX_DEPTH then inp.bad = true; return false end
  if desc.jsonArray then
    local fd = desc.fields[1]
    if fd == nil then inp.bad = true; return false end
    return read_array(T, fd, inp, v, fd.name, depth, graph, report)
  end
  if peek(inp) ~= '{' then inp.bad = true; return false end
  inp.p = inp.p + 1
  return read_keys(T, desc, inp, v, depth, nil, graph, report)
end

local function read_pointer(T: any, d: any, inp: Input, holder: any, key: any, depth: number, graph: any, report: any): boolean
  if depth + 1 > MAX_DEPTH then inp.bad = true; return false end
  if peek(inp) ~= '{' then inp.bad = true; return false end
  inp.p = inp.p + 1
  local c = peek(inp)
  if c == '}' then
    inp.p = inp.p + 1
    local node = walk_default(T, T.Tables[d.target])
    holder[key] = node
    return required_seen(T.Tables[d.target], node, {}, report)
  end
  if c == nil then inp.bad = true; return false end
  local start = inp.p
  local _, _, ok = scan_string(inp, MAX_KEY)
  if not ok then return false end
  local k = inp.t:sub(start + 1, inp.p - 2)
  if peek(inp) ~= ':' then inp.bad = true; return false end
  inp.p = inp.p + 1
  if k ~= "&node" then
    local node = walk_default(T, T.Tables[d.target])
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
  local node = walk_default(T, T.Tables[d.target])
  holder[key] = node
  graph[label] = { node = nil, st = nil, open = true }
  if not read_keys(T, T.Tables[d.target], inp, node, depth + 1, nil, graph, report) then
    return false
  end
  graph[label] = { node = node, st = d.target, open = false }
  return true
end

local function read_base64(inp: Input, bound: number, report: any): (string | false)?
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
    if ch == '=' or bad then
    else
      local at = BASE64:find(ch, 1, true)
      if at == nil then
        bad = true
      else
        acc = band(bor(lshift(acc, 6), at - 1), 0xffffff)
        held = held + 6
        if held >= 8 then
          held = held - 8
          if bound < 0 or #out < bound then
            out[#out + 1] = string.char(band(rshift(acc, held), 0xff))
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

read_array = function(T: any, fd: any, inp: Input, holder: any, key: any, depth: number, graph: any, report: any): boolean
  if peek(inp) ~= '[' then inp.bad = true; return false end
  inp.p = inp.p + 1
  local out = {}
  if fd.fixed then
    for i = 1, fd.bound do out[i] = zero_elem(T, fd.elem) end
  end
  local bound = fd.list and 0x7fffffff or fd.bound
  local placed = 0
  local eshape = shape_of(T, fd.elem)
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

local function read_map(T: any, fd: any, inp: Input, holder: any, key: any, depth: number, graph: any, report: any): boolean
  if peek(inp) ~= '{' then inp.bad = true; return false end
  inp.p = inp.p + 1
  local out, seen = {}, {}
  local kf = fd.key_field
  local string_key = kf.shape == "string"
  while true do
    local c = peek(inp)
    if c == '}' then inp.p = inp.p + 1; break end
    if c == nil then inp.bad = true; return false end
    -- a map key is text storage whatever its kind (§16.2, §16.3), and the
    -- key stored is the string's DECODED bytes, escapes included
    local keytext, clamped, ok = scan_string(inp, MAX_KEY, true)
    if not ok then return false end
    local kraw = keytext :: string
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
    elseif not skip and value_shape(inp) ~= shape_of(T, vf) then
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
      local tmp: { v: any } = { v = nil }
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

local function read_keyed(T: any, fd: any, inp: Input, holder: any, key: any, depth: number, graph: any, report: any): boolean
  if peek(inp) ~= '{' then inp.bad = true; return false end
  inp.p = inp.p + 1
  local out = {}
  for i = 1, fd.bound do out[i] = zero_elem(T, fd.elem) end
  local seen = {}
  local eshape = shape_of(T, fd.elem)
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

local function read_union(T: any, u: any, inp: Input, depth: number, graph: any, report: any): any
  if peek(inp) ~= '{' then inp.bad = true; return nil end
  inp.p = inp.p + 1
  if peek(inp) == '}' then inp.p = inp.p + 1; return { tag = 0 } end
  local start = inp.p
  local _, _, ok = scan_string(inp, MAX_KEY)
  if not ok then return nil end
  local name = inp.t:sub(start + 1, inp.p - 2)
  if peek(inp) ~= ':' then inp.bad = true; return nil end
  inp.p = inp.p + 1
  local tag: number, arm: any = 0, nil
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
  elseif value_shape(inp) ~= shape_of(T, arm.field) then
    report.kindMismatch = report.kindMismatch + 1
    if not skip_value(inp, depth + 1, graph) then return nil end
  else
    out.tag = tag
    if not read_value(T, arm.field, inp, out, arm.slot, depth + 1, graph, report, false) then
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

-- DECIMAL MAGNITUDE COMPARE for a float32 tie (docs/SPEC-TABLES.md §16.1:
-- the nearest float32 to a decimal token is computed EXACTLY, never by
-- narrowing a double). Two decimal strings are compared as digits * 10^k.
local function decimal_parts(s: string): (string, number)
  local t = s
  if t:sub(1, 1) == "-" or t:sub(1, 1) == "+" then t = t:sub(2) end
  local m1, e1 = t:match("^([^eE]+)[eE]([-+]?%d+)$")
  local mant: string, ex: string = t, "0"
  if m1 ~= nil and e1 ~= nil then mant, ex = m1, e1 end
  local exp = tonumber(ex) or 0
  local dot = mant:find(".", 1, true)
  local digits, frac
  if dot ~= nil then
    digits = mant:sub(1, dot - 1) .. mant:sub(dot + 1)
    frac = #mant - dot
  else
    digits, frac = mant, 0
  end
  digits = digits:gsub("^0+", "")
  if digits == "" then return "", 0 end
  return digits, exp - frac
end

local function decimal_cmp(a: string, b: string): number
  local da, ka = decimal_parts(a)
  local db, kb = decimal_parts(b)
  if da == "" and db == "" then return 0 end
  if da == "" then return -1 end
  if db == "" then return 1 end
  local oa, ob = #da + ka, #db + kb
  if oa ~= ob then return oa < ob and -1 or 1 end
  local n = math.max(#da, #db)
  local pa, pb = da .. string.rep("0", n - #da), db .. string.rep("0", n - #db)
  if pa == pb then return 0 end
  return pa < pb and -1 or 1
end

-- A double that sits EXACTLY midway between two float32 values is a tie the
-- double cannot break: its own rounding already chose one neighbour. The Go
-- engine rounds the decimal TOKEN (ties to even), so the token's exact decimal
-- against the midpoint's decides. `string.format("%.99e", v)` gives the
-- double's exact leading decimal digits (a double is a finite decimal).
local function f32_tie(x: number, token: string): number?
  if x == 0 or x ~= x or x == math.huge or x == -math.huge then return nil end
  local ax = math.abs(x)
  local e = math.floor(math.log(ax) / math.log(2))
  local p = 2 ^ e
  if p > ax then e = e - 1 elseif p * 2 <= ax then e = e + 1 end
  local spacing = e < -126 and 2 ^ -149 or 2 ^ (e - 23)
  local half = spacing / 2
  local q = ax / half
  if q ~= math.floor(q) or math.floor(q) % 2 == 0 then return nil end
  local lo, hi = ax - half, ax + half
  local t = token:sub(1, 1) == "-" and token:sub(2) or token
  local c = decimal_cmp(t, string.format("%.99e", ax))
  local mag
  if c > 0 then
    mag = hi
  elseif c < 0 then
    mag = lo
  else
    mag = math.floor(lo / spacing + 0.5) % 2 == 0 and lo or hi
  end
  return x < 0 and -mag or mag
end

read_value = function(T: any, d: any, inp: Input, holder: any, key: any, depth: number, graph: any, report: any, elem: boolean): boolean
  local s = d.shape
  if s == "pointer" then
    return read_pointer(T, d, inp, holder, key, depth, graph, report)
  elseif s == "string" then
    local out, clamped, ok = scan_string(inp, d.bound, true)
    if not ok then return false end
    if clamped then report.clamped = report.clamped + 1 end
    holder[key] = out
    return true
  elseif s == "wstring" then
    local out, clamped, ok = scan_wstring(inp, d.bound, true)
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
    local out, _, ok = scan_string(inp, -1, true)
    if not ok then return false end
    holder[key] = { value = out }
    return true
  elseif s == "bytes_blob" then
    -- §16.2 `*bytes`: a body that is not base64 is kind_mismatch, the
    -- reference left null; a decoded body is boxed as the `*string` row is.
    local before = report.kindMismatch
    local out = read_base64(inp, -1, report)
    if out == nil then return false end
    -- The null reference is false in an element slot (a map value or an
    -- array element stays present, §16.2 map row) and nil in a field slot;
    -- an explicit branch, since `elem and false or nil` is always nil.
    if report.kindMismatch ~= before then
      if elem then holder[key] = false else holder[key] = nil end
    else holder[key] = { value = out == false and "" or out } end
    return true
  elseif s == "array" then
    if d.keyed ~= nil then return read_keyed(T, d, inp, holder, key, depth, graph, report) end
    return read_array(T, d, inp, holder, key, depth, graph, report)
  elseif s == "map" then
    return read_map(T, d, inp, holder, key, depth, graph, report)
  elseif s == "table" then
    local node = walk_default(T, T.Tables[d.target])
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
    -- §16.2 enum row: "None" and an unknown name read as the enum zero, which
    -- is false in an element slot (zero_elem: a map entry stays an entry, a
    -- list keeps its length) and nil in a field slot. An explicit branch,
    -- since `elem and false or nil` is always nil.
    if name == "None" then
      if elem then holder[key] = false else holder[key] = nil end
      return true
    end
    for _, v in ipairs(d.variants) do
      if v[1] == name then holder[key] = name; return true end
    end
    if elem then holder[key] = false else holder[key] = nil end
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
          if v == nm then bits = bor(bits, lshift(1, i - 1)); found = true; break end
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
        -- the TOKEN, not the narrowed double, decides an exact float32 tie
        local tie = f32_tie(x, token)
        x = tie ~= nil and tie or narrow
      end
    end
    holder[key] = x
    return true
  elseif s == "int" then
    if d.timestamp then
      local text, _, ok = scan_string(inp, -1)
      if not ok then return false end
      local ns = parse_timestamp(text :: string) -- an ok scan returns its string
      if ns == nil then
        -- §16.2's timestamp row: the whole text is refused and the report
        -- names the field, the same name the Go reader gives it
        report.reason = "timestamp_not_rfc3339: " .. (d.raw.name or d.name or key)
        report.malformed = true
        inp.bad = true
        return false
      end
      holder[key] = ns
      return true
    end
    local token = scan_number(inp)
    if token == nil then inp.bad = true; return false end
    local v
    if d.fbits ~= nil or d.dom == "int128" then
      v = place_wide(d, token, report)
    else
      local integral = not (token:find("[%.eE]") ~= nil)
      v = place_integer(d, token, integral, report)
    end
    if v ~= nil then holder[key] = v end
    return true
  end
  inp.bad = true
  return false
end

-- the strict parser's two-digit field and Gregorian calendar helpers, the
-- same rules internal/rfc3339 applies (docs/SPEC-TABLES.md §2.11, §16.2)
local function ts_two(s: string, i: number): (number, boolean)
  local a, b = s:byte(i), s:byte(i + 1)
  if a == nil or b == nil or a < 48 or a > 57 or b < 48 or b > 57 then return 0, false end
  return (a - 48) * 10 + (b - 48), true
end
local function ts_leap(y: number): boolean return y % 4 == 0 and (y % 100 ~= 0 or y % 400 == 0) end
local function ts_month_days(y: number, m: number): number
  if m == 1 or m == 3 or m == 5 or m == 7 or m == 8 or m == 10 or m == 12 then return 31 end
  if m == 4 or m == 6 or m == 9 or m == 11 then return 30 end
  if m == 2 then return ts_leap(y) and 29 or 28 end
  return 0
end
local function ts_days(y: number, m: number, d: number): number
  -- days_from_civil (Hinnant), the inverse of civil_from_days below
  local yy = y - (m <= 2 and 1 or 0)
  local era = (yy >= 0 and yy or yy - 399) // 400
  local yoe = yy - era * 400
  local doy = (153 * (m + (m > 2 and -3 or 9)) + 2) // 5 + d - 1
  local doe = yoe * 365 + yoe // 4 - yoe // 100 + doy
  return era * 146097 + doe - 719468
end

parse_timestamp = function(text: string): integer?
  -- exactly RFC 3339 §5.6's date-time, the standard's own spelling and no
  -- more (docs/SPEC-TABLES.md §2.11, §16.2): T/t, Z/z or sign HH:MM with both
  -- fields in range, month 01-12, a day valid for that month and year, hour
  -- 00-23, minute 00-59, second 00-59 (a leap second names no count here).
  -- A fraction of 1*DIGIT after a dot truncates to nanoseconds. An instant
  -- outside Time.UnixNano's domain names no count and is refused too. The
  -- count is carried as an integer so a 64-bit instant stays exact.
  if #text < 20 then return nil end
  if text:sub(5, 5) ~= "-" or text:sub(8, 8) ~= "-"
    or text:sub(14, 14) ~= ":" or text:sub(17, 17) ~= ":" then return nil end
  if text:sub(11, 11) ~= "T" and text:sub(11, 11) ~= "t" then return nil end
  local year = 0
  for i = 1, 4 do
    local c = text:byte(i)
    if c == nil or c < 48 or c > 57 then return nil end
    year = year * 10 + (c - 48)
  end
  local month, ok = ts_two(text, 6); if not ok then return nil end
  local day, ok2 = ts_two(text, 9); if not ok2 then return nil end
  local hour, ok3 = ts_two(text, 12); if not ok3 then return nil end
  local minute, ok4 = ts_two(text, 15); if not ok4 then return nil end
  local second, ok5 = ts_two(text, 18); if not ok5 then return nil end
  if month < 1 or month > 12 or day < 1 or day > ts_month_days(year, month) then return nil end
  if hour > 23 or minute > 59 or second > 59 then return nil end

  -- time-secfrac = "." 1*DIGIT, truncated to nanoseconds
  local index = 20
  local nanos = 0
  if text:sub(index, index) == "." then
    index = index + 1
    local digits = 0
    while index <= #text do
      local c = text:byte(index)
      if c < 48 or c > 57 then break end
      if digits < 9 then nanos = nanos * 10 + (c - 48) end
      digits = digits + 1
      index = index + 1
    end
    if digits == 0 then return nil end
    for _ = digits, 8 do nanos = nanos * 10 end
  end

  local offset = 0
  if index > #text then return nil end
  local c = text:sub(index, index)
  if c == "Z" or c == "z" then
    index = index + 1
    if index <= #text then return nil end
  elseif c == "+" or c == "-" then
    local sign = c == "-" and -1 or 1
    index = index + 1
    local oh, ook = ts_two(text, index); if not ook then return nil end
    index = index + 2
    if text:sub(index, index) ~= ":" then return nil end
    index = index + 1
    local om, mok = ts_two(text, index); if not mok then return nil end
    index = index + 2
    if oh > 23 or om > 59 or index <= #text then return nil end
    offset = sign * (oh * 3600 + om * 60)
  else
    return nil
  end

  local secs = ts_days(year, month, day) * 86400 + hour * 3600 + minute * 60 + second - offset
  -- Time.UnixNano's domain: the second before 1677-09-21T00:12:43 is
  -- -9223372037 and its fraction must reach 145224192 nanoseconds; the
  -- second of 2262-04-11T23:47:16 is 9223372036 and its fraction may not
  -- pass 854775807 (internal/rfc3339's strict parser)
  if secs < -9223372037 or secs > 9223372036 then return nil end
  if secs == -9223372037 and nanos < 145224192 then return nil end
  if secs == 9223372036 and nanos > 854775807 then return nil end
  return integer.add(integer.mul(integer.create(secs), 1000000000i), integer.create(nanos))
end

-- ONE TEXT: the root's object or declared array and nothing after it (§16.1, §16.10)
local function core_from(T: any, name: string, text: string, report: any): any
  local desc = T.Tables[name]
  if desc == nil then report.malformed = true; return nil end
  local inp = new_in(text)
  byte_order_mark(inp)
  if desc.jsonArray and value_shape(inp) ~= 'a' then
    report.malformed = true
    report.reason = "json_root_not_array: " .. name
    return nil
  end
  local graph = nil
  if desc.variable then graph = {} end
  local v = walk_default(T, desc)
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

local function W(): any return { parts = {} } end
local function raw(w: any, s: string) w.parts[#w.parts + 1] = s end
local function put(w: any, c: string) w.parts[#w.parts + 1] = c end
local function line(w: any, depth: number)
  w.parts[#w.parts + 1] = "\n" .. string.rep("  ", depth)
end

-- §16.3's write-side rule for a narrow text: the strict reader refuses
-- ill-formed UTF-8, a U+0000 (which no wire carries) and a value past its
-- bound, so the writer refuses the value rather than emitting a text that
-- reads back differently (§2.13, §4.2). The refusal is named by Go's name
-- for the same value (§16.3, the write refuses what the strict read refuses).
local refuse_reason: string = ""
local function narrow_text_ok(s: string, bound: number): boolean
  if bound >= 0 and #s > bound then refuse_reason = "clamped"; return false end
  local i = 1
  while i <= #s do
    local c = s:byte(i)
    if c == 0 then refuse_reason = "text_holds_zero"; return false end
    if c < 0x80 then
      i = i + 1
    else
      local width = utf8_seq(s, i)
      if width < 0 then refuse_reason = "string_not_utf8"; return false end
      i = i + width
    end
  end
  return true
end

-- §16.3's write-side rule for a wide text: no zero unit and no lone surrogate
-- (the units the wire's kind 33 and the strict reader refuse).
local function wide_text_ok(units: { number }, bound: number): boolean
  if bound >= 0 and #units > bound then refuse_reason = "clamped"; return false end
  local i = 1
  while i <= #units do
    local u = units[i]
    if u == 0 then refuse_reason = "text_holds_zero"; return false end
    if u >= 0xd800 and u <= 0xdbff then
      local low = units[i + 1]
      if low == nil or low < 0xdc00 or low > 0xdfff then refuse_reason = "string_not_utf8"; return false end
      i = i + 2
    elseif u >= 0xdc00 and u <= 0xdfff then
      refuse_reason = "string_not_utf8"; return false
    else
      i = i + 1
    end
  end
  return true
end

local function int_cmp(a: any, b: any): number
  if typeof(a) == "integer" and typeof(b) == "integer" then
    if integer.lt(a, b) then return -1 elseif integer.gt(a, b) then return 1 end
    return 0
  end
  local x: number = if typeof(a) == "integer" then integer.tonumber(a) else (a :: number)
  local y: number = if typeof(b) == "integer" then integer.tonumber(b) else (b :: number)
  if x < y then return -1 elseif x > y then return 1 end
  return 0
end

-- §5's write-side bound, the rule the strict reader applies as a clamp: a
-- value off its declared range, past its bits(N) width or outside its storage
-- width has no honest text, so the writer refuses it rather than writing a
-- value that reads back as a different one.
local function int_ok(fd: any, v: any): boolean
  if fd.timestamp then return true end
  local f = fd.fbits or 0
  if fd.dom == "int128" then
    if fd.lo ~= nil and fd.hi ~= nil then
      local cmp = fd.signed ~= false and int128_cmp or wide_ucmp
      local lo, hi = wide_shl(wide_of(fd, fd.lo), f), wide_shl(wide_of(fd, fd.hi), f)
      if cmp(v, lo) < 0 or cmp(v, hi) > 0 then refuse_reason = "clamped"; return false end
    end
    if fd.bits ~= nil and fd.bits > 0 and fd.bits < 128 then
      if v.hi ~= 0i then refuse_reason = "clamped"; return false end
      local bmax = integer.sub(integer.lshift(1i, integer.create(fd.bits)), 1i)
      if integer.gt(v.lo, bmax) then refuse_reason = "clamped"; return false end
    end
    return true
  end
  if fd.lo ~= nil and fd.hi ~= nil then
    if fd.fbits ~= nil then
      local cmp = fd.signed ~= false and int128_cmp or wide_ucmp
      local raw = wide_of(fd, v)
      local lo, hi = wide_shl(wide_of(fd, fd.lo), f), wide_shl(wide_of(fd, fd.hi), f)
      if cmp(raw, lo) < 0 or cmp(raw, hi) > 0 then refuse_reason = "clamped"; return false end
    else
      if int_cmp(v, fd.lo) < 0 or int_cmp(v, fd.hi) > 0 then refuse_reason = "clamped"; return false end
    end
  end
  if fd.bits ~= nil and fd.bits > 0 and fd.bits < 64 then
    local n: number = if typeof(v) == "integer" then integer.tonumber(v) else (v :: number)
    if n < 0 or n > 2 ^ fd.bits - 1 then refuse_reason = "clamped"; return false end
  end
  local width = fd.width or 8
  if width < 8 then
    local n: number = if typeof(v) == "integer" then integer.tonumber(v) else (v :: number)
    local bits = width * 8
    if fd.signed ~= false then
      local lim = 2 ^ (bits - 1)
      if n < -lim or n > lim - 1 then refuse_reason = "clamped"; return false end
    elseif n < 0 or n > 2 ^ bits - 1 then
      refuse_reason = "clamped"; return false
    end
  elseif fd.dom == "number" then
    local n: number = if typeof(v) == "integer" then integer.tonumber(v) else (v :: number)
    if fd.signed ~= false then
      if n < -(2 ^ 63) or n > 2 ^ 63 - 1 then refuse_reason = "clamped"; return false end
    elseif n < 0 or n > 2 ^ 64 - 1 then
      refuse_reason = "clamped"; return false
    end
  end
  return true
end

-- §16.3: a non-finite float has no JSON spelling, and a compressed float off
-- its declared range is one the strict reader clamps. A float32 field is
-- finite AFTER its narrowing (docs/SPEC-TABLES.md §3.3, as the packet rule):
-- a double past the float32 range narrows to an infinity and is refused too.
local narrowScratch = buffer.create(4)
local function float_ok(fd: any, v: number): boolean
  local x = v
  if fd.f32 and x == x then buffer.writef32(narrowScratch, 0, x); x = buffer.readf32(narrowScratch, 0) end
  if x ~= x or x == math.huge or x == -math.huge then refuse_reason = "non_finite_float"; return false end
  if fd.lo ~= nil and (v < fd.lo or v > fd.hi) then refuse_reason = "clamped"; return false end
  return true
end

local function write_string(w: any, s: string)
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
      raw(w, "\\u00" .. hex:sub((rshift(c, 4)) + 1, (rshift(c, 4)) + 1) .. hex:sub((band(c, 0xf)) + 1, (band(c, 0xf)) + 1))
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

local function write_wstring(w: any, units: { number })
  local hex = "0123456789abcdef"
  put(w, '"')
  local i = 1
  while i <= #units do
    local code = units[i]
    if code >= 0xd800 and code <= 0xdbff and i + 1 <= #units then
      local low = units[i + 1]
      if low >= 0xdc00 and low <= 0xdfff then
        code = 0x10000 + (lshift((code - 0xd800), 10)) + (low - 0xdc00)
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
      raw(w, "\\u00" .. hex:sub((rshift(code, 4)) + 1, (rshift(code, 4)) + 1) .. hex:sub((band(code, 0xf)) + 1, (band(code, 0xf)) + 1))
    else
      raw(w, utf8_encode(code))
    end
    i = i + 1
  end
  put(w, '"')
end

local function write_base64(w: any, data: string)
  put(w, '"')
  local i = 1
  while #data - i + 1 >= 3 do
    local a, b, c = data:byte(i, i + 2)
    local t = bor(bor(lshift(a, 16), lshift(b, 8)), c)
    raw(w, BASE64:sub((band(rshift(t, 18), 63)) + 1, (band(rshift(t, 18), 63)) + 1)
      .. BASE64:sub((band(rshift(t, 12), 63)) + 1, (band(rshift(t, 12), 63)) + 1)
      .. BASE64:sub((band(rshift(t, 6), 63)) + 1, (band(rshift(t, 6), 63)) + 1)
      .. BASE64:sub((band(t, 63)) + 1, (band(t, 63)) + 1))
    i = i + 3
  end
  local left = #data - i + 1
  if left > 0 then
    local a = data:byte(i)
    local b = left == 2 and data:byte(i + 1) or 0
    local t = bor(lshift(a, 16), lshift(b, 8))
    raw(w, BASE64:sub((band(rshift(t, 18), 63)) + 1, (band(rshift(t, 18), 63)) + 1)
      .. BASE64:sub((band(rshift(t, 12), 63)) + 1, (band(rshift(t, 12), 63)) + 1)
      .. (left == 2 and BASE64:sub((band(rshift(t, 6), 63)) + 1, (band(rshift(t, 6), 63)) + 1) or "=")
      .. "=")
  end
  put(w, '"')
end


-- the unsigned decimal of a 64-bit pattern, with integer arithmetic never a
-- float and never a signed `%d` (docs/PORT-LUA.md, 64-BIT AND 128-BIT INTEGERS)
local function u128_text(v: any): string
  local lo, hi = v.lo, v.hi
  local digits = {}
  repeat
    local rem = 0i
    local nlo, nhi = 0i, 0i
    for bit = 127, 0, -1 do
      local b
      if bit >= 64 then b = integer.band(integer.rshift(hi, integer.create(bit - 64)), 1i)
      else b = integer.band(integer.rshift(lo, integer.create(bit)), 1i) end
      rem = integer.add(integer.mul(rem, 2i), b)
      if integer.ge(rem, 10i) then
        rem = integer.sub(rem, 10i)
        if bit >= 64 then nhi = integer.bor(nhi, integer.lshift(1i, integer.create(bit - 64)))
        else nlo = integer.bor(nlo, integer.lshift(1i, integer.create(bit))) end
      end
    end
    digits[#digits + 1] = string.char(48 + integer.tonumber(rem))
    lo, hi = nlo, nhi
  until lo == 0i and hi == 0i
  local out = {}
  for i = #digits, 1, -1 do out[#out + 1] = digits[i] end
  return table.concat(out)
end

local function i128_abs_text(v: any): string
  local lo = integer.add(integer.bnot(v.lo), 1i)
  local hi = integer.add(integer.bnot(v.hi), lo == 0i and 1i or 0i)
  return u128_text({ lo = lo, hi = hi })
end

local function int_text(fd: any, v: any): string
  if fd.dom == "int128" then
    if fd.signed and int128_neg(v) then return "-" .. i128_abs_text(v) end
    return u128_text(v)
  end
  if fd.dom == "integer" then
    if fd.signed then return string.format("%d", v) end
    return string.format("%u", v)
  end
  if typeof(v) == "integer" then
    if fd.signed then return string.format("%d", v) end
    return string.format("%u", v)
  end
  if fd.signed then return string.format("%d", v) end
  if v >= 0 then return string.format("%d", v) end
  -- luau-analyze 0.741 types string.format's %u argument as number, while the
  -- runtime formats an integer's unsigned 64 bits (docs/PORT-LUAU.md, THE TYPE
  -- MAPPING): the integer crosses the builtin's signature as any
  return string.format("%u", integer.create(v) :: any)
end

-- a 128-bit value shifted right by sh bits, logically
local function wide_shr(v: any, sh: number): any
  if sh == 0 then return v end
  if sh >= 128 then return { lo = 0i, hi = 0i } end
  if sh >= 64 then return { lo = integer.rshift(v.hi, integer.create(sh - 64)), hi = 0i } end
  local lo = integer.bor(integer.rshift(v.lo, integer.create(sh)), integer.lshift(v.hi, integer.create(64 - sh)))
  return { lo = lo, hi = integer.rshift(v.hi, integer.create(sh)) }
end

-- a 128-bit value's low sh bits
local function wide_low(v: any, sh: number): any
  if sh >= 128 then return v end
  if sh >= 64 then
    if sh == 64 then return { lo = v.lo, hi = 0i } end
    return { lo = v.lo, hi = integer.band(v.hi, integer.sub(integer.lshift(1i, integer.create(sh - 64)), 1i)) }
  end
  if sh == 0 then return { lo = 0i, hi = 0i } end
  return { lo = integer.band(v.lo, integer.sub(integer.lshift(1i, integer.create(sh)), 1i)), hi = 0i }
end

-- a fixed field's raw value as §16.2's text: the shortest EXACT decimal in
-- whole units with at least one fractional digit, `1.0`, `-0.25`,
-- `3.0000152587890625` (wide.go FormatWide). The fraction r / 2^F is
-- r × 5^F / 10^F, so its F decimal digits are r × 5^F, zero-padded, with
-- the trailing zeros dropped; no float is on the path.
local function fixed_text(fd: any, x: any): string
  local v = wide_of(fd, x)
  local f = fd.fbits or 0
  local negative = fd.signed ~= false and integer.lt(v.hi, 0i)
  local mag = negative and u128_negate(v) or v
  local whole = u128_text(wide_shr(mag, f))
  local frac = wide_low(mag, f)
  local text = (negative and "-" or "") .. whole .. "."
  if frac.lo == 0i and frac.hi == 0i then return text .. "0" end
  local n = bn_from_u128(frac)
  for _ = 1, f // 6 do n = bn_muladd(n, 15625, 0) end
  for _ = 1, f % 6 do n = bn_muladd(n, 5, 0) end
  local digits = bn_digits(n)
  digits = string.rep("0", f - #digits) .. digits
  return text .. (digits:gsub("0+$", ""))
end

local function civil_from_days(z: number): (number, number, number)
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

local function write_timestamp(w: any, ns: any)
  -- a 64-bit count is carried as an integer so every instant, the ends of
  -- Time.UnixNano's domain included, writes exactly (docs/SPEC-TABLES.md
  -- §2.11, §16.2; docs/PORT-LUAU.md, 64-BIT AND 128-BIT INTEGERS)
  local v: integer
  if typeof(ns) == "integer" then v = ns else v = integer.create(ns) end
  local day = 86400000000000i
  local days = integer.div(v, day)
  local rem = integer.rem(v, day)
  if integer.lt(rem, 0i) then
    days = integer.sub(days, 1i)
    rem = integer.add(rem, day)
  end
  local rest = integer.tonumber(rem)
  local sec = rest // 1000000000
  local frac = rest % 1000000000
  local y, m, d = civil_from_days(integer.tonumber(days))
  raw(w, string.format('"%04d-%02d-%02dT%02d:%02d:%02d', y, m, d,
    sec // 3600, (sec % 3600) // 60, sec % 60))
  if frac ~= 0 then
    local f = string.format("%09d", frac):gsub("0+$", "")
    raw(w, "." .. f)
  end
  raw(w, 'Z"')
end

local function format_g(v: number, prec: number): string
  return string.format("%." .. prec .. "g", v)
end

local function float_text(v: number, single: boolean): string?
  if v ~= v or v == math.huge or v == -math.huge then return nil end
  local low, high = single and 6 or 15, single and FLOAT32_DIGITS or 17
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
local function bytes_less(a: string, b: string): boolean
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

local function map_entries(cache: any, fd: any, x: any): { any }
  if x == nil then return {} end
  local got = cache[x]
  if got ~= nil then return got end
  local kf = fd.key_field
  local keys = {}
  for key in pairs(x) do keys[#keys + 1] = key end
  table.sort(keys, function(a: any, b: any): boolean
    if kf.shape == "string" then return bytes_less(a, b) end
    if kf.signed then return a < b end
    return ult(a, b)
  end)
  got = {}
  for i, key in ipairs(keys) do got[i] = key end
  cache[x] = got
  return got
end

local function count_of(fd: any, v: any): number
  local s = fd.shape
  if s == "bytes" then return #v end
  if fd.list then return #v end
  if fd.fixed then return fd.bound or 0 end
  return math.min(#v, fd.bound or 0)
end

-- THE NUMBERING PASS (§16.7): how many slots name each node, and whether the
-- graph has a cycle, which the text refuses as the wire does (§3.1).
local edge_value: (any, any, any, any, any, { any }, any) -> ()
local edge_union: (any, any, any, { any }, any) -> ()
local edges: (any, any, any, { any }, any) -> { any }
edge_value = function(T: any, fd: any, val: any, holder: any, name: any, out: { any }, cache: any)
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

edge_union = function(T: any, u: any, val: any, out: { any }, cache: any)
  local tag = val and val.tag or 0
  if tag == 0 or u == nil then return end
  local arm = u.arms[tag]
  if arm == nil or arm.field == nil then return end
  edge_value(T, arm.field, val[arm.slot], val, arm.slot, out, cache)
end

edges = function(T: any, desc: any, v: any, out: { any }, cache: any): { any }
  for _, fd in ipairs(desc.fields) do
    if not (fd.optional and not v[fd.name .. "_present"]) then
      edge_value(T, fd, v[fd.name], v, fd.name, out, cache)
    end
  end
  return out
end

local function count_nodes(T: any, desc: any, root: any): any
  local g: any = { count = {}, labels = {}, next = 0 }
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
      -- a blob is owned by one slot (§2.5): the text writer refuses a second
      -- slot naming it, blob_shared, as every writer does
      if e.blob and n > 1 then return nil end
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

local write_value: (any, any, any, number, any, any, boolean) -> boolean
local write_table: (any, any, any, number, any, any) -> boolean
local write_array: (any, any, any, number, any, any) -> boolean
local write_pointer: (T: any, target: string, node: any, depth: number, g: any, w: any) -> boolean
local write_field: (any, any, any, number, any, any) -> boolean
local write_fields: (any, any, any, number, any, any, boolean) -> (boolean, boolean)

write_fields = function(T: any, desc: any, v: any, depth: number, g: any, w: any, any: boolean): (boolean, boolean)
  for _, fd in ipairs(desc.fields) do
    if not (fd.optional and not v[fd.name .. "_present"]) then
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

-- §16.10: an array-root table writes its sole field directly, wherever it appears.
write_table = function(T: any, desc: any, v: any, depth: number, g: any, w: any): boolean
  if depth > MAX_DEPTH then return false end
  if desc.jsonArray then
    local fd = desc.fields[1]
    if fd == nil then return false end
    return write_array(T, fd, v[fd.name] or {}, depth, g, w)
  end
  local ok, any = write_fields(T, desc, v, depth, g, w, false)
  if not ok then return false end
  if not any then raw(w, "{}"); return true end
  line(w, depth)
  put(w, '}')
  return true
end

write_pointer = function(T: any, target: string, node: any, depth: number, g: any, w: any): boolean
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

local function write_union(T: any, u: any, val: any, depth: number, g: any, w: any): boolean
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
    if not write_value(T, arm.field, val[arm.slot], depth + 1, g, w, false) then return false end
  end
  line(w, depth)
  put(w, '}')
  return true
end

write_array = function(T: any, fd: any, val: any, depth: number, g: any, w: any): boolean
  -- §2.9, §5: a counted array past its bound is a value the strict reader
  -- clamps, so the writer refuses it instead of writing the first bound.
  if not fd.fixed and fd.keyed == nil and not fd.list and #val > (fd.bound or 0) then
    refuse_reason = "clamped"
    return false
  end
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

local function write_keyed(T: any, fd: any, val: any, depth: number, g: any, w: any): boolean
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

local function write_map(T: any, fd: any, val: any, depth: number, g: any, w: any): boolean
  local keys = map_entries({}, fd, val)
  if #keys == 0 then raw(w, "{}"); return true end
  local kf = fd.key_field
  put(w, '{')
  for i, key in ipairs(keys) do
    if i > 1 then put(w, ',') end
    line(w, depth + 1)
    if kf.shape == "string" then
      if not narrow_text_ok(key, kf.bound or -1) then return false end
      write_string(w, key)
    else
      if not int_ok(kf, key) then return false end
      write_string(w, int_text(kf, key))
    end
    raw(w, ": ")
    if not write_value(T, fd.value_field, val[key], depth + 1, g, w, true) then return false end
  end
  line(w, depth)
  put(w, '}')
  return true
end

write_value = function(T: any, d: any, val: any, depth: number, g: any, w: any, elem: boolean): boolean
  local s = d.shape
  if s == "pointer" then
    return write_pointer(T, d.target :: string, val, depth, g, w)
  elseif s == "string" then
    local text = val or ""
    if not narrow_text_ok(text, d.bound or -1) then return false end
    write_string(w, text)
    return true
  elseif s == "wstring" then
    local units = val or {}
    if not wide_text_ok(units, d.bound or -1) then return false end
    write_wstring(w, units)
    return true
  elseif s == "bytes" then
    local data = val or ""
    if d.bound ~= nil and d.bound >= 0 and #data > d.bound then refuse_reason = "clamped"; return false end
    write_base64(w, data)
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
    if s == "string_blob" then
      if not narrow_text_ok(body, -1) then return false end
      write_string(w, body)
    else
      write_base64(w, body)
    end
    return true
  elseif s == "table" then
    local desc = T.Tables[d.target]
    if desc == nil then return false end
    return write_table(T, desc, val or walk_default(T, desc), depth, g, w)
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
    local names: { string } = d.variants or {}
    for i, name in ipairs(names) do
      if (band(bits, lshift(1, i - 1))) ~= 0 then
        if not first then put(w, ',') end
        first = false
        line(w, depth + 1)
        write_string(w, name)
        seen = bor(seen, lshift(1, i - 1))
      end
    end
    if (band(bits, bnot(seen))) ~= 0 then return false end
    line(w, depth)
    put(w, ']')
    return true
  elseif s == "bool" then
    raw(w, val and "true" or "false")
    return true
  elseif s == "float" then
    if not float_ok(d, val) then return false end
    local text = float_text(val, d.f32)
    if text == nil then return false end
    raw(w, text)
    return true
  elseif s == "int" then
    if d.timestamp then write_timestamp(w, val); return true end
    if not int_ok(d, val) then return false end
    raw(w, d.fbits ~= nil and fixed_text(d, val) or int_text(d, val))
    return true
  end
  return false
end

write_field = function(T: any, fd: any, v: any, depth: number, g: any, w: any): boolean
  local ok
  if fd.shape == "pointer" then
    ok = write_pointer(T, fd.target, v[fd.name], depth, g, w)
  else
    ok = write_value(T, fd, v[fd.name], depth, g, w, false)
  end
  return ok
end

-- ONE INSTANCE as ONE text (§16.1): a trailing newline belongs to the form;
-- an array root is its field's own text with no framing newline, exactly as
-- schema-go's tabletext writer emits it (§16.10).
local function core_to(T: any, name: string, v: any): string?
  local desc = T.Tables[name]
  if desc == nil or v == nil then return nil end
  local g = count_nodes(T, desc, v)
  if g == nil then return nil end
  local w = W()
  if not write_table(T, desc, v, 0, g, w) then return nil end
  if not desc.jsonArray then w.parts[#w.parts + 1] = "\n" end
  return table.concat(w.parts)
end

----------------------------------------------------------------------------
-- THE DESCRIPTOR ADAPTER and the value translation (docs/PORT-LUAU.md, THE
-- TYPE MAPPING). The walk above is the Lua leg's; this layer supplies the
-- table module's descriptors as the walk reads them and moves a value between
-- the Lua-shaped working value (strings, enum names, {tag}) and the Luau
-- shaped one (buffers, enum numbers, {Type}).
----------------------------------------------------------------------------

local St = require("./schema_tables")
local MAP_KEY_ID: integer = 0x3dc94a19365b10eci

local adapt_field: (any, any) -> any
local adapt_table: (any) -> any
local adapt_union: (any) -> any
local TCache: any = {}
local UCache: any = {}

local ProvidedFlags: any = nil

-- SetFlags hands the walk the flags variant names a field descriptor does not
-- carry (docs/SPEC-TABLES.md §16.2, the flags row).
function J.SetFlags(f: any)
  ProvidedFlags = f
end

local function flag_names(owner: any, fd: any): { string }?
  -- the descriptor carries a flags field's variant names (luautable.go's
  -- valueDesc); SetFlags is the override a caller may still hand in
  if fd ~= nil and fd.flags ~= nil and ProvidedFlags == nil then return fd.flags end
  local g = ProvidedFlags
  if g == nil or owner == nil then return nil end
  return g[owner.name .. "." .. (fd.name or fd.slot or "")]
end

adapt_table = function(desc: any): any
  local got = TCache[desc.name]
  if got ~= nil then return got end
  local a = { name = desc.name, variable = desc.variable, jsonArray = desc.jsonArray, new = desc.new, raw = desc, fields = {} }
  TCache[desc.name] = a
  for _, fd in desc.fields do
    a.fields[#a.fields + 1] = adapt_field(fd, desc)
  end
  return a
end

adapt_union = function(desc: any): any
  local got = UCache[desc.name]
  if got ~= nil then return got end
  local a = { name = desc.name, raw = desc, arms = {} }
  UCache[desc.name] = a
  for i, arm in desc.arms do
    local name = arm.json or arm.name
    local entry = { name = name, tag = i, slot = arm.slot, void = arm.s == "void", raw = arm }
    if not entry.void then
      entry.field = adapt_field(arm, desc)
      entry.field.name = name
    end
    a.arms[i] = entry
  end
  return a
end

adapt_field = function(fd: any, owner: any): any
  local s = fd.s
  local o: any = {
    name = fd.slot or fd.name,
    key = fd.json or fd.name,
    optional = fd.present ~= nil,
    present = fd.present,
    raw = fd,
  }
  if s == "bool" then o.shape = "bool"
  elseif s == "int" then
    local names = flag_names(owner, fd)
    if names ~= nil then
      o.shape = "flags"
      o.variants = names
    else
    o.shape = "int"
    o.width = fd.kw
    o.signed = fd.signed
    o.dom = fd.dom
    o.lo, o.hi = fd.lo, fd.hi
    o.bits = fd.bits
    o.fbits = fd.fbits
    o.timestamp = fd.timestamp
    if fd.fbits ~= nil then o.fixed_units = true end
    end
  elseif s == "float" then o.shape = "float"; o.f32 = fd.f32; o.lo = fd.lo; o.hi = fd.hi; o.res = fd.res
  elseif s == "str" then o.shape = fd.wide and "wstring" or "string"; o.bound = fd.bound; o.wide = fd.wide
  elseif s == "text" then o.shape = "string"; o.bound = -1
  elseif s == "bytes" then o.shape = "bytes"; o.bound = fd.bound
  elseif s == "enum" then
    o.shape = "enum"
    o.variants = fd.vs
  elseif s == "ptr" then o.shape = "pointer"; o.target = fd.t; adapt_table(St.lookup(fd.t))
  elseif s == "sblob" then o.shape = "string_blob"
  elseif s == "bblob" then o.shape = "bytes_blob"
  elseif s == "tab" then o.shape = "table"; o.target = fd.t; adapt_table(St.lookup(fd.t))
  elseif s == "union" then o.shape = "union"; o.target = fd.t; adapt_union(St.lookup(fd.t))
  elseif s == "arr" then
    o.shape = "array"
    o.elem = adapt_field(fd.e, owner)
    o.bound = fd.bound or 0
    o.fixed = fd.array == "fixed"
    o.list = fd.array == "list"
    o.keyed = fd.array == "keyed" and fd.keyvs or nil
    o.elem_kind = fd.ek
    o.str_elem = fd.e ~= nil and (fd.e.s == "str")
    o.wide_elem = fd.e ~= nil and fd.e.wide == true
    if o.elem.shape == "int" then
      local names = flag_names(owner, fd)
      if names ~= nil then o.elem.shape = "flags"; o.elem.variants = names end
    end
    if fd.e ~= nil and fd.e.s == "ptr" then o.elem.target = fd.e.t end
    if fd.e ~= nil and fd.e.s == "tab" then o.elem.target = fd.e.t end
    if fd.e ~= nil and fd.e.s == "union" then o.elem.target = fd.e.t; adapt_union(St.lookup(fd.e.t)) end
  elseif s == "map" then
    o.shape = "map"
    o.entry = fd.t
    o.entry_desc = adapt_table(St.lookup(fd.t))
    local kf, vf
    for _, f in o.entry_desc.fields do
      if f.raw.id == MAP_KEY_ID then kf = f else vf = f end
    end
    o.key_field = kf
    o.value_field = vf
  elseif s == "flags" then
    o.shape = "flags"
    o.variants = flag_names(owner, fd)
  else
    error("schema: schema_json.luau cannot read shape " .. tostring(s))
  end
  return o
end

----------------------------------------------------------------------------
-- values, Lua-shaped <-> Luau-shaped
----------------------------------------------------------------------------

local function vlen(x: any): number
  if typeof(x) == "buffer" then return buffer.len(x) end
  if type(x) == "table" then return #x end
  if typeof(x) == "string" then return #x end
  return 0
end

local function to_string(b: any, n: number?): string
  if typeof(b) == "string" then return b end
  if typeof(b) ~= "buffer" then return "" end
  local len = n or buffer.len(b)
  if len > buffer.len(b) then len = buffer.len(b) end
  return buffer.readstring(b, 0, len)
end

local function from_string(s: any, capacity: number?): buffer
  if typeof(s) == "buffer" then return s end
  if typeof(s) ~= "string" then s = "" end
  local b = buffer.create(math.max(#s, capacity or #s))
  if #s > 0 then buffer.writestring(b, 0, s) end
  return b
end

local function wide_units(s: string): { number }
  local out = {}
  local b = from_string(s)
  for i = 0, math.floor(#s / 2) - 1 do out[i + 1] = buffer.readu16(b, i * 2) end
  return out
end

local function wide_bytes(units: { number }): string
  local b = buffer.create(#units * 2)
  for i, u in units do buffer.writeu16(b, (i - 1) * 2, u) end
  return buffer.readstring(b, 0, #units * 2)
end

local function enum_name(fd: any, n: any): string
  local i = typeof(n) == "number" and n or 0
  if i == 0 then return "None" end
  local pair = fd.variants[i]
  if pair == nil then return "None" end
  return pair[1]
end

local function enum_number(fd: any, name: string): number
  if name == "None" then return 0 end
  for i, pair in fd.variants do if pair[1] == name then return i end end
  return 0
end

local to_lua: (any, any, any, any) -> any
local from_lua: (any, any, any, any) -> any

local function conv_to_lua(fd: any, x: any, A: any, memo: any): any
  local s = fd.shape
  if s == "bool" or s == "float" then return x
  elseif s == "int" then
    return x
  elseif s == "string" then
    if fd.wide then
      return from_string(wide_bytes(x or {}), (#x or 0) * 2)
    end
    return from_string(x or "", fd.bound and fd.bound >= 0 and fd.bound or #(x or ""))
  elseif s == "wstring" then
    return from_string(wide_bytes(x or {}), (#x or 0) * 2)
  elseif s == "bytes" then return from_string(x or "", fd.bound)
  elseif s == "string_blob" or s == "bytes_blob" then
    if x == nil or x == false then return nil end
    local body = typeof(x) == "table" and x.value or x
    return from_string(body or "")
  elseif s == "enum" then return enum_number(fd, x)
  elseif s == "flags" then
    if typeof(x) == "integer" then return x end
    return integer.create(x or 0)
  elseif s == "pointer" or s == "table" then
    if x == nil then return nil end
    if x == false then return false end
    return from_lua(A.Tables[fd.target], x, A, memo)
  elseif s == "union" then
    local tag = x ~= nil and x.tag or 0
    local out: any = { Type = tag }
    if tag ~= 0 then
      local arm = A.Unions[fd.target].arms[tag]
      if arm ~= nil and arm.field ~= nil then
        out[arm.slot] = conv_to_lua(arm.field, x[arm.slot], A, memo)
        if arm.raw.s == "str" then out[arm.slot .. "Length"] = #x[arm.slot] end
        if arm.raw.s == "bytes" then out[arm.slot .. "Length"] = #x[arm.slot] end
        -- a counted array arm carries its Count, as a table field does
        if arm.raw.s == "arr" and arm.raw.array == "counted" then out[arm.slot .. "Count"] = #x[arm.slot] end
      end
    end
    return out
  elseif s == "array" then
    if typeof(x) == "buffer" then
      local out = {}
      -- a [N]uint8 or an array of string buffers
      return out
    end
    local out = {}
    -- a fixed or keyed array rides at its bound (§16.2: fewer elements pad
    -- with defaults), so a slot the walk left empty (an enum at None, a
    -- scalar the text never named) lands at its zero as Load lands it
    local n = #x
    if (fd.fixed or fd.keyed ~= nil) and (fd.bound or 0) > n then n = fd.bound end
    local e = fd.elem
    for i = 1, n do
      local xi = x[i]
      if xi == nil and e.shape ~= "table" and e.shape ~= "union" and e.shape ~= "array" and e.shape ~= "map" then
        xi = zero_elem(A, e)
      end
      out[i] = conv_to_lua(e, xi, A, memo)
    end
    return out
  elseif s == "map" then
    local out = {}
    if x ~= nil then
      for k, v in x do
        local e = fd.entry_desc.new()
        e[fd.key_field.raw.slot] = conv_to_lua(fd.key_field, k, A, memo)
        if fd.key_field.raw.s == "str" or fd.key_field.raw.s == "bytes" then
          e[fd.key_field.raw.slot .. "Length"] = vlen(k)
        end
        -- §16.2 map row and the `*T` row: a null pointer value is an entry
        -- whose reference is absent, nil in the entry's storage as a null
        -- pointer field is, so Save names no node for it; the walk's false
        -- (a Lua table cannot hold nil) is never converted as a node. A
        -- `*bytes`/`*string` value's false already converts to nil.
        local cv = nil
        if fd.value_field.shape ~= "pointer" or v ~= false then
          cv = conv_to_lua(fd.value_field, v, A, memo)
        end
        e[fd.value_field.raw.slot] = cv
        if fd.value_field.raw.s == "str" or fd.value_field.raw.s == "bytes" then
          e[fd.value_field.raw.slot .. "Length"] = vlen(v)
        end
        -- a counted array map value rides its used count beside the array, as
        -- a table field does (docs/SPEC-TABLES.md §4.2), so Save writes only
        -- the live elements
        if fd.value_field.raw.s == "arr" and fd.value_field.raw.array == "counted" then
          e[fd.value_field.raw.slot .. "Count"] = #v
        end
        out[#out + 1] = e
      end
    end
    return out
  end
  return x
end

local function conv_from_lua(fd: any, x: any, A: any, memo: any): any
  memo.count = memo.count or {}
  memo.len = memo.len or {}
  local s = fd.shape
  if s == "bool" or s == "float" then return x
  elseif s == "int" then
    -- a timestamp is a 64-bit count and stays an integer so the text form's
    -- nanosecond count is exact (docs/PORT-LUAU.md, 64-BIT AND 128-BIT INTEGERS)
    return x
  elseif s == "string" then
    if x == nil then return "" end
    return to_string(x, memo.len[x])
  elseif s == "wstring" then
    -- a wstring's Length counts UTF-16 code units, two bytes each (§16.2, the
    -- wstring(N) row; docs/PORT-LUAU.md, THE TYPE MAPPING)
    local n = memo.len[x]
    return wide_units(to_string(x, n ~= nil and n * 2 or nil))
  elseif s == "string_blob" or s == "bytes_blob" then
    -- a loaded blob is its own node (§3.1, §16.7): it crosses as a box, one per
    -- Luau buffer, so two empty blobs stay two nodes and one buffer named
    -- twice stays one (the writer refuses that graph)
    if x == nil or x == false then return nil end
    memo.blob = memo.blob or {}
    local box = memo.blob[x]
    if box == nil then
      box = { value = to_string(x, memo.len[x]) }
      memo.blob[x] = box
    end
    return box
  elseif s == "bytes" then
    if x == nil or x == false then return nil end
    return to_string(x, memo.len[x])
  elseif s == "enum" then
    if x == nil or x == 0 then return false end
    return enum_name(fd, x)
  elseif s == "flags" then
    if typeof(x) == "integer" then return integer.tonumber(x) end
    return x or 0
  elseif s == "pointer" or s == "table" then
    if x == nil or x == false then return nil end
    return to_lua(A.Tables[fd.target], x, A, memo)
  elseif s == "union" then
    local tag = x ~= nil and x.Type or 0
    local out: any = { tag = tag }
    if tag ~= 0 then
      local arm = A.Unions[fd.target].arms[tag]
      if arm ~= nil and arm.field ~= nil then
        if arm.raw.s == "str" or arm.raw.s == "bytes" then
          memo.len = memo.len or {}
          memo.len[x[arm.slot]] = x[arm.slot .. "Length"] or 0
        end
        -- a counted array arm rides at its Count, as a table field does
        if arm.raw.s == "arr" and arm.raw.array == "counted" then
          memo.count[x[arm.slot]] = x[arm.slot .. "Count"] or 0
        end
        out[arm.slot] = conv_from_lua(arm.field, x[arm.slot], A, memo)
      end
    end
    return out
  elseif s == "array" then
    local out = {}
    local n = memo.count[x]
    if n == nil then n = #x end
    -- a count outside 0..bound is one the strict reader clamps (§2.9, §5), so
    -- the walk records the value as unwritable rather than truncating it here
    if n < 0 then memo.invalid = true; refuse_reason = "clamped"; n = 0 end
    if (fd.bound or 0) > 0 and n > fd.bound then memo.invalid = true; refuse_reason = "clamped"; n = fd.bound end
    if fd.elem.shape == "pointer" then
      -- a null element is nil in the Luau storage and false in the walk's
      -- array, so a null past the last live one still counts (§2.5, §16.2)
      for i = 1, n do
        local e = x[i]
        if e == nil or e == false then out[i] = false else out[i] = conv_from_lua(fd.elem, e, A, memo) end
      end
    else
      if n > #x then n = #x end
      for i = 1, n do out[i] = conv_from_lua(fd.elem, x[i], A, memo) end
    end
    return out
  elseif s == "map" then
    local out = {}
    if x ~= nil then
      for _, e in x do
        local kv = e[fd.key_field.raw.slot]
        if fd.key_field.raw.s == "str" or fd.key_field.raw.s == "bytes" then
          memo.len[kv] = e[fd.key_field.raw.slot .. "Length"] or 0
        end
        local k = conv_from_lua(fd.key_field, kv, A, memo)
        local vv = e[fd.value_field.raw.slot]
        if fd.value_field.raw.s == "str" or fd.value_field.raw.s == "bytes" then
          memo.len[vv] = e[fd.value_field.raw.slot .. "Length"] or 0
        end
        -- a counted array map value's live count rides beside the array, as a
        -- table field does (docs/SPEC-TABLES.md §4.2), so ToJson writes only
        -- the used elements, not every bound slot
        if fd.value_field.raw.s == "arr" and fd.value_field.raw.array == "counted" then
          memo.count[vv] = e[fd.value_field.raw.slot .. "Count"] or 0
        end
        local cv = conv_from_lua(fd.value_field, vv, A, memo)
        -- §16.2 map row and the `*T`/`*bytes`/`*string` rows: an entry whose
        -- value is a null reference is still an entry, held as false in the
        -- walk (a Lua table cannot hold nil), so ToJson writes it as null.
        local vs = fd.value_field.shape
        if cv == nil and (vs == "pointer" or vs == "string_blob" or vs == "bytes_blob") then cv = false end
        out[k] = cv
      end
    end
    return out
  end
  return x
end

to_lua = function(desc: any, v: any, A: any, memo: any): any
  if v == nil then return nil end
  local got = memo.v2l and memo.v2l[v]
  if got ~= nil then return got end
  local out: any = {}
  memo.v2l = memo.v2l or {}
  memo.v2l[v] = out
  memo.l2v = memo.l2v or {}
  memo.l2v[out] = v
  for _, fd in desc.fields do
    local luau_slot = fd.raw.slot or fd.raw.name
    if fd.raw.s == "str" or fd.raw.s == "bytes" then
      memo.len = memo.len or {}
      memo.len[v[luau_slot]] = v[luau_slot .. "Length"] or 0
    end
    if fd.raw.s == "arr" then
      memo.count = memo.count or {}
      local av = v[luau_slot]
      if fd.raw.array == "counted" then memo.count[av] = v[luau_slot .. "Count"] or 0
      elseif fd.raw.array == "list" then memo.count[av] = av ~= nil and (av.n or #av) or 0
      elseif fd.raw.array == "fixed" or fd.raw.array == "keyed" then memo.count[av] = fd.raw.bound or 0 end
    end
    if fd.optional then
      if v[fd.raw.present] then
        out[luau_slot] = conv_from_lua(fd, v[luau_slot], A, memo)
        out[luau_slot .. "_present"] = true
      end
    else
      out[luau_slot] = conv_from_lua(fd, v[luau_slot], A, memo)
    end
  end
  return out
end

from_lua = function(desc: any, v: any, A: any, memo: any): any
  if v == nil then return nil end
  local got = memo.l2v and memo.l2v[v]
  if got ~= nil then return got end
  local out: any = {}
  memo.l2v = memo.l2v or {}
  memo.l2v[v] = out
  memo.v2l = memo.v2l or {}
  memo.v2l[out] = v
  for _, fd in desc.fields do
    local lv = v[fd.raw.slot or fd.raw.name]
    if fd.raw.present ~= nil then
      -- the walk marks a present optional as <slot>_present (§2.3); the Luau
      -- value carries it in the descriptor's own present slot
      if v[fd.name .. "_present"] then
        out[fd.name] = conv_to_lua(fd, lv, A, memo)
        out[fd.raw.present] = true
        if fd.raw.s == "str" then
          out[fd.name .. "Length"] = vlen(lv)
        elseif fd.raw.s == "bytes" then out[fd.name .. "Length"] = vlen(lv)
        elseif fd.raw.s == "arr" and fd.raw.array == "counted" then out[fd.name .. "Count"] = #lv end
      end
    else
      out[fd.name] = conv_to_lua(fd, lv, A, memo)
      if fd.raw.s == "str" then
        out[fd.name .. "Length"] = vlen(lv)
      elseif fd.raw.s == "bytes" then out[fd.name .. "Length"] = vlen(lv)
      elseif fd.raw.s == "arr" then
        if fd.raw.array == "counted" then out[fd.name .. "Count"] = #lv end
        if fd.str_elem then
          local lens = table.create(#lv)
          for i = 1, #lv do lens[i] = vlen(lv[i]) end
          out[fd.name .. "Length"] = lens
        end
      end
    end
  end
  return out
end

local function adapted(root: string): (any, any, any)
  local desc = St.lookup(root)
  local A = { Tables = {}, Unions = {} }
  local t = adapt_table(desc)
  for name, d in TCache do A.Tables[name] = d end
  for name, u in UCache do A.Unions[name] = u end
  return desc, A, t
end

-- FromJsonTolerant is the TOLERANT text read (docs/SPEC-TABLES.md §6.5, §16):
-- it keeps the recovered value with the counters counted onto the report, and
-- answers no value only where the text does not say what the instance holds:
-- a malformed text, a zero or ill-formed string placed in text (§16.3) and a
-- missing required field (§2.3), each with the count and its reason.
function J.FromJsonTolerant(root: string, text: buffer, report: any): any
  local _, A, adesc = adapted(root)
  local input = buffer.readstring(text, 0, buffer.len(text))
  local lua = core_from(A, root, input, report)
  -- FromJson answers no value on any refusal (docs/SPEC-TABLES.md §16.3,
  -- §16): a partial value is not one the read placed.
  if lua == nil or report.malformed then return nil end
  local memo = { l2u = {}, u2l = {}, len = {} }
  return from_lua(adesc, lua, A, memo)
end

-- the strict text read's refusals, in Go's TableReport.Err() order
-- (docs/SPEC-TABLES.md §6.5): unknown, widened and duplicate alone never refuse
local STRICT: { { string } } = {
  { "blobShared", "blob_shared" },
  { "clamped", "clamped" },
  { "kindMismatch", "kind_mismatch" },
  { "nonFinite", "non_finite" },
}

-- THE PLAIN NAME IS THE STRICT READ (docs/SPEC-TABLES.md §6.5): FromJson
-- overwrites the caller's report (optional, nil allowed) with this read's
-- counters, and refuses with one named reason and no recovered value every
-- read that crosses a security or meaning boundary — a malformed text, a
-- missing required field, a blob named from two slots, a clamped value, a
-- kind mismatch. A caller who needs the recovered value past one of those
-- asks for it by name, through FromJsonTolerant.
function J.FromJson(root: string, text: buffer, report: any): any
  local rep: any = report or {}
  rep.malformed = false
  rep.unknown, rep.kindMismatch, rep.clamped, rep.widened, rep.duplicate = 0, 0, 0, 0, 0
  rep.missingRequired, rep.nonFinite, rep.blobShared = 0, 0, 0
  rep.reason = nil
  local v = J.FromJsonTolerant(root, text, rep)
  if v == nil then
    if rep.reason == nil then rep.reason = "malformed" end
    return nil
  end
  for _, k in STRICT do
    if (rep[k[1]] or 0) > 0 then
      rep.reason = k[2]
      return nil
    end
  end
  return v
end

-- ToJson writes the value's §16 text and refuses (returns nil and names the
-- refusal in the optional report) every value the strict reader would refuse
-- (docs/SPEC-TABLES.md §16.3): an out-of-range integer, a non-finite float,
-- ill-formed or zero-holding text, a text or count past its bound. The name
-- is Go's name for the same refusal.
function J.ToJson(root: string, v: any, report: any): buffer?
  local _, A, adesc = adapted(root)
  local memo: any = { l2u = {}, u2l = {}, len = {} }
  refuse_reason = ""
  local lua = to_lua(adesc, v, A, memo)
  if memo.invalid then
    if report then report.reason = refuse_reason ~= "" and refuse_reason or "clamped" end
    return nil
  end
  local text = core_to(A, root, lua)
  if text == nil then
    if report then report.reason = refuse_reason ~= "" and refuse_reason or "clamped" end
    return nil
  end
  return from_string(text)
end


return J
