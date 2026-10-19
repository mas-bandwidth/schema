// THE COOKED FORM in Squirrel (docs/SPEC-TABLES.md §7): the READ side.
//
// A cook is a LOAD-TRUSTED-DATA-FROM-TOOLS format, not a wire. Tooling writes
// a region for one build version and that build points at it: Open matches the
// header and returns the root where it lies. There is no walk, no fix-up and
// no allocation beyond one handle, which is what makes Open O(1) in the
// file's size.
//
// A COOK IS TRUSTED INPUT, LOADED FROM DISK. Open's checks are IDENTITY checks
// — is this file for THIS build — and not a trust boundary: there is NO
// PER-NODE VALIDATION AT LOAD, ever.
//
// Bytes that are not a cook of this build are refused with a named refusal, and
// the report's `refused` is set true (§11); a CALLER's error throws, naming
// the fix.
local _M = vargv[0];

// The header words carry unsigned 64-bit values that can exceed Squirrel's
// signed integer range, so the big words (the magic, the byte order, the build
// version, the reserved words) are compared BYTE-WISE against the byte arrays
// the module emits. The part lengths and the alignment are small counts, read
// as integers.
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

_M.Cook <- function(T, root, bytes, report) {
  local form = ("cookForms" in T) && (root in T.cookForms) ? T.cookForms[root] : null;
  if (form == null) return refuse(report, "bad_layout");
  if (bytes.len() < T.TableCookHeaderBytes) return refuse(report, "truncated");
  if (!eqB(bytes, 0, T.TableCookMagicBytes)) return refuse(report, "not_a_cook");
  if (!eqB(bytes, 16, T.TableCookByteOrderBytes)) return refuse(report, "foreign_order");
  if (!eqB(bytes, 8, T.BuildVersionBytes)) return refuse(report, "wrong_build_version");
  local zero = [0, 0, 0, 0, 0, 0, 0, 0];
  if (!eqB(bytes, 48, zero) || !eqB(bytes, 56, zero)) return refuse(report, "reserved_not_zero");
  local alignment = u64le(bytes, 40);
  if (alignment < 8 || alignment > T.TableCookMaxAlign) return refuse(report, "bad_alignment");
  if ((alignment & (alignment - 1)) != 0) return refuse(report, "bad_alignment");
  if ((alignment % form.align) != 0) return refuse(report, "bad_alignment");
  local dataOffset = (64 + alignment - 1) & ~(alignment - 1);
  local dataLength = u64le(bytes, 24);
  local attribution = u64le(bytes, 32);
  if (dataLength > bytes.len() || attribution > bytes.len() - dataLength) return refuse(report, "truncated");
  if (dataOffset > bytes.len() - dataLength - attribution) return refuse(report, "bad_layout");
  if (dataOffset + dataLength + attribution != bytes.len()) return refuse(report, "truncated");
  if (dataLength < form.size) return refuse(report, "truncated");
  return { Bytes = bytes, Region = dataOffset, Length = dataLength };
};
