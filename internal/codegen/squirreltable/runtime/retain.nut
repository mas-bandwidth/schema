// THE VARIABLE TABLE WIRE'S RETAIN-UNKNOWN OPT-IN (docs/SPEC-TABLES.md §6.6),
// the Squirrel leg's LoadRetain / MeasureRetain / SaveRetain. It is a port of
// internal/codegen/jstable/retain.go and internal/tablewire/retain.go.
//
// A retained record is READER-PRIVATE (§6.6, "A retained record is
// reader-private"): it carries the field's id, its kind and a RESOLVED payload
// in which every reference is the sixty-four-bit id it named. Capture is one
// pass over the bytes the plain load skipped; the save re-interns each id into
// the file's one table and writes the record at the end of its own body, before
// the terminator and, for the root, before the node table.
local _M = vargv[0];
local _require = vargv[1];
// required once here, never per call (a require walks the name)
local L = _require("schema_table_load");
local S = _require("schema_table_save");

local NODE_TABLE_ID = 0xffffffffffffffff;
local BUILD_VERSION_ID = 0xfffffffffffffffe;
local MESSAGE_VOCABULARY_ID = 0xfffffffffffffffd;

// §6.6's walk cap: 64 nested bodies, the field's own payload not counted.
local RETAIN_DEPTH_CAP = 64;

// §3's scalar widths, the same table load.nut and save.nut use.
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

// kinds whose payload is a length and its bytes, copied verbatim (§3, §6.6)
local isLengthKind = function(kind) {
  return kind == 12 || kind == 31 || kind == 32 || kind == 33;
};

local cursor = function(b, at, end) { return { b = b, at = at, end = end, bad = false }; };

local cleb = function(c) {
  local v = 0, shift = 0, start = c.at;
  for (local i = 0; i < 10; i++) {
    if (c.at >= c.end) { c.bad = true; c.at = start; return 0; }
    local x = c.b[c.at] & 0xff;
    c.at++;
    if (i == 9 && x > 1) { c.bad = true; c.at = start; return 0; }
    if (i == 9) v = 0x4000000000000000;
    else v = v | ((x & 0x7f) << shift);
    if ((x & 0x80) == 0) {
      if (i > 0 && x == 0) { c.bad = true; c.at = start; return 0; }
      return v;
    }
    shift += 7;
  }
  c.bad = true; c.at = start; return 0;
};

local cu8 = function(c) {
  if (c.at + 1 > c.end) { c.bad = true; return 0; }
  local v = c.b[c.at] & 0xff;
  c.at++;
  return v;
};
local cu64 = function(c) {
  if (c.at + 8 > c.end) { c.bad = true; return 0; }
  local b = c.b, at = c.at;
  local lo = (b[at] & 0xff) | ((b[at + 1] & 0xff) << 8) |
             ((b[at + 2] & 0xff) << 16) | ((b[at + 3] & 0xff) << 24);
  local hi = (b[at + 4] & 0xff) | ((b[at + 5] & 0xff) << 8) |
             ((b[at + 6] & 0xff) << 16) | ((b[at + 7] & 0xff) << 24);
  c.at += 8;
  return lo | (hi << 32);
};

// THE RESOLVED FORM's writer: a list of bytes. Lengths inside a record are this
// port's own spelling, written AFTER what they frame (§6.6).
local outU8 = function(out, v) { out.append(v & 0xff); };
local outU64 = function(out, v) {
  for (local i = 0; i < 8; i++) out.append((v >>> (8 * i)) & 0xff);
};
local outRaw = function(out, b, at, n) {
  for (local i = 0; i < n; i++) out.append(b[at + i] & 0xff);
};

// idOf resolves one reference against the file's id table and answers the id it
// names (§6.6, "Retention form"). A reference at zero, past the table, or at a
// reserved id drops the record.
local idOf = function(c, ref, table) {
  if (ref == 0 || ref > table.len()) { c.bad = true; return 0; }
  local id = table[ref - 1];
  if (id == 0 || id == NODE_TABLE_ID || id == BUILD_VERSION_ID || id == MESSAGE_VOCABULARY_ID) {
    c.bad = true;
    return 0;
  }
  return id;
};

local resolvePayload, resolveContent, resolveBody;

// skipPayload steps one payload by §3's framing alone and answers false on
// damage; kinds 13, 14, 15, 16, 17 and 30 are never copied (§6.6).
local skipPayload = function(c, kind) {
  local width = kindWidth(kind);
  if (width > 0) {
    if (c.at + width > c.end) return false;
    c.at += width;
    return true;
  }
  if (isLengthKind(kind)) {
    local n = cleb(c);
    if (c.bad || n < 0 || n > c.end - c.at) return false;
    c.at += n;
    return true;
  }
  return false;
};

