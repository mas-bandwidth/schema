// THE HAND-EDITABLE TEXT FORM (docs/SPEC-TABLES.md §16.9): the strict
// s-expression authoring form whose document retains its source bytes. A
// successful LoadTextForm returns a document and its typed table value; the
// rewrite of an unedited document is the input byte for byte (§16.9.1,
// §16.9.2). A leaf edit replaces only its own original span (§16.9.3).
// Refusals carry the stable names of §16.9.4. This is the Squirrel leg of the
// Go oracle (internal/codegen/gotable/textform.go).
local _M = vargv[0];
local _require = vargv[1];

// ---- the document record (retained source bytes and leaf spans) ----
local _Doc = class {
  bytes = null;
  spans = null;
};

// _asString turns the harness's input blob into the document's text; the
// interpreter's strings are the source bytes.
local _asString = function(b) {
  if (typeof b == "string") return b;
  local s = "";
  for (local i = 0; i < b.len(); i++) s += format("%c", b[i]);
  return s;
};

// ---- the strict lexer (docs/SPEC-TABLES.md §16.9.1) ----
// A token is { k = "(" | ")" | "str" | "atom", s, e }; gaps are SP, TAB, LF,
// CRLF and semicolon comments to LF, CRLF or EOF.
local _lex = function(text) {
  local toks = [];
  local n = text.len();
  local i = 0;
  while (i < n) {
    local c = text[i];
    if (c == ' ' || c == '\t' || c == '\n' || c == '\r') {
      i += 1;
    } else if (c == ';') {
      while (i < n && text[i] != '\n' && text[i] != '\r') i += 1;
    } else if (c == '(' || c == ')') {
      toks.push({ k = format("%c", c), s = i, e = i + 1 });
      i += 1;
    } else if (c == '"') {
      local s = i;
      i += 1;
      local done = false;
      while (i < n) {
        if (text[i] == '"') { i += 1; done = true; break; }
        if (text[i] == '\\') { i += 2; } else { i += 1; }
      }
      if (!done || i > n) return { toks = toks, err = "text_unterminated", at = n };
      toks.push({ k = "str", s = s, e = i });
    } else {
      local s = i;
      while (i < n && text[i] != ' ' && text[i] != '\t' &&
             text[i] != '\n' && text[i] != '\r' &&
             text[i] != '(' && text[i] != ')' && text[i] != ';' &&
             text[i] != '"') i += 1;
      toks.push({ k = "atom", s = s, e = i });
    }
  }
  return { toks = toks, err = null, at = 0 };
};

// _parse reads one tree node; a list keeps its items in source order. An
// unclosed list is text_unterminated (§16.9.4).
local _parse = null;
_parse = function(toks, i) {
  if (i >= toks.len()) return { err = "text_unterminated", at = -1 };
  local tk = toks[i];
  if (tk.k == ")") return { err = "text_syntax", at = tk.s };
  if (tk.k != "(") return { node = { t = "leaf", tok = tk }, next = i + 1, err = null };
  local items = [];
  local j = i + 1;
  while (j < toks.len() && toks[j].k != ")") {
    local r = _parse(toks, j);
    if (r.err != null) return r;
    items.push(r.node);
    j = r.next;
  }
  if (j >= toks.len()) return { err = "text_unterminated", at = tk.s };
  return { node = { t = "list", open = tk, close = toks[j], items = items }, next = j + 1, err = null };
};

// ---- scalar spelling (docs/SPEC-TABLES.md §16.3) ----
local _raw = function(text, tok) { return text.slice(tok.s, tok.e); };

