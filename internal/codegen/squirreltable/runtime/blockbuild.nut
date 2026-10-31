// THE BLOCK FORM in Squirrel (docs/SPEC-TABLES.md §19.1): the BUILDER.
//
// One extent, 64-byte aligned at its base, laid out in this order: the
// PROJECTION at offset 0 — the three uint64 prologue words `magic`,
// `build_version` and `byte_order`, then the table's own fields — and then
// each out-of-line array in declaration order, at max(64, alignof(element)).
// Rows sit at `offset_of + i * stride`; the used extent is the greatest
// `offset_of + count * stride`, rounded up to 64, never less than the
// projection's own size (§19.1).
//
// STORAGE IS THE CALLER'S, ASKED EXACTLY ONCE. A Squirrel blob has no address,
// so the base is the caller's named lead into that blob, exactly as the cook
// and the block reader name theirs. Create calls the allocator once; Begin,
// the accessors and the writers call nothing.
//
// THE BUILDER IS SINGLE-THREADED, and §19.1's multi-threaded fill is NOT
// CARRIED: that obligation is a host-language property, and this leg has one
// thread. The caller's fill is a serial loop.
//
// Begin REFUSES a negative count and a count past its declared maximum, and
// the refusal names the array, its count and its maximum; it does not clamp
// (§19.1). The layout is settled from the counts before any worker starts.
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

// writeRaw writes a little-endian fixed-width scalar (§19.1's prologue and
// triples, and §19.3's C ABI slots).
local writeRaw = function(b, at, v, n) {
  for (local i = 0; i < n; i++) { b[at + i] = v & 0xff; v = v >> 8; }
};

local alignUp = function(v, a) {
  return ((v + a - 1) / a) * a;
};

// A FLOAT32 IS NARROWED BY BIT SURGERY, preserving the NaN payload exactly and
// rounding to nearest, ties to even — the same round the save runtime makes
// (docs/PORT-SQUIRREL.md, 64-BIT AND 128-BIT INTEGERS).
local roundShift = function(full, shift) {
  if (shift <= 0) return full << (-shift);
  local q = full >>> shift;
  local rem = full & ((1 << shift) - 1);
  local half = 1 << (shift - 1);
  if (rem > half || (rem == half && (q & 1) != 0)) q = q + 1;
  return q;
};

local narrowF32 = function(x) {
  local b = castf2i(x);
  local sign = (b >>> 63) & 1;
  local exp = (b >>> 52) & 0x7ff;
  local mant = b & 0xfffffffffffff;
  local out;
  if (exp == 0x7ff) {
    if (mant == 0) { out = (sign << 31) | 0x7f800000; return out; }
    local payload = (mant >>> 29) & 0x007fffff;
    if (payload == 0) payload = 0x00400000;
    out = (sign << 31) | 0x7f800000 | payload;
    return out;
  }
  if (exp == 0) { return sign << 31; }
  local e = exp - 1023;
  if (e < -126) {
    local s = -e - 97;
    if (s >= 54) return sign << 31;
    local N = 0x10000000000000 | mant;
    local half = 1 << (s - 1);
    local q = (N + half) >>> s;
    if (((N & ((1 << s) - 1)) == half) && (q & 1)) q = q - 1;
    if (q == 0x800000) return (sign << 31) | 0x00800000;
    return (sign << 31) | (q & 0x007fffff);
  }
  local full = 0x10000000000000 | mant;
  local q = roundShift(full, 29);
  if (q >= 0x1000000) { q = q >>> 1; e = e + 1; }
  if (e > 127) return (sign << 31) | 0x7f800000;
  return (sign << 31) | ((e + 127) << 23) | (q & 0x007fffff);
};

local formOf = function(T, root) {
  return ("blockForms" in T) && (root in T.blockForms) ? T.blockForms[root] : null;
};

