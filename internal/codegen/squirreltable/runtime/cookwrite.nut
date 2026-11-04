// THE COOKED FORM in Squirrel (docs/SPEC-TABLES.md §7.6): the WRITE side.
// A runtime writes the cooked file of a value and the bytes are `schema cook`'s,
// byte for byte, in both byte orders. The layout constants are the cook-read
// side's (cookForms, the header words), and the value is a loaded table
// instance (the load runtime's shape).
//
// CookMeasure(T, root, value) answers the whole file's byte length; Cook(T,
// root, value, buffer, order) writes it and answers whether it fit.
local _M = vargv[0];

local STRING_BLOB_ID = 0x704be0d8faaffc58;
local BYTES_BLOB_ID = 0x2f2ec0474f1c4fe4;
local u64le = function(b, at) {
  local lo = (b[at] & 0xff) | ((b[at + 1] & 0xff) << 8) |
             ((b[at + 2] & 0xff) << 16) | ((b[at + 3] & 0xff) << 24);
  local hi = (b[at + 4] & 0xff) | ((b[at + 5] & 0xff) << 8) |
             ((b[at + 6] & 0xff) << 16) | ((b[at + 7] & 0xff) << 24);
  return lo | (hi << 32);
};
local CUR;
local MAGIC = function() { return u64le(CUR.TableCookMagicBytes, 0); };
local BUILD_VERSION = function() { return u64le(CUR.BuildVersionBytes, 0); };
local HEADER = function() { return CUR.TableCookHeaderBytes; };

local put = function(buf, at, v, width, order) {
  for (local i = 0; i < width; i++) {
    local shift = order == 2 ? width - 1 - i : i;
    buf[at + i] = (v >>> (shift * 8)) & 0xff;
  }
};
local put128 = function(buf, at, lo, hi, order) {
  if (order == 2) { put(buf, at, hi, 8, order); put(buf, at + 8, lo, 8, order); }
  else { put(buf, at, lo, 8, order); put(buf, at + 8, hi, 8, order); }
};
// A FLOAT32 IS NARROWED BY BIT SURGERY (internal/tablewire NarrowF32,
// docs/PORT-SQUIRREL.md): the bits Go's float32() writes, ties to even.
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
    local sh = -e - 97;
    if (sh >= 54) return sign << 31;
    local N = 0x10000000000000 | mant;
    local half = 1 << (sh - 1);
    local q = (N + half) >>> sh;
    if (((N & ((1 << sh) - 1)) == half) && (q & 1)) q = q - 1;
    if (q == 0x800000) return (sign << 31) | 0x00800000;
    return (sign << 31) | (q & 0x007fffff);
  }
  local full = 0x10000000000000 | mant;
  local q = roundShift(full, 29);
  if (q >= 0x1000000) { q = q >>> 1; e = e + 1; }
  if (e > 127) return (sign << 31) | 0x7f800000;
  return (sign << 31) | ((e + 127) << 23) | (q & 0x007fffff);
};
local ciUp = function(v, a) { return (v + a - 1) & ~(a - 1); };
local recordFields, elemDefault, number, layout, indexOfTable, indexOfBlob, scalarWrite, writeField, recordWrite, elementWrite, unionForm, unionWrite;
local hasExtent, fieldHasExtent, carveRecord, carveField, carveElement;
local SAVE;
// ONE FIELD: the cook layout facts the read side emits (docs/SPEC-TABLES.md
// §7.2) joined with the descriptor's value shape. A cook field's `offset` is
// its record offset; the arm and element forms use the same join.
local joinField = function(cf, fd) {
  return {
    member = fd.member, shape = fd.shape, elem = fd.elem, optional = cf.presentOffset >= 0,
    offset = cf.offset, elemSize = cf.elemSize, storage = cf.storage, table = fd.table,
    isArray = cf.isArray, bound = cf.bound, countOffset = cf.countOffset,
    presentOffset = cf.presentOffset, isPointer = fd.shape == "ptr", variants = fd.variants,
    f32 = ("f32" in fd) && fd.f32, nf = ("nf" in fd) ? fd.nf : null, list = ("list" in fd) && fd.list,
    counted = ("counted" in fd) && fd.counted,
    elemAlign = cf.elemAlign, entry = ("entry" in fd) ? fd.entry : null,
    mapkey = ("mapkey" in fd) ? fd.mapkey : null
  };
};
recordFields = function(T, name) {
  local form = T.cookForms[name];
  local desc = T.Descriptors[name];
  local out = [];
  for (local i = 0; i < form.fields.len(); i++) out.append(joinField(form.fields[i], desc.fields[i]));
  return out;
};

