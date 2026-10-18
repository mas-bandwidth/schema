// THE BASE64 TEXT CARRIER (docs/SPEC-TABLES.md §16.8): SaveText is the
// padded standard-alphabet base64 of the whole variable binary wire, and
// LoadText is the strict read over the decoded wire. The base64 codec is
// written here in Squirrel because the pinned interpreter ships none. A body
// that does not decode is base64_damaged; a decoded wire the binary reader
// refuses keeps the binary reader's name (§2.14, §16.8).
local _M = vargv[0];
local _require = vargv[1];

local FORM_VARIABLE = 1;
local NODE_TABLE_ID = 0xffffffffffffffff;
local STRING_BLOB_ID = 0x704be0d8faaffc58;
local BYTES_BLOB_ID = 0x2f2ec0474f1c4fe4;

local tableDesc = function(T, name) { return T.Descriptors[name]; };
local unionDesc = function(T, name) { return T[name + "Descriptor"]; };

// ---- THE WRITER: a growable byte list, everything little-endian (§3) ----
local Writer = class {
  b = null;
  constructor() { b = []; }
  function len() { return b.len(); }
  function u8(v) { b.append(v & 0xff); }
  function u16(v) { b.append(v & 0xff); b.append((v >>> 8) & 0xff); }
  function u32(v) { b.append(v & 0xff); b.append((v >>> 8) & 0xff); b.append((v >>> 16) & 0xff); b.append((v >>> 24) & 0xff); }
  function u64(v) {
    for (local i = 0; i < 8; i++) { b.append(v & 0xff); v = v >>> 8; }
  }
  function f64(f) { u64(castf2i(f)); }
  function leb(v) {
    while (v >= 128) { b.append((v & 0x7f) | 0x80); v = v >>> 7; }
    b.append(v & 0x7f);
  }
  function raw(a) {
    if (typeof a == "array") { foreach (x in a) b.append(x & 0xff); }
    else { for (local i = 0; i < a.len(); i++) b.append(a[i] & 0xff); }
  }
  function toBlob() {
    local out = blob(0);
    foreach (x in b) out.writen(x & 0xff, 'b');
    return out;
  }
};

local utf8Encode = function(s) {
  local out = [];
  foreach (c in s) {
    local cp = c & 0xff;
    if (cp < 0x80) out.append(cp);
    else if (cp < 0x800) { out.append(0xc0 | (cp >> 6)); out.append(0x80 | (cp & 0x3f)); }
    else if (cp < 0x10000) { out.append(0xe0 | (cp >> 12)); out.append(0x80 | ((cp >> 6) & 0x3f)); out.append(0x80 | (cp & 0x3f)); }
    else { out.append(0xf0 | (cp >> 18)); out.append(0x80 | ((cp >> 12) & 0x3f)); out.append(0x80 | ((cp >> 6) & 0x3f)); out.append(0x80 | (cp & 0x3f)); }
  }
  return out;
};

local narrowF32 = function(f) {
  if (f != f) {
    local bits = castf2i(f);
    local lo = bits & 0xffffffff;
    local hi = (bits >>> 32) & 0xffffffff;
    local payload = (((hi & 0xfffff) << 3) | (lo >>> 29)) & 0x7fffff;
    if (payload == 0) payload = 0x400000;
    return ((hi & 0x80000000) | 0x7f800000 | payload) & 0xffffffff;
  }
  local tmp = blob(4);
  tmp.writen(f, 'f');
  return ((tmp[0] & 0xff) | ((tmp[1] & 0xff) << 8) | ((tmp[2] & 0xff) << 16) | ((tmp[3] & 0xff) << 24)) & 0xffffffff;
};

local arrayCount = function(fd, val) {
  return fd.list ? val.len() : (val.len() < fd.bound ? val.len() : fd.bound);
};