local _unquote = function(s) {
  // s includes the quotes; return the decoded value or a marker string on a
  // malformed or unpaired-surrogate escape (text_encoding, §16.9.4).
  if (s.len() < 2 || s[0] != '"' || s[s.len() - 1] != '"') return null;
  local out = "";
  local i = 1;
  local n = s.len() - 1;
  while (i < n) {
    local c = s[i];
    if (c != '\\') { out += format("%c", c); i += 1; continue; }
    i += 1;
    if (i >= n) return null;
    local e = s[i];
    if (e == '"' || e == '\\' || e == '/') { out += format("%c", e); i += 1; }
    else if (e == 'b') { out += format("%c", 8); i += 1; }
    else if (e == 'f') { out += format("%c", 12); i += 1; }
    else if (e == 'n') { out += format("%c", 10); i += 1; }
    else if (e == 'r') { out += format("%c", 13); i += 1; }
    else if (e == 't') { out += format("%c", 9); i += 1; }
    else if (e == 'u') {
      if (i + 4 >= n) return null;
      local hex = s.slice(i + 1, i + 5);
      local v = 0;
      for (local k = 0; k < 4; k++) {
        local ch = hex[k];
        local d = -1;
        if (ch >= '0' && ch <= '9') d = ch - '0';
        else if (ch >= 'a' && ch <= 'f') d = ch - 'a' + 10;
        else if (ch >= 'A' && ch <= 'F') d = ch - 'A' + 10;
        if (d < 0) return null;
        v = v * 16 + d;
      }
      i += 5;
      if (v >= 0xD800 && v <= 0xDBFF) {
        // a high surrogate must be followed by a low surrogate
        if (i + 6 > n || s[i] != '\\' || s[i + 1] != 'u') return null;
        local hex2 = s.slice(i + 2, i + 6);
        local w = 0;
        for (local k = 0; k < 4; k++) {
          local ch = hex2[k];
          local d = -1;
          if (ch >= '0' && ch <= '9') d = ch - '0';
          else if (ch >= 'a' && ch <= 'f') d = ch - 'a' + 10;
          else if (ch >= 'A' && ch <= 'F') d = ch - 'A' + 10;
          if (d < 0) return null;
          w = w * 16 + d;
        }
        if (w < 0xDC00 || w > 0xDFFF) return null;
        i += 6;
      } else if (v >= 0xDC00 && v <= 0xDFFF) {
        return null;
      }
      out += "?";
    } else return null;
  }
  return out;
};

// _utf8len counts the bytes of a decoded string; the table form's str bound
// is bytes (SPEC §4.5, §16.9.1).
local _utf8len = function(s) { return s.len(); };

local _b64 = function(s) {
  local val = function(c) {
    if (c >= 'A' && c <= 'Z') return c - 'A';
    if (c >= 'a' && c <= 'z') return c - 'a' + 26;
    if (c >= '0' && c <= '9') return c - '0' + 52;
    if (c == '+') return 62;
    if (c == '/') return 63;
    return -1;
  };
  if (s.len() % 4 != 0) return null;
  local out = blob(0);
  local i = 0, n = s.len();
  while (i + 4 <= n) {
    local a = val(s[i]), b = val(s[i + 1]), c = val(s[i + 2]), d = val(s[i + 3]);
    if (a < 0 || b < 0) return null;
    local tail = i + 4 == n;
    if (s[i + 2] == '=') {
      if (!tail || s[i + 3] != '=' || (b & 0x0f) != 0) return null;
      out.writen((a << 2) | (b >> 4), 'b');
    } else if (s[i + 3] == '=') {
      if (!tail || c < 0 || (c & 0x03) != 0) return null;
      out.writen((a << 2) | (b >> 4), 'b');
      out.writen(((b & 0x0f) << 4) | (c >> 2), 'b');
    } else {
      if (c < 0 || d < 0) return null;
      out.writen((a << 2) | (b >> 4), 'b');
      out.writen(((b & 0x0f) << 4) | (c >> 2), 'b');
      out.writen(((c & 0x03) << 6) | d, 'b');
    }
    i += 4;
  }
  return out;
};

local _num = function(s) {
  // a JSON number spelling; the Squirrel table form's integer leaves accept
  // an exponent only when the value is integral (§16.3).
  local n = s.len();
  if (n == 0) return null;
  local i = 0;
  if (s[i] == '-' || s[i] == '+') i += 1;
  local digits = 0;
  while (i < n && s[i] >= '0' && s[i] <= '9') { i += 1; digits += 1; }
  if (digits == 0) return null;
  local frac = false;
  if (i < n && s[i] == '.') {
    frac = true; i += 1;
    local f = 0;
    while (i < n && s[i] >= '0' && s[i] <= '9') { i += 1; f += 1; }
    if (f == 0) return null;
  }
  local exp = false;
  if (i < n && (s[i] == 'e' || s[i] == 'E')) {
    exp = true; i += 1;
    if (i < n && (s[i] == '-' || s[i] == '+')) i += 1;
    local x = 0;
    while (i < n && s[i] >= '0' && s[i] <= '9') { i += 1; x += 1; }
    if (x == 0) return null;
  }
  if (i != n) return null;
  return { dot = frac, frac = frac || exp, value = s.tofloat() };
};

