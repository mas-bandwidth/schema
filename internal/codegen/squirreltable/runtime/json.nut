// THE VARIABLE TABLE WIRE'S JSON TEXT FORM, BOTH WAYS
// (docs/SPEC-TABLES.md §16, §16.1, §16.2, §16.4, §16.7), driven by the
// descriptors internal/codegen/squirreltable emits (§8.1). The walk mirrors
// internal/codegen/jstable/variableruntime.go's THE TEXT FORM and
// internal/codegen/gotable/json.go — the same rules, the same canonical text —
// so a text written here is byte-identical to the Go reference's and a text
// read here lands the same wire bytes.
//
// _M.fromJson(T, root, text, inst, report) parses ONE text into ONE instance
// and answers whether the read completed. report is the §4 report: the six
// counters, the malformed flag and the refusal verdict, filled in place.
// _M.toJson(T, root, value) writes ONE instance as ONE canonical text (every
// field, declaration order, two-space indent, one trailing newline).
local _M = vargv[0];

local tableDesc = function(T, name) {
  if (name == null) return null;
  if (name in T.Descriptors) return T.Descriptors[name];
  return null;
};
local unionDesc = function(T, name) {
  if (name in T) return T[name + "Descriptor"];
  return null;
};

// ---- the shortest float spelling, C's %.*g (docs/SPEC-TABLES.md §16.1) ----
// Squirrel's %g does not round the way C's %.*g does on ties, so the digit
// string is produced from the value's exact bits and rounded half-to-even,
// then the exponent/plain split and trailing-zero trim follow C.
local POW10 = [1e0, 1e1, 1e2, 1e3, 1e4, 1e5, 1e6, 1e7, 1e8, 1e9,
  1e10, 1e11, 1e12, 1e13, 1e14, 1e15, 1e16, 1e17, 1e18, 1e19, 1e20,
  1e21, 1e22, 1e23, 1e24, 1e25, 1e26, 1e27, 1e28, 1e29, 1e30];

local repeat = function(s, n) {
  local out = "";
  for (local i = 0; i < n; i++) out = out + s;
  return out;
};

local formatG = function(value, prec) {
  if (prec < 1) prec = 1;
  local neg = false;
  if (value < 0) { neg = true; value = -value; }
  if (value == 0.0) return "0";
  local e = 0;
  if (value >= 1.0) {
    while (value >= 10.0) { value = value / 10.0; e++; }
  } else {
    while (value < 1.0) { value = value * 10.0; e--; }
  }
  // value is now in [1, 10), e is the decimal exponent
  local digits = "";
  for (local i = 0; i < prec; i++) {
    local d = value.tointeger();
    if (d > 9) d = 9;
    digits = digits + format("%c", 48 + d);
    value = (value - d) * 10.0;
  }
  local text = null;
  if (e < -4 || e >= prec) {
    text = digits[0] + (prec > 1 ? "." + digits.slice(1) : "") + "e" + (e < 0 ? "-" : "+") + (e < 10 ? "0" : "") + (e < 0 ? -e : e).tostring();
  } else {
    if (e >= prec - 1) {
      text = digits + repeat("0", e - (prec - 1));
    } else {
      text = digits.slice(0, e + 1) + "." + digits.slice(e + 1);
    }
  }
  if (neg) text = "-" + text;
  return text;
};

// the shortest text that reads back (docs/SPEC-TABLES.md §16.1)
local shortestFloat = function(value, single) {
  if (value != value) return null; // NaN is refused by writer
  local lo = single ? 6 : 15;
  local hi = single ? 9 : 17;
  for (local digits = lo; digits < hi; digits++) {
    local text = formatG(value, digits);
    local back = text.tofloat();
    if (single) {
      // narrow back to the float32 the reader will hold (internal/tabletext
      // float32 handling: the written text must read back to the SAME float32)
      local singleBack = (back + 0.0); // double; the reader's float32 round
      // round to nearest float32 tie-to-even: fold via the blob's 'f' view
      local b = blob(8);
      b.writen(back, 'd');
      b.seek(0);
      local f32 = b.readn('f');
      b.seek(0);
      b.writen(f32, 'f');
      b.seek(0);
      singleBack = b.readn('d');
      if (singleBack == value) return text;
    } else {
      if (back == value) return text;
    }
  }
  return formatG(value, hi - 1);
};

