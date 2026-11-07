// THE HAND-EDITABLE TEXT FORM (docs/SPEC-TABLES.md §16.9): a table with its
// document, the strict authoring form for fixed and variable tables alike.
// The Squirrel leg of internal/codegen/gotable/textform.go, rule for rule.
//
// It rides on the JSON text form (§16), and that is the design rather than a
// shortcut. The reader resolves every value against the descriptors (§8.1)
// STRICTLY, refusing by name everything §16.9.4 names, and transcodes the
// accepted document into the canonical JSON of the same value; the JSON walk
// (schema_table_json, FromJson) then lands it. Every place §16.2 would clamp,
// skip or last-wins was refused before the walk ran, so the walk's report is
// zero on every accepted document, and a nonzero report is refused rather
// than trusted. The document keeps the source bytes, so a rewrite is the
// source and a leaf edit replaces one token span (§16.9.3).
//
//   _M.DefaultLimits()                         the contract's limits (§16.9.4)
//   _M.LoadTextForm(T, root, text, limits, report)
//       the document, its typed value at doc.value, or null with report.name
//       the stable refusal name, report.offset and report.path (§16.9.4)
//   _M.SaveTextForm(T, root, doc)              the retained bytes, a blob
//   _M.EditTextForm(T, root, doc, edits, report)
//       a NEW document from leaf edits [{ path, text }] by original span
//       (§16.9.3), or null with the refusal in report; doc is untouched
//
// A document answers Bytes(), Comments() ([{ offset, text, path }] in source
// order, §16.9.3's attachment), Span(path) ([start, end] or null), Paths()
// and Scalar(path). T is a unit's table module; text is a string or a blob.
local _M = vargv[0];
local _require = vargv[1];
local _json = _require("schema_table_json");

// The stable refusal names (§16.9.4).
local UNKNOWN_KEY = "text_unknown_key";
local DUPLICATE_KEY = "text_duplicate_key";
local BOUND = "text_bound";
local UNTERMINATED = "text_unterminated";
local SYNTAX = "text_syntax";
local KIND_MISMATCH = "text_kind_mismatch";
local UNKNOWN_VARIANT = "text_unknown_variant";
local ENCODING = "text_encoding";
local TIMESTAMP = "text_timestamp";
local MISSING_REQUIRED = "text_missing_required";
local INACTIVE_FIELD = "text_inactive_field";
local REFERENCE = "text_reference";
local LIMIT = "text_limit";
local EDIT_CONFLICT = "text_edit_conflict";

_M.Names <- [UNKNOWN_KEY, DUPLICATE_KEY, BOUND, UNTERMINATED, SYNTAX, KIND_MISMATCH,
  UNKNOWN_VARIANT, ENCODING, TIMESTAMP, MISSING_REQUIRED, INACTIVE_FIELD, REFERENCE,
  LIMIT, EDIT_CONFLICT];

// §16.9.4's depth cap; a caller's depth may lower it.
local MAX_DEPTH = 128;

// The conformance contract's limits: the document's bytes, its tokens (atoms
// and parentheses), its nodes (lists), the bytes its strings decode to, and
// its list depth (§16.9.4).
_M.DefaultLimits <- function() {
  return { bytes = 65536, tokens = 8192, nodes = 4096, allocation = 65536, depth = MAX_DEPTH };
};

// resolved: a missing, zero or negative limit takes the default; depth never
// exceeds 128.
local resolved = function(l) {
  local d = _M.DefaultLimits();
  if (l == null) return d;
  local out = {};
  foreach (k, v in d) {
    local x = (k in l) ? l[k] : null;
    out[k] <- (x == null || x <= 0) ? v : x;
  }
  if (out.depth > MAX_DEPTH) out.depth = MAX_DEPTH;
  return out;
};

local has = function(d, k) { return (k in d) && d[k] != null; };

// ---- BYTES AND STRINGS ------------------------------------------------------

local CH = array(256);
for (local i = 0; i < 256; i++) CH[i] = format("%c", i);

local join = function(parts) {
  if (parts.len() == 0) return "";
  local a = parts;
  while (a.len() > 1) {
    local b = array((a.len() + 1) / 2);
    for (local i = 0; i < b.len(); i++) {
      b[i] = (2 * i + 1 < a.len()) ? a[2 * i] + a[2 * i + 1] : a[2 * i];
    }
    a = b;
  }
  return a[0];
};

// the bytes of src[from, to) as a string
local slice = function(src, from, to) {
  local parts = array(to - from);
  for (local i = from; i < to; i++) parts[i - from] = CH[src[i] & 0xff];
  return join(parts);
};

// a string or blob as a fresh blob of its bytes
local blobOf = function(x) {
  local b = blob(x.len());
  for (local i = 0; i < x.len(); i++) b[i] = x[i] & 0xff;
  return b;
};

local put = function(out, s) { foreach (c in s) out.writen(c & 0xff, 'b'); };

// the length of ONE well-formed UTF-8 sequence at s[i] (Go's
// utf8.DecodeRune: no overlong form, no surrogate, nothing past U+10FFFF),
// or -1
local utf8Len = function(s, i, n) {
  local c = s[i] & 0xff;
  if (c < 0x80) return 1;
  local need = 0, lo = 0x80, hi = 0xbf;
  if (c >= 0xc2 && c <= 0xdf) need = 1;
  else if (c == 0xe0) { need = 2; lo = 0xa0; }
  else if (c >= 0xe1 && c <= 0xec) need = 2;
  else if (c == 0xed) { need = 2; hi = 0x9f; }
  else if (c >= 0xee && c <= 0xef) need = 2;
  else if (c == 0xf0) { need = 3; lo = 0x90; }
  else if (c >= 0xf1 && c <= 0xf3) need = 3;
  else if (c == 0xf4) { need = 3; hi = 0x8f; }
  else return -1;
  if (i + need >= n) return -1;
  for (local k = 1; k <= need; k++) {
    local b = s[i + k] & 0xff;
    if (k == 1 ? (b < lo || b > hi) : (b < 0x80 || b > 0xbf)) return -1;
  }
  return need + 1;
};

// the UTF-8 bytes of code point r, as a string
local utf8Of = function(r) {
  if (r < 0x80) return CH[r];
  if (r < 0x800) return CH[0xc0 | (r >> 6)] + CH[0x80 | (r & 0x3f)];
  if (r < 0x10000) return CH[0xe0 | (r >> 12)] + CH[0x80 | ((r >> 6) & 0x3f)] + CH[0x80 | (r & 0x3f)];
  return CH[0xf0 | (r >> 18)] + CH[0x80 | ((r >> 12) & 0x3f)] + CH[0x80 | ((r >> 6) & 0x3f)] + CH[0x80 | (r & 0x3f)];
};

local isDigit = function(c) { return c >= '0' && c <= '9'; };
local isLetter = function(c) { return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || c == '_'; };

