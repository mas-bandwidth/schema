// THE FIXED FORM READER and WRITER (docs/SPEC-TABLES.md §3.4,
// docs/FIXED-FORM-ALGORITHM.md). The generated per-table code builds the
// header, the layout and the record template; these are the shared byte
// primitives: little-endian composition from bytes, so nothing depends on the
// host's order (docs/PORT-SQUIRREL.md, BYTE BUFFERS).
local _M = vargv[0];

local nib = function(c) { return c <= '9' ? c - '0' : c - 'a' + 10; };

_M.blobOfHex <- function(s) {
  local out = blob(s.len() / 2);
  for (local i = 0; i < s.len(); i += 2) out[i / 2] = nib(s[i]) * 16 + nib(s[i + 1]);
  return out;
};

_M.u64le <- function(b, at) {
  local lo = b[at] | b[at+1] << 8 | b[at+2] << 16 | b[at+3] << 24;
  local hi = b[at+4] | b[at+5] << 8 | b[at+6] << 16 | b[at+7] << 24;
  return lo | (hi << 32);
};

_M.u32le <- function(b, at) {
  return b[at] | b[at+1] << 8 | b[at+2] << 16 | b[at+3] << 24;
};

_M.puti32 <- function(b, at, v) {
  b[at] = v & 0xff; b[at+1] = (v >> 8) & 0xff; b[at+2] = (v >> 16) & 0xff; b[at+3] = (v >> 24) & 0xff;
};

_M.geti32 <- function(b, at) {
  local v = _M.u32le(b, at);
  return v >= 0x80000000 ? v - 4294967296 : v;
};

// ONE LEAF AT ITS DECLARED STORAGE WIDTH, SIGN-EXTENDED (SPEC §4.3).
_M.get <- function(b, at, w) {
  if (w == 1) { local v = b[at]; return v >= 128 ? v - 256 : v; }
  if (w == 2) { local v = b[at] | b[at+1] << 8; return v >= 0x8000 ? v - 65536 : v; }
  if (w == 4) { return _M.geti32(b, at); }
  return _M.u64le(b, at);
};

_M.put <- function(b, at, w, v) {
  for (local i = 0; i < w; i++) { b[at + i] = (v >> (8 * i)) & 0xff; }
};

// A FIXED-FORM FLOAT IS ITS IEEE-754 BIT PATTERN, AND FINITE
// (docs/SPEC-TABLES.md §3.3, §3.4, the packet wire's rule): a writer refuses a NaN or an infinity, a float32 one that
// narrows to an infinity included, and a read refuses one on its RAW BITS
// before the field is written, as non_finite_float. Every finite float32
// narrows and widens exactly through the blob's 'f' view (the C conversion,
// round to nearest, ties to even), -0 and the subnormals included.
local scratch = blob(8);
local f32Bits = function(v) {
  scratch.seek(0); scratch.writen(v.tofloat(), 'f');
  scratch.seek(0); return scratch.readn('i') & 0xffffffff;
};
_M.putF32 <- function(b, at, v) {
  local bits = f32Bits(v);
  if ((bits & 0x7f800000) == 0x7f800000) return false;
  _M.put(b, at, 4, bits);
  return true;
};
_M.putF64 <- function(b, at, v) {
  local bits = castf2i(v.tofloat());
  if ((bits & 0x7ff0000000000000) == 0x7ff0000000000000) return false;
  _M.put(b, at, 8, bits);
  return true;
};
// getF32 / getF64: the value, or null for a NaN or an infinity on the bits
_M.getF32 <- function(b, at) {
  local bits = _M.u32le(b, at);
  if ((bits & 0x7f800000) == 0x7f800000) return null;
  scratch.seek(0); scratch.writen(bits, 'i');
  scratch.seek(0); return scratch.readn('f');
};
_M.getF64 <- function(b, at) {
  local bits = _M.u64le(b, at);
  if ((bits & 0x7ff0000000000000) == 0x7ff0000000000000) return null;
  return casti2f(bits);
};

// textIllFormed reports whether the string payload at b[at..at+n) is NOT the
// text its kind says (docs/FIXED-FORM-ALGORITHM.md §4.5, §5.4 fix 11): UTF-8
// with no zero byte. A zero byte, an overlong or surrogate sequence, a value
// past U+10FFFF, an invalid lead, or a truncated tail is ill-formed. It
// mirrors tableFixedTextIllFormed in the Go leg.
_M.textIllFormed <- function(b, at, n) {
  local i = 0;
  while (i < n) {
    local c = b[at + i] & 0xff;
    if (c == 0) return true;
    if (c < 0x80) { i++; continue; }
    if (c < 0xC2) return true;
    if (c < 0xE0) {
      if (i + 2 > n) return true;
      if ((b[at+i+1] & 0xc0) != 0x80) return true;
      i += 2; continue;
    }
    if (c < 0xF0) {
      if (i + 3 > n) return true;
      local c1 = b[at+i+1] & 0xff;
      if (c == 0xE0 && c1 < 0xA0) return true;
      if (c == 0xED && c1 > 0x9F) return true;
      if ((c1 & 0xc0) != 0x80) return true;
      if ((b[at+i+2] & 0xc0) != 0x80) return true;
      i += 3; continue;
    }
    if (c < 0xF5) {
      if (i + 4 > n) return true;
      local c1 = b[at+i+1] & 0xff;
      if (c == 0xF0 && c1 < 0x90) return true;
      if (c == 0xF4 && c1 > 0x8F) return true;
      if ((c1 & 0xc0) != 0x80) return true;
      if ((b[at+i+2] & 0xc0) != 0x80) return true;
      if ((b[at+i+3] & 0xc0) != 0x80) return true;
      i += 4; continue;
    }
    return true;
  }
  return false;
};

// wideUnits reads n UTF-16 code units at b[at..at+2n), two bytes each
// little-endian, into the array wide-text storage holds, or answers null when
// they are NOT wide text (docs/FIXED-FORM-ALGORITHM.md §4.5, §5.4 fix 11;
// SPEC.md §4.12): a zero unit, a high surrogate not followed by a low one, or
// a low surrogate first. It mirrors the Go leg's wide text check.
_M.wideUnits <- function(b, at, n) {
  local units = array(n);
  for (local i = 0; i < n; i++) units[i] = (b[at + 2 * i] & 0xff) | ((b[at + 2 * i + 1] & 0xff) << 8);
  for (local i = 0; i < n; i++) {
    local u = units[i];
    if (u == 0) return null;
    if (u >= 0xD800 && u <= 0xDBFF) {
      if (i + 1 >= n || units[i + 1] < 0xDC00 || units[i + 1] > 0xDFFF) return null;
      i++;
    } else if (u >= 0xDC00 && u <= 0xDFFF) return null;
  }
  return units;
};