// framed reads one length and resolves the content it frames; a nested length
// past its parent drops the record (§6.6).
resolveContent = function(c, kind, table, out, depth) {
  if (c.bad) return;
  if (depth > RETAIN_DEPTH_CAP) { c.bad = true; return; }
  switch (kind) {
    case 17: { c.bad = true; return; }
    case 13: { resolveBody(c, table, out, depth); break; }
    case 14: case 16: {
      local ek = cu8(c);
      if (c.bad) return;
      if (ek == 17) { c.bad = true; return; }
      outU8(out, ek);
      local count = cleb(c);
      if (c.bad || count < 0) { c.bad = true; return; }
      outU64(out, count);
      for (local i = 0; i < count; i++) {
        if (c.bad) return;
        if (kind == 16) {
          local ref = cleb(c);
          if (c.bad) return;
          outU64(out, idOf(c, ref, table));
          if (c.bad) return;
          local n = cleb(c);
          if (c.bad || n < 0 || c.at + n > c.end) { c.bad = true; return; }
          local sub = cursor(c.b, c.at, c.at + n);
          c.at += n;
          local mark = out.len();
          outU64(out, 0);
          resolveContent(sub, ek, table, out, depth + 1);
          if (sub.bad || sub.at != sub.end) c.bad = true;
          if (c.bad) return;
          local len = out.len() - mark - 8;
          for (local k = 0; k < 8; k++) out[mark + k] = (len >>> (8 * k)) & 0xff;
        } else {
          resolvePayload(c, ek, table, out, depth);
        }
      }
      break;
    }
    case 15: case 30: resolvePayload(c, kind, table, out, depth); break;
    default: {
      local from = c.at;
      if (!skipPayload(c, kind)) { c.bad = true; return; }
      outRaw(out, c.b, from, c.at - from);
    }
  }
  if (!c.bad && c.at != c.end) c.bad = true;
};

resolvePayload = function(c, kind, table, out, depth) {
  if (c.bad) return;
  switch (kind) {
    case 17: c.bad = true; return;
    case 30: {
      local ref = cleb(c);
      if (c.bad) return;
      if (ref == 0) { outU64(out, 0); return; }
      outU64(out, idOf(c, ref, table));
      return;
    }
    case 15: {
      local ref = cleb(c);
      if (c.bad) return;
      if (ref == 0) { outU64(out, 0); return; }
      outU64(out, idOf(c, ref, table));
      if (c.bad) return;
      local armKind = cu8(c);
      if (c.bad) return;
      outU8(out, armKind);
      local n = cleb(c);
      if (c.bad || n < 0 || c.at + n > c.end) { c.bad = true; return; }
      local sub = cursor(c.b, c.at, c.at + n);
      c.at += n;
      local mark = out.len();
      outU64(out, 0);
      resolveContent(sub, armKind, table, out, depth + 1);
      if (sub.bad || sub.at != sub.end) c.bad = true;
      if (c.bad) return;
      local len = out.len() - mark - 8;
      for (local k = 0; k < 8; k++) out[mark + k] = (len >>> (8 * k)) & 0xff;
      return;
    }
    case 13: case 14: case 16: {
      local n = cleb(c);
      if (c.bad || n < 0 || n > c.end - c.at) { c.bad = true; return; }
      local sub = cursor(c.b, c.at, c.at + n);
      c.at += n;
      local mark = out.len();
      outU64(out, 0);
      resolveContent(sub, kind, table, out, depth + 1);
      if (sub.bad || sub.at != sub.end) c.bad = true;
      if (c.bad) return;
      local len = out.len() - mark - 8;
      for (local k = 0; k < 8; k++) out[mark + k] = (len >>> (8 * k)) & 0xff;
      return;
    }
    default: {
      local from = c.at;
      if (!skipPayload(c, kind)) { c.bad = true; return; }
      outRaw(out, c.b, from, c.at - from);
    }
  }
};

resolveBody = function(c, table, out, depth) {
  for (;;) {
    if (c.bad) return;
    local ref = cleb(c);
    if (c.bad) return;
    if (ref == 0) { outU64(out, 0); return; }
    outU64(out, idOf(c, ref, table));
    if (c.bad) return;
    local kind = cu8(c);
    if (c.bad) return;
    outU8(out, kind);
    resolvePayload(c, kind, table, out, depth);
  }
};

