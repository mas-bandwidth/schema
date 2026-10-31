// THE VARIABLE TABLE WIRE'S LOAD (docs/SPEC-TABLES.md §3, §3.1, §4), driven
// by the descriptors internal/codegen/squirreltable emits (§8.1). The walk is
// a port of internal/codegen/jstable/variableruntime.go's THE LOAD,
// RESOLUTION, ONE BODY and AN ARRAY BODY, which are ports of
// internal/tablewire's decode.go, decodenodes.go and nodetable.go.
//
// _M.load(T, root, bytes, report) is the strict read: the root or null (§6.5).
// _M.loadTolerant(T, root, bytes, report) keeps the recovered root and counters.
// T is a unit's table module (the exports table schema_module.nut requires);
// root is the root table's schema name. report is the caller's §4 report: the
// six counters, the malformed flag and the refusal verdict. It is filled in
// place, never created here.
local _M = vargv[0];
local _require = vargv[1];
// required once here, never per call (a require walks the name)
local R = _require("schema_table_refusal");

// THE RESERVED IDS the language holds back (docs/SPEC-TABLES.md §3.1, §5).
local NODE_TABLE_ID = 0xffffffffffffffff;
local BUILD_VERSION_ID = 0xfffffffffffffffe;
local MESSAGE_VOCABULARY_ID = 0xfffffffffffffffd;
local STRING_BLOB_ID = 0x704be0d8faaffc58;
local BYTES_BLOB_ID = 0x2f2ec0474f1c4fe4;
local MAP_KEY_ID = 0x3dc94a19365b10ec;
local MAP_VALUE_ID = 0x7ce4fd9430e80cea;

local FORM_VARIABLE = 1;
// the id table's maximum entry count (docs/SPEC-TABLES.md §3; ir.TableWireIdTableBound)
local ID_TABLE_BOUND = 4096;

// CRC-32C, the Castagnoli polynomial, reflected 0x82F63B78, initial and final
// 0xFFFFFFFF (docs/SPEC-TABLES.md §2.14), the trailer an integrity root appends
// over every byte before it.
local crc32c = function(b, n) {
  local crc = 0xFFFFFFFF;
  for (local i = 0; i < n; i++) {
    crc = (crc ^ (b[i] & 0xff)) & 0xFFFFFFFF;
    for (local k = 0; k < 8; k++) {
      if (crc & 1) crc = ((crc >> 1) ^ 0x82F63B78) & 0xFFFFFFFF;
      else crc = (crc >> 1) & 0xFFFFFFFF;
    }
  }
  return (crc ^ 0xFFFFFFFF) & 0xFFFFFFFF;
};

local u32le = function(b, at) {
  return (b[at] & 0xff) | ((b[at + 1] & 0xff) << 8) |
         ((b[at + 2] & 0xff) << 16) | ((b[at + 3] & 0xff) << 24);
};

// §2.14's read order for an integrity root: the form byte is read first (the
// caller has checked it), then the trailer before any field is decoded. A file
// too short to hold one, or whose last four bytes are not the CRC-32C of the
// bytes before them, is the whole-file damage named integrity_mismatch;
// otherwise the trailer comes off and the rest reads as §3's file.
local integrityOpen = function(b) {
  if (b.len() < 5 || u32le(b, b.len() - 4) != crc32c(b, b.len() - 4)) return null;
  local out = blob(b.len() - 4);
  for (local i = 0; i < out.len(); i++) out[i] = b[i];
  return out;
};

local newReader = function(b, at, end) {
  return { b = b, at = at, end = end, bad = false };
};

local u8 = function(r) { local v = r.b[r.at] & 0xff; r.at++; return v; };

// LEB128 (docs/SPEC-TABLES.md §3): canonical, and a failed read leaves the
// cursor where it started. The last byte may not be zero, and the tenth byte
// carries only the top bit.
local leb = function(r) {
  local v = 0, shift = 0;
  local start = r.at;
  for (local i = 0; i < 10; i++) {
    if (r.at >= r.end) { r.bad = true; r.at = start; return 0; }
    local x = r.b[r.at] & 0xff;
    r.at++;
    if (i == 9 && x > 1) { r.bad = true; r.at = start; return 0; }
    if (i == 9) {
      // a value at or past 2^63 is not a length or a count this blob can
      // carry; keep a sentinel that every bound check refuses
      v = 0x4000000000000000;
    } else {
      v = v | ((x & 0x7f) << shift);
    }
    if ((x & 0x80) == 0) {
      if (i > 0 && x == 0) { r.bad = true; r.at = start; return 0; }
      return v;
    }
    shift += 7;
  }
  r.bad = true;
  r.at = start;
  return 0;
};

local u64At = function(b, off) {
  local lo = (b[off] & 0xff) | ((b[off + 1] & 0xff) << 8) |
             ((b[off + 2] & 0xff) << 16) | ((b[off + 3] & 0xff) << 24);
  local hi = (b[off + 4] & 0xff) | ((b[off + 5] & 0xff) << 8) |
             ((b[off + 6] & 0xff) << 16) | ((b[off + 7] & 0xff) << 24);
  return lo | (hi << 32);
};

// the kind's byte width (docs/SPEC-TABLES.md §3), and a timestamp is the
// int64's (SPEC.md §4.13)
local kindWidth = function(kind) {
  switch (kind) {
    case 1: case 2: case 6: case 20: case 25: return 1;
    case 3: case 7: case 21: case 26: return 2;
    case 4: case 8: case 10: case 22: case 27: return 4;
    case 5: case 9: case 11: case 23: case 28: case 36: return 8;
    case 18: case 19: case 24: case 29: return 16;
  }
  return 0;
};

local kindSigned = function(kind) {
  switch (kind) {
    case 2: case 3: case 4: case 5: case 18:
    case 20: case 21: case 22: case 23: case 24: case 36: return true;
  }
  return false;
};

// §4's WIDENING RUNGS: a narrower kind on its own signedness ladder, f32 into
// f64, and a 64-bit integer into its 128-bit width.
local widens = function(from, to) {
  if (from >= 2 && from <= 5) return (to > from && to <= 5) || to == 18;
  if (from >= 6 && from <= 9) return (to > from && to <= 9) || to == 19;
  if (from == 10 && to == 11) return true;
  return false;
};

local readU16 = function(r) {
  local v = (r.b[r.at] & 0xff) | ((r.b[r.at + 1] & 0xff) << 8);
  r.at += 2;
  return v;
};
local readU32 = function(r) {
  local v = (r.b[r.at] & 0xff) | ((r.b[r.at + 1] & 0xff) << 8) |
            ((r.b[r.at + 2] & 0xff) << 16) | ((r.b[r.at + 3] & 0xff) << 24);
  r.at += 4;
  return v;
};
local readU64 = function(r) {
  local v = u64At(r.b, r.at);
  r.at += 8;
  return v;
};

// A TABLE FLOAT IS FINITE (docs/SPEC-TABLES.md §3.3, the packet wire's rule): a read refuses a NaN or an infinity on its RAW BITS,
// before the bits become a value, as non_finite_float. readPrim answers the
// sentinel NONFINITE for one, and the site that asked records the field's
// <Owner>.<field> name on the load's state (refuseNonFinite) and lands
// nothing that survives: the load answers the refusal at its end.
local NONFINITE = { nonFinite = true };
// a finite double narrowed to the nearest float32 (Go's float32(v)): the
// blob's 'f' view is the C conversion, round to nearest, ties to even
local F32_SCRATCH = blob(4);
local narrowToF32 = function(v) {
  F32_SCRATCH.seek(0); F32_SCRATCH.writen(v, 'f');
  F32_SCRATCH.seek(0); return F32_SCRATCH.readn('f');
};
local finiteF32 = function(bits) { return (bits & 0x7f800000) != 0x7f800000; };
local finiteF64 = function(bits) { return (bits & 0x7ff0000000000000) != 0x7ff0000000000000; };

// a FINITE float32 widens to a double exactly, -0 and the subnormals
// included; the surgery is on the bits, so no conversion is trusted
local widenF32 = function(bits) {
  bits = bits & 0xffffffff;
  local sign = bits & 0x80000000;
  local exp = (bits >> 23) & 0xff;
  local mant = bits & 0x007fffff;
  local high = 0;
  if (sign != 0) high = 0x8000000000000000;
  if (exp == 0xff) {
    high = high | 0x7ff0000000000000;
    if (mant != 0) high = high | (mant << 29);
  } else if (exp == 0) {
    if (mant != 0) {
      local e = -126;
      while ((mant & 0x800000) == 0) { mant = mant << 1; e--; }
      mant = mant & 0x007fffff;
      high = high | ((e + 1023) << 52) | (mant << 29);
    }
  } else {
    high = high | ((exp - 127 + 1023) << 52) | (mant << 29);
  }
  return casti2f(high);
};