local hexOf = function(s, from, to) {
  local v = 0;
  for (local i = from; i < to; i++) {
    local c = s[i] & 0xff;
    if (c >= '0' && c <= '9') v = (v << 4) | (c - '0');
    else if (c >= 'a' && c <= 'f') v = (v << 4) | (c - 'a' + 10);
    else if (c >= 'A' && c <= 'F') v = (v << 4) | (c - 'A' + 10);
    else return -1;
  }
  return v;
};

// ---- EXACT INTEGERS (the JSON walk's wide.go port, json.nut) -----------------
// A magnitude is little-endian limbs of 16 bits; a value is [negative, limbs].

local MIN = 1 << 63;
local bnNorm = function(a) { while (a.len() > 0 && a[a.len() - 1] == 0) a.pop(); return a; };
local bnZero = function(a) { return a.len() == 0; };
local bnMulSmall = function(a, m, add) {
  local out = [];
  local carry = add;
  foreach (x in a) { local t = x * m + carry; out.append(t & 0xffff); carry = t >> 16; }
  while (carry > 0) { out.append(carry & 0xffff); carry = carry >> 16; }
  return bnNorm(out);
};
local bnDivSmall = function(a, m) {
  local q = array(a.len(), 0);
  local r = 0;
  for (local i = a.len() - 1; i >= 0; i--) {
    local t = (r << 16) | a[i];
    q[i] = t / m;
    r = t % m;
  }
  return [bnNorm(q), r];
};
local bnShl = function(a, n) {
  local out = a;
  while (n >= 16) { local t = [0]; t.extend(out); out = t; n -= 16; }
  if (n > 0) out = bnMulSmall(out, 1 << n, 0);
  return bnNorm(out);
};
local bnCmp = function(a, b) {
  if (a.len() != b.len()) return a.len() < b.len() ? -1 : 1;
  for (local i = a.len() - 1; i >= 0; i--) if (a[i] != b[i]) return a[i] < b[i] ? -1 : 1;
  return 0;
};
local bnSubOne = function(a) {
  local out = clone a;
  for (local i = 0; i < out.len(); i++) {
    if (out[i] > 0) { out[i] -= 1; break; }
    out[i] = 0xffff;
  }
  return bnNorm(out);
};
local svCmp = function(x, y) {
  local xn = x[0] && !bnZero(x[1]), yn = y[0] && !bnZero(y[1]);
  if (xn != yn) return xn ? -1 : 1;
  local c = bnCmp(x[1], y[1]);
  return xn ? -c : c;
};
local bnFromU64 = function(v) {
  local out = [];
  for (local i = 0; i < 4; i++) { out.append(v & 0xffff); v = v >>> 16; }
  return bnNorm(out);
};
local bnFromPair = function(lo, hi) {
  local m = [];
  for (local i = 0; i < 4; i++) { m.append(lo & 0xffff); lo = lo >>> 16; }
  for (local i = 0; i < 4; i++) { m.append(hi & 0xffff); hi = hi >>> 16; }
  return bnNorm(m);
};
local svFromPair = function(p, signed) {
  local lo = p[0], hi = p[1];
  if (signed && hi < 0) return [true, bnFromPair(~lo + 1, ~hi + (lo == 0 ? 1 : 0))];
  return [false, bnFromPair(lo, hi)];
};
local svFromInt = function(v, signed) {
  if (signed && v < 0) return [true, bnFromU64(v == MIN ? MIN : -v)];
  return [false, bnFromU64(v)];
};
local bnDecimal = function(a) {
  if (bnZero(a)) return "0";
  local parts = [];
  local m = a;
  while (!bnZero(m)) {
    local qr = bnDivSmall(m, 10000);
    m = qr[0];
    local s = qr[1].tostring();
    if (!bnZero(m)) while (s.len() < 4) s = "0" + s;
    parts.insert(0, s);
  }
  return join(parts);
};
local svDecimal = function(x) {
  return (x[0] && !bnZero(x[1]) ? "-" : "") + bnDecimal(x[1]);
};
// 2^n as a magnitude
local bnPow2 = function(n) { return bnShl([1], n); };

// ---- TIMESTAMPS (docs/SPEC-TABLES.md §2.11) ---------------------------------
// RFC 3339 §5.6's date-time exactly, as the JSON walk and the engine read it:
// lower-case t and z are accepted, the leap second is refused, the fraction is
// truncated to nanoseconds, and the instant must be an int64 nanosecond count.

local daysFromCivil = function(y, m, d) {
  y -= m <= 2 ? 1 : 0;
  local era = (y >= 0 ? y : y - 399) / 400;
  local yoe = y - era * 400;
  local doy = (153 * (m + (m > 2 ? -3 : 9)) + 2) / 5 + d - 1;
  local doe = yoe * 365 + yoe / 4 - yoe / 100 + doy;
  return era * 146097 + doe - 719468;
};

local rfc3339 = function(s) {
  local n = s.len();
  local two = function(at) {
    if (at + 2 > n) return -1;
    local a = s[at] & 0xff, b = s[at + 1] & 0xff;
    if (!isDigit(a) || !isDigit(b)) return -1;
    return (a - '0') * 10 + (b - '0');
  };
  if (n < 20) return null;
  if (s[4] != '-' || s[7] != '-' || s[13] != ':' || s[16] != ':') return null;
  if (s[10] != 'T' && s[10] != 't') return null;
  local year = 0;
  for (local i = 0; i < 4; i++) {
    local c = s[i] & 0xff;
    if (!isDigit(c)) return null;
    year = year * 10 + (c - '0');
  }
  local month = two(5), day = two(8), hour = two(11), minute = two(14), second = two(17);
  if (month < 0 || day < 0 || hour < 0 || minute < 0 || second < 0) return null;
  if (month < 1 || month > 12) return null;
  local mdays = [31, 28, 31, 30, 31, 30, 31, 31, 30, 31, 30, 31];
  local leap = (year % 4 == 0 && year % 100 != 0) || year % 400 == 0;
  if (day < 1 || day > mdays[month - 1] + (month == 2 && leap ? 1 : 0)) return null;
  if (hour > 23 || minute > 59 || second > 59) return null;
  local at = 19, nanos = 0;
  if (at < n && s[at] == '.') {
    at++;
    local digits = 0;
    while (at < n && isDigit(s[at] & 0xff)) {
      if (digits < 9) nanos = nanos * 10 + ((s[at] & 0xff) - '0');
      digits++;
      at++;
    }
    if (digits == 0) return null;
    for (; digits < 9; digits++) nanos *= 10;
  }
  if (at >= n) return null;
  local offset = 0;
  local c = s[at];
  if (c == 'Z' || c == 'z') {
    if (at + 1 != n) return null;
  } else if (c == '+' || c == '-') {
    local oh = two(at + 1);
    if (oh < 0 || at + 3 >= n || s[at + 3] != ':') return null;
    local om = two(at + 4);
    if (om < 0 || oh > 23 || om > 59 || at + 6 != n) return null;
    offset = (oh * 3600 + om * 60) * (c == '-' ? -1 : 1);
  } else {
    return null;
  }
  local seconds = daysFromCivil(year, month, day) * 86400 + hour * 3600 + minute * 60 + second - offset;
  if (seconds < -9223372037 || seconds > 9223372036) return null;
  if (seconds == -9223372037 && nanos < 145224192) return null;
  if (seconds == 9223372036 && nanos > 854775807) return null;
  return seconds * 1000000000 + nanos;
};