// Reuse the wire traversal for sorted map entries, composed guards and
// pointer identity. Cook permits shared blobs and attributes each one once.
number = function(T, rootName, root) {
  local numbered = SAVE.numberForCook(T, rootName, root);
  if (numbered == null) {
    _M.CookReason <- SAVE.SaveReason;
    return null;
  }
  local form = T.cookForms[rootName];
  local nodes = [{ id = T.Descriptors[rootName].typeId, kind = "t", form = form, v = root, size = form.size, align = form.nodeAlign }];
  foreach (e in numbered.nodes) {
    if (e.kind == "t") {
      local nf = T.cookForms[e.d.name];
      nodes.append({ id = e.d.typeId, kind = "t", form = nf, v = e.v, size = nf.size, align = nf.nodeAlign });
    } else {
      nodes.append({ id = e.kind == "s" ? STRING_BLOB_ID : BYTES_BLOB_ID, kind = e.kind, form = null, v = e.v, size = 8 + e.v.len() + (e.kind == "s" ? 1 : 0), align = 8 });
    }
  }
  return nodes;
};

fieldHasExtent = function(T, f) {
  if (f.shape == "map" || (f.shape == "arr" && f.list)) return true;
  if (f.shape == "arr") return fieldHasExtent(T, f.elem);
  if (f.shape == "tab") return hasExtent(T, f.table);
  if (f.shape == "union") {
    foreach (a in T[f.table + "Descriptor"].arms)
      if (a.payload != null && fieldHasExtent(T, a.payload)) return true;
  }
  return false;
};
hasExtent = function(T, name) {
  foreach (f in T.Descriptors[name].fields) if (fieldHasExtent(T, f)) return true;
  return false;
};

carveElement = function(T, f, value, at, extentBase, c, buf, regionBase, order, nodes, offsets) {
  if (f.shape == "tab") return carveRecord(T, f.table, value, at, extentBase, c, buf, regionBase, order, nodes, offsets);
  if (f.shape == "union") {
    local uf = unionForm(T, f.table);
    local tag = value == null ? 0 : value.Type;
    if (tag == null || tag < 1 || tag > uf.arms.len()) return true;
    local arm = uf.arms[tag - 1];
    if (arm == null) return true;
    return carveField(T, arm, value[arm.member], at + arm.offset, extentBase, c, buf, regionBase, order, nodes, offsets);
  }
  return true;
};
carveField = function(T, f, value, at, extentBase, c, buf, regionBase, order, nodes, offsets) {
  if (f.shape == "map" || (f.shape == "arr" && f.list)) {
    local isMap = f.shape == "map";
    local indices = null;
    local size = f.elemSize, align = f.elemAlign;
    if (isMap) {
      local ef = T.cookForms[f.entry];
      size = ef.size; align = ef.align;
      if (value != null) {
        if (!(value in c.maps)) c.maps[value] <- SAVE.mapWriteOrder(value, f.mapkey);
        indices = c.maps[value];
      }
    }
    local n = value == null ? 0 : (isMap ? indices.len() : value.len());
    c.at = ciUp(c.at, align);
    local start = c.at;
    c.at += n * size;
    local arrayAt = extentBase + start;
    if (buf != null) {
      put(buf, at, n == 0 ? 0 : arrayAt - at, 8, order);
      put(buf, at + 8, n, 4, order);
    }
    for (local i = 0; i < n; i++) {
      local ev = value[isMap ? indices[i] : i];
      local ea = arrayAt + i * size;
      if (buf != null) {
        local ok = isMap
          ? recordWrite(T, recordFields(T, f.entry), ev, buf, regionBase, ea, order, nodes, offsets)
          : elementWrite(T, f.elem, ev, buf, regionBase, ea, order, nodes, offsets, size);
        if (!ok) return false;
      }
      if (isMap) {
        if (!carveRecord(T, f.entry, ev, ea, extentBase, c, buf, regionBase, order, nodes, offsets)) return false;
      } else if (!carveElement(T, f.elem, ev, ea, extentBase, c, buf, regionBase, order, nodes, offsets)) return false;
    }
    return true;
  }
  if (f.shape == "arr") {
    local n = f.counted ? (value == null ? 0 : value.len()) : f.bound;
    if (n > f.bound) n = f.bound;
    for (local i = 0; i < n; i++) {
      local ev = value != null && i < value.len() ? value[i] : elemDefault(T, f.elem);
      if (!carveElement(T, f.elem, ev, at + i * f.elemSize, extentBase, c, buf, regionBase, order, nodes, offsets)) return false;
    }
    return true;
  }
  return carveElement(T, f, value, at, extentBase, c, buf, regionBase, order, nodes, offsets);
};
carveRecord = function(T, name, value, at, extentBase, c, buf, regionBase, order, nodes, offsets) {
  foreach (f in recordFields(T, name)) {
    local v = value[f.member];
    if (f.optional && !value[f.member + "Present"]) {
      if (f.shape == "map" && v != null && v.len() != 0) {
        _M.CookReason <- "invalid_value";
        return false;
      }
      continue;
    }
    if (!carveField(T, f, v, at + f.offset, extentBase, c, buf, regionBase, order, nodes, offsets)) return false;
  }
  return true;
};