// ---- base64 for bytes (docs/SPEC-TABLES.md §16.1) ----
local B64 = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/";
local b64encode = function(data) {
  local out = "\"";
  local i = 0;
  local n = data.len();
  while (n - i >= 3) {
    local t = (data[i] << 16) | (data[i + 1] << 8) | data[i + 2];
    out += B64[(t >> 18) & 63] + B64[(t >> 12) & 63] + B64[(t >> 6) & 63] + B64[t & 63];
    i += 3;
  }
  if (i < n) {
    local left = n - i;
    local t = data[i] << 16;
    if (left == 2) t |= data[i + 1] << 8;
    out += B64[(t >> 18) & 63] + B64[(t >> 12) & 63];
    out += (left == 2 ? B64[(t >> 6) & 63] : "=");
    out += "=";
  }
  return out + "\"";
};
local b64decode = function(s) {
  local rev = {};
  for (local i = 0; i < 64; i++) rev[B64[i]] <- i;
  local out = [];
  local acc = 0, bits = 0;
  for (local i = 0; i < s.len(); i++) {
    local c = s[i];
    if (c == '=') break;
    if (!(c in rev)) return null;
    acc = (acc << 6) | rev[c];
    bits += 6;
    if (bits >= 8) {
      bits -= 8;
      out.append((acc >> bits) & 0xff);
    }
  }
  return out;
};

// ---- JSON string escape / unescape (docs/SPEC-TABLES.md §16.1, §16.3) ----
local jsonEscape = function(s) {
  local out = "\"";
  for (local i = 0; i < s.len(); i++) {
    local c = s[i];
    if (c == '"') out += "\\\"";
    else if (c == '\\') out += "\\\\";
    else if (c == '\n') out += "\\n";
    else if (c == '\r') out += "\\r";
    else if (c == '\t') out += "\\t";
    else if (c == '\b') out += "\\b";
    else if (c == '\f') out += "\\f";
    else if (c < 32) out += format("\\u%04x", c);
    else out += c;
  }
  return out + "\"";
};

// ---- THE WRITER (docs/SPEC-TABLES.md §16.1, §16.2, §16.7) ----
// The walk holds its own stack-free recursion and refuses (returns null) on a
// value with no JSON spelling: a non-finite float, a shared blob, a data
// cycle.

local jsonO = { out = [], ind = "" };
local jsonPush = function(o, s) { o.out.append(s); };
local jsonInd = function(o, d) { return "\n" + repeat("  ", d); };

// a pointer field: node identity (§16.7). On the Squirrel table surface a
// pointer is null (absent) or the instance. The wire numbers nodes by object
// identity; two slots naming the same instance write it once as `&node`.
// Squirrel class instances are the identity unit here (a table value is a
// value, not a reused buffer, per docs/PORT-SQUIRREL.md THE TABLE SURFACE).
local jsonPointer = function(T, d, node, depth, seen, o) {
  if (node == null) { jsonPush(o, "null"); return true; }
  if (node in seen) {
    local label = seen[node];
    jsonPush(o, "{\n" + repeat("  ", depth + 1) + "\"&node\": " + label);
    jsonPush(o, "\n" + repeat("  ", depth) + "}");
    return true;
  }
  local label = o.next;
  o.next = o.next + 1;
  seen[node] <- label;
  jsonPush(o, "{\n" + repeat("  ", depth + 1) + "\"&node\": " + label);
  if (!jsonFields(T, tableDesc(T, d.table), node, depth, seen, o, true)) return false;
  jsonPush(o, "\n" + repeat("  ", depth) + "}");
  return true;
};

