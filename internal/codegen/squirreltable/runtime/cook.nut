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

_M.Cook <- function(T, root, bytes, report, lead = null) {
  local form = ("cookForms" in T) && (root in T.cookForms) ? T.cookForms[root] : null;
  if (form == null) return refuse(report, "bad_layout");
  if (bytes == null) return refuse(report, "unaligned_base");
  if (bytes.len() < T.TableCookHeaderBytes) return refuse(report, "truncated");
  // THE MAGIC IS READ BYTEWISE, before anything else: it establishes the byte
  // order every other header word is written in (§7). The SAME constant
  // byte-reversed is a cook of the other order, and a byte_order word that
  // contradicts the magic is not a cook in either order (§7, §7.1).
  if (!eqB(bytes, 0, T.TableCookMagicBytes)) {
    local reversed = [];
    for (local i = T.TableCookMagicBytes.len() - 1; i >= 0; i--) reversed.append(T.TableCookMagicBytes[i]);
    if (eqB(bytes, 0, reversed)) return refuse(report, "foreign_order");
    return refuse(report, "not_a_cook");
  }
  if (!eqB(bytes, 16, T.TableCookByteOrderBytes)) return refuse(report, "not_a_cook");
  if (!eqB(bytes, 8, T.BuildVersionBytes)) return refuse(report, "wrong_build_version");
  local zero = [0, 0, 0, 0, 0, 0, 0, 0];
  if (!eqB(bytes, 48, zero) || !eqB(bytes, 56, zero)) return refuse(report, "reserved_not_zero");
  local dataLength = u64le(bytes, 24);
  local attribution = u64le(bytes, 32);
  local alignment = u64le(bytes, 40);
  // THE ALIGNMENT WORD IS DATA (§7): a power of two, at least eight, at most
  // sixty-four, and a multiple of the ROOT's alignof. It is checked before it
  // is used to derive the data offset, so bad_alignment precedes truncated.
  if (alignment < 8 || alignment > T.TableCookMaxAlign) return refuse(report, "bad_alignment");
  if ((alignment & (alignment - 1)) != 0) return refuse(report, "bad_alignment");
  if ((alignment % form.align) != 0) return refuse(report, "bad_alignment");
  local dataOffset = (64 + alignment - 1) & ~(alignment - 1);
  // THE TWO PART LENGTHS against the caller's length, subtracted rather than
  // added so no sum can carry: dataOffset + data + attribution is the whole
  // file, and anything else is truncated or has trailing bytes (§7).
  if (bytes.len() < dataOffset) return refuse(report, "truncated");
  if (dataLength > bytes.len() - dataOffset) return refuse(report, "truncated");
  if (attribution != bytes.len() - dataOffset - dataLength) return refuse(report, "truncated");
  if (dataLength < form.size) return refuse(report, "truncated");
  // THE BASE'S ALIGNMENT is the CALLER's placement, not the file's (§7): a
  // Squirrel blob has no address, so the caller names the lead its cook starts
  // at in the buffer it holds (the analogue of a view's byteOffset), and a
  // lead the region's alignment does not divide is unaligned_base.
  if (lead != null && (lead % alignment) != 0) return refuse(report, "unaligned_base");
  return { Bytes = bytes, Region = dataOffset, Length = dataLength };
};