// a scalar at the WIRE kind's width, sign-extended where the kind is signed
local readPrim = function(r, kind, ...) {
  local report = vargv.len() > 0 ? vargv[0] : null;
  local width = kindWidth(kind);
  if (width == 0 || r.at + width > r.end) { r.bad = true; return null; }
  local v;
  switch (kind) {
    case 1: { local b = r.b[r.at] & 0xff; r.at += 1; if (b > 1 && report != null) report.malformed = true; return b != 0; }
    case 10: { local bits = readU32(r); return finiteF32(bits) ? widenF32(bits) : NONFINITE; }
    case 11: { local bits = readU64(r); return finiteF64(bits) ? casti2f(bits) : NONFINITE; }
    case 2: case 20: {
      local x = r.b[r.at] & 0xff; r.at += 1;
      v = (x & 0x80) ? x - 256 : x;
      return v;
    }
    case 3: case 21: {
      local x = readU16(r);
      v = (x & 0x8000) ? x - 65536 : x;
      return v;
    }
    case 4: case 22: {
      local x = readU32(r);
      v = (x & 0x80000000) ? x - 4294967296 : x;
      return v;
    }
    case 5: case 23: case 36: return readU64(r);
    case 6: case 25: { v = r.b[r.at] & 0xff; r.at += 1; return v; }
    case 7: case 26: return readU16(r);
    case 8: case 27: return readU32(r);
    case 9: case 28: return readU64(r);
    case 18: case 19: case 24: case 29: {
      local lo = readU64(r), hi = readU64(r);
      return [lo, hi];
    }
  }
  r.bad = true;
  return null;
};

// UNSIGNED 64-bit ORDER, which Squirrel's signed integer does not give: the
// sign bit is flipped so the two's-complement pattern sorts as unsigned.
local u64cmp = function(a, b) {
  local MIN = 0x8000000000000000;
  local au = a ^ MIN, bu = b ^ MIN;
  return au < bu ? -1 : (au > bu ? 1 : 0);
};

// A 128-bit value and a declared bound on the same [low, high] pair. A bound
// that fits 64 bits is sign-extended; the descriptor writes both forms.
local widePair = function(v) {
  if (typeof v == "array") return v;
  return [v, v < 0 ? -1 : 0];
};

local wideCompare = function(a, b, signed) {
  if (signed) {
    local MIN = 0x8000000000000000;
    local an = (a[1] & MIN) != 0, bn = (b[1] & MIN) != 0;
    if (an != bn) return an ? -1 : 1;
  }
  local c = u64cmp(a[1], b[1]);
  if (c != 0) return c;
  return u64cmp(a[0], b[0]);
};

// THE §4 CLAMPS: a declared integer range, then a bits(N) ceiling.
// docs/SPEC-TABLES.md §4: "A 128-bit integer clamps to its declared bounds in
// 128 bits"; §4.1 counts that event `clamped`, as it does for every scalar.
local clampScalar = function(fd, kind, x, report) {
  if (x == null) return null;
  // A COMPRESSED FLOAT32 CLAMPS TO ITS DECLARED RANGE (docs/SPEC-TABLES.md
  // §3.3, §4; the Go engine's scalarAt): the finite value compares with the
  // declared bounds as doubles, one past an end lands as that end narrowed
  // to float32 and counts clamped, and a -0 lands as +0, uncounted. A float32
  // widened into a float64 field (kind 10 under 11) clamps nothing.
  if (fd.shape == "float") {
    if (kind != 10 || !fd.f32 || fd.fmin == null) return x;
    if (x < fd.fmin) { x = narrowToF32(fd.fmin); report.clamped = report.clamped + 1; }
    else if (x > fd.fmax) { x = narrowToF32(fd.fmax); report.clamped = report.clamped + 1; }
    else if (x == 0.0) x = 0.0;
    return x;
  }
  // A NARROWER INTEGER WIDENED INTO A 128-BIT FIELD (docs/SPEC-TABLES.md §4's
  // rungs: a signed kind into int128, an unsigned one into uint128) lands as
  // the field's own [low, high] pair, sign- or zero-extended from the wire
  // kind, and then clamps in 128 bits like any 128-bit value
  if (fd.width == 16 && typeof x == "integer")
    x = [x, kindSigned(kind) && x < 0 ? -1 : 0];
  if (typeof x == "array") {
    if (fd.lo != null && fd.hi != null) {
      local lo = widePair(fd.lo), hi = widePair(fd.hi);
      if (wideCompare(x, lo, fd.signed) < 0) {
        x = [lo[0], lo[1]];
        report.clamped = report.clamped + 1;
      } else if (wideCompare(x, hi, fd.signed) > 0) {
        x = [hi[0], hi[1]];
        report.clamped = report.clamped + 1;
      }
    }
    return x;
  }
  local width = kindWidth(kind);
  if (fd.lo != null && typeof fd.lo == "integer") {
    if (kindSigned(kind)) {
      if (x < fd.lo) { x = fd.lo; report.clamped = report.clamped + 1; }
      else if (x > fd.hi) { x = fd.hi; report.clamped = report.clamped + 1; }
    } else {
      local MIN = 0x8000000000000000;
      local xu = x ^ MIN, lou = fd.lo ^ MIN, hiu = fd.hi ^ MIN;
      if (xu < lou) { x = fd.lo; report.clamped = report.clamped + 1; }
      else if (xu > hiu) { x = fd.hi; report.clamped = report.clamped + 1; }
    }
  }
  if (fd.bits != null && fd.bits > 0 && fd.bits < width * 8) {
    local max;
    if (fd.bits >= 63) max = 0x7fffffffffffffff;
    else max = (1 << fd.bits) - 1;
    if (x > max) { x = max; report.clamped = report.clamped + 1; }
  }
  return x;
};

// A NaN OR AN INFINITY IS REFUSED BY EVERY READ (docs/SPEC-TABLES.md §3.3),
// tolerant and strict alike: the first field the walk met one on is named
// <Owner>.<field> (the descriptor's nf) on the load's state, and the load
// answers non_finite_float at its end, outranking every other verdict. The
// sentinel the site holds is never handed back.
local refuseNonFinite = function(desc, x, st) {
  if (x != NONFINITE) return false;
  if (st.nonFinite == null) st.nonFinite = desc.nf;
  return true;
};

// the raw-bits test on an unknown entry a read is retaining (§3.3, §6.6):
// kind 10's four bytes and kind 11's eight, little-endian; true for any
// other kind, and for a payload that is not its kind's width
local retainedFinite = function(b, from, to, kind) {
  if (kind == 10 && to - from == 4)
    return finiteF32((b[from] & 0xff) | ((b[from + 1] & 0xff) << 8) | ((b[from + 2] & 0xff) << 16) | ((b[from + 3] & 0xff) << 24));
  if (kind == 11 && to - from == 8) return finiteF64(u64At(b, from));
  return true;
};

// skipping a field the reader cannot name (docs/SPEC-TABLES.md §3)
local skipField = function(r, kind) {
  local width = kindWidth(kind);
  if (width > 0) {
    if (r.at + width > r.end) return false;
    r.at += width;
    return true;
  }
  switch (kind) {
    case 12: case 13: case 14: case 16: case 31: case 32: case 33: {
      local L = leb(r);
      if (r.bad || r.at + L > r.end) return false;
      r.at += L;
      return true;
    }
    case 15: {
      local arm = leb(r);
      if (r.bad) return false;
      if (arm != 0) {
        if (r.at + 1 > r.end) return false;
        r.at += 1;
        local L = leb(r);
        if (r.bad || r.at + L > r.end) return false;
        r.at += L;
      }
      return true;
    }
    case 17: case 30: {
      local x = leb(r);
      return !r.bad;
    }
  }
  return false;
};

