// test/conformance/squirrel/main.nut — the SQUIRREL conformance driver's one
// script (test/conformance/README.md, test/conformance/squirrel/driver; docs/
// PORT-SQUIRREL.md, THE HARNESS).
//
// usage: sq main.nut <manifest> list
//        sq main.nut <manifest> <surface> <outdir>
//
// The working directory is the repository root, so every path the manifest
// names resolves from here. The generated corpus is build/conformance-squirrel
// by default (make/squirrel.mk); $SCHEMA_SQUIRREL_CONFORMANCE overrides it, and
// the negative controls (test/conformance/squirrel/negative-control) point the
// same driver at a sabotaged copy. The driver answers every surface the harness
// asks (`list`); a case whose unit the target refuses is answered
// `<case>.absent`, and a writer's refusal BY NAME is the absence holding that
// name. One process per surface, one file per case, and nothing on stderr,
// which the driver script treats as a crash: a failure throws, it is never
// answered as an absence.

local argv = vargv;
if (argv.len() < 2) { return; }

// THE PINNED INTERPRETER IS PART OF THE LEG (docs/PORT-SQUIRREL.md, 64-BIT AND
// 128-BIT INTEGERS): make dist-squirrel builds sq64 with -DSQUSEDOUBLE, so a
// float is 8 bytes and the wire's f32/f64 bit patterns survive. A 4-byte-float
// sq truncates casti2f's 64-bit result, so every loaded float becomes zero and
// the save elides the field: the wire surface then answers short bodies instead
// of the corpus bytes. Refuse that interpreter by name rather than answering
// with wrong bytes.
if (_floatsize_ != 8) {
  throw "squirrel conformance: this interpreter has " + _floatsize_ +
        "-byte floats; run make dist-squirrel (SQUSEDOUBLE) and set SQ to its sq";
}

local manifest = argv[0];
local mode = argv[1];
local outdir = argv.len() >= 3 ? argv[2] : "";
local corpus = "build/conformance-squirrel";
local override = getenv("SCHEMA_SQUIRREL_CONFORMANCE");
if (override != null && override.len() > 0) corpus = override;

local exists = function(path) {
  try { local f = file(path, "rb"); f.close(); return true; }
  catch (e) { return false; }
};

local readBytes = function(path) {
  local f = file(path, "rb");
  local b = f.readblob(f.len());
  f.close();
  return b;
};

local readText = function(path) {
  local b = readBytes(path);
  local s = "";
  for (local i = 0; i < b.len(); i++) s += format("%c", b[i] & 0xff);
  return s;
};

local writeText = function(path, s) {
  local f = file(path, "wb");
  for (local i = 0; i < s.len(); i++) f.writen(s[i] & 0xff, 'b');
  f.close();
};

local writeBytes = function(path, b) {
  local f = file(path, "wb");
  for (local i = 0; i < b.len(); i++) f.writen(b[i] & 0xff, 'b');
  f.close();
};

local writeAbsent = function(name) {
  local f = file(outdir + "/" + name + ".absent", "wb");
  f.close();
};

local splitFields = function(line) {
  local out = [];
  local cur = "";
  local n = line.len();
  for (local i = 0; i <= n; i++) {
    local c = i < n ? line[i] : 32;
    if (c == 32 || c == 9 || c == 13 || i == n) {
      if (cur.len() > 0) { out.append(cur); cur = ""; }
    } else {
      cur += format("%c", c & 0xff);
    }
  }
  return out;
};

local manifestLines = function(s) {
  local out = [];
  local start = 0;
  local n = s.len();
  for (local i = 0; i < n; i++) {
    if (s[i] == 10) { out.append(s.slice(start, i)); start = i + 1; }
  }
  if (start < n) out.append(s.slice(start));
  return out;
};

local shq = function(s) {
  local out = "'";
  for (local i = 0; i < s.len(); i++) {
    if ((s[i] & 0xff) == 39) out += "'\\''";
    else out += format("%c", s[i] & 0xff);
  }
  return out + "'";
};