// ---- THE SAVE (docs/SPEC-TABLES.md §3, §3.1), a port of the JavaScript leg
// (internal/codegen/jstable/variableruntime.go TableVariableSave) ----
local save = function(T, c, value) {
  local nodes = { list = [], index = [] };
  local findIndex = function(arr, v) { foreach (k, x in arr) { if (x == v) return k + 1; } return 0; };
  local addBlob = function(fd, val) {
    local typeId = fd.shape == "sblob" ? STRING_BLOB_ID : BYTES_BLOB_ID;
    local body;
    if (fd.shape == "sblob") body = utf8Encode(val); else body = val;
    if (findIndex(nodes.index, val) != 0) return;
    nodes.list.append({ typeId = typeId, body = body });
    nodes.index.append(val);
  };
  local gather;
  gather = function(c, v, out) {
    foreach (fd in c.fields) {
      local val = v[fd.member];
      if (fd.shape == "sblob" || fd.shape == "bblob") {
        if (val != null) out.append({ blob = true, fd = fd, val = val });
      } else if (fd.shape == "ptr") {
        if (val != null) out.append({ blob = false, c = tableDesc(T, fd.table), val = val });
      } else if (fd.shape == "tab") {
        gather(tableDesc(T, fd.table), val, out);
      } else if (fd.shape == "arr") {
        local n = arrayCount(fd, val);
        if (fd.elem.shape == "ptr") {
          for (local i = 0; i < n; i++) { if (val[i] != null) out.append({ blob = false, c = tableDesc(T, fd.elem.table), val = val[i] }); }
        } else if (fd.elem.shape == "tab") {
          for (local i = 0; i < n; i++) gather(tableDesc(T, fd.elem.table), val[i], out);
        }
      }
    }
    return out;
  };

  nodes.index.append(value);
  local open = [ value ];
  local stack = [ { c = c, v = value, edges = gather(c, value, []), i = 0 } ];
  while (stack.len() > 0) {
    local top = stack[stack.len() - 1];
    if (top.i >= top.edges.len()) {
      open.remove(open.len() - 1);
      stack.remove(stack.len() - 1);
      continue;
    }
    local e = top.edges[top.i];
    top.i++;
    if (e.blob) { addBlob(e.fd, e.val); continue; }
    local isOpen = false;
    foreach (o in open) { if (o == e.val) { isOpen = true; break; } }
    if (isOpen) return null;
    if (findIndex(nodes.index, e.val) != 0) continue;
    nodes.list.append({ typeId = e.c.typeId, codec = e.c, v = e.val });
    nodes.index.append(e.val);
    open.append(e.val);
    stack.append({ c = e.c, v = e.val, edges = gather(e.c, e.val, []), i = 0 });
  }

  local ids = { list = [], index = {} };
  local idsRef = function(id) {
    foreach (k, v in ids.index) { if (k == id) return v; }
    ids.list.append(id);
    local n = ids.list.len();
    ids.index[id] <- n;
    return n;
  };
  local nodeIndex = function(val) { return findIndex(nodes.index, val); };

  local writeField = null;
  local writeBody = null;
  local writeElement = null;
  local writeKeyedElem = null;

  writeBody = function(c, w, v) {
    foreach (fd in c.fields) {
      if (!writeField(fd, w, v)) return false;
    }
    return true;
  };

  writeField = function(fd, w, v) {
    local val = v[fd.member];
    if (fd.shape == "bool") {
      if (val != fd.def) { w.leb(idsRef(fd.id)); w.u8(1); w.u8(val ? 1 : 0); }
      return true;
    }
    if (fd.shape == "int") {
      if (val != fd.def) {
        w.leb(idsRef(fd.id)); w.u8(fd.kind);
        if (fd.width == 1) w.u8(val);
        else if (fd.width == 2) w.u16(val);
        else if (fd.width == 4) w.u32(val);
        else if (fd.width == 16) { w.u64(val[0]); w.u64(val[1]); }
        else w.u64(val);
      }
      return true;
    }
    if (fd.shape == "float") {
      if (fd.f32 ? narrowF32(val) != narrowF32(fd.def) : val != fd.def) {
        w.leb(idsRef(fd.id)); w.u8(fd.kind);
        if (fd.f32) w.u32(narrowF32(val)); else w.f64(val);
      }
      return true;
    }
    if (fd.shape == "flags") {
      if (val != fd.def) { w.leb(idsRef(fd.id)); w.u8(fd.kind); w.u64(val); }
      return true;
    }
    if (fd.shape == "str") {
      if (val != fd.def) {
        local b = utf8Encode(val);
        w.leb(idsRef(fd.id)); w.u8(12);
        w.leb(b.len());
        w.raw(b);
      }
      return true;
    }
    if (fd.shape == "enum") {
      if (val != fd.def) {
        local vref = 0;
        if (val != null) {
          local found = -1;
          foreach (pair in fd.variants) { if (pair[0] == val) { found = idsRef(pair[1]); break; } }
          if (found < 0) return false;
          vref = found;
        }
        w.leb(idsRef(fd.id)); w.u8(30); w.leb(vref);
      }
      return true;
    }
    if (fd.shape == "ptr") {
      if (val != null) { w.leb(idsRef(fd.id)); w.u8(17); w.leb(nodeIndex(val)); }
      return true;
    }
    if (fd.shape == "sblob" || fd.shape == "bblob") {
      if (val != null) { w.leb(idsRef(fd.id)); w.u8(17); w.leb(nodeIndex(val)); }
      return true;
    }
    if (fd.shape == "tab") {
      local mark = ids.list.len();
      local ref = idsRef(fd.id);
      local body = Writer();
      if (!writeBody(tableDesc(T, fd.table), body, val)) return false;
      body.u8(0);
      if (body.len() <= 1) { ids.list.remove(ids.list.len() - 1); return true; }
      w.leb(ref); w.u8(13); w.leb(body.len()); w.raw(body.b);
      return true;
    }
    if (fd.shape == "union") {
      local u = unionDesc(T, fd.table);
      local tag = val.Type;
      if (tag == 0) return true;
      local arm = null;
      foreach (a in u.arms) { if (a.tag == tag) { arm = a; break; } }
      if (arm == null) return false;
      local ref = idsRef(arm.id);
      if (arm.payload == null) { w.leb(ref); w.u8(32); w.leb(0); return true; }
      local body = Writer();
      if (!writeField(arm.payload, body, val)) return false;
      w.leb(ref); w.u8(arm.payload.kind); w.leb(body.len()); w.raw(body.b);
      return true;
    }
    if (fd.shape == "bytes" || fd.shape == "arr") {
      local n = fd.shape == "bytes" ? val.len() : arrayCount(fd, val);
      if (n == 0) return true;
      local ref = idsRef(fd.id);
      local body = Writer();
      if (fd.shape == "bytes") {
        body.u8(6);
        body.leb(n);
        body.raw(val);
      } else if (fd.kind == 16) {
        // keyed array [E]T (§2.4): every present element rides its key ref
        local keys = [];
        foreach (pair in fd.keyvariants) keys.append(pair);
        body.u8(fd.elem.kind);
        local countBody = Writer();
        local nwritten = 0;
        for (local i = 0; i < n; i++) {
          local id = keys[i][1];
          local elBody = Writer();
          if (!writeKeyedElem(fd.elem, elBody, val[i])) return false;
          countBody.leb(idsRef(id));
          countBody.leb(elBody.len());
          countBody.raw(elBody.b);
          nwritten++;
        }
        body.leb(nwritten);
        body.raw(countBody.b);
      } else {
        body.u8(fd.elem.kind);
        body.leb(n);
        for (local i = 0; i < n; i++) {
          if (!writeElement(fd.elem, body, val[i])) return false;
        }
      }
      if (body.len() <= 1) return true;
      w.leb(ref); w.u8(14); w.leb(body.len()); w.raw(body.b);
      return true;
    }
    return false;
  };

  writeElement = function(e, w, val) {
    if (e.shape == "bool") { w.u8(val ? 1 : 0); return true; }
    if (e.shape == "int") {
      if (e.width == 1) w.u8(val);
      else if (e.width == 2) w.u16(val);
      else if (e.width == 4) w.u32(val);
      else if (e.width == 16) { w.u64(val[0]); w.u64(val[1]); }
      else w.u64(val);
      return true;
    }
    if (e.shape == "float") { if (e.f32) w.u32(narrowF32(val)); else w.f64(val); return true; }
    if (e.shape == "flags") { w.u64(val); return true; }
    if (e.shape == "enum") {
      local vref = 0;
      if (val != null) {
        local found = -1;
        foreach (pair in e.variants) { if (pair[0] == val) { found = idsRef(pair[1]); break; } }
        if (found < 0) return false;
        vref = found;
      }
      w.leb(vref);
      return true;
    }
    if (e.shape == "ptr") { w.leb(nodeIndex(val)); return true; }
    if (e.shape == "tab") {
      local body = Writer();
      if (!writeBody(tableDesc(T, e.table), body, val)) return false;
      body.u8(0);
      w.leb(body.len());
      w.raw(body.b);
      return true;
    }
    if (e.shape == "union") {
      local u = unionDesc(T, e.table);
      local tag = val.Type;
      if (tag == 0) { w.leb(0); return true; }
      local arm = null;
      foreach (a in u.arms) { if (a.tag == tag) { arm = a; break; } }
      if (arm == null) return false;
      w.leb(idsRef(arm.id));
      if (arm.payload == null) { w.u8(32); w.leb(0); return true; }
      local body = Writer();
      if (!writeField(arm.payload, body, val)) return false;
      w.u8(arm.payload.kind);
      w.leb(body.len());
      w.raw(body.b);
      return true;
    }
    if (e.shape == "bytes") {
      local body = Writer();
      body.u8(6);
      body.leb(val.len());
      body.raw(val);
      w.leb(body.len());
      w.raw(body.b);
      return true;
    }
    if (e.shape == "arr") {
      local body = Writer();
      body.u8(e.elem.kind);
      body.leb(arrayCount(e, val));
      for (local i = 0; i < arrayCount(e, val); i++) {
        if (!writeElement(e.elem, body, val[i])) return false;
      }
      w.leb(body.len());
      w.raw(body.b);
      return true;
    }
    return false;
  };

  writeKeyedElem = function(e, w, val) {
    if (e.shape == "enum") {
      local vref = 0;
      if (val != null) {
        local found = -1;
        foreach (pair in e.variants) { if (pair[0] == val) { found = idsRef(pair[1]); break; } }
        if (found < 0) return false;
        vref = found;
      }
      w.leb(vref);
      return true;
    }
    if (e.shape == "tab") { if (!writeBody(tableDesc(T, e.table), w, val)) return false; w.u8(0); return true; }
    if (e.shape == "bool") { w.u8(val ? 1 : 0); return true; }
    if (e.shape == "int") {
      if (e.width == 1) w.u8(val);
      else if (e.width == 2) w.u16(val);
      else if (e.width == 4) w.u32(val);
      else if (e.width == 16) { w.u64(val[0]); w.u64(val[1]); }
      else w.u64(val);
      return true;
    }
    if (e.shape == "float") { if (e.f32) w.u32(narrowF32(val)); else w.f64(val); return true; }
    if (e.shape == "ptr") { w.leb(nodeIndex(val)); return true; }
    return false;
  };

  local w = Writer();
  w.u8(FORM_VARIABLE);
  if (!writeBody(c, w, value)) return null;
  if (nodes.list.len() > 0) {
    w.leb(idsRef(NODE_TABLE_ID));
    w.u8(12);
    local sub = Writer();
    sub.leb(nodes.list.len());
    foreach (n in nodes.list) {
      sub.leb(idsRef(n.typeId));
      if (!("codec" in n)) {
        sub.leb(n.body.len());
        sub.raw(n.body);
        continue;
      }
      local body = Writer();
      if (!writeBody(n.codec, body, n.v)) return null;
      body.u8(0);
      sub.leb(body.len());
      sub.raw(body.b);
    }
    w.leb(sub.len());
    w.raw(sub.b);
  }
  w.u8(0);
  foreach (id in ids.list) w.u64(id);
  w.u64(ids.list.len());
  return w.toBlob();
};