// UTF-8, byte exact and validating (docs/SPEC-TABLES.md §3)
local utf8Decode = function(b, at, len) {
  local s = "";
  local i = 0;
  while (i < len) {
    local b0 = b[at + i] & 0xff;
    if (b0 == 0) return { s = s, ok = false, used = i };
    if (b0 < 0x80) { s = s + format("%c", b0); i++; continue; }
    local need, cp;
    if ((b0 & 0xe0) == 0xc0) { need = 1; cp = b0 & 0x1f; }
    else if ((b0 & 0xf0) == 0xe0) { need = 2; cp = b0 & 0x0f; }
    else if ((b0 & 0xf8) == 0xf0) { need = 3; cp = b0 & 0x07; }
    else return { s = s, ok = false, used = i };
    if (i + need >= len) return { s = s, ok = false, used = i };
    for (local k = 1; k <= need; k++) {
      local bb = b[at + i + k] & 0xff;
      if ((bb & 0xc0) != 0x80) return { s = s, ok = false, used = i };
      cp = (cp << 6) | (bb & 0x3f);
    }
    local min = need == 1 ? 0x80 : (need == 2 ? 0x800 : 0x10000);
    if (cp < min || cp > 0x10ffff || (cp >= 0xd800 && cp <= 0xdfff)) {
      return { s = s, ok = false, used = i };
    }
    // a Squirrel string holds BYTES: the sequence just validated is kept as
    // its own UTF-8 bytes, never as format("%c", cp), which keeps one byte
    for (local k = 0; k <= need; k++) s = s + format("%c", b[at + i + k] & 0xff);
    i += 1 + need;
  }
  return { s = s, ok = true, used = len };
};

// WIDE TEXT, kind 33 (docs/SPEC-TABLES.md §3, §4; SPEC.md §4.12): L BYTES
// holding L / 2 UTF-16 code units, two bytes each little-endian. Storage is the
// code units, an array of integers. wideDecode answers the units, or null when
// the payload is not wide text: SURROGATES PAIRED and no zero unit among them,
// checked as the payload ARRIVES and before the reader's bound, the rule kind
// 12 takes for UTF-8 (wideValid in internal/tablewire/text.go). The caller has
// refused an odd L as framing damage first.
local wideDecode = function(b, at, len) {
  local n = len / 2;
  local units = array(n);
  for (local i = 0; i < n; i++) units[i] = (b[at + 2 * i] & 0xff) | ((b[at + 2 * i + 1] & 0xff) << 8);
  for (local i = 0; i < n; i++) {
    local u = units[i];
    if (u == 0) return null;
    if (u >= 0xD800 && u <= 0xDBFF) {
      // a high surrogate whose low half is not the next unit, or is absent
      if (i + 1 >= n) return null;
      local lo = units[i + 1];
      if (lo < 0xDC00 || lo > 0xDFFF) return null;
      i++;
    } else if (u >= 0xDC00 && u <= 0xDFFF) {
      return null; // a low surrogate first
    }
  }
  return units;
};

// A WIDE CLAMP NEVER SPLITS A PAIR (docs/SPEC-TABLES.md §3): the first `bound`
// units, less a final high surrogate whose low half did not fit
// (wideBoundary in internal/tablewire/text.go).
local wideClamp = function(units, bound) {
  local cut = bound;
  if (cut > 0 && units[cut - 1] >= 0xD800 && units[cut - 1] <= 0xDBFF) cut--;
  return units.slice(0, cut);
};

local blobOfRange = function(b, at, len) {
  local out = blob(0);
  for (local i = 0; i < len; i++) out.writen(b[at + i] & 0xff, 'b');
  return out;
};

// WHAT AN ARRAY BODY ANSWERS (docs/SPEC-TABLES.md §2.3, §2.6, §3, §4): the
// body was this array's (SELECTED, a short inert body included); its count
// was damaged (DAMAGED: a field still rode, so an optional is present, while
// an arm holding it is not selected); or it declared another element kind
// (FOREIGN: the field keeps its declared default, an optional stays absent,
// and an arm reads None)
local ARRAY_SELECTED = 0, ARRAY_DAMAGED = 1, ARRAY_FOREIGN = 2;

// mutual recursion: every load walk names one of these
local bodyRead, readField, readFieldArray, readArrayBody, readArrayElems, readElement, readKeyedElem, readUnionInto, readArmPayload, readMapField;

// THE NODE TABLE (docs/SPEC-TABLES.md §3.1): the root body's reserved field,
// scanned before any value decode. The numbering is positional and whole or
// nothing: a bad record answers a malformed report and no numbering at all.
local nodeScan = function(bytes, table, end, report) {
  local w = newReader(bytes, 1, end);
  local at = -1, len = 0, present = false, bad = false;
  while (!bad) {
    local ref = leb(w);
    if (w.bad) break;
    if (ref == 0) break;
    if (ref > table.len()) break;
    if (w.at >= w.end) break;
    local id = table[ref - 1];
    local kind = u8(w);
    if (id == NODE_TABLE_ID) {
      present = true;
      if (kind != 12) { bad = true; break; }
      local L = leb(w);
      if (w.bad || w.at + L > w.end) { bad = true; break; }
      at = w.at;
      len = L;
      w.at += L;
      continue;
    }
    if (!skipField(w, kind)) break;
  }
  if (present && bad) { report.malformed = true; return null; }
  if (!present) return [];
  local rec = newReader(bytes, at, at + len);
  local count = leb(rec);
  if (rec.bad) { report.malformed = true; return null; }
  local raw = [];
  while (rec.at < rec.end) {
    local tref = leb(rec);
    if (rec.bad || tref == 0 || tref > table.len()) { report.malformed = true; return null; }
    local tlen = leb(rec);
    if (rec.bad || rec.at + tlen > rec.end) { report.malformed = true; return null; }
    raw.append({ tid = table[tref - 1], b = bytes, at = rec.at, end = rec.at + tlen });
    rec.at += tlen;
  }
  if (count != raw.len()) { report.malformed = true; return null; }
  return raw;
};

local unionDesc = function(T, name) { return T[name + "Descriptor"]; };
local tableDesc = function(T, name) { return T.Descriptors[name]; };

// THE GUARD'S DEMAND (docs/SPEC-TABLES.md §2.3, §4, §16.2): a field inside an
// `if` group is demanded only while every [member, want] pair of its composed
// condition (the `guard` slot internal/codegen/squirreltable/json.go emits)
// holds on the same instance. The same evaluation save.nut and json.nut use;
// a required field on the untaken side is absent from the wire and is not
// missing. A field with no guard slot always holds.
local guardHolds = function(fd, v) {
  if (!("guard" in fd) || fd.guard == null) return true;
  foreach (term in fd.guard) {
    if (term[0] == null) return false;
    if ((v[term[0]] ? true : false) != term[1]) return false;
  }
  return true;
};

// ONE BODY (docs/SPEC-TABLES.md §3, §4): fields by id in any order, a field
// this table cannot name skipped by its kind and counted unknown, a kind that
// widens decoded at its own width and counted widened, any other kind a kind
// mismatch skipped whole. A repeated id is read again and the last wins,
// silently: duplicate is never raised for a field id (§4). At the body's
// terminator every `| required` field the wire did not carry, and whose guard
// holds, refuses the record: report.malformed and the field, whole at any
// depth (docs/SPEC-TABLES.md §2.3).
bodyRead = function(c, r, T, table, v, report, st, nested) {
  if (report.countRefusal) return false;
  local carried = {};
  for (;;) {
    local ref = leb(r);
    if (r.bad) { report.malformed = true; return false; }
    if (ref == 0) {
      foreach (fd in c.fields) {
        if (!fd.required || (fd.member in carried)) continue;
        if (!guardHolds(fd, v)) continue;
        report.malformed = true;
        report.missingRequired = true;
        return false;
      }
      return true;
    }
    if (ref > table.len()) { report.malformed = true; return false; }
    if (r.at >= r.end) { report.malformed = true; return false; }
    local kind = u8(r);
    local id = table[ref - 1];
    if (id == BUILD_VERSION_ID || id == MESSAGE_VOCABULARY_ID) {
      report.malformed = true;
      return false;
    }
    if (id == NODE_TABLE_ID) {
      if (nested) { report.malformed = true; return false; }
      if (c.variable) {
        if (!skipField(r, kind)) { report.malformed = true; return false; }
        continue;
      }
    }
    if (!(id in c.byId)) {
      report.unknown = report.unknown + 1;
      // CAPTURE THE UNKNOWN FIELD'S SPAN before the plain read skips it
      // (docs/SPEC-TABLES.md §6.6): the resolving walk is retain.nut's
      local start = r.at;
      if (!skipField(r, kind)) { report.malformed = true; return false; }
      if (st.retain != null) {
        // A RETAINED UNKNOWN FLOAT IS TESTED LIKE A KNOWN ONE (§3.3, §6.6):
        // kind 10's or 11's raw bits, and a NaN or an infinity refuses the
        // read, named <Table>.0x<id>
        if (!retainedFinite(r.b, start, r.at, kind)) {
          if (st.nonFinite == null) st.nonFinite = c.name + format(".0x%016x", id);
          continue;
        }
        st.retain.capture(v, r.b, start, r.at, id, kind, table, report);
      }
      continue;
    }
    local fd = c.byId[id];
    carried[fd.member] <- true;
    // a field id met again in one body is read again and the last wins; it
    // is not a `duplicate`, which is a MAP KEY met twice (docs/SPEC-TABLES.md
    // §4, §2.8), and this target carries no map
    if (kind != fd.kind) {
      if (widens(kind, fd.kind)) {
        local x = readPrim(r, kind, report);
        if (x == null || r.bad) { report.malformed = true; return false; }
        if (refuseNonFinite(fd, x, st)) continue;
        x = clampScalar(fd, kind, x, report);
        v[fd.member] = x;
        if (fd.optional) v[fd.member + "Present"] = true;
        // a MAP ENTRY's key widening is the MAP's one event (docs/SPEC-TABLES.md
        // §2.8): bodyRead skips it here, and readMapField counts it once.
        if (!(c.mapentry && fd.name == "key")) report.widened = report.widened + 1;
        continue;
      }
      report.kindMismatch = report.kindMismatch + 1;
      if (!skipField(r, kind)) { report.malformed = true; return false; }
      continue;
    }
    if (!readField(fd, r, T, table, v, report, st, nested)) return false;
    // A COLLECTION COUNT PAST THE INT32 STORAGE CAP STOPS THE READ (docs/SPEC-TABLES.md
    // §2.9, §6.1; the engine's CountRefusal): the caller discards the partial
    // value, and no event after the cap is read
    if (report.countRefusal) return false;
  }
};

