// THE VARIABLE TABLE WIRE'S SAVE AND MEASURE (docs/SPEC-TABLES.md §3, §3.1),
// driven by the descriptors internal/codegen/squirreltable emits (§8.1). The
// walk is a port of internal/codegen/jstable/variableruntime.go's THE SAVE,
// ONE FIELD and THE NUMBERING, which are ports of internal/tablewire's
// encode.go and nodes.go.
//
// _M.save(T, root, value) answers the form-1 blob or null; _M.measure answers
// its byte length or -1. T is a unit's table module; root is the root table's
// schema name.
local _M = vargv[0];

// THE RESERVED IDS (docs/SPEC-TABLES.md §3.1, §5), the same ones load.nut names.
local NODE_TABLE_ID = 0xffffffffffffffff;
local STRING_BLOB_ID = 0x704be0d8faaffc58;
local BYTES_BLOB_ID = 0x2f2ec0474f1c4fe4;
local MAP_KEY_ID = 0x3dc94a19365b10ec;
local MAP_VALUE_ID = 0x7ce4fd9430e80cea;

local FORM_VARIABLE = 1;

// THE GUARD'S ELISION (docs/SPEC-TABLES.md §4, §16.2): a field inside an
// `if` group rides only while every [member, want] pair of its composed
// condition (the `guard` slot internal/codegen/squirreltable/json.go emits)
// holds on the same instance; a field on the untaken side is neither written
// nor numbered, exactly as the text form's guardHolds (json.nut) and the
// engine's GuardHolds elide it. A field with no guard slot always rides.
local guardHolds = function(fd, v) {
  if (!("guard" in fd) || fd.guard == null) return true;
  foreach (term in fd.guard) {
    if (term[0] == null) return false;
    if ((v[term[0]] ? true : false) != term[1]) return false;
  }
  return true;
};

local newWriter = function() {
  // owner and field name the refusal a deprecated variant makes
  // (docs/SPEC-TABLES.md §2.15)
  return { b = [], owner = null, field = null, reason = null };
};
local wu8 = function(w, v) { w.b.append(v & 0xff); };

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

// canonical unsigned LEB128 (docs/SPEC-TABLES.md §3)
local wleb = function(w, v) {
  while (v >= 128) { wu8(w, (v & 0x7f) | 0x80); v = v >>> 7; }
  wu8(w, v & 0x7f);
};

// a little-endian fixed-width scalar, low byte first (§3)
local wfixed = function(w, v, width) {
  for (local i = 0; i < width; i++) { wu8(w, v & 0xff); v = v >>> 8; }
};

local appendWriter = function(w, sub) {
  foreach (x in sub.b) w.b.append(x);
};

// the id table interns ids in FIRST-USE order, so the write order is the bytes
// (docs/SPEC-TABLES.md §3)
local newIds = function() { return { list = [], index = {}, ref = null, retain = null }; };
local idsRef = function(ids, id) {
  if (id in ids.index) return ids.index[id];
  ids.list.append(id);
  local n = ids.list.len();
  ids.index[id] <- n;
  return n;
};
local idsMark = function(ids) { return ids.list.len(); };
local idsRollback = function(ids, mark) {
  while (ids.list.len() > mark) {
    local id = ids.list.pop();
    delete ids.index[id];
  }
};

// A FLOAT32 IS NARROWED BY BIT SURGERY (internal/tablewire NarrowF32,
// docs/PORT-SQUIRREL.md, 64-BIT AND 128-BIT INTEGERS): the NaN payload is
// preserved exactly, and every other value is IEEE round-to-nearest,
// ties-to-even, the bytes Go's float32() writes.
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
    // a float32 SUBNORMAL (docs/SPEC-TABLES.md §3): the value is
    // 1.mantissa * 2^e = N * 2^(e-52), and the subnormal unit is 2^-149,
    // so the significand is N >> (-e-97), rounded to nearest, ties to even.
    local s = -e - 97;
    if (s >= 54) return sign << 31;
    local N = 0x10000000000000 | mant;
    local half = 1 << (s - 1);
    local q = (N + half) >>> s;
    if (((N & ((1 << s) - 1)) == half) && (q & 1)) q = q - 1;
    if (q == 0x800000) return (sign << 31) | 0x00800000;
    return (sign << 31) | (q & 0x007fffff);
  }
  local full = 0x10000000000000 | mant; // 53 bits, the implicit one included
  local q = roundShift(full, 29);
  if (q >= 0x1000000) { q = q >>> 1; e = e + 1; }
  if (e > 127) return (sign << 31) | 0x7f800000;
  return (sign << 31) | ((e + 127) << 23) | (q & 0x007fffff);
};