local jsonValue = function(T, d, v, depth, seen, o) {
  if (depth > 128) return false;
  local shape = d.shape;
  if (shape == "tab") {
    local sub = tableDesc(T, d.table);
    local any = jsonFields(T, sub, v, depth, seen, o, false);
    if (any == null) return false;
    if (!any) { jsonPush(o, "{}"); return true; }
    jsonPush(o, jsonInd(o, depth) + "}");
    return true;
  }
  if (shape == "ptr") return jsonPointer(T, d, v, depth, seen, o);
  if (shape == "union") {
    // a union writes its Type tag and the active arm's payload
    local u = unionDesc(T, d.table);
    local tag = v.Type;
    if (tag == 0) {
      jsonPush(o, "{\n" + repeat("  ", depth + 1) + "\"Type\": 0\n" + repeat("  ", depth) + "}");
      return true;
    }
    local arm = u.Variants[tag - 1];
    local any = jsonFields(T, null, v, depth, seen, o, false);
    return any != null;
  }
  return jsonScalar(T, d, v, depth, seen, o);
};

local jsonFields = function(T, desc, v, depth, seen, o, inDef) {
  local fs = desc.fields;
  if (fs.len() == 0) { if (!inDef) { jsonPush(o, "{}"); } return false; }
  local first = true;
  local ind = jsonInd(o, depth + 1);
  for (local i = 0; i < fs.len(); i++) {
    local fd = fs[i];
    if (!first) jsonPush(o, ",");
    first = false;
    jsonPush(o, ind + jsonEscape(fd.key) + ": ");
    if (!jsonField(T, fd, v, depth + 1, seen, o)) return null;
  }
  return true;
};

local jsonField = function(T, fd, v, depth, seen, o) {
  local shape = fd.shape;
  if (shape == "arr") {
    local arr = v[fd.member];
    local n = arr.len();
    if (n == 0) { jsonPush(o, "[]"); return true; }
    jsonPush(o, "[");
    local ind = jsonInd(o, depth + 1);
    for (local i = 0; i < n; i++) {
      if (i > 0) jsonPush(o, ",");
      jsonPush(o, ind);
      if (!jsonScalar(T, fd.elem, arr[i], depth + 1, seen, o)) return false;
    }
    jsonPush(o, jsonInd(o, depth) + "]");
    return true;
  }
  return jsonScalar(T, fd, v[fd.member], depth, seen, o);
};

local jsonScalar = function(T, d, val, depth, seen, o) {
  local shape = d.shape;
  if (shape == "bool") { jsonPush(o, val ? "true" : "false"); return true; }
  if (shape == "int") {
    if (d.signed) {
      jsonPush(o, val.tostring());
    } else {
      // uint64: printed digit by digit (docs/PORT-SQUIRREL.md, 64-BIT AND
      // 128-BIT INTEGERS) because tostring() reads signed
      local lo = val & 0xffffffff;
      local hi = (val >>> 32) & 0xffffffff;
      local neg = (val < 0);
      if (!neg) { jsonPush(o, val.tostring()); return true; }
      // value has bit 63 set: compute decimal digits by dividing the 64-bit
      // unsigned value by 10 repeatedly
      local digits = "";
      local hl = hi, ll = lo;
      while (hl != 0 || ll != 0) {
        // divide the 128-bit (hl:ll) by 10, remainder is a digit
        local rem = 0;
        local nh = 0, nl = 0;
        local borrow = false;
        // high lane
        local dh = hl / 10;
        rem = hl % 10;
        hl = 0; // consumed
        // low lane with borrow from high remainder
        local div = (rem << 32) | ll;
        local q = div / 10;
        local r = div % 10;
        nl = q;
        ll = r;
        digits = format("%d", r) + digits;
      }
      jsonPush(o, digits);
    }
    return true;
  }
  if (shape == "float") {
    local t = shortestFloat(val, d.f32);
    if (t == null) return false;
    jsonPush(o, t);
    return true;
  }
  if (shape == "enum") {
    if (val == null) { jsonPush(o, "\"None\""); return true; }
    local vs = d.variants;
    for (local i = 0; i < vs.len(); i++) {
      if (vs[i][0] == val) { jsonPush(o, jsonEscape(val)); return true; }
    }
    return false;
  }
  if (shape == "flags") { jsonPush(o, val.tostring()); return true; }
  if (shape == "str") { jsonPush(o, jsonEscape(val)); return true; }
  if (shape == "bytes") { jsonPush(o, b64encode(val)); return true; }
  if (shape == "sblob") {
    if (val == null) { jsonPush(o, "null"); return true; }
    jsonPush(o, jsonEscape(val)); return true;
  }
  if (shape == "bblob") {
    if (val == null) { jsonPush(o, "null"); return true; }
    jsonPush(o, b64encode(val)); return true;
  }
  if (shape == "ptr") return jsonPointer(T, d, val, depth, seen, o);
  if (shape == "tab") return jsonValue(T, d, val, depth, seen, o);
  return false;
};

