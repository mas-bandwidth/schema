// THE FIXED ROOTS (docs/SPEC-TABLES.md §3.4, §6.5).
//
// A root declared `fixed table` is a form 3 file: the one read of it is the
// generated <Root>FixedLoad, which names a form 1 file previous_form, and its
// write is <Root>FixedSave. The corpus holds every fixed root's positive
// instances in form 3 (testdata/conformance/tables/wire/), so the surfaces
// that ask for the read (wire, report, json-write) and the write (wire,
// report, json-read, json-hostile, framing) take the fixed form here, as the
// Go leg's test/conformance/go/fixed.go does.
//
// THIS LEG HOLDS A FIXED VALUE IN A DIFFERENT SHAPE FROM A VARIABLE ONE. The
// fixed form fills a preallocated class (a string is its bytes and a length,
// an array its storage and a count, a union its tag and every arm, an
// optional a present flag); the variable form's value, which carries the §16
// text, is a plain object (a string is a string, an array its elements, a
// union {arm, value}, an absent optional null). The two are bridged here,
// field by field, from the unit's generated VIEW (docs/SPEC-TABLES.md §8), so
// the value the fixed read leaves is written as text by the generated ToJson
// and the value the text read leaves is written as form 3 by the generated
// FixedSave. The bridge only moves values: it never repairs one, and a value
// the bridge cannot place (a string over its bound, an enum name the unit
// does not declare) is a failure by name.
import { existsSync } from "node:fs";

const utf8 = new TextDecoder("utf-8", { fatal: true });
const utf8Out = new TextEncoder();

// Every fixed root the corpus carries, and the module its fixed codec is in.
// The value classes live in the unit's home <Pkg>Table.js, the view in its
// <Pkg>View.js, both beside the unit's <Pkg>Variable.js.
export const fixedRoots = [
  ["tabledemo", "RootConfig", "examples/TablesTable.js"],
  ["tabledemo", "ProfileConfig", "examples/TablesTable.js"],
  ["tabledemo", "LoadoutConfig", "examples/TablesTable.js"],
  ["tabledemo", "ArchiveConfig", "examples/NestedTable.js"],
  ["tabledemo", "PackConfig", "examples/PackTable.js"],
  ["tabledemo", "KeyedConfig", "examples/KeyedTable.js"],
  ["tblv1", "Cfg", "v1/V1Table.js"],
  ["tblv2", "Cfg", "v2/V2Table.js"],
  ["tblr1", "Cfg", "r1/R1Table.js"],
  ["tblr2", "Cfg", "r2/R2Table.js"],
  ["tblp1", "Chain", "p1/P1Table.js"],
  ["tblp3", "Chain", "p3/P3Table.js"],
  ["tblm1", "Msg", "m1/M1Table.js"],
  ["tblm2", "Msg", "m2/M2Table.js"],
  ["listdemo", "Bounded", "lists/MigrateTable.js"],
  ["scalars", "SimState", "scalars/ScalarsTable.js"],
  ["tblscalars2", "SimState", "scalars2/Scalars2Table.js"],
  ["messagedemo", "ToolMessage", "messages/MessagesTable.js"],
  ["backenddemo", "LoginRequest", "backend/BackendTable.js"],
  ["backenddemo", "MatchResult", "backend/BackendTable.js"],
  ["backenddemo", "StorePurchase", "backend/BackendTable.js"],
  ["backenddemo", "Envelope", "backend/BackendTable.js"],
  ["vocabdemo", "Wide00", "vocab/VocabTable.js"],
  ["vocabdemo", "Wide09", "vocab/VocabTable.js"],
  ["vocab9demo", "Wide00", "vocab9/Vocab9Table.js"],
  ["vocab9demo", "Wide19", "vocab9/Vocab9Table.js"],
  ["widedemo", "Caption", "wide/CaptionTable.js"],
  ["widedemo", "Stamp", "wide/CaptionTable.js"],
];

export function isFixedRoot(unit, root) {
  return fixedRoots.some((r) => r[0] === unit && r[1] === root);
}

const camel = (name) => name.replace(/_([a-z0-9])/g, (_, c) => c.toUpperCase());

function bad(where, why) {
  throw new Error("fixed bridge: " + where + ": " + why);
}