local _isnum = function(s) { return _num(s) != null; };

local _key = function(text, tok) {
  if (tok.k == "atom") {
    local r = _raw(text, tok);
    if (r.len() > 0 && r[0] == ':') return r.slice(1, r.len());
    return null;
  }
  if (tok.k == "str") return _unquote(_raw(text, tok));
  return null;
};

// ---- the resolver (docs/SPEC-TABLES.md §16.9.1, §16.9.2) ----
local _ctx = null;

local _fail = function(name, at) {
  if (_ctx.err == null) { _ctx.err = name; _ctx.at = at < 0 ? 0 : at; }
};

local _descriptor = function(T, name) {
  if (name == null) return null;
  if (name in T.Descriptors) return T.Descriptors[name];
  local key = name + "Descriptor";
  if (key in T) return T[key];
  return null;
};

local _field = function(desc, name) {
  foreach (f in desc.fields) if (f.name == name || f.key == name) return f;
  return null;
};

// _scalar validates one leaf against a field descriptor and records its span
// for a later edit (§16.9.3).
local _scalar = function(text, tok, fd, path) {
  local raw = _raw(text, tok);
  local shape = fd.shape;
  if (tok.k == "atom" && raw == "null") {
    if (fd.optional) return true;
    _fail("text_kind_mismatch", tok.s);
    return false;
  }
  if (shape == "str" || (shape == "bytes")) {
    if (tok.k != "str") { _fail("text_syntax", tok.s); return false; }
    local s = _unquote(raw);
    if (s == null) { _fail("text_encoding", tok.s); return false; }
    if (shape == "bytes") {
      local b = _b64(s);
      if (b == null) { _fail("text_encoding", tok.s); return false; }
      if (fd.bound > 0 && b.len() > fd.bound) { _fail("text_bound", tok.s); return false; }
    } else if (fd.bound > 0 && _utf8len(s) > fd.bound) {
      _fail("text_bound", tok.s);
      return false;
    }
    _ctx.spans[path] <- { s = tok.s, e = tok.e, fd = fd };
    return true;
  }
  if (shape == "bool") {
    if (tok.k == "atom" && (raw == "true" || raw == "false")) {
      _ctx.spans[path] <- { s = tok.s, e = tok.e, fd = fd };
      return true;
    }
    _fail("text_kind_mismatch", tok.s);
    return false;
  }
  if (shape == "enum") {
    local name = null;
    if (tok.k == "atom" && raw.len() > 0 && raw[0] == ':') name = raw.slice(1, raw.len());
    else if (tok.k == "str") name = _unquote(raw);
    if (name == null) { _fail("text_kind_mismatch", tok.s); return false; }
    local found = false;
    foreach (v in fd.variants) if (v[0] == name) found = true;
    if (!found) { _fail("text_unknown_variant", tok.s); return false; }
    _ctx.spans[path] <- { s = tok.s, e = tok.e, fd = fd };
    return true;
  }
  if (shape == "flags") {
    if (tok.t != "list") { _fail("text_kind_mismatch", tok.open.s); return false; }
    local seen = {};
    foreach (item in tok.items) {
      if (item.t != "leaf") { _fail("text_kind_mismatch", tok.open.s); return false; }
      local name = _key(text, item.tok);
      if (name == null) { _fail("text_kind_mismatch", item.tok.s); return false; }
      if (name in seen) { _fail("text_duplicate_key", item.tok.s); return false; }
      seen[name] <- true;
      local found = false;
      if ("flagNames" in fd) foreach (v in fd.flagNames) if (v == name) found = true;
      if (!found) { _fail("text_unknown_variant", item.tok.s); return false; }
    }
    return true;
  }
  if (shape == "union") {
    if (tok.t != "list") { _fail("text_kind_mismatch", tok.open.s); return false; }
    local pairs = [];
    local i = 0;
    while (i < tok.items.len()) {
      if (i + 1 >= tok.items.len() || tok.items[i].t != "leaf") { _fail("text_kind_mismatch", tok.open.s); return false; }
      local arm = _key(text, tok.items[i].tok);
      pairs.push({ name = arm, val = tok.items[i + 1], tok = tok.items[i].tok });
      i += 2;
    }
    if (pairs.len() != 1) { _fail("text_kind_mismatch", tok.open.s); return false; }
    local ud = _descriptor(_ctx.T, fd.table);
    local arm = null;
    if (ud != null && pairs[0].name != null) foreach (a in ud.arms) if (a.name == pairs[0].name) arm = a;
    if (arm == null) { _fail("text_unknown_variant", pairs[0].tok.s); return false; }
    if (arm.payload != null) {
      if (!_value(text, pairs[0].val, arm.payload, path)) return false;
    }
    return true;
  }
  if (shape == "int" || shape == "float") {
    if (fd.kind == 36) {
      // timestamp: the leaf is an RFC 3339 instant, never a number (§16.5)
      _fail("text_timestamp", tok.s);
      return false;
    }
    if (tok.k != "atom") { _fail("text_kind_mismatch", tok.s); return false; }
    local num = _num(raw);
    if (num == null) { _fail("text_kind_mismatch", tok.s); return false; }
    if (shape == "int") {
      if (num.dot) {
        if (!("frac" in fd)) { _fail("text_kind_mismatch", tok.s); return false; }
        // a fixed fraction must be exact in its scale (§16.3)
        local scaled = num.value * (1 << fd.frac);
        if (scaled != floor(scaled)) { _fail("text_kind_mismatch", tok.s); return false; }
      }
      local v = num.value;
      if (fd.lo != null) {
        if (typeof fd.lo == "array") {
          // 128-bit bounds are compared only when the value fits a Squirrel int
        } else if (v < fd.lo) { _fail("text_bound", tok.s); return false; }
      }
      if (fd.hi != null && typeof fd.hi != "array") {
        if (v > fd.hi) { _fail("text_bound", tok.s); return false; }
      }
    }
    _ctx.spans[path] <- { s = tok.s, e = tok.e, fd = fd };
    return true;
  }
  _fail("text_kind_mismatch", tok.s);
  return false;
};

