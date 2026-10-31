// THE STRICT READ AND ITS NAMED REFUSALS (docs/SPEC-TABLES.md §6.5, §11,
// §2.14). ReadStrict answers the root or refuses with ONE name, the set §6.5
// states and Go's TableReport.Err() answers: wire_damaged (the file's own
// framing), non_finite_float, malformed, clamped and kind_mismatch, each with the
// first field the walk saw move it; a form byte the root's own form does not read
// (previous_form, message_form_as_file, newer_form); and integrity_mismatch.
// It does NOT refuse unknown, widened or duplicate alone (§6.5: unknown is
// evolution, widened decoded exactly; a repeat is last-wins, §4): those stay
// counted on the report. It is a wrapper over schema_table_load's tolerant
// walk and edits nothing in it: the counters are the load's report (§4), and
// the field is read off the walk's own frame when a counter first moves (THE
// WITNESS, below).
local _M = vargv[0];
local _load = vargv[1]("schema_table_load");

local FORM_VARIABLE = 1;
local FORM_MESSAGE = 2;
local FORM_FIXED = 3;

// the report's counters in load.nut's spelling, and §11's name for each
local COUNTERS = {
  unknown = "unknown", kindMismatch = "kind_mismatch", widened = "widened",
  clamped = "clamped", duplicate = "duplicate", malformed = "malformed",
  refused = "refused",
  countRefusal = "count_refusal"
};

// CRC-32C, the Castagnoli polynomial, reflected 0x82F63B78, initial and final
// 0xFFFFFFFF (docs/SPEC-TABLES.md §2.14). No table: a strict read is not hot.
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

// §2.14's order: the form byte first (the caller has read it), then the
// trailer before any field is decoded. A file too short to hold one, or whose
// last four bytes are not the CRC-32C of the bytes before them, is the
// whole-file damage named integrity_mismatch; otherwise the trailer comes off.
local integrityOpen = function(b) {
  if (b.len() < 5 || u32le(b, b.len() - 4) != crc32c(b, b.len() - 4)) return null;
  local out = blob(b.len() - 4);
  for (local i = 0; i < out.len(); i++) out[i] = b[i];
  return out;
};

// THE WITNESS: the field a counter first moved on (docs/SPEC-TABLES.md §11,
// a refusal names the field). load.nut moves a counter as report.<c> = ...
// with the field in its own frame: `fd` (a field descriptor: bodyRead,
// readField, the array and table readers), `p` (a union arm payload, or a
// pending pointer whose `fd` is the slot), or, for an id this table cannot
// name, only the wire `id` (or a node record's `rec.tid`), spelled #<hex>
// because no name exists. The report handed to the load carries NO counter
// slots of its own, so every counter write reaches the delegate's _set, which
// reads those frames with getstackinfos and records the first field per
// counter. The frame walk stops
// at ReadStrict's own frame (its _strictFrame local). The local spellings of
// load.nut are the contract this reads; TestSquirrelStrictReadRefusesByName
// requires a named field for every counter it drives, so a rename there
// fails that test rather than going quiet.
local fieldOf = function(locals) {
  if ("fd" in locals) {
    local fd = locals.fd;
    if (typeof fd == "table" && ("name" in fd)) return fd.name;
  }
  if ("p" in locals) {
    local p = locals.p;
    if (typeof p == "table") {
      if (("fd" in p) && typeof p.fd == "table" && ("name" in p.fd)) return p.fd.name;
      if ("name" in p) return p.name;
    }
  }
  return null;
};