local baseName = function(path) {
  local cut = -1;
  local at;
  while ((at = path.find("/", cut + 1)) != null) cut = at;
  return path.slice(cut + 1);
};

// THE TABLE MODULE'S NAME. The target names it <Package>Tables, and the
// manifest key is not the package (unit scalars is package scalardemo), so the
// unit directory's *Tables.nut files name it. A unit that also contains a
// source file named Tables.schema emits a per-file Tables.nut beside the
// package module, so the file whose header says it is the TABLE surface is the
// one; zero or more than one is no module.
local tableModuleName = function(dir) {
  local tmp = getenv("TMPDIR");
  if (tmp == null || tmp.len() == 0) tmp = getenv("TMP");
  if (tmp == null || tmp.len() == 0) tmp = dir;
  local h = 2166136261;
  for (local i = 0; i < dir.len(); i++) h = ((h ^ (dir[i] & 0xff)) * 16777619) & 0xffffffff;
  local listing = tmp + "/schema-tables-" + format("%08x", h) + ".txt";
  local name = null;
  try {
    system("ls -1 -- " + shq(dir) + "/*Tables.nut > " + shq(listing) + " 2>/dev/null");
    local text = readText(listing);
    local lines = [];
    local start = 0;
    for (local i = 0; i < text.len(); i++) {
      if (text[i] == 10) {
        if (i > start) lines.append(text.slice(start, i));
        start = i + 1;
      }
    }
    if (start < text.len()) lines.append(text.slice(start));
    local found = 0;
    foreach (path in lines) {
      local leaf = baseName(path);
      if (leaf.len() <= 4 || leaf.slice(leaf.len() - 4) != ".nut") continue;
      local src = "";
      try {
        local g = file(path, "rb");
        local gn = g.len() < 512 ? g.len() : 512;
        local gb = gn > 0 ? g.readblob(gn) : blob(0);
        g.close();
        for (local k = 0; k < gb.len(); k++) src += format("%c", gb[k] & 0xff);
      } catch (e) { continue; }
      if (src.find("the TABLE surface") == null) continue;
      found++;
      name = leaf.slice(0, leaf.len() - 4);
    }
    if (found != 1) name = null;
  } catch (e) { name = null; }
  try { remove(listing); } catch (e) {}
  return name;
};

local newReport = function() {
  return { unknown = 0, kindMismatch = 0, widened = 0, clamped = 0, duplicate = 0, malformed = false, refused = false };
};

// One unit is opened once per process: the module loader and the table module
// are the same objects for every case of the unit, so a surface pays the
// dofile once rather than once a case.
local unitCache = {};
local openUnit = function(unit) {
  if (unit in unitCache) return unitCache[unit];
  local dir = corpus + "/" + unit;
  local module = tableModuleName(dir);
  if (module == null) throw "main.nut: " + unit + " has no single *Tables.nut with a TABLE SURFACE";
  local schema = dofile(dir + "/schema_module.nut", true);
  local opened = [schema, schema.require(module)];
  unitCache[unit] <- opened;
  return opened;
};

// A unit the target generated: the corpus directory carries its runtime.
local generated = function(unit) {
  return exists(corpus + "/" + unit + "/schema_table_load.nut");
};

// An instance the corpus carries on the wire alone (FORMAT.md, `no-text`).
local noText = function(f) { return f.len() > 5 && f[5] == "no-text"; };

local loadReport = function(unit, root, bytes) {
  local opened = openUnit(unit);
  local schema = opened[0];
  local T = opened[1];
  local L = schema.require("schema_table_load");
  local report = newReport();
  L.loadTolerant(T, root, bytes, report);
  local verdict = report.refused ? "refused" : "read";
  local m = report.malformed ? "true" : "false";
  return report.unknown.tostring() + "," + report.kindMismatch.tostring() + "," +
         report.widened.tostring() + "," + report.clamped.tostring() + "," +
         report.duplicate.tostring() + "," + m + "," + verdict + "\n";
};