local writeF64 = function(w, x) { wfixed(w, castf2i(x), 8); };

// A TABLE FLOAT IS FINITE (docs/SPEC-TABLES.md §3.3, the packet wire's rule): every writer refuses a NaN or an infinity, and a float32
// field's value must stay finite at float32 too, so a double past float32's
// range, which narrows to an infinity, is refused rather than written as one.
// The refusal names the field, non_finite_float: <Owner>.<field>, on
// SaveReason. A finite value rides as its bits, -0 and the subnormals exact.
local floatFinite = function(d, val) {
  val = val.tofloat();
  if (d.f32) return (narrowF32(val) & 0x7f800000) != 0x7f800000;
  return (castf2i(val) & 0x7ff0000000000000) != 0x7ff0000000000000;
};
local refuseNonFinite = function(d) {
  if (_M.SaveReason == null) _M.SaveReason <- "non_finite_float: " + d.nf;
  return false;
};

// ELISION COMPARES A FLOAT'S BITS (docs/SPEC-TABLES.md §3.3): -0 under a +0
// default rides, and a float32 compares at float32
local floatIsDefault = function(d, val, def) {
  // a COMPRESSED float compares by value at float32 (the engine's
  // cellIsDefault): its -0 is +0 (§3.3), so it elides as +0 does
  if (d.f32 && d.fmin != null) {
    local a = narrowF32(val.tofloat()), b = narrowF32(def.tofloat());
    return a == b || ((a & 0x7fffffff) == 0 && (b & 0x7fffffff) == 0);
  }
  if (d.f32) return narrowF32(val.tofloat()) == narrowF32(def.tofloat());
  return castf2i(val.tofloat()) == castf2i(def.tofloat());
};

local scalarEqual = function(val, def) { return val == def; };
local scalarDefault = function(val, def) {
  if (typeof val == "array") {
    if (typeof def != "array" || val.len() != def.len()) return false;
    for (local i = 0; i < val.len(); i++) if (val[i] != def[i]) return false;
    return true;
  }
  return scalarEqual(val, def);
};

local tableDesc = function(T, name) { return T.Descriptors[name]; };
local unionDesc = function(T, name) { return T[name + "Descriptor"]; };

// the wire kind of a field descriptor (docs/SPEC-TABLES.md §3): the descriptor
// stores the scalar kind, so the length-shaped and reference shapes name their
// own number here
local wireKind = function(d) {
  if (d.shape == "union") return 15;
  if (d.shape == "tab") return 13;
  if (d.shape == "ptr" || d.shape == "sblob" || d.shape == "bblob") return 17;
  if (d.shape == "str") return 12;
  if (d.shape == "wstr") return 33;
  if (d.shape == "bytes") return 14;
  if (d.shape == "arr") return d.kind == 16 ? 16 : 14;
  if (d.shape == "enum") return 30;
  return d.kind;
};

// WIDE TEXT'S PAYLOAD (docs/SPEC-TABLES.md §3; kind 33): the code units two
// bytes each little-endian, framed by the caller. Save checks the USED LENGTH
// and nothing about the content (SPEC.md §4.12): ill-formed text is a
// read-side verdict. A slot that is not a code unit at all, an integer in
// [0, 0xFFFF], is not wide text storage and refuses the save.
local wideFits = function(val, bound) {
  if (typeof val != "array" || val.len() > bound) return false;
  foreach (u in val) if (typeof u != "integer" || u < 0 || u > 0xFFFF) return false;
  return true;
};
local wideUnits = function(w, val) {
  foreach (u in val) { wu8(w, u); wu8(w, u >> 8); }
};

local hexBytes = function(b) {
  local s = "";
  for (local i = 0; i < b.len(); i++) s = s + format("%02x", b[i] & 0xff);
  return s;
};

local blobEqString = function(b, s) {
  if (b.len() != s.len()) return false;
  for (local i = 0; i < s.len(); i++) {
    if ((b[i] & 0xff) != (s[i] & 0xff)) return false;
  }
  return true;
};

