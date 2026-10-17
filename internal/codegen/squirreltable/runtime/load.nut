// THE VARIABLE TABLE WIRE'S LOAD (docs/SPEC-TABLES.md §3, §3.1, §4), driven
// by the descriptors internal/codegen/squirreltable emits (§8.1). The walk is
// a port of internal/codegen/jstable/variableruntime.go's THE LOAD,
// RESOLUTION, ONE BODY and AN ARRAY BODY, which are ports of
// internal/tablewire's decode.go, decodenodes.go and nodetable.go.
//
// _M.load(T, root, bytes, report) answers the loaded root instance or null.
// T is a unit's table module (the exports table schema_module.nut requires);
// root is the root table's schema name. report is the caller's §4 report: the
// six counters, the malformed flag and the refusal verdict. It is filled in
// place, never created here.
local _M = vargv[0];

// THE RESERVED IDS the language holds back (docs/SPEC-TABLES.md §3.1, §5).
local NODE_TABLE_ID = 0xffffffffffffffff;
local BUILD_VERSION_ID = 0xfffffffffffffffe;
local MESSAGE_VOCABULARY_ID = 0xfffffffffffffffd;
local STRING_BLOB_ID = 0x704be0d8faaffc58;
local BYTES_BLOB_ID = 0x2f2ec0474f1c4fe4;

local FORM_VARIABLE = 1;

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

// a float32 widens to a double by BIT SURGERY, never through the blob's 'f'
// conversion (docs/PORT-SQUIRREL.md, 64-BIT AND 128-BIT INTEGERS)
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
local readPrim = function(r, kind) {
  local width = kindWidth(kind);
  if (width == 0 || r.at + width > r.end) { r.bad = true; return null; }
  local v;
  switch (kind) {
    case 1: { v = (r.b[r.at] & 0xff) != 0; r.at += 1; return v; }
    case 10: return widenF32(readU32(r));
    case 11: { local bits = readU64(r); return casti2f(bits); }
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

// THE §4 CLAMPS: a declared integer range, then a bits(N) ceiling.
local clampScalar = function(fd, kind, x, report) {
  if (x == null) return null;
  if (typeof x == "array") return x; // a 128-bit value's range is the codec's
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
    s = s + format("%c", cp);
    i += 1 + need;
  }
  return { s = s, ok = true, used = len };
};

local blobOfRange = function(b, at, len) {
  local out = blob(0);
  for (local i = 0; i < len; i++) out.writen(b[at + i] & 0xff, 'b');
  return out;
};

// mutual recursion: every load walk names one of these
local bodyRead, readField, readFieldArray, readArrayBody, readElement, readKeyedElem, readUnionInto, readArmPayload;

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

// ONE BODY (docs/SPEC-TABLES.md §3, §4): fields by id in any order, a field
// this table cannot name skipped by its kind and counted unknown, a kind that
// widens decoded at its own width and counted widened, any other kind a kind
// mismatch skipped whole. A repeated id is counted duplicate and last wins.
bodyRead = function(c, r, T, table, v, report, st, nested) {
  local seen = {};
  for (;;) {
    local ref = leb(r);
    if (r.bad) { report.malformed = true; return false; }
    if (ref == 0) { return true; }
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
      if (!skipField(r, kind)) { report.malformed = true; return false; }
      continue;
    }
    local fd = c.byId[id];
    if (id in seen) { report.duplicate = report.duplicate + 1; }
    seen[id] <- true;
    if (kind != fd.kind) {
      if (widens(kind, fd.kind)) {
        local x = readPrim(r, kind);
        if (x == null || r.bad) { report.malformed = true; return false; }
        x = clampScalar(fd, kind, x, report);
        v[fd.member] = x;
        if (fd.optional) v[fd.member + "Present"] = true;
        report.widened = report.widened + 1;
        continue;
      }
      report.kindMismatch = report.kindMismatch + 1;
      if (!skipField(r, kind)) { report.malformed = true; return false; }
      continue;
    }
    if (!readField(fd, r, T, table, v, report, st, nested)) return false;
  }
};