if (mode == "list") {
  foreach (name in ["wire", "report", "json-read", "json-write", "json-hostile",
                     "cook", "cook-foreign", "block", "block-foreign", "block-dump",
                     "forgery", "cook-forgery", "cook-reason", "block-reason",
                     "cook-write", "message", "retain", "retain-save"]) print(name + "\n");
  return;
}

local lines = manifestLines(readText(manifest));

// THE EVOLUTION CLASS (reports.txt): the tolerant load's report, never the
// value. A unit the target refused is the one per-case absence; a crash is a
// failure, never an absence.
if (mode == "report") {
  foreach (ln in lines) {
    local f = splitFields(ln);
    if (f.len() < 5 || f[0] != "report") continue;
    local cname = f[1]; local unit = f[2]; local root = f[3]; local wirepath = f[4];
    if (!generated(unit)) { writeAbsent(cname); continue; }
    writeText(outdir + "/" + cname, loadReport(unit, root, readBytes(wirepath)));
  }
  return;
}

// LOAD THEN SAVE, the bytes. A writer that refuses the value BY NAME (§2.5:
// one blob named from two slots is `blob_shared`, refused by every writer)
// answers `<case>.absent` holding the name, which the harness scores as the
// row's named refusal; a nameless refusal is a failure.
if (mode == "wire") {
  foreach (ln in lines) {
    local f = splitFields(ln);
    if (f.len() < 5 || f[0] != "instance") continue;
    local cname = f[1]; local unit = f[2]; local root = f[3]; local wirepath = f[4];
    if (!generated(unit)) { writeAbsent(cname); continue; }
    local opened = openUnit(unit);
    local T = opened[1];
    local L = opened[0].require("schema_table_load");
    local S = opened[0].require("schema_table_save");
    local report = newReport();
    local v = L.loadTolerant(T, root, readBytes(wirepath), report);
    if (v == null || report.refused) throw cname + ": load refused " + wirepath;
    local out = S.save(T, root, v);
    if (out == null) {
      if (S.SaveReason == null) throw cname + ": save refused the value";
      writeText(outdir + "/" + cname + ".absent", S.SaveReason);
      continue;
    }
    if (S.measure(T, root, v) != out.len()) throw cname + ": measure is not the save's length";
    writeBytes(outdir + "/" + cname, out);
  }
  return;
}

// THE TEXT FORM (docs/SPEC-TABLES.md §16). json-read: FromJson the pinned text,
// Save, the bytes. json-write: Load the wire, ToJson, the text, and its
// ToJsonMeasure must be the text's length. Both go through the root's
// generated names (<Root>FromJson is the strict read: a clean instance text
// reads with nothing to refuse).
if (mode == "json-read" || mode == "json-write") {
  local write = mode == "json-write";
  foreach (ln in lines) {
    local f = splitFields(ln);
    if (f.len() < 5 || f[0] != "instance" || noText(f)) continue;
    local cname = f[1]; local unit = f[2]; local root = f[3]; local wirepath = f[4];
    local outname = write ? cname + ".json" : cname;
    if (!generated(unit)) { writeAbsent(outname); continue; }
    local opened = openUnit(unit);
    local T = opened[1];
    if (write) {
      local v = T[root + "Load"](readBytes(wirepath), newReport());
      if (v == null) throw cname + ": load refused " + wirepath;
      local text = T[root + "ToJson"](v);
      if (text == null) throw cname + ": ToJson refused the value";
      if (T[root + "ToJsonMeasure"](v) != text.len()) throw cname + ": ToJsonMeasure is not the text's length";
      writeText(outdir + "/" + outname, text);
    } else {
      local report = newReport();
      local v = T[root + "FromJson"](readBytes("testdata/conformance/tables/json/" + cname + ".json"), report);
      if (v == null) throw cname + ": FromJson refused the text: " + (("reason" in report) ? report.reason : "?");
      local out = T[root + "Save"](v);
      if (out == null) throw cname + ": save refused the value";
      if (T[root + "Measure"](v) != out.len()) throw cname + ": Measure is not the save's length";
      writeBytes(outdir + "/" + outname, out);
    }
  }
  return;
}