// ---- THE TOKEN TREE (§16.9.1) -----------------------------------------------
// A node is a table: kind '(' list, '"' string, '0' number, ':' keyword,
// 't' true, 'f' false, 'z' null; start and end (exclusive) byte offsets;
// items (a list's); text (a string decoded, a keyword's name, a number's
// token); path and pathed (the value's path once resolved); pairs (resolved as
// key/value pairs); canon (a resolved scalar's semantic value, for a no-op
// edit); comments (the spans of the comments directly inside a list).

local newNode = function(kind, start, end, text) {
  return { kind = kind, start = start, end = end, items = [], text = text,
    path = "", pathed = false, pairs = false, canon = null, comments = [] };
};

// newParser: p.err is the first refusal, [name, offset]
local newParser = function(src, limits) {
  return { src = src, n = src.len(), pos = 0, limits = limits, tokens = 0, nodes = 0,
    alloc = 0, cur = null, outside = [], err = null };
};

local fail = function(p, name, at) {
  if (p.err == null) p.err = [name, at];
  return false;
};

local token = function(p, at) {
  p.tokens++;
  if (p.tokens > p.limits.tokens) return fail(p, LIMIT, at);
  return true;
};

// gap skips SP, TAB, LF, CRLF and line comments (§16.9.1); a bare CR is
// syntax, and a comment's bytes are well-formed UTF-8 or encoding.
local gap = function(p) {
  local src = p.src, n = p.n;
  while (p.pos < n) {
    local c = src[p.pos] & 0xff;
    if (c == ' ' || c == '\t' || c == '\n') {
      p.pos++;
    } else if (c == '\r') {
      if (p.pos + 1 < n && (src[p.pos + 1] & 0xff) == '\n') { p.pos += 2; continue; }
      return fail(p, SYNTAX, p.pos);
    } else if (c == ';') {
      local start = p.pos;
      p.pos++;
      while (p.pos < n) {
        local d = src[p.pos] & 0xff;
        if (d == '\n') break;
        if (d == '\r') {
          if (p.pos + 1 < n && (src[p.pos + 1] & 0xff) == '\n') break;
          return fail(p, SYNTAX, p.pos);
        }
        if (d < 0x80) { p.pos++; continue; }
        local k = utf8Len(src, p.pos, n);
        if (k < 0) return fail(p, ENCODING, p.pos);
        p.pos += k;
      }
      local span = [start, p.pos];
      if (p.cur != null) p.cur.comments.append(span);
      else p.outside.append(span);
    } else {
      return true;
    }
  }
  return true;
};

// an atom ends at a gap, a parenthesis, a quote, a comment or EOF
local delimited = function(p) {
  if (p.pos >= p.n) return true;
  switch (p.src[p.pos] & 0xff) {
    case ' ': case '\t': case '\n': case '\r': case ';': case '(': case ')': case '"':
      return true;
  }
  return fail(p, SYNTAX, p.pos);
};

// number is RFC 8259's number grammar, and nothing looser.
local number = function(p) {
  local src = p.src, n = p.n;
  if ((src[p.pos] & 0xff) == '-') p.pos++;
  if (p.pos >= n || !isDigit(src[p.pos] & 0xff)) return fail(p, SYNTAX, p.pos);
  if ((src[p.pos] & 0xff) == '0') p.pos++;
  else while (p.pos < n && isDigit(src[p.pos] & 0xff)) p.pos++;
  if (p.pos < n && (src[p.pos] & 0xff) == '.') {
    p.pos++;
    if (p.pos >= n || !isDigit(src[p.pos] & 0xff)) return fail(p, SYNTAX, p.pos);
    while (p.pos < n && isDigit(src[p.pos] & 0xff)) p.pos++;
  }
  if (p.pos < n && ((src[p.pos] & 0xff) == 'e' || (src[p.pos] & 0xff) == 'E')) {
    p.pos++;
    if (p.pos < n && ((src[p.pos] & 0xff) == '+' || (src[p.pos] & 0xff) == '-')) p.pos++;
    if (p.pos >= n || !isDigit(src[p.pos] & 0xff)) return fail(p, SYNTAX, p.pos);
    while (p.pos < n && isDigit(src[p.pos] & 0xff)) p.pos++;
  }
  return true;
};

// str is an RFC 8259 string, decoded (§16.9.1): a raw control byte is syntax,
// an invalid escape, ill-formed UTF-8 or an unpaired surrogate is encoding,
// and EOF inside the string or an escape is unterminated. Answers the decoded
// bytes as a string, or null.
local str = function(p) {
  local src = p.src, n = p.n;
  local at = p.pos;
  local i = at + 1;
  local parts = [];
  local size = 0;
  while (true) {
    if (i >= n) { fail(p, UNTERMINATED, n); return null; }
    local c = src[i] & 0xff;
    if (c == '"') {
      p.pos = i + 1;
      p.alloc += size;
      if (p.alloc > p.limits.allocation) { fail(p, LIMIT, at); return null; }
      return join(parts);
    } else if (c == '\\') {
      if (i + 1 >= n) { fail(p, UNTERMINATED, n); return null; }
      local e = src[i + 1] & 0xff;
      local one = null;
      switch (e) {
        case '"': case '\\': case '/': one = CH[e]; break;
        case 'b': one = CH[8]; break;
        case 'f': one = CH[12]; break;
        case 'n': one = CH[10]; break;
        case 'r': one = CH[13]; break;
        case 't': one = CH[9]; break;
      }
      if (one != null) { parts.append(one); size++; i += 2; continue; }
      if (e != 'u') { fail(p, ENCODING, i); return null; }
      if (i + 6 > n) {
        if (hexOf(src, i + 2, n) >= 0) { fail(p, UNTERMINATED, n); return null; }
        fail(p, ENCODING, i);
        return null;
      }
      local r = hexOf(src, i + 2, i + 6);
      if (r < 0) { fail(p, ENCODING, i); return null; }
      local next = i + 6;
      if (r >= 0xD800 && r <= 0xDBFF) {
        if (next + 6 > n || (src[next] & 0xff) != '\\' || (src[next + 1] & 0xff) != 'u') { fail(p, ENCODING, i); return null; }
        local low = hexOf(src, next + 2, next + 6);
        if (low < 0xDC00 || low > 0xDFFF) { fail(p, ENCODING, i); return null; }
        r = 0x10000 + ((r - 0xD800) << 10) + (low - 0xDC00);
        next += 6;
      } else if (r >= 0xDC00 && r <= 0xDFFF) {
        fail(p, ENCODING, i);
        return null;
      }
      local u = utf8Of(r);
      parts.append(u);
      size += u.len();
      i = next;
    } else if (c < 0x20) {
      // a raw newline or control byte: strings escape them (§16.9.1)
      fail(p, SYNTAX, i);
      return null;
    } else if (c < 0x80) {
      parts.append(CH[c]);
      size++;
      i++;
    } else {
      local k = utf8Len(src, i, n);
      if (k < 0) { fail(p, ENCODING, i); return null; }
      parts.append(slice(src, i, i + k));
      size += k;
      i += k;
    }
  }
};