readField = function(fd, r, T, table, v, report, st, nested) {
  local shape = fd.shape;
  if (shape == "bool" || shape == "int" || shape == "float" || shape == "flags") {
    local x = readPrim(r, fd.kind, report);
    if (x == null || r.bad) { report.malformed = true; return false; }
    if (refuseNonFinite(fd, x, st)) return true;
    x = clampScalar(fd, fd.kind, x, report);
    v[fd.member] = x;
    if (fd.optional) v[fd.member + "Present"] = true;
    return true;
  }
  if (shape == "str") {
    local L = leb(r);
    if (r.bad || r.at + L > r.end) { report.malformed = true; return false; }
    local dec = utf8Decode(r.b, r.at, L);
    if (!dec.ok) {
      report.malformed = true;
      v[fd.member] = fd.def;
      r.at += L;
      return true;
    }
    // UNBOUNDED TEXT has no capacity to clamp at (docs/SPEC-TABLES.md
    // §2.13.1): its `bound` is 0, not a declared length.
    local capacity = fd.unbounded ? -1 : fd.bound;
    if (capacity >= 0 && L > capacity) {
      report.clamped = report.clamped + 1;
      local cut = capacity;
      while (cut > 0 && ((r.b[cut + r.at] & 0xc0) == 0x80)) cut--;
      v[fd.member] = utf8Decode(r.b, r.at, cut).s;
    } else {
      v[fd.member] = dec.s;
    }
    r.at += L;
    if (fd.optional) v[fd.member + "Present"] = true;
    return true;
  }
  if (shape == "wstr") {
    // WIDE TEXT (docs/SPEC-TABLES.md §3, §4; kind 33): an ODD L is framing
    // damage on the body that carries it, and ill-formed content is damage
    // too: either way the field reads its declared default (empty), one
    // malformed counts, and the parent reads on past L. A payload past the
    // bound clamps at a unit boundary that never splits a pair. The field
    // rode, so an optional is present whatever its content.
    local L = leb(r);
    if (r.bad || r.at + L > r.end) { report.malformed = true; return false; }
    local units = (L & 1) != 0 ? null : wideDecode(r.b, r.at, L);
    if (units == null) {
      report.malformed = true;
      v[fd.member] = [];
    } else {
      if (units.len() > fd.bound) {
        report.clamped = report.clamped + 1;
        units = wideClamp(units, fd.bound);
      }
      v[fd.member] = units;
    }
    r.at += L;
    if (fd.optional) v[fd.member + "Present"] = true;
    return true;
  }
  if (shape == "enum") {
    local ref = leb(r);
    if (r.bad) { report.malformed = true; return false; }
    if (ref == 0) { v[fd.member] = null; }
    else {
      if (ref > table.len()) { report.malformed = true; return false; }
      local id = table[ref - 1];
      local name = null, dep = false;
      foreach (pair in fd.variants) {
        if (pair[1] == id) { name = pair[0]; dep = pair.len() > 2 && pair[2]; break; }
      }
      if (name == null) {
        report.unknown = report.unknown + 1;
        // AN UNKNOWN ENUM VARIANT REFERENCE IS AN EXCLUDED CLASS (§6.6): the field is the
        // reader's, so a retained copy would write its id a second time
        if (st.retain != null) report.retainLost = report.retainLost + 1;
        v[fd.member] = null;
      } else if (dep) {
        // a DEPRECATED VARIANT READS AS THE FIELD'S DECLARED DEFAULT, never
        // unknown (docs/SPEC-TABLES.md §2.15)
        v[fd.member] = fd.def;
      } else {
        v[fd.member] = name;
      }
    }
    if (fd.optional) v[fd.member + "Present"] = true;
    return true;
  }
  if (shape == "ptr" || shape == "sblob" || shape == "bblob") {
    local index = leb(r);
    if (r.bad) { report.malformed = true; return false; }
    local target = null;
    if (shape == "ptr") target = tableDesc(T, fd.table);
    st.pending.append({ holder = v, key = fd.member, index = index, fd = fd, target = target, field = fd.name });
    if (fd.optional) v[fd.member + "Present"] = true;
    return true;
  }
  if (shape == "tab") {
    local L = leb(r);
    if (r.bad || r.at + L > r.end) { report.malformed = true; return false; }
    local sub = newReader(r.b, r.at, r.at + L);
    local inst = tableDesc(T, fd.table).make();
    v[fd.member] = inst;
    bodyRead(tableDesc(T, fd.table), sub, T, table, inst, report, st, true);
    if (report.countRefusal) return false;
    if (sub.at != sub.end) {
      report.malformed = true;
      v[fd.member] = tableDesc(T, fd.table).make();
    }
    r.at += L;
    if (fd.optional) v[fd.member + "Present"] = true;
    return true;
  }
  if (shape == "union") {
    // A UNION FIELD IS REPLACED ONCE ITS ARM HEADER READS (the engine's
    // unionCell): a repeat whose arm reference names no entry, or whose kind
    // or L cannot be read, is framing damage that stops this body and leaves
    // the earlier occurrence standing (docs/SPEC-TABLES.md §4). Only an
    // ELEMENT of an array of unions resets at its arm id.
    local u = unionDesc(T, fd.table);
    local inst = u.make();
    if (!readUnionInto(r, T, table, u, inst, report, st)) return false;
    v[fd.member] = inst;
    if (fd.optional) v[fd.member + "Present"] = true;
    return true;
  }
  if (shape == "bytes" || shape == "arr") {
    return readFieldArray(fd, r, T, table, v, report, st);
  }
  if (shape == "map") {
    return readMapField(fd, r, T, table, v, report, st);
  }
  return false;
};

// A MAP FIELD (docs/SPEC-TABLES.md §2.8, §3): kind 14 over element kind 13,
// an array of the generated `{ key, value }` entry in ASCENDING KEY ORDER.
// The entry is an ordinary table of the closure, so its descriptor decodes
// the key and the value at their own field kinds; a key met again is
// last-wins and counted duplicate, a descending key is malformed, and an
// entry count the body cannot hold is malformed.
// the counters a map entry's decode can move, by name (readMapField)
local mapEntryCounters = function(report) {
  local out = {};
  foreach (k in ["unknown", "kindMismatch", "widened", "clamped", "duplicate"]) {
    local x = null;
    try { x = report[k]; } catch (e) { x = null; }
    if (x != null) out[k] <- x;
  }
  return out;
};

local readMapEntry = function(ed, es, T, table, ent, report, st) {
  if (!bodyRead(ed, es, T, table, ent, report, st, true)) return false;
  return es.at == es.end;
};

local mapKeyCmp = function(a, b) {
  if (a == null && b == null) return 0;
  if (a == null) return -1;
  if (b == null) return 1;
  if (a < b) return -1;
  if (a > b) return 1;
  return 0;
};