// THE TEXT FORM's hostile battery (§16.2, §16.3, §17.5): the TOLERANT read of
// <tree>/<root>.json, its report, or `refused` where the read was malformed.
if (mode == "json-hostile") {
  foreach (ln in lines) {
    local f = splitFields(ln);
    if (f.len() < 5 || f[0] != "json-hostile") continue;
    local cname = f[1]; local unit = f[2]; local root = f[3]; local tree = f[4];
    if (!generated(unit)) { writeAbsent(cname); continue; }
    local opened = openUnit(unit);
    local J = opened[0].require("schema_table_json");
    local r = newReport();
    J.FromJsonTolerant(opened[1], root, readBytes(tree + "/" + root + ".json"), r);
    local verdict = r.malformed ? "refused\n" :
      r.unknown + "," + r.kindMismatch + "," + r.widened + "," + r.clamped + "," + r.duplicate + ",false,read\n";
    writeText(outdir + "/" + cname, verdict);
  }
  return;
}

// =============================================================================
// THE ACCELERATORS: the block (§19) and the cook (§7). A Squirrel blob has no
// address, so the caller's BUFFER PLACEMENT travels as the Open's `lead`
// argument (the bytes past an aligned base) and `null` is no buffer at all.
// =============================================================================

// the block rows name an image, not a root: the image's ROOT is the one fixed
// fact every leg carries beside the manifest (test/conformance/go/blocks.go)
local blockRoots = { block_render = "RenderFrame", block_padded = "PaddedFrame" };
local blockRootOf = function(name) {
  foreach (prefix, root in blockRoots) {
    if (name.len() >= prefix.len() && name.slice(0, prefix.len()) == prefix) return root;
  }
  throw "main.nut: no block root for " + name;
};

// the unit and root of each cook CASE, read from the cook rows
local cookRows = {};
local blockRows = {};
foreach (ln in lines) {
  local f = splitFields(ln);
  if (f.len() >= 5 && f[0] == "cook") cookRows[f[1]] <- { unit = f[2], root = f[3], file = f[4] };
  if (f.len() >= 4 && f[0] == "block") blockRows[f[1]] <- { unit = f[2], root = blockRootOf(f[1]), file = f[3] };
}

// THE CLAIMED EXTENT: a buffer of exactly the claim, what fits copied, the rest
// zero; -1 is the file's own length.
local claim = function(data, extent) {
  if (extent < 0) return data;
  local out = blob(extent);
  local n = data.len() < extent ? data.len() : extent;
  for (local i = 0; i < n; i++) out[i] = data[i];
  return out;
};

// THE FOREIGN FILE: the MAGIC word at offset 0 reversed, which is what it reads
// as to a reader of the other byte order (test/conformance/README.md).
local foreign = function(data) {
  local out = blob(data.len());
  for (local i = 0; i < data.len(); i++) out[i] = data[i];
  if (out.len() >= 8) {
    for (local i = 0; i < 4; i++) { local t = out[i]; out[i] = out[7 - i]; out[7 - i] = t; }
  }
  return out;
};

local accelOpen = function(kind, unit, root, bytes, lead) {
  local T = openUnit(unit)[1];
  local report = newReport();
  local h = kind == "block" ? T.blockOpen(root, bytes, report, lead) : T.cookOpen(root, bytes, report, lead);
  if (h != null) return [h, null];
  if (!("reason" in report) || report.reason == null) throw "main.nut: a " + kind + " refusal names no reason";
  return [null, report.reason];
};