// ---- capture --------------------------------------------------------------

local capture = function(store, inst, bytes, start, end, id, kind, table, report) {
  if (kind == 17) {
    // A FIELD WHOSE PAYLOAD CARRIES A NODE INDEX: excluded at the outer kind
    // (§6.6, the exclusions table)
    report.retainLost = report.retainLost + 1;
    return;
  }
  local c = cursor(bytes, start, end);
  local out = [];
  resolvePayload(c, kind, table, out, 0);
  if (c.bad || c.at != end) {
    // the plain read's framing was sound; the walk's verdict changes nothing
    // else, and the record is dropped with one retain_lost (§6.6)
    report.retainLost = report.retainLost + 1;
    return;
  }
  local cost = 9 + out.len();
  if (store.used + cost > store.capacity) {
    // A RECORD IS NEVER PARTIAL: a full buffer degrades one field at a time
    report.retainLost = report.retainLost + 1;
    return;
  }
  store.recs.append({ body = inst, id = id, kind = kind, payload = out, placed = false });
  store.used = store.used + cost;
  report.retained = report.retained + 1;
};

// ---- emit -----------------------------------------------------------------

local emitPayload, emitContent, emitBody;

local emitRaw = function(w, c, from, n) {
  for (local i = 0; i < n; i++) w.b.append(c.b[from + i] & 0xff);
};

local emitLeb = function(w, v) {
  while (v >= 128) { w.b.append((v & 0x7f) | 0x80); v = v >>> 7; }
  w.b.append(v & 0x7f);
};

// THE EMIT IN FLIGHT: the store, the unit's known ids, and whether the record
// being emitted went bad. ONE FILE STILL CARRIES ONE ID TABLE (§6.6): an id this
// build can name takes its entry from the generated table, a retained id takes
// one from the caller's list, and a retained id past the list's capacity drops
// its record with one retain_lost; the save is never refused.
local EMIT = null;

local intern = function(store, id) {
  foreach (x in store.ids) if (x == id) return true;
  if (store.ids.len() >= store.idCapacity) return false;
  store.ids.append(id);
  return true;
};

local emitRef = function(w, ids, id) {
  if (EMIT != null && !(id in EMIT.known) && !intern(EMIT.store, id)) {
    EMIT.bad = true;
    emitLeb(w, 0);
    return;
  }
  emitLeb(w, ids.ref(id));
};

emitContent = function(w, c, kind, ids) {
  switch (kind) {
    case 13: { emitBody(w, c, ids); break; }
    case 14: case 16: {
      local ek = cu8(c);
      if (c.bad) return;
      w.b.append(ek & 0xff);
      local count = cu64(c);
      if (c.bad) return;
      emitLeb(w, count);
      for (local i = 0; i < count; i++) {
        if (c.bad) return;
        if (kind == 16) {
          local id = cu64(c);
          if (c.bad) return;
          emitRef(w, ids, id);
          local n = cu64(c);
          if (c.bad || c.at + n > c.end) { c.bad = true; return; }
          local sub = cursor(c.b, c.at, c.at + n);
          c.at += n;
          local inner = { b = [] };
          emitContent(inner, sub, ek, ids);
          if (c.bad) return;
          emitLeb(w, inner.b.len());
          foreach (x in inner.b) w.b.append(x);
        } else {
          emitPayload(w, c, ek, ids);
        }
      }
      break;
    }
    case 15: case 30: emitPayload(w, c, kind, ids); break;
    default: {
      local from = c.at;
      if (!skipPayload(c, kind)) { c.bad = true; return; }
      emitRaw(w, c, from, c.at - from);
    }
  }
  if (!c.bad && c.at != c.end) c.bad = true;
};

emitPayload = function(w, c, kind, ids) {
  if (c.bad) return;
  switch (kind) {
    case 30: {
      local id = cu64(c);
      if (c.bad) return;
      if (id == 0) { emitLeb(w, 0); return; }
      emitRef(w, ids, id);
      return;
    }
    case 15: {
      local id = cu64(c);
      if (c.bad) return;
      if (id == 0) { emitLeb(w, 0); return; }
      emitRef(w, ids, id);
      local armKind = cu8(c);
      if (c.bad) return;
      w.b.append(armKind & 0xff);
      local n = cu64(c);
      if (c.bad || c.at + n > c.end) { c.bad = true; return; }
      local sub = cursor(c.b, c.at, c.at + n);
      c.at += n;
      local inner = { b = [] };
      emitContent(inner, sub, armKind, ids);
      if (c.bad) return;
      emitLeb(w, inner.b.len());
      foreach (x in inner.b) w.b.append(x);
      return;
    }
    case 13: case 14: case 16: {
      local n = cu64(c);
      if (c.bad || c.at + n > c.end) { c.bad = true; return; }
      local sub = cursor(c.b, c.at, c.at + n);
      c.at += n;
      local inner = { b = [] };
      emitContent(inner, sub, kind, ids);
      if (c.bad) return;
      emitLeb(w, inner.b.len());
      foreach (x in inner.b) w.b.append(x);
      return;
    }
    default: {
      local from = c.at;
      if (!skipPayload(c, kind)) { c.bad = true; return; }
      emitRaw(w, c, from, c.at - from);
    }
  }
};

