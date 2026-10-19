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
local ciUp = function(v, a) { return (v + a - 1) & ~(a - 1); };
local blobKey = function(kind, val) {
  if (kind == "s") return "s:" + val;
  local s = "";
  for (local i = 0; i < val.len(); i++) s += format("%02x", val[i] & 0xff);
  return "b:" + s;
};

local recordFields, armEdges, edgesOf, number, layout, indexOfTable, indexOfBlob, scalarWrite, writeField, recordWrite, elementWrite;

recordFields = function(T, name) {
  local form = T.cookForms[name];
  local desc = T.Descriptors[name];
  local out = [];
  for (local i = 0; i < form.fields.len(); i++) {
    local cf = form.fields[i];
    local fd = desc.fields[i];
    out.append({
      member = fd.member, shape = fd.shape, elem = fd.elem, optional = cf.presentOffset >= 0,
      offset = cf.offset, elemSize = cf.elemSize, storage = cf.storage, table = fd.table,
      isArray = cf.isArray, bound = cf.bound, countOffset = cf.countOffset,
      presentOffset = cf.presentOffset, isPointer = cf.isPointer, variants = fd.variants
    });
  }
  return out;
};

edgesOf = function(T, flds, inst, out) {
  foreach (f in flds) {
    if (f.optional && !inst[f.member + "Present"]) continue;
    local val = inst[f.member];
    if (f.isPointer) {
      if (val != null) {
        if (f.table != null) out.append({ kind = "t", table = f.table, v = val });
        else out.append({ kind = f.shape == "sblob" ? "s" : "b", v = val });
      }
    } else if (f.shape == "tab") {
      edgesOf(T, recordFields(T, f.table), val, out);
    } else if (f.shape == "union") {
      armEdges(T, f.table, val, out);
    } else if (f.shape == "arr") {
      local e = f.elem;
      local n = val == null ? 0 : val.len();
      for (local i = 0; i < n; i++) {
        if (e.shape == "ptr" || e.shape == "sblob" || e.shape == "bblob") {
          if (val[i] != null) {
            if (e.shape == "ptr") out.append({ kind = "t", table = e.table, v = val[i] });
            else out.append({ kind = e.shape == "sblob" ? "s" : "b", v = val[i] });
          }
        } else if (e.shape == "tab") {
          edgesOf(T, recordFields(T, e.table), val[i], out);
        } else if (e.shape == "union") {
          armEdges(T, e.table, val[i], out);
        }
      }
    }
  }
};
armEdges = function(T, uname, inst, out) {
  if (inst == null) return;
  local t = inst.Type;
  if (t == null || t == 0) return;
  local u = T[uname + "Descriptor"];
  local p = null;
  foreach (a in u.arms) { if (a.tag == t) { p = a.payload; break; } }
  if (p == null) return;
  local val = inst[p.member];
  if (p.shape == "ptr") { if (val != null) out.append({ kind = "t", table = p.table, v = val }); }
  else if (p.shape == "sblob" || p.shape == "bblob") { if (val != null) out.append({ kind = p.shape == "sblob" ? "s" : "b", v = val }); }
  else if (p.shape == "tab") { edgesOf(T, recordFields(T, p.table), val, out); }
  else if (p.shape == "arr") {
    local n = val.len();
    for (local i = 0; i < n; i++) {
      if (p.elem.shape == "ptr") { if (val[i] != null) out.append({ kind = "t", table = p.elem.table, v = val[i] }); }
      else if (p.elem.shape == "sblob" || p.elem.shape == "bblob") { if (val[i] != null) out.append({ kind = p.elem.shape == "sblob" ? "s" : "b", v = val[i] }); }
      else if (p.elem.shape == "tab") { edgesOf(T, recordFields(T, p.elem.table), val[i], out); }
      else if (p.elem.shape == "union") { armEdges(T, p.elem.table, val[i], out); }
    }
  }
};