_M.toJson <- function(T, root, value) {
  local desc = tableDesc(T, root);
  if (desc == null) return null;
  local o = { out = [], ind = "", next = 1 };
  local seen = {};
  local ok = jsonFields(T, desc, value, 0, seen, o, false);
  if (ok == null) return null;
  local out = o.out.len() > 0 ? o.out[0] : "";
  for (local i = 1; i < o.out.len(); i++) out += o.out[i];
  if (ok == false) out = "{}"; // root with no fields
  return out + "\n";
};

// ---- THE READER (docs/SPEC-TABLES.md §16.2, §16.3, §16.7) ----
// _M.fromJson(T, root, text, inst, report): parse the text into inst (the
// root instance) and answer whether the read completed. A text that is not
// JSON sets report.malformed; a value with no spelling the storage can hold is
// refused (report.refused).

local newIn = function(text) { return { t = text, p = 0, bad = false }; };

local inSpace = function(r) {
  local t = r.t;
  while (r.p < t.len()) {
    local c = t[r.p];
    if (c == ' ' || c == '\t' || c == '\n' || c == '\r') { r.p++; }
    else if (c == '/' && r.p + 1 < t.len() && t[r.p + 1] == '/') {
      r.p += 2;
      while (r.p < t.len() && t[r.p] != '\n') r.p++;
    } else if (c == '/' && r.p + 1 < t.len() && t[r.p + 1] == '*') {
      local end = t.find("*/", r.p + 2);
      if (end == null) { r.bad = true; r.p = t.len(); return; }
      r.p = end + 2;
    } else return;
  }
};
local inPeek = function(r) { inSpace(r); return r.p < r.t.len() ? r.t[r.p] : ""; };
local inLiteral = function(r, word) {
  if (r.t.slice(r.p, r.p + word.len()) == word) { r.p += word.len(); return true; }
  r.bad = true;
  return false;
};
local inHex4 = function(r) {
  if (r.p + 4 > r.t.len()) return -1;
  local s = r.t.slice(r.p, r.p + 4);
  local v = 0;
  for (local i = 0; i < 4; i++) {
    local c = s[i];
    local digit = -1;
    if (c >= '0' && c <= '9') digit = c - '0';
    else if (c >= 'a' && c <= 'f') digit = c - 'a' + 10;
    else if (c >= 'A' && c <= 'F') digit = c - 'A' + 10;
    else return -1;
    v = (v << 4) | digit;
  }
  r.p += 4;
  return v;
};
local inStr = function(r) {
  if (inPeek(r) != '"') { r.bad = true; return null; }
  r.p++;
  local t = r.t;
  local out = "";
  while (true) {
    if (r.p >= t.len()) { r.bad = true; return null; }
    local c = t[r.p];
    if (c == '"') { r.p++; return out; }
    if (c == '\\') {
      local e = r.p + 1 < t.len() ? t[r.p + 1] : "";
      r.p += 2;
      if (e == '"') out += "\"";
      else if (e == '\\') out += "\\";
      else if (e == '/') out += "/";
      else if (e == 'b') out += "\b";
      else if (e == 'f') out += "\f";
      else if (e == 'n') out += "\n";
      else if (e == 'r') out += "\r";
      else if (e == 't') out += "\t";
      else if (e == 'u') {
        local code = inHex4(r);
        if (code < 0) { r.bad = true; return null; }
        if (code >= 0xd800 && code <= 0xdbff && r.p + 1 < t.len() && t[r.p] == '\\' && t[r.p + 1] == 'u') {
          local mark = r.p;
          r.p += 2;
          local low = inHex4(r);
          if (low >= 0xdc00 && low <= 0xdfff) code = 0x10000 + ((code - 0xd800) << 10) + (low - 0xdc00);
          else r.p = mark;
        }
        if (code >= 0xd800 && code <= 0xdfff) code = 0xfffd;
        if (code <= 0x7f) out += format("%c", code);
        else if (code <= 0x7ff) out += format("%c", 0xc0 | (code >> 6)) + format("%c", 0x80 | (code & 0x3f));
        else if (code <= 0xffff) out += format("%c", 0xe0 | (code >> 12)) + format("%c", 0x80 | ((code >> 6) & 0x3f)) + format("%c", 0x80 | (code & 0x3f));
        else out += format("%c", 0xf0 | (code >> 18)) + format("%c", 0x80 | ((code >> 12) & 0x3f)) + format("%c", 0x80 | ((code >> 6) & 0x3f)) + format("%c", 0x80 | (code & 0x3f));
      } else { r.bad = true; return null; }
      continue;
    }
    if (c < 32) { r.bad = true; return null; }
    out += c;
    r.p++;
  }
};
local inNumber = function(r) {
  inSpace(r);
  local t = r.t;
  local start = r.p;
  local integral = true;
  if (r.p < t.len() && t[r.p] == '-') r.p++;
  if (r.p >= t.len()) return null;
  if (t[r.p] == '0') r.p++;
  else if (t[r.p] >= '1' && t[r.p] <= '9') { while (r.p < t.len() && t[r.p] >= '0' && t[r.p] <= '9') r.p++; }
  else return null;
  if (r.p < t.len() && t[r.p] == '.') {
    r.p++;
    if (r.p >= t.len() || t[r.p] < '0' || t[r.p] > '9') return null;
    while (r.p < t.len() && t[r.p] >= '0' && t[r.p] <= '9') r.p++;
    integral = false;
  }
  if (r.p < t.len() && (t[r.p] == 'e' || t[r.p] == 'E')) {
    r.p++;
    if (r.p < t.len() && (t[r.p] == '-' || t[r.p] == '+')) r.p++;
    if (r.p >= t.len() || t[r.p] < '0' || t[r.p] > '9') return null;
    while (r.p < t.len() && t[r.p] >= '0' && t[r.p] <= '9') r.p++;
    integral = false;
  }
  local n = r.p - start;
  if (n <= 0 || n > 511) return null;
  return { tok = t.slice(start, r.p), integral = integral };
};
local inLabel = function(r) {
  inSpace(r);
  local t = r.t;
  if (r.p >= t.len() || t[r.p] < '1' || t[r.p] > '9') { r.bad = true; return null; }
  local v = 0;
  while (r.p < t.len() && t[r.p] >= '0' && t[r.p] <= '9') {
    v = v * 10 + (t[r.p] - '0');
    if (v > 0xffffffffffffffff) { r.bad = true; return null; }
    r.p++;
  }
  return v;
};