// the numbering's node key for a blob: a *string has no storage identity in
// Squirrel, and a load hands equal content to equal slots, so content keys it;
// a *bytes blob is keyed by its bytes for the same reason (blob_shared).
local blobKey = function(kind, val) {
  if (kind == "s") return "s:" + val;
  return "b:" + hexBytes(val);
};

local mapKeyCmp = function(a, b) {
  if (a == null && b == null) return 0;
  if (a == null) return -1;
  if (b == null) return 1;
  if (a < b) return -1;
  if (a > b) return 1;
  return 0;
};

// THE ORDER A MAP'S ENTRIES RIDE (docs/SPEC-TABLES.md §2.8): ascending by key,
// a key met twice keeping its LAST entry, whatever order the caller built
// them in. The writer holds the sort, so the numbering walks this order and
// the field writes it. The sort breaks a key tie on the entry's position, so
// "last" is the caller's last however the sort is implemented.
local mapWriteOrder = function(val, keyMember) {
  local order = [];
  for (local i = 0; i < val.len(); i++) order.append(i);
  order.sort(function(x, y) {
    local c = mapKeyCmp(val[x][keyMember], val[y][keyMember]);
    return c != 0 ? c : (x < y ? -1 : (x > y ? 1 : 0));
  });
  local idx = [];
  local haveLast = false;
  local last = null;
  foreach (e in order) {
    local key = val[e][keyMember];
    if (haveLast && mapKeyCmp(last, key) == 0) { idx[idx.len() - 1] = e; }
    else { idx.append(e); haveLast = true; last = key; }
  }
  return idx;
};

// THE NUMBERING (docs/SPEC-TABLES.md §3.1): first-visit order of a depth-first
// pre-order walk from the root over POINTER EDGES, descending through every
// by-value edge. Node 1 is the root; record k is index k + 1.
local edgesOf;
local shapeEdges;
local armEdges;

shapeEdges = function(T, d, val, out) {
  if (d.shape == "ptr") {
    if (val != null) out.append({ kind = "t", d = tableDesc(T, d.table), v = val });
    return;
  }
  if (d.shape == "sblob" || d.shape == "bblob") {
    if (val != null) out.append({ kind = d.shape == "sblob" ? "s" : "b", v = val });
    return;
  }
  if (d.shape == "tab") {
    edgesOf(T, tableDesc(T, d.table), val, out);
    return;
  }
  if (d.shape == "union") {
    armEdges(T, unionDesc(T, d.table), val, out);
    return;
  }
  if (d.shape == "map") {
    // a map reaches its entry values BY VALUE, in the order it writes them,
    // ascending by key (docs/SPEC-TABLES.md §2.8, §3.1)
    local kd = d.mapkey;
    local vd = d.mapvalue;
    foreach (i in mapWriteOrder(val, kd.member)) {
      local ent = val[i];
      shapeEdges(T, kd, ent[kd.member], out);
      shapeEdges(T, vd, ent[vd.member], out);
    }
    return;
  }
  if (d.shape == "arr") {
    local e = d.elem;
    local n = val.len();
    if (e.shape == "ptr") {
      local td = tableDesc(T, e.table);
      for (local i = 0; i < n; i++) {
        if (val[i] != null) out.append({ kind = "t", d = td, v = val[i] });
      }
    } else if (e.shape == "sblob" || e.shape == "bblob") {
      for (local i = 0; i < n; i++) {
        if (val[i] != null) out.append({ kind = e.shape == "sblob" ? "s" : "b", v = val[i] });
      }
    } else if (e.shape == "tab") {
      local td = tableDesc(T, e.table);
      for (local i = 0; i < n; i++) edgesOf(T, td, val[i], out);
    } else if (e.shape == "union") {
      local u = unionDesc(T, e.table);
      for (local i = 0; i < n; i++) armEdges(T, u, val[i], out);
    }
  }
};

edgesOf = function(T, c, v, out) {
  foreach (fd in c.fields) {
    if (!guardHolds(fd, v)) continue;
    if (fd.optional && !v[fd.member + "Present"]) continue;
    local val = v[fd.member];
    if (fd.shape == "tab" && val == null) continue;
    if (fd.shape == "arr" && val == null) continue;
    shapeEdges(T, fd, val, out);
  }
};

