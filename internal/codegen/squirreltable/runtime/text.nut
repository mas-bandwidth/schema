// THE BASE64 TEXT CARRIER (docs/SPEC-TABLES.md §16.8): SaveText is the
// padded standard-alphabet base64 of the whole variable binary wire, and
// LoadText is the strict read over the decoded wire. The base64 codec is
// written here in Squirrel because the pinned interpreter ships none. A text
// that is not the canonical encoding of its bytes is base64_not_canonical; a
// body that does not decode is base64_damaged; a decoded wire the binary
// reader refuses keeps the binary reader's name (§2.14, §16.8).
local _M = vargv[0];
local _require = vargv[1];
// required once here, never per call (a require walks the name)
local S = _require("schema_table_save");
local R = _require("schema_table_refusal");

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

// The padded standard encoding's length for n bytes, Go's
// base64.StdEncoding.EncodedLen (docs/SPEC-TABLES.md §16.8).
local b64encodedLen = function(n) { return ((n + 2) / 3) * 4; };

// b64decode(s, strict) is the padded standard decode of §16.8, dropping CR and
// LF bytes as Go's DecodeString does. strict also requires the pad bits a final
// quantum drops to be zero, as Go's Strict() does: a pad bit set is
// base64_not_canonical, not damage. null when the body is not padded standard
// base64 at all — a byte outside the alphabet and '=', a missing, extra or
// misplaced '=', or a body that does not decode — which LoadText names
// base64_damaged. Cite docs/SPEC-TABLES.md §16.8.
local b64decode = function(s, strict) {
  // CR and LF are ignored anywhere, as encoding/base64's decoder ignores them.
  local clean = "";
  for (local k = 0; k < s.len(); k++) {
    local ch = s[k];
    if (ch != '\r' && ch != '\n') clean += format("%c", ch);
  }
  if (clean.len() % 4 != 0) return null;
  local out = blob(0);
  local i = 0;
  local n = clean.len();
  while (i < n) {
    local a = b64val(clean[i]), b = b64val(clean[i + 1]);
    if (a < 0 || b < 0) return null;
    local c = b64val(clean[i + 2]), d = b64val(clean[i + 3]);
    local pad = 0;
    if (clean[i + 2] == '=') {
      // a two-character pad is the tail's alone, and '=' is the last byte
      if (clean[i + 3] != '=' || i + 4 != n) return null;
      pad = 2;
    } else if (c < 0) {
      return null;
    } else if (clean[i + 3] == '=') {
      // a one-character pad is the tail's alone, and '=' is the last byte
      if (i + 4 != n) return null;
      pad = 1;
    } else if (d < 0) {
      return null;
    }
    // the bits a padded quantum drops are zero in the canonical encoding
    if (strict) {
      if (pad == 2 && (b & 0x0f) != 0) return null;
      if (pad == 1 && (c & 0x03) != 0) return null;
    }
    out.writen(((a << 2) | (b >> 4)) & 0xff, 'b');
    if (pad < 2) out.writen((((b & 0xf) << 4) | (c >> 2)) & 0xff, 'b');
    if (pad < 1) out.writen((((c & 3) << 6) | d) & 0xff, 'b');
    i += 4;
  }
  return out;
};

// ---- THE SURFACE ----
// SaveText(T, rootName, value) returns the padded base64 of the binary wire.
// The wire itself is the table save's (docs/SPEC-TABLES.md §3, §3.1): the text
// carrier adds no framing of its own (§16.8).
_M.SaveText <- function(T, rootName, value) {
  local wire = S.save(T, rootName, value);
  if (wire == null) return null;
  return b64encode(wire);
};

// LoadText(T, rootName, text, report): the strict read over the decoded wire
// (docs/SPEC-TABLES.md §6.5, §16.8). A text whose padded standard base64 is
// not the canonical encoding of its bytes is base64_not_canonical — a pad bit
// set, or a CR or LF present — and every other base64 failure is
// base64_damaged (§16.8). A decoded wire then goes through ReadStrict (§6.5),
// so a refusal carries the binary reader's name and field; unknown, widened and
// duplicate alone do not refuse.
_M.LoadText <- function(T, rootName, text, report) {
  if (!("unknown" in report)) report.unknown <- 0;
  if (!("kindMismatch" in report)) report.kindMismatch <- 0;
  if (!("widened" in report)) report.widened <- 0;
  if (!("clamped" in report)) report.clamped <- 0;
  if (!("duplicate" in report)) report.duplicate <- 0;
  if (!("malformed" in report)) report.malformed <- false;
  if (!("refused" in report)) report.refused <- false;
  report.reason <- null;
  report.field <- null;
  // The oracle's order (Go's emitted LoadText, schema-go d808c3f54): the
  // strict decode first, the plain decode to tell not-canonical from damaged,
  // then the encoded-length check that catches a CR or LF (§16.8).
  local wire = b64decode(text, true);
  if (wire == null) {
    report.refused = true;
    report.reason = b64decode(text, false) == null ? "base64_damaged" : "base64_not_canonical";
    return null;
  }
  if (text.len() != b64encodedLen(wire.len())) {
    report.refused = true;
    report.reason = "base64_not_canonical";
    return null;
  }
  local value = R.ReadStrict(T, rootName, wire, report);
  if (value == null) {
    report.refused = true;
    return null;
  }
  return value;
};