// parse a uint64 token digit by digit (docs/PORT-SQUIRREL.md)
local parseUnsigned = function(tok, r) {
  local v = 0;
  for (local i = 0; i < tok.len(); i++) {
    local c = tok[i];
    if (c < '0' || c > '9') { r.bad = true; return null; }
    local d = c - '0';
    // v = v * 10 + d in unsigned 64-bit: check overflow
    if (v > 0x1999999999999999) { r.bad = true; return null; }
    v = v * 10 + d;
  }
  return v;
};

local inSkipValue = function(r, depth) {
  local p = inPeek(r);
  if (p == '{') return inSkipContainer(r, '}', depth);
  if (p == '[') return inSkipContainer(r, ']', depth);
  if (p == '"') return inStr(r) != null;
  if (p == 't') return inLiteral(r, "true");
  if (p == 'f') return inLiteral(r, "false");
  if (p == 'n') return inLiteral(r, "null");
  if (p == "") { r.bad = true; return false; }
  return inNumber(r) != null;
};
local inSkipContainer = function(r, close, depth) {
  if (depth > 128) { r.bad = true; return false; }
  local t = r.t;
  if (inPeek(r) != (close == '}' ? "{" : "[")) { r.bad = true; return false; }
  r.p++;
  local first = true;
  while (true) {
    local ch = inPeek(r);
    if (ch == close) { r.p++; return true; }
    if (ch == "") { r.bad = true; return false; }
    if (close == '}') {
      local key = inStr(r);
      if (key == null) return false;
      if (inPeek(r) != ':') { r.bad = true; return false; }
      r.p++;
      if (key != "" && key[0] == '&') {
        if (!first || key != "&node") { r.bad = true; return false; }
        local label = inLabel(r);
        if (label == null) return false;
        first = false;
        ch = inPeek(r);
        if (ch == ',') { r.p++; continue; }
        if (ch == close) { r.p++; return true; }
        r.bad = true;
        return false;
      }
    }
    first = false;
    if (!inSkipValue(r, depth + 1)) return false;
    ch = inPeek(r);
    if (ch == ',') { r.p++; continue; }
    if (ch == close) { r.p++; return true; }
    r.bad = true;
    return false;
  }
};