// the canonical dumps' little-endian words
local du = function(b, at, n) {
  local v = 0;
  for (local i = n - 1; i >= 0; i--) v = (v << 8) | (b[at + i] & 0xff);
  return v;
};
local ds = function(b, at, n) {
  local v = du(b, at, n);
  if (n < 8) {
    local bit = 1 << (n * 8 - 1);
    if ((v & bit) != 0) v -= (bit << 1);
  }
  return v;
};
local dumpText = function(b, at, used) {
  if (used < 0) used = 0;
  local out = "\"";
  for (local i = 0; i < used; i++) {
    local c = b[at + i] & 0xff;
    if (c >= 0x20 && c < 0x7f && c != 34 && c != 92) out += format("%c", c);
    else out += format("\\x%02x", c);
  }
  return out + "\" len=" + used;
};
local dumpScalar = function(b, at, storage, size) {
  if (storage == "Bool") return ((b[at] & 0xff) != 0) ? "true" : "false";
  if (storage == "Float") {
    if (size == 4) return format("0x%08x", du(b, at, 4));
    return format("0x%08x%08x", du(b, at + 4, 4), du(b, at, 4));
  }
  if (storage == "Signed") return format("%d", ds(b, at, size));
  return format("%d", du(b, at, size));
};

// THE BLOCK ROW DUMP (test/conformance/go/blockdump.go): the projection, then
// every out-of-line array's rows at their offsets
local blockRecord;
blockRecord = function(T, info, b, storage, path, out) {
  foreach (f in info.fields) {
    if (f.outOfLine) continue;
    local name = path.len() == 0 ? f.name : path + "." + f.name;
    if (f.counted) {
      local used = ds(b, storage + f.countOffset, 4);
      if (used < 0 || used > f.bound) throw "main.nut: " + info.name + "." + f.name + " carries a used length of " + used;
      out.append("  " + name + " = " + dumpText(b, storage + f.offset, used) + "\n");
    } else {
      local slots = f.isArray ? f.bound : 1;
      for (local sl = 0; sl < slots; sl++) {
        local where = f.isArray ? name + "[" + sl + "]" : name;
        local value = storage + f.offset + sl * f.elemSize;
        if (f.table != null) blockRecord(T, T.blockRecords[f.table], b, value, where, out);
        else out.append("  " + where + " = " + dumpScalar(b, value, f.storage, f.elemSize) + "\n");
      }
    }
    if (f.optional) out.append("  " + name + "#present = " + (((b[storage + f.presentOffset] & 0xff) != 0) ? "true" : "false") + "\n");
  }
};
local blockDump = function(T, h) {
  local b = h.Bytes;
  local form = h.Form;
  local out = [format("projection %s @0\n", form.projection.name)];
  blockRecord(T, form.projection, b, 0, "", out);
  foreach (a in form.arrays) {
    local offsetOf = du(b, a.offsetOfOffset, 8);
    local count = du(b, a.countOffset, 4);
    local stride = du(b, a.strideOffset, 4);
    local rowInfo = T.blockRecords[a.elem];
    out.append(format("array %s %s @%d count=%d stride=%d\n", a.name, rowInfo.name, offsetOf, count, stride));
    for (local r = 0; r < count; r++) {
      local at = offsetOf + r * stride;
      out.append(format("row %d @%d\n", r, at));
      blockRecord(T, rowInfo, b, at, "", out);
    }
  }
  local s = "";
  foreach (piece in out) s += piece;
  return s;
};

