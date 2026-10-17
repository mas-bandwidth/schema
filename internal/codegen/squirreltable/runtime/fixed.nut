// THE FIXED FORM READER and WRITER (docs/SPEC-TABLES.md §3.4,
// docs/FIXED-FORM-ALGORITHM.md). The generated per-table code builds the
// header, the layout and the record template; these are the shared byte
// primitives: little-endian composition from bytes, so nothing depends on the
// host's order (docs/PORT-SQUIRREL.md, BYTE BUFFERS).
local _M = vargv[0];

_M.blobOfHex <- function(s) {
  local nib = function(c) { return c <= '9' ? c - '0' : c - 'a' + 10; };
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