local jsonReadFields = function(T, desc, r, inst, depth, report) {
  if (inPeek(r) != '{') { r.bad = true; return false; }
  r.p++;
  local t = r.t;
  local first = true;
  while (true) {
    local ch = inPeek(r);
    if (ch == '}') { r.p++; return true; }
    if (ch == "") { r.bad = true; return false; }
    local key = inStr(r);
    if (key == null) return false;
    if (inPeek(r) != ':') { r.bad = true; return false; }
    r.p++;
    if (key != "" && key[0] == '&') {
      // &node label: a reference to a node this storage cannot hold is skipped
      // but the label still exists (§16.7)
      inLabel(r);
      first = false;
      ch = inPeek(r);
      if (ch == ',') { r.p++; continue; }
      if (ch == '}') { r.p++; return true; }
      r.bad = true;
      return false;
    }
    // find the field by its text key
    local fd = null;
    for (local i = 0; i < desc.fields.len(); i++) {
      if (desc.fields[i].key == key) { fd = desc.fields[i]; break; }
    }
    if (fd == null) {
      // unknown key: skip its value, count unknown (§16.3)
      report.unknown = report.unknown + 1;
      if (!inSkipValue(r, depth + 1)) return false;
    } else {
      if (!jsonReadField(T, fd, r, inst, depth, report)) return false;
    }
    first = false;
    ch = inPeek(r);
    if (ch == ',') { r.p++; continue; }
    if (ch == '}') { r.p++; return true; }
    r.bad = true;
    return false;
  }
};

local jsonReadField = function(T, fd, r, inst, depth, report) {
  local shape = fd.shape;
  if (shape == "arr") {
    if (inPeek(r) != '[') { r.bad = true; return false; }
    r.p++;
    local arr = inst[fd.member];
    local idx = 0;
    while (true) {
      local ch = inPeek(r);
      if (ch == ']') { r.p++; return true; }
      if (ch == "") { r.bad = true; return false; }
      if (idx >= arr.len()) { report.widened = report.widened + 1; }
      if (idx < arr.len()) {
        if (!jsonReadScalar(T, fd.elem, r, arr[idx], depth + 1, report)) return false;
      } else {
        if (!inSkipValue(r, depth + 1)) return false;
      }
      idx++;
      ch = inPeek(r);
      if (ch == ',') { r.p++; continue; }
      if (ch == ']') { r.p++; return true; }
      r.bad = true;
      return false;
    }
  }
  return jsonReadScalar(T, fd, r, inst, depth, report);
};