// the property a fixed class holds a field in, which must exist: a field the
// class does not carry is a bridge that does not know the unit, never a skip
function prop(fx, f, suffix, where) {
  const p = camel(f.name) + suffix;
  if (!(p in fx)) { bad(where, "the fixed value has no property " + p); }
  return p;
}

// ---- fixed value -> variable value

function enumName(voc, v, where) {
  for (const x of voc.variants) {
    if (x.value === v) { return x.value === 0 ? null : x.name; }
  }
  bad(where, "enum " + voc.name + " has no variant " + String(v));
}

function scalarToVar(f, v, where) {
  const voc = f.vocabulary;
  if (voc !== null && voc.kind === "enum") { return enumName(voc, v, where); }
  if (voc !== null && voc.kind === "flags") { return BigInt(v); }
  if (voc !== null && voc.kind === "union") {
    if (v.type === 0) { return null; }
    const arm = voc.variants.find((x) => x.value === v.type);
    if (arm === undefined) { bad(where, "union " + voc.name + " has no arm " + String(v.type)); }
    // an arm that carries one field (a string, a scalar, a list) holds it on
    // the union's object as a table holds a field
    if (arm.field !== null) { return { arm: arm.name, value: toVar({ fields: [arm.field] }, v, where)[arm.field.name] }; }
    if (arm.payload === null) { return { arm: arm.name, value: null }; }
    const key = camel(arm.name);
    if (!(key in v)) { bad(where, "the fixed union has no arm property " + key); }
    return { arm: arm.name, value: toVar(arm.payload, v[key], where + "." + arm.name) };
  }
  if (f.table !== null) { return toVar(f.table, v, where); }
  return v;
}

function textToVar(f, bytes, n, where) {
  if (f.kind === 33) { return String.fromCharCode(...bytes.subarray(0, n)); }
  try { return utf8.decode(bytes.subarray(0, n)); } catch { bad(where, "a string that is not UTF-8"); }
}

export function toVar(t, fx, where) {
  const out = {};
  for (const f of t.fields) {
    const w = where + "." + f.name;
    if (f.optional && !fx[prop(fx, f, "Present", w)]) { out[f.name] = null; continue; }
    const p = prop(fx, f, "", w);
    const val = fx[p];
    if (f.kind === 12 || f.kind === 33) {
      out[f.name] = textToVar(f, val, fx[prop(fx, f, "Length", w)], w);
    } else if (f.isArray && f.shape === "value") {
      // bytes(N): its storage and its length
      out[f.name] = val.slice(0, fx[prop(fx, f, "Length", w)]);
    } else if (f.isArray) {
      const n = f.counted ? fx[prop(fx, f, "Count", w)] : val.length;
      const a = new Array(n);
      for (let i = 0; i < n; i++) { a[i] = scalarToVar(f, val[i], w + "[" + i + "]"); }
      out[f.name] = a;
    } else {
      out[f.name] = scalarToVar(f, val, w);
    }
  }
  return out;
}

// ---- variable value -> fixed value (the fixed value starts at its Init)

function enumValue(voc, name, where) {
  if (name === null || name === undefined) { return 0; }
  for (const x of voc.variants) { if (x.name === name) { return x.value; } }
  bad(where, "enum " + voc.name + " has no variant " + String(name));
}

// scalarToFixed answers the slot's new value; a table or union fills the
// slot's object in place and answers it
function scalarToFixed(f, v, slot, where) {
  const voc = f.vocabulary;
  if (voc !== null && voc.kind === "enum") { return enumValue(voc, v, where); }
  if (voc !== null && voc.kind === "flags") { return typeof slot === "bigint" ? BigInt(v) : Number(v); }
  if (voc !== null && voc.kind === "union") {
    if (v === null || v === undefined) { slot.type = 0; return slot; }
    const arm = voc.variants.find((x) => x.name === v.arm);
    if (arm === undefined) { bad(where, "union " + voc.name + " has no arm " + String(v.arm)); }
    slot.type = arm.value;
    if (arm.field !== null) {
      toFixed({ fields: [arm.field] }, { [arm.field.name]: v.value }, slot, where);
    } else if (arm.payload !== null) {
      const key = camel(arm.name);
      if (!(key in slot)) { bad(where, "the fixed union has no arm property " + key); }
      toFixed(arm.payload, v.value, slot[key], where + "." + arm.name);
    }
    return slot;
  }
  if (f.table !== null) { toFixed(f.table, v, slot, where); return slot; }
  return v;
}