armEdges = function(T, u, inst, out) {
  if (inst == null) return;
  local t = inst.Type;
  if (t == null || t == 0) return;
  local arm = null;
  foreach (a in u.arms) { if (a.tag == t) { arm = a; break; } }
  if (arm == null) return;
  local p = arm.payload;
  if (p == null) return;
  shapeEdges(T, p, inst[p.member], out);
};

local number = function(T, c, root) {
  local nodes = [];
  local seenBlobs = { s = {}, b = {} };
  local open = [root];
  local rootVisible = true;
  local stack = [];
  local push = function(inst, d) {
    local edges = [];
    edgesOf(T, d, inst, edges);
    stack.append({ v = inst, d = d, edges = edges, i = 0 });
  };
  push(root, c);
  while (stack.len() > 0) {
    local top = stack[stack.len() - 1];
    if (top.i >= top.edges.len()) { open.pop(); stack.pop(); continue; }
    local e = top.edges[top.i];
    top.i = top.i + 1;
    if (e.kind == "s" || e.kind == "b") {
      // A BLOB IS OWNED BY ONE SLOT (docs/SPEC-TABLES.md §2.5): a second slot
      // naming one blob is refused by name, a record the writer never shares.
      // A *bytes blob is keyed by its object, and a *string by its value (a
      // Squirrel string has no storage identity), so both spot the second
      // naming of one blob.
      if (e.v in seenBlobs[e.kind]) {
        _M.SaveReason <- "blob_shared";
        return null;
      }
      seenBlobs[e.kind][e.v] <- true;
      nodes.append({ kind = e.kind, v = e.v });
      continue;
    }
    local target = e.v;
    local isOpen = false;
    foreach (o in open) { if (o == target) { isOpen = true; break; } }
    if (isOpen) return null; // a data cycle is refused (§3.1)
    local found = -1;
    for (local i = 0; i < nodes.len(); i++) {
      if (nodes[i].kind == "t" && nodes[i].v == target) { found = i; break; }
    }
    if (found >= 0) continue;
    nodes.append({ kind = "t", v = target, d = e.d, key = null });
    open.append(target);
    push(target, e.d);
  }
  return { nodes = nodes, root = root };
};

// the node index of one table or blob (0 null, 1 root, k + 1 record k)
local indexOfTable = function(nodes, root, val) {
  if (val == null) return 0;
  if (val == root) return 1;
  for (local i = 0; i < nodes.len(); i++) {
    if (nodes[i].kind == "t" && nodes[i].v == val) return i + 2;
  }
  return -1;
};
local indexOfBlob = function(nodes, kind, val) {
  if (val == null) return 0;
  for (local i = 0; i < nodes.len(); i++) {
    if (nodes[i].kind == kind && nodes[i].v == val) return i + 2;
  }
  return -1;
};

// one field's value inside an array body or an arm payload (no id, no kind)
local writePrim, writeElement, writeValue, writeUnionValue, writeTabBody, writeBare, writeMapField;

// ONE SCALAR VALUE (docs/SPEC-TABLES.md §3): a bool one byte, an integer or
// flags little-endian at its width, a float32 narrowed and a float64 whole,
// an enum as a reference into the id table (§5).
writePrim = function(w, d, val, ids) {
  if (d.shape == "bool") { wu8(w, val ? 1 : 0); return true; }
  if (d.shape == "float") {
    if (!floatFinite(d, val)) return refuseNonFinite(d);
    if (d.f32) { wfixed(w, narrowF32(val.tofloat()), 4); } else { writeF64(w, val.tofloat()); }
    return true;
  }
  if (d.shape == "enum") {
    if (val == null) { wleb(w, 0); return true; }
    foreach (pair in d.variants) {
      if (pair[0] == val) {
        if (pair.len() > 2 && pair[2]) {
          // A STRICT WRITE REFUSES A VALUE HOLDING A DEPRECATED VARIANT
          // (docs/SPEC-TABLES.md §2.15): the value is the caller's.
          local where = (w.owner == null ? "" : w.owner + ".") + (w.field == null ? "" : w.field);
          w.reason = "invalid_value: " + where + " names the deprecated variant " + val;
          return false;
        }
        wleb(w, idsRef(ids, pair[1])); return true;
      }
    }
    return false;
  }
  if (d.shape == "int" || d.shape == "flags") {
    if (d.width == 16) {
      wfixed(w, val[0], 8);
      wfixed(w, val[1], 8);
    } else {
      wfixed(w, val, d.width);
    }
    return true;
  }
  return false;
};