readField = function(fd, r, T, table, v, report, st, nested) {
  local shape = fd.shape;
  if (shape == "bool" || shape == "int" || shape == "float" || shape == "flags") {
    local x = readPrim(r, fd.kind);
    if (x == null || r.bad) { report.malformed = true; return false; }
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
    if (L > fd.bound) {
      report.clamped = report.clamped + 1;
      local cut = fd.bound;
      while (cut > 0 && ((r.b[cut + r.at] & 0xc0) == 0x80)) cut--;
      v[fd.member] = utf8Decode(r.b, r.at, cut).s;
    } else {
      v[fd.member] = dec.s;
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
      local name = null;
      foreach (pair in fd.variants) { if (pair[1] == id) { name = pair[0]; break; } }
      if (name == null) {
        report.unknown = report.unknown + 1;
        v[fd.member] = null;
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
    st.pending.append({ holder = v, key = fd.member, index = index, fd = fd, target = target });
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
    if (sub.at != sub.end) {
      report.malformed = true;
      v[fd.member] = tableDesc(T, fd.table).make();
    }
    r.at += L;
    if (fd.optional) v[fd.member + "Present"] = true;
    return true;
  }
  if (shape == "union") {
    local u = unionDesc(T, fd.table);
    local inst = u.make();
    v[fd.member] = inst;
    local ok = readUnionInto(r, T, table, u, inst, report, st);
    if (fd.optional) v[fd.member + "Present"] = true;
    return ok;
  }
  if (shape == "bytes" || shape == "arr") {
    return readFieldArray(fd, r, T, table, v, report, st);
  }
  return false;
};

readFieldArray = function(fd, r, T, table, v, report, st) {
  local L = leb(r);
  if (r.bad || r.at + L > r.end) { report.malformed = true; return false; }
  local sub = newReader(r.b, r.at, r.at + L);
  readArrayBody(fd, sub, T, table, v, report, st);
  r.at += L;
  if (fd.optional) v[fd.member + "Present"] = true;
  return true;
};

// AN ARRAY BODY (docs/SPEC-TABLES.md §2.9, §3): a body shorter than its own
// header is inert; a damaged count is malformed and keeps the field; an
// element kind that is not this array's (and does not widen) is a kind
// mismatch and keeps the field; a bounded array's excess is clamped.
readArrayBody = function(fd, r, T, table, v, report, st) {
  if (r.end - r.at < 2) return true;
  local ek = u8(r);
  local count = leb(r);
  if (r.bad) { report.malformed = true; return true; }
  local want = fd.shape == "bytes" ? 6 : fd.elem.kind;
  if (ek != want) {
    if (!widens(ek, want)) {
      report.kindMismatch = report.kindMismatch + 1;
      return true;
    }
    report.widened = report.widened + 1;
  }
  local keep = count;
  if (fd.list) {
    if (count > 0x7fffffff) { v[fd.member] = []; report.malformed = true; return true; }
  } else if (count > fd.bound) {
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
    return true;
  }
  local out = [];
  if (fd.kind == 16) {
    for (local i = 0; i < count; i++) {
      local keyRef = leb(r);
      if (r.bad || keyRef == 0 || keyRef > table.len()) { report.malformed = true; break; }
      local id = table[keyRef - 1];
      local elen = leb(r);
      if (r.bad || r.at + elen > r.end) { report.malformed = true; break; }
      local sr = newReader(r.b, r.at, r.at + elen);
      local res = readKeyedElem(fd.elem, sr, ek, T, table, report, st);
      if (!res[1]) break;
      out.append(res[0]);
      r.at += elen;
    }
    v[fd.member] = out;
    return true;
  }
  for (local i = 0; i < keep; i++) {
    if (!readElement(fd, fd.elem, r, T, table, out, report, st, ek)) break;
  }
  v[fd.member] = out;
  return true;
};

readKeyedElem = function(e, sr, ek, T, table, report, st) {
  if (e.shape == "enum") {
    local ref = leb(sr);
    if (sr.bad || sr.at != sr.end) { report.malformed = true; return [null, false]; }
    if (ref == 0) return [null, true];
    if (ref > table.len()) { report.malformed = true; return [null, false]; }
    local id = table[ref - 1];
    local name = null;
    foreach (pair in e.variants) { if (pair[1] == id) { name = pair[0]; break; } }
    if (name == null) report.unknown = report.unknown + 1;
    return [name, true];
  }
  if (e.shape == "tab") {
    local d = tableDesc(T, e.table);
    local inst = d.make();
    bodyRead(d, sr, T, table, inst, report, st, true);
    if (sr.at != sr.end) { report.malformed = true; return [null, false]; }
    return [inst, true];
  }
  local x = readPrim(sr, ek);
  if (x == null || sr.bad) { report.malformed = true; return [null, false]; }
  return [x, true];
};

readElement = function(fd, e, r, T, table, out, report, st, ek) {
  if (e.shape == "ptr") {
    local index = leb(r);
    if (r.bad) { report.malformed = true; return false; }
    out.append(null);
    st.pending.append({ holder = out, key = out.len() - 1, index = index, fd = e,
      target = tableDesc(T, e.table) });
    return true;
  }
  if (e.shape == "enum") {
    local ref = leb(r);
    if (r.bad) { report.malformed = true; return false; }
    if (ref == 0) { out.append(null); return true; }
    if (ref > table.len()) { report.malformed = true; return false; }
    local id = table[ref - 1];
    local name = null;
    foreach (pair in e.variants) { if (pair[1] == id) { name = pair[0]; break; } }
    if (name == null) report.unknown = report.unknown + 1;
    out.append(name);
    return true;
  }
  if (e.shape == "tab") {
    local L = leb(r);
    if (r.bad || r.at + L > r.end) { report.malformed = true; return false; }
    local sub = newReader(r.b, r.at, r.at + L);
    local d = tableDesc(T, e.table);
    local inst = d.make();
    bodyRead(d, sub, T, table, inst, report, st, true);
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
  local x = readPrim(r, ek);
  if (x == null || r.bad) { report.malformed = true; return false; }
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
    inst.Type = 0;
    r.at = bodyEnd;
    return true;
  }
  local want = arm.payload == null ? 32 : arm.payload.kind;
  if (armKind != want) {
    if (!widens(armKind, want)) {
      report.kindMismatch = report.kindMismatch + 1;
      inst.Type = 0;
      r.at = bodyEnd;
      return true;
    }
    report.widened = report.widened + 1;
  }
  inst.Type = arm.tag;
  local ok = readArmPayload(arm, sub, armKind, T, table, inst, report, st);
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
    local x = readPrim(sub, wireKind);
    if (x == null || sub.bad) { report.malformed = true; inst.Type = 0; return true; }
    inst[p.member] = clampScalar(p, wireKind, x, report);
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
  if (shape == "ptr") {
    local index = leb(sub);
    if (sub.bad || sub.at != sub.end) { report.malformed = true; inst.Type = 0; return true; }
    st.pending.append({ holder = inst, key = p.member, index = index, fd = p,
      target = tableDesc(T, p.table) });
    return true;
  }
  if (shape == "enum") {
    local ref = leb(sub);
    if (sub.bad || sub.at != sub.end) { report.malformed = true; inst.Type = 0; return true; }
    if (ref == 0) { inst[p.member] = null; return true; }
    if (ref > table.len()) { report.malformed = true; inst.Type = 0; return true; }
    local id = table[ref - 1];
    local name = null;
    foreach (pair in p.variants) { if (pair[1] == id) { name = pair[0]; break; } }
    if (name == null) report.unknown = report.unknown + 1;
    inst[p.member] = name;
    return true;
  }
  if (shape == "tab") {
    local d = tableDesc(T, p.table);
    local child = d.make();
    inst[p.member] = child;
    bodyRead(d, sub, T, table, child, report, st, true);
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
    if (!readUnionInto(sub, T, table, u, child, report, st)) return false;
    return true;
  }
  if (shape == "bytes" || shape == "arr") {
    readArrayBody(p, sub, T, table, inst, report, st);
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
            if (n.kind == "s") value = n.s;
            else report.kindMismatch = report.kindMismatch + 1;
          } else if (p.fd.shape == "bblob") {
            if (n.kind == "b") value = blobOfRange(n.b, n.at, n.end - n.at);
            else report.kindMismatch = report.kindMismatch + 1;
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

// THE LOAD (docs/SPEC-TABLES.md §3, §3.1, §4): the form byte, the id table,
// the node table's records first, then every record's body, then the root
// body, then the one numbering's resolution.
_M.load <- function(T, rootName, bytes, report) {
  if (report.unknown == null) report.unknown <- 0;
  if (report.kindMismatch == null) report.kindMismatch <- 0;
  if (report.widened == null) report.widened <- 0;
  if (report.clamped == null) report.clamped <- 0;
  if (report.duplicate == null) report.duplicate <- 0;
  if (report.malformed == null) report.malformed <- false;
  if (report.refused == null) report.refused <- false;

  if (bytes.len() < 1) { report.malformed = true; return null; }
  if ((bytes[0] & 0xff) != FORM_VARIABLE) { report.refused = true; return null; }
  if (bytes.len() < 9) { report.malformed = true; return null; }
  local count = u64At(bytes, bytes.len() - 8);
  if (count < 0 || count * 8 > bytes.len() - 9) { report.malformed = true; return null; }
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

  local c = tableDesc(T, rootName);
  local v = c.make();
  local byType = {};
  foreach (name, d in T.Descriptors) { byType[d.typeId] <- d; }
  local raw = nodeScan(bytes, table, end, report);
  local st = { good = raw != null, nodes = [], root = v, pending = [] };
  if (raw != null) {
    foreach (rec in raw) {
      if (rec.tid == STRING_BLOB_ID) {
        local dec = utf8Decode(rec.b, rec.at, rec.end - rec.at);
        if (!dec.ok) { report.malformed = true; st.nodes.append(null); continue; }
        st.nodes.append({ kind = "s", s = dec.s });
        continue;
      }
      if (rec.tid == BYTES_BLOB_ID) {
        st.nodes.append({ kind = "b", b = rec.b, at = rec.at, end = rec.end });
        continue;
      }
      if (!(rec.tid in byType)) {
        report.unknown = report.unknown + 1;
        st.nodes.append(null);
        continue;
      }
      local d = byType[rec.tid];
      st.nodes.append({ kind = "t", d = d, v = d.make(), b = rec.b, at = rec.at, end = rec.end });
    }
    foreach (n in st.nodes) {
      if (n == null || n.kind != "t") continue;
      bodyRead(n.d, newReader(n.b, n.at, n.end), T, table, n.v, report, st, true);
    }
  }
  bodyRead(c, newReader(bytes, 1, end), T, table, v, report, st, false);
  resolve(st, c, report);
  return v;
};