// THE KEY SCAN (docs/SPEC-TABLES.md §2.8): the reader does not assume where
// the key sits, so before an entry's body decodes it scans that body's field
// headers for the key's id. The key's KIND is the reader's declaration: a
// kind the declaration widens decodes, and any other disagreement is the
// map's ONE kind_mismatch, which empties it. docs/SPEC-TABLES.md §3, "A BODY'S
// TERMINATOR IS THE END OF ITS PAYLOAD", puts entry framing damage here, at
// the key scan, before a slot is chosen.
local mapKeyScan = function(es, table, kd) {
  local scan = newReader(es.b, es.at, es.end);
  local bad = false;
  local widened = false;
  for (;;) {
    local ref = leb(scan);
    // A FRAMING FAILURE IS Malformed, NOT kind_mismatch, unless a key under
    // the wrong kind was already seen (the engine's mapKey returns keyOk
    // false and mapField checks the kind first).
    if (scan.bad) return [bad, widened, false];
    // THE TERMINATOR MUST CONSUME THE ENTRY'S L (the engine's mapKey): a
    // body whose zero lands before its end is framing damage, not a key.
    if (ref == 0) return [bad, widened, scan.at == scan.end];
    if (ref > table.len()) return [bad, widened, false];
    if (scan.at >= scan.end) return [bad, widened, false];
    local id = table[ref - 1];
    local kind = u8(scan);
    if (id == MAP_KEY_ID) {
      if (kind != kd.kind) {
        if (widens(kind, kd.kind)) widened = true;
        else bad = true;
      } else {
        bad = false;
        if (kd.shape == "str") {
          // A KEY A STRING VALUE WOULD REFUSE AS MALFORMED MAKES THE MAP
          // MALFORMED (§2.8; scalarOrString in tablewire): ill-formed UTF-8 or
          // a zero byte is not a key at any length, so the scan fails here
          local at = scan.at;
          local L = leb(scan);
          if (scan.bad || scan.at + L > scan.end || !utf8Decode(scan.b, scan.at, L).ok) return [bad, widened, false];
          scan.at = at;
        }
      }
    }
    if (!skipField(scan, kind)) return [bad, widened, false];
  }
};

readMapField = function(fd, r, T, table, v, report, st) {
  // PRESENCE OF THE FIELD IS PRESENCE (docs/SPEC-TABLES.md §2.8, §16.2): an
  // optional map's body, even an empty one, is the present value; only an
  // absent field elides.
  if (fd.optional) v[fd.member + "Present"] = true;
  local L = leb(r);
  if (r.bad || r.at + L > r.end) { report.malformed = true; return false; }
  local end = r.at + L;
  local empty = [];
  if (L < 2) {
    // a body shorter than its own header is inert (docs/SPEC-TABLES.md §3):
    // the field keeps the value it has, and a repeat replaces nothing
    r.at = end;
    return true;
  }
  // THE MAP'S HEADER IS READ AGAINST THE PARENT, as every array's is (the
  // engine's mapField and subTo; docs/SPEC-TABLES.md §2.8, §3): a count that
  // runs past the map's L still reads, the element kind is judged, and the
  // entries walk an empty body
  local elemKind = u8(r);
  local count = leb(r);
  if (r.bad) {
    r.bad = false;
    report.malformed = true;
    r.at = end;
    return true;
  }
  local sub = newReader(r.b, r.at, end < r.at ? r.at : end);
  if (elemKind != 13) {
    // a foreign ELEMENT kind is the ordinary array kind mismatch (§2.8, §4);
    // a skipped repeat keeps the earlier occurrence
    report.kindMismatch = report.kindMismatch + 1;
    r.at = end;
    return true;
  }
  if (count > 0x7fffffff) {
    // a count past the int32 storage cap refuses the whole value (§2.8)
    report.countRefusal = true;
    v[fd.member] = empty;
    r.at = end;
    return false;
  }
  local ed = tableDesc(T, fd.entry);
  local arr = [];
  local last = null;
  local haveLast = false;
  local widened = false;
  for (local i = 0; i < count; i++) {
    local el = leb(sub);
    if (sub.bad || sub.at + el > sub.end) { report.malformed = true; break; }
    local es = newReader(sub.b, sub.at, sub.at + el);
    local scan = mapKeyScan(es, table, fd.mapkey);
    if (scan[1] && !widened) {
      // ONE `widened` counts for the map, at the first key it widens (§2.8)
      widened = true;
      report.widened = report.widened + 1;
    }
    if (scan[0]) {
      // A MAP WITH HALF ITS KEYS IS NOT A MAP (§2.8): the map resets to
      // EMPTY, ONE kind_mismatch counts for it, and the rest skips by L.
      report.kindMismatch = report.kindMismatch + 1;
      arr = [];
      break;
    }
    if (!scan[2]) {
      // docs/SPEC-TABLES.md §2.8, §3: the entry's terminator did not land on
      // its end, so this is framing damage; the entry is not decoded and no
      // counter inside it is read
      report.malformed = true;
      break;
    }
    local ent = ed.make();
    // The engine places the key BEFORE it decodes the entry (tablewire
    // mapField): a descending entry is never decoded, so nothing inside it
    // counts. This reader learns the key by decoding, so it notes the
    // counters first and puts them back when the key turns out to descend.
    // They are read and written by name, never cloned: the strict read's
    // report is a delegate witness whose slots live behind _get and _set.
    local before = mapEntryCounters(report);
    if (!readMapEntry(ed, es, T, table, ent, report, st)) {
      if (report.countRefusal) return false;
      report.malformed = true;
      break;
    }
    local key = ent[fd.mapkey.member];
    local cmp = haveLast ? mapKeyCmp(last, key) : -1;
    if (cmp > 0) {
      // DESCENDING: not a body any conforming writer produced (§2.8)
      foreach (k, x in before) report[k] = x;
      report.malformed = true;
      break;
    }
    if (cmp == 0) {
      // EQUAL: a DUPLICATE; last wins WHOLE and the count excludes it (§2.8)
      report.duplicate = report.duplicate + 1;
      arr[arr.len() - 1] = ent;
    } else {
      arr.append(ent);
      haveLast = true;
      last = key;
    }
    sub.at += el;
  }
  v[fd.member] = arr;
  r.at = end;
  return true;
};

// AN ARRAY FIELD'S HEADER IS READ AGAINST THE PARENT, and only its elements
// are bounded by its L (docs/SPEC-TABLES.md §2.8, §3; the engine's arrayBody
// and subTo). A canonical count can be up to ten bytes, so a count spelled
// wide enough runs past L and still reads: a positional count clamps at the
// bound, a keyed body's element kind is judged, and the elements walk an
// empty body. The same holds for every array shape, positional, unbounded
// and keyed.
readFieldArray = function(fd, r, T, table, v, report, st) {
  local L = leb(r);
  if (r.bad || r.at + L > r.end) { report.malformed = true; return false; }
  local end = r.at + L;
  local sel = ARRAY_SELECTED;
  if (L >= 2) {
    local ek = u8(r);
    local count = leb(r);
    if (r.bad) {
      // a damaged count is framing damage inside the field's own L: counted,
      // and the walk goes on past it
      r.bad = false;
      report.malformed = true;
      sel = ARRAY_DAMAGED;
    } else {
      local sub = newReader(r.b, r.at, end < r.at ? r.at : end);
      sel = readArrayElems(fd, sub, ek, count, T, table, v, report, st);
    }
  }
  r.at = end;
  if (fd.optional && sel != ARRAY_FOREIGN) v[fd.member + "Present"] = true;
  return true;
};

// AN ARRAY BODY (docs/SPEC-TABLES.md §2.9, §3): a body shorter than its own
// header is inert; a damaged count is malformed and keeps the field; an
// element kind that is not this array's (and does not widen) is a kind
// mismatch and keeps the field; a bounded array's excess is clamped. An ARM's
// array is framed by the arm's L, so its header is read inside it.
readArrayBody = function(fd, r, T, table, v, report, st) {
  if (r.end - r.at < 2) return ARRAY_SELECTED;
  local ek = u8(r);
  local count = leb(r);
  if (r.bad) { report.malformed = true; return ARRAY_DAMAGED; }
  return readArrayElems(fd, r, ek, count, T, table, v, report, st);
};