// Create takes the caller's alloc/free pair plus a named base lead and asks the
// allocator EXACTLY ONCE (§19.1). The default is the standard blob class.
_M.Create <- function(T, root, alloc, free, lead) {
  local form = formOf(T, root);
  if (form == null) return null;
  if (lead == null) lead = 0;
  if (lead < 0 || (lead % T.TableBlockAlign) != 0) return null;
  if (alloc == null) alloc = function(n) { return blob(n); };
  if (free == null) free = function(b) {};
  local want = form.maxBytes + 63;
  local raw = alloc(want);
  if (raw == null || raw.len() < want) { if (raw != null) free(raw); return null; }
  return { created = true, blob = raw, Base = lead, alloc = alloc, free = free, form = form, maxBytes = form.maxBytes };
};

// Destroy releases through the same pair, once; a destroyed storage refuses
// Begin rather than pointing at bytes it no longer owns (§19.1).
_M.Destroy <- function(T, storage) {
  if (storage == null || !storage.created) return false;
  storage.free(storage.blob);
  storage.created = false;
  storage.blob = null;
  storage.alloc = null;
  storage.free = null;
  return true;
};

// Begin writes the prologue and every array's offset_of, count and stride. It
// touches no row, and it refuses before any byte changes (§19.1).
_M.Begin <- function(T, root, storage, counts, refusal) {
  if (refusal != null) { refusal.Array <- null; refusal.Count <- 0; refusal.Maximum <- 0; }
  local form = formOf(T, root);
  if (form == null) return null;
  if (storage == null || !storage.created) return null;
  foreach (a in form.arrays) {
    local c = (counts != null && (a.name in counts)) ? counts[a.name] : 0;
    if (typeof c != "integer" || c < 0 || c > a.max) {
      if (refusal != null) { refusal.Array <- a.name; refusal.Count <- c; refusal.Maximum <- a.max; }
      return null;
    }
  }
  local blob = storage.blob;
  local origin = storage.Base;
  // THE PROJECTION AT OFFSET 0: magic, build_version, byte_order (§19.1).
  local magic = T.TableBlockMagicBytes;
  for (local i = 0; i < magic.len(); i++) blob[origin + i] = magic[i] & 0xff;
  local version = T.BuildVersionBytes;
  for (local i = 0; i < version.len(); i++) blob[origin + 8 + i] = version[i] & 0xff;
  for (local i = 0; i < 8; i++) blob[origin + 16 + i] = (i == 0) ? 1 : 0;
  local offset = form.projection.size;
  foreach (a in form.arrays) {
    local c = (counts != null && (a.name in counts)) ? counts[a.name] : 0;
    offset = alignUp(offset, a.align);
    writeRaw(blob, origin + a.offsetOfOffset, offset, 8);
    writeRaw(blob, origin + a.countOffset, c, 4);
    writeRaw(blob, origin + a.strideOffset, a.stride, 4);
    offset += c * a.stride;
  }
  local used = alignUp(offset, T.TableBlockAlign);
  if (used < form.projection.size) used = form.projection.size;
  return { Blob = blob, Base = origin, Used = used, Form = form, Counts = counts };
};

// Bytes is the used extent: proportional to the frame, not to the maxima
// (§19.1).
_M.Bytes <- function(T, block) {
  return block == null ? 0 : block.Used;
};

// Rows is one out-of-line array's typed base: an accessor is one add, block
// base + offset_of (§19.1).
_M.Rows <- function(T, block, name) {
  if (block == null) return null;
  foreach (a in block.Form.arrays) {
    if (a.name == name) {
      local offsetOf = u64le(block.Blob, block.Base + a.offsetOfOffset);
      local count = u32le(block.Blob, block.Base + a.countOffset);
      return { At = block.Base + offsetOf, Count = count, Stride = a.stride, Elem = a.elem, Name = a.name };
    }
  }
  return null;
};

local findField = function(info, name) {
  foreach (f in info.fields) if (f.name == name) return f;
  return null;
};

local splitPath = function(path) {
  local out = [];
  local start = 0;
  for (local i = 0; i <= path.len(); i++) {
    local c = (i < path.len()) ? path[i] : '.';
    if (c == '.') { out.append(path.slice(start, i)); start = i + 1; }
  }
  return out;
};