writeTabBody = function(T, d, val, w, ids, nodes, root) {
  writeValue(T, d, val, w, ids, nodes, root, false);
};

// the fields of one body in declaration order, then its zero reference
local writeFields = function(T, c, v, w, ids, nodes, root) {
  foreach (fd in c.fields) {
    if (!guardHolds(fd, v)) continue;
    local present = !fd.optional || v[fd.member + "Present"];
    if (!present) continue;
    local val = v[fd.member];
    local prevOwner = w.owner, prevField = w.field;
    w.owner = c.name; w.field = fd.name;
    local ok = writeValue(T, fd, val, w, ids, nodes, root, true);
    w.owner = prevOwner; w.field = prevField;
    if (!ok) return false;
  }
  // THE RETAINED TAIL: at the end of their own body, in the order retained,
  // before the terminator and (for the root) before the node table
  // (docs/SPEC-TABLES.md §6.6)
  if (ids.retain != null) ids.retain.emit(v, w, ids, nodes, root);
  return true;
};

local writeArrayBody = function(T, d, val, w, ids, nodes, root) {
  local e = d.elem;
  wu8(w, e.kind == 0 ? 6 : e.kind);
  local n = val.len();
  if (e.shape == "tab" && e.kind == 12) {
    // A PLAIN TEXT LIST's element is kind 12 (docs/SPEC-TABLES.md §2.13, §3):
    // its storage is the generated element record, but the wire carries the
    // record's one `value` string as its own L and bytes.
    wleb(w, n);
    for (local i = 0; i < n; i++) {
      local s = val[i].Value;
      wleb(w, s.len());
      for (local j = 0; j < s.len(); j++) wu8(w, s[j]);
    }
    return n;
  }
  if (d.kind == 16) {
    // AN ENUM-KEYED ARRAY (docs/SPEC-TABLES.md §2.4, §3.2): only the slots that
    // are not their element's default ride, each as key reference, L, payload.
    // The key id is interned BEFORE the slot's payload, as the reference's
    // writer does (internal/tablewire encode.go, emitWireArrayElement).
    local entries = [];
    for (local i = 0; i < n; i++) {
      local rides = false;
      if (e.shape == "tab") {
        local probe = newWriter();
        if (!writeBare(T, e, val[i], probe, newIds(), nodes, root)) return -1;
        rides = probe.b.len() > 1;
      } else if (e.shape == "union") {
        rides = val[i] != null && val[i].Type != null && val[i].Type != 0;
      } else if (e.shape == "ptr" || e.shape == "sblob" || e.shape == "bblob") {
        rides = val[i] != null;
      } else if (e.shape == "float") {
        rides = !floatIsDefault(e, val[i], e.def);
      } else {
        rides = !scalarDefault(val[i], e.def);
      }
      // a slot whose only content is a retained field is not its default
      // (§6.6): it rides on the retained tail like any declared field would
      if (!rides && ids.retain != null && ids.retain.has(val[i])) rides = true;
      if (!rides) continue;
      local keyRef = idsRef(ids, d.keyvariants[i][1]);
      local payload = newWriter();
      if (!writeBare(T, e, val[i], payload, ids, nodes, root)) return -1;
      entries.append({ key = keyRef, payload = payload });
    }
    wleb(w, entries.len());
    foreach (ent in entries) {
      wleb(w, ent.key);
      wleb(w, ent.payload.b.len());
      appendWriter(w, ent.payload);
    }
    return entries.len();
  }
  wleb(w, n);
  for (local i = 0; i < n; i++) {
    local v = val[i];
    if (e.shape == "tab") {
      local body = newWriter();
      if (!writeFields(T, tableDesc(T, e.table), v, body, ids, nodes, root)) return -1;
      wu8(body, 0);
      wleb(w, body.b.len());
      appendWriter(w, body);
    } else if (e.shape == "union") {
      if (!writeUnionValue(T, unionDesc(T, e.table), v, w, ids, nodes, root)) return -1;
    } else if (e.shape == "ptr") {
      local idx = indexOfTable(nodes, root, v);
      if (idx < 0) return -1;
      wleb(w, idx);
    } else if (e.shape == "sblob" || e.shape == "bblob") {
      local kind = e.shape == "sblob" ? "s" : "b";
      local idx = indexOfBlob(nodes, kind, v);
      if (idx < 0) return -1;
      wleb(w, idx);
    } else {
      if (!writePrim(w, e, v, ids)) return -1;
    }
  }
  return n;
};