local value = null;
value = function(p, depth) {
  local src = p.src, n = p.n;
  local start = p.pos;
  if (start >= n) { fail(p, UNTERMINATED, start); return null; }
  if (!token(p, start)) return null;
  local c = src[start] & 0xff;
  if (c == '(') {
    if (depth + 1 > p.limits.depth) { fail(p, LIMIT, start); return null; }
    p.nodes++;
    if (p.nodes > p.limits.nodes) { fail(p, LIMIT, start); return null; }
    local node = newNode('(', start, 0, null);
    p.pos++;
    local outer = p.cur;
    p.cur = node;
    while (true) {
      if (!gap(p)) return null;
      if (p.pos >= n) { fail(p, UNTERMINATED, p.pos); return null; }
      if ((src[p.pos] & 0xff) == ')') {
        if (!token(p, p.pos)) return null;
        p.pos++;
        break;
      }
      local item = value(p, depth + 1);
      if (item == null) return null;
      node.items.append(item);
    }
    p.cur = outer;
    node.end = p.pos;
    return node;
  }
  if (c == '"') {
    local text = str(p);
    if (text == null) return null;
    return newNode('"', start, p.pos, text);
  }
  if (c == ':') {
    p.pos++;
    if (p.pos >= n || !isLetter(src[p.pos] & 0xff)) { fail(p, SYNTAX, p.pos); return null; }
    while (p.pos < n) {
      local d = src[p.pos] & 0xff;
      if (!isLetter(d) && !isDigit(d) && d != '-' && d != '/' && d != '.') break;
      p.pos++;
    }
    if (!delimited(p)) return null;
    return newNode(':', start, p.pos, slice(src, start + 1, p.pos));
  }
  if (c == '-' || isDigit(c)) {
    if (!number(p) || !delimited(p)) return null;
    return newNode('0', start, p.pos, slice(src, start, p.pos));
  }
  if (isLetter(c)) {
    while (p.pos < n && (isLetter(src[p.pos] & 0xff) || isDigit(src[p.pos] & 0xff))) p.pos++;
    local word = slice(src, start, p.pos);
    local kind = 0;
    if (word == "true") kind = 't';
    else if (word == "false") kind = 'f';
    else if (word == "null") kind = 'z';
    else {
      // an unquoted symbol is not data in this form (§16.9.1)
      fail(p, SYNTAX, start);
      return null;
    }
    if (!delimited(p)) return null;
    return newNode(kind, start, p.pos, null);
  }
  // a comma, a BOM, a stray ')' or any other byte
  fail(p, SYNTAX, start);
  return null;
};

// parse lexes and parses the whole bounded document first, so a lexical or
// syntax failure wins over every semantic one (§16.9.4). Answers
// [root, outside comments] or sets p.err.
local parse = function(p) {
  if (p.n > p.limits.bytes) { fail(p, LIMIT, p.limits.bytes); return null; }
  if (!gap(p)) return null;
  if (p.pos >= p.n || (p.src[p.pos] & 0xff) != '(') { fail(p, SYNTAX, p.pos); return null; }
  local root = value(p, 0);
  if (root == null) return null;
  if (!gap(p)) return null;
  // exactly one root record: a second form is syntax
  if (p.pos != p.n) { fail(p, SYNTAX, p.pos); return null; }
  return root;
};

// ---- RESOLVING THE TREE AGAINST THE DESCRIPTORS (§16.9.2) -------------------
// r.out is the JSON the walk lands, a blob; r.paths maps a path to its node.

local tableDesc = function(T, name) { return T.Descriptors[name]; };
local unionDesc = function(T, name) { return T[name + "Descriptor"]; };

local rfail = function(r, name, node, at) {
  if (r.err == null) r.err = [name, at, node.path];
  return false;
};

local mark = function(r, node, path) {
  node.path = path;
  node.pathed = true;
  r.paths[path] <- node;
};

local raw = function(r, s) { put(r.out, s); };

// jsonString writes decoded, well-formed UTF-8 as a JSON string.
local HEX = "0123456789abcdef";
local jsonString = function(r, s) {
  local out = r.out;
  out.writen('"', 'b');
  foreach (ch in s) {
    local c = ch & 0xff;
    if (c == '"' || c == '\\') { out.writen('\\', 'b'); out.writen(c, 'b'); }
    else if (c < 0x20) { put(out, "\\u00"); out.writen(HEX[c >> 4], 'b'); out.writen(HEX[c & 15], 'b'); }
    else out.writen(c, 'b');
  }
  out.writen('"', 'b');
};

local joinPath = function(path, key) { return path == "" ? key : path + "." + key; };
local indexPath = function(path, i) { return path + "[" + i + "]"; };
local isKey = function(node) { return node.kind == ':' || node.kind == '"'; };

local record = null;
local field = null;
local scalar = null;

// guardHolds evaluates a field's composed branch guard (§16.2, json.go's
// [member, want] pairs) over the record's bools, read where present and
// defaulted otherwise; a pair naming no field is false.
local guardHolds = function(desc, fd, seen, items) {
  if (!has(fd, "guard")) return true;
  foreach (pair in fd.guard) {
    local on = false;
    if (pair[0] != null) {
      foreach (j, g in desc.fields) {
        if (g.member != pair[0]) continue;
        local k = seen[j];
        if (k != null) {
          for (local i = 0; i + 1 < items.len(); i += 2) if (items[i] == k) on = items[i + 1].kind == 't';
        } else {
          on = g.def == true;
        }
        break;
      }
    }
    if (on != pair[1]) return false;
  }
  return true;
};