local _value = null;
local _record = null;

// _multi validates the list-shaped leaves: flags (§16.4) and union arms
// (§2.6, §16.9.1).
local _multi = function(text, node, fd, path) {
  if (fd.shape == "flags") {
    if (node.t != "list") { _fail("text_kind_mismatch", node.open.s); return false; }
    local seen = {};
    foreach (item in node.items) {
      if (item.t != "leaf") { _fail("text_kind_mismatch", node.open.s); return false; }
      local name = _key(text, item.tok);
      if (name == null) { _fail("text_kind_mismatch", item.tok.s); return false; }
      if (name in seen) { _fail("text_duplicate_key", item.tok.s); return false; }
      seen[name] <- true;
      local found = false;
      if ("flagNames" in fd) foreach (v in fd.flagNames) if (v == name) found = true;
      if (!found) { _fail("text_unknown_variant", item.tok.s); return false; }
    }
    return true;
  }
  if (node.t != "list") { _fail("text_kind_mismatch", node.open.s); return false; }
  local pairs = [];
  local i = 0;
  while (i < node.items.len()) {
    if (i + 1 >= node.items.len() || node.items[i].t != "leaf") { _fail("text_kind_mismatch", node.open.s); return false; }
    local arm = _key(text, node.items[i].tok);
    pairs.push({ name = arm, val = node.items[i + 1], tok = node.items[i].tok });
    i += 2;
  }
  if (pairs.len() != 1) { _fail("text_kind_mismatch", node.open.s); return false; }
  local ud = _descriptor(_ctx.T, fd.table);
  local arm = null;
  if (ud != null && pairs[0].name != null) foreach (a in ud.arms) if (a.name == pairs[0].name) arm = a;
  if (arm == null) { _fail("text_unknown_variant", pairs[0].tok.s); return false; }
  if (arm.payload != null) {
    if (!_value(text, pairs[0].val, arm.payload, path)) return false;
  }
  return true;
};