// A pointer-array ELEMENT's pending slot (load.nut's readElement) carries an
// element descriptor with no name: the field is the one whose array IS that
// slot's holder, found below the root or below a node record in resolve's
// frame (`st`, and `root`, the root's descriptor). The holder may sit nested
// deeper — inside an inline table, a union arm or an array element — so the
// search walks the descriptor tree each owner carries, from the owner's own
// fields down through its table fields, its union arms and its array
// elements' tables and unions, and answers the field whose array is the
// holder, exactly as the top-level case does. The walk is bounded by the
// instances already visited and by the depth of the descriptor tree
// (descriptors can refer back to themselves through pointers, which resolve
// only after this walk and hold no nested value here).
local holderField = function(locals, T) {
  if (!("p" in locals) || !("st" in locals) || !("root" in locals)) return null;
  local p = locals.p, st = locals.st;
  if (typeof p != "table" || !("holder" in p) || typeof p.holder != "array") return null;
  if (T == null || typeof T != "table") return null;
  local owners = [[locals.root, st.root]];
  foreach (n in st.nodes) if (n != null && n.kind == "t") owners.append([n.d, n.v]);
  local maxDepth = 0;
  foreach (k, d in T.Descriptors) if (typeof d == "table" && ("fields" in d)) maxDepth++;
  local descend = null;
  local search = function(desc, inst, depth) {
    if (depth > maxDepth || inst == null || (typeof inst != "table" && typeof inst != "instance")) return null;
    if (desc == null || typeof desc != "table") return null;
    if ("fields" in desc) {
      foreach (f in desc.fields) {
        if (!(f.member in inst)) continue;
        local v = inst[f.member];
        if (v == p.holder) return f.name;
        local r = descend(f, v, depth + 1);
        if (r != null) return r;
      }
      return null;
    }
    if ("arms" in desc) {
      local tag = inst.Type;
      foreach (a in desc.arms) {
        if (a.tag != tag) continue;
        if (a.payload == null) return null;
        if (!(a.member in inst)) return null;
        local v = inst[a.member];
        if (v == p.holder) return a.payload.name;
        return descend(a.payload, v, depth + 1);
      }
      return null;
    }
    return null;
  };
  descend = function(f, v, depth) {
    if (v == null) return null;
    if (typeof v == "table" || typeof v == "instance") {
      if (f.shape == "tab" && ("table" in f) && f.table != null) return search(T.Descriptors[f.table], v, depth);
      if (f.shape == "union" && ("table" in f) && f.table != null) return search(T[f.table + "Descriptor"], v, depth);
      return null;
    }
    if (typeof v == "array") {
      if (f.shape != "arr" || !("elem" in f) || f.elem == null) return null;
      local e = f.elem;
      foreach (el in v) {
        local r = null;
        if (e.shape == "tab" && ("table" in e) && e.table != null && (typeof el == "table" || typeof el == "instance"))
          r = search(T.Descriptors[e.table], el, depth);
        else if (e.shape == "union" && ("table" in e) && e.table != null && (typeof el == "table" || typeof el == "instance"))
          r = search(T[e.table + "Descriptor"], el, depth);
        if (r != null) return r;
      }
      return null;
    }
    return null;
  };
  foreach (o in owners) {
    local r = search(o[0], o[1], 0);
    if (r != null) return r;
  }
  return null;
};

local witness = function(store, first) {
  local r = {};
  r.setdelegate({
    _get = function(k) {
      if (k in store) return store[k];
      throw null;
    },
    _set = function(k, v) {
      if (!(k in store)) throw null;
      local moved = typeof v == "bool" ? (v && !store[k]) : (v > store[k]);
      store[k] = v;
      if (!moved || (k in first)) return;
      local name = null;
      local idName = null;
      local T = null;
      for (local lv = 2; ; lv++) {
        local si = ::getstackinfos(lv);
        if (si == null) break;
        local fr = si.locals;
        if ("_strictFrame" in fr) break;
        if (T == null && "T" in fr && typeof fr.T == "table") { T = fr.T; break; }
      }
      for (local lv = 2; ; lv++) {
        local si = ::getstackinfos(lv);
        if (si == null) break;
        local locals = si.locals;
        if ("_strictFrame" in locals) break;
        name = fieldOf(locals);
        if (name == null) name = holderField(locals, T);
        if (name != null) break;
        // a record of a type this build cannot name (the node scan's `rec`)
        // is named by its type id, ahead of any stale loop `id` beside it
        if (idName == null && ("rec" in locals) && typeof locals.rec == "table" &&
            ("tid" in locals.rec) && typeof locals.rec.tid == "integer")
          idName = format("#%016x", locals.rec.tid);
        // only bodyRead's `id` (its frame holds the table descriptor `c`) is a
        // FIELD id; an enum reader's `id` is a variant's
        if (idName == null && ("c" in locals) && ("id" in locals) && typeof locals.id == "integer")
          idName = format("#%016x", locals.id);
      }
      // an id this build cannot name is the field the walk saw, ahead of any
      // field that holds it (bodyRead counts it before its fd is in scope).
      if (idName != null && k == "unknown") name = idName;
      first[k] <- name;
    }
  });
  return r;
};

// the report's reset: every counter at rest, no reason, no field
local reset = function(report) {
  foreach (k, _ in COUNTERS)
    report[k] <- (k == "malformed" || k == "refused" || k == "countRefusal") ? false : 0;
  report.missingRequired <- false;
  report.reason <- null;
  report.field <- null;
  report.nonFiniteFloat <- null;
  report.unknownField <- null;
  report.duplicateField <- null;
};