// THE ELEMENTS of an array whose header (element kind, count) is read, out of
// a reader bounded by the array's own body.
readArrayElems = function(fd, r, ek, count, T, table, v, report, st) {
  local want = fd.shape == "bytes" ? 6 : fd.elem.kind;
  if (ek != want) {
    if (!widens(ek, want)) {
      report.kindMismatch = report.kindMismatch + 1;
      return ARRAY_FOREIGN;
    }
    report.widened = report.widened + 1;
  }
  local keep = count;
  if (fd.list) {
    if (count > 0x7fffffff) { v[fd.member] = []; report.countRefusal = true; return ARRAY_SELECTED; }
  } else if (count > fd.bound && fd.kind != 16) {
    // a keyed body's count is how many triples ride, each landing at its own
    // key's slot, so it is never clamped (docs/SPEC-TABLES.md §2.4)
    keep = fd.bound;
    report.clamped = report.clamped + 1;
  }
  if (fd.shape == "bytes") {
    local out = blob(0);
    for (local i = 0; i < keep; i++) {
      if (r.at >= r.end) { report.malformed = true; break; }
      out.writen(u8(r), 'b');
    }
    v[fd.member] = out;
    return ARRAY_SELECTED;
  }
  if (fd.elem.shape == "tab" && fd.elem.kind == 12) {
    // A PLAIN TEXT LIST's element is kind 12 (docs/SPEC-TABLES.md §2.13, §3):
    // its own L and bytes, landed in the element record's one `value` string.
    local d = tableDesc(T, fd.elem.table);
    local out = [];
    for (local i = 0; i < keep; i++) {
      local L = leb(r);
      if (r.bad || r.at + L > r.end) { report.malformed = true; break; }
      local dec = utf8Decode(r.b, r.at, L);
      if (!dec.ok) { report.malformed = true; r.at += L; break; }
      local inst = d.make();
      inst.Value = dec.s;
      out.append(inst);
      r.at += L;
    }
    v[fd.member] = out;
    return ARRAY_SELECTED;
  }
  local out = [];
  if (fd.kind == 16) {
    // A keyed slot lands at its VARIANT's ordinal (docs/SPEC-TABLES.md §2.4):
    // slot k is variant k's value, and a default slot is simply absent from
    // the wire and keeps the constructor's born value.
    out = v[fd.member];
    if (out == null || out.len() == 0) out = array(fd.bound, fd.elem.def);
    for (local i = 0; i < count; i++) {
      local keyRef = leb(r);
      if (r.bad || keyRef == 0 || keyRef > table.len()) { report.malformed = true; break; }
      local id = table[keyRef - 1];
      local slot = -1;
      if (fd.keyvariants != null) {
        for (local k = 0; k < fd.keyvariants.len(); k++) {
          if (fd.keyvariants[k][1] == id) { slot = k; break; }
        }
      }
      local elen = leb(r);
      if (r.bad || r.at + elen > r.end) { report.malformed = true; break; }
      if (slot < 0) {
        // a slot this reader cannot name is counted once and skipped by its
        // length, never decoded (docs/SPEC-TABLES.md §2.4, §4)
        report.unknown = report.unknown + 1;
        // AN UNKNOWN KEYED-ARRAY SLOT IS AN EXCLUDED CLASS (§6.6): a slot is not a
        // field and the reader rewrites the array body whole
        if (st.retain != null) report.retainLost = report.retainLost + 1;
        r.at += elen;
        continue;
      }
      local sr = newReader(r.b, r.at, r.at + elen);
      local res = readKeyedElem(fd, fd.elem, sr, ek, T, table, out, slot, report, st);
      if (report.countRefusal) break;
      if (res[1]) out[slot] = res[0];
      r.at += elen;
    }
    v[fd.member] = out;
    return ARRAY_SELECTED;
  }
  for (local i = 0; i < keep; i++) {
    local reached = r.at < r.end;
    if (!readElement(fd, fd.elem, r, T, table, out, report, st, ek)) {
      // AN ELEMENT OF AN ARRAY OF UNIONS IS RESET THE MOMENT ITS ARM ID IS READ
      // (docs/SPEC-TABLES.md §4), before the kind and the arm length are
      // checked, so a repeat whose own framing is damaged leaves no arm an
      // earlier occurrence decoded standing: the slot reads None. A counted or
      // listed array's live count stops before it, so only a fixed [N] array
      // keeps the reset slot; an element the body cannot reach is untouched.
      if (reached && fd.elem.shape == "union" && !fd.list && !fd.counted)
        out.append(unionDesc(T, fd.elem.table).make());
      break;
    }
  }
  if (!fd.list && !fd.counted) {
    // A FIXED [N] ARRAY KEEPS ITS N SLOTS (docs/SPEC-TABLES.md §2.9, §4): the
    // decoded prefix lands first, and a slot the body did not reach keeps the
    // value it held. The prefix array is the one kept, because a pointer
    // element's pending resolution holds it.
    local cur = v[fd.member];
    if (cur != null && cur.len() == fd.bound) {
      for (local i = out.len(); i < cur.len(); i++) out.append(cur[i]);
    }
  }
  v[fd.member] = out;
  return ARRAY_SELECTED;
};

// ONE KEYED SLOT'S VALUE (docs/SPEC-TABLES.md §2.4, §4), read inside the
// slot's own length. It answers [value, lands]: a value too damaged to read
// leaves the slot as it was, and the walk always goes on to the next triple,
// past this one's length; a table element that does not fill its length
// exactly is damage and the slot reads the table's defaults.
readKeyedElem = function(fd, e, sr, ek, T, table, out, slot, report, st) {
  if (e.shape == "ptr" || e.shape == "sblob" || e.shape == "bblob") {
    local index = leb(sr);
    if (sr.bad || sr.at != sr.end) { report.malformed = true; return [null, false]; }
    local target = e.shape == "ptr" ? tableDesc(T, e.table) : null;
    st.pending.append({ holder = out, key = slot, index = index, fd = e,
      target = target, field = fd.name });
    return [null, true];
  }
  if (e.shape == "enum") {
    local ref = leb(sr);
    if (sr.bad) { report.malformed = true; return [null, false]; }
    if (ref == 0) return [null, true];
    if (ref > table.len()) { report.malformed = true; return [null, false]; }
    local id = table[ref - 1];
    local name = null, dep = false;
    foreach (pair in e.variants) {
      if (pair[1] == id) { name = pair[0]; dep = pair.len() > 2 && pair[2]; break; }
    }
    if (name == null) {
      report.unknown = report.unknown + 1;
      // AN UNKNOWN ENUM VARIANT REFERENCE IS AN EXCLUDED CLASS (§6.6): the field is the
      // reader's, so a retained copy would write its id a second time
      if (st.retain != null) report.retainLost = report.retainLost + 1;
    }
    // a DEPRECATED VARIANT READS AS THE ELEMENT'S DEFAULT
    // (docs/SPEC-TABLES.md §2.15)
    return [dep ? e.def : name, true];
  }
  if (e.shape == "tab") {
    local d = tableDesc(T, e.table);
    local inst = d.make();
    bodyRead(d, sr, T, table, inst, report, st, true);
    if (report.countRefusal) return [null, false];
    if (sr.at != sr.end) { report.malformed = true; inst = d.make(); }
    return [inst, true];
  }
  local x = readPrim(sr, ek, report);
  if (x == null || sr.bad) { report.malformed = true; return [null, false]; }
  if (refuseNonFinite(e, x, st)) return [0.0, true];
  x = clampScalar(e, ek, x, report);
  return [x, true];
};

readElement = function(fd, e, r, T, table, out, report, st, ek) {
  if (e.shape == "ptr" || e.shape == "sblob" || e.shape == "bblob") {
    local index = leb(r);
    if (r.bad) { report.malformed = true; return false; }
    out.append(null);
    local target = e.shape == "ptr" ? tableDesc(T, e.table) : null;
    st.pending.append({ holder = out, key = out.len() - 1, index = index, fd = e,
      target = target, field = fd.name });
    return true;
  }
  if (e.shape == "enum") {
    local ref = leb(r);
    if (r.bad) { report.malformed = true; return false; }
    if (ref == 0) { out.append(null); return true; }
    if (ref > table.len()) { report.malformed = true; return false; }
    local id = table[ref - 1];
    local name = null, dep = false;
    foreach (pair in e.variants) {
      if (pair[1] == id) { name = pair[0]; dep = pair.len() > 2 && pair[2]; break; }
    }
    if (name == null) {
      report.unknown = report.unknown + 1;
      // AN UNKNOWN ENUM VARIANT REFERENCE IS AN EXCLUDED CLASS (§6.6): the field is the
      // reader's, so a retained copy would write its id a second time
      if (st.retain != null) report.retainLost = report.retainLost + 1;
    }
    // an ELEMENT reads the element default when the variant is deprecated
    // (docs/SPEC-TABLES.md §2.15)
    out.append(dep ? e.def : name);
    return true;
  }
  if (e.shape == "tab") {
    local L = leb(r);
    if (r.bad || r.at + L > r.end) { report.malformed = true; return false; }
    local sub = newReader(r.b, r.at, r.at + L);
    local d = tableDesc(T, e.table);
    local inst = d.make();
    bodyRead(d, sub, T, table, inst, report, st, true);
    if (report.countRefusal) return false;
    if (sub.at != sub.end) { report.malformed = true; inst = d.make(); }
    r.at += L;
    out.append(inst);
    return true;
  }
  if (e.shape == "union") {
    local u = unionDesc(T, e.table);
    local inst = u.make();
    if (!readUnionInto(r, T, table, u, inst, report, st)) return false;
    out.append(inst);
    return true;
  }
  local x = readPrim(r, ek, report);
  if (x == null || r.bad) { report.malformed = true; return false; }
  // an element's declared range and bit width clamp as a field's do
  // (docs/SPEC-TABLES.md §4)
  if (refuseNonFinite(e, x, st)) { out.append(0.0); return true; }
  x = clampScalar(e, ek, x, report);
  out.append(x);
  return true;
};