local writeScalar = function(b, at, f, value) {
  local n = f.elemSize;
  if (f.storage == "Bool") { b[at] = value ? 1 : 0; return true; }
  if (f.storage == "Float") {
    // A NaN OR AN INFINITY IS REFUSED (docs/SPEC-TABLES.md §3.3): the
    // builder writes no byte and Set answers false, SetReason naming
    // non_finite_float; a float32 must stay finite at float32
    local bits = (n == 4) ? narrowF32(value.tofloat()) : castf2i(value.tofloat());
    local finite = (n == 4) ? (bits & 0x7f800000) != 0x7f800000 : (bits & 0x7ff0000000000000) != 0x7ff0000000000000;
    if (!finite) { _M.SetReason <- "non_finite_float"; return false; }
    writeRaw(b, at, bits, n);
    return true;
  }
  if (f.storage == "Signed" || f.storage == "Unsigned") {
    writeRaw(b, at, value, n);
    return true;
  }
  return false;
};

local writeValue = function(b, f, at, idx, value) {
  if (f.counted && (f.storage == "String" || f.storage == "Bytes")) {
    // the fixed buffer stands where it is; the used length is beside it
    // (§19.3, §2.7)
    local len = (value == null) ? 0 : value.len();
    for (local i = 0; i < len; i++) b[at + i] = value[i] & 0xff;
    writeRaw(b, at - f.offset + f.countOffset, len, 4);
    return true;
  }
  if (f.counted && f.storage == "WString") {
    // WIDE TEXT: the code units two bytes each, the used length in units
    // beside them (§19.3, §7.2); a slot that is not a code unit refuses
    if (typeof value != "array" || value.len() > f.bound) return false;
    foreach (i, u in value) {
      if (typeof u != "integer" || u < 0 || u > 0xFFFF) return false;
      writeRaw(b, at + 2 * i, u, 2);
    }
    writeRaw(b, at - f.offset + f.countOffset, value.len(), 4);
    return true;
  }
  if (f.isArray) {
    if (idx != null) return writeScalar(b, at, f, value);
    if (typeof value != "array") return false;
    // the whole array refuses before any byte changes when one element is
    // a NaN or an infinity (docs/SPEC-TABLES.md §3.3)
    if (f.storage == "Float") {
      local probe = blob(8);
      foreach (x in value) if (!writeScalar(probe, 0, f, x)) return false;
    }
    for (local i = 0; i < value.len(); i++) writeScalar(b, at + i * f.elemSize, f, value[i]);
    return true;
  }
  return writeScalar(b, at, f, value);
};

local setPath;
setPath = function(T, block, info, at, segs, k, value) {
  local seg = segs[k];
  local name = seg, idx = null;
  local open = seg.find("[");
  if (open != null) {
    name = seg.slice(0, open);
    idx = seg.slice(open + 1, -1).tointeger();
  }
  local present = false;
  if (name.len() > 8 && name.slice(-8) == ":present") { present = true; name = name.slice(0, -8); }
  local f = findField(info, name);
  if (f == null) return false;
  local target = at + f.offset;
  // An enum-keyed array's path index is the enum's ordinal, dense from 1
  // (§2.4); the slot it names is zero-based. The marks are emitted beside the
  // reader's records, which do not carry them.
  local keyed = ("blockKeyed" in T) && (info.name in T.blockKeyed) && (name in T.blockKeyed[info.name]);
  if (idx != null && !f.outOfLine) target += ((keyed ? idx - 1 : idx) * f.elemSize);
  if (k + 1 < segs.len()) {
    if (f.table == null) return false;
    return setPath(T, block, T.blockRecords[f.table], target, segs, k + 1, value);
  }
  if (present) {
    if (!f.optional || f.presentOffset < 0) return false;
    block.Blob[at + f.presentOffset] = value ? 1 : 0;
    return true;
  }
  return writeValue(block.Blob, f, target, idx, value);
};

// Set writes one field through the SAME descriptors the reader walks
// (§19.2): a dotted path, with `[i]` for a fixed-array slot and `:present` for
// an optional's companion.
_M.Set <- function(T, block, record, at, path, value) {
  _M.SetReason <- null;
  if (block == null) return false;
  local info = (record == null || record == block.Form.name) ? block.Form.projection : T.blockRecords[record];
  if (info == null) return false;
  return setPath(T, block, info, at, splitPath(path), 0, value);
};