layout = function(T, nodes) {
  local offsets = array(nodes.len());
  local maps = {};
  local cursor = 0, align = 8;
  foreach (k, nd in nodes) {
    if (nd.align > align) align = nd.align;
    cursor = ciUp(cursor, nd.align);
    offsets[k] = cursor;
    local size = nd.size;
    if (nd.kind == "t" && hasExtent(T, nd.form.name)) {
      local c = { at = 0, maps = maps };
      if (!carveRecord(T, nd.form.name, nd.v, 0, 0, c, null, 0, 1, nodes, offsets)) return null;
      size = nd.form.record + c.at;
    }
    cursor += size;
  }
  return { offsets = offsets, data = ciUp(cursor, align), align = align, maps = maps };
};

indexOfTable = function(nodes, root, val) {
  if (val == null) return -1;
  if (val == root) return 0;
  for (local i = 1; i < nodes.len(); i++) {
    if (nodes[i].kind == "t" && nodes[i].v == val) return i;
  }
  return -1;
};
indexOfBlob = function(nodes, kind, val) {
  for (local i = 0; i < nodes.len(); i++) {
    if (nodes[i].kind == kind && nodes[i].v == val) return i;
  }
  return -1;
};

scalarWrite = function(buf, at, f, val, order) {
  // THE SCALAR'S OWN WIDTH, not the FIELD's size: an optional field's size
  // carries its one-byte presence companion (docs/SPEC-TABLES.md §20.2).
  local w = f.elemSize - ((("optional" in f) && f.optional) ? 1 : 0);
  if (f.shape == "bool") { put(buf, at, val ? 1 : 0, 1, order); return true; }
  if (f.shape == "float") {
    // A NaN OR AN INFINITY IS REFUSED (docs/SPEC-TABLES.md §3.3): the cook
    // writer answers false with CookReason non_finite_float: <Owner>.<field>,
    // and a float32 must stay finite at float32
    local bits = (f.f32 || w == 4) ? narrowF32(val.tofloat()) : castf2i(val.tofloat());
    local finite = (f.f32 || w == 4) ? (bits & 0x7f800000) != 0x7f800000 : (bits & 0x7ff0000000000000) != 0x7ff0000000000000;
    if (!finite) {
      if (_M.CookReason == null) _M.CookReason <- "non_finite_float: " + f.nf;
      return false;
    }
    put(buf, at, bits, (f.f32 || w == 4) ? 4 : 8, order);
    return true;
  }
  if (f.shape == "enum") {
    local ordinal = 0;
    local i = 0;
    foreach (pair in f.variants) {
      i++;
      if (pair[0] == val) { ordinal = i; break; }
    }
    put(buf, at, ordinal, w, order);
    return;
  }
  if (w == 16) { put128(buf, at, val[0], val[1], order); return; }
  put(buf, at, val, w, order);
};

// THE BORN ELEMENT of a bounded array's unfilled slots (docs/SPEC-TABLES.md
// §2.9, §7.6): a table or union at its constructor defaults, a scalar at its
// zero, a reference null.
elemDefault = function(T, e) {
  if (e == null) return null;
  if (e.shape == "tab") return T.Descriptors[e.table].make();
  if (e.shape == "union") return T[e.table + "Descriptor"].make();
  if (e.shape == "ptr" || e.shape == "sblob" || e.shape == "bblob" || e.shape == "enum") return null;
  if (e.shape == "str") return "";
  if (e.shape == "bytes") return blob(0);
  if (e.shape == "bool") return false;
  if (e.shape == "float") return 0.0;
  if (("width" in e) && e.width == 16) return [0, 0];
  return 0;
};