// THE COOK NODE DUMP (test/conformance/go/cooks.go): every node reached from
// the root in visit order, every field at its offset, every pointer by its
// self-relative delta
local cookNode, cookStorage;
cookNode = function(T, formName, b, off0, length, offset, seen, order, out) {
  if (offset < 0 || offset + T.cookForms[formName].size > length) throw "main.nut: node " + formName + " at " + offset + " does not fit " + length;
  if (offset in seen) {
    if (seen[offset] != formName) throw "main.nut: node " + offset + " is two tables";
    return;
  }
  seen[offset] <- formName;
  out.append(format("node %d %s @%d\n", order.len(), formName, offset));
  order.append(offset);
  cookStorage(T, formName, b, off0, length, offset, "", seen, order, out);
};
cookStorage = function(T, formName, b, off0, length, storage, path, seen, order, out) {
  local form = T.cookForms[formName];
  foreach (f in form.fields) {
    local name = path.len() == 0 ? f.name : path + "." + f.name;
    local used = -1;
    if (f.countOffset >= 0) used = ds(b, off0 + storage + f.countOffset, 4);
    if (f.isPointer) {
      local delta = ds(b, off0 + storage + f.offset, 8);
      if (delta == 0) { out.append("  " + name + " = null\n"); continue; }
      local target = storage + f.offset + delta;
      if (target < 0 || target >= length) throw "main.nut: " + name + " resolves outside the region";
      out.append(format("  %s = -> @%d\n", name, target));
      cookNode(T, f.table, b, off0, length, target, seen, order, out);
      continue;
    }
    local slots = f.isArray ? f.bound : 1;
    if (f.storage == "String" || f.storage == "Bytes") {
      out.append("  " + name + " = " + dumpText(b, off0 + storage + f.offset, used >= 0 ? used : 0) + "\n");
    } else if (f.storage == "Record") {
      for (local sl = 0; sl < slots; sl++) {
        local sp = f.isArray ? name + "[" + sl + "]" : name;
        cookStorage(T, f.table, b, off0, length, storage + f.offset + sl * f.elemSize, sp, seen, order, out);
      }
    } else {
      for (local sl = 0; sl < slots; sl++) {
        local sp = f.isArray ? name + "[" + sl + "]" : name;
        out.append("  " + sp + " = " + dumpScalar(b, off0 + storage + f.offset + sl * f.elemSize, f.storage, f.elemSize) + "\n");
      }
    }
    if (f.isArray && f.countOffset >= 0) out.append(format("  %s#count = %d\n", name, used));
    if (f.presentOffset >= 0) out.append("  " + name + "#present = " + (((b[off0 + storage + f.presentOffset] & 0xff) != 0) ? "true" : "false") + "\n");
  }
};
local cookDump = function(T, root, h) {
  local out = [];
  cookNode(T, root, h.Bytes, h.Region, h.Length, 0, {}, [], out);
  local s = "";
  foreach (piece in out) s += piece;
  return s;
};

// block, block-foreign: Open the image (its magic reversed for the foreign
// one), `open` or `refuse`. block-dump: the canonical row dump.
if (mode == "block" || mode == "block-foreign" || mode == "block-dump") {
  foreach (name, row in blockRows) {
    local data = readBytes(row.file);
    if (mode == "block-foreign") data = foreign(data);
    local got = accelOpen("block", row.unit, row.root, data, 0);
    if (mode == "block-dump") {
      if (got[0] == null) throw name + ": Open refused the image: " + got[1];
      writeText(outdir + "/" + name, blockDump(openUnit(row.unit)[1], got[0]));
    } else {
      writeText(outdir + "/" + name, got[0] != null ? "open\n" : "refuse\n");
    }
  }
  return;
}

// cook, cook-foreign: Open the cooked file at its root, the canonical node
// dump; the foreign one is the same file with its magic reversed.
if (mode == "cook" || mode == "cook-foreign") {
  foreach (name, row in cookRows) {
    local data = readBytes(row.file);
    if (mode == "cook-foreign") data = foreign(data);
    local got = accelOpen("cook", row.unit, row.root, data, 0);
    if (mode == "cook-foreign") {
      writeText(outdir + "/" + name, got[0] != null ? "open\n" : "refuse\n");
      continue;
    }
    if (got[0] == null) throw name + ": Open refused the cook: " + got[1];
    writeText(outdir + "/" + name, cookDump(openUnit(row.unit)[1], row.root, got[0]));
  }
  return;
}