// AN ARRAY FIELD RIDES (docs/SPEC-TABLES.md §3): a list or counted array when
// it holds any element, a fixed array when any element is not its default.
local arrayRides = function(fd, val) {
  local n = val.len();
  if (n == 0) return false;
  if (fd.list || fd.counted) return true;
  local e = fd.elem;
  if (e.shape == "tab") return true;
  if (e.shape == "union") {
    for (local i = 0; i < n; i++) { if (val[i] != null && val[i].Type != null && val[i].Type != 0) return true; }
    return false;
  }
  if (e.shape == "ptr" || e.shape == "sblob" || e.shape == "bblob") {
    for (local i = 0; i < n; i++) { if (val[i] != null) return true; }
    return false;
  }
  for (local i = 0; i < n; i++) {
    if (!scalarDefault(val[i], e.def)) return true;
  }
  return false;
};

// A UNION VALUE (docs/SPEC-TABLES.md §3): the arm reference (0 for none),
// then the payload kind, its length and the payload; a payload-free arm
// rides as kind 32 with length 0.
writeUnionValue = function(T, u, inst, w, ids, nodes, root) {
  local t = inst.Type;
  if (t == null || t == 0) { wleb(w, 0); return true; }
  local arm = null;
  foreach (a in u.arms) { if (a.tag == t) { arm = a; break; } }
  if (arm == null) return false;
  local ref = idsRef(ids, arm.id);
  wleb(w, ref);
  local p = arm.payload;
  if (p == null) { wu8(w, 32); wleb(w, 0); return true; }
  wu8(w, wireKind(p));
  local payload = newWriter();
  if (!writeValue(T, p, inst[p.member], payload, ids, nodes, root, false)) return false;
  wleb(w, payload.b.len());
  appendWriter(w, payload);
  return true;
};

// ONE VALUE WITHOUT ITS FIELD FRAMING, for an arm payload or a keyed slot:
// the arm's or entry's L already frames it (docs/SPEC-TABLES.md §3).
writeBare = function(T, d, val, w, ids, nodes, root) {
  local shape = d.shape;
  if (shape == "ptr") {
    local idx = indexOfTable(nodes, root, val);
    if (idx < 0) return false;
    wleb(w, idx);
    return true;
  }
  if (shape == "sblob" || shape == "bblob") {
    local kind = shape == "sblob" ? "s" : "b";
    local idx = indexOfBlob(nodes, kind, val);
    if (idx < 0) return false;
    wleb(w, idx);
    return true;
  }
  if (shape == "tab") {
    if (!writeFields(T, tableDesc(T, d.table), val, w, ids, nodes, root)) return false;
    wu8(w, 0);
    return true;
  }
  if (shape == "union") return writeUnionValue(T, unionDesc(T, d.table), val, w, ids, nodes, root);
  if (shape == "str") {
    for (local i = 0; i < val.len(); i++) wu8(w, val[i]);
    return true;
  }
  if (shape == "wstr") {
    if (!wideFits(val, d.bound)) return false;
    wideUnits(w, val);
    return true;
  }
  if (shape == "bytes") {
    wu8(w, 6); wleb(w, val.len());
    for (local i = 0; i < val.len(); i++) wu8(w, val[i]);
    return true;
  }
  if (shape == "arr") return writeArrayBody(T, d, val, w, ids, nodes, root) >= 0;
  return writePrim(w, d, val, ids);
};

