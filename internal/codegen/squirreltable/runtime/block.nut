// THE BLOCK FORM in Squirrel (docs/SPEC-TABLES.md §19): the READ side.
//
// A block is a fixed-pitch projection with out-of-line row arrays at 64-byte
// boundaries (§19.1). Open checks the prologue and every array triple once in
// O(1) and points at the caller's bytes without copying (§19.2).
//
// Bytes that are not a block of this build are refused with a named refusal,
// and the report's `refused` is set true (§11, §19.2).
local _M = vargv[0];

local u32le = function(b, at) {
  return (b[at] & 0xff) | ((b[at + 1] & 0xff) << 8) |
         ((b[at + 2] & 0xff) << 16) | ((b[at + 3] & 0xff) << 24);
};

local u64le = function(b, at) {
  local lo = (b[at] & 0xff) | ((b[at + 1] & 0xff) << 8) |
             ((b[at + 2] & 0xff) << 16) | ((b[at + 3] & 0xff) << 24);
  local hi = (b[at + 4] & 0xff) | ((b[at + 5] & 0xff) << 8) |
             ((b[at + 6] & 0xff) << 16) | ((b[at + 7] & 0xff) << 24);
  return lo | (hi << 32);
};

local eqB = function(b, at, want) {
  for (local i = 0; i < want.len(); i++) if (b[at + i] != want[i]) return false;
  return true;
};

// refuse names a refusal (§11) and returns null.
local refuse = function(report, reason) {
  if (report != null) { report.refused = true; report.reason <- reason; }
  return null;
};

_M.Block <- function(T, root, bytes, report, lead = null) {
  local form = ("blockForms" in T) && (root in T.blockForms) ? T.blockForms[root] : null;
  if (form == null) return refuse(report, "bad_layout");
  if (bytes == null) return refuse(report, "unaligned_base");
  if (bytes.len() < form.projection.size) return refuse(report, "truncated");
  // THE MAGIC IS READ BYTEWISE, before anything else: it establishes the byte
  // order every other prologue word is written in (§19.1, §19.2). The same
  // constant byte-reversed is a block of the other order, and a byte_order
  // word that contradicts the magic is not a block in either order.
  if (!eqB(bytes, 0, T.TableBlockMagicBytes)) {
    local reversed = [];
    for (local i = T.TableBlockMagicBytes.len() - 1; i >= 0; i--) reversed.append(T.TableBlockMagicBytes[i]);
    if (eqB(bytes, 0, reversed)) return refuse(report, "foreign_order");
    return refuse(report, "not_a_cook");
  }
  if (!eqB(bytes, 16, T.TableBlockByteOrderBytes)) return refuse(report, "not_a_cook");
  if (!eqB(bytes, 8, T.BuildVersionBytes)) return refuse(report, "wrong_build_version");
  local extent = bytes.len();
  local used = form.projection.size;
  // EVERY NUMBER BELOW COMES FROM THE INSTANCE, and each term is bounded
  // before it is added (§19.2): a forged offset_of near 2^63 or negative in
  // signed 64-bit arithmetic is refused before any sum can wrap.
  foreach (a in form.arrays) {
    local offsetOf = u64le(bytes, a.offsetOfOffset);
    local count = u32le(bytes, a.countOffset);
    local stride = u32le(bytes, a.strideOffset);
    if (stride != a.stride) return refuse(report, "bad_layout");
    if (count < 0 || count > a.max) return refuse(report, "bad_layout");
    if (offsetOf < form.projection.size || (offsetOf % a.align) != 0) return refuse(report, "bad_layout");
    if (offsetOf > extent) return refuse(report, "bad_layout");
    local rows = count * stride;
    if (rows > extent - offsetOf) return refuse(report, "bad_layout");
    local end = offsetOf + rows;
    if (end > used) used = end;
  }
  local align = T.TableBlockAlign;
  local padding = (align - (used % align)) % align;
  if (padding > extent - used) return refuse(report, "truncated");
  used += padding;
  if (lead != null && (lead % align) != 0) return refuse(report, "unaligned_base");
  return { Bytes = bytes, Used = used, Form = form };
};