// ONE UNION'S ARMS: the tag's width and every non-void arm's cook field,
// joined to the read-side arm payload (docs/SPEC-TABLES.md §2.6, §7.2).
unionForm = function(T, uname) {
  local u = T[uname + "Descriptor"];
  local c = T.cookWriteUnions[uname];
  local arms = [];
  for (local i = 0; i < u.arms.len(); i++) {
    if (u.arms[i].payload == null || c.arms[i] == null) arms.append(null);
    else arms.append(joinField(c.arms[i], u.arms[i].payload));
  }
  return { tagSize = c.tagSize, arms = arms };
};

// A UNION IS THE TAG AT ITS BASE AND THE SET ARM AT THE OVERLAY'S ARM OFFSET
// (docs/SPEC-TABLES.md §2.6, §7.2): every byte outside the set arm stays zero.
unionWrite = function(T, uname, val, buf, regionBase, at, order, nodes, offsets) {
  local uf = unionForm(T, uname);
  local t = (val == null) ? 0 : val.Type;
  local tag = 0;
  if (t != null && t >= 1 && t <= uf.arms.len()) tag = t;
  put(buf, at, tag, uf.tagSize, order);
  if (tag == 0) return true;
  local af = uf.arms[tag - 1];
  if (af == null) return true; // a void arm rides as its tag alone (§2.6)
  return writeField(T, af, val, buf, regionBase, at + af.offset, order, nodes, offsets);
};

writeField = function(T, f, inst, buf, regionBase, at, order, nodes, offsets) {
  if (f.optional) {
    local present = inst[f.member + "Present"];
    put(buf, at + f.presentOffset - f.offset, present ? 1 : 0, 1, order);
  }
  if (f.shape == "map" || (f.shape == "arr" && f.list)) return true;
  if (f.shape == "ptr" || f.shape == "sblob" || f.shape == "bblob") {
    local target = inst[f.member];
    if (target == null) { put(buf, at, 0, 8, order); return true; }
    local idx;
    if (f.shape == "ptr") idx = indexOfTable(nodes, null, target);
    else idx = indexOfBlob(nodes, f.shape == "sblob" ? "s" : "b", target);
    if (idx < 0) return false;
    put(buf, at, offsets[idx] - (at - regionBase), 8, order);
    return true;
  }
  if (f.shape == "str") {
    local str = inst[f.member];
    local used = str == null ? 0 : str.len();
    for (local i = 0; i < used; i++) buf[at + i] = str[i] & 0xff;
    if (f.countOffset >= 0) put(buf, at + f.countOffset - f.offset, used, 4, order);
    return true;
  }
  if (f.shape == "wstr") {
    // WIDE TEXT (docs/SPEC-TABLES.md §7.2): char16_t units in the cook's own
    // byte order, the used length in CODE UNITS at the count companion
    local units = inst[f.member];
    local used = units == null ? 0 : units.len();
    for (local i = 0; i < used; i++) put(buf, at + 2 * i, units[i], 2, order);
    if (f.countOffset >= 0) put(buf, at + f.countOffset - f.offset, used, 4, order);
    return true;
  }
  if (f.shape == "tab") {
    return recordWrite(T, recordFields(T, f.table), inst[f.member], buf, regionBase, at, order, nodes, offsets);
  }
  if (f.shape == "union") {
    return unionWrite(T, f.table, inst[f.member], buf, regionBase, at, order, nodes, offsets);
  }
  if (f.isArray) {
    local val = inst[f.member];
    // A BOUNDED ARRAY WRITES ITS WHOLE STORAGE (docs/SPEC-TABLES.md §7.6): the
    // slots past the live count keep the element's born value, exactly as the
    // reference's storage does.
    local n = f.bound;
    for (local i = 0; i < n; i++) {
      local ev = (val != null && i < val.len()) ? val[i] : elemDefault(T, f.elem);
      if (!elementWrite(T, f.elem, ev, buf, regionBase, at + i * f.elemSize, order, nodes, offsets, f.elemSize)) return false;
    }
    if (f.countOffset >= 0) put(buf, at + f.countOffset - f.offset, val == null ? 0 : val.len(), 4, order);
    return true;
  }
  if (f.shape == "bytes") {
    local b = inst[f.member];
    local n = b == null ? 0 : b.len();
    for (local i = 0; i < n; i++) buf[at + i] = b[i] & 0xff;
    if (f.countOffset >= 0) put(buf, at + f.countOffset - f.offset, n, 4, order);
    return true;
  }
  return scalarWrite(buf, at, f, inst[f.member], order) != false;
};

