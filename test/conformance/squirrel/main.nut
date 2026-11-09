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
  // an empty file is an empty blob: readblob(0) raises "no data left to read"
  local b = f.len() == 0 ? blob(0) : f.readblob(f.len());
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
  return { unknown = 0, mismatch = 0, widened = 0, oor = 0, repeated = 0, bodyDamaged = false, refused = false };
};

// THE STRICT READ (docs/SPEC-TABLES.md §6.5, docs/plans/strict-only.md). One
// call returns the whole value or null. The report carries the refusal's
// name. loadWalk keeps a recovered value; these surfaces call load.
local strictLoad = function(opened, root, bytes) {
  local report = newReport();
  local v = opened[0].require("schema_table_load").load(opened[1], root, bytes, report);
  return [v, report];
};

// The schema name's storage member, read off the descriptor.
local memberOf = function(T, root, schemaName) {
  foreach (fd in T.Descriptors[root].fields) {
    if (fd.name == schemaName) return fd.member;
  }
  return null;
};

// An accepted read writes `read`, the §16 text, and <case>.wire, the save of
// that value. A refusal writes `refused` and no wire.

// FRAMING's form-1 file at a fixed root is the variable read, and the save
// that follows is this build's form 3 (docs/LAYERS/1-framing.md). ReadStrict
// answers previous_form for that form byte. Clear fixed around the strict
// read, then put the descriptor back before the save.
local framingLoad = function(opened, root, bytes) {
  local T = opened[1];
  local d = T.Descriptors[root];
  local fixedReader = d.fixed && ((root + "FixedLoad") in T);
  if (!(fixedReader && bytes != null && bytes.len() >= 1 && (bytes[0] & 0xff) == 1)) {
    return strictLoad(opened, root, bytes);
  }
  d.fixed = false;
  local got;
  try {
    got = strictLoad(opened, root, bytes);
  } catch (e) {
    d.fixed = true;
    throw e;
  }
  d.fixed = true;
  return got;
};

local writeReadBack = function(T, root, v, cname) {
  if (v == null) {
    writeText(outdir + "/" + cname, "refused\n");
    return;
  }
  local json = T[root + "ToJson"](v);
  if (json == null) throw cname + ": ToJson refused the value";
  local n = T[root + "Measure"](v);
  local out = T[root + "Save"](v);
  if (n < 0 || out == null || out.len() != n) throw cname + ": the write-back wire does not match the measure";
  writeBytes(outdir + "/" + cname + ".wire", out);
  writeText(outdir + "/" + cname, "read\n" + json);
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

if (mode == "list") {
  foreach (name in ["wire", "report", "framing", "json-read", "json-write", "json-hostile",
                     "cook", "cook-foreign", "block", "block-foreign", "block-dump",
                     "forgery", "cook-forgery", "cook-reason", "block-reason",
                     "cook-write", "message", "retain", "retain-save"]) print(name + "\n");
  return;
}

local lines = manifestLines(readText(manifest));

// THE EVOLUTION CLASS (reports.txt) and THE FRAMING VECTORS
// (docs/LAYERS/1-framing.md): the strict read, then `read` and the §16 text,
// or `refused` and nothing else. An accepted read also writes <case>.wire,
// the canonical save of that value. A unit the target refused is the one
// per-case absence; a crash is a failure, never an absence.
if (mode == "report" || mode == "framing") {
  foreach (ln in lines) {
    local f = splitFields(ln);
    if (f.len() < 5 || f[0] != mode) continue;
    local cname = f[1]; local unit = f[2]; local root = f[3]; local wirepath = f[4];
    if (!generated(unit)) { writeAbsent(cname); continue; }
    local opened = openUnit(unit);
    local bytes = readBytes(wirepath);
    local got = mode == "framing" ? framingLoad(opened, root, bytes) : strictLoad(opened, root, bytes);
    writeReadBack(opened[1], root, got[0], cname);
  }
  return;
}

// LOAD THEN SAVE, the bytes. The strict read refuses a file whose two slots
// name one blob (§2.5, blob_shared). That row answers <case>.absent holding
// the name. Any other refusal of a corpus instance is a failure. A writer
// that refuses by name answers the same absence; a nameless refusal is a
// failure.
if (mode == "wire") {
  foreach (ln in lines) {
    local f = splitFields(ln);
    if (f.len() < 5 || f[0] != "instance") continue;
    local cname = f[1]; local unit = f[2]; local root = f[3]; local wirepath = f[4];
    if (!generated(unit)) { writeAbsent(cname); continue; }
    local opened = openUnit(unit);
    local T = opened[1];
    local S = opened[0].require("schema_table_save");
    local got = strictLoad(opened, root, readBytes(wirepath));
    local v = got[0];
    local report = got[1];
    if (v == null) {
      local shared = report.reason == "blob_shared" || (("sharedBlob" in report) && report.sharedBlob > 0);
      if (shared) {
        writeText(outdir + "/" + cname + ".absent", "blob_shared");
        continue;
      }
      throw cname + ": load refused " + wirepath;
    }
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

// THE TEXT FORM (docs/SPEC-TABLES.md §16). json-read: FromJson, the strict
// text read, of the pinned text, then Save, the bytes. json-write: the
// strict load of the wire, ToJson, the text, and its ToJsonMeasure must be
// the text's length.
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
      local got = strictLoad(opened, root, readBytes(wirepath));
      local v = got[0];
      if (v == null) throw cname + ": load refused " + wirepath;
      local text = T[root + "ToJson"](v);
      if (text == null) throw cname + ": ToJson refused the value";
      if (T[root + "ToJsonMeasure"](v) != text.len()) throw cname + ": ToJsonMeasure is not the text's length";
      writeText(outdir + "/" + outname, text);
    } else {
      local report = newReport();
      local J = opened[0].require("schema_table_json");
      local v = J.FromJson(T, root, readBytes("testdata/conformance/tables/json/" + cname + ".json"), report);
      if (v == null || report.bodyDamaged) throw cname + ": FromJson refused the text";
      local out = T[root + "Save"](v);
      if (out == null) throw cname + ": save refused the value";
      if (T[root + "Measure"](v) != out.len()) throw cname + ": Measure is not the save's length";
      writeBytes(outdir + "/" + outname, out);
    }
  }
  // WRITE-REFUSAL (testdata/conformance/tables/FORMAT.md): the wire loaded at
  // the root, then the damage put into storage in code, which no read can
  // deliver: `narrow:<field>` sets that top-level text field to the bytes
  // 61 FF 62 (a Squirrel string holds those exact bytes), and `wide:<field>`
  // sets that wide field to the units 0061 D800 0062. The writer must refuse,
  // measure and write alike, and the answer is `refused`; a text it wrote
  // instead is the answer, which the harness scores red.
  if (write) foreach (ln in lines) {
    local f = splitFields(ln);
    if (f.len() < 6 || f[0] != "json-write-refusal") continue;
    local cname = f[1]; local unit = f[2]; local root = f[3]; local wirepath = f[4]; local damage = f[5];
    local outname = cname + ".json";
    if (!generated(unit)) { writeAbsent(outname); continue; }
    local opened = openUnit(unit);
    local T = opened[1];
    local got = strictLoad(opened, root, readBytes(wirepath));
    local v = got[0];
    if (v == null) throw cname + ": load refused " + wirepath;
    if (damage != "none") {
      local colon = damage.find(":");
      if (colon == null) throw cname + ": damage " + damage + " names no field";
      local kind = damage.slice(0, colon);
      local name = damage.slice(colon + 1);
      local member = memberOf(T, root, name);
      if (member == null || !(member in v)) throw cname + ": damage " + damage + " names no text field of " + root;
      if (kind == "narrow") v[member] = format("%c%c%c", 0x61, 0xff, 0x62);
      else if (kind == "wide") v[member] = [0x61, 0xd800, 0x62];
      else throw cname + ": unknown damage " + damage;
    }
    local answer = "refused\n";
    local size = T[root + "ToJsonMeasure"](v);
    if (size >= 0) {
      answer = T[root + "ToJson"](v);
      if (answer == null || answer.len() != size) throw cname + ": ToJson did not write ToJsonMeasure's answer";
    } else if (T[root + "ToJson"](v) != null) {
      throw cname + ": ToJsonMeasure refused and ToJson wrote";
    }
    writeText(outdir + "/" + outname, answer);
  }
  return;
}

