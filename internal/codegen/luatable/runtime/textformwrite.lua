-- textformwrite.lua — the Lua table runtime's HAND-EDITABLE TEXT FORM document
-- writer and lossless scalar edit (docs/SPEC-TABLES.md §16.9.3). Emitted beside
-- the unit's <Package>Tables.lua by internal/codegen/luatable, which embeds
-- runtime/*.lua by glob.
--
-- §16.9.3: a read answers a DOCUMENT and its value; the document retains the
-- original byte sequence, token spans, trivia and field/element identity. A
-- read then write with no edit reproduces the input byte for byte, and an edit
-- of one existing scalar replaces only that value's token span, every other
-- byte staying identical. The strict read itself is textform.lua (§16.9.1,
-- §16.9.2, §16.9.4); this file keeps the source bytes beside it and lays the
-- spans over the same parse, so the writer is the read the Go writer is: Save
-- returns the retained bytes and Edit applies its splices by ORIGINAL span and
-- reads the edited document back before returning it.
local TF = require("textform")

local W = {}

local function fail(name, pos, path)
  error({ name = name, offset = (pos or 1) - 1, path = path }, 0)
end

---------------------------------------------------------------------------
-- a span-carrying parse of a document the strict reader already accepted
-- (§16.9.1's grammar: gap, comments, atoms, strings and lists). The reader
-- owns the refusals; this parse only remembers where each token lives.
---------------------------------------------------------------------------

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

local function hex4(st)
  local s = st.t:sub(st.p, st.p + 3)
  if #s ~= 4 or not s:match("^%x%x%x%x$") then fail("text_encoding", st.p) end
  st.p = st.p + 4
  return tonumber(s, 16)
end

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
  return { k = "str", v = table.concat(out), s = start, e = st.p - 1 }
end

local keyword_pattern = "^:[A-Za-z_][A-Za-z0-9_/%.%-]*$"

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
  local e = st.p - 1
  if tok == "true" then return { k = "bool", v = true, s = start, e = e } end
  if tok == "false" then return { k = "bool", v = false, s = start, e = e } end
  if tok == "null" then return { k = "null", s = start, e = e } end
  if tok:match(keyword_pattern) then return { k = "kw", v = tok:sub(2), s = start, e = e } end
  if is_number(tok) then return { k = "num", v = tok, s = start, e = e } end
  fail("text_syntax", start)
end

local parse_value

local function scan_list(st)
  local open = st.p
  st.p = st.p + 1
  local items = {}
  while true do
    skip_gap(st)
    if st.p > #st.t then fail("text_unterminated", open) end
    if st.t:byte(st.p) == 41 then
      st.p = st.p + 1
      return { k = "list", items = items, s = open, e = st.p - 1 }
    end
    items[#items + 1] = parse_value(st)
  end
end

parse_value = function(st)
  skip_gap(st)
  if st.p > #st.t then fail("text_unterminated", st.p) end
  local c = st.t:byte(st.p)
  if c == 40 then return scan_list(st) end
  if c == 34 then return scan_string(st) end
  return scan_atom(st)
end

-- parse_document is the whole form: one root list and trailing trivia only.
local function parse_document(text)
  local st = { t = text, p = 1 }
  local root = parse_value(st)
  skip_gap(st)
  if st.p <= #text then fail("text_syntax", st.p) end
  return root
end

---------------------------------------------------------------------------
-- the path map (§16.9.3: field/element identity over preserved spans). The
-- keys are the Go document's paths: "" (root), "field", "field.child",
-- "field[2]", a keyed slot's and a union arm's name, and a map entry's
-- "[i][0]" key and "[i][1]" value.
---------------------------------------------------------------------------

local function join(path, key)
  if path == "" then return key end
  return path .. "." .. key
end

local function at(path, i)
  return path .. "[" .. tostring(i) .. "]"
end

local function is_key(node)
  return node.k == "kw" or node.k == "str"
end

local function find_field(desc, key)
  for _, fd in ipairs(desc.fields) do
    if fd.key == key then return fd end
  end
  return nil
end

local mark_field

local function mark_record(T, desc, node, path, first, paths, allow_label)
  local items = node.items
  local i = first
  if allow_label and #items >= 2 and is_key(items[1]) and items[1].v == "&node" then
    -- a definition's label is the first pair (§16.9.2 sharing). The path stays
    -- the whole pointer value, which is compound: replacing a reference itself
    -- is a structural edit, not a leaf edit (§16.9.3).
    i = 3
    if i > #items then return end
  end
  while i + 1 <= #items do
    local key, value = items[i], items[i + 1]
    if is_key(key) then
      local fd = find_field(desc, key.v)
      if fd ~= nil then mark_field(T, fd, value, join(path, fd.key), paths) end
    end
    i = i + 2
  end
end

local function mark_array(T, d, node, path, paths)
  local items = node.items
  if d.keyed ~= nil then
    local i = 1
    while i + 1 <= #items do
      if is_key(items[i]) then
        local sp = join(path, items[i].v)
        paths[sp] = items[i + 1]
        mark_field(T, d.elem, items[i + 1], sp, paths)
      end
      i = i + 2
    end
    return
  end
  for i, item in ipairs(items) do
    local ip = at(path, i - 1)
    paths[ip] = item
    mark_field(T, d.elem, item, ip, paths)
  end
end

local function mark_map(T, d, node, path, paths)
  for i, pair in ipairs(node.items) do
    if pair.k == "list" then
      local pp = at(path, i - 1)
      paths[pp] = pair
      if #pair.items >= 2 then
        local kp, vp = at(pp, 0), at(pp, 1)
        paths[kp] = pair.items[1]
        paths[vp] = pair.items[2]
        mark_field(T, d.value_field, pair.items[2], vp, paths)
      end
    end
  end
end

local function mark_union(T, d, node, path, paths)
  local u = T.Unions[d.target]
  if u == nil then return end
  local items = node.items
  if #items < 2 or not is_key(items[1]) then return end
  local arm
  for _, a in ipairs(u.arms) do
    if a.name == items[1].v then
      arm = a
      break
    end
  end
  if arm == nil then return end
  local ap = join(path, items[1].v)
  if arm.field == nil then
    paths[ap] = items[2]
  else
    mark_field(T, arm.field, items[2], ap, paths)
  end
end

mark_field = function(T, d, node, path, paths)
  paths[path] = node
  local s = d.shape
  if node.k == "null" then return end
  if s == "array" then
    mark_array(T, d, node, path, paths)
  elseif s == "map" then
    mark_map(T, d, node, path, paths)
  elseif s == "union" then
    mark_union(T, d, node, path, paths)
  elseif s == "table" then
    mark_record(T, T.Tables[d.target], node, path, 1, paths, false)
  elseif s == "pointer" then
    if d.target ~= nil and T.Tables[d.target] ~= nil and node.k == "list" then
      mark_record(T, T.Tables[d.target], node, path, 1, paths, true)
    end
  end
end

local function mark_document(T, name, _text, root)
  local paths = {}
  paths[""] = root
  mark_record(T, T.Tables[name], root, "", 1, paths, false)
  return paths
end

---------------------------------------------------------------------------
-- the edit (§16.9.3): one existing scalar replaced by its token span only
---------------------------------------------------------------------------

-- one scalar token, the only value a leaf edit replaces. A list spelling or
-- a second token is the structural/text_edit_conflict case, never an edit.
local function parse_edit_text(text)
  local st = { t = text, p = 1 }
  local node = parse_value(st)
  if st.p - 1 ~= #text or node.k == "list" then return nil end
  return node
end

-- same_value compares two read values as values, including a float's zero's
-- sign, so a semantic no-op keeps the old spelling (§16.9.3).
local function same_value(a, b)
  if type(a) ~= type(b) then return false end
  if type(a) ~= "table" then
    if type(a) == "number" and a == 0 and b == 0 then return (1 / a) == (1 / b) end
    return a == b
  end
  for k, v in pairs(a) do
    if not same_value(v, b[k]) then return false end
  end
  for k in pairs(b) do
    if a[k] == nil then return false end
  end
  return true
end

local function apply(src, splices)
  local out, cursor = {}, 1
  for _, sp in ipairs(splices) do
    out[#out + 1] = src:sub(cursor, sp.s - 1)
    out[#out + 1] = sp.text
    cursor = sp.e + 1
  end
  out[#out + 1] = src:sub(cursor)
  return table.concat(out)
end

-- W.Load / W.LoadTextForm reads one hand-editable document (§16.9.1–§16.9.4)
-- and answers the DOCUMENT and its value. The strict read is textform.lua's;
-- this file keeps its source bytes and lays the value spans over the same
-- parse (§16.9.3: the document retains what the table alone cannot).
function W.Load(T, name, text, limits)
  if type(text) ~= "string" then fail("text_kind_mismatch", 1) end
  if T.Tables[name] == nil then fail("text_unknown_key", 1) end
  local v = TF.Load(T, name, text, limits)
  local root = parse_document(text)
  local doc = { T = T, name = name, text = text, limits = limits, paths = mark_document(T, name, text, root) }
  return doc, v
end

W.LoadDocument = W.Load
W.LoadTextForm = W.Load

-- W.Save / W.SaveTextForm writes the retained bytes (§16.9.3: a read then
-- write with no edit reproduces the input byte for byte).
function W.Save(doc)
  if type(doc) ~= "table" or type(doc.text) ~= "string" then fail("text_edit_conflict", 1) end
  return doc.text
end

W.SaveTextForm = W.Save

-- W.Span answers the byte span of the value at path (1-based, inclusive).
function W.Span(doc, path)
  local node = doc.paths[path]
  if node == nil then return nil end
  return node.s, node.e
end

-- W.Edit / W.EditTextForm replaces existing scalar values by their original
-- token spans (§16.9.3): an absent value or a compound is a structural edit
-- and refuses text_edit_conflict; a semantic no-op keeps the old spelling;
-- the edited document is read back under the same schema before it returns.
-- edits is a sequence of { path = "...", text = "<one token>" }.
function W.Edit(doc, edits)
  if type(doc) ~= "table" or type(doc.T) ~= "table" then fail("text_edit_conflict", 1) end
  local old = TF.Load(doc.T, doc.name, doc.text, doc.limits)
  local splices = {}
  for _, e in ipairs(edits or {}) do
    local node = doc.paths[e.path]
    if node == nil or node.k == "list" then fail("text_edit_conflict", node and node.s or 1, e.path) end
    if type(e.text) ~= "string" then fail("text_edit_conflict", node.s, e.path) end
    local value = parse_edit_text(e.text)
    if value == nil then fail("text_edit_conflict", node.s, e.path) end
    splices[#splices + 1] = { s = node.s, e = node.e, text = e.text, path = e.path }
  end
  table.sort(splices, function(a, b)
    return a.s < b.s
  end)
  for i = 2, #splices do
    if splices[i].s <= splices[i - 1].e then fail("text_edit_conflict", splices[i].s, splices[i].path) end
  end
  -- a semantic no-op retains the old spelling: each splice is judged alone
  -- against the original document, so a real edit beside it is never lost
  local kept = {}
  for _, sp in ipairs(splices) do
    local single = apply(doc.text, { sp })
    local v = TF.Load(doc.T, doc.name, single, doc.limits)
    if not same_value(v, old) then kept[#kept + 1] = sp end
  end
  local text = apply(doc.text, kept)
  local v = TF.Load(doc.T, doc.name, text, doc.limits)
  local root = parse_document(text)
  local out = {
    T = doc.T,
    name = doc.name,
    text = text,
    limits = doc.limits,
    paths = mark_document(doc.T, doc.name, text, root),
  }
  return out, v
end

W.EditTextForm = W.Edit

return W