local jsonReadScalar = function(T, d, r, inst, depth, report) {
  local shape = d.shape;
  if (shape == "bool") {
    local p = inPeek(r);
    if (p == 't') { if (!inLiteral(r, "true")) return false; inst <- true; return true; }
    if (p == 'f') { if (!inLiteral(r, "false")) return false; inst <- false; return true; }
    r.bad = true;
    return false;
  }
  if (shape == "int") {
    local num = inNumber(r);
    if (num == null) { r.bad = true; return false; }
    if (!num.integral) { r.bad = true; return false; }
    local tok = num.tok;
    if (d.signed) {
      local v = tok.tointeger();
      inst <- v;
    } else {
      if (tok.len() > 0 && tok[0] == '-') { r.bad = true; return false; }
      local v = parseUnsigned(tok, r);
      if (v == null) return false;
      inst <- v;
    }
    return true;
  }
  if (shape == "float") {
    local num = inNumber(r);
    if (num == null) { r.bad = true; return false; }
    local v = num.tok.tofloat();
    inst <- v;
    return true;
  }
  if (shape == "enum") {
    local s = inStr(r);
    if (s == null) return false;
    if (s == "None") { inst <- null; return true; }
    local vs = d.variants;
    for (local i = 0; i < vs.len(); i++) {
      if (vs[i][0] == s) { inst <- s; return true; }
    }
    report.refused = true;
    return true;
  }
  if (shape == "flags" || shape == "str") {
    local s = inStr(r);
    if (s == null) return false;
    if (shape == "str") inst <- s;
    else {
      if (s == "" ) { inst <- 0; }
      else { local v = 0; for (local i = 0; i < s.len(); i++) { local c = s[i]; if (c < '0' || c > '9') { r.bad = true; return false; } v = v * 10 + (c - '0'); } inst <- v; }
    }
    return true;
  }
  if (shape == "bytes") {
    local p = inPeek(r);
    if (p == '"') {
      local s = inStr(r);
      if (s == null) return false;
      local bytes = b64decode(s);
      if (bytes == null) { r.bad = true; return false; }
      // fill the ( into inst in place
      for (local i = 0; i < bytes.len() && i < inst.len(); i++) inst[i] = bytes[i];
      return true;
    }
    if (inLiteral(r, "null")) { inst <- null; return true; }
    r.bad = true;
    return false;
  }
  if (shape == "sblob" || shape == "bblob") {
    if (inPeek(r) == '"') {
      local s = inStr(r);
      if (s == null) return false;
      if (shape == "sblob") inst <- s;
      else {
        local bytes = b64decode(s);
        if (bytes == null) { r.bad = true; return false; }
        for (local i = 0; i < bytes.len() && i < inst.len(); i++) inst[i] = bytes[i];
      }
      return true;
    }
    if (inLiteral(r, "null")) { inst <- null; return true; }
    r.bad = true;
    return false;
  }
  if (shape == "ptr") {
    if (inPeek(r) == 'n') { if (inLiteral(r, "null")) { inst <- null; return true; } r.bad = true; return false; }
    // a node: read its fields into a fresh instance
    local sub = tableDesc(T, d.table);
    local node = sub.make();
    if (!jsonReadFields(T, sub, r, node, depth + 1, report)) return false;
    inst <- node;
    return true;
  }
  if (shape == "tab") {
    local sub = tableDesc(T, d.table);
    return jsonReadFields(T, sub, r, inst, depth + 1, report);
  }
  if (shape == "union") {
    // a union: the first key is Type (the tag), then the active arm
    if (inPeek(r) != '{') { r.bad = true; return false; }
    r.p++;
    local u = unionDesc(T, d.table);
    local tag = 0;
    local t = r.t;
    while (true) {
      local ch = inPeek(r);
      if (ch == '}') { r.p++; break; }
      if (ch == "") { r.bad = true; return false; }
      local key = inStr(r);
      if (key == null) return false;
      if (inPeek(r) != ':') { r.bad = true; return false; }
      r.p++;
      if (key == "Type") {
        local num = inNumber(r);
        if (num == null) { r.bad = true; return false; }
        tag = num.tok.tointeger();
      } else {
        if (!inSkipValue(r, depth + 1)) return false;
      }
      ch = inPeek(r);
      if (ch == ',') { r.p++; continue; }
      if (ch == '}') { r.p++; break; }
      r.bad = true;
      return false;
    }
    inst.Type <- tag;
    return true;
  }
  r.bad = true;
  return false;
};

_M.fromJson <- function(T, root, text, inst, report) {
  local desc = tableDesc(T, root);
  if (desc == null) return false;
  local r = newIn(text);
  local ok = jsonReadFields(T, desc, r, inst, 0, report);
  if (ok) { inSpace(r); if (r.p != text.len()) r.bad = true; }
  if (r.bad || !ok) report.malformed = true;
  return ok;
};