// THE FORGERY BATTERIES: `forgery <name> <kind> <subject> <file> <extent>
// <pointer>`, the subject a block's name or a cook's root. forgery and
// cook-forgery answer `open` or `refuse`; the two -reason surfaces answer the
// refusal's name, or `ok`.
if (mode == "forgery" || mode == "cook-forgery" || mode == "block-reason" || mode == "cook-reason") {
  local kind = (mode == "forgery" || mode == "block-reason") ? "block" : "cook";
  local reasons = mode == "block-reason" || mode == "cook-reason";
  foreach (ln in lines) {
    local f = splitFields(ln);
    if (f.len() < 7 || f[0] != "forgery" || f[2] != kind) continue;
    local name = f[1]; local subject = f[3];
    local unit, root;
    if (kind == "block") {
      if (!(subject in blockRows)) throw name + ": no block row " + subject;
      unit = blockRows[subject].unit; root = blockRows[subject].root;
    } else {
      unit = null; root = subject;
      foreach (c in cookRows) if (c.root == subject) { unit = c.unit; break; }
      if (unit == null) throw name + ": no cook row opens " + subject;
    }
    local data = claim(readBytes(f[4]), f[5].tointeger());
    local lead = 0;
    if (f[6] == "null") data = null;
    else lead = f[6].tointeger();
    local got = accelOpen(kind, unit, root, data, lead);
    local answer;
    if (reasons) answer = got[0] != null ? "ok\n" : got[1] + "\n";
    else answer = got[0] != null ? "open\n" : "refuse\n";
    writeText(outdir + "/" + name, answer);
  }
  return;
}

// THE COOK WRITER (§7.6): Load the instance's wire, Cook it little-endian
// (order 1) and big-endian (order 2), the bytes as <instance> and
// <instance>-be. A buffer one byte short must refuse and write nothing. The
// expectation is the tool's file: `schema cook` is the reference.
if (mode == "cook-write") {
  local instances = {};
  foreach (ln in lines) {
    local f = splitFields(ln);
    if (f.len() >= 5 && f[0] == "instance") instances[f[1]] <- f;
  }
  foreach (ln in lines) {
    local f = splitFields(ln);
    if (f.len() < 2 || f[0] != "cook-write") continue;
    local name = f[1];
    if (!(name in instances)) throw name + ": cook-write names no instance";
    local inst = instances[name];
    local unit = inst[2]; local root = inst[3];
    if (!generated(unit)) { writeAbsent(name); writeAbsent(name + "-be"); continue; }
    local T = openUnit(unit)[1];
    local report = newReport();
    // the TOLERANT load, as the wire surface's: a blob two slots name
    // (blob_shared) is refused by the strict read by name, and the cook of the
    // value it holds is still the tool's file
    local v = openUnit(unit)[0].require("schema_table_load").loadTolerant(T, root, readBytes(inst[4]), report);
    if (v == null || report.malformed || report.refused) throw name + ": the cook input did not load";
    local size = T.CookMeasure(root, v);
    if (size < 1) throw name + ": CookMeasure refused the value";
    foreach (order in [1, 2]) {
      local short = blob(size - 1);
      for (local i = 0; i < short.len(); i++) short[i] = 0xa5;
      if (T.Cook(root, v, short, order)) throw name + ": a short buffer cooked";
      for (local i = 0; i < short.len(); i++) if ((short[i] & 0xff) != 0xa5) throw name + ": a short buffer was written to";
      local buf = blob(size);
      if (!T.Cook(root, v, buf, order)) throw name + ": Cook refused the value";
      writeBytes(outdir + "/" + name + (order == 2 ? "-be" : ""), buf);
    }
  }
  return;
}