// record resolves a list as a table's key/value pairs from pair index first
// (a definition's label is pair 0); label is the JSON already written for it
// (§16.9.2). Keys are unique; guards and required fields are held after the
// whole record, in declaration order (§2.3, §16.9.4).
record = function(r, node, desc, path, first, label) {
  if (node.kind != '(') return rfail(r, KIND_MISMATCH, node, node.start);
  node.pairs = true;
  local items = node.items;
  if (items.len() % 2 != 0) return rfail(r, SYNTAX, node, node.end - 1);
  local array = has(desc, "jsonArray") && label == "";
  if (!array) { raw(r, "{"); raw(r, label); }
  local wrote = label != "";
  local seen = ::array(desc.fields.len(), null);
  for (local i = first; i < items.len(); i += 2) {
    local k = items[i], v = items[i + 1];
    if (!isKey(k)) return rfail(r, SYNTAX, k, k.start);
    // the sharing construct only as a pointee's first pair (§16.9.2)
    if (k.text.len() > 0 && k.text[0] == '&') return rfail(r, REFERENCE, node, k.start);
    local index = -1;
    foreach (j, fd in desc.fields) if (fd.key == k.text) { index = j; break; }
    if (index < 0) return rfail(r, UNKNOWN_KEY, node, k.start);
    local fd = desc.fields[index];
    local fp = joinPath(path, fd.key);
    k.path = fp;
    k.pathed = true;
    if (seen[index] != null) return rfail(r, DUPLICATE_KEY, k, k.start);
    seen[index] = k;
    if (!array) {
      if (wrote) raw(r, ",");
      jsonString(r, k.text);
      raw(r, ":");
    }
    if (!field(r, v, fd, fp)) return false;
    wrote = true;
  }
  if (array && !wrote) raw(r, "[]");
  foreach (j, fd in desc.fields) {
    local active = guardHolds(desc, fd, seen, items);
    if (seen[j] != null && !active) return rfail(r, INACTIVE_FIELD, seen[j], seen[j].start);
    if (seen[j] == null && active && fd.required) return rfail(r, MISSING_REQUIRED, node, node.end - 1);
  }
  if (!array) raw(r, "}");
  return true;
};

// text is a string(N): a bound of bytes; a negative bound is unbounded.
local text = function(r, v, bound) {
  if (v.kind != '"') return rfail(r, KIND_MISMATCH, v, v.start);
  if (bound >= 0 && v.text.len() > bound) return rfail(r, BOUND, v, v.start);
  v.canon = v.text;
  jsonString(r, v.text);
  return true;
};

// wideText is a wstring(N) (§16.9.2): the same string, its bound N in UTF-16
// CODE UNITS after unescaping, so an astral code point (a four-byte UTF-8
// sequence) counts two. The decoded text is well-formed, so the lead byte
// alone says how many units each code point takes.
local wideText = function(r, v, bound) {
  if (v.kind != '"') return rfail(r, KIND_MISMATCH, v, v.start);
  local units = 0;
  foreach (ch in v.text) {
    local c = ch & 0xff;
    if ((c & 0xc0) == 0x80) continue;
    units += (c >= 0xf0) ? 2 : 1;
  }
  if (units > bound) return rfail(r, BOUND, v, v.start);
  v.canon = v.text;
  jsonString(r, v.text);
  return true;
};

