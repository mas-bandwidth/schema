// THE FIXED FORM READER and WRITER (docs/SPEC-TABLES.md §3.4, docs/FIXED-FORM-ALGORITHM.md).
// This runtime is emitted as schema_table_fixed.nut beside the modules. The
// generated per-table code in fixedform.go builds the header, the layout and
// the record template; the helpers here are the shared byte primitives a write
// and a read both use: little-endian loads and stores composed from bytes, so
// nothing depends on the host's byte order (docs/PORT-SQUIRREL.md, BYTE
// BUFFERS), and the fixed-form file's framing checks.
local _M = vargv[0];
local _require = vargv[1];

_M.u64le <- function(b, at) {
  local lo = b[at] | b[at+1] << 8 | b[at+2] << 16 | b[at+3] << 24;
  local hi = b[at+4] | b[at+5] << 8 | b[at+6] << 16 | b[at+7] << 24;
  return lo + hi * 4294967296.0;
};

_M.putu64le <- function(b, at, v) {
  local lo = v.tointeger() & 0xFFFFFFFF;
  local hi = ((v - lo) / 4294967296.0).tointeger();
  b[at] = lo & 0xff; b[at+1] = (lo >> 8) & 0xff; b[at+2] = (lo >> 16) & 0xff; b[at+3] = (lo >> 24) & 0xff;
  b[at+4] = hi & 0xff; b[at+5] = (hi >> 8) & 0xff; b[at+6] = (hi >> 16) & 0xff; b[at+7] = (hi >> 24) & 0xff;
};

_M.u32le <- function(b, at) {
  return b[at] | b[at+1] << 8 | b[at+2] << 16 | b[at+3] << 24;
};