// THE MESSAGE FORM (§3.3): AnnounceRead the connection's announcement into one
// direction's vocabulary, LoadMessages the batch against it into a storage of
// the form's batch maximum, then MeasureMessages and SaveMessages, the bytes.
// A variable table has no message form (§3.3): that row is absent by name.
if (mode == "message") {
  local connections = {};
  foreach (ln in lines) {
    local f = splitFields(ln);
    if (f.len() >= 5 && f[0] == "connection") connections[f[1]] <- { unit = f[2], file = f[4] };
  }
  foreach (ln in lines) {
    local f = splitFields(ln);
    if (f.len() < 6 || f[0] != "message") continue;
    local name = f[1]; local root = f[3];
    if (!(f[2] in connections)) throw name + ": no connection " + f[2];
    local conn = connections[f[2]];
    if (f[5] == "-") {
      // a named refusal row: the root is a variable table (§3.3)
      writeText(outdir + "/" + name + ".absent", "variable table: the message form is fixed tables only (SPEC-TABLES §3.3)");
      continue;
    }
    if (!generated(conn.unit)) { writeAbsent(name); continue; }
    local T = openUnit(conn.unit)[1];
    if (!T.Descriptors[root].fixed) {
      writeText(outdir + "/" + name + ".absent", "variable table: the message form is fixed tables only (SPEC-TABLES §3.3)");
      continue;
    }
    local v = {};
    local ar = {};
    if (!T.AnnounceRead(v, readBytes(conn.file), ar)) throw name + ": AnnounceRead refused: " + (("reason" in ar) ? ar.reason : "?");
    local storage = array(T.TableMessageBatchMax);
    for (local i = 0; i < storage.len(); i++) storage[i] = T[root]();
    local lr = {};
    local count = T[root + "LoadMessages"](storage, v, readBytes(f[5]), lr);
    if (count < 1 || lr.malformed || (("refused" in lr) && lr.refused)) throw name + ": LoadMessages answered " + count;
    local values = storage.slice(0, count);
    local size = T[root + "MeasureMessages"](values, {});
    if (size < 0) throw name + ": MeasureMessages refused";
    local out = blob(size);
    if (T[root + "SaveMessages"](values, out, {}) != size) throw name + ": SaveMessages did not write the measure";
    writeBytes(outdir + "/" + name, out);
  }
  return;
}

// RETAIN-UNKNOWN (§6.6): LoadRetain the wire at the row's two capacities,
// MeasureRetain, SaveRetain. `retain` prints the load's counters and the
// retain_lost the save adds, `<retained>,<retain_lost>,<unknown> <save lost>`;
// `retain-save` writes the saved bytes. `short` is one byte short of the last
// record: load once at a roomy capacity, read what the store used, and load
// again one byte under it. The id list's capacity is a count.
if (mode == "retain" || mode == "retain-save") {
  local saves = mode == "retain-save";
  foreach (ln in lines) {
    local f = splitFields(ln);
    if (f.len() < 2 || f[0] != "retain") continue;
    local name = f[1];
    if (f.len() < 7) throw name + ": a retain row takes unit, root, wire and two capacities";
    local unit = f[2]; local root = f[3];
    if (!generated(unit)) { writeAbsent(name); continue; }
    local T = openUnit(unit)[1];
    local wire = readBytes(f[4]);
    local capacity = 1 << 20;
    local idCapacity = f[6] == "full" ? (1 << 17) : f[6].tointeger();
    local store = { capacity = capacity, idCapacity = idCapacity };
    local report = newReport();
    if (f[5] == "short") {
      if (T[root + "LoadRetain"](wire, store, report) == null) throw name + ": the capacity probe refused";
      capacity = store.used - 1;
      if (capacity < 0) capacity = 0;
      store = { capacity = capacity, idCapacity = idCapacity };
      report = newReport();
    } else if (f[5] != "full") {
      throw name + ": capacity " + f[5] + " is neither full nor short";
    }
    local v = T[root + "LoadRetain"](wire, store, report);
    if (v == null || report.malformed) throw name + ": LoadRetain failed";
    local retained = report.retained, lost = report.retainLost, unknown = report.unknown;
    local n = T[root + "MeasureRetain"](v, store);
    if (n < 0) throw name + ": MeasureRetain refused";
    local out = blob(n);
    if (T[root + "SaveRetain"](v, store, out, report) != n) throw name + ": SaveRetain did not write the measure";
    local saveLost = report.retainLost - lost;
    local again = blob(n);
    local second = newReport();
    if (T[root + "SaveRetain"](v, store, again, second) != n || second.retainLost != saveLost) throw name + ": the save is not repeatable";
    for (local i = 0; i < n; i++) if (again[i] != out[i]) throw name + ": the save is not repeatable";
    if (saves) writeBytes(outdir + "/" + name, out);
    else writeText(outdir + "/" + name, retained + "," + lost + "," + unknown + " " + saveLost + "\n");
  }
  return;
}

// Every surface main.nut answers is named in `list` and returned above; the
// driver script asks `list` before a surface and answers exit 2 for the rest.
throw "main.nut: no surface " + mode;