// A UNION (§2.6, §3): the arm reference, the arm's kind, then its L-framed
// payload. An arm this build cannot name counts unknown and reads None; a
// kind that is not the arm's is a kind mismatch and reads None too.
readUnionInto = function(r, T, table, u, inst, report, st) {
  local armRef = leb(r);
  if (r.bad) { report.malformed = true; return false; }
  if (armRef == 0) { inst.Type = 0; return true; }
  if (armRef > table.len()) { report.malformed = true; return false; }
  local armID = table[armRef - 1];
  local armKind = u8(r);
  local L = leb(r);
  if (r.bad || r.at + L > r.end) { report.malformed = true; return false; }
  local bodyEnd = r.at + L;
  local sub = newReader(r.b, r.at, bodyEnd);
  local arm = null;
  foreach (a in u.arms) { if (a.id == armID) { arm = a; break; } }
  if (arm == null) {
    report.unknown = report.unknown + 1;
    // AN UNKNOWN UNION ARM ID IS AN EXCLUDED CLASS (§6.6), one level in from an
    // unknown enum variant: the union field is the reader's
    if (st.retain != null) report.retainLost = report.retainLost + 1;
    inst.Type = 0;
    r.at = bodyEnd;
    return true;
  }
  local want = arm.payload == null ? 32 : arm.payload.kind;
  local widened = false;
  if (armKind != want) {
    if (!widens(armKind, want)) {
      report.kindMismatch = report.kindMismatch + 1;
      inst.Type = 0;
      r.at = bodyEnd;
      return true;
    }
    widened = true;
  }
  inst.Type = arm.tag;
  local ok = readArmPayload(arm, sub, armKind, T, table, inst, report, st);
  // WIDENED AT AN ARM (docs/SPEC-TABLES.md §4) counts only once the payload
  // has decoded at its own width: an L that is not that width is the arm's
  // own framing damage, malformed alone, and the union reads None
  if (widened && ok && inst.Type == arm.tag) report.widened = report.widened + 1;
  r.at = bodyEnd;
  return ok;
};

readArmPayload = function(arm, sub, wireKind, T, table, inst, report, st) {
  local p = arm.payload;
  if (p == null) {
    if (sub.at != sub.end) { report.malformed = true; inst.Type = 0; }
    return true;
  }
  local shape = p.shape;
  if (shape == "int" || shape == "bool" || shape == "float" || shape == "flags") {
    local width = kindWidth(wireKind);
    if (width == 0 || sub.end - sub.at != width) {
      report.malformed = true;
      inst.Type = 0;
      return true;
    }
    local x = readPrim(sub, wireKind, report);
    if (x == null || sub.bad) { report.malformed = true; inst.Type = 0; return true; }
    if (refuseNonFinite(p, x, st)) return true;
    x = clampScalar(p, wireKind, x, report);
    inst[p.member] = x;
    if (p.optional) inst[p.member + "Present"] = true;
    return true;
  }
  if (shape == "str") {
    local L = sub.end - sub.at;
    local dec = utf8Decode(sub.b, sub.at, L);
    if (!dec.ok) { report.malformed = true; inst.Type = 0; return true; }
    if (L > p.bound) {
      report.clamped = report.clamped + 1;
      local cut = p.bound;
      while (cut > 0 && ((sub.b[sub.at + cut] & 0xc0) == 0x80)) cut--;
      inst[p.member] = utf8Decode(sub.b, sub.at, cut).s;
    } else {
      inst[p.member] = dec.s;
    }
    sub.at = sub.end;
    return true;
  }
  if (shape == "wstr") {
    // WIDE TEXT at an arm (docs/SPEC-TABLES.md §3): the arm's L is the BYTE
    // length; an odd L or ill-formed content is that arm's own damage, the
    // union reads None and the parent reads on past L.
    local L = sub.end - sub.at;
    local units = (L & 1) != 0 ? null : wideDecode(sub.b, sub.at, L);
    if (units == null) { report.malformed = true; inst.Type = 0; return true; }
    if (units.len() > p.bound) {
      report.clamped = report.clamped + 1;
      units = wideClamp(units, p.bound);
    }
    inst[p.member] = units;
    if (p.optional) inst[p.member + "Present"] = true;
    sub.at = sub.end;
    return true;
  }
  if (shape == "ptr" || shape == "sblob" || shape == "bblob") {
    local index = leb(sub);
    if (sub.bad || sub.at != sub.end) { report.malformed = true; inst.Type = 0; return true; }
    local target = shape == "ptr" ? tableDesc(T, p.table) : null;
    st.pending.append({ holder = inst, key = p.member, index = index, fd = p,
      target = target, field = arm.name });
    return true;
  }
  if (shape == "enum") {
    local ref = leb(sub);
    if (sub.bad || sub.at != sub.end) { report.malformed = true; inst.Type = 0; return true; }
    if (ref == 0) { inst[p.member] = null; return true; }
    if (ref > table.len()) { report.malformed = true; inst.Type = 0; return true; }
    local id = table[ref - 1];
    local name = null, dep = false;
    foreach (pair in p.variants) {
      if (pair[1] == id) { name = pair[0]; dep = pair.len() > 2 && pair[2]; break; }
    }
    if (name == null) {
      report.unknown = report.unknown + 1;
      // AN UNKNOWN ENUM VARIANT REFERENCE IS AN EXCLUDED CLASS (§6.6): the field is the
      // reader's, so a retained copy would write its id a second time
      if (st.retain != null) report.retainLost = report.retainLost + 1;
    }
    // a DEPRECATED VARIANT READS AS THE FIELD'S DECLARED DEFAULT
    // (docs/SPEC-TABLES.md §2.15)
    inst[p.member] = dep ? p.def : name;
    return true;
  }
  if (shape == "tab") {
    local d = tableDesc(T, p.table);
    local child = d.make();
    inst[p.member] = child;
    bodyRead(d, sub, T, table, child, report, st, true);
    if (report.countRefusal) return false;
    if (sub.at != sub.end) {
      report.malformed = true;
      inst.Type = 0;
      inst[p.member] = d.make();
    }
    return true;
  }
  if (shape == "union") {
    local u = unionDesc(T, p.table);
    local child = u.make();
    inst[p.member] = child;
    // damage inside this arm's own L is the arm's framing damage: the outer
    // union reads None and the parent reads on past L (docs/SPEC-TABLES.md §3)
    if (!readUnionInto(sub, T, table, u, child, report, st)) inst.Type = 0;
    return true;
  }
  if (shape == "bytes" || shape == "arr") {
    if (readArrayBody(p, sub, T, table, inst, report, st) != ARRAY_SELECTED) inst.Type = 0;
    return true;
  }
  return false;
};

// RESOLUTION (docs/SPEC-TABLES.md §3.1): 0 is null, 1 is the root, k + 1 is
// record k. A record of another kind than the slot declares is a kind
// mismatch, an index past the numbering is malformed, and a numbering that
// could not be read resolves nothing.
local resolve = function(st, root, report) {
  foreach (p in st.pending) {
    local value = null;
    if (st.good && p.index != 0) {
      if (p.index == 1) {
        if (p.fd.shape == "ptr" && p.target == root) value = st.root;
        else report.kindMismatch = report.kindMismatch + 1;
      } else if (p.index - 2 >= st.nodes.len()) {
        report.malformed = true;
      } else {
        local n = st.nodes[p.index - 2];
        if (n != null) {
          if (p.fd.shape == "sblob") {
            if (n.kind == "s") {
              if (n.named) {
                st.blobShared = st.blobShared + 1;
                if (st.blobField == null) st.blobField = p.field;
              } else {
                n.named = true;
              }
              value = n.s;
            } else report.kindMismatch = report.kindMismatch + 1;
          } else if (p.fd.shape == "bblob") {
            if (n.kind == "b") {
              if (n.named) {
                st.blobShared = st.blobShared + 1;
                if (st.blobField == null) st.blobField = p.field;
              } else {
                n.named = true;
              }
              // A BLOB IS OWNED BY ONE SLOT (docs/SPEC-TABLES.md §2.5): the
              // tolerant read keeps the ONE node and hands every slot naming it
              // the same blob, so a later save sees the sharing and refuses.
              if (n.val == null) n.val = blobOfRange(n.b, n.at, n.end - n.at);
              value = n.val;
            } else report.kindMismatch = report.kindMismatch + 1;
          } else if (n.kind == "t" && n.d == p.target) {
            value = n.v;
          } else {
            report.kindMismatch = report.kindMismatch + 1;
          }
        }
      }
    }
    p.holder[p.key] = value;
  }
};