number = function(T, rootName, root) {
  local form = T.cookForms[rootName];
  local nodes = [{ id = T.Descriptors[rootName].typeId, kind = "t", form = form, v = root, key = null, size = form.size, align = form.align }];
  local open = [root];
  local stack = [];
  local push = function(inst, flds) {
    local edges = [];
    edgesOf(T, flds, inst, edges);
    stack.append({ v = inst, edges = edges, i = 0 });
  };
  push(root, recordFields(T, rootName));
  while (stack.len() > 0) {
    local top = stack[stack.len() - 1];
    if (top.i >= top.edges.len()) { open.pop(); stack.pop(); continue; }
    local e = top.edges[top.i];
    top.i = top.i + 1;
    if (e.kind == "s" || e.kind == "b") {
      local key = blobKey(e.kind, e.v);
      local found = false;
      for (local i = 0; i < nodes.len(); i++) {
        if (nodes[i].key == key) { found = true; break; }
      }
      if (!found) nodes.append({ id = e.kind == "s" ? STRING_BLOB_ID : BYTES_BLOB_ID, kind = e.kind, form = null, v = e.v, key = key, size = 8 + e.v.len() + (e.kind == "s" ? 1 : 0), align = 8 });
      continue;
    }
    local target = e.v;
    local isOpen = false;
    foreach (o in open) { if (o == target) { isOpen = true; break; } }
    if (isOpen) return null;
    local found = false;
    for (local i = 0; i < nodes.len(); i++) {
      if (nodes[i].kind == "t" && nodes[i].v == target) { found = true; break; }
    }
    if (found) continue;
    local nform = T.cookForms[e.table];
    nodes.append({ id = T.Descriptors[e.table].typeId, kind = "t", form = nform, v = target, key = null, size = nform.size, align = nform.align });
    open.append(target);
    push(target, recordFields(_M, e.table));
  }
  return nodes;
};

layout = function(nodes) {
  local offsets = array(nodes.len());
  offsets[0] = 0;
  local cursor = nodes[0].size;
  local align = nodes[0].align > 8 ? nodes[0].align : 8;
  for (local k = 1; k < nodes.len(); k++) {
    local a = nodes[k].align > 8 ? nodes[k].align : 8;
    cursor = ciUp(cursor, a);
    offsets[k] = cursor;
    cursor += nodes[k].size;
    if (a > align) align = a;
  }
  local data = ciUp(cursor, align);
  return { offsets = offsets, data = data, align = align };
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
  local key = blobKey(kind, val);
  for (local i = 0; i < nodes.len(); i++) {
    if (nodes[i].kind == kind && nodes[i].key == key) return i;
  }
  return -1;
};