emitBody = function(w, c, ids) {
  for (;;) {
    local id = cu64(c);
    if (c.bad) return;
    if (id == 0) { w.b.append(0); return; }
    emitRef(w, ids, id);
    local kind = cu8(c);
    if (c.bad) return;
    w.b.append(kind & 0xff);
    emitPayload(w, c, kind, ids);
    if (c.bad) return;
  }
};

// ONE MARK, BOTH STORES: a record's ids are appended to the file's table and to
// the caller's list as the emit reaches them, so a record that goes bad partway
// is undone in both, and counts one retain_lost (§6.6; internal/tablewire/
// retain.go, `emitRetained`).
local emit = function(store, inst, w, ids) {
  foreach (rec in store.recs) {
    if (rec.body != inst) continue;
    local mark = ids.list.len();
    local idMark = store.ids.len();
    EMIT = { store = store, known = store.known, bad = false };
    local c = cursor(rec.payload, 0, rec.payload.len());
    local inner = { b = [] };
    emitRef(inner, ids, rec.id);
    inner.b.append(rec.kind & 0xff);
    emitPayload(inner, c, rec.kind, ids);
    local bad = EMIT.bad || c.bad || c.at != c.end;
    EMIT = null;
    rec.placed = true; // the walk REACHED it, whatever came of the emit
    if (bad) {
      while (ids.list.len() > mark) delete ids.index[ids.list.pop()];
      while (store.ids.len() > idMark) store.ids.pop();
      store.emitLost = store.emitLost + 1;
      continue;
    }
    foreach (x in inner.b) w.b.append(x);
  }
};

// ---- the surface ----------------------------------------------------------

// Each save starts from a clean interner and an unplaced record set: a second
// save of the same region reaches the same ids in the same order, which is what
// makes it byte-identical (§6.6).
local resetPlaced = function(T, store) {
  foreach (rec in store.recs) rec.placed = false;
  store.ids <- [];
  store.emitLost <- 0;
  store.known <- ("TableRetainKnown" in T) ? T.TableRetainKnown : {};
  if (!("idCapacity" in store)) store.idCapacity <- 0;
};

_M.LoadRetain <- function(T, rootName, bytes, store, report) {
  store.recs <- [];
  store.used <- 0;
  if (!("capacity" in store)) store.capacity <- 0;
  store.ids <- [];
  if (!("idCapacity" in store)) store.idCapacity <- 0;
  if (!("retained" in report)) report.retained <- 0;
  if (!("retainLost" in report)) report.retainLost <- 0;
  report.retained = 0;
  report.retainLost = 0;
  store.capture <- function(inst, b, start, end, id, kind, table, r) {
    capture(store, inst, b, start, end, id, kind, table, r);
  };
  store.emit <- function(inst, w, ids, nodes, root) {
    emit(store, inst, w, ids);
  };
  store.has <- function(inst) {
    foreach (rec in store.recs) if (rec.body == inst) return true;
    return false;
  };
  return L.loadTolerant(T, rootName, bytes, report, store);
};

_M.MeasureRetain <- function(T, rootName, value, store) {
  resetPlaced(T, store);
  local b = S.save(T, rootName, value, store);
  return b == null ? -1 : b.len();
};

_M.SaveRetain <- function(T, rootName, value, store, out, report) {
  if (report == null) return -1;
  resetPlaced(T, store);
  if (!("retainLost" in report)) report.retainLost <- 0;
  local b = S.save(T, rootName, value, store);
  if (b == null) return -1;
  local lost = store.emitLost;
  foreach (rec in store.recs) if (!rec.placed) lost++;
  report.retainLost = report.retainLost + lost;
  if (out != null) {
    for (local i = 0; i < b.len(); i++) out[i] = b[i] & 0xff;
  }
  return b.len();
};