// THE TOLERANT LOAD (docs/SPEC-TABLES.md §3, §3.1, §4): the form byte, the id
// table, the node table's records first, then every record's body, then the
// root body, then the one numbering's resolution. It keeps the recovered value
// and its counters; the plain name is the strict read (§6.5).
_M.loadTolerant <- function(T, rootName, bytes, report, retain = null, trailerChecked = false) {
  if (report.unknown == null) report.unknown <- 0;
  if (report.kindMismatch == null) report.kindMismatch <- 0;
  if (report.widened == null) report.widened <- 0;
  if (report.clamped == null) report.clamped <- 0;
  if (report.duplicate == null) report.duplicate <- 0;
  // THIS READ'S OWN non_finite_float (docs/SPEC-TABLES.md §3.3): a name an
  // earlier read left on a reused report does not refuse this one
  if (report.getdelegate() == null) report.nonFiniteFloat <- null;
  if (report.malformed == null) report.malformed <- false;
  if (report.refused == null) report.refused <- false;
  if (!report.rawin("countRefusal")) {
    if (report.getdelegate() == null) report.countRefusal <- false;
    else report.countRefusal = false;
  }

  if (bytes.len() < 1) { report.malformed = true; return null; }
  if ((bytes[0] & 0xff) != FORM_VARIABLE) { report.refused = true; return null; }
  local c = tableDesc(T, rootName);
  // an integrity root's trailer is checked and stripped before any field is
  // decoded (docs/SPEC-TABLES.md §2.14): a mismatch is the whole-file damage,
  // and the rest reads as §3's file. ReadStrict has already opened it and
  // passes trailerChecked, so the tolerant load and the strict read share one
  // order without stripping twice.
  if (!trailerChecked && ("integrity" in c) && c.integrity != null && c.integrity != "") {
    local body = integrityOpen(bytes);
    if (body == null) { report.malformed = true; return null; }
    bytes = body;
  }
  if (bytes.len() < 9) { report.malformed = true; return null; }
  local count = u64At(bytes, bytes.len() - 8);
  // the count is bounded by division, never by count * 8, which wraps for a
  // count past 2^60 (docs/SPEC-TABLES.md §3: the id table is inside the file),
  // and THE ENTRY COUNT HAS A MAXIMUM (§3, THE DISTINCTNESS CHECK;
  // ir.TableWireIdTableBound): a table above it is malformed on the
  // whole-wire rule, whatever the buffer would hold
  if (count < 0 || count > (bytes.len() - 9) / 8 || count > ID_TABLE_BOUND) { report.malformed = true; return null; }
  local end = bytes.len() - 8 - count * 8;
  local table = [];
  local seen = {};
  for (local i = 0; i < count; i++) {
    local id = u64At(bytes, end + i * 8);
    if (id in seen) { report.malformed = true; return null; }
    seen[id] <- true;
    table.append(id);
  }

  // the root body's own framing: an early terminator is damage
  local w = newReader(bytes, 1, end);
  for (;;) {
    local ref = leb(w);
    if (w.bad) break;
    if (ref == 0) {
      if (w.at != w.end) { report.malformed = true; return null; }
      break;
    }
    if (ref > table.len()) break;
    if (w.at >= w.end) break;
    local kind = u8(w);
    if (!skipField(w, kind)) break;
  }

  local v = c.make();
  local byType = {};
  foreach (name, d in T.Descriptors) { byType[d.typeId] <- d; }
  // only a VARIABLE-class root owns the node table (docs/SPEC-TABLES.md
  // §3.1): a value-only root has no pointer and no numbering, and the
  // reserved id in its body is a field it cannot name, counted unknown
  local raw = c.variable ? nodeScan(bytes, table, end, report) : [];
  local st = { good = raw != null, nodes = [], root = v, pending = [], retain = retain,
    blobShared = 0, blobField = null, nonFinite = null };
  if (raw != null) {
    foreach (rec in raw) {
      if (rec.tid == STRING_BLOB_ID) {
        local dec = utf8Decode(rec.b, rec.at, rec.end - rec.at);
        if (!dec.ok) { report.malformed = true; st.nodes.append(null); continue; }
        st.nodes.append({ kind = "s", s = dec.s, named = false });
        continue;
      }
      if (rec.tid == BYTES_BLOB_ID) {
        st.nodes.append({ kind = "b", b = rec.b, at = rec.at, end = rec.end, val = null, named = false });
        continue;
      }
      if (!(rec.tid in byType)) {
        report.unknown = report.unknown + 1;
        // an unknown NODE RECORD is an excluded class (§6.6) and is never
        // retained; a retaining read still reports it as retain_lost
        if (retain != null) report.retainLost = report.retainLost + 1;
        st.nodes.append(null);
        continue;
      }
      local d = byType[rec.tid];
      st.nodes.append({ kind = "t", d = d, v = d.make(), b = rec.b, at = rec.at, end = rec.end });
    }
    foreach (n in st.nodes) {
      if (n == null || n.kind != "t") continue;
      bodyRead(n.d, newReader(n.b, n.at, n.end), T, table, n.v, report, st, true);
      if (report.countRefusal) break;
    }
  }
  local finished = bodyRead(c, newReader(bytes, 1, end), T, table, v, report, st, false);
  resolve(st, c, report);
  // WHETHER THE ROOT BODY READ TO ITS OWN TERMINATOR (docs/SPEC-TABLES.md §4,
  // §4.2): a builder whose root body stopped holds a partial value, which a
  // builder caller discards. It is told only to a caller whose report
  // declares the slot `stopped`; the value is returned either way.
  if ("stopped" in report) report.stopped = !finished;
  // A NON-FINITE FLOAT REFUSES THE READ, the tolerant one too, and outranks
  // every other verdict the walk reached (docs/SPEC-TABLES.md §3.3): nothing
  // lands. The report names it, reason non_finite_float: <Owner>.<field>; a
  // strict caller (ReadStrict, refusal.nut) reads the name off its own
  // report and answers non_finite_float with that field.
  if (st.nonFinite != null) {
    if (report.getdelegate() == null) {
      report.refused = true;
      report.nonFiniteFloat = st.nonFinite;
      report.reason <- "non_finite_float: " + st.nonFinite;
      return null;
    }
    local strict = ::getstackinfos(2);
    if (strict != null && ("_strictFrame" in strict.locals)) strict.locals.report.nonFiniteFloat <- st.nonFinite;
    return null;
  }
  if (report.getdelegate() == null) {
    report.blobShared <- st.blobShared;
    if (st.blobShared > 0 && !report.malformed && !report.countRefusal) {
      report.reason <- "blob_shared";
      report.field <- st.blobField;
    }
    return v;
  }
  local caller = ::getstackinfos(2);
  if (caller != null && ("_strictFrame" in caller.locals)) {
    local cr = caller.locals.report;
    local s = caller.locals.store;
    cr.blobShared <- st.blobShared;
    if (st.blobShared > 0 && !s.countRefusal && !s.malformed) {
      cr.reason = "blob_shared";
      cr.field = st.blobField;
      local saved = { clamped = s.clamped, kindMismatch = s.kindMismatch };
      delete s.clamped;
      delete s.kindMismatch;
      local seen = {};
      s.setdelegate({
        _get = function(k) {
          if (k in saved) {
            if (k in seen) return 0;
            seen[k] <- true;
            return saved[k];
          }
          throw null;
        }
      });
      return null;
    }
  }
  return v;
};

// THE STRICT LOAD (docs/SPEC-TABLES.md §6.5): the plain name is the strict
// read. It runs the tolerant walk and refuses whatever ReadStrict refuses,
// leaving the reason and the first field that moved its counter in the report
// and answering null; the recovered value is never handed back. The retaining
// read (LoadRetain, §6.6) and the wire-fuzz diagnostic name the Tolerant twin.
// REPORT fills the counters and flags a caller's report table does not hold
// yet, so a <T>Load entry hands the walk one whole report; a fresh table when
// the caller passed none.
local REPORT_COUNTERS = ["unknown", "kindMismatch", "widened", "clamped", "duplicate"];
_M.report <- function(report) {
  if (report == null) report = {};
  foreach (k in REPORT_COUNTERS) if (!(k in report)) report[k] <- 0;
  if (!("malformed" in report)) report.malformed <- false;
  if (!("refused" in report)) report.refused <- false;
  return report;
};

_M.load <- function(T, rootName, bytes, report) {
  return R.ReadStrict(T, rootName, bytes, report);
};