// ReadStrict(T, root, bytes, report): the root instance or null, with
// report.reason the name of the first refusal (§6.5's vocabulary, the same
// spellings in every port) and report.field the first field the walk saw move
// that counter (null for a form byte, a trailer, or damage no field holds).
// NEITHER CLASS BORROWS THE OTHER'S FORM (docs/SPEC-TABLES.md, The three
// wires): a fixed root with a fixed reader reads form 3 through its own <Root>FixedLoad, which
// names its own refusals (and answers previous_form for a form byte 1 it is
// handed, §3.4); ReadStrict at a declared-fixed root READS a form-1 file as
// the variable form, the accepting read (the ruling, docs/SPEC-TABLES.md
// §6.5). A variable root reads form 1, and a form-3 file at it is newer_form
// (§6.5's previous_form is only a form THIS form is ahead of). Form 2 is
// message_form_as_file at either. A form byte the root does not read moves
// no counter (§3, §6.5).
_M.ReadStrict <- function(T, root, bytes, report) {
  local _strictFrame = true;
  if (report == null) report = {};
  reset(report);
  local d = T.Descriptors[root];
  if (bytes == null || bytes.len() < 1) {
    report.malformed = true;
    report.reason = "wire_damaged";
    return null;
  }
  local form = bytes[0] & 0xff;
  // a DECLARED fixed root this build carries no fixed reader for (the Squirrel
  // fixed form refuses its construct, squirreltable/fixedform.go) is read and
  // written here as form 1 only: form 3 is a form this call does not carry,
  // unknown_form, and its form 1 is read as a variable root's
  local fixedReader = d.fixed && ((root + "FixedLoadTolerant") in T);
  if (d.fixed && !fixedReader && form == FORM_FIXED) { report.reason = "unknown_form"; return null; }
  if (fixedReader && form == FORM_FIXED) {
    local out = [d.make()];
    local n = T[root + "FixedLoadTolerant"](bytes, out, 1);
    if (n < 0) {
      report.reason = T.FixedReason;
      // a non-finite float names its field (docs/SPEC-TABLES.md §3.3)
      if (("FixedField" in T) && T.FixedField != null) report.field = T.FixedField;
      return null;
    }
    if (n != 1) { report.malformed = true; report.reason = "wire_damaged"; return null; }
    return out[0];
  }
  if (form != FORM_VARIABLE) {
    report.reason = form == FORM_MESSAGE ? "message_form_as_file" : "newer_form";
    return null;
  }
  if (("integrity" in d) && d.integrity != null && d.integrity != "") {
    local body = integrityOpen(bytes);
    if (body == null) { report.malformed = true; report.reason = "integrity_mismatch"; return null; }
    bytes = body;
  }
  local store = {};
  foreach (k, _ in COUNTERS) store[k] <- report[k];
  store.missingRequired <- false;
  store.walkDamaged <- false;
  local first = {};
  // the trailer was opened above (§2.14), so the load must not strip it again
  local v = _load.loadTolerant(T, root, bytes, witness(store, first), null, true);
  foreach (k, _ in COUNTERS) report[k] = store[k];
  report.missingRequired = store.missingRequired;
  // A NON-FINITE FLOAT OUTRANKS EVERY OTHER VERDICT (docs/SPEC-TABLES.md
  // §3.3): the load named it on this report, <Owner>.<field>
  if (report.nonFiniteFloat != null) {
    report.reason = "non_finite_float";
    report.field = report.nonFiniteFloat;
    return null;
  }
  // A MISSING REQUIRED FIELD IS ITS OWN NAME (docs/SPEC-TABLES.md §2.3): the
  // load sets malformed for the refusal, and this slot says why, so the strict
  // caller answers missing_required with the field rather than a bare
  // malformed or wire_damaged.
  if (store.missingRequired) {
    report.reason = "missing_required";
    report.field = ("missingRequired" in first) ? first.missingRequired : null;
    return null;
  }
  // a collection count past the int32 storage cap refuses the whole value
  // (§2.8), the builder-path refusal Go answers as *tablewire.CountRefusal on
  // the same bytes; it stands ahead of any damage or counter it met on the way
  if (store.countRefusal) {
    report.reason = "count_refusal";
    report.field = ("countRefusal" in first) ? first.countRefusal : null;
    return null;
  }
  // damage first, then the counters in Err()'s order, so one wire always
  // answers one name (§6.5, §11); unknown, widened and duplicate alone read
  // Both paths discard the value. The witness records whether the file was
  // opened and its body walked: failure before that is wire_damaged, while
  // damage in the body is malformed, matching Go's strict error names.
  if (v == null && store.malformed && !store.walkDamaged) {
    report.reason = "wire_damaged";
    return null;
  }
  // damage, then the refusing counters in Go's Err() order
  foreach (k in ["malformed", "clamped", "kindMismatch"]) {
    local moved = typeof store[k] == "bool" ? store[k] : store[k] > 0;
    if (!moved) continue;
    report.reason = COUNTERS[k];
    report.field = (k in first) ? first[k] : null;
    return null;
  }
  // the fields of the counters that do not refuse, for a caller that asks
  report.unknownField = ("unknown" in first) ? first.unknown : null;
  report.duplicateField = ("duplicate" in first) ? first.duplicate : null;
  return v;
};

// the names this file owns, for a caller that wants the spelling rather than
// the reason string (docs/SPEC-TABLES.md §11, §6.5)
_M.ReadStrictNames <- {
  previous_form = true, message_form_as_file = true, newer_form = true,
  integrity_mismatch = true, wire_damaged = true, unknown_form = true, malformed = true,
  non_finite_float = true, clamped = true, kind_mismatch = true, count_refusal = true,
  missing_required = true,
  // the fixed reader's own, answered through <Root>FixedLoad (§3.4)
  layout_malformed = true, layout_record_too_large = true, no_layout = true,
  plan_too_large = true
};