// _value validates one value node against a field descriptor.
_value = function(text, node, fd, path) {
  if (node.t == "leaf" && _raw(text, node.tok) == "null" && fd.optional) return true;
  if (fd.shape == "arr") {
    if (node.t != "list") { _fail("text_kind_mismatch", node.t == "leaf" ? node.tok.s : node.open.s); return false; }
    local count = node.items.len();
    if (fd.counted && fd.bound > 0 && count > fd.bound) { _fail("text_bound", node.open.s); return false; }
    if (fd.list && fd.min > 0 && count < fd.min) { _fail("text_bound", node.open.s); return false; }
    if (!fd.counted && fd.bound > 0 && count != fd.bound) { _fail("text_bound", node.open.s); return false; }
    if (fd.elem == null) { _fail("text_kind_mismatch", node.open.s); return false; }
    for (local i = 0; i < count; i++) {
      if (!_value(text, node.items[i], fd.elem, path + "[" + i + "]")) return false;
    }
    return true;
  }
  if (fd.shape == "tab" || fd.shape == "ptr" || fd.shape == "sblob" || fd.shape == "bblob") {
    local target = fd.table;
    if (fd.shape == "sblob" || fd.shape == "bblob") {
      return _scalar(text, node.t == "leaf" ? node.tok : node.open, fd, path);
    }
    local td = _descriptor(_ctx.T, target);
    if (td == null) { _fail("text_kind_mismatch", node.t == "leaf" ? node.tok.s : node.open.s); return false; }
    return _record(text, node, td, path);
  }
  if (fd.shape == "flags" || fd.shape == "union") return _multi(text, node, fd, path);
  if (node.t != "leaf") { _fail("text_kind_mismatch", node.open.s); return false; }
  return _scalar(text, node.tok, fd, path);
};

// _record validates a keyword record against a table descriptor; keys are
// unique after aliasing (§16.9.4 text_unknown_key, text_duplicate_key).
_record = function(text, node, desc, path) {
  if (node.t != "list") { _fail("text_reference", node.t == "leaf" ? node.tok.s : node.open.s); return false; }
  local seen = {};
  local i = 0;
  while (i < node.items.len()) {
    local kt = node.items[i];
    if (kt.t != "leaf") { _fail("text_syntax", kt.open.s); return false; }
    local raw = _raw(text, kt.tok);
    if (kt.tok.k == "str" && raw.len() > 1 && raw[1] == '&') { _fail("text_reference", kt.tok.s); return false; }
    local name = _key(text, kt.tok);
    if (name == null) { _fail("text_syntax", kt.tok.s); return false; }
    if (name.len() > 0 && name[0] == '&') { _fail("text_reference", kt.tok.s); return false; }
    local fd = _field(desc, name);
    if (fd == null) { _fail("text_unknown_key", kt.tok.s); return false; }
    if (i + 1 >= node.items.len()) { _fail("text_unterminated", kt.tok.s); return false; }
    if (fd.name in seen) { _fail("text_duplicate_key", kt.tok.s); return false; }
    seen[fd.name] <- true;
    local sub = path == "" ? fd.name : path + "." + fd.name;
    if (!_value(text, node.items[i + 1], fd, sub)) return false;
    i += 2;
  }
  if (i != node.items.len()) { _fail("text_syntax", node.close.s); return false; }
  return true;
};

// _read parses and validates a whole document; it returns the document record
// or sets _ctx.err.
local _read = function(T, rootName, text) {
  _ctx = { T = T, text = text, err = null, at = 0, spans = {} };
  local lex = _lex(text);
  if (lex.err != null) { _ctx.err = lex.err; _ctx.at = lex.at; return null; }
  if (lex.toks.len() == 0) { _ctx.err = "text_syntax"; return null; }
  local r = _parse(lex.toks, 0);
  if (r.err != null) { _ctx.err = r.err; _ctx.at = r.at; return null; }
  if (r.next != lex.toks.len()) { _ctx.err = "text_syntax"; _ctx.at = lex.toks[r.next].s; return null; }
  local desc = _descriptor(T, rootName);
  if (desc == null) { _ctx.err = "text_reference"; return null; }
  if (!_record(text, r.node, desc, "")) return null;
  local doc = _Doc();
  doc.bytes = text;
  doc.spans = _ctx.spans;
  return doc;
};