function textToFixed(f, s, bytes, where) {
  let units;
  if (f.kind === 33) {
    units = new Uint16Array(s.length);
    for (let i = 0; i < s.length; i++) { units[i] = s.charCodeAt(i); }
  } else {
    units = utf8Out.encode(s);
  }
  if (units.length > bytes.length) { bad(where, "a string over its bound"); }
  bytes.fill(0);
  bytes.set(units);
  return units.length;
}

export function toFixed(t, vv, fx, where) {
  for (const f of t.fields) {
    const w = where + "." + f.name;
    const val = vv[f.name];
    if (f.optional) {
      const present = prop(fx, f, "Present", w);
      fx[present] = val !== null && val !== undefined;
      if (!fx[present]) { continue; }
    }
    const p = prop(fx, f, "", w);
    if (f.kind === 12 || f.kind === 33) {
      fx[prop(fx, f, "Length", w)] = textToFixed(f, val, fx[p], w);
    } else if (f.isArray && f.shape === "value") {
      if (val.length > fx[p].length) { bad(w, "bytes over their bound"); }
      fx[p].fill(0);
      fx[p].set(val);
      fx[prop(fx, f, "Length", w)] = val.length;
    } else if (f.isArray) {
      if (val.length > fx[p].length) { bad(w, "an array over its bound"); }
      if (!f.counted && val.length !== fx[p].length) { bad(w, "a fixed-length array of " + val.length); }
      for (let i = 0; i < val.length; i++) { fx[p][i] = scalarToFixed(f, val[i], fx[p][i], w + "[" + i + "]"); }
      if (f.counted) { fx[prop(fx, f, "Count", w)] = val.length; }
    } else {
      fx[p] = scalarToFixed(f, val, fx[p], w);
    }
  }
  return fx;
}

// fixedCodec rebinds a variable codec (main.mjs codecOf) to the fixed form: the
// read is <Root>FixedLoad into one fresh value, accepting a file of exactly one
// record, and the write is <Root>FixedSave of that one record. The value it
// hands the rest of the row is the VARIABLE value, so ToJson and FromJson stay
// the unit's own; `variableLoad` keeps the variable form's read for the
// framing vectors, which are form 1 bytes read at a fixed root.
export async function fixedCodec(generated, load, unit, root, variablePath, codec) {
  const row = fixedRoots.find((r) => r[0] === unit && r[1] === root);
  if (row === undefined) { return codec; }
  const home = variablePath.replace(/Variable\.js$/, "");
  for (const p of [row[2], home + "Table.js", home + "View.js"]) {
    if (!existsSync(generated + "/" + p)) { bad(unit + "." + root, "the module " + p + " is missing"); }
  }
  const F = await load(row[2]);
  const T = await load(home + "Table.js");
  const t = (await load(home + "View.js")).TableUnitView.descriptors[root];
  const Class = T[root], init = T["Init" + root];
  const fixedLoad = F[root + "FixedLoad"], fixedSave = F[root + "FixedSave"], fixedMeasure = F[root + "FixedMeasure"];
  for (const [name, fn] of [["class", Class], ["Init", init], ["FixedLoad", fixedLoad], ["FixedSave", fixedSave], ["FixedMeasure", fixedMeasure]]) {
    if (typeof fn !== "function") { bad(unit + "." + root, "the fixed surface has no " + name); }
  }
  if (t === undefined) { bad(unit + "." + root, "the view has no descriptor"); }
  const plan = F[root + "FixedNewPlan"]();
  const fresh = () => { const x = new Class(); init(x); return x; };
  return {
    ...codec,
    fixed: true,
    variableLoad: codec.load,
    variableReport: codec.report,
    report: () => new T.TableFixedReport(),
    // the fixed read: the value, or null with the refusal on the report
    load: (wire, r) => {
      const x = fresh();
      const n = fixedLoad([x], 1, wire, wire.length, plan, r);
      if (n !== 1 || r.refused !== 0 || r.damaged) {
        if (n === 0 && r.refused === 0 && !r.damaged) { r.damaged = true; }
        return null;
      }
      return toVar(t, x, root);
    },
    // the fixed write of a variable value, or null where it refuses
    save: (v) => {
      const x = toFixed(t, v, fresh(), root);
      const out = new Uint8Array(fixedMeasure(1));
      return fixedSave([x], 1, out) === out.length ? out : null;
    },
  };
}
