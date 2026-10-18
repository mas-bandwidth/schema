// THE BASE64 TEXT CARRIER (docs/SPEC-TABLES.md §16.8): SaveText is the
// padded standard-alphabet base64 of the whole variable binary wire, and
// LoadText is the strict read over the decoded wire. The base64 codec is
// written here in Squirrel because the pinned interpreter ships none. A body
// that does not decode is base64_damaged; a decoded wire the binary reader
// refuses keeps the binary reader's name (§2.14, §16.8).
local _M = vargv[0];
local _require = vargv[1];

// ---- BASE64 codec (standard alphabet, padded; docs/SPEC-TABLES.md §16.8) ----
local B64 = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/";
local b64encode = function(b) {
  local out = "";
  local i = 0;
  local n = b.len();
  while (n - i >= 3) {
    local t = (b[i] << 16) | (b[i + 1] << 8) | b[i + 2];
    out += format("%c", B64[(t >> 18) & 63]) + format("%c", B64[(t >> 12) & 63]) +
           format("%c", B64[(t >> 6) & 63]) + format("%c", B64[t & 63]);
    i += 3;
  }
  if (i < n) {
    local left = n - i;
    local t = b[i] << 16;
    if (left == 2) t = t | (b[i + 1] << 8);
    out += format("%c", B64[(t >> 18) & 63]) + format("%c", B64[(t >> 12) & 63]);
    out += left == 2 ? format("%c", B64[(t >> 6) & 63]) : "=";
    out += "=";
  }
  return out;
};

local b64val = function(c) {
  if (c >= 'A' && c <= 'Z') return c - 'A';
  if (c >= 'a' && c <= 'z') return c - 'a' + 26;
  if (c >= '0' && c <= '9') return c - '0' + 52;
  if (c == '+') return 62;
  if (c == '/') return 63;
  return -1;
};

local b64decode = function(s) {
  if (s.len() % 4 != 0) return null;
  local out = blob(0);
  local i = 0;
  local n = s.len();
  while (i + 4 <= n) {
    local a = b64val(s[i]), b = b64val(s[i + 1]);
    if (a < 0 || b < 0) return null;
    local c = b64val(s[i + 2]), d = b64val(s[i + 3]);
    if (c < 0 && s[i + 2] != '=') return null;
    if (d < 0 && s[i + 3] != '=') return null;
    // padding is the tail's alone, and a padded quantum carries no more
    if (s[i + 3] == '=' && i + 4 != n) return null;
    if (s[i + 2] == '=' && s[i + 3] != '=') return null;
    out.writen(((a << 2) | (b >> 4)) & 0xff, 'b');
    if (s[i + 2] != '=') {
      if (c < 0) return null;
      out.writen((((b & 0xf) << 4) | (c >> 2)) & 0xff, 'b');
    }
    if (s[i + 3] != '=') {
      if (d < 0) return null;
      out.writen((((c & 3) << 6) | d) & 0xff, 'b');
    }
    i += 4;
  }
  return out;
};

// ---- THE SURFACE ----
// SaveText(T, rootName, value) returns the padded base64 of the binary wire.
// The wire itself is the table save's (docs/SPEC-TABLES.md §3, §3.1): the text
// carrier adds no framing of its own (§16.8).
_M.SaveText <- function(T, rootName, value) {
  local S = _require("schema_table_save");
  local wire = S.save(T, rootName, value);
  if (wire == null) return null;
  return b64encode(wire);
};

// LoadText(T, rootName, text, report): the strict read over the decoded wire
// (docs/SPEC-TABLES.md §6.5, §16.8). A body that does not decode is
// base64_damaged; a decoded wire the binary reader refuses keeps the binary
// reader's name. Unknown and widened alone do not refuse.
_M.LoadText <- function(T, rootName, text, report) {
  if (!("unknown" in report)) report.unknown <- 0;
  if (!("kindMismatch" in report)) report.kindMismatch <- 0;
  if (!("widened" in report)) report.widened <- 0;
  if (!("clamped" in report)) report.clamped <- 0;
  if (!("duplicate" in report)) report.duplicate <- 0;
  if (!("malformed" in report)) report.malformed <- false;
  if (!("refused" in report)) report.refused <- false;
  local wire = b64decode(text);
  if (wire == null) { report.refused = true; return null; }
  local L = _require("schema_table_load");
  local value = L.load(T, rootName, wire, report);
  if (report.malformed || report.refused || report.kindMismatch > 0 ||
      report.clamped > 0 || report.duplicate > 0) {
    report.refused = true;
    return null;
  }
  return value;
};