// ---- BASE64 codec (standard alphabet, padded; docs/SPEC-TABLES.md §16.8) ----
local B64 = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/";
local b64encode = function(b) {
  local out = "";
  local i = 0;
  local n = b.len();
  while (n - i >= 3) {
    local t = (b[i] << 16) | (b[i + 1] << 8) | b[i + 2];
    out += format("%c", B64[(t >> 18) & 63]) + format("%c", B64[(t >> 12) & 63]) +
           format("%c", B64[(t >> 6) & 63]) + format("%c", B64[t & 63]);
    i += 3;
  }
  if (i < n) {
    local left = n - i;
    local t = b[i] << 16;
    if (left == 2) t = t | (b[i + 1] << 8);
    out += format("%c", B64[(t >> 18) & 63]) + format("%c", B64[(t >> 12) & 63]);
    out += left == 2 ? format("%c", B64[(t >> 6) & 63]) : "=";
    out += "=";
  }
  return out;
};

local b64val = function(c) {
  if (c >= 'A' && c <= 'Z') return c - 'A';
  if (c >= 'a' && c <= 'z') return c - 'a' + 26;
  if (c >= '0' && c <= '9') return c - '0' + 52;
  if (c == '+') return 62;
  if (c == '/') return 63;
  return -1;
};