// THE TEXT FORM's hostile battery (§16.2, §16.3, §17.5): FromJson, the
// strict text read. `read` and the §16 text, plus <case>.wire, or `refused`
// and no wire.
if (mode == "json-hostile") {
  foreach (ln in lines) {
    local f = splitFields(ln);
    if (f.len() < 5 || f[0] != "json-hostile") continue;
    local cname = f[1]; local unit = f[2]; local root = f[3]; local tree = f[4];
    if (!generated(unit)) { writeAbsent(cname); continue; }
    local opened = openUnit(unit);
    local J = opened[0].require("schema_table_json");
    local report = newReport();
    local v = J.FromJson(opened[1], root, readBytes(tree + "/" + root + ".json"), report);
    writeReadBack(opened[1], root, v, cname);
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

// THE COOK WRITER (§7.6). The derived line is
// `cook-write <instance> <JSON input or -> <source or -> <alias or ->`.
// Text input is FromJson; a dash is the strict load of the instance wire.
// A share pair points the alias slot at the source slot's blob. Cook the
// value little-endian (order 1) and big-endian (order 2). A buffer one byte
// short must refuse and write nothing.
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
    local opened = openUnit(unit);
    local T = opened[1];
    local input = f.len() > 2 ? f[2] : "-";
    local source = f.len() > 3 ? f[3] : "-";
    local alias = f.len() > 4 ? f[4] : "-";
    local v;
    if (input != "-") {
      local J = opened[0].require("schema_table_json");
      local report = newReport();
      v = J.FromJson(T, root, readBytes(input), report);
      if (v == null) throw name + ": cook input text did not read";
    } else {
      local got = strictLoad(opened, root, readBytes(inst[4]));
      v = got[0];
      if (v == null) throw name + ": cook input did not load";
    }
    if (source != "-") {
      local a = memberOf(T, root, source);
      local b = memberOf(T, root, alias);
      if (a == null || b == null || !(a in v) || !(b in v)) throw name + ": the share recipe " + source + ":" + alias + " did not apply";
      v[b] = v[a];
    }
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
    if (count < 1 || lr.bodyDamaged || (("refused" in lr) && lr.refused)) throw name + ": LoadMessages answered " + count;
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
// MeasureRetain, SaveRetain. `retain` prints the one read's verdict and the
// §16 text of the known value (`read`), or `refused`. `retain-save` writes
// the saved bytes. `short` is one byte short of the last record: load once
// at a roomy capacity, read what the store used, and load again one byte
// under it. The id list's capacity is a count.
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
    if (v == null || report.bodyDamaged || report.refused) {
      if (!saves) writeText(outdir + "/" + name, "refused\n");
      continue;
    }
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
    else {
      local json = T[root + "ToJson"](v);
      if (json == null) throw name + ": ToJson refused the value";
      writeText(outdir + "/" + name, "read\n" + json);
    }
  }
  return;
}

// Every surface main.nut answers is named in `list` and returned above; the
// driver script asks `list` before a surface and answers exit 2 for the rest.
throw "main.nut: no surface " + mode;