scalarWrite = function(buf, at, f, val, order) {
  local w = f.elemSize;
  if (f.shape == "bool") { put(buf, at, val ? 1 : 0, 1, order); return; }
  if (f.shape == "float") {
    if (w == 4) { put(buf, at, castf2i(val), 4, order); } else { put(buf, at, castf2i(val), 8, order); }
    return;
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

writeField = function(T, f, inst, buf, regionBase, at, order, nodes, offsets) {
  if (f.presentOffset >= 0) put(buf, at + f.presentOffset - f.offset, inst[f.member + "Present"] ? 1 : 0, 1, order);
  if (f.isPointer) {
    local target = inst[f.member];
    if (target == null) { put(buf, at, 0, 8, order); return true; }
    local idx;
    if (f.table != null) idx = indexOfTable(nodes, null, target);
    else idx = indexOfBlob(nodes, f.shape == "sblob" ? "s" : "b", target);
    if (idx < 0) return false;
    local delta = offsets[idx] - (at - regionBase);
    put(buf, at, delta, 8, order);
    return true;
  }
  if (f.shape == "str") {
    local s = inst[f.member];
    local used = s.len();
    for (local i = 0; i < used; i++) buf[at + i] = s[i] & 0xff;
    if (f.countOffset >= 0) put(buf, at + f.countOffset - f.offset, used, 4, order);
    return true;
  }
  if (f.shape == "tab") { return recordWrite(T, recordFields(T, f.table), inst[f.member], buf, regionBase, at, order, nodes, offsets); }
  if (f.shape == "union") {
    local uinst = inst[f.member];
    local t = uinst.Type;
    local u = T[f.table + "Descriptor"];
    local tag = 0;
    local p = null;
    foreach (a in u.arms) { if (a.tag == t) { tag = a.tag; p = a.payload; break; } }
    put(buf, at, tag, 1, order);
    if (p != null && tag != 0) {
      if (p.shape == "ptr") {
        local target = uinst[p.member];
        if (target != null) {
          local idx = indexOfTable(nodes, null, target);
          if (idx < 0) return false;
          local delta = offsets[idx] - (at - regionBase);
          put(buf, at, delta, 8, order);
        } else put(buf, at, 0, 8, order);
      } else if (p.shape == "tab") {
        if (!recordWrite(T, recordFields(T, p.table), uinst[p.member], buf, regionBase, at, order, nodes, offsets)) return false;
      } else if (p.shape == "str") {
        local s = uinst[p.member];
        for (local i = 0; i < s.len(); i++) buf[at + i] = s[i] & 0xff;
      } else if (p.shape == "bytes") {
        local b = uinst[p.member];
        for (local i = 0; i < b.len(); i++) buf[at + i] = b[i] & 0xff;
      } else {
        scalarWrite(buf, at, p, uinst[p.member], order);
      }
    }
    return true;
  }
  if (f.isArray) {
    local val = inst[f.member];
    local n = f.bound;
    if (val != null && val.len() < n) n = val.len();
    for (local i = 0; i < n; i++) {
      if (!elementWrite(T, f.elem, val[i], buf, regionBase, at + i * f.elemSize, order, nodes, offsets, f.elemSize)) return false;
    }
    if (f.countOffset >= 0) put(buf, at + f.countOffset - f.offset, val.len(), 4, order);
    return true;
  }
  if (f.shape == "bytes") {
    local b = inst[f.member];
    local n = b.len();
    for (local i = 0; i < n; i++) buf[at + i] = b[i] & 0xff;
    if (f.countOffset >= 0) put(buf, at + f.countOffset - f.offset, n, 4, order);
    return true;
  }
  scalarWrite(buf, at, f, inst[f.member], order);
  return true;
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
    local delta = offsets[idx] - (at - regionBase);
    put(buf, at, delta, 8, order);
    return true;
  }
  if (e.shape == "tab") return recordWrite(T, recordFields(T, e.table), val, buf, regionBase, at, order, nodes, offsets);
  if (e.shape == "union") {
    local t = val.Type;
    local u = T[e.table + "Descriptor"];
    local tag = 0;
    local p = null;
    foreach (a in u.arms) { if (a.tag == t) { tag = a.tag; p = a.payload; break; } }
    put(buf, at, tag, 1, order);
    if (p != null && tag != 0) {
      if (p.shape == "ptr") {
        local target = val[p.member];
        if (target != null) {
          local idx = indexOfTable(nodes, null, target);
          if (idx < 0) return false;
          local delta = offsets[idx] - (at - regionBase);
          put(buf, at, delta, 8, order);
        } else put(buf, at, 0, 8, order);
      } else if (p.shape == "tab") {
        if (!recordWrite(T, recordFields(T, p.table), val[p.member], buf, regionBase, at, order, nodes, offsets)) return false;
      } else if (p.shape == "str" || p.shape == "bytes") {
        local b = val[p.member];
        for (local i = 0; i < b.len(); i++) buf[at + i] = b[i] & 0xff;
      } else {
        scalarWrite(buf, at, p, val[p.member], order);
      }
    }
    return true;
  }
  if (e.shape == "str" || e.shape == "bytes") {
    local b = val;
    for (local i = 0; i < b.len(); i++) buf[at + i] = b[i] & 0xff;
    return true;
  }
  scalarWrite(buf, at, e, val, order);
  return true;
};

_M.CookMeasure <- function(T, root, value) {
  CUR = T;
  local form = ("cookForms" in T) && (root in T.cookForms) ? T.cookForms[root] : null;
  if (form == null || value == null) return -1;
  local nodes = number(T, root, value);
  if (nodes == null) return -1;
  local lay = layout(nodes);
  local dataOffset = ciUp(HEADER(), lay.align);
  return dataOffset + lay.data + nodes.len() * 16;
};

_M.Cook <- function(T, root, value, buf, order) {
  CUR = T;
  local form = ("cookForms" in T) && (root in T.cookForms) ? T.cookForms[root] : null;
  if (form == null || value == null || order < 1 || order > 2) return false;
  local nodes = number(T, root, value);
  if (nodes == null) return false;
  local lay = layout(nodes);
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
    } else {
      put(buf, at, nd.v.len(), 4, order);
      for (local i = 0; i < nd.v.len(); i++) buf[at + 8 + i] = nd.v[i] & 0xff;
    }
    put(buf, dataOffset + lay.data + k * 16, lay.offsets[k], 8, order);
    put(buf, dataOffset + lay.data + k * 16 + 8, nd.id, 8, order);
  }
  return true;
};