recordWrite = function(T, flds, inst, buf, regionBase, recBase, order, nodes, offsets) {
  foreach (f in flds) {
    if (!writeField(T, f, inst, buf, regionBase, recBase + f.offset, order, nodes, offsets)) return false;
  }
  return true;
};

elementWrite = function(T, e, val, buf, regionBase, at, order, nodes, offsets, esz) {
  if (e == null) return true;
  if (!("elemSize" in e)) e.elemSize <- esz;
  if (e.shape == "ptr" || e.shape == "sblob" || e.shape == "bblob") {
    if (val == null) { put(buf, at, 0, 8, order); return true; }
    local idx;
    if (e.shape == "ptr") idx = indexOfTable(nodes, null, val);
    else idx = indexOfBlob(nodes, e.shape == "sblob" ? "s" : "b", val);
    if (idx < 0) return false;
    put(buf, at, offsets[idx] - (at - regionBase), 8, order);
    return true;
  }
  if (e.shape == "tab") return recordWrite(T, recordFields(T, e.table), val, buf, regionBase, at, order, nodes, offsets);
  if (e.shape == "union") return unionWrite(T, e.table, val, buf, regionBase, at, order, nodes, offsets);
  if (e.shape == "str" || e.shape == "bytes") {
    local b = val;
    local n = b == null ? 0 : b.len();
    for (local i = 0; i < n; i++) buf[at + i] = b[i] & 0xff;
    return true;
  }
  return scalarWrite(buf, at, e, val, order) != false;
};

_M.CookMeasure <- function(T, save, root, value) {
  CUR = T;
  SAVE = save;
  _M.CookReason <- null;
  local form = ("cookForms" in T) && (root in T.cookForms) ? T.cookForms[root] : null;
  if (form == null || value == null) return -1;
  local nodes = number(T, root, value);
  if (nodes == null) return -1;
  local lay = layout(T, nodes);
  if (lay == null) return -1;
  local dataOffset = ciUp(HEADER(), lay.align);
  local total = dataOffset + lay.data + nodes.len() * 16;
  // THE MEASURE REFUSES WHAT THE WRITE REFUSES (docs/SPEC-TABLES.md §3.3):
  // a NaN or an infinity anywhere in the value is -1 with the Cook's own
  // reason, non_finite_float: <Owner>.<field>, so measure and write never
  // disagree. The walk is the write itself, into scratch.
  if (!_M.Cook(T, save, root, value, blob(total), 1)) return -1;
  return total;
};

_M.Cook <- function(T, save, root, value, buf, order) {
  CUR = T;
  SAVE = save;
  _M.CookReason <- null;
  local form = ("cookForms" in T) && (root in T.cookForms) ? T.cookForms[root] : null;
  if (form == null || value == null || order < 1 || order > 2) return false;
  local nodes = number(T, root, value);
  if (nodes == null) return false;
  local lay = layout(T, nodes);
  if (lay == null) return false;
  local dataOffset = ciUp(HEADER(), lay.align);
  local total = dataOffset + lay.data + nodes.len() * 16;
  if (buf.len() < total) return false;
  for (local i = 0; i < total; i++) buf[i] = 0;
  put(buf, 0, MAGIC(), 8, order);
  put(buf, 8, BUILD_VERSION(), 8, order);
  put(buf, 16, order, 8, order);
  put(buf, 24, lay.data, 8, order);
  put(buf, 32, nodes.len() * 16, 8, order);
  put(buf, 40, lay.align, 8, order);
  for (local k = 0; k < nodes.len(); k++) {
    local nd = nodes[k];
    local at = dataOffset + lay.offsets[k];
    if (nd.kind == "t") {
      if (!recordWrite(CUR, recordFields(CUR, nd.form.name), nd.v, buf, dataOffset, at, order, nodes, lay.offsets)) return false;
      if (hasExtent(T, nd.form.name)) {
        local c = { at = 0, maps = lay.maps };
        if (!carveRecord(T, nd.form.name, nd.v, at, at + nd.form.record, c, buf, dataOffset, order, nodes, lay.offsets)) return false;
      }
    } else {
      put(buf, at, nd.v.len(), 4, order);
      for (local i = 0; i < nd.v.len(); i++) buf[at + 8 + i] = nd.v[i] & 0xff;
    }
    put(buf, dataOffset + lay.data + k * 16, lay.offsets[k], 8, order);
    put(buf, dataOffset + lay.data + k * 16 + 8, nd.id, 8, order);
  }
  return true;
};