local b64decode = function(s) {
  if (s.len() % 4 != 0) return null;
  local out = blob(0);
  local i = 0;
  local n = s.len();
  while (i + 4 <= n) {
    local a = b64val(s[i]), b = b64val(s[i + 1]);
    if (a < 0 || b < 0) return null;
    local c = b64val(s[i + 2]), d = b64val(s[i + 3]);
    if (c < 0 && s[i + 2] != '=') return null;
    if (d < 0 && s[i + 3] != '=') return null;
    out.writen(((a << 2) | (b >> 4)) & 0xff, 'b');
    if (s[i + 2] != '=') {
      if (c < 0) return null;
      out.writen((((b & 0xf) << 4) | (c >> 2)) & 0xff, 'b');
    }
    if (s[i + 3] != '=') {
      if (d < 0) return null;
      out.writen((((c & 3) << 6) | d) & 0xff, 'b');
    }
    i += 4;
  }
  return out;
};

// ---- THE SURFACE ----
// SaveText(T, rootName, value) returns the padded base64 of the binary wire.
_M.SaveText <- function(T, rootName, value) {
  local c = tableDesc(T, rootName);
  local wire = save(T, c, value);
  if (wire == null) return null;
  return b64encode(wire);
};

// LoadText(T, rootName, text, report): the strict read over the decoded wire.
// A body that does not decode is base64_damaged; a decoded wire the binary
// reader refuses keeps the binary reader's name.
_M.LoadText <- function(T, rootName, text, report) {
  if (!("unknown" in report)) report.unknown <- 0;
  if (!("kindMismatch" in report)) report.kindMismatch <- 0;
  if (!("widened" in report)) report.widened <- 0;
  if (!("clamped" in report)) report.clamped <- 0;
  if (!("duplicate" in report)) report.duplicate <- 0;
  if (!("malformed" in report)) report.malformed <- false;
  if (!("refused" in report)) report.refused <- false;
  local wire = b64decode(text);
  if (wire == null) { report.refused = true; return null; }
  local L = _require("schema_table_load");
  local value = L.load(T, rootName, wire, report);
  if (report.malformed || report.refused || report.unknown > 0 || report.kindMismatch > 0 ||
      report.clamped > 0 || report.duplicate > 0) {
    report.refused = true;
    return null;
  }
  return value;
};