// A MAP FIELD (docs/SPEC-TABLES.md §2.8): the entries ride in ASCENDING KEY
// ORDER with no key twice — a writer's rule, byte-identical output requires
// it — as kind 14 over element kind 13, each entry an L-framed `{ key, value }`
// body written through the generated entry's own descriptor. An EMPTY plain
// map elides (the by-value rule, §3); an OPTIONAL map is present when
// writeFields reaches it, and a present empty map is WRITTEN (§2.8): absent
// and present-empty are distinct, so only absence elides. An entry ALWAYS
// rides, because identity here is the key.
writeMapField = function(T, d, val, w, ids, nodes, root) {
  local n = val.len();
  // a present empty optional map writes its field header and `0d 00` (§2.8)
  if (n == 0 && !d.optional) return true;
  local idx = mapWriteOrder(val, d.mapkey.member);
  local mark = idsMark(ids);
  local field = newWriter();
  wleb(field, idsRef(ids, d.id)); wu8(field, 14);
  local body = newWriter();
  wu8(body, 13);
  wleb(body, idx.len());
  local ed = tableDesc(T, d.entry);
  foreach (e in idx) {
    local entry = newWriter();
    if (!writeFields(T, ed, val[e], entry, ids, nodes, root)) { idsRollback(ids, mark); return false; }
    wu8(entry, 0);
    wleb(body, entry.b.len());
    appendWriter(body, entry);
  }
  wleb(field, body.b.len());
  appendWriter(field, body);
  appendWriter(w, field);
  return true;
};

// ONE FIELD (docs/SPEC-TABLES.md §3): elided at its declared default, a
// pointer and a blob at null, an array when empty, a nested table when its
// body is its terminator alone; an elided field costs the id table nothing.
writeValue = function(T, d, val, w, ids, nodes, root, framed) {
  local shape = d.shape;
  local always = d.optional || d.required;
  if (!framed) return writeBare(T, d, val, w, ids, nodes, root);
  if (shape == "ptr") {
    if (!always && val == null) return true;
    local idx = indexOfTable(nodes, root, val);
    if (idx < 0) return false;
    wleb(w, idsRef(ids, d.id)); wu8(w, 17); wleb(w, idx);
    return true;
  }
  if (shape == "sblob" || shape == "bblob") {
    if (!always && val == null) return true;
    local kind = shape == "sblob" ? "s" : "b";
    local idx = indexOfBlob(nodes, kind, val);
    if (idx < 0) return false;
    wleb(w, idsRef(ids, d.id)); wu8(w, 17); wleb(w, idx);
    return true;
  }
  if (shape == "tab") {
    local mark = idsMark(ids);
    local field = newWriter();
    wleb(field, idsRef(ids, d.id)); wu8(field, 13);
    local body = newWriter();
    if (!writeFields(T, tableDesc(T, d.table), val, body, ids, nodes, root)) {
      idsRollback(ids, mark); return false;
    }
    wu8(body, 0);
    if (!always && body.b.len() <= 1) { idsRollback(ids, mark); return true; }
    wleb(field, body.b.len());
    appendWriter(field, body);
    appendWriter(w, field);
    return true;
  }
  if (shape == "union") {
    if (!always && (val.Type == null || val.Type == 0)) return true;
    wleb(w, idsRef(ids, d.id)); wu8(w, 15);
    return writeUnionValue(T, unionDesc(T, d.table), val, w, ids, nodes, root);
  }
  if (shape == "str") {
    if (!always && scalarDefault(val, d.def)) return true;
    if (val.len() > d.bound) return false;
    wleb(w, idsRef(ids, d.id)); wu8(w, 12);
    wleb(w, val.len());
    for (local i = 0; i < val.len(); i++) wu8(w, val[i]);
    return true;
  }
  if (shape == "wstr") {
    // the empty value is the default wide text takes (SPEC §4.12), so it elides
    if (!always && typeof val == "array" && val.len() == 0) return true;
    if (!wideFits(val, d.bound)) return false;
    wleb(w, idsRef(ids, d.id)); wu8(w, 33);
    wleb(w, val.len() * 2);
    wideUnits(w, val);
    return true;
  }
  if (shape == "bytes") {
    if (!always && blobEqString(val, d.def)) return true;
    if (val.len() > d.bound) return false;
    wleb(w, idsRef(ids, d.id)); wu8(w, 14);
    local body = newWriter();
    wu8(body, 6); wleb(body, val.len());
    for (local i = 0; i < val.len(); i++) wu8(body, val[i]);
    wleb(w, body.b.len());
    appendWriter(w, body);
    return true;
  }
  if (shape == "arr") {
    if (d.kind == 16) {
      // a keyed array interns its header id first, then its slot vocabulary;
      // roll the whole field back when every slot is at its default
      local mark = idsMark(ids);
      local field = newWriter();
      wleb(field, idsRef(ids, d.id)); wu8(field, 16);
      local body = newWriter();
      local count = writeArrayBody(T, d, val, body, ids, nodes, root);
      if (count < 0) { idsRollback(ids, mark); return false; }
      if (!always && count == 0) { idsRollback(ids, mark); return true; }
      wleb(field, body.b.len());
      appendWriter(field, body);
      appendWriter(w, field);
      return true;
    }
    if (!always && !arrayRides(d, val)) return true;
    if (!d.list && val.len() > d.bound) return false;
    wleb(w, idsRef(ids, d.id)); wu8(w, 14);
    local body = newWriter();
    local n = writeArrayBody(T, d, val, body, ids, nodes, root);
    if (n < 0) return false;
    wleb(w, body.b.len());
    appendWriter(w, body);
    return true;
  }
  if (shape == "map") {
    return writeMapField(T, d, val, w, ids, nodes, root);
  }
  // a scalar: bool, int, flags, float or enum
  if (!always) {
    if (shape == "float") {
      if (floatIsDefault(d, val, d.def)) return true;
    } else if (scalarDefault(val, d.def)) {
      return true;
    }
  }
  wleb(w, idsRef(ids, d.id)); wu8(w, wireKind(d));
  return writePrim(w, d, val, ids);
};