// blob is standard padded base64 with no stray byte and zero pad bits; a
// negative bound is a *bytes blob, which has none.
local B64 = function(c) {
  if (c >= 'A' && c <= 'Z') return c - 'A';
  if (c >= 'a' && c <= 'z') return c - 'a' + 26;
  if (c >= '0' && c <= '9') return c - '0' + 52;
  if (c == '+') return 62;
  if (c == '/') return 63;
  return -1;
};
local decodedLength = function(s) {
  local n = s.len();
  if (n % 4 != 0) return -1;
  local out = 0;
  for (local i = 0; i < n; i += 4) {
    local a = B64(s[i] & 0xff), b = B64(s[i + 1] & 0xff);
    local c2 = s[i + 2] & 0xff, c3 = s[i + 3] & 0xff;
    if (a < 0 || b < 0) return -1;
    local tail = i + 4 == n;
    if (c2 == '=') {
      if (!tail || c3 != '=' || (b & 0x0f) != 0) return -1;
      out += 1;
    } else if (c3 == '=') {
      local c = B64(c2);
      if (!tail || c < 0 || (c & 0x03) != 0) return -1;
      out += 2;
    } else {
      if (B64(c2) < 0 || B64(c3) < 0) return -1;
      out += 3;
    }
  }
  return out;
};
local blobValue = function(r, v, bound) {
  if (v.kind != '"') return rfail(r, KIND_MISMATCH, v, v.start);
  foreach (ch in v.text) {
    local c = ch & 0xff;
    if (!((c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '+' || c == '/' || c == '=')) {
      return rfail(r, ENCODING, v, v.start);
    }
  }
  local size = decodedLength(v.text);
  if (size < 0) return rfail(r, ENCODING, v, v.start);
  if (bound >= 0 && size > bound) return rfail(r, BOUND, v, v.start);
  v.canon = v.text;
  jsonString(r, v.text);
  return true;
};

local enumValue = function(r, v, d) {
  if (!isKey(v)) return rfail(r, KIND_MISMATCH, v, v.start);
  foreach (pair in d.variants) {
    if (pair[0] == v.text) {
      v.canon = v.text;
      jsonString(r, v.text);
      return true;
    }
  }
  return rfail(r, UNKNOWN_VARIANT, v, v.start);
};

local flagsValue = function(r, v, d, path) {
  if (v.kind != '(') return rfail(r, KIND_MISMATCH, v, v.start);
  local names = has(d, "flagNames") ? d.flagNames : [];
  local seen = {};
  raw(r, "[");
  foreach (i, item in v.items) {
    mark(r, item, indexPath(path, i));
    if (!isKey(item)) return rfail(r, KIND_MISMATCH, item, item.start);
    local bit = -1;
    foreach (b, name in names) if (name == item.text) { bit = b; break; }
    if (bit < 0) return rfail(r, UNKNOWN_VARIANT, item, item.start);
    if (bit in seen) return rfail(r, DUPLICATE_KEY, item, item.start);
    seen[bit] <- true;
    item.canon = item.text;
    if (i > 0) raw(r, ",");
    jsonString(r, item.text);
  }
  raw(r, "]");
  return true;
};

// timestamp reads the one strict RFC 3339 parser (§2.11, §16.9.4).
local timestampValue = function(r, v) {
  if (v.kind != '"') return rfail(r, KIND_MISMATCH, v, v.start);
  local nanos = rfc3339(v.text);
  if (nanos == null) return rfail(r, TIMESTAMP, v, v.start);
  v.canon = nanos.tostring();
  jsonString(r, v.text);
  return true;
};

// magnitude is the decimal exponent of a number token's most significant
// nonzero digit, so a magnitude no table kind holds is answered before an
// exact conversion would allocate for it; zero answers null.
local magnitude = function(tok) {
  local i = 0, n = tok.len();
  if (tok[0] == '-') i++;
  local whole = 0, digits = 0, lead = -1, point = false;
  for (; i < n && tok[i] != 'e' && tok[i] != 'E'; i++) {
    if (tok[i] == '.') { point = true; continue; }
    if (lead < 0 && tok[i] != '0') lead = digits;
    digits++;
    if (!point) whole = digits;
  }
  if (lead < 0) return null;
  local exp = 0;
  if (i < n) {
    i++;
    local minus = false;
    if (i < n && (tok[i] == '+' || tok[i] == '-')) { minus = tok[i] == '-'; i++; }
    for (; i < n; i++) if (exp < 1000000) exp = exp * 10 + (tok[i] - '0');
    if (minus) exp = -exp;
  }
  return whole - lead - 1 + exp;
};

// exact parses a number token (RFC 8259, already checked) as [negative,
// digits magnitude, exp10]: the value is digits x 10^exp10.
local exact = function(tok) {
  local i = 0, n = tok.len();
  local neg = tok[0] == '-';
  if (neg) i++;
  local m = [];
  local exp = 0;
  local point = false;
  for (; i < n && tok[i] != 'e' && tok[i] != 'E'; i++) {
    if (tok[i] == '.') { point = true; continue; }
    m = bnMulSmall(m, 10, tok[i] - '0');
    if (point) exp--;
  }
  if (i < n) {
    i++;
    local minus = false;
    if (i < n && (tok[i] == '+' || tok[i] == '-')) { minus = tok[i] == '-'; i++; }
    local e = 0;
    for (; i < n; i++) if (e < 1000000) e = e * 10 + (tok[i] - '0');
    exp += minus ? -e : e;
  }
  return [neg, m, exp];
};

// the storage domain of an integral descriptor: its declared raw range when
// it has one, else its width's (§4, §16.9.2)
local domain = function(d) {
  local signed = d.signed;
  if (d.lo != null && d.hi != null) {
    if (typeof d.lo == "array") return [svFromPair(d.lo, signed), svFromPair(d.hi, signed)];
    return [svFromInt(d.lo, signed), svFromInt(d.hi, signed)];
  }
  local width = d.bits > 0 ? d.bits : d.width * 8;
  if (signed) {
    local half = bnPow2(width - 1);
    return [[true, half], [false, bnSubOne(half)]];
  }
  return [[false, []], [false, bnSubOne(bnPow2(width))]];
};

// integer is every integral kind and every fixed-point kind, exactly: the
// token becomes a rational, scaled by 2^F for a fixed kind, and must be an
// integer inside the storage width and the declared range (§16.9.2).
local integer = function(r, v, d) {
  if (v.kind != '0') return rfail(r, KIND_MISMATCH, v, v.start);
  local frac = has(d, "frac") ? d.frac : 0;
  local value = [false, []];
  local mag = magnitude(v.text);
  if (mag != null) {
    if (mag > 60) return rfail(r, BOUND, v, v.start);
    if (mag < -60) return rfail(r, KIND_MISMATCH, v, v.start);
    local x = exact(v.text);
    local m = bnShl(x[1], frac);
    local e = x[2];
    for (; e > 0; e--) m = bnMulSmall(m, 10, 0);
    for (; e < 0; e++) {
      local qr = bnDivSmall(m, 10);
      if (qr[1] != 0) return rfail(r, KIND_MISMATCH, v, v.start);
      m = qr[0];
    }
    value = [x[0], m];
  }
  local dom = domain(d);
  if (svCmp(value, dom[0]) < 0 || svCmp(value, dom[1]) > 0) return rfail(r, BOUND, v, v.start);
  v.canon = svDecimal(value);
  // the wide walk converts the token's digits exactly (§16.2)
  if (d.kind >= 18) raw(r, v.text);
  else raw(r, v.canon);
  return true;
};

// a token as a double: tofloat() is strtod only when the text carries a
// point or an exponent, so an integral token gets one
local tokenFloat = function(tok) {
  local dot = false;
  foreach (c in tok) if (c == '.' || c == 'e' || c == 'E') { dot = true; break; }
  return (dot ? tok : tok + "e0").tofloat();
};

// a double rounded to float32 and back (the blob's 'f' store narrows)
local narrow = function(x) {
  local b = blob(4);
  b.writen(x, 'f');
  b.seek(0);
  return b.readn('f');
};

local isInf = function(x) { return x != 0.0 && x * 0.5 == x; };

// float: the token is already RFC 8259's grammar, so the one failure left is
// a magnitude past the storage, an infinity, or a declared range (§16.9.2).
local floatValue = function(r, v, d) {
  if (v.kind != '0') return rfail(r, KIND_MISMATCH, v, v.start);
  local x = tokenFloat(v.text);
  if (isInf(x)) return rfail(r, BOUND, v, v.start);
  if (d.f32) {
    // past float32's largest finite value by half an ulp: an infinity
    local ax = x < 0 ? -x : x;
    if (ax >= 3.4028235677973366e38) return rfail(r, BOUND, v, v.start);
    x = narrow(x);
  }
  if (has(d, "fmin") && (x < d.fmin || x > d.fmax)) return rfail(r, BOUND, v, v.start);
  v.canon = format("%.17g", x);
  raw(r, v.text);
  return true;
};

local keyed = function(r, v, d, path) {
  if (v.kind != '(') return rfail(r, KIND_MISMATCH, v, v.start);
  v.pairs = true;
  if (v.items.len() % 2 != 0) return rfail(r, SYNTAX, v, v.end - 1);
  local slots = d.keyvariants;
  local seen = ::array(slots.len(), false);
  raw(r, "{");
  for (local i = 0; i < v.items.len(); i += 2) {
    local k = v.items[i], value = v.items[i + 1];
    if (!isKey(k)) return rfail(r, SYNTAX, k, k.start);
    // nothing is stored for None, so it names no slot (§2.4)
    local slot = -1;
    foreach (s, pair in slots) if (pair[0] == k.text) { slot = s; break; }
    if (slot < 0) return rfail(r, UNKNOWN_KEY, v, k.start);
    local sp = joinPath(path, k.text);
    k.path = sp;
    k.pathed = true;
    if (seen[slot]) return rfail(r, DUPLICATE_KEY, k, k.start);
    seen[slot] = true;
    if (i > 0) raw(r, ",");
    jsonString(r, k.text);
    raw(r, ":");
    mark(r, value, sp);
    if (!scalar(r, value, d.elem, sp)) return false;
  }
  raw(r, "}");
  return true;
};

local union = function(r, v, d, path) {
  if (v.kind != '(') return rfail(r, KIND_MISMATCH, v, v.start);
  v.pairs = true;
  if (v.items.len() % 2 != 0) return rfail(r, SYNTAX, v, v.end - 1);
  local arms = unionDesc(r.T, d.table).arms;
  local chosen = null;
  raw(r, "{");
  for (local i = 0; i < v.items.len(); i += 2) {
    local k = v.items[i], value = v.items[i + 1];
    if (!isKey(k)) return rfail(r, SYNTAX, k, k.start);
    if (k.text.len() > 0 && k.text[0] == '&') return rfail(r, REFERENCE, v, k.start);
    local arm = null;
    foreach (a in arms) if (a.name == k.text) { arm = a; break; }
    if (arm == null) return rfail(r, UNKNOWN_KEY, v, k.start);
    local ap = joinPath(path, k.text);
    k.path = ap;
    k.pathed = true;
    if (chosen == arm) return rfail(r, DUPLICATE_KEY, k, k.start);
    // a union holds one arm: two different arms is the wrong shape
    if (chosen != null) return rfail(r, KIND_MISMATCH, k, k.start);
    chosen = arm;
    jsonString(r, k.text);
    raw(r, ":");
    if (arm.payload == null) {
      mark(r, value, ap);
      if (value.kind != 'z') return rfail(r, KIND_MISMATCH, value, value.start);
      value.canon = "null";
      raw(r, "null");
      continue;
    }
    if (!field(r, value, arm.payload, ap)) return false;
  }
  raw(r, "}");
  return true;
};

// pointer is a table reference (§16.7): null, a record, a labelled
// definition ("&node" N then the record) or a reference to a COMPLETED
// definition of the same table.
local pointer = function(r, v, d, path) {
  if (v.kind == 'z') { v.canon = "null"; raw(r, "null"); return true; }
  if (v.kind != '(') return rfail(r, KIND_MISMATCH, v, v.start);
  local desc = tableDesc(r.T, d.table);
  local items = v.items;
  if (items.len() == 0 || !isKey(items[0]) || items[0].text != "&node") return record(r, v, desc, path, 0, "");
  if (items.len() < 2) return rfail(r, SYNTAX, v, v.end - 1);
  items[0].path = path;
  items[0].pathed = true;
  local label = items[1];
  label.path = path;
  label.pathed = true;
  if (label.kind != '0' || label.text[0] == '0') return rfail(r, REFERENCE, v, label.start);
  foreach (c in label.text) if (!isDigit(c)) return rfail(r, REFERENCE, v, label.start);
  local key = label.text;
  local state = (key in r.labels) ? r.labels[key] : null;
  if (items.len() == 2) {
    if (state == null || !state.done || state.desc != desc) return rfail(r, REFERENCE, v, label.start);
    v.pairs = true;
    raw(r, "{\"&node\":" + key + "}");
    return true;
  }
  // a second definition under one label
  if (state != null) return rfail(r, REFERENCE, v, label.start);
  state = { desc = desc, done = false };
  r.labels[key] <- state;
  if (!record(r, v, desc, path, 2, "\"&node\":" + key)) return false;
  state.done = true;
  return true;
};

local sequence = function(r, v, d, path) {
  if (v.kind != '(') return rfail(r, KIND_MISMATCH, v, v.start);
  local count = v.items.len();
  if (d.counted) {
    if (count > d.bound || count < d.min) return rfail(r, BOUND, v, v.start);
  } else if (d.list) {
    if (count < d.min) return rfail(r, BOUND, v, v.start);
  } else if (count != d.bound) {
    // a fixed array is EXACTLY its bound: short and long are both bound
    return rfail(r, BOUND, v, v.start);
  }
  raw(r, "[");
  foreach (i, item in v.items) {
    if (i > 0) raw(r, ",");
    local ip = indexPath(path, i);
    mark(r, item, ip);
    if (!scalar(r, item, d.elem, ip)) return false;
  }
  raw(r, "]");
  return true;
};

// field resolves one field's value at the field's own level (§16.9.2).
field = function(r, v, d, path) {
  mark(r, v, path);
  if (d.optional && v.kind == 'z') { v.canon = "null"; raw(r, "null"); return true; }
  if (d.shape == "arr") return d.kind == 16 ? keyed(r, v, d, path) : sequence(r, v, d, path);
  if (d.shape == "str") return text(r, v, d.bound);
  if (d.shape == "wstr") return wideText(r, v, d.bound);
  if (d.shape == "bytes") return blobValue(r, v, d.bound);
  return scalar(r, v, d, path);
};

// scalar resolves one value at the ELEMENT level: an element of an array, a
// keyed slot, or a plain field the field level did not claim.
scalar = function(r, v, d, path) {
  switch (d.shape) {
    case "ptr": return pointer(r, v, d, path);
    case "sblob":
      if (v.kind == 'z') { v.canon = "null"; raw(r, "null"); return true; }
      return text(r, v, -1);
    case "bblob":
      if (v.kind == 'z') { v.canon = "null"; raw(r, "null"); return true; }
      return blobValue(r, v, -1);
    case "union": return union(r, v, d, path);
    case "tab": return record(r, v, tableDesc(r.T, d.table), path, 0, "");
    case "enum": return enumValue(r, v, d);
    case "flags": return flagsValue(r, v, d, path);
    case "str": return text(r, v, d.bound);
    case "wstr": return wideText(r, v, d.bound);
    case "bytes": return blobValue(r, v, d.bound);
    case "bool":
      if (v.kind != 't' && v.kind != 'f') return rfail(r, KIND_MISMATCH, v, v.start);
      v.canon = v.kind == 't' ? "true" : "false";
      raw(r, v.canon);
      return true;
    case "float": return floatValue(r, v, d);
    case "int":
      if (d.kind == 36) return timestampValue(r, v);
      return integer(r, v, d);
  }
  return rfail(r, KIND_MISMATCH, v, v.start);
};

// ---- COMMENT ATTACHMENT (§16.9.3) -------------------------------------------

local lineBreak = function(src, from, to) {
  for (local i = from; i < to; i++) if ((src[i] & 0xff) == '\n') return true;
  return false;
};

// owner is the path a comment directly inside list n attaches to: between a
// key and its value, that field; after a complete value on the same physical
// line, that value; otherwise standalone, the next sibling, else the
// container.
local owner = function(src, n, c) {
  local children = [];
  if (n.pairs) {
    for (local i = 0; i + 1 < n.items.len(); i += 2) {
      local k = n.items[i], v = n.items[i + 1];
      local p = k.pathed ? k.path : n.path;
      if (k.end <= c[0] && c[0] < v.start) return p;
      children.append([k.start, v.end, p]);
    }
  } else {
    foreach (item in n.items) children.append([item.start, item.end, item.pathed ? item.path : n.path]);
  }
  for (local i = children.len() - 1; i >= 0; i--) {
    if (children[i][1] <= c[0]) {
      if (!lineBreak(src, children[i][1], c[0])) return children[i][2];
      break;
    }
  }
  foreach (ch in children) if (ch[0] > c[0]) return ch[2];
  return n.path;
};

local attach = null;
attach = function(src, n, out) {
  foreach (c in n.comments) out.append({ offset = c[0], text = slice(src, c[0], c[1]), path = owner(src, n, c) });
  foreach (item in n.items) if (item.kind == '(') attach(src, item, out);
};

// ---- THE DOCUMENT -------------------------------------------------------------

// A read's document: the source bytes, every value's span and every comment's
// attachment, with the typed value it read. It is immutable; an edit returns
// a new one.
local Document = class {
  root = null;
  src = null;
  limits = null;
  paths = null;
  comments = null;
  value = null;
  // Bytes is a copy of the document's source.
  function Bytes() { return blobOf(this.src); }
  // Comments is every comment in source order with its attachment.
  function Comments() {
    local out = [];
    foreach (c in this.comments) out.append({ offset = c.offset, text = c.text, path = c.path });
    return out;
  }
  // Span is [start, end) of the value at path, or null when none is spelled.
  function Span(path) {
    if (!(path in this.paths)) return null;
    local n = this.paths[path];
    return [n.start, n.end];
  }
  // Paths is every path the document spells a value at, sorted.
  function Paths() {
    local out = [];
    foreach (k, v in this.paths) out.append(k);
    out.sort();
    return out;
  }
  // Scalar reports whether the value at path is a scalar token, the one value
  // kind a leaf edit replaces.
  function Scalar(path) { return (path in this.paths) && this.paths[path].kind != '('; }
};

// read reads one document strictly and answers [document, the canonical JSON
// of its value], or [null, [name, offset, path]].
local read = function(T, rootName, src, limits) {
  local p = newParser(src, limits);
  local root = parse(p);
  if (root == null) return [null, [p.err[0], p.err[1], ""]];
  local r = { T = T, out = blob(0), paths = {}, labels = {}, err = null };
  mark(r, root, "");
  if (!record(r, root, tableDesc(T, rootName), "", 0, "")) return [null, r.err];
  local doc = Document();
  doc.root = rootName;
  doc.src = src;
  doc.limits = limits;
  doc.paths = r.paths;
  local comments = [];
  foreach (c in p.outside) comments.append({ offset = c[0], text = slice(src, c[0], c[1]), path = "" });
  attach(src, root, comments);
  comments.sort(function(a, b) { return a.offset <=> b.offset; });
  doc.comments = comments;
  return [doc, r.out];
};

// land runs the JSON walk over the transcoded value. The strict resolve
// refuses every event the walk can count, so a count here is a disagreement
// between the two and is never accepted silently.
local land = function(T, rootName, js) {
  local report = {};
  local v = _json.FromJsonTolerant(T, rootName, js, report);
  local name = null;
  if (report.clamped > 0) name = BOUND;
  else if (report.unknown > 0) name = UNKNOWN_KEY;
  else if (report.duplicate > 0) name = DUPLICATE_KEY;
  else if (report.malformed || report.refused) name = SYNTAX;
  else if (report.kindMismatch > 0 || report.widened > 0) name = KIND_MISMATCH;
  return name == null ? [v, null] : [null, name];
};

local refuse = function(report, err) {
  report.refused <- true;
  report.name <- err[0];
  report.offset <- err[1];
  report.path <- err.len() > 2 ? err[2] : "";
  return null;
};

local clear = function(report) {
  report.refused <- false;
  report.name <- null;
  report.offset <- 0;
  report.path <- "";
};

// loaded reads src and lands its value, or refuses into report.
local loaded = function(T, rootName, src, limits, report) {
  local got = read(T, rootName, src, limits);
  if (got[0] == null) return refuse(report, got[1]);
  local landed = land(T, rootName, got[1]);
  if (landed[0] == null) return refuse(report, [landed[1], 0, ""]);
  got[0].value = landed[0];
  return got[0];
};

// LoadTextForm reads one document (§16.9): the document AND its typed value
// (doc.value), or null with report naming the refusal and nothing else.
_M.LoadTextForm <- function(T, rootName, text, limits, report) {
  clear(report);
  return loaded(T, rootName, blobOf(text), resolved(limits), report);
};

// SaveTextForm writes a document LoadTextForm or EditTextForm returned: the
// retained bytes, so a read then a write with no edit is the input byte for
// byte (§16.9.3). A document of another root is an edit conflict: null.
_M.SaveTextForm <- function(T, rootName, doc) {
  if (doc == null || !(doc instanceof Document) || doc.root != rootName) return null;
  return doc.Bytes();
};

local apply = function(src, splices) {
  local out = blob(0);
  local at = 0;
  foreach (s in splices) {
    for (local i = at; i < s.start; i++) out.writen(src[i] & 0xff, 'b');
    put(out, s.text);
    at = s.end;
  }
  for (local i = at; i < src.len(); i++) out.writen(src[i] & 0xff, 'b');
  return out;
};

// EditTextForm replaces existing scalar values (§16.9.3): each edit's token
// span and nothing else, all spans taken from the original document, a
// semantic no-op keeping the old spelling. An absent value is an insert and a
// compound is edited through its children: both are structural, not a leaf
// edit. The edited document is read again under the same limits before it is
// returned, and a refusal leaves the old document untouched.
_M.EditTextForm <- function(T, rootName, doc, edits, report) {
  clear(report);
  if (doc == null || !(doc instanceof Document) || doc.root != rootName) return refuse(report, [EDIT_CONFLICT, 0, ""]);
  local splices = [];
  foreach (e in edits) {
    local n = (e.path in doc.paths) ? doc.paths[e.path] : null;
    if (n == null || n.kind == '(') return refuse(report, [EDIT_CONFLICT, 0, e.path]);
    local spelling = blobOf(e.text);
    local p = newParser(spelling, doc.limits);
    local v = value(p, 0);
    if (v == null) return refuse(report, [p.err[0], p.err[1] + n.start, e.path]);
    if (v.kind == '(' || p.pos != spelling.len()) return refuse(report, [EDIT_CONFLICT, n.start, e.path]);
    splices.append({ start = n.start, end = n.end, text = e.text, path = e.path, canon = n.canon });
  }
  splices.sort(function(a, b) { return a.start <=> b.start; });
  for (local i = 1; i < splices.len(); i++) {
    if (splices[i].start < splices[i - 1].end) return refuse(report, [EDIT_CONFLICT, splices[i].start, splices[i].path]);
  }
  while (true) {
    local src = apply(doc.src, splices);
    local got = read(T, rootName, src, doc.limits);
    if (got[0] == null) return refuse(report, got[1]);
    // a semantic no-op keeps the old spelling (§16.9.3)
    local kept = [];
    foreach (s in splices) {
      local n = (s.path in got[0].paths) ? got[0].paths[s.path] : null;
      if (n == null || n.canon != s.canon) kept.append(s);
    }
    if (kept.len() == splices.len()) return loaded(T, rootName, src, doc.limits, report);
    splices = kept;
  }
};
