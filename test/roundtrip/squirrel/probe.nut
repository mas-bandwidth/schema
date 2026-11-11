// The round-trip gate's Squirrel probe (test/roundtrip), run in the generated
// directory:  sq probe.nut <unit module> <inputs dir>
// Every call is the unit module's generated per-root API (<Root>Save,
// <Root>Load, <Root>FixedSave, ...), found by the root's name.
local schema = dofile("schema_module.nut", true);
local T = schema.require(vargv[0]);
local inputs = vargv[1];

local readBlob = function(p) {
  local f = null;
  try { f = file(p, "rb"); } catch (e) { return null; }
  local n = f.len();
  local b = n > 0 ? f.readblob(n) : blob(0);
  f.close();
  return b;
};
local toStr = function(b) {
  local parts = [];
  local s = "";
  for (local i = 0; i < b.len(); i++) {
    s += format("%c", b[i]);
    if (s.len() >= 4096) { parts.append(s); s = ""; }
  }
  parts.append(s);
  local out = "";
  foreach (p in parts) out += p;
  return out;
};
local same = function(a, b) {
  if (a == null || b == null || a.len() != b.len()) return false;
  for (local i = 0; i < a.len(); i++) if ((a[i] & 0xff) != (b[i] & 0xff)) return false;
  return true;
};
local keep = function(root, name, ext, b) {
  local f = file(inputs + "/out/" + root + "/" + name + ext, "wb");
  f.writeblob(b);
  f.close();
};
local say = function(root, name, verb, verdict) {
  print("RT " + root + " " + name + " " + verb + " " + verdict + "\n");
};
local why = function(r) { return ("reason" in r) ? ("" + r.reason) : "?"; };
// call answers f's verdict, and a throw as a refusal by name.
local call = function(f) {
  try { return f(); } catch (e) { return "refused:threw " + e; }
};

foreach (line in split(toStr(readBlob(inputs + "/cases.txt")), "\n")) {
  local parts = split(line, " ");
  if (parts.len() < 3) continue;
  local root = parts[0], name = parts[1], cls = parts[2];
  if (!((root + "FromJson") in T)) { say(root, name, "probe", "refused:the unit generated no such root"); continue; }
  local text = toStr(readBlob(inputs + "/samples/" + root + "/" + name + ".json"));
  local v = null;
  say(root, name, "fromjson", call(function() {
    local r = {};
    v = T[root + "FromJson"](text, r);
    return v == null ? "refused:" + why(r) : "ok";
  }));
  if (v == null) continue;
  local want = T[root + "ToJson"](v);
  local loadVerdict = function(b) {
    local r = {};
    local got = T[root + "Load"](b, r);
    if (got == null) return "refused:" + why(r);
    return T[root + "ToJson"](got) == want ? "ok" : "diff";
  };
  local eng = readBlob(inputs + "/engine/" + root + "/" + name + ".bin");
  local wire = null;
  say(root, name, "save", call(function() {
    wire = T[root + "Save"](v);
    if (wire == null) return "refused:" + schema.require("schema_table_save").SaveReason;
    keep(root, name, ".save", wire);
    return "ok";
  }));
  if (wire != null) say(root, name, "load", call(function() { return loadVerdict(wire); }));
  if (eng != null) say(root, name, "loadeng", call(function() { return loadVerdict(eng); }));
  if (cls == "fixed") {
    say(root, name, "fixedsave", call(function() {
      local fw = blob(T[root + "FixedMeasure"](1));
      local n = T[root + "FixedSave"](fw, [v]);
      if (n != fw.len()) return "refused:" + T.FixedReason;
      keep(root, name, ".fixed", fw);
      return "ok";
    }));
    if (eng != null) {
      say(root, name, "fixedloadeng", call(function() {
        local out = [T[root]()];
        local n = T[root + "FixedLoad"](eng, out, 4);
        if (n != 1) return "refused:" + T.FixedReason;
        return T[root + "ToJson"](out[0]) == want ? "ok" : "diff";
      }));
    }
  }
  if (cls == "variable") {
    if (wire != null) {
      say(root, name, "retain", call(function() {
        local store = { capacity = 1 << 20, idCapacity = 1 << 16 };
        local r = {};
        local got = T[root + "LoadRetain"](wire, store, r);
        if (got == null) return "refused:" + why(r);
        local n = T[root + "MeasureRetain"](got, store);
        if (n < 0) return "refused:measure";
        local back = blob(n);
        local sr = {};
        if (T[root + "SaveRetain"](got, store, back, sr) != n) return "refused:" + why(sr);
        return (T[root + "ToJson"](got) == want && same(back, wire)) ? "ok" : "diff";
      }));
    }
    say(root, name, "json", call(function() {
      local r = {};
      local j = T[root + "FromJson"](want, r);
      if (j == null) return "refused:" + why(r);
      return T[root + "ToJson"](j) == want ? "ok" : "diff";
    }));
  }
}