// THE SAVE (docs/SPEC-TABLES.md §3, §3.1): the form byte, the root's fields,
// the node table LAST in the root body under the reserved id at kind 12, the
// terminator, then the id table and its count.
_M.save <- function(T, rootName, value, retain = null) {
  _M.SaveReason <- null;
  local c = tableDesc(T, rootName);
  local numbered = number(T, c, value);
  if (numbered == null) return null;
  local nodes = numbered.nodes;
  local ids = newIds();
  ids.ref = function(id) { return idsRef(ids, id); };
  ids.retain = retain;
  local w = newWriter();
  wu8(w, FORM_VARIABLE);
  if (!writeFields(T, c, value, w, ids, nodes, value)) {
    if (w.reason != null) _M.SaveReason <- w.reason;
    return null;
  }
  if (nodes.len() > 0) {
    wleb(w, idsRef(ids, NODE_TABLE_ID)); wu8(w, 12);
    local sub = newWriter();
    wleb(sub, nodes.len());
    foreach (n in nodes) {
      if (n.kind == "s") {
        wleb(sub, idsRef(ids, STRING_BLOB_ID));
        wleb(sub, n.v.len());
        for (local i = 0; i < n.v.len(); i++) wu8(sub, n.v[i]);
        continue;
      }
      if (n.kind == "b") {
        wleb(sub, idsRef(ids, BYTES_BLOB_ID));
        wleb(sub, n.v.len());
        for (local i = 0; i < n.v.len(); i++) wu8(sub, n.v[i]);
        continue;
      }
      wleb(sub, idsRef(ids, n.d.typeId));
      local body = newWriter();
      if (!writeFields(T, n.d, n.v, body, ids, nodes, value)) return null;
      wu8(body, 0);
      wleb(sub, body.b.len());
      appendWriter(sub, body);
    }
    wleb(w, sub.b.len());
    appendWriter(w, sub);
  }
  wu8(w, 0);
  foreach (id in ids.list) wfixed(w, id, 8);
  wfixed(w, ids.list.len(), 8);
  local out = blob(w.b.len());
  for (local i = 0; i < w.b.len(); i++) out[i] = w.b[i] & 0xff;
  // an integrity root appends the CRC-32C trailer over every byte before it
  // (docs/SPEC-TABLES.md §2.14), four bytes little-endian, after the id table.
  if (("integrity" in c) && c.integrity != null && c.integrity != "") {
    local n = out.len();
    local ext = blob(n + 4);
    for (local i = 0; i < n; i++) ext[i] = out[i];
    local chk = crc32c(out, n);
    ext[n] = chk & 0xff;
    ext[n + 1] = (chk >>> 8) & 0xff;
    ext[n + 2] = (chk >>> 16) & 0xff;
    ext[n + 3] = (chk >>> 24) & 0xff;
    out = ext;
  }
  return out;
};

// THE MEASURE (docs/SPEC-TABLES.md §3.1): it derives the same numbering and
// the same bytes, so measure == save holds across a pointer graph.
_M.measure <- function(T, rootName, value, retain = null) {
  local b = _M.save(T, rootName, value, retain);
  if (b == null) return -1;
  return b.len();
};