// ---- the surface ----
// LoadTextForm(T, rootName, text, limits, report) returns the document (the
// typed value is read through the table loader beside it) or null, setting
// report.refused and report.name on refusal (§16.9.4).
_M.LoadTextForm <- function(T, rootName, text, limits, report) {
  if (!("refused" in report)) report.refused <- false;
  if (!("name" in report)) report.name <- null;
  if (!("offset" in report)) report.offset <- 0;
  if (!("path" in report)) report.path <- null;
  report.refused = false;
  report.name = null;
  if (text == null) { report.refused = true; report.name = "text_syntax"; return null; }
  local src = _asString(text);
  if (limits != null && ("bytes" in limits) && src.len() > limits.bytes) {
    report.refused = true; report.name = "text_limit"; return null;
  }
  local doc = _read(T, rootName, src);
  if (doc == null) {
    report.refused = true;
    report.name = _ctx.err;
    report.offset = _ctx.at;
    return null;
  }
  return doc;
};

// SaveTextForm(T, rootName, doc) returns the retained source bytes.
_M.SaveTextForm <- function(T, rootName, doc) {
  if (doc == null) return null;
  return doc.bytes;
};

// _lexOne reads exactly one leaf token from an edit's spelling (§16.9.3).
local _lexOne = function(text) {
  local lex = _lex(text);
  if (lex.err != null || lex.toks.len() != 1) return null;
  return lex.toks[0];
};

// EditTextForm(T, rootName, doc, edits, limits, report) applies leaf edits by
// original span. A compound, absent, duplicated or overlapping target is
// text_edit_conflict; a bound or kind violation keeps the checker's name.
_M.EditTextForm <- function(T, rootName, doc, edits, limits, report) {
  if (!("refused" in report)) report.refused <- false;
  if (!("name" in report)) report.name <- null;
  if (!("offset" in report)) report.offset <- 0;
  report.refused = false;
  report.name = null;
  if (doc == null) { report.refused = true; report.name = "text_edit_conflict"; return null; }
  local text = doc.bytes;
  local targets = [];
  local used = {};
  foreach (ed in edits) {
    if (!("path" in ed) || !("text" in ed) || !(ed.path in doc.spans)) {
      report.refused = true; report.name = "text_edit_conflict"; return null;
    }
    if (ed.path in used) { report.refused = true; report.name = "text_edit_conflict"; return null; }
    used[ed.path] <- true;
    local sp = doc.spans[ed.path];
    local tok = _lexOne(ed.text);
    if (tok == null || tok.k == "(" || tok.k == ")") {
      report.refused = true; report.name = "text_edit_conflict"; return null;
    }
    // the new spelling is a leaf for this field; validate it in isolation
    local saved = _ctx;
    _ctx = { T = T, text = ed.text, err = null, at = 0, spans = {} };
    local ok = _scalar(ed.text, tok, sp.fd, ed.path);
    local err = _ctx.err;
    _ctx = saved;
    if (!ok) { report.refused = true; report.name = err; return null; }
    // a semantic no-op keeps the old spelling (layout count 2e0, §16.9.3)
    local old = text.slice(sp.s, sp.e);
    local same = old == ed.text;
    if (!same && sp.fd.shape == "int") {
      local a = _num(old), b = _num(ed.text);
      if (a != null && b != null && a.value == b.value) same = true;
    }
    if (!same) targets.push({ s = sp.s, e = sp.e, text = ed.text });
  }
  // disjoint by ORIGINAL span; overlap is a conflict
  for (local i = 0; i < targets.len(); i++) {
    for (local j = i + 1; j < targets.len(); j++) {
      if (targets[j].s < targets[i].s) { local t = targets[i]; targets[i] = targets[j]; targets[j] = t; }
    }
  }
  for (local i = 1; i < targets.len(); i++) {
    if (targets[i].s < targets[i - 1].e) {
      report.refused = true; report.name = "text_edit_conflict"; return null;
    }
  }
  local out = "";
  local at = 0;
  foreach (t in targets) {
    out += text.slice(at, t.s) + t.text;
    at = t.e;
  }
  out += text.slice(at, text.len());
  return _read(T, rootName, out);
};
